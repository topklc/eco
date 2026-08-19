package state

import (
	"testing"

	"github.com/toprakkilic/eco/internal/model"
)

// The notification pipeline as a table - blueprint §4.4's evaluation order,
// every combination of its four inputs, in one place.
//
// The order is: the buffer's level (resolved against the network's default),
// then whether the message matches a highlight, then suppression when the
// buffer is the one being read. Playback is not a fourth step but a rule that
// cuts across all three: a reconnect replaying a mention must not raise it
// again, and neither must a status line - which is the audit's severe #2 and
// v1's most complained-about bug.
//
// Every row here is a sentence someone could argue with, which is why they are
// spelled out rather than computed: a table that derives its expectation from
// the same rule it is testing tests nothing.
func TestNotificationPipeline(t *testing.T) {
	const (
		mention = true
		plain   = false
		focused = true
		away    = false
		replay  = true
		live    = false
	)
	for _, tc := range []struct {
		name     string
		level    NotifyLevel
		netLevel NotifyLevel
		mention  bool
		focused  bool
		playback bool
		want     bool
	}{
		// --- the default level: mentions, inherited from the network ---
		{"default level, a mention, not looking", NotifyDefault, NotifyDefault, mention, away, live, true},
		{"default level, plain chat", NotifyDefault, NotifyDefault, plain, away, live, false},
		{"default level, a mention in the buffer on screen", NotifyDefault, NotifyDefault, mention, focused, live, false},

		// --- All: every message that counts as unread ---
		{"all, plain chat", NotifyAll, NotifyDefault, plain, away, live, true},
		{"all, a mention", NotifyAll, NotifyDefault, mention, away, live, true},
		{"all, but the buffer is on screen", NotifyAll, NotifyDefault, plain, focused, live, false},
		{"all, and a mention in the buffer on screen", NotifyAll, NotifyDefault, mention, focused, live, false},

		// --- Mentions: the explicit form of the default ---
		{"mentions, a mention", NotifyMentions, NotifyDefault, mention, away, live, true},
		{"mentions, plain chat", NotifyMentions, NotifyDefault, plain, away, live, false},

		// --- Mute: nothing, ever, however loud ---
		{"mute, a mention", NotifyMute, NotifyDefault, mention, away, live, false},
		{"mute, plain chat", NotifyMute, NotifyDefault, plain, away, live, false},
		{"mute beats the network's all", NotifyMute, NotifyAll, mention, away, live, false},

		// --- the network default, and the buffer overriding it ---
		{"the network says all", NotifyDefault, NotifyAll, plain, away, live, true},
		{"the network says mute", NotifyDefault, NotifyMute, mention, away, live, false},
		{"the buffer overrides a muted network", NotifyAll, NotifyMute, plain, away, live, true},
		{"the buffer overrides a loud network", NotifyMentions, NotifyAll, plain, away, live, false},

		// --- playback: never, at any level, focused or not ---
		{"playback of a mention", NotifyDefault, NotifyDefault, mention, away, replay, false},
		{"playback at the all level", NotifyAll, NotifyDefault, plain, away, replay, false},
		{"playback of a mention while muted", NotifyMute, NotifyDefault, mention, away, replay, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			ch := h.joinChannel("#eco", "alice")
			h.s.SetNetworkNotifyLevel(h.n, tc.netLevel)
			h.s.SetNotifyLevel(ch, tc.level)
			if tc.focused {
				h.s.Focus(ch)
			} else {
				h.s.Blur(h.n)
			}
			meta := h.meta()
			if tc.playback {
				meta = h.playback()
			}
			text := "the build is green"
			if tc.mention {
				text = testNick + ": the build is green"
			}
			added := mustFind[model.MessageAdded](t, h.apply(model.PrivMsg{
				EventMeta: meta, Target: "#eco", Sender: user("alice"), Text: text,
			}))
			if added.Notify != tc.want {
				t.Errorf("Notify = %v, want %v (level %v, network %v, mention %v, focused %v, playback %v)",
					added.Notify, tc.want, tc.level, tc.netLevel, tc.mention, tc.focused, tc.playback)
			}
		})
	}
}

// A private message is a highlight by definition and a service NOTICE is not -
// the rule the level is applied ON TOP of. Muting a conversation therefore
// silences it, and turning a channel up to All does not make NickServ pop a
// toast.
func TestNotificationPipelineQueriesAndNotices(t *testing.T) {
	h := newHarness(t)
	h.s.Blur(h.n)

	added := mustFind[model.MessageAdded](t, h.apply(model.PrivMsg{
		EventMeta: h.meta(), Target: testNick, Sender: user("alice"), Text: "you around?",
	}))
	if !added.Notify {
		t.Error("a private message did not notify")
	}
	h.s.SetNotifyLevel(h.id("alice"), NotifyMute)
	added = mustFind[model.MessageAdded](t, h.apply(model.PrivMsg{
		EventMeta: h.meta(), Target: testNick, Sender: user("alice"), Text: "still there?",
	}))
	if added.Notify {
		t.Error("a muted conversation notified")
	}

	// A NOTICE from a service is not a highlight, so raising the level to All
	// is the only thing that could make it notify - and that is the level
	// saying so, not the query rule.
	notice := mustFind[model.MessageAdded](t, h.apply(model.Notice{
		EventMeta: h.meta(), Target: testNick, Sender: user("NickServ"), Text: "you are now identified",
	}))
	if notice.Notify {
		t.Error("a service NOTICE notified at the default level")
	}
}

// Our own messages never notify us, at any level. It is the same rule that
// keeps them out of the unread count (countsUnread), stated here because a
// level of All is exactly where a careless implementation would let them
// through.
func TestNotificationPipelineOurOwnMessages(t *testing.T) {
	h := newHarness(t)
	ch := h.joinChannel("#eco", "alice")
	h.s.SetNotifyLevel(ch, NotifyAll)
	h.s.Blur(h.n)

	added := mustFind[model.MessageAdded](t, h.apply(model.PrivMsg{
		EventMeta: h.meta(), Target: "#eco", Sender: selfUser(), Text: "on it",
	}))
	if added.Notify {
		t.Error("our own message notified us")
	}
}
