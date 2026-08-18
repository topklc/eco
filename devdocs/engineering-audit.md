# Engineering audit report

Full-codebase audit by a three-engineer team with fix authority, plus an
integration pass. Date: 2026-08-16. Every fix verified with go vet and
the live race-detected test suite against testnet.ergo.chat.

## Bugs found and fixed

### Severe

1. **Auto-reconnect never rejoined channels** (session). After a dropped
   connection the client reconnected but silently rejoined nothing; the
   sidebar showed every channel while the user was no longer in any of
   them. Reconnects now batch-rejoin every open channel buffer, and the
   existing history refetch plus msgid dedupe fill the gap without
   duplicates.
2. **Live multiline messages could not highlight or count as unread**
   (session). Any message inside a batch was classified as history
   playback, and live multiline messages arrive in batches. A multiline
   message that mentioned you never notified, never badged, never
   counted. Live batches are now distinguished by the absence of a
   per-message msgid plus a two minute recency window.
3. **Profile delete could corrupt a neighbor** (networks). After
   deleting a profile the form kept showing the deleted data, and Save
   wrote it over whichever profile slid into the selected slot.
4. **Cross-network draft misfiling** (ui). Switching buffers across
   networks saved the unsent draft under the wrong network's key, and
   same-named buffers on different networks carried text into each
   other. Draft slots are now owned by the exact buffer shown.

### Significant

5. **Plaintext view froze at the scrollback cap** (ui). The incremental
   renderer validated by length only; once trimming began, length stayed
   constant and updates stopped forever.
6. **The channel Create button could join the wrong channel** (browser).
   It reused the top-match logic, so "Create #dev" joined #developers
   when that matched the filter. The button now takes the literal name.
7. **Clicking the second offline pinned network connected the first**
   (ui). Offline pinned rows shared an identity; matching now includes
   the profile name.
8. **Keyboard buffer cycling could initiate a connection** (ui).
   Ctrl+PageUp/PageDown landing on an offline pinned row triggered its
   click action. Cycling now skips offline rows; deliberate Alt+N jumps
   still work.
9. **Reconnecting inherited ghost badges** (ui). Unread counts and
   mention flags from a closed session survived into a new session with
   the same network name.
10. **Channel rename bounced the viewer to the server log** (session +
    ui, fixed at integration). A draft/channel-rename now migrates the
    buffer's draft, unread count, and mention flag, and the viewer
    follows the renamed buffer.
11. **Mentions inside multiline continuations were silent** (session +
    ui, fixed at integration). A mention arriving in the middle of a
    merged multiline message now raises the notification and sidebar
    badge like any other mention.

### Minor

12. Renamed or deleted profiles left orphaned passwords in the keyring;
    both paths now clean up their entries.
13. Nine commands with empty arguments sent protocol-invalid lines
    (/redact, /kill, /whois, /whowas, /setname, /knock, /wallops,
    /invite, /me); each now prints usage. /notice with only a target and
    /history in the server log explain themselves. Raw passthrough no
    longer sends a trailing space.
14. The notify-send path resolver existed but was not wired into the
    popup path.
15. Websocket URLs with explicit ports displayed as host:8443:443.

## Optimizations applied (behavior-preserving)

- JOIN, NAMES, and topic handlers consolidated to one lock acquisition
  each instead of two.
- STS policy writes prune expired entries.
- richSegments allocates with exact chunk capacity.
- jumpToUnread uses precomputed row keys.
- A pane switch clears the pending-relayout flag, avoiding one redundant
  full relayout when a flush lands across the switch.
- loadNetworks deduplicated onto loadNetworkMeta, removing a
  hand-synced default list.

## Recommendations (not applied, for decision)

1. **Tradeoff items from the optimization review**: remove the legacy
   ergoview keyring migration (dead code for the public); downsample the
   embedded icon to 256px (~55KB binary savings, visually identical at
   render sizes); strip debug symbols for ~30% smaller binaries at the
   cost of external debugger symbolization.
2. **Version control**: the project has no git repository; every future
   audit and before/after comparison would benefit from one.
3. **Scrollback field apply-cadence**: typing "3000" applies at "300"
   then "3000"; harmless, a debounce would change apply timing and was
   declined under the lossless mandate.
4. **Theme select vs custom background**: with a custom background set,
   the luminance override makes the Theme select appear inert; a UX
   decision, documented in the preferences code.
5. **Redaction UI**: /redact requires msgids that the UI never surfaces;
   a right-click "delete message" on own messages would make the feature
   reachable.
6. **Accepted quirks, reviewed and deliberately kept**: shift-key state
   can stick until the next press if focus leaves mid-hold; whoisTarget
   persists after a WHOIS completes; the seenIDs cache resets wholesale
   at 8192 entries; stale pendingEcho entries across reconnects are
   harmless because display is local-first.

## Verification

gofmt clean, go vet clean, and the live test suite (real TLS connect,
capability negotiation, join, send, echo dedupe) passes under the race
detector. The running build includes every fix above.
