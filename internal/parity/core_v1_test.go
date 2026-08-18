package parity

import (
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/app"
	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/testsrv"
)

// v1Core drives the shipping Session through its twelve UICallbacks. It is the
// reference: where the two cores disagree and the divergence list does not say
// otherwise, this side is right.
type v1Core struct {
	srv  *testsrv.Server
	sess *app.Session
	rec  *v1Recorder
}

func newV1Core(t *testing.T, setup func(*testsrv.Server)) core {
	t.Helper()
	srv := testsrv.New(t)
	if setup != nil {
		setup(srv)
	}
	rec := &v1Recorder{}
	sess := app.NewSession(app.Config{Server: "irc.test", Port: "6667", Nick: nick}, rec.callbacks())
	rec.sess = sess // written before Connect starts the read goroutine
	sess.Dial = srv.Dial
	return &v1Core{srv: srv, sess: sess, rec: rec}
}

func (c *v1Core) server() *testsrv.Server { return c.srv }

func (c *v1Core) start(t *testing.T) {
	t.Helper()
	c.sess.Connect()
	c.srv.Register(nick)
}

// settle is a no-op: ircevent dispatches the session's callbacks synchronously
// in its read loop, so the server's PONG already means every line before it has
// been folded in.
func (c *v1Core) settle(*testing.T) {}

func (c *v1Core) do(t *testing.T, a action) {
	t.Helper()
	switch a.kind {
	case actJoin:
		c.sess.JoinUser(a.target)
	case actPart:
		c.sess.Part(a.target)
	case actCycle:
		c.sess.Cycle(a.target)
	case actSay:
		c.sess.SendMessage(a.target, a.text)
	case actMe:
		c.sess.SendAction(a.target, a.text)
	case actClose:
		c.sess.CloseBuffer(a.target)
	case actIgnore:
		app.SetIgnored(a.target, true)
		t.Cleanup(func() { app.SetIgnored(a.target, false) })
	default:
		t.Fatalf("v1: unknown action %q", a.kind)
	}
}

func (c *v1Core) take() []Fact { return collapseAmends(c.rec.drain()) }

func (c *v1Core) snapshot() []bufferSnap {
	var out []bufferSnap
	redacted := c.rec.redactedIDs()
	for _, b := range c.sess.Buffers() {
		// v1's Session keeps a redacted message in the buffer and tells the UI
		// to drop the row (ui.go), which is where the M5 suite had to stub it.
		// Applying the recorded RedactMsg callbacks here is the v1 half of that
		// behaviour, the same way the v2 core's conductor supplies the
		// join→MODE/CHATHISTORY policy that lives in the Session today.
		var lines []model.Line
		for _, l := range b.Lines {
			if l.MsgID != "" && redacted[foldName(b.Name)+"|"+l.MsgID] {
				continue
			}
			lines = append(lines, l)
		}
		snap := bufferSnap{
			Name:  foldName(b.Name),
			Topic: b.Topic,
			Modes: sortModes(b.Modes),
			Key:   b.Key,
			Lines: lineFacts(b.Name, lines),
		}
		for n, p := range b.Members {
			snap.Members = append(snap.Members, memberLine(n, p))
		}
		sort.Strings(snap.Members)
		out = append(out, snap)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (c *v1Core) stop(t *testing.T) {
	t.Helper()
	c.sess.Quit()
	// The Session's teardown seam is unexported, so the assertion from outside
	// the package is the connection going down: Quit writes QUIT, the fake
	// server closes the pipe on it, and ircevent's Loop unwinds.
	waitUntil(t, "the v1 session to disconnect", 10*time.Second, func() bool {
		return !c.sess.Connected()
	})
}

// sortModes puts a tracked mode set in a canonical order. v1 appends modes in
// arrival order and v2 keeps them sorted; the *set* is what a user sees.
func sortModes(m string) string {
	r := strings.Split(m, "")
	sort.Strings(r)
	return strings.Join(r, "")
}

// v1Recorder is a UICallbacks that records. The session calls back from
// ircevent's read goroutine, so every field is guarded, and it never touches
// *testing.T — the read loop can outlive a test body.
type v1Recorder struct {
	mu       sync.Mutex
	facts    []Fact
	redacted map[string]bool // "buffer|msgid" the UI was told to drop
	sess     *app.Session
}

func (r *v1Recorder) redactedIDs() map[string]bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]bool, len(r.redacted))
	for k := range r.redacted {
		out[k] = true
	}
	return out
}

func (r *v1Recorder) add(f Fact) {
	r.mu.Lock()
	r.facts = append(r.facts, f)
	r.mu.Unlock()
}

func (r *v1Recorder) drain() []Fact {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.facts
	r.facts = nil
	return out
}

func (r *v1Recorder) callbacks() app.UICallbacks {
	return app.UICallbacks{
		AddLine: func(buffer string, l model.Line) {
			r.add(factFromLine(buffer, l))
		},
		AmendBuffer: func(buffer string) {
			// AmendBuffer says "the last line of this buffer changed" and
			// nothing else, so the recorder reads the line back. Without that
			// the merged text of a multiline message would exist only inside
			// the session and could not be compared at all.
			f := Fact{Kind: kindAmend, Buffer: foldName(buffer)}
			if b := r.sess.Buffer(buffer); b != nil && len(b.Lines) > 0 {
				amended := factFromLine(buffer, b.Lines[len(b.Lines)-1])
				f.Text, f.Flags = amended.Text, amended.Flags
			}
			r.add(f)
		},
		BufferList:    func() { r.add(Fact{Kind: kindRail}) },
		MembersChange: func(buffer string) { r.add(Fact{Kind: kindMembers, Buffer: foldName(buffer)}) },
		TopicChange: func(buffer, topic string) {
			r.add(Fact{Kind: kindTopic, Buffer: foldName(buffer), Text: topic})
		},
		StatusChange: func(connected bool, status string) {
			// v1's status text is a sentence for a label; the comparable part is
			// which of the two states it reports.
			state := model.ConnDisconnected
			if connected {
				state = model.ConnReady
			}
			r.add(Fact{Kind: kindConn, Text: state.String()})
		},
		RedactMsg: func(buffer, msgid string) {
			r.mu.Lock()
			if r.redacted == nil {
				r.redacted = map[string]bool{}
			}
			r.redacted[foldName(buffer)+"|"+msgid] = true
			r.mu.Unlock()
			r.add(Fact{Kind: kindRedact, Buffer: foldName(buffer), Text: msgid})
		},
		STS: func(port string, durationSecs int64) {
			r.add(Fact{Kind: kindSTS, Text: port})
		},
		RenameBuffer: func(oldName, newName string) {
			r.add(Fact{Kind: kindRename, Buffer: foldName(newName), Text: foldName(oldName)})
		},
		HighlightNote: func(buffer, nick, text string) {
			r.add(Fact{Kind: kindHighlight, Buffer: foldName(buffer), Nick: nick, Text: text})
		},
		JoinFailed: func(channel, reason string, needKey bool) {
			r.add(Fact{Kind: kindJoinFail, Buffer: foldName(channel), Text: reason,
				Flags: cond("needkey", needKey)})
		},
		ChannelListDone: func() { r.add(Fact{Kind: kindListDone}) },
	}
}
