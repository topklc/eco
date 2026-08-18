package irc

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/ergochat/irc-go/ircevent"
	"github.com/ergochat/irc-go/ircmsg"
	"github.com/toprakkilic/eco/internal/irc/sasl"
	"github.com/toprakkilic/eco/internal/irc/transport"
	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/proto"
)

// ErrNotConnected is returned by every send method when there is no live
// connection to write to.
var ErrNotConnected = errors.New("irc: not connected")

// Client is one network's wire connection. See the package doc for the
// threading contract; in one line: it emits on its own goroutine, and the
// consumer of Events() owns nothing but the reading.
type Client struct {
	cfg   Config
	clock model.Clock

	events chan model.Event

	// done is closed by Close; finished is closed when the supervisor
	// goroutine has returned and the event channel is closed.
	done     chan struct{}
	finished chan struct{}

	// published snapshots: written by the decode goroutine, read by anyone.
	// Each stored value is immutable from the moment it is stored.
	props  atomic.Pointer[proto.ServerProps]
	caps   atomic.Pointer[proto.CapSet]
	status atomic.Uint32

	ledger *ledger

	mu      sync.Mutex
	conn    *ircevent.Connection
	nick    string
	started bool
	closed  bool
}

// New builds a Client. Nothing happens on the wire until Start.
func New(cfg Config) *Client {
	cfg = cfg.withDefaults()
	c := &Client{
		cfg:      cfg,
		clock:    cfg.Clock,
		events:   make(chan model.Event, cfg.EventBuffer),
		done:     make(chan struct{}),
		finished: make(chan struct{}),
		nick:     cfg.Nick,
	}
	c.ledger = newLedger(cfg.Clock)
	c.props.Store(proto.DefaultProps())
	c.caps.Store(proto.NewCapSet(cfg.Caps))
	c.status.Store(uint32(model.ConnDisconnected))
	return c
}

// Network returns the id stamped on every event this client emits.
func (c *Client) Network() model.NetworkID { return c.cfg.Network }

// Events is the typed stream. It is closed once the client has stopped and
// will emit nothing further, which makes "range over Events()" a complete
// consumer.
func (c *Client) Events() <-chan model.Event { return c.events }

// Props returns the current server properties, built from ISUPPORT. The
// returned value is an immutable snapshot: it is replaced, never mutated, so
// a consumer may hold it across goroutines. It is the same information the
// ISupport events carry, offered for callers that would rather ask than
// accumulate.
func (c *Client) Props() *proto.ServerProps { return c.props.Load() }

// Caps returns the capability state. Treat it as read-only: it is a snapshot
// the client will not touch again, but it is not a defensive copy per call.
func (c *Client) Caps() *proto.CapSet { return c.caps.Load() }

// Enabled reports whether a capability is negotiated and in force.
func (c *Client) Enabled(name string) bool { return c.caps.Load().Enabled(name) }

// STS returns the sts policy the server advertised, if any. sts is read from
// the advertised set because a client never requests it.
func (c *Client) STS() (proto.STSPolicy, bool, error) { return c.caps.Load().STS() }

// MultilineLimits returns the negotiated draft/multiline limits and whether
// multiline is enabled at all.
func (c *Client) MultilineLimits() (proto.MultilineLimits, bool) {
	return c.caps.Load().Multiline()
}

// Status returns the connection state machine's current position.
func (c *Client) Status() model.ConnStatus { return model.ConnStatus(c.status.Load()) }

// CurrentNick returns the nick the server assigned, falling back to the
// configured one before registration completes.
func (c *Client) CurrentNick() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.nick
}

// Start brings the connection up and keeps it up, reconnecting with backoff
// until Close. It is safe to call more than once; only the first call does
// anything.
func (c *Client) Start() {
	c.mu.Lock()
	if c.started || c.closed {
		c.mu.Unlock()
		return
	}
	c.started = true
	c.mu.Unlock()
	go c.run()
}

