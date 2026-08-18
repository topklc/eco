// Package testsrv is a scripted fake IRC server for eco's offline test
// suite. It speaks the protocol over a net.Pipe, so a real Session can be
// driven end to end with no network, no sleeps and no wall-clock dependence.
//
// # Wiring
//
// The server hands out a dialer; the Session under test takes it through its
// test seam (see app.Session.Dial), and every byte the client writes lands in
// the server's line queue:
//
//	srv := testsrv.New(t)
//	sess := app.NewSession(cfg, ui)
//	sess.Dial = srv.Dial      // the seam
//	sess.Connect()
//	srv.Register("eco")       // CAP negotiation, 001-005, ISUPPORT, MOTD
//
// # Driving a session
//
//   - Send writes a line to the client. Any line, at any time: that is also
//     the way to inject unsolicited traffic mid-test.
//   - Expect asserts on the next line the client sends.
//   - WaitFor scans the queued lines for the first match and removes it,
//     leaving the rest queued — so waiting for one thing never eats the
//     lines a later assertion needs.
//   - Script and Replay run a whole transcript as an ordered list of steps.
//
// Patterns are literal text with '*' as a wildcard, matched against a prefix
// of the line: "JOIN #eco" matches "JOIN #eco", "PRIVMSG #eco :*" matches any
// message to the channel, and a leading '*' matches anywhere in the line. No
// regexp escaping games with lines full of ':' and '#'.
//
// # Determinism
//
// Every wait is bounded by Timeout and fails the test loudly with the lines
// that were actually queued; nothing sleeps. To assert on session state after
// sending, use Sync: it PINGs the client and waits for the PONG. ircevent
// dispatches callbacks synchronously in its read loop, so once the PONG comes
// back every line sent before it has been fully processed by the session.
// That is the barrier that makes state assertions deterministic:
//
//	srv.Send("@msgid=1 :alice!a@h PRIVMSG #eco :hi")
//	srv.Sync()
//	// the line is in the buffer now, guaranteed
//
// # Rules
//
// Expect, WaitFor, Send, Register, Script, Replay and Sync call t.Fatalf on
// failure, so call them from the test goroutine. The server closes the
// connection when the client sends QUIT, the way a real server does, which
// makes teardown immediate. New registers its own t.Cleanup.
package testsrv

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// DefaultTimeout bounds every wait in the harness. Tests are local and
// synchronous, so this is a "something is wrong" budget, not a latency one.
const DefaultTimeout = 5 * time.Second

var errTimeout = errors.New("timed out")

// Server is a scripted IRC server. Create one with New; the exported fields
// are configuration and must be set before the client connects.
type Server struct {
	t *testing.T

	// Host is the server name used as the source prefix of generated lines.
	Host string
	// Timeout bounds every Expect, WaitFor, Send and Sync.
	Timeout time.Duration
	// Caps, when non-nil, is the exact capability set advertised in CAP LS
	// and acknowledged; anything else the client requests is NAKed. nil
	// advertises DefaultCaps and ACKs whatever is requested.
	Caps []string
	// ISupport, when non-nil, replaces DefaultISupport in the 005 burst.
	ISupport []string
	// Barrier, when set, is run by Sync after the PONG comes back, and must
	// return once the client has finished folding in every line sent before it.
	//
	// It exists because Sync's promise ("everything sent before this call has
	// been processed") is only self-evident for a client that processes lines on
	// the goroutine that answers the PING. A client that hands its decoded
	// events to another goroutine — which is what internal/app's Session does
	// since the v2 cutover — has to close that gap itself, so it supplies the
	// second half of the barrier here. Sync then repeats the PING/PONG round
	// trip, so anything the client wrote while the barrier ran is queued before
	// Sync returns and Pending() sees it.
	Barrier func()

	dialed chan *conn

	mu     sync.Mutex
	cur    *conn
	all    []*conn
	closed bool
	nick   string
	seq    int
}

// New returns a server bound to t. Teardown is registered with t.Cleanup:
// every connection is closed and later dials are refused.
func New(t *testing.T) *Server {
	t.Helper()
	s := &Server{
		t:       t,
		Host:    "irc.test",
		Timeout: DefaultTimeout,
		dialed:  make(chan *conn, 16),
	}
	t.Cleanup(s.shutdown)
	return s
}

