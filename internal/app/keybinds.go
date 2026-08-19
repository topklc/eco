package app

import (
	"encoding/json"
	"runtime"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
)

// The keyboard model as a TABLE - blueprint §4.3, and the same anti-drift rule
// M10 applied to commands.
//
// Everything the keyboard does is one row below: the rebindable actions AND the
// fixed keys, in one list, because the three things that need the list need all
// of it. Registration needs the rebindable rows; conflict detection needs the
// fixed ones too, or rebinding Ctrl+K onto Tab is a change nothing objects to
// and the composer silently loses tab completion; and the documentation page is
// GENERATED from the whole table, so a binding is documented by existing.
//
// v1 had this list four times - the registration, the Preferences page, the
// help markdown and the power-user page - and by M12 they already disagreed
// about which key stepped buffers. There is now exactly one list, and a test
// that adds a row to it at runtime and demands the page show it (keybinds_test).
//
// # Combos are lists
//
// Each action carries a LIST of combos, stored as JSON, so Alt+Up and
// Ctrl+PageUp can both mean "previous conversation" without one of them being a
// second-class citizen - §4.3's "per-action combo lists (JSON)".
//
// # Mod, not Ctrl
//
// The portable modifier is spelled "Mod" in the table and resolves to
// fyne.KeyModifierShortcutDefault: Control today, Command when eco is built for
// macOS. Writing "Ctrl" into the defaults would have to be found and changed in
// twelve places on the day that matters.

// keyGroup names a section of the generated shortcut documentation. The order
// here is the order the page is written in.
type keyGroup string

const (
	groupBuffers    keyGroup = "Moving between conversations"
	groupPanes      keyGroup = "Panes"
	groupScrollback keyGroup = "Reading back"
	groupFinding    keyGroup = "Finding things"
	groupComposing  keyGroup = "Writing a message"
	groupApp        keyGroup = "The application"
)

// keyGroups is the order groups appear in the documentation.
var keyGroups = []keyGroup{
	groupBuffers, groupPanes, groupScrollback, groupFinding, groupComposing, groupApp,
}

// keyAction is one row of the table: something the keyboard does, and the
// combos that do it.
type keyAction struct {
	// ID is stable and is the preference key's suffix ("kb_" + ID). It is set
	// for fixed rows too, so a test or a help anchor can name one.
	ID    string
	Label string
	Group keyGroup

	// Combos are the DEFAULTS for a rebindable action, and the reserved combos
	// for a fixed one. Every entry must parse: conflict detection compares
	// parsed combos, and a row that cannot be parsed is a row that silently
	// reserves nothing.
	Combos []string

	// Display overrides how the combos are written in the documentation, for
	// the rows where a list of nine is not the clearest way to say it.
	Display string

	// Fixed marks a binding the user cannot move. Fixed rows are still in the
	// table because they are still documented and still conflict.
	Fixed bool

	// Reserved marks a row that exists ONLY to reserve keys another widget
	// owns - the text box's copy and paste. Nothing registers them: they are
	// already handled by the widget that has focus, and taking them would be
	// how a rebind quietly breaks Ctrl+V.
	Reserved bool

	// Note is one extra sentence for the documentation, where the label is not
	// the whole story.
	Note string
}

