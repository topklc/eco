package state

import (
	"sort"
	"strings"
	"time"

	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/proto"
)

// Unlimited as Options.Scrollback keeps every message a buffer ever saw.
const Unlimited = -1

// DefaultScrollback is how many messages a buffer retains when Options leaves
// Scrollback at zero. It matches v1's config default so the RAM profile does
// not change under the cutover.
const DefaultScrollback = 2000

// DefaultTypingTimeout is how long a typing indicator survives without a
// refresh. draft/typing says clients should expire after 6 seconds.
const DefaultTypingTimeout = 6 * time.Second

// ServerBufferName is the display name of a network's status buffer. Its id is
// model.ServerBuffer(network) — an empty folded name — so the name is display
// data only and is never a key.
const ServerBufferName = "server"

// listChunk is how many LIST rows accumulate before the store emits a
// ListUpdated. LIST results stream (blueprint §5.4); they are not held back
// until RPL_LISTEND.
const listChunk = 64

// NotifyLevel is a buffer's notification setting (blueprint §4.4). The zero
// value inherits the network default, so a buffer that was never configured
// costs nothing to store.
type NotifyLevel uint8

const (
	NotifyDefault  NotifyLevel = 0 // inherit the network's default level
	NotifyAll      NotifyLevel = 1 // every message that counts as unread
	NotifyMentions NotifyLevel = 2 // highlights and queries only
	NotifyMute     NotifyLevel = 3 // never
)

// String returns the setting's name.
func (l NotifyLevel) String() string {
	switch l {
	case NotifyDefault:
		return "default"
	case NotifyAll:
		return "all"
	case NotifyMentions:
		return "mentions"
	case NotifyMute:
		return "mute"
	}
	return "unknown"
}

// Options configures a Store. The zero value is usable: it reads the wall
// clock, mints ids from a fresh sequence and keeps DefaultScrollback messages
// per buffer.
type Options struct {
	// Clock is the one time source for messages that arrive without a
	// server-time tag. Tests pass model.FixedClock or model.StepClock.
	Clock model.Clock
	// IDs mints model.MessageID values. Pass model.NewIDSeq(maxRowID) to
	// continue an archive's numbering; nil means a fresh sequence.
	IDs *model.IDSeq
	// Scrollback is how many messages each buffer retains. Zero means
	// DefaultScrollback; Unlimited keeps everything.
	Scrollback int
	// DefaultNotify is the notification level a network uses for buffers that
	// have not set their own. Zero means NotifyMentions.
	DefaultNotify NotifyLevel
	// HighlightWords are extra words that raise a highlight, on top of our own
	// nick. Matching folds Unicode case and requires whole words.
	HighlightWords []string
	// HideJoinParts suppresses join/part/quit status lines by default. It is
	// the global preference; SetHideJoinParts overrides it per buffer.
	HideJoinParts bool
	// SmartJoinParts shows a channel's join/part/quit lines only for nicks who
	// have spoken there recently (blueprint §4.3; jpfilter.go). It is the
	// default-ON filter, and is evaluated only where HideJoinParts has not
	// already refused the line.
	SmartJoinParts bool
	// SpeakerWindow is how recently a nick must have spoken to count. Zero
	// means DefaultSpeakerWindow.
	SpeakerWindow time.Duration
	// TypingTimeout is how long a typing indicator lives. Zero means
	// DefaultTypingTimeout.
	TypingTimeout time.Duration
}

// Store is the single-writer model of every network. See the package doc for
// the threading contract.
type Store struct {
	clock          model.Clock
	ids            *model.IDSeq
	scrollback     int
	defaultNotify  NotifyLevel
	highlightWords []string
	hideJoinParts  bool
	smartJP        bool
	speakerWin     time.Duration
	typingTimeout  time.Duration

	nets  map[model.NetworkID]*network
	order []model.NetworkID
}

