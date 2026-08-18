package parity

import "github.com/toprakkilic/eco/internal/testsrv"

// The scenarios. Each one is the wire traffic of a case the M5 regression suite
// already encodes against v1, replayed here against both cores. They are
// transcripts, not assertions: what they must produce is "the same thing the
// other core produced", which is the only assertion this package makes.

type scenario struct {
	name  string
	steps []step
	// setup, when set, configures the fake server before either core dials —
	// the capability set it advertises, the ISUPPORT tokens it sends. It runs
	// identically for both cores, so it is part of the transcript.
	setup func(*testsrv.Server)
}

// joinBurst is the server side of a successful JOIN: the echo, the topic, the
// member list and its terminator, all in one group because they are one
// exchange.
func joinBurst(ch, topic, names string) step {
	return send(
		":eco!eco@test JOIN "+ch,
		":irc.test 332 eco "+ch+" :"+topic,
		":irc.test 353 eco = "+ch+" :"+names,
		":irc.test 366 eco "+ch+" :End of /NAMES list.",
	)
}

// joined is the whole user-initiated join: ask, watch it go out, answer it, and
// consume the two requests joining provokes.
func joined(ch, topic, names string) []step {
	return []step{
		act(join(ch)),
		wait("*JOIN " + ch),
		joinBurst(ch, topic, names),
		wait("*MODE " + ch),
		wait("*CHATHISTORY LATEST " + ch + "*"),
	}
}

