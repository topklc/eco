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

**Next: M5** — `internal/testsrv` (scripted `net.Pipe` fake server +
transcript replay) and the regression contract, which must be green
*before* the protocol core is rewritten in M6.
