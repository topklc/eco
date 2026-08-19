package app

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/toprakkilic/eco/internal/archive"
	"github.com/toprakkilic/eco/internal/config"
	"github.com/toprakkilic/eco/internal/irc"
	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/ui/dispatch"
)

// The session's half of blueprint §5.4: the archive is not a feature bolted
// onto the side of the app, it is where the app's memory lives. What used to be
// "the buffer holds every line it has seen since you started eco" is now "the
// buffer holds a bounded window over a file that outlives the process".
//
// Three paths meet here:
//
//   - the LIVE path - every message the store accepts is handed to the
//     write-behind Recorder as the same change is folded into the projection;
//   - the COLD path - a buffer that opens with an empty window asks the archive
//     for its newest page, and its read marker with it, which is what makes
//     history and unread survive a restart;
//   - the BACKWARD path - reaching the top of a buffer pages older messages out
//     of the archive, and when the archive runs out, off the server with
//     CHATHISTORY BEFORE.
//
// Threading. The store is single-writer and lives on the pump goroutine; the
// disk must never be on that goroutine. Everything below that touches the file
// synchronously runs on arc's one worker goroutine, which takes s.mu when it
// has something to fold back in and releases it before emitting, exactly as the
// pump does.

// hydrateLimit is how much of a buffer's tail is restored when it opens cold.
// It is far more than a window holds, so the first screenful is always covered
// and the rest is scroll-up's head start. (Until M11(c) the UI drew at most 250
// lines of it; the Timeline draws the dozen rows on screen and keeps the rest.)
const hydrateLimit = 200

// pageLimit is one scroll-up page.
const pageLimit = archive.DefaultPageLimit

// arc is one profile's archive as the session uses it: the database, the
// write-behind Recorder the live path feeds, and one worker goroutine for
// everything that has to touch the disk synchronously - pages, markers, buffer
// settings, redaction, the channel cache.
//
// The worker is what keeps the disk off the goroutine that owns the store. Its
// queue is ordered and unbounded: ordered because a redaction must not overtake
// the message it withdraws, unbounded because the things on it are the things
// the archive must not lose, and it is fed no faster than a server can talk.
// (The message path is the other way round - the Recorder drops under a burst
// rather than stall the UI. Losing a log line is survivable; losing the erasure
// of one is not.)
type arc struct {
	db  *archive.DB
	rec *archive.Recorder

	ctx    context.Context
	cancel context.CancelFunc

	mu      sync.Mutex
	pending []func()
	closed  bool

	wake chan struct{}
	done chan struct{}

	closeOnce sync.Once
	closeErr  error
}

// openArc opens a profile's archive and starts its Recorder and worker.
func openArc(path string) (*arc, error) {
	db, err := archive.Open(path, archive.Options{})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	rec, err := archive.NewRecorder(ctx, db, archive.RecorderOptions{})
	if err != nil {
		cancel()
		db.Close() //nolint:errcheck // the open failed; this is the unwind
		return nil, err
	}
	a := &arc{
		db: db, rec: rec, ctx: ctx, cancel: cancel,
		wake: make(chan struct{}, 1),
		done: make(chan struct{}),
	}
	go a.run()
	return a, nil
}

// do queues one disk task. It never blocks: callers hold the store's lock.
func (a *arc) do(f func()) {
	if a == nil || f == nil {
		return
	}
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.pending = append(a.pending, f)
	a.mu.Unlock()
	select {
	case a.wake <- struct{}{}:
	default: // a wake-up is already pending; one is enough
	}
}

func (a *arc) run() {
	defer close(a.done)
	for {
		a.mu.Lock()
		if len(a.pending) == 0 {
			closed := a.closed
			a.mu.Unlock()
			if closed {
				return
			}
			<-a.wake
			continue
		}
		f := a.pending[0]
		a.pending = a.pending[1:]
		a.mu.Unlock()
		f()
	}
}

// sync waits until every task queued before this call has run and everything
// the Recorder accepted is on disk. It is the archive's half of Session.settle
// - the seam that lets a test assert about the file without sleeping.
func (a *arc) sync() {
	if a == nil {
		return
	}
	ack := make(chan struct{})
	a.do(func() { close(ack) })
	select {
	case <-ack:
	case <-a.done:
	}
	_ = a.rec.Flush(a.ctx)
}

// close drains the queue, flushes the Recorder and closes the file. Everything
// the session accepted before this call is on disk when it returns.
func (a *arc) close() error {
	if a == nil {
		return nil
	}
	a.closeOnce.Do(func() {
		a.mu.Lock()
		a.closed = true
		a.mu.Unlock()
		select {
		case a.wake <- struct{}{}:
		default:
		}
		<-a.done
		a.closeErr = a.rec.Close()
		a.cancel()
		if err := a.db.Close(); err != nil && a.closeErr == nil {
			a.closeErr = err
		}
	})
	return a.closeErr
}