// keyActions is the whole keyboard. Adding a row here is all it takes for the
// key to be registered, rebindable (unless Fixed), conflict-checked and
// documented.
var keyActions = []keyAction{
	// --- moving between conversations ---
	{ID: "prev", Label: "Previous conversation", Group: groupBuffers,
		Combos: []string{"Alt+Up", "Mod+PageUp"},
		Note:   "Skips network headers, and never dials a server you are not connected to."},
	{ID: "next", Label: "Next conversation", Group: groupBuffers,
		Combos: []string{"Alt+Down", "Mod+PageDown"}},
	{ID: "unread", Label: "Jump to the next mention, then to unread", Group: groupBuffers,
		Combos: []string{"Alt+A"},
		Note: "Mentions first, then anything unread, in rail order, and it opens a " +
			"folded network when that is where the mention is."},
	{ID: "pins", Label: "Jump to a pinned conversation", Group: groupBuffers,
		Combos: []string{"Alt+1", "Alt+2", "Alt+3", "Alt+4", "Alt+5",
			"Alt+6", "Alt+7", "Alt+8", "Alt+9"},
		Display: "Alt+1 … Alt+9", Fixed: true,
		Note: "Pin one from its right-click menu. The digits follow the conversation, " +
			"not the row it happens to sit on."},
	{ID: "close", Label: "Close this conversation", Group: groupBuffers,
		Combos: []string{"Mod+W"}},

	// --- panes ---
	{ID: "rail", Label: "Show or hide the conversation rail", Group: groupPanes,
		Combos: []string{"Mod+B"}},
	{ID: "people", Label: "Show or hide the people list", Group: groupPanes,
		Combos: []string{"Mod+U"},
		Note:   "eco remembers the answer for each conversation separately."},
	{ID: "reader", Label: "Reader mode: plain text you can select and copy", Group: groupPanes,
		Combos: []string{"F6"}, Fixed: true},

	// --- reading back ---
	{ID: "page", Label: "Scroll the conversation a page", Group: groupScrollback,
		Combos: []string{"PageUp", "PageDown"}, Display: "PageUp / PageDown", Fixed: true,
		Note: "They work from the message box, which is where the keyboard lives."},
	{ID: "top", Label: "Jump to the oldest message loaded", Group: groupScrollback,
		Combos: []string{"Mod+Home"}},
	{ID: "bottom", Label: "Jump to the newest message and follow it again", Group: groupScrollback,
		Combos: []string{"Mod+End"}},
	{ID: "end", Label: "Jump to the newest message", Group: groupScrollback,
		Combos: []string{"End"}, Fixed: true,
		Note: "Only when the message box is empty - otherwise End is the end of your line."},

	// --- finding things ---
	{ID: "switcher", Label: "Go to a channel or conversation", Group: groupFinding,
		Combos: []string{"Mod+K"},
		Note:   "Ranks the conversations you are in, channels you are not, and “create it”."},
	{ID: "find", Label: "Find in this conversation", Group: groupFinding,
		Combos: []string{"Mod+F"}},
	{ID: "finder", Label: "Search everything eco remembers", Group: groupFinding,
		Combos: []string{"Mod+Shift+F"}},

	// --- writing a message ---
	{ID: "send", Label: "Send the message", Group: groupComposing,
		Combos: []string{"Return"}, Display: "Enter", Fixed: true},
	{ID: "newline", Label: "Add a line to the message", Group: groupComposing,
		Combos: []string{"Shift+Return"}, Display: "Shift+Enter", Fixed: true},
	{ID: "complete", Label: "Complete a nick, a #channel or a /command", Group: groupComposing,
		Combos: []string{"Tab"}, Fixed: true,
		Note: "Press again to take the next candidate; the row of them is shown above the box."},
	{ID: "completeback", Label: "Step back through the candidates", Group: groupComposing,
		Combos: []string{"Shift+Tab"}, Fixed: true},
	{ID: "history", Label: "Walk through the messages you have sent", Group: groupComposing,
		Combos: []string{"Up", "Down"}, Display: "Up / Down", Fixed: true},
	{ID: "editing", Label: "Select all, copy, cut, paste, undo", Group: groupComposing,
		Combos:  []string{"Mod+A", "Mod+C", "Mod+X", "Mod+V", "Mod+Z"},
		Display: "Mod+A, Mod+C, Mod+X, Mod+V, Mod+Z", Fixed: true, Reserved: true,
		Note: "The text box's own keys. eco will not let you rebind over them."},

	// --- the application ---
	{ID: "escape", Label: "Close the topmost thing, or come back to the newest message",
		Group: groupApp, Combos: []string{"Escape"}, Fixed: true,
		Note: "A dialog first, then a running completion, then the switcher, the finder or " +
			"the search bar, then Reader mode - and with none of those open, the present."},
	{ID: "quit", Label: "Quit eco", Group: groupApp, Combos: []string{"Mod+Q"}},
}

// lookupAction returns a table row by id.
func lookupAction(id string) *keyAction {
	for i := range keyActions {
		if keyActions[i].ID == id {
			return &keyActions[i]
		}
	}
	return nil
}

// --- combos --------------------------------------------------------------------

// combo is one key with its modifiers, canonicalised so two spellings of the
// same chord are one value.
type combo struct {
	Mod fyne.KeyModifier
	Key fyne.KeyName
}

