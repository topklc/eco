# eco v2 — Final Blueprint

**Status:** RATIFIED — this is the build document. It supersedes the three council designs
("beginner-first", "power-first", "balanced") and incorporates the judge panel's verdicts.
**Base design:** *beginner-first* (chosen by 2 of 3 judges, including the engineering-feasibility
judge, who verified its code contact and migration ordering against the actual v1 tree), amended
by the grafts and corrections listed in §2. Every judge-flagged conflict is ruled explicitly in §2;
nothing in this document papers over a disagreement.

Inputs: `devdocs/v2/research.md` (11-expert panel), the three council designs, three judge
verdicts (beginner lens, power lens, feasibility lens). v1 facts used below were verified against
the tree: 6,376 lines of Go across 27 files in one `package main`; `session.go` 1,394 lines;
`ui.go` 1,198 lines; `UICallbacks` has **12** fields; module `github.com/toprakkilic/eco`,
Go 1.25, Fyne 2.8.0; roughly **one third** of v1 by line count moves verbatim (not the 60%
one design claimed).

---

## 1. Product vision & governance

### 1.1 Vision

eco v2 is the first IRC client a Discord native genuinely enjoys in the first five minutes —
and the same binary answers WeeChat questions in month three without a "power mode" switch.
A person picks a name, clicks a card, and lands in a live channel with zero protocol vocabulary.
Months later that same person has durable, searchable history; unread state that agrees with
their phone; per-channel notification levels; a quick switcher across networks; and complete
keyboard operation — all discovered progressively, because the UI teaches its own depth.

The pixel-perfect mIRC color/art engine — the one asset in v1 that took real pain to build and
that no competitor replicates — is preserved verbatim under golden-image tests. "Pasted art just
looks right" is itself a first-five-minutes delight.

### 1.2 Personas

- **Primary (session one):** the invited newcomer — a Discord native whose friend or community
  runs an Ergo server and says "install eco and click Connect." Every default and every screen
  is designed for this person.
- **Primary (month three):** the *same person*, grown into a daily driver across 2–3 networks.
  The retention truth (from the power judge, adopted): *a newcomer on a daily-driver client grows
  into it; a veteran on a newcomer client leaves.* The week-one newcomer IS the month-three
  power user; losing them at that transition hands them to irssi.
- **Secondary:** the small-community operator who invited them — needs discoverable moderation
  (right-click delete/kick/ban, reachable redaction) and the per-profile auto-join-all opt-in.

### 1.3 Governance — the rules that settle future arguments

1. **Two clocks govern every surface decision and every default** (beginner-first's
   constitution, kept intact):
   - *Time-to-first-conversation:* under 2 minutes on the seeded testnet without opening docs.
   - *Fearlessness:* no default screen may show protocol jargon, raw errors, or
     irreversible-feeling state before the user has sent a message.
