package app

import (
	"strings"
	"time"

	"fyne.io/fyne/v2"
)

// handleInput processes what the user typed into the message box.
// Returns the buffer to switch to, if any.
func (u *UI) handleInput(text string) {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return
	}
	// The session and buffer a command applies to are the ones on screen: read
	// them once, from the viewmodel, rather than re-asking per branch.
	sess, cur := u.session(), u.current()
	if !strings.HasPrefix(text, "/") || strings.HasPrefix(text, "//") {
		if sess != nil {
			sess.SendMessage(cur, strings.TrimPrefix(text, "/"))
		}
		return
	}

	parts := strings.SplitN(text[1:], " ", 2)
	cmd := strings.ToLower(parts[0])
	arg := ""
	if len(parts) > 1 {
		arg = strings.TrimSpace(parts[1])
	}

	switch cmd {
	case "server", "servers", "network", "networks", "connect":
		u.ShowNetworkManager()
		return
	case "testnotify":
		u.notify("eco", "Notifications are working.")
		return
	}

	if sess == nil || sess.irc == nil {
		return
	}
	irc := sess.irc

	switch cmd {
	case "join", "j":
		if arg == "" {
			sess.status(cur, "Usage: /join #channel")
			return
		}
		f := strings.SplitN(arg, " ", 2)
		if !strings.HasPrefix(f[0], "#") {
			f[0] = "#" + f[0]
		}
		if len(f) == 2 {
			sess.JoinWithKey(f[0], strings.TrimSpace(f[1]))
		} else {
			sess.JoinUser(f[0])
		}
	case "part", "leave":
		target := cur
		if arg != "" {
			target = arg
			if !strings.HasPrefix(target, "#") {
				target = "#" + target
			}
		}
		// only channels can be parted; without this a /part in the server
		// buffer or a PM would destroy that buffer via the refusal path
		if !strings.HasPrefix(target, "#") {
			sess.status(cur, "Usage: /part [#channel]")
			return
		}
		sess.Part(target)
	case "close":
		if cur != serverBuffer {
			if strings.HasPrefix(cur, "#") {
				sess.Part(cur)
			} else {
				sess.CloseBuffer(cur)
			}
		}
	case "msg", "query", "q":
		f := strings.SplitN(arg, " ", 2)
		if len(f) == 0 || f[0] == "" {
			sess.status(cur, "Usage: /msg nick message")
			return
		}
		sess.getBuffer(f[0])
		u.selectBuffer(f[0])
		if len(f) == 2 {
			sess.SendMessage(f[0], f[1])
		}
	case "me":
		if arg == "" {
			sess.status(cur, "Usage: /me does something")
			return
		}
		sess.SendAction(cur, arg)
	case "nick":
		if arg == "" {
			sess.status(cur, "Usage: /nick newnickname")
			return
		}
		irc.SetNick(arg)
	case "topic":
		if arg == "" {
			irc.Send("TOPIC", cur)
		} else {
			irc.Send("TOPIC", cur, arg)
		}
	case "whois":
		if arg == "" {
			sess.status(cur, "Usage: /whois nick")
			return
		}
		// the reply follows this request's own correlation, so it lands in the
		// buffer that asked even when another /whois is in flight
		sess.Whois(cur, arg)
	case "away":
		if arg == "" {
			irc.Send("AWAY")
			sess.status(cur, "You are no longer away.")
		} else {
			irc.Send("AWAY", arg)
			sess.status(cur, "You are now away: "+arg)
		}
	case "notice":
		f := strings.SplitN(arg, " ", 2)
		if len(f) == 2 {
			irc.Notice(f[0], f[1])
		} else {
			sess.status(cur, "Usage: /notice target message")
		}
	case "mode":
		f := strings.Fields(arg)
		irc.Send("MODE", f...)
	case "kick":
		f := strings.SplitN(arg, " ", 2)
		if len(f) >= 1 && f[0] != "" {
			params := []string{cur, f[0]}
			if len(f) == 2 {
				params = append(params, f[1])
			}
			irc.Send("KICK", params...)
		}
	case "invite":
		if arg == "" {
			sess.status(cur, "Usage: /invite nick")
			return
		}
		irc.Send("INVITE", arg, cur)
	case "list":
		sess.RequestList()
	case "names":
		irc.Send("NAMES", cur)
	case "redact":
		// Ergo draft/message-redaction: /redact <msgid> [reason]
		if arg == "" {
			sess.status(cur, "Usage: /redact msgid [reason]")
			return
		}
		f := strings.SplitN(arg, " ", 2)
		params := []string{cur, f[0]}
		if len(f) == 2 {
			params = append(params, f[1])
		}
		irc.Send("REDACT", params...)
	case "register":
		f := strings.Fields(arg)
		if len(f) < 1 {
			sess.status(cur, "Usage: /register password [email]")
			return
		}
		// prefer the standard account-registration command; NickServ fallback
		if sess.hasCap("draft/account-registration") {
			email := "*"
			if len(f) > 1 {
				email = f[1]
			}
			irc.Send("REGISTER", "*", email, f[0])
			sess.status(cur, "Registering your current nickname as an account...")
		} else {
			irc.Privmsg("NickServ", "REGISTER "+strings.Join(f, " "))
			sess.status(cur, "Sent registration request to NickServ.")
		}
	case "verify":
		f := strings.Fields(arg)
		switch len(f) {
		case 1:
			irc.Send("VERIFY", sess.CurrentNick(), f[0])
		case 2:
			irc.Send("VERIFY", f[0], f[1])
		default:
			sess.status(cur, "Usage: /verify [account] code")
		}
	case "identify":
		irc.Privmsg("NickServ", "IDENTIFY "+arg)
	case "ns":
		irc.Privmsg("NickServ", arg)
	case "cs":
		irc.Privmsg("ChanServ", arg)
	case "hs":
		irc.Privmsg("HostServ", arg)
	case "history":
		// fetch more scrollback (Ergo chathistory)
		if cur == serverBuffer {
			sess.status(cur, "History works in channels and conversations, not the server log.")
			return
		}
		irc.Send("CHATHISTORY", "LATEST", cur, "*", "200")
	case "setname":
		if arg == "" {
			sess.status(cur, "Usage: /setname your real name")
			return
		}
		irc.Send("SETNAME", arg)
	case "disconnect":
		u.closeSession(sess)
	case "quit", "exit":
		if arg != "" {
			irc.Send("QUIT", arg)
		}
		for _, s := range u.sessions {
			s.Quit()
		}
		// give the QUIT lines a moment to flush before exiting the app
		go func() {
			time.Sleep(300 * time.Millisecond)
			fyne.Do(func() { u.app.Quit() })
		}()
	case "oper":
		f := strings.Fields(arg)
		if len(f) < 2 {
			sess.status(cur, "Usage: /oper name password")
			return
		}
		irc.Send("OPER", f[0], f[1])
	case "who":
		if arg == "" {
			arg = cur
		}
		irc.Send("WHO", arg)
	case "whowas":
		if arg == "" {
			sess.status(cur, "Usage: /whowas nick")
			return
		}
		irc.Send("WHOWAS", arg)
	case "motd", "time", "admin", "info", "version", "links", "lusers", "map", "rules":
		irc.Send(strings.ToUpper(cmd))
	case "stats", "ison", "userhost", "monitor":
		irc.Send(strings.ToUpper(cmd), strings.Fields(arg)...)
	case "knock":
		if arg == "" {
			sess.status(cur, "Usage: /knock #channel")
			return
		}
		irc.Send("KNOCK", arg)
	case "wallops":
		if arg == "" {
			sess.status(cur, "Usage: /wallops message")
			return
		}
		irc.Send("WALLOPS", arg)
	case "kill":
		if arg == "" {
			sess.status(cur, "Usage: /kill nick [reason]")
			return
		}
		f := strings.SplitN(arg, " ", 2)
		irc.Send("KILL", f...)
	case "ctcp":
		f := strings.SplitN(arg, " ", 2)
		if len(f) < 2 {
			sess.status(cur, "Usage: /ctcp nick command")
			return
		}
		irc.Privmsg(f[0], "\x01"+strings.ToUpper(f[1])+"\x01")
	case "op", "deop", "voice", "devoice", "hop", "dehop":
		modes := map[string]string{"op": "+o", "deop": "-o", "voice": "+v",
			"devoice": "-v", "hop": "+h", "dehop": "-h"}
		targets := strings.Fields(arg)
		if len(targets) == 0 {
			sess.status(cur, "Usage: /"+cmd+" nick [nick...]")
			return
		}
		m := modes[cmd]
		params := []string{cur, string(m[0]) + strings.Repeat(string(m[1]), len(targets))}
		irc.Send("MODE", append(params, targets...)...)
	case "ban", "unban":
		if arg == "" {
			sess.status(cur, "Usage: /"+cmd+" nick-or-mask")
			return
		}
		mask := arg
		if !strings.ContainsAny(mask, "!*@") {
			mask += "!*@*"
		}
		flag := "+b"
		if cmd == "unban" {
			flag = "-b"
		}
		irc.Send("MODE", cur, flag, mask)
	case "kickban":
		f := strings.SplitN(arg, " ", 2)
		if len(f) < 1 || f[0] == "" {
			sess.status(cur, "Usage: /kickban nick [reason]")
			return
		}
		irc.Send("MODE", cur, "+b", f[0]+"!*@*")
		params := []string{cur, f[0]}
		if len(f) == 2 {
			params = append(params, f[1])
		}
		irc.Send("KICK", params...)
	case "cycle", "rejoin":
		if strings.HasPrefix(cur, "#") {
			sess.Cycle(cur)
		}
	case "clear":
		sess.ClearBuffer(cur)
		u.renderBuffer()
	case "back":
		irc.Send("AWAY")
		sess.status(cur, "You are no longer away.")
	case "raw", "quote":
		irc.SendRaw(arg)
	case "helpwindow", "docs":
		u.showHelp()
	case "help":
		u.showHelp()
		sess.status(cur, "Chat: /join /part /close /msg /query /me /notice /ctcp /clear /cycle. You: /nick /away /back /setname /register /verify /identify. Channel ops: /topic /mode /op /deop /voice /devoice /hop /dehop /ban /unban /kick /kickban /invite /knock. Info: /whois /whowas /who /list /names /history /motd /time /version /admin /info /links /lusers /map /rules /stats /ison /userhost /monitor. Services: /ns /cs /hs. Oper: /oper /kill /wallops. App: /server /disconnect /redact /raw /quit /help. Anything else is sent as a message; unknown /commands pass through to the server.")
	default:
		// unknown command: pass through raw so power users aren't blocked
		irc.SendRaw(strings.TrimSpace(strings.ToUpper(cmd) + " " + arg))
	}
}
