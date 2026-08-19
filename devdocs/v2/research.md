# eco v2, expert research panel findings

Eleven-expert panel output feeding the v2 design council. Generated 2026-08-18.


---

## Senior UI/visual designer (Fyne-native visual system for eco v2)

### Top findings
- forcedTheme (menu.go) is already a proto token system: synthetic fyne.ThemeColorName keys ('irc0'-'irc98', 'ircx0'-'ircx98', 'nickself') resolve through one Color() lookup, plus a pure-neutral dark surface ramp (0x14 window / 0x1B input / 0x1F overlay / 0x23 menu). This is the correct Fyne pattern and the seed of v2's token layer, but it is incomplete: dotGreen/dotOrange/dotRed, swatch colors, and the welcome screen hardcode NRGBA at call sites and bypass theming entirely.
- The sidebar (ui.go bufferList) is a flat widget.List of Labels faking structure with text: indentation via four literal spaces, unread as an appended '(3)' string, offline as a '(offline)' suffix, mentions as WarningImportance, connection state absent from rows. There are no badges, activity dots, or tree affordances, everything a modern chat sidebar signals visually is encoded as string concatenation.
- The message pane is one monolithic RichText holding every visible line's segments (capped at 250 lines / 750 segments). Density is deliberately irssi-tight (SizeNameLineSpacing forced to 0, a good, defended choice), but the architecture forecloses row anatomy: no hover affordances, no per-row actions, no unread divider, no day separators, and text selection is impossible, hence the parallel 'plaintext mode' toggle that swaps in a MultiLineEntry, a maintenance-heavy second renderer of the same data.
- Typography is split-personality: channels render in the proportional theme font while the server buffer forces monospace, and gridline.go's art path assumes mono metrics. The app already ships DejaVu Sans Mono Bold/BoldOblique (embedded in menu.go) specifically because Fyne ignores the Bold flag for monospace, the hard part of committing to mono chat is already done.
- The mIRC color engine (colors.go/gridline.go) is genuinely excellent and rare: dual readability-tuned 16-color palettes per variant, the exact modern-IRC extended table, exact-fidelity 'ircx' names for text over painted backgrounds, the bgTextSegment stack hack for RichText's missing background support, and TextGrid-based terminal-grid art lines. Nothing in the ecosystem replicates this; it is v2's crown jewel.
- Personalization depth is unusual and well-engineered: custom background color flips the theme variant by luminance so unreadable combinations are impossible, accent derives Primary/Hyperlink/Focus/Selection with tuned alphas, background image with opacity slider, per-point font sizing (10-28pt) with dialogSize() scaling dialogs to match. These are differentiators, keep the mechanics.
- Preferences (prefs.go) has good bones, instant-apply, lazy-rendered tabs, zenity native color picker because Fyne's wheel 'renders badly', but AppTabs with 7 tabs in a fixed 640x480 GridWrap dialog is already at capacity, and tab labels will truncate at larger font scales.
- Empty states exist only at app level (welcome logo + connect button). An empty channel, empty member list, zero-result channel browser, and freshly joined buffer all render as blank panes, the exact moments a beginner-targeted client most needs orientation.
- Connection status lives in a 12px canvas.Circle whose meaning is tooltip-only, invisible on mobile-ish setups and undiscoverable; disconnected state instructions ('use the gear menu') live in a hover tip.
- Fyne 2.8 verified capabilities for v2: widget.List.SetItemHeight(id, h) enables variable-height rows (list.go:205), widget.Tree exists for network/channel hierarchy, container.NewThemeOverride allows per-surface token scoping (e.g. a darker sidebar), desktop.Hoverable works on custom row widgets. RichText still has no user-facing selection and no per-segment hover, those require leaving the single-RichText architecture.

### Recommendations
- 1. Promote forcedTheme into a full semantic token layer, still as synthetic theme color names resolved in Color(): surface tokens (bg/raised/input/overlay, the existing dark ramp, plus a light ramp), text tokens (primary/secondary/faint = ColorNamePlaceHolder replacement), state tokens (ok/warn/danger for the status dot, unread, mention, self), keeping ircN/ircxN untouched. Route every hardcoded NRGBA in ui.go through it. Use container.NewThemeOverride to give the sidebar a one-step-darker surface than the chat pane, visual hierarchy Fyne can actually deliver.
- 2. Rebuild the message pane as a widget.List of per-message row widgets (one RichText or artLine per row, heights via SetItemHeight computed from wrapped text). This unlocks the whole v2 row anatomy: muted mono timestamp gutter, colored bold nick column, wrapped body, desktop.Hoverable row highlight with a small hover action cluster (copy/reply/menu), an unread divider row, and day-separator rows. It also kills plaintext mode: per-row copy + a 'copy message' context item replaces the parallel Entry renderer. Fallback if List proves too costly for 250-line churn: keep the single RichText but add a per-line right-click via a custom RichText subclass, accept no hover affordances in that world.
- 3. Commit to monospace for the entire message pane (channels included), DejaVu Sans Mono + shipped Bold/BoldOblique; proportional theme font stays for all chrome (sidebar, top bar, dialogs, forms). This aligns the timestamp gutter for free, makes pasted art safe everywhere, matches the irssi-dense identity, and ends the channel/server font split. Pad or right-align nicks to a soft column (min 8 cells, no hard truncation) for irssi-style scannability.
- 4. Sidebar v2 rows as a custom widget (HBox: 8px status/activity dot via canvas.Circle, name label, spacer, unread pill = rounded canvas.Rectangle + count in a custom mini-widget): neutral pill for unread, accent/mention-token pill for highlights, dot colors from state tokens showing per-network connection state inline. Keep widget.List (flat, stable, cheap) with indent via real padding, or adopt widget.Tree only if collapsible networks is a real v2 requirement, Tree's chevrons cost horizontal space at 170px width; consider 200-220px default with a draggable container.Split instead of fixed rectangles.
- 5. Keep irssi-dense as the density verdict: LineSpacing 0, no avatars, no message bubbles, no inter-message gap. Discord-airy wastes the one resource IRC power content (art, walls of text) needs. Add exactly one airy concession: 4px group spacing when the nick changes, implementable as a spacer row in the List architecture.
- 6. Preferences: replace AppTabs with a left nav (widget.List) + content pane in an HSplit inside a resizable dialog (or container.InnerWindow); keeps instant-apply and lazy pages, scales past 7 sections and past 28pt fonts. Keep zenity for color picking, the judgment that Fyne's picker renders badly is correct in 2.8.
- 7. Design empty states as a reusable centered VBox component (icon via theme icons, one-line title, one action button): empty channel ('No messages yet'), empty member pane, channel browser zero-results, and the existing welcome screen refit to the same pattern.
- 8. Promote connection status out of the tooltip-only dot: keep the dot but pair it with the network name in the top bar, and mirror state on each network root row. Disconnected guidance becomes an inline banner row above the composer (a hidden/shown HBox), not a hover tip.
- 9. Add a contrast guard to custom accent/nick picks reusing the existing luminance logic from applySettings, warn (not block) when a chosen color falls under ~3:1 against the active surface token.

### Keep from v1
- The entire mIRC color engine: dual readability-tuned palettes, exact extended table, ircx exact-fidelity names, bgTextSegment, gridLineSegment/artLine terminal-grid rendering, and the render_art_test.go PNG harness, port verbatim.
- forcedTheme's synthetic-theme-color-name mechanism and its pure-neutral dark surface ramp, extend, don't replace.
- Embedded DejaVu Sans Mono Bold/BoldOblique and the Font() override working around Fyne's ignored mono-bold flag.
- SizeNameLineSpacing=0 and the irssi-dense stance generally.
- Luminance-driven variant flip for custom backgrounds, accent-derived Primary/Focus/Selection alphas, background image + opacity, per-point font scaling with dialogSize().
- Instant-apply preferences with lazy tab rendering, and zenity for native color/file pickers.
- Coalesced 80ms flush, stick-to-bottom gap detection, smooth scroll animation, per-buffer drafts keyed by currentKey, unread/highlight semantics (playback and status lines never count).
- The designer-specced preset palettes in prefs.go (accent wheel, both-backgrounds-safe nick band, background lightness ramp).

