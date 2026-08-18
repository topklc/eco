// legacy_session.go is the v1 protocol core: one ircevent connection, twelve
// hand-written callbacks and the text-matching heuristics (pendingEcho, liveML,
// isPlayback, whoisTarget) the blueprint replaced at M7.
//
// NOTHING IN THE APP RUNS ON IT ANY MORE. Session (session.go) is the adapter
// over internal/irc + internal/state, and it is what the UI, the commands and
// the regression suite drive. This file survives exactly one more milestone for
// one reason: internal/parity is the dual-core gate (blueprint R-3), and a gate
// needs both cores. It is deleted with the gate at the next stage.
package app

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ergochat/irc-go/ircevent"
	"github.com/ergochat/irc-go/ircfmt"
	"github.com/ergochat/irc-go/ircmsg"
	"github.com/toprakkilic/eco/internal/config"
	"github.com/toprakkilic/eco/internal/irc/sasl"
	"github.com/toprakkilic/eco/internal/irc/transport"
	"github.com/toprakkilic/eco/internal/render"
)

// requestedCaps is every IRCv3 capability (stable and draft) the client can
// take advantage of. Requesting an unsupported cap is non-fatal, so we ask
// for everything, including Ergo's draft extensions.
var requestedCaps = []string{
	"account-notify",
	"account-tag",
	"away-notify",
	"batch",
	"cap-notify",
	"chghost",
	"echo-message",
	"extended-join",
	"extended-monitor",
	"invite-notify",
	"labeled-response",
	"message-tags",
	"multi-prefix",
	"server-time",
	"setname",
	"standard-replies",
	"userhost-in-names",
	// drafts (Ergo supports most of these)
	"draft/chathistory",
	"draft/event-playback",
	"draft/read-marker",
	"draft/message-redaction",
	"draft/multiline",
	"draft/channel-rename",
	"draft/relaymsg",
	"draft/languages",
	"draft/persistence",
	"draft/pre-away",
	"draft/account-registration",
}

type LegacySession struct {
	Name string // network profile name, fixed at creation

	// Dial, when non-nil, replaces the dialer used to reach the server
	// (ircevent's Connection.DialContext, which the websocket transport
	// already uses). This is the TEST SEAM: internal/testsrv hands the
	// offline regression suite one end of a net.Pipe here, so a real
	// Session can be driven with no network. Production code leaves it nil.
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)

	// stopped is closed when the connection goroutine started by Connect has
	// returned. It is the TEARDOWN SEAM: Quit only sets the quit flag and
	// writes QUIT — the server closing the connection is what actually unwinds
	// ircevent's Loop — so without this the offline suite could not prove a
	// test leaves no session goroutine behind (see stopSession in
	// regression_test.go). Production code never reads it. Connect is called
	// once per Session, so there is only ever one channel to close.
	stopped chan struct{}

	mu   sync.Mutex
	irc  *ircevent.Connection
	ui   UICallbacks
	bufs map[string]*Buffer // lowercase key
	// ordered buffer names for display
	order    []string
	channels []ChannelInfo // last LIST result

	historyRequested map[string]bool // channels we already pulled scrollback for
	pendingEcho      map[string]int  // locally displayed own messages awaiting echo
	whoisTarget      string          // buffer that issued the last /whois
	keepOnPart       string          // channel being cycled; do not close its buffer
	pendingPart      map[string]bool // channels the user asked to leave, awaiting the server's answer
	pendingJoin      map[string]bool // channels the user explicitly asked to join; refusals may prompt
	mlKey            string          // batch|nick|buffer of the last multiline part
	seenIDs          map[string]bool // buffer|msgid pairs already displayed
	mlMax            atomic.Int64    // cached draft/multiline max-lines
	firstScanDone    bool            // join-all runs only on the session's first LIST
	manualList       bool            // the next LIST result was requested by the user

	Config Config
}

func NewLegacySession(cfg Config, ui UICallbacks) *LegacySession {
	s := &LegacySession{ui: ui, bufs: map[string]*Buffer{}, Config: cfg,
		historyRequested: map[string]bool{}, pendingEcho: map[string]int{},
		seenIDs: map[string]bool{}}
	s.getBuffer(serverBuffer)
	return s
}

func (s *LegacySession) lower(name string) string { return strings.ToLower(name) }

// leadingInt parses leading decimal digits, matching Sscanf %d's tolerance
// of trailing text, without reflection.
func leadingInt(v string) int {
	n := 0
	for i := 0; i < len(v) && v[i] >= '0' && v[i] <= '9'; i++ {
		n = n*10 + int(v[i]-'0')
	}
	return n
}

// getBuffer returns (creating if needed) a buffer. Callers must not hold mu.
func (s *LegacySession) getBuffer(name string) *Buffer {
	s.mu.Lock()
	b, created := s.getBufferLocked(name)
	s.mu.Unlock()
	if created && s.ui.BufferList != nil {
		s.ui.BufferList()
	}
	return b
}

func (s *LegacySession) getBufferLocked(name string) (*Buffer, bool) {
	key := s.lower(name)
	if b, ok := s.bufs[key]; ok {
		return b, false
	}
	b := &Buffer{Name: name, Members: map[string]string{}}
	s.bufs[key] = b
	s.order = append(s.order, key)
	return b, true
}

// applyModeDelta folds a MODE change (e.g. "+ik-m key") into the tracked
// mode set. Only paramless channel flags and the key are kept; list and
// member modes just consume their parameter. Caller holds s.mu.
func (b *Buffer) applyModeDelta(params []string) {
	if len(params) == 0 {
		return
	}
	arg := 1
	adding := true
	for _, m := range params[0] {
		switch m {
		case '+':
			adding = true
		case '-':
			adding = false
		case 'k':
			if arg < len(params) {
				if adding {
					b.Key = params[arg]
				}
				arg++
			}
			if !adding {
				b.Key = ""
			}
			b.setMode('k', adding)
		case 'b', 'e', 'I', 'o', 'v', 'h', 'q', 'a':
			if arg < len(params) {
				arg++ // takes a mask/nick; not tracked here
			}
		case 'l':
			if adding && arg < len(params) {
				arg++
			}
			b.setMode('l', adding)
		default:
			b.setMode(m, adding)
		}
	}
}

