package parity

import "strings"

// hasFlag reports whether a fact carries one of its canonical flags.
func hasFlag(f Fact, name string) bool {
	for _, v := range strings.Split(f.Flags, ",") {
		if v == name {
			return true
		}
	}
	return false
}

// serverStatus reports whether a fact is a status line in the network's status
// buffer — the log where both cores put connection chrome and stray numerics.
func serverStatus(f Fact) bool {
	return f.Kind == kindStatus && f.Buffer == serverBufferName
}

// The expected-divergence list: every place the new core is deliberately not
// v1, with the reason. Nothing else is forgiven — a fact with no counterpart
// and no entry here fails the scenario that produced it.
//
// Rules for this list, which exist so it stays a list of decisions and not a
// list of excuses:
//
//  1. Every entry says WHY in one sentence a reviewer can check.
//  2. Every entry must fire: TestParity fails on one that never explains
//     anything, so the list cannot outlive the differences it describes.
//  3. An entry matches a KIND of fact, or a line one of the cores generates
//     itself — never a scenario's payload — and narrows with Where to the
//     exchange it belongs to, so it cannot absorb the next regression that
//     happens to look similar. Injecting a bug into internal/state and watching
//     the gate go red is the check that this still holds.
var divergences = []Divergence{
	{
		ID: "labeled-echo-resolution",
		Reason: "MUST 3: v2 sends its own messages with a label and resolves the " +
			"pending row in place when the labeled echo arrives, so the row gains the " +
			"server's real msgid and server-time. v1 matched the echo by buffer|text " +
			"and dropped it, which is why two identical messages were indistinguishable " +
			"and its own messages could never be redacted or read-marked.",
		Match: func(s Side, f Fact) bool { return s == SideV2 && f.Kind == kindResolve },
	},
	{
		ID: "reopened-buffer-refetches-history",
		Reason: "v1 keys \"this channel's scrollback is already fetched\" by channel " +
			"name in a session-wide map that a closed buffer never clears, so reopening " +
			"a buffer asks for nothing and comes back empty (the M5 characterization " +
			"test ReopenedBufferInheritsDedupeState pins the bug). v2 keys it on the " +
			"buffer, which went away with the close, so the reopened buffer refetches " +
			"— and the msgid index went with it, so nothing duplicates.",
		Where: func(label string) bool {
			return strings.HasPrefix(label, "reopened_buffer_refetches_history/")
		},
		Match: func(s Side, f Fact) bool {
			return s == SideV2 && f.Kind == kindMsg && hasFlag(f, "playback")
		},
	},
	{
		ID: "history-chunk",
		Reason: "MUST 2: v2 delivers a chathistory batch as one HistoryLoaded at BATCH " +
			"close so the UI prepends a block; v1 had no such event and drew replayed " +
			"lines one at a time. The messages inside the chunk are compared normally.",
		Match: func(s Side, f Fact) bool { return s == SideV2 && f.Kind == kindHistory },
	},
	{
		ID: "derived-unread",
		Reason: "v2 derives unread and highlight counts in the store, from the read " +
			"marker, and reports them as UnreadChanged. In v1 the counting lives in " +
			"ui.go, where no headless test can reach it, and disagrees with the log by " +
			"construction.",
		Match: func(s Side, f Fact) bool { return s == SideV2 && f.Kind == kindUnread },
	},
	{
		ID: "typed-connection-states",
		Reason: "v2's connection is a state machine (connecting, registering, " +
			"authenticating, ready, failed) so the UI can say what is happening and " +
			"autojoin can wait for authentication; v1's StatusChange had one bool and " +
			"only ever reported the two endpoints.",
		Match: func(s Side, f Fact) bool {
			if s != SideV2 || f.Kind != kindConn {
				return false
			}
			return f.Text == "connecting" || f.Text == "registering" ||
				f.Text == "authenticating" || f.Text == "failed"
		},
	},
	{
		ID: "self-identity",
		Reason: "v2 reports our own nick/account/away as SelfChanged, which is what " +
			"makes highlight matching and the composer's identity a fact rather than a " +
			"re-read of the connection object; v1 had no callback for it.",
		Match: func(s Side, f Fact) bool { return s == SideV2 && f.Kind == kindSelf },
	},
	{
		ID: "typed-channel-modes",
		Reason: "v2 reports channel mode state as ModeChanged (flags, key, limit) so " +
			"the UI and the rejoin plan can read it; v1 tracked modes in the Buffer and " +
			"never told anyone they changed.",
		Match: func(s Side, f Fact) bool { return s == SideV2 && f.Kind == kindMode },
	},
	{
		ID: "multiline-assembled-once",
		Reason: "MUST 2: v2 assembles a multiline batch at BATCH close and hands over " +
			"one message; v1 handed over the first part and then amended the row per " +
			"continuation, which is why it needed a second callback (HighlightNote) to " +
			"say a line it had already delivered had become a mention.",
		Match: func(s Side, f Fact) bool { return s == SideV1 && f.Kind == kindHighlight },
	},
	{
		ID: "redaction-tombstone",
		Reason: "v2 replaces a redacted message with a tombstone in the store, so the " +
			"surrounding conversation still reads and the archive can purge the row; v1 " +
			"kept the original text in the Buffer and asked ui.go to drop the widget.",
		Match: func(s Side, f Fact) bool { return s == SideV2 && f.Kind == kindTombstone },
	},
	{
		ID: "cycle-clears-the-roster-visibly",
		Reason: "On the PART of a /cycle both cores empty the member list, but v1 does " +
			"it without firing MembersChange, so the roster on screen keeps showing " +
			"people who are no longer there until the rejoin's NAMES lands. v2 reports " +
			"the emptying as a MembersChanged reset — and then reports the refill, " +
			"which v1 also stays silent about because for v1 the roster never appeared " +
			"to change at all. Both halves of that one bug are matched here, and only " +
			"inside the cycle scenario.",
		Where: func(label string) bool { return strings.HasPrefix(label, "cycle_keeps_the_buffer/") },
		Match: func(s Side, f Fact) bool { return s == SideV2 && f.Kind == kindMembers },
	},
	{
		ID: "v1-connection-chrome",
		Reason: "v1 writes the connection banner into the status buffer as chat lines " +
			"(\"Connecting to host:port ...\", the negotiated capability list). v2 reports " +
			"the same information as typed state — ConnStateChanged and the CapSet — and " +
			"leaves the wording to the UI, which is where new-string lookup (R-17) starts.",
		Match: func(s Side, f Fact) bool {
			return s == SideV1 && serverStatus(f) &&
				(strings.HasPrefix(f.Text, "Connecting to ") ||
					strings.HasPrefix(f.Text, "Active IRCv3 capabilities: "))
		},
	},
	{
		ID: "ready-banner-position",
		Reason: "Both cores log \"Connected! You are <nick>\" at the end of registration, " +
			"one line apart: ircevent prepends its own RPL_ENDOFMOTD handler, so v1's " +
			"banner lands before the last MOTD line, while v2 announces readiness after " +
			"it (which is also where SASL is sequenced, so autojoin cannot fire early).",
		Match: func(s Side, f Fact) bool {
			return serverStatus(f) && strings.HasPrefix(f.Text, "Connected! You are ")
		},
	},
	{
		ID: "inbound-action-delivered",
		Reason: "v1 never showed an incoming /me at all: it enabled the library's CTCP " +
			"support, which rewrites the command to CTCP_ACTION before dispatch, and " +
			"registered no callback for it — making its own ACTION branch in " +
			"handleMessage dead code for inbound messages. v2 decodes CTCP itself " +
			"(blueprint: all wire decoding in internal/irc) and delivers the action. " +
			"This covers an action of our own that arrives over the wire too (an " +
			"echo-message for a /me the user typed on another client): v1 drops it for " +
			"the same reason, it having never reached the ACTION branch. The entry " +
			"forgives one side only, so it cannot hide v2 losing or altering the /me " +
			"the user types HERE — v1's copy would go unmatched and fail the gate; " +
			"TestParity_LocalActionIsNotForgiven pins that.",
		Match: func(s Side, f Fact) bool {
			return s == SideV2 && f.Kind == kindMsg && hasFlag(f, "action")
		},
	},
	{
		ID: "sts-is-connection-config",
		Reason: "An STS policy is configuration for the *next* dial, not something " +
			"that happened in a buffer: v2 parses it into the CapSet and publishes it " +
			"through Client.STS(), for the connect path to read before it opens a " +
			"socket. v1 pushed it at the UI as a callback, which then had to route it " +
			"back down to config.STSRecord. See TestParity_STSPolicyReachesTheClient.",
		Match: func(s Side, f Fact) bool { return s == SideV1 && f.Kind == kindSTS },
	},
	{
		ID: "rail-repaint-on-rename",
		Reason: "v1's BufferList is an unnamed 'repaint the whole rail' ping, so a " +
			"rename fires it on top of RenameBuffer; v2 says what moved and where with " +
			"BufferRenamed alone, which is what lets the UI migrate the draft and the " +
			"scroll anchor instead of rebuilding the list. The buffer set itself is " +
			"compared in the state snapshot.",
		Where: func(label string) bool { return strings.Contains(label, " RENAME ") },
		Match: func(s Side, f Fact) bool { return s == SideV1 && f.Kind == kindRail },
	},
}
