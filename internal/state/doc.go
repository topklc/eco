// Package state is the client's protocol-level memory: one Store holding, per
// network, the buffers, members, topics, channel modes, messages, read markers,
// unread counts, ignore rules and LIST cache that the UI renders.
//
// THREADING: state is confined to the dispatcher goroutine. Store carries no
// mutex, starts no goroutines, performs no I/O and never blocks — it is a pure
// reducer over events, so the state mutex can never cross a package boundary
// because there is no state mutex. Every method must be called from the one
// goroutine that owns the Store.
//
// THE SHAPE
//
//	irc.Client → model.Event → Store.Apply(Event) → []model.Change → dispatcher
//
// Apply is total: an event it does not model returns no changes rather than
// panicking, and an event for an unknown network creates that network's state
// on the spot. Changes are self-contained (see model.Change): a consumer never
// calls back into the store to interpret one, because by then the store has
// moved on.
//
// Everything the user does that has state consequences enters through the
// intent API — OpenBuffer, CloseBuffer, RequestJoin, RequestPart, RequestCycle,
// Echo, Focus, MarkRead — which returns the same []model.Change. Wire writes
// are not this package's business: internal/irc owns those. What the store
// offers instead is the *plan* (RejoinTargets, GapFillTargets, PendingJoin) so
// the layer that does write to the wire never has to guess.
//
// WHAT THIS FIXES FROM v1 (internal/app/session.go, the specification this was
// ported from)
//
//   - Identity is folded. Every buffer and member key is a
//     proto.ServerProps.Fold of the name under the network's advertised
//     CASEMAPPING, never strings.ToLower, and channels are recognised by
//     CHANTYPES rather than a hardcoded '#'. STATUSMSG targets ("@#chan") route
//     to the channel instead of opening a bogus query.
//   - Echoes resolve by label. An outgoing message is registered with Echo,
//     rendered immediately as model.PendingEcho, and resolved in place when the
//     labeled echo arrives — gaining the server's real msgid and time. v1's
//     buffer|text matching (pendingEcho) is gone, so two identical messages in a
//     row are distinguishable and our own messages are redactable.
//   - Playback is decided by batch context. The store reads
//     EventMeta.Playback, which internal/irc resolves from the real batch
//     registry. v1's "has a batch tag" plus the 2-minute liveML recency window
//     are both gone, and multiline arrives pre-assembled as one message.
//   - Whois correlates per request. TrackWhois records which buffer asked
//     about which nick; replies route by the nick in the reply, and 318 clears
//     the entry. v1's single whoisTarget slot (never cleared, last writer wins)
//     is gone.
//   - msgid dedupe is exact and per buffer, bounded by the buffer's own
//     scrollback rather than v1's wholesale wipe of a shared 8192-entry map.
//   - Unread and highlights are derived here, from the read marker, so the UI
//     renders a number instead of inventing one. Highlight matching folds
//     Unicode and honours the user's word list; per-buffer notification levels
//     (Mute / Mentions / All, defaulting to the network's) decide Notify.
//
// Nothing in this package reads a clock it was not given (Options.Clock) or
// mints an id from a package-level counter (Options.IDs), so a test pins both.
package state
