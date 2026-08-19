package widgets

import (
	"time"

	"github.com/toprakkilic/eco/internal/model"
)

// The row model: everything the Timeline decides about a buffer BEFORE any
// pixel exists. Which lines share a nick header, where the day turns over,
// where the NEW divider sits, which row gets the one bit of air §4.2 allows
// — all of it is a pure function of the lines and the read marker, so all of
// it is tested headless (rows_test.go). The widget below only draws what
// this file decided.
type RowKind uint8

const (
	// RowMessage is one line of chat: gutter, nick column, wrapped body.
	RowMessage RowKind = iota
	// RowArt is one line of mIRC background art, drawn as a character grid
	// by the render engine. It is its own kind because a grid cannot share
	// a row shape with wrapped prose.
	RowArt
	// RowDay is the "—— Monday, 2 June ——" rule between two calendar days.
	RowDay
	// RowNew is the one persistent "NEW — since 14:02" rule, seeded from
	// the read marker when the buffer is opened.
	RowNew
	// RowLoading is the "Loading older…" row shown at the top while a
	// backfill is in flight.
	RowLoading
)

// Row is one entry in the Timeline's list. Rows are values, not widgets:
// the List has a handful of row widgets and pours these through them.
type Row struct {
	Kind RowKind
	// Line is the message a RowMessage or RowArt draws.
	Line model.Line
	// Index is where that line sits in the buffer, or -1 for the rows the
	// Timeline invents (dividers, the loading row). It is what a row menu
	// resolves back to a message.
	Index int
	// Head is true when this row carries the nick header. Author grouping
	// is exactly this bit being false.
	Head bool
	// Gap is the author-change concession: a few pixels of air above the
	// row, and the only spacing decision in the whole timeline.
	Gap bool
	// At is the day a RowDay names, or the marker time a RowNew names.
	At time.Time
}

// GroupWindow is how long an author keeps their header. Two minutes is the
// blueprint's number (§4.2); past it the same nick starts a new group,
// because a reply half an hour later is not part of the same thought.
const GroupWindow = 2 * time.Minute

// RowOptions is what the buffer's state contributes to the row model.
type RowOptions struct {
	// Since seeds the NEW divider: the first line after it opens the unread
	// run. The zero time means the buffer has no marker and gets no divider.
	//
	// It is deliberately NOT the live read marker. The divider is placed
	// once, when the buffer is opened, and then holds still while you read
	// — a line that moves as you catch up is not a landmark.
	Since time.Time
	// Loading puts the "Loading older…" row on top.
	Loading bool
	// Now is the clock day names are relative to; zero means time.Now.
	Now time.Time
	// Window overrides GroupWindow (tests, and nothing else so far).
	Window time.Duration
	// IsArt reports whether a line must be drawn as a character grid. It is
	// injected so the row model stays free of Fyne: the app passes
	// render.Art's verdict.
	IsArt func(model.Line) bool
}

// BuildRows turns a buffer's lines into the rows the Timeline draws.
func BuildRows(lines []model.Line, opts RowOptions) []Row {
	b := newRowBuilder(opts)
	for _, l := range lines {
		b.add(l)
	}
	return b.rows
}

// rowBuilder is BuildRows kept open. Nothing a row decides looks further back
// than the row above it, so the state that separates "build the buffer" from
// "one more message arrived" is four fields — and keeping them means an
// append costs one line's work instead of the whole buffer's.
type rowBuilder struct {
	opts   RowOptions
	window time.Duration
	rows   []Row

	prev      model.Line // the last line that produced a row
	havePrev  bool       // ...and nothing has interrupted it since
	newPlaced bool
	n         int // lines consumed, which is the next line's index
}

func newRowBuilder(opts RowOptions) *rowBuilder {
	b := &rowBuilder{opts: opts, window: opts.Window, newPlaced: opts.Since.IsZero()}
	if b.window == 0 {
		b.window = GroupWindow
	}
	b.rows = make([]Row, 0, 64)
	if opts.Loading {
		b.rows = append(b.rows, Row{Kind: RowLoading, Index: -1})
	}
	return b
}

