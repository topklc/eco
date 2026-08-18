# eco threat model (v2 security floor)

One page, per blueprint §5.6. Ratified at M0.

## Adversaries and surfaces

**Malicious or compromised server.** The server controls every byte we parse.
- Over-wide art lines are a width-DoS on the Timeline: art rows CLIP with
  horizontal scroll inside the row (ruled explicitly; never resize the pane).
- Hostile numerics/params must never index arrays unchecked (v1's handlers
  guard `len(e.Params)`; the v2 protocol core keeps that discipline under
  table tests).
- History playback is untrusted: msgid dedupe and batch typing decide what
  mutates state, not message text.

**Hostile channel content.** Links are never auto-fetched (NEVER list: no
inline URL previews). Art/color codes render through the frozen engine which
bounds palette indices. Mentions resolve only against known members.

**Shared machine.** The archive and DM logs are plaintext on disk: DM logging
is opt-in per the first-DM prompt; retention settings and per-profile purge
must exist in the same release that ships the archive (M9). Notification
toasts have a content-privacy option (name-only).

**Credential handling.** Passwords live in the system keyring only. If no
keyring is available, v2 REFUSES to persist the secret and warns — it never
falls back to plaintext JSON (v1 did).

**Exec surface.** Desktop notifications shell out (notify-send/zenity paths):
backends are feature-detected behind the Notifier interface and every argv
passes the `--` injection guard. No other user-controlled exec exists; adding
one requires updating this document.

## TLS floor

wss:// or TLS with MinVersion 1.2 for anything carrying credentials; STS
policies persist with expiry/revocation (v1 logic carried verbatim).
Plaintext connections remain possible only with the explicit "TLS-only" pref
off, and never silently when a password is present (warning line kept).

## Update path

Security fixes are only real if deliverable: the update notifier (R-16) is a
MUST-adjacent commitment; releases are tagged and CI-built.
