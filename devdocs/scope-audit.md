# Scope audit

What eco currently supports that it does not really need to. Written as
an honest look at scope creep, in three tiers. Date: 2026-08-16.

## Tier 1: real removal candidates

1. **Legacy keyring migration** (`networks.go`, the `keyringGet`
   fallback to the old service name). It exists solely to migrate
   passwords from the development machine's earlier install, and that
   migration already ran. Dead weight for anyone who downloads eco.
   Safe to delete before shipping.
2. **Plain `ws://` support**. Unencrypted websocket IRC is a lab
   curiosity; nobody should ship traffic that way, and eco even blocks
   it under TLS-only mode. Requiring `wss://` would delete a code path
   and remove a footgun.
3. **`/ison` and `/userhost`**. Genuinely obsolete numerics-era
   commands; `/monitor` and `/whois` cover their use cases, and the raw
   passthrough would still work for anyone who insists.

## Tier 2: negotiated but doing nothing

These cost near zero (a cap request each), and the "chase every draft"
identity of the client argues for keeping them. But the README should
say "negotiated" rather than "supported" where nothing happens beyond
the ACK.

4. **`draft/languages`**: requested, nothing parsed, no UI.
5. **`extended-monitor`**: negotiated with no monitor UI behind it.
6. **`draft/pre-away`** and **`draft/relaymsg`**: negotiated for
   correctness, but eco changes no behavior because of them.
7. **`account-tag`, `account-notify`, `extended-join`**: account
   information is received and none of it is displayed.

## Tier 3: niche but intentional, keep

8. **Notify on every message**: spammy mode few will enable, but it is
   a single checkbox and was an explicit request.
9. **SCRAM-SHA-256**: over TLS the security win versus PLAIN is modest,
   but it is real and already tested.
10. **Join-all with the 75 channel cap**: unusual for an IRC client in
    general, but it is the whole point of eco on a small network.

## Recommendation

Cut items 1 and 2. Cut or keep item 3 on taste. Soften the README
wording for tier 2 from "supported" to "negotiated". Touch nothing in
tier 3.
