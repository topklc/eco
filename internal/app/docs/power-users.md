# eco for power users

Written for people arriving from irssi, WeeChat, or a decade of ZNC
configs. This documents what eco actually does on the wire, verified
against the implementation, not marketing prose.

## Command semantics

Anything not starting with `/` is a message to the current buffer. A
leading `//` sends a literal `/`. **Unknown slash commands pass through
raw**: `/foo bar baz` becomes the line `FOO bar baz`. Note the
passthrough does not add a trailing colon, so multi word final
parameters only survive on commands eco knows about; for anything
exotic use `/raw` (alias `/quote`) and write the exact line yourself,
colon included: `/raw PRIVMSG #chan :hello world`.

### Targeting rules

Commands that take a channel default to the **current buffer**:
`/topic`, `/names`, `/who`, `/kick`, `/invite nick`, `/history`,
`/redact`, all the mode shortcuts, `/cycle`, `/knock` excepted (it
takes an explicit channel). `/part` with no argument parts the current
channel; with an argument it parts that one without switching.

### Mode shortcuts

`/op`, `/deop`, `/voice`, `/devoice`, `/hop`, `/dehop` accept multiple
nicks and build one MODE line: `/op alice bob carol` sends
`MODE #chan +ooo alice bob carol`.

`/ban target` and `/unban target`: if the argument contains any of
`! * @` it is used as the mask verbatim, otherwise a bare nick becomes
`nick!*@*`. So `/ban *!*@198.51.100.7` works as written and
`/ban troll` sets `troll!*@*`. This is a nick mask, not an account or
host ban; craft your own mask for anything stickier.

`/kickban nick [reason]` sets `nick!*@*` then kicks, two lines, ban
first so the rejoin race is closed.

`/mode` is raw-ish: `/mode #chan +ntk secret` splits on whitespace and
sends as parameters, no trailing colon needed for mode arguments.

### Channel lifecycle

`/cycle` (alias `/rejoin`) parts and rejoins the current channel while
**keeping the buffer and its scrollback**: eco marks the channel
internally so the self PART does not close the window, clears the
member list, and the rejoin repopulates it. `/close` on a channel is
`/part`; on a query it just drops the buffer. `/clear` wipes the
displayed history of the buffer client side and also forgets its
duplicate suppression state, so a following `/history` refills it from
the server.

### Message redaction

`/redact <msgid> [reason]` sends `REDACT #chan msgid` (Ergo
`draft/message-redaction`). Msgids arrive on every message via
`message-tags`; eco stores them and uses them for dedup, but does not
currently display them, so in practice you get msgids from another
client, from `/raw`-level debugging, or you redact programmatically.
Incoming redactions edit the affected line to `(message deleted)` in
place.

### Oper hygiene

`/oper name password` sends OPER with exactly two parameters. `/kill
nick [reason]` and `/wallops text` handle their multi word trailing
parameter correctly (this is why they exist as built-ins instead of
relying on passthrough). All numeric replies the client does not
specially handle, including `381`, land in the server buffer, so oper
command feedback is visible.

## Authentication

Three independent mechanisms, all configured per network profile.

### SASL PLAIN

The default when an **Account password** is set. Runs during connection
registration (between CAP negotiation and 001), which means you are
identified before any channel join, before autojoin, before join-all.
If **SASL account** is empty the account defaults to your nickname,
which is Ergo's model. Failure is deliberately nonfatal: you still
connect, unauthenticated, with the failure printed in the server
buffer. PLAIN transmits the password (base64, not encryption); use it
over TLS only. eco warns loudly in the server buffer if credentials are
about to leave over an unencrypted transport.

### SCRAM-SHA-256

Selectable per profile under Advanced. The IRC library only speaks
PLAIN and EXTERNAL in-registration, so eco drives SCRAM itself
**immediately after registration completes** (the `sasl` cap permits
post-registration AUTHENTICATE; Ergo supports this). Consequences worth
knowing: for a few hundred milliseconds after connect you are not yet
identified, so `+R` channels in your autojoin may reject the first
join, and Ergo's always-on machinery sees you identify slightly late.