// modNames maps a display modifier onto its bit. "Mod" is the portable one and
// is what the table's defaults use.
var modNames = []struct {
	name string
	bit  fyne.KeyModifier
}{
	{"Mod", fyne.KeyModifierShortcutDefault},
	{"Ctrl", fyne.KeyModifierControl},
	{"Control", fyne.KeyModifierControl},
	{"Cmd", fyne.KeyModifierSuper},
	{"Command", fyne.KeyModifierSuper},
	{"Super", fyne.KeyModifierSuper},
	{"Alt", fyne.KeyModifierAlt},
	{"Option", fyne.KeyModifierAlt},
	{"Shift", fyne.KeyModifierShift},
}

// keyNames maps the display spelling of a key onto Fyne's KeyName, for the ones
// where the two differ. Fyne calls Page Up "Prior" and Page Down "Next", which
// are X11's names and nobody else's.
var keyNames = map[string]fyne.KeyName{
	"PageUp":   fyne.KeyPageUp,
	"PageDown": fyne.KeyPageDown,
	"Enter":    fyne.KeyReturn,
	"Esc":      fyne.KeyEscape,
	"Space":    fyne.KeySpace,
}

// keyDisplay is keyNames inverted: what to WRITE for a Fyne key name.
var keyDisplay = map[fyne.KeyName]string{
	fyne.KeyPageUp:   "PageUp",
	fyne.KeyPageDown: "PageDown",
	fyne.KeyReturn:   "Enter",
	fyne.KeyEnter:    "Enter",
	fyne.KeyEscape:   "Escape",
}

// parseCombo turns "Mod+Shift+F" into a combo. The key is the part after the
// last '+', so modifier names may themselves contain one.
func parseCombo(s string) (combo, bool) {
	parts := strings.Split(strings.TrimSpace(s), "+")
	if len(parts) == 0 || parts[len(parts)-1] == "" {
		return combo{}, false
	}
	c := combo{}
	for _, p := range parts[:len(parts)-1] {
		bit, ok := modBit(strings.TrimSpace(p))
		if !ok {
			return combo{}, false
		}
		c.Mod |= bit
	}
	key := strings.TrimSpace(parts[len(parts)-1])
	if key == "" {
		return combo{}, false
	}
	if mapped, ok := keyNames[key]; ok {
		c.Key = mapped
	} else {
		c.Key = fyne.KeyName(key)
	}
	return c, true
}

func modBit(name string) (fyne.KeyModifier, bool) {
	for _, m := range modNames {
		if strings.EqualFold(m.name, name) {
			return m.bit, true
		}
	}
	return 0, false
}

// String renders a combo the way it is written down: modifiers in a fixed
// order, then the key. The platform's own name for the portable modifier is
// used, so a Linux user reads "Ctrl+K" and a Mac user will read "Cmd+K".
func (c combo) String() string {
	var b strings.Builder
	write := func(bit fyne.KeyModifier, name string) {
		if c.Mod&bit != 0 {
			b.WriteString(name)
			b.WriteString("+")
		}
	}
	write(fyne.KeyModifierControl, ctrlName())
	write(fyne.KeyModifierSuper, superName())
	write(fyne.KeyModifierAlt, "Alt")
	write(fyne.KeyModifierShift, "Shift")
	if d, ok := keyDisplay[c.Key]; ok {
		b.WriteString(d)
	} else {
		b.WriteString(string(c.Key))
	}
	return b.String()
}

// ctrlName and superName spell the two hard modifiers for this platform. On
// macOS the portable modifier IS Super, so it is the one that reads "Cmd".
func ctrlName() string { return "Ctrl" }

func superName() string {
	if runtime.GOOS == "darwin" {
		return "Cmd"
	}
	return "Super"
}

// shortcut is the combo as Fyne's canvas wants it.
func (c combo) shortcut() *desktop.CustomShortcut {
	return &desktop.CustomShortcut{KeyName: c.Key, Modifier: c.Mod}
}

// matches reports whether a received shortcut is this combo.
func (c combo) matches(cs *desktop.CustomShortcut) bool {
	return cs != nil && cs.KeyName == c.Key && cs.Modifier == c.Mod
}

// bare reports whether a combo carries no modifier that could keep it out of
// the message box. Shift alone does not count: Shift+K is the letter K.
func (c combo) bare() bool {
	return c.Mod&(fyne.KeyModifierControl|fyne.KeyModifierAlt|fyne.KeyModifierSuper) == 0
}

