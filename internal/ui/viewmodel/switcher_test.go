package viewmodel

import (
	"strings"
	"testing"

	"github.com/toprakkilic/eco/internal/ui/dispatch"
)

// The Switcher's ranking, tested where it is decided. Fuzzy order is the kind
// of thing that is "obviously right" until somebody changes a constant and
// nothing anywhere goes red, so every band boundary below is a case: what a
// prefix beats, what recency can and cannot move, and what the list does when
// two rows are equally good (it must still be the same list twice).

// titles renders a ranked list compactly: "#go" for a buffer, "+#go" for a
// channel to join, "*#go" for the create row.
func titles(cs []Candidate) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		switch c.Kind {
		case SwitchChannel:
			out = append(out, "+"+c.Name)
		case SwitchCreate:
			out = append(out, "*"+c.Name)
		default:
			out = append(out, c.Name)
		}
	}
	return out
}

// chans builds LIST-cache candidates for a session, most populated first.
func chans(id dispatch.SessionID, names ...string) []Candidate {
	out := make([]Candidate, 0, len(names))
	for i, n := range names {
		out = append(out, Candidate{Session: id, Name: n, Users: 100 - i})
	}
	return out
}

// The match bands, one case each. The order is a claim about intent, not about
// string distance: what I typed IS this name beats it starts this name beats it
// starts a word in this name beats it is somewhere in this name beats the
// letters are in there somewhere.
func TestMatchBandsAreOrderedByIntent(t *testing.T) {
	for _, c := range []struct {
		name, query string
		want        int
	}{
		{"#eco", "#eco", scoreExact},
		{"#eco-dev", "#eco", scorePrefix},
		{"#eco", "eco", scoreSigilPrefix},
		{"#the-eco-room", "eco", scoreWordStart},
		{"#theecoroom", "eco", scoreSubstring - 4},
		{"#emacs-config-old", "eco", scoreSubsequence - 2},
	} {
		got, ok := matchScore(c.name, c.query)
		if !ok {
			t.Fatalf("%q against %q did not match at all", c.name, c.query)
		}
		if got != c.want {
			t.Errorf("%q against %q scored %d, want %d", c.name, c.query, got, c.want)
		}
	}
	if _, ok := matchScore("#golang", "zzz"); ok {
		t.Error("#golang matched zzz; a subsequence miss must be a miss")
	}
}

// The bands above are asserted against the constants that define them, which
// proves each case lands in the band it was meant for and NOTHING about the
// order of the bands themselves: move scorePrefix onto scoreSubstring and every
// row of that table still passes. So the order is its own test, and it is
// stated as an order rather than as five numbers.
//
// It is asserted twice: on the constants, and on a ranked list, because the
// second is the sentence a user would say and the first is the reason.
func TestTheBandsAreOrderedAndStayApart(t *testing.T) {
	bands := []struct {
		name  string
		score int
	}{
		{"exact", scoreExact},
		{"prefix", scorePrefix},
		{"prefix past the sigil", scoreSigilPrefix},
		{"word start", scoreWordStart},
		{"substring", scoreSubstring},
		{"subsequence", scoreSubsequence},
	}
	for i := 1; i < len(bands); i++ {
		hi, lo := bands[i-1], bands[i]
		if hi.score <= lo.score {
			t.Errorf("%s (%d) does not outrank %s (%d)", hi.name, hi.score, lo.name, lo.score)
		}
		// The gap is the whole reason a bonus cannot lift a worse kind of match
		// over a better one: kindBonus and the recency top must both fit inside
		// it with room to spare.
		if gap := hi.score - lo.score; gap <= kindBonus+recencyTop {
			t.Errorf("%s and %s are %d apart, which a bonus (%d) can cross",
				hi.name, lo.name, gap, kindBonus+recencyTop)
		}
	}

	// and the same claim where a person met it: "eco" typed, an open buffer you
	// were just in where "eco" starts a word, and a channel from the LIST whose
	// name starts with it. Both bonuses at full strength used to make these two
	// score identically, and the kind tiebreak then put the worse match first.
	h := newHarness()
	h.AddSession(sA, "libera")
	h.SetBuffers(sA, []string{"#the-eco-room"})
	h.Select(ref(sA, "#the-eco-room"))
	got := titles(h.Switch("eco", []Candidate{{Session: sA, Name: "#ecosystem", Users: 900}}))
	if len(got) < 2 || got[0] != "+#ecosystem" {
		t.Errorf("ranked %v; a name that STARTS with what was typed comes first, "+
			"however recently the other one was read", got)
	}
}

