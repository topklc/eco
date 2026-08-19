package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/toprakkilic/eco/internal/ui/viewmodel"
)

// Two rules of §4.1 that are properties of the WIRING rather than of any one
// surface, and are therefore the two easiest things in this milestone to undo
// by accident:
//
//   - the Parity Rule — "a kebab menu carrying every action that also lives on
//     right-click" — which is only true if there is one list, and
//   - "every close restores composer focus through one shared helper", which is
//     only true if every rung of the Escape stack ends at that helper.
//
// Both are asserted here as properties, not as inventories: a list of the items
// each menu ought to have would be the second copy the rule exists to forbid.

// --- the Parity Rule ---------------------------------------------------------------

// The structural half. A menu-bearing pane is handed ONE builder, by name, and
// the pane hands the same one to both gestures (widgets/rail_test.go and
// widgets/people_test.go pin that end). An inline literal here — `Menu:
// func(row) {...}` — would be a second list the moment anybody added an item to
// the other one, so it is refused in the source.
func TestEveryPaneMenuIsOneNamedBuilder(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, 0)
	if err != nil {
		t.Fatalf("parsing this package: %v", err)
	}
	want := map[string]string{
		"RailOptions":   "rowMenuItems",
		"PeopleOptions": "memberMenuItems",
	}
	seen := map[string]bool{}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.CompositeLit)
				if !ok {
					return true
				}
				sel, ok := lit.Type.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				builder, ok := want[sel.Sel.Name]
				if !ok {
					return true
				}
				seen[sel.Sel.Name] = true
				for _, el := range lit.Elts {
					kv, ok := el.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					if key, _ := kv.Key.(*ast.Ident); key == nil || key.Name != "Menu" {
						continue
					}
					got, ok := kv.Value.(*ast.SelectorExpr)
					if !ok || got.Sel.Name != builder {
						t.Errorf("%s.Menu is not the shared %s builder — the ⋯ and the "+
							"right-click menu would be two lists (%s)",
							sel.Sel.Name, builder, fset.Position(kv.Pos()))
					}
				}
				return true
			})
		}
	}
	for opts := range want {
		if !seen[opts] {
			t.Errorf("no %s literal found; this gate is watching nothing", opts)
		}
	}
}

// The same rule for the surface that has no options struct to carry a builder:
// the chat header's kebab pops a menu directly, so the structural claim is about
// its function — it ASKS the shared builder and constructs nothing itself. A
// literal inside it would be the second list, and it would be a second list
// nothing compares against, because the comparison below can only compare what
// the builder returns.
func TestAMenuPopperBuildsNothingItself(t *testing.T) {
	poppers := map[string]string{
		"showBufferMenu": "bufferMenuItems",
	}
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, 0)
	if err != nil {
		t.Fatalf("parsing this package: %v", err)
	}
	found := map[string]bool{}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				builder, watched := poppers[fn.Name.Name]
				if !watched {
					continue
				}
				found[fn.Name.Name] = true
				calls := false
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					switch x := n.(type) {
					case *ast.CallExpr:
						if sel, ok := x.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == builder {
							calls = true
						}
					case *ast.CompositeLit:
						if isMenuItemSlice(x.Type) {
							t.Errorf("%s builds menu items of its own (%s); the kebab has to "+
								"offer the row menu, not a copy of it",
								fn.Name.Name, fset.Position(x.Pos()))
						}
					}
					return true
				})
				if !calls {
					t.Errorf("%s never calls %s", fn.Name.Name, builder)
				}
			}
		}
	}
	for name := range poppers {
		if !found[name] {
			t.Errorf("no %s found; this gate is watching nothing", name)
		}
	}
}

// isMenuItemSlice reports whether a composite literal's type is []*fyne.MenuItem.
func isMenuItemSlice(t ast.Expr) bool {
	arr, ok := t.(*ast.ArrayType)
	if !ok || arr.Len != nil {
		return false
	}
	star, ok := arr.Elt.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "MenuItem"
}

// The behavioural half, for the third consumer §4.1 names: the chat header's
// kebab has to offer the buffer's row menu, item for item. It is a different
// call site from the rail's, so "one builder" is a claim that has to be shown
// and not only read.
func TestTheHeaderKebabIsTheRowMenu(t *testing.T) {
	u := newTestUI(t)
	s := attachSession(t, u, "libera")
	s.getBuffer("#eco")
	s.getBuffer("alice")
	u.refreshBuffers()

	for _, buffer := range []string{"#eco", "alice", serverBuffer} {
		u.selectRow(s, buffer)
		r := u.vm.Current()
		row := viewmodel.Row{Kind: viewmodel.RowBuffer, Ref: r, Live: true,
			Profile: u.vm.Profile(r.Session), Pin: u.vm.PinnedAt(r)}

		kebab := menuLabels(u.bufferMenuItems())
		rail := menuLabels(u.rowMenuItems(row))
		if !eqStrings(kebab, rail) {
			t.Errorf("%s: the header kebab offers\n  %v\nand the rail row offers\n  %v",
				buffer, kebab, rail)
		}
		if len(kebab) == 0 {
			t.Errorf("%s: the menu is empty; the comparison above proved nothing", buffer)
		}
	}
}

