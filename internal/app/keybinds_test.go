package app

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
)

// M12(c)'s keyboard, tested at the two places it can go wrong: the table (what
// is bound, what conflicts, what the page says) and the window (what the keys
// actually do).

// --- combos ------------------------------------------------------------------------

// Parsing and rendering are inverses, and the portable modifier resolves to
// this platform's. A combo that does not round-trip is a stored preference that
// reads back as a different key.
func TestComboRoundTrip(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Mod+K", "Ctrl+K"},
		{"Ctrl+K", "Ctrl+K"},
		{"Mod+Shift+F", "Ctrl+Shift+F"},
		{"Shift+Ctrl+F", "Ctrl+Shift+F"}, // canonical order, whatever was typed
		{"Alt+Up", "Alt+Up"},
		{"Mod+PageUp", "Ctrl+PageUp"},
		{"Shift+Return", "Shift+Enter"},
		{"Alt+1", "Alt+1"},
		{"F6", "F6"},
	} {
		c, ok := parseCombo(tc.in)
		if !ok {
			t.Errorf("parseCombo(%q) failed", tc.in)
			continue
		}
		if got := c.String(); got != tc.want {
			t.Errorf("parseCombo(%q).String() = %q, want %q", tc.in, got, tc.want)
		}
		again, ok := parseCombo(c.String())
		if !ok || again != c {
			t.Errorf("%q did not survive a round trip: %v vs %v", tc.in, again, c)
		}
	}
	for _, bad := range []string{"", "+", "Ctrl+", "Frobnicate+K"} {
		if _, ok := parseCombo(bad); ok {
			t.Errorf("parseCombo(%q) succeeded; it is not a combo", bad)
		}
	}
}

// Fyne's own names for two keys are X11's, and a combo has to speak both: the
// canvas wants "Prior", a person wants "PageUp".
func TestComboSpeaksFyneKeyNames(t *testing.T) {
	c, ok := parseCombo("Mod+PageUp")
	if !ok {
		t.Fatal("Mod+PageUp did not parse")
	}
	if c.Key != fyne.KeyPageUp {
		t.Errorf("key = %q, want %q", c.Key, fyne.KeyPageUp)
	}
	if got := c.shortcut(); got.KeyName != fyne.KeyPageUp || got.Modifier != fyne.KeyModifierControl {
		t.Errorf("shortcut = %+v, want Ctrl+Prior", got)
	}
}

// Every row of the table must parse, or it reserves nothing and documents a key
// that does not exist. This is the cheapest guard against a typo in a default.
func TestEveryTableComboParses(t *testing.T) {
	seen := map[string]string{}
	for _, a := range keyActions {
		if a.ID == "" {
			t.Error("a table row has no id")
		}
		if len(a.Combos) == 0 {
			t.Errorf("%s has no combos", a.ID)
		}
		if prev, ok := seen[a.ID]; ok {
			t.Errorf("%s is in the table twice (%s)", a.ID, prev)
		}
		seen[a.ID] = a.Label
		for _, s := range a.Combos {
			if _, ok := parseCombo(s); !ok {
				t.Errorf("%s: %q does not parse", a.ID, s)
			}
		}
	}
}

// The defaults must not collide with each other. This is the table checking
// itself: conflict detection can only protect the user's edits, and a shipped
// collision would be one action silently shadowing another.
func TestTheDefaultsDoNotConflict(t *testing.T) {
	owner := map[combo]string{}
	for _, a := range keyActions {
		for _, c := range parseCombos(a.Combos) {
			if prev, taken := owner[c]; taken {
				t.Errorf("%s and %s both default to %s", prev, a.ID, c)
			}
			owner[c] = a.ID
		}
	}
}

// --- conflicts ----------------------------------------------------------------------

// Live conflict detection, over the whole table: another action's combo, a
// FIXED one (the Alt digits), and a reserved text-box key are all refusals, and
// each names what is in the way. §4.3 asks for exactly these three.
func TestConflictDetection(t *testing.T) {
	u := newTestUI(t)
	p := u.app.Preferences()

	for _, tc := range []struct {
		name    string
		editing string
		combo   string
		want    string // the action id expected to own it, "" for free
	}{
		{"another action's key", "find", "Mod+K", "switcher"},
		{"a fixed Alt digit", "find", "Alt+3", "pins"},
		{"a reserved editing key", "find", "Mod+V", "editing"},
		{"the action's own key is not a conflict", "switcher", "Mod+K", ""},
		{"an unused combo", "find", "Ctrl+Alt+J", ""},
	} {
		c, ok := parseCombo(tc.combo)
		if !ok {
			t.Fatalf("%s: %q did not parse", tc.name, tc.combo)
		}
		got, taken := conflictFor(p, tc.editing, c)
		if (tc.want == "") == taken {
			t.Errorf("%s: conflictFor(%s) taken = %v, want %v", tc.name, tc.combo, taken, tc.want != "")
			continue
		}
		if taken && got.ActionID != tc.want {
			t.Errorf("%s: %s is held by %q, want %q", tc.name, tc.combo, got.ActionID, tc.want)
		}
	}
}

