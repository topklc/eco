# Running eco with your own Ergo server

eco was designed around Ergo's IRCv3 feature set, so an Ergo network is
where every part of the client lights up. This guide covers both sides
of the wire: what to set in `ergo.yaml`, and what eco does with it.
Config paths refer to Ergo's default configuration layout; after any
change, `/rehash` from an oper session (or restart Ergo) applies it.

## History and scrollback

eco requests `draft/chathistory` and `draft/event-playback` on every
connection. When the server grants them, eco fetches the last 100
messages automatically the first time you enter a channel, and
`/history` (or right-click the messages and pick Load older messages) pulls 200
more on demand. Timestamps come from `server-time`, so replayed
messages show when they were really said.

On the Ergo side this needs the `history` block enabled:

`history:`
`    enabled: true`
`    channel-length: 2048`
`    client-length: 256`
`    autoresize-window: 3d`
`    chathistory-maxmessages: 1000`

Notes for eco specifically:

- `chathistory-maxmessages` caps a single CHATHISTORY response. Keep it
  at 200 or higher so eco's `/history` requests come back full.
- `channel-length` is the real depth of your scrollback. eco keeps up
  to 2000 lines per buffer in memory, so a `channel-length` in the low
  thousands pairs well.
- Leave `autoreplay-on-join` off or low. eco fetches history itself and
  deduplicates by message id, so double delivery is harmless, but there
  is no reason to send everything twice.
- For persistent history across server restarts, configure the
  `datastore` and set `history: persistent` options per your retention
  policy. eco does not care where the history lives, only that
  CHATHISTORY answers.

## Multiline messages

eco composes multiline messages with Shift+Enter and sends them as one
`draft/multiline` batch. The line counter in the compose bar and the
truncation warning both follow whatever the server advertises in the
capability value, read live at negotiation time. eco never hardcodes
the limit.

Ergo advertises the values from its `limits` section:

`limits:`
`    multiline:`
`        max-bytes: 4096`
`        max-lines: 24`

Raise `max-lines` and reconnect, and eco's counter ceiling follows
automatically. Remember each multiline message relays as one atomic
batch, so very large limits are a flood lever in busy channels.

## The colored MOTD

eco renders mIRC formatting everywhere, including the server buffer,
and shows the MOTD in monospace so ASCII art lines up. If your MOTD
shows literal `$c` and `$b` codes, that is Ergo sending them as plain
text because formatting is disabled. Enable it:

`server:`
`    motd: ergo.motd`
`    motd-formatting: true`

The dollar codes in the MOTD file then convert to real IRC formatting,
and eco displays the colors.

## STS, TLS, and websockets

### STS

eco implements the `sts` capability fully: on a TLS connection it
persists your advertised policy per host and refreshes the expiry every
visit, and it will refuse to connect that host in plaintext while the
policy is live. On a plaintext connection it upgrades immediately to
the advertised port but, per the spec, does not persist attacker
visible policy data. Configure Ergo:

`server:`
`    sts:`
`        enabled: true`
`        duration: 1mo2d5m`
`        port: 6697`

With this set, a user who mistakenly configures your network without
TLS gets bounced to an encrypted connection automatically and stays
encrypted from then on.

### Websocket listeners

eco connects over the IRCv3 websocket transport when the Server field
is a `ws://` or `wss://` URL. On the Ergo side, add a websocket
listener, with TLS for wss:

`server:`
`    listeners:`
`        ":8097":`
`            websocket: true`
`            tls:`
`                cert: fullchain.pem`
`                key: privkey.pem`

Then the eco server field is `wss://irc.example.com:8097`. If you
terminate TLS at a reverse proxy instead, point eco at the proxy URL
and set the listener to plain websocket behind it.

One important note on `allowed-origins`: that setting exists to protect
against hostile web pages connecting from browsers, and it filters on
the Origin header browsers send. eco is a native client and sends no
Origin header at all, which Ergo accepts regardless of the
`allowed-origins` list. You do not need to add anything for eco.

## Accounts

### Registration from inside eco