// --- opening -----------------------------------------------------------------

// attachArchive opens the profile's archive, if it was given one, and returns
// the id sequence the store must mint from: one that continues the archive's
// own numbering, so a restored message's id is its rowid and "later" means the
// same thing on both sides of the disk (model.IDSeq).
//
// A failure is not fatal. eco without an archive is eco before M9 - history
// lives in RAM and dies with the process - and that is a much better outcome
// than refusing to connect because a directory is read-only. The error is
// reported into the network's log once the session is up.
func (s *Session) attachArchive(path string) *model.IDSeq {
	if path == "" {
		return nil
	}
	a, err := openArc(path)
	if err != nil {
		s.arcErr = err
		return nil
	}
	s.arc = a
	maxRow, err := a.db.MaxRowID(a.ctx)
	if err != nil {
		s.arcErr = err
		_ = a.close()
		s.arc = nil
		return nil
	}
	s.arcBase = maxRow
	s.applyRetention()
	return model.NewIDSeq(uint64(maxRow))
}

// applyRetention pushes the retention preference into the file that stores it
// and enforces it. Session-only retention is enforced by the archive at open
// and at close; the rest is a purge of everything past the horizon.
func (s *Session) applyRetention() {
	a := s.arc
	if a == nil {
		return
	}
	want := archive.Retention(config.Current().Retention)
	if !want.Valid() {
		want = archive.RetainForever
	}
	a.do(func() {
		if cur, err := a.db.Retention(a.ctx); err != nil || cur != want {
			if err := a.db.SetRetention(a.ctx, want); err != nil {
				s.archiveFailed("saving the retention setting", err)
				return
			}
		}
		n, err := a.db.ApplyRetention(a.ctx)
		if err != nil {
			s.archiveFailed("applying retention", err)
			return
		}
		if n > 0 {
			// A horizon the user set is an erasure they asked for, so it is
			// paid for here rather than left as free pages holding the text
			// of what was supposedly deleted (archive.DB.Scrub).
			if err := a.db.Scrub(a.ctx); err != nil {
				s.archiveFailed("reclaiming space after a history sweep", err)
			}
		}
	})
}

// archiveFailed reports a disk problem where the user can see it, once per
// sentence rather than once per row: an archive that has stopped working is
// worth one line in the network's log and no more.
func (s *Session) archiveFailed(doing string, err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	if s.arcReported {
		s.mu.Unlock()
		return
	}
	s.arcReported = true
	items := s.statusLocked(serverBuffer, "History: "+doing+" failed ("+err.Error()+"). This session is not being saved.")
	s.mu.Unlock()
	s.emit(items)
}

// --- the live path -----------------------------------------------------------

// recordLocked hands one accepted change to the archive. It runs inside
// applyChanges, under s.mu, and never touches the disk itself: the Recorder
// takes a message and returns, and everything else goes on the worker's queue.
//
// What is NOT recorded here: a change the session is folding back in FROM the
// archive (s.noRecord), and a pending echo that still has a label - that
// message has no msgid yet and will be recorded when its echo resolves it,
// which is the one place it is durable and dedupable (archive package doc).
func (s *Session) recordLocked(c model.Change) {
	if s.arc == nil || s.noRecord {
		return
	}
	switch ch := c.(type) {
	case model.MessageAdded:
		if ch.Message.Pending == model.PendingEcho && ch.Message.Label != "" {
			return
		}
		s.recordMessageLocked(ch.Message)

	case model.MessageResolved:
		s.recordMessageLocked(ch.Message)

	case model.HistoryLoaded:
		for _, m := range ch.Messages {
			s.recordMessageLocked(m)
		}

	case model.MessageRedacted:
		s.redactLocked(ch)

	case model.BufferCreated:
		s.ensureBufferLocked(ch.Info, "")

	case model.ModeChanged:
		// The key is worth keeping: a rejoin that has to ask for it is a rejoin
		// that fails silently, which is the v1 bug the store already fixed for
		// one process's lifetime.
		if ch.Key != "" {
			if b, ok := s.store.BufferInfo(ch.Meta().Buffer); ok {
				s.ensureBufferLocked(b, ch.Key)
			}
		}

	case model.ReadMarkerMoved:
		s.persistMarkerLocked(ch)

	case model.ListUpdated:
		s.cacheChannelsLocked(ch)
	}
}

// recordMessageLocked queues one message for the disk, applying the two
// exclusions this layer owns: the status lines the app invented for itself
// (they are chrome, not conversation) and, when the user has asked for it, every
// private message.
func (s *Session) recordMessageLocked(m model.Message) {
	if s.arc == nil || m.Buffer.IsZero() {
		return
	}
	if s.excludedLocked(m.Buffer) {
		return
	}
	s.arc.rec.Record(archive.FromModel(m))
}