// Conflict detection reads what is BOUND, not what the table defaults to: after
// a rebind, the old combo is free and the new one is taken.
func TestConflictsFollowTheUsersBindings(t *testing.T) {
	u := newTestUI(t)
	p := u.app.Preferences()
	free, _ := parseCombo("Ctrl+Alt+J")
	setCombos(p, "find", []combo{free})

	if _, taken := conflictFor(p, "switcher", free); !taken {
		t.Error("the combo Find was just rebound to is not reported as taken")
	}
	old, _ := parseCombo("Mod+F")
	if _, taken := conflictFor(p, "switcher", old); taken {
		t.Error("Find's old combo is still reported as taken after the rebind")
	}
}

// A combo with no Ctrl/Alt/Super never reaches the app — focus lives in the
// message box, so a bare key is a character someone is trying to type. The
// capture widget refuses it and says why.
func TestBareKeysAreRefused(t *testing.T) {
	for _, tc := range []struct {
		combo   string
		refused bool
	}{
		{"K", true},
		{"Shift+K", true},
		{"Mod+K", false},
		{"Alt+K", false},
		{"Ctrl+Shift+K", false},
	} {
		c, ok := parseCombo(tc.combo)
		if !ok {
			t.Fatalf("%q did not parse", tc.combo)
		}
		if got := comboRefusal(c) != ""; got != tc.refused {
			t.Errorf("%s refused = %v, want %v", tc.combo, got, tc.refused)
		}
	}
	if comboRefusal(combo{Key: desktop.KeyShiftLeft, Mod: fyne.KeyModifierShift}) == "" {
		t.Error("a modifier pressed on its own was accepted as a binding")
	}
}

// --- the capture widget ---------------------------------------------------------------

// The recorder reads the key plus the modifiers QUERIED at that moment — §4.3's
// fix for v1's shiftHeld desync. The test holds Shift down through the seam no
// test driver provides, and the recorded combo has to carry it.
func TestKeyCaptureReadsQueriedModifiers(t *testing.T) {
	u := newTestUI(t)
	u.modifiers = func() fyne.KeyModifier {
		return fyne.KeyModifierControl | fyne.KeyModifierShift
	}
	var got combo
	var okd bool
	cap := newKeyCapture(u.app, combo{}, func(c combo, ok bool) { got, okd = c, ok })
	cap.mods = u.heldModifiers

	cap.Tapped(nil)
	cap.TypedKey(&fyne.KeyEvent{Name: fyne.KeyJ})
	if !okd {
		t.Fatalf("recording Ctrl+Shift+J was refused: %v", got)
	}
	if want := "Ctrl+Shift+J"; got.String() != want {
		t.Errorf("recorded %q, want %q", got.String(), want)
	}
	if cap.recording {
		t.Error("the field is still recording after taking a chord")
	}
}

// A field that is not recording ignores keys, and one that loses focus stops:
// otherwise a stray keystroke rebinds something.
func TestKeyCaptureOnlyRecordsWhenAsked(t *testing.T) {
	u := newTestUI(t)
	fired := 0
	cap := newKeyCapture(u.app, combo{}, func(combo, bool) { fired++ })
	cap.mods = func() fyne.KeyModifier { return fyne.KeyModifierControl }

	cap.TypedKey(&fyne.KeyEvent{Name: fyne.KeyJ})
	if fired != 0 {
		t.Error("a key was recorded without the field being asked to listen")
	}
	cap.Tapped(nil)
	cap.FocusLost()
	cap.TypedKey(&fyne.KeyEvent{Name: fyne.KeyJ})
	if fired != 0 {
		t.Error("a key was recorded after the field lost focus")
	}
}

// Recording Ctrl+K must RECORD Ctrl+K, not open the switcher. The canvas would
// rather treat it as a shortcut, so the field takes shortcuts too.
func TestKeyCaptureTakesShortcutsRatherThanFiringThem(t *testing.T) {
	u := newTestUI(t)
	var got combo
	cap := newKeyCapture(u.app, combo{}, func(c combo, ok bool) {
		if ok {
			got = c
		}
	})
	cap.mods = func() fyne.KeyModifier { return fyne.KeyModifierControl }
	cap.Tapped(nil)
	cap.TypedShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyK, Modifier: fyne.KeyModifierControl})
	if got.String() != "Ctrl+K" {
		t.Errorf("recorded %q, want Ctrl+K", got.String())
	}
	if u.switcherBox != nil && u.switcherBox.Visible() {
		t.Error("recording Ctrl+K opened the switcher instead of recording it")
	}
}