What you gain: the password never crosses the wire, only a salted
proof, and the exchange is **mutually authenticating**: the server's
final message contains a signature proving it also knows your
credential material, which eco's SCRAM library verifies and aborts on
mismatch (`AUTHENTICATE *`). A server that merely stole your account
name cannot fake that. The conversation handles 400 byte AUTHENTICATE
chunking in both directions. Ergo note: SCRAM only works for accounts
whose credentials are stored in the newer passphrase format; old
accounts need one `/ns PASSWD` cycle first, otherwise you get a 904.

### Server password (PASS)

Advanced field, sent as `PASS` before registration. This is not account
auth on modern networks; it exists for bouncers and for servers with a
connection password. See the bouncer recipes.

## Bouncer recipes

eco's multi-network support maps one profile to one bouncer network.
Pin each profile (**Pin to the sidebar** in the profile) and every
bouncer network keeps a permanent sidebar root you can reconnect with
one click.

### ZNC

One profile per ZNC network:

- **Server**: your ZNC host, **Port**: your ZNC port, TLS on
- **Server password (PASS)** under Advanced: `user/network:password`,
  for example `alice/libera:hunter2`
- Leave Account password empty unless you also want SASL to the
  remote network through ZNC's sasl module

### soju

One profile per soju network, SASL is the native path:

- **Account password**: your soju password
- **SASL account** under Advanced: `user/network`, for example
  `alice/oftc`
- PLAIN mechanism; soju terminates SASL itself

With either bouncer, chathistory playback dedup matters: bouncers
replay history on attach, and eco suppresses anything whose msgid it
has already displayed, so repeated attach/detach cycles do not
duplicate your scrollback.

## Transport

### TLS

Certificate verification is always on; there is no insecure-skip
toggle, deliberately. SNI is set from the hostname, minimum version is
pinned to TLS 1.2. Default port 6697.

### STS