// New returns an empty Store.
func New(o Options) *Store {
	s := &Store{
		clock:          o.Clock,
		ids:            o.IDs,
		scrollback:     o.Scrollback,
		defaultNotify:  o.DefaultNotify,
		highlightWords: append([]string(nil), o.HighlightWords...),
		hideJoinParts:  o.HideJoinParts,
		smartJP:        o.SmartJoinParts,
		speakerWin:     o.SpeakerWindow,
		typingTimeout:  o.TypingTimeout,
		nets:           map[model.NetworkID]*network{},
	}
	if s.clock == nil {
		s.clock = model.SystemClock()
	}
	if s.ids == nil {
		s.ids = model.NewIDSeq(0)
	}
	if s.scrollback == 0 {
		s.scrollback = DefaultScrollback
	}
	if s.defaultNotify == NotifyDefault {
		s.defaultNotify = NotifyMentions
	}
	if s.typingTimeout <= 0 {
		s.typingTimeout = DefaultTypingTimeout
	}
	return s
}

// network is one connection's state. Everything keyed by a name is keyed by
// props.Fold of that name — never by the display form.
type network struct {
	id      model.NetworkID
	props   *proto.ServerProps
	caps    *proto.CapSet
	batches *proto.BatchTracker

	status  model.ConnStatus
	nick    string
	folded  string
	account string
	away    bool

	bufs  map[string]*buffer
	order []string // folded keys, creation order, server buffer first

	ignores []ignoreRule
	whois   map[string]model.BufferID // folded nick -> the buffer that asked

	pending     map[string]model.MessageID // outgoing label -> message awaiting its echo
	pendingJoin map[string]bool            // folded channel -> the user asked for this join
	pendingPart map[string]bool            // folded channel -> the user asked to leave
	labelPart   map[string]string          // label -> folded channel, for a labeled PART refusal
	keepOnPart  map[string]bool            // folded channel -> /cycle in flight, keep the buffer
	names       map[string][]model.Member  // folded channel -> NAMES accumulating for this round

	list listCache

	defaultNotify  NotifyLevel
	highlightWords []string
	hideJoinParts  bool
	smartJP        bool
	speakerWin     time.Duration
	focus          model.BufferID

	// hasTyping short-circuits the expiry sweep. It is set when an indicator
	// starts and recomputed by the sweep itself, so it can only ever be
	// pessimistic — never stale in the direction that would strand one.
	hasTyping bool
}

// listCache holds the last channel list. Items are handed out in chunks as they
// stream in; the cache exists for the quick switcher, not as the delivery path.
type listCache struct {
	items   []model.ChannelInfo
	index   map[string]int // folded channel -> position in items
	unsent  int            // items[unsent:] have not been reported yet
	closed  bool           // a ListEnd landed; the next ListItem starts a new scan
	started bool
}

// network returns the state for n, creating it (with its server buffer) on
// first sight. Events arrive for a network before anything configures it, so
// this can never fail.
func (s *Store) network(id model.NetworkID) *network {
	if n, ok := s.nets[id]; ok {
		return n
	}
	n := &network{
		id:             id,
		props:          proto.DefaultProps(),
		caps:           proto.NewCapSet(proto.DefaultCapRequest()),
		batches:        proto.NewBatchTracker(),
		bufs:           map[string]*buffer{},
		whois:          map[string]model.BufferID{},
		pending:        map[string]model.MessageID{},
		pendingJoin:    map[string]bool{},
		pendingPart:    map[string]bool{},
		labelPart:      map[string]string{},
		keepOnPart:     map[string]bool{},
		names:          map[string][]model.Member{},
		defaultNotify:  s.defaultNotify,
		highlightWords: append([]string(nil), s.highlightWords...),
		hideJoinParts:  s.hideJoinParts,
		smartJP:        s.smartJP,
		speakerWin:     s.speakerWin,
	}
	sb := &buffer{
		id:      model.ServerBuffer(id),
		name:    ServerBufferName,
		kind:    model.BufferServer,
		members: map[string]model.Member{},
		byMsgID: map[string]model.MessageID{},
		pos:     map[model.MessageID]int{},
	}
	n.bufs[""] = sb
	n.order = []string{""}
	s.nets[id] = n
	s.order = append(s.order, id)
	return n
}

