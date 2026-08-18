package transcript

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/irc"
	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/state"
	"github.com/toprakkilic/eco/internal/testsrv"
)

// transcriptNet is the network id every event in this suite carries.
const transcriptNet model.NetworkID = "transcript"

// fixedClock pins the store's clock. Times are not recorded, but a fixed clock
// keeps typing expiry and message ordering deterministic.
var fixedClock = time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)

// core is the thing under test: internal/irc.Client + internal/state.Store, with
// the small conductor between them — the piece that decides to ask for MODE and
// CHATHISTORY after joining, and to rejoin after a reconnect.
//
// The conductor is here rather than imported because it is application policy,
// not wire and not state: internal/app's Session has its own copy (reactLocked),
// and the 37 fake-server tests in that package are what hold *it* to account.
// What this suite records is the pair below the app — the layer §5.3 promises
// stays put while the UI above it is rebuilt.
type core struct {
	srv    *testsrv.Server
	client *irc.Client
	store  *state.Store
	events <-chan model.Event

	facts []Fact
}

func newCore(t *testing.T, setup func(*testsrv.Server)) *core {
	t.Helper()
	srv := testsrv.New(t)
	if setup != nil {
		setup(srv)
	}
	client := irc.New(irc.Config{
		Network:     transcriptNet,
		Server:      "irc.test",
		Port:        "6667",
		Nick:        nick,
		QuitMessage: "eco, a friendly IRC client",
		Version:     "eco 0.1",
		EventBuffer: 8192,
		// A decode bug must fail the test rather than be swallowed by the
		// library's callback recover.
		AllowPanic: true,
		// The fake server never answers a keepalive PING, so the keepalive is
		// pushed past any test's lifetime.
		Timeout:   10 * time.Second,
		KeepAlive: 10 * time.Minute,
		Dial:      srv.Dial,
		Clock:     model.FixedClock(fixedClock),
		// Reconnect backoff fires at once: the ladder is asserted in
		// internal/irc's own tests, and here it would only be a sleep.
		Timer: func(time.Duration) <-chan time.Time {
			ch := make(chan time.Time, 1)
			ch <- time.Time{}
			return ch
		},
		Rand: func() float64 { return 0.5 },
	})
	store := state.New(state.Options{
		Clock:      model.FixedClock(fixedClock),
		IDs:        model.NewIDSeq(0),
		Scrollback: state.Unlimited,
	})
	c := &core{srv: srv, client: client, store: store, events: client.Events()}
	// The status buffer exists before the first log line, the way app.NewSession
	// creates it.
	c.observe(store.EnsureNetwork(transcriptNet))
	return c
}

func (c *core) server() *testsrv.Server { return c.srv }

func (c *core) start(t *testing.T) {
	t.Helper()
	c.client.Start()
	c.srv.Register(nick)
	c.settle(t)
	if got := c.store.Status(transcriptNet); got != model.ConnReady {
		t.Fatalf("store status after registration = %v, want ready", got)
	}
}

// settle folds every event the client has already emitted. It is a
// non-blocking drain and that is exact, not hopeful: the decoder emits from
// ircevent's read loop, so by the time the fake server's PONG comes back every
// event for the lines before it is already in the channel.
func (c *core) settle(t *testing.T) {
	t.Helper()
	for {
		select {
		case ev, ok := <-c.events:
			if !ok {
				return // the client stopped; nothing more will arrive
			}
			c.fold(t, ev)
		default:
			return
		}
	}
}

// fold is the dispatcher goroutine: apply, record, then react. Everything below
// runs on the harness's own goroutine, which is what state.Store's threading
// contract requires.
func (c *core) fold(t *testing.T, ev model.Event) {
	c.observe(c.store.Apply(ev))
	c.react(t, ev)
}

// react is the conductor. It watches events rather than changes because the
// decisions it makes ("we just joined", "we are ready") are about the wire, and
// because writing to the wire is not something a Change is allowed to imply.
func (c *core) react(t *testing.T, ev model.Event) {
	switch e := ev.(type) {
	case model.ConnState:
		if e.Status == model.ConnReady {
			c.onReady(t)
		}
	case model.Join:
		if !e.Playback && strings.EqualFold(e.Sender.Nick, c.client.CurrentNick()) {
			c.onSelfJoin(t, e.Channel)
		}
	}
}

// onReady rejoins the channels the user considers themselves in. The store
// hands over the plan, keys included — the app's own conductor batches it ten
// channels to a line, which is a policy, not behaviour of the core.
func (c *core) onReady(t *testing.T) {
	t.Helper()
	for _, target := range c.store.RejoinTargets(transcriptNet) {
		if _, err := c.client.Join(target.Channel, target.Key); err != nil {
			t.Fatalf("rejoining %s: %v", target.Channel, err)
		}
	}
}

