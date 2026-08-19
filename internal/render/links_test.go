package render

import (
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/toprakkilic/eco/internal/model"
)

// The M11(b) additions to the frozen engine: a body without the columns the
// Timeline draws itself, the art row as a widget, and the URL half of
// linkification - a v1 gap. None of them may change what Segments produces,
// which is what the golden suite guards; these tests guard the rest.

func plainLine(text string) model.Line {
	return model.Line{Time: time.Date(2025, 6, 1, 15, 4, 0, 0, time.UTC), Nick: "alice", Text: text}
}

// linkedText returns the text of every hyperlink segment, in order.
func linkedText(segs []widget.RichTextSegment) []string {
	var out []string
	for _, s := range segs {
		if h, ok := s.(*widget.HyperlinkSegment); ok {
			out = append(out, h.Text)
		}
	}
	return out
}

func segText(segs []widget.RichTextSegment) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteString(s.Textual())
	}
	return b.String()
}

// TestURLsAreOnlyLinkedWhenAsked keeps the frozen output frozen: with no URL
// resolver - which is every pre-M11 caller and the whole golden corpus - a
// link is text, exactly as it always was.
func TestURLsAreOnlyLinkedWhenAsked(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	l := plainLine("see https://example.com/x for details")
	if got := linkedText(Segments(l, Options{})); len(got) != 0 {
		t.Errorf("links %v with no resolver, want none", got)
	}
	opts := Options{URL: func(string) func() { return func() {} }}
	if got := linkedText(Segments(l, opts)); len(got) != 1 || got[0] != "https://example.com/x" {
		t.Errorf("links %v, want the one URL", got)
	}
}

func TestURLBoundaries(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	opts := Options{URL: func(string) func() { return func() {} }}
	cases := []struct {
		text string
		want []string
	}{
		{"https://example.com", []string{"https://example.com"}},
		{"http://example.com/a_b-c?d=1&e=2#f", []string{"http://example.com/a_b-c?d=1&e=2#f"}},
		{"see https://eco.chat/faq.", []string{"https://eco.chat/faq"}},
		{"(https://eco.chat/faq)", []string{"https://eco.chat/faq"}},
		{"https://en.wikipedia.org/wiki/Foo_(bar)", []string{"https://en.wikipedia.org/wiki/Foo_(bar)"}},
		{"<https://eco.chat/faq>", []string{"https://eco.chat/faq"}},
		{"two https://a.example and https://b.example here", []string{"https://a.example", "https://b.example"}},
		{"https://user@host.example/p", []string{"https://user@host.example/p"}},
		{"notreallyhttps://example.com", nil},
		{"https://", nil},
		{"ftp://example.com/file", nil},
		{"javascript://alert", nil},
		{"file:///etc/passwd", nil},
		{"no link at all", nil},
	}
	for _, c := range cases {
		got := linkedText(Body(plainLine(c.text), opts))
		if len(got) != len(c.want) {
			t.Errorf("%q: links %v, want %v", c.text, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%q: links %v, want %v", c.text, got, c.want)
				break
			}
		}
	}
}

// TestURLTapCallsTheResolver: the link is not decoration.
func TestURLTapCallsTheResolver(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	var asked, tapped string
	segs := Body(plainLine("go to https://eco.chat/docs now"), Options{
		URL: func(raw string) func() {
			asked = raw
			return func() { tapped = raw }
		},
	})
	if asked != "https://eco.chat/docs" {
		t.Fatalf("the resolver was asked about %q", asked)
	}
	for _, s := range segs {
		if h, ok := s.(*widget.HyperlinkSegment); ok {
			h.OnTapped()
		}
	}
	if tapped != "https://eco.chat/docs" {
		t.Errorf("tapping the link opened %q", tapped)
	}
}

// TestMentionsAndURLsCoexist: a nick and a link in one sentence both work,
// and a URL containing an @ is not carved up by the mention scanner.
func TestMentionsAndURLsCoexist(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	opts := Options{
		Mention: func(nick string) func() {
			if nick == "bob" {
				return func() {}
			}
			return nil
		},
		URL: func(string) func() { return func() {} },
	}
	segs := Body(plainLine("@bob see https://x.example/a@b and @nobody"), opts)
	got := linkedText(segs)
	want := []string{"@bob", "https://x.example/a@b"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("links %v, want %v", got, want)
	}
	if txt := segText(segs); txt != "@bob see https://x.example/a@b and @nobody" {
		t.Errorf("the text came out as %q", txt)
	}
}

