package command

import (
	"reflect"
	"strings"
	"testing"
)

// TestUnknownCommandIsNotSentToTheServer is the M10 behaviour change, and the
// one a user sees. v1's default branch upper-cased whatever was typed and wrote
// it to the wire; a typo therefore went to the server, which answered with a
// numeric nobody reads — or, for a verb that happened to exist, did something
// the user never asked for.
func TestUnknownCommandIsNotSentToTheServer(t *testing.T) {
	env := fakeEnv{}
	Run(Parse("/jion #eco", inChannel), &env)

	if len(env.log) != 1 || !strings.HasPrefix(env.log[0], "hint ") {
		t.Fatalf("/jion did %q, want exactly one hint and nothing on the wire", env.log)
	}
	hint := env.log[0]
	for _, want := range []string{"Unknown command /jion.", "Did you mean /join?", "/raw JION #eco"} {
		if !strings.Contains(hint, want) {
			t.Errorf("hint %q does not contain %q", hint, want)
		}
	}
}

// TestUnknownCommandOffersAPassthroughThatWorks: the intercept names /raw, and
// taking it up sends exactly the line v1 would have sent. The escape hatch is
// the whole reason intercepting is safe.
func TestUnknownCommandOffersAPassthroughThatWorks(t *testing.T) {
	in := Parse("/mymode #eco +Z", inChannel)
	if in.Kind != KindUnknown {
		t.Fatalf("/mymode parsed as %v, want unknown", in.Kind)
	}
	if got, want := in.Passthrough(), "MYMODE #eco +Z"; got != want {
		t.Fatalf("Passthrough() = %q, want %q", got, want)
	}
	env := fakeEnv{}
	Run(Parse("/raw "+in.Passthrough(), inChannel), &env)
	if want := []string{`RAW "MYMODE #eco +Z"`}; !reflect.DeepEqual(env.log, want) {
		t.Errorf("taking the offer did %q, want %q", env.log, want)
	}
}

// TestSuggestions pins the guess. A wrong suggestion reads as a correction, so
// "nothing close" has to be a real answer.
func TestSuggestions(t *testing.T) {
	for _, tc := range []struct{ typed, want string }{
		{"jion", "join"},   // two letters transposed
		{"joing", "join"},  // one too many
		{"jon", "join"},    // one missing
		{"prat", "part"},   // transposed again
		{"msgg", "msg"},    //
		{"nickk", "nick"},  //
		{"wh", "who"},      // an abbreviation is not a typo
		{"qu", "quit"},     // several verbs start with it: the shortest wins
		{"whoi", "whois"},  //
		{"topik", "topic"}, //
		{"zzzzz", ""},      // nothing is close
		{"x", ""},          // too short to guess from
		{"", ""},           // a lone slash suggests nothing
	} {
		if got := Suggest(tc.typed); got != tc.want {
			t.Errorf("Suggest(%q) = %q, want %q", tc.typed, got, tc.want)
		}
	}
}

// TestSuggestionsAreStable: the same input suggests the same verb every time,
// whatever order the table's map happens to iterate in.
func TestSuggestionsAreStable(t *testing.T) {
	for _, typed := range []string{"jion", "qu", "wh", "cs1", "hopp"} {
		first := Suggest(typed)
		for i := 0; i < 50; i++ {
			if got := Suggest(typed); got != first {
				t.Fatalf("Suggest(%q) = %q then %q: the guess is not deterministic",
					typed, first, got)
			}
		}
	}
}

// TestUnknownWithNoSuggestion still tells the user what to do.
func TestUnknownWithNoSuggestion(t *testing.T) {
	env := fakeEnv{}
	Run(Parse("/zzzzz arg", inChannel), &env)
	if len(env.log) != 1 {
		t.Fatalf("/zzzzz did %q, want one hint", env.log)
	}
	hint := env.log[0]
	if strings.Contains(hint, "Did you mean") {
		t.Errorf("hint %q guessed at a command when nothing was close", hint)
	}
	for _, want := range []string{"Unknown command /zzzzz.", "/help", "/raw ZZZZZ arg"} {
		if !strings.Contains(hint, want) {
			t.Errorf("hint %q does not contain %q", hint, want)
		}
	}
}

// TestALoneSlashExplainsTheEscape: "/" is not a command and never was, and the
// answer to it is the one thing a user in that state needs to know.
func TestALoneSlashExplainsTheEscape(t *testing.T) {
	env := fakeEnv{}
	Run(Parse("/", inChannel), &env)
	if len(env.log) != 1 || !strings.Contains(env.log[0], "//") {
		t.Errorf("a lone slash did %q, want a hint that mentions the // escape", env.log)
	}
}

// TestKnownCommandsAreNeverIntercepted: every verb and alias in the table
// parses as a command, not as an unknown. This is what stops the intercept from
// eating a command that exists.
func TestKnownCommandsAreNeverIntercepted(t *testing.T) {
	for _, v := range Verbs() {
		if in := Parse(v, inChannel); in.Kind != KindCommand {
			t.Errorf("%s parsed as %v, want a command", v, in.Kind)
		}
		// and in capitals, which is how a user's phone types it
		if in := Parse(strings.ToUpper(v), inChannel); in.Kind != KindCommand {
			t.Errorf("%s parsed as %v, want a command", strings.ToUpper(v), in.Kind)
		}
	}
}