func (b *Buffer) setMode(m rune, on bool) {
	has := strings.ContainsRune(b.Modes, m)
	if on && !has {
		b.Modes += string(m)
	} else if !on && has {
		b.Modes = strings.ReplaceAll(b.Modes, string(m), "")
	}
}

// Cycle parts and rejoins a channel, keeping its buffer.
func (s *LegacySession) Cycle(ch string) {
	if s.irc == nil {
		return
	}
	s.mu.Lock()
	s.keepOnPart = ch
	s.mu.Unlock()
	s.irc.Part(ch)
	s.irc.Join(ch)
}

func (s *LegacySession) Buffers() []*Buffer {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Buffer, 0, len(s.order))
	for _, k := range s.order {
		out = append(out, s.bufs[k])
	}
	return out
}

func (s *LegacySession) Buffer(name string) *Buffer {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bufs[s.lower(name)]
}

// JoinUser is a join the USER initiated (command, browser, menu): a
// refusal for these may prompt in the UI, unlike autojoin/reconnect
// joins, which must never pop dialogs.
func (s *LegacySession) JoinUser(ch string) { s.JoinWithKey(ch, "") }

// JoinWithKey joins a keyed (+k) channel on the user's behalf.
func (s *LegacySession) JoinWithKey(ch, key string) {
	if s.irc == nil {
		return
	}
	s.mu.Lock()
	if s.pendingJoin == nil {
		s.pendingJoin = map[string]bool{}
	}
	s.pendingJoin[s.lower(ch)] = true
	s.mu.Unlock()
	if key == "" {
		s.irc.Join(ch)
	} else {
		s.irc.Send("JOIN", ch, key)
	}
}

// Part leaves a channel and closes its buffer. The buffer normally closes
// on the server's PART echo, but the server refuses PART when we are not
// actually on the channel (a stale buffer after a failed rejoin), so the
// 442/403 reply closes it too; with no live connection it closes locally.
func (s *LegacySession) Part(ch string) {
	if s.irc == nil || !s.irc.Connected() {
		s.CloseBuffer(ch)
		return
	}
	s.mu.Lock()
	if s.pendingPart == nil {
		s.pendingPart = map[string]bool{}
	}
	s.pendingPart[s.lower(ch)] = true
	s.mu.Unlock()
	s.irc.Part(ch)
}

func (s *LegacySession) CloseBuffer(name string) {
	key := s.lower(name)
	s.mu.Lock()
	delete(s.bufs, key)
	for i, k := range s.order {
		if k == key {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	s.mu.Unlock()
	if s.ui.BufferList != nil {
		s.ui.BufferList()
	}
}

func (s *LegacySession) addLine(buffer string, l Line) {
	s.mu.Lock()
	b, created := s.getBufferLocked(buffer)
	b.Lines = append(b.Lines, l)
	// a cap of 0 means All: keep every line in memory
	if cap := int(config.Current().ScrollbackCap); cap > 0 && len(b.Lines) > cap {
		b.Lines = b.Lines[len(b.Lines)-cap:]
	}
	name := b.Name
	s.mu.Unlock()
	if created && s.ui.BufferList != nil {
		s.ui.BufferList()
	}
	if s.ui.AddLine != nil {
		s.ui.AddLine(name, l)
	}
}

func (s *LegacySession) status(buffer, text string) {
	s.addLine(buffer, Line{Time: time.Now(), Text: text, Status: true})
}

func (s *LegacySession) SortedMembers(buffer string) []string {
	b := s.Buffer(buffer)
	if b == nil {
		return nil
	}
	rank := func(prefixes string) int {
		order := "~&@%+"
		best := len(order) + 1
		for _, p := range prefixes {
			if i := strings.IndexRune(order, p); i >= 0 && i < best {
				best = i
			}
		}
		return best
	}
	// copy with precomputed sort keys under the lock, sort outside it: the
	// old comparator lowercased per comparison while blocking the IRC loop
	type ent struct {
		name, lower, prefixes string
		rank                  int
	}
	s.mu.Lock()
	ents := make([]ent, 0, len(b.Members))
	for n, p := range b.Members {
		ents = append(ents, ent{n, strings.ToLower(n), p, rank(p)})
	}
	s.mu.Unlock()
	sort.Slice(ents, func(i, j int) bool {
		if ents[i].rank != ents[j].rank {
			return ents[i].rank < ents[j].rank
		}
		return ents[i].lower < ents[j].lower
	})
	names := make([]string, len(ents))
	for i, e := range ents {
		if e.prefixes != "" {
			names[i] = string(e.prefixes[0]) + e.name
		} else {
			names[i] = e.name
		}
	}
	return names
}

// RequestList asks the server for the channel list on the user's behalf;
// the browser opens when the result arrives.
func (s *LegacySession) RequestList() {
	if s.irc == nil {
		return
	}
	s.mu.Lock()
	s.manualList = true
	s.mu.Unlock()
	s.irc.Send("LIST")
}

// ClearBuffer wipes the displayed history of one buffer (client side only).
func (s *LegacySession) ClearBuffer(name string) {
	key := s.lower(name)
	s.mu.Lock()
	if b := s.bufs[key]; b != nil {
		b.Lines = nil
	}
	// forget displayed msgids and the fetch guard so /history can refill
	prefix := key + "|"
	for id := range s.seenIDs {
		if strings.HasPrefix(id, prefix) {
			delete(s.seenIDs, id)
		}
	}
	delete(s.historyRequested, key)
	s.mu.Unlock()
}

func (s *LegacySession) ChannelList() []ChannelInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ChannelInfo(nil), s.channels...)
}

