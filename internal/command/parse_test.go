package command

import (
	"reflect"
	"testing"
)

// TestParseClassifies pins what a line of composer text IS before anything is
// done about it: a message, a command, or a verb nothing claims.
func TestParseClassifies(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		kind Kind
		verb string // KindCommand/KindUnknown
		body string // KindMessage
	}{
		{name: "empty", text: "", kind: KindEmpty},
		{name: "a bare newline is empty too", text: "\n", kind: KindEmpty},
		{name: "plain text", text: "hello everyone", kind: KindMessage, body: "hello everyone"},
		{name: "text keeps its inner newlines", text: "one\ntwo", kind: KindMessage, body: "one\ntwo"},
		{name: "a trailing newline is dropped", text: "hello\n", kind: KindMessage, body: "hello"},
		{name: "text that only looks like a path", text: "cd /usr/bin", kind: KindMessage,
			body: "cd /usr/bin"},
		{name: "the // escape eats exactly one slash", text: "//me waves",
			kind: KindMessage, body: "/me waves"},
		{name: "three slashes leave two", text: "///slashes", kind: KindMessage, body: "//slashes"},
		{name: "the escape works for text that is not a command", text: "//hello",
			kind: KindMessage, body: "/hello"},
		{name: "a command", text: "/join #eco", kind: KindCommand, verb: "join"},
		{name: "a command in capitals", text: "/JOIN #eco", kind: KindCommand, verb: "join"},
		{name: "an alias", text: "/q bob", kind: KindCommand, verb: "q"},
		{name: "a command with no arguments", text: "/list", kind: KindCommand, verb: "list"},
		{name: "an unknown verb", text: "/jion #eco", kind: KindUnknown, verb: "jion"},
		{name: "a lone slash", text: "/", kind: KindUnknown, verb: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := Parse(tc.text, inChannel)
			if in.Kind != tc.kind {
				t.Fatalf("Parse(%q).Kind = %v, want %v", tc.text, in.Kind, tc.kind)
			}
			if in.Verb != tc.verb {
				t.Errorf("Parse(%q).Verb = %q, want %q", tc.text, in.Verb, tc.verb)
			}
			if in.Text != tc.body {
				t.Errorf("Parse(%q).Text = %q, want %q", tc.text, in.Text, tc.body)
			}
			if (in.Cmd != nil) != (tc.kind == KindCommand) {
				t.Errorf("Parse(%q).Cmd = %v, want a command: %v", tc.text, in.Cmd,
					tc.kind == KindCommand)
			}
			if in.Ctx != inChannel {
				t.Errorf("Parse(%q) lost its context: %+v", tc.text, in.Ctx)
			}
		})
	}
}

// TestParseArgumentShapes pins the table's argument specs: what each command
// gets handed. The tail of a line is never re-spaced, re-quoted or otherwise
// tidied — for /msg and /kick that tail is a message, and its spaces are the
// user's.
func TestParseArgumentShapes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		text   string
		raw    string
		fields []string
		rest   string
		n      int
	}{
		{name: "target plus the rest", text: "/msg bob hello there",
			raw: "bob hello there", fields: []string{"bob"}, rest: "hello there", n: 2},
		{name: "the rest keeps the user's spacing", text: "/msg bob   hello  there",
			raw: "bob   hello  there", fields: []string{"bob"}, rest: "  hello  there", n: 2},
		{name: "leading and trailing space around the arguments goes",
			text: "/msg    bob hi   ", raw: "bob hi", fields: []string{"bob"}, rest: "hi", n: 2},
		{name: "target alone", text: "/msg bob", raw: "bob", fields: []string{"bob"}, n: 1},
		{name: "no arguments at all", text: "/msg", n: 0},
		{name: "every word split", text: "/mode #eco +o alice",
			raw: "#eco +o alice", fields: []string{"#eco", "+o", "alice"}, n: 3},
		{name: "channel and key", text: "/join #eco sekrit",
			raw: "#eco sekrit", fields: []string{"#eco"}, rest: "sekrit", n: 1 + 1},
		{name: "the whole argument as one", text: "/me waves at everyone",
			raw: "waves at everyone", n: 1},
		{name: "quotes are characters, not syntax", text: `/msg bob "hello world"`,
			raw: `bob "hello world"`, fields: []string{"bob"}, rest: `"hello world"`, n: 2},
		{name: "an apostrophe is a character too", text: `/me doesn't split`,
			raw: `doesn't split`, n: 1},
		{name: "a slash inside an argument is left alone", text: "/topic see http://eco/",
			raw: "see http://eco/", n: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := Parse(tc.text, inChannel)
			if in.Kind != KindCommand {
				t.Fatalf("Parse(%q) is not a command", tc.text)
			}
			if in.Args.Raw != tc.raw {
				t.Errorf("Raw = %q, want %q", in.Args.Raw, tc.raw)
			}
			if !reflect.DeepEqual(in.Args.Fields, tc.fields) {
				t.Errorf("Fields = %q, want %q", in.Args.Fields, tc.fields)
			}
			if in.Args.Rest != tc.rest {
				t.Errorf("Rest = %q, want %q", in.Args.Rest, tc.rest)
			}
			if in.Args.N != tc.n {
				t.Errorf("N = %d, want %d", in.Args.N, tc.n)
			}
		})
	}
}

// TestArgsFieldIsSafe: a Run may read up to its spec's Min, and reading past it
// must not panic — that is what lets the entries be one-liners.
func TestArgsFieldIsSafe(t *testing.T) {
	a := Parse("/msg bob", inChannel).Args
	if got := a.Field(0); got != "bob" {
		t.Errorf("Field(0) = %q, want bob", got)
	}
	for _, i := range []int{-1, 1, 99} {
		if got := a.Field(i); got != "" {
			t.Errorf("Field(%d) = %q, want empty", i, got)
		}
	}
}

// TestParseIsIndependentOfTheContext: the same text parses the same way
// wherever it was typed. Only Run may care where it is.
func TestParseIsIndependentOfTheContext(t *testing.T) {
	for _, ctx := range []Context{inChannel, inQuery, inServer, {}} {
		in := Parse("/kick bob rude", ctx)
		if in.Verb != "kick" || in.Args.Field(0) != "bob" || in.Args.Rest != "rude" {
			t.Fatalf("in %+v the parse changed: %+v", ctx, in.Args)
		}
	}
}

// TestContextIsChannel is the question NeedsChannel is answered with.
func TestContextIsChannel(t *testing.T) {
	for _, tc := range []struct {
		ctx  Context
		want bool
	}{
		{Context{Buffer: "#eco"}, true},
		{Context{Buffer: "alice"}, false},
		{Context{Buffer: "server", Server: true}, false},
		{Context{Buffer: "#eco", Server: true}, false}, // the log wins, whatever it is called
		{Context{}, false},
	} {
		if got := tc.ctx.IsChannel(); got != tc.want {
			t.Errorf("%+v.IsChannel() = %v, want %v", tc.ctx, got, tc.want)
		}
	}
}