// excludedLocked reports whether a buffer is one the user asked never to write
// down. The per-buffer opt-out is enforced inside the Recorder (it is the
// archive's own rule, and it has to survive a restart); this is the global
// "never log DMs" switch, which is a preference and can flip while a message is
// already on its way.
func (s *Session) excludedLocked(id model.BufferID) bool {
	if !config.Current().ExcludeDMLogs {
		return false
	}
	info, ok := s.store.BufferInfo(id)
	return ok && info.Kind == model.BufferQuery
}

// ensureBufferLocked records a buffer's identity so the archive can name it
// after a restart - and so the per-buffer settings hanging off that row
// (logging, notify level, key) have somewhere to live.
func (s *Session) ensureBufferLocked(info model.BufferInfo, key string) {
	a := s.arc
	if a == nil {
		return
	}
	meta := archive.BufferMeta{
		Name:    archive.FoldedName(info.ID),
		Display: info.Name,
		Kind:    info.Kind,
		Key:     key,
	}
	dmExcluded := config.Current().ExcludeDMLogs && info.Kind == model.BufferQuery
	a.do(func() {
		if err := a.db.EnsureBuffer(a.ctx, meta); err != nil {
			s.archiveFailed("saving a buffer", err)
			return
		}
		if dmExcluded {
			// The preference is the user's standing answer; write it through so
			// the archive enforces it too, and so it is still true next time.
			if err := a.db.SetLogging(a.ctx, meta.Name, false); err != nil {
				s.archiveFailed("turning logging off", err)
			}
		}
	})
}

// redactLocked purges a withdrawn message from the file. The Recorder is
// flushed first: the message being withdrawn may still be in its queue, and
// deleting a row that has not been written yet leaves the withdrawn text on
// disk forever.
func (s *Session) redactLocked(ch model.MessageRedacted) {
	a := s.arc
	if a == nil || ch.MsgID == "" {
		return
	}
	by, reason, at := ch.ByNick, ch.Reason, s.store.Clock()
	a.do(func() {
		if err := a.rec.Flush(a.ctx); err != nil {
			s.archiveFailed("flushing before a deletion", err)
		}
		if _, err := a.db.Redact(a.ctx, ch.MsgID, by, reason, at); err != nil {
			s.archiveFailed("deleting a message", err)
		}
	})
}

// persistMarkerLocked stores a read marker. The archive resolves conflicts the
// same way the store does - max(server, local), ADR-3 - so it does not matter
// which of the two moved first.
func (s *Session) persistMarkerLocked(ch model.ReadMarkerMoved) {
	a := s.arc
	if a == nil {
		return
	}
	folded := archive.FoldedName(ch.Meta().Buffer)
	m := archive.Marker{MsgID: ch.MsgID, At: ch.At}
	a.do(func() {
		if _, _, err := a.db.SetReadMarker(a.ctx, folded, m); err != nil {
			s.archiveFailed("saving a read marker", err)
		}
	})
}

// cacheChannelsLocked streams a LIST scan into list_cache as it arrives
// (blueprint §5.4: LIST results are never resident). The store hands them over
// in chunks and forgets them at the end of the scan; this is where they land.
func (s *Session) cacheChannelsLocked(ch model.ListUpdated) {
	a := s.arc
	if a == nil || len(ch.Items) == 0 {
		return
	}
	items := append([]model.ChannelInfo(nil), ch.Items...)
	at := s.store.Clock()
	a.do(func() {
		if err := a.db.PutChannels(a.ctx, items, at); err != nil {
			s.archiveFailed("saving the channel list", err)
		}
	})
}

// --- the cold path -----------------------------------------------------------

// hydrateLocked asks the archive for a buffer's newest page and its read
// marker, once per buffer per session. This is what makes "history survives a
// restart" true from the user's side: the channel you join is already full of
// what was said in it, and its badge is what you left it at.
//
// Only rows that predate this session are restored (baseRow is the archive's
// highest rowid at open). Everything above that line was written by this
// process, is already in the window, and - for the rows that carry no msgid,
// like a join notice - cannot be deduped against it. The cut is what keeps the
// live path and the cold path from ever describing the same message twice.
func (s *Session) hydrateLocked(id model.BufferID) {
	a := s.arc
	if a == nil || id.IsServer() || s.hydrated[id.Folded] {
		return
	}
	s.hydrated[id.Folded] = true
	folded := archive.FoldedName(id)
	a.do(func() {
		meta, hasMeta, err := a.db.Buffer(a.ctx, folded)
		if err != nil {
			s.archiveFailed("reading a buffer", err)
			return
		}
		rows, err := a.db.Latest(a.ctx, folded, hydrateLimit)
		if err != nil {
			s.archiveFailed("reading history", err)
			return
		}
		complete := len(rows) < hydrateLimit

		s.mu.Lock()
		var items []dispatch.Item
		if hasMeta {
			// The buffer's own settings come back with its history: the read
			// marker §4.4 derives everything from, and the notification level
			// the user set on it (attention.go).
			s.restoreNotifyLevelLocked(id, meta.NotifyLevel)
			if !meta.Marker.IsZero() {
				items = append(items, s.applyRestoredLocked(
					s.store.RestoreMarker(id, meta.Marker.MsgID, meta.Marker.At))...)
			}
		}
		items = append(items, s.restoreLocked(id, rows, complete)...)
		s.mu.Unlock()
		s.emit(items)
	})
}

