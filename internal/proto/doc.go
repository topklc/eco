// Package proto holds eco's protocol tables and the parsers that fill them:
// ISUPPORT (005) → ServerProps, CHANMODES/PREFIX-driven mode-delta parsing, the
// capability set model, and the batch type registry.
//
// It is pure. It imports the standard library and internal/model, nothing else
// — no ircevent, no net, no fyne, no state, no globals, no clocks. The
// dependency runs one way only: model is the leaf, proto builds on it, irc and
// state build on proto. Nothing in proto starts a goroutine or holds a lock.
//
// # Why this package exists
//
// v1 hardcoded the answers this package parses: strings.ToLower as the
// casemapping, '#' as the only channel type, "~&@%+" as the prefix rank, and a
// literal switch over mode letters in Buffer.applyModeDelta. Each of those is a
// correctness bug on a server that says otherwise:
//
//   - On an rfc1459 server, []\~ are the uppercase forms of {}|^, so
//     strings.ToLower makes "nick[]" and "nick{}" two different people.
//   - With CHANTYPES=#&, '&' channels are channels; v1 treated them as queries
//     and never rejoined them.
//   - With STATUSMSG=@+, a PRIVMSG to "@#chan" is a channel message; v1 opened
//     a query buffer named "@#chan".
//   - With PREFIX=(qaohv)~&@%+, member rank is data; with CHANMODES it is data
//     that decides whether a mode letter consumes a parameter.
//
// Everything here is therefore table-driven off what the server advertised,
// with documented defaults for the case where it advertised nothing.
package proto