// EnsureNetwork creates a network's state and its server buffer, returning the
// BufferCreated for the latter. Connecting code calls it so the status buffer
// exists before the first log line; Apply does the same thing lazily.
func (s *Store) EnsureNetwork(id model.NetworkID) []model.Change {
	if id == "" {
		return nil
	}
	if _, ok := s.nets[id]; ok {
		return nil
	}
	n := s.network(id)
	return []model.Change{model.BufferCreated{
		ChangeMeta: model.BufMeta(n.bufs[""].id),
		Info:       n.bufs[""].info(),
	}}
}

// Networks returns every network the store knows, in creation order.
func (s *Store) Networks() []model.NetworkID {
	return append([]model.NetworkID(nil), s.order...)
}

// server returns the network's status buffer, which always exists.
func (n *network) server() *buffer { return n.bufs[""] }

// find returns the buffer with this id, or nil.
func (n *network) find(id model.BufferID) *buffer {
	if id.Network != n.id {
		return nil
	}
	return n.bufs[id.Folded]
}

// lookup returns the buffer for a raw name, or nil.
func (n *network) lookup(name string) *buffer { return n.bufs[n.props.Fold(name)] }

// isSelf reports whether a sender is us. Sender.Self is authoritative when the
// irc layer set it; the nick comparison is the fallback and folds properly.
func (n *network) isSelf(s model.Sender) bool {
	if s.Self {
		return true
	}
	return n.nick != "" && s.Nick != "" && n.props.Equal(s.Nick, n.nick)
}

// isSelfNick reports whether a bare nick is ours.
func (n *network) isSelfNick(nick string) bool {
	return n.nick != "" && nick != "" && n.props.Equal(nick, n.nick)
}

// hidden reports whether join/part/quit lines are suppressed for a buffer.
func (n *network) hidden(b *buffer) bool {
	if b.hideJP != nil {
		return *b.hideJP
	}
	return n.hideJoinParts
}

// level returns the notification level in force for a buffer.
func (n *network) level(b *buffer) NotifyLevel {
	if b.notify != NotifyDefault {
		return b.notify
	}
	if n.defaultNotify != NotifyDefault {
		return n.defaultNotify
	}
	return NotifyMentions
}

// bulk reports whether an event arrived inside a netsplit/netjoin batch, in
// which case membership churn is reported as one bulk delta instead of a
// hundred status lines.
func (n *network) bulk(meta model.EventMeta) bool {
	return meta.Batch != "" && n.batches.IsBulkMembership(meta.Batch)
}

// ensure returns the buffer for a raw name, creating it if needed. The returned
// changes carry the BufferCreated, which must be emitted before anything that
// refers to the buffer.
func (s *Store) ensure(n *network, name string) (*buffer, []model.Change) {
	return s.ensureRestored(n, name, false)
}

func (s *Store) ensureRestored(n *network, name string, restored bool) (*buffer, []model.Change) {
	folded := n.props.Fold(name)
	if b, ok := n.bufs[folded]; ok {
		return b, nil
	}
	b := &buffer{
		id:      model.BufferID{Network: n.id, Folded: folded},
		name:    name,
		kind:    n.props.BufferKind(name),
		members: map[string]model.Member{},
		byMsgID: map[string]model.MessageID{},
		pos:     map[model.MessageID]int{},
	}
	if folded == "" {
		b.kind, b.name = model.BufferServer, ServerBufferName
	}
	n.bufs[folded] = b
	n.order = append(n.order, folded)
	return b, []model.Change{model.BufferCreated{
		ChangeMeta: model.BufMeta(b.id),
		Info:       b.info(),
		Restored:   restored,
	}}
}