### Discard from v1
- Plaintext/select mode (selectEntry, updateSelectView, selectBtn, the append-only optimization and its trim-cap caveat), a second renderer of every buffer existing only because one big RichText can't select; per-row copy in the List architecture replaces it wholesale.
- The single monolithic RichText message pane with maxShownLines/maxShownSegments caps, replaced by the per-row List; the caps become List virtualization for free.
- Text-hack sidebar encoding: four-space indent, '(3)' suffix, '(offline)' suffix, WarningImportance-as-mention, all replaced by the custom row widget with dot + pill.
- Fixed 170px canvas.Rectangle width-forcing for both side panes, replace with container.Split so users control widths.
- Hardcoded dotGreen/dotOrange/dotRed and other call-site NRGBA constants, fold into state tokens.
- The proportional-font channel / mono server split in segmentsFor, superseded by all-mono chat.
- AppTabs preferences shell (keep the pages' content, replace the container).
- Tooltip-only status semantics: tooltips stay as reinforcement, stop being the sole carrier of connection state.

### Details
Grounding: read /home/lenovo/eco/menu.go, prefs.go, ui.go, colors.go, gridline.go in full or near-full, and verified Fyne v2.8 capabilities against /home/lenovo/gopath/pkg/mod/fyne.io/fyne/v2@v2.8.0 (List.SetItemHeight at widget/list.go:205; widget.Tree present; container.NewThemeOverride in container/theme.go; no user selection or hover API on RichText).

Fyne capability map for the v2 spec, piece by piece. Token system: fully deliverable, Fyne themes are a function (name, variant) -> color, and v1 already proves arbitrary synthetic names flow through RichTextStyle.ColorName and widget Importance; ThemeOverride containers scope tokens per surface. Message rows with hover: NOT deliverable inside one RichText (no per-segment hover, no selection); deliverable via widget.List + custom Hoverable row widgets + SetItemHeight, with row height computed via fyne.MeasureText per wrap, the main engineering risk in the whole plan is relayout cost on window resize (every row's height recomputes); prototype this first, budget a fallback of fixed-height rows with internal RichText wrap disabled. Unread pills and activity dots: trivially deliverable (canvas.Rectangle with CornerRadius exists in 2.8, canvas.Circle for dots). Tree sidebar: widget.Tree works but costs indent width and adds selection-model complexity; a flat List with styled rows meets the two-level (network > buffer) reality, recommend List unless collapse is a hard requirement. Native tooltips: still absent in 2.8; the dweymouth/fyne-tooltip dependency stays. Color picker: Fyne's dialog.NewColorPicker remains visually weak; zenity stays. RichText backgrounds: still unsupported natively; bgTextSegment's stack hack and the last-segment-smear workaround (colors.go:208) remain load-bearing, port with their comments intact.

Typography plan detail: chat pane = DejaVu Sans Mono at user's pt size, timestamps in text-faint token, nicks bold mono in nick-color tokens, status lines in text-faint with the existing '· ' sigil; chrome = theme default proportional; headings only in dialogs. The mono commitment also fixes a v1 inconsistency where hasBackgroundArt applies a stricter painted-ratio test in channels than in the (mono) server buffer.

Density verdict rationale: IRC content is line-oriented, art-bearing, and high-velocity; irssi-dense wins, and v1's owner already fought Fyne (LineSpacing 0) to get it. The single group-spacing concession restores scannability without bubbles.

Settings surfaces: pages carry over as-is (they are good instant-apply forms); only the shell changes. Add the contrast guard by reusing applySettings' luminance code (menu.go:148).

Sequencing suggestion for the council: (1) token layer extraction, low risk, unblocks everything; (2) message-row List prototype under render_art_test.go's harness, the one risky bet, validate before committing; (3) sidebar rows; (4) settings shell and empty states last, pure polish, no architectural coupling.

---

## Senior product/UX researcher, chat products (v2 redesign input for the eco design council)

### Top findings
- The single-RichText message pane is v1's deepest interaction dead end: no selection (worked around with a 'plaintext mode' toggle that swaps in a monospace Entry), no per-message hover or click targets, and a context menu that can only act on 'the last message' (showMessageMenu in rowmenus.go offers 'Copy last message' / 'Delete your last message'). Every modern pattern the owner wants, hover actions, grouping, unread separators, per-message copy/redact, is blocked by this one architectural choice.
- Right-click is the primary action surface for a client that claims to be beginner-friendly. Autojoin, ignore, channel settings, invite, mark-as-read, reconnect, and op tools exist ONLY in TappedSecondary menus (rowmenus.go); nothing hints they are there. The devdocs even record the Fyne trap this causes (TappedSecondary swallowing left clicks).
- Unread state is ephemeral and local: an in-memory map[string]int plus a highlight set, purged on disconnect and lost on restart, while session.go requests draft/read-marker and then never implements MARKREAD. Ergo offers cross-restart/cross-device read sync and v1 leaves it on the table. Same pattern: away-notify, account-notify, extended-monitor negotiated but unused in the UI ('negotiated but doing nothing' per the scope audit); TAGMSG (typing) is explicitly ignored with a comment.
- The channel browser (browser.go) is v1's best UX idea: one search box that filters LIST results, joins the top match on Enter, and offers 'Create #x' with an options form when nothing matches, browse, join, and create as one flow. But it is buried in a modal behind a toolbar icon, and there is no equivalent quick switcher for buffers you are already in (Alt+1..9 jumps to absolute list rows, which shift as buffers open/close).
- Navigation is a flat widget.List faking a tree with four-space indents; multi-network structure, unread badges ('(n)' appended to the label text plus bold), and highlight state (WarningImportance text color) all live in label styling. Clicking an offline pinned network row silently connects, the code itself documents having to special-case keyboard navigation so cycling doesn't trigger connections.
- The scroll/flush engineering is genuinely good and hard-won: stickBottom with an 80px threshold, 80ms coalesced refresh, 250-line render window, AmendBuffer refusing to yank a reader who scrolled up, smooth scroll animation. This discipline (documented in lessons-learned.md) must survive v2 verbatim even as the widget architecture changes.
- History loading is hidden: chathistory playback works on join, but 'Load older messages' lives only in the message-pane right-click menu firing '/history'. No scroll-to-top infinite loading, no search of any kind (in-buffer or global), the largest pure feature gap versus IRCCloud/The Lounge.
- The mIRC color/grid-art engine (colors.go, gridline.go, render_art_test.go harness) is a real differentiator no modern competitor has at this fidelity; it renders as RichText segments and one grid segment type, so it can port cleanly into a per-message-row architecture.
- Composer fundamentals are already right: per-buffer drafts keyed correctly across cross-session switches, tab completion covering nicks/@mentions/#channels-from-LIST/slash-commands, input history with draft preservation, multiline with a line-cap counter. This layer needs polish, not redesign.
- Notifications fire (highlight or notify-all) but go nowhere: clicking a notification does not focus the buffer, there is no mention rollup anywhere, and 'jump to unread' picks the first unread row rather than prioritizing mentions.

### Recommendations
- P0, Rebuild the message pane as a recycled list of per-message row widgets (Fyne List or custom), not one RichText blob. Each row: gutter (time), author block, body segments (reusing the existing segment renderers, including gridLineSegment for art). This single change unlocks selection/copy (killing plaintext mode), hover actions (copy, quote-reply, redact-own via draft/message-redaction), unread and date separators as row types, and author grouping. Keep the flush/stickBottom discipline exactly as-is.
- P0, Implement draft/read-marker properly: send MARKREAD on buffer blur/quit, seed the unread separator ('NEW, since 14:02') and sidebar badges from the server marker on connect. Unread becomes durable and multi-device. Persist highlight state the same way locally.
- P0, Ship a quick switcher (Ctrl+K) that fuses three things v1 already has data for: open buffers (ranked by recency/unread), LIST results, and the create-channel flow from browser.go. This becomes the primary navigation verb; the channel browser survives as a discovery view, no longer a modal.
- P1, Author grouping, IRC-tuned: group consecutive messages from the same nick within ~2 minutes, compact single-line density (no avatars, they add zero information in IRC and cost horizontal space; keep colored nicks). Never group status/join/part lines or the server buffer. Halloy proves this reads well for IRC traffic.
- P1, Discoverability parity rule: every right-click action must also exist in a visible surface, channel header menu (topic, settings, invite, autojoin toggle, leave), member-pane hover affordance, or the switcher. Right-click stays as the power-user shortcut, never the only path.
- P1, Real sidebar tree: collapsible network sections with per-network connection dot and unread rollup on the header, channels and DMs sub-grouped, right-aligned badge pills (count; filled/accented for mentions), dimmed offline networks with an explicit 'Connect' affordance instead of click-to-connect. Alt+1..9 binds to pinned buffers, not absolute rows.
- P1, Scroll-up loads older history via chathistory automatically (with a loading row), replacing the hidden /history menu item; add in-buffer search (local scrollback first, chathistory-backed later). Add a jump-to-bottom pill showing the count of new messages when scrolled up.
- P2, Typing indicators via +typing: send only in DMs by default (channel opt-in), render as one muted line above the composer, never in the message log. Cheap with message-tags already negotiated; respect the presence-leak concern by making sending toggleable.
- P2, Use the caps already negotiated: away-notify dims members, account-notify/extended-join can badge registered users, MONITOR gets a minimal DM-presence dot. Either wire them or drop them from the cap list per the scope audit.
- P2, Inline link/image previews strictly opt-in per network, default OFF: client-side URL fetching leaks the reader's IP to any URL pasted in a channel; IRC norms are hostile to auto-fetch. When off, offer hover 'preview' on demand. Do not build threads, read receipts, or reactions-as-input: no interoperable server support; at most render received TAGMSG reactions passively.

### Keep from v1
- The entire scroll/refresh discipline: stickBottom 80px threshold, 80ms coalesced flush, bounded render window, AmendBuffer's don't-yank guard, smooth scroll (ui.go), port verbatim.
- The color/art rendering engine (colors.go, gridline.go) and its PNG snapshot harness, a genuine differentiator; carry the segment renderers into the new row widgets.
- browser.go's unified search/join/create flow with the channel-options diff form, promote it into the quick switcher rather than redesigning it.
- Composer layer: per-buffer drafts (including the cross-session keying fix), tab completion (nicks, @mentions, #channels incl. LIST, slash commands), input history with draft slot, multiline with line-cap warning (entry.go).
- Session/UI callback hygiene: stale/active guards so torn-down connections can't mutate UI, per-session skey scoping, unread purge on disconnect.
- Rebindable keybinds (keybinds.go), STS handling with the secure-connection persistence rule, SCRAM, the @nick-opens-DM mention resolver, channel-op menus on members, pinned-network concept (minus click-to-connect), embedded docs/help.

### Discard from v1
- Single-RichText message pane and everything it forced: plaintext-mode toggle (selectEntry/selectOn machinery), 'last message only' context menu, replaced wholesale by per-message rows.
- Text-styling-as-state in the sidebar: '(n)' suffixes, bold-for-unread, WarningImportance-for-mention, four-space fake indentation, replaced by a real tree with badge pills.
- Click-an-offline-row-to-connect; the code already documents it as a trap for keyboard navigation.
- Modal-first IA: channel browser, network manager, and settings as blocking dialogs; move discovery and settings to in-window views/overlays with Escape handling retained.
- Alt+1..9 bound to absolute, shifting list rows.
- Dead capability weight per the scope audit: draft/languages, pre-away/relaymsg negotiation with no behavior, /ison, /userhost, plain ws://, request only what v2 acts on (and actually act on read-marker, away-notify, typing).
- The 100-entry global input history shared across buffers (make it per-buffer alongside drafts).

### Details
V2 information architecture, concretely.

Left rail (~220px, collapsible): one section per network. Section header = network name, connection dot, collapse chevron, unread rollup when collapsed; overflow menu (not right-click-only) with connect/disconnect/edit/browse. Under it: Channels then DMs, alphabetical with unread-first optional. Each row: name, muted last-activity time optional, right-aligned badge (grey pill = unread count, accent pill = mentions). Offline pinned networks appear dimmed with an inline Connect button. Bottom of rail: self card, current nick, away toggle, gear.

Center: buffer header (name, member-count button toggling the right pane, topic on a second line click-to-expand and click-to-edit-with-ops, search icon, kebab menu holding today's right-click channel actions). Below: message list, day dividers, one persistent NEW separator sourced from the server read marker, author-grouped rows, hover strip (copy, quote, redact when own+msgid), art lines rendered by the grid engine as their own row type, scroll-up auto-loads chathistory, jump-to-bottom pill with new-message count. Above composer: typing line (+typing, DMs by default) and the compose hint. Composer unchanged in behavior.

Right pane (per-buffer remembered visibility): member list with filter box, sectioned by prefix (~& / @ / % / + / rest), away-dimmed via away-notify, ignored-greyed as today; hover kebab replaces right-click-only op actions.

Overlays: Ctrl+K quick switcher = fuzzy over open buffers + LIST + 'create #x…' row; Escape-dismissable panels instead of modal dialogs where feasible.

Notifications/unread: source of truth is the server read marker per buffer; the rail badge, the NEW separator, and the OS badge (mention count only) all derive from it. Desktop notification click focuses the buffer. Jump-to-unread cycles mention-buffers first, then unread.

Pattern verdicts: transfers, unread separators (best-in-class fit thanks to read-marker), quick switcher, hover actions, scroll-up history, in-buffer search, author grouping (time-windowed, dense, no avatars), typing in DMs. Traps, threads, read receipts, reactions-as-input, avatars, auto-fetching link previews (privacy leak; opt-in only), typing broadcast in large channels.

Architecture implication for the council: decompose ui.go's god-struct into a buffer store (session-owned lines + markers), a per-buffer view model (rows: message/group-continuation/separator/art), and thin Fyne row widgets; unread/notification logic becomes one service consuming read-marker events instead of three ad-hoc maps (unread, hlBuffers, drafts) keyed by hand-rolled skeys. All files referenced: /home/lenovo/eco/ui.go, /home/lenovo/eco/rowmenus.go, /home/lenovo/eco/browser.go, /home/lenovo/eco/entry.go, /home/lenovo/eco/session.go, /home/lenovo/eco/devdocs/scope-audit.md, /home/lenovo/eco/devdocs/lessons-learned.md, /home/lenovo/eco/devdocs/ui-polish-plan.md.

---

## UX researcher, IRC-beginner first-run walkthrough (Discord/WhatsApp mental model)

### Top findings
- The entire first-run funnel is a config-database dialog: the welcome screen's only button opens 'Servers & Identities' (networks.go:263), a two-pane profile manager whose form exposes Network/Server/Nickname/'Account password' plus an Advanced section with Port, TLS, 'Server password (PASS)', 'SASL account', 'SASL mechanism (PLAIN/SCRAM-SHA-256)'. A Discord native has never typed a server address and has no account yet; four of these labels are pure protocol jargon on screen one.
- Post-connect landing is the raw 'server' buffer (startSession → selectRow(sess, serverBuffer), networks.go:349): monospace MOTD, 'Active IRCv3 capabilities: account-notify, away-notify, ...' (session.go:549), 'Channel scan complete: N channels found.' To a Discord user this is terminal noise, and there is no concept in their world for a 'server log' being the home screen.
- On small networks (≤75 channels) eco silently JOINs every channel (session.go:626-636). The sidebar floods with dozens of rows the user never chose; on large networks a modal channel-browser dialog pops over the noise instead (ChannelListDone → showChannelBrowser). Both defaults take control away at the exact moment the user is most disoriented.
- Failure handling is effectively silent: a bad server address yields 'Connection failed: dial tcp: lookup ...' (raw Go error) written only into the server buffer (session.go:506), plus a 12px red dot whose explanation ('right-click the network to reconnect') exists only as a hover tooltip (ui.go:642). No dialog, no retry button. The welcome screen is already hidden by then, so the user stares at an empty pane.
- Wrong 'Account password' fails silently: SASLOptional:true (session.go:465) means SASL failure numerics 904/905 are just printed as monospace lines in the server log; the user believes they are logged in and nothing tells them otherwise. There is also no visible logged-in/nick identity chip anywhere, and a 433 nick collision is handled invisibly by the library.
- Registration, the single most important retention step per the docs, is a slash-command ritual: /register password, watch the server log for 'check your email', /verify code, then manually copy the password back into the profile form (docs/getting-started.md:106-117). Zero UI support for the flow the docs themselves call 'worth doing on any network'.
- Autoscan sends LIST on every connect (session.go:584-586); on Libera that is tens of thousands of 322 replies at first connect, just to maybe pop a browser the user did not ask for. It is also the reason the landing screen is covered in scan chatter.
- Genuinely good bones a beginner benefits from: seeded Ergo Testnet + Libera profiles with a working nick default ('newbie'+suffix) so 'just press Connect' works; the channel browser's search/join/create-in-one-flow with the explicit 'Create #x' affordance (browser.go:87-133); per-buffer drafts; the JoinFailed dialog that prompts for a channel key (ui.go:608); STS auto-upgrade; keyring password storage; unusually well-written embedded docs.
- Unknown slash commands are sent raw to the server (commands.go:336-337), so a typo like '/jion #chat' produces a cryptic server numeric in the server log instead of a 'did you mean /join' hint.
- Empty states are missing everywhere: connected-but-no-channels shows nothing actionable in the center pane; a quiet channel shows nothing; the only pointer to 'find channels' is a tooltip-less-until-hover magnifier icon.

### Recommendations
- Replace the manager-as-onboarding with a 2-step wizard: (1) 'What should people call you?' with the newbie+suffix prefilled; (2) three cards, 'Try the sandbox' (Ergo Testnet), 'Join the open-source hub' (Libera), 'I have a server address' (single text field). Everything currently in the form's Advanced accordion moves to Settings, never onboarding. Keep 'Servers & Identities' as a power-user manager reached from settings, renamed 'Networks'.
- Never land on the server buffer. On connect, land in a real channel (testnet: auto-join one curated default like #chat) or an embedded (non-modal) channel-discovery view in the center pane. Demote the server buffer to a collapsed 'Network log' row (or behind a debug toggle) and stop printing the IRCv3 caps list and scan-progress chatter there by default.
- Kill join-all as a default. Replace with 'join a starter set' (top 3-5 by user count, shown as a confirmation chip-list) or a checkbox picker. If join-all survives, gate it to tiny networks (<10 channels) and always say why channels appeared.
- Make failure a first-class screen, not a log line: map dial/TLS/SASL errors to human sentences with actions, 'Couldn't find that server. Check the address.' [Edit] [Retry]; 'Wrong account password' with a fix-profile button; a visible banner when SASL fails instead of silent continuation. The status dot should gain adjacent text; hover-only and right-click-only recovery paths must have visible equivalents.
- Build registration into the UI: a 'Claim this nickname' button (identity chip → dialog with password/email) that runs REGISTER/VERIFY, shows the email-verification step as a form, and saves the password to the keyring automatically, replacing the /register → read log → /verify → paste-into-profile loop.
- Language pass with progressive disclosure: 'SASL account/mechanism' → 'Account name / Login method'; 'Server password (PASS)' hidden entirely unless a bouncer preset is chosen; 'TLS' → 'Encrypted connection (recommended)' shown only in Advanced; 'server log' → 'Network log'; no user-facing 'buffer'.
- Add empty states: no-channels-joined → centered 'Find your people' CTA opening discovery; empty channel → 'Quiet right now, say hi'; disconnected network row → inline [Reconnect] button rather than click-the-row-to-connect (which is a surprising side effect today, ui.go:363-374).
- Make LIST lazy: only scan when the discovery view opens (with a progress indicator), not on every connect; cache results per network.
- Intercept unknown slash commands with a 'Unknown command /jion, did you mean /join? (Press Enter again to send raw)' instead of raw passthrough by default.
- Add an identity chip (current nick + logged-in state) to the top bar so nick changes, 433 renames, and SASL state are always visible.

### Keep from v1
- Seeded Ergo Testnet + Libera profiles with working 'newbie'+randSuffix nick, press-Connect-and-it-works is the right instinct (networks.go:55-61)
- Channel browser's unified search/join/create flow with the explicit 'Create #name' button and auto-# prefixing (browser.go)
- Per-buffer drafts, unread/mention model, and the follow-rename state migration (ui.go:678-707)
- JoinFailed dialog that prompts for a channel key instead of dumping numeric 475 (ui.go:608-625)
- STS auto-upgrade to TLS and the TLS-by-default (6697) posture
- Keyring storage of passwords with JSON fallback (networks.go:93-110)
- The embedded docs' tone and content, best-in-class beginner writing; reuse its language in-product
- Cleartext-password warning on unencrypted connections (networks.go:315-317)
- Scroll behavior: stick-to-bottom only when already at bottom; reading history never yanked (ui.go:929-941)
- Escape-closes-dialog tracking and rebindable shortcuts

### Discard from v1
- Welcome screen → full network-manager dialog as the first-run path; replace with the wizard (the manager stays, but only as a settings screen)
- Server buffer as the post-connect landing view and home for all feedback (errors, scan chatter, caps dump)
- Join-all-on-connect default (joinall=true, limit 75), sidebar flooding is the opposite of Discord's opt-in channel model (session.go:626)
- Modal channel-browser popping automatically on first connect (ChannelListDone → showChannelBrowser); make discovery an embedded, user-initiated view
- Eager LIST on every connect (autoscan default true), expensive on big networks and the source of most landing noise
- 'SASL mechanism', 'Server password (PASS)' labels and the Account-password field on the essentials form before any account exists
- Raw passthrough of unknown slash commands as the default behavior (commands.go:336)
- Hover-only status-dot detail and right-click-only reconnect as the sole recovery affordances
- Click-an-offline-row-to-connect side effect without a visible button
- 'welcome' as lowercase buffer title and the bare logo screen with a single button, the space should teach, not just point

### Details
Walkthrough evidence, ranked by drop-off risk.

1. First 60 seconds. main.go starts idle: logo + "Connect to a server". That button opens showNetworkManagerFor (networks.go:117), a 640x380 two-pane dialog. The essentials form already contains "Account password" (placeholder: "password of your registered account, if any"), a concept the beginner cannot yet have, and the Advanced accordion reads Port / TLS / Server password (PASS) / SASL account / SASL mechanism / Auto-join. Estimated highest drop-off point: the user is asked to understand identity, transport, and auth before seeing a single message. Mitigating grace: profile 0 (Ergo Testnet) is preselected with a valid nick, so blind "Connect" works, v2 should make that the designed path, not the lucky one.

2. First connect. startSession immediately selects the server buffer. The user's first screen of IRC is: "Connecting to testnet.ergo.chat:6697 ...", MOTD art, "Active IRCv3 capabilities: ...", "Channel scan complete: 47 channels found.", "Joining all 47 channels.", then the sidebar explodes with 47 rows. On Libera the same flow instead fires a LIST of tens of thousands of channels and pops a modal browser over the log. Either way the user never chose anything, and the center pane is protocol output.

3. Failure paths. conn.Connect error → one line in the server log plus a red 12px dot whose remedy text is tooltip-only. SASL failure: SASLOptional means the session proceeds silently; 904/905 are catch-all monospace lines (session.go:1092-1095). Nick collision is invisible. There is no retry affordance anywhere in the UI, only right-click → reconnect on a sidebar row.

4. Account model. /register → watch log → /verify → manually copy password into the profile form. Every step is command- or log-mediated; docs carry the whole burden (getting-started.md:106-117).

5. Language. On-screen: SASL, PASS, TLS, mechanism, server log; docs say "buffer" is avoided but the buffer title literally reads "server". The input placeholder ("Type a message… /help for commands") and the docs' tone show the team can write beginner-first copy, v2 should apply that voice to every label, error, and status line.

Files read: /home/lenovo/eco/main.go, ui.go, networks.go, browser.go, session.go, commands.go, menu.go, prefs.go, docs/getting-started.md.

---

## 20-year IRC veteran (irssi/WeeChat daily driver; has used every GUI client) reviewing eco v1 for the v2 design council

### Top findings
- The protocol layer is genuinely strong for a v1: full IRCv3/Ergo cap set (chathistory, server-time, echo-message with instant local echo, multiline, redaction, STS with policy persistence, SASL PLAIN+SCRAM, IRC-over-WebSocket), msgid dedupe, reconnect-with-rejoin. This is better modern-IRC plumbing than HexChat has ever had.
- There is NO search and NO lastlog anywhere, not even a find-in-buffer. For someone in 40 channels this is the single biggest dealbreaker; 'what did X say about Y yesterday' is unanswerable. Related: history lives only in RAM (scrollbackCap in session.go addLine), so every restart forgets everything unless the server is Ergo with chathistory.
- History pagination is fake: both /history (commands.go:212) and the 'Load older messages' context item (rowmenus.go:427) send CHATHISTORY LATEST again, you can never scroll back past the last 200 messages. Real clients page with BEFORE <oldest-msgid>.
- Activity management is one-size-fits-all: a single global unread map and hlBuffers set, no per-channel notification levels (mute / highlight-only / everything), no highlight word lists beyond exact own-nick word match (containsWord, ASCII-only isNickChar), and notify() fires even for the buffer you are actively looking at in a focused window (AddLine callback, ui.go:654). In 40 channels this is either silence or notification hell.
- The sidebar is a flat widget.List in join order: no activity sorting, no drag reorder, no collapsing a network's tree, and Alt+A jumps to the first unread row in list order rather than the next highlight. Alt+1..9 covers 9 of ~43 rows.
- Rendering architecture is the wall v2 must break: one RichText holding every segment of the visible buffer, capped at 250 lines (maxShownLines), full re-render on every buffer switch, and no text selection, the workaround is a parallel read-only plaintext Entry toggle. The mIRC color/grid-art engine itself (colors.go + gridline.go + render harness) is excellent and hard-won; the container around it is the problem.
- Several correctness mechanisms are text-matching heuristics that will bite on non-Ergo servers: echo dedupe keyed on buffer|text (pendingEcho, a server rewriting a message breaks it), live-vs-playback multiline detection via a 2-minute recency window (session.go:1215), and a single global whoisTarget that races if you /whois in two buffers. labeled-response is negotiated but unused, it is the correct tool for all three.
- Nick/presence tracking is absent: away-notify is negotiated and explicitly ignored (session.go:983 empty callback), MONITOR has no UI (raw command only), account-tag/account-notify data is received and never displayed, and TAGMSG (typing indicators) is dropped. draft/read-marker is negotiated but never sent/consumed, so multi-device with Ergo persistence or soju loses your read position.
- Ignore is lowercase-nick-only and global across networks (ignore.go), no hostmask patterns, no network scoping, no /ignore command. Join/part filtering is all-or-nothing globally or per-channel; there is no WeeChat-style smart filter (show join/part only for recently-active people), which is the feature that makes 40 busy channels readable.
- Zero extensibility: no aliases, no triggers, no hooks, no scripting of any kind. Every power user's first question ('can I make it do X when Y happens') has the answer 'no'. Also missing for power users: SASL EXTERNAL (CertFP), lag/ping indicator, exponential backoff (fixed 30s ReconnectFreq), persistent input history (100 entries, RAM only).

### Recommendations
- 1. Local message store on disk (pure-Go SQLite e.g. modernc, one DB per profile) as the authoritative buffer model: append live messages, backfill history with CHATHISTORY BEFORE <oldest msgid>, dedupe by msgid, survive restarts, and index for search. This one change unlocks #2 and real infinite scrollback.
- 2. Search: Ctrl+F find-in-buffer plus a global search pane (nick/channel/network/date filters) over the SQLite store; fall back to server-side CHATHISTORY for gaps. Nothing else on this list matters more to the 40-channel user.
- 3. Per-buffer notification levels (mute / mentions-only / all) with per-network defaults, user-defined highlight word/regex lists, and suppress notifications for the focused visible buffer. Store per-buffer state keyed by network+buffer, not globally.
- 4. Activity-aware sidebar: collapsible network trees, distinct visual states (mention > message > event) matching the notification levels, optional activity-sort or unread-first section, drag reorder, and a hotlist-style next-highlight/next-unread cycle (WeeChat Alt+A semantics: highlights first).
- 5. Replace the single-RichText message pane with a virtualized per-row list that reuses the existing colors.go/gridline.go segment engine per row, giving native mouse selection/copy, cheap appends, and no 250-line window, delete the plaintext select-mode fork entirely.
- 6. Smart join/part filter (default on): show join/part/quit only for nicks that spoke in the last N minutes; keep the per-channel and global overrides.
- 7. Kill the heuristics using caps you already negotiate: correlate echo-message via labeled-response labels instead of text matching, track batch types explicitly (chathistory vs multiline vs playback) instead of the 2-minute window, and make whois replies correlate per-request. Implement draft/read-marker send+receive so read state syncs across devices/bouncers.
- 8. Presence layer: surface away-notify in the member list (dim/italic), a MONITOR-backed friends list with online/offline notifications, show account names from account-tag in whois/hover, and send/render +typing TAGMSG (it targets modern Ergo, be the client that actually feels alive).
- 9. Minimum extensibility: /alias with parameter substitution, trigger rules (pattern -> action: notify/hide/color/run command), and a documented internal event bus those hang off. Defer full scripting (e.g. gopher-lua) to v2.x but design the bus for it now.
- 10. Hostmask-pattern ignores scoped per network, an /ignore command, SASL EXTERNAL client certs, exponential backoff with jitter for reconnect, a lag indicator on the status dot, and persisted input history per buffer.
- Architecture for all of the above: split package main into core (protocol+store, zero Fyne imports, fully testable offline with a scripted fake server, v1's only tests are live against testnet.ergo.chat) and ui packages, communicating over a typed event stream instead of the 13-field UICallbacks struct whose handlers currently reach back into session internals under s.mu (e.g. RedactMsg in ui.go).

### Keep from v1
- colors.go + gridline.go + render_art_test.go: the pixel-perfect mIRC color/art engine with PNG snapshot harness is the crown jewel, port it untouched as the per-row renderer
- The full cap-negotiation breadth and session.go's protocol coverage: STS handling, SCRAM, WebSocket transport, account-registration/VERIFY flow, join-refusal prompts (475 key dialog), part-refusal buffer cleanup, reconnect-rejoin batching
- Instant local echo with dedupe UX (keep the behavior, replace the text-match mechanism with labeled-response)
- Per-buffer drafts keyed by session+buffer (currentKey ownership fix), rename-follows-buffer state migration
- Keyring-only password storage with graceful fallback; pinned offline network rows with one-click reconnect
- Coalesced 80ms render flush + stick-to-bottom scroll discipline, the right instincts, keep them in the virtualized list
- Tab completion design (nicks with 'name: ' addressing, channels, /commands, @mentions) and Shift+Enter multiline with server max-lines hint
- Rebindable shortcut framework (extend the 5 actions, keep the mechanism), context-menu system in rowmenus.go, channel browser, embedded docs/help
- devdocs/lessons-learned.md, the Fyne sharp-edges list is institutional knowledge the v2 team must read first
- The beginner-friendly identity: sensible defaults, real protocol names under Advanced, unknown /commands passing through raw

### Discard from v1
- Single-RichText message pane + maxShownLines=250 window + the selectOn plaintext-Entry fork, replaced wholesale by a virtualized selectable row list
- pendingEcho text-key dedupe, the liveML 2-minute recency heuristic, and the global whoisTarget, all replaced by labeled-response/batch-type correlation
- In-memory-only Buffer.Lines with scrollbackCap trimming as the source of truth, becomes a cache over the disk store
- CHATHISTORY LATEST for 'load older', replace with BEFORE-based pagination
- Global nick-only ignore set and the binary global/per-channel join-part hiding, replaced by scoped hostmask ignores and the smart filter
- notify-on-every-message global toggle, subsumed by per-buffer notification levels
- Join-all-channels-on-connect as a default behavior (75-channel cap): keep as an explicit per-profile opt-in for tiny Ergo communities, off by default, it is wrong for anyone on 3 real networks
- Plain ws:// transport and the legacy keyring migration path (v1's own scope-audit already recommends cutting both), /ison and /userhost as first-class commands
- Flat mixed rowRef list for the sidebar, becomes a real tree with per-network collapse
- 100-entry volatile input history and the fixed 30s ReconnectFreq

### Details
What separates a daily driver from a toy, ranked for someone in 40 channels / 3 networks: (1) trustworthy history, persistent, searchable, infinitely pageable; (2) attention management, per-channel notification levels, highlight lists, an activity-ranked way to answer "where do I need to look next"; (3) frictionless navigation, hotlist cycling, quick-switcher, collapsible network trees; (4) presence truth, who's here, who's away, who came online (MONITOR), synced read state across devices; (5) selection/copy that just works; (6) hooks so the client bends instead of the user leaving. Dealbreakers that make power users walk: can't search, can't select text, notification spam or silence with nothing between, history that dies on restart, no CertFP, no way to script around annoyances.

v1's honest position: its protocol layer is modern and careful, better than most GUI clients, and its rendering engine is genuinely special. But it has none of items 1, 2 (beyond a global unread count), 4, or 6, and item 5 only via a workaround. It is currently a beautiful terminal into Ergo's server-side memory, not a client with a memory of its own.

The architectural key for v2 is one decision: a local message store (SQLite) as the model, with the protocol core split into a UI-free package emitting typed events. Search, infinite scrollback, restart persistence, read markers, and offline-testability all fall out of that single move. The second key is per-row virtualized rendering wrapping the existing color engine, which simultaneously fixes selection, the 250-line window, and buffer-switch cost. Everything else on the list is incremental on top of those two.

Files behind specific claims: /home/lenovo/eco/session.go (pendingEcho text dedupe ~1180, liveML heuristic ~1215, whoisTarget ~113, empty AWAY callback 983, LATEST-only fetch 720), /home/lenovo/eco/commands.go:212 (/history LATEST), /home/lenovo/eco/rowmenus.go:427 (Load older = /history), /home/lenovo/eco/ui.go (AddLine notify path 654-676, maxShownLines 297, selectOn fork, RedactMsg mutating under sess.mu 794-814), /home/lenovo/eco/ignore.go (global nick-only), /home/lenovo/eco/entry.go (100-entry history), /home/lenovo/eco/keybinds.go (5 rebindable actions), /home/lenovo/eco/devdocs/lessons-learned.md and scope-audit.md.

---

## IRC protocol engineer (Ergo contributor, IRCv3 specs)

### Top findings
- Casemapping is wrong: session.go:148 lower() is strings.ToLower and ISUPPORT (005) is explicitly swallowed as 'handled'. CASEMAPPING, CHANTYPES, PREFIX, STATUSMSG are all hardcoded ('#' checks at session.go:569/951/1145, prefix rank string '~&@%+' at 345/913). '&' channels never match anything (treated as PMs, never rejoined), STATUSMSG targets like '@#chan' open a bogus PM buffer, and b.Members is keyed by raw un-folded nick so a server-cased NICK/QUIT can desync member lists.
- isPlayback() (session.go:442) treats *any* batch tag as history playback, then patches live multiline back in via a 2-minute wall-clock recency heuristic (liveML, session.go:1215). There is no BATCH command handler at all, the client never learns batch IDs or types (chathistory vs draft/multiline vs netjoin). Clock skew >2min makes live multiline silently lose highlights/unread; a fresh replayed multiline counts as live. This is the single biggest structural flaw.
- Echo dedup matches on buffer|text (pendingEcho, session.go:1181-1193, 1319-1324) instead of labeled-response, even though the label cap is requested. Consequences: locally rendered own lines have empty MsgID (so they can never be redacted, replied to, or read-marked), and under Ergo always-on multiclient another device sending the identical text gets its echo eaten. seenIDs dedup does a wholesale wipe at 8192 entries (session.go:1171), reopening duplicate windows.
- Reconnect rejoin (session.go:562-582) filters s.order by '#' prefix and joins without keys, +k channels the client knows the key for (b.Key is tracked in applyModeDelta!) fail to rejoin. Gap-fill after reconnect is a blanket CHATHISTORY LATEST * 100 per channel via cleared historyRequested; anything beyond 100 missed messages is lost and there is no CHATHISTORY AFTER <last msgid> and no TARGETS to resurrect PM conversations.
- SCRAM races autojoin: Connect() registers the join-on-connect callback before setupSCRAM's, so with SCRAM-SHA-256 the client JOINs channels before authentication completes, +R channels refuse, cloaks/vhosts aren't applied. Also SASLOptional:true means PLAIN auth failure silently degrades to an unauthenticated session with no UI signal beyond a server-log line.
- pendingPart's 442/403 handlers (session.go:760-774) fire on those numerics from *any* command (TOPIC, KICK, MODE can also yield 442), so an unlucky interleaving closes a live buffer; disconnect clearing mitigates but doesn't eliminate it. JOIN handler also marks historyRequested for *any* joiner's JOIN (session.go:706-707), not just self.
- MODE handler sends NAMES on every channel mode change (session.go:959) instead of parsing prefix-mode deltas, on a busy channel this is a self-inflicted NAMES flood and a full member-list rebuild per +v.
- Requested caps are write-only: draft/read-marker is negotiated but MARKREAD is never sent or consumed; TAGMSG (typing) is explicitly ignored (session.go:686); away-notify is a no-op; extended-monitor is requested but MONITOR is never used; chghost has no callback. The cap list is aspirational, not implemented.
- The transport and policy layers are actually good: wsconn.go is a clean, correct IRCv3 websocket adapter (frame-per-line, read limit, text.ircv3.net subprotocol); sts.go persists policies with expiry/revocation correctly; scram.go's post-registration SASL conversation with SASLBuffer chunking is right.
- Architecture: UICallbacks fire synchronously on the ircevent loop goroutine with ad-hoc mutex juggling (mlKey is mutated with no lock at session.go:1259); mutating the last Line in place for multiline merge couples wire framing to display state. There is no event log the UI consumes, the session reaches into the UI 13 different ways.

### Recommendations
- P0, Server-properties layer: parse ISUPPORT into a per-network struct (casemapping fold function, CHANTYPES, PREFIX modes+symbols, STATUSMSG, MODES, MONITOR limit). Every map key (buffers, members, seenIDs) goes through fold(); delete strings.ToLower keying. This unblocks half the bug list.
- P0, Real batch tracking: handle BATCH +ref/-ref, keep an open-batch registry {id -> type, params, parent}. Playback = enclosing batch chain contains chathistory; multiline = assemble parts at batch close into one Message, then emit. Delete isPlayback-by-tag-presence and the 2-minute liveML heuristic entirely.
- P0, Labeled-response for all outgoing messages: attach a label, hold the message as 'pending' in the store, resolve to the server echo (gaining the real msgid and server time) or to a FAIL. Delete pendingEcho text matching. This gives own-message redaction, replies, accurate ordering, and correct always-on multiclient behavior for free.
- P1, Event-sourced session layer: session goroutine is the single writer to a per-network store; it emits typed events on one channel; a thin adapter marshals onto the Fyne main thread. UI never touches session mutexes.
- P1, Reconnect done right: per-buffer gap-fill with CHATHISTORY AFTER <last known msgid> (timestamp fallback), CHATHISTORY TARGETS to discover PM activity while offline, rejoin using tracked channel keys and CHANTYPES-aware buffer classification, exponential backoff replacing the flat 30s ReconnectFreq.
- P1, Read markers: send MARKREAD on buffer focus/scroll-to-bottom, consume MARKREAD to sync unread state across devices; derive unread counts from marker position, not a local int. This is the killer feature for Ergo always-on users.
- P1, Chathistory pagination: infinite scroll via CHATHISTORY BEFORE msgid=<oldest>, delivered as HistoryChunk events prepended to the buffer; msgid index makes dedup exact instead of the 8192-wipe map.
- P2, Typing indicators: send TAGMSG +typing=active/paused/done with 3s debounce, render a subtle presence line; trivially cheap once TAGMSG isn't discarded.
- P2, Redaction UX: context-menu 'Delete message' on own messages (enabled by msgid-from-label), send REDACT, render tombstones in place; keep the existing incoming REDACT path.
- P2, Fix the SCRAM/autojoin race: gate autojoin and chathistory fetches on auth settling (903/904/skip) when SCRAM is active; surface SASL failure prominently, not as a log line.
- P2, Multi-network: a NetworkManager owning sessions; every event and buffer ID carries a network ID; per-network server-properties and cap sets; UI sidebar groups buffers under network headers. Design PM/channel identity as (network, folded-name) tuples from day one.
- P3, Surface away-notify and MONITOR (friend presence dots), handle chghost/SETNAME in member state, and watch draft/metadata-2 in Ergo for avatars/display-names, request it now, wire it when stable.

### Keep from v1
- wsconn.go essentially verbatim, correct IRCv3 websocket framing, read limit, subprotocol
- sts.go policy persistence model (expiry, duration=0 revocation, pruning)
- scram.go's post-registration SASL conversation and SASLBuffer chunking (just sequence it before joins)
- Request-everything cap strategy, including drafts
- msgid-based display dedup as a concept (rebuild on a real per-buffer msgid index)
- Join-refusal UX: pendingJoin distinguishing user-initiated joins, JoinFailed with needKey prompting for +k
- First-scan-only join-all with the 75-channel cap, and manual-vs-background LIST distinction
- applyModeDelta's mode folding including channel Key capture
- Multiline send batching with server max-lines enforcement and truncation notice
- containsWord whole-word highlight matching
- ergochat/irc-go as the protocol library

### Discard from v1
- strings.ToLower as casemapping and every hardcoded '#'/'~&@%+', replaced by the ISUPPORT layer
- isPlayback = 'has batch tag' plus the liveML 2-minute recency heuristic, replaced by typed batch tracking
- pendingEcho text-matching echo suppression, replaced by labeled-response correlation
- Incremental last-line mutation for multiline merge (mlKey, unlocked mutable state), replaced by assemble-at-batch-close
- UICallbacks struct with 13 direct UI hooks called from the IRC goroutine, replaced by one typed event stream
- Blanket CHATHISTORY LATEST 100 on every (re)join with historyRequested reset on disconnect, replaced by AFTER-based gap-fill
- NAMES on every MODE change, replaced by parsing prefix-mode deltas from PREFIX
- seenIDs 8192-entry wholesale wipe, replaced by per-buffer bounded msgid index
- Global whoisTarget single-slot routing, replaced by labeled-response or a request-correlation map
- Buffers owning render-ready Line slices as the source of truth, store holds protocol-level Messages; rendering is the UI's projection

### Details
Scope read end-to-end: /home/lenovo/eco/session.go (1394 lines), wsconn.go, scram.go, sts.go, plus CHATHISTORY/echo call sites in commands.go.

The v1 session layer is a genuinely competent single-file client core with excellent comments, but it is built on three foundations that cannot carry v2: string-identity (ToLower keys, text-matched echoes), tag-sniffing instead of batch tracking, and synchronous UI callbacks from the protocol goroutine. Every awkward heuristic in the file (liveML's 2-minute window, pendingEcho counters, the historyRequested reset dance) is a compensation for one of those three.

Proposed v2 state model, concretely:

- NetworkManager -> []*Network. Network owns: ServerProps (from ISUPPORT: fold(), chantypes, prefix table, statusmsg, limits), CapSet, ConnState (Disconnected/Connecting/Registering/Authed/Ready with backoff), and a Store.
- Store is single-writer (the session goroutine). Buffers map[foldedName]*Buffer; Buffer holds Kind (channel/query/server), Members map[foldedNick]Member{Nick, Prefixes, Account, Away}, Topic, Modes, Key, ReadMarker (msgid+time), and a message log: ring of Message{ID msgid, Label, Time, SenderNick, SenderAccount, Kind (privmsg/notice/action/status/tombstone), Body, Tags, Pending bool} with a msgid->index map for dedup, redaction, and pagination anchors.
- Outgoing path: SendMessage creates a Pending Message with a label, renders immediately; echo (matched by label, else msgid) upgrades it in place, one MessageResolved event, no text comparison.
- Batches: registry {batchID -> {type, params, parentID, collected []Message}}. chathistory batches emit HistoryChunk{buffer, msgs, direction} at close; multiline batches emit one merged Message; unknown batch types pass through as live.
- Event types the UI consumes (all tagged with NetworkID): ConnStateChanged, BufferCreated/Closed/Renamed, MessageAdded, MessageResolved, MessageRedacted, HistoryChunk, MembersChanged (delta, not 'go re-sort everything'), TopicChanged, ModeChanged, TypingChanged, ReadMarkerMoved, ListUpdated, JoinFailed, AuthResult, StsSeen. One channel, one Fyne-thread pump, UI holds zero locks.

Always-on/bouncer story (this is Ergo's superpower and v1 barely touches it): on Ready, (1) CHATHISTORY TARGETS timestamp=<lastSeen> to surface PM buffers with offline activity; (2) per known buffer, CHATHISTORY AFTER msgid=<last stored> to fill gaps exactly; (3) consume MARKREAD so unread badges agree with your phone; (4) send MARKREAD on focus; (5) treat 'same account, different device' self-messages as first-class (they already arrive; labeled-response makes them safe). draft/pre-away is already requested, use it so brief UI closes don't flap away status. Multi-network is then just N of these state machines behind one event bus; buffer identity is (networkID, foldedName) everywhere, which also cleanly scopes ignore lists, keybind targets, and notification rules per network.

Sequencing note for the council: the ISUPPORT layer, batch registry, and labeled-response pipeline should land before any UI work, because they change the shape of the data the UI binds to (Messages with stable IDs instead of appended Lines). The rendering engine (colors.go/gridline.go) is orthogonal and ports as-is.

---

## Staff engineer, v1 architecture autopsy and v2 redesign for eco (Go + Fyne IRC client)

### Top findings
- No real layer boundary: the UI reaches into Session internals, ui.go's RedactMsg callback locks sess.mu and rewrites Buffer.Lines in place; commands.go and rowmenus.go poke sess.whoisTarget under a manual sess.mu.Lock() and call sess.irc.Send directly. The 'protocol layer' is advisory.
- UICallbacks (session.go:75) is a 12-field ad-hoc callback struct fired from the ircevent goroutine; the UI wraps every one in fyne.Do plus hand-rolled stale()/active() session-liveness guards, repeated 12 times. One forgotten guard or fyne.Do is a race or a ghost-session UI mutation. This is the single biggest structural liability.
- Session mutex discipline is convention, not structure: one sync.Mutex guards everything; *Buffer pointers escape Buffer()/getBuffer() and callers re-lock to touch them; rules like 'callers must not hold mu' live in comments; s.mlKey is written unlocked (safe only because the ircevent loop is single-goroutine, undocumented). External code locking your mutex is the definitive layering smell.
- Configuration is ~14 mutable globals spread over four files: atomics (hideJoinParts, joinAllChannels, autoScanChannels, scrollbackCap, notify*) plus unsynchronized vars (showTimestamps, timeLayout, colorfulNicks, selfNickColor) plus the ignoreMu-guarded global ignored/jpHidden maps. Session behavior silently depends on them; ignore lists are global across networks; nothing is injectable for tests.
- Zero transport seam: Session.Connect() constructs its own ircevent.Connection, so every protocol test (smoke, send, part, create) hits live testnet.ergo.chat, slow, flaky, offline-hostile. The hardest logic in the app (echo dedupe, msgid dedupe, multiline merge with the liveML recency heuristic, playback classification in handleMessage) has no unit tests for exactly this reason.
- Command handling bypasses the session: commands.go is a 300-line switch on *UI that mostly calls u.sess.irc (the raw library connection) directly, mixing pure protocol commands, UI commands (/close, /clear), and app commands (/quit) in one untestable function. Wire writes are scattered across commands.go, rowmenus.go, browser.go, and session.go.
- UI is a god object: ~35 fields on the UI struct mixing viewmodel state (rows, unread, hlBuffers, drafts, currentKey), rendering caches (segments, selectCount), scroll physics, and widget refs. Real product logic, unread counting, draft migration on rename, cross-session buffer identity via skey strings, lives inside fyne.Do closures and cannot be tested headless.
- The render engine (colors.go + gridline.go + segmentsFor) is the crown jewel: near-pure, already snapshot-tested via render_art_test.go, encoding hard-won knowledge (per-variant mIRC palettes, exact-vs-readability color split, block-glyph geometry, RichText line-termination workarounds). Its only impurity is reading four display globals.
- Session identity is the mutable Name string (skey = Name + '\x00' + buffer); reconnect 'replaces' a session by name, and closeSession must manually purge name-prefixed unread/highlight keys, a stable session ID would delete a whole class of bookkeeping.
- The devdocs (engineering-audit.md, lessons-learned.md) show the team already paying interest on this architecture: 8 of the 12 audit bugs (draft misfiling, ghost badges, rename bounce, silent multiline mentions, stale-session flips) are exactly the state-synchronization bugs an event-sourced state layer makes unrepresentable.

### Recommendations
- Adopt this v2 layout in the same module: cmd/eco (main) + internal/irc (ircevent wrapper, scram, wsconn, caps, STS parsing, emits typed events, no fyne import), internal/state (Store: buffers/lines/members/unread/dedupe/merge, pure Go), internal/render (colors, gridline, Line→segments), internal/ui (widgets, dispatcher, viewmodel) with internal/ui/dialogs, internal/command (parse + execute), internal/config (Settings snapshot, profiles, keyring, STS store, keybinds), internal/notify.
- Key interface 1, event flow (wire→widget): irc.Client exposes Events() <-chan irc.Event (typed union: PrivMsg, Join, Part, Quit, Nick, Topic, Mode, ListItem, CapChange, ConnState, Redact, Rename, JoinRefused...). state.Store.Apply(Event) mutates the model and returns []state.Change (LineAdded, BufferAdded/Closed/Renamed, TopicChanged, MembersChanged, UnreadChanged, ConnStateChanged). A single ui.Dispatcher goroutine drains changes, coalesces (keep the 80ms tick), and calls fyne.Do once per batch, replacing all 12 UICallbacks and their per-callback stale/active guards with one guarded funnel.
- Key interface 2, command flow (input→wire): entry submits text → command.Parse(text, ctx) → typed Command → command.Run(env) where env is a narrow interface {Session() state.SessionHandle; Send(irc.Outgoing); UI UIHooks}. UIHooks (OpenBuffer, ShowDialog, Quit) is the only UI dependency, so 90% of commands unit-test headless. All wire writes go through irc.Client methods; nothing outside internal/irc touches ircevent.
- Give irc.Client a transport seam: Dialer func(ctx) (net.Conn, error) (wsconn already fits). Build a scripted in-memory IRC server on net.Pipe and convert echo-dedupe/multiline/playback/part-refusal behavior into fast offline table tests; keep the live testnet tests behind a build tag.
- Replace all config globals with config.Settings, an immutable struct behind atomic.Pointer[Settings]; state.Store and render take it as a parameter (RenderOptions for timestamps/timeLayout/colorfulNicks). Make ignore lists and join/part hiding per-network data owned by internal/state, persisted via internal/config.
- Migration order (always compiling): (1) move main.go → cmd/eco, everything else → internal/app with package rename, one mechanical commit; (2) extract leaf packages verbatim: render (colors.go, gridline.go), notify, config (sts.go, ignore/keybind persistence, network profiles), irc bits (wsconn.go, scram.go); (3) introduce config.Settings and delete the globals, first real rewiring, small diff per call site; (4) split session.go: typed events + Store.Apply in internal/state (handler bodies port near-verbatim, signatures change), ircevent wiring stays in internal/irc; swap UICallbacks for the dispatcher; (5) extract internal/command as a table of registered commands over the env interface; (6) carve ui.go: viewmodel (rows/unread/drafts/skey→session-ID) into a testable struct, dialogs to internal/ui/dialogs; (7) add the net.Pipe fake-server harness and port the four live tests.
- Give sessions a stable ID at creation (not Name); key unread/draft/highlight maps by (sessionID, bufferKey); rename and reconnect then need no key migration or prefix purges.
- Make redaction and clear-buffer state operations (Store.Apply(RedactEvent)) instead of the UI editing Lines under the session lock; the UI never holds a state mutex in v2, it only receives Changes and reads snapshots.
- Product/UX for v2: keep the beginner-first flow (welcome screen, channel browser as front door, join-refusal dialogs) but revisit join-all-channels-on-connect as a default (scope-audit already doubts it); surface account-tag/away data you already negotiate (member list presence, account labels); consider a virtualized message list widget so the 250-line render window and plaintext-mode workaround can eventually merge into one selectable view.
- Write down the threading contract as one sentence in each package doc: internal/irc emits on its own goroutine; internal/state is confined to the dispatcher goroutine; internal/ui runs only on the Fyne thread; internal/render is pure. v1's bug history is mostly violations of an unwritten version of this.

### Keep from v1
- colors.go + gridline.go wholesale, palettes (readability-tuned light/dark mIRC 16, exact extended 83, classic-for-backgrounds split), bgTextSegment, gridLineSegment/artLine block-glyph geometry, the hasBackgroundArt prose-vs-art heuristic, and render_art_test.go as the regression harness; move nearly verbatim into internal/render
- The Line model and its semantic flags (Action/Notice/Status/Self/Highlight/Playback), a good render-agnostic unit
- The battle-tested protocol heuristics in handleMessage: msgid dedupe, pendingEcho counting, multiline batch merge with the liveML recency guard, pendingPart/pendingJoin refusal handling, playback classification, port the logic intact into state.Store.Apply with unit tests around it
- wsconn.go, scram.go, sts.go, clean, self-contained, correct (STS secure-context rule, SASL buffer chunking, ws frame adaptation); verbatim moves
- entry.go's completion/history UX (word-under-cursor completion, @mention and #channel modes, soft-wrap cursor fallbacks) and keybinds.go's rebindable-shortcut scheme
- forcedTheme (menu.go): variant forcing, luminance-driven variant from custom background, embedded bold mono faces, LineSpacing=0 for art, move to internal/ui/theme
- networks.go persistence: keyring-first secret storage, loadNetworkMeta vs loadNetworks split (no DBus on hot paths), profile pinning
- UI behaviors users already rely on: draft-per-buffer with rename migration, unread/highlight badging rules (playback/status never count), stick-to-bottom with 80px grace, coalesced 80ms flush, channel browser join-or-create flow, per-channel join/part hiding, ignored-users management
- The docs/ set and embedded-help approach, and devdocs as institutional memory
- The live testnet tests, demoted to an optional build-tagged suite, they remain valuable end-to-end checks against real Ergo

### Discard from v1
- UICallbacks struct and the per-callback fyne.Do + stale()/active() guard pattern, replaced by typed Changes through one dispatcher
- All mutable config globals (hideJoinParts, autoScanChannels, joinAllChannels, scrollbackCap, notifyEnabled/soundEnabled/notifyAll, showTimestamps, colorfulNicks, selfNickColor, timeLayout) and the global ignored/jpHidden maps, replaced by config.Settings snapshots and per-network state
- Direct UI access to Session.mu, Buffer.Lines, sess.irc, and sess.whoisTarget (ui.go RedactMsg/updateSelectView/showBuffer, commands.go, rowmenus.go), the mutex never crosses the package boundary in v2
- The 300-line handleInput switch on *UI, replaced by a command table over a narrow env interface
- Session identity by mutable Name string and skey prefix-purging in closeSession, replaced by stable session IDs
- Session.Connect() constructing its own ircevent.Connection with no injection point, replaced by a Dialer seam
- The exported-field Buffer struct as the cross-layer contract (callers re-locking to read Lines), v2 hands out copies/snapshots only
- Whois routing via a single whoisTarget field externally poked under the lock, becomes a request-correlation map inside internal/state
- Per the owner's own scope-audit: plain ws:// transport, /ison, /userhost, and the dead legacy keyring migration path
- The 'unknown slash command → uppercase raw passthrough' default (surprising typo behavior; keep passthrough behind explicit /raw, offer 'unknown command' feedback instead)

### Details
v1 is one 6.4k-line package main where three architectures interleave: a protocol/state layer (session.go, 1394 lines), a UI layer (ui.go 1198 + menu/prefs/rowmenus/browser/entry/keybinds/help), and a shared-mutable-globals config layer. The seams that exist are inverted: Session depends on UI-shaped callbacks (UICallbacks names like AmendBuffer, BufferList are refresh instructions, not domain events), while the UI depends on Session internals (its mutex, its Buffer slices, its raw ircevent connection).

Testability follows directly: the only fast tests are modes_test.go (pure applyModeDelta) and render_art_test.go (pure-ish render). Everything about message handling, the most defect-prone code per the team's own engineering audit, requires a live network because the connection is unmockable and the logic is welded to ircevent callbacks. UI logic (unread, drafts, rename migration, session switching) is untestable because it exists only inside fyne.Do closures on a god-object UI struct.

The v2 shape is an event pipeline: internal/irc translates ircmsg into typed events on a channel; internal/state.Store.Apply folds events into a model and emits typed Changes; a single ui.Dispatcher owns fyne.Do, coalescing, and session-liveness; internal/render turns Lines into segments given RenderOptions; internal/command turns input text into typed commands executed against a narrow env. Wire writes exist only in internal/irc; the state mutex exists only in internal/state; fyne.Do exists only in the dispatcher.

Migration is seven always-compiling steps (see recommendations): the first two are mechanical moves (cmd/eco + internal/app, then verbatim leaf extraction of render/notify/config/wsconn/scram), the middle two are the real rewrites (Settings replacing globals; session.go split into irc+state with UICallbacks→Changes), the last three extract commands, carve the UI viewmodel, and add the net.Pipe fake-server harness. Roughly 60% of v1 by line count moves nearly verbatim (render engine, transports, auth, persistence, entry, keybinds, browser, help, dialogs); the rewrites are session.go's wiring (handler bodies survive, signatures don't), ui.go's callback/dispatch and viewmodel core, commands.go, and applySettings.

Files read: all of /home/lenovo/eco/v1/*.go, /home/lenovo/eco/v1/devdocs/*.md, go.mod; the live tree at /home/lenovo/eco is currently identical to v1.

---

## QA / test-infrastructure engineer

### Top findings
- Only one true unit test exists: modes_test.go (23 lines, applyModeDelta). Everything else (smoke_test, part_live_test, create_live_test, send_test) is a live integration test against testnet.ergo.chat with no build tags, `make test` / `go test .` requires internet and a healthy public testnet, so the default suite is unrunnable offline and flaky by construction.
- The live tests are nevertheless excellent regression encodings of real v1 bugs: part_live_test covers normal PART, refused PART (442/403) closing a stale buffer, and self-KICK auto-leave; send_test covers local-echo dedupe. The scenarios are gold; the delivery mechanism (untagged, hardcoded 25s/20s/15s deadlines, shared #chat) is the problem.
- render_art_test.go asserts nothing. It renders to a PNG in t.TempDir() (discarded) unless ECO_ART_PNG is set, and silently replaces its whole corpus with /home/lenovo/Downloads/ircd-motd-puckman-hq.txt if that file exists, machine-dependent input. But its bones are exactly right for goldens: fyne test app, forcedTheme with pinned fontScale, offscreen window, Canvas().Capture().
- The session/UI seam is real and load-bearing: session.go (1394 lines) has no Fyne imports and talks to the UI only through UICallbacks with staleness guards. Yet nearly every severe audit bug (batch-vs-live msgid classification, reconnect-rejoin, draft skey misfiling, multiline mention silence) is pure logic buried in unexported methods of package main, unit-testable in shape, untested in practice.
- Assertions couple to display strings: tests match substrings of formatted UI lines ("You joined #chat", containsAll on "mode"/"+imsk") through a shared channel, and even hand-roll contains() instead of strings.Contains. Any copy change breaks the suite; state is never asserted directly.
- There is no fake/scripted IRC server anywhere, so the event-stream classification logic, the source of the two worst v1 bugs (chathistory request storm from replayed self-JOIN; live multiline batches invisible to highlighting for weeks), can only be exercised against live Ergo, where those conditions are hard to reproduce deterministically.
- The build is machine-shaped: CGO_LDFLAGS=-L$HOME/.local/lib papering over a missing libXxf86vm (needs libxxf86vm-dev + xorg-dev + libgl1-mesa-dev + gcc in any CI image), Go at ~/go, MinGW hand-extracted into ~/.local/mingw, and until this snapshot no git repo at all. There is no CI, no version stamp in the binary, and `make install` copies to ~/.local/bin, the documented 'stale binary' debugging trap: nothing tells you whether the running eco contains the fix you just built.
- entry.go documents a subtle completion bug class in comments: with soft wrap on, Fyne's CursorRow counts visual rows, so cursor-position math on a wrapped draft is wrong; v1's fix falls back to end-of-text completion. This kind of widget-adjacent logic (completion cycling, compHead splitting, @-mention preservation, history) is currently only testable by hand.
- devdocs/memory.md contains a 'Key invariants (break these and audited bugs return)' list, batch events never mutate state, echo dedupe, reconnect rejoin, STS only over TLS, skey-owned drafts, fyne.Do discipline. This list is literally a test plan nobody wrote down as tests.

### Recommendations
- Layer v2 so the bug-prone logic is pure and unit-tested: an irc/proto package (line parse/serialize, ISUPPORT, caps), an irc/state package of reducers (buffers, members, mode deltas, batch/playback classification, msgid dedupe, reconnect-rejoin planning) that consume parsed events and emit state diffs, a completion package (pure function: draft text + cursor + member list -> replacement), and a format package (mIRC codes -> theme-independent segment model). Table-driven tests feed raw IRC transcripts and assert on state, never on display strings.
- Build a scripted in-process fake server (or transcript record/replay captured once from testnet) as the primary integration layer: deterministic reproduction of exactly the streams that bit v1, history playback batches with self-JOIN, live multiline batches without per-message msgid, delayed echo-message under fakelag, 442/403 PART refusals, KICK, draft/channel-rename, reconnect. This is the single highest-value new test asset.
- Gate live tests behind `//go:build live`: `make test` runs unit+fake-server offline with -race; `make test-live` runs the tagged suite. Live tests get per-phase context timeouts (not one 25s After), unique random channels (keep randSuffix), avoid shared #chat for send assertions, and t.Skip with a loud reason only on initial dial failure, protocol failures after connect still fail hard. In CI they run as a separate nightly/on-demand job that never blocks merges.
- Turn the render harness into a golden-image suite: corpus checked into testdata/art/ (delete the Downloads override), pinned theme variant + fontScale + window size, render via the fyne test driver (headless, deterministic), byte-compare PNGs against testdata/golden/<name>_<theme>.png with a -update flag to regenerate. Run for both palettes (light/dark) and at least two font scales; additionally unit-test gridline.go's column math numerically so alignment failures diagnose themselves without pixel-diffing.
- Stand up CI (GitHub Actions, ubuntu): (1) apt-get install gcc libgl1-mesa-dev xorg-dev libxxf86vm-dev libxkbcommon-dev; (2) gofmt -l + go vet; (3) go test -race (unit + fake-server + goldens, all headless); (4) build linux binary; (5) separate windows cross-build job using distro mingw-w64 instead of the hand-rolled ~/.local/mingw; (6) upload artifacts. Document the libXxf86vm gotcha in the workflow file itself.
- Kill the stale-binary class: stamp every build with -ldflags "-X main.version=$(git describe --always --dirty)", print it at startup and in /version and the About dialog, and make `make install` verify the installed binary's stamp matches the fresh build. A dirty-tree suffix makes 'am I running my fix?' answerable in one glance.
- Adopt the day-one regression list below as named tests (TestRegression_...) with comments linking each to the v1 bug it encodes; port devdocs/memory.md's invariants list into test names so the audit knowledge survives the rewrite.
- Keep -race on by default for every suite (it caught real bugs in v1) and add a leak check (goleak) around session teardown, since v1's ghost-callback/staleness-guard machinery exists precisely because dying sessions kept firing.

### Keep from v1
- The Session/UI split with UICallbacks and per-session staleness guards, the seam that makes headless testing possible; widen it in v2, don't reinvent it.
- The live test scenarios themselves (part/refused-part/kick, create+MODE+TOPIC as op, echo dedupe, caps smoke), re-home them behind a `live` build tag rather than rewriting them.
- modes_test.go's shape: pure reducer in, state asserted out. It is the template for the entire irc/state test suite.
- The render harness mechanics: test.NewApp, forcedTheme with pinned fontScale, offscreen window, Canvas().Capture() to PNG, the golden suite is one deterministic corpus and one compare step away.
- randSuffix nick/channel isolation and the habit of running everything under -race.
- The devdocs invariants and lessons-learned docs, they are the regression-test spec for v2.

### Discard from v1
- Untagged live-network tests as the default `go test .`, the suite must pass on an airplane; network access becomes opt-in via build tag.
- Assertions via substring matching on formatted display lines (containsAll, "You joined #chat") through a shared string channel, assert on state and typed events instead; also drop the hand-rolled contains() for strings.Contains.
- render_art_test's silent corpus swap from /home/lenovo/Downloads and its assertion-free PNG dump, replace with checked-in testdata and golden comparison.
- Single package main for all logic, unexported everything on god objects is why 5k lines have one unit test; testability requires the package layering.
- One flat 25-second time.After deadline per test phase, replace with context-based per-step timeouts sized to the operation.
- The hand-bootstrapped ~/.local/mingw path baked into the Makefile as the only Windows build route, CI should use packaged mingw-w64; keep the local script only as a dev convenience.

### Details
Evidence base: all six test files, Makefile, go.mod, README build notes, and devdocs (engineering-audit.md's 15-bug list, lessons-learned.md, memory.md invariants).

Test pyramid for v2, concretely. Bottom: pure-function tests, proto parsing (server-time as RFC3339Nano, not fixed-layout, per lessons-learned), mode-delta reducers (extend modes_test with list modes, arg-taking modes from ISUPPORT CHANMODES rather than hardcoding), msgid dedupe bounds (v1's seenIDs wholesale-reset-at-8192 quirk deserves a test either way), completion as (draft, cursor, members, channels, commands) -> replacement including the soft-wrap CursorRow trap and @-mention preservation, and gridline column math. Middle: fake-server transcript tests driving a full Session and asserting emitted state diffs, this is where batch classification, echo dedupe, rejoin, and rename-migration live. Top: tagged live tests against testnet.ergo.chat, nightly, non-blocking, plus golden-image render tests (which are cheap and deterministic enough to run on every PR).

Top 10 day-one regression tests, each mapped to an actual v1 bug:
1. PART closes its buffer; a 442/403-refused PART on a stale never-joined buffer also closes it (v1 "leaving channels doesn't work").
2. Self-KICK closes the buffer (part_live_test scenario 3).
3. Reconnect rejoins every open channel and refetches history with zero duplicate lines (severe bug #1 + msgid dedupe).
4. A batch-tagged playback self-JOIN mutates no member state and triggers no history request (the chathistory storm that tripped fakelag).
5. A live multiline batch message that mentions the user notifies, badges, and counts unread (severe bug #2: msgid-absence + recency classification).
6. A sent message renders exactly once even when echo-message arrives seconds late (send_test, made deterministic on the fake server).
7. Drafts are owned by session+buffer: cross-network switches and same-named buffers never leak text (severe bug #4), and Tab completion on a soft-wrapped multi-row draft completes the final word without corrupting the middle (entry.go's documented CursorRow hazard).
8. Channel-rename migrates draft, unread count, and mention flag, and the viewer follows (audit bug #10); Create joins the literal typed name, not the top filter match (bug #6).
9. Golden art renders: mIRC background-color grid alignment, the 16-color palette rows, bold mono, img2irc half-block art, fg-blocks-over-bg, per theme palette, byte-exact.
10. Version stamp: the binary reports git describe at startup/About, and an install-path check confirms the installed binary matches the fresh build (the stale-binary debugging trap).

CI image note for the council: Fyne CGO builds fail on stock ubuntu runners without `libxxf86vm-dev` (v1's whole `CGO_LDFLAGS=-L$HOME/.local/lib` symlink hack exists because that package was missing and sudo was unavailable); the workflow must apt-install gcc, libgl1-mesa-dev, xorg-dev, libxxf86vm-dev, libxkbcommon-dev before `go build`. The fyne test driver is fully headless, no Xvfb needed for unit or golden tests.

Flakiness policy for live tests: dial failure skips with a visible SKIPPED annotation; anything after registration fails hard; nightly schedule plus manual dispatch; never a merge gate. Everything the live tests currently uniquely verify (except real TLS/caps against Ergo) moves down to the fake-server layer.

---

## Accessibility & keyboard-first interaction expert

### Top findings
- Keyboard model today is 5 rebindable actions (close/prev/next/unread/quit, keybinds.go:20-26) plus fixed Alt+1..9; there is no quick switcher, no keyboard scrollback control, and no way to reach the buffer list, member list, or topic without a mouse.
- Message scrollback is keyboard-inaccessible: focus lives permanently in chatEntry (ui.go:527, 926), which consumes Up/Down for history and Tab for completion; msgScroll is never focused, so PageUp/PageDown do nothing. Reading history requires a mouse wheel.
- Tab completion cycles forward only (entry.go:160-239); shiftHeld is already tracked (entry.go:72-84) but complete() ignores it, so Shift+Tab reverse cycling is a ~5-line fix that v1 never made. The shiftHeld KeyDown/KeyUp tracking is also fragile: shift pressed before focus, or released while a dialog has focus, desyncs it (this is the Shift+Enter newline path too).
- Escape has exactly one meaning: close the tracked dialog (ui.go:520-524, trackDialog at 286). It does not cancel a completion cycle, clear a search, dismiss the channel browser, or jump-to-bottom; and trackDialog holds only ONE dialog, so nested/stacked dialogs orphan the outer one.
- Focus after dialog close is not restored: trackDialog's SetOnClosed only nils activeDialog; focus returns wherever Fyne left it (often nothing), and only a buffer switch (showBuffer, ui.go:926) re-focuses the input. Every dialog close should refocus the input explicitly.
- The keybind system cannot express Shift-only or Super/Cmd modifiers (keybindModifiers, keybinds.go:28), has no conflict detection (two actions can share a combo; first case in handleShortcut's switch silently wins, ui.go:533-555), can collide with the fixed Alt+digit jumps (Alt+1..9 digits are offered in keybindKeys), and its two-Select UI (modifier+key dropdowns) cannot capture what a key actually is on the user's layout.
- forcedTheme (menu.go) already does the hard parts right: font scaling 10-28pt applied to text sizes, luminance-guarded custom backgrounds, pure-neutral dark surfaces. But custom accent colors bypass any contrast check (menu.go:51-63), there is no high-contrast preset, and the 180ms smooth-scroll animation (ui.go:78-95) has no reduced-motion opt-out.
- Fyne 2.8 has effectively no desktop screen-reader story: canvas-drawn widgets are not exposed via AT-SPI/UIA/NSAccessibility, so RichText chat is invisible to Orca/NVDA/VoiceOver. The existing selectEntry plain-text mode (ui.go:309-312, 456-460) is the accidental seed of the only viable mitigation.
- Focus traversal in Fyne is weak for this layout: chatEntry's AcceptsTab()=true (entry.go:61) removes it from Tab traversal entirely, Lists are only partially keyboard-navigable, and there is no visible focus-region indicator across the three panes. v1 sidesteps this by never moving focus, which is a coherent choice worth keeping deliberate in v2.
- cursorToEndExact literally replays 32 synthetic KeyDown events to move the cursor (entry.go:244-249) because Fyne's soft-wrap CursorRow can't be mapped to text offsets; cursorOffset() gives up beyond visual row 0 (entry.go:257-262), so mid-draft completion in a wrapped message completes the wrong word. This is a Fyne API gap v2 must design around, not fix.

### Recommendations
- Define the full v2 keyboard model as a spec before code. Buffers: Alt+Up/Down = prev/next, Alt+A = next unread (keep), Alt+1..9 fixed jumps (keep), Ctrl+W close (keep). Panes: Ctrl+B toggle buffer list, Ctrl+U toggle member list. Scrollback: PageUp/PageDown page the message view while input stays focused (delegate from chatEntry.TypedKey to msgScroll), Ctrl+Home/Ctrl+End top/bottom, End or Escape jump-to-bottom-and-resume-follow, with an 'N new messages' pill when scrolled up.
- Build a quick switcher (Ctrl+K): modal fuzzy-filter over all buffers across networks, unread-first ordering, typing filters, Up/Down selects, Enter switches, Escape cancels and restores prior focus. This is the single highest-leverage keyboard feature for a multi-network client and replaces most need for pane focus traversal.
- Give Escape a documented priority stack, dispatched from one place: (1) close topmost dialog, make trackDialog a stack, not a single field; (2) dismiss completion popup / cancel active tab-cycle; (3) cancel quick switcher or browser search; (4) jump message view to bottom. Every dialog and popup close must end with Canvas().Focus(u.input).
- Completion v2: use the already-tracked shiftHeld to make Shift+Tab cycle backwards; render candidates in a small popup above the input (current selection highlighted) so cycling is visible; replace the KeyDown/KeyUp shift tracking with desktop.Driver modifier query or reset shiftHeld on FocusLost to fix desync. Keep the ': ' address suffix and the head/tail-preserving design, it is good. Avoid mid-wrapped-line completion promises until Fyne exposes real cursor offsets; instead keep the input single-visual-line where possible or track the rune offset yourself by intercepting all mutations.
- Rebindable keybinds v2: per-action LIST of combos (JSON in prefs, e.g. kb_next=["Ctrl+PageDown","Alt+Down"]); a press-to-record capture widget (focusable, reads KeyDown + queried modifiers) instead of two Select dropdowns; live conflict detection at record time against all bindings including fixed Alt+digits and reserved input keys (Tab, Enter, Up/Down), shown inline with a one-click 'steal' option; platform-aware defaults using fyne.KeyModifierShortcutDefault so macOS gets Cmd. Skip multi-key chords, no IRC competitor uses them, they fight Fyne's shortcut model, and per-action lists cover the real need (two idioms for one action).
- Screen reader mitigation (Fyne cannot give you a real one): promote selectEntry into a first-class 'reader mode' (toggle, e.g. F6 or Ctrl+R), a plain selectable text view of the current buffer that OS-level SR text review can partially read; pair it with an optional plain-text session log per buffer; and state honestly in docs that native SR support awaits upstream Fyne. Do not fake it with tooltips.
- Accessibility prefs in forcedTheme: (a) high-contrast preset (pure black/white surfaces, thickened focus ring via ColorNameFocus alpha 0xFF, bolder InputBorder); (b) run every user-picked accent through a contrast check against the current background and auto-lighten/darken to >=4.5:1, same spirit as the existing bg-luminance guard; (c) a 'reduce motion' toggle that makes smoothScrollToBottom snap instantly (skip the 180ms animation) and disables any future transitions; (d) verify nick palette colors in colors.go against both variants at the same threshold.
- Keep focus policy explicit and simple: input owns focus by default; every operation that steals it (dialogs, browser, switcher, reader mode) must restore it on exit through one shared helper. Add a visible focus indicator only for the states where focus legitimately leaves the input (switcher list, reader mode), rather than attempting full three-pane Tab traversal that Fyne cannot render well.
- Document the whole map: generate the shortcuts help page (help.go/docs) from the keyActions table itself so bindings and docs can never drift, and show current (not default) combos.

### Keep from v1
- Input-always-focused model with TypedShortcut pass-through on chatEntry (entry.go:65-70), app shortcuts working while typing is the correct core architecture; v2 should extend it, not replace it with focus traversal.
- stepBuffer skipping offline pinned roots so keyboard browsing never triggers a connection (ui.go:567-576), a genuinely thoughtful safety rule.
- Per-buffer draft preservation across switches (ui.go:875-885) and completion head/tail preservation across the cycle (entry.go:222-238).
- The completion source design: slash commands at line start, # channels from joined+LIST, @mentions keeping the @, ': ' suffix when addressing (entry.go:191-232).
- forcedTheme's font point-size scaling (10-28pt) and the background-luminance guard that forces the variant so light-on-light is impossible (menu.go:136-149).
- stickBottom logic that never yanks the view down while the user reads history (ui.go:929-936), and Escape-closes-dialog as a baseline.
- The rebindable-actions table + prefs-string storage shape (keyActions/comboFor) as the skeleton to generalize, and the honest 'Alt+1..9 stays fixed' rule.

### Discard from v1
- The two-dropdown (modifier Select + key Select) rebinding UI (keybinds.go:116-156), replace with a press-to-record capture widget with conflict detection; dropdowns can't represent layouts, Shift-only, or Cmd.
- Single-combo-per-action and the fixed 4-modifier whitelist (keybinds.go:28), replace with per-action combo lists and platform-aware modifiers.
- Single-slot activeDialog tracking (ui.go:286-293), replace with a dialog stack plus mandatory focus restore on close.
- shiftHeld via raw KeyDown/KeyUp bookkeeping (entry.go:72-84), desync-prone; query modifiers or reset on focus events.
- cursorToEndExact's 32-synthetic-keypress hack and the row-0-only cursorOffset fallback (entry.go:244-262), design v2 input so it never needs to reverse-map soft-wrapped cursor positions (own the offset yourself).
- Forward-only Tab cycling with no visible candidate list, superseded by popup + Shift+Tab.
- Unconditional 180ms scroll animation with no reduced-motion path (ui.go:78-95), and unvalidated custom accent colors (menu.go:51-63).
- The flat 100-entry global input history (entry.go:94-103), make it per-buffer and persistent, matching the per-buffer draft model.

### Details
Evidence base: /home/lenovo/eco/entry.go (chatEntry: history, completion, shift tracking, cursor hacks), /home/lenovo/eco/keybinds.go (5-action rebind table, parseCombo, two-Select prefs UI), /home/lenovo/eco/ui.go lines 78-95 (smooth scroll), 286-293 (trackDialog), 520-555 (Escape + handleShortcut), 557-597 (stepBuffer/jumpToUnread/registerShortcuts), 855-940 (showBuffer focus + stickBottom), /home/lenovo/eco/menu.go 22-150 (forcedTheme colors/sizes/applySettings).

What Fyne makes hard, concretely: (1) No desktop accessibility-tree exposure in 2.8, canvas widgets are pixels to NVDA/Orca/VoiceOver; nothing eco can do fixes that, so the pragmatic ceiling is keyboard-complete operation + reader mode + plain-text logs, with SR support explicitly deferred to upstream. (2) Focus traversal: AcceptsTab()=true removes the input from Tab order by design; List keyboard support is partial; a full three-pane Tab cycle would be half-broken and invisible. v1's input-monopoly focus model is the right answer, v2 should formalize it (one restore-focus helper, explicit exceptions). (3) Cursor offsets under word wrap are not recoverable from public API (the 32-keypress hack is the proof); v2's completion and history should own a rune-offset model internally. (4) Shortcut dispatch: canvas AddShortcut + per-widget TypedShortcut is workable but means every focusable custom widget must forward CustomShortcuts to handleShortcut, as chatEntry already does, make that a shared mixin. (5) No system reduced-motion or high-contrast signal reaches Fyne apps, so both must be explicit preferences.

Priority order for the council: quick switcher (Ctrl+K) > keyboard scrollback (PageUp/PageDown from the input) > Escape stack + dialog focus restore > Shift+Tab reverse completion with candidate popup > keybind capture UI with per-action lists and conflict detection > reduce-motion and high-contrast prefs > reader mode. The first four are cheap against v1's architecture (handleShortcut and chatEntry.TypedKey are the only dispatch points) and together take the client from mouse-required to fully keyboard-operable, which for a canvas-rendered Fyne app is the strongest accessibility statement available.

---

## Product manager, open-source developer tools, eco v2 redesign brief

### Top findings
- The stated persona ("beginner friendly IRC client") is real but underspecified: every distinctive design choice, join-all with a 75-channel cap, seeded testnet profile, auto-chathistory, keyring, one-dialog connect, serves an invited newcomer joining a small self-hosted Ergo community, not a generic beginner or a Libera power user. devdocs/scope-audit.md even says join-all "is the whole point of eco on a small network."
- v1's own audits are unusually honest and are the best v2 spec that exists: engineering-audit.md documents 15 real bugs (reconnect-rejoin, batch/playback misclassification, draft misfiling), and memory.md lists the invariants whose violation caused them. None of these invariants are encoded as automated tests beyond two live smoke tests, that is the top architectural debt.
- The mIRC color/art engine (colors.go + gridline.go + render_art_test.go PNG snapshots) is the only genuinely hard-won, hard-to-recreate asset in the codebase. Everything else is replaceable; this is not. It must be treated as a frozen library ported first under golden-image tests.
- There is a large gap between capabilities negotiated and capabilities used: draft/read-marker, extended-monitor, account-tag/account-notify, draft/languages are ACKed and do nothing (scope-audit tier 2). The single highest-value unbuilt feature for the always-on-Ergo persona is read-marker sync, unread state surviving restarts and devices.
- ui.go is a 1,200-line monolith in package main carrying layout, rendering, state keys, and callback wiring; the draft-misfiling and ghost-badge bugs both came from state keyed loosely across that file. v2's architecture win is a real package split with an explicit state store, not new features.
- lessons-learned.md catalogs Fyne 2.x's hard ceilings (unselectable RichText, O(visible) refresh cost, markdown renderer limits, dialog scaling). v2 UX ambitions must be designed inside these limits from day one; the plaintext-mode toggle and 80ms coalesced repaint are workarounds v2 will still need unless Fyne 2.8 has moved.
- The five-item ui-polish-plan.md was approved-pending and never shipped; it is free, debated, already-specified UX improvement that should simply be v2's baseline.
- Redaction is shipped but unreachable (/redact needs msgids the UI never shows), flagged in the engineering audit; a right-click "delete message" is a one-day fix with outsized trust value for community moderation.
- Docs are a real product asset: six embedded single-source guides including an honest power-users wire-behavior doc. The single-source go:embed pipeline should survive v2 untouched.
- The project only just got a git repo (one initial commit) after every audit recommended it; v2 must start with repo + CI gating on the live race-detected testnet tests, or the audit-driven quality culture doesn't transfer.

### Recommendations
- Pick the persona explicitly and gate every v2 feature on it: "the invited newcomer, someone whose friend or community runs an Ergo server and says 'install eco and click Connect'." Secondary persona: the small-network operator who invited them (needs moderation ergonomics: right-click delete, kick/ban menus). Power users are served by not breaking /raw passthrough, nothing more.
- Phase 0, before any redesign code: init CI, port colors.go/gridline.go/render harness verbatim as an internal package, and convert every invariant in memory.md ('Key invariants') plus every bug in engineering-audit.md into an automated regression test. This is the contract v2 is built against.
- Do a strangler rewrite, not a big bang: keep session.go's protocol layer and callback seam (proven across 4 audits), rebuild the UI layer package by package. A ground-up protocol rewrite re-buys the batch/playback, echo-dedupe, and reconnect bugs v1 already paid for.
- MUST for v2: (1) rendering engine preserved under golden tests, the product's soul; (2) read-marker sync, unread state across restarts is the biggest felt gap for always-on Ergo; (3) right-click message delete wiring /redact, shipped feature currently unreachable; (4) ship all five ui-polish-plan items plus optional item 6, already designed and debated; (5) package split with one state store keyed by (session, buffer), root cause class of 4 audited bugs; (6) first-run onboarding kept to one dialog with Advanced fold, the persona's make-or-break moment; (7) surface account-tag data (verified/registered badge on nicks), data already parsed, pure display work, helps newcomers trust who they talk to.
- SHOULD: monitor-based friends list UI (cap already negotiated, natural for small communities); in-buffer scrollback search (Fyne-feasible via filtered re-render); native text selection if Fyne 2.8 allows, else keep plaintext toggle; typing indicators via draft/typing; log-to-disk export; honest README wording ('negotiated' vs 'supported') per scope-audit.
- LATER: macOS build and Flatpak/winget packaging (distribution, not product); link/image inline preview (real value, real scope risk, needs its own design round); draft/languages UI; theming beyond the existing light/dark palettes.
- Success criteria for calling v2 done: (a) render harness PNGs perceptually identical to v1 goldens; (b) all invariant/regression tests green under -race against testnet.ergo.chat in CI; (c) a fresh user reaches first sent message on the seeded testnet in under 2 minutes without opening docs; (d) unread/read state survives an app restart against an Ergo server with persistence; (e) no source file over ~600 lines and session layer still imports zero Fyne; (f) idle CPU ~1% and RSS no worse than v1's ~205MB; (g) single-binary Linux + Windows builds reproduce from make.
- Adopt a written 'negotiated is not a feature' rule: no new IRCv3 cap gets requested in v2 unless a visible behavior ships with it in the same release. This is the anti-scope-creep mechanism the scope-audit implicitly asks for.

### Keep from v1
- colors.go + gridline.go + render_art_test.go, the pixel-perfect mIRC/art engine and its PNG golden harness; port verbatim, freeze, test
- session.go protocol layer and the UICallbacks seam with per-session staleness guards, survived four audits and made multi-network tractable
- The invariant set in memory.md: batch-tagged events never mutate state, local-echo-first with msgid dedupe, reconnect rejoin+refetch, STS persist-only-over-TLS, skey-keyed UI state
- Live race-detected tests against testnet.ergo.chat as the merge gate
- Join-all with 75-channel cap and channel-browser fallback, the small-network onboarding signature move
- One-dialog connect with Advanced fold, seeded testnet + Libera profiles, keyring storage, TLS-by-default
- Single-source embedded docs (docs/ via go:embed) including the honest power-users wire doc
- The ui-polish-plan.md items (all six) as v2 baseline behavior
- Lessons-learned.md as onboarding doc for anyone touching Fyne code, the sharp-edges list prevents re-learning by bug

### Discard from v1
- Single package main with a 1,200-line ui.go, replace with a real package split (protocol / state / render / ui) and one explicit state store; loose state keying caused four audited bugs
- Plain ws:// support, scope-audit tier 1; a footgun eco itself blocks under TLS-only mode; require wss://
- Legacy ergoview keyring migration, dead code that only ever served the dev machine
- /ison and /userhost, obsolete; /monitor, /whois, and raw passthrough cover them
- The 'chase every draft cap' negotiation policy, replace with 'no cap without shipped behavior'; keep only caps whose ACK alone improves correctness (relaymsg, pre-away, persistence)
- README claims of 'supported' for caps that are merely ACKed, v2 docs must say negotiated where nothing happens
- The static 60-character input placeholder and other items the polish plan already replaced on paper
- The plaintext-mode copy toggle if (and only if) Fyne 2.8 offers selectable rich text, otherwise it stays; it is a workaround, not a feature

### Details
Who eco is for. Not "beginners" in the abstract. The design center, visible in every load-bearing decision, is the invited newcomer to a small self-hosted Ergo community: someone with zero IRC context whose community admin runs an Ergo server and needs onboarding to be one dialog. Join-all-under-75 is nonsense on Libera and perfect on a 12-channel community server; auto-chathistory, always-on persistence caps, seeded testnet, and keyring-by-default all point the same way. The secondary persona is that community's operator (Toprak himself is the archetype): they need moderation to be discoverable (right-click delete/kick/ban), not command-line archaeology. Power users get a stable /raw and honest wire docs and nothing else, the moment v2 starts competing with irssi/WeeChat it has lost.

What v2 actually is. The audits show v1's product surface is roughly right and its architecture is the debt: a monolithic UI file, invariants living in a markdown doc instead of tests, and a pile of negotiated-but-inert capabilities. So v2 is (a) an architectural re-founding under a regression contract, and (b) closing the loop on three specific product gaps: read-marker sync (persistent unread state, the always-on persona's biggest daily irritation), reachable redaction, and displaying the account data already being parsed. That is a modest, defensible feature delta. The rest of v2's budget goes to the state-store refactor and test conversion.

Biggest risks, in order. (1) Rendering regression: the art engine is the only asset that took real pain to build (two palettes with shared hue identity, grid art lines, exact rendering); mitigation is Phase 0 verbatim port under golden PNGs, frozen thereafter. (2) Second-system effect: a ground-up rewrite resets four audits' worth of debugging capital, the batch/playback discrimination and echo-dedupe logic encode weeks of live-server pain; mitigation is the strangler approach keeping session.go. (3) Scope creep: v1's identity of chasing every draft cap plus a council of enthusiastic designers is exactly how a 5k-line client becomes a 25k-line one; mitigation is the persona gate, the "no cap without behavior" rule, and the NEVER list below. (4) Losing the beginner focus: every SHOULD item must be re-argued from the newcomer persona, not from feature parity with other clients.

NEVER list (with teeth, v2 refuses to build these, and the council should hold the line): DCC file transfer and CTCP-era file exchange (security surface, zero persona value); any embedded scripting/plugin language (that is irssi's market, not eco's); multi-protocol support or Matrix/Discord/Slack bridging (eco is an IRC client; bridges live server-side); a built-in bouncer/proxy mode (Ergo persistence is the answer; wsconn stays a transport shim); plain ws:// transport; resurrecting /ison and /userhost; a theming DSL or user CSS beyond the existing theme/palette/background options; AI features in the client; forking or patching Fyne internals beyond what the art engine already requires; and requesting any IRCv3 capability without shipping visible behavior in the same release. Each of these has either a documented v1 rejection (scope-audit), a persona mismatch, or a maintenance-surface argument behind it, "someone asked in an issue" is not grounds to reopen them.

---

## text-rendering engineer

### Top findings
- The engine is three layers that already have clean seams: palette/color resolution (colors.go:22-151), prose rendering ircfmt chunks -> RichText segments (richSegments, colors.go:178-212), and art rendering chunks -> TextGrid cells -> artLine widget that draws block glyphs as exact rectangles (gridline.go:87-262). The art/prose fork is one predicate, hasBackgroundArt (gridline.go:46-70).
- Color theming works via a name-indirection protocol: segments carry synthetic theme color names ('ircN' readability-tuned per variant, 'ircxN' exact palette), resolved lazily by forcedTheme.Color -> lookupIRCColor (menu.go:36-41, colors.go:123-139). This is why prose recolors for free on light/dark switch; any extraction must preserve or deliberately replace this protocol.
- Hard-won invariants are encoded as workarounds, not types: a bgTextSegment must never end a line or RichText smears the last colored cell to the right edge (colors.go:206-211); hyperlink segments cannot terminate a row (ui.go:1160-1164); LineSpacing must be 0 for multi-row art (menu.go:116-118); art fg uses exact palette while prose fg uses tuned palette (gridline.go:108-113).
- State leaks: package globals showTimestamps, colorfulNicks, selfNickColor, timeLayout (colors.go:154-159); nickPaletteIdx/namedNickColors; fyne.CurrentApp().Settings() reached from inside gridCellsFor (gridline.go:88-90) and artCellSize (gridline.go:139); gridCellsFor bakes resolved concrete colors into cells at build time, so art lines only follow a theme change if segmentsFor is re-run; the mono flag is derived from buffer name at every call site (strings.EqualFold(name, serverBuffer)).
- Performance: there is no segment cache anywhere. showBuffer re-parses and re-renders up to 250 lines on every buffer switch (ui.go:896-916); appendLine + the 80ms coalesced flush refreshes the entire RichText, O(all shown segments) (lessons-learned.md confirms this cost); artLineRenderer.rebuild() reconstructs every canvas object for every visible art line on each refresh (gridline.go:162-206).
- maxShownSegments = 250*3 (ui.go:296-299) is a wrong-unit cap: segmentsFor emits a variable count (timestamp + nick + N chunks + mention splits + terminator), so heavy lines trim to far fewer than 250, and the trim at ui.go:957-958 cuts at an arbitrary segment boundary, it can decapitate a line mid-way (orphan body without timestamp/nick). Also the trim only runs when stickBottom, so a scrolled-up user accumulates segments without bound.
- Selection is a known hole: RichText is unselectable in Fyne (lessons-learned.md), so v1 ships a plaintext MultiLineEntry overlay (selectEntry/selectOn, ui.go:1043-1065) that throws away all color/format on toggle. gridLineSegment stubs Select/SelectedText entirely (gridline.go:27-29).
- URL linkification does not exist, only @mention hyperlinks (mentionSegments, colors.go:260-302). No image/media preview, no emoji strategy (Fyne has no color-emoji fallback), no strikethrough (ircfmt exposes it; fyne.TextStyle cannot express it), no search-in-buffer.
- render_art_test.go is a scratch harness, not a regression gate: it writes a PNG for eyeballing and silently swaps in a file from /home/lenovo/Downloads if present (render_art_test.go:39-44). The pixel-perfection is verified by human eyes, not by checked-in goldens.
- The art widget's MinSize is cells x cellWidth with no wrapping (gridline.go:146-149), so an over-wide art line forces the whole RichText wide; v1 tolerates this but v2's virtualized view must decide clip-vs-hscroll explicitly.

### Recommendations
- Extract internal/render with this API: RenderOptions{ShowTimestamps bool, TimeLayout string, ColorfulNicks bool, SelfNickColor fyne.ThemeColorName, Mono bool, Epoch uint64} passed explicitly; Segments(l Line, opts RenderOptions, links LinkResolver) []widget.RichTextSegment as the single entry point (it owns ircfmt.Split, the art fork, and prefix construction); ResolveColor(name fyne.ThemeColorName, v fyne.ThemeVariant) (color.Color, bool) exported for the app theme to delegate to, keeping the 'ircN'/'ircxN' name protocol as package API; NickColorName(nick) exported for member-list reuse. Line moves to a shared model package. LinkResolver interface {Mention(nick) func(); URL(raw) func()} replaces the bare func(string) func().
- Kill the globals: showTimestamps/timeLayout/colorfulNicks/selfNickColor become RenderOptions fields built once per applySettings and handed down; gridCellsFor and artCellSize take a resolver/measure function instead of calling fyne.CurrentApp(), this is also what makes the package testable without a live app.
- Add per-line segment caching keyed by (line identity, RenderOptions.Epoch, mono): bump Epoch on any settings/theme change to invalidate everything at once. This makes buffer switches O(new lines only) and makes a virtualized list cheap. Cache the art cell slices too, so artLine.Refresh stops rebuilding rectangles when nothing changed.
- Replace the single RichText with a virtualized message list for v2: one row = one Line, each row a small cached RichText (or artLine). This fixes O(all-segments) refresh per flush, gives natural line-granular trimming, per-row context menus (rowmenus.go already wants this), and a place to hang selection, search highlighting, and previews. Keep the 80ms coalesced flush in front of it.
- Retire maxShownSegments; cap by lines. If any segment-level cap survives an interim release, trim on line boundaries (segmentsFor already knows where lines start), and apply the cap regardless of stickBottom.
- Ship URL linkification in the same pass as mentions: scan plain chunks for schemes/www., emit HyperlinkSegment with an open-external confirm policy; never inside art lines.
- Image/preview policy: no automatic fetching ever (privacy + abuse in public channels); click-to-load thumbnail rows below the message, per-network allowlist toggle, size cap. Design the row model so a preview is an attachment of a Line, not a segment.
- Selection: build it into the v2 virtualized view (per-row hit-testing over cached glyph advances; art rows select as their Textual() string). Keep the plaintext overlay as a copy-all fallback until it works. Do not wait on upstream Fyne RichText selection.
- Turn render_art_test.go into a real golden test inside internal/render: checked-in reference PNGs, per-pixel compare with small tolerance, corpus of the current 9 inline cases plus a checked-in MOTD art file; delete the /home/lenovo/Downloads override. This is the only guardrail protecting the pixel-perfect claim through the v2 refactor.
- Emoji: accept Fyne's monochrome limitation, ship a fallback font with good symbol coverage, and document that color emoji is out of scope for v2.0; add :shortcode: completion in entry.go instead, which is cheap and beginner-friendly.

### Keep from v1
- Both tuned 16-color palettes (light/dark) + exact extendedPalette 16-98 + classicPalette for backgrounds, and the three-way rule of when each applies (tuned for prose fg, exact for fg-over-bg and for art), colors.go:22-151, gridline.go:108-113
- The 'ircN'/'ircxN' theme-color-name indirection that makes prose recolor live on variant switch (colors.go:93-119, menu.go:40)
- The entire art pipeline: hasBackgroundArt heuristic (mono || painted*2 >= total, block-glyph detection 0x2580-0x259F), gridCellsFor, and artLine's rectangle-drawn blockGeom table incl. eighth-blocks and shade alphas, gridline.go
- bgTextSegment (rect stacked behind text) and the never-end-a-line-with-bg/hyperlink terminator workarounds, these encode real Fyne layout bugs
- Embedded DejaVu bold-mono fonts via forcedTheme.Font (menu.go:216-229) and SizeNameLineSpacing=0 for art continuity
- mentionSegments word-boundary scanning and the snapshot-once mentionResolver pattern that keeps the session lock off the render path (ui.go:1172-1197)
- The 80ms coalesced flush with segsDirty/unreadDirty split (ui.go:945-974) and the stickBottom near-bottom follow rule
- The offscreen PNG render-harness concept in render_art_test.go, promote it, don't drop it

### Discard from v1
- Package globals showTimestamps/timeLayout/colorfulNicks/selfNickColor in colors.go:154-159, replace with explicit RenderOptions
- fyne.CurrentApp().Settings() reads inside gridCellsFor/artCellSize, inject resolver and measurer
- maxShownSegments segment-count cap and its mid-line front-trim (ui.go:296-299, 957-958), replace with line-granular capping in a virtualized view
- One monolithic RichText holding every visible line, refreshed whole per flush, replace with virtualized per-line rows
- Full re-parse/re-render of the last 250 lines on every buffer switch in showBuffer, replaced by the per-line cache
- The plaintext selectEntry overlay as the primary selection mechanism (keep only as temporary copy-all fallback)
- The hardcoded /home/lenovo/Downloads/ircd-motd-puckman-hq.txt override in render_art_test.go:39, replace with checked-in fixtures and goldens
- mono-ness derived from buffer name at each call site (strings.EqualFold(name, serverBuffer)), make it a buffer property carried in RenderOptions

### Details
The extraction is genuinely low-risk because the seams already exist: session.go produces Line, ui.go's segmentsFor is the only entry point, and everything below it (richSegments, mentionSegments, bgTextSegment, hasBackgroundArt, gridCellsFor, artLine, the palettes) is self-contained except for four globals and two fyne.CurrentApp() reaches. Proposed package shape: internal/render exporting Segments(Line, RenderOptions, LinkResolver) []widget.RichTextSegment, ResolveColor(name, variant) for the app theme to delegate into, NickColorName, and the golden-test harness; internal/model owning Line. The subtle contract to preserve during extraction is color-resolution timing: prose segments resolve lazily via theme names (live re-theming works), while art cells resolve eagerly at build time (gridline.go:92, 112), so a theme change must bump the cache epoch and rebuild art rows, which v1 gets away with only because applySettings happens to trigger a full rebuild.

Performance priorities in order: (1) per-line segment cache keyed on (line, epoch, mono), it makes everything else cheap; (2) virtualized per-line message view replacing the single RichText, which converts the O(all-visible-segments) refresh per 80ms flush into O(changed rows) and gives line-granular history trimming, selection anchoring, and per-row menus; (3) delete maxShownSegments, it's a wrong-unit heuristic (3 segs/line is optimistic; timestamp+nick+chunks+mention splits+terminator routinely exceed it) whose front-trim can cut mid-line and which never runs while scrolled up; (4) stop artLineRenderer.rebuild() from reconstructing every rectangle on every refresh by caching built visuals per row.

Missing features, with policy stances: URL linkification is the highest-value gap (mentions already prove the HyperlinkSegment path); previews must be click-to-load only, modeled as Line attachments rather than segments, with per-network opt-in, auto-fetching URLs posted in public IRC channels is a privacy/abuse hole; selection must be owned by the new message view (art rows select as Textual() text) rather than waiting on upstream Fyne; emoji stays monochrome with shortcode completion; strikethrough is representable by ircfmt but not by fyne.TextStyle, either draw it in the custom row widget or document the gap.

Finally, the pixel-perfect claim is currently protected only by a human looking at a PNG. Before any v2 refactor touches gridline.go, convert render_art_test.go into checked-in golden-image comparisons inside the new package; that single test is what lets the council refactor aggressively without re-fighting the alignment battles recorded in lessons-learned.md.

---

## Completeness critic, gaps, contradictions, and unvalidated assumptions across the 10-expert eco v2 research panel

### Top findings
- SECURITY/THREAT MODEL IS ABSENT: no expert audited hostile input or credential handling. Verified in code: saveNetworks (networks.go:90-110) silently falls back to storing SASL and server passwords as PLAINTEXT JSON in Fyne preferences when no keyring/Secret Service is available (common on minimal Linux setups); nobody flagged it. Also unexamined: desktop notifications include full message text (ui.go:656,709, lock-screen leakage), exec of external binaries (notify-send/paplay/zenity/powershell) as an attack/portability surface, no cert-pinning/CertFP UI, and no threat model for a malicious server or channel content (over-wide art lines already force unbounded widget width per the rendering expert, an unrecognized DoS vector).
- PRIVACY OF THE PROPOSED MESSAGE STORE IS UNDESIGNED: three experts converge on a durable on-disk SQLite log as v2's foundation, but nobody addressed retention policy, DM logs on shared machines, encryption-at-rest, per-network/per-buffer logging opt-out, or the interaction with REDACT, if v2 honors message deletion for moderation trust (PM's MUST #3) but keeps redacted text in a local search index, the feature is self-contradicting. IRC culture is log-sensitive; this needs an explicit privacy design, not just a schema.
- NOBODY VALIDATED FYNE ITSELF FOR A GROUND-UP V2: the a11y expert proves Fyne 2.8 cannot do screen readers, text selection, or cursor offsets; the rendering expert documents RichText workarounds as 'invariants'; the designer's whole plan is Fyne-shaped. For a from-scratch v2 the toolkit question (stay on Fyne vs gioui/Qt/webview) was never asked, every expert assumed the answer. Even if Fyne wins (the art engine and shipped code argue it should), the decision should be made once, explicitly, with the a11y ceiling accepted in writing.
- STRANGLER VS REWRITE CONTRADICTION: the PM mandates 'keep session.go's protocol layer, strangler rewrite, a ground-up protocol rewrite re-buys the bugs v1 paid for', while the protocol engineer's P0s (ISUPPORT fold layer, real BATCH tracking, labeled-response echo) require deleting session.go's core heuristics, and the staff engineer's migration plan rewrites session.go's event flow entirely. These are incompatible plans for the same file; the council must pick one migration philosophy before phase 0.
- PERSONA VS ROADMAP CONTRADICTION: the PM gates v2 on 'the invited newcomer on a small Ergo server; power users get /raw, nothing more', while the IRC veteran's top recs (SQLite global search, triggers/aliases, smart join-part filters, 40-channel activity management) and the a11y expert's full keyboard model are a power-user roadmap. Search itself is ranked #1 by the veteran and appears only as a SHOULD for the PM. Unvalidated assumption underneath: no actual user of either persona was interviewed, the entire panel reasoned from code and docs.
- PERFORMANCE ON HUGE CHANNELS/NETWORKS WAS NEVER SIZED: experts cover render-window costs, but nobody examined the member pane with 2000+ nicks (u.members fully re-sorted via SortedMembers on every MembersChange, ui.go:773-779), RAM cost of holding Libera's ~30k LIST results for tab completion, the proposed SQLite store's size/vacuum behavior after months in 40 channels, or reconnect burst behavior on large networks. v1's 205MB RSS baseline is mentioned but not explained; no expert set performance budgets for the v2 architecture they proposed.
- DISTRIBUTION, UPDATES, AND SANDBOX CONSTRAINTS ARE A BLIND SPOT: PM defers packaging to LATER, but packaging constrains architecture now, Flatpak sandboxing breaks the exec-based notify-send/paplay/zenity paths and changes keyring access (portals); an unsigned Windows .exe hits SmartScreen, killing the beginner persona's 'install and click Connect' on the platform most beginners use; there is no update channel at all, so STS/TLS/security fixes cannot reach users. macOS is wholly absent despite Fyne supporting it and the notification/sound/keyring code being Linux+Windows only (notify.go).
- I18N/UNICODE IS COMPLETELY UNEXAMINED: all UI strings are hardcoded English with no extraction plan, docs are English-only, highlight matching is explicitly ASCII-only (containsWord/isNickChar, a Turkish/German/CJK nick never highlights), and the designer's commit-to-DejaVu-Sans-Mono-everywhere rec silently breaks CJK/Arabic/emoji-heavy channels (no CJK glyphs, no RTL story, double-width cells vs the mono column model). The mono-everywhere decision also conflicts with the Discord-native beginner persona and was validated by no one.
- AUTHORITATIVE-STATE CONFLICT: the veteran makes the local SQLite store 'the authoritative buffer model'; the protocol engineer makes server-side read-markers and CHATHISTORY the source of truth for unread/history (the killer Ergo feature). With bouncers/multiclient these diverge (duplicates, marker vs local-count disagreement, server-redacted vs locally-stored lines); the sync/merge model between local store and server history was never designed. Related smaller gap: no expert planned v1→v2 data migration (profiles, keyring entries, prefs, STS store) even though real users exist.
- SIDEBAR ARCHITECTURE DISAGREEMENT LEFT OPEN: veteran and product-UX both require collapsible per-network trees with activity sort; the visual designer explicitly recommends keeping a flat widget.List and adopting Tree 'only if collapsible networks is a real requirement'. Since multi-network grouping is also load-bearing in the protocol engineer's NetworkManager rec, this is a decided-by-default conflict that will surface mid-build if the council doesn't rule on it.

### Recommendations
- Run a 1-day security/privacy review before architecture freeze: fix the plaintext-password keyring fallback (refuse to persist, or encrypt with an app key and warn), define notification-content redaction option, write a 1-page threat model (malicious server, hostile channel content, shared machine), and specify the disk-store privacy contract: default logging scope, per-buffer opt-out, retention setting, REDACT deletes from the local store and index.
- Force three explicit council decisions with written ADRs before code: (1) toolkit, Fyne yes/no, with the a11y ceiling accepted in the ADR; (2) migration philosophy, strangler on session.go vs event-sourced rewrite (recommended resolution: rewrite the event/batch/echo core per the protocol engineer, but port it behind the QA engineer's fake-server regression suite so v1's paid-for bugs are encoded as tests first, this reconciles the PM's real concern); (3) sidebar flat-list vs tree.
- Resolve the persona conflict with a two-tier roadmap: v2.0 ships the newcomer persona (PM's MUST list) plus exactly two power-user features that also serve newcomers, durable history with in-buffer search, and per-buffer notification levels; defer triggers/aliases/global-search-pane to v2.x. Validate with 3-5 real users of each persona (the small-Ergo community operators are reachable) before locking scope, nobody on the panel talked to a user.
- Design the local-store/server-history sync model explicitly: server msgid is the primary key, CHATHISTORY and read-markers are authoritative where the server supports them, local store is a cache+extension (search, non-Ergo servers); document merge rules for duplicates, redactions, and marker conflicts. This single document reconciles the veteran's and protocol engineer's plans.
- Set performance budgets now and add them to CI: member-pane update cost at 3,000 nicks, memory ceiling for LIST results (stream to disk or cap), SQLite store size/query latency at 1M messages, reconnect behavior on a 40-channel profile. Add one synthetic 'huge channel' scenario to the planned fake-server harness.
- Treat packaging as a v2.0 architectural input, not a LATER: pick Flatpak+winget as targets, route notifications/sound through Fyne/portal-compatible APIs instead of exec (or feature-detect), budget for Windows code-signing, and add a minimal update notifier (version check against a static URL) so security fixes are deliverable.
- Do a cheap i18n floor even if full l10n waits: extract user-facing strings behind a lookup from day one, make highlight/nick matching Unicode-aware (casefold per CASEMAPPING, not ASCII), and test the mono-font decision against a CJK/emoji corpus before committing, fall back to mono-for-gutter/art + proportional body if it fails.
- Add a v1→v2 migration task to phase 0: import profiles, keyring entries, STS policies, keybinds, and prefs; test it, since the seeded-profile first-run and the migration path share code.

### Keep from v1
- Keyring-first credential storage (networks.go), but with the plaintext-JSON fallback fixed, not silent
- STS policy persistence with expiry/revocation (sts.go) and TLS MinVersion 1.2 floor (session.go:461)
- notify-send '--' argument-injection guard (notify.go), small but correct security habit worth porting
- The mIRC color/grid-art engine under golden tests (panel consensus, endorsed)
- Embedded single-source docs pipeline (go:embed), extend it to generated keybinding docs per the a11y expert
- Fake-server + regression-test-first phase 0 (QA engineer's plan), it is also the mechanism that resolves the strangler-vs-rewrite conflict safely
- Seeded testnet/Libera profiles and the channel-browser join/create flow

### Discard from v1
- Silent plaintext password fallback in saveNetworks, the one concrete security bug the whole panel missed
- exec()-based notification/sound/color-picker paths as the only implementation, they break under Flatpak sandboxing and don't exist on macOS; keep at most as feature-detected fallbacks
- Full message text in desktop notifications as a hardcoded behavior, make notification content a privacy setting
- ASCII-only highlight matching (containsWord/isNickChar), replace with CASEMAPPING-aware Unicode folding
- Unbounded in-RAM retention of LIST results and full member re-sort per change, replace with capped/streamed structures sized against explicit performance budgets
- The panel's implicit assumptions: 'Fyne is a given', 'packaging comes later', 'logging to disk is unambiguously good', and 'monospace everywhere is safe', each needs an explicit decision, not inheritance

### Details
Method: read the panel's condensed findings, then verified suspected blind spots directly in /home/lenovo/eco. Key evidence: networks.go:90-110 (saveNetworks) keeps SASL/server passwords in plaintext preferences JSON whenever keyring.Set fails, the comment even documents it ('the password stays in the JSON as before'); ui.go:656 and 709 pass full message bodies to u.notify, which shells out to notify-send (notify.go); notify.go's sound path is freedesktop-only with a PowerShell branch for Windows and nothing for macOS; the Makefile confirms the hand-rolled ~/.local/mingw Windows build with no signing story; FyneApp.toml shows version 0.1.0 with no update mechanism; ui.go:381-386,773-779 shows the member list as a fully re-sorted flat slice; session.go:595/1090 confirms LIST results accumulate per connect; go.mod confirms Fyne 2.8 with go-locale present but unused for i18n. The ten experts are individually strong and collectively cover protocol, rendering, architecture, UX, testing, and product scope very well, the misses cluster in exactly the areas outside any single expert's brief: security/privacy as a discipline, distribution/updates as an architectural input, internationalization, scale limits beyond the render path, and four genuine inter-expert contradictions (migration philosophy, persona/scope, authoritative state, sidebar structure) that no one was tasked to arbitrate. The single most consequential process gap: every expert reasoned from code and docs; no finding is grounded in observation of an actual user, despite the persona debate being the panel's central unresolved tension. Recommended next step for the council: three written ADRs (toolkit, migration philosophy, state authority), a 1-day security/privacy pass fixing the keyring fallback, and short validation interviews before the v2 scope is frozen.