// Close stops the client: it sends QUIT if a connection is up, stops
// reconnecting, and waits for the event channel to close. It is idempotent,
// and safe to call on a client that was never started.
//
// It returns once the connection has actually unwound, which normally means
// as soon as the server answers QUIT by closing the link. A server that
// ignores QUIT delays it by at most Config.Timeout, after which the library
// gives up on it.
func (c *Client) Close() {
	c.mu.Lock()
	if c.closed {
		conn := c.conn
		c.mu.Unlock()
		if conn != nil {
			conn.Quit()
		}
		<-c.finished
		return
	}
	c.closed = true
	started, conn := c.started, c.conn
	c.mu.Unlock()

	close(c.done)
	if conn != nil {
		// wake the supervisor out of the registration handshake, which does
		// not watch our done channel: Quit sets the library's quit flag, so
		// the handshake unwinds instead of waiting out its timeout.
		conn.Quit()
	}
	if !started {
		close(c.events)
		close(c.finished)
	}
	<-c.finished
}

func (c *Client) stopping() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

// emit hands one event to the consumer. It blocks while the channel is full —
// backpressure, not loss — and gives up only when the client is closing.
func (c *Client) emit(ev model.Event) {
	select {
	case c.events <- ev:
	case <-c.done:
	}
}

// meta builds the envelope for a client-generated event (one with no wire
// line behind it).
func (c *Client) meta() model.EventMeta {
	return model.EventMeta{Network: c.cfg.Network, Time: c.clock.Now()}
}

func (c *Client) setStatus(s model.ConnStatus) { c.status.Store(uint32(s)) }

func (c *Client) setNick(n string) {
	if n == "" {
		return
	}
	c.mu.Lock()
	c.nick = n
	c.mu.Unlock()
}

func (c *Client) connection() *ircevent.Connection {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn
}

func (c *Client) setConn(conn *ircevent.Connection) {
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()
}

// run is the supervisor goroutine: connect, serve, back off, repeat.
func (c *Client) run() {
	defer close(c.finished)
	defer close(c.events)

	failures := 0
	for {
		if c.stopping() {
			return
		}
		attempt := failures + 1

		c.setStatus(model.ConnConnecting)
		c.emit(model.ConnState{EventMeta: c.meta(), Status: model.ConnConnecting,
			Nick: c.CurrentNick(), Attempt: attempt})

		conn, dec := c.newConnection()
		c.setConn(conn)

		err := conn.Connect()
		if err == nil {
			// Registered. Serve until the connection dies or we are closed,
			// then join every goroutine the library started before touching
			// the event channel again: Quit first so Loop unwinds instead of
			// reconnecting on its own schedule.
			select {
			case <-dec.dead:
			case <-c.done:
			}
			conn.Quit()
			conn.Loop()
			failures = 0
		} else {
			failures++
		}
		c.setConn(nil)
		c.setStatus(model.ConnDisconnected)

		reason := ""
		if err != nil {
			reason = err.Error()
		} else if e := dec.lastError(); e != "" {
			reason = e
		}

		if c.stopping() {
			c.emit(model.ConnState{EventMeta: c.meta(), Status: model.ConnDisconnected,
				Nick: c.CurrentNick(), Err: reason, Attempt: attempt})
			return
		}
		if dec.authFatal() {
			// Bad credentials do not get better by being retried; a
			// reconnect storm against an auth failure is how an account gets
			// throttled. Report it as terminal and stop.
			c.setStatus(model.ConnFailed)
			c.emit(model.ConnState{EventMeta: c.meta(), Status: model.ConnFailed,
				Nick: c.CurrentNick(), Err: reason, Attempt: attempt})
			return
		}

		delay := c.cfg.Backoff.Delay(max(failures, 1), c.cfg.Rand)
		c.emit(model.ConnState{EventMeta: c.meta(), Status: model.ConnDisconnected,
			Nick: c.CurrentNick(), Err: reason, Attempt: attempt, RetryIn: delay})

		select {
		case <-c.cfg.Timer(delay):
		case <-c.done:
			return
		}
	}
}