// remove drops a buffer and everything hanging off it. The server buffer is
// never removed: it survives every teardown path (v1 invariant).
func (n *network) remove(b *buffer) bool {
	if b == nil || b.id.IsServer() {
		return false
	}
	if _, ok := n.bufs[b.id.Folded]; !ok {
		return false
	}
	delete(n.bufs, b.id.Folded)
	for i, k := range n.order {
		if k == b.id.Folded {
			n.order = append(n.order[:i], n.order[i+1:]...)
			break
		}
	}
	delete(n.pendingJoin, b.id.Folded)
	delete(n.pendingPart, b.id.Folded)
	delete(n.keepOnPart, b.id.Folded)
	delete(n.names, b.id.Folded)
	for label, folded := range n.labelPart {
		if folded == b.id.Folded {
			delete(n.labelPart, label)
		}
	}
	if n.focus == b.id {
		n.focus = model.BufferID{}
	}
	return true
}

// --- read-only projections ---------------------------------------------------

// Buffers returns a network's buffers in display order (status buffer first).
func (s *Store) Buffers(id model.NetworkID) []model.BufferInfo {
	n, ok := s.nets[id]
	if !ok {
		return nil
	}
	out := make([]model.BufferInfo, 0, len(n.order))
	for _, k := range n.order {
		if b := n.bufs[k]; b != nil {
			out = append(out, b.info())
		}
	}
	return out
}

// BufferInfo returns one buffer's identity, and whether it exists.
func (s *Store) BufferInfo(id model.BufferID) (model.BufferInfo, bool) {
	b := s.buffer(id)
	if b == nil {
		return model.BufferInfo{}, false
	}
	return b.info(), true
}

// buffer resolves a BufferID across networks.
func (s *Store) buffer(id model.BufferID) *buffer {
	n, ok := s.nets[id.Network]
	if !ok {
		return nil
	}
	return n.bufs[id.Folded]
}

// Members returns a channel's members ranked the way the roster shows them:
// highest prefix first, then folded nick.
func (s *Store) Members(id model.BufferID) []model.Member {
	b := s.buffer(id)
	if b == nil {
		return nil
	}
	n := s.nets[id.Network]
	out := make([]model.Member, 0, len(b.members))
	for _, m := range b.members {
		out = append(out, m)
	}
	sortMembers(n.props.Prefix, out)
	return out
}

// sortMembers orders members by prefix rank then folded nick.
func sortMembers(p proto.PrefixMap, ms []model.Member) {
	rank := func(m model.Member) int {
		h := p.Highest(m.Prefixes)
		if h == 0 {
			return len(p.Prefixes)
		}
		return p.Rank(h)
	}
	sort.Slice(ms, func(i, j int) bool {
		ri, rj := rank(ms[i]), rank(ms[j])
		if ri != rj {
			return ri < rj
		}
		return ms[i].Folded < ms[j].Folded
	})
}

// Messages returns a copy of a buffer's retained messages, oldest first.
func (s *Store) Messages(id model.BufferID) []model.Message {
	b := s.buffer(id)
	if b == nil {
		return nil
	}
	return append([]model.Message(nil), b.msgs...)
}

// Message returns one message by its store id.
func (s *Store) Message(id model.BufferID, mid model.MessageID) (model.Message, bool) {
	b := s.buffer(id)
	if b == nil {
		return model.Message{}, false
	}
	if m := b.at(mid); m != nil {
		return *m, true
	}
	return model.Message{}, false
}

// Topic returns a buffer's topic and whether one is known. A known empty topic
// means the channel has none, which is not the same as never having learned it.
func (s *Store) Topic(id model.BufferID) (topic string, known bool) {
	b := s.buffer(id)
	if b == nil {
		return "", false
	}
	return b.topic, b.topicKnown
}

// Modes returns a channel's tracked flag modes, key and limit.
func (s *Store) Modes(id model.BufferID) (modes, key string, limit int) {
	b := s.buffer(id)
	if b == nil {
		return "", "", 0
	}
	return b.modes, b.key, b.limit
}

// Unread returns a buffer's unread and highlight counts.
func (s *Store) Unread(id model.BufferID) (unread, highlights int) {
	b := s.buffer(id)
	if b == nil {
		return 0, 0
	}
	return b.unread, b.highlights
}