// forgetArchiveState drops what this layer remembers about a buffer that no
// longer exists. It is what makes closing and reopening a conversation restore
// its history again instead of leaving it empty: the window is gone with the
// buffer, so the next one is cold and has to be filled from the file. (This is
// the same reasoning as state.NeedsHistory, which resets per buffer for exactly
// the same reason.)
func (s *Session) forgetArchiveState(folded string) {
	delete(s.hydrated, folded)
	delete(s.paging, folded)
	delete(s.pagingWire, folded)
	delete(s.exhausted, folded)
}

// restoreLocked folds a page of archived rows into the store and returns the
// items that put them on screen. Callers hold s.mu.
//
// One class of row is dropped: a row THIS session wrote (rowid above the
// archive's high-water mark at open) that carries no msgid. Every other row can
// be recognised again - by its msgid, or by the id it kept from the archive -
// but a status line eco wrote a moment ago has neither, and reading it back
// would put a second "You joined #eco" above the first.
func (s *Session) restoreLocked(id model.BufferID, rows []archive.Message, complete bool) []dispatch.Item {
	if len(rows) == 0 {
		return nil
	}
	msgs := make([]model.Message, 0, len(rows))
	self := s.selfFoldedLocked()
	for _, r := range rows {
		if r.MsgID == "" && r.RowID > s.arcBase {
			continue
		}
		msgs = append(msgs, restoredMessage(id, r, self, s.foldLocked(r.Sender)))
	}
	return s.applyRestoredLocked(s.store.RestoreMessages(id, msgs, complete))
}

// applyRestoredLocked folds changes that came FROM the archive, with recording
// switched off for the duration. Writing them back would be a slow way of
// achieving nothing for a row with a msgid (INSERT OR IGNORE collapses it) and
// an actual duplicate for a row without one.
func (s *Session) applyRestoredLocked(cs []model.Change) []dispatch.Item {
	if len(cs) == 0 {
		return nil
	}
	prev := s.noRecord
	s.noRecord = true
	items := s.applyChanges(cs)
	s.noRecord = prev
	return items
}

// restoredMessage projects an archived row back onto the store's message.
//
// Two fields cannot come off the disk and are rebuilt here. Self is a
// comparison against the nick we are using now, because the archive stores who
// spoke and not whether that was us. Highlight is left false: a mention you
// have already seen is not a mention again, which is the same rule playback
// follows. Playback itself is false, deliberately - these messages are not a
// replay, they are the buffer's own contents, and the unread count derived
// against the restored read marker is the whole point of restoring them.
func restoredMessage(id model.BufferID, r archive.Message, selfFolded, senderFolded string) model.Message {
	return model.Message{
		ID:       model.MessageID(r.RowID),
		MsgID:    r.MsgID,
		Buffer:   id,
		Time:     r.Time,
		Sender:   model.Sender{Nick: r.Sender, Account: r.Account, Self: selfFolded != "" && senderFolded == selfFolded},
		Kind:     r.Kind,
		Text:     r.Body,
		Tags:     r.Tags,
		Pending:  r.Pending,
		Redacted: r.Redacted,
	}
}

// selfFoldedLocked is our own nick, folded with the network's casemapping.
func (s *Session) selfFoldedLocked() string {
	nick, _, _ := s.store.Self(s.netID)
	if nick == "" && s.client != nil {
		nick = s.client.CurrentNick()
	}
	if nick == "" {
		return ""
	}
	return s.foldLocked(nick)
}

// --- the backward path -------------------------------------------------------

