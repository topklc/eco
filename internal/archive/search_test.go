package archive

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/model"
)

// searchCorpus is the conversation every search test asks questions of.
func searchCorpus(t *testing.T, db *DB) {
	t.Helper()
	r := newRecorder(t, db, RecorderOptions{})
	record(t, r,
		msg("#eco", "s1", at(1), "alice", "the deploy is green"),
		msg("#eco", "s2", at(2), "bob", "did the deploy finish?"),
		msg("#eco", "s3", at(3), "Alice", "deployment notes are in the wiki"),
		msg("#eco", "s4", at(4), "carol", "lunch?"),
		msg("#ops", "s5", at(5), "alice", "the deploy broke staging"),
		msg("#ops", "s6", at(6), "dave", "café crème"),
		msg("bob", "s7", at(7), "bob", "private word about the deploy"),
	)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestSearchFindsAndRanks pins the headline behaviour: full-text search across
// the archive, best match first, with enough on each hit to jump to it.
func TestSearchFindsAndRanks(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)
	searchCorpus(t, db)

	hits, err := db.Search(ctx, "deploy", Filters{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 4 {
		t.Fatalf("searching for deploy found %v, want the four messages that say it", hitBodies(hits))
	}
	for i := 1; i < len(hits); i++ {
		if hits[i-1].Rank > hits[i].Rank {
			t.Errorf("hits are not ranked best-first: %v", hits)
		}
	}
	for _, h := range hits {
		if h.RowID == 0 || h.Buffer == "" || h.Time.IsZero() {
			t.Errorf("hit %+v cannot be jumped to", h)
		}
		if !strings.Contains(h.Snippet, MarkStart) {
			t.Errorf("hit %q has no marked snippet: %q", h.Body, h.Snippet)
		}
	}

	// A hit is a place in the timeline: Around turns it into context.
	ctxRows, err := db.Around(ctx, hits[0].RowID, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(ctxRows) == 0 {
		t.Error("a search hit's rowid gave no context")
	}
}

// TestSearchIsCaseAndAccentInsensitive pins the tokenizer choice: people do
// not type accents or capitals when they are looking for something.
func TestSearchIsCaseAndAccentInsensitive(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)
	searchCorpus(t, db)

	for _, q := range []string{"GREEN", "green", "Green"} {
		hits, err := db.Search(ctx, q, Filters{}, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) != 1 {
			t.Errorf("searching %q found %v, want the one green line", q, hitBodies(hits))
		}
	}
	for _, q := range []string{"cafe", "café", "CAFE", "creme"} {
		hits, err := db.Search(ctx, q, Filters{}, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) != 1 {
			t.Errorf("searching %q found %v, want the café line", q, hitBodies(hits))
		}
	}
}

// TestSearchFilters pins the filters, both as arguments and as typed grammar.
func TestSearchFilters(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)
	searchCorpus(t, db)

	cases := []struct {
		name  string
		query string
		f     Filters
		want  []string
	}{
		{
			name: "buffer", query: "deploy", f: Filters{Buffer: "#eco"},
			want: []string{"the deploy is green", "did the deploy finish?"},
		},
		{
			name: "buffer typed as in:", query: "deploy in:#eco",
			want: []string{"the deploy is green", "did the deploy finish?"},
		},
		{
			name: "sender, case-insensitively", query: "deploy", f: Filters{Sender: "ALICE"},
			want: []string{"the deploy is green", "the deploy broke staging"},
		},
		{
			name: "sender typed as from:", query: "deploy from:alice",
			want: []string{"the deploy is green", "the deploy broke staging"},
		},
		{
			name: "sender and buffer together", query: "from:alice in:#ops deploy",
			want: []string{"the deploy broke staging"},
		},
		{
			name: "since", query: "deploy", f: Filters{Since: at(5)},
			want: []string{"the deploy broke staging", "private word about the deploy"},
		},
		{
			name: "until", query: "deploy", f: Filters{Until: at(3)},
			want: []string{"the deploy is green", "did the deploy finish?"},
		},
		{
			name: "no such sender", query: "deploy from:nobody", want: nil,
		},
		{
			name: "filters only, newest first", query: "from:alice in:#eco", want: []string{"deployment notes are in the wiki", "the deploy is green"},
		},
		{
			name: "phrase", query: `"deploy is"`, want: []string{"the deploy is green"},
		},
		{
			name: "prefix", query: "deploy*",
			want: []string{"the deploy is green", "did the deploy finish?", "deployment notes are in the wiki", "the deploy broke staging", "private word about the deploy"},
		},
		{
			name: "two words are ANDed", query: "deploy staging", want: []string{"the deploy broke staging"},
		},
		{
			name: "a word nobody said", query: "kubernetes", want: nil,
		},
		{
			// If AND were an operator this would find the staging line. It
			// is a word, so the query asks for a message containing all
			// three, and nobody said "and".
			name: "AND is a word, not an operator", query: `deploy AND staging`, want: nil,
		},
		{
			name: "the same query without the operator-shaped word", query: `deploy staging`,
			want: []string{"the deploy broke staging"},
		},
		{
			name: "a query with nothing in it finds nothing", query: "   ", want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hits, err := db.Search(ctx, tc.query, tc.f, 50)
			if err != nil {
				t.Fatal(err)
			}
			got := hitBodies(hits)
			if len(tc.want) == 0 {
				if len(got) != 0 {
					t.Fatalf("Search(%q) = %v, want nothing", tc.query, got)
				}
				return
			}
			if !sameSet(got, tc.want) {
				t.Errorf("Search(%q) = %v, want %v", tc.query, got, tc.want)
			}
		})
	}
}

