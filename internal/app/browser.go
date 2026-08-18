package app

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// showChannelBrowser presents the LIST results: searchable, sorted by size,
// double duty as the beginner's "where do I go?" screen. Click to join.
func (u *UI) showChannelBrowser() {
	sess := u.session()
	if sess == nil {
		return
	}
	// The list comes out of the archive's list_cache, which is where a scan
	// streams to (blueprint §5.4) and where it survives a restart — so the
	// browser has something to show before the first LIST of the session
	// finishes, or without one at all. The page held here is bounded; the
	// search box asks the cache again rather than filtering a resident copy.
	all := sess.Channels("", browseLimit)
	shown := all

	search := widget.NewEntry()
	search.SetPlaceHolder("Type to search or create a channel…")

	count := widget.NewLabel(fmt.Sprintf("%d channels. Click one to join.", len(all)))

	var d dialog.Dialog
	var list *widget.List
	list = widget.NewList(
		func() int { return len(shown) },
		func() fyne.CanvasObject {
			name := widget.NewLabel("#channel")
			name.TextStyle = fyne.TextStyle{Bold: true}
			topic := widget.NewLabel("topic")
			topic.Truncation = fyne.TextTruncateEllipsis
			return container.NewBorder(nil, nil, name, nil, topic)
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i >= len(shown) {
				return
			}
			row := o.(*fyne.Container)
			row.Objects[1].(*widget.Label).SetText(fmt.Sprintf("%s (%d)", shown[i].Name, shown[i].Users))
			row.Objects[0].(*widget.Label).SetText(shown[i].Topic)
		},
	)
	list.OnSelected = func(i widget.ListItemID) {
		if i < len(shown) && sess.irc != nil {
			sess.JoinUser(shown[i].Name)
			d.Hide()
		}
	}
	// Enter joins the top match; with no match it joins the typed name,
	// so browsing and creating channels are one flow
	joinTyped := func() {
		if sess.irc == nil {
			return
		}
		if len(shown) > 0 {
			sess.JoinUser(shown[0].Name)
			d.Hide()
			return
		}
		if name := strings.TrimSpace(search.Text); name != "" {
			if !strings.HasPrefix(name, "#") {
				name = "#" + name
			}
			sess.JoinUser(name)
			d.Hide()
		}
	}
	search.OnSubmitted = func(string) { joinTyped() }

	// explicit create affordance: appears whenever the typed name does not
	// exactly match an existing channel. It must join the TYPED name, never
	// the top match: with "dev" typed and "#developers" listed, the button
	// says "Create #dev" and joinTyped would join #developers instead.
	// clicking Create opens a small options page first (topic, key,
	// invite-only, ...). The creator is op, so the modes and topic can be
	// applied immediately after the join.
	createTyped := func() {
		if sess.irc == nil {
			return
		}
		name := strings.TrimSpace(search.Text)
		if name == "" {
			return
		}
		if !strings.HasPrefix(name, "#") {
			name = "#" + name
		}
		f := newChannelOptionsForm("", "", "")
		dialog.ShowForm("Create "+name, "Create", "Cancel", f.items(), func(ok bool) {
			if !ok || sess.irc == nil {
				return
			}
			// the creator is op, so modes and topic apply right after the join
			sess.JoinUser(name)
			for _, c := range f.commands(name) {
				sess.irc.Send(c[0], c[1:]...)
			}
			d.Hide()
		}, u.win)
	}
	createBtn := widget.NewButtonWithIcon("", theme.ContentAddIcon(), createTyped)
	createBtn.Importance = widget.HighImportance
	createBtn.Hide()

	updateCreate := func() {
		name := strings.TrimSpace(search.Text)
		if name == "" {
			createBtn.Hide()
			return
		}
		if !strings.HasPrefix(name, "#") {
			name = "#" + name
		}
		for _, c := range shown {
			if strings.EqualFold(c.Name, name) {
				createBtn.Hide()
				return
			}
		}
		createBtn.SetText("Create " + name)
		createBtn.Show()
	}

	search.OnChanged = func(q string) {
		q = strings.TrimSpace(q)
		if q == "" {
			shown = all
		} else {
			// The cache is asked again rather than a resident copy filtered:
			// what is on screen is a page of it, and a network's tenth
			// thousandth channel has to be findable from the same box.
			shown = sess.Channels(q, browseLimit)
		}
		if len(shown) == 0 && q != "" {
			name := strings.TrimSpace(search.Text)
			if !strings.HasPrefix(name, "#") {
				name = "#" + name
			}
			count.SetText("No matches. Press Enter to join or create " + name)
		} else {
			count.SetText(fmt.Sprintf("%d channels. Click one to join.", len(shown)))
		}
		list.Refresh()
		updateCreate()
	}

	content := container.NewBorder(container.NewVBox(search, count), createBtn, nil, nil, list)
	d = dialog.NewCustom("Browse channels", "Close",
		container.NewGridWrap(u.dialogSize(560, 420), content), u.win)
	u.trackDialog(d)
	d.Show()
	u.win.Canvas().Focus(search)
}
