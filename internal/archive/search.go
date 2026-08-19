package archive

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// Filters narrows a search. The zero value narrows nothing.
type Filters struct {
	// Buffer is a folded buffer name, matched exactly. The caller folds it,
	// because only the caller knows the network's CASEMAPPING — and an exact
	// match is what lets the query use idx_messages_buffer_ts.
	Buffer string

	// Sender matches a nick, ASCII-case-insensitively. It is not folded per
	// CASEMAPPING for the same reason it cannot be: pass the nick as the
	// server spells it and the comparison handles the case.
	Sender string

	// Since is inclusive, Until exclusive. Either may be zero for "open".
	Since time.Time
	Until time.Time
}

// isZero reports whether these filters narrow nothing.
func (f Filters) isZero() bool {
	return f.Buffer == "" && f.Sender == "" && f.Since.IsZero() && f.Until.IsZero()
}

// Hit is one search result: the message, where it sits, and why it matched.
type Hit struct {
	Message

	// Rank is the FTS5 bm25 score with the body weighted above the sender.
	// Lower is better, and hits arrive best-first, so a caller that just
	// renders the slice never has to look at it.
	Rank float64

	// Snippet is the matching part of the body with the matched terms
	// wrapped in Mark.../...MarkEnd. It is empty for a filters-only search,
	// which has no terms to point at.
	Snippet string
}

// The snippet markers. They are ASCII control characters rather than markup so
// that a body containing "<b>" cannot forge a highlight; the renderer turns
// them into whatever it draws with.
//
// They must be bytes IRC FORMATTING never uses, and the first pair chosen was
// not: \x02 is bold and \x03 is the colour introducer, so a renderer stripping
// the markers out of a snippet also removed the message's own colour codes'
// introducers and left the digits behind as text — "\x0304RED ALERT" came back
// as "04RED ALERT". Reordering the two strippers does not fix it, because the
// other order lets a marker be read as a colour code and eat what follows: two
// meanings for one byte has no safe order. So the markers are bytes the
// formatting set does not claim (it uses \x02 \x03 \x04 \x0F \x11 \x16 \x1D
// \x1E \x1F), and anything picked here must stay outside it.
const (
	MarkStart = "\x14"
	MarkEnd   = "\x15"
)

// DefaultSearchLimit is how many hits Search returns when asked for no limit.
const DefaultSearchLimit = 100

const maxSearchLimit = 1000

// Search runs a query and returns ranked hits, best first.
//
// THE QUERY GRAMMAR (all of it)
//
//	from:nick          only messages sent by nick
//	in:#channel        only messages in that buffer (folded name)
//	before:2026-01-31  only messages before that date, exclusive
//	after:2026-01-31   only messages on or after that date, inclusive
//	on:2026-01-31      only messages on that calendar day
//	"exact phrase"     the words, adjacent, in that order
//	prefix*            words starting with prefix
//	everything else    a word that must appear
//
// Words are ANDed: there is no OR, no NOT, no NEAR and no parenthesis, and a
// query that looks like it has them searches for those words literally. Dates
// are YYYY-MM-DD and are read as UTC days; a caller that wants the user's own
// midnight parses the date itself and passes Filters. A date that does not
// parse is not an error and not a filter — it is a search term.
//
// Filters passed in the f argument and filters typed in the query are ANDed
// together, except that a typed filter of the same kind replaces the passed
// one — what the user just typed is what the user meant.
//
// Matching is by FTS5's unicode61 tokenizer with diacritics removed, so search
// is case- and accent-insensitive and splits on punctuation: "don't" finds
// "dont", "@bob" finds "bob". Redacted messages never match — their body is
// gone from the row and from the index.
//
// A query with terms is ranked by bm25 over at most SearchWindow matches (see
// there — a word in every message is not worth two seconds). A query that is
// only filters ("show me everything from:alice in:#eco") has nothing to rank,
// so it comes back newest first with Rank 0, which is the useful order for
// that question.
func (d *DB) Search(ctx context.Context, query string, f Filters, limit int) ([]Hit, error) {
	terms, qf := ParseQuery(query)
	f = mergeFilters(f, qf)

	switch {
	case limit <= 0:
		limit = DefaultSearchLimit
	case limit > maxSearchLimit:
		limit = maxSearchLimit
	}

	if match := ftsMatch(terms); match != "" {
		return d.searchByText(ctx, match, f, limit)
	}
	return d.searchByFilters(ctx, f, limit)
}

