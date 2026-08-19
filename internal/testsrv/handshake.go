package testsrv

import "strings"

// DefaultCaps is what the fake server advertises in CAP LS: every capability
// eco requests, with the values Ergo attaches. It is mirrored here rather
// than imported from internal/app, which imports this package.
//
// sts is deliberately absent: advertising it would make every test that
// registers persist an STS policy.
var DefaultCaps = []string{
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
	"sasl=PLAIN,EXTERNAL,SCRAM-SHA-256",
	"server-time",
	"setname",
	"standard-replies",
	"userhost-in-names",
	"draft/chathistory",
	"draft/event-playback",
	"draft/read-marker",
	"draft/message-redaction",
	"draft/multiline=max-bytes=4096,max-lines=24",
	"draft/channel-rename",
	"draft/relaymsg",
	"draft/languages=1,en,~en",
	"draft/persistence",
	"draft/pre-away",
	"draft/account-registration=before-connect",
}

// DefaultISupport is the 005 burst: an Ergo-shaped set with the tokens the
// client actually reads (CHANTYPES, PREFIX, CASEMAPPING, CHATHISTORY).
var DefaultISupport = []string{
	"AWAYLEN=390", "BOT=B", "CASEMAPPING=ascii", "CHANLIMIT=#:100",
	"CHANMODES=beI,k,fl,CEMRUimnstu", "CHANNELLEN=64", "CHANTYPES=#",
	"CHATHISTORY=1000", "ELIST=U", "EXCEPTS", "EXTBAN=,m", "FORWARD=f",
	"INVEX", "KICKLEN=390", "MAXLIST=beI:60", "MAXTARGETS=4", "MODES",
	"MONITOR=100", "NETWORK=TestNet", "NICKLEN=32", "PREFIX=(qaohv)~&@%+",
	"STATUSMSG=~&@%+", "TARGMAX=NAMES:1,LIST:1,KICK:,WHOIS:1,PRIVMSG:4",
	"TOPICLEN=390", "UTF8ONLY", "WHOX", "draft/CHATHISTORY=1000",
}

// isupportPerLine keeps each 005 under the 512-byte line limit.
const isupportPerLine = 13

// Register drives the whole registration handshake the way ircevent performs
// it: wait for CAP LS, advertise, ACK or NAK every CAP REQ as it arrives,
// then on CAP END send 001-005, the ISUPPORT burst and the MOTD. It returns
// once the client has finished processing all of it (Register ends with a
// Sync), so anything the client sends on connect - autojoin JOINs, LIST - is
// already queued and assertable.
//
// nick is the nick the server registers the client under; the nick the client
// actually asked for is available from Nick.
func (s *Server) Register(nick string) {
	s.t.Helper()
	s.WaitFor("CAP LS") // PASS and WEBIRC may come first
	s.Send(":%s CAP * LS :%s", s.Host, strings.Join(s.caps(), " "))
	for {
		fields := strings.Fields(s.next())
		if len(fields) == 0 {
			continue
		}
		switch strings.ToUpper(fields[0]) {
		case "NICK":
			if len(fields) > 1 {
				s.mu.Lock()
				s.nick = strings.TrimPrefix(fields[1], ":")
				s.mu.Unlock()
			}
		case "AUTHENTICATE":
			if len(fields) > 1 {
				s.authenticate(fields[1], nick)
			}
		case "CAP":
			if len(fields) < 2 {
				continue
			}
			switch strings.ToUpper(fields[1]) {
			case "REQ":
				s.replyCapReq(strings.TrimPrefix(strings.Join(fields[2:], " "), ":"))
			case "END":
				s.Welcome(nick)
				return
			}
		}
	}
}

// Welcome sends the post-registration burst (001-005, ISUPPORT, MOTD) and
// syncs. Register calls it; tests that script their own handshake (SASL,
// capability refusals) can call it themselves.
func (s *Server) Welcome(nick string) {
	s.t.Helper()
	s.Send(":%s 001 %s :Welcome to the TestNet IRC Network %s", s.Host, nick, nick)
	s.Send(":%s 002 %s :Your host is %s, running version testsrv-1", s.Host, nick, s.Host)
	s.Send(":%s 003 %s :This server was created at the beginning of the test", s.Host, nick)
	s.Send(":%s 004 %s %s testsrv-1 BEIRUabefhiklmnoqstuv Iabefhiklmnoqstuv", s.Host, nick, s.Host)
	tokens := s.isupport()
	for i := 0; i < len(tokens); i += isupportPerLine {
		end := min(i+isupportPerLine, len(tokens))
		s.Send(":%s 005 %s %s :are supported by this server", s.Host, nick, strings.Join(tokens[i:end], " "))
	}
	s.Send(":%s 375 %s :- %s Message of the Day -", s.Host, nick, s.Host)
	s.Send(":%s 372 %s :- testsrv, a scripted fake server", s.Host, nick)
	s.Send(":%s 376 %s :End of /MOTD command.", s.Host, nick)
	s.Sync()
}

// Nick returns the nick the client asked for in registration.
func (s *Server) Nick() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nick
}

// authenticate answers ircevent's in-registration SASL exchange: a challenge
// for the mechanism, then success for the client's response. That is all the
// default handshake needs - tests for SASL failure, or for the SCRAM exchange
// eco runs after registration, script AUTHENTICATE themselves.
func (s *Server) authenticate(arg, nick string) {
	s.t.Helper()
	switch strings.ToUpper(arg) {
	case "PLAIN", "EXTERNAL":
		s.Send("AUTHENTICATE +")
	default: // the client's base64 response (or "+" for EXTERNAL)
		s.Send(":%s 900 %s %s!%s@test %s :You are now logged in as %s", s.Host, nick, nick, nick, nick, nick)
		s.Send(":%s 903 %s :SASL authentication successful", s.Host, nick)
	}
}

// replyCapReq answers one CAP REQ, which may carry several capabilities.
func (s *Server) replyCapReq(req string) {
	s.t.Helper()
	var acked, naked []string
	for _, name := range strings.Fields(req) {
		if s.supports(name) {
			acked = append(acked, name)
		} else {
			naked = append(naked, name)
		}
	}
	if len(acked) > 0 {
		s.Send(":%s CAP * ACK :%s", s.Host, strings.Join(acked, " "))
	}
	if len(naked) > 0 {
		s.Send(":%s CAP * NAK :%s", s.Host, strings.Join(naked, " "))
	}
}

// supports reports whether a requested capability is granted. The default
// (Caps nil) grants everything; a configured Caps list is exact.
func (s *Server) supports(name string) bool {
	if s.Caps == nil {
		return true
	}
	for _, c := range s.Caps {
		if capName(c) == name {
			return true
		}
	}
	return false
}

func (s *Server) caps() []string {
	if s.Caps != nil {
		return s.Caps
	}
	return DefaultCaps
}

func (s *Server) isupport() []string {
	if s.ISupport != nil {
		return s.ISupport
	}
	return DefaultISupport
}

// capName strips the value from an advertised "name=value" capability.
func capName(token string) string {
	name, _, _ := strings.Cut(token, "=")
	return name
}
