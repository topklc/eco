package app

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ergochat/irc-go/ircfmt"
	"github.com/toprakkilic/eco/internal/config"
	"github.com/toprakkilic/eco/internal/irc"
	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/state"
	"github.com/toprakkilic/eco/internal/ui/dispatch"
	"github.com/toprakkilic/eco/internal/ui/viewmodel"
)

// Session is one network connection: internal/irc.Client (the wire) and
// internal/state.Store (the model), with the conductor between them and a
// projection of the store into the Buffer shape ui.go still reads.
//
// The shape is deliberate. The store is the single source of truth for identity
// and correctness: folded buffer keys, per-buffer msgid dedupe, labelled echo
// resolution, batch assembly, highlight rules, mode and member state, the
// reconnect plan. What lives here is a *projection* of that into the v1 Buffer
// shape (Lines/Members/Topic/Modes/Key), because ui.go reads those fields
// directly. M8's viewmodel carve-out is what deletes the projection.
//
// Threading: one goroutine (pump) drains Client.Events(), applies each event to
// the store and folds the resulting Changes into the projection. Everything
// touching the store or the projection holds s.mu — the store is single-writer,
// and the UI reads Buffer fields under the same lock v1 used. The dispatch
// Items a step produced are collected under the lock and emitted after it is
// released, so nothing the screen does can reach back in and deadlock.
//
// M8 replaced the twelve UICallbacks with that funnel. There is no longer any
// UI state here, no "is this session still the live one?" question (the
// dispatcher answers it once, by session id) and no fyne.Do (it happens once
// per batch, inside internal/ui/dispatch).
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

	// disp is the funnel to the screen and sid is this session's identity in
	// it — stable for the session's whole life, and never s.Name (which the
	// user may edit and a reconnect reuses).
	disp *dispatch.Dispatcher
	sid  dispatch.SessionID

	bufs  map[string]*Buffer // folded buffer key -> projection
	order []string           // folded keys, display order, server buffer first

	firstScanDone bool              // join-all runs only on the session's first LIST
	manualList    bool              // the next LIST result was requested by the user
	quitting      bool              // Quit was called; a disconnect is expected
	jpMark        int               // join/part visibility generation last pushed to the store
	notifyLevel   state.NotifyLevel // last level pushed to the store
	attnMark      string            // attention settings (words, smart filter) last pushed
	dmPrompted    bool              // the first-DM logging prompt has been raised

	// The archive (blueprint §5.4, and archive.go for the shape of it). arc is
	// nil when this session keeps no history: the offline suite's default, and
	// the fallback when the file could not be opened.
	arc         *arc
	arcErr      error
	arcBase     int64           // the archive's highest rowid when this session opened it
	arcReported bool            // a disk failure has already been put in the log
	noRecord    bool            // folding changes that CAME from the archive
	hydrated    map[string]bool // buffers whose cold-start page was requested
	paging      map[string]bool // buffers with a scroll-up in flight
	pagingWire  map[string]bool // ...of those, the ones waiting on the server
	exhausted   map[string]bool // buffers nobody has anything older for

	// The reconnect catch-up, and whether this session has ever been ready
	// (the first connection has no gap to fill).
	gap            gapFill
	everReady      bool
	targetsPending bool

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

	// Archive is the path to this profile's message archive (blueprint §5.4).
	// Empty means the session keeps no history on disk — what every test that
	// does not ask for one gets, and what a profile falls back to when its data
	// directory cannot be written.
	Archive string
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

// serverBuffer is the display name of a network's status buffer. It is the
// viewmodel's constant so the sidebar's root rows and this layer's buffer
// lookups cannot drift apart (and state.ServerBufferName is the third copy the
// two are checked against by every test that opens the server log).
const serverBuffer = viewmodel.ServerBuffer

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

