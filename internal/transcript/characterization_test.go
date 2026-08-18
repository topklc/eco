package transcript

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/config"
	"github.com/toprakkilic/eco/internal/model"
)

// TestTranscripts is the suite: every scenario is replayed against the core and
// compared, fact for fact, with the transcript recorded under testdata.
//
// The subtests are deliberately not parallel: the core reads the process-wide
// settings snapshot that pinSettings publishes.
func TestTranscripts(t *testing.T) {
	pinSettings(t)
	for _, sc := range scenarios() {
		t.Run(sc.name, func(t *testing.T) {
			_, run := runScenario(t, sc)
			// A transcript that produces nothing compares equal to a record of
			// nothing, so every scenario has to be shown to do something before
			// its green means anything.
			if n := factsAfterRegistration(run); n == 0 {
				t.Fatalf("the run produced no facts outside registration: the "+
					"transcript for %q does not exercise anything", sc.name)
			}
			checkRecord(t, sc.name, run)
		})
	}
}

// TestEveryRecordHasAScenario is the anti-rot rule, and the reason a scenario
// cannot quietly go missing: a record with no scenario left to produce it means
// a case that used to be defended no longer is. Deleting one is allowed — it
// just has to be done on purpose, with both halves.
func TestEveryRecordHasAScenario(t *testing.T) {
	have := map[string]bool{}
	for _, sc := range scenarios() {
		// Two scenarios under one name would share one record, and the second
		// would overwrite the first at the next -update: a scenario lost in the
		// only way this test could otherwise miss.
		if have[sc.name] {
			t.Errorf("two scenarios are named %q; they would share one record", sc.name)
		}
		have[sc.name] = true
	}
	files, err := filepath.Glob(filepath.Join("testdata", "*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no records in testdata: the suite would pass by recording nothing")
	}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".txt")
		if !have[name] {
			t.Errorf("%s records a scenario that no longer exists. A case that was "+
				"defended is not defended any more: restore the scenario, or delete "+
				"the record deliberately and say why.", f)
		}
	}
	if len(files) != len(have) {
		t.Errorf("%d records for %d scenarios: a scenario with no record is one "+
			"nobody is checking", len(files), len(have))
	}
}

// runScenario drives one scenario and hands back both the core and its run, for
// the anchors below that assert on the core's own state.
func runScenario(t *testing.T, sc scenario) (*core, coreRun) {
	t.Helper()
	c := newCore(t, sc.setup)
	return c, runCore(t, c, sc.steps)
}

// factsAfterRegistration counts what a run produced once it was connected.
func factsAfterRegistration(r coreRun) int {
	n := 0
	for _, g := range r.groups {
		if g.label == "registration" {
			continue
		}
		n += len(g.facts)
	}
	return n
}

// pinSettings makes the process-wide settings snapshot deterministic: no channel
// scan on connect, no auto-join storm, no hidden join/part lines and unbounded
// scrollback (the store is configured to match).
func pinSettings(t *testing.T) {
	t.Helper()
	prev := config.Current()
	next := *prev
	next.AutoScanChannels = false
	next.JoinAllChannels = false
	next.HideJoinParts = false
	next.ScrollbackCap = 0
	config.Publish(&next)
	t.Cleanup(func() { config.Publish(prev) })
}

// scenarioNamed returns one scenario by name, failing if it has been renamed
// out from under a test that depends on what it exercises.
func scenarioNamed(t *testing.T, name string) scenario {
	t.Helper()
	for _, sc := range scenarios() {
		if sc.name == name {
			return sc
		}
	}
	t.Fatalf("the %q scenario has gone missing", name)
	return scenario{}
}

// The anchors. Everything below asserts a behaviour directly, not through the
// recorded transcript — so a careless `-update` cannot bless it away. They are
// the claims the dual-core gate made in its divergence list and its own
// self-tests, kept as executable statements now that the reference core is gone.

