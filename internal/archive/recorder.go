package archive

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// RecorderOptions configures NewRecorder. The zero value is the shipping
// configuration.
type RecorderOptions struct {
	// Queue is how many messages may wait to be written. 0 means 4096.
	// A full queue drops rather than blocks — see Record.
	Queue int

	// Batch is the largest number of rows one transaction writes. 0 means
	// 256. A reconnect burst arrives faster than fsync, so this is what
	// keeps the transaction count down.
	Batch int

	// Interval is how long a partial batch waits for company before being
	// written anyway. 0 means 200ms. This is the live path's latency: a
	// message the user just sent is on disk within roughly this long.
	Interval time.Duration

	// OnError is called (from the Recorder's goroutine) with any write that
	// failed. nil means the error is only counted, in Stats.Errors and
	// Recorder.Err. It must not block or call back into the Recorder.
	OnError func(error)
}

func (o RecorderOptions) queue() int {
	if o.Queue <= 0 {
		return 4096
	}
	return o.Queue
}

func (o RecorderOptions) batch() int {
	if o.Batch <= 0 {
		return 256
	}
	return o.Batch
}

func (o RecorderOptions) interval() time.Duration {
	if o.Interval <= 0 {
		return 200 * time.Millisecond
	}
	return o.Interval
}

// Stats is a Recorder's running count. Every field is cumulative.
type Stats struct {
	Accepted uint64 // Record returned true this many times
	Written  uint64 // rows that landed in messages
	Deduped  uint64 // rows an existing msgid collapsed onto (ADR-3)
	Dropped  uint64 // Record refused: the queue was full, or the Recorder is closed
	Skipped  uint64 // buffers with logging off — these never reach a statement
	Batches  uint64 // transactions committed
	Errors   uint64 // batches that failed to write
}

// Recorder is the live path's write-behind: it takes messages from whatever
// goroutine produces them and writes them in batches from one goroutine of its
// own, so the caller never waits for the disk.
type Recorder struct {
	db   *DB
	opts RecorderOptions

	// mu makes "Record accepted it" and "the queue is still being drained"
	// one fact rather than two. Record takes it for reading (so any number of
	// producers proceed at once and none of them ever waits on I/O); Close
	// takes it for writing exactly once. That is what makes Close
	// deterministic: after it returns, no further send can be in flight.
	mu     sync.RWMutex
	closed bool
	queue  chan item

	done chan struct{}

	// loggingOff is copy-on-write: readers load the pointer, SetLogging
	// swaps a new map in. A buffer named here never reaches the disk.
	loggingOff atomic.Pointer[map[string]bool]

	accepted, written, deduped, dropped, skipped, batches, errs atomic.Uint64
	lastErr                                                     atomic.Pointer[error]
}

// item is one thing the writer goroutine consumes: a message, or a flush
// request. They travel down the same channel so that a Flush cannot overtake
// the messages recorded before it.
type item struct {
	msg   Message
	flush chan error // non-nil for a flush request
	quit  bool
}

// NewRecorder starts a recorder for db. It reads the per-buffer logging
// opt-outs once, then keeps them current from SetLogging.
func NewRecorder(ctx context.Context, db *DB, opts RecorderOptions) (*Recorder, error) {
	if db == nil {
		return nil, errors.New("archive: NewRecorder with a nil DB")
	}
	off, err := db.LoggingOff(ctx)
	if err != nil {
		return nil, err
	}
	r := &Recorder{
		db:    db,
		opts:  opts,
		queue: make(chan item, opts.queue()),
		done:  make(chan struct{}),
	}
	r.loggingOff.Store(&off)
	db.addRecorder(r)
	go r.run()
	return r, nil
}