2. **The foundation rule governs architecture** (power-first's causality, adopted): you cannot
   retrofit a message store, a notification engine, or a typed event bus. Foundations are built
   for the month-three daily driver even when session one never sees them.
3. **Tiebreaker when 1 and 2 collide:** a foundation item may never add screens, words, or
   required choices to the first session (depth is a shortcut, never a toll booth); a surface
   item may never compromise wire correctness or the store's integrity. If a dispute survives
   both tests, the two clocks win — that is what "beginner-first as base" means.
4. **Parity Rule:** every right-click or keyboard action has a visible surface somewhere
   (header kebab, hover strip, switcher, settings). Power paths are shortcuts, never secrets.
5. **Teach-in-place (passive form):** menus show their shortcut and slash-command equivalent
   ("Join a channel… — /join"); the jump pill names its key ("N new messages ↓ (End)").
   The usage-counting hint engine is deferred (§2 R-12).
6. **"Negotiated is not a feature":** no IRCv3 capability is requested unless visible behavior
   ships in the same release.
7. **The NEVER list has teeth** (§3.4). "Someone asked in an issue" does not reopen it.

---

## 2. Ruled conflicts

Every disagreement flagged by the panel or the judges, with the ruling and the reason.
These are binding.

| # | Conflict | Positions | RULING | Why |
|---|----------|-----------|--------|-----|
| R-1 | **Base design / governance** | beginner judge & feasibility judge: beginner-first base. power judge: power-first base ("architecture for the veteran"). | **beginner-first is the base.** Its two-clock constitution governs surfaces; power-first's foundation rule is adopted as co-governance with the explicit tiebreaker in §1.3. | 2 of 3 judges; deepest verified code contact; safest migration ordering. The power judge's real complaint was priority starvation of specific features — fixed by R-8, not by switching base. |
| R-2 | **Toolkit** | Critic demanded an explicit ADR; all designs assumed Fyne. | **Fyne 2.8 stays.** The a11y ceiling (no AT-SPI/UIA exposure, no RichText selection API) is accepted **in writing** and mitigated per R-5/§4.8. No forking or patching Fyne beyond what the art engine already requires. | The art engine, the shipped code, the theme system, and the fixed constraint all point one way. Deciding it explicitly ends re-litigation. |
| R-3 | **Migration philosophy** (PM's strangler vs protocol engineer's rewrite) | Strangler keeps session.go; rewrite deletes its core heuristics. | **Test-first core rewrite.** v1's paid-for bugs (memory.md invariants + the 15-bug audit + the QA top-10) are encoded as offline fake-server regression tests FIRST (M5); the session core is rewritten under that green suite (M6) with **dual-core parity** — the suite runs against BOTH the v1 session and the new core until they emit identical Changes — then the old core is deleted at cutover (M7), never "later". Everything outside the core is strangled incrementally. | Reconciles both concerns: the debugging capital is preserved as tests, not as code; the correct foundations (fold, BATCH, labels) cannot be grafted onto text-matching heuristics. Dual-core parity (power-first's device) is the strongest stability guarantee any plan offered. |
| R-4 | **State authority** (veteran's local-store-as-truth vs protocol engineer's server-as-truth) | — | **The server wins where it speaks; the archive extends it everywhere else.** msgid is the primary/merge key. CHATHISTORY and MARKREAD are authoritative on servers that support them; the SQLite archive is a durable cache + extension (search, offline reads, non-chathistory networks, where it IS authoritative). REDACT deletes the row **and** its FTS entry. Marker conflicts resolve to **max(server, local)** position. Multiclient (phone+desktop) transcripts are named regression cases. On networks without read-markers/chathistory, the UI says so honestly ("history and read-state are local to this device on this network") — never silent divergence. | The critic's demanded reconciliation; max(server,local) and multiclient transcripts grafted per the feasibility judge; honest degradation copy per the power judge. |
| R-5 | **"Native selection" overclaim** (all three designs) | All designs promised native drag selection; feasibility judge proved Fyne 2.8 has no RichText selection and `Label.Selectable` cannot carry colored segments. | **Honest mechanism, mandated wording:** per-row **Copy message** (hover cluster + context menu), Copy-link/Copy-selection-of-rows via multi-row select-mode is **not** promised; **Reader mode (F6)** — a selectable plain-text view of the current buffer — is promoted to MUST as the copy-anything/screen-reader fallback; custom cross-row drag selection (hit-testing over cached glyph advances) is designed as a v2.x LATER item, and the Timeline row model must not preclude it. The words "native selection" are banned from docs and code comments. | A blueprint that designs around an API that does not exist fails at the riskiest step. This is the feasibility judge's mandatory correction, adopted verbatim. |
| R-6 | **Sidebar: flat list vs tree** (designer's flat vs veteran's tree; power-first ADR'd a real tree) | — | **Styled flat `widget.List` with collapse implemented as row filtering** (beginner-first's mechanism) + **disclosure by invitation** (balanced's rule): with one network, no section headers, no chevrons, no network chrome — a plain channel list; network headers, collapse, rollup pills, and per-network identity appear when network #2 is added. `widget.Tree` is rejected. | Collapse IS a real requirement (veteran), but Tree's template/selection model makes pills, inline [Connect] buttons, and rollup rows harder (feasibility). All three judges grafted disclosure-by-invitation. |
| R-7 | **Join defaults** (PM's join-all vs beginner researcher's flooding) | beginner-first: starter chips + opt-in; balanced: tiered. | **Tiered:** ≤10 channels → auto-join everything with an explanatory banner ("Joined 7 channels — this community is small enough to see all of it"); 11–75 → starter-set chip list (top 5 by users, one click); >75 → embedded Discover view. Per-profile "auto-join everything" opt-in survives for operators. LIST is lazy (on Discover open), cached per network — never eager on connect. | All three judges grafted the tiering; it reuses v1's existing `joinAllLimit=75` machinery in the 323 handler (cheapest to build); it gives the invited newcomer the Discord-server-join feeling on exactly the networks where that is right. |
| R-8 | **Power-feature starvation** (power judge's dealbreakers with beginner-first) | beginner-first had global search, smart join/part filter, Shift+Tab, keybind capture, CertFP, hostmask ignores as SHOULD; highlight word lists absent. | **Promoted to MUST:** global Finder (Ctrl+Shift+F, thin overlay on the FTS index that already exists for Ctrl+F); smart join/part filter (default on — it serves beginners as noise reduction); user highlight word lists with CASEMAPPING-aware Unicode folding (also fixes the ASCII-only `containsWord` i18n bug); per-network hostmask ignores + `/ignore`; SASL EXTERNAL (CertFP) while the sasl package is open; Shift+Tab reverse completion with candidate popup; press-to-record keybind capture with conflict detection. | Each is cheap against the architecture this blueprint already mandates; deferring them re-ships the exact dealbreakers the power judge documented ("month-three me opens Ctrl+Shift+F and it isn't there"). None adds a screen, word, or choice to session one — they pass the §1.3 tiebreaker. |
| R-9 | **Aliases / triggers / hooks** (veteran's "the client must bend" vs beginner-first's refusal) | beginner-first: refused. power-first: v2.x on a designed bus. | **LATER (v2.x), not NEVER:** `/alias` with parameter substitution and data-driven trigger rules hang off the typed Event/Change pipeline, which IS the event bus — it exists as architectural fact, not promise. **Embedded scripting languages remain NEVER.** | The bus costs nothing extra (it is the core architecture); an ideological wall converts month-three users into irssi users. Scripting is still irssi's market. |
| R-10 | **Mono-everywhere typography** (designer's commitment vs critic's CJK/emoji flag) | All designs gated it, but at Step 15 — after the identity was baked. | **Corpus pulled forward:** CJK/emoji/RTL samples enter the golden corpus at **M1** (day one); the all-mono default is decided by a named gate at **M11**, before the Timeline default flips. Designed fallback: mono for gutter/timestamps/nicks/art + proportional body (the render engine's per-segment fonts can express this). | Both the beginner and power judges demanded the gate move before the Timeline freeze; typography alienation is the largest remaining way to lose the Discord native. |
| R-11 | **Empty states & emoji** (beginner judge's flags on power-first) | — | Empty-state component is **MUST** everywhere a beginner can be stranded; **`:shortcode:` emoji completion is MUST** (monochrome rendering accepted and documented; color emoji is LATER). | A Discord teen types `:joy:` in the first five minutes; empty panes are the exact stranding moments the research flags. Both are cheap (completion engine + static table; one reusable component). |
| R-12 | **Teach-in-place hint engine** (balanced's usage counters vs power judge's "labels only" vs feasibility's scope flag) | — | **Passive labels are MUST** (shortcut + slash-command shown in menus, keys named on pills). The usage-counting "offer the fast path after third use" engine is **LATER**. | 90% of the value at ~0% of the machinery; zero cost to veterans; removes a scope-creep vector the feasibility judge flagged. |
| R-13 | **Goldens vs mechanical split ordering** | power-first split first, froze second; beginner-first froze first. | **Goldens freeze BEFORE any file moves** (M1 before M2). Theme-token work (M11) is additionally gated on "goldens must not change". | A regression introduced by the move itself must not be baked into the masters (feasibility judge). |
| R-14 | **Mid-ladder shippability** (feasibility: beginner-first had no checkpoint; balanced had a cliff) | — | Named ship checkpoints: **C1** "armored" after M5 (internal), **C2 v2.0-alpha "v1 face, v2 memory"** after M9 (history + unread survive restart under the v1 UI), **C3 v2.0-beta** after M12. Beginner-visible wins ship as they land (keyring warning M3, "caught up N messages" M9, unknown-command intercept M10) rather than pooling at the end. | Solo-dev morale and stakeholder patience need shippable artifacts; power-first's checkpoint device was the best structural idea any design offered. |
| R-15 | **Windows SmartScreen / code signing** | All designs deferred; beginner judge called it a minute-one killer. | Still LATER as budget, but now a **named pre-GA decision gate**: before the v2.0 tag, either fund a signing cert or ship the download page with an explicit SmartScreen walkthrough and document the risk acceptance. Betas ship unsigned. | A blueprint cannot conjure money, but it can refuse to let the decision happen by default. |
| R-16 | **Update delivery** | Absent from power-first; SHOULD elsewhere. | **Minimal update notifier is MUST** (version check against a static URL, notify-only, no auto-update). | A daily driver that cannot receive security fixes is a liability; STS/TLS fixes need a delivery channel (power judge, adopting beginner-first's item). |
| R-17 | **i18n floor** | Unexamined in v1; SHOULD in designs. | **MUST-lite:** all *new* user-facing strings go through a lookup table from M6 onward; v1 strings are extracted opportunistically as each file is rewritten; full l10n is LATER. **Unicode/CASEMAPPING-aware matching is MUST** (highlights, nick handling). | Retrofitting l10n into a shipped daily driver is the expensive path; the folding fix is a correctness bug, not a nicety. |
| R-18 | **No observed users** (balanced's own risk; power judge grafted) | — | 3–5 interviews with real small-Ergo operators and their invited newcomers run during M0–M2 (non-blocking). Findings may re-rank SHOULD items; they may not unseat MUST architecture. | Cheap insurance against imagined personas; must not stall the ladder. |
| R-19 | **Bouncer/soju story** (power judge: "Ergo persistence is the answer" is false on Libera) | — | v2.0: the archive is the answer for history on non-chathistory networks (it is authoritative there per R-4), with honest degradation copy for read-marker sync. Explicit soju/BOUNCER-networks support is **LATER**. Built-in bouncer mode remains **NEVER**. | Honesty over false promises; the store already covers the daily-driver need on Libera-class networks. |

---

## 3. Scope

### 3.1 MUST (v2.0 does not tag without these)

**Protocol correctness core**
1. ISUPPORT layer: per-network ServerProps — CASEMAPPING `fold()` applied to every map key,
   CHANTYPES, PREFIX (modes+symbols), STATUSMSG, MODES/CHANMODES limits. Deletes
   `strings.ToLower` keying and every hardcoded `#` / `~&@%+`.
2. Real BATCH registry `{id → type, params, parent, collected}`: playback classified by
   enclosing-chathistory chain; multiline assembled at batch close. Deletes
   isPlayback-by-tag-presence and the 2-minute `liveML` heuristic.
3. Labeled-response correlation for all outgoing messages: render Pending immediately, resolve
   in place on echo (real msgid + server time) or FAIL. Deletes `pendingEcho` text matching;
   own messages become redactable/markable; multiclient echo is safe. Whois and request/reply
   flows correlate per label (deletes the single `whoisTarget` slot).
4. SCRAM sequenced before autojoin (fixes the +R race); SASL failure is a visible banner,
   never a silent log line. SASL EXTERNAL (CertFP) client certificates (R-8).
5. Reconnect done right: exponential backoff with jitter; rejoin with tracked channel keys;
   per-buffer `CHATHISTORY AFTER <last msgid>` gap-fill; `CHATHISTORY TARGETS` for offline DM
   discovery; quiet "reconnected — caught up 214 messages" status line, never a replay flood.

**Memory & attention**
6. Per-profile SQLite archive (modernc.org/sqlite, pure Go): msgid-keyed messages + FTS5,
   write-behind from state; buffer RAM lines become a bounded cache over it. Survives restarts.
7. Infinite scroll-up: archive first, then `CHATHISTORY BEFORE msgid=<oldest>`; exact msgid
   dedupe (deletes the 8192-wipe map).
8. Read-marker sync: send MARKREAD on blur/focus-bottom; consume it for cross-device unread.
   Rail pills, the NEW divider, jump-to-unread, hotlist order, and the OS badge ALL derive from
   the marker (max(server, local) on conflict; honest local-only copy on non-supporting networks).
9. In-buffer search (Ctrl+F) and the global Finder (Ctrl+Shift+F) over FTS5 with
   `nick:/channel:/network:/before:/after:` filters (R-8).
10. Per-buffer notification levels (All / Mentions only / Mute) with per-network defaults;
    user highlight word lists with CASEMAPPING-aware Unicode folding; focused-visible-buffer
    suppression; notification click focuses the buffer; notification-content privacy setting
    (full text / sender only / count only).
11. Smart join/part filter, default on: show join/part/quit only for recently-active nicks;
    per-channel and global overrides (R-8).
12. Per-network hostmask-pattern ignores with an `/ignore` command (R-8).

**Timeline & rendering**
13. Render engine ported **verbatim** into `internal/render` under checked-in golden-image PNG
    tests (both palettes, two font scales, CJK/emoji/RTL corpus) — frozen before any other
    refactor (M1).
14. Virtualized per-message row Timeline replacing the monolithic RichText: per-row
    RichText/artLine via `widget.List` + `SetItemHeight`; hover action cluster (Copy, Quote,
    Delete-own); NEW divider + day separators; author grouping (2-min window, never status
    lines); jump-to-bottom pill with count; scroll-up loading row; behind `ECO_NEWCHAT` flag
    with a 10k-line relayout budget and a designed fixed-height-row fallback. Plaintext select
    mode and `maxShownSegments` deleted after the flip. Honest copy stance per R-5; Reader mode
    (F6) is MUST.
15. Reachable redaction: hover/context "Delete message" on own messages (msgids now real),
    tombstone rendering; REDACT deletes from archive **and** FTS index.
16. URL linkification in the same pass as mentions (HyperlinkSegment, open-external confirm,
    never inside art rows). `:shortcode:` emoji completion (monochrome accepted) (R-11).

**Beginner surface**
17. Two-step welcome wizard: name (prefilled `newbie`+suffix) → three cards (sandbox / Libera /
    "I have a server address", TLS 6697 assumed) with a quiet "Advanced setup" link. The network
    manager survives only as Settings ▸ Networks with every protocol field.
18. Never land on the server buffer: testnet auto-joins a curated default channel; otherwise
    tiered join per R-7. Server buffer demoted to a collapsed "Network log" row; caps/scan
    chatter suppressed there by default.
19. Humanized failure: dial/TLS/SASL/nick errors as plain sentences with [Retry]/[Edit]/[Fix
    login] in inline banners; connection state text adjacent to the dot; 433 renames visible.
20. Identity chip (nick + logged-in state, away toggle, gear) permanently at the Rail bottom;
    "Claim this nickname" flow running REGISTER/VERIFY with the email code as a form field,
    password saved to keyring.
21. Empty-state component everywhere a beginner can be stranded (empty channel, no channels,
    Discover zero-results, disconnected). Unknown-command intercept ("did you mean /join? —
    Enter again sends raw"; raw passthrough only via explicit `/raw` or second Enter).
22. Rail v2 per R-6: custom rows (status dot, name, grey unread pill, accent mention pill),
    disclosure-by-invitation network headers with explicit [Connect] on dimmed offline rows
    (click-row-to-connect deleted), draggable `container.Split` widths.
23. Quick Switcher (Ctrl+K): fuzzy over open buffers (mention/unread-first) + cached LIST
    results + "Create #x" row — browser.go's flow promoted to the primary navigation verb.
24. Keyboard survival kit: PageUp/PageDown scrollback from the always-focused composer;
    Ctrl+Home/End; documented Escape priority stack backed by a dialog **stack** (not v1's
    single slot); one shared focus-restore helper on every dialog close; Shift+Tab reverse
    completion with a visible candidate popup (shiftHeld desync fixed via modifier query);
    Alt+A hotlist (mentions first, then unread); Alt+1..9 on user-pinned buffers; press-to-record
    keybind capture with conflict detection; shortcuts help page generated from the binding table.
25. Settings shell: left-nav list + content pane (replacing 7-tab AppTabs), opening on a single
    **Essentials** page (theme, font size, notifications); instant-apply and zenity pickers kept.
26. Teach-in-place passive labels per R-12.

**Security, privacy, ship floor**
27. Keyring fallback fixed: when no keyring exists, eco **refuses to persist** secrets and shows
    a warning — the silent plaintext-JSON fallback is deleted. 1-page threat model written at M0.
28. Archive privacy contract shipped in the same release as the archive: logging on by default
    with per-buffer opt-out, DM logging prompt on first message for shared-machine profiles,
    retention setting (forever / 365 / 90 / 30 days / session-only, with vacuum), REDACT purge,
    one toggle to exclude all DMs. No encryption-at-rest in v2.0 — documented honestly.
29. v1→v2 data migration on first launch: profiles, keyring entries, STS policies, prefs,
    keybinds — tested.
30. Minimal update notifier (static-URL version check, notify only) (R-16).
31. Test/CI foundation as product: scripted net.Pipe fake server encoding every memory.md
    invariant and audit bug; golden render suite on every PR; live tests behind `//go:build live`
    (nightly, never merge-gating); `-race` default; goleak on session teardown; version stamped
    via `-ldflags -X main.version=$(git describe)` shown at startup and in About.
32. Theme token layer: forcedTheme promoted to semantic surface/text/state tokens; every
    call-site NRGBA (status dots, swatches, welcome) routed through it; `ircN`/`ircxN` protocol
    untouched; goldens must not change.

### 3.2 SHOULD (budget-permitting in v2.0; first candidates for v2.0.x)

- Typing indicators (+typing): send in DMs by default with 3s debounce (channel opt-in,
  sending toggleable); rendered as one muted line above the composer.
- Presence layer from caps already negotiated: away-notify dims People rows; account-tag/
  account-notify registered badges; chghost/SETNAME tracked; MONITOR-backed DM presence dot.
- Lag/ping hint on the network status area.
- Per-buffer persistent input history (replacing the 100-entry global RAM ring).
- Accessibility prefs: reduce-motion (snap scroll), high-contrast preset, accent contrast guard
  (warn under ~3:1 reusing the luminance logic).
- Link previews: strictly click-to-load, per-network opt-in, default OFF, modeled as Line
  attachments (never auto-fetch — reader IP leak).
- Plain-text session logs per buffer (reader-mode companion).
- Packaging groundwork: portal-aware notification path behind the Notifier interface;
  Flatpak/winget target lists.

### 3.3 LATER (v2.x — designed for, not shipped in v2.0)

- `/alias` with parameter substitution and data-driven trigger rules on the typed Event/Change
  bus (R-9).
- Custom cross-row drag selection via per-row hit-testing over cached glyph advances (R-5).
- Usage-counting teach-in-place hint engine (R-12).
- soju / BOUNCER-networks explicit support (R-19).
- macOS build; Flatpak/winget packages; Windows code signing (pre-GA decision gate, R-15).
- Color emoji; full l10n; theming beyond theme/palette/background options within token limits.

### 3.4 NEVER (refused, with teeth)

- DCC file transfer and CTCP-era file exchange.
- Embedded scripting/plugin language (Lua/JS/etc.) — that is irssi's market.
- Multi-protocol support or Matrix/Discord/Slack bridging.
- Built-in bouncer/proxy mode (server-side persistence + the archive are the answer).
- Plain `ws://` transport; `/ison`; `/userhost`.
- Threads, read receipts, reactions-as-input, avatars (received TAGMSG reactions may render
  passively at most).
- Auto-fetching link/image previews.
- Theming DSL or user CSS.
- AI features in the client.
- Requesting any IRCv3 capability without shipping visible behavior in the same release.
- Forking or patching Fyne beyond what the art engine already requires.

---

## 4. UX specification

### 4.1 Window anatomy — named regions

One window; three regions in draggable `container.Split`s (v1's fixed 170px rectangles die),
plus an overlay layer.

- **Rail** (left, default 220px, Ctrl+B toggles; one-step-darker surface via `ThemeOverride`):
  the buffer sidebar. Custom row widgets in a **styled flat `widget.List`** — 8px status/activity
  dot, name, right-aligned pill (grey = unread count, accent-filled = mentions). Per R-6,
  with one network there is no network chrome (quiet caption at most); at network #2, **network
  header rows** appear: name, connection dot, collapse chevron (collapse = filtering rows),
  unread rollup pill when collapsed, and a visible ⋯ overflow menu (Connect / Disconnect / Edit /
  Find channels / Show network log) — right-click mirrors it, never replaces it. Channels then
  DMs under each header. Offline networks render dimmed with an inline **[Connect]** button;
  clicking the row itself never connects. Rail bottom: **identity chip** — current nick,
  logged-in badge, away toggle, gear.
- **Chat** (center), three stacked parts:
  - **Header:** buffer name; member-count button (toggles People); topic on a second line
    (click-to-expand, click-to-edit with ops); search icon (Ctrl+F); kebab menu carrying every
    action that also lives on right-click (Parity Rule): notifications level, autojoin toggle,
    channel settings, invite, mark read, leave.
  - **Timeline:** §4.2.
  - **Composer:** v1's entry behavior preserved — per-buffer drafts, Shift+Enter multiline with
    the n/max hint, tab completion (nicks with `: ` addressing, #channels incl. LIST, /commands,
    @mentions), plus Shift+Tab reverse cycling with a visible candidate popup, `:shortcode:`
    emoji completion, unknown-command intercept. Above it, single-line contextual strips:
    typing indicator (DMs, SHOULD), disconnected banner with [Reconnect], SASL-failed banner
    with [Fix login].
- **People** (right, per-buffer remembered visibility, Ctrl+U): member list with filter box,
  sectioned by prefix rank (~& / @ / % / + / rest), away-dimmed (away-notify), registered badges
  (account-tag), hover kebab for op actions.
- **Overlays** (Escape-dismissable, never blocking modals where avoidable, every close restores
  composer focus through one shared helper): **Switcher** (Ctrl+K), **Finder** (Ctrl+Shift+F),
  **Discover** (embedded center view, not a popup), **Settings** (left-nav + pane), **Welcome**.

### 4.2 The Timeline

Virtualized per-message rows. Row anatomy, irssi-dense (LineSpacing 0, no avatars, no bubbles):
muted mono timestamp gutter (text-faint token), bold colored nick column (soft-aligned, min 8
cells, no hard truncation), wrapped body rendered by the verbatim segment engine; art lines are
their own row type drawn by `artLine`. One airy concession: 4px spacing when the author changes.
Author grouping: same nick within 2 minutes under one nick header — never grouping status lines
or the Network log. Special rows: day dividers; one persistent **NEW — since 14:02** divider
seeded from the server read marker; "Loading older…" row during backfill. Hover reveals the
action strip — Copy message, Quote, Delete (own, → REDACT, → tombstone) — mirrored in the row
context menu. When scrolled up, the jump pill reads "N new messages ↓ (End)". Arrival never
yanks the view (v1's stickBottom discipline, verbatim). Whole chat pane monospace (DejaVu Sans
Mono + shipped bold faces) **pending the M11 corpus gate**; fallback: mono gutter/nicks/art +
proportional body. Chrome stays proportional.

Copy stance (R-5): per-row copy + Reader mode (F6, selectable plain-text view of the buffer).
No "native selection" claims anywhere.

### 4.3 Navigation & keyboard model (spec'd before code)

Focus lives in the Composer; app shortcuts pass through it (v1's correct core, kept).
- **Buffers:** Alt+Up/Down prev/next (skipping offline roots — v1's safety rule kept);
  **Alt+A hotlist** — mentions first, then unread (WeeChat semantics); Alt+1..9 = user-pinned
  buffers (not absolute rows); Ctrl+K for everything else; Ctrl+W close.
- **Panes:** Ctrl+B Rail, Ctrl+U People, F6 Reader mode.
- **Scrollback:** PageUp/PageDown delegate from the Composer to the Timeline; Ctrl+Home/End;
  End (or Escape, per the stack) jump-to-bottom-and-follow.
- **Search:** Ctrl+F in-buffer, Ctrl+Shift+F Finder.
- **Escape priority stack**, dispatched from one place: (1) topmost dialog — dialog **stack**,
  not v1's single `activeDialog` slot; (2) completion popup / cancel tab-cycle; (3) Switcher /
  Finder / search; (4) jump-to-bottom. Every close path ends with the shared
  focus-restore-to-composer helper.
- **Rebinding:** per-action combo lists (JSON), press-to-record capture widget (reads key +
  queried modifiers — fixing shiftHeld desync), live conflict detection incl. fixed Alt+digits
  and reserved input keys, platform-aware defaults (`KeyModifierShortcutDefault` so macOS gets
  Cmd later). No multi-key chords. The shortcuts help page is generated from the binding table.

### 4.4 Notification & unread model

One pipeline, marker-derived. Source of truth: the server read marker per buffer
(archive-local fallback where unsupported, honestly labeled). Everything derives from it:
Rail pills, NEW divider, jump-to-unread, hotlist order, OS badge (mention count only).
Evaluation order: per-buffer level (Mute / Mentions / All, per-network default, set from header
kebab or row context menu) → highlight match (own nick + user word list, Unicode-folded per
CASEMAPPING) → suppression if the buffer is focused and visible → desktop notification (body
per privacy setting; click focuses the buffer). Playback and status lines never count (v1 rule
kept). Three ad-hoc maps (unread, hlBuffers, drafts-adjacent) become one service.

### 4.5 First-run flow

Step 1: "What should people call you?" — one field, `newbie`+suffix prefilled, big Continue.
Step 2: three cards — *Try the sandbox* (Ergo testnet), *Join the open-source hub* (Libera),
*I have a server address* (one text field; TLS 6697 assumed) — plus the quiet "Advanced setup"
link opening the full Networks form. No SASL, PASS, port, or mechanism vocabulary on the happy
path. On connect the user lands **in a channel**, never the server buffer: sandbox auto-joins
the curated default (e.g. #chat); elsewhere the tiered join logic of R-7 runs. Failures are
screens/banners with actions, not log lines. The Network log is a collapsed row under its
header — where yesterday's beginner discovers what the wire looks like, on their own schedule.

### 4.6 Key flows

- **Joining & discovery.** Ctrl+K: type, Enter joins top match; no match → "Create #name" with
  the options form (browser.go's flow, promoted). Discover is the embedded browsing view (lazy
  LIST + progress, cached per network, zero-results → Create empty state). The +k key prompt
  survives join refusals.
- **Chatting.** Grouped, dense, per §4.2. Delete-own sends REDACT and renders a tombstone.
  Typos get "Unknown command /jion — did you mean /join? (Enter again sends raw)".
- **Identity & claiming.** The identity chip always shows nick + auth state (fixing invisible
  SASL failure and silent 433 renames). Its menu and "this nick is registered" hints offer
  **Claim this nickname**: password (+email) form → REGISTER → VERIFY code as a form field →
  keyring save → logged-in badge confirmed.
- **Failure.** "Couldn't find that server. Check the address. [Edit] [Retry]" in the center
  pane or banner; SASL failure banner with [Fix login]; the dot always has adjacent text.
- **Multi-network.** Each network is an independent state machine; buffer identity is
  (networkID, folded name) everywhere; notification defaults, ignores, and highlight words
  scope per network; the Switcher and Finder span networks. Adding a network = gear → Networks
  → Add (same three-card form).
- **Month-three morning (the retention scenario).** Badges and NEW dividers are already correct
  — the phone moved the markers overnight. Alt+A hops mentions-first; each buffer opens at its
  divider; MARKREAD fires on leave; scroll-up pages out of the archive then CHATHISTORY BEFORE;
  "what did X say about the deploy?" is Ctrl+Shift+F. A noisy channel gets header kebab →
  Notifications → Mute; a critical one gets a highlight word from the same menu. Reconnects
  show "reconnected — caught up 214 messages".

### 4.7 Empty states

One reusable component (icon, one-line title, one action): empty channel → "Quiet right now —
say hi"; no channels joined → "Find your people" [Open Discover]; Discover zero-results →
[Create #name]; disconnected → sentence + [Reconnect]. The Welcome screen refits the same
pattern.

### 4.8 Accessibility stance (honest)

Fyne 2.8 exposes no accessibility tree to screen readers; eco cannot fix that client-side.
The shipped maximum, stated plainly in docs: complete keyboard operation (§4.3), Reader mode
(F6) + optional plain-text logs, high-contrast preset and reduce-motion pref (SHOULD),
generated shortcut docs. Native SR support is explicitly deferred to upstream Fyne.

---

## 5. Technical architecture

### 5.1 Governing ADRs

- **ADR-1 Toolkit:** Fyne 2.8 stays; a11y ceiling accepted in writing (R-2).
- **ADR-2 Migration:** test-first core rewrite with dual-core parity; strangler outside the
  core (R-3).
- **ADR-3 State authority:** server msgid/CHATHISTORY/MARKREAD where supported; archive as
  cache+extension with written merge rules (R-4).
- **ADR-4 Sidebar:** styled flat List + collapse-as-filtering + disclosure by invitation (R-6).

### 5.2 Package layout (module `github.com/toprakkilic/eco`, unchanged; Go 1.25, Fyne 2.8)

```
cmd/eco/                 main: wiring only (config → networks → dispatcher → ui); version stamp
internal/model/          shared types: Message, Line (+semantic flags), BufferID{NetworkID,
                         Folded string}, Member, typed Event and Change unions, stable IDs. Pure.
internal/proto/          pure protocol tables & parsing: ISUPPORT → ServerProps (fold(),
                         CHANTYPES, PREFIX, STATUSMSG, MODES/CHANMODES limits), cap-set model,
                         batch type registry defs. Table-tested, zero deps on irc/state.
internal/irc/            wire client: ircevent wrapper with Dialer seam → Events() <-chan
                         model.Event; labeled-response ledger; BATCH runtime registry;
                         SCRAM-before-join sequencing; exponential backoff+jitter; STS parse.
                         Zero fyne imports.
  internal/irc/transport/  wsconn.go (verbatim)
  internal/irc/sasl/       scram.go (verbatim) + EXTERNAL (CertFP)
internal/state/          Store: single-writer reducers. Apply(Event) → []Change. Buffers
                         (folded keys), members, topics/modes/keys, markers, unread derivation,
                         notification-rule evaluation, per-network ignores, pending-message
                         resolution, reconnect planning. Pure Go, zero fyne imports.
internal/archive/        SQLite (modernc.org/sqlite) per profile: messages + FTS5, markers,
                         buffer meta, list cache; write-behind Recorder consuming Changes;
                         Pager serving backfill; retention/opt-out enforcement. Pure.
internal/render/         THE FROZEN COMPONENT: colors.go + gridline.go + bgTextSegment +
                         artLine verbatim, with their workaround comments; Segments(Line,
                         RenderOptions, LinkResolver) []RichTextSegment; ResolveColor(name,
                         variant); NickColorName(nick); per-line segment cache keyed
                         (lineID, epoch, mono); golden tests + checked-in corpus.
internal/command/        Parse(text, ctx) → typed Command; Run(env) over narrow env
                         {Session() state.Handle; Send(irc.Outgoing); UI UIHooks}.
internal/config/         immutable Settings behind atomic.Pointer[Settings]; profiles;
                         keyring (refuse+warn fallback); STS policy store; keybind
                         persistence; v1 import.
internal/notify/         Notifier interface; feature-detected backends (notify-send/portal-
                         ready/Windows); '--' injection guard kept; content-privacy option.
internal/ui/             Dispatcher (the ONLY fyne.Do site), viewmodel (rows, drafts, pins,
                         scroll anchors — testable, no widget refs), rail.go, timeline.go,
                         roster.go, composer.go, switcher.go, finder.go, discover.go.
  internal/ui/theme/       forcedTheme → semantic token layer (+ untouched ircN/ircxN)
  internal/ui/widgets/     bufferRow, messageRow, pill, statusDot, emptyState, capture widget
  internal/ui/dialogs/     settings shell, networks page, claim-nick, join-options
  internal/ui/onboard/     welcome wizard, tiered-join logic
internal/testsrv/        scripted net.Pipe fake IRC server + transcript replay
internal/transcript/     test-only: 21 scripted scenarios replayed against irc+state and
                         compared with recorded behaviour in testdata (was internal/parity,
                         the M6 dual-core gate; see the M7 progress entry)
docs/                    embedded guides (go:embed), pipeline unchanged
testdata/                art corpus (incl. CJK/emoji/RTL), golden PNGs, fake-server transcripts
```

### 5.3 Layer interfaces & event flow

Exactly one pipeline, one threading contract (each package doc carries its one sentence:
`irc` emits on its own goroutine; `state` is confined to the dispatcher goroutine; `ui` runs
only on the Fyne thread; `render` and `archive` are pure/reentrant; the state mutex never
crosses a package boundary; `fyne.Do` exists only in the Dispatcher).

```
wire → irc.Client (own goroutine)
     → Events() <-chan model.Event
         PrivMsg | Notice | Join | Part | Quit | Nick | Topic | Mode | BatchOpen | BatchClose |
         CapChange | ConnState | Redact | MarkRead | ListItem | JoinRefused | AuthResult |
         HistoryChunk | Typing | …           (every event tagged NetworkID)
     → state.Store.Apply(Event) → []model.Change     (dispatcher goroutine, single writer)
         MessageAdded | MessageResolved | MessageRedacted | HistoryChunk | BufferCreated |
         BufferClosed | BufferRenamed | MembersChanged(delta) | TopicChanged | UnreadChanged |
         ReadMarkerMoved | ConnStateChanged | TypingChanged | ListUpdated | AuthResult | …
     → archive Recorder (write-behind queue, own goroutine)
     → ui.Dispatcher: drains Changes, coalesces on the proven 80ms tick,
       ONE fyne.Do per batch — replacing all 12 UICallbacks and their hand-rolled
       stale()/active() guards with a single funnel keyed by stable session IDs
       (never the mutable Name string).
```

Reverse path: Composer text → `command.Parse` → typed Command → `command.Run(env)`;
`UIHooks` = {OpenBuffer, ShowDialog, ShowHint, Quit}. **All wire writes live in
`internal/irc`**; nothing else touches ircevent; ~90% of commands unit-test headless.

Outgoing messages are created Pending with a label, rendered instantly, and resolved in place
by the labeled echo (gaining real msgid + server time) — deleting `pendingEcho`, `liveML`,
`isPlayback`, and `whoisTarget` wholesale.

### 5.4 Storage design

One SQLite DB per network profile at the platform data dir
(`<data>/eco/archive/<profileID>.db`), WAL mode, `modernc.org/sqlite` (pure Go — keeps the
mingw cross-build simple).

```sql
messages(
  rowid INTEGER PRIMARY KEY,
  msgid TEXT UNIQUE,              -- dedupe + merge key + pagination anchor (ADR-3)
  buffer_folded TEXT NOT NULL,    -- fold() applied
  ts INTEGER NOT NULL,            -- server-time ms
  sender TEXT, account TEXT,
  kind INTEGER NOT NULL,          -- privmsg/notice/action/status/tombstone
  body TEXT, tags_json TEXT,
  pending INTEGER NOT NULL DEFAULT 0,
  redacted INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_messages_buffer_ts ON messages(buffer_folded, ts, rowid);
-- FTS5 external-content on messages(body, sender, buffer_folded); REDACT deletes row + FTS entry
buffers(
  name_folded TEXT PRIMARY KEY, display_name TEXT, kind INTEGER, key TEXT,
  last_msgid TEXT, read_marker_msgid TEXT, read_marker_ts INTEGER,
  notify_level INTEGER, logging INTEGER NOT NULL DEFAULT 1, pinned INTEGER
);
list_cache(channel TEXT PRIMARY KEY, users INTEGER, topic TEXT, fetched_at INTEGER);
```

- **Live path:** state appends → Recorder batch-inserts (write-behind). Buffer RAM lines are a
  bounded cache over the archive.
- **History path:** scroll-up serves from the archive; misses issue `CHATHISTORY BEFORE
  msgid=<oldest>`; reconnect issues `CHATHISTORY AFTER <last>` per buffer + `TARGETS` for
  offline DMs; LIST results **stream into `list_cache`**, never resident in RAM.
- **Authority & merge (ADR-3):** dedupe collapses on msgid; server wins where it speaks;
  REDACT deletes row + FTS entry and leaves a tombstone message; marker conflicts →
  max(server, local); the archive is authoritative on non-chathistory networks, with honest
  UI copy about local-only state.
- **Privacy contract (ships with the archive, M9, not after):** logging on by default
  (Discord-native expectation) with per-buffer opt-out; DM logging prompt on shared-machine
  profiles; retention forever/365/90/30/session-only with vacuum; exclude-all-DMs toggle;
  notification-content setting; no encryption-at-rest in v2.0, documented.
- **Budgets (CI):** query p95 at 1M rows; insert throughput during a 40-channel reconnect
  burst; DB size/vacuum behavior tracked. Numbers are set at first real profiling (M9) and
  may be revised only with a written note (risk R-10).

### 5.5 Render engine contract (frozen)

`colors.go` + `gridline.go` + `bgTextSegment` + `artLine` move verbatim with their workaround
comments intact (never-terminate-a-line-with-bg, hyperlink-terminator, LineSpacing 0, exact-vs-
tuned palette split, eager art-color resolution → theme changes bump the cache epoch).
API: `Segments(Line, RenderOptions{ShowTimestamps, TimeLayout, ColorfulNicks, SelfNickColor,
Mono, Epoch}, LinkResolver{Mention, URL}) []widget.RichTextSegment`; `ResolveColor`;
`NickColorName`. The four display globals and the two `fyne.CurrentApp()` reaches
(`gridCellsFor`, `artCellSize`) are replaced by RenderOptions + injected resolver/measure
functions **at M4, under already-green goldens** — never during extraction (R-13). Per-line
segment cache keyed (lineID, epoch, mono) makes buffer switches O(new lines) and the
virtualized Timeline cheap. Goldens gate every later step; the M11 token work is additionally
gated on "goldens must not change".

### 5.6 Security floor

- Threat model (1 page, M0): malicious server (over-wide art lines → width DoS: Timeline art
  rows clip with h-scroll inside the row, decided explicitly; hostile numerics), hostile
  channel content (links, art), shared machine (archive, DM logs, notification content),
  credential handling (keyring-only; refuse+warn fallback), exec surface (notify/zenity behind
  feature-detected interfaces; `--` injection guard kept).
- TLS floor: wss:// only; TLS MinVersion 1.2; STS persistence with expiry/revocation (verbatim).
- Update notifier (R-16) so security fixes are deliverable.

### 5.7 v1 file-by-file disposition (all 27 .go files)

| v1 file | lines | Disposition | Destination | Notes |
|---|---|---|---|---|
| `main.go` | 20 | rewritten | `cmd/eco/main.go` | wiring only; version stamp printed at startup + About |
| `colors.go` | ~340 | **verbatim** | `internal/render` | palettes, ircN/ircxN protocol, bgTextSegment + smear workaround comments; globals → RenderOptions at M4 |
| `gridline.go` | ~262 | **verbatim** | `internal/render` | hasBackgroundArt, gridCellsFor, artLine blockGeom; `fyne.CurrentApp()` reaches → injected seams at M4; per-row visual cache added |
| `render_art_test.go` | 66 | adapted | `internal/render` | → checked-in corpus + golden PNG byte-compare, both palettes × two scales, `-update` flag; Downloads override deleted (M1, in place) |
| `wsconn.go` | 85 | **verbatim** | `internal/irc/transport` | correct IRCv3 websocket adapter |
| `scram.go` | ~70 | **verbatim** | `internal/irc/sasl` | sequenced before autojoin; EXTERNAL added alongside |
| `sts.go` | 77 | **verbatim** | `internal/config` (store) + parse hook in `internal/irc` | expiry/revocation model kept |
| `session.go` | 1394 | **rewritten** | `internal/irc` + `internal/state` | handler *logic* ports into Store.Apply reducers under the M5 suite; wiring, casemapping, batch/echo/whois correlation replaced; join-refusal UX, applyModeDelta (extended per CHANMODES), multiline send batching survive as reducer logic; pendingEcho/liveML/isPlayback/whoisTarget deleted |
| `ui.go` | 1198 | **rewritten** | `internal/ui/*` | god-struct → Dispatcher + viewmodel + widgets; preserved behaviors: 80ms flush, stickBottom, AmendBuffer don't-yank, draft/rename migration (now tested); deleted: plaintext select mode, maxShownSegments, 12 stale/active guards, click-row-to-connect |
| `entry.go` | ~300 | adapted | `internal/ui` composer | completion/draft/history UX preserved; shiftHeld → modifier query; owns a rune-offset model (no 32-keypress hack); history → per-buffer persistent (SHOULD) |
| `browser.go` | ~150 | adapted | `internal/ui` switcher + discover | join/create flow promoted to Ctrl+K; Discover keeps browsing; Create joins the literal typed name (audit bug #6 test) |
| `commands.go` | ~340 | **rewritten** | `internal/command` | 300-line switch → registered table over narrow env; unknown-command intercept; `/raw` explicit passthrough |
| `rowmenus.go` | ~430 | **rewritten** | `internal/ui` | per-row context menus (last-message-only limitation gone); every action mirrored on a visible surface (Parity Rule); op menus survive |
| `networks.go` | ~430 | adapted | `internal/config` + `internal/ui/dialogs` | profile store shape + keyring-first kept; **silent plaintext fallback deleted → refuse+warn**; manager dialog → Settings ▸ Networks page; seeded profiles kept |
| `menu.go` | ~240 | adapted | `internal/ui/theme` + shell | forcedTheme mechanism, embedded bold-mono fonts, luminance guard, dialogSize → token layer verbatim-then-extended; menus rebuilt |
| `prefs.go` | ~330 | adapted | `internal/ui/dialogs/settings` | page contents carry over (instant-apply, zenity); AppTabs shell → Essentials + left-nav |
| `keybinds.go` | ~180 | adapted | `internal/config` + `internal/ui/widgets` | mechanism generalized: per-action combo lists, capture widget, conflict detection; two-dropdown UI deleted |
| `ignore.go` | ~90 | **rewritten** | `internal/state` (+ persistence in config) | global nick-only set → per-network hostmask patterns + `/ignore` |
| `notify.go` | ~80 | **verbatim** | `internal/notify` | behind Notifier interface, feature-detected; `--` guard kept; content-privacy option added |
| `help.go` | ~110 | adapted | `internal/ui` + docs pipeline | shortcuts page generated from the live binding table; go:embed pipeline unchanged |
| `icon.go` | 8 | verbatim | `cmd/eco` | embedded icon |
| `util.go` | 12 | verbatim | `internal/model` (or inlined) | trivial helpers |
| `modes_test.go` | 23 | adapted | `internal/state` | the template for all reducer tests; extended with list/arg modes per CHANMODES |
| `smoke_test.go` | 29 | adapted | `//go:build live` (M0) + scenario re-encoded on testsrv (M5) | caps smoke |
| `send_test.go` | ~40 | adapted | same | echo dedupe under delayed echo → deterministic fake-server test |
| `part_live_test.go` | ~70 | adapted | same | PART / refused PART (442/403) / self-KICK scenarios |
| `create_live_test.go` | ~60 | adapted | same | create + MODE + TOPIC as op |

Deleted concepts (no destination): plaintext select mode, `maxShownSegments`, `pendingEcho`,
`liveML`, `whoisTarget`, isPlayback-by-tag, skey prefix-purging (stable session IDs), config
globals (~14), click-row-to-connect, join-all default, eager LIST, plain ws://, `/ison`,
`/userhost`, legacy ergoview keyring migration, single-slot `activeDialog`, 100-entry global
input history (replaced), fixed 30s ReconnectFreq, NAMES-on-every-MODE, 8192-wipe seenIDs.

---

## 6. Migration plan

Fifteen milestones, M0–M14. Every milestone leaves `go build ./...` green and
`go test ./...` passing offline. Ship checkpoints: **C1** (internal) after M5, **C2 =
v2.0-alpha** after M9, **C3 = v2.0-beta** after M12, **v2.0** after M14.

### M0 — Safety rails (no code moves) — START HERE

Deliverables, in order:
1. **CI** at `.github/workflows/ci.yml`, ubuntu-latest:
   - `apt-get update && apt-get install -y gcc libgl1-mesa-dev xorg-dev libxxf86vm-dev libxkbcommon-dev`
     — with an in-file comment documenting the libXxf86vm gotcha (v1's
     `CGO_LDFLAGS=-L$HOME/.local/lib` hack exists because this package was missing).
   - `test -z "$(gofmt -l .)"` and `go vet ./...`.
   - `go test -race ./...` — fully headless (the fyne test driver needs no Xvfb).
   - Linux build with `-ldflags "-X main.version=$(git describe --always --dirty)"`.
   - Separate Windows job: `apt-get install -y gcc-mingw-w64`, then
     `CGO_ENABLED=1 GOOS=windows CC=x86_64-w64-mingw32-gcc go build` (replaces the hand-rolled
     `~/.local/mingw`; the local script stays as a dev convenience).
   - Upload both artifacts.
2. **Version stamp:** add `var version = "dev"` to main.go, print at startup, show in About.
   `make install` verifies the installed binary's stamp matches the fresh build (kills the
   stale-binary trap).
3. **Live-test gating:** add `//go:build live` to `smoke_test.go`, `send_test.go`,
   `part_live_test.go`, `create_live_test.go`. `make test` = offline `-race`; `make test-live`
   = `go test -race -tags live` with per-phase context timeouts. `go test ./...` now passes on
   an airplane.
4. **Threat model:** write `devdocs/v2/threat-model.md` (1 page, §5.6 scope). Ratify the ADRs
   (§5.1) by committing this blueprint.
5. **Interviews:** contact 3–5 small-Ergo operators + their invited newcomers (R-18);
   non-blocking.

Done when: CI green on a clean runner with no network; binary prints a git-describe version;
the flat `package main` still builds and runs identically.

### M1 — Freeze the crown jewel (goldens BEFORE any file moves)

In place, still `package main`:
1. Create `testdata/art/` — move the 9 inline cases out of `render_art_test.go`, add a
   checked-in MOTD art file, mIRC background-grid samples, img2irc half-block art, the 16-color
   palette rows, **and CJK / emoji / RTL / double-width text samples** (R-10 — corpus exists
   from day one; the mono *decision* gate runs at M11).
2. Delete the `/home/lenovo/Downloads/ircd-motd-puckman-hq.txt` override
   (render_art_test.go:39–44).
3. Convert the harness to golden tests: `test.NewApp()` + forcedTheme with pinned variant and
   font scale, fixed offscreen window size, `Canvas().Capture()` → PNG, **byte-compare**
   against `testdata/golden/<case>_<palette>_<scale>.png`, `-update` flag to regenerate.
   Matrix: both palettes (light/dark) × two font scales (13pt, 18pt).
4. Add a numeric unit test for gridline column math (alignment failures self-diagnose without
   pixel-diffing).

Done when: goldens byte-stable across two consecutive CI runs; every corpus file (incl. CJK/
emoji) renders without panic. **These goldens gate every later milestone.**
Tests added: golden suite (runs on every PR forever), column-math unit test.

### M2 — Mechanical split
`main.go` → `cmd/eco/`; every other root .go file → `internal/app/` (package app) in ONE
rename-only commit; export the few symbols main needs. `go build ./cmd/eco` green; goldens
green (they moved with the package). Tests: unchanged suite must pass untouched.

### M3 — Verbatim leaf extraction (one compiling commit each)
`internal/render` (colors.go, gridline.go, segmentsFor entry, goldens — still reading globals);
`internal/notify` (behind the Notifier interface, feature-detected backends);
`internal/irc/transport` (wsconn.go); `internal/irc/sasl` (scram.go);
`internal/config` (sts.go store, networks.go profile store — **with the plaintext-JSON keyring
fallback replaced by refuse-to-persist + visible warning**: first beginner-visible security win,
ships in any build cut from here). `internal/app` imports them all.
Tests: goldens green after the render move (the point of M1-before-M2); a unit test asserting
the keyring-refusal path warns and does not write secrets.

### M4 — Kill the globals
`config.Settings` immutable snapshot behind `atomic.Pointer[Settings]`; render takes explicit
`RenderOptions`; the `fyne.CurrentApp()` reaches in `gridCellsFor`/`artCellSize` get injected
resolver/measure functions (**under green goldens** — the balanced seam inventory executed at
the safe point); ignore lists and join/part hiding become per-network data. Small compiling
diffs per call-site batch (~14 globals die).
Tests: goldens unchanged; settings-snapshot race test under `-race`.

### M5 — The regression contract (BEFORE the rewrite) → checkpoint C1 "armored"
`internal/testsrv`: scripted `net.Pipe` fake IRC server + transcript replay. Add a Dialer seam
to the **v1** Session. Encode as `TestRegression_*` against the still-v1 session logic:
- every `devdocs/memory.md` invariant and every engineering-audit bug;
- the QA top-10 (playback self-JOIN storms, live multiline mentions/highlights, refused PART
  closes buffer, self-KICK, delayed-echo dedupe, draft ownership across networks, rename
  migrates draft/unread/viewer, reconnect rejoins without duplicates, create-literal-name,
  version stamp);
- **multiclient transcripts** (phone + desktop echo and marker races — R-4);
- a **10k-line synthetic buffer** transcript (perf corpus for M11).
Assert on state/Changes, never display strings. CI runs everything offline with `-race` + goleak.
Done when: suite green against v1. This suite is the safety net for everything after.

### M6 — New protocol core, dark (dual-core parity)
`internal/model` (Event/Change/Message/BufferID) + `internal/proto` (ISUPPORT/fold/CHANTYPES/
PREFIX/STATUSMSG/MODES, cap sets, batch types — pure, table-tested) + `internal/irc.Client`
(typed Events, labeled-response ledger, BATCH registry, SCRAM-before-join, backoff+jitter,
CertFP mechanism) + `internal/state.Store` (Apply → []Change; folded keys; stable session IDs;
per-network ignores; reducers ported from session.go handler bodies).
**Run the M5 suite against BOTH cores until they emit identical Changes** (R-3). The old
session still runs the app; the new core ships dark. New-string lookup table starts here (R-17).
Tests: proto table tests; reducer tests (modes_test.go pattern, extended); dual-core parity runs.

### M7 — Cutover and delete
Switch `internal/app`'s Session to delegate to irc+state through a thin adapter mapping Changes
onto the old 12-field UICallbacks; the v1 UI runs unmodified on the new engine. **Delete
session.go's event core, pendingEcho, liveML, isPlayback, whoisTarget in this same milestone**
— no dual-core limbo. Live-tagged tests re-pointed and green.
Tests: full M5 suite green on the new core only; nightly live suite green.

### M8 — Dispatcher
Replace the UICallbacks adapter with `ui.Dispatcher`: one goroutine draining Changes, 80ms
coalescing kept, ONE `fyne.Do` per batch, liveness by stable session ID. Delete the 12
stale/active guards. Carve the viewmodel (rows/drafts/pins/unread) out of the UI struct as a
testable type.
Tests: dispatcher unit tests (coalescing, stale-session drops); goleak on session teardown;
viewmodel headless tests (draft/rename migration — formerly untestable audit bugs).

### M9 — Archive + markers → **checkpoint C2: tag v2.0-alpha "v1 face, v2 memory"**
`internal/archive` (schema §5.4): Recorder write-behind; Pager; scroll-up pagination via
`CHATHISTORY BEFORE msgid`; reconnect gap-fill `AFTER` + `TARGETS`; MARKREAD send (blur/
focus-bottom) and consume; unread now derives from markers (max(server,local)); REDACT purges
row + FTS; LIST streams to `list_cache`; retention + per-buffer opt-out + DM prompt; the quiet
"reconnected — caught up N messages" line. The v1 UI shows the new unread numbers unchanged.
**Ship it: history and unread survive restart under the v1 face.**
Tests: archive unit tests (dedupe, redact-purge incl. FTS, retention/vacuum); marker-conflict
and multiclient transcript tests; first real profiling → CI perf budgets set (1M-row query p95,
reconnect-burst insert throughput, RSS ≤ v1's ~205MB, idle CPU ~1%).

### M10 — Command table
`internal/command`: parser + registered table over the narrow env; unknown-command intercept
with suggestion; `/raw` as the explicit passthrough; `/ignore` lands here; commands.go's switch
deleted. All wire writes now live only in `internal/irc`.
Tests: headless command tests (~90% of commands); intercept/second-Enter behavior.

### M11 — UI slice 1: theme tokens + Timeline (the risky bet, gated)
(a) Promote forcedTheme into `internal/ui/theme` semantic tokens; route every hardcoded NRGBA
through it; ThemeOverride darkens the Rail. **Goldens must not change.**
(b) Build `widgets.MessageList` behind `ECO_NEWCHAT`: per-row RichText/artLine via
`widget.List`+`SetItemHeight` (heterogeneous row types: message/art/divider/loading), hover
cluster, NEW + day dividers, author grouping, per-row Copy, jump pill, scroll-up paging, URL
linkification, Reader mode (F6). Validate against goldens and the **10k-line resize-relayout
budget**; designed fallback: fixed-height rows (accepting degraded hover/wrap) — decision
written down either way.
(c) **Mono gate (R-10):** run the CJK/emoji/RTL corpus; decide all-mono vs mono-gutter+
proportional-body. Then flip the default, delete the monolithic RichText, plaintext select
mode, and maxShownSegments.
Tests: goldens (unchanged in (a), extended rows in (b)); relayout perf budget in CI; corpus
gate result recorded in this file's changelog.

### M12 — UI slice 2: navigation, search, attention → **checkpoint C3: tag v2.0-beta**
Rail v2 (flat styled List, disclosure-by-invitation, pills, [Connect], draggable Splits);
People pane with hover kebabs; Ctrl+K Switcher (absorbing browser.go); Ctrl+F in-buffer search
+ Ctrl+Shift+F Finder (query grammar over FTS); smart join/part filter; per-buffer notification
levels UI + highlight-word lists wired to the marker-derived service; full keyboard model
(PageUp/PageDown delegation, Escape stack + dialog stack, focus-restore helper, Shift+Tab
popup, Alt+A hotlist, pinned Alt+1..9, capture widget).
Tests: viewmodel tests for hotlist ordering/pinning; switcher fuzzy-rank tests; notification
pipeline table tests (level × highlight × focus); 3k-nick People-pane budget in CI.

### M13 — Beginner surface
Welcome wizard (2 steps + Advanced link); tiered join (R-7) + land-in-channel; Network log
demotion + chatter suppression; humanized error banners with Retry/Edit/Fix-login; identity
chip; Claim-this-nickname (REGISTER/VERIFY); empty-state component everywhere; Settings shell
(Essentials + left-nav, pages carried over); teach-in-place passive labels; typing indicators
+ presence badges as SHOULD budget allows.
Tests: wizard flow tests on testsrv (happy path, dial fail, SASL fail, 433, +k refusal);
tiered-join boundary tests (10/11, 75/76); registration flow against a scripted REGISTER/VERIFY
transcript.

### M14 — Ship gate → tag v2.0
v1→v2 data migration (profiles, keyring entries, STS, prefs, keybinds) with tests; update
notifier; docs honesty pass (a11y ceiling stated, "Network log"/"Login"/"Encrypted connection"
renames, negotiated-vs-supported wording); README + download page (SmartScreen decision per
R-15); performance budgets all green in CI; delete `internal/app` remnants; prune the cap list
to negotiated-AND-used; final live-testnet soak.
Done when: the §9 definition-of-done checklist is fully green.

---

## 7. Test strategy (cross-cutting)

The pyramid (QA expert's plan, adopted):
1. **Pure table tests** — proto parsing (server-time as RFC3339Nano), fold()/casemapping,
   mode deltas (incl. list/arg modes), completion as a pure function (draft, cursor, members →
   replacement; the soft-wrap CursorRow trap), gridline column math, notification rule
   evaluation, tiered-join boundaries.
2. **Fake-server transcript tests** (`internal/testsrv`, net.Pipe, deterministic) — drive
   irc+state, assert on Changes/state, never display strings. Carry the regression list (M5),
   multiclient races, reconnect/rename/redact flows, the 10k-line synthetic buffer, and a
   synthetic huge-channel scenario.
3. **Golden-image render tests** — every PR, both palettes × two scales + CJK/emoji corpus;
   byte-compare; `-update` to regenerate deliberately.
4. **Live tests** — `//go:build live` against testnet.ergo.chat, nightly + manual dispatch,
   per-phase context timeouts, unique random channels, skip-only-on-dial-failure; **never
   merge-gating**.

Infrastructure: `-race` on everything; goleak around session teardown; CI perf budgets
(Timeline relayout on 10k lines, People pane at 3k nicks, archive p95 at 1M rows, reconnect
burst, LIST streaming, idle CPU ~1%, RSS ≤ ~205MB) added at M9/M11/M12 as each surface lands;
version stamp verified by test. Per-milestone additions are listed inline in §6.

---

## 8. Risk register

| # | Risk | Mitigation |
|---|------|------------|
| 1 | **Fyne variable-height List relayout cost** (every row's wrapped height recomputes on resize) — the one risky architectural bet. | M11 flag (`ECO_NEWCHAT`) + 10k-line CI budget before the flip; designed fallback: fixed-height rows; heterogeneous row types (message/art/divider) named as complexity and prototyped first. If the fallback triggers, hover actions degrade — accepted in writing. |
| 2 | **Core rewrite re-buys v1's paid-for protocol bugs.** | M5 suite lands BEFORE M6; dual-core parity until Changes match; old core deleted at M7 (no limbo); nightly live suite. Residual: real-Ergo edge cases no transcript captured. |
| 3 | **Selection expectations** — users expect drag-select; Fyne 2.8 cannot deliver it. | R-5 honesty: per-row Copy + Reader mode shipped and documented; cross-row selection designed as v2.x; no "native selection" claims anywhere. |
| 4 | **Fyne a11y ceiling** (no screen-reader tree — unfixable client-side). | Keyboard-complete + Reader mode + plain-text logs + honest docs (§4.8). Stated, not hidden — otherwise it becomes a trust problem. |
| 5 | **Archive privacy blowback** (IRC culture is log-sensitive). | Privacy contract ships in the SAME release as the archive (M9): per-buffer opt-out, DM prompt, retention+vacuum, REDACT purges row+FTS, DM-exclude toggle. |
| 6 | **Mono-everywhere breaks CJK/emoji channels.** | Corpus in goldens from M1; named decision gate at M11 before the default flips; designed fallback typography. |
| 7 | **Read-marker/chathistory absent on non-Ergo networks** → sync silently degrades. | Archive is authoritative there (ADR-3); max(server,local) merge; honest UI copy; multiclient transcripts in the suite. soju support explicitly LATER. |
| 8 | **Windows SmartScreen on unsigned exe** kills minute one on beginners' main platform. | R-15 named pre-GA decision gate (fund signing or ship documented walkthrough); betas unsigned. |
| 9 | **Solo-maintainer bandwidth vs 15 milestones** (M6–M9 and M11 are multi-week each). | Always-compiling ladder; three named ship checkpoints (C1/C2/C3); green golden+regression boards as visible progress; the SHOULD list is the designated scope valve — MUSTs are not. Skipping M1 or M5 under pressure is the named failure mode; the council holds that line. |
| 10 | **Perf budgets set without measurement** (v1's 205MB RSS unexplained). | First profiling at M9 sets numbers; budgets revisable only with a written note; huge-channel and reconnect-burst scenarios in the fake-server suite. |
| 11 | **Dual-persona scope creep** (progressive power invites building both roadmaps at once). | NEVER list with teeth; "no cap without behavior"; LATER discipline (R-9, R-12); two-clock gate on every surface addition. |
| 12 | **Dark-work morale/stakeholder patience during M5–M8** (little visible product change). | C1 checkpoint; beginner-visible wins ship early (keyring warning M3, caught-up line M9, intercept M10); alpha at M9 is a real release. |
| 13 | **Flatpak sandbox vs exec-based notify/zenity paths; macOS absent.** | Notifier/picker behind feature-detected interfaces from M3 so portal backends slot in without refactor; macOS is LATER, stated. |
| 14 | **No finding grounded in observed users.** | R-18 interviews during M0–M2; may re-rank SHOULDs, cannot unseat MUST architecture. |

---

## 9. Definition of done for v2.0

- Golden PNGs byte-identical to the M1 masters (modulo deliberate `-update` commits with
  written rationale); art corpus incl. CJK/emoji renders clean.
- Full offline suite (unit + fake-server + goldens) green under `-race` with goleak; nightly
  live suite green against testnet.ergo.chat.
- A fresh user reaches a first sent message on the seeded testnet in **under 2 minutes**
  without opening docs (clock 1), seeing zero protocol jargon or raw errors (clock 2).
- History, unread state, and read markers survive an app restart; unread agrees with a phone
  client against Ergo; non-supporting networks show the honest local-only copy.
- Ctrl+K, Ctrl+F, Ctrl+Shift+F, Alt+A, PageUp/PageDown, Escape stack, Shift+Tab all work;
  the shortcuts page is generated from the live binding table.
- No silent credential persistence anywhere; threat model committed; update notifier live.
- v1→v2 migration imports profiles/keyring/STS/prefs/keybinds on first launch (tested).
- No source file over ~600 lines; `internal/irc`+`internal/state` import zero Fyne; `fyne.Do`
  appears only in the Dispatcher; wire writes only in `internal/irc`.
- CI perf budgets green (Timeline 10k relayout, People 3k nicks, archive 1M rows p95,
  reconnect burst, LIST streamed); idle CPU ~1%; RSS ≤ v1's ~205MB.
- Docs honesty pass done (a11y ceiling, negotiated-vs-supported, jargon renames);
  R-15 SmartScreen decision recorded; Linux + Windows binaries reproduce from `make`.

---

## Progress log

Appended as milestones land, so the blueprint stays the single source of
truth about where v2 actually is.

- **M0 — done** (`bce989e`). CI (Linux + Windows cross, with the
  libXxf86vm gotcha documented), version stamping via `-X main.version`
  shown at startup and in About, live tests moved behind `//go:build live`
  so `make test` is a fast offline `-race` suite, `devdocs/v2/threat-model.md`
  ratified.
- **M1 — done** (`533f9e5`). Golden-image suite: 11 corpus cases under
  `testdata/art` (including the real Ergo MOTD art and CJK/emoji/RTL/
  double-width samples) × light/dark × 13pt/18pt = 44 checked-in PNGs,
  byte-compared, `-update` to regenerate. Plus numeric column-math tests so
  alignment failures name the broken quantity.
- **M2 — done** (`10467b1`). Mechanical split: `cmd/eco` + `internal/app`,
  one rename-only commit, single exported seam `app.Main(version)`.
- **M3 — done** (`e66ed2b`, `0414fae`, `445537f`). Leaves extracted:
  `internal/irc/transport` (wsconn), `internal/irc/sasl` (SCRAM, now
  receiver-free with a status sink), `internal/render` (colors, gridline,
  segments, and the IRC theme — goldens byte-identical after the move),
  `internal/model` (Line; app keeps a type alias so no call site churned),
  `internal/config` (profiles, STS, keyring) and `internal/notify`.
  Security fix shipped with the move: **no keyring → the password is
  dropped, never written to preferences JSON**, and the network manager
  says so. Two tests pin it.
- **M4 — done** (`93894de`). `config.Settings` immutable snapshot behind
  `atomic.Pointer`, published by `applySettings`; every display/behavior
  global deleted. Render already took explicit `Options` (pulled forward
  from M4 in M3.2 because the goldens needed it). Race test included.

**Deviation from the plan, recorded:** the render golden tests live in
`internal/render` and construct `render.Theme` directly. The IRC theme
(palettes, bold-mono faces, art line spacing) moved into `internal/render`
rather than `internal/ui/theme`, because the golden output depends on it —
keeping them together makes the frozen contract self-contained and avoids a
test-only import cycle. A UI-level semantic token layer can still wrap
`render.Theme` when the UI is rebuilt.

- **M5 — done** → **checkpoint C1 "armored"**. `internal/testsrv` (scripted
  `net.Pipe` fake server, transcript replay, and `Sync()` — a PING/PONG
  barrier — as the *only* synchronisation primitive) plus a `Session.Dial`
  seam, and 37 `TestRegression_*` cases (plus subtests) driving a real v1
  Session against it.
  Everything is offline, `-race`, and asserts on Session/Buffer state and
  callback invocations, never on a rendered string.

  **What the suite now covers.** Channel lifecycle: PART closed by its echo,
  PART refused by 442/403 (and *not* closing a channel nobody asked about),
  `/part` guarded in non-channel buffers, other users' PART/KICK, self-KICK
  auto-leave, `/cycle` keeping buffer and scrollback, join refusals
  471/473/474/475 surfaced once with the key prompt only on 475, autojoin
  refusals staying silent, and pendingPart/pendingJoin cleared on disconnect.
  Message pipeline: echo dedupe (immediate, delayed behind unrelated traffic,
  and per-part for multiline), per-buffer msgid dedupe across playback and
  refetch, whole-word highlighting with case folding, PMs highlighting and
  service NOTICEs not, no highlight from playback or status lines, multiline
  merge (incl. `draft/multiline-concat`) and mentions in continuations,
  REDACT routing, and `/ignore` suppressing lines and PM buffers with instant
  un-ignore. Buffers/state: case-insensitive buffer identity, the scrollback
  trim at caps 0/1/2/5, CloseBuffer removing the display-order entry (no nil
  ghosts), the server buffer surviving every teardown path, the full
  flag-tuple contract that unread and notifications derive from, whois
  routing, and a **10 000-line synthetic transcript** — the M11 perf corpus,
  which also pins the `seenIDs` 8192 bound. Connection/state tracking:
  reconnect rejoining every open channel exactly once (batched, no
  autojoin/rejoin double-join, one history refetch each), playback JOIN storms
  touching neither members nor history, `draft/channel-rename` migrating the
  buffer, channel-mode tracking from the wire (arg modes, member modes, `-k`,
  324 replacing rather than merging), the member list across
  NAMES/JOIN/PART/QUIT with multi-prefix and userhost-in-names, and the STS
  policy parsed from CAP LS with `duration=0` revoking it.

  **Hygiene.** Teardown is an assertion, not a hope: `Session.stopped` is a
  new test seam (closed when the connection goroutine returns) and the shared
  `stopSession` cleanup *waits* on it, so any test leaking a Session goroutine
  fails. Verified by temporarily removing the fake server's QUIT handling and
  watching it go red. No test touches the network — the four live tests keep
  their `//go:build live` tag and are absent from `make test`'s file list.
  Green across three uncached `make test` runs, ten `-shuffle=on` runs and
  `-count=2`/`-count=3`; gofmt and `go vet` clean.

  **Characterization tests — these ASSERT v1 BUGS and must flip at M6/M7.**
  Each is marked in place with the string `CHARACTERIZATION, expected to
  change at M6/M7`, so `grep -rn CHARACTERIZATION internal/app` is the
  cutover worklist. They are: (a) the three echo-dedupe tests, which pin the
  right outcome over `pendingEcho`'s *text* matching — MUST 3 replaces it with
  labeled-response, after which two identical messages sent in a row stop
  being indistinguishable; (b) own messages carrying **no** MsgID, the same
  bug from the buffer side, which is why they cannot be redacted or
  read-marked; (c) `TestRegression_ReopenedBufferInheritsDedupeState` — a
  reopened buffer inherits `seenIDs`/`historyRequested`, so it refetches
  nothing and silently swallows the replay (invert at M6, do not delete);
  (d) the `seenIDs` 8192 bound inside the 10k corpus, where v1 *empties* the
  map rather than evicting, so a busy buffer starts duplicating old history;
  (e) `TestRegression_WhoisRepliesRouteToTheAskingBuffer`, pinning both halves
  of the single `whoisTarget` slot (never cleared, last writer wins);
  (f) multiline's incremental `AmendBuffer` count and the `HighlightNote`
  callback, both artifacts of amending a line already handed to the UI —
  MUST 2 assembles at batch close and M8 deletes the callback surface;
  (g) `MembersChange`/`TopicChange` passing wire casing, asserted
  case-insensitively until the folded `BufferID` makes equality assertable;
  (h) `msgRedactUI`, a fixture standing in for redaction logic that lives in
  ui.go and is unreachable headlessly until M7.

  **Gaps, recorded rather than papered over.** Three items from the M5 list
  are *not* covered, all for the same reason — they live in ui.go, which has
  no headless seam in v1: draft ownership by session+buffer across networks
  and the soft-wrap Tab-completion hazard (QA #7); rename migrating draft,
  unread and the viewer, and Create joining the literal typed name (QA #8,
  the buffer half *is* covered); and "STS persists only from TLS connections",
  where the CAP LS parse is pinned but the TLS gate is not. The blueprint
  already routes these to M8's viewmodel carve-out, which is where they become
  testable — M8's test list must not be trimmed. Also absent: the version-stamp
  test (QA #10) and a synthetic huge-channel (3k-nick) scenario, both cheap to
  add once there is something to assert against.

- **M6 (wire half) — done.** `internal/irc.Client`: config in, typed
  `Events() <-chan model.Event` out, one supervisor goroutine, zero fyne and
  zero display strings. It wraps `ircevent` for the connection, registration,
  PLAIN/EXTERNAL SASL and the write path, but **decodes every inbound line
  itself from one raw hook** — because the library buffers whole batches and
  discards labelled replies it did not label, so a client that wants to see
  BATCH framing and its own labels cannot use its typed callbacks. One hook is
  also what lets the event order be promised to be the wire order.

  **The four v1 heuristics this deletes, with the tests that pin them.**
  (a) *Labelled-response ledger*: every outgoing message carries a label, and
  `Send*` returns a `Handle` holding it, so `state.Echo` can create the pending
  message and the echo resolves it. Two identical messages sent in a row are
  now distinguishable, and our own messages arrive with their real msgid —
  which is what makes them redactable and read-markable at all. A reply grouped
  into a `labeled-response` BATCH inherits the batch's label; a bare `ACK`
  arrives as `Numeric{Code:"ACK"}` carrying it. (b) *Real BATCH registry*
  (`proto.BatchTracker`): playback is "something in my enclosing chain is a
  chathistory batch", never "this line has a batch tag"; multiline assembles at
  BATCH close, so the two-minute wall-clock recency window is gone. Nested
  multiline-inside-chathistory is a test. (c) *ISUPPORT feeds
  `proto.ServerProps`*, published as an immutable snapshot and reset on every
  reconnect. (d) *Exponential backoff with jitter* replacing the flat 30s,
  driven by injectable `Timer` and `Rand` seams so the ladder is asserted
  rather than endured.

  Also: SCRAM is sequenced **before** `ConnReady` (and a server that refuses
  the `sasl` cap aborts rather than hanging); a SASL failure is terminal, not a
  reconnect storm; CAP NEW is requested and CAP DEL stood down; `sts` is read
  from the *advertised* set; CTCP is answered by eco rather than by the library
  (whose CTCP rewrite mutates the shared parameter slice out from under the raw
  hook), and never for replayed history. Both server-controlled maps are
  bounded — a batch stops being held past `maxBatchEvents` and the chunk says
  it is incomplete; the ledger evicts its oldest half rather than clearing
  itself, which is the mistake v1's 8192-entry msgid map made.

  48 tests against `internal/testsrv`, `-race` clean across `-count=3` and five
  `-shuffle=on` runs: registration and server properties, message decode,
  labelled echo resolution (including two identical texts the server rewrites),
  multiline send/echo/truncation, chathistory chunking and truncation
  reporting, nested batches, netjoin and unknown batches staying live, backoff
  ladder and reset, cap ACK/NAK/NEW/DEL/LS-continuation, STS, SASL
  PLAIN/optional/terminal-failure and SCRAM sequencing, numerics, and teardown.
  Nothing imports the package: it ships dark, exactly as M6 requires.

- **M6 — done** (parity gate). `internal/parity` is the dual-core gate R-3
  asked for: one scripted `internal/testsrv` transcript, replayed against the
  v1 Session and against `irc.Client` + `state.Store`, with both sides
  normalised onto one canonical `Fact` (folded buffer, kind, nick, text, flags)
  and diffed. Everything is in `_test.go` files behind a doc-only `doc.go`, so
  the production build graph never contains the harness's import of
  `internal/app` and retiring the gate at M7 is one `rm -r`.

  **What it compares.** Two things, not one. (a) The *stream* — what each core
  told the UI: v1's twelve `UICallbacks`, v2's `[]model.Change` — grouped by
  transcript step and sorted inside each group, because both cores see the same
  line at the same point and the order two facts about one line come out in is
  not behaviour. (b) The *final state* of every buffer: topic, modes, key,
  members, and the whole line log in order. The state half is what catches
  multiline: v1's callback stream only ever carries the first part plus
  `AmendBuffer` pings, so the assembled text exists nowhere but the buffer.

  **The three normalisations**, each of which had to earn its place: v1's
  amendments are folded back into the message they amend (the recorder reads
  the amended line back through `Session.Buffer`, which is the only way the
  merged text is observable at all); roster traffic is reduced to the question
  a user answers by looking at the screen — *after this step, who is in the
  room?* — by keeping only the last roster per buffer per step and dropping one
  identical to what is already on screen, which is what the dispatcher does for
  real on its 80ms tick (§5.3); and a redacted line is dropped from v1's log
  snapshot, since v1 keeps the text and asks ui.go to drop the widget — the
  same courtesy the v2 side gets from the harness's conductor, which supplies
  the join→MODE/CHATHISTORY and reconnect→rejoin policy that lives in the
  Session today and will live in M7's adapter tomorrow.

  **A members fact carries the roster, not a ping** — and the difference is the
  whole gate. The first version of this harness compared "this buffer's roster
  changed" as a contentless fact. Contentless facts are fungible: v1's habit of
  answering every MODE with a full NAMES rebuild needed a divergence entry to
  forgive it, and that entry then forgave *any* unmatched roster fact in a MODE
  step — so a v2 core that silently stopped reporting `+o` passed. The same
  fungibility hid a lost JOIN delta behind the NAMES reset that follows it in
  the same step. Both were found by injecting the bugs and watching the gate
  stay green, both are fixed by the fact carrying the resulting member list,
  and the `mode-delta-not-names-refresh` entry was **deleted** rather than
  narrowed: with the roster in the fact, v1's redundant rebuild normalises away
  as the no-op it is, and needs no forgiveness at all.

  **21 scenarios**, the M5 suite's cases as wire traffic: registration + join +
  message (with msgid dedupe, notice, action, query, folded targets),
  labelled echo of two identical messages (plus an unlabelled echo-message for
  something the user sent from another client, which must arrive wearing the
  self flag), PART echo and PART refusal, other users joining, parting and
  quitting a live channel one line at a time, self-kick, kick of another,
  /cycle, reconnect + rejoin, playback batch (incl. a replayed self-JOIN and a
  repeated batch),
  multiline merge, multiline nested inside chathistory, reopened buffer,
  redact, mode deltas, rename, join refusals (asked and unasked), ignore, nick
  change (theirs and ours), closing a query, WHOIS replies and STS. The gate
  itself is tested: `TestParity_ReportsADisagreement` proves it fails readably,
  `TestParity_EqualRunsPass` proves equal runs are silent, and every scenario
  must produce facts outside registration.

  **The evidence the comparison is not weakened is a mutation sweep**, not an
  assertion. Eighteen bugs were injected one at a time into `internal/state`,
  `internal/proto` and `internal/irc` and the gate re-run on each: highlight
  matching disabled, msgid dedupe disabled, the ignore filter disabled, the
  NAMES reset silenced, an other-user JOIN/PART/QUIT silenced, a member-mode
  change silenced, `TopicChanged` silenced, the message log reversed, the self
  flag cleared, the playback flag cleared, `Fold` made the identity function,
  REDACT made a no-op, RENAME made a no-op, a join refusal not reported, the
  multiline separator changed from `\n` to a space, and the user's own /me
  dropped. **All eighteen go red against the gate as it now stands.** Thirteen
  were run against the gate as first written and went red there too; **two went
  green and were real holes** — the roster fungibility above, which is why a
  members fact now carries the roster and why a scenario exists in which
  another user joins a channel that is already open. The remaining three (the
  self flag, and an other-user PART and QUIT silenced) were only run after
  those fixes, because before them the transcripts contained nothing to catch:
  no inbound message was ever our own, and no roster moved outside a NAMES
  burst. Two further attempts were discarded as no-ops that patched dead
  anchors rather than live behaviour — a mutation that does not change the
  cores proves nothing about the gate, and counting one would have been the
  same self-deception the sweep exists to prevent. `mutate.sh` is not checked
  in; the sweep is cheap to reproduce and M7 should repeat it against the
  cut-over core.

  **What the new core does differently by design** — the divergence list, each
  entry with its reason in `divergence_test.go`, each one required to fire (a
  dead entry fails the suite, so the list cannot outlive its differences):

  | ID | The difference |
  |---|---|
  | `labeled-echo-resolution` | Own messages are sent with a label and resolved in place by the echo, gaining the server's msgid and server-time. v1 matched the echo by buffer name plus text and dropped it. |
  | `reopened-buffer-refetches-history` | "Already fetched" is keyed on the buffer, which the close destroyed, so a reopened buffer refetches. v1's session-wide map made it come back empty. |
  | `history-chunk` | A chathistory batch arrives as one `HistoryLoaded` at BATCH close. |
  | `derived-unread` | Unread/highlight counts are derived in the store from the read marker; in v1 they live in ui.go. |
  | `typed-connection-states` | Connecting/registering/authenticating/ready/failed, against v1's single bool. |
  | `self-identity` | `SelfChanged` for our own nick/account/away. |
  | `typed-channel-modes` | `ModeChanged` (flags, key, limit); v1 told nobody. |
  | `multiline-assembled-once` | Assembled at BATCH close, so v1's `HighlightNote` second-chance callback has no counterpart. |
  | `redaction-tombstone` | A redacted message becomes a tombstone in the store. |
  | `cycle-clears-the-roster-visibly` | /cycle's PART reports the emptied roster and the rejoin reports the refill; v1 emptied it silently, so for v1 the roster never appeared to change at all. Scoped to the cycle scenario. |
  | `v1-connection-chrome` | "Connecting to …" and the negotiated cap list are not chat lines in v2. |
  | `ready-banner-position` | Readiness is announced after the last MOTD line, not before it (which is where SASL sequencing hangs). |
  | `inbound-action-delivered` | **v1 never showed an incoming /me**: it left the library's CTCP rewrite on and registered no `CTCP_ACTION` callback, so its own ACTION branch is dead code inbound. Found by this harness. Covers an action of ours arriving over the wire too (a /me typed on another client). It forgives one side only, so it cannot hide v2 mishandling the /me typed *here* — v1's copy would go unmatched; `TestParity_LocalActionIsNotForgiven` pins that, and the mutation sweep confirms it. |
  | `sts-is-connection-config` | An STS policy is published on the CapSet for the next dial (`Client.STS()`), not pushed at the UI. |
  | `rail-repaint-on-rename` | A rename is `BufferRenamed` alone, not a whole-rail repaint. |

  **Fixed in the new core because the gate said so** (v1 is the reference where
  the blueprint does not deliberately differ): `state.applyNumeric` now has a
  `quietNumerics` set — 004, 250, 252–254, 329, 333 — which v1 suppressed and
  v2 was logging as banner noise. It is deliberately not v1's whole
  `handledNumerics` table: everything else on that table has a typed event in
  v2 and never reaches the log path.

  **What M7 must re-verify at cutover**, because the harness cannot:
  1. **WHOIS routing.** The formatting of all seven numerics is compared and
     matches, but *which buffer asked* is not: v1's `whoisTarget` is set from
     commands.go with no headless seam. The adapter must call
     `store.TrackWhois` where v1 set the slot.
  2. **STS persistence.** Nothing calls `client.STS()` yet. The connect path
     must read it and feed `config.STSRecord`, still only from TLS connections.
  3. **Redaction in the view.** The store leaves a tombstone; ui.go currently
     removes a row. One of the two has to change, deliberately.
  4. **Formatting codes.** v2 stores topics and message text with formatting
     intact (stripping is the render layer's job). The UI must not double-strip
     or leak raw codes. Not expressible as a per-fact divergence, so it is a
     cutover check.
  5. **STATUSMSG targets.** v2 routes `@#chan` to the channel; v1 opened a
     query. Same shape of difference as (4) — a buffer identity swap the
     per-fact differ cannot express — so it is verified by hand at cutover.
  6. **Everything the conductor stands in for.** The join→MODE/CHATHISTORY
     policy, the reconnect rejoin plan (now with the +k key from
     `RejoinTargets`), and `Echo`/`RequestPart`/`RequestCycle`/`RequestJoin`
     being called where v1 set its pending maps. `core_v2_test.go` is the
     smallest working example of that adapter; M7 should read it first.
  7. The M5 characterization list (`grep -rn CHARACTERIZATION internal/app`)
     still stands as the set of expectations to invert. Two of them
     — own-message msgid and the reopened buffer — already have their v2
     behaviour pinned here, by `TestParity_LabeledEchoCarriesTheServerMsgID`
     and the `reopened-buffer-refetches-history` scenario.

  One seam was added to v1 for this: `app.SetIgnored`, in the same family as
  `Session.Dial`, because the production toggle needs `fyne.Preferences` and a
  headless harness has no business creating an app. Nothing in the app calls
  it.

- **M7 (cutover half) — done.** `internal/app`'s `Session` is now a thin adapter
  over `internal/irc.Client` + `internal/state.Store`: one goroutine drains
  `Events()`, applies each event to the store, and folds the resulting
  `[]model.Change` into the twelve `UICallbacks` and into a *projection* of the
  store in the v1 `Buffer` shape (Lines/Members/Topic/Modes/Key), because ui.go
  reads those fields directly. The v1 UI runs unmodified on the new engine: the
  only call sites that changed are the two `/whois` ones (they now call
  `Session.Whois`, which records who asked — M7 re-verify item 1) and ui.go's
  `RedactMsg`, which stopped overwriting the line the store already tombstoned
  (item 3). `Session.irc` survives as a five-verb façade over `irc.Client`, so
  commands.go, browser.go and rowmenus.go did not have to be rewritten twice —
  M10's command table is where they go.

  **The conductor** — the policy that is neither wire nor state — is
  `reactLocked`: join → MODE + CHATHISTORY (once per open buffer), ready →
  autojoin then rejoin `RejoinTargets` batched ten to a line (with the +k key v1
  forgot), CAP LS/NEW → the sts advertisement, which still reaches ui.go and
  still persists only from a TLS connection (item 2). `core_v2_test.go` was the
  working sketch; this is the same shape with the app's own policies.

  **Deliberate divergences that landed with it**, beyond the M6 list: our own
  messages are *pending rows* resolved in place by the labelled echo, so they
  carry the server's msgid (redactable at last) and two identical messages are
  distinguishable; a labelled 442/403 is attributed to the PART that caused it;
  a reopened buffer refetches its history; dedupe has no 8192 ceiling; whois
  replies follow their own request; multiline is assembled before the UI sees
  it, so `AmendBuffer`/`HighlightNote` no longer fire for it; `@#chan` routes to
  the channel (item 5); topics are stripped once, in the adapter, and message
  text keeps its formatting codes for the render layer (item 4). One new hazard
  the label-only design creates is pinned rather than argued about: on a server
  with `echo-message` but no `labeled-response` there is nothing to correlate,
  so eco does not render a local copy at all and lets the echo be the message
  (`TestRegression_EchoWithoutLabeledResponse`); with neither cap the local copy
  is the message (`…WithoutEchoMessage`).

  **The M5 suite is green on the new core with no test deleted.** Nine
  characterization expectations were inverted in place, each keeping its
  history in the comment (`grep -rn "CHANGED AT M7" internal/app`): labelled
  echo resolution ×3, own-message msgid, reopened-buffer refetch, the seenIDs
  ceiling, per-request whois, multiline's amend count and `HighlightNote`, and
  the canonical-casing of `MembersChange`/`TopicChange`. One more moved for the
  same reason and is called out because it was *not* on the M5 list: a rejoin no
  longer fires a contentless `MembersChange` ping, because the store reports
  member deltas and rejoining a channel you were already in is not one — the
  same normalisation the parity harness applies to v1's redundant NAMES
  rebuilds. The test now asserts the roster survives the reconnect intact.

  **One seam was needed to keep 37 fake-server tests unedited**:
  `testsrv.Server.Barrier`. `Sync()`'s promise ("everything sent before this
  call has been processed") was self-evident only for a client that folds events
  in on the goroutine that answers the PING; the adapter does not, so it
  supplies the second half of the barrier (`Session.settle`) and `Sync` repeats
  the round trip afterwards, so anything the adapter wrote while the barrier ran
  is queued before `Pending()` is read. `testsrv.Match` also grew one rule — a
  pattern that does not start with `@` matches the line without its tags — since
  every correlatable command now goes out labelled.

  **The mutation sweep was repeated against the cut-over core**, this time
  against the adapter itself, which is the part the M6 sweep could not reach:
  resolve not updating its row, the roster never republished, the topic not
  stripped, no CHATHISTORY on self-join, the ignore filter off, the scrollback
  trim off, the rejoin skipped, and a redaction not projected. All eight go red.

  **`internal/parity` still gates both cores.** The v1 side is pinned rather
  than followed: `session.go`'s core was renamed `app.LegacySession` and left in
  place, unreachable from the app, so the reference is pre-cutover behaviour
  frozen — a gate whose two sides are the same code proves nothing. Nothing else
  changed in the harness.

- **M7 — done** (deletion). The v1 event core is gone: `legacy_session.go`,
  1366 lines, deleted whole. With it went `pendingEcho` (echo matched by
  buffer|text), `liveML` and its two-minute wall-clock recency window,
  `isPlayback` (the "does this line carry a batch tag" guess), the never-cleared
  `whoisTarget` slot, the session-wide `seenIDs`/`historyRequested` maps and the
  8192-entry reset, `strings.ToLower` buffer keying, `Buffer.applyModeDelta`/
  `setMode` (superseded by `proto.ParseModeDelta`, whose own tests are far more
  thorough), the duplicate `containsWord` (`state`'s copy is the one that ships),
  `requestedCaps`, `msgTime`, `leadingInt` and the twelve `ircevent` callback
  registrations. Two things fell out with it: `modes_test.go`, whose subject no
  longer exists, and `app.SetIgnored`, an exported test seam that existed only
  for the dual-core harness. `internal/app` no longer imports `ircevent`,
  `ircmsg` or `internal/irc/sasl` — every wire decision in the binary now lives
  behind `internal/irc`. Nothing was deleted on a hunch: each symbol was grepped
  to zero across the package (methods included), and `go build`, `go vet`,
  `go vet -tags live` and the full suite were the arbiters.

  **`go list -deps ./cmd/eco` contains `internal/irc` and `internal/state`**, and
  no longer reaches `ircevent` except through `internal/irc`. There is one core.

  **Behaviour the user can see did not change in this stage** — the cutover
  itself was the previous commit. What changed is what a wrong turn now costs:
  there is no second implementation to fall back to, and no code path that can
  quietly resurrect the old heuristics.

  **`internal/parity` became `internal/transcript`** — a characterization suite,
  not a retired one. Its 21 scenarios, its canonical `Fact`, its two-sided
  comparison (the Change stream grouped per step, and the final state of every
  buffer) and its normalisations survive unchanged; what changed is the reference
  side. Where the harness used to compute v1's answer live, it now reads a
  recorded one: `testdata/<scenario>.txt`, one fact per line, produced by
  `-update`. **The records were taken while the dual-core gate was still green
  and were not touched again**, so they are not "whatever the code did when
  somebody pressed -update" — they are v1's behaviour plus the fifteen
  divergences this log already lists, frozen at the last commit where both cores
  could still be checked against each other. Deleted with the v1 side:
  `core_v1_test.go`, and `divergence_test.go` — a list of *forgivenesses*, which
  a single-core suite does not need, and whose content is the table above.

  **The strictness is anchored twice.** Four tests assert behaviour directly
  rather than through a record, so `-update` cannot bless a regression away: the
  labelled echo carrying the server's msgid, the STS policy reaching the client,
  the user's own `/me` being delivered, and a members fact naming the room (the
  fungibility hole the M6 sweep found). `TestEveryRecordHasAScenario` fails on a
  record whose scenario has gone missing, so the scenarios cannot rot either.
  And **the mutation sweep was repeated against the new suite: 17 bugs injected
  one at a time into `internal/state`, `internal/proto` and `internal/irc`, all
  17 caught** — highlight matching, msgid dedupe, the ignore filter, `Fold` made
  the identity, the self flag, the playback flag, the multiline separator, REDACT,
  RENAME, another user's PART and QUIT, the NAMES reset, a nick change's roster
  update, a member `+o`, `TopicChanged`, a join refusal, and the message log
  reversed. An eighteenth was discarded rather than counted: clearing
  `Sender.Self` in the *decoder* is invisible downstream because `state.isSelf`
  recomputes identity from the folded nick, so it proves nothing about the suite
  (it does go red in `internal/irc`'s own tests, which is where it belongs). The
  failure report was rewritten to align on the longest common subsequence, so a
  dropped fact reads as one `-` line rather than "everything after here changed".

  **The live suite is re-pointed and builds** (`go vet -tags live ./...` clean;
  the tests are never run here — no network). Three of the four needed nothing:
  they were written against `Session`'s surface, which the adapter kept.
  `TestSendMessageEcho` was rewritten, because what it was watching changed
  meaning — it now waits for the local row to gain the *server's* msgid, which is
  the labelled-response path end to end against a real Ergo, and skips that half
  if the server negotiated neither `labeled-response` nor `echo-message`. It
  reads the projection under the session lock rather than through
  `Session.Buffer`, since the nightly runs with `-race`.

  **M8 inherits** a single core, a UI still talking to it through the twelve
  `UICallbacks` and the `Buffer` projection in session.go (the adapter, ~400
  lines of it, is what the Dispatcher replaces), the conductor `reactLocked` as
  the one place application policy lives, and `internal/transcript` as the
  behaviour it must not change while it rewrites the layer above — the records
  are keyed on the Change stream, which is exactly the seam M8 moves the UI onto,
  so they should stay byte-identical through it. The M5 gaps M8 was already
  routed (draft ownership across networks, rename migrating draft/unread/viewer,
  the soft-wrap Tab hazard, "STS persists only from TLS") are still open and
  still untestable until the viewmodel is carved out.

- **M7 verification — done** (independent adversarial pass; six holes found and
  closed, six tests added, no production code changed).

  **The cutover is structurally real.** `go list -deps ./cmd/eco` contains
  `internal/irc` and `internal/state`; `ircevent`/`ircmsg` are reachable only
  through `internal/irc`, and `internal/app` imports neither. Every deleted
  heuristic greps to zero as *code*: the surviving hits for `pendingEcho`,
  `liveML`, `isPlayback`, `whoisTarget`, `seenIDs` and `historyRequested` are
  all comments naming the bug a replacement exists to kill. `strings.ToLower`
  survives in `internal/app` only above the core — the ignore list and its
  join/part key (`ignore.go`), the completion prefix match (`entry.go`), the
  channel-list search (`browser.go`), the command verb (`commands.go`) and two
  ui.go maps (`skey`, the mention resolver). None is a *buffer* key into the
  store, which is `Fold`ed throughout; they are ASCII folds of display strings,
  and they are M8/M10's to route through `proto.Fold`, noted here so the next
  milestone does not have to rediscover them.

  **The records' provenance was verified rather than believed**, because "these
  were taken while the dual-core gate was green" is exactly the kind of claim a
  single-core suite can no longer check for itself. Four links, each checked:
  `internal/parity` at d86fc72 was re-run from a clean worktree and is green (6
  tests, both cores); `internal/state`, `internal/proto` and `internal/irc`
  contain **no production-code change** between d86fc72 and HEAD (the only diffs
  are comments, `testsrv.TryNext` and an `auth_test.go` de-flake); the v2-side
  normalisation — `factsFromChange`, `factFromMessage`, `sortFacts`,
  `collapseRosters` — is byte-identical, only the v1-side `factFromLine`/
  `collapseAmends`/`kindAmend`/`kindHighlight` machinery was removed; and the
  scenario set is identical name for name. The records therefore are what the
  dual-core-certified core produced. **No test was deleted at the cutover**: the
  M5 roster of 37 `TestRegression_*` is intact, the one apparent removal being
  `ReopenedBufferInheritsDedupeState` renamed to `…RefetchesHistory` — the same
  test, inverted.

  **The nine inverted characterization expectations were read as a diff against
  M5 and are strengthened, not weakened.** Every one either tightened an
  assertion or replaced a mechanism assertion with a content assertion:
  case-insensitive "the UI heard about this channel somehow" became equality
  *plus* a negative check that the wire casing fires nothing; "no CHATHISTORY,
  zero messages" became "exactly one, exactly one"; "own lines never carry a
  msgid" kept the pending half and added the resolved msgid, the preserved Self
  flag and the no-badge check; the whois quirks became per-request routing with
  a two-in-flight scenario; the seenIDs ceiling flipped from "+1 duplicate" to
  "0". Two swapped a count for content and are the only ones worth arguing
  about — the rejoin's `MembersChange` ping (1 → 0, compensated by asserting the
  roster survives the reconnect intact) and multiline's `HighlightNote` (1 → 0,
  compensated by asserting the delivered line is already flagged). Both are the
  v1 mechanism being replaced by the requirement it served, and the sweep below
  confirms both tests still bite. Two test-only stand-ins for v1 behaviour were
  deleted with the behaviour: `msgRedactUI` (the UI-layer tombstone) and
  `bufWhoisFrom` (which wrote `s.whoisTarget` directly); the tests now drive
  `Session.Whois` and assert on the session's own state.

  **The armor was measured by mutation, in two rounds of one-bug-at-a-time
  injection with the internal/app suite as the gate — 36 bugs, 34 valid.**
  Round 1 (20 bugs: the JOIN delta, member modes, `Fold`, msgid dedupe,
  highlights, label echo resolution, `IsPlayback`, REDACT, the part refusal,
  join-refusal gating, RENAME, history order, and eight in the adapter) went red
  28 times of 34 first try. **Six went green, and every one was a real hole:**

  | Mutation | Missed by | Closed by |
  |---|---|---|
  | member mode `+o` never applied | app | positive roster assertions in `…ChannelModeTrackingFromTheWire`, which asserted only that member modes do *not* leak into `b.Modes` |
  | a nick change leaves a roster ghost | app **and state** | a NICK step in `…MemberListTracksNamesJoinPartQuit`, and a second nick in `TestNickMovesTheMemberKey` |
  | a history chunk is reversed | **everything** | `TestRegression_PlaybackPreservesTranscriptOrder`, an order assertion in `TestHistoryChunkArrivesAsOneBlock`, and `TestRecordedBehaviour_AHistoryChunkKeepsItsOrder` |
  | a /cycle's PART refusal closes the buffer | **everything** | `TestRegression_CyclePartRefusalKeepsBuffer` |
  | the rejoin forgets the +k key | app and transcript | `TestRegression_ReconnectRejoinsKeyedChannelWithItsKey` |
  | our own messages count toward unread | app | *nothing* — see below |

  Three of those deserve naming, because each was invisible for a *reason* the
  next milestone can walk into again:

  1. **`sortFacts` makes the transcript records blind to intra-step order.** It
     sorts each step's facts canonically, on the stated reasoning that the order
     two facts about one step come out in is not behaviour. True for a roster
     and a status line; false for the messages inside one `HistoryLoaded`. The
     buffer snapshot does not cover it either — that is the store's own log,
     which the ordinary reducers append in wire order, so a chunk handed to the
     UI backwards leaves it untouched. Fixed without regenerating a single
     record, by the anchor above: `core` now keeps the chunks and the test reads
     their slice order directly. **M8 must know this**: the records are its
     contract, and they do not pin the order of anything within one step.
  2. **A re-casing cannot see a missing delete.** `TestNickMovesTheMemberKey`
     tested `alice → ALICE`, which folds to the same key, so the entry is
     overwritten whether or not the old one is removed. The ghost only appears
     on a change to a genuinely different nick.
  3. **`countsUnread` is not consumed by internal/app at all**, which is why
     mutating it leaves that suite green. That is the `derived-unread`
     divergence working as designed — v1's ui.go counts from `Line` flags and
     still does, and `internal/state` and `internal/transcript` both catch it.
     It is left as it is deliberately, and it stops being acceptable the moment
     M9 makes the UI read the store's counters.

  Round 2 (16 bugs in territory round 1 never touched: whois routing, the STS
  advertisement, the multiline separator, self-KICK, /cycle, STATUSMSG routing,
  formatting codes, the disconnect cleanup, the +k key, unread derivation,
  playback JOINs, join batching, the closed-buffer order entry, rename
  migration, the wire label, query highlights) went red 11 of 14 valid first
  try; two mutations did not compile and were repaired rather than counted, on
  the M6 rule that a mutation which does not change the cores proves nothing.
  **After the fixes: 35 of 36 red, and the one exception is item 3 above.**

  Everything else in the M7 claim checks out as stated: `gofmt` clean, `go vet`
  clean, `go vet -tags live ./...` clean (the live tests build; they are never
  run here), `make build` green, and the full suite green under `-race` with
  `-count=1`, again under `-shuffle=on`, and again at `-count=2` on the five
  changed packages. **What could not be verified here**, and is named rather
  than glossed: the live suite has never been executed against a real Ergo from
  this environment, so `TestSendMessageEcho`'s rewritten half — the labelled
  echo end to end — is proven against `internal/testsrv` and compiled against
  Ergo, not run against it.

- **M8 (dispatcher half) — done.** `internal/ui/dispatch` is the funnel §5.3
  promised: one goroutine draining Changes, the proven 80ms window kept, and
  **exactly one `fyne.Do` per batch** — the whole batch, not the whole callback.
  The twelve `UICallbacks` and the twelve hand-rolled `stale()`/`active()`
  guards are gone. `internal/app`'s Session emits `dispatch.Item`s and the UI
  implements `dispatch.Sink`; `fyne.Do` now appears in this package and nowhere
  in the delivery path.

  **Liveness is one rule in one place.** A session is a `dispatch.SessionID`
  fixed at `NewSession` (the network id, monotonic, never `Name`), registered by
  `Open` and retired by `Close` — called from `UI.closeSession`, the one place
  the app removes a session. The check happens once, at delivery, so a session
  torn down *by the sink*, mid-batch (the STS plaintext upgrade does exactly
  that) stops being delivered to from the very next item. What survives in the
  UI is `showing()`, which is a rendering question and not a liveness one.

  **The Sink is named from the Change union, not from the callbacks**:
  `LinesAdded`, `LineAmended`, `LineRedacted`, `BufferListChanged`,
  `BufferRenamed`, `MembersChanged`, `TopicChanged`, `ConnStateChanged`,
  `JoinRefused`, `STSOffered`, `ChannelListDone`, `BatchDone`. Three facts the
  store cannot own — is this disconnect one the user asked for, what did CAP LS
  advertise, did somebody *ask* for this scan — ride the same queue as
  dispatch-local Changes rather than through a side channel, so there is one
  stream and one order. `HighlightNote` is deleted: M7's batch assembly already
  made the second-chance mention callback unreachable, and the four tests that
  asserted it never fired now assert the requirement it served (one delivery per
  message, flag included), which is strictly stronger.

  **Order is pinned by test, at last.** The package doc states the guarantee
  (arrival order, an Emit never split, per-session order preserved across
  interleaving) and `TestOrderingWithinBatch` / `…AcrossSessions` /
  `…WithinAHistoryChunk` hold it. M7's warning that the transcript records
  cannot see intra-step order was taken literally: a mutation reversing a
  history chunk *on the way to the screen* (buffer order intact) was green
  against the whole suite until `TestRegression_PlaybackPreservesTranscriptOrder`
  grew a screen-side assertion. Likewise a status line that skipped the funnel
  was invisible until `TestFunnel_TheScreenSeesTheBufferInOrder` compared the two
  streams. Both holes are closed; both mutations now go red.

  **Backpressure is defined and shutdown is deterministic.** The queue is
  bounded (256 Emits); a full queue blocks the emitter and so the wire, because
  a dropped `BufferCreated` is a buffer that never appears. Nothing is ever
  dropped for being late — the only drop is the liveness rule. `Stop` drains,
  delivers a final batch and returns once the goroutine is gone
  (`TestStopLeavesNoGoroutine`, `TestStopReleasesABlockedEmitter`); it must not
  be called from the sink's thread, so the running app does not call it at all
  and the per-session teardown that matters at runtime is `Close`.
  `Flush` is the same barrier without the stop, and it is what `Session.settle`
  (and therefore `srv.Sync`) now runs through, so the offline suite stayed
  sleep-free.

  **Behaviour is unchanged with two deliberate exceptions**, both noted here
  rather than glossed: a torn-down session no longer raises desktop
  notifications (the old `AddLine` notified *before* its stale check), and a
  local verb's repaint (`CloseBuffer`, `SendMessage`) is now coalesced with
  everything else instead of firing a synchronous callback — the repaint it
  triggered was already 80ms deferred, so what changed is the callback, not what
  anyone sees. One test gained an explicit `settleUI` barrier for it.

  **M8's viewmodel half inherits** the Sink as the permanent seam and the
  `Buffer{ID, Name}` pair as the shape names arrive in. What is still in the UI
  struct and belongs in the viewmodel: `unread`/`hlBuffers`/`drafts` keyed by
  `skey` (profile name + `strings.ToLower`, NOT `proto.Fold` — and it must stay
  profile-scoped, because drafts deliberately survive a reconnect), `current` as
  a display name compared with `EqualFold`, and `rows`. The M5 gaps are still
  open and still where M7 left them (draft ownership across networks, rename
  migrating draft/unread/viewer, the soft-wrap Tab hazard); `Session.Buffer`'s
  projection is still what `showBuffer` reads, and it is the next thing to go.
  The `derived-unread` divergence is untouched: the app still counts from
  `Line` flags, and `MessageAdded.Notify`/`UnreadChanged` still reach the funnel
  and stop, unhandled, in `deliver`'s default — which is M9's to pick up.

- **M8 (viewmodel half) — done.** `internal/ui/viewmodel` is the state the window
  renders, carved out of the UI struct: the sidebar's rows, the buffer on
  screen, the draft slot and who owns it, the attention counts, the roster, and
  whether the message pane is following the tail. It holds **no widgets** (a
  test reads the package's own import list and fails on `fyne`), it is confined
  to the Fyne thread like the UI it serves, and it reaches out through exactly
  one hook — `Hooks.MarkRead`. `internal/ui/viewmodel` rather than
  `internal/app/viewmodel` because it needs no unexported app type: sessions are
  `dispatch.SessionID` + profile name, buffers are display names, and the
  layering reads `app → ui/viewmodel → ui/dispatch → model`.

  **The UI is a renderer now.** `UI.sess`, `current`, `currentKey`, `rows`,
  `unread`, `hlBuffers`, `drafts`, `members`, `networkName` and `stickBottom` are
  deleted fields; `u.session()` and `u.current()` resolve through the viewmodel
  every time, so the pointer and the model cannot disagree (the stale one was
  always the pointer). `rowRef` and `skey` are gone — `viewmodel.Row` and the
  model's own key replace them. `showBuffer` split into `showRef` (intent, then
  render) and `renderBuffer` (render only, for an amended row, a `/clear` or a
  settings change), which is why a repaint no longer pretends to be a buffer
  switch. The composer's `OnChanged` feeds `SetDraft`, so the text in the widget
  is state the model owns rather than a field the switch has to remember to ask
  for.

  **The five audit bugs are pinned by table tests, headlessly** (17 tests, and a
  ten-mutation sweep in which all ten go red): draft ownership across networks
  including the reconnect that must keep the text and the first show that must
  not erase it; rename migrating draft, counts and the viewer, both while it is
  on screen and while it is not, and not touching the same name on another
  network; the buffer-vanished fallback landing on *that session's* server
  buffer and never another network's; unread bookkeeping (badges, suppression,
  mark-read, per-network keys, the purge a closed session takes with it, proven
  *after a reconnect* because the key is profile-scoped); pinned-but-offline
  rows, telling two of them apart by profile, and `Step` refusing to land on one
  — selecting an offline root dials a server, and keyboard browsing must not.

  **The `derived-unread` divergence is closed, the store's way** (§4.4). The
  Sink gained `UnreadChanged`, the session forwards `model.UnreadChanged`, and
  `internal/app`'s own count — the `!Status && !Playback && !Self` loop in
  `LinesAdded` — is deleted. There is one derivation left in the program,
  `state.countsUnread` against the read marker, and
  `TestRegression_UnreadCountsComeFromTheStore` now watches it end to end: the
  two M7 mutations that used to leave this package green (playback counting,
  our own multiclient message counting) both go red here. The marker is what
  zeroes a count, so `Session.MarkRead` (store.MarkReadLatest) is called when
  the user shows a buffer and when they leave one — §4.4's focus/blur half,
  local only; M9 puts `MARKREAD` on the wire and adds the NEW divider. Until
  the round trip lands the model applies the same zero itself, and §4.4's last
  evaluation step — suppression while the buffer is visible — is applied where
  the badge is read, so the buffer you are looking at never shows a count.

  **Behaviour is unchanged except where it is noted.** Two deltas, both small
  and deliberate: the channel browser now captures the session it was opened
  for, so a join from it cannot land on a network switched to while it was open
  (it already captured for Create); and "near the bottom" is one definition
  (`UI.nearBottom`) shared by the follow decision and the amend-repaint guard,
  which differ only at a gap of exactly 80px. What did NOT change: the
  notification rule still reads the Line's flags in `LinesAdded` — the store's
  `notifies`/`MessageAdded.Notify` and the per-buffer level are M9/M12's, and
  wiring half of it now would put the second copy back.

  **M9 inherits** a viewmodel whose counts come from the store and whose markers
  move locally, so the archive's job is to persist them and the server's is to
  agree; `Session.Buffer`'s projection is still what `renderBuffer` reads (it
  outlived this milestone); `ReadMarkerMoved` still stops in `deliver`'s
  default, which is where the NEW divider will pick it up; and the app still
  does not consume `Store.Focus`, so a highlight in the buffer you are watching
  still raises a desktop notification.

- **M8 — done. Verified: 57 mutations, 46 red on the first try, eight real blind
  spots, all eight now closed.** The gates, uncached: `gofmt` clean; `go vet
  ./...` and `go vet -tags live ./...` clean; `make build` green; the full suite
  green under `-race` with `-count=1`, again under `-shuffle=on`, and again at
  `-count=2` on the changed packages. Because a dispatcher is where a flaky
  suite would be born, a flake hunt on top of that: `internal/ui/dispatch` 124
  times (×40, ×15 at `-cpu=1`, ×15 at `-cpu=8`, ×6 shuffled, and 4 concurrent
  copies ×12 of the wall-clock tests under deliberate CPU contention) and
  `internal/app` 30 times. **No flake, not once.** The frozen armour is
  untouched: `git diff` from M7 to here reports **zero bytes** changed under
  `internal/transcript/testdata` (21 records) and `internal/render` (44
  goldens), and `TestRegression_*` grew from 43 to 44.

  **The structural claims hold.** `UICallbacks` and the twelve `stale()`/
  `active()` guards exist nowhere outside `v1/`; the only hits left in the tree
  are four comments explaining what was deleted. Exactly one `fyne.Do` carries a
  Change (`dispatch.emit`, defaulted from `Options.Do`, which `NewUI` never
  overrides). `go list` confirms the layering the milestone claimed —
  `app → ui/viewmodel → ui/dispatch → model`, with `viewmodel` importing only
  `dispatch` and `strings`, and nothing below the UI importing `internal/app`.
  §5.3's "`fyne.Do` exists only in the Dispatcher" is **not literally true**,
  and the package doc now says so instead of claiming the program has one call
  site: three remain in `internal/app` — two native-dialog callbacks hopping
  back from zenity's own goroutine (`prefs.go`) and the delayed quit
  (`commands.go`). None of them delivers anything. `UI` really is a renderer:
  `sess`, `current`, `currentKey`, `rows`, `unread`, `hlBuffers`, `drafts`,
  `members`, `networkName`, `stickBottom`, `rowRef` and `skey` are gone, and the
  only `map[` left in `ui.go` is a local in the mention resolver. The leak
  assertion is armour rather than decoration: `TestStopLeavesNoGoroutine` runs
  20 New/Stop cycles, and a `Stop` that returns without waiting for its
  goroutine goes red. The running app deliberately never calls `Stop` — the
  funnel is process-lifetime — so the teardown that matters is `Close`, and that
  one is now pinned by a test rather than by a comment (hole 2).

  **Unread is settled, not deferred again.** One derivation survives in the
  program, `state.countsUnread` against the read marker, and breaking it now
  goes red in `internal/app`'s own suite — which is exactly what M7's
  verification could not say. `Session.Buffer.Unread` is dead and a test keeps
  it dead; `Session.MarkRead → store.MarkReadLatest` is covered end to end
  (removing the marker move goes red, as does dropping the `UnreadChanged` that
  the marker's move emits). What is still duplicated is the *notification* rule:
  the store computes `MessageAdded.Notify` and nothing reads it. That is
  M9/M12's, it is recorded as such, and it is at least tested now where it is
  enforced (hole 4).

  **The eight blind spots, in five places, and the tests that close them:**

  1. **The session id had no test at all.** `dispatch.SessionID` is minted in
     `NewSession`, and keying it on `cfg.Server` — which a reconnect and the STS
     plaintext upgrade both repeat — passed the entire suite. One teardown would
     then silence the connection that replaced it, which is the exact failure
     the stable id exists to prevent.
     `TestFunnel_EachSessionHasItsOwnIdentity`.
  2. **Nothing tested the app's half of the liveness rule.** `UI.closeSession`
     is the only caller of `dispatch.Close`, and no test called `closeSession` —
     the funnel test called `d.Close` itself, under a comment saying it was
     "exactly what closeSession does". Deleting that line left the suite green
     and the funnel painting a dead session's traffic into a window that no
     longer lists it. `closeSession`'s model half is now `UI.retireSession`, and
     `TestFunnel_TearingDownASessionRetiresIt` holds it.
  3. **`internal/app/sink.go` — the real Sink — was executed by no test.** Every
     test hands the funnel a recorder instead, so the whole file was mutable at
     will: never filing the store's count, never migrating a rename, and
     answering "is this on screen?" by name alone (which paints one network's
     messages into another network's pane) were all green. `sink_test.go` covers
     the widget-free half headlessly, which since M8 is where the decisions are.
  4. **The notification rule was asserted against a copy of itself.** The suite
     checked `bufNotifies`, a mirror written in the test file, so changing the
     shipping rule — to notify for replayed history, i.e. a reconnect raising a
     hundred toasts — broke nothing. The rule is now `notifies()`, the mirror
     calls it, and a table pins §4.4.
  5. **The viewmodel's own suite had two gaps**: "the viewer lands on the newest
     remaining network" was indistinguishable from "the first" with only two
     sessions in the fixture, and a session registered twice doubling the
     sidebar was unguarded. Both are tables now.

  **Three mutations stayed green, and are named rather than glossed.** Two are
  equivalent mutants — an empty `Emit` costing a queue slot (nothing observable
  follows) and `MembersChanged` asking for the roster under `b.Name` instead of
  `u.current()`, which `showing` has already proven equal and `SortedMembers`
  folds anyway. The third is real and uncovered: deleting `BatchDone`'s
  `u.bufferList.Refresh()` changes nothing any test can see. **Whether a widget
  is TOLD to repaint is still untestable here.** Everything a repaint *decides*
  is in the viewmodel and tested; the call itself needs a canvas, and so does
  the rest of `sink.go` — the join dialog, the topic label, the member pane, the
  STS upgrade branch.

  **What could not be verified at all**: the live suite still has never run
  against a real Ergo from this environment (unchanged since M7), and no GUI was
  started here, so every claim about pixels rests on the viewmodel's tables and
  the render goldens, not on a window anyone looked at.

  **M9 inherits** the funnel and the Sink as the permanent seam, with
  `UnreadChanged` already in the method set and with `ReadMarkerMoved`,
  `TypingChanged`, `ModeChanged`, `SelfChanged`, `AuthStateChanged`,
  `ListUpdated` and `MessageFailed` still stopping in `deliver`'s default —
  that default is the list of what M9/M12 have to pick up, and it is one place.
  Counts come from the store and markers move locally, so the archive's job is
  to persist them and the server's (`MARKREAD`) is to agree. `Session.Buffer`'s
  projection is still what `renderBuffer` reads. The app still does not consume
  `Store.Focus` or `MessageAdded.Notify`, so a highlight in the buffer you are
  watching still raises a desktop notification and the per-buffer level is not
  wired. **The limit left in place deliberately**: `internal/app`'s widget half
  is tested only as far as it can be reached without a canvas. Closing that
  needs a headless Fyne test app, which this milestone chose not to introduce;
  the cheaper pattern — moving the decision out of the widget, as
  `retireSession` and `notifies` did — is the one M9 should follow, and the two
  places it most obviously applies are `BatchDone`'s repaint bookkeeping and
  `refreshBuffers`.

- **M9 — done. History and unread survive a restart, under the v1 face.** The
  milestone shipped in two commits: `internal/archive` (schema §5.4, no
  callers — its own progress is in that commit's message) and this one, the
  wiring. The gates, uncached: `gofmt` clean; `go vet ./...` and `go vet -tags
  live ./...` clean; `make build` green; the Windows cross-build green through
  the mingw toolchain (`modernc.org/sqlite` is pure Go, which is why); the full
  suite green under `-race` with `-count=1`, again under `-shuffle=on`, and
  again at `-count=4` on the changed packages, with the new archive tests run
  ten more times under `-race` on their own. **No flake.** The frozen armour is
  untouched: `git diff` reports **zero bytes** changed under
  `internal/transcript/testdata` (21 records) and `internal/render` (44
  goldens).

  **What the app does now that it did not.** Every change the store accepts is
  offered to the archive as the same change is folded into the projection
  (`Session.recordLocked`), so the live path is write-behind and the UI never
  waits for the disk. A buffer that opens asks the file for its newest page and
  its read marker, which is the whole of "history survives a restart" from the
  user's side. Scroll-up pages from the archive and, on a miss, issues
  `CHATHISTORY BEFORE msgid=<oldest>` and merges the reply above the window. A
  reconnect issues `CHATHISTORY AFTER` per open buffer plus `TARGETS`, and says
  one quiet sentence — *"Reconnected — caught up N messages."* — instead of
  replaying a flood. `MARKREAD` goes on the wire when a buffer is shown or left,
  a `MARKREAD` from another device moves the marker here, and the marker is
  stored, so the badge you left is the badge you come back to. LIST streams into
  `list_cache` and the browser reads it from there — including its search box,
  which asks the cache rather than filtering a resident copy — and the store's
  transient copy is dropped at the end of the scan (`Store.ForgetList`).

  **The privacy contract shipped in the same commit, not after it** (R-5): a
  per-buffer opt-out and an "Erase saved history…" on every channel's and every
  person's right-click menu, enforced in the Recorder and stored in the file it
  governs, so it survives a restart; a first-DM prompt raised once per profile,
  decided in the session and shown by the window; retention
  forever/365/90/30/session-only and an exclude-all-DMs toggle in Preferences →
  History; `REDACT` purging the row and its FTS entry (the queue is flushed
  first, or the withdrawn text would be written after the delete); and the
  no-encryption-at-rest statement written out in the FAQ, the getting-started
  guide, the power-user guide and the prompt itself. The one thing eco will not
  do here is pretend: the file is plaintext, that is said in the user's own
  vocabulary, and the answer offered is disk encryption rather than a password
  to lose.

  **Four decisions worth naming, because a later reader will want the reason.**
  (1) *A restored message's id IS its archive rowid.* The store is opened with
  `IDs: model.NewIDSeq(maxRowID)`, so every id it mints is above every id the
  file can hand back and one comparison orders a window that is half memory and
  half disk — which is what makes `m.ID > markerID` still mean "unread" after a
  restart. (2) *Restored messages are not Playback.* Playback means "the server
  is saying this again" and is what stops a replay from badging a buffer; these
  are the buffer's own contents. Nothing notifies for them because they arrive
  as `HistoryLoaded`, which never does. (3) *A row this session wrote with no
  msgid is never restored* — it can be recognised neither by msgid nor by an id
  the archive minted, so reading it back would put a second "You joined #eco"
  above the first. (4) *The marker's msgid beats its timestamp.* Server-time is
  millisecond-resolution and a burst shares a millisecond; taking the later of
  the two answers marks messages read that nobody has seen, and that is exactly
  how an unread count comes back as zero.

  **The M8-documented duplication is closed.** `MessageAdded.Notify` is now
  consumed: `Sink.LinesAdded` carries the store's decision, `internal/app`'s own
  copy of §4.4 is deleted, and the "notify me about everything" preference is
  pushed into the store as the network's default level
  (`Session.syncNotifyLocked`) so the rule is evaluated once, where the
  per-buffer level already lives. `dispatch.Sink` grew four methods —
  `LinesPrepended`, `HistoryLoading`, `AskDMLogging`, and the `notify` argument
  — and `deliver`'s default is one entry shorter.

  **Verified: 31 mutations, 23 red on the first try, seven real blind spots, all
  seven now closed, one equivalent mutant named.** The blind spots: the msgid-less
  row filter and the "a restore is not written back" rule were both covered only
  by an accident (the store's ids and the archive's rowids usually coincide, so
  a restore that broke both still looked right) — `TestArchive_RestoringNever
  DoublesWhatIsHere` states them directly now; the hydrate-once guard was not
  only untested but WRONG, keyed on "has this buffer ever been hydrated" rather
  than on the buffer's life, so closing and reopening a conversation left it
  empty (`forgetArchiveState`, `TestArchive_ReopeningAClosedBufferRestoresIt
  Again`); the notify-everything preference could be dropped on the floor
  between the window and the store without a test noticing; "stop asking when
  nobody can answer" was unheld, which matters because the UI fires `LoadOlder`
  from a scroll position; the marker's re-resolution after a restore was covered
  only in the order where `setMarker` happened to do the work anyway; and
  `RestoreHeadroom` was invisible because every test ran with an unlimited
  scrollback. The equivalent mutant: `gapReportLocked`'s `reported` guard, which
  cannot fire twice anyway because reporting resets the gap to inactive.

  **Stated rather than glossed.** A `CHATHISTORY BEFORE` chunk is reordered in
  the store (`moveTailToFront`) AND prepended in the projection, and the app
  suite can only see the second, because `messageLines` reads the projection —
  the store's half is held by `internal/state`'s own test and the two are
  maintained separately until M12 deletes the projection. Gap-fill messages
  arrive as playback and so do NOT badge a buffer (the v1 rule the regression
  suite pins); the caught-up line is the signal instead, and the restart path is
  where marker-derived counts come back. The catch-up count is approximate by
  construction: `TARGETS` is issued first so the buffers it discovers are
  counted, and a server that answers out of order reports short. Retention is
  applied at open and when the setting changes, not on a timer. `LoadOlder` and
  `MarkRead` emit from the Fyne thread, which the funnel's doc says is the
  session's goroutine's job; the queue is 256 deep and they emit one item, so
  this is the same latent shape M8 shipped with `MarkRead` and not a new one.
  Two sessions on one profile — a reconnect replacing a session — briefly hold
  the same file open, which the archive's two-pool design and `BEGIN IMMEDIATE`
  are built for and its concurrency tests cover.

  **What could not be verified at all**: the live suite still has never run
  against a real Ergo from this environment, and no GUI was started here, so
  every claim about pixels rests on the viewmodel's tables and the render
  goldens. Specifically untested for want of a canvas: `LinesPrepended`'s
  scroll-offset arithmetic (the "keep the reader where they are" half of
  scroll-up), the `OnScrolled` trigger, the "Loading older…" label, the
  first-DM dialog itself, and the Preferences → History pane. Every DECISION
  behind those is in the session or the funnel and is tested there, which is the
  pattern M8 asked M9 to follow.

  **M10 inherits** an app whose memory is on disk and whose attention model has
  exactly one derivation left in the program. `Store.Focus` is still not called,
  deliberately: §4.4 suppresses a notification for a buffer that is "focused and
  visible", this layer cannot answer the second half yet, and suppressing on
  buffer focus alone would silence mentions while the window is behind another
  one. `ReadMarkerMoved` still stops in `deliver`'s default — the marker moves in
  three places now, but the NEW divider that would draw it is M12's. The
  projection (`Session.Buffer.Lines`) outlived this milestone again. And
  `commands.go`'s `/history` still writes `CHATHISTORY LATEST` by hand through
  `irc.Send`, which is one of the wire writes M10's command table is meant to
  take away.

- **M9 — done (verified).** An independent adversarial pass over the two M9
  commits, on the standing rule that reading code and approving it is failure.
  **74 bugs were injected one at a time and reverted after each; 64 went red on
  the first try, 8 were real holes and 2 are equivalent mutants named below.**
  Separately, a privacy audit that asks the FILE rather than the API found one
  thing the milestone claimed and did not do, and one new test found a live bug
  in the wire path. All of it is fixed here; nothing was weakened to make it
  pass, and `git diff` reports zero bytes changed under
  `internal/transcript/testdata` (21 records) and `internal/render` (44
  goldens).

  **The headline claim, proved end to end and with a control.**
  `TestArchive_HistorySurvivesARestart` records through a real session, closes
  it and the file, opens a second session over the same path and finds the
  history. That test alone would also pass if the second session were keeping
  the first one's messages in RAM, or if the rejoin were replaying them off the
  fake server, so it now has a negative control:
  `TestArchive_NothingComesBackWhenNothingWasRecorded` runs the identical script
  with recording turned off for the buffer and requires the history to be
  **gone**. The two together are what say the surviving history came off the
  disk. Where the disk is is pinned too: `TestArchiveIDIsAFilenameForOneProfile`
  takes nine profile names — including `../../etc/passwd`, `C:\evil`, `""` and a
  unicode one — through `archiveID` into `archive.DefaultPath` and requires
  every one to land inside `<XDG_DATA_HOME>/eco/archive/`, to be stable across
  calls (an unstable name means a restart opens an empty archive) and never to
  collide with another profile's. That mapping had no test at all.

  **The eight holes the sweep found, and what closes each.**
  (1) *`CHATHISTORY BEFORE` was not anchored on a msgid.* Blanking the msgid in
  the request changed nothing any test could see — and the assertion that
  exposed it showed the shipped code was already sending
  `timestamp=<eco's local clock>` in the commonest case, because `LoadOlder`
  anchored the wire on the oldest ROW and the oldest row of a freshly joined
  channel is the status line eco wrote for itself, which has no msgid and
  carries no server-time. A client whose clock runs fast was asking for the
  history before a moment in the future. Fixed with `state.Store.OldestNamed`
  (the oldest row the server can name), used for the wire while
  `OldestAnchored` keeps answering for the disk. (2) *Session-only retention's
  purge at Close was untested*, because every test reopened through `Open`,
  which purges again — so Close could stop purging and the suite would not
  notice. (3) *The `from:` filter's exact comparison could be deleted*: the FTS
  match narrows loosely, and unicode61 splits `bob_smith` into `bob` and
  `smith`, so `from:bob` was answerable with bob_smith's messages.
  (4) *`arc.close` could stop waiting for its worker* — everything else settles
  first, which drains the queue and leaves the promise untested. (5) *Restore's
  dedupe by local id* was covered only by the msgid check standing in front of
  it. (6) *The read marker's rounding to milliseconds* was invisible because
  `Marker.Equal` compares to the millisecond, which is the difference the
  rounding exists to remove. (7) *"Erase saved history…" could stop flushing the
  write-behind queue first*, which would let a message still in the queue be
  written AFTER the erase and survive it. (8) *The retention sweep's scrub* —
  a hole in code added by this pass, found by re-running the sweep against it.

  **The two equivalent mutants, named rather than counted.** Dropping
  `, m.rowid DESC` from the pager's ORDER BY changes no result, because
  `(buffer_folded, ts, rowid)` is the index and SQLite walks it backwards; that
  is now a checked property rather than an accident —
  `TestAPageIsASeekAndAWalkOfTheIndex` EXPLAINs the statement the code actually
  runs (`pageSQL`) and requires the index and no TEMP B-TREE, which is also the
  local alarm for "the index stopped being used" that the budget can only raise
  globally and never under `-race`. And the Recorder's drain loop after a quit
  item cannot fire: `Record` holds the read lock across its send and `Close`
  takes the write lock once, so the quit is always last in a FIFO queue. It
  stays as documented defence.

  **The privacy finding no mutation could produce, because it was a missing
  feature.** `Search()` is not a witness for "the words left the index": it
  joins `messages_fts` against `messages` and drops `redacted = 1`, so a stale
  entry finds nothing to join to (a deleted row) or joins to the tombstone and
  is filtered out (a redacted one) — either way the search comes back empty
  while the words are still there. Asking `messages_fts` directly showed the
  index was in fact correct. Asking the FILE showed it was not: **after a
  REDACT, a Forget or a retention purge, the deleted message's words were still
  in the archive's bytes in plaintext, and `Vacuum` alone did not remove them.**
  FTS5 records a deletion by APPENDING a delete marker; the segment holding the
  original token is never touched, and VACUUM copies it across faithfully. Only
  the FTS5 `'optimize'` merges the segments and drops what the markers cancel,
  and only a VACUUM after that returns the pages. Neither was called anywhere in
  the program — `Vacuum` and `Optimize` had no callers outside tests at all, so
  "retention … with vacuum" (§5.4) was not shipped and the FAQ's "**No.** A
  message withdrawn … is removed from the history file" was not true of the
  file. Fixed: `archive.DB.Scrub` (optimize, then vacuum, with the reason in its
  doc comment) now runs on the three deliberate erasures — "Erase saved
  history…", a retention sweep that deleted something, and session-only purge at
  Open and at Close — and five tests read the raw bytes of the `.db`, `-wal` and
  `-shm` files and require the words to be absent. A `REDACT` arriving on the
  wire deliberately does NOT scrub (see the cost below); the FAQ, the getting
  started guide and the power-user guide now say that in the user's own
  vocabulary and point at the button that does.

  **The privacy contract as tested, not as described.** A buffer with logging
  off writes nothing (`TestArchive_PerBufferOptOutStopsTheDisk`, and
  `TestLoggingOptOutNeverTouchesDisk` at the Recorder, where the message never
  reaches a statement); the opt-out survives a restart, and a mutation that
  loaded the opt-out set with the wrong predicate is caught. The exclude-DMs
  toggle stops private messages and not channels, and is written through to the
  file. Retention deletes at every horizon and now vacuums, verified in bytes.
  A redacted message is gone from the table, from the index asked directly, and
  from the file after a scrub. Session-only leaves no rows AND no words after
  Close, checked through a read-only open that does not purge. The
  no-encryption-at-rest statement is in the FAQ, the getting-started guide, the
  power-user guide and the Preferences pane.

  **Measured, uncached, on this laptop** (`make test-budget-1m`, no race
  detector — instrumented, a pure-Go SQLite writes about twenty times slower):
  1,000,000 rows over 40 buffers, **298.0 MB on disk**.

      Latest(100)                          p50   547us   p95   945us   (budget p95 50ms)
      Page(100, anchored deep in history)  p50  3.77ms   p95  6.46ms   (budget p95 50ms)
      LastMsgID                            p50    86us   p95   103us   (budget p95 50ms)
      Search, rare word (1 in 503)         p50  16.6ms   p95  18.6ms   (budget p95 750ms)
      Search, mid word (1 in 7)            p50  74.8ms   p95  84.0ms
      Search, word in every message        p50 123.6ms   p95 129.4ms
      Search, two words                    p50 144.6ms   p95 150.5ms
      Search, word in one buffer           p50  59.1ms   p95  63.2ms
      Search, common word in one buffer    p50 254.2ms   p95 261.2ms   <- worst case
      Search, from:nick                    p50  32.3ms   p95  33.7ms

      Reconnect burst, 40 channels x 500:  20,000 rows in 1.957s = 10,218 msg/s,
                                           0 dropped, 80 transactions, slowest
                                           Record call 139us (budget >= 2000 msg/s)
      The same burst replayed:             233ms, 0 written, 20,000 deduped

  **No budget is missed.** Every number is within noise of the ones the M9
  commit recorded, independently reproduced. The one that differs is the replay
  rate (233ms here against the 161ms recorded), which is machine load and not a
  regression; it is written down as measured rather than smoothed.

  **Scrub's cost, measured because it is new and it blocks the writer**: on the
  same 1M-row archive, erasing one buffer of 40 (25,000 rows) took the file from
  298.0 MB to 384.2 MB (the delete goes through the WAL) and the scrub then to
  **229.1 MB, in 4.85s**. That is why it is on the deliberate erasures and not on
  the live path, and it runs on the archive's worker goroutine, never the Fyne
  thread. Stated rather than glossed: for those seconds the single writer
  connection is held, so a reconnect burst arriving in the middle of an erase
  would fill the Recorder's 4,096-message queue and drop lines — which is the
  write-behind's documented failure mode, reached by a rarer route.

  **Gates, all uncached.** `gofmt` clean; `go vet ./...` and `go vet -tags live
  ./...` clean; `make build` green; the Windows cross-build green through the
  mingw toolchain (`PE32+ x86-64` — `modernc.org/sqlite` is pure Go, which is
  the whole reason it was chosen); the full suite green under `-race` with
  `-count=1`, again with `-shuffle=on`, and `-count=2` across every changed
  package. **Flake hunt on the Recorder's goroutine specifically**: its tests
  plus the concurrency and crash tests at `-count=30 -race -shuffle=on` (467s),
  and the app-level archive tests at `-count=12 -race -shuffle=on`. **No flake.**

  **What could not be verified.** The live suite has still never run against a
  real Ergo from this environment and no GUI was started here, so nothing about
  pixels was checked; M9's own milestone line also asks for **RSS ≤ v1's ~205MB
  and idle CPU ~1%**, and neither was measured by the milestone or by this pass,
  because both need a running window. They remain owed. Windows and macOS paths
  are reasoned about and cross-compiled, never executed. And the scrub's
  guarantee is about SQLite's file, not about the filesystem underneath it: a
  copy-on-write filesystem, an SSD's wear levelling or a backup can still hold
  the old blocks, which is one more reason the answer to "I need this private"
  is disk encryption.

  **M10 inherits** an archive whose erasures are physical and whose wire anchors
  are msgids, and three things left deliberately: the pager's ORDER BY
  tiebreaker is now defended by a query-plan test rather than by luck, so a
  schema change that drops `idx_messages_buffer_ts` fails locally instead of in
  the budget run; `DB.Scrub` is called from three places and from nowhere on the
  live path, so if REDACT ever needs to be physical immediately, that is a new
  decision with a measured price attached (4.85s at 1M rows); and
  `commands.go`'s `/history` still writes `CHATHISTORY LATEST` by hand through
  `irc.Send` — now the only place in the program that anchors history by hand,
  and one of the wire writes M10's command table takes away.

- **M10 — done.** `internal/command` is the table §5.3 asked for, and
  `commands.go`'s 341-line switch is gone. `Parse(text, Context) Input`
  classifies one line — a message (including the `//` escape, which eats exactly
  one slash), one of the table's commands with its arguments split the way that
  entry's `ArgSpec` asks, or a verb nothing claims — and `Run(Input, Env)`
  performs it. `Env` is three small interfaces and nothing else: `Session` (the
  chat side: send, join, part, cycle, close, clear, whois, list, fetch history,
  nick, caps, the ignore list), `Wire` (24 typed verbs, implemented by
  `*irc.Client` itself) and `UIHooks` (OpenBuffer, ShowDialog, ShowHint, Notify,
  Refresh, Disconnect, Quit). The package imports no Fyne, no store, no
  ircevent, no `internal/irc`; the whole table is exercised headless against one
  fake struct.

  **64 commands, 76 verbs, one source of truth.** Everything the switch did is
  in the table as data or as a three-line `Run`, and the two lists that used to
  copy it are deleted: `entry.go`'s hand-written `slashCommands` (Tab completion
  now filters `command.Verbs()`) and the giant `/help` string (now
  `command.Summary()`, with the documentation window's Commands section spliced
  from `command.HelpMarkdown()` at a `<!--commands-->` marker). `/help name`
  explains one command from the same entry. The guards the engineering audit
  added survive with tests they never had: `/part` refuses a non-channel before
  anything reaches the wire, `/join` gives a bare name its `#`, `/cycle` keeps
  the buffer, `/quit` still flushes for 300ms before the window closes (the
  delay is in the UI hook, where it belongs), and `/history` refuses the network
  log.

  **All wire writes now live in `internal/irc`.** Sixteen typed verbs were added
  there — `Privmsg`, `Notice`, `Ctcp` (the `\x01` framing and the upper-casing
  are protocol, so they moved), `Topic`, `Away`, `SetName`, `Register`,
  `Verify`, `Oper`, `Who`, `Whowas`, `Knock`, `Wallops`, `Kill`, `Quit`,
  `ServerQuery` — so `*irc.Client` satisfies `command.Wire` outright;
  `var _ command.Wire = (*irc.Client)(nil)` in `internal/app` states the
  layering. `/history` no longer hand-writes `CHATHISTORY LATEST`: it goes
  through `Session.FetchHistory` → `Client.History`, which means it is labelled
  and capped by the server's advertised limit like every other history request.
  The app's `wire` façade lost the four methods only the commands used; what is
  left (`Send`, `Join`, `CurrentNick`) is browser.go's and rowmenus.go's, and
  they are M12's to rewrite.

  **The unknown-command intercept** is the beginner-visible win this milestone
  owed (R-14). v1's default branch upper-cased whatever was typed and wrote it
  to the wire; a typo became a numeric nobody reads. `/jion #eco` now answers
  *"Unknown command /jion. Did you mean /join? Type /help for the list, or send
  it to the server anyway with /raw JION #eco."* — the suggestion is a
  Damerau-Levenshtein search over the table (a transposition costs one edit, or
  "/jion" sits the same distance from /join as from /ison and the tie-break
  answers with nonsense), a prefix is treated as an abbreviation rather than a
  typo, and nothing within two edits means no guess at all. `/raw` and `/quote`
  still send anything, and the offer names the exact line v1 would have sent.

  **Deliberate deviations from "identical behaviour", all of them narrowings of
  an error path:** the commands whose whole meaning is "this channel"
  (`/topic`, `/names`, `/cycle`, `/kick`, `/kickban`, `/ban`, `/unban`,
  `/invite`, the mode shortcuts) now say so instead of asking the server about a
  nickname; `/mode` with no arguments asks about the buffer you are in instead
  of sending a bare `MODE`; `/help` works with no connection (the dialog needs
  none); `/ignore` and `/unignore` are new commands over the existing
  preference-backed list. Everything else — including the silence when there is
  no connection, and `/identify` and the services shortcuts accepting an empty
  argument — is v1's behaviour on purpose.

  **Tests: 23 in `internal/command` (161 with subtests), covering every entry's
  effect against the fake env, the parse shapes, the intercept and its
  suggestions, and the generated help;** plus 10 wiring tests in `internal/app`
  (the table reaching a real session and a real socket, a hint landing in the
  buffer it was typed in, completion and the documentation page derived from the
  table) and a wire-line test per new verb in `internal/irc`. **An
  eight-mutation sweep, one bug at a time and reverted after each, went red 8/8**
  (`/join` stops adding the `#`; the `NeedsChannel` guard deleted; `/kickban`
  kicks before it bans; the `//` escape dropped; `/history` asks for 100;
  the intercept passes the verb through after all; `Verbs()` unsorted and
  including hidden; the `/part` guard removed). The last of those was caught by
  the *existing* app regression suite —
  `TestRegression_PartInNonChannelBufferKeepsBuffer`, written when the audit
  found that guard — which is what says the port did not quietly change what the
  old tests were watching.

  **M11 inherits** a command layer with no reach into the UI toolkit and a
  `Session` interface that is, in effect, the first written-down list of what
  the app layer owes the rest of the program — 14 methods, all of them things
  `internal/state` or `internal/irc` already know how to do. Two things are left
  deliberately: `Command.Run` takes `Env`, not a context, so a command that
  wants to be cancellable (a long `/list`, a `/history` walk) has nowhere to put
  one yet; and the intercept has no second-Enter passthrough — `/raw` is the
  explicit escape hatch, and adding a "type it again" affordance would need
  state the composer does not keep.

- **M10 verification — done.** An **81-mutation sweep**, one bug at a time and
  reverted after each, over the parser, the table, every audit-era guard, the
  env, the intercept, the completion list, the generated help and the app's
  adapter. **Seventy of those ran against the milestone as committed and 56 went
  red on the first try; 14 stayed green, 13 of them real holes.** They are
  closed, with the tests that close them named here because each one is a
  property the milestone claimed and nothing checked.

  **The two "deleted" lists could still drift, which was the milestone's central
  claim.** `entry.go`'s `slashMatches` could be replaced by a 76-element literal
  and `commandsDoc()` by a second renderer, and the whole suite stayed green:
  every existing test asked "does your list agree with the table *today*",
  which a snapshot taken today passes forever — exactly how v1's two lists
  began. Fixed structurally, not by another assertion: the filter moved into
  `command.Complete`, so `slashMatches` is a one-line delegation and the UI
  holds no list of commands at all; the page is asserted to be `docCommands`
  with `command.HelpMarkdown()` spliced in **verbatim**; and
  `command.AddForTest` (which panics outside a test binary) lets
  `internal/app` register a command the composer has never heard of and demand
  that Tab and the documentation page both show it. That last one is the only
  form of the question a frozen copy cannot pass.

  **Tab completion itself was untested.** `slashMatches` was covered; its only
  caller was not, so `chatEntry.complete` could stop calling it and every
  completion test stayed green. `TestTabCompletesASlashCommandFromTheTable`
  now drives the actual key, including the cycle and the nickname case.

  **`/quit`'s 300ms flush could be deleted silently** — the guard M10 moved into
  the UI hook had no test at all, because `u.app` is nil in every headless test
  and the hook returned before reaching the delay. The wait is now a named
  `quitFlush` and the close goes through a `closeApp` variable, so a test can
  watch *when* the window closes; it asserts both the elapsed time and the
  constant, since a wait of zero is still a wait.

  **The env's narrowness was a comment.** `internal/command` could import
  `internal/irc` — and through it `ircevent`, `net/http` and the transport —
  with nothing to stop it. `TestPackageStaysNarrow` walks the package's
  transitive imports with `go/build` and fails on Fyne, ergochat, the store, the
  archive, the UI packages, `database/sql` and `net`.

  **Four wiring bugs in the app's adapter were invisible:** `/join #chan key`
  could drop the key (nothing asserted the key reached the wire — a `+k`
  channel would simply fail to open); `ShowHint` could read the *current*
  buffer instead of the one captured at parse time, which is the one thing the
  adapter's own comment says it exists to prevent; `/msg` could open a
  conversation without showing it; and `/ignore`/`/unignore` — new commands
  with no app-level test — could pass the flag through inverted, so `/unignore`
  would ignore. Each now has a test through the real session and, where it
  applies, the real socket. Showing a buffer needed one seam: `selectRef` on a
  UI with no widgets now moves the viewmodel and returns — since M8 the
  viewmodel *is* which buffer is on screen, and everything after it is painting.

  **Two wire lines were untested where "omitted" and "empty" differ.**
  `Client.Kick` and `Client.Redact` special-case a missing reason, and without
  that branch `ForceTrailing` writes `KICK #eco bob :` — a kick whose comment is
  blank — where v1 sent `KICK #eco bob`. The verb table's assertions all used
  `testsrv.Match`, which is happy with the shorter line as a *prefix* of the
  longer one; `TestAnOmittedReasonIsOmittedNotEmpty` compares the whole line.

  **The rest were smaller:** `NeedsChannel` consulted `Ctx.IsChannel()` but
  nothing proved it, so it could be rewritten to test the buffer's name and let
  `/kick` through in a network log that happened to be `#`-prefixed; the
  duplicate-verb panic that keeps two entries from claiming one verb had no
  test; `All()` handed out a copy with nothing insisting on it; and the
  generated help's group order — Chat before Server operators, the order a
  beginner meets the client in — could be reversed in either dimension
  unnoticed (the within-group check had to read its expectation from `All()`
  rather than from `groups()`, which is the thing under test). Eleven further
  mutations were run against the new tests; three found holes in *them* and one
  found a tautology in one written that hour (asserting `elapsed >= quitFlush`
  passes when `quitFlush` is zero). All are closed.

  **Behaviour preservation: every one of v1's 75 verbs is present** — checked by
  diffing the old switch's case labels against the table, which reported nothing
  missing and `/ignore` + `/unignore` added. Beyond the deviations the entry
  above already lists, the audit found **three unrecorded ones, all of them a
  silence becoming a sentence**: `/kick` with no nickname was a silent no-op in
  v1 and now answers `Usage: /kick nick [reason]`; `/cycle` outside a channel
  was silent and now says so; and `/docs` and `/helpwindow`, being aliases of
  `/help` rather than their own case, now also print the command summary and
  accept `/docs join`. Nothing else changed.

  **What "all wire writes live in internal/irc" does and does not mean.**
  `ircevent` and `ircmsg` appear in `internal/irc` and nowhere else — confirmed
  by grep, and now by `TestPackageStaysNarrow` for the command layer
  specifically. But `internal/app` still *names* protocol verbs through the
  generic `wire.Send`: `rowmenus.go` (the channel-settings form, `INVITE`, the
  member menu's `MODE ±o/±v/+b` and `KICK`, and its own kickban that bans before
  it kicks) and `browser.go` (the modes and topic applied after creating a
  channel). The bytes are formatted in `internal/irc`; the *decision* about
  which verb and which parameters is not. Those are the two surfaces M12
  rewrites, and the façade shrinks to nothing when it does.

  **M11 also inherits** a completion path that is now provably the table's —
  which matters because M11 rebuilds the composer's neighbours — and one new
  seam it should keep: `UI.built()`, the honest name for "this window has no
  widgets", which is what lets the command tests drive a real session with no
  Fyne window. The rest of the paint path is still untestable headless
  (`renderBuffer` wants eight widgets and a canvas), so `/clear`'s repaint and
  the buffer-list refresh remain unverified below the viewmodel; that is the
  ceiling the mutation sweep hit, and it is M11's to raise.