// ReadMarker returns a buffer's read marker.
func (s *Store) ReadMarker(id model.BufferID) (msgid string, at time.Time, ok bool) {
	b := s.buffer(id)
	if b == nil || (b.markerMsgID == "" && b.markerAt.IsZero()) {
		return "", time.Time{}, false
	}
	return b.markerMsgID, b.markerAt, true
}

// Typing returns the nicks currently typing in a buffer, sorted.
func (s *Store) Typing(id model.BufferID) []string {
	b := s.buffer(id)
	if b == nil {
		return nil
	}
	out := make([]string, 0, len(b.typing))
	for _, t := range b.typing {
		out = append(out, t.nick)
	}
	sort.Strings(out)
	return out
}

// Self returns our identity on a network.
func (s *Store) Self(id model.NetworkID) (nick, account string, away bool) {
	n, ok := s.nets[id]
	if !ok {
		return "", "", false
	}
	return n.nick, n.account, n.away
}

// Status returns a network's connection status.
func (s *Store) Status(id model.NetworkID) model.ConnStatus {
	if n, ok := s.nets[id]; ok {
		return n.status
	}
	return model.ConnDisconnected
}

// Props returns a copy of a network's server properties. It is a clone so a
// caller cannot mutate the store's copy — the store's copy is the one every
// key was folded with.
func (s *Store) Props(id model.NetworkID) *proto.ServerProps {
	n, ok := s.nets[id]
	if !ok {
		return proto.DefaultProps()
	}
	return n.props.Clone()
}

// BufferID is the sanctioned way for a caller to name a buffer: it folds with
// the network's own casemapping.
func (s *Store) BufferID(id model.NetworkID, name string) model.BufferID {
	if n, ok := s.nets[id]; ok {
		return n.props.BufferID(id, name)
	}
	return proto.DefaultProps().BufferID(id, name)
}

// HasCap reports whether a capability is enabled on a network.
func (s *Store) HasCap(id model.NetworkID, name string) bool {
	n, ok := s.nets[id]
	return ok && n.caps.Enabled(name)
}

// CapValue returns an enabled capability's value.
func (s *Store) CapValue(id model.NetworkID, name string) (string, bool) {
	n, ok := s.nets[id]
	if !ok {
		return "", false
	}
	return n.caps.Value(name)
}

// ChannelList returns the cached LIST result, most populated channel first.
func (s *Store) ChannelList(id model.NetworkID) []model.ChannelInfo {
	n, ok := s.nets[id]
	if !ok {
		return nil
	}
	out := append([]model.ChannelInfo(nil), n.list.items...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Users > out[j].Users })
	return out
}

// PendingJoin reports whether the user explicitly asked to join a channel and
// the server has not answered yet. A refusal for one of these may prompt; an
// autojoin or rejoin refusal must stay quiet (v1 invariant).
func (s *Store) PendingJoin(id model.NetworkID, channel string) bool {
	n, ok := s.nets[id]
	return ok && n.pendingJoin[n.props.Fold(channel)]
}

// PendingPart reports whether the user asked to leave a channel and the server
// has not answered yet.
func (s *Store) PendingPart(id model.NetworkID, channel string) bool {
	n, ok := s.nets[id]
	return ok && n.pendingPart[n.props.Fold(channel)]
}

// PendingJoins lists the folded channels with an unanswered user-initiated JOIN,
// sorted. PendingParts is the same for PART. Both exist so a caller can ask
// "is anything still outstanding?" — a question the per-channel predicates
// above cannot answer, and one the reconnect contract turns on: a request that
// outlives its connection can never be answered, so both sets must come out of
// a disconnect empty.
func (s *Store) PendingJoins(id model.NetworkID) []string {
	return sortedSet(s.nets[id], pendingJoinSet)
}

// PendingParts lists the folded channels with an unanswered PART, sorted.
func (s *Store) PendingParts(id model.NetworkID) []string {
	return sortedSet(s.nets[id], pendingPartSet)
}

func pendingJoinSet(n *network) map[string]bool { return n.pendingJoin }
func pendingPartSet(n *network) map[string]bool { return n.pendingPart }