// sameSet compares hits as a set: ranking order is bm25's business and is
// pinned separately, so a filter test that also pinned the order would be
// testing the ranker by accident.
func sameSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := map[string]int{}
	for _, g := range got {
		seen[g]++
	}
	for _, w := range want {
		seen[w]--
		if seen[w] < 0 {
			return false
		}
	}
	return true
}

// TestSearchOrderForFiltersOnly pins the documented order of a query with
// nothing to rank: newest first, because that is the useful answer to "show me
// everything from alice".
func TestSearchOrderForFiltersOnly(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)
	searchCorpus(t, db)

	hits, err := db.Search(ctx, "from:alice", Filters{}, 50)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"the deploy broke staging", "deployment notes are in the wiki", "the deploy is green"}
	if !equalStrings(hitBodies(hits), want) {
		t.Errorf("Search(from:alice) = %v, want %v", hitBodies(hits), want)
	}
	for _, h := range hits {
		if h.Rank != 0 || h.Snippet != "" {
			t.Errorf("a filters-only hit claims a rank or a snippet: %+v", h)
		}
	}
}

// TestParseQuery is the grammar's table. It is the documentation's test: every
// row here is a line in Search's doc comment.
func TestParseQuery(t *testing.T) {
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
	cases := []struct {
		in    string
		terms []string
		f     Filters
	}{
		{in: "", terms: nil},
		{in: "hello", terms: []string{"hello"}},
		{in: "hello world", terms: []string{"hello", "world"}},
		{in: `"hello world"`, terms: []string{"hello world"}},
		{in: `an "exact phrase" here`, terms: []string{"an", "exact phrase", "here"}},
		{in: `unterminated "quote`, terms: []string{"unterminated", "quote"}},
		{in: "from:alice", f: Filters{Sender: "alice"}},
		{in: "in:#eco", f: Filters{Buffer: "#eco"}},
		{in: "nick:bob buffer:#ops", f: Filters{Sender: "bob", Buffer: "#ops"}},
		{in: "deploy from:alice in:#eco", terms: []string{"deploy"}, f: Filters{Sender: "alice", Buffer: "#eco"}},
		{in: "before:2026-01-31", f: Filters{Until: day(2026, time.January, 31)}},
		{in: "after:2026-01-01", f: Filters{Since: day(2026, time.January, 1)}},
		{in: "since:2026-01-01 until:2026-02-01", f: Filters{Since: day(2026, time.January, 1), Until: day(2026, time.February, 1)}},
		{in: "on:2026-01-15", f: Filters{Since: day(2026, time.January, 15), Until: day(2026, time.January, 16)}},
		// Anything the grammar does not recognise is a search term, not an error.
		{in: "before:lunch", terms: []string{"before:lunch"}},
		{in: "on:tuesday", terms: []string{"on:tuesday"}},
		{in: "http://example.com/x", terms: []string{"http://example.com/x"}},
		{in: "from:", terms: []string{"from:"}},
		{in: `"from:alice"`, terms: []string{"from:alice"}}, // quoted, so it is text
		{in: "FROM:alice", f: Filters{Sender: "alice"}},     // keys are case-insensitive
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			terms, f := ParseQuery(tc.in)
			if !equalStrings(terms, tc.terms) {
				t.Errorf("ParseQuery(%q) terms = %v, want %v", tc.in, terms, tc.terms)
			}
			if f.Buffer != tc.f.Buffer || f.Sender != tc.f.Sender ||
				!f.Since.Equal(tc.f.Since) || !f.Until.Equal(tc.f.Until) {
				t.Errorf("ParseQuery(%q) filters = %+v, want %+v", tc.in, f, tc.f)
			}
		})
	}
}