// NewSession builds a session and registers it with the funnel, so it is live
// from birth and nothing it produces can reach the screen unannounced. Nothing
// happens on the wire until Connect.
func NewSession(cfg Config, d *dispatch.Dispatcher) *Session {
	netID := model.NetworkID(fmt.Sprintf("net-%d", sessionSeq.next()))
	s := &Session{
		disp:       d,
		sid:        dispatch.SessionID(netID),
		Config:     cfg,
		bufs:       map[string]*Buffer{},
		barrier:    make(chan chan struct{}),
		netID:      netID,
		hydrated:   map[string]bool{},
		paging:     map[string]bool{},
		pagingWire: map[string]bool{},
		exhausted:  map[string]bool{},
	}
	d.Open(s.sid)
	scrollback := int(config.Current().ScrollbackCap)
	if scrollback <= 0 {
		scrollback = state.Unlimited // v1 spells "keep everything" as 0
	}
	// The archive is opened BEFORE the store, because the store's id sequence
	// continues the archive's rowids: that is what makes a restored message's
	// id its own, and what lets one comparison order a window that is half
	// memory and half file (model.IDSeq).
	ids := s.attachArchive(cfg.Archive)
	s.notifyLevel = notifyLevel(config.Current().NotifyAll)
	set := config.Current()
	s.store = state.New(state.Options{
		Scrollback:     scrollback,
		HideJoinParts:  set.HideJoinParts,
		SmartJoinParts: set.SmartJoinParts,
		SpeakerWindow:  speakerWindow(set),
		HighlightWords: set.HighlightWords,
		DefaultNotify:  s.notifyLevel,
		IDs:            ids,
	})
	s.attnMark = attentionGeneration(set)
	// The status buffer exists before the first log line, as it did in v1.
	s.emit(s.applyChanges(s.store.EnsureNetwork(s.netID)))
	if s.arcErr != nil {
		s.status(serverBuffer, "History is not being saved on this connection: "+s.arcErr.Error())
	}
	return s
}

// notifyLevel maps the "tell me about everything" preference onto the store's
// per-network default. §4.4's evaluation order starts at the buffer's level, so
// the preference has to BE a level — otherwise the rule is evaluated twice, in
// two places, which is the duplication M8 left behind.
func notifyLevel(all bool) state.NotifyLevel {
	if all {
		return state.NotifyAll
	}
	return state.NotifyMentions
}

// ID is this session's identity in the funnel. The UI uses it to retire a
// session it has torn down (dispatch.Dispatcher.Close) and to tell whose change
// it is being handed.
func (s *Session) ID() dispatch.SessionID { return s.sid }

