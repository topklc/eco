package parity

import (
	"strings"
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/config"
	"github.com/toprakkilic/eco/internal/model"
)

// TestParity is the gate. Every scenario is replayed against both cores and
// their output is compared; a disagreement the divergence list does not cover
// fails the scenario with a diff that says which side produced what.
//
// The subtests are deliberately not parallel: both cores read the process-wide
// settings snapshot, and v1's ignore list is a package global.
func TestParity(t *testing.T) {
	pinSettings(t)
	all := scenarios()
	ran := 0
	for _, sc := range all {
		t.Run(sc.name, func(t *testing.T) {
			ran++
			v1 := runCore(t, newV1Core(t, sc.setup), sc.steps)
			v2 := runCore(t, newV2Core(t, sc.setup), sc.steps)
			// A transcript that produces nothing compares equal to another
			// transcript that produces nothing, so every scenario has to be
			// shown to do something before its green means anything.
			if n := factsAfterRegistration(v1); n == 0 {
				t.Fatalf("the v1 run produced no facts outside registration: the "+
					"transcript for %q does not exercise anything", sc.name)
			}
			if ms := compare(sc.name, v1, v2); len(ms) > 0 {
				t.Fatal("\n" + report(sc.name, ms))
			}
		})
	}
	if ran != len(all) {
		return // a -run filter skipped scenarios; the counts below would lie
	}

	// The divergence list has to stay honest. An entry that never explains
	// anything is either a difference that has been fixed — in which case
	// deleting it makes the gate stricter — or a rule broad enough to swallow a
	// future regression while looking harmless. Both are worth failing for.
	for _, d := range divergences {
		if firedCount(d.ID) == 0 {
			t.Errorf("divergence %q never fired: no scenario produces the difference it "+
				"describes, so it is either already fixed (delete the entry) or untested "+
				"(add a scenario that exercises it)", d.ID)
		}
	}
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

// TestParity_LabeledEchoCarriesTheServerMsgID proves the claim the
// "labeled-echo-resolution" divergence entry makes, which the diff itself
// cannot: the resolve fact it forgives exists because v2's own messages end up
// with the server's msgid, and v1's never do. That is what makes an own message
// redactable and read-markable at all, and it is the reason the M5 suite marks
// its echo tests CHARACTERIZATION.
func TestParity_LabeledEchoCarriesTheServerMsgID(t *testing.T) {
	pinSettings(t)
	steps := concat(
		joined("#eco", "topic", "@alice eco"),
		[]step{
			act(say("#eco", "hello there")),
			wait("*PRIVMSG #eco*hello there"),
			send("@%LABEL%;msgid=e1 :eco!eco@test PRIVMSG #eco :hello there"),
		},
	)

	v1 := newV1Core(t, nil).(*v1Core)
	runCore(t, v1, steps)
	for _, l := range v1.sess.Buffer("#eco").Lines {
		if l.Self && l.MsgID != "" {
			t.Fatalf("v1 suddenly gives its own messages a msgid (%q); the divergence "+
				"entry is stale", l.MsgID)
		}
	}

	v2 := newV2Core(t, nil).(*v2Core)
	runCore(t, v2, steps)
	found := false
	for _, m := range v2.store.Messages(v2.store.BufferID(parityNet, "#eco")) {
		if !m.Sender.Self {
			continue
		}
		found = true
		if m.MsgID != "e1" {
			t.Errorf("v2's own message has msgid %q, want the server's %q", m.MsgID, "e1")
		}
		if m.Pending != model.Delivered {
			t.Errorf("v2's own message is still %v after its labeled echo", m.Pending)
		}
	}
	if !found {
		t.Fatal("v2 recorded no message of our own at all")
	}
}

// TestParity_STSPolicyReachesTheClient proves the other half of the
// "sts-is-connection-config" entry: v2 produces no Change for an sts policy
// because it holds it as connection configuration, not because it lost it.
// M7's connect path has to read it from here.
func TestParity_STSPolicyReachesTheClient(t *testing.T) {
	pinSettings(t)
	var sc scenario
	for _, s := range scenarios() {
		if s.name == "sts_policy" {
			sc = s
		}
	}
	if sc.setup == nil {
		t.Fatal("the sts_policy scenario has gone missing")
	}

	v2 := newV2Core(t, sc.setup).(*v2Core)
	runCore(t, v2, sc.steps)
	policy, ok, err := v2.client.STS()
	if err != nil || !ok {
		t.Fatalf("v2 client STS() = (%+v, %v, %v), want the advertised policy", policy, ok, err)
	}
	if policy.Port != 6697 || policy.Duration != 2592000*time.Second {
		t.Errorf("STS policy = port %d, duration %v; want port 6697, 2592000s",
			policy.Port, policy.Duration)
	}
}

// TestParity_LocalActionIsNotForgiven guards the one entry broad enough to be
// worth worrying about. "inbound-action-delivered" forgives any v2 action
// message with no counterpart, which is right for an action off the wire (v1
// shows none) but must not extend to the /me the user types here, which both
// cores do show. It cannot, because the entry names SideV2: drop v2's local
// action and v1's copy is the one left unmatched. This asserts that directly,
// so nobody widens the entry to both sides without the gate saying so.
func TestParity_LocalActionIsNotForgiven(t *testing.T) {
	local := Fact{Kind: kindMsg, Buffer: "#eco", Nick: nick, Text: "waves",
		Flags: flags("action", "self")}
	if id := explain("echo_and_own_message/state: log of #eco", SideV1, local); id != "" {
		t.Fatalf("a v1-only local /me is forgiven by %q; the divergence list has been "+
			"widened to both sides and no longer catches v2 dropping the user's own action", id)
	}
	if id := explain("echo_and_own_message/state: log of #eco", SideV2, local); id == "" {
		t.Fatal("the inbound-action entry no longer matches a v2 action; if that is " +
			"deliberate the entry and this test should go together")
	}
}

// TestParity_ReportsADisagreement is the gate's own gate: it proves the differ
// fails, and fails readably, when the cores disagree about something no entry
// covers. Without it a harness that silently matched everything would look
// exactly like a harness that passed.
func TestParity_ReportsADisagreement(t *testing.T) {
	a := coreRun{
		groups: []group{{label: "step", facts: sortFacts([]Fact{
			{Kind: kindMsg, Buffer: "#eco", Nick: "alice", Text: "hello"},
			{Kind: kindStatus, Buffer: "#eco", Text: "alice joined"},
		})}},
		state: []bufferSnap{{Name: "#eco", Topic: "old", Members: []string{"alice"}}},
	}
	b := coreRun{
		groups: []group{{label: "step", facts: sortFacts([]Fact{
			{Kind: kindMsg, Buffer: "#eco", Nick: "alice", Text: "hello", Flags: "highlight"},
			{Kind: kindStatus, Buffer: "#eco", Text: "alice joined"},
		})}},
		state: []bufferSnap{{Name: "#eco", Topic: "new", Members: []string{"alice"}}},
	}
	ms := compare("synthetic", a, b)
	if len(ms) != 2 {
		t.Fatalf("compare found %d mismatches, want 2 (one stream, one state): %v", len(ms), ms)
	}
	out := report("synthetic", ms)
	for _, want := range []string{
		`scenario "synthetic"`,
		`stream step`,
		`v1 only: msg #eco <alice> "hello"`,
		`v2 only: msg #eco <alice> [highlight] "hello"`,
		`state: buffer #eco`,
		`topic="old"`,
		`topic="new"`,
		"add an entry to divergences",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the report does not mention %q; a diff nobody can read is not a gate:\n%s", want, out)
		}
	}
}

// TestParity_EqualRunsPass is the other half: identical runs must produce no
// mismatches at all, so a green scenario means "the same", not "the differ
// found nothing to say".
func TestParity_EqualRunsPass(t *testing.T) {
	run := coreRun{
		groups: []group{{label: "step", facts: sortFacts([]Fact{
			{Kind: kindMsg, Buffer: "#eco", Nick: "alice", Text: "hello"},
		})}},
		state: []bufferSnap{{Name: "#eco", Lines: []Fact{
			{Kind: kindMsg, Buffer: "#eco", Nick: "alice", Text: "hello"},
		}}},
	}
	if ms := compare("identical", run, run); len(ms) > 0 {
		t.Fatalf("identical runs reported %d mismatches:\n%s", len(ms), report("identical", ms))
	}
}

// pinSettings makes the process-wide settings snapshot deterministic for both
// cores: no channel scan on connect, no auto-join storm, no hidden join/part
// lines and unbounded scrollback (the v2 store is configured to match).
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
