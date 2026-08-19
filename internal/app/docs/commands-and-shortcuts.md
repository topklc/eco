# Commands and keyboard shortcuts

## Keyboard shortcuts

**Enter** sends the message

**Shift+Enter** adds a new line to a message

**Tab** completes a nickname, press again to cycle matches

**Up / Down** walk through previously sent messages

**Ctrl+W** closes the current channel or conversation

**Ctrl+PageUp / Ctrl+PageDown** previous / next buffer

**Alt+1 through Alt+9** jump to buffer 1 through 9

**Alt+A** jumps to the next buffer with unread messages

**Escape** closes an open dialog

Right-click a channel or conversation in the sidebar for leave, close, and mark-as-read.

## Commands

Anything that does not start with / is sent as a message; to send a message that
starts with a slash, type the slash twice (`//hello`). An unknown /command is
not sent to the server — eco names the closest command it knows, and `/raw`
sends a line through untouched when that is what you meant.

<!--commands-->

Notable behaviors: `/whois` answers in the buffer where you asked. `/register password [email]` uses the modern account registration when the server supports it and falls back to NickServ. `/redact <msgid>` deletes one of your messages on servers that allow it. `/quit` exits the app entirely; `/disconnect` only drops the connection.