// A subsequence match with fewer breaks in it is the better guess.
func TestFewerGapsIsABetterFuzzyMatch(t *testing.T) {
	tight, ok1 := matchScore("#messages", "mesg")
	loose, ok2 := matchScore("#my-easy-song", "mesg")
	if !ok1 || !ok2 {
		t.Fatalf("both should match: %v %v", ok1, ok2)
	}
	if tight <= loose {
		t.Errorf("#messages scored %d and #my-easy-song %d; the tighter run must win", tight, loose)
	}
}

// The whole list, ranked: a buffer you are in comes above a channel you are
// not at the same match quality, and the create row is last because it is an
// action rather than a place.
func TestSwitchRanksBuffersChannelsAndCreate(t *testing.T) {
	h := newHarness()
	h.AddSession(sA, "libera")
	h.SetBuffers(sA, []string{ServerBuffer, "#eco", "#eco-dev", "alice"})

	got := titles(h.Switch("eco", []Candidate{
		{Session: sA, Name: "#eco-ci", Users: 40},
		{Session: sA, Name: "#golang", Users: 900},
	}))
	// "eco" names an existing buffer exactly, so there is no create row for
	// it; #golang matches nothing and is not in the list at all
	want := []string{"#eco", "#eco-dev", "+#eco-ci"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Switch(\"eco\") = %v, want %v", got, want)
	}
}

// A channel already open is not offered twice: the buffer and the LIST row that
// named it are one thing to the person typing.
func TestSwitchDropsListRowsForBuffersAlreadyOpen(t *testing.T) {
	h := newHarness()
	h.AddSession(sA, "libera")
	h.SetBuffers(sA, []string{ServerBuffer, "#eco"})

	got := titles(h.Switch("eco", chans(sA, "#eco", "#eco-dev")))
	want := []string{"#eco", "+#eco-dev"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Switch = %v, want %v - #eco is open, so the LIST row is a duplicate", got, want)
	}
}

// Recency orders an empty Ctrl+K, and cannot cross a match band with something
// typed: the buffer read a moment ago does not outrank the one whose name
// actually starts with what was typed.
func TestRecencyOrdersTheEmptyQueryAndNeverCrossesABand(t *testing.T) {
	h := newHarness()
	h.AddSession(sA, "libera")
	h.SetBuffers(sA, []string{ServerBuffer, "#alpha", "#beta", "#gamma"})
	h.Select(ref(sA, "#gamma"))
	h.Select(ref(sA, "#beta"))

	got := titles(h.Switch("", nil))
	if len(got) < 2 || got[0] != "#beta" || got[1] != "#gamma" {
		t.Errorf("empty Ctrl+K = %v, want the two most recent first", got)
	}

	// #gamma was read recently; "al" still belongs to #alpha
	got = titles(h.Switch("al", nil))
	if len(got) == 0 || got[0] != "#alpha" {
		t.Errorf("Switch(%q) = %v, want #alpha first - recency must not cross a band", "al", got)
	}
}

// The same query twice is the same list twice, whatever order the buffers were
// filed in. Ties are broken by kind, then popularity, then name, then network.
func TestRankingIsDeterministic(t *testing.T) {
	h := newHarness()
	h.AddSession(sA, "libera")
	h.AddSession(sB, "ergo")
	h.SetBuffers(sA, []string{"#zeta", "#delta"})
	h.SetBuffers(sB, []string{"#delta"})

	first := titles(h.Switch("delta", chans(sA, "#delta-old", "#delta-new")))
	for i := 0; i < 20; i++ {
		if got := titles(h.Switch("delta", chans(sA, "#delta-old", "#delta-new"))); strings.Join(got, ",") != strings.Join(first, ",") {
			t.Fatalf("run %d gave %v, first run gave %v", i, got, first)
		}
	}
	// the two #delta buffers tie on everything but the network, and the
	// tiebreak is the network name, so ergo comes before libera
	if first[0] != "#delta" || first[1] != "#delta" {
		t.Fatalf("the two exact matches are not first: %v", first)
	}
	// popularity separates the two channels
	if got := first[len(first)-2:]; got[0] != "+#delta-old" || got[1] != "+#delta-new" {
		t.Errorf("channel tiebreak = %v, want the more populated first", got)
	}
}

