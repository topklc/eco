package app

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/ergochat/irc-go/ircfmt"
	"github.com/toprakkilic/eco/internal/config"
	"github.com/toprakkilic/eco/internal/irc"
	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/state"
)

// Session is the M7 cutover: a thin adapter over internal/irc.Client (the wire)
// and internal/state.Store (the model), presenting the surface the v1 UI already
// speaks — the twelve UICallbacks, Buffer/Buffers/SortedMembers, and the
// outgoing verbs the commands call.
//
// The shape is deliberate. The store is the single source of truth for identity
// and correctness: folded buffer keys, per-buffer msgid dedupe, labelled echo
// resolution, batch assembly, highlight rules, mode and member state, the
// reconnect plan. What lives here is a *projection* of that into the v1 Buffer
// shape (Lines/Members/Topic/Modes/Key), because ui.go reads those fields
// directly. M8's Dispatcher deletes the projection along with the callbacks when
// the UI learns to read Changes.
//
// Threading: one goroutine (pump) drains Client.Events(), applies each event to
// the store and folds the resulting Changes into the projection. Everything
// touching the store or the projection holds s.mu — the store is single-writer,
// and the UI reads Buffer fields under the same lock v1 used. Callbacks are
// collected under the lock and fired after it is released, so a callback that
// calls back into the Session cannot deadlock.
type Session struct {
	Name string // network profile name, fixed at creation

	// Dial, when non-nil, replaces the dialer used to reach the server. This is
	// the TEST SEAM: internal/testsrv hands the offline regression suite one end
	// of a net.Pipe here, so a real Session can be driven with no network.
	// Production code leaves it nil.
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)

	// stopped is closed when the pump goroutine has returned. It is the
	// TEARDOWN SEAM: Quit is asynchronous, so without this the offline suite
	// could not prove a test leaves no goroutine behind (see stopSession in
	// regression_test.go). Production code never reads it.
	stopped chan struct{}

	// barrier is the other half of testsrv.Server.Barrier: a test asks the pump
	// to finish everything already queued before it asserts. Nothing in
	// production sends on it.
	barrier chan chan struct{}

	mu     sync.Mutex
	client *irc.Client
	store  *state.Store
	netID  model.NetworkID

	// irc is the small write-side façade the commands and dialogs reach for.
	// It is nil until Connect, which is the "are we live?" test they make.
	irc *wire

	ui    UICallbacks
	bufs  map[string]*Buffer // folded buffer key -> projection
	order []string           // folded keys, display order, server buffer first

	channels      []ChannelInfo // last completed LIST
	firstScanDone bool          // join-all runs only on the session's first LIST
	manualList    bool          // the next LIST result was requested by the user
	quitting      bool          // Quit was called; a disconnect is expected
	jpMark        int           // join/part visibility generation last pushed to the store

	quitOnce sync.Once

	Config Config
}

// Config is one network's connection settings.
type Config struct {
	Server     string
	Port       string
	TLS        bool
	ServerPass string // connection password (PASS command)
	Nick       string
	SASLUser   string
	SASLPass   string
	SASLMech   string // "PLAIN" (default) or "SCRAM-SHA-256"
	Channels   string // comma separated autojoin
}

// Line is what the session layer hands to the UI: a buffer name plus a rendered
// row. It lives in internal/model; the alias keeps every call site in this
// package unchanged while the layers are pulled apart.
type Line = model.Line

// ChannelInfo is one row of a LIST result.
type ChannelInfo struct {
	Name  string
	Users int
	Topic string
}

// UICallbacks is the v1 UI's twelve-slot interface to a session. M8 replaces the
// whole struct with ui.Dispatcher draining model.Changes; until then the adapter
// below maps every Change onto one of these.
type UICallbacks struct {
	ChannelListDone func() // RPL_LISTEND received; Session.ChannelList() has the data
	AddLine         func(buffer string, l Line)
	AmendBuffer     func(buffer string) // a line changed in place; redraw if visible
	BufferList      func()              // buffers changed (added/removed/renamed)
	MembersChange   func(buffer string)
	TopicChange     func(buffer, topic string)
	StatusChange    func(connected bool, status string)
	RedactMsg       func(buffer, msgid string)
	STS             func(port string, durationSecs int64) // sts cap seen in CAP LS/NEW
	RenameBuffer    func(oldName, newName string)
	HighlightNote   func(buffer, nick, text string)            // a merged multiline line became a mention
	JoinFailed      func(channel, reason string, needKey bool) // join refused; needKey means +k (prompt for the key)
}

// Buffer is the UI's view of one conversation. It is a projection of the store,
// rebuilt from Changes; nothing outside this file may write to it.
type Buffer struct {
	Name    string // channel, nick, or "server"
	Topic   string
	Members map[string]string // nick -> prefixes (e.g. "@+")
	Modes   string            // tracked channel modes without params (e.g. "imns")
	Key     string            // channel key if +k is set and known
	Unread  int               // unused: the v1 UI keeps its own counts (see M5 contract)
	Lines   []Line

	// ids is Lines' parallel array of store message ids, so a MessageResolved or
	// a MessageRedacted can find the row it names. It is unexported because it
	// is bookkeeping, not view state.
	ids []model.MessageID
}