// TestRecordedBehaviour_LabeledEchoCarriesTheServerMsgID pins MUST 3, the change
// the whole cutover rests on: our own messages are pending rows resolved in
// place by the labelled echo, so they end up with the server's msgid — which is
// what makes them redactable and read-markable at all. v1 matched the echo by
// buffer|text and its own messages never had a msgid.
func TestRecordedBehaviour_LabeledEchoCarriesTheServerMsgID(t *testing.T) {
	pinSettings(t)
	c, _ := runScenario(t, scenarioNamed(t, "echo_and_own_message"))
	found := 0
	for _, m := range c.store.Messages(c.store.BufferID(transcriptNet, "#eco")) {
		if !m.Sender.Self || m.Kind != model.MsgPrivmsg {
			continue
		}
		found++
		if m.MsgID == "" {
			t.Errorf("our own message %q has no msgid: the labelled echo did not "+
				"resolve it, and it cannot be redacted or read-marked", m.Text)
		}
		if m.Pending != model.Delivered {
			t.Errorf("our own message %q is still %v after its echo", m.Text, m.Pending)
		}
	}
	if found == 0 {
		t.Fatal("the core recorded no message of our own at all")
	}
}

// TestRecordedBehaviour_STSPolicyReachesTheClient pins the other side of the sts
// event: it produces no Change because it is configuration for the next dial,
// held on the CapSet — not because it was lost. internal/app's connect path
// reads it from here.
func TestRecordedBehaviour_STSPolicyReachesTheClient(t *testing.T) {
	pinSettings(t)
	c, _ := runScenario(t, scenarioNamed(t, "sts_policy"))
	policy, ok, err := c.client.STS()
	if err != nil || !ok {
		t.Fatalf("client STS() = (%+v, %v, %v), want the advertised policy", policy, ok, err)
	}
	if policy.Port != 6697 || policy.Duration != 2592000*time.Second {
		t.Errorf("STS policy = port %d, duration %v; want port 6697, 2592000s",
			policy.Port, policy.Duration)
	}
}

// TestRecordedBehaviour_TheUsersOwnActionIsDelivered replaces the dual-core
// gate's TestParity_LocalActionIsNotForgiven. That test existed because the
// "inbound-action-delivered" divergence entry — v1 never showed an incoming /me
// at all — was broad enough to also swallow the /me the user types HERE. The
// entry is gone with the reference core; this keeps the thing it protected.
func TestRecordedBehaviour_TheUsersOwnActionIsDelivered(t *testing.T) {
	pinSettings(t)
	_, run := runScenario(t, scenarioNamed(t, "echo_and_own_message"))
	want := Fact{Kind: kindMsg, Buffer: "#eco", Nick: nick, Text: "waves",
		Flags: flags("action", "self")}
	if !hasFact(run, want) {
		t.Fatalf("the /me the user typed here never reached a buffer: no %s in the run", want)
	}
}

// TestRecordedBehaviour_AMembersFactCarriesTheRoster is the mutation sweep's
// hardest-won lesson, kept as an assertion. A roster fact that says only
// "something changed" is fungible: two of them cancel in any diff, so a core
// that quietly stopped reporting who joined reads as agreement. Every members
// fact must name the room.
func TestRecordedBehaviour_AMembersFactCarriesTheRoster(t *testing.T) {
	pinSettings(t)
	_, run := runScenario(t, scenarioNamed(t, "other_user_join_part_and_quit"))
	var rosters []string
	for _, g := range run.groups {
		for _, f := range g.facts {
			if f.Kind == kindMembers && f.Buffer == "#eco" {
				rosters = append(rosters, f.Text)
			}
		}
	}
	// dave arrives, bob leaves, alice quits: three rosters after the join burst,
	// each naming a different room.
	want := []string{
		"alice(@),bob(+),dave,eco",
		"alice(@),dave,eco",
		"dave,eco",
	}
	for _, w := range want {
		if !containsString(rosters, w) {
			t.Errorf("no members fact carries the roster %q; got %v", w, rosters)
		}
	}
}