// onSelfJoin asks for the channel's modes and its scrollback, once per open
// buffer — the two requests app.Session issues from the same event.
func (c *core) onSelfJoin(t *testing.T, channel string) {
	t.Helper()
	if err := c.client.Mode(channel); err != nil {
		t.Fatalf("MODE %s: %v", channel, err)
	}
	id := c.store.BufferID(transcriptNet, channel)
	if !c.store.HasCap(transcriptNet, "draft/chathistory") || !c.store.NeedsHistory(id) {
		return
	}
	if _, err := c.client.History(irc.HistoryRequest{Target: channel, Direction: model.HistoryLatest}); err != nil {
		t.Fatalf("CHATHISTORY %s: %v", channel, err)
	}
	c.store.MarkHistoryRequested(id)
}

func (c *core) do(t *testing.T, a action) {
	t.Helper()
	id := c.store.BufferID(transcriptNet, a.target)
	switch a.kind {
	case actJoin:
		c.store.RequestJoin(transcriptNet, a.target)
		if _, err := c.client.Join(a.target, ""); err != nil {
			t.Fatalf("JOIN %s: %v", a.target, err)
		}
	case actPart:
		h, err := c.client.Part(a.target, "")
		if err != nil {
			// No connection to ask: close the buffer locally, as the app does.
			c.observe(c.store.PartLocally(id))
			return
		}
		c.store.RequestPart(id, h.Label())
	case actCycle:
		c.store.RequestCycle(id)
		if _, err := c.client.Part(a.target, ""); err != nil {
			t.Fatalf("PART %s: %v", a.target, err)
		}
		if _, err := c.client.Join(a.target, ""); err != nil {
			t.Fatalf("JOIN %s: %v", a.target, err)
		}
	case actSay:
		h, err := c.client.SendMessage(a.target, a.text)
		if err != nil {
			t.Fatalf("PRIVMSG %s: %v", a.target, err)
		}
		_, out := c.store.Echo(id, state.Outgoing{
			Label: h.Label(), Text: strings.Join(h.Lines, "\n"), Kind: model.MsgPrivmsg,
		})
		c.observe(out)
	case actMe:
		h, err := c.client.SendAction(a.target, a.text)
		if err != nil {
			t.Fatalf("ACTION %s: %v", a.target, err)
		}
		_, out := c.store.Echo(id, state.Outgoing{
			Label: h.Label(), Text: foldNewlines(a.text), Kind: model.MsgAction,
		})
		c.observe(out)
	case actClose:
		c.observe(c.store.CloseBuffer(id))
	case actIgnore:
		c.store.AddIgnore(transcriptNet, a.target)
		t.Cleanup(func() { c.store.RemoveIgnore(transcriptNet, a.target) })
	default:
		t.Fatalf("unknown action %q", a.kind)
	}
}

// foldNewlines is what SendAction does to a body: a single ACTION cannot carry
// a line break.
func foldNewlines(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r", " "), "\n", " ")
}

func (c *core) observe(cs []model.Change) {
	c.facts = append(c.facts, factsFromChanges(cs, c.roster)...)
}

// roster is the resulting member list a MembersChanged delta produced. It is
// read after Apply, so it is the roster the dispatcher would paint.
func (c *core) roster(id model.BufferID) string {
	ms := c.store.Members(id)
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, memberLine(m.Nick, m.Prefixes))
	}
	sort.Strings(out)
	return rosterText(out)
}

func (c *core) take() []Fact {
	out := c.facts
	c.facts = nil
	return out
}

func (c *core) snapshot() []bufferSnap {
	var out []bufferSnap
	for _, info := range c.store.Buffers(transcriptNet) {
		modes, key, _ := c.store.Modes(info.ID)
		topic, _ := c.store.Topic(info.ID)
		snap := bufferSnap{
			Name:  bufName(info.ID),
			Topic: topic,
			Modes: sortModes(modes),
			Key:   key,
		}
		for _, m := range c.store.Members(info.ID) {
			snap.Members = append(snap.Members, memberLine(m.Nick, m.Prefixes))
		}
		sort.Strings(snap.Members)
		for _, m := range c.store.Messages(info.ID) {
			snap.Lines = append(snap.Lines, factFromMessage(m))
		}
		out = append(out, snap)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (c *core) stop(*testing.T) { c.client.Close() }

// sortModes puts a tracked mode set in a canonical order: the *set* is what a
// user sees, and the order the store happens to keep it in is not behaviour.
func sortModes(m string) string {
	r := strings.Split(m, "")
	sort.Strings(r)
	return strings.Join(r, "")
}