const serverBuffer = "server"

// redactedText is what a withdrawn message leaves on screen. The store keeps a
// tombstone (Kind MsgTombstone, no text); this is its rendering, and it is the
// same sentence v1's ui.go wrote into the line.
const redactedText = "(message deleted)"

// joinBatch is how many channels share one JOIN line on a rejoin. Forty open
// channels used to mean forty protocol lines on every reconnect.
const joinBatch = 10

// joinAllLimit keeps "join everything" sane: fine on a small Ergo network, but
// on a huge one the browser is the answer.
const joinAllLimit = 75

// sessionSeq numbers network ids, which must be non-empty and unique per
// Session: every Event and Change carries one, and the store keys on it.
var sessionSeq atomic64

// NewSession builds a session. Nothing happens on the wire until Connect.
func NewSession(cfg Config, ui UICallbacks) *Session {
	s := &Session{
		ui:      ui,
		Config:  cfg,
		bufs:    map[string]*Buffer{},
		barrier: make(chan chan struct{}),
		netID:   model.NetworkID(fmt.Sprintf("net-%d", sessionSeq.next())),
	}
	scrollback := int(config.Current().ScrollbackCap)
	if scrollback <= 0 {
		scrollback = state.Unlimited // v1 spells "keep everything" as 0
	}
	s.store = state.New(state.Options{
		Scrollback:    scrollback,
		HideJoinParts: config.Current().HideJoinParts,
	})
	// The status buffer exists before the first log line, as it did in v1.
	s.applyChanges(s.store.EnsureNetwork(s.netID))
	return s
}

// --- read-only surface -------------------------------------------------------

// Buffers returns every buffer in display order.
func (s *Session) Buffers() []*Buffer {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Buffer, 0, len(s.order))
	for _, k := range s.order {
		out = append(out, s.bufs[k])
	}
	return out
}

// Buffer returns the buffer with this name, or nil. Names are matched with the
// network's casemapping, so "#Chan" and "#chan" are one buffer.
func (s *Session) Buffer(name string) *Buffer {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bufs[s.foldLocked(name)]
}

// SortedMembers returns a buffer's members ranked the way the roster shows them:
// highest prefix first, then folded nick, each prefixed with its highest mode.
func (s *Session) SortedMembers(buffer string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	folded := s.foldLocked(buffer)
	if _, ok := s.bufs[folded]; !ok {
		return nil
	}
	ms := s.store.Members(model.BufferID{Network: s.netID, Folded: folded})
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		if m.Prefixes != "" {
			out = append(out, string(m.Prefixes[0])+m.Nick)
			continue
		}
		out = append(out, m.Nick)
	}
	return out
}

// ChannelList returns the last completed LIST result, most populated first.
func (s *Session) ChannelList() []ChannelInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ChannelInfo(nil), s.channels...)
}

// Connected reports whether the session is registered and usable.
func (s *Session) Connected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connectedLocked()
}

func (s *Session) connectedLocked() bool {
	return s.client != nil && s.client.Status() == model.ConnReady
}

// CurrentNick returns the nick the server assigned, falling back to the
// configured one before registration completes.
func (s *Session) CurrentNick() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil {
		return s.Config.Nick
	}
	return s.client.CurrentNick()
}

// hasCap reports whether a capability is in force on this connection.
func (s *Session) hasCap(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.client != nil && s.client.Enabled(name)
}

// multilineMaxLines returns the server's advertised draft/multiline line limit.
func (s *Session) multilineMaxLines() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil {
		return defaultMultilineMax
	}
	limits, ok := s.client.MultilineLimits()
	if !ok || limits.Lines() <= 0 {
		return defaultMultilineMax
	}
	return limits.Lines()
}

// defaultMultilineMax is Ergo's default, used until the cap says otherwise.
const defaultMultilineMax = 24

// --- lifecycle ---------------------------------------------------------------

// Connect brings the connection up and starts the adapter. It is called once
// per Session.
func (s *Session) Connect() {
	s.mu.Lock()
	if s.client != nil {
		s.mu.Unlock()
		return
	}
	cfg := irc.Config{
		Network:     s.netID,
		Server:      s.Config.Server,
		Port:        s.Config.Port,
		TLS:         s.Config.TLS,
		ServerPass:  s.Config.ServerPass,
		Nick:        s.Config.Nick,
		User:        s.Config.Nick,
		RealName:    s.Config.Nick,
		SASL:        s.saslConfig(),
		QuitMessage: "eco, a friendly IRC client",
		Version:     "eco 0.1",
		Dial:        s.Dial,
	}
	client := irc.New(cfg)
	s.client = client
	s.irc = &wire{c: client}
	s.stopped = make(chan struct{})
	events := client.Events()
	s.mu.Unlock()

	go s.pump(events)
	client.Start()
}