// Dial is the seam handed to the client: it matches ircevent's DialContext
// signature and returns the client end of a fresh net.Pipe. network and addr
// are ignored — there is no network.
func (s *Server) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, errors.New("testsrv: server is closed")
	}
	client, server := net.Pipe()
	c := &conn{net: server, wake: make(chan struct{}, 1)}
	s.all = append(s.all, c)
	s.mu.Unlock()

	go c.read()
	select {
	case s.dialed <- c:
	default:
		// unreachable in practice: ircevent dials once per connection
		s.t.Errorf("testsrv: too many pending connections (%d)", cap(s.dialed))
	}
	return client, nil
}

// Send writes one line to the client, appending CRLF. Extra arguments are
// applied as a fmt format; with no arguments the line is sent verbatim, so
// '%' needs no escaping.
func (s *Server) Send(line string, args ...any) {
	s.t.Helper()
	if len(args) > 0 {
		line = fmt.Sprintf(line, args...)
	}
	c := s.current()
	_ = c.net.SetWriteDeadline(time.Now().Add(s.timeout()))
	_, err := io.WriteString(c.net, line+"\r\n")
	_ = c.net.SetWriteDeadline(time.Time{})
	if err != nil {
		s.t.Fatalf("testsrv: sending %q: %v", line, err)
	}
}

// Expect asserts that the next line from the client matches pattern and
// returns it.
func (s *Server) Expect(pattern string) string {
	s.t.Helper()
	c := s.current()
	line, err := c.take(nil, s.timeout())
	if err != nil {
		s.failWait(c, fmt.Sprintf("expecting %q", pattern), err)
	}
	if !Match(pattern, line) {
		s.t.Fatalf("testsrv: expected %q, got %q (still queued: %q)", pattern, line, c.pending())
	}
	return line
}

// WaitFor returns the first queued line matching pattern, waiting for more
// lines if needed. The matched line is consumed; every other queued line is
// left in place for later assertions.
func (s *Server) WaitFor(pattern string) string {
	s.t.Helper()
	c := s.current()
	line, err := c.take(func(l string) bool { return Match(pattern, l) }, s.timeout())
	if err != nil {
		s.failWait(c, fmt.Sprintf("waiting for %q", pattern), err)
	}
	return line
}

// failWait reports a wait that never produced a line, loudly: what was awaited,
// why the wait ended, and everything the client did send.
func (s *Server) failWait(c *conn, what string, err error) {
	s.t.Helper()
	reason := err.Error()
	if errors.Is(err, errTimeout) {
		reason = fmt.Sprintf("nothing matched within %v", s.timeout())
	}
	s.t.Fatalf("testsrv: %s: %s (unconsumed lines from the client: %q)", what, reason, c.pending())
}

// Sync is the barrier: it PINGs the client and waits for the matching PONG.
// Every line sent before Sync has been fully processed by the session by the
// time Sync returns, so state assertions need no sleeps.
func (s *Server) Sync() {
	s.t.Helper()
	s.ping()
	if s.Barrier == nil {
		return
	}
	s.Barrier()
	// A second round trip: the barrier may have made the client write (a
	// rejoin, a MODE query), and those bytes must be queued before Sync returns
	// or a Pending() assertion races them.
	s.ping()
}

// ping is one PING/PONG round trip.
func (s *Server) ping() {
	s.t.Helper()
	s.mu.Lock()
	s.seq++
	token := fmt.Sprintf("sync-%d", s.seq)
	s.mu.Unlock()
	s.Send("PING :%s", token)
	s.WaitFor("PONG*" + token)
}

// TryNext pops the next line the client sent, reporting false when none came:
// the connection ended, or nothing arrived within Timeout.
//
// Expect and WaitFor fail the test when that happens, which is right for a
// script that owns the whole conversation. TryNext is for the one that does
// not — a handshake the client may legitimately abandon halfway, such as the
// terminal SASL failure that closes the link mid-registration. The caller keeps
// its own deadline for the outcome it is actually waiting for, so a silent
// timeout here cannot turn into a silent pass there.
func (s *Server) TryNext() (string, bool) {
	c := s.currentOrNil()
	if c == nil {
		return "", false
	}
	line, err := c.take(nil, s.timeout())
	if err != nil {
		return "", false
	}
	return line, true
}

