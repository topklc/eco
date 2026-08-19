# eco

A simple, modern, beginner friendly IRC client written in Go. Built on
[ergochat/irc-go](https://github.com/ergochat/irc-go) with a
[Fyne](https://fyne.io) GUI, designed around [Ergo](https://ergo.chat)'s
IRCv3 feature set. It works with any IRC server, and lights up every
extra feature when the server supports it.

eco was developed with AI assistance.

## IRCv3 capabilities

Every capability below is requested on connect. Unsupported ones are
skipped without error, so eco works against any server.

### Stable capabilities

| Capability | What eco does with it |
|---|---|
| `account-notify` | Tracks account login and logout events |
| `account-tag` | Receives account information attached to messages |
| `away-notify` | Receives away state changes from the server |
| `batch` | Handles grouped server messages (history playback, netsplits) |
| `cap-notify` | Notices when the server adds or removes capabilities |
| `chghost` | Handles host changes without fake quit and rejoin noise |
| `echo-message` | Confirms sent messages. Local display is instant; the echo is deduplicated so nothing shows twice, even on laggy servers |
| `extended-join` | Receives account and real name on join |
| `extended-monitor` | Extended monitor support |
| `invite-notify` | Shows who invited whom into a channel |
| `labeled-response` | Correlates commands with their replies |
| `message-tags` | Full message tag parsing (msgid, time, account, batch) |
| `multi-prefix` | Sees all mode prefixes of every member, not just the highest |
| `sasl` | SASL PLAIN during registration, or SCRAM-SHA-256 (selectable per profile) driven immediately after connect |
| `server-time` | Every message shows its true timestamp, including history |
| `setname` | Handles real name changes |
| `standard-replies` | Displays FAIL, WARN, and NOTE replies |
| `sts` | Strict transport security: advertised policies are remembered per host with expiry, plaintext connections upgrade to TLS automatically on the advertised port, and future connects to that host always use TLS |
| `userhost-in-names` | Parses user@host in NAMES replies |

### Draft capabilities (Ergo extras)

| Capability | What eco does with it |
|---|---|
| `draft/chathistory` | Automatically fetches the last 100 messages when you join a channel, once per channel. `/history` fetches 200 more on demand |
| `draft/event-playback` | Replays historical joins, parts, and other events inside history. Playback is recognized by its batch tag and never mutates live state, so no request storms and no phantom members |
| `draft/message-redaction` | When a message is deleted on the server, it updates live to "(message deleted)". `/redact <msgid> [reason]` deletes your own |
| `draft/multiline` | Negotiated; multiline messages are received and displayed line by line |
| `draft/channel-rename` | When a channel is renamed, the buffer follows it seamlessly |
| `draft/relaymsg` | Negotiated, so relayed messages from bridges display correctly |
| `draft/read-marker` | Negotiated (marker sync is on the roadmap) |
| `draft/languages` | Negotiated; server language preferences are supported |
| `draft/persistence` | Negotiated, for Ergo's always-on client persistence |
| `draft/pre-away` | Negotiated, for cleaner away handling with persistence |
| `draft/account-registration` | `/register` uses the standard REGISTER command with structured success, failure, and email-verification replies; `/verify` completes email codes. Falls back to NickServ on servers without the cap |

## Features

### Connecting and identity

* Simple connect dialog: network name, server, password, and nickname up front; port, TLS, SASL, and auto-join under Advanced
* The Advanced box uses the real protocol names for power users: Port, TLS, SASL account, SASL password, and Auto-join
* Multiple saved network profiles with add, delete, and save
* Sensible defaults: TLS on, port 6697, seeded profiles for the Ergo testnet and Libera.Chat
* SASL login with PLAIN or SCRAM-SHA-256 (chosen per profile under Advanced); failure is nonfatal so you still connect
* Server passwords (PASS) for bouncers such as soju and ZNC: for ZNC use "user/network:password" as the server password, for soju either set the SASL account to "user/network" or use the server password the same way
* Multiple networks at once: each connected network is a root in the sidebar with its channels beneath it, and you switch by clicking (or Ctrl+PageUp/PageDown across everything)
* Pin a network in its profile and it stays in the sidebar even while disconnected, one click reconnects it
* All passwords stored in the system keyring (GNOME Keyring or any Secret Service), never on disk; graceful fallback if no keyring exists
* STS support: advertised policies are remembered per host, plaintext connections upgrade to TLS automatically, and future connects always go encrypted
* An optional TLS-only mode in Preferences that refuses every unencrypted connection (plain TCP and plain ws:// alike)
* IRC over WebSocket: enter a `wss://host/path` (or `ws://`) address in the Server field and eco speaks the IRCv3 websocket transport, one frame per line; wss carries its own encryption
* Automatic reconnect every 30 seconds after a dropped connection
* A quiet "offline" indicator in the top bar whenever the connection is down

### Channels

* Automatic channel scan (LIST) on connect, toggleable
* Joins every channel on the network by default, batched politely, with a safety cap of 75 channels; on bigger networks it opens the channel browser instead
* Channel browser: every channel with user counts and topics, sorted by size, live search across names and topics, click to join
* Scrollback loads automatically on join via chathistory
* Topic display, topic changes, kicks, invites, renames, and mode changes all handled

### Messages

* Full mIRC formatting rendered: all 16 colors, bold, italic, underline, and monospace, in messages and in the server MOTD
* Every person gets their own stable nick color
* Mentions of your nick render in a highlight color and raise a desktop notification; private messages do the same
* Your own messages display instantly, never waiting on server lag
* `/me` actions, NOTICEs, CTCP handling (VERSION replies are automatic)
* Timestamps on every line, sourced from server-time when available

### Input

* Tab completion of nicknames from the current channel, with cycling on repeated Tab presses
* Up and Down arrows walk through the last 100 sent messages, preserving your unsent draft
* Anything that is not a command is simply sent as a message
* An unknown slash command is not sent to the server: eco names the closest command it knows and offers `/raw`, which sends the line untouched — so nothing is ever impossible, and a typo is never a surprise

### Interface

* One settings icon opens the Control Center, a single hub for every management action: preferences, channel browser, join channel, servers and identities, history, disconnect, about, and quit
* Minimalist single window: buffer sidebar with unread counters, message pane, member list sorted by rank, slim top bar
* Both side panes collapse behind toolbar icons (a list icon for buffers, a person icon for members) and the chat area reclaims the space
* Click any member to open a private conversation
* Virtualized message pane: only the rows on screen are drawn, so a ten-thousand-line buffer opens in a tenth of a second and nothing is capped away
* Coalesced rendering: bursts of hundreds of messages cause a single repaint
* Copy a message from its own hover strip or right-click menu; **F6** opens Reader mode, the conversation as selectable plain text (there is no drag-select across messages — the toolkit cannot do it, and eco says so rather than pretending)

### Customization (Preferences)

* Theme: System, Light, or Dark
* Font size slider from 10 to 28 points, default 16; every dialog scales with it so nothing gets cut off
* Your own nick color: Green, Blue, Orange, Purple, Red, Teal, or Pink
* Per person nick coloring, toggleable
* Custom background color with a full color picker
* Custom background image, softly translucent behind the chat
* Show or hide timestamps
* Show or hide join, part, and quit messages
* Channel scan on connect, toggleable
* Join all channels on connect, toggleable
* Desktop notifications, ping sound, and notify-on-every-message, each toggleable
* Toolbar tooltips, toggleable
* TLS-only mode
* Every setting persists between launches

### Commands

Chat: `/join` `/part` `/close` `/msg` `/query` `/me` `/notice` `/ctcp`
`/clear` `/cycle`
Identity: `/nick` `/away` `/back` `/setname` `/register` `/verify` `/identify`
Channel operators: `/topic` `/mode` `/op` `/deop` `/voice` `/devoice`
`/hop` `/dehop` `/ban` `/unban` `/kick` `/kickban` `/invite` `/knock`
Information: `/whois` `/whowas` `/who` `/list` `/names` `/history` `/motd`
`/time` `/version` `/admin` `/info` `/links` `/lusers` `/map` `/rules`
`/stats` `/ison` `/userhost` `/monitor`
Services: `/ns` `/cs` `/hs`
Server operators: `/oper` `/kill` `/wallops`
Application: `/server` `/disconnect` `/redact` `/raw` `/quit` `/help`

Highlights: `/whois` prints its answer in the buffer where you asked.
`/register password [email]` and `/identify password` talk to NickServ.
`/ns`, `/cs`, and `/hs` are shortcuts to Ergo's services. `/server`
reopens the network manager. `/quit` exits the whole app; `/disconnect`
drops the connection but keeps the window.

## System requirements

**Minimum**

* 64 bit Linux (glibc 2.31 or newer, roughly Ubuntu 20.04 / Debian 11 era) or Windows 10
* x86-64 CPU; eco idles at about 1% CPU on a laptop
* 512MB free memory (eco itself uses about 205MB resident)
* 100MB disk space
* OpenGL 2.1 capable graphics; any hardware from 2010 onward qualifies, including integrated graphics

**Notes**

* Linux needs a desktop session (X11 or Wayland). Notifications and the ping sound use the desktop's services when present and degrade quietly when absent. Passwords use the system keyring when one exists
* Windows has no dependencies: a single exe, with the Credential Manager and toast notifications built in
* Headless machines without a GPU or display cannot run eco

## Starting it

Installed at `~/.local/bin/eco` with a desktop entry, so you can
launch it from your app menu (search "eco") or run `eco` in a
terminal.

## Build and test

```sh
make install   # builds and installs the binary, icon, and desktop entry
```

Or by hand:

```sh
go build -o eco .
./eco
```

The app icon is `assets/icon.png` (transparent, 512x512) and is embedded into
the binary at compile time, so every build carries it automatically.

Requires Go 1.23 or newer plus the usual Fyne Linux build dependencies
(gcc and the X11 and OpenGL headers). On this machine `libXxf86vm.so`
is linked from `~/.local/lib`, so build with
`CGO_LDFLAGS="-L$HOME/.local/lib"` if needed.

`go test .` runs two live tests against `testnet.ergo.chat`: one
verifies connection and capability negotiation, the other verifies the
message send and echo path.

## Windows

`./scripts/build-windows.sh` prepares everything and builds `eco.exe`: it
checks for Go, downloads and extracts the MinGW cross-compiler into
`~/.local/mingw` if missing (no root needed), and compiles. Once the
toolchain exists, `make windows` also cross-compiles `eco.exe` (a windowed PE binary for
x86-64) using the MinGW toolchain in `~/.local/mingw`. Everything works
on Windows out of the box: passwords go to the Windows Credential
Manager, notifications use native toasts, and the ping sound uses the
system notification sound.

## Architecture

| File | Purpose |
|---|---|
| `session.go` | IRC protocol layer: connection, capabilities, callbacks, buffers |
| `ui.go` | Fyne interface: layout, rendering, callback wiring |
| `entry.go` | The input widget: tab completion and input history |
| `commands.go` | Slash command handling |
| `networks.go` | Network profiles, keyring storage, the connect dialog |
| `browser.go` | The channel browser |
| `menu.go` | Control Center, preferences, theming |
| `colors.go` | mIRC color palette and formatted text rendering |