func scenarios() []scenario {
	return []scenario{
		{
			name: "registration_join_message",
			steps: concat(
				joined("#eco", "a channel for testing", "@alice eco +bob"),
				[]step{
					send("@msgid=m1 :alice!alice@test PRIVMSG #eco :hello eco, welcome"),
					// the same msgid again must not duplicate the message
					send("@msgid=m1 :alice!alice@test PRIVMSG #eco :hello eco, welcome"),
					send(":bob!bob@test PRIVMSG #eco :a line with no msgid"),
					send("@msgid=n1 :alice!alice@test NOTICE #eco :a notice about economy"),
					send("@msgid=a1 :alice!alice@test PRIVMSG #eco :\x01ACTION waves at eco\x01"),
					send("@msgid=p1 :carol!carol@test PRIVMSG eco :private hello"),
					// the same query, differently cased: one buffer, not two
					send("@msgid=p2 :CAROL!carol@test PRIVMSG eco :and again"),
					// a channel target the server re-cased: still #eco
					send("@msgid=m5 :alice!alice@test PRIVMSG #ECO :cased target"),
					send("@msgid=s1 :irc.test NOTICE eco :a server notice"),
				},
			),
		},
		{
			name: "echo_and_own_message",
			steps: concat(
				joined("#eco", "topic", "@alice eco"),
				[]step{
					act(say("#eco", "hello there")),
					wait("*PRIVMSG #eco*hello there"),
					send("@%LABEL%;msgid=e1 :eco!eco@test PRIVMSG #eco :hello there"),
					// the same text twice: v1 cannot tell the two echoes apart
					act(say("#eco", "again")),
					wait("*PRIVMSG #eco*again"),
					send("@%LABEL%;msgid=e2 :eco!eco@test PRIVMSG #eco :again"),
					act(me("#eco", "waves")),
					wait("*PRIVMSG #eco*ACTION waves*"),
					send("@%LABEL%;msgid=e3 :eco!eco@test PRIVMSG #eco :\x01ACTION waves\x01"),
				},
			),
		},
		{
			name: "part_echo_and_refusal",
			steps: concat(
				joined("#eco", "topic", "@alice eco"),
				[]step{
					act(part("#eco")),
					wait("*PART #eco"),
					send(":eco!eco@test PART #eco"),
				},
				joined("#dev", "dev topic", "@alice eco"),
				[]step{
					// the server thinks we are not there: the refusal has to
					// close the buffer or it stays open forever
					act(part("#dev")),
					wait("*PART #dev"),
					send("@%LABEL% :irc.test 442 eco #dev :You're not on that channel"),
				},
			),
		},
		{
			// Closing a query the user is done with, and then hearing from the
			// same person again: the buffer comes back, empty.
			name: "close_query_buffer",
			steps: []step{
				send("@msgid=q1 :carol!carol@test PRIVMSG eco :are you there"),
				act(closeBuffer("carol")),
				send("@msgid=q2 :carol!carol@test PRIVMSG eco :still there"),
			},
		},
		{
			name: "other_user_part_and_quit",
			steps: concat(
				joined("#eco", "topic", "@alice eco +bob"),
				[]step{
					send(":bob!bob@test PART #eco :later"),
					send(":alice!alice@test QUIT :Ping timeout"),
				},
			),
		},
		{
			name: "self_kick",
			steps: concat(
				joined("#eco", "topic", "@alice eco"),
				[]step{
					send(":alice!alice@test KICK #eco eco :spam"),
				},
			),
		},
		{
			name: "kick_of_another",
			steps: concat(
				joined("#eco", "topic", "@alice eco +bob"),
				[]step{
					send(":alice!alice@test KICK #eco bob :spam"),
				},
			),
		},
		{
			name: "cycle_keeps_the_buffer",
			steps: concat(
				joined("#eco", "topic", "@alice eco"),
				[]step{
					send("@msgid=c1 :alice!alice@test PRIVMSG #eco :before the cycle"),
					act(cycle("#eco")),
					wait("*PART #eco"),
					wait("*JOIN #eco"),
					send(":eco!eco@test PART #eco"),
					send(
						":eco!eco@test JOIN #eco",
						":irc.test 353 eco = #eco :@alice eco",
						":irc.test 366 eco #eco :End of /NAMES list.",
					),
					wait("*MODE #eco"),
				},
			),
		},
		{
			name: "reconnect_rejoins",
			steps: concat(
				joined("#eco", "topic", "@alice eco"),
				[]step{
					send("@msgid=r1 :alice!alice@test PRIVMSG #eco :before the drop"),
					drop(),
					wait("*JOIN #eco"),
					joinBurst("#eco", "topic", "@alice eco"),
					wait("*MODE #eco"),
					wait("*CHATHISTORY LATEST #eco*"),
					// the replayed message must not duplicate
					send("@msgid=r1 :alice!alice@test PRIVMSG #eco :before the drop"),
				},
			),
		},
		{
			name: "playback_batch",
			steps: concat(
				joined("#eco", "topic", "@alice eco"),
				[]step{
					send(
						"@%LABEL% :irc.test BATCH +hist chathistory #eco",
						"@batch=hist;msgid=h1;time=2024-03-01T11:00:00.000Z :alice!alice@test PRIVMSG #eco :old one",
						"@batch=hist;msgid=h2;time=2024-03-01T11:01:00.000Z :bob!bob@test PRIVMSG #eco :old two mentioning eco",
						"@batch=hist;msgid=h3;time=2024-03-01T11:02:00.000Z :dave!dave@test JOIN #eco",
						// our own replayed JOIN: the storm that must touch neither
						// the member list nor the history request (QA #1)
						"@batch=hist;msgid=h4;time=2024-03-01T11:03:00.000Z :eco!eco@test JOIN #eco",
						":irc.test BATCH -hist",
					),
					// the same history again must not duplicate anything
					send(
						":irc.test BATCH +hist2 chathistory #eco",
						"@batch=hist2;msgid=h1;time=2024-03-01T11:00:00.000Z :alice!alice@test PRIVMSG #eco :old one",
						":irc.test BATCH -hist2",
					),
				},
			),
		},
		{
			name: "multiline_merge",
			steps: concat(
				joined("#eco", "topic", "@alice eco"),
				[]step{
					send(
						"@msgid=ml1 :irc.test BATCH +ml draft/multiline #eco",
						"@batch=ml :alice!alice@test PRIVMSG #eco :first part",
						"@batch=ml :alice!alice@test PRIVMSG #eco :second part mentions eco",
						"@batch=ml;draft/multiline-concat :alice!alice@test PRIVMSG #eco :!",
						":irc.test BATCH -ml",
					),
				},
			),
		},
		{
			// A multiline batch nested inside a chathistory batch: the case where
			// v1's "does this line have a batch tag" test and its two-minute
			// wall-clock recency window meet, and v2 walks the batch chain.
			name: "multiline_inside_playback",
			steps: concat(
				joined("#eco", "topic", "@alice eco"),
				[]step{
					send(
						":irc.test BATCH +h chathistory #eco",
						"@batch=h;msgid=mm1;time=2024-03-01T11:00:00.000Z :irc.test BATCH +m draft/multiline #eco",
						"@batch=m;time=2024-03-01T11:00:00.000Z :alice!alice@test PRIVMSG #eco :replayed one",
						"@batch=m;time=2024-03-01T11:00:00.000Z :alice!alice@test PRIVMSG #eco :replayed two",
						"@batch=h :irc.test BATCH -m",
						":irc.test BATCH -h",
					),
				},
			),
		},
		{
			name: "redact",
			steps: concat(
				joined("#eco", "topic", "@alice eco"),
				[]step{
					send("@msgid=x1 :alice!alice@test PRIVMSG #eco :something regrettable"),
					send("@msgid=x2 :alice!alice@test PRIVMSG #eco :and something fine"),
					send(":alice!alice@test REDACT #eco x1 :spam"),
				},
			),
		},
		{
			name: "mode_deltas",
			steps: concat(
				joined("#eco", "topic", "@alice eco +bob"),
				[]step{
					send(":alice!alice@test MODE #eco +ik hunter2"),
					replyTo("NAMES #eco",
						":irc.test 353 eco = #eco :@alice eco +bob",
						":irc.test 366 eco #eco :End of /NAMES list."),
					send(":alice!alice@test MODE #eco -k hunter2"),
					replyTo("NAMES #eco",
						":irc.test 353 eco = #eco :@alice eco +bob",
						":irc.test 366 eco #eco :End of /NAMES list."),
					send(":alice!alice@test MODE #eco +o bob"),
					replyTo("NAMES #eco",
						":irc.test 353 eco = #eco :@alice eco @+bob",
						":irc.test 366 eco #eco :End of /NAMES list."),
					send(":irc.test 324 eco #eco +int 30"),
				},
			),
		},
		{
			name: "channel_rename",
			steps: concat(
				joined("#eco", "topic", "@alice eco"),
				[]step{
					send("@msgid=b1 :alice!alice@test PRIVMSG #eco :before the rename"),
					send(":irc.test RENAME #eco #eco-new"),
					send("@msgid=b2 :alice!alice@test PRIVMSG #eco-new :after the rename"),
				},
			),
		},
		{
			name: "join_refusals",
			steps: []step{
				// a join the user asked for: the UI must be able to prompt
				act(join("#secret")),
				wait("*JOIN #secret"),
				send("@%LABEL% :irc.test 475 eco #secret :Cannot join channel (+k)"),
				// a refusal for a channel nobody asked for stays in the log
				send(":irc.test 473 eco #invite-only :Cannot join channel (+i)"),
			},
		},
		{
			name: "ignore",
			steps: concat(
				joined("#eco", "topic", "@alice eco"),
				[]step{
					act(ignoreNick("spammer")),
					send("@msgid=i1 :spammer!spammer@test PRIVMSG #eco :buy my coins"),
					send("@msgid=i2 :spammer!spammer@test PRIVMSG eco :buy my coins, eco"),
					send("@msgid=i3 :alice!alice@test PRIVMSG #eco :still here"),
				},
			),
		},
		{
			// The M5 characterization test that has to flip here
			// (regression_buffers_test.go: ReopenedBufferInheritsDedupeState).
			// v1 keys "we already fetched this channel's scrollback" by channel
			// name in a session-wide map, so a buffer you close and reopen asks
			// for nothing and comes back empty. v2 keys it on the buffer, which
			// went away with the close, so the reopened buffer refetches. The
			// server here answers only the core that actually asks.
			name: "reopened_buffer_refetches_history",
			steps: concat(
				joined("#eco", "topic", "@alice eco"),
				[]step{
					act(part("#eco")),
					wait("*PART #eco"),
					send(":eco!eco@test PART #eco"),
				},
				[]step{
					act(join("#eco")),
					wait("*JOIN #eco"),
					joinBurst("#eco", "topic", "@alice eco"),
					wait("*MODE #eco"),
					replyTo("*CHATHISTORY LATEST #eco*",
						"@%LABEL% :irc.test BATCH +h chathistory #eco",
						"@batch=h;msgid=old1;time=2024-03-01T11:00:00.000Z :alice!alice@test PRIVMSG #eco :from before",
						":irc.test BATCH -h"),
				},
			),
		},
		{
			// WHOIS replies with nothing waiting for them: both cores format the
			// seven numerics and drop them in the status buffer. The routing half
			// (which buffer asked) has no headless path in v1 — its whoisTarget
			// is set from commands.go — so it is an M7 re-verify item, not a
			// parity scenario.
			name: "whois_replies",
			steps: []step{
				send(":irc.test 311 eco alice alice test * :Alice Example"),
				send(":irc.test 312 eco alice irc.test :Test Network"),
				send(":irc.test 313 eco alice :is an IRC operator"),
				send(":irc.test 317 eco alice 42 1700000000 :seconds idle, signon time"),
				send(":irc.test 319 eco alice :@#eco +#dev"),
				send(":irc.test 330 eco alice aliceacct :is logged in as"),
				send(":irc.test 671 eco alice :is using a secure connection"),
				send(":irc.test 318 eco alice :End of /WHOIS list"),
			},
		},
		{
			// An sts policy in CAP LS. testsrv leaves it out of DefaultCaps on
			// purpose, so this scenario adds it back for both cores.
			name:  "sts_policy",
			setup: func(srv *testsrv.Server) { srv.Caps = append(testsrv.DefaultCaps, "sts=port=6697,duration=2592000") },
			steps: []step{
				send(":irc.test NOTICE eco :nothing else happens here"),
			},
		},
		{
			name: "nick_change",
			steps: concat(
				joined("#eco", "topic", "@alice eco +bob"),
				[]step{
					send(":bob!bob@test NICK bobby"),
					send("@msgid=t1 :alice!alice@test TOPIC #eco :a new topic"),
					// our own nick changes: highlight matching has to follow it
					send(":eco!eco@test NICK eco2"),
					send("@msgid=t2 :alice!alice@test PRIVMSG #eco :ping eco2, are you there"),
					send("@msgid=t3 :alice!alice@test PRIVMSG #eco :and eco is nobody now"),
				},
			),
		},
	}
}

// concat flattens step lists so a scenario reads as a sequence of phases.
func concat(groups ...[]step) []step {
	var out []step
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}