// emit hands one step's items to the funnel. It must be called with s.mu
// released: a full queue blocks here, and holding the store's lock while the
// screen catches up is how a UI thread ends up owning the model.
func (s *Session) emit(items []dispatch.Item) {
	if len(items) == 0 {
		return
	}
	s.disp.Emit(s.sid, items)
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

// topicOf is a buffer's topic, or "" when there is no such buffer. It exists
// so the window can ask for one fact under the lock instead of holding a
// *Buffer and reaching into it.
func (s *Session) topicOf(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b := s.bufs[s.foldLocked(name)]; b != nil {
		return b.Topic
	}
	return ""
}

// Roster returns a buffer's members as the store ranks them: highest prefix
// first, then folded nick, each carrying everything the People pane draws —
// its prefixes, its account (account-tag) and its away state (away-notify).
func (s *Session) Roster(buffer string) []model.Member {
	s.mu.Lock()
	defer s.mu.Unlock()
	folded := s.foldLocked(buffer)
	if _, ok := s.bufs[folded]; !ok {
		return nil
	}
	return s.store.Members(model.BufferID{Network: s.netID, Folded: folded})
}

// Self is who we are on this network: the nick the server settled on, the
// account we are logged in as (empty when we are not) and our own away state.
// It is the store's answer, not a copy kept beside it — the identity chip asks
// for it on a buffer switch, and SelfChanged pushes the same three facts when
// they move.
func (s *Session) Self() (nick, account string, away bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	nick, account, away = s.store.Self(s.netID)
	if nick == "" {
		nick = s.Config.Nick // before registration, the one we asked for
	}
	return nick, account, away
}

// Prefixes is the network's ISUPPORT PREFIX table: the membership mode letters
// and the prefix characters that grant them, highest rank first. It is what
// the People pane ranks and titles its sections from — v1's hardcoded "~&@%+"
// is wrong on any server that advertises something else.
func (s *Session) Prefixes() (modes, prefixes string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.store.Props(s.netID)
	return p.Prefix.Modes, p.Prefix.Prefixes
}

// SortedMembers is the roster as display strings — the nick with its highest
// prefix in front — which is what tab completion walks.
func (s *Session) SortedMembers(buffer string) []string {
	ms := s.Roster(buffer)
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

// ChannelList returns the last completed LIST result, most populated first. It
// is the browser's "do I have anything to show?" — the browser itself asks
// Channels, which queries the cache the scan streamed into.
func (s *Session) ChannelList() []ChannelInfo { return s.Channels("", browseLimit) }

// browseLimit is how much of the channel cache the browser holds at once. A
// network with fifty thousand channels is a search problem, not a list.
const browseLimit = 2000

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
//
// The archive is closed on that same goroutine and not on this one: closing it
// flushes, and a flush is a disk write that the window must not wait for.
// Everything the session accepted is on disk by the time it finishes.
func (s *Session) Quit() {
	s.mu.Lock()
	client := s.client
	s.quitting = true
	s.mu.Unlock()
	s.quitOnce.Do(func() {
		go func() {
			if client != nil {
				client.Close()
			}
			_ = s.closeArchive()
		}()
	})
}

// closeArchive shuts the archive down and waits for it. It is idempotent and
// safe to call alongside Quit, which is what a test that wants to reopen the
// file — and prove history survived — calls to know the writing has stopped.
func (s *Session) closeArchive() error { return s.arc.close() }

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
// emitted AND the funnel has handed the result to the screen. It is a TEST SEAM
// in the same family as Dial and stopped: the fake server's PING/PONG barrier
// proves the client has *decoded* every earlier line, this proves the session
// has *folded it in* and the sink has *seen* it (see testsrv.Server.Barrier).
//
// The funnel half matters since M8: delivery is coalesced on an 80ms tick, so
// without a barrier through it an assertion made straight after srv.Sync() would
// be racing the tick.
// Since M9 there is a third stage: the archive's worker. A message reaching the
// store queues a write, and a buffer opening queues a READ that comes back as
// more items, so "everything has landed" is only true once the disk has caught
// up and the funnel has been drained again after it.
func (s *Session) settle() {
	s.mu.Lock()
	started := s.stopped
	s.mu.Unlock()
	if started == nil {
		s.disp.Flush() // Connect was never called; there is no pump to wait for
		s.arc.sync()
		s.disp.Flush()
		return
	}
	ack := make(chan struct{})
	select {
	case s.barrier <- ack:
	case <-started:
		s.disp.Flush()
		s.arc.sync()
		s.disp.Flush()
		return
	}
	select {
	case <-ack:
	case <-started:
	}
	s.disp.Flush()
	s.arc.sync()
	s.disp.Flush()
}

// handle folds one event in: ignore filter, store, projection, wire reactions.
func (s *Session) handle(ev model.Event) {
	ev, keep := filterIgnored(ev)
	if !keep {
		return
	}
	s.mu.Lock()
	s.syncHiddenLocked()
	s.syncNotifyLocked()
	s.syncAttentionLocked()
	items := s.applyChanges(s.store.Apply(ev))
	items = append(items, s.reactLocked(ev)...)
	s.mu.Unlock()
	s.emit(items)
}

// --- the conductor -----------------------------------------------------------

// reactLocked is the application policy that sits between the wire and the
// store: what eco *asks for* in response to what it was told. internal/irc will
// not invent a request and the store may not write to a socket, so the two
// decisions v1 made from its callbacks — "we just joined, fetch modes and
// scrollback" and "we are ready, rejoin what the user had open" — live here.
func (s *Session) reactLocked(ev model.Event) []dispatch.Item {
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
			return s.stsAdvertised(e)
		}
	case model.HistoryTarget:
		// A conversation the server had traffic for while we were away. The
		// store has opened its buffer; fetching what was said is this side's.
		s.gapTargetLocked(e)
	case model.BatchClose:
		if isTargetsBatch(e.Type) {
			return s.targetsDoneLocked()
		}
	case model.Numeric:
		if strings.EqualFold(e.Code, "FAIL") && len(e.Params) > 0 &&
			strings.EqualFold(e.Params[0], "CHATHISTORY") {
			return s.chathistoryFailedLocked()
		}
	}
	return nil
}

// isTargetsBatch reports whether a batch type is a CHATHISTORY TARGETS reply.
// It has no entry in the batch registry on purpose (its contents are ordinary
// live lines), so the name is matched here, where the one thing that cares
// about it lives.
func isTargetsBatch(typ string) bool {
	return strings.EqualFold(typ, "chathistory-targets") ||
		strings.EqualFold(typ, "draft/chathistory-targets")
}