// searchByText is the FTS path: the index picks the candidates, the columns
// narrow them.
//
// The buffer and sender filters are pushed into the MATCH expression as well
// as being checked on the columns. FTS5 intersects two postings lists far
// faster than SQLite can rank every message containing "deploy" and then throw
// away the ones from other channels — and since the tokenizer's idea of a
// name is looser than the column's, the exact comparison is still what decides.
func (d *DB) searchByText(ctx context.Context, match string, f Filters, limit int) ([]Hit, error) {
	if q := columnMatch("buffer_folded", f.Buffer); q != "" {
		match += " AND " + q
	}
	if q := columnMatch("sender", f.Sender); q != "" {
		match += " AND " + q
	}

	where := []string{`messages_fts MATCH ?`, `m.redacted = 0`}
	args := []any{match}

	// Bound the ranking (see SearchWindow). Skipped when the query names a
	// date range, because there the user is asking about a particular week
	// and the newest matches are the wrong ones to keep.
	if f.Since.IsZero() && f.Until.IsZero() {
		floor, err := d.searchFloor(ctx, match)
		if err != nil {
			return nil, err
		}
		if floor > 0 {
			where = append(where, `messages_fts.rowid >= ?`)
			args = append(args, floor)
		}
	}

	where, args = appendColumnFilters(where, args, f)
	args = append(args, limit)

	rows, err := d.r.QueryContext(ctx, `
SELECT `+messageColumns+`,
       bm25(messages_fts, 10.0, 2.0, 0.5) AS rank,
       snippet(messages_fts, 0, '`+MarkStart+`', '`+MarkEnd+`', '…', 12)
FROM messages_fts JOIN messages m ON m.rowid = messages_fts.rowid
WHERE `+strings.Join(where, " AND ")+`
ORDER BY rank
LIMIT ?`, args...)
	if err != nil {
		if isFTSSyntaxError(err) {
			// The grammar quotes everything it emits, so this means a term
			// the tokenizer cannot use at all. No hits is the truthful
			// answer; an error would only make the UI shout at the user.
			return nil, nil
		}
		return nil, fmt.Errorf("archive: search: %w", err)
	}
	defer rows.Close()

	var out []Hit
	for rows.Next() {
		var (
			h       Hit
			rank    float64
			snippet sql.NullString
		)
		m, err := scanMessage(rowScanner{rows: rows, extra: []any{&rank, &snippet}})
		if err != nil {
			return nil, fmt.Errorf("archive: search: %w", err)
		}
		h.Message = m
		h.Rank = rank
		h.Snippet = snippet.String
		out = append(out, h)
	}
	return out, rows.Err()
}

// SearchWindow is how many matches a query ranks. Ranking is the expensive
// half of a search: bm25 has to score every matching message before it knows
// which hundred to return, so a word that appears in a million messages costs
// a million scores — measured at 2.3s on a 1M-row archive, which is not a
// search box, it is a progress bar.
//
// So a search that matches more than this ranks the newest SearchWindow
// matches and says so here rather than in a footnote. The trade is honest: a
// word rare enough to be worth searching for matches far fewer than 20,000
// messages, and for a word that does not — "the" — ranking the whole archive
// by relevance was never going to produce a useful answer anyway. The same
// query at 1M rows costs 60ms with the window instead of 2.3s without it.
//
// Anchoring on rowid rather than on ts is deliberate: rowid is the order the
// archive learned things, it is what the FTS index is sorted by, and it is
// therefore the only bound SQLite can apply without reading the rows.
const SearchWindow = 20000

// searchWindow is the window in force for this archive (Options.SearchWindow,
// or the package default).
func (d *DB) searchWindow() int {
	if d.opts.SearchWindow > 0 {
		return d.opts.SearchWindow
	}
	return SearchWindow
}