// --- storage ---------------------------------------------------------------------------

// Combos are stored as a JSON list (§4.3), a list survives the round trip, and
// the pre-M12 single-string form still reads — a user who rebound a key before
// this milestone keeps their binding.
func TestCombosStoreAsAList(t *testing.T) {
	u := newTestUI(t)
	p := u.app.Preferences()

	want := parseCombos([]string{"Alt+J", "Ctrl+Alt+J"})
	setCombos(p, "find", want)
	if got := combosFor(p, "find"); !sameCombos(got, want) {
		t.Errorf("combos = %v, want %v", got, want)
	}
	if raw := p.String(bindingKey("find")); !strings.HasPrefix(raw, "[") {
		t.Errorf("stored form = %q, want a JSON list", raw)
	}

	// the old form: one combo, as a bare string
	p.SetString(bindingKey("close"), "Ctrl+Alt+W")
	got := combosFor(p, "close")
	if len(got) != 1 || got[0].String() != "Ctrl+Alt+W" {
		t.Errorf("a pre-M12 binding read back as %v", got)
	}

	// back to the defaults: the override is removed rather than frozen
	setCombos(p, "find", parseCombos(lookupAction("find").Combos))
	if raw := p.String(bindingKey("find")); raw != "" {
		t.Errorf("resetting to the default left %q behind", raw)
	}
}

// A fixed row cannot be rebound, and nothing pretends otherwise.
func TestFixedBindingsCannotBeStored(t *testing.T) {
	u := newTestUI(t)
	p := u.app.Preferences()
	other, _ := parseCombo("Ctrl+Alt+P")
	setCombos(p, "pins", []combo{other})
	if raw := p.String(bindingKey("pins")); raw != "" {
		t.Errorf("a fixed action stored an override: %q", raw)
	}
	if got := combosFor(p, "pins"); len(got) != 9 {
		t.Errorf("the fixed Alt digits changed: %v", got)
	}
}

// Rebinding an action REPLACES a key rather than adding one: the canvas has to
// forget the old shortcut, or the action answers to both and the key the user
// moved it off is still taken — which is also how a conflict check starts
// telling the truth about a combo nothing is bound to any more.
func TestRebindingReleasesTheOldKey(t *testing.T) {
	u := newTestUI(t)
	p := u.app.Preferences()

	old := combosFor(p, "find")
	if len(old) == 0 {
		t.Fatal("Ctrl+F is unbound to begin with")
	}
	if !u.handleShortcut(old[0].shortcut()) {
		t.Fatalf("%v does not open the search bar to begin with", old[0])
	}
	u.closeOverlays()

	next, _ := parseCombo("Ctrl+Alt+J")
	setCombos(p, "find", []combo{next})
	u.applyKeybinds()

	if !u.handleShortcut(next.shortcut()) {
		t.Error("the new key does not reach the action")
	}
	u.closeOverlays()

	// The old chord is dead in both places it could still be alive: the table
	// no longer claims it, and it is no longer in the set the canvas was given.
	// (Fyne 2.8's canvas cannot be asked what it holds, so the set eco keeps is
	// the only readable half — and every handler re-asks the table anyway,
	// which is what makes a stale registration inert rather than wrong.)
	if u.handleShortcut(old[0].shortcut()) {
		t.Errorf("%v still reaches an action after being rebound away", old[0])
	}
	for _, cs := range u.boundKeys {
		if old[0].matches(cs) {
			t.Errorf("%v is still in the registered set after being rebound away", old[0])
		}
	}
}

// The Shortcuts page lists the fixed keys UNDER the rebindable form, and the
// two lists are the same table filtered two ways. A filter that let a
// rebindable row through would tell the user Ctrl+K cannot be moved while the
// form above it is moving it.
func TestTheFixedKeysListIsOnlyTheFixedKeys(t *testing.T) {
	page := fixedKeysMarkdown(nil)
	for _, a := range keyActions {
		mentioned := strings.Contains(page, a.Label)
		if a.Fixed && !mentioned {
			t.Errorf("%q is fixed and is not in the fixed-keys list", a.Label)
		}
		if !a.Fixed && mentioned {
			t.Errorf("%q is rebindable and is listed as fixed", a.Label)
		}
	}
}

// --- the generated documentation ---------------------------------------------------------