// LoadOlder is scroll-up: the reader reached the top of a buffer and wants the
// page above it. The archive answers when it can, and when it cannot the
// server is asked with CHATHISTORY BEFORE the oldest message we hold.
//
// It is safe to call repeatedly - the UI fires it from a scroll position, which
// is a thing that happens dozens of times a second - because a request already
// in flight for that buffer swallows the next one.
func (s *Session) LoadOlder(name string) {
	s.mu.Lock()
	id := s.idLocked(name)
	if id.IsServer() || s.paging[id.Folded] || s.exhausted[id.Folded] {
		s.mu.Unlock()
		return
	}
	oldest, ok := s.store.Oldest(id)
	if !ok {
		s.mu.Unlock()
		return // nothing on screen to page above; the cold path covers this
	}
	// The wire is anchored on the oldest row the SERVER can name - the oldest
	// one carrying a msgid (state.OldestNamed), which is what §5.4 spells as
	// `CHATHISTORY BEFORE msgid=<oldest>`. It is not simply the oldest row: a
	// freshly joined channel's oldest row is the status line eco wrote for
	// itself, which has no msgid and carries eco's LOCAL clock, and anchoring
	// on that asks the server a question in a timebase the two do not share.
	// Falling back to the oldest row keeps a buffer holding nothing but eco's
	// own lines able to ask at all. The archive is anchored separately, on the
	// oldest row that can name a position on disk (state.OldestAnchored).
	wire := oldest
	if named, ok := s.store.OldestNamed(id); ok {
		wire = named
	}
	anchor, anchored := s.store.OldestAnchored(id, model.MessageID(s.arcBase))
	s.paging[id.Folded] = true
	items := []dispatch.Item{s.loadingItem(id, true)}
	s.mu.Unlock()
	s.emit(items)

	if s.arc == nil || !anchored {
		s.askServerForOlder(id, wire)
		return
	}
	a := s.arc
	q := archive.PageQuery{Buffer: archive.FoldedName(id), BeforeMsgID: anchor.MsgID, Limit: pageLimit}
	if anchor.MsgID == "" {
		q.BeforeRowID = int64(anchor.ID)
	}
	a.do(func() {
		rows, err := a.db.Page(a.ctx, q)
		if err != nil {
			s.archiveFailed("reading older messages", err)
			s.finishPaging(id)
			return
		}
		s.mu.Lock()
		items := s.restoreLocked(id, rows, len(rows) < pageLimit)
		s.mu.Unlock()
		s.emit(items)
		// Nothing came back - the archive is empty this far up, or everything
		// it held there was already on screen. That is the miss, and the miss
		// is the server's question. Asking on "no rows" alone would miss the
		// second case and leave the reader at a wall.
		if len(items) == 0 {
			s.askServerForOlder(id, wire)
			return
		}
		s.finishPaging(id)
	})
}

// askServerForOlder is the miss path: the archive has nothing older, so the
// server is asked. The reply arrives as an ordinary chathistory batch and is
// merged by msgid like any other, then recorded - so the same scroll-up next
// time is served from disk.
//
// When there is nobody to ask, the buffer is marked exhausted: the UI fires
// this from a scroll position, and a question with no possible answer must not
// be asked again on every pixel. A reconnect clears the mark.
func (s *Session) askServerForOlder(id model.BufferID, oldest model.Message) {
	s.mu.Lock()
	name := s.nameOf(id)
	sent := false
	if s.connectedLocked() && s.store.HasCap(s.netID, "draft/chathistory") {
		req := irc.HistoryRequest{Target: name, Direction: model.HistoryBefore,
			MsgID: oldest.MsgID, Time: oldest.Time, Limit: pageLimit}
		if _, err := s.client.History(req); err == nil {
			sent = true
			s.pagingWire[id.Folded] = true
		}
	}
	var items []dispatch.Item
	if !sent {
		s.exhausted[id.Folded] = true
		items = s.finishPagingLocked(id)
	}
	s.mu.Unlock()
	s.emit(items)
}

// finishPaging clears the in-flight flag and takes the "Loading older…" state
// off the screen.
func (s *Session) finishPaging(id model.BufferID) {
	s.mu.Lock()
	items := s.finishPagingLocked(id)
	s.mu.Unlock()
	s.emit(items)
}

func (s *Session) finishPagingLocked(id model.BufferID) []dispatch.Item {
	if !s.paging[id.Folded] {
		return nil
	}
	delete(s.paging, id.Folded)
	delete(s.pagingWire, id.Folded)
	return []dispatch.Item{s.loadingItem(id, false)}
}

// wireHistoryLocked closes the loop for a scroll-up that had to go to the
// server: the chunk has landed, so the buffer is no longer loading - and if
// the server says that was the end of it, no scroll-up will ask again.
func (s *Session) wireHistoryLocked(ch model.HistoryLoaded) []dispatch.Item {
	id := ch.Meta().Buffer
	if !s.pagingWire[id.Folded] {
		return nil
	}
	if len(ch.Messages) == 0 || ch.Complete {
		s.exhausted[id.Folded] = true
	}
	return s.finishPagingLocked(id)
}

// loadingItem is the "Loading older…" state as a change on the one queue
// everything else travels on, so the UI learns about it in order with the rows
// it is waiting for rather than through a side channel.
func (s *Session) loadingItem(id model.BufferID, loading bool) dispatch.Item {
	return dispatch.Of(dispatch.HistoryState{
		ChangeMeta: model.BufMeta(id),
		Loading:    loading,
	}, s.nameOf(id))
}

