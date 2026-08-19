package dispatch

import (
	"testing"

	"github.com/toprakkilic/eco/internal/model"
)

// M9's additions to the vocabulary, held where the vocabulary lives: deliver()
// is the whole Change-to-Sink mapping, and every one of these is a case in it
// that used to be absent or wrong.

// A message's notification is the STORE's decision, carried on the change, and
// the funnel hands it over untouched. Before M9 the screen re-derived it from
// the row's flags - the last duplicated rule in the program (M8's progress
// entry), and one that disagreed with the store the moment a per-buffer level
// was set.
func TestNotifyRidesOnTheChange(t *testing.T) {
	d, r := newFunnel(t, forever)
	d.Open("s1")

	d.Emit("s1", []Item{
		Of(model.MessageAdded{ChangeMeta: model.BufMeta(buf("#eco"))}, "#eco", line("quiet")),
		Of(model.MessageAdded{ChangeMeta: model.BufMeta(buf("#eco")), Notify: true}, "#eco", line("loud")),
	})
	d.Flush()

	calls, _, _, _ := r.snapshot()
	assertCalls(t, calls, []string{
		"lines s1 #eco [quiet]",
		"lines s1 #eco [loud] !",
	})
}

// A history chunk NEVER notifies, whatever it contains. This is the reconnect
// storm in one line: a hundred replayed messages must not be a hundred toasts.
func TestHistoryNeverNotifies(t *testing.T) {
	d, r := newFunnel(t, forever)
	d.Open("s1")

	d.Emit("s1", []Item{Of(model.HistoryLoaded{
		ChangeMeta: model.BufMeta(buf("#eco")),
		Direction:  model.HistoryLatest,
	}, "#eco", line("old one"), line("old two"))})
	d.Flush()

	calls, _, _, _ := r.snapshot()
	assertCalls(t, calls, []string{"lines s1 #eco [old one,old two]"})
}

// Direction is what tells a block of history from a block of news. A BEFORE
// chunk is the page ABOVE the reader - scroll-up, from the archive or from the
// server - and delivering it as LinesAdded puts yesterday under today.
func TestHistoryBeforeIsPrepended(t *testing.T) {
	d, r := newFunnel(t, forever)
	d.Open("s1")

	d.Emit("s1", []Item{
		Of(model.HistoryLoaded{
			ChangeMeta: model.BufMeta(buf("#eco")),
			Direction:  model.HistoryBefore,
		}, "#eco", line("older one"), line("older two")),
		Of(model.HistoryLoaded{
			ChangeMeta: model.BufMeta(buf("#eco")),
			Direction:  model.HistoryAfter,
		}, "#eco", line("newer")),
	})
	d.Flush()

	calls, _, _, _ := r.snapshot()
	assertCalls(t, calls, []string{
		"older s1 #eco [older one,older two]",
		"lines s1 #eco [newer]",
	})
}

// The "Loading older…" state travels on the same queue as the rows it is
// waiting for, so it can never arrive after them.
func TestHistoryLoadingIsDelivered(t *testing.T) {
	d, r := newFunnel(t, forever)
	d.Open("s1")

	d.Emit("s1", []Item{
		Of(HistoryState{ChangeMeta: model.BufMeta(buf("#eco")), Loading: true}, "#eco"),
		Of(model.HistoryLoaded{
			ChangeMeta: model.BufMeta(buf("#eco")),
			Direction:  model.HistoryBefore,
		}, "#eco", line("older")),
		Of(HistoryState{ChangeMeta: model.BufMeta(buf("#eco"))}, "#eco"),
	})
	d.Flush()

	calls, _, _, _ := r.snapshot()
	assertCalls(t, calls, []string{
		"loading s1 #eco true",
		"older s1 #eco [older]",
		"loading s1 #eco false",
	})
}

// The first-DM privacy question is a delivery like any other: the session
// decides when it is due, the window owns the dialog, and it goes through the
// one funnel rather than a call from a goroutine that is not the UI's.
func TestAskDMLoggingIsDelivered(t *testing.T) {
	d, r := newFunnel(t, forever)
	d.Open("s1")

	d.Emit("s1", []Item{Of(AskDMLogging{ChangeMeta: model.BufMeta(buf("carol"))}, "carol")})
	d.Flush()

	calls, _, _, _ := r.snapshot()
	assertCalls(t, calls, []string{"askdm s1 carol"})
}

// A retired session's M9 traffic is dropped with the rest of it. The liveness
// rule lives in exactly one place, and adding methods to the Sink must not add
// a way around it.
func TestNewDeliveriesObeyLiveness(t *testing.T) {
	d, r := newFunnel(t, forever)
	d.Open("s1")
	d.Close("s1")

	d.Emit("s1", []Item{
		Of(HistoryState{ChangeMeta: model.BufMeta(buf("#eco")), Loading: true}, "#eco"),
		Of(model.HistoryLoaded{ChangeMeta: model.BufMeta(buf("#eco")), Direction: model.HistoryBefore}, "#eco", line("older")),
		Of(AskDMLogging{ChangeMeta: model.BufMeta(buf("carol"))}, "carol"),
	})
	d.Flush()

	if calls, _, _, _ := r.snapshot(); len(calls) != 0 {
		t.Errorf("a retired session still reached the screen: %v", calls)
	}
}