eco requests `draft/account-registration`. When Ergo grants it,
`/register password` registers the current nickname as an account using
the standard REGISTER command, and eco translates the structured
replies into plain language, including the email verification flow:

`accounts:`
`    registration:`
`        enabled: true`
`        email-verification:`
`            enabled: true`
`            sender: "admin@example.com"`
`            require-tls: true`

With email verification on, eco tells the user to check their mail and
type `/verify code`. With it off, `/register password` completes
immediately and logs the user in. On servers without the capability eco
falls back to messaging NickServ, so the same command works everywhere.

### Logging in

The network profile's Account password field drives SASL. eco defaults
the SASL account name to the nickname, which matches Ergo's model where
your account and primary nick are the same thing. PLAIN runs during
connection registration; SCRAM-SHA-256 (selectable per profile under
Advanced) runs immediately after connect and verifies the server knows
your credentials too.

SCRAM caveat that catches every Ergo admin once: Ergo can only do SCRAM
for accounts whose credentials are stored in its modern passphrase
format. Accounts registered on older Ergo versions fail SCRAM until the
password is set again, which rewrites the stored credential:

`/ns PASSWD oldpassword newpassword newpassword`

After that, SCRAM works for that account.

### Always-on and persistence

eco negotiates `draft/persistence` and `draft/pre-away`. Those matter
when your users enable Ergo's always-on mode, where the server keeps
their presence alive between client connections:

`accounts:`
`    multiclient:`
`        enabled: true`
`        always-on: opt-in`
`        auto-away: opt-in`

With always-on enabled for an account, closing eco does not part
channels or drop the nick; reopening eco and connecting replays what
was missed via chathistory. This is the closest thing to a built-in
bouncer, and for an Ergo-only community it removes the need for ZNC or
soju entirely.

## Operating your network from eco

### Becoming an oper

`/oper name password` works from the eco input line, and because eco
displays every server numeric in the server log, the confirmation (and
any failure reason) is visible there. Define opers in `ergo.yaml` under
the `opers` block with a hashed password from `ergo genpasswd`.

### Moderation

The mode shortcuts operate on the channel you are viewing: `/op`,
`/deop`, `/voice`, `/devoice`, `/hop`, `/dehop` take one or more nicks,
`/ban` and `/unban` accept a bare nick (eco expands it to
`nick!*@*`) or a full mask, `/kick` takes a nick and optional reason,
and `/kickban` does both at once. `/mode` passes anything else through
untouched.

### Message redaction

`/redact msgid reason` sends the standard REDACT command, and every eco
client in the channel replaces the deleted message with a tombstone
live. This requires Ergo 2.13 or newer with history storage enabled,
and Ergo's own rules decide who may delete what (authors and channel
operators, per its configuration). Message ids are visible to eco
internally; the common flow is redacting via another client or bot that
surfaces ids, or using `/history` context. Surfacing msgids in the eco
UI is a reasonable future feature request.

### Watching the server

Because eco surfaces every numeric it does not specifically handle,
oper-facing output such as `/stats`, `/lusers`, `/map`, and Ergo's
snomask notices all land readably in the server buffer, which is the one
place eco still sets messages in a fixed-width face, so columns from the
server line up. Reader mode (**F6**, or the copy icon) is how you paste that
output somewhere else.

## Tuning a small community network

eco's default behavior joins every channel when the network has 75 or
fewer, which turns a small Ergo instance into something closer to a
community chat app: new users see every room immediately. Settings that
pair well with that shape:

- Keep the channel count meaningful. Join-all is a feature when every
  channel matters and noise when half are dead. Drop unused channels
  with ChanServ.
- Register your channels (`/cs register #channel`) so topics, modes,
  and op grants survive restarts.
- Enable history persistence so newcomers joining every channel land in
  living conversations instead of empty scrollback.
- Consider `always-on: opt-in` so regulars keep presence without a
  bouncer.
- Set `motd-formatting: true` and write a colorful MOTD; with eco's
  monospace server log it is your network's front page.
- If you outgrow 75 channels, eco users can disable join-all in
  Preferences and lean on the channel browser, which reads your LIST
  output sorted by size.