// Pending returns the lines the client has sent that no assertion has
// consumed yet.
func (s *Server) Pending() []string {
	c := s.currentOrNil()
	if c == nil {
		return nil
	}
	return c.pending()
}

// Close drops the current connection the way a server-side disconnect does,
// and detaches from it: the next assertion waits for the client to redial.
func (s *Server) Close() {
	s.mu.Lock()
	c := s.cur
	s.cur = nil
	s.mu.Unlock()
	if c != nil {
		c.close()
	}
}

// NextConn detaches from the current connection and waits for the client to
// dial again — the reconnect seam. Raise Timeout above the client's reconnect
// delay first.
func (s *Server) NextConn() {
	s.t.Helper()
	s.mu.Lock()
	s.cur = nil
	s.mu.Unlock()
	s.current()
}

// Step is one step of a scripted session: exactly one of Expect and Send.
type Step struct {
	Expect string // pattern the next client line must match
	Send   string // line to send to the client
}

// Script runs steps in order.
func (s *Server) Script(steps ...Step) {
	s.t.Helper()
	for i, st := range steps {
		switch {
		case (st.Expect == "") == (st.Send == ""):
			s.t.Fatalf("testsrv: step %d must set exactly one of Expect and Send: %+v", i, st)
		case st.Expect != "":
			s.Expect(st.Expect)
		default:
			s.Send(st.Send)
		}
	}
}

// Replay runs a transcript: one step per line, "C:" for a line the client
// must send next and "S:" for a line the server sends. Blank lines and lines
// starting with '#' are ignored, and leading whitespace is trimmed so
// transcripts can be indented raw strings.
//
//	srv.Replay(`
//	    C: JOIN #eco
//	    S: :eco!eco@test JOIN #eco
//	    S: :irc.test 366 eco #eco :End of /NAMES list.
//	`)
func (s *Server) Replay(transcript string) {
	s.t.Helper()
	steps, err := ParseTranscript(transcript)
	if err != nil {
		s.t.Fatalf("testsrv: %v", err)
	}
	s.Script(steps...)
}

// ParseTranscript turns the "C:"/"S:" text format into steps. See Replay.
func ParseTranscript(transcript string) ([]Step, error) {
	var steps []Step
	for n, raw := range strings.Split(transcript, "\n") {
		line := strings.TrimRight(strings.TrimLeft(raw, " \t"), " \t\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		dir, rest, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("transcript line %d: want %q or %q prefix, got %q", n+1, "C:", "S:", line)
		}
		// IRC lines never start with a space, so indented payloads are safe
		// to left-trim; that lets transcripts be aligned however reads best
		rest = strings.TrimLeft(rest, " \t")
		if rest == "" {
			return nil, fmt.Errorf("transcript line %d: empty step", n+1)
		}
		switch strings.ToUpper(strings.TrimSpace(dir)) {
		case "C":
			steps = append(steps, Step{Expect: rest})
		case "S":
			steps = append(steps, Step{Send: rest})
		default:
			return nil, fmt.Errorf("transcript line %d: unknown direction %q", n+1, dir)
		}
	}
	return steps, nil
}

// Match reports whether pat matches a prefix of line. '*' matches any run of
// characters; everything else is literal, so a leading '*' means "anywhere in
// the line" and a trailing one is implied.
//
// A pattern that does not itself start with '@' is matched against the line
// with its IRCv3 message tags removed, so "JOIN #eco" still means "the client
// joined #eco" now that every correlatable command goes out labelled. Start a
// pattern with '@' to assert on the tags themselves.
func Match(pat, line string) bool {
	if !strings.HasPrefix(pat, "@") {
		line = StripTags(line)
	}
	parts := strings.Split(pat, "*")
	if !strings.HasPrefix(line, parts[0]) {
		return false
	}
	line = line[len(parts[0]):]
	for _, p := range parts[1:] {
		i := strings.Index(line, p)
		if i < 0 {
			return false
		}
		line = line[i+len(p):]
	}
	return true
}

// StripTags returns a protocol line without its IRCv3 message tags.
func StripTags(line string) string {
	if !strings.HasPrefix(line, "@") {
		return line
	}
	if i := strings.IndexByte(line, ' '); i >= 0 {
		return strings.TrimLeft(line[i+1:], " ")
	}
	return ""
}