// searchFloor returns the rowid of the window-th newest match, or 0 when there
// are fewer matches than that. It walks the postings list backwards and stops,
// so it costs the window rather than the archive.
func (d *DB) searchFloor(ctx context.Context, match string) (int64, error) {
	var rowid int64
	err := d.r.QueryRowContext(ctx, `
SELECT rowid FROM messages_fts WHERE messages_fts MATCH ?
ORDER BY rowid DESC LIMIT 1 OFFSET ?`, match, d.searchWindow()-1).Scan(&rowid)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		if isFTSSyntaxError(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("archive: search: %w", err)
	}
	return rowid, nil
}

// searchByFilters is the no-terms path ("everything from:alice in:#eco").
// There is nothing to rank, so the answer is the newest matches — which is the
// useful order for that question, and the one the (buffer_folded, ts, rowid)
// index already produces when a buffer is named.
//
// A sender filter still goes through the FTS index to pick the candidate rows,
// because messages has no index on sender and 1M rows is a long scan. The
// exact comparison on the column then decides, so the tokenizer's looser idea
// of a word only ever costs a few extra candidates.
func (d *DB) searchByFilters(ctx context.Context, f Filters, limit int) ([]Hit, error) {
	if f.isZero() {
		return nil, nil
	}
	where := []string{`m.redacted = 0`}
	var args []any
	if f.Sender != "" {
		if match := columnMatch("sender", f.Sender); match != "" {
			where = append(where, `m.rowid IN (SELECT rowid FROM messages_fts WHERE messages_fts MATCH ?)`)
			args = append(args, match)
		}
	}
	where, args = appendColumnFilters(where, args, f)
	args = append(args, limit)

	rows, err := d.r.QueryContext(ctx, `
SELECT `+messageColumns+` FROM messages m
WHERE `+strings.Join(where, " AND ")+`
ORDER BY m.ts DESC, m.rowid DESC
LIMIT ?`, args...)
	if err != nil {
		if isFTSSyntaxError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("archive: search: %w", err)
	}
	ms, err := scanMessages(rows)
	if err != nil {
		return nil, fmt.Errorf("archive: search: %w", err)
	}
	out := make([]Hit, 0, len(ms))
	for _, m := range ms {
		out = append(out, Hit{Message: m})
	}
	return out, nil
}

// appendColumnFilters adds the non-text filters. Sender is compared on the
// column even when it also narrowed the FTS match, because the tokenizer's
// idea of a word is looser than a nick: sender:bob would also match "bob_" in
// a body-less world, and the exact comparison is what makes from: mean from.
func appendColumnFilters(where []string, args []any, f Filters) ([]string, []any) {
	if f.Buffer != "" {
		where = append(where, `m.buffer_folded = ?`)
		args = append(args, f.Buffer)
	}
	if f.Sender != "" {
		where = append(where, `m.sender = ? COLLATE NOCASE`)
		args = append(args, f.Sender)
	}
	if !f.Since.IsZero() {
		where = append(where, `m.ts >= ?`)
		args = append(args, f.Since.UnixMilli())
	}
	if !f.Until.IsZero() {
		where = append(where, `m.ts < ?`)
		args = append(args, f.Until.UnixMilli())
	}
	return where, args
}

// rowScanner lets scanMessage read a row that carries extra trailing columns.
type rowScanner struct {
	rows  *sql.Rows
	extra []any
}

func (r rowScanner) Scan(dest ...any) error {
	return r.rows.Scan(append(dest, r.extra...)...)
}

func isFTSSyntaxError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "fts5: syntax error") || strings.Contains(s, "no such column")
}

// mergeFilters ANDs two filter sets, with the typed one (b) winning where both
// speak.
func mergeFilters(a, b Filters) Filters {
	if b.Buffer != "" {
		a.Buffer = b.Buffer
	}
	if b.Sender != "" {
		a.Sender = b.Sender
	}
	if !b.Since.IsZero() {
		a.Since = b.Since
	}
	if !b.Until.IsZero() {
		a.Until = b.Until
	}
	return a
}