// TestSearchSurvivesHostileQueries pins that no user input reaches FTS5 as an
// operator: a query full of syntax is a query for those words, not an error
// and not an injection.
func TestSearchSurvivesHostileQueries(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)
	searchCorpus(t, db)

	for _, q := range []string{
		`"`, `""`, `deploy"`, `NEAR(deploy staging)`, `deploy OR lunch`, `deploy NOT green`,
		`*`, `**`, `^deploy`, `{body}:deploy`, `body:deploy`, `(deploy`, `deploy)`,
		`'; DROP TABLE messages; --`, `\`, `~!@#$%^&*()`, "\x00", strings.Repeat("a", 5000),
	} {
		hits, err := db.Search(ctx, q, Filters{}, 10)
		if err != nil {
			t.Errorf("Search(%q) errored: %v", q, err)
			continue
		}
		_ = hits
	}
	// and the table is still there
	if n, err := db.Count(ctx, ""); err != nil || n != 7 {
		t.Errorf("after the hostile queries the archive holds %d rows (%v), want 7", n, err)
	}
}

// TestSearchLimit pins that the limit is honoured and capped.
func TestSearchLimit(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)
	searchCorpus(t, db)

	hits, err := db.Search(ctx, "deploy", Filters{}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Errorf("Search with limit 2 returned %d hits", len(hits))
	}
	if hits, err = db.Search(ctx, "deploy", Filters{}, 0); err != nil || len(hits) != 4 {
		t.Errorf("Search with no limit returned %d hits (%v), want all 4", len(hits), err)
	}
}

