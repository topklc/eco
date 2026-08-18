# Lessons learned building eco

Everything this project taught us, from first line to shippable
binaries. Written for whoever works on eco (or any Fyne or IRC project)
next.

## Fyne, the sharp edges

- **RichText cannot be selected.** No released Fyne version supports
  mouse selection in rich text. The only selectable widget is Entry,
  which is single-style. eco's answer is plaintext mode: a toggle that
  swaps the colored pane for a read-only monospace Entry.
- **RichText refresh cost is O(visible segments).** Never refresh per
  message. Coalesce (eco batches on an 80ms tick), cap the rendered
  window (250 lines) independently of retained history, and gate the
  refresh on whether visible content actually changed. A busy background
  channel must not repaint the foreground.
- **The markdown renderer chokes on tables and fenced code blocks.**
  Tables render through a nested construct that swallows wheel events;
  fenced code becomes non-wrapping monospace grids. Ship docs as
  headings, bold lines, and per-line inline code.
- **External Scroll around RichText breaks wheel scrolling.** Use the
  widget's own Scroll field (fyne.ScrollVerticalOnly).
- **Implementing TappedSecondary swallows left clicks.** The driver
  routes ALL clicks to any widget implementing any tap interface. A
  widget with a right-click menu must also implement Tapped and forward
  it, or plain clicks die silently.
- **SetChecked and SetSelected fire their callbacks.** Building a
  settings dialog with callbacks attached before initial values means
  one full apply per control at open. Initialize first, attach second.
- **Hidden Border-layout panes do not give space back on their own.**
  Refresh the parent containers after Hide/Show or the layout keeps the
  reserved space.
- **AppTabs builds every tab eagerly.** Six markdown documents parsed
  before first paint is visible lag. Lazy-render on tab selection.
- **The stock dialog dismiss button stretches to dialog width** at
  larger sizes; NewCustomWithoutButtons plus your own compact
  bottom-right button looks right.
- **Dialogs do not scale with font size.** A GridWrap sized at a 16pt
  baseline clips at 24pt. Scale dialog sizes by fontsize/16.
- **fyne.Do is mandatory** for every UI mutation from a non-UI
  goroutine, and shortcuts reaching a focused Entry need TypedShortcut
  forwarding on the entry itself, because canvas shortcuts do not fire
  while a Shortcutable widget has focus.
- **The built-in color picker renders poorly** (low-res raster wheel)
  and the built-in file dialog is nonnative. ncruces/zenity gives the
  real OS dialogs for both, one library, cross-platform.
- **Theme Color() is called constantly during paint.** Anything beyond
  a map hit (string parsing, prefix checks) in that path is a real cost.
- **fyne-tooltip** (dweymouth) provides the tooltips core Fyne lacks;
  wrap the window content in its tooltip layer.
- **Fyne notifications fail silently on some Linux desktops.** Shell
  out to notify-send when present (with "--" before positional args, a
  message starting with a dash must never become a flag), fall back to
  Fyne's path otherwise.

## IRC and IRCv3, the protocol lessons

- **Event playback will bite you exactly once.** History playback
  includes JOIN events; treating a replayed self-JOIN as real triggered
  a chathistory request storm that tripped server fakelag and made the
  client look frozen. Anything with a batch tag must never mutate
  member state or trigger requests.
- **But live multiline messages ALSO arrive in batches.** Batch tag
  alone cannot mean "playback": live multilines were invisible to
  highlighting for weeks. Distinguish by per-message msgid (playback
  has one, multiline parts do not) plus a recency window.
- **Local echo first, always.** Display sent messages immediately and
  dedupe the server echo (echo-message) when it arrives; on a fakelagged
  server the echo can be seconds late and typing must never feel broken.
- **Dedupe by msgid per buffer.** Repeated /history fetches and
  overlapping server playback will re-send messages; a bounded seen-set
  makes redisplay impossible and makes history refetch after reconnect
  safe.
- **Auto-reconnect does not rejoin.** The library reconnects the socket
  and nothing else; rejoin open channels yourself and refetch history
  (the msgid dedupe makes that free of duplicates).
- **The raw passthrough has no trailing colon.** Unknown /commands
  forwarded raw break on multi-word final arguments; commands like
  /wallops and /kill need explicit handling through the proper
  serializer.