func (s *LegacySession) Connected() bool {
	return s.irc != nil && s.irc.Connected()
}

func (s *LegacySession) CurrentNick() string {
	if s.irc == nil {
		return s.Config.Nick
	}
	return s.irc.CurrentNick()
}

func msgTime(e ircmsg.Message) time.Time {
	if ok, v := e.GetTag("time"); ok {
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			return t.Local()
		}
	}
	return time.Now()
}

// isPlayback reports whether a message is part of a server batch (history
// playback rather than a live event). Playback must never mutate member
// state or trigger new history requests, or a request storm results.
func isPlayback(e ircmsg.Message) bool {
	ok, _ := e.GetTag("batch")
	return ok
}

func (s *LegacySession) isSelf(nick string) bool {
	return s.irc != nil && strings.EqualFold(nick, s.irc.CurrentNick())
}

// Connect builds the connection and starts the event loop in a goroutine.
func (s *LegacySession) Connect() {
	cfg := s.Config
	conn := &ircevent.Connection{
		Server:        cfg.Server + ":" + cfg.Port,
		Nick:          cfg.Nick,
		User:          cfg.Nick,
		RealName:      cfg.Nick,
		UseTLS:        cfg.TLS,
		Password:      cfg.ServerPass,
		TLSConfig:     &tls.Config{ServerName: cfg.Server, MinVersion: tls.VersionTLS12},
		RequestCaps:   requestedCaps,
		SASLLogin:     cfg.SASLUser,
		SASLPassword:  cfg.SASLPass,
		SASLOptional:  true,
		UseSASL:       cfg.SASLUser != "",
		QuitMessage:   "eco, a friendly IRC client",
		Version:       "eco 0.1",
		ReconnectFreq: 30 * time.Second,
		EnableCTCP:    true,
	}
	// SCRAM-SHA-256 runs post-registration under our own control; disable
	// the library's in-registration SASL and request the cap ourselves
	scramActive := cfg.SASLMech == "SCRAM-SHA-256" && cfg.SASLUser != ""
	if scramActive {
		conn.UseSASL = false
		conn.RequestCaps = append(append([]string{}, requestedCaps...), "sasl")
	}

	// IRC over WebSocket: the Server field is a ws:// or wss:// URL; the
	// websocket layer owns the transport (including TLS for wss)
	if transport.IsWebSocketURL(cfg.Server) {
		wsURL := cfg.Server
		// display-only (DialContext ignores it); do not double the port
		// when the URL already carries one, e.g. wss://host:8443/path
		display := transport.DisplayHost(wsURL)
		if !strings.Contains(display, ":") {
			display += ":443"
		}
		conn.Server = display
		conn.UseTLS = false
		conn.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return transport.Dial(ctx, wsURL)
		}
	}

	// test seam (see Session.Dial): an injected dialer wins over both the
	// websocket transport and the default TCP dialer
	if s.Dial != nil {
		conn.DialContext = s.Dial
	}

	s.irc = conn
	s.registerCallbacks()
	if scramActive {
		sasl.SetupSCRAM(conn, cfg.SASLUser, cfg.SASLPass, func(msg string) { s.status(serverBuffer, msg) })
	}

	s.status(serverBuffer, fmt.Sprintf("Connecting to %s ...", conn.Server))
	s.stopped = make(chan struct{})
	go func() {
		defer close(s.stopped)
		if err := conn.Connect(); err != nil {
			s.status(serverBuffer, "Connection failed: "+err.Error())
			if s.ui.StatusChange != nil {
				s.ui.StatusChange(false, "Connection failed: "+err.Error())
			}
			return
		}
		conn.Loop()
		if s.ui.StatusChange != nil {
			s.ui.StatusChange(false, "Disconnected")
		}
	}()
}

func (s *LegacySession) Quit() {
	if s.irc != nil {
		s.irc.Quit()
	}
}

func (s *LegacySession) hasCap(name string) bool {
	if s.irc == nil {
		return false
	}
	_, ok := s.irc.AcknowledgedCaps()[name]
	return ok
}