// TestSearchRanksAWindowOfTheNewestMatches pins the bound on ranking. A term
// that matches more messages than the window ranks the newest ones; a term
// that does not is ranked exactly, which is the case that matters. The window
// is an Option so this can be shown with ten messages instead of twenty
// thousand.
func TestSearchRanksAWindowOfTheNewestMatches(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "window.db"), Options{
		Clock: model.FixedClock(epoch), SearchWindow: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := testCtx(t)

	r := newRecorder(t, db, RecorderOptions{})
	var ms []Message
	for i := 0; i < 10; i++ {
		ms = append(ms, msg("#eco", fmt.Sprintf("w%02d", i), at(i), "alice", fmt.Sprintf("common word number %d", i)))
	}
	// One rare word, in the oldest message of all.
	ms[0].Body = "zarquon lives here"
	record(t, r, ms...)

	// "common" matches nine messages, more than the window of four, so the
	// answer is the newest four of them.
	hits, err := db.Search(ctx, "common", Filters{}, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 4 {
		t.Fatalf("a windowed search returned %d hits, want the window's 4: %v", len(hits), hitBodies(hits))
	}
	for _, h := range hits {
		if h.Time.Before(at(6)) {
			t.Errorf("a windowed search reached past the newest four: %q at %s", h.Body, h.Time)
		}
	}

	// The rare word is found wherever it is, because its match set fits in
	// the window. This is the case people actually search for.
	hits, err = db.Search(ctx, "zarquon", Filters{}, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Body != "zarquon lives here" {
		t.Errorf("the rare word search returned %v, want the oldest message", hitBodies(hits))
	}

	// A dated query is never windowed: the user is asking about a week, and
	// the newest matches are the wrong ones to keep.
	hits, err = db.Search(ctx, "common", Filters{Until: at(4)}, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 3 {
		t.Errorf("a dated search returned %v, want the three older matches", hitBodies(hits))
	}
}

// TestMergeFilters pins the rule that a typed filter beats a passed one: what
// the user just typed is what the user meant.
func TestMergeFilters(t *testing.T) {
	passed := Filters{Buffer: "#eco", Sender: "alice", Since: at(1), Until: at(9)}
	typed := Filters{Buffer: "#ops", Since: at(5)}
	got := mergeFilters(passed, typed)
	if got.Buffer != "#ops" {
		t.Errorf("buffer = %q, want the typed #ops", got.Buffer)
	}
	if got.Sender != "alice" {
		t.Errorf("sender = %q, want the passed alice to survive", got.Sender)
	}
	if !got.Since.Equal(at(5)) || !got.Until.Equal(at(9)) {
		t.Errorf("dates = %s..%s, want the typed since and the passed until", got.Since, got.Until)
	}
}

// TestFromMeansThatNickAndNotAWordInIt pins the half of the sender filter that
// the FTS index cannot do. from: is pushed into the MATCH expression because
// intersecting two postings lists is far cheaper than ranking every message and
// throwing most of them away — but the tokenizer's idea of a name is looser
// than a nick's: unicode61 splits "bob_smith" into "bob" and "smith", so the
// index alone answers from:bob with bob_smith's messages too. The exact
// comparison on the column is what makes from: mean from, and without a case
// like this one the whole filter can be deleted and the suite stays green.
func TestFromMeansThatNickAndNotAWordInIt(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)
	r := newRecorder(t, db, RecorderOptions{})
	record(t, r,
		msg("#eco", "n1", at(1), "bob", "deploying now"),
		msg("#eco", "n2", at(2), "bob_smith", "deploying too"),
		msg("#eco", "n3", at(3), "BOB", "deploying as well"), // one person, two spellings
		msg("#eco", "n4", at(4), "bob|away", "deploying also"),
	)
	want := []string{"deploying now", "deploying as well"}
	for _, query := range []string{"deploying from:bob", "from:bob"} {
		hits, err := db.Search(ctx, query, Filters{}, 50)
		if err != nil {
			t.Fatal(err)
		}
		if got := hitBodies(hits); !sameSet(got, want) {
			t.Errorf("Search(%q) = %v, want %v — from: is matching a word inside the nick",
				query, got, want)
		}
	}
	// The same through the Filters argument, which is the path the UI uses.
	hits, err := db.Search(ctx, "deploying", Filters{Sender: "bob"}, 50)
	if err != nil {
		t.Fatal(err)
	}
	if got := hitBodies(hits); !sameSet(got, want) {
		t.Errorf("Search with Filters{Sender: \"bob\"} = %v, want %v", got, want)
	}
}
