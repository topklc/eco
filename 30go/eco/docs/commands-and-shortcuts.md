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

Anything that does not start with / is sent as a message. Unknown /commands pass through to the server untouched.

Chat: `/join` `/part` `/close` `/msg` `/query` `/me` `/notice` `/ctcp` `/clear` `/cycle`

You: `/nick` `/away` `/back` `/setname` `/register` `/verify` `/identify`

Channel operators: `/topic` `/mode` `/op` `/deop` `/voice` `/devoice` `/hop` `/dehop` `/ban` `/unban` `/kick` `/kickban` `/invite` `/knock`

Information: `/whois` `/whowas` `/who` `/list` `/names` `/history` `/motd` `/time` `/version` `/admin` `/info` `/links` `/lusers` `/map` `/rules` `/stats` `/monitor`

Services: `/ns` `/cs` `/hs` (NickServ, ChanServ, HostServ)

Server operators: `/oper` `/kill` `/wallops`

Application: `/server` `/disconnect` `/redact` `/raw` `/quit` `/help`

Notable behaviors: `/whois` answers in the buffer where you asked. `/register password [email]` uses the modern account registration when the server supports it and falls back to NickServ. `/redact <msgid>` deletes one of your messages on servers that allow it. `/quit` exits the app entirely; `/disconnect` only drops the connection.