func (s *LegacySession) registerCallbacks() {
	c := s.irc

	c.AddConnectCallback(func(e ircmsg.Message) {
		s.refreshMultilineMax()
		s.status(serverBuffer, "Connected! You are "+c.CurrentNick())
		if s.ui.StatusChange != nil {
			s.ui.StatusChange(true, "Connected to "+s.Config.Server+" as "+c.CurrentNick())
		}
		caps := c.AcknowledgedCaps()
		if len(caps) > 0 {
			names := make([]string, 0, len(caps))
			for k := range caps {
				names = append(names, k)
			}
			sort.Strings(names)
			s.status(serverBuffer, "Active IRCv3 capabilities: "+strings.Join(names, ", "))
		}
		for _, ch := range strings.Split(s.Config.Channels, ",") {
			ch = strings.TrimSpace(ch)
			if ch != "" {
				c.Join(ch)
			}
		}
		// After an auto-reconnect, rejoin the channels whose buffers are
		// open: the library does not rejoin anything itself, and join-all
		// only runs on the session's first scan, so without this a dropped
		// connection left every buffer dead. First connect is a no-op
		// (no channel buffers exist yet).
		autojoined := map[string]bool{}
		for _, ch := range strings.Split(s.Config.Channels, ",") {
			autojoined[strings.ToLower(strings.TrimSpace(ch))] = true
		}
		var rejoin []string
		s.mu.Lock()
		for _, k := range s.order {
			if strings.HasPrefix(k, "#") && !autojoined[k] {
				if b := s.bufs[k]; b != nil {
					rejoin = append(rejoin, b.Name)
				}
			}
		}
		s.mu.Unlock()
		for i := 0; i < len(rejoin); i += 10 {
			end := i + 10
			if end > len(rejoin) {
				end = len(rejoin)
			}
			c.Join(strings.Join(rejoin[i:end], ","))
		}

		if config.Current().AutoScanChannels {
			c.Send("LIST")
		}
	})

	// LIST replies feed the channel browser
	c.AddCallback("321", func(e ircmsg.Message) {
		s.mu.Lock()
		s.channels = nil
		s.mu.Unlock()
	})
	c.AddCallback("322", func(e ircmsg.Message) {
		if len(e.Params) < 3 {
			return
		}
		users := leadingInt(e.Params[2])
		topic := ""
		if len(e.Params) > 3 {
			topic = ircfmt.Strip(e.Params[3])
		}
		s.mu.Lock()
		s.channels = append(s.channels, ChannelInfo{Name: e.Params[1], Users: users, Topic: topic})
		s.mu.Unlock()
	})
	c.AddCallback("323", func(e ircmsg.Message) {
		s.mu.Lock()
		sort.Slice(s.channels, func(i, j int) bool { return s.channels[i].Users > s.channels[j].Users })
		chans := make([]string, 0, len(s.channels))
		for _, ci := range s.channels {
			chans = append(chans, ci.Name)
		}
		firstScan := !s.firstScanDone
		s.firstScanDone = true
		manual := s.manualList
		s.manualList = false
		s.mu.Unlock()
		s.status(serverBuffer, fmt.Sprintf("Channel scan complete: %d channels found.", len(chans)))

		// joinAllLimit keeps "join everything" sane: fine on a small Ergo
		// network, but on huge networks we show the browser instead.
		// Only the session's FIRST scan may auto-join: a later /list must
		// never re-join channels the user deliberately left.
		const joinAllLimit = 75
		if config.Current().JoinAllChannels && firstScan && len(chans) > 0 && len(chans) <= joinAllLimit {
			for i := 0; i < len(chans); i += 10 {
				end := i + 10
				if end > len(chans) {
					end = len(chans)
				}
				c.Join(strings.Join(chans[i:end], ","))
			}
			s.status(serverBuffer, fmt.Sprintf("Joining all %d channels.", len(chans)))
			return
		}
		if config.Current().JoinAllChannels && firstScan && len(chans) > joinAllLimit {
			s.status(serverBuffer, fmt.Sprintf("Too many channels to join them all (%d > %d); pick from the browser instead.", len(chans), joinAllLimit))
		}
		// pop the browser for user-requested scans and the first connect,
		// but never for background rescans after an auto-reconnect
		if (manual || firstScan) && s.ui.ChannelListDone != nil {
			s.ui.ChannelListDone()
		}
	})

	// IRCv3 STS: watch CAP LS/NEW for an sts policy advertisement (and
	// keep the cached multiline limit fresh on capability changes)
	c.AddCallback("CAP", func(e ircmsg.Message) {
		s.refreshMultilineMax()
		if len(e.Params) < 3 || s.ui.STS == nil {
			return
		}
		sub := e.Params[1]
		if sub != "LS" && sub != "NEW" {
			return
		}
		for _, tok := range strings.Fields(e.Params[len(e.Params)-1]) {
			if v, ok := strings.CutPrefix(tok, "sts="); ok {
				port, duration := config.ParseSTSValue(v)
				s.ui.STS(port, duration)
			}
		}
	})

	c.AddDisconnectCallback(func(e ircmsg.Message) {
		// allow history to be re-fetched after the reconnect so messages
		// missed while offline appear; msgid dedupe prevents duplicates
		s.mu.Lock()
		s.historyRequested = map[string]bool{}
		// unanswered part/join requests died with the connection; a stale
		// pendingPart flag would let a later unrelated 442/403 close a
		// live, rejoined channel buffer
		s.pendingPart = map[string]bool{}
		s.pendingJoin = map[string]bool{}
		s.mu.Unlock()
		s.status(serverBuffer, "Disconnected from server.")
		if s.ui.StatusChange != nil {
			s.ui.StatusChange(false, "Disconnected. Reconnecting...")
		}
	})

	c.AddCallback("PRIVMSG", func(e ircmsg.Message) { s.handleMessage(e, false) })
	c.AddCallback("NOTICE", func(e ircmsg.Message) { s.handleMessage(e, true) })
	c.AddCallback("TAGMSG", func(e ircmsg.Message) {}) // typing indicators etc.; ignore quietly

	c.AddCallback("JOIN", func(e ircmsg.Message) {
		if len(e.Params) < 1 {
			return
		}
		nick := ircevent.ExtractNick(e.Source)
		ch := e.Params[0]
		if isPlayback(e) {
			if !joinPartsHiddenFor(s.Name, ch) && !s.isSelf(nick) {
				s.addLine(ch, Line{Time: msgTime(e), Text: nick + " joined", Status: true})
			}
			return
		}
		lch := s.lower(ch)
		s.mu.Lock()
		b, created := s.getBufferLocked(ch)
		if _, ok := b.Members[nick]; !ok {
			b.Members[nick] = ""
		}
		requested := s.historyRequested[lch]
		s.historyRequested[lch] = true
		s.mu.Unlock()
		if created && s.ui.BufferList != nil {
			s.ui.BufferList()
		}
		if s.isSelf(nick) {
			s.mu.Lock()
			delete(s.pendingJoin, lch)
			s.mu.Unlock()
			s.status(ch, "You joined "+ch)
			c.Send("MODE", ch) // learn the channel's modes for the settings dialog
			// Ergo/IRCv3 chathistory: fetch recent scrollback, once per channel
			if s.hasCap("draft/chathistory") && !requested {
				c.Send("CHATHISTORY", "LATEST", ch, "*", "100")
			}
		} else if !joinPartsHiddenFor(s.Name, ch) {
			s.addLine(ch, Line{Time: msgTime(e), Text: nick + " joined", Status: true})
		}
		if s.ui.MembersChange != nil {
			s.ui.MembersChange(ch)
		}
	})

	// join refusals: 471 full, 473 invite-only, 474 banned, 475 wrong key.
	// Surface them to the UI (475 prompts for the channel key) instead of
	// only a line in the server log the user may never look at.
	for _, num := range []string{"471", "473", "474", "475"} {
		needKey := num == "475"
		c.AddCallback(num, func(e ircmsg.Message) {
			if len(e.Params) < 2 {
				return
			}
			ch := e.Params[1]
			reason := "Cannot join " + ch
			if len(e.Params) > 2 {
				reason = e.Params[len(e.Params)-1]
			}
			s.status(serverBuffer, "Cannot join "+ch+": "+reason)
			s.mu.Lock()
			asked := s.pendingJoin[s.lower(ch)]
			delete(s.pendingJoin, s.lower(ch))
			s.mu.Unlock()
			// autojoin/reconnect refusals stay in the log; only joins the
			// user just asked for may interrupt with a dialog
			if asked && s.ui.JoinFailed != nil {
				s.ui.JoinFailed(ch, reason, needKey)
			}
		})
	}

	// PART refusals: the server sends 442 (not on channel) or 403 (no such
	// channel) instead of a PART echo, which used to leave the buffer stuck
	// open forever. Close it if the user asked to leave that channel.
	partRefused := func(e ircmsg.Message) {
		if len(e.Params) < 2 {
			return
		}
		ch := e.Params[1]
		s.mu.Lock()
		pending := s.pendingPart[s.lower(ch)]
		delete(s.pendingPart, s.lower(ch))
		s.mu.Unlock()
		if pending {
			s.CloseBuffer(ch)
		}
	}
	c.AddCallback("442", partRefused)
	c.AddCallback("403", partRefused)

	c.AddCallback("PART", func(e ircmsg.Message) {
		if len(e.Params) < 1 {
			return
		}
		nick := ircevent.ExtractNick(e.Source)
		ch := e.Params[0]
		if isPlayback(e) {
			if !joinPartsHiddenFor(s.Name, ch) && !s.isSelf(nick) {
				s.addLine(ch, Line{Time: msgTime(e), Text: nick + " left", Status: true})
			}
			return
		}
		if s.isSelf(nick) {
			s.mu.Lock()
			keep := strings.EqualFold(s.keepOnPart, ch)
			s.keepOnPart = ""
			delete(s.pendingPart, s.lower(ch))
			s.mu.Unlock()
			if keep {
				if b := s.Buffer(ch); b != nil {
					s.mu.Lock()
					b.Members = map[string]string{}
					s.mu.Unlock()
				}
				s.status(ch, "Rejoining "+ch)
				return
			}
			s.CloseBuffer(ch)
			return
		}
		if b := s.Buffer(ch); b != nil {
			s.mu.Lock()
			delete(b.Members, nick)
			s.mu.Unlock()
			if !joinPartsHiddenFor(s.Name, ch) {
				s.addLine(ch, Line{Time: msgTime(e), Text: nick + " left", Status: true})
			}
			if s.ui.MembersChange != nil {
				s.ui.MembersChange(ch)
			}
		}
	})

	c.AddCallback("KICK", func(e ircmsg.Message) {
		if len(e.Params) < 2 {
			return
		}
		ch, victim := e.Params[0], e.Params[1]
		reason := ""
		if len(e.Params) > 2 {
			reason = " (" + e.Params[2] + ")"
		}
		// we were removed (admin kick, channel purge): leave automatically
		// instead of keeping a dead buffer we are no longer in
		if s.isSelf(victim) && !isPlayback(e) {
			s.mu.Lock()
			delete(s.pendingPart, s.lower(ch))
			s.mu.Unlock()
			s.status(serverBuffer, "You were kicked from "+ch+" by "+ircevent.ExtractNick(e.Source)+reason)
			s.CloseBuffer(ch)
			return
		}
		if b := s.Buffer(ch); b != nil {
			s.mu.Lock()
			delete(b.Members, victim)
			s.mu.Unlock()
			s.addLine(ch, Line{Time: msgTime(e), Text: victim + " was kicked by " + ircevent.ExtractNick(e.Source) + reason, Status: true})
			if s.ui.MembersChange != nil {
				s.ui.MembersChange(ch)
			}
		}
	})

	c.AddCallback("QUIT", func(e ircmsg.Message) {
		if isPlayback(e) {
			return
		}
		nick := ircevent.ExtractNick(e.Source)
		// one pass under one lock, then fire callbacks: the per-buffer
		// relock pattern took 3+ lock acquisitions per affected buffer
		var affected []string
		s.mu.Lock()
		for _, k := range s.order {
			if b := s.bufs[k]; b != nil {
				if _, ok := b.Members[nick]; ok {
					delete(b.Members, nick)
					affected = append(affected, b.Name)
				}
			}
		}
		s.mu.Unlock()
		for _, name := range affected {
			if !joinPartsHiddenFor(s.Name, name) {
				s.addLine(name, Line{Time: msgTime(e), Text: nick + " quit", Status: true})
			}
			if s.ui.MembersChange != nil {
				s.ui.MembersChange(name)
			}
		}
	})

	c.AddCallback("NICK", func(e ircmsg.Message) {
		if len(e.Params) < 1 {
			return
		}
		old := ircevent.ExtractNick(e.Source)
		newNick := e.Params[0]
		var affected []string
		s.mu.Lock()
		for _, k := range s.order {
			if b := s.bufs[k]; b != nil {
				if p, ok := b.Members[old]; ok {
					delete(b.Members, old)
					b.Members[newNick] = p
					affected = append(affected, b.Name)
				}
			}
		}
		s.mu.Unlock()
		for _, name := range affected {
			s.addLine(name, Line{Time: msgTime(e), Text: old + " is now known as " + newNick, Status: true})
			if s.ui.MembersChange != nil {
				s.ui.MembersChange(name)
			}
		}
	})

	// NAMES replies build the member list (multi-prefix + userhost-in-names aware)
	c.AddCallback("353", func(e ircmsg.Message) {
		if len(e.Params) < 4 {
			return
		}
		ch := e.Params[2]
		s.mu.Lock()
		b, created := s.getBufferLocked(ch)
		for _, entry := range strings.Fields(e.Params[3]) {
			prefixes := ""
			for len(entry) > 0 && strings.ContainsRune("~&@%+", rune(entry[0])) {
				prefixes += string(entry[0])
				entry = entry[1:]
			}
			nick := ircevent.ExtractNick(entry) // strips user@host if present
			if nick != "" {
				b.Members[nick] = prefixes
			}
		}
		s.mu.Unlock()
		if created && s.ui.BufferList != nil {
			s.ui.BufferList()
		}
	})
	c.AddCallback("366", func(e ircmsg.Message) {
		if len(e.Params) < 2 {
			return
		}
		if s.ui.MembersChange != nil {
			s.ui.MembersChange(e.Params[1])
		}
	})

	// Topic
	c.AddCallback("332", func(e ircmsg.Message) {
		if len(e.Params) >= 3 {
			s.setTopic(e.Params[1], e.Params[2])
		}
	})
	c.AddCallback("TOPIC", func(e ircmsg.Message) {
		if len(e.Params) < 2 {
			return
		}
		s.setTopic(e.Params[0], e.Params[1])
		s.addLine(e.Params[0], Line{Time: msgTime(e), Text: ircevent.ExtractNick(e.Source) + " set the topic: " + ircfmt.Strip(e.Params[1]), Status: true})
	})

	c.AddCallback("MODE", func(e ircmsg.Message) {
		if len(e.Params) >= 2 && strings.HasPrefix(e.Params[0], "#") {
			s.mu.Lock()
			if b := s.bufs[s.lower(e.Params[0])]; b != nil {
				b.applyModeDelta(e.Params[1:])
			}
			s.mu.Unlock()
			s.addLine(e.Params[0], Line{Time: msgTime(e), Text: "mode " + strings.Join(e.Params[1:], " ") + " by " + ircevent.ExtractNick(e.Source), Status: true})
			// refresh prefixes
			c.Send("NAMES", e.Params[0])
		}
	})

	// 324 RPL_CHANNELMODEIS: full mode state, queried after our own join
	c.AddCallback("324", func(e ircmsg.Message) {
		if len(e.Params) < 3 {
			return
		}
		s.mu.Lock()
		if b := s.bufs[s.lower(e.Params[1])]; b != nil {
			b.Modes, b.Key = "", ""
			b.applyModeDelta(e.Params[2:])
		}
		s.mu.Unlock()
	})

	c.AddCallback("INVITE", func(e ircmsg.Message) {
		if len(e.Params) < 2 {
			return
		}
		s.status(serverBuffer, ircevent.ExtractNick(e.Source)+" invited you to "+e.Params[1]+". Use /join "+e.Params[1]+" to accept.")
	})

	c.AddCallback("AWAY", func(e ircmsg.Message) {}) // away-notify; could surface later

	// draft/message-redaction
	c.AddCallback("REDACT", func(e ircmsg.Message) {
		if len(e.Params) >= 2 && s.ui.RedactMsg != nil {
			s.ui.RedactMsg(e.Params[0], e.Params[1])
		}
	})

	// draft/channel-rename
	c.AddCallback("RENAME", func(e ircmsg.Message) {
		if len(e.Params) >= 2 {
			oldName, newName := e.Params[0], e.Params[1]
			s.mu.Lock()
			key := s.lower(oldName)
			if b, ok := s.bufs[key]; ok {
				delete(s.bufs, key)
				b.Name = newName
				s.bufs[s.lower(newName)] = b
				for i, k := range s.order {
					if k == key {
						s.order[i] = s.lower(newName)
					}
				}
			}
			s.mu.Unlock()
			if s.ui.RenameBuffer != nil {
				s.ui.RenameBuffer(oldName, newName)
			}
			if s.ui.BufferList != nil {
				s.ui.BufferList()
			}
			s.status(newName, "Channel renamed from "+oldName+" to "+newName)
		}
	})

	// WHOIS replies, routed to the buffer that asked
	whoisLine := func(f func(p []string) string, minParams int) func(ircmsg.Message) {
		return func(e ircmsg.Message) {
			if len(e.Params) < minParams {
				return
			}
			s.mu.Lock()
			target := s.whoisTarget
			s.mu.Unlock()
			if target == "" {
				target = serverBuffer
			}
			s.status(target, f(e.Params))
		}
	}
	c.AddCallback("311", whoisLine(func(p []string) string {
		return p[1] + " is " + p[2] + "@" + p[3] + " (" + p[len(p)-1] + ")"
	}, 5))
	c.AddCallback("312", whoisLine(func(p []string) string {
		return p[1] + " is connected via " + p[2] + " (" + p[len(p)-1] + ")"
	}, 3))
	c.AddCallback("313", whoisLine(func(p []string) string { return p[1] + " is an IRC operator" }, 2))
	c.AddCallback("317", whoisLine(func(p []string) string { return p[1] + " has been idle " + p[2] + " seconds" }, 3))
	c.AddCallback("319", whoisLine(func(p []string) string { return p[1] + " is in: " + p[len(p)-1] }, 3))
	c.AddCallback("330", whoisLine(func(p []string) string { return p[1] + " is logged in as " + p[2] }, 3))
	c.AddCallback("671", whoisLine(func(p []string) string { return p[1] + " is using a secure connection" }, 2))
	c.AddCallback("318", whoisLine(func(p []string) string { return "End of WHOIS for " + p[1] }, 2))

	// draft/account-registration: structured REGISTER and VERIFY replies
	c.AddCallback("REGISTER", func(e ircmsg.Message) {
		if len(e.Params) < 2 {
			return
		}
		switch e.Params[0] {
		case "SUCCESS":
			s.status(serverBuffer, "Account registered and logged in as "+e.Params[1]+".")
		case "VERIFICATION_REQUIRED":
			s.status(serverBuffer, "Almost done: check your email for a code, then type /verify "+e.Params[1]+" <code>")
		}
	})
	c.AddCallback("VERIFY", func(e ircmsg.Message) {
		if len(e.Params) >= 2 && e.Params[0] == "SUCCESS" {
			s.status(serverBuffer, "Account "+e.Params[1]+" verified. You are logged in.")
		}
	})

	// standard-replies: FAIL/WARN/NOTE
	for _, cmd := range []string{"FAIL", "WARN", "NOTE"} {
		cmd := cmd
		c.AddCallback(cmd, func(e ircmsg.Message) {
			s.status(serverBuffer, cmd+": "+strings.Join(e.Params, " "))
		})
	}

	// Interesting numerics land in the server buffer
	// keep raw formatting codes; the UI renders them (MOTDs are often colorful)
	for _, num := range []string{"001", "002", "003", "251", "255", "265", "266", "372", "375", "376", "396", "900", "901"} {
		c.AddCallback(num, func(e ircmsg.Message) {
			if len(e.Params) > 0 {
				s.status(serverBuffer, e.Params[len(e.Params)-1])
			}
		})
	}
	// every numeric not handled elsewhere lands in the server log, so
	// replies to commands like /oper (381) are never silently lost
	handledNumerics := map[string]bool{
		"001": true, "002": true, "003": true, "004": true, "005": true,
		"250": true, "251": true, "252": true, "253": true, "254": true,
		"255": true, "265": true, "266": true,
		"311": true, "312": true, "313": true, "317": true, "318": true,
		"319": true, "330": true, "671": true,
		"321": true, "322": true, "323": true,
		"324": true, "329": true,
		"471": true, "473": true, "474": true, "475": true,
		"332": true, "333": true, "353": true, "366": true,
		"372": true, "375": true, "376": true, "396": true,
		"900": true, "901": true, "903": true, "904": true, "905": true,
		"906": true, "907": true,
	}
	c.AddRawCallback(func(raw string, e ircmsg.Message, err error) {
		if err != nil || len(e.Command) != 3 || len(e.Params) < 2 {
			return
		}
		if e.Command[0] < '0' || e.Command[0] > '9' || handledNumerics[e.Command] {
			return
		}
		text := strings.Join(e.Params[1:], " ")
		if e.Command[0] == '4' || e.Command[0] == '5' {
			text = "Error: " + text
		}
		s.status(serverBuffer, text)
	})
}