// --- reconnect gap-fill -------------------------------------------------------

// gapFill is one reconnect's catch-up. It exists so the user is told "you
// missed twelve messages" once, instead of watching a hundred lines replay.
type gapFill struct {
	active   bool
	pending  int // history requests issued and not yet answered
	caught   int // messages the answers brought back
	reported bool
}

// gapFillLocked is what a RECONNECT does instead of replaying everything:
// CHATHISTORY AFTER the newest message we already have, per open buffer, plus
// TARGETS so a private message that arrived while we were away is discovered at
// all. The first connection of a session does not do this - there is no gap to
// fill, and the per-buffer LATEST on join is the right request there.
func (s *Session) gapFillLocked() {
	if !s.store.HasCap(s.netID, "draft/chathistory") {
		return
	}
	s.gap = gapFill{active: true}

	// TARGETS first, so the buffers it discovers are asked about before the
	// counter can reach zero and report a total that does not include them.
	since := s.gapSinceLocked()
	if _, err := s.client.HistoryTargets(since, s.store.Clock(), 100); err == nil {
		s.gap.pending++
		s.targetsPending = true
	}

	for _, g := range s.store.GapFillTargets(s.netID) {
		if g.Buffer.IsServer() {
			continue
		}
		req := irc.HistoryRequest{Target: g.Target, Direction: model.HistoryAfter,
			MsgID: g.MsgID, Time: g.At, Limit: pageLimit}
		if _, err := s.client.History(req); err == nil {
			s.gap.pending++
		}
	}
	if s.gap.pending == 0 {
		s.gap = gapFill{}
	}
}

// gapSinceLocked is how far back TARGETS should look: the newest message this
// session knows about, or a day, whichever is closer. A client that has been
// off for a week does not want a week of conversations opening at once.
func (s *Session) gapSinceLocked() time.Time {
	now := s.store.Clock()
	since := now.Add(-24 * time.Hour)
	for _, g := range s.store.GapFillTargets(s.netID) {
		if g.At.After(since) {
			since = g.At
		}
	}
	return since
}

// gapChunkLocked counts one answered gap-fill request. When the last one lands
// the user gets the quiet sentence the blueprint asks for, in the network's log
// - one line for the whole reconnect, whatever it brought back.
func (s *Session) gapChunkLocked(ch model.HistoryLoaded) []dispatch.Item {
	if !s.gap.active {
		return nil
	}
	s.gap.caught += len(ch.Messages)
	if s.gap.pending > 0 {
		s.gap.pending--
	}
	return s.gapReportLocked()
}

// gapTargetLocked is a conversation TARGETS discovered - a private message that
// arrived while we were offline, in a buffer nothing else would have opened. It
// is fetched like any other gap, and counted like one.
func (s *Session) gapTargetLocked(e model.HistoryTarget) {
	if !s.gap.active || s.client == nil || !s.store.HasCap(s.netID, "draft/chathistory") {
		return
	}
	req := irc.HistoryRequest{Target: e.Target, Direction: model.HistoryLatest, Limit: pageLimit}
	if _, err := s.client.History(req); err == nil {
		s.gap.pending++
	}
}

// targetsDoneLocked closes the TARGETS half of the gap-fill: the batch that
// carried the answers has ended, so everything it is going to discover has been
// discovered and asked about.
func (s *Session) targetsDoneLocked() []dispatch.Item {
	if !s.targetsPending {
		return nil
	}
	s.targetsPending = false
	if !s.gap.active {
		return nil
	}
	if s.gap.pending > 0 {
		s.gap.pending--
	}
	return s.gapReportLocked()
}

// chathistoryFailedLocked handles a server that refuses a CHATHISTORY request
// (`FAIL CHATHISTORY ...`). Something asked for is never going to be answered,
// so whatever was waiting on it stops waiting: the catch-up line still gets
// printed, and a "Loading older…" that will never load stops saying so.
func (s *Session) chathistoryFailedLocked() []dispatch.Item {
	var out []dispatch.Item
	for folded := range s.pagingWire {
		out = append(out, s.finishPagingLocked(model.BufferID{Network: s.netID, Folded: folded})...)
	}
	if !s.gap.active {
		return out
	}
	if s.targetsPending {
		s.targetsPending = false
	} else if s.gap.pending > 0 {
		s.gap.pending--
	}
	return append(out, s.gapReportLocked()...)
}

func (s *Session) gapReportLocked() []dispatch.Item {
	if s.gap.pending > 0 || s.gap.reported || s.targetsPending {
		return nil
	}
	caught := s.gap.caught
	s.gap = gapFill{reported: true}
	if caught == 0 {
		return s.statusLocked(serverBuffer, "Reconnected - nothing was missed.")
	}
	if caught == 1 {
		return s.statusLocked(serverBuffer, "Reconnected - caught up 1 message.")
	}
	return s.statusLocked(serverBuffer,
		fmt.Sprintf("Reconnected - caught up %d messages.", caught))
}