// TestRecordedBehaviour_AHistoryChunkKeepsItsOrder is the M7-verification
// anchor. A chathistory batch arrives as ONE HistoryLoaded carrying a slice of
// messages (the `history-chunk` divergence), and every consumer — internal/app's
// adapter today, M8's Dispatcher tomorrow — appends that slice in slice order.
// The slice IS the scrollback.
//
// It needs an anchor of its own because neither half of the recorded comparison
// can see it. sortFacts orders each step's facts canonically, on the reasoning
// that the order two facts about the same step come out in is not behaviour —
// true for a roster and a status line, false for the messages inside one chunk.
// And the buffer snapshot is the store's own log, which is appended by the
// ordinary reducers and so stays right even when the chunk handed to the UI is
// backwards. Reversing the chunk (one `append` turned into a prepend) left this
// suite, internal/state and internal/app all green.
func TestRecordedBehaviour_AHistoryChunkKeepsItsOrder(t *testing.T) {
	pinSettings(t)
	c, _ := runScenario(t, scenarioNamed(t, "playback_batch"))
	if len(c.history) == 0 {
		t.Fatal("the playback scenario produced no history chunk at all")
	}
	want := []string{"old one", "old two mentioning eco"}
	var got []string
	for _, m := range c.history[0].Messages {
		if m.Kind == model.MsgPrivmsg {
			got = append(got, m.Text)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("the first chunk carried %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("the chunk reached the UI as %q, want %q — a scrollback that reads backwards", got, want)
		}
	}
}

// TestRecordedBehaviour_ReportsADisagreement is the suite's own gate: it proves
// a changed record fails, and fails readably. Without it, a comparison that
// silently matched everything would look exactly like a passing suite.
func TestRecordedBehaviour_ReportsADisagreement(t *testing.T) {
	before := record("synthetic", coreRun{
		groups: []group{{label: "0 S: PRIVMSG", facts: sortFacts([]Fact{
			{Kind: kindMsg, Buffer: "#eco", Nick: "alice", Text: "hello"},
			{Kind: kindStatus, Buffer: "#eco", Text: "alice joined"},
		})}},
		state: []bufferSnap{{Name: "#eco", Topic: "old", Members: []string{"alice"}}},
	})
	after := record("synthetic", coreRun{
		groups: []group{{label: "0 S: PRIVMSG", facts: sortFacts([]Fact{
			{Kind: kindMsg, Buffer: "#eco", Nick: "alice", Text: "hello", Flags: "highlight"},
			{Kind: kindStatus, Buffer: "#eco", Text: "alice joined"},
		})}},
		state: []bufferSnap{{Name: "#eco", Topic: "new", Members: []string{"alice"}}},
	})
	if before == after {
		t.Fatal("two different runs rendered identically: the record is not recording " +
			"the flags or the topic, and no change to either would ever fail")
	}
	out := diffRecords("synthetic", before, after)
	for _, want := range []string{
		`scenario "synthetic"`,
		`recorded`,
		`msg #eco <alice> "hello"`,
		`msg #eco <alice> [highlight] "hello"`,
		`topic="old"`,
		`topic="new"`,
		"-update",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the report does not mention %q; a diff nobody can read is not a "+
				"gate:\n%s", want, out)
		}
	}
}

// TestRecordedBehaviour_IdenticalRunsMatch is the other half: the same run twice
// must render identically, or a green suite would only mean the comparison found
// nothing to say. It is also the determinism check — map iteration, goroutine
// interleaving and the fixed clock all have to land in the same place twice.
func TestRecordedBehaviour_IdenticalRunsMatch(t *testing.T) {
	pinSettings(t)
	sc := scenarioNamed(t, "registration_join_message")
	_, first := runScenario(t, sc)
	_, second := runScenario(t, sc)
	a, b := record(sc.name, first), record(sc.name, second)
	if a != b {
		t.Fatal("\n" + diffRecords(sc.name, a, b))
	}
}

// hasFact reports whether a run's stream contains this fact.
func hasFact(run coreRun, want Fact) bool {
	for _, g := range run.groups {
		for _, f := range g.facts {
			if f == want {
				return true
			}
		}
	}
	return false
}

func containsString(hay []string, needle string) bool {
	for _, v := range hay {
		if v == needle {
			return true
		}
	}
	return false
}
