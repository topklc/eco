# Project memory

The working memory of the eco project: current state, where everything
lives, and how to pick the work back up. Update this when anything
structural changes.

## What eco is

A beginner friendly, IRCv3-complete IRC client in Go (Fyne GUI), built
around Ergo's feature set, developed with AI assistance for Toprak
Kilic, who runs his own Ergo network. MIT licensed. Version 0.1.

## Current state (2026-08-16)

Shippable. Four audit rounds complete (last one a three-engineer team
pass fixing 15 bugs), full optimization pass applied, live race-detected
tests green, both release binaries current in dist/. The user has called
it his dream IRC client. Shipping format: bare binaries only, no
archives; docs and license are embedded in-app.

## Layout

- Root: all Go sources, one package. session.go is the protocol layer
  (no Fyne imports), ui.go the interface, and the seam between them is
  the UICallbacks struct with per-session staleness guards.
- Other sources: commands.go (slash commands), entry.go (input widget:
  tab completion, history, shortcuts), networks.go (profiles, keyring,
  connect flow), menu.go (theme, Control Center, applySettings),
  prefs.go (preferences dialog), browser.go (channel browser), help.go
  (embedded docs viewer, About with license), colors.go (mIRC palettes,
  formatting renderer), sts.go, scram.go, wsconn.go, notify.go.
- assets/ icon; docs/ the six USER guides (embedded via go:embed, so
  editing them updates in-app help on rebuild); devdocs/ internal
  (never shipped); dist/ build outputs; scripts/build-windows.sh
  bootstraps MinGW.

## Build and test

- make install: build plus desktop integration on this machine.
- make windows / make dist: cross-compile via ~/.local/mingw.
- go test -race . runs LIVE tests against testnet.ergo.chat (connect
  and caps; send and echo dedupe). These gate every change.
- Environment: Go at ~/go, GOPATH=~/gopath,
  CGO_LDFLAGS="-L$HOME/.local/lib" (libXxf86vm symlink, no sudo).

## Key invariants (break these and audited bugs return)

- Batch-tagged events never mutate member state or trigger history
  requests; live multiline is distinguished by missing msgid plus
  recency.
- Own messages display locally first; server echoes and history
  replays are deduped (pendingEcho, per-buffer msgid seen-set).
- Reconnect rejoins open channels and refetches history; dedupe makes
  that safe.
- STS policies persist only from TLS connections.
- All UI state is keyed by session AND buffer (skey); the draft slot is
  owned by the buffer actually shown (currentKey).
- Every UI mutation from callbacks goes through fyne.Do; message pane
  refreshes are coalesced and gated on visible change.
- Secrets are resolved from the keyring only at connect time, never in
  the sidebar refresh path.

## Deliberately open

- devdocs/ui-polish-plan.md: five approved-pending polish items.
- Redaction lacks a UI handle for msgids (right-click delete idea).
- Declined tradeoffs: icon downsample, symbol stripping.
- No git repository yet (recommended in every audit).
- Negotiated but unused caps: draft/read-marker, draft/typing absent.

## Companion documents

lessons-learned.md (the accumulated knowledge), engineering-audit.md
(the team audit record), scope-audit.md (what could be cut),
ui-polish-plan.md (pending UI items).
