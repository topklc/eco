// Package model holds the types shared between the protocol, state, render and
// UI layers. It is pure: standard library only - no fyne, no ircevent, no
// network, no globals, no clocks it does not take as an argument.
//
// # The two unions
//
// The v2 pipeline is one directional flow (blueprint §5.3):
//
//	wire → irc.Client → Events() <-chan model.Event
//	     → state.Store.Apply(Event) → []model.Change
//	     → archive Recorder / ui.Dispatcher
//
// [Event] is what the wire produces. [Change] is what the store produces for
// the UI. Both are closed unions.
//
// # Why interface + concrete structs (and not one tagged struct)
//
// Decision, so nobody relitigates it: each variant is its own struct and the
// union is an interface. The alternative - a single tagged struct with a Kind
// field and the union of every variant's fields - was rejected because it makes
// invalid states representable (a Join with a Topic string set), it grows a
// wide struct that every consumer copies, and it gives the compiler nothing to
// check: a typo'd field access still builds. With concrete structs, a consumer
// writes a type switch, each case names exactly the fields that exist, and
// adding a field to one variant cannot silently widen every other one.
//
// The accepted costs: a type switch is not exhaustiveness-checked by the
// compiler (mitigated by a default case plus [Event.EventName] for the
// "unhandled X" log line), and each event is an interface value, so it heap
// allocates. At IRC message rates that is noise.
//
// Both interfaces carry a name method rather than an empty sealing method:
// [Event.EventName] and [Change.ChangeName] both seal the union to this package
// in practice *and* give logs, test failures and internal/transcript's records
// a stable label for free.
//
// # Naming rule
//
// Event names are wire nouns - [PrivMsg], [Join], [Mode], [Topic]. Change names
// are past-tense facts about the store - [MessageAdded], [TopicChanged],
// [BufferCreated]. Where both unions need the same concept the Change takes the
// past-tense name ([HistoryChunk] the event vs [HistoryLoaded] the change,
// [AuthResult] the event vs [AuthStateChanged] the change), so a bare type name
// always says which side of the pipeline you are on.
//
// # Identity rule
//
// Nothing is keyed by a mutable display string. Buffers are [BufferID]
// (network + casefolded name), messages are [MessageID] (a local stable
// counter) plus the server's msgid when there is one. The display name travels
// as data, never as a key.
package model