// parseCombos parses a list, dropping what does not parse.
func parseCombos(list []string) []combo {
	out := make([]combo, 0, len(list))
	for _, s := range list {
		if c, ok := parseCombo(s); ok {
			out = append(out, c)
		}
	}
	return out
}

// --- what is bound right now -----------------------------------------------------

// combosFor returns the combos in force for an action: the user's list when
// they have one, the table's defaults otherwise. A fixed action is always its
// own list - storing an override for one would be a preference that does
// nothing, which is worse than no preference at all.
func combosFor(p fyne.Preferences, id string) []combo {
	a := lookupAction(id)
	if a == nil {
		return nil
	}
	if a.Fixed || p == nil {
		return parseCombos(a.Combos)
	}
	if raw := p.String(bindingKey(id)); raw != "" {
		if list, ok := decodeCombos(raw); ok {
			return list
		}
	}
	return parseCombos(a.Combos)
}

// bindingKey is where one action's combos are stored.
func bindingKey(id string) string { return "kb_" + id }

// decodeCombos reads the stored form: a JSON array of combo strings. A plain
// string is accepted too, because that is what M10-era preferences hold - a
// user who rebound a key before this milestone keeps their binding.
func decodeCombos(raw string) ([]combo, bool) {
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err == nil {
		out := parseCombos(list)
		return out, len(out) > 0
	}
	if c, ok := parseCombo(raw); ok {
		return []combo{c}, true
	}
	return nil, false
}

// encodeCombos writes the stored form.
func encodeCombos(list []combo) string {
	out := make([]string, 0, len(list))
	for _, c := range list {
		out = append(out, c.String())
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return "" // unreachable: []string always marshals
	}
	return string(raw)
}

// setCombos stores an action's combos, or clears the override when the list is
// the table's default again.
func setCombos(p fyne.Preferences, id string, list []combo) {
	a := lookupAction(id)
	if a == nil || a.Fixed || p == nil {
		return
	}
	if sameCombos(list, parseCombos(a.Combos)) {
		p.RemoveValue(bindingKey(id))
		return
	}
	p.SetString(bindingKey(id), encodeCombos(list))
}

func sameCombos(a, b []combo) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- conflicts -------------------------------------------------------------------

// conflict describes a combo that is already taken.
type conflict struct {
	ActionID string
	Label    string
	Fixed    bool
}

// conflictFor reports what already owns a combo, ignoring the action being
// edited. It is the whole of §4.3's "live conflict detection incl. fixed
// Alt+digits and reserved input keys": the fixed rows are in the same table, so
// they are checked by the same walk, and there is no second list of "things you
// must not take" to forget to update.
func conflictFor(p fyne.Preferences, editing string, c combo) (conflict, bool) {
	for _, a := range keyActions {
		if a.ID == editing {
			continue
		}
		for _, have := range combosFor(p, a.ID) {
			if have == c {
				return conflict{ActionID: a.ID, Label: a.Label, Fixed: a.Fixed}, true
			}
		}
	}
	return conflict{}, false
}

// comboRefusal explains why a combo cannot be bound at all, or "" when it can.
// A combo with no Ctrl/Alt/Super never reaches the app: focus lives in the
// message box, so a bare key is a character someone is trying to type.
func comboRefusal(c combo) string {
	if c.Key == "" {
		return "Press a key."
	}
	if isModifierKey(c.Key) {
		return "That is a modifier on its own - hold it and press another key."
	}
	if c.bare() {
		return "Add Ctrl or Alt: on its own, that key belongs to the message box."
	}
	return ""
}

// isModifierKey reports whether a key name is one of the modifiers themselves,
// which the capture widget sees as key presses like any other.
func isModifierKey(k fyne.KeyName) bool {
	switch k {
	case desktop.KeyShiftLeft, desktop.KeyShiftRight,
		desktop.KeyControlLeft, desktop.KeyControlRight,
		desktop.KeyAltLeft, desktop.KeyAltRight,
		desktop.KeySuperLeft, desktop.KeySuperRight,
		desktop.KeyMenu, desktop.KeyCapsLock:
		return true
	}
	return false
}

// --- registration ------------------------------------------------------------------

// matchesAction reports whether a received shortcut is one of an action's
// current combos.
func (u *UI) matchesAction(cs *desktop.CustomShortcut, id string) bool {
	for _, c := range combosFor(u.app.Preferences(), id) {
		if c.matches(cs) {
			return true
		}
	}
	return false
}

