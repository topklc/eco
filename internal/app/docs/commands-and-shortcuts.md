# Commands and keyboard shortcuts

## Keyboard shortcuts

This list is generated from the keyboard itself, and shows the keys **you** have
bound: change one in Preferences → Shortcuts and this page changes with it.

<!--shortcuts-->

The switcher (Ctrl+K) ranks every channel and conversation you are in, the
channels you are *not* in from the last channel scan, and, when nothing is
called what you typed, an offer to create it, so joining a room and switching
to one are the same keystroke.

The in-buffer search colours its matches where they stand; **Enter** and
**Shift+Enter** step forward and back through them, and when you step past the
oldest one on screen eco reaches into its history file and opens the next one
where it was said. The finder searches everything eco remembers, and its query
understands `from:nick`, `in:#channel`, `before:2026-01-31`, `after:2026-01-31`,
`on:2026-01-31`, `"quoted phrases"` and `prefix*`; everything else is a word
that has to appear.

Right-click a channel or conversation in the sidebar, or use the **⋮** button
in the header of the one you are reading, which offers the same list, for its
notification level (all messages, only mentions, or mute), a pin on one of the
Alt digits, mark-as-read, leave and close.
Right-click a network, or use the **⋯** button on its row, for connect,
disconnect, edit, find channels and its log; the two offer the same things. A
network you are not connected to has a **Connect** button on its row, and
clicking the row itself never dials.

Right-click (or hover) a message for Copy message, Copy nick, Quote, and, on
your own messages, Delete. Hover someone in the people list for the same menu
its right-click gives.

## Copying text

eco copies a message at a time, or a run of them through Reader mode. There is
no drag-select across the conversation: the toolkit eco is built on cannot
select text across separate widgets, and every message in the pane is one. So:

* **one message**, hover it and click the copy icon, or right-click it and
  choose *Copy message*. *Copy nick* takes just the name.
* **several messages**, press **F6**. Reader mode replaces the pane with the
  conversation as plain text, in a box that selects and copies like any other:
  drag, Shift+arrows, Ctrl+A, Ctrl+C. It is a still picture, messages keep
  arriving behind it and are there when you close it, and it shows the part of
  the buffer you were reading (up to 500 messages), which it says at the top
  when that is not all of it.
* **a link**, click it. Links in messages open in your browser.

## Commands

Anything that does not start with / is sent as a message; to send a message that
starts with a slash, type the slash twice (`//hello`). An unknown /command is
not sent to the server, eco names the closest command it knows, and `/raw`
sends a line through untouched when that is what you meant.

<!--commands-->

Notable behaviors: `/whois` answers in the buffer where you asked. `/register password [email]` uses the modern account registration when the server supports it and falls back to NickServ. `/redact <msgid>` deletes one of your messages on servers that allow it. `/quit` exits the app entirely; `/disconnect` only drops the connection.