func (s *LegacySession) setTopic(ch, topic string) {
	t := ircfmt.Strip(topic)
	s.mu.Lock()
	b, created := s.getBufferLocked(ch)
	b.Topic = t
	s.mu.Unlock()
	if created && s.ui.BufferList != nil {
		s.ui.BufferList()
	}
	if s.ui.TopicChange != nil {
		s.ui.TopicChange(ch, t)
	}
}

func (s *LegacySession) handleMessage(e ircmsg.Message, notice bool) {
	if len(e.Params) < 2 {
		return
	}
	nick := ircevent.ExtractNick(e.Source)
	target := e.Params[0]
	text := e.Params[1]
	if isIgnored(nick) && !s.isSelf(nick) {
		return
	}

	msgid := ""
	if ok, v := e.GetTag("msgid"); ok {
		msgid = v
	}

	buffer := target
	self := s.isSelf(nick)
	if !strings.HasPrefix(target, "#") {
		// private message: buffer is the other party
		if self {
			buffer = target // echo-message of our own PM
		} else {
			buffer = nick
		}
	}
	if nick == "" || strings.Contains(nick, ".") {
		// server notice
		buffer = serverBuffer
	}

	lbuf := s.lower(buffer)
	playback := isPlayback(e)

	// never display the same msgid twice in a buffer: a repeated /history
	// fetch (or a server replaying overlapping chathistory) must not
	// duplicate messages that are already on screen
	if msgid != "" {
		idKey := lbuf + "|" + msgid
		s.mu.Lock()
		if s.seenIDs[idKey] {
			s.mu.Unlock()
			return
		}
		if len(s.seenIDs) > 8192 {
			s.seenIDs = map[string]bool{}
		}
		s.seenIDs[idKey] = true
		s.mu.Unlock()
	}

	// drop the server echo of a message we already displayed locally;
	// multiline echoes arrive inside a batch, so no playback exemption here
	if self {
		key := lbuf + "|" + text
		s.mu.Lock()
		if n := s.pendingEcho[key]; n > 0 {
			if n == 1 {
				delete(s.pendingEcho, key)
			} else {
				s.pendingEcho[key] = n - 1
			}
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
	}

	action := false
	if strings.HasPrefix(text, "\x01ACTION ") && strings.HasSuffix(text, "\x01") {
		action = true
		text = strings.TrimSuffix(strings.TrimPrefix(text, "\x01ACTION "), "\x01")
	} else if strings.HasPrefix(text, "\x01") {
		return // other CTCP; ircevent answers VERSION etc. for us
	}

	// merge the parts of an incoming multiline batch into one message block.
	// multiline parts carry a batch tag but no msgid of their own, which
	// distinguishes them from history playback (every replayed message has
	// its own msgid, and merging those would glue separate messages together)
	hasBatch, batchID := e.GetTag("batch")

	// A live multiline message also arrives inside a batch, so the batch
	// tag alone cannot mean "history playback": that conflation made live
	// multiline messages invisible to highlighting and unread counting.
	// Multiline parts carry no msgid; the recency guard keeps a replayed
	// old multiline (nested in a chathistory batch, tag-identical) from
	// counting as live.
	liveML := hasBatch && msgid == "" && time.Since(msgTime(e)) < 2*time.Minute
	effPlayback := playback && !liveML

	mlKey := ""
	if hasBatch && batchID != "" && msgid == "" && !action && !notice {
		mlKey = batchID + "|" + nick + "|" + lbuf
	}
	if mlKey != "" && mlKey == s.mlKey {
		sep := "\n"
		if ok, _ := e.GetTag("draft/multiline-concat"); ok {
			sep = ""
		}
		// a mention in a continuation line must still mark the message
		hl := false
		if !self && !effPlayback {
			if cur := s.CurrentNick(); cur != "" && containsWord(text, cur) {
				hl = true
			}
		}
		s.mu.Lock()
		if b := s.bufs[lbuf]; b != nil && len(b.Lines) > 0 {
			last := &b.Lines[len(b.Lines)-1]
			newlyHighlighted := false
			if !last.Self && !last.Highlight {
				if cur := s.CurrentNick(); cur != "" && containsWord(text, cur) {
					last.Highlight = true
					newlyHighlighted = true
				}
			}
			b.Lines[len(b.Lines)-1].Text += sep + text
			if hl {
				b.Lines[len(b.Lines)-1].Highlight = true
			}
			s.mu.Unlock()
			if newlyHighlighted && s.ui.HighlightNote != nil {
				s.ui.HighlightNote(buffer, nick, text)
			}
			if s.ui.AmendBuffer != nil {
				s.ui.AmendBuffer(buffer)
			}
			return
		}
		s.mu.Unlock()
	}
	s.mlKey = mlKey

	highlight := false
	if !self && !effPlayback {
		// notices are excluded from the PM rule so services (NickServ etc.)
		// do not raise a desktop notification for every routine notice
		if !strings.HasPrefix(target, "#") && buffer != serverBuffer && !notice {
			highlight = true // private message
		} else if cur := s.CurrentNick(); cur != "" && containsWord(text, cur) {
			highlight = true // nick mentioned
		}
	}

	s.addLine(buffer, Line{
		Time: msgTime(e), Nick: nick, Text: text,
		Action: action, Notice: notice, MsgID: msgid, Self: self,
		Highlight: highlight, Playback: effPlayback,
	})
}

// --- outgoing ---

func (s *LegacySession) SendMessage(buffer, text string) {
	if s.irc == nil || !s.irc.Connected() {
		s.status(buffer, "Not connected.")
		return
	}
	if buffer == serverBuffer {
		s.status(buffer, "This is the server log. Join a channel to chat (try /join #chat).")
		return
	}
	lines := strings.Split(text, "\n")
	if len(lines) > 1 {
		// enforce the server's advertised multiline limit (Ergo default 24)
		if max := s.multilineMaxLines(); len(lines) > max {
			lines = lines[:max]
			s.status(buffer, fmt.Sprintf("Message truncated to the server's %d line limit.", max))
		}
		for i, ln := range lines {
			if ln == "" {
				lines[i] = " "
			}
		}
	}

	if len(lines) > 1 && s.hasCap("draft/multiline") {
		msgs := make([]ircmsg.Message, len(lines))
		for i, ln := range lines {
			msgs[i] = ircmsg.MakeMessage(nil, "", "PRIVMSG", buffer, ln)
		}
		s.irc.SendBatch(msgs, nil, "draft/multiline", buffer)
	} else {
		for _, ln := range lines {
			s.irc.Privmsg(buffer, ln)
		}
	}

	// Show our own message immediately, as ONE block. The per line server
	// echoes (if any) are deduped in handleMessage, so nothing doubles up.
	if s.hasCap("echo-message") {
		lbuf := s.lower(buffer)
		s.mu.Lock()
		for _, ln := range lines {
			s.pendingEcho[lbuf+"|"+ln]++
		}
		s.mu.Unlock()
	}
	s.addLine(buffer, Line{Time: time.Now(), Nick: s.irc.CurrentNick(),
		Text: strings.Join(lines, "\n"), Self: true})
}

// multilineMaxLines returns the cached max-lines limit; parsing the caps
// map per call was running on every keystroke of a multiline draft.
func (s *LegacySession) multilineMaxLines() int {
	if v := s.mlMax.Load(); v > 0 {
		return int(v)
	}
	return 24
}

// refreshMultilineMax reparses the limit from the negotiated cap value.
func (s *LegacySession) refreshMultilineMax() {
	if s.irc == nil {
		return
	}
	max := 24
	for _, kv := range strings.Split(s.irc.AcknowledgedCaps()["draft/multiline"], ",") {
		if v, ok := strings.CutPrefix(kv, "max-lines="); ok {
			if n := leadingInt(v); n > 0 {
				max = n
			}
		}
	}
	s.mlMax.Store(int64(max))
}

// containsWord reports whether nick occurs in text as a whole word, so
// a nick like "eco" does not highlight on "economy".
func containsWord(text, nick string) bool {
	t, n := strings.ToLower(text), strings.ToLower(nick)
	for i := 0; ; {
		j := strings.Index(t[i:], n)
		if j < 0 {
			return false
		}
		j += i
		end := j + len(n)
		beforeOK := j == 0 || !render.IsNickChar(t[j-1])
		afterOK := end == len(t) || !render.IsNickChar(t[end])
		if beforeOK && afterOK {
			return true
		}
		i = j + 1
	}
}

func (s *LegacySession) SendAction(buffer, text string) {
	if s.irc == nil || !s.irc.Connected() {
		return
	}
	// a single ACTION cannot contain line breaks; a newline would make the
	// protocol line unsendable
	text = strings.ReplaceAll(text, "\n", " ")
	s.irc.Action(buffer, text)
	if s.hasCap("echo-message") {
		s.mu.Lock()
		s.pendingEcho[s.lower(buffer)+"|\x01ACTION "+text+"\x01"]++
		s.mu.Unlock()
	}
	s.addLine(buffer, Line{Time: time.Now(), Nick: s.irc.CurrentNick(), Text: text, Action: true, Self: true})
}
