# Commands and keyboard shortcuts

## Keyboard shortcuts

**Enter** sends the message

**Shift+Enter** adds a new line to a message

**Tab** completes a nickname, press again to cycle matches

**Up / Down** walk through previously sent messages

**Ctrl+W** closes the current channel or conversation

**Ctrl+PageUp / Ctrl+PageDown** previous / next buffer

**Alt+1 through Alt+9** jump to buffer 1 through 9

**Alt+A** jumps to the next buffer with unread messages, opening a folded
network if that is where it is

**Ctrl+B** shows or hides the channel list on the left

**Ctrl+U** shows or hides the people list on the right. eco remembers the
answer for each channel and conversation separately.

**F6** opens Reader mode: the conversation as plain text you can select and
copy with the mouse or the keyboard. F6 or **Escape** closes it again.

**Escape** closes an open dialog, then Reader mode.

Right-click a channel or conversation in the sidebar for leave, close, and mark-as-read.
Right-click a network — or use the **⋯** button on its row — for connect,
disconnect, edit, find channels and its log; the two offer the same things. A
network you are not connected to has a **Connect** button on its row, and
clicking the row itself never dials.

Right-click (or hover) a message for Copy message, Copy nick, Quote, and — on
your own messages — Delete. Hover someone in the people list for the same menu
its right-click gives.

## Copying text

eco copies a message at a time, or a run of them through Reader mode. There is
no drag-select across the conversation: the toolkit eco is built on cannot
select text across separate widgets, and every message in the pane is one. So:

* **one message** — hover it and click the copy icon, or right-click it and
  choose *Copy message*. *Copy nick* takes just the name.
* **several messages** — press **F6**. Reader mode replaces the pane with the
  conversation as plain text, in a box that selects and copies like any other:
  drag, Shift+arrows, Ctrl+A, Ctrl+C. It is a still picture — messages keep
  arriving behind it and are there when you close it — and it shows the part of
  the buffer you were reading (up to 500 messages), which it says at the top
  when that is not all of it.
* **a link** — click it. Links in messages open in your browser.

## Commands

Anything that does not start with / is sent as a message; to send a message that
starts with a slash, type the slash twice (`//hello`). An unknown /command is
not sent to the server — eco names the closest command it knows, and `/raw`
sends a line through untouched when that is what you meant.

<!--commands-->

Notable behaviors: `/whois` answers in the buffer where you asked. `/register password [email]` uses the modern account registration when the server supports it and falls back to NickServ. `/redact <msgid>` deletes one of your messages on servers that allow it. `/quit` exits the app entirely; `/disconnect` only drops the connection.