// --- privacy controls ---------------------------------------------------------

// SetLogging turns the archive on or off for one buffer. The setting lives in
// the file it governs, so it survives a restart, and the Recorder is told
// before this returns - a privacy control that only works through one of two
// doors is not a privacy control (archive.DB.SetLogging).
func (s *Session) SetLogging(name string, on bool) {
	a := s.arc
	if a == nil {
		return
	}
	s.mu.Lock()
	folded := archive.FoldedName(s.idLocked(name))
	s.mu.Unlock()
	a.do(func() {
		if err := a.db.SetLogging(a.ctx, folded, on); err != nil {
			s.archiveFailed("changing a logging setting", err)
		}
	})
}

// Logging reports whether a buffer is being written to disk. It reads the file
// rather than a cached answer, because the answer is the file's.
func (s *Session) Logging(name string) bool {
	a := s.arc
	if a == nil {
		return false
	}
	s.mu.Lock()
	folded := archive.FoldedName(s.idLocked(name))
	s.mu.Unlock()
	meta, ok, err := a.db.Buffer(a.ctx, folded)
	if err != nil || !ok {
		return err == nil // an unknown buffer has not opted out of anything
	}
	return meta.Logging
}

// Archiving reports whether this session has an archive at all.
func (s *Session) Archiving() bool { return s.arc != nil }

// ApplyDMLogging pushes the "never log private messages" preference onto every
// query buffer this session holds. The preference is the standing answer; this
// is what makes it retroactive for buffers that were opened before it changed.
func (s *Session) ApplyDMLogging(exclude bool) {
	a := s.arc
	if a == nil {
		return
	}
	s.mu.Lock()
	var names []string
	for _, folded := range s.order {
		id := model.BufferID{Network: s.netID, Folded: folded}
		if info, ok := s.store.BufferInfo(id); ok && info.Kind == model.BufferQuery {
			names = append(names, archive.FoldedName(id))
		}
	}
	s.mu.Unlock()
	a.do(func() {
		for _, folded := range names {
			if err := a.db.SetLogging(a.ctx, folded, !exclude); err != nil {
				s.archiveFailed("changing a logging setting", err)
				return
			}
		}
	})
}

// ForgetBuffer erases one conversation from the archive: the rows, their
// search index entries, and the buffer's own settings row keeps only its
// identity. It is "delete this conversation" and it is not undoable.
func (s *Session) ForgetBuffer(name string) {
	a := s.arc
	if a == nil {
		return
	}
	s.mu.Lock()
	folded := archive.FoldedName(s.idLocked(name))
	s.mu.Unlock()
	a.do(func() {
		if err := a.rec.Flush(a.ctx); err != nil {
			s.archiveFailed("flushing before an erase", err)
		}
		if _, err := a.db.Forget(a.ctx, folded); err != nil {
			s.archiveFailed("erasing a conversation", err)
			return
		}
		// "Erase" is a promise about the file and not about a query: deleting
		// the rows leaves the words in the search index's segments and on the
		// free list, where anything reading the bytes still finds them. Scrub
		// is what makes it true (archive.DB.Scrub).
		if err := a.db.Scrub(a.ctx); err != nil {
			s.archiveFailed("erasing a conversation", err)
		}
	})
}

// --- the channel cache --------------------------------------------------------