func sortedSet(n *network, pick func(*network) map[string]bool) []string {
	if n == nil {
		return nil
	}
	set := pick(n)
	out := make([]string, 0, len(set))
	for k, v := range set {
		if v {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// --- reconnect planning ------------------------------------------------------

// RejoinTarget is a channel to rejoin after a reconnect, with the key the store
// learned from MODE — v1 rejoined keyed channels without their key and they
// silently failed.
type RejoinTarget struct {
	Channel string
	Key     string
}

// RejoinTargets lists the channels the user considers themselves in, sorted by
// folded name so a reconnect is reproducible. Buffers the user closed are
// absent; buffers whose join was refused are absent until they succeed.
func (s *Store) RejoinTargets(id model.NetworkID) []RejoinTarget {
	n, ok := s.nets[id]
	if !ok {
		return nil
	}
	var out []RejoinTarget
	for _, k := range n.order {
		b := n.bufs[k]
		if b == nil || b.kind != model.BufferChannel || !b.wantJoin {
			continue
		}
		out = append(out, RejoinTarget{Channel: b.name, Key: b.key})
	}
	sort.Slice(out, func(i, j int) bool {
		return n.props.Fold(out[i].Channel) < n.props.Fold(out[j].Channel)
	})
	return out
}

// GapFill is one buffer's anchor for CHATHISTORY AFTER: everything the server
// has newer than this is what we missed.
type GapFill struct {
	Buffer model.BufferID
	Target string
	MsgID  string
	At     time.Time
}

// GapFillTargets lists every buffer with something to catch up from, newest
// anchor per buffer, sorted by folded name. A buffer with no messages yet has
// no anchor and is skipped: LATEST covers it.
func (s *Store) GapFillTargets(id model.NetworkID) []GapFill {
	n, ok := s.nets[id]
	if !ok {
		return nil
	}
	var out []GapFill
	for _, k := range n.order {
		b := n.bufs[k]
		if b == nil || b.kind == model.BufferServer || len(b.msgs) == 0 {
			continue
		}
		g := GapFill{Buffer: b.id, Target: b.name}
		for i := len(b.msgs) - 1; i >= 0; i-- {
			if m := b.msgs[i]; m.MsgID != "" && m.Pending == model.Delivered {
				g.MsgID, g.At = m.MsgID, m.Time
				break
			}
		}
		if g.MsgID == "" {
			g.At = b.msgs[len(b.msgs)-1].Time
		}
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Buffer.Folded < out[j].Buffer.Folded })
	return out
}

// NeedsHistory reports whether a buffer has not been backfilled since it was
// (re)opened. It is the replacement for v1's historyRequested map, and it is
// per buffer, so closing and reopening a buffer refetches instead of silently
// swallowing the replay.
func (s *Store) NeedsHistory(id model.BufferID) bool {
	b := s.buffer(id)
	return b != nil && !b.historyDone
}

// MarkHistoryRequested records that a backfill was issued for a buffer.
func (s *Store) MarkHistoryRequested(id model.BufferID) {
	if b := s.buffer(id); b != nil {
		b.historyDone = true
	}
}

// --- small helpers -----------------------------------------------------------

// now returns the store's clock reading.
func (s *Store) now() time.Time { return s.clock.Now() }

// Clock returns the store's clock reading. It exists so the layers around the
// store stamp their own facts — an archive write, a gap-fill window — off the
// same time source the store's messages carry, rather than reaching for
// time.Now and drifting from it in every test that injects a clock.
func (s *Store) Clock() time.Time { return s.now() }

// eventTime is the time to stamp on a message: server-time when the server sent
// one, otherwise the store's clock.
func (s *Store) eventTime(meta model.EventMeta) time.Time {
	if !meta.Time.IsZero() {
		return meta.Time
	}
	return s.now()
}

// isServerSource reports whether a message came from the server itself rather
// than a user. A server source has no user@host and a dotted name; v1 tested
// only for the dot, which misfiled a nick containing one.
func isServerSource(s model.Sender) bool {
	if s.IsZero() {
		return true
	}
	return s.User == "" && s.Host == "" && strings.Contains(s.Nick, ".")
}