// A menu built for a buffer whose session has gone is no menu at all, rather
// than a list of items that dereference a nil session when they are clicked.
func TestARowMenuWithNoSessionIsEmpty(t *testing.T) {
	u := newTestUI(t)
	row := viewmodel.Row{Kind: viewmodel.RowBuffer, Live: true, Profile: "gone",
		Ref: viewmodel.Ref{Session: "s-gone", Name: "#eco"}}
	if got := u.rowMenuItems(row); len(got) != 0 {
		t.Errorf("a buffer row with no session behind it offered %v", menuLabels(got))
	}
}

// menuLabels flattens a menu to the words it puts on screen, submenus included,
// with the check marks — because "Notifications ▸ Mute ✓" is an action offer and
// two menus that disagree about it are two menus.
func menuLabels(items []*fyne.MenuItem) []string {
	var out []string
	for _, it := range items {
		switch {
		case it == nil:
			out = append(out, "<nil>")
		case it.IsSeparator:
			out = append(out, "---")
		default:
			label := it.Label
			if it.Checked {
				label += " ✓"
			}
			if it.Disabled {
				label += " (disabled)"
			}
			out = append(out, label)
			if it.ChildMenu != nil {
				for _, sub := range menuLabels(it.ChildMenu.Items) {
					out = append(out, "  "+sub)
				}
			}
		}
	}
	return out
}

// --- the Escape stack, and the one focus helper ------------------------------------

// §4.1: "every close restores composer focus through one shared helper". Every
// rung of the stack is walked here, each one closed with the key the user
// presses, and the keyboard has to be back in the message box afterwards —
// including the dialog rung, where the toolkit's own overlay stack is what does
// it and a hand-rolled close could quietly take it away.
func TestEveryEscapeRungGivesTheKeyboardBack(t *testing.T) {
	rungs := []struct {
		name string
		open func(t *testing.T, u *UI, s *Session)
		want escLayer
	}{
		{"a dialog", func(t *testing.T, u *UI, s *Session) {
			d := dialog.NewCustomWithoutButtons("Settings", widget.NewLabel("x"), u.win)
			u.trackDialog(d)
			d.Show()
		}, escDialog},
		{"the Switcher", func(t *testing.T, u *UI, s *Session) { u.showSwitcher() }, escOverlay},
		{"the Finder", func(t *testing.T, u *UI, s *Session) { u.showFinder() }, escOverlay},
		{"the search bar", func(t *testing.T, u *UI, s *Session) { u.showSearch() }, escOverlay},
		{"Reader mode", func(t *testing.T, u *UI, s *Session) { u.toggleReader() }, escReader},
		{"the scrollback", func(t *testing.T, u *UI, s *Session) {
			u.timeline.Resize(fyne.NewSize(400, 200))
			u.timeline.SetBuffer(manyLines(200), time.Time{})
			u.timeline.ScrollToTop()
		}, escBottom},
	}
	for _, c := range rungs {
		t.Run(c.name, func(t *testing.T) {
			u := newTestUI(t)
			s := attachSession(t, u, "libera")
			u.refreshBuffers()
			u.selectRow(s, serverBuffer)
			c.open(t, u, s)

			if got := escapeTarget(u.escapeState()); got != c.want {
				t.Fatalf("with %s open Escape means %d, want %d", c.name, got, c.want)
			}
			if !u.handleEscape() {
				t.Fatalf("Escape did nothing with %s open", c.name)
			}
			if got := escapeTarget(u.escapeState()); got == c.want {
				t.Errorf("Escape left %s open", c.name)
			}
			if got := u.win.Canvas().Focused(); got != fyne.Focusable(u.input) {
				t.Errorf("after closing %s the keyboard is at %T, want the composer", c.name, got)
			}
		})
	}
}

// The scrollback rung is a JUMP, not just a focus change: Escape with nothing
// open means "back to the present", the same thing End does and the same thing
// the jump pill's label promises.
func TestEscapeWithNothingOpenComesBackToThePresent(t *testing.T) {
	u := newTestUI(t)
	u.timeline.Resize(fyne.NewSize(400, 200))
	u.timeline.SetBuffer(manyLines(200), time.Time{})
	u.timeline.ScrollToTop()
	if u.timeline.NearBottom() {
		t.Fatal("the fixture is not scrolled up")
	}
	if !u.handleEscape() {
		t.Fatal("Escape did nothing in the scrollback")
	}
	if !u.timeline.NearBottom() {
		t.Error("Escape restored focus but did not come back to the newest message")
	}
	if u.handleEscape() {
		t.Error("Escape claimed a keypress with nothing left to dismiss")
	}
}

// The close BUTTON is a close path too. Escape reaches the composer through
// closeOverlays; the ✕ on the search bar calls closeSearch directly, so it is
// its own path to the shared helper and its own way to lose the keyboard.
func TestTheSearchBarsCloseButtonGivesTheKeyboardBack(t *testing.T) {
	u := newTestUI(t)
	s := attachSession(t, u, "libera")
	u.refreshBuffers()
	u.selectRow(s, serverBuffer)

	u.showSearch()
	if u.win.Canvas().Focused() == fyne.Focusable(u.input) {
		t.Fatal("the bar opened without taking the keyboard; this test proves nothing")
	}
	u.searchBar.Cancel()
	if u.searchBar.Visible() {
		t.Error("the close button did not close the bar")
	}
	if got := u.win.Canvas().Focused(); got != fyne.Focusable(u.input) {
		t.Errorf("the close button left the keyboard at %T, want the composer", got)
	}
}