// saslConfig maps the profile's credentials onto the irc layer's SASL settings.
// Optional stays true, as it was in v1: a failed authentication is now reported
// as an AuthStateChanged and a line in the log rather than swallowed, and making
// it fatal at the same time would take the connection away from a user whose
// only symptom used to be an unregistered nick.
func (s *Session) saslConfig() irc.SASL {
	if s.Config.SASLUser == "" {
		return irc.SASL{}
	}
	mech := s.Config.SASLMech
	if mech == "" {
		mech = irc.MechPlain
	}
	return irc.SASL{
		Mechanism: mech,
		User:      s.Config.SASLUser,
		Pass:      s.Config.SASLPass,
		Optional:  true,
	}
}

// Quit closes the connection. It returns immediately — the QUIT is written and
// the unwind happens on its own goroutine — because it is called from the Fyne
// thread and a server that ignores QUIT would otherwise freeze the window.
func (s *Session) Quit() {
	s.mu.Lock()
	client := s.client
	s.quitting = true
	s.mu.Unlock()
	if client == nil {
		return
	}
	s.quitOnce.Do(func() { go client.Close() })
}

// pump is the adapter's goroutine: drain events, apply them, project the
// changes, fire the callbacks. See the type comment for the threading contract.
func (s *Session) pump(events <-chan model.Event) {
	defer close(s.stopped)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return
			}
			s.handle(ev)
		case ack := <-s.barrier:
			s.drain(events)
			close(ack)
		}
	}
}

// drain applies everything already queued without blocking.
func (s *Session) drain(events <-chan model.Event) {
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return
			}
			s.handle(ev)
		default:
			return
		}
	}
}

// settle blocks until the pump has applied every event the client has already
// emitted. It is a TEST SEAM in the same family as Dial and stopped: the fake
// server's PING/PONG barrier proves the client has *decoded* every earlier line,
// and this proves the adapter has *folded it in* (see testsrv.Server.Barrier).
func (s *Session) settle() {
	s.mu.Lock()
	started := s.stopped
	s.mu.Unlock()
	if started == nil {
		return // Connect was never called; there is no pump to wait for
	}
	ack := make(chan struct{})
	select {
	case s.barrier <- ack:
	case <-started:
		return
	}
	select {
	case <-ack:
	case <-started:
	}
}

// handle folds one event in: ignore filter, store, projection, wire reactions.
func (s *Session) handle(ev model.Event) {
	ev, keep := filterIgnored(ev)
	if !keep {
		return
	}
	s.mu.Lock()
	s.syncHiddenLocked()
	cbs := s.applyChanges(s.store.Apply(ev))
	cbs = append(cbs, s.reactLocked(ev)...)
	s.mu.Unlock()
	for _, cb := range cbs {
		cb()
	}
}

// --- the conductor -----------------------------------------------------------

// reactLocked is the application policy that sits between the wire and the
// store: what eco *asks for* in response to what it was told. internal/irc will
// not invent a request and the store may not write to a socket, so the two
// decisions v1 made from its callbacks — "we just joined, fetch modes and
// scrollback" and "we are ready, rejoin what the user had open" — live here.
func (s *Session) reactLocked(ev model.Event) []func() {
	switch e := ev.(type) {
	case model.ConnState:
		if e.Status == model.ConnReady {
			return s.onReadyLocked()
		}
	case model.Join:
		if !e.Playback && s.isSelfLocked(e.Sender) {
			s.onSelfJoinLocked(e.Channel)
		}
	case model.CapChange:
		if e.Op == model.CapLS || e.Op == model.CapNEW {
			return s.stsCallbacks(e)
		}
	}
	return nil
}

// onReadyLocked joins the profile's autojoin channels and rejoins everything
// else the user still considers themselves in. The two lists are kept apart the
// way v1 kept them: a channel in both was joined twice, which doubled its join
// line and its history fetch.
func (s *Session) onReadyLocked() []func() {
	autojoined := map[string]bool{}
	for _, ch := range strings.Split(s.Config.Channels, ",") {
		ch = strings.TrimSpace(ch)
		if ch == "" {
			continue
		}
		autojoined[s.foldLocked(ch)] = true
		s.join(ch, "")
	}

	var plain []string
	for _, t := range s.store.RejoinTargets(s.netID) {
		if autojoined[s.foldLocked(t.Channel)] {
			continue
		}
		if t.Key != "" {
			// v1 rejoined keyed channels without their key and they silently
			// failed; the store remembers the key from MODE.
			s.join(t.Channel, t.Key)
			continue
		}
		plain = append(plain, t.Channel)
	}
	for i := 0; i < len(plain); i += joinBatch {
		s.join(strings.Join(plain[i:min(i+joinBatch, len(plain))], ","), "")
	}

	if config.Current().AutoScanChannels {
		_, _ = s.client.List()
	}
	return nil
}

// onSelfJoinLocked asks for the channel's modes and its scrollback, once per
// open buffer — the same two requests v1 issued from its JOIN handler.
func (s *Session) onSelfJoinLocked(channel string) {
	_ = s.client.Mode(channel)
	id := model.BufferID{Network: s.netID, Folded: s.foldLocked(channel)}
	if !s.store.HasCap(s.netID, "draft/chathistory") || !s.store.NeedsHistory(id) {
		return
	}
	if _, err := s.client.History(irc.HistoryRequest{
		Target: channel, Direction: model.HistoryLatest,
	}); err == nil {
		s.store.MarkHistoryRequested(id)
	}
}