// --- the grammar -------------------------------------------------------------

// ParseQuery splits a user's query into search terms and the filters they
// typed. It never fails: anything it does not recognise is a search term, so
// "before:lunch" searches for the word "lunch" rather than complaining. See
// Search for the whole grammar.
//
// Dates parse in UTC. A caller that wants the user's day boundaries can parse
// with its own location and pass Filters instead.
func ParseQuery(q string) (terms []string, f Filters) {
	for _, tok := range splitQuery(q) {
		if tok.quoted {
			terms = append(terms, tok.text)
			continue
		}
		key, val, ok := strings.Cut(tok.text, ":")
		if !ok || val == "" {
			terms = append(terms, tok.text)
			continue
		}
		switch strings.ToLower(key) {
		case "from", "nick":
			f.Sender = val
		case "in", "buffer", "channel":
			f.Buffer = val
		case "before", "until":
			if t, err := parseDate(val); err == nil {
				f.Until = t
				continue
			}
			terms = append(terms, tok.text)
		case "after", "since":
			if t, err := parseDate(val); err == nil {
				f.Since = t
				continue
			}
			terms = append(terms, tok.text)
		case "on", "date":
			if t, err := parseDate(val); err == nil {
				f.Since = t
				f.Until = t.AddDate(0, 0, 1)
				continue
			}
			terms = append(terms, tok.text)
		default:
			terms = append(terms, tok.text)
		}
	}
	return terms, f
}

func parseDate(s string) (time.Time, error) {
	return time.ParseInLocation("2006-01-02", s, time.UTC)
}

type token struct {
	text   string
	quoted bool
}

// splitQuery splits on whitespace, keeping "quoted phrases" together. An
// unterminated quote runs to the end of the query, because the user is still
// typing and half a phrase is a better guess than none.
func splitQuery(q string) []token {
	var out []token
	var cur strings.Builder
	inQuote := false
	flush := func(quoted bool) {
		if cur.Len() > 0 {
			out = append(out, token{text: cur.String(), quoted: quoted})
			cur.Reset()
		}
	}
	for _, r := range q {
		switch {
		case r == '"':
			flush(inQuote)
			inQuote = !inQuote
		case unicode.IsSpace(r) && !inQuote:
			flush(false)
		default:
			cur.WriteRune(r)
		}
	}
	flush(inQuote)
	return out
}

// ftsMatch builds the FTS5 MATCH expression for the search terms. Everything
// is quoted, so no user text can reach FTS5 as an operator: "AND", "OR",
// "NEAR(a b)" and a stray column filter are all just words to search for. A
// trailing * survives as the prefix operator, because that one is worth
// having. The result is "" when there is nothing tokenizable to search for.
func ftsMatch(terms []string) string {
	var parts []string
	for _, t := range terms {
		if q := quoteFTS(t); q != "" {
			parts = append(parts, q)
		}
	}
	return strings.Join(parts, " AND ")
}

// columnMatch is an FTS column filter that narrows a search to candidate rows.
// "" means the value has nothing the tokenizer can index (a nick of pure
// punctuation, say), and the caller falls back to the column comparison alone.
func columnMatch(column, value string) string {
	if value == "" {
		return ""
	}
	if q := quoteFTS(value); q != "" {
		return column + " : " + q
	}
	return ""
}

// quoteFTS renders one term as an FTS5 string literal, keeping a trailing *.
// A term with nothing tokenizable in it (punctuation only) returns "": FTS5
// would reject the empty phrase, and there is nothing to search for anyway.
func quoteFTS(t string) string {
	prefix := false
	if strings.HasSuffix(t, "*") {
		t = strings.TrimSuffix(t, "*")
		prefix = true
	}
	if !hasTokenizable(t) {
		return ""
	}
	q := `"` + strings.ReplaceAll(t, `"`, `""`) + `"`
	if prefix {
		q += "*"
	}
	return q
}

// hasTokenizable reports whether unicode61 would find a token here. It treats
// letters and digits as token characters, which is what unicode61 does with
// its default categories.
func hasTokenizable(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}
