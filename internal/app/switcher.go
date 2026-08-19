package app

import (
	"strconv"
	"strings"

	"fyne.io/fyne/v2/dialog"

	"github.com/toprakkilic/eco/internal/ui/dispatch"
	"github.com/toprakkilic/eco/internal/ui/viewmodel"
	"github.com/toprakkilic/eco/internal/ui/widgets"
)

// The Switcher (Ctrl+K) where it meets the app - blueprint §4.1 and §4.6.
//
// v1 had two answers to "where do I go?": a sidebar you scrolled and a Browse
// channels dialog that only knew about channels you were NOT in. They were
// different widgets, different keystrokes and different mental models for one
// question, and the second one - browser.go - is gone rather than moved. The
// ranking that merges them is the viewmodel's (switcher.go there, tested in a
// table); this file is where the rows get their words and what Enter does.
//
// The one rule worth stating: a channel from the LIST cache is a candidate, not
// a mode. Typing "eco" offers the #eco you are in, the #eco-dev you are not,
// and "Create #eco-ci" if nothing is called that - three kinds of row in one
// list, ordered by how well they answer, so joining a room and switching to one
// are the same gesture.

// switchListLimit is how much of each network's LIST cache the Switcher ranks.
// It is small on purpose: the cache is asked again on every keystroke (the same
// discipline browser.go had - a network's ten thousandth channel has to be
// findable from the box, so what is on screen is a page and not a resident
// copy), and sixty rows is what the panel can show.
const switchListLimit = 40

// showSwitcher opens Ctrl+K.
func (u *UI) showSwitcher() {
	if !u.built() || len(u.sessions) == 0 {
		return
	}
	u.openOverlay(u.switcherBox, u.switcher)
}

// switcherRows ranks the buffers and the LIST cache against what has been typed
// and turns the answer into rows. The Palette asks for this on every keystroke.
func (u *UI) switcherRows() []widgets.PaletteRow {
	q := u.switcher.Query()
	u.switchRows = u.vm.Switch(q, u.listCandidates(q))
	rows := make([]widgets.PaletteRow, 0, len(u.switchRows))
	for _, c := range u.switchRows {
		rows = append(rows, u.switcherRow(c))
	}
	return rows
}

// switcherRow is one candidate's words.
func (u *UI) switcherRow(c viewmodel.Candidate) widgets.PaletteRow {
	row := widgets.PaletteRow{Title: c.Name, Detail: c.Topic, Meta: u.networkNote(c.Session)}
	switch c.Kind {
	case viewmodel.SwitchBuffer:
		if s := u.sessionFor(c.Session); s != nil {
			row.Detail = s.topicOf(c.Name)
		}
		// the badge, said in words rather than drawn again: this list is not
		// the rail, and a second pill here would be a second thing to keep in
		// step with §4.4
		if c.Counts.Highlights > 0 {
			row.Meta = join(strconv.Itoa(c.Counts.Highlights)+" mentions", row.Meta)
		} else if c.Counts.Unread > 0 {
			row.Meta = join(strconv.Itoa(c.Counts.Unread)+" unread", row.Meta)
		}
	case viewmodel.SwitchChannel:
		if c.Users > 0 {
			row.Meta = join(strconv.Itoa(c.Users)+" here", row.Meta)
		}
	case viewmodel.SwitchCreate:
		row.Title = "Create " + c.Name
		row.Accent = true
		row.Detail = "a new channel, with topic and mode options"
	}
	return row
}

// networkNote names the network on a row, and says nothing when there is only
// one - the same restraint rule the rail applies to its headers (R-6): with one
// network, every row is on it and saying so on each is noise.
func (u *UI) networkNote(id dispatch.SessionID) string {
	if len(u.sessions) < 2 {
		return ""
	}
	return u.vm.Profile(id)
}

// listCandidates asks every connected network's LIST cache for the channels
// matching what was typed. The bare name is what is asked - a leading '#' is
// eco's sigil and the cache stores it, but somebody typing "eco" means the same
// thing as somebody typing "#eco".
func (u *UI) listCandidates(query string) []viewmodel.Candidate {
	match := strings.TrimLeft(strings.TrimSpace(query), "#&")
	var out []viewmodel.Candidate
	for _, s := range u.sessions {
		for _, ch := range s.Channels(match, switchListLimit) {
			out = append(out, viewmodel.Candidate{
				Session: s.ID(),
				Name:    ch.Name,
				Network: s.Name,
				Users:   ch.Users,
				Topic:   ch.Topic,
			})
		}
	}
	return out
}

// switcherChose is Enter: show the buffer, join the channel, or open the create
// form. It is the one place the three meanings are told apart.
func (u *UI) switcherChose(i int) {
	if i < 0 || i >= len(u.switchRows) {
		return
	}
	c := u.switchRows[i]
	u.closeOverlays()
	switch c.Kind {
	case viewmodel.SwitchBuffer:
		u.selectRef(c.Ref)
	case viewmodel.SwitchChannel:
		if s := u.sessionFor(c.Session); s != nil {
			s.JoinUser(c.Name)
			u.selectRow(s, c.Name)
		}
	case viewmodel.SwitchCreate:
		u.createChannel(c.Session, c.Name)
	}
}

// createChannel is browser.go's create flow, promoted to §4.6's "no match →
// 'Create #name' with the options form". The creator is op, so the topic and
// the modes are applied straight after the join.
//
// It joins the TYPED name and never the top match: with "dev" typed and
// "#developers" listed, this says "Create #dev" and means it. That was
// browser.go's rule and it is the one thing about the old dialog that had to
// survive intact.
func (u *UI) createChannel(id dispatch.SessionID, name string) {
	s := u.sessionFor(id)
	if s == nil || name == "" {
		return
	}
	f := newChannelOptionsForm("", "", "")
	dialog.ShowForm("Create "+name, "Create", "Cancel", f.items(), func(ok bool) {
		if !ok || s.irc == nil {
			return
		}
		s.JoinUser(name)
		for _, c := range f.commands(name) {
			s.irc.Send(c[0], c[1:]...)
		}
		u.selectRow(s, name)
	}, u.win)
}

// join puts two notes in one line, skipping either if it is empty.
func join(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + " · " + b
}