// stsCallbacks reports an sts policy advertised in CAP LS/NEW. The irc layer
// publishes it on the CapSet for the next dial (Client.STS); this hands it to
// ui.go, which owns the "persist only from a TLS connection" rule.
func (s *Session) stsCallbacks(e model.CapChange) []func() {
	if s.ui.STS == nil {
		return nil
	}
	var out []func()
	for _, c := range e.Caps {
		if !strings.EqualFold(c.Name, "sts") {
			continue
		}
		port, duration := config.ParseSTSValue(c.Value)
		out = append(out, func() { s.ui.STS(port, duration) })
	}
	return out
}

// isSelfLocked reports whether a sender is us.
func (s *Session) isSelfLocked(sender model.Sender) bool {
	if sender.Self {
		return true
	}
	nick, _, _ := s.store.Self(s.netID)
	if nick == "" && s.client != nil {
		nick = s.client.CurrentNick()
	}
	return nick != "" && strings.EqualFold(sender.Nick, nick)
}

// join writes one JOIN, ignoring "not connected" — the caller is reacting to
// something the server said, so there is nobody left to tell.
func (s *Session) join(target, key string) {
	if target == "" {
		return
	}
	_, _ = s.client.Join(target, key)
}

// --- outgoing ----------------------------------------------------------------

// SendMessage sends the composer's text to a buffer and shows it immediately.
//
// The local copy is a *pending* message carrying the labelled-response label
// internal/irc put on the wire; the echo resolves that row in place, giving it
// the server's msgid and timestamp. That is what replaced v1's pendingEcho,
// which matched the echo by its text and so could not tell two identical
// messages apart, and left our own messages with no msgid to redact.
func (s *Session) SendMessage(buffer, text string) {
	if strings.EqualFold(buffer, serverBuffer) {
		s.status(buffer, "This is the server log. Join a channel to chat (try /join #chat).")
		return
	}
	s.mu.Lock()
	if !s.connectedLocked() {
		s.mu.Unlock()
		s.status(buffer, "Not connected.")
		return
	}
	h, err := s.client.SendMessage(buffer, text)
	if err != nil {
		s.mu.Unlock()
		s.status(buffer, "Not connected.")
		return
	}
	var cbs []func()
	if h.Truncated {
		cbs = append(cbs, s.statusLocked(buffer,
			fmt.Sprintf("Message truncated to the server's %d line limit.", len(h.Lines)))...)
	}
	cbs = append(cbs, s.echoLocked(buffer, h, model.MsgPrivmsg)...)
	s.mu.Unlock()
	run(cbs)
}

// SendAction sends a /me. A single ACTION cannot carry a line break, so the
// body is folded to one line before it goes out and before it is echoed.
func (s *Session) SendAction(buffer, text string) {
	s.mu.Lock()
	if !s.connectedLocked() {
		s.mu.Unlock()
		return
	}
	h, err := s.client.SendAction(buffer, text)
	if err != nil {
		s.mu.Unlock()
		return
	}
	cbs := s.echoOneLocked(buffer, h.Label(), foldNewlines(text), model.MsgAction)
	s.mu.Unlock()
	run(cbs)
}

// echoLocked renders what was just sent. One pending row per independently
// resolvable echo: a draft/multiline batch comes back as one message, and lines
// sent separately (no multiline capability) come back one at a time.
func (s *Session) echoLocked(buffer string, h irc.Handle, kind model.MessageKind) []func() {
	if h.Multiline || len(h.Labels) <= 1 {
		return s.echoOneLocked(buffer, h.Label(), strings.Join(h.Lines, "\n"), kind)
	}
	var out []func()
	for i, line := range h.Lines {
		label := ""
		if i < len(h.Labels) {
			label = h.Labels[i]
		}
		out = append(out, s.echoOneLocked(buffer, label, line, kind)...)
	}
	return out
}

func (s *Session) echoOneLocked(buffer, label, text string, kind model.MessageKind) []func() {
	b, cbs := s.openLocked(buffer)
	if b == nil {
		return cbs
	}
	if label == "" && s.client.Enabled("echo-message") {
		// The server will send this message back to us and we have no label to
		// recognise it by, so rendering a local copy now would show it twice.
		// Its own echo is the message. (v1 dropped the echo by matching its
		// text; that is the heuristic MUST 3 deleted, and re-adding it here
		// would re-add the bug where two identical messages ate one echo.)
		return cbs
	}
	_, changes := s.store.Echo(s.idLocked(buffer), state.Outgoing{
		Label: label, Text: text, Kind: kind,
	})
	return append(cbs, s.applyChanges(changes)...)
}

// foldNewlines is what an ACTION does to a body: a line break would make the
// protocol line unsendable.
func foldNewlines(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r", " "), "\n", " ")
}

// JoinUser is a join the USER initiated (command, browser, menu): a refusal for
// one of these may prompt in the UI, unlike autojoin and reconnect joins, which
// must never pop dialogs.
func (s *Session) JoinUser(ch string) { s.JoinWithKey(ch, "") }