// Channels answers the browser from list_cache, which is where a LIST scan
// streams to and where it survives a restart. match is a substring of the
// channel name; an empty match is "the most populated ones".
//
// Without an archive the store's own transient copy is the answer, which is
// what the offline suite runs on and what a profile whose archive failed to
// open falls back to.
func (s *Session) Channels(match string, limit int) []ChannelInfo {
	if a := s.arc; a != nil {
		found, err := a.db.Channels(a.ctx, match, limit)
		if err == nil {
			out := make([]ChannelInfo, 0, len(found))
			for _, c := range found {
				out = append(out, ChannelInfo{Name: c.Name, Users: c.Users, Topic: c.Topic})
			}
			return out
		}
		s.archiveFailed("reading the channel list", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ChannelInfo, 0, 16)
	for _, c := range s.store.ChannelList(s.netID) {
		if match != "" && !strings.Contains(strings.ToLower(c.Name), strings.ToLower(match)) &&
			!strings.Contains(strings.ToLower(c.Topic), strings.ToLower(match)) {
			continue
		}
		out = append(out, ChannelInfo{Name: c.Name, Users: c.Users, Topic: c.Topic})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// --- searching ----------------------------------------------------------------

// Search is M12's two search surfaces meeting the file: the Finder
// (Ctrl+Shift+F) asks across everything, and Ctrl+F's bar asks about one buffer
// once the loaded window has run out of answers. Both are archive.DB.Search
// with its own query grammar (from:/in:/before:/after:) - parsed there, in one
// place, so the two surfaces cannot come to understand a query differently.
//
// Threading: these run SYNCHRONOUSLY on the caller's goroutine, which for both
// callers is the Fyne thread. That is deliberate and it is not the rule the
// worker exists to enforce. The worker keeps the DISK off the goroutine that
// owns the store, because that goroutine is the pump and a stalled pump is a
// stalled connection. A search is a read, the archive has a reader pool of its
// own on a WAL file, and reads there never wait for the writer (proven in
// archive's TestReadsDoNotWaitForWrites). What a search must not do is take an
// unbounded amount of time, and that is what archive.SearchWindow is for: 60ms
// on a million rows, measured, rather than 2.3s.
func (s *Session) Search(query string, f archive.Filters, limit int) []archive.Hit {
	a := s.arc
	if a == nil {
		return nil
	}
	hits, err := a.db.Search(a.ctx, query, f, limit)
	if err != nil {
		s.archiveFailed("searching the archive", err)
		return nil
	}
	return hits
}

// SearchBuffer is the same question about one conversation. The folding is done
// here because only the session knows the network's CASEMAPPING, which is the
// same reason archive.Filters asks for a folded name rather than folding one.
func (s *Session) SearchBuffer(name, query string, limit int) []archive.Hit {
	if s.arc == nil {
		return nil
	}
	s.mu.Lock()
	folded := archive.FoldedName(s.idLocked(name))
	s.mu.Unlock()
	return s.Search(query, archive.Filters{Buffer: folded}, limit)
}

// BufferName answers what a hit's stored buffer is called on screen. A folded
// name is the archive's key, not something to show anybody.
//
// Three answers in order, and the order is the point: an open buffer knows its
// own name; a closed one was written down with the name it had (the buffers
// table's display column, which is why it exists); and failing both, the folded
// key itself is at least true - a channel's folded name is its own name,
// lowercased. What it must never fall back to is the network log, which is what
// asking nameOf alone would have returned for every buffer this session has not
// opened, quietly filing months of history under "server".
func (s *Session) BufferName(folded string) string {
	if folded == "" || folded == archive.ServerBufferName {
		return serverBuffer
	}
	s.mu.Lock()
	b := s.bufs[folded]
	s.mu.Unlock()
	if b != nil {
		return b.Name
	}
	if a := s.arc; a != nil {
		if meta, ok, err := a.db.Buffer(a.ctx, folded); err == nil && ok && meta.Display != "" {
			return meta.Display
		}
	}
	return folded
}

// LineIndex is where a message sits in a buffer's on-screen window, or -1. It
// is what turns a search hit into a place to scroll to, and it is the same
// index the pane draws - the projection's rows and its store ids are one array
// pair (Buffer.ids).
func (s *Session) LineIndex(name string, id model.MessageID) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.bufs[s.idLocked(name).Folded]
	if b == nil {
		return -1
	}
	return b.index(id)
}

// jumpContext is how much conversation comes with a message jumped to: enough
// above it to read into, and the rest of the way back to what is already on
// screen below it.
const jumpContext = 60

// jumpReach is how far back a jump will pull in one go.
//
// It is a bound on the WINDOW, not on the search. The buffer on screen is a
// bounded cache over the archive (state.RestoreHeadroom), and Around returns a
// contiguous run - so a message within reach is pulled in with everything
// between it and what was already there, and the reader can scroll from one to
// the other without a hole. A message further back than this is still findable,
// and the Finder still shows it with its context in the list; what it is not is
// openable in place, because the window it would have to open into does not
// stretch that far.
const jumpReach = 4000

// JumpTo opens a buffer at one archived message: the M9 pager's Around, folded
// back into the window through the same restore path scroll-up uses, so the
// history that arrives is history the buffer really has rather than a second
// view of it. It reports whether the message ended up on screen.
//
// It is a no-op when the message is already in the window, which is the common
// case for Ctrl+F and costs one walk of the row ids.
func (s *Session) JumpTo(name string, rowID int64) bool {
	if rowID <= 0 {
		return false
	}
	if s.LineIndex(name, model.MessageID(rowID)) >= 0 {
		return true
	}
	a := s.arc
	if a == nil {
		return false
	}
	rows, err := a.db.Around(a.ctx, rowID, jumpContext, jumpReach)
	if err != nil {
		s.archiveFailed("opening a message from the archive", err)
		return false
	}
	s.mu.Lock()
	id := s.idLocked(name)
	items := s.restoreLocked(id, rows, false)
	s.mu.Unlock()
	s.emit(items)
	return s.LineIndex(name, model.MessageID(rowID)) >= 0
}