// The drift gate, in the only form a hand-written copy cannot pass: a binding
// this package has never heard of is added to the table at runtime, and the
// documentation page has to show it — label, keys and note — without anything
// being told. M10 did this for commands; the shortcut list had drifted the same
// way by M12, which is why it is done here too.
func TestABindingThisPackageHasNeverSeenIsDocumented(t *testing.T) {
	doc := commandsDocWith(nil)
	if strings.Contains(doc, "Frobnicate the widget") {
		t.Fatal("the test's own binding is already in the page")
	}

	remove := addKeyActionForTest(keyAction{
		ID: "zzdrift", Label: "Frobnicate the widget", Group: groupApp,
		Combos: []string{"Ctrl+Alt+Z"}, Note: "Only in a test.",
	})
	defer remove()

	doc = commandsDocWith(nil)
	for _, want := range []string{"**Ctrl+Alt+Z**", "Frobnicate the widget", "Only in a test."} {
		if !strings.Contains(doc, want) {
			t.Errorf("the documentation page does not contain %q: "+
				"the shortcut section is not the table, it is a copy of it", want)
		}
	}

	remove()
	if strings.Contains(commandsDocWith(nil), "Frobnicate the widget") {
		t.Error("after the row left the table, the page still documents it")
	}
}

// The page shows the keys the USER has, not the defaults — the second half of
// "generated": a rebound key documents itself.
func TestTheDocumentationShowsTheUsersOwnBindings(t *testing.T) {
	u := newTestUI(t)
	p := u.app.Preferences()
	mine, _ := parseCombo("Ctrl+Alt+G")
	setCombos(p, "switcher", []combo{mine})

	doc := u.commandsDoc()
	if !strings.Contains(doc, "**Ctrl+Alt+G**") {
		t.Error("the page does not show the rebound key")
	}
	if strings.Contains(doc, "**Ctrl+K** — Go to a channel") {
		t.Error("the page still shows the default the user replaced")
	}
}

// Every group with rows gets a section, and every row lands in one: a row whose
// group nobody declared must still be documented rather than vanishing.
func TestEveryBindingIsInTheGeneratedPage(t *testing.T) {
	md := keybindHelpMarkdown(nil)
	for _, a := range keyActions {
		if !strings.Contains(md, a.Label) {
			t.Errorf("%s is missing from the generated shortcut list", a.ID)
		}
	}
	remove := addKeyActionForTest(keyAction{
		ID: "zzorphan", Label: "An orphan action", Group: "Nowhere in particular",
		Combos: []string{"Ctrl+Alt+O"},
	})
	defer remove()
	if !strings.Contains(keybindHelpMarkdown(nil), "An orphan action") {
		t.Error("a row in an undeclared group was dropped from the page")
	}
}

// The prose in the shipped documentation may name a shortcut, but only one that
// exists. This is the other half of the anti-drift rule: the generated list
// cannot go stale, and a hand-written sentence beside it now cannot either.
func TestTheDocsNeverNameAShortcutThatIsNotBound(t *testing.T) {
	bound := map[string]bool{}
	for _, a := range keyActions {
		for _, c := range parseCombos(a.Combos) {
			bound[c.String()] = true
		}
	}
	docs := map[string]string{
		"commands-and-shortcuts.md": docCommands,
		"getting-started.md":        docGettingStarted,
		"power-users.md":            docPowerUsers,
		"irc-basics.md":             docIRCBasics,
		"faq.md":                    docFAQ,
		"ergo-guide.md":             docErgoGuide,
	}
	for name, body := range docs {
		for _, mention := range namedCombos(body) {
			if !bound[mention] {
				t.Errorf("%s names %q, which nothing is bound to", name, mention)
			}
		}
	}
}

// namedCombos finds the "Ctrl+X"-shaped things in a document. It deliberately
// only recognises combos whose KEY eco knows: "Shift+arrows" is prose about a
// text box, not a claim about a binding.
func namedCombos(doc string) []string {
	known := map[string]bool{}
	for _, a := range keyActions {
		for _, c := range parseCombos(a.Combos) {
			known[string(c.Key)] = true
		}
	}
	var out []string
	for _, word := range strings.FieldsFunc(doc, func(r rune) bool {
		return r == ' ' || r == '\n' || r == '\t' || r == '(' || r == ')' ||
			r == ',' || r == '.' || r == ':' || r == ';' || r == '`' || r == '*'
	}) {
		if !strings.Contains(word, "+") {
			continue
		}
		c, ok := parseCombo(word)
		if !ok || c.Mod == 0 || !known[string(c.Key)] {
			continue
		}
		out = append(out, c.String())
	}
	return out
}