// JoinWithKey joins a keyed (+k) channel on the user's behalf.
func (s *Session) JoinWithKey(ch, key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil || ch == "" {
		return
	}
	s.store.RequestJoin(s.netID, ch)
	_, _ = s.client.Join(ch, key)
}

// Part leaves a channel and closes its buffer. The buffer normally closes on the
// server's PART echo, but a server that thinks we are not on the channel answers
// 442/403 instead, and that reply has to close it too; with no live connection it
// closes locally.
func (s *Session) Part(ch string) {
	s.mu.Lock()
	if !s.connectedLocked() {
		cbs := s.applyChanges(s.store.PartLocally(s.idLocked(ch)))
		s.mu.Unlock()
		run(cbs)
		return
	}
	h, err := s.client.Part(ch, "")
	if err != nil {
		cbs := s.applyChanges(s.store.PartLocally(s.idLocked(ch)))
		s.mu.Unlock()
		run(cbs)
		return
	}
	s.store.RequestPart(s.idLocked(ch), h.Label())
	s.mu.Unlock()
}

// Cycle parts and rejoins a channel, keeping its buffer and its scrollback.
func (s *Session) Cycle(ch string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil {
		return
	}
	s.store.RequestCycle(s.idLocked(ch))
	if _, err := s.client.Part(ch, ""); err != nil {
		return
	}
	_, _ = s.client.Join(ch, "")
}

// CloseBuffer removes a buffer the user closed. The network's status buffer
// cannot be closed: it survives every teardown path.
func (s *Session) CloseBuffer(name string) {
	s.mu.Lock()
	cbs := s.applyChanges(s.store.CloseBuffer(s.idLocked(name)))
	s.mu.Unlock()
	run(cbs)
}

// ClearBuffer wipes one buffer's history, client side, so /history can refill it.
func (s *Session) ClearBuffer(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	folded := s.foldLocked(name)
	s.store.ClearBuffer(model.BufferID{Network: s.netID, Folded: folded})
	if b := s.bufs[folded]; b != nil {
		b.Lines, b.ids = nil, nil
	}
}

// RequestList asks the server for the channel list on the user's behalf; the
// browser opens when the result arrives.
func (s *Session) RequestList() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil {
		return
	}
	s.manualList = true
	_, _ = s.client.List()
}

// Whois asks about a nick and records which buffer asked, so the replies land
// where the question was. This is the per-request correlation that replaced v1's
// single whoisTarget slot — which was never cleared, so the last caller kept
// receiving other buffers' answers.
func (s *Session) Whois(from, nick string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil || nick == "" {
		return
	}
	s.store.TrackWhois(s.netID, nick, s.idLocked(from))
	_, _ = s.client.Whois(nick)
}

// getBuffer returns a buffer, creating it if the user just named one that does
// not exist yet (/msg, a mention link).
func (s *Session) getBuffer(name string) *Buffer {
	s.mu.Lock()
	b, cbs := s.openLocked(name)
	s.mu.Unlock()
	run(cbs)
	return b
}

// status writes a client-generated line into a buffer. These are the app's own
// sentences (usage errors, connection chrome); they live in the projection only,
// so the store's message log stays a record of the conversation.
func (s *Session) status(buffer, text string) {
	s.mu.Lock()
	cbs := s.statusLocked(buffer, text)
	s.mu.Unlock()
	run(cbs)
}

func (s *Session) statusLocked(buffer, text string) []func() {
	b, cbs := s.openLocked(buffer)
	if b == nil {
		return cbs
	}
	l := Line{Time: time.Now(), Text: text, Status: true}
	name := b.Name
	s.appendLine(b, 0, l)
	return append(cbs, s.addLineCB(name, l))
}

// --- projection --------------------------------------------------------------

// applyChanges folds a batch of Changes into the projection and returns the UI
// callbacks they imply. Callers hold s.mu; the returned callbacks run after it
// is released.
func (s *Session) applyChanges(cs []model.Change) []func() {
	var out []func()
	for _, c := range cs {
		switch ch := c.(type) {
		case model.BufferCreated:
			s.mirrorCreate(ch.Info)
			out = append(out, s.bufferListCB())

		case model.BufferClosed:
			s.mirrorRemove(ch.Meta().Buffer.Folded)
			out = append(out, s.bufferListCB())

		case model.BufferRenamed:
			if cb := s.mirrorRename(ch); cb != nil {
				out = append(out, cb, s.bufferListCB())
			}

		case model.MessageAdded:
			out = append(out, s.addMessage(ch.Meta().Buffer, ch.Message)...)

		case model.HistoryLoaded:
			for _, m := range ch.Messages {
				out = append(out, s.addMessage(ch.Meta().Buffer, m)...)
			}

		case model.MessageResolved:
			out = append(out, s.resolveMessage(ch)...)

		case model.MessageFailed:
			out = append(out, s.statusLocked(s.nameOf(ch.Meta().Buffer),
				"Message not sent: "+firstNonEmpty(ch.Reason, ch.Code))...)

		case model.MessageRedacted:
			out = append(out, s.redactMessage(ch)...)

		case model.MembersChanged:
			out = append(out, s.membersChanged(ch.Meta().Buffer)...)

		case model.TopicChanged:
			out = append(out, s.topicChanged(ch)...)

		case model.ModeChanged:
			if b := s.bufs[ch.Meta().Buffer.Folded]; b != nil {
				b.Modes, b.Key = ch.Modes, ch.Key
			}

		case model.ConnStateChanged:
			out = append(out, s.connStateChanged(ch)...)

		case model.ListUpdated:
			out = append(out, s.listUpdated(ch)...)

		case model.JoinFailed:
			if s.ui.JoinFailed != nil {
				channel, reason, needKey := ch.Channel, ch.Reason, ch.NeedKey
				out = append(out, func() { s.ui.JoinFailed(channel, reason, needKey) })
			}
		}
	}
	return out
}