- **Numerics you do not display are commands that look dead.** /oper
  looked broken because 381 landed nowhere. Route every unhandled
  numeric to the server log; allowlist only the noisy handled ones.
- **STS: persist policies only from secure connections.** A plaintext
  advertisement is attacker-controlled; honoring its duration lets a
  MITM pin a bogus policy. Upgrade immediately from plaintext, persist
  only over TLS.
- **SCRAM in ircevent does not exist**; PLAIN and EXTERNAL only, during
  registration. Post-registration AUTHENTICATE (which Ergo permits)
  plus xdg-go/scram covers SCRAM-SHA-256, with the caveat that Ergo
  accounts predating the newer credential format need a password reset.
- **server-time is not always milliseconds.** Parse RFC3339Nano, not a
  fixed layout, or non-Ergo history gets stamped "now".
- **mIRC colors assume white paper.** Navy, maroon, and friends are
  invisible on dark backgrounds; raw yellow and white are invisible on
  light. Ship two palettes with shared hue identity and resolve by
  theme variant, after variant forcing.
- **Websocket IRC is one line per text frame, no CRLF**, subprotocol
  text.ircv3.net, adapted to a net.Conn by inserting CRLF on read and
  splitting on write. Cap frame size; the IRC line limit does not
  protect you before the frame is fully read.

## Architecture decisions that held up

- **Session/UI split with a callbacks struct.** The Session knows no
  Fyne; the UI wires UICallbacks. This made the multi-network refactor
  tractable and lets the session layer be tested against the real
  network headlessly.
- **Callbacks bound to their session with a staleness guard.** A dying
  connection firing events into a UI that moved on caused ghost state;
  every callback checks its session is still the live one.
- **Live tests against the real testnet** (connect and caps, send and
  echo) under the race detector caught more than any amount of reading;
  they gate every change.
- **State keyed by session and buffer (skey), and the key must be the
  buffer actually shown**, not recomputed from possibly-reassigned
  globals; the draft misfiling bug came from exactly that gap.
- **Preferences as flat keys with instant apply** stayed manageable;
  atomics bridge settings read by the protocol goroutine.
- **Single source docs.** The markdown files are the repo docs, the
  shipped docs, and (via go:embed) the in-app help. Edit once.

## Process lessons

- **Fresh-eyes audits keep paying.** Round after round on "finished"
  code found real bugs every time: the reconnect-rejoin hole and the
  delete-corrupts-neighbor bug survived three prior audits.
- **Expert panels work when they must ground claims in source.** The
  UX, design, and docs agents were required to read the code first;
  the one time an agent reviewed from memory it proposed a fix for
  something already shipped. Debate rounds killed weak proposals
  before they reached the user.
- **Lossless optimization needs the word lossless enforced.** Marking
  tradeoff items (symbol stripping, icon downsampling) separately from
  free wins kept trust.
- **Measure before believing.** The "slow preferences" was callbacks
  firing during construction; the "lag" was keyring DBus in the render
  path. Both were invisible in the code review and obvious once traced.
- **Disjoint file ownership lets agents fix in parallel safely**;
  cross-file issues get reported to an integrator instead of fixed
  blind.
- **Debug logging to a file beat all speculation** for the
  cannot-send-messages bug: three trace lines located a protocol storm
  that four theories had missed.

## Environment quirks (this machine)

- Go toolchain lives at ~/go (not in PATH); build with
  GOPATH=~/gopath and CGO_LDFLAGS="-L$HOME/.local/lib" (local symlink
  for libXxf86vm, no sudo available).
- MinGW for Windows cross-builds extracted into ~/.local/mingw from
  Debian packages, no root; scripts/build-windows.sh bootstraps it.
- GNOME Keyring is present; zenity provides native dialogs; notify-send
  works; paplay plays the ping.

## Still open, deliberately

- Five-item UI polish plan (devdocs/ui-polish-plan.md) awaiting
  approval.
- Redaction has no UI for msgids; a right-click delete would fix it.
- Tradeoffs declined so far: icon downsample, -s -w stripping.
- No git repository yet; recommended repeatedly.
- Read-marker sync and typing indicators are negotiated but unused.