// add appends one line, asking opts.IsArt what kind of row it needs.
func (b *rowBuilder) add(l model.Line) {
	art := false
	if b.opts.IsArt != nil {
		art = b.opts.IsArt(l)
	}
	b.addKind(l, art)
}

// addKind appends one line whose kind the caller already knows — the widget
// does, because rendering the line is what told it.
func (b *rowBuilder) addKind(l model.Line, art bool) {
	i := b.n
	b.n++
	// the day rule goes above the first line of every calendar day
	if !b.havePrev || !sameDay(b.prev.Time, l.Time) {
		b.rows = append(b.rows, Row{Kind: RowDay, Index: -1, At: l.Time})
		b.havePrev = false // a divider breaks the group, and the air
	}
	// the NEW rule goes above the first line the reader has not seen.
	// Status lines and our own messages never open it: neither was ever
	// unread. Playback does, unlike the unread COUNTER
	// (state.countsUnread) — after a restart the replayed messages are
	// precisely the ones still waiting to be read.
	if !b.newPlaced && l.Time.After(b.opts.Since) && !l.Status && !l.Self {
		b.rows = append(b.rows, Row{Kind: RowNew, Index: -1, At: b.opts.Since})
		b.newPlaced = true
		b.havePrev = false
	}
	kind := RowMessage
	if art {
		kind = RowArt
	}
	head, gap := true, false
	if b.havePrev {
		gap = groupKey(b.prev) != groupKey(l)
		if kind == RowMessage {
			head = gap || !groupable(l) ||
				l.Time.Before(b.prev.Time) || l.Time.Sub(b.prev.Time) > b.window
		}
	}
	b.rows = append(b.rows, Row{Kind: kind, Line: l, Index: i, Head: head, Gap: gap})
	b.prev, b.havePrev = l, true
}

// groupable reports whether a line can share a nick header at all. Status
// lines have no author; actions carry their nick inside the sentence ("*
// alice waves"), so stacking them under one header would lose it.
func groupable(l model.Line) bool {
	return !l.Status && !l.Action && l.Nick != ""
}

// groupKey identifies the author of a row for grouping and for the air above
// an author change. Status lines share one key so a run of joins and parts
// stays tight; everything else is keyed by the nick as it is written.
func groupKey(l model.Line) string {
	switch {
	case l.Status:
		return "\x00status"
	case l.Action:
		return "\x00action\x00" + l.Nick
	case l.Notice:
		return "\x00notice\x00" + l.Nick
	}
	return l.Nick
}

// sameDay asks whether two instants fall on the same day IN THE READER'S OWN
// ZONE, which is the only zone the pane ever shows. Lines reach here as UTC
// instants — the archive hands them back as time.UnixMilli(ts).UTC() and an
// IRCv3 server-time tag parses to Z — so comparing them in their own location
// would put the rule at UTC midnight while the gutter beside it and DayLabel
// above it both print local time. For anyone east or west of Greenwich that is
// a rule in the middle of an ordinary evening and no rule at all where the date
// actually turns over.
func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Local().Date()
	by, bm, bd := b.Local().Date()
	return ay == by && am == bm && ad == bd
}

// DayLabel names a day the way a person would: today and yesterday by name,
// this week by weekday, anything older by date.
func DayLabel(day, now time.Time) string {
	if now.IsZero() {
		now = time.Now()
	}
	day, now = day.Local(), now.Local()
	switch {
	case sameDay(day, now):
		return "Today"
	case sameDay(day, now.AddDate(0, 0, -1)):
		return "Yesterday"
	}
	if age := now.Sub(day); age > 0 && age < 6*24*time.Hour {
		return day.Format("Monday")
	}
	if day.Year() == now.Year() {
		return day.Format("Monday, 2 January")
	}
	return day.Format("2 January 2006")
}