// mirrorCreate adds a buffer to the projection.
func (s *Session) mirrorCreate(info model.BufferInfo) *Buffer {
	if b, ok := s.bufs[info.ID.Folded]; ok {
		return b
	}
	b := &Buffer{Name: info.Name, Members: map[string]string{}}
	s.bufs[info.ID.Folded] = b
	s.order = append(s.order, info.ID.Folded)
	// A new buffer adopts the network's join/part visibility for its name.
	s.store.SetHideJoinParts(info.ID, joinPartsHiddenFor(s.Name, info.Name))
	return b
}

// mirrorRemove drops a buffer from both the map and the display order — v1's
// nil-ghost bug was deleting from only one of them.
func (s *Session) mirrorRemove(folded string) {
	if _, ok := s.bufs[folded]; !ok {
		return
	}
	delete(s.bufs, folded)
	for i, k := range s.order {
		if k == folded {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
}

// mirrorRename moves a buffer to its new identity, keeping everything it holds.
func (s *Session) mirrorRename(ch model.BufferRenamed) func() {
	b, ok := s.bufs[ch.From.Folded]
	if !ok {
		return nil
	}
	oldName := b.Name
	delete(s.bufs, ch.From.Folded)
	newFolded := ch.Meta().Buffer.Folded
	b.Name = ch.Name
	s.bufs[newFolded] = b
	for i, k := range s.order {
		if k == ch.From.Folded {
			s.order[i] = newFolded
			break
		}
	}
	newName := b.Name
	return func() {
		if s.ui.RenameBuffer != nil {
			s.ui.RenameBuffer(oldName, newName)
		}
	}
}

// addMessage appends one store message to a buffer's rows.
func (s *Session) addMessage(id model.BufferID, m model.Message) []func() {
	b := s.bufs[id.Folded]
	if b == nil {
		return nil
	}
	l := lineOf(m)
	s.appendLine(b, m.ID, l)
	return []func(){s.addLineCB(b.Name, l)}
}

// appendLine adds a row and applies the scrollback cap. Trimming is projection
// side only: the UI is handed every line and does its own bookkeeping.
func (s *Session) appendLine(b *Buffer, id model.MessageID, l Line) {
	b.Lines = append(b.Lines, l)
	b.ids = append(b.ids, id)
	// a cap of 0 means All: keep every line in memory
	if cap := int(config.Current().ScrollbackCap); cap > 0 && len(b.Lines) > cap {
		b.Lines = b.Lines[len(b.Lines)-cap:]
		b.ids = b.ids[len(b.ids)-cap:]
	}
}

// resolveMessage upgrades a pending row in place: it gains the server's msgid
// and timestamp, and stops being pending. The row keeps its position, so the UI
// amends the line it already drew instead of drawing a second one — and only
// hears about it when the visible text actually changed (a server that rewrote
// the message), because a redraw per sent message is a redraw per keystroke of
// somebody's paste.
func (s *Session) resolveMessage(ch model.MessageResolved) []func() {
	b := s.bufs[ch.Meta().Buffer.Folded]
	if b == nil {
		return nil
	}
	i := b.index(ch.ID)
	if i < 0 {
		return nil
	}
	before := b.Lines[i]
	b.Lines[i] = lineOf(ch.Message)
	if before.Text == b.Lines[i].Text || s.ui.AmendBuffer == nil {
		return nil
	}
	name := b.Name
	return []func(){func() { s.ui.AmendBuffer(name) }}
}

// redactMessage turns a withdrawn message into its tombstone. The store already
// dropped the text; this is the row the reader sees, and it is the same one v1's
// ui.go wrote — the difference is that the core owns it now, so the line is gone
// from state as well as from the screen.
func (s *Session) redactMessage(ch model.MessageRedacted) []func() {
	b := s.bufs[ch.Meta().Buffer.Folded]
	if b == nil {
		return nil
	}
	if i := b.index(ch.ID); i >= 0 {
		b.Lines[i] = Line{Time: b.Lines[i].Time, Text: redactedText, Status: true, MsgID: ch.MsgID}
	}
	if s.ui.RedactMsg == nil {
		return nil
	}
	name, msgid := b.Name, ch.MsgID
	return []func(){func() { s.ui.RedactMsg(name, msgid) }}
}

// membersChanged republishes a buffer's roster. The store hands out deltas; the
// v1 member list is a whole map, so it is rebuilt from the store's ranked view.
func (s *Session) membersChanged(id model.BufferID) []func() {
	b := s.bufs[id.Folded]
	if b == nil {
		return nil
	}
	members := make(map[string]string, len(b.Members))
	for _, m := range s.store.Members(id) {
		members[m.Nick] = m.Prefixes
	}
	b.Members = members
	if s.ui.MembersChange == nil {
		return nil
	}
	name := b.Name
	return []func(){func() { s.ui.MembersChange(name) }}
}

// topicChanged republishes a topic. The store keeps it exactly as the wire
// carried it, formatting codes and all; the topic bar is a plain label, so the
// codes are stripped here rather than leaked into it.
func (s *Session) topicChanged(ch model.TopicChanged) []func() {
	b := s.bufs[ch.Meta().Buffer.Folded]
	if b == nil {
		return nil
	}
	b.Topic = ircfmt.Strip(ch.Topic)
	if s.ui.TopicChange == nil {
		return nil
	}
	name, topic := b.Name, b.Topic
	return []func(){func() { s.ui.TopicChange(name, topic) }}
}

// connStateChanged maps the typed connection state onto v1's (connected, text)
// pair, which is all ui.go's status dot understands.
func (s *Session) connStateChanged(ch model.ConnStateChanged) []func() {
	if s.ui.StatusChange == nil {
		return nil
	}
	var connected bool
	var text string
	switch ch.Status {
	case model.ConnReady:
		connected, text = true, "Connected to "+s.Config.Server+" as "+ch.Nick
	case model.ConnFailed:
		text = "Connection failed: " + firstNonEmpty(ch.Err, "the server rejected the connection")
	case model.ConnDisconnected:
		text = "Disconnected"
		if ch.RetryIn > 0 && !s.quitting {
			text = "Disconnected. Reconnecting..."
		}
	default:
		return nil // connecting/registering/authenticating: v1 said nothing
	}
	return []func(){func() { s.ui.StatusChange(connected, text) }}
}

// listUpdated collects a channel scan and, when it ends, does what v1's 323
// handler did: report the count, optionally join everything on the first scan,
// and open the browser for a scan somebody asked for.
func (s *Session) listUpdated(ch model.ListUpdated) []func() {
	if !ch.Done {
		return nil
	}
	items := s.store.ChannelList(s.netID)
	s.channels = make([]ChannelInfo, 0, len(items))
	names := make([]string, 0, len(items))
	for _, ci := range items {
		s.channels = append(s.channels, ChannelInfo{Name: ci.Name, Users: ci.Users, Topic: ci.Topic})
		names = append(names, ci.Name)
	}
	firstScan := !s.firstScanDone
	s.firstScanDone = true
	manual := s.manualList
	s.manualList = false

	out := s.statusLocked(serverBuffer,
		fmt.Sprintf("Channel scan complete: %d channels found.", len(names)))

	// Only the session's FIRST scan may auto-join: a later /list must never
	// re-join channels the user deliberately left.
	if config.Current().JoinAllChannels && firstScan && len(names) > 0 && len(names) <= joinAllLimit {
		for i := 0; i < len(names); i += joinBatch {
			s.join(strings.Join(names[i:min(i+joinBatch, len(names))], ","), "")
		}
		return append(out, s.statusLocked(serverBuffer,
			fmt.Sprintf("Joining all %d channels.", len(names)))...)
	}
	if config.Current().JoinAllChannels && firstScan && len(names) > joinAllLimit {
		out = append(out, s.statusLocked(serverBuffer, fmt.Sprintf(
			"Too many channels to join them all (%d > %d); pick from the browser instead.",
			len(names), joinAllLimit))...)
	}
	// pop the browser for user-requested scans and the first connect, but never
	// for a background rescan after an auto-reconnect
	if (manual || firstScan) && s.ui.ChannelListDone != nil {
		out = append(out, func() { s.ui.ChannelListDone() })
	}
	return out
}

// lineOf projects a store message onto the row the render layer draws.
func lineOf(m model.Message) Line {
	l := Line{
		Time:      m.Time,
		Text:      m.Text,
		MsgID:     m.MsgID,
		Self:      m.Sender.Self,
		Highlight: m.Highlight,
		Playback:  m.Playback,
	}
	switch m.Kind {
	case model.MsgStatus:
		l.Status = true
	case model.MsgTombstone:
		l.Status, l.Text = true, redactedText
	case model.MsgAction:
		l.Action, l.Nick = true, m.Sender.Nick
	case model.MsgNotice:
		l.Notice, l.Nick = true, m.Sender.Nick
	default:
		l.Nick = m.Sender.Nick
	}
	return l
}

// index returns the row holding a store message, or -1. It walks backwards: the
// message being resolved or redacted is almost always a recent one.
func (b *Buffer) index(id model.MessageID) int {
	if id == 0 {
		return -1
	}
	for i := len(b.ids) - 1; i >= 0; i-- {
		if b.ids[i] == id {
			return i
		}
	}
	return -1
}

// --- small helpers -----------------------------------------------------------

// openLocked returns a buffer by name, creating it if needed. The server buffer
// answers to its display name even though its folded key is empty.
func (s *Session) openLocked(name string) (*Buffer, []func()) {
	folded := s.foldLocked(name)
	if b, ok := s.bufs[folded]; ok {
		return b, nil
	}
	if folded == "" {
		return nil, nil // no status buffer yet: the network is not set up
	}
	_, changes := s.store.OpenBuffer(s.netID, name)
	cbs := s.applyChanges(changes)
	return s.bufs[folded], cbs
}

// foldLocked maps a display name onto its buffer key.
func (s *Session) foldLocked(name string) string {
	if strings.EqualFold(name, serverBuffer) {
		return "" // the status buffer's identity is the network, not a name
	}
	return s.store.BufferID(s.netID, name).Folded
}

// idLocked is foldLocked as a BufferID.
func (s *Session) idLocked(name string) model.BufferID {
	return model.BufferID{Network: s.netID, Folded: s.foldLocked(name)}
}

// nameOf returns a buffer's display name.
func (s *Session) nameOf(id model.BufferID) string {
	if b := s.bufs[id.Folded]; b != nil {
		return b.Name
	}
	return serverBuffer
}

func (s *Session) addLineCB(buffer string, l Line) func() {
	return func() {
		if s.ui.AddLine != nil {
			s.ui.AddLine(buffer, l)
		}
	}
}

func (s *Session) bufferListCB() func() {
	return func() {
		if s.ui.BufferList != nil {
			s.ui.BufferList()
		}
	}
}

// syncHiddenLocked pushes the app's join/part visibility preferences into the
// store, which is what decides whether a JOIN leaves a line. Both sources are
// generation-counted, so the common case is two comparisons.
func (s *Session) syncHiddenLocked() {
	mark := jpGeneration()
	if mark == s.jpMark {
		return
	}
	s.jpMark = mark
	s.store.SetNetworkHideJoinParts(s.netID, config.Current().HideJoinParts)
	for _, folded := range s.order {
		b := s.bufs[folded]
		if b == nil {
			continue
		}
		s.store.SetHideJoinParts(model.BufferID{Network: s.netID, Folded: folded},
			joinPartsHiddenFor(s.Name, b.Name))
	}
}

// filterIgnored drops messages from ignored nicks before they reach the store,
// which is where v1 dropped them too. The store has a per-network ignore list of
// its own (blueprint MUST 12); wiring the UI to it is M8's job, and until then
// the process-wide v1 set is the one the preferences dialog writes.
func filterIgnored(ev model.Event) (model.Event, bool) {
	switch e := ev.(type) {
	case model.PrivMsg:
		return ev, !ignoredSender(e.Sender)
	case model.Notice:
		return ev, !ignoredSender(e.Sender)
	case model.Typing:
		return ev, !ignoredSender(e.Sender)
	case model.HistoryChunk:
		kept := make([]model.Event, 0, len(e.Events))
		for _, sub := range e.Events {
			if s, ok := filterIgnored(sub); ok {
				kept = append(kept, s)
			}
		}
		e.Events = kept
		return e, true
	}
	return ev, true
}

func ignoredSender(sender model.Sender) bool {
	return !sender.Self && sender.Nick != "" && isIgnored(sender.Nick)
}

// run fires the callbacks collected under the lock.
func run(cbs []func()) {
	for _, cb := range cbs {
		cb()
	}
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// wire is the write side of a session as the commands, the browser and the
// dialogs use it: Session.irc. It exists so those call sites did not have to
// change at the cutover — they were written against ircevent's Connection, and
// this is the same handful of verbs against internal/irc.Client. M10's command
// table replaces the whole surface with typed commands.
type wire struct{ c *irc.Client }

// Send writes one command with no label.
func (w *wire) Send(command string, params ...string) { _ = w.c.Send(command, params...) }

// SendRaw writes an already-formatted protocol line.
func (w *wire) SendRaw(line string) { _ = w.c.SendRaw(line) }

// Privmsg sends a message without a local echo — these are commands addressed to
// services, not something the user typed into a buffer.
func (w *wire) Privmsg(target, text string) { _, _ = w.c.SendMessage(target, text) }

// Notice sends a NOTICE.
func (w *wire) Notice(target, text string) { _, _ = w.c.SendNotice(target, text) }

// Join joins a channel on the client's own initiative — no "the user asked for
// this" flag, so a refusal stays in the log instead of raising a dialog.
func (w *wire) Join(channel string) { _, _ = w.c.Join(channel, "") }

// SetNick asks for a different nick.
func (w *wire) SetNick(nick string) { _ = w.c.SetNick(nick) }

// CurrentNick returns the nick the server assigned.
func (w *wire) CurrentNick() string { return w.c.CurrentNick() }

// EnabledCaps lists the capabilities in force, for logs and the live suite.
func (w *wire) EnabledCaps() []string { return w.c.Caps().EnabledNames() }

// atomic64 is a tiny monotonic counter; sync/atomic's types would need a named
// import here only for this.
type atomic64 struct {
	mu sync.Mutex
	n  int
}

func (a *atomic64) next() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.n++
	return a.n
}