// Enter on a name nothing answers to offers to create it - with the typed name,
// never the top match. browser.go's rule, and the one thing about it that had
// to survive intact.
func TestCreateRowOffersTheTypedName(t *testing.T) {
	h := newHarness()
	h.AddSession(sA, "libera")
	h.SetBuffers(sA, []string{ServerBuffer, "#developers"})
	h.Select(ref(sA, ServerBuffer))

	got := titles(h.Switch("dev", nil))
	if len(got) == 0 || got[len(got)-1] != "*#dev" {
		t.Errorf("Switch(%q) = %v, want a Create #dev row last", "dev", got)
	}
	if got[0] != "#developers" {
		t.Errorf("Switch(%q) = %v, want the existing channel ranked first", "dev", got)
	}

	// an exact match takes the offer away: there is nothing to create
	if got := titles(h.Switch("#developers", nil)); got[len(got)-1] != "#developers" {
		t.Errorf("Switch(%q) = %v, want no create row", "#developers", got)
	}
}

// ChannelName is what the user meant by what they typed, and it is the same
// answer the app joins with.
func TestChannelName(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"eco", "#eco"},
		{"#eco", "#eco"},
		{"&local", "&local"},
		{"  eco  ", "#eco"},
		{"", ""},
		{"#", ""},
		{"two words", ""},
		{"a,b", ""},
	} {
		if got := ChannelName(c.in); got != c.want {
			t.Errorf("ChannelName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The list is capped: a query matching a whole network is still a panel.
func TestSwitchIsCapped(t *testing.T) {
	h := newHarness()
	h.AddSession(sA, "libera")
	var names []string
	for i := 0; i < 200; i++ {
		names = append(names, "#chan"+string(rune('a'+i%26))+string(rune('a'+i/26)))
	}
	h.SetBuffers(sA, names)
	// The count, and then the constant. Comparing the answer against
	// SwitchLimit alone passes for any value of SwitchLimit, including one that
	// caps nothing at all - so the number the panel was designed around is
	// written down here too, and moving it is a deliberate edit in two places.
	if got := len(h.Switch("chan", nil)); got != SwitchLimit+1 {
		t.Errorf("Switch returned %d rows for 200 matching buffers, want %d plus the create row",
			got, SwitchLimit)
	}
	if SwitchLimit != 60 {
		t.Errorf("SwitchLimit is %d, want the screenful-and-a-bit (60) the panel is sized for", SwitchLimit)
	}
}

// Being in a room is evidence, and it breaks a tie - but it is only evidence.
// A channel whose name answers the query better still comes first, because what
// was typed says more about where the user wants to be than where they have
// been.
func TestBeingInARoomBreaksATieButNotABand(t *testing.T) {
	tie := newHarness()
	tie.AddSession(sA, "libera")
	tie.SetBuffers(sA, []string{"#the-eco-room"})
	if got := titles(tie.Switch("eco", []Candidate{{Session: sA, Name: "#my-eco-notes", Users: 5}})); got[0] != "#the-eco-room" {
		t.Errorf("equal match quality ranked %v; the room you are in should be first", got)
	}

	// inside one band it does move things: a hit two characters later in a
	// buffer you are in beats an earlier one in a channel you are not, because
	// the penalty for a late hit is deliberately small
	within := newHarness()
	within.AddSession(sA, "libera")
	within.SetBuffers(sA, []string{"#chatecosystem"})
	if got := titles(within.Switch("eco", []Candidate{{Session: sA, Name: "#anecochat", Users: 900}})); got[0] != "#chatecosystem" {
		t.Errorf("inside one band the list is %v; the room you are in should be first", got)
	}

	band := newHarness()
	band.AddSession(sA, "libera")
	band.SetBuffers(sA, []string{"#the-eco-room"})
	if got := titles(band.Switch("eco", []Candidate{{Session: sA, Name: "#ecosystem", Users: 5}})); got[0] != "+#ecosystem" {
		t.Errorf("a better match ranked %v; being joined must not cross a band", got)
	}
}
