package testsrv

import "testing"

// The pattern semantics every transcript in the regression suite depends on.
func TestMatch(t *testing.T) {
	cases := []struct {
		pat, line string
		want      bool
	}{
		{"JOIN #eco", "JOIN #eco", true},
		{"JOIN", "JOIN #eco", true},          // prefix
		{"JOIN #eco", "JOIN #economy", true}, // prefix, deliberately
		{"#eco", "JOIN #eco", false},         // anchored at the start
		{"*#eco", "JOIN #eco", true},         // leading * matches anywhere
		{"PRIVMSG #eco :*", "PRIVMSG #eco :hello", true},
		{"PRIVMSG * :hi", "PRIVMSG #eco :hi", true},
		{"PRIVMSG * :hi", "PRIVMSG #eco :bye", false},
		{"CAP REQ", "CAP LS 302", false},
		{"PONG*sync-1", "PONG sync-1", true},
		{"PONG*sync-2", "PONG :sync-2", true},
		{"", "anything", true},
	}
	for _, c := range cases {
		if got := Match(c.pat, c.line); got != c.want {
			t.Errorf("Match(%q, %q) = %v, want %v", c.pat, c.line, got, c.want)
		}
	}
}

func TestParseTranscript(t *testing.T) {
	steps, err := ParseTranscript(`
		# a comment, and a blank line above

		C: JOIN #eco
		S: :eco!eco@test JOIN #eco
	`)
	if err != nil {
		t.Fatalf("ParseTranscript: %v", err)
	}
	want := []Step{
		{Expect: "JOIN #eco"},
		{Send: ":eco!eco@test JOIN #eco"},
	}
	if len(steps) != len(want) {
		t.Fatalf("got %d steps, want %d: %+v", len(steps), len(want), steps)
	}
	for i := range want {
		if steps[i] != want[i] {
			t.Errorf("step %d = %+v, want %+v", i, steps[i], want[i])
		}
	}

	if _, err := ParseTranscript("JOIN #eco"); err == nil {
		t.Error("a line without a C:/S: prefix must be an error")
	}
	if _, err := ParseTranscript("X: JOIN #eco"); err == nil {
		t.Error("an unknown direction must be an error")
	}
}
