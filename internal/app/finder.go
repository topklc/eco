package app

import (
	"sort"
	"strings"

	"github.com/ergochat/irc-go/ircfmt"

	"github.com/toprakkilic/eco/internal/archive"
	"github.com/toprakkilic/eco/internal/config"
	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/ui/dispatch"
	"github.com/toprakkilic/eco/internal/ui/widgets"
)

// The Finder (Ctrl+Shift+F) - blueprint §4.3 and §4.6's "what did X say about
// the deploy? is Ctrl+Shift+F".
//
// It is full-text search over everything eco remembers, in the same panel the
// Switcher uses, and the query grammar is archive's: from:, in:, before:,
// after:, on:, "quoted phrases", prefix*. There is no second parser here and no
// UI for the filters - a row of dropdowns for "from" and "in" would be four
// more widgets saying what four characters already say, and the grammar is
// documented in one place (archive.Search) because it is implemented in one.
//
// Enter opens the message where it lives: the buffer, with the conversation
// around it pulled out of the archive (Session.JumpTo), and the reader landed
// on the line itself.

// finderLimit is how many hits are ranked into the panel. Sixty rows is what
// fits; asking for a few more than that is what makes "and there is more" an
// honest note rather than a guess.
const finderLimit = 61

// finderMinQuery is how much has to be typed before the disk is asked. One
// letter matches most of the archive, ranks it, and answers a question nobody
// has finished asking.
const finderMinQuery = 2

// finderHit is one row of the Finder: where the message is, and enough to open
// it again.
type finderHit struct {
	session dispatch.SessionID
	buffer  string // display name
	rowID   int64
	hit     archive.Hit
}

// showFinder opens Ctrl+Shift+F. It seeds the field with whatever Ctrl+F was
// last looking for, so "not in this conversation, then" is one keystroke.
func (u *UI) showFinder() {
	if !u.built() || len(u.sessions) == 0 {
		return
	}
	u.openOverlay(u.finderBox, u.finder)
	if u.lastFind != "" {
		u.finder.SetQuery(u.lastFind)
	}
}

// finderQueryRows runs the query and turns the hits into rows. The Palette asks
// for it on every keystroke; see the note on finderMinQuery, and on
// Session.Search for why a synchronous disk read is the right shape here.
func (u *UI) finderQueryRows() []widgets.PaletteRow {
	q := strings.TrimSpace(u.finder.Query())
	u.finderRows = nil
	if len([]rune(q)) < finderMinQuery {
		return nil
	}
	u.finderRows = u.searchAll(q)
	rows := make([]widgets.PaletteRow, 0, len(u.finderRows))
	for _, h := range u.finderRows {
		rows = append(rows, finderRow(h, u.networkNote(h.session), len(u.sessions) > 1))
	}
	return rows
}

// searchAll asks every open network's archive and merges the answers.
//
// Merging by rank across archives is not quite comparing like with like - bm25
// is scored against the corpus it ran on - but the alternative is grouping the
// results by network, which asks the reader to know which network a sentence
// was said on before they can look for it. That is the wrong way round: the
// question is "who said this", and where is part of the answer.
func (u *UI) searchAll(query string) []finderHit {
	var out []finderHit
	for _, s := range u.sessions {
		for _, h := range s.Search(query, archive.Filters{}, finderLimit) {
			out = append(out, finderHit{
				session: s.ID(),
				buffer:  s.BufferName(h.Buffer),
				rowID:   h.RowID,
				hit:     h,
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].hit.Rank != out[j].hit.Rank {
			return out[i].hit.Rank < out[j].hit.Rank // bm25: lower is better
		}
		return out[i].hit.Time.After(out[j].hit.Time)
	})
	if len(out) > finderLimit {
		out = out[:finderLimit]
	}
	return out
}

// finderRow is one hit's words: the buffer it was said in, the nick and the
// snippet, and when.
func finderRow(h finderHit, network string, multi bool) widgets.PaletteRow {
	title := h.buffer
	if multi && network != "" {
		title += " · " + network
	}
	body := snippetText(h.hit)
	if h.hit.Sender != "" {
		body = h.hit.Sender + ": " + body
	}
	return widgets.PaletteRow{
		Title:  title,
		Detail: body,
		Meta:   h.hit.Time.Local().Format(finderTimeLayout()),
	}
}

// finderTimeLayout dates a hit as well as timing it: a search result is
// somewhere in months of history, so the clock alone says nothing. The time
// half is the user's own choice of layout, because that is the one they read
// everywhere else.
func finderTimeLayout() string { return "2 Jan " + config.Current().TimeLayout }

// snippetText is the archive's snippet made safe to draw. Two passes, in this
// order and not the other: the match markers come out first (they are control
// characters, chosen so a body containing "<b>" cannot forge a highlight -
// archive.MarkStart), and the IRC formatting after, so a stripper never sees a
// marker and mistakes it for a colour code.
//
// The markers are dropped rather than drawn. A palette row is one line of
// context; the highlight that matters is the one in the conversation the reader
// is about to be standing in.
func snippetText(h archive.Hit) string {
	text := h.Snippet
	if text == "" {
		text = h.Body
	}
	text = markStripper.Replace(text)
	return strings.TrimSpace(foldNewlines(ircfmt.Strip(text)))
}

var markStripper = strings.NewReplacer(archive.MarkStart, "", archive.MarkEnd, "")

// finderChose is Enter: open the message where it was said.
func (u *UI) finderChose(i int) {
	if i < 0 || i >= len(u.finderRows) {
		return
	}
	h := u.finderRows[i]
	u.closeOverlays()
	u.openHit(h)
}

// openHit takes the reader to one archived message: the network, the buffer,
// the conversation around it, and the line itself.
//
// The buffer is opened if it is not already: a hit can be in a channel this
// session has not joined, and refusing to show it would make the Finder able to
// find things it cannot open.
func (u *UI) openHit(h finderHit) {
	s := u.sessionFor(h.session)
	if s == nil || h.buffer == "" {
		return
	}
	s.getBuffer(h.buffer)
	u.selectRow(s, h.buffer)
	if !s.JumpTo(h.buffer, h.rowID) {
		// Further back than the window can stretch (Session.jumpReach). The
		// buffer is open at its newest page, which is where the reader now is;
		// saying so is better than silently landing somewhere else.
		s.status(h.buffer, "That message is further back than this conversation can open in place. "+
			"scroll up, or search again with a narrower date (before: / after:).")
		return
	}
	u.repaintBuffer()
	if i := s.LineIndex(h.buffer, model.MessageID(h.rowID)); i >= 0 {
		u.timeline.ScrollToLine(i)
	}
}
