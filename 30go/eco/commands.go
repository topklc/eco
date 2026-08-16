package main

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
	if !strings.HasPrefix(text, "/") || strings.HasPrefix(text, "//") {
		if u.sess != nil {
			u.sess.SendMessage(u.current, strings.TrimPrefix(text, "/"))
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

	if u.sess == nil || u.sess.irc == nil {
		return
	}
	irc := u.sess.irc

	switch cmd {
	case "join", "j":
		if arg == "" {
			u.sess.status(u.current, "Usage: /join #channel")
			return
		}
		if !strings.HasPrefix(arg, "#") {
			arg = "#" + arg
		}
		irc.Join(arg)
	case "part", "leave":
		target := u.current
		if arg != "" {
			target = arg
		}
		irc.Part(target)
	case "close":
		if u.current != serverBuffer {
			if strings.HasPrefix(u.current, "#") {
				irc.Part(u.current)
			} else {
				u.sess.CloseBuffer(u.current)
			}
		}
	case "msg", "query", "q":
		f := strings.SplitN(arg, " ", 2)
		if len(f) == 0 || f[0] == "" {
			u.sess.status(u.current, "Usage: /msg nick message")
			return
		}
		u.sess.getBuffer(f[0])
		u.selectBuffer(f[0])
		if len(f) == 2 {
			u.sess.SendMessage(f[0], f[1])
		}
	case "me":
		if arg == "" {
			u.sess.status(u.current, "Usage: /me does something")
			return
		}
		u.sess.SendAction(u.current, arg)
	case "nick":
		if arg == "" {
			u.sess.status(u.current, "Usage: /nick newnickname")
			return
		}
		irc.SetNick(arg)
	case "topic":
		if arg == "" {
			irc.Send("TOPIC", u.current)
		} else {
			irc.Send("TOPIC", u.current, arg)
		}
	case "whois":
		if arg == "" {
			u.sess.status(u.current, "Usage: /whois nick")
			return
		}
		u.sess.mu.Lock()
		u.sess.whoisTarget = u.current
		u.sess.mu.Unlock()
		irc.Send("WHOIS", arg)
	case "away":
		if arg == "" {
			irc.Send("AWAY")
			u.sess.status(u.current, "You are no longer away.")
		} else {
			irc.Send("AWAY", arg)
			u.sess.status(u.current, "You are now away: "+arg)
		}
	case "notice":
		f := strings.SplitN(arg, " ", 2)
		if len(f) == 2 {
			irc.Notice(f[0], f[1])
		} else {
			u.sess.status(u.current, "Usage: /notice target message")
		}
	case "mode":
		f := strings.Fields(arg)
		irc.Send("MODE", f...)
	case "kick":
		f := strings.SplitN(arg, " ", 2)
		if len(f) >= 1 && f[0] != "" {
			params := []string{u.current, f[0]}
			if len(f) == 2 {
				params = append(params, f[1])
			}
			irc.Send("KICK", params...)
		}
	case "invite":
		if arg == "" {
			u.sess.status(u.current, "Usage: /invite nick")
			return
		}
		irc.Send("INVITE", arg, u.current)
	case "list":
		u.sess.RequestList()
	case "names":
		irc.Send("NAMES", u.current)
	case "redact":
		// Ergo draft/message-redaction: /redact <msgid> [reason]
		if arg == "" {
			u.sess.status(u.current, "Usage: /redact msgid [reason]")
			return
		}
		f := strings.SplitN(arg, " ", 2)
		params := []string{u.current, f[0]}
		if len(f) == 2 {
			params = append(params, f[1])
		}
		irc.Send("REDACT", params...)
	case "register":
		f := strings.Fields(arg)
		if len(f) < 1 {
			u.sess.status(u.current, "Usage: /register password [email]")
			return
		}
		// prefer the standard account-registration command; NickServ fallback
		if u.sess.hasCap("draft/account-registration") {
			email := "*"
			if len(f) > 1 {
				email = f[1]
			}
			irc.Send("REGISTER", "*", email, f[0])
			u.sess.status(u.current, "Registering your current nickname as an account...")
		} else {
			irc.Privmsg("NickServ", "REGISTER "+strings.Join(f, " "))
			u.sess.status(u.current, "Sent registration request to NickServ.")
		}
	case "verify":
		f := strings.Fields(arg)
		switch len(f) {
		case 1:
			irc.Send("VERIFY", u.sess.CurrentNick(), f[0])
		case 2:
			irc.Send("VERIFY", f[0], f[1])
		default:
			u.sess.status(u.current, "Usage: /verify [account] code")
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
		if u.current == serverBuffer {
			u.sess.status(u.current, "History works in channels and conversations, not the server log.")
			return
		}
		irc.Send("CHATHISTORY", "LATEST", u.current, "*", "200")
	case "setname":
		if arg == "" {
			u.sess.status(u.current, "Usage: /setname your real name")
			return
		}
		irc.Send("SETNAME", arg)
	case "disconnect":
		u.closeSession(u.sess)
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
			u.sess.status(u.current, "Usage: /oper name password")
			return
		}
		irc.Send("OPER", f[0], f[1])
	case "who":
		if arg == "" {
			arg = u.current
		}
		irc.Send("WHO", arg)
	case "whowas":
		if arg == "" {
			u.sess.status(u.current, "Usage: /whowas nick")
			return
		}
		irc.Send("WHOWAS", arg)
	case "motd", "time", "admin", "info", "version", "links", "lusers", "map", "rules":
		irc.Send(strings.ToUpper(cmd))
	case "stats", "ison", "userhost", "monitor":
		irc.Send(strings.ToUpper(cmd), strings.Fields(arg)...)
	case "knock":
		if arg == "" {
			u.sess.status(u.current, "Usage: /knock #channel")
			return
		}
		irc.Send("KNOCK", arg)
	case "wallops":
		if arg == "" {
			u.sess.status(u.current, "Usage: /wallops message")
			return
		}
		irc.Send("WALLOPS", arg)
	case "kill":
		if arg == "" {
			u.sess.status(u.current, "Usage: /kill nick [reason]")
			return
		}
		f := strings.SplitN(arg, " ", 2)
		irc.Send("KILL", f...)
	case "ctcp":
		f := strings.SplitN(arg, " ", 2)
		if len(f) < 2 {
			u.sess.status(u.current, "Usage: /ctcp nick command")
			return
		}
		irc.Privmsg(f[0], "\x01"+strings.ToUpper(f[1])+"\x01")
	case "op", "deop", "voice", "devoice", "hop", "dehop":
		modes := map[string]string{"op": "+o", "deop": "-o", "voice": "+v",
			"devoice": "-v", "hop": "+h", "dehop": "-h"}
		targets := strings.Fields(arg)
		if len(targets) == 0 {
			u.sess.status(u.current, "Usage: /"+cmd+" nick [nick...]")
			return
		}
		m := modes[cmd]
		params := []string{u.current, string(m[0]) + strings.Repeat(string(m[1]), len(targets))}
		irc.Send("MODE", append(params, targets...)...)
	case "ban", "unban":
		if arg == "" {
			u.sess.status(u.current, "Usage: /"+cmd+" nick-or-mask")
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
		irc.Send("MODE", u.current, flag, mask)
	case "kickban":
		f := strings.SplitN(arg, " ", 2)
		if len(f) < 1 || f[0] == "" {
			u.sess.status(u.current, "Usage: /kickban nick [reason]")
			return
		}
		irc.Send("MODE", u.current, "+b", f[0]+"!*@*")
		params := []string{u.current, f[0]}
		if len(f) == 2 {
			params = append(params, f[1])
		}
		irc.Send("KICK", params...)
	case "cycle", "rejoin":
		if strings.HasPrefix(u.current, "#") {
			ch := u.current
			u.sess.mu.Lock()
			u.sess.keepOnPart = ch
			u.sess.mu.Unlock()
			irc.Part(ch)
			irc.Join(ch)
		}
	case "clear":
		u.sess.ClearBuffer(u.current)
		u.showBuffer(u.current)
	case "back":
		irc.Send("AWAY")
		u.sess.status(u.current, "You are no longer away.")
	case "raw", "quote":
		irc.SendRaw(arg)
	case "helpwindow", "docs":
		u.showHelp()
	case "help":
		u.showHelp()
		u.sess.status(u.current, "Chat: /join /part /close /msg /query /me /notice /ctcp /clear /cycle. You: /nick /away /back /setname /register /verify /identify. Channel ops: /topic /mode /op /deop /voice /devoice /hop /dehop /ban /unban /kick /kickban /invite /knock. Info: /whois /whowas /who /list /names /history /motd /time /version /admin /info /links /lusers /map /rules /stats /ison /userhost /monitor. Services: /ns /cs /hs. Oper: /oper /kill /wallops. App: /server /disconnect /redact /raw /quit /help. Anything else is sent as a message; unknown /commands pass through to the server.")
	default:
		// unknown command: pass through raw so power users aren't blocked
		irc.SendRaw(strings.TrimSpace(strings.ToUpper(cmd) + " " + arg))
	}
}