// newConnection builds one attempt's library connection and the decoder that
// turns its traffic into events. Nothing is dialed yet.
func (c *Client) newConnection() (*ircevent.Connection, *decoder) {
	cfg := c.cfg

	tlsCfg := cfg.TLSConfig
	if tlsCfg == nil {
		tlsCfg = &tls.Config{ServerName: cfg.Server, MinVersion: tls.VersionTLS12}
	}

	conn := &ircevent.Connection{
		Server:        cfg.Server + ":" + cfg.Port,
		Nick:          cfg.Nick,
		User:          cfg.User,
		RealName:      cfg.RealName,
		UseTLS:        cfg.TLS,
		Password:      cfg.ServerPass,
		TLSConfig:     tlsCfg,
		RequestCaps:   append([]string(nil), cfg.Caps...),
		QuitMessage:   cfg.QuitMessage,
		Version:       cfg.Version,
		Timeout:       cfg.Timeout,
		KeepAlive:     cfg.KeepAlive,
		ReconnectFreq: noAutoReconnect,
		// The library's CTCP support rewrites the message it hands on — it
		// strips the \x01 wrapper out of the shared parameter slice — so the
		// raw hook would see a body that had already been chewed. eco decodes
		// and answers CTCP itself instead (see decoder.ctcpReply).
		EnableCTCP: false,
		Debug:      cfg.Debug,
		AllowPanic: cfg.AllowPanic,
	}
	if !cfg.Debug {
		// eco correlates labels itself, so the library sees labels it has no
		// callback for and says so on every reply. Silence it.
		conn.Log = log.New(io.Discard, "", 0)
	}

	// SASL: PLAIN and EXTERNAL are the library's, in registration. SCRAM is
	// ours, after registration, because that is where it is possible.
	switch {
	case cfg.SASL.scram():
		conn.UseSASL = false
		if !containsFold(conn.RequestCaps, "sasl") {
			conn.RequestCaps = append(conn.RequestCaps, "sasl")
		}
	case cfg.SASL.external():
		conn.UseSASL = true
		conn.SASLMech = MechExternal
		conn.SASLLogin = cfg.SASL.User
		conn.SASLOptional = cfg.SASL.Optional
	case cfg.SASL.Enabled():
		conn.UseSASL = true
		conn.SASLMech = MechPlain
		conn.SASLLogin = cfg.SASL.User
		conn.SASLPassword = cfg.SASL.Pass
		conn.SASLOptional = cfg.SASL.Optional
	}

	// IRC over WebSocket: Server is the URL and the transport owns TLS.
	if transport.IsWebSocketURL(cfg.Server) {
		wsURL := cfg.Server
		display := transport.DisplayHost(wsURL)
		if !strings.Contains(display, ":") {
			display += ":443"
		}
		conn.Server = display
		conn.UseTLS = false
		conn.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return transport.Dial(ctx, wsURL)
		}
	}
	// The injected dialer wins over both the websocket transport and TCP.
	if cfg.Dial != nil {
		conn.DialContext = cfg.Dial
	}

	dec := newDecoder(c, conn)
	// A reconnect starts from the defaults. The previous server's properties
	// and capabilities do not carry over — a pool may hand us a different
	// server — so republish them now rather than leaving the old snapshot
	// readable until the new server's first 005.
	c.props.Store(dec.props.Clone())
	c.caps.Store(dec.caps.Clone())

	conn.AddRawCallback(dec.line)
	conn.AddDisconnectCallback(func(ircmsg.Message) { dec.markDead() })

	if cfg.SASL.scram() {
		// The status sink is deliberately dropped: those are human sentences,
		// and this package does not produce any. The outcome reaches the
		// consumer as an AuthResult event decoded from the wire instead.
		sasl.SetupSCRAM(conn, cfg.SASL.User, cfg.SASL.Pass, func(string) {})
	}
	return conn, dec
}

func containsFold(list []string, want string) bool {
	for _, v := range list {
		if strings.EqualFold(v, want) {
			return true
		}
	}
	return false
}