// onReadyLocked joins the profile's autojoin channels and rejoins everything
// else the user still considers themselves in. The two lists are kept apart the
// way v1 kept them: a channel in both was joined twice, which doubled its join
// line and its history fetch.
func (s *Session) onReadyLocked() []dispatch.Item {
	// A reconnect fills the gap instead of replaying it: CHATHISTORY AFTER the
	// newest message we hold, per buffer, and TARGETS for the conversations
	// nothing would otherwise reopen. The FIRST connection has no gap — the
	// per-buffer LATEST on join is the right request there.
	reconnect := s.everReady
	s.everReady = true
	clear(s.exhausted) // a new connection may know things the old one did not
	if reconnect {
		s.gapFillLocked()
	}

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

// stsAdvertised reports an sts policy advertised in CAP LS/NEW. The irc layer
// publishes it on the CapSet for the next dial (Client.STS); this puts it on
// the funnel for ui.go, which owns the "persist only from a TLS connection"
// rule. It is a dispatch-local Change because no store Change carries it: the
// advertisement is a fact about the connection, not about the model.
func (s *Session) stsAdvertised(e model.CapChange) []dispatch.Item {
	var out []dispatch.Item
	for _, c := range e.Caps {
		if !strings.EqualFold(c.Name, "sts") {
			continue
		}
		port, duration := config.ParseSTSValue(c.Value)
		out = append(out, dispatch.Of(dispatch.STSOffered{
			ChangeMeta: model.NetMeta(s.netID),
			Port:       port,
			Duration:   duration,
		}, ""))
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
	var items []dispatch.Item
	if h.Truncated {
		items = append(items, s.statusLocked(buffer,
			fmt.Sprintf("Message truncated to the server's %d line limit.", len(h.Lines)))...)
	}
	items = append(items, s.echoLocked(buffer, h, model.MsgPrivmsg)...)
	s.mu.Unlock()
	s.emit(items)
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
	items := s.echoOneLocked(buffer, h.Label(), foldNewlines(text), model.MsgAction)
	s.mu.Unlock()
	s.emit(items)
}

// echoLocked renders what was just sent. One pending row per independently
// resolvable echo: a draft/multiline batch comes back as one message, and lines
// sent separately (no multiline capability) come back one at a time.
func (s *Session) echoLocked(buffer string, h irc.Handle, kind model.MessageKind) []dispatch.Item {
	if h.Multiline || len(h.Labels) <= 1 {
		return s.echoOneLocked(buffer, h.Label(), strings.Join(h.Lines, "\n"), kind)
	}
	var out []dispatch.Item
	for i, line := range h.Lines {
		label := ""
		if i < len(h.Labels) {
			label = h.Labels[i]
		}
		out = append(out, s.echoOneLocked(buffer, label, line, kind)...)
	}
	return out
}

func (s *Session) echoOneLocked(buffer, label, text string, kind model.MessageKind) []dispatch.Item {
	b, items := s.openLocked(buffer)
	if b == nil {
		return items
	}
	if label == "" && s.client.Enabled("echo-message") {
		// The server will send this message back to us and we have no label to
		// recognise it by, so rendering a local copy now would show it twice.
		// Its own echo is the message. (v1 dropped the echo by matching its
		// text; that is the heuristic MUST 3 deleted, and re-adding it here
		// would re-add the bug where two identical messages ate one echo.)
		return items
	}
	_, changes := s.store.Echo(s.idLocked(buffer), state.Outgoing{
		Label: label, Text: text, Kind: kind,
	})
	return append(items, s.applyChanges(changes)...)
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
		items := s.applyChanges(s.store.PartLocally(s.idLocked(ch)))
		s.mu.Unlock()
		s.emit(items)
		return
	}
	h, err := s.client.Part(ch, "")
	if err != nil {
		items := s.applyChanges(s.store.PartLocally(s.idLocked(ch)))
		s.mu.Unlock()
		s.emit(items)
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
	items := s.applyChanges(s.store.CloseBuffer(s.idLocked(name)))
	s.mu.Unlock()
	s.emit(items)
}

// MarkRead moves a buffer's read marker to its newest message, which is what
// makes the store's unread count for it zero. The UI calls it when the user
// shows a buffer and when they leave one: the store cannot see the screen, so
// "the buffer you are looking at is read" is a fact only this side knows.
//
// The marker goes three places, and they are the same fact: the store (where
// the count is derived from it), the archive (where it outlives the process,
// via the ReadMarkerMoved the store emits) and the server (MARKREAD, where the
// user's other devices read it). Conflicts everywhere resolve to the later
// position, so the three cannot disagree for long — ADR-3.
func (s *Session) MarkRead(name string) {
	s.mu.Lock()
	id := s.idLocked(name)
	_, at, changes := s.store.MarkReadLatest(id)
	items := s.applyChanges(changes)
	if !at.IsZero() && !id.IsServer() && s.connectedLocked() && s.client.Enabled("draft/read-marker") {
		_ = s.client.MarkRead(s.nameOf(id), at)
	}
	s.mu.Unlock()
	s.emit(items)
}

// ReadMarker is where a buffer's read marker stands, for the Timeline to hang
// its NEW divider on when the buffer is opened. It is read once per buffer
// switch, deliberately: the divider is a landmark, and a landmark that follows
// the marker down the screen as you read is not one.
func (s *Session) ReadMarker(name string) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, at, _ := s.store.ReadMarker(s.idLocked(name))
	return at
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
// Switcher refills itself when the scan lands (UI.ChannelListDone).
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

// wireClient is the connection the command table writes through, or nil when
// this session has never connected. "Is there a wire?" is the question
// Command.NeedsConn is answered with, and it is asked from the Fyne thread, so
// it is answered under the lock like every other read of s.client.
func (s *Session) wireClient() *irc.Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.client
}

// FetchHistory asks the server for the newest page of a buffer's history: what
// /history does. Scroll-up is LoadOlder's job — this is the user saying "there
// must be more than this", and it is the one place CHATHISTORY LATEST is
// written now that the command table cannot reach the wire itself.
func (s *Session) FetchHistory(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil || name == "" {
		return
	}
	_, _ = s.client.History(irc.HistoryRequest{
		Target: name, Direction: model.HistoryLatest, Limit: historyCommandLimit,
	})
}

// historyCommandLimit is how much /history asks for — v1's number, capped again
// by whatever the server advertises.
const historyCommandLimit = 200

// getBuffer returns a buffer, creating it if the user just named one that does
// not exist yet (/msg, a mention link).
func (s *Session) getBuffer(name string) *Buffer {
	s.mu.Lock()
	b, items := s.openLocked(name)
	s.mu.Unlock()
	s.emit(items)
	return b
}

// status writes a client-generated line into a buffer. These are the app's own
// sentences (usage errors, connection chrome); they live in the projection only,
// so the store's message log stays a record of the conversation.
func (s *Session) status(buffer, text string) {
	s.mu.Lock()
	items := s.statusLocked(buffer, text)
	s.mu.Unlock()
	s.emit(items)
}

// statusLocked writes one of the app's own sentences into a buffer. It is a
// message the app minted rather than one the store recorded, and it takes the
// same path as everything else so it keeps its place in the order: a usage
// error printed after the line that provoked it must stay after it.
func (s *Session) statusLocked(buffer, text string) []dispatch.Item {
	b, items := s.openLocked(buffer)
	if b == nil {
		return items
	}
	id := s.idLocked(buffer)
	m := model.Message{Time: time.Now(), Text: text, Kind: model.MsgStatus}
	l, into := s.rowLocked(id, m)
	if into == nil {
		return items
	}
	return append(items, dispatch.Of(
		model.MessageAdded{ChangeMeta: model.BufMeta(id), Message: m}, into.Name, l))
}

// --- projection --------------------------------------------------------------

// applyChanges folds a batch of Changes into the projection and returns the
// Items that put them on screen, in the order the store produced them. Callers
// hold s.mu; the Items are emitted after it is released.
//
// Not every Change produces an Item: a mode change only moves projection state,
// and a resolved message is only worth redrawing when its text actually moved.
// What is forwarded is forwarded whole — the funnel does the naming, this does
// the folding.
func (s *Session) applyChanges(cs []model.Change) []dispatch.Item {
	var out []dispatch.Item
	for _, c := range cs {
		// Every accepted change is offered to the archive before it is offered
		// to the screen: the disk is the model's memory, not the view's.
		s.recordLocked(c)
		switch ch := c.(type) {
		case model.BufferCreated:
			b := s.mirrorCreate(ch.Info)
			out = append(out, dispatch.Of(ch, b.Name))
			s.hydrateLocked(ch.Info.ID)
			out = append(out, s.dmPromptLocked(ch.Info)...)

		case model.BufferClosed:
			name := s.nameOf(ch.Meta().Buffer)
			s.mirrorRemove(ch.Meta().Buffer.Folded)
			s.forgetArchiveState(ch.Meta().Buffer.Folded)
			out = append(out, dispatch.Of(ch, name))

		case model.BufferRenamed:
			if it, ok := s.mirrorRename(ch); ok {
				out = append(out, it)
			}

		case model.MessageAdded:
			if l, b := s.rowLocked(ch.Meta().Buffer, ch.Message); b != nil {
				out = append(out, dispatch.Of(ch, b.Name, l))
			}

		case model.HistoryLoaded:
			out = append(out, s.historyLoaded(ch)...)
			if !s.noRecord {
				// A block that came off the WIRE closes a scroll-up that had to
				// ask for it, and counts towards the reconnect's catch-up line.
				out = append(out, s.wireHistoryLocked(ch)...)
				out = append(out, s.gapChunkLocked(ch)...)
			}

		case model.MessageResolved:
			out = append(out, s.resolveMessage(ch)...)

		case model.MessageFailed:
			out = append(out, s.statusLocked(s.nameOf(ch.Meta().Buffer),
				"Message not sent: "+firstNonEmpty(ch.Reason, ch.Code))...)

		case model.MessageRedacted:
			out = append(out, s.redactMessage(ch)...)

		case model.MembersChanged:
			out = append(out, s.membersChanged(ch)...)

		case model.SelfChanged:
			// Who we are on this network: the nick the server settled on, the
			// account we are logged in as, and our own away state. The rail's
			// identity chip is the one thing that draws it, and before M12
			// nothing did, so this was not forwarded at all.
			out = append(out, dispatch.Item{Change: ch})

		case model.UnreadChanged:
			// The store derives the counts from the read marker; the screen
			// files them. Forwarding this is what ended the divergence M7's
			// verification recorded, where internal/app counted from Line flags
			// and nothing consumed state's own counters.
			out = append(out, dispatch.Of(ch, s.nameOf(ch.Meta().Buffer)))

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
			out = append(out, dispatch.Of(ch, s.nameOf(ch.Meta().Buffer)))
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
// The Item carries both identities, because everything the UI files per buffer
// — draft, unread, mention flag, the viewer — has to move with it.
func (s *Session) mirrorRename(ch model.BufferRenamed) (dispatch.Item, bool) {
	b, ok := s.bufs[ch.From.Folded]
	if !ok {
		return dispatch.Item{}, false
	}
	from := dispatch.Buffer{ID: ch.From, Name: b.Name}
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
	it := dispatch.Of(ch, b.Name)
	it.From = from
	return it, true
}

// rowLocked appends one message to a buffer's rows and returns the row and the
// buffer it landed in, or a nil buffer when the projection does not hold one.
func (s *Session) rowLocked(id model.BufferID, m model.Message) (Line, *Buffer) {
	b := s.bufs[id.Folded]
	if b == nil {
		return Line{}, nil
	}
	l := lineOf(m)
	s.appendLine(b, m.ID, l)
	return l, b
}

// historyLoaded folds a whole chunk in and hands it over as ONE item carrying N
// rows, in the chunk's order. Order matters here and is guaranteed nowhere
// else: internal/transcript's records sort each step's facts, so a chunk
// delivered backwards is invisible to them (M7 verification). The funnel pins
// it, and this is the change that needs it pinned.
//
// A BEFORE chunk — scroll-up, from the archive or from the server — goes in
// front of the rows already there rather than after them, which is the whole
// difference between "load older messages" and "say it all again".
func (s *Session) historyLoaded(ch model.HistoryLoaded) []dispatch.Item {
	b := s.bufs[ch.Meta().Buffer.Folded]
	if b == nil || len(ch.Messages) == 0 {
		return nil
	}
	lines := make([]Line, 0, len(ch.Messages))
	ids := make([]model.MessageID, 0, len(ch.Messages))
	for _, m := range ch.Messages {
		lines = append(lines, lineOf(m))
		ids = append(ids, m.ID)
	}
	if ch.Direction == model.HistoryBefore {
		b.Lines = append(append(make([]Line, 0, len(lines)+len(b.Lines)), lines...), b.Lines...)
		b.ids = append(append(make([]model.MessageID, 0, len(ids)+len(b.ids)), ids...), b.ids...)
		return []dispatch.Item{dispatch.Of(ch, b.Name, lines...)}
	}
	for i, l := range lines {
		s.appendLine(b, ids[i], l)
	}
	return []dispatch.Item{dispatch.Of(ch, b.Name, lines...)}
}

// dmPromptLocked raises the first-DM logging question, once per profile: the
// moment a private conversation is about to be written to disk is the moment to
// say so, and to offer not to. The decision is the session's (it knows the
// buffer's kind and whether there is an archive at all); the dialog is the
// window's, which is why this is an item on the queue and not a call.
func (s *Session) dmPromptLocked(info model.BufferInfo) []dispatch.Item {
	if s.arc == nil || s.dmPrompted || info.Kind != model.BufferQuery {
		return nil
	}
	if config.Current().DMLogAnswered {
		return nil
	}
	s.dmPrompted = true
	return []dispatch.Item{dispatch.Of(dispatch.AskDMLogging{
		ChangeMeta: model.BufMeta(info.ID),
	}, info.Name)}
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
func (s *Session) resolveMessage(ch model.MessageResolved) []dispatch.Item {
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
	if before.Text == b.Lines[i].Text {
		return nil
	}
	return []dispatch.Item{dispatch.Of(ch, b.Name)}
}

// redactMessage turns a withdrawn message into its tombstone. The store already
// dropped the text; this is the row the reader sees, and it is the same one v1's
// ui.go wrote — the difference is that the core owns it now, so the line is gone
// from state as well as from the screen.
func (s *Session) redactMessage(ch model.MessageRedacted) []dispatch.Item {
	b := s.bufs[ch.Meta().Buffer.Folded]
	if b == nil {
		return nil
	}
	if i := b.index(ch.ID); i >= 0 {
		b.Lines[i] = Line{Time: b.Lines[i].Time, Text: redactedText, Status: true, MsgID: ch.MsgID}
	}
	return []dispatch.Item{dispatch.Of(ch, b.Name)}
}

// membersChanged republishes a buffer's roster. The store hands out deltas; the
// v1 member list is a whole map, so it is rebuilt from the store's ranked view.
func (s *Session) membersChanged(ch model.MembersChanged) []dispatch.Item {
	id := ch.Meta().Buffer
	b := s.bufs[id.Folded]
	if b == nil {
		return nil
	}
	members := make(map[string]string, len(b.Members))
	for _, m := range s.store.Members(id) {
		members[m.Nick] = m.Prefixes
	}
	b.Members = members
	return []dispatch.Item{dispatch.Of(ch, b.Name)}
}

// topicChanged republishes a topic. The store keeps it exactly as the wire
// carried it, formatting codes and all; the topic bar is a plain label, so the
// codes are stripped once, here, into both the projection and the Item — the
// two cannot then disagree about what the topic says.
func (s *Session) topicChanged(ch model.TopicChanged) []dispatch.Item {
	b := s.bufs[ch.Meta().Buffer.Folded]
	if b == nil {
		return nil
	}
	ch.Topic = ircfmt.Strip(ch.Topic)
	b.Topic = ch.Topic
	return []dispatch.Item{dispatch.Of(ch, b.Name)}
}

// connStateChanged hands the connection state over with the one fact the store
// cannot know attached: whether the client is going to dial again by itself, or
// whether this silence is the one the user asked for. The wording is the UI's.
func (s *Session) connStateChanged(ch model.ConnStateChanged) []dispatch.Item {
	switch ch.Status {
	case model.ConnReady, model.ConnFailed, model.ConnDisconnected:
	default:
		return nil // connecting/registering/authenticating: v1 said nothing
	}
	return []dispatch.Item{dispatch.Of(dispatch.ConnState{
		ChangeMeta: model.NetMeta(s.netID),
		Status:     ch.Status,
		Nick:       ch.Nick,
		Err:        ch.Err,
		Retrying:   ch.RetryIn > 0 && !s.quitting,
	}, "")}
}

// listUpdated collects a channel scan and, when it ends, does what v1's 323
// handler did: report the count, optionally join everything on the first scan,
// and open the browser for a scan somebody asked for.
func (s *Session) listUpdated(ch model.ListUpdated) []dispatch.Item {
	if !ch.Done {
		return nil
	}
	found := s.store.ChannelList(s.netID)
	names := make([]string, 0, len(found))
	for _, ci := range found {
		names = append(names, ci.Name)
	}
	// The scan has streamed into list_cache as it arrived (recordLocked); with
	// an archive to hold it, the store's copy is now the second one, and a
	// second copy of a fifty-thousand-row answer is exactly what §5.4 says a
	// LIST must not leave in memory.
	if s.arc != nil {
		s.store.ForgetList(s.netID)
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
	// Requested is what tells the browser to pop: a scan somebody asked for, or
	// the first connect — never a background rescan after an auto-reconnect.
	return append(out, dispatch.Of(dispatch.ChannelListDone{
		ChangeMeta: model.NetMeta(s.netID),
		Requested:  manual || firstScan,
	}, ""))
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
func (s *Session) openLocked(name string) (*Buffer, []dispatch.Item) {
	folded := s.foldLocked(name)
	if b, ok := s.bufs[folded]; ok {
		return b, nil
	}
	if folded == "" {
		return nil, nil // no status buffer yet: the network is not set up
	}
	_, changes := s.store.OpenBuffer(s.netID, name)
	items := s.applyChanges(changes)
	return s.bufs[folded], items
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

// syncNotifyLocked pushes the "tell me about every message" preference into the
// store as the network's default notification level. §4.4's evaluation order
// starts there, so this is what makes the store's answer the WHOLE answer — the
// screen obeys MessageAdded.Notify and evaluates nothing of its own.
//
// The network default is the PROFILE's when the user set one, and the global
// preference otherwise: §4.4 asks for a per-network default, and a per-network
// setting that the global one silently overwrote would be a control that does
// nothing on every network but the last one touched.
func (s *Session) syncNotifyLocked() {
	want := s.wantNotifyLevel()
	if want == s.notifyLevel {
		return
	}
	s.notifyLevel = want
	s.store.SetNetworkNotifyLevel(s.netID, want)
}

// wantNotifyLevel is the network default this session should be running with.
func (s *Session) wantNotifyLevel() state.NotifyLevel {
	if l, ok := s.profileNotify(); ok {
		return l
	}
	return notifyLevel(config.Current().NotifyAll)
}

// syncAttentionLocked pushes the two settings §4.4 and §4.3 put in Preferences —
// the highlight words and the smart join/part filter — into the store, which is
// the only place either is evaluated. Like the join/part generation above it,
// this is a string comparison per event and a push only when something moved.
func (s *Session) syncAttentionLocked() {
	set := config.Current()
	mark := attentionGeneration(set)
	if mark == s.attnMark {
		return
	}
	s.attnMark = mark
	s.store.SetHighlightWords(s.netID, set.HighlightWords)
	s.store.SetNetworkSmartJoinParts(s.netID, set.SmartJoinParts)
	s.store.SetNetworkSpeakerWindow(s.netID, speakerWindow(set))
}

// attentionGeneration is a value that changes whenever anything
// syncAttentionLocked pushes changes.
func attentionGeneration(set *config.Settings) string {
	return strconv.FormatBool(set.SmartJoinParts) + "\x00" +
		strconv.Itoa(set.SpeakerMinutes) + "\x00" +
		strings.Join(set.HighlightWords, "\x00")
}

// speakerWindow is the smart filter's recency window as a duration.
func speakerWindow(set *config.Settings) time.Duration {
	if set.SpeakerMinutes <= 0 {
		return 0 // the store's default
	}
	return time.Duration(set.SpeakerMinutes) * time.Minute
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

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// wire is what is left of the write side the browser and the row menus reach
// for: Session.irc. It existed so those call sites did not have to change at
// the M7 cutover — they were written against ircevent's Connection, and this is
// the same handful of verbs against internal/irc.Client.
//
// M10 took the commands off it: every verb the command table writes is a typed
// method on internal/irc.Client, reached through internal/command's Wire, and
// what remains here is the generic Send those two UI surfaces still use. They
// are M12's to rewrite, and this shrinks to nothing when they are.
type wire struct{ c *irc.Client }

// Send writes one command with no label.
func (w *wire) Send(command string, params ...string) { _ = w.c.Send(command, params...) }

// Join joins a channel on the client's own initiative — no "the user asked for
// this" flag, so a refusal stays in the log instead of raising a dialog.
func (w *wire) Join(channel string) { _, _ = w.c.Join(channel, "") }

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