eco honors the IRCv3 `sts` capability. On a TLS connection that
advertises a policy, the policy (port plus expiry duration) is
**persisted per host**, and every future connection to that host is
forced to TLS regardless of the profile, until expiry. Each secure
connection refreshes the timer; an advertised `duration=0` revokes.
On a **plaintext** connection an advertised policy triggers an
immediate reconnect over TLS on the advertised port, but is **not
persisted**: on plaintext the advertisement is attacker controlled,
and persisting it would let a MITM pin a bogus policy (this is the
spec's own requirement, implemented correctly here). The residual risk
is the classic trust-on-first-use window; close it by just enabling TLS
in the profile, which is the default anyway.

### TLS-only mode

Preferences, Security: refuses every unencrypted connection, plain TCP
and plain `ws://` alike, with a dialog instead of a connect. STS
policies bypass nothing here; they only ever make things stricter.

### WebSocket

Put a URL in the Server field: `wss://irc.example.org/webirc`. eco
speaks the IRCv3 websocket transport: one IRC line per text frame, no
CRLF inside frames, subprotocol `text.ircv3.net`, no Origin header (so
Ergo's `allowed-origins` for browsers does not apply to eco). Frames
from the server are capped at 512KB. `wss` carries its own TLS
(verified, same rules); `ws://` is treated as unencrypted for the
TLS-only check. STS logic is skipped for websocket URLs since the
scheme already pins the transport.

## IRCv3 behavior in detail

### chathistory and playback

On joining a channel eco requests `CHATHISTORY LATEST <chan> * 100`,
**once per channel per connection**. `/history` fetches 200 more at any
time. Two layers keep this sane: anything carrying a `batch` tag is
treated as **playback** and never mutates live state (no member list
changes, no history re-requests, so no request storms against
`draft/event-playback` servers), and every displayed msgid is
remembered per buffer, so overlapping fetches and bouncer replays never
duplicate lines.

Scrolling to the top of a buffer asks the **local archive** for the
page above it, and only when the archive has nothing older does it
issue `CHATHISTORY BEFORE msgid=<oldest>` — so re-reading yesterday
costs one local query rather than a round trip, and works on a server
that has already forgotten it.

A **reconnect** does not replay: eco issues `CHATHISTORY AFTER` the
newest message it already holds, per open buffer, plus `CHATHISTORY
TARGETS` over the gap so private messages that arrived while you were
away are found at all. A channel named by TARGETS is deliberately not
reopened — it announces itself by being rejoined, and reopening every
channel you have ever left on every reconnect is the failure that rule
avoids. The whole catch-up is reported as one line in the network log:
*"Reconnected — caught up 12 messages."*

### read markers

Showing a buffer, leaving it, or marking it read from the row menu
moves its read marker and sends `MARKREAD`, so your other clients agree
about what you have seen; a `MARKREAD` from one of them moves it here.
Conflicts resolve to the later position, in every direction — server,
archive and memory — so a device that was behind can never mark
something unread again. **Unread counts are derived from that marker
and from nothing else**, which is why they survive a restart: the
marker is stored with the history.

### the archive

One SQLite file per network profile in `eco/archive/`, written behind a
queue so the UI never waits for the disk, with an FTS5 index over
message bodies. Messages dedupe on msgid; a `REDACT` deletes the row
and its index entry and leaves a tombstone. `LIST` results stream into
the same file rather than living in memory, which is what lets the
channel browser open instantly and work before the first scan of a
session finishes. Retention (forever / a year / 90 / 30 days / session
only), the per-buffer opt-out and the "never save private messages"
switch are all described under **Your history** in the getting started
guide. The file is not encrypted; that is documented rather than
solved.

### echo and local display

Your own messages display instantly without waiting for the server's
`echo-message`. The echo, when it arrives, is matched against a pending
set and dropped, so nothing doubles even on a server applying heavy
fakelag. Practical effect: typing never feels laggy, and the message
you see is what you sent, not yet what the network accepted; rejection
errors surface in the server buffer.

### multiline

Multi line input (Shift+Enter) is sent as one `draft/multiline` batch
when the server supports it, otherwise as separate PRIVMSGs. The line
cap is read live from the cap advertisement (`max-lines`, Ergo default
24); eco truncates with a warning rather than sending a rejectable
batch. Incoming multiline batches are reassembled into one message
block, honoring `draft/multiline-concat` for continuation without a
break. CTCP ACTIONs cannot contain newlines, so `/me` flattens them.

### capability posture

eco requests every stable cap plus the Ergo draft set
(`draft/chathistory`, `draft/event-playback`,
`draft/message-redaction`, `draft/multiline`, `draft/channel-rename`,
`draft/relaymsg`, `draft/read-marker`, `draft/languages`,
`draft/persistence`, `draft/pre-away`, `draft/account-registration`).
Requests are non-fatal, so the same binary behaves like a classic
client on a 2005-era ircd. `draft/channel-rename` is fully wired: a
renamed channel's buffer follows the rename. Account registration
prefers the standard `REGISTER`/`VERIFY` commands with structured
replies and falls back to NickServ.

## Keyboard efficiency

- **Ctrl+PageUp / Ctrl+PageDown**: previous and next sidebar row,
  across all connected networks, wrapping
- **Alt+1 .. Alt+9**: jump to sidebar row N
- **Alt+A**: next buffer with unread, any network
- **Ctrl+W**: close current buffer (part if channel, drop if query)
- **Tab**: nick completion against the current channel's member list,
  prefix match, repeated Tab cycles candidates; at line start it
  appends `: `, mid-sentence a space; typing anything resets the cycle
- **Up / Down**: last 100 sent lines, most recent first, with your
  unsent draft preserved at the bottom of the stack; inside a multiline
  draft the arrows navigate the draft instead
- **Shift+Enter**: newline (input grows to 5 rows, then scrolls)
- **Escape**: closes the open dialog

Drafts are **per buffer, per network**: half-typed text stays with its
conversation when you switch, and cannot follow you into the wrong one.
All shortcuts work while the input is focused, which is always. The
shortcuts operate over the row list you see, so with three networks
connected Alt+2 might be a network root; selecting a root shows that
network's server log, and selecting anything switches the active
network for input routing, the status dot, and the member pane.
