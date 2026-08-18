package main

import (
	"github.com/ergochat/irc-go/ircevent"
	"github.com/ergochat/irc-go/ircmsg"
	"github.com/ergochat/irc-go/ircutils"
	"github.com/xdg-go/scram"
)

// SCRAM-SHA-256 SASL. ircevent only implements PLAIN and EXTERNAL during
// registration, so eco drives the SCRAM conversation itself right after
// connecting (the sasl cap permits post-registration authentication, which
// Ergo supports).
func (s *Session) setupSCRAM(c *ircevent.Connection) {
	var conv *scram.ClientConversation
	buf := ircutils.NewSASLBuffer(10 * 1024)

	abort := func(why string) {
		s.status(serverBuffer, "SCRAM login failed: "+why)
		c.Send("AUTHENTICATE", "*")
		conv = nil
	}

	c.AddConnectCallback(func(e ircmsg.Message) {
		if _, ok := c.AcknowledgedCaps()["sasl"]; !ok {
			s.status(serverBuffer, "SCRAM login skipped: the server did not accept the sasl capability.")
			return
		}
		client, err := scram.SHA256.NewClient(s.Config.SASLUser, s.Config.SASLPass, "")
		if err != nil {
			s.status(serverBuffer, "SCRAM setup failed: "+err.Error())
			return
		}
		conv = client.NewConversation()
		buf.Clear()
		c.Send("AUTHENTICATE", "SCRAM-SHA-256")
	})

	c.AddCallback("AUTHENTICATE", func(e ircmsg.Message) {
		if conv == nil || len(e.Params) == 0 {
			return
		}
		done, data, err := buf.Add(e.Params[0])
		if err != nil {
			abort(err.Error())
			return
		}
		if !done {
			return // more 400-byte chunks coming
		}
		resp, err := conv.Step(string(data))
		if err != nil {
			abort(err.Error())
			return
		}
		if resp == "" {
			// server verification consumed; acknowledge to finish
			c.Send("AUTHENTICATE", "+")
			return
		}
		for _, chunk := range ircutils.EncodeSASLResponse([]byte(resp)) {
			c.Send("AUTHENTICATE", chunk)
		}
	})

	c.AddCallback("903", func(e ircmsg.Message) {
		if conv != nil {
			s.status(serverBuffer, "Logged in via SCRAM-SHA-256.")
			conv = nil
		}
	})
	for _, num := range []string{"904", "905", "906", "907"} {
		c.AddCallback(num, func(e ircmsg.Message) {
			if conv != nil {
				s.status(serverBuffer, "SCRAM login failed: "+lastParamOf(e))
				conv = nil
			}
		})
	}
}

func lastParamOf(e ircmsg.Message) string {
	if len(e.Params) == 0 {
		return ""
	}
	return e.Params[len(e.Params)-1]
}