// Record hands a message to the writer and returns immediately. It never
// blocks on the disk, never returns an error, and is safe to call from any
// goroutine.
//
// The return value is whether the archive took responsibility for the message:
//
//   - false because the buffer has logging turned off — the message is dropped
//     on purpose and counted in Stats.Skipped. Nothing about it reaches a
//     statement, let alone the file.
//   - false because the queue is full or the Recorder is closed, counted in
//     Stats.Dropped. That is the write-behind's honest failure mode: under a
//     burst big enough to outrun the disk, the archive loses lines rather than
//     the UI losing frames.
//   - true means it is queued, and Close will not return until it is written.
//
// A message with no msgid cannot be deduped, and a message that is still a
// pending echo has no msgid yet: record it when it resolves (package doc).
func (r *Recorder) Record(m Message) bool {
	if m.Buffer == "" {
		r.dropped.Add(1)
		return false
	}
	if off := r.loggingOff.Load(); off != nil && (*off)[m.Buffer] {
		r.skipped.Add(1)
		return false
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		r.dropped.Add(1)
		return false
	}
	select {
	case r.queue <- item{msg: m}:
		r.accepted.Add(1)
		return true
	default:
		r.dropped.Add(1)
		return false
	}
}

// Flush blocks until everything Record accepted before this call is on disk.
// It is what a test uses instead of sleeping, and what the app calls before it
// reads back its own writes.
func (r *Recorder) Flush(ctx context.Context) error {
	r.mu.RLock()
	if r.closed {
		r.mu.RUnlock()
		// Close is flushing, or has. Waiting for it is the honest answer to
		// "is everything on disk?" — returning early would not be.
		select {
		case <-r.done:
			return r.Err()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	ack := make(chan error, 1)
	select {
	case r.queue <- item{flush: ack}:
		r.mu.RUnlock()
	case <-ctx.Done():
		r.mu.RUnlock()
		return ctx.Err()
	}
	select {
	case err := <-ack:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close stops the recorder and flushes. When it returns, every message Record
// accepted is on disk (or its error has been reported), the goroutine is gone,
// and further Records are refused rather than silently lost.
//
// It is safe to call twice and safe to call while other goroutines are still
// calling Record.
func (r *Recorder) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		<-r.done
		return r.Err()
	}
	r.closed = true
	// Nothing can be mid-send now: every Record holds the read lock across
	// its send, so by the time this returns the queue holds exactly the
	// messages Record already accepted.
	r.queue <- item{quit: true}
	r.mu.Unlock()

	<-r.done
	r.db.removeRecorder(r)
	return r.Err()
}

// Err returns the last write error, or nil.
func (r *Recorder) Err() error {
	if p := r.lastErr.Load(); p != nil {
		return *p
	}
	return nil
}

// Stats is a snapshot of the running counts.
func (r *Recorder) Stats() Stats {
	return Stats{
		Accepted: r.accepted.Load(),
		Written:  r.written.Load(),
		Deduped:  r.deduped.Load(),
		Dropped:  r.dropped.Load(),
		Skipped:  r.skipped.Load(),
		Batches:  r.batches.Load(),
		Errors:   r.errs.Load(),
	}
}

// SetLogging turns disk logging for a buffer on or off. It is DB.SetLogging —
// which already tells every live Recorder — under a name the caller holding a
// Recorder will find.
func (r *Recorder) SetLogging(ctx context.Context, folded string, on bool) error {
	return r.db.SetLogging(ctx, folded, on)
}

func (r *Recorder) setLoggingCached(folded string, on bool) {
	for {
		old := r.loggingOff.Load()
		next := make(map[string]bool, len(*old)+1)
		for k, v := range *old {
			next[k] = v
		}
		if on {
			delete(next, folded)
		} else {
			next[folded] = true
		}
		if r.loggingOff.CompareAndSwap(old, &next) {
			return
		}
	}
}

// run is the writer goroutine: the only place in the package that inserts a
// message.
func (r *Recorder) run() {
	defer close(r.done)

	batch := make([]Message, 0, r.opts.batch())
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	armed := false

	disarm := func() {
		if armed && !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		armed = false
	}
	write := func() error {
		if len(batch) == 0 {
			return nil
		}
		err := r.writeBatch(batch)
		batch = batch[:0]
		disarm()
		return err
	}

	// handle consumes one item. It reports whether the recorder is done.
	handle := func(it item) (quit bool) {
		switch {
		case it.quit:
			return true
		case it.flush != nil:
			it.flush <- write()
			return false
		default:
			batch = append(batch, it.msg)
			if len(batch) >= r.opts.batch() {
				_ = write()
				return false
			}
			if !armed {
				timer.Reset(r.opts.interval())
				armed = true
			}
			return false
		}
	}

	for {
		select {
		case it := <-r.queue:
			if !handle(it) {
				continue
			}
			// Quit. Whatever is already in the queue is what Record
			// accepted, and Close promised it: drain it all, then write.
			for {
				select {
				case extra := <-r.queue:
					handle(extra) // a second quit is a no-op here
					continue
				default:
				}
				break
			}
			_ = write()
			return

		case <-timer.C:
			armed = false
			_ = write()
		}
	}
}

// writeBatch inserts one batch in one transaction. Errors are counted and
// reported, never returned to the producer: by the time a write fails the
// caller is long gone.
func (r *Recorder) writeBatch(batch []Message) error {
	err := r.insert(context.Background(), batch)
	r.batches.Add(1)
	if err != nil {
		r.errs.Add(1)
		r.lastErr.Store(&err)
		if r.opts.OnError != nil {
			r.opts.OnError(err)
		}
	}
	return err
}

// insert writes rows and keeps buffers.last_msgid current for the buffers the
// batch touched. INSERT OR IGNORE is the dedupe (ADR-3): a msgid already
// stored collapses onto the row that is there, and a NULL msgid never collides.
func (r *Recorder) insert(ctx context.Context, batch []Message) (err error) {
	tx, err := r.db.w.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("archive: begin: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback() //nolint:errcheck // the write already failed
		}
	}()

	ins, err := tx.PrepareContext(ctx, `
INSERT OR IGNORE INTO messages(msgid, buffer_folded, ts, sender, account, kind, body, tags_json, pending, redacted)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("archive: prepare insert: %w", err)
	}
	defer ins.Close()

	touched := make(map[string]struct{}, 8)
	var written, deduped uint64
	for _, m := range batch {
		tags, terr := m.tagsArg()
		if terr != nil {
			tags = nil // unmarshalable tags are not worth losing the message over
		}
		red := 0
		if m.Redacted {
			red = 1
		}
		res, ierr := ins.ExecContext(ctx, m.msgidArg(), m.Buffer, m.unixMilli(), m.Sender, m.Account,
			int64(m.Kind), m.Body, tags, int64(m.Pending), red)
		if ierr != nil {
			err = fmt.Errorf("archive: insert into %q: %w", m.Buffer, ierr)
			return err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			written++
		} else {
			deduped++
		}
		touched[m.Buffer] = struct{}{}
	}

	// last_msgid is the CHATHISTORY AFTER anchor. Recomputing it per buffer
	// per batch costs one index seek and is always right, which "remember the
	// newest thing I just inserted" is not when a batch replays old history.
	upd, err := tx.PrepareContext(ctx, `
INSERT INTO buffers(name_folded, display_name, last_msgid)
VALUES(?, ?, (SELECT msgid FROM messages WHERE buffer_folded = ? AND msgid IS NOT NULL ORDER BY ts DESC, rowid DESC LIMIT 1))
ON CONFLICT(name_folded) DO UPDATE SET last_msgid = excluded.last_msgid
WHERE excluded.last_msgid IS NOT NULL`)
	if err != nil {
		return fmt.Errorf("archive: prepare buffer update: %w", err)
	}
	defer upd.Close()
	for name := range touched {
		if _, uerr := upd.ExecContext(ctx, name, name, name); uerr != nil {
			err = fmt.Errorf("archive: update buffer %q: %w", name, uerr)
			return err
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("archive: commit: %w", err)
	}
	r.written.Add(written)
	r.deduped.Add(deduped)
	return nil
}