// applyKeybinds (re)registers every canvas shortcut in the table: the
// rebindable ones as the user has them, and the fixed ones that are chords
// (Alt+digits) rather than bare keys. Bare fixed keys - Escape, Tab, PageUp,
// End, F6 - are not shortcuts at all; they arrive as key events and are claimed
// in handleKey, which is what lets the composer see the ones the app declines.
func (u *UI) applyKeybinds() {
	cv := u.win.Canvas()
	for _, old := range u.boundKeys {
		cv.RemoveShortcut(old)
	}
	u.boundKeys = u.boundKeys[:0]
	p := u.app.Preferences()
	for _, a := range keyActions {
		if a.Reserved {
			continue // the focused widget's own keys; see the field's comment
		}
		for _, c := range combosFor(p, a.ID) {
			if c.bare() {
				continue // a key event, not a shortcut; see handleKey
			}
			cs := c.shortcut()
			u.boundKeys = append(u.boundKeys, cs)
			cv.AddShortcut(cs, func(fyne.Shortcut) { u.handleShortcut(cs) })
		}
	}
}

// --- the generated documentation -----------------------------------------------------

// keybindHelpMarkdown renders the table as the documentation window's shortcut
// section, with the combos the user actually has. It is spliced into the page
// at shortcutsMarker (help.go), the same way the command list is - so a binding
// added to the table is documented by existing, and a rebound key documents
// itself.
func keybindHelpMarkdown(p fyne.Preferences) string {
	var b strings.Builder
	for _, g := range groupsInOrder() {
		var rows []string
		for _, a := range keyActions {
			if a.Group != g {
				continue
			}
			line := "**" + actionCombos(p, a) + "** - " + a.Label
			if a.Note != "" {
				line += ". " + a.Note
			}
			rows = append(rows, line)
		}
		if len(rows) == 0 {
			continue
		}
		b.WriteString("### ")
		b.WriteString(string(g))
		b.WriteString("\n\n")
		for _, r := range rows {
			b.WriteString(r)
			b.WriteString("\n\n")
		}
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// actionCombos is how one row's keys are written: the Display override when it
// has one, otherwise its combos joined with "or".
func actionCombos(p fyne.Preferences, a keyAction) string {
	if a.Display != "" {
		// A display string may still name the portable modifier, so it goes
		// through the same rendering as everything else.
		return renderModWords(a.Display)
	}
	list := combosFor(p, a.ID)
	out := make([]string, 0, len(list))
	for _, c := range list {
		out = append(out, c.String())
	}
	if len(out) == 0 {
		return "unbound"
	}
	return strings.Join(out, " or ")
}

// renderModWords replaces the portable modifier's name with the platform's, so
// a Display string reads the way the keys do.
func renderModWords(s string) string {
	return strings.ReplaceAll(s, "Mod", modWord())
}

// modWord is what this platform calls fyne.KeyModifierShortcutDefault.
func modWord() string {
	if fyne.KeyModifierShortcutDefault == fyne.KeyModifierSuper {
		return superName()
	}
	return ctrlName()
}

// groupsInOrder returns the groups that have rows, in the declared order, with
// anything a new row invented appended - so a group name that is not in
// keyGroups still gets its section rather than vanishing.
func groupsInOrder() []keyGroup {
	seen := map[keyGroup]bool{}
	var out []keyGroup
	for _, g := range keyGroups {
		for _, a := range keyActions {
			if a.Group == g && !seen[g] {
				seen[g] = true
				out = append(out, g)
			}
		}
	}
	var extra []keyGroup
	for _, a := range keyActions {
		if !seen[a.Group] {
			seen[a.Group] = true
			extra = append(extra, a.Group)
		}
	}
	sort.Slice(extra, func(i, j int) bool { return extra[i] < extra[j] })
	return append(out, extra...)
}

// addKeyActionForTest appends a row to the table and returns a function that
// removes it again. It exists for the drift gate in keybinds_test.go: the only
// honest way to prove the documentation is GENERATED is to add a binding this
// package has never heard of and require the page to show it. It is the same
// seam command.AddForTest is, for the same reason.
func addKeyActionForTest(a keyAction) func() {
	keyActions = append(keyActions, a)
	return func() {
		for i := range keyActions {
			if keyActions[i].ID == a.ID {
				keyActions = append(keyActions[:i], keyActions[i+1:]...)
				return
			}
		}
	}
}