// Tag returns the value of one message tag on a line, and whether it was there.
// A valueless tag reports ("", true). It is how a test answers a labelled
// command the way a real server does — echoing the client's own label back.
func Tag(line, name string) (string, bool) {
	if !strings.HasPrefix(line, "@") {
		return "", false
	}
	tags := line[1:]
	if i := strings.IndexByte(tags, ' '); i >= 0 {
		tags = tags[:i]
	}
	for _, tag := range strings.Split(tags, ";") {
		key, value, _ := strings.Cut(tag, "=")
		if key == name {
			return value, true
		}
	}
	return "", false
}

func (s *Server) timeout() time.Duration {
	if s.Timeout > 0 {
		return s.Timeout
	}
	return DefaultTimeout
}

// next pops the next client line, failing the test if none arrives.
func (s *Server) next() string {
	s.t.Helper()
	c := s.current()
	line, err := c.take(nil, s.timeout())
	if err != nil {
		s.failWait(c, "reading the next line", err)
	}
	return line
}

func (s *Server) currentOrNil() *conn {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur
}

// current returns the connection assertions talk to, waiting for the client
// to dial if none is attached yet.
func (s *Server) current() *conn {
	s.t.Helper()
	if c := s.currentOrNil(); c != nil {
		return c
	}
	timer := time.NewTimer(s.timeout())
	defer timer.Stop()
	select {
	case c := <-s.dialed:
		s.mu.Lock()
		s.cur = c
		s.mu.Unlock()
		return c
	case <-timer.C:
		s.t.Fatalf("testsrv: no client connected after %v (is Session.Dial wired to srv.Dial, and Connect called?)", s.timeout())
		return nil // unreachable: Fatalf ends the goroutine
	}
}

func (s *Server) shutdown() {
	s.mu.Lock()
	s.closed = true
	conns := append([]*conn(nil), s.all...)
	s.cur = nil
	s.mu.Unlock()
	for _, c := range conns {
		c.close()
	}
}

// conn is one accepted connection: the server end of a net.Pipe plus the
// queue of lines read from it.
type conn struct {
	net net.Conn

	mu     sync.Mutex
	lines  []string
	err    error
	closed bool
	wake   chan struct{} // buffered(1) signal that lines or err changed
}

// read drains the socket into the queue. It runs for the life of the
// connection and never touches *testing.T, so it is safe past test end.
func (c *conn) read() {
	r := bufio.NewReader(c.net)
	for {
		line, err := r.ReadString('\n')
		if line = strings.TrimRight(line, "\r\n"); line != "" {
			c.push(line)
			// real servers close on QUIT; doing the same keeps teardown
			// immediate instead of waiting for a timeout
			if strings.HasPrefix(strings.ToUpper(line), "QUIT") {
				c.close()
				return
			}
		}
		if err != nil {
			c.fail(err)
			return
		}
	}
}

func (c *conn) push(line string) {
	c.mu.Lock()
	c.lines = append(c.lines, line)
	c.mu.Unlock()
	c.signal()
}

func (c *conn) fail(err error) {
	c.mu.Lock()
	if c.err == nil {
		c.err = err
	}
	c.mu.Unlock()
	c.signal()
}

func (c *conn) close() {
	c.mu.Lock()
	first := !c.closed
	c.closed = true
	if c.err == nil {
		c.err = net.ErrClosed
	}
	c.mu.Unlock()
	if first {
		c.net.Close()
	}
	c.signal()
}

func (c *conn) signal() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *conn) pending() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.lines...)
}

// take removes and returns the first queued line satisfying pred, or the head
// of the queue when pred is nil, waiting up to timeout for one to arrive.
// Lines that do not match are left queued.
func (c *conn) take(pred func(string) bool, timeout time.Duration) (string, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		c.mu.Lock()
		for i, l := range c.lines {
			if pred != nil && !pred(l) {
				continue
			}
			c.lines = append(c.lines[:i], c.lines[i+1:]...)
			c.mu.Unlock()
			return l, nil
		}
		err := c.err
		c.mu.Unlock()
		if err != nil {
			return "", err
		}
		select {
		case <-c.wake:
		case <-timer.C:
			return "", errTimeout
		}
	}
}
