// Package model holds the types shared between the protocol, state, render
// and UI layers. It is pure: no fyne, no network, no globals.
package model

import "time"

// Line is one rendered message row: what the session layer hands to the UI
// and what the render layer turns into widgets.
type Line struct {
	Time      time.Time
	Nick      string // empty for status lines
	Text      string
	Action    bool // /me
	Notice    bool
	Status    bool // joins/parts/errors etc.
	MsgID     string
	Self      bool
	Highlight bool // your nick was mentioned, or it is a PM to you
	Playback  bool // replayed history, not a live message
}