// TestBodyIsSegmentsWithoutTheColumns: Body drops the gutter and the nick,
// and changes nothing else about how the text reads.
func TestBodyIsSegmentsWithoutTheColumns(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	opts := Options{ShowTimestamps: true, TimeLayout: "15:04", ColorfulNicks: true, SelfColor: "irc3"}
	for _, l := range []model.Line{
		plainLine("plain words"),
		{Time: plainLine("").Time, Nick: "alice", Text: "waves", Action: true},
		{Time: plainLine("").Time, Nick: "serv", Text: "a notice", Notice: true},
		{Time: plainLine("").Time, Text: "bob joined", Status: true},
		{Time: plainLine("").Time, Nick: "bob", Text: "hi \x0304red\x03", Highlight: true},
	} {
		full := segText(Segments(l, opts))
		body := segText(Body(l, opts))
		if !strings.HasSuffix(full, body) {
			t.Errorf("Segments(%q) = %q does not end with Body = %q", l.Text, full, body)
		}
		if body == "" {
			t.Errorf("Body(%q) is empty", l.Text)
		}
	}
}

// TestBodyKeepsTheHighlightColour: the one thing the body still decides.
func TestBodyKeepsTheHighlightColour(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	hot := Body(model.Line{Nick: "bob", Text: "your nick", Highlight: true}, Options{})
	cold := Body(model.Line{Nick: "bob", Text: "your nick"}, Options{})
	name := func(segs []widget.RichTextSegment) fyne.ThemeColorName {
		return segs[0].(*widget.TextSegment).Style.ColorName
	}
	if name(hot) != theme.ColorNameWarning {
		t.Errorf("a highlighted body is %v, want warning", name(hot))
	}
	if name(cold) != theme.ColorNameForeground {
		t.Errorf("an ordinary body is %v, want foreground", name(cold))
	}
}

// TestArtIsItsOwnRow: background art comes back as a widget and leaves no
// body behind, which is how the Timeline knows it needs a grid row.
func TestArtIsItsOwnRow(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	opts := Options{ShowTimestamps: true, TimeLayout: "15:04"}
	art := plainLine("\x0300,01" + strings.Repeat("█", 40))
	obj, ok := Art(art, opts)
	if !ok || obj == nil {
		t.Fatal("background art did not come back as an art row")
	}
	if obj.MinSize().Width <= 0 || obj.MinSize().Height <= 0 {
		t.Errorf("the art row measures %v", obj.MinSize())
	}
	if body := Body(art, opts); body != nil {
		t.Errorf("an art line also produced a body of %d segments", len(body))
	}
	if _, ok := Art(plainLine("just words"), opts); ok {
		t.Error("ordinary prose was mistaken for art")
	}
	if body := Body(plainLine("just words"), opts); body == nil {
		t.Error("ordinary prose produced no body")
	}
}

// TestNickColour is the one decision the Timeline borrows rather than
// re-making: whose colour a nick is written in.
func TestNickColour(t *testing.T) {
	self := model.Line{Nick: "me", Self: true}
	other := model.Line{Nick: "alice"}
	opts := Options{ColorfulNicks: true, SelfColor: "nickself"}
	if got := NickColor(self, opts); got != "nickself" {
		t.Errorf("own nick colour %v, want nickself", got)
	}
	if got, want := NickColor(other, opts), NickColorName("alice"); got != want {
		t.Errorf("alice's colour %v, want %v", got, want)
	}
	if got := NickColor(other, Options{}); got != theme.ColorNamePrimary {
		t.Errorf("with colourful nicks off the colour is %v, want the accent", got)
	}
}

// TestPlainText is what Copy puts on the clipboard.
func TestPlainText(t *testing.T) {
	if got := PlainText(plainLine("\x0304red\x03 and \x02bold\x02")); got != "red and bold" {
		t.Errorf("PlainText = %q", got)
	}
}
