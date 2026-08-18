package sasl

import (
	"github.com/ergochat/irc-go/ircevent"
	"github.com/ergochat/irc-go/ircmsg"
	"github.com/ergochat/irc-go/ircutils"
	"github.com/xdg-go/scram"
)

// SetupSCRAM drives SCRAM-SHA-256 SASL on c. ircevent only implements PLAIN
// and EXTERNAL during registration, so eco runs the SCRAM conversation itself
// right after connecting (the sasl cap permits post-registration
// authentication, which Ergo supports). status receives human-readable
// progress lines for the server log.
func SetupSCRAM(c *ircevent.Connection, user, pass string, status func(string)) {
	var conv *scram.ClientConversation
	buf := ircutils.NewSASLBuffer(10 * 1024)

	abort := func(why string) {
		status("SCRAM login failed: " + why)
		c.Send("AUTHENTICATE", "*")
		conv = nil
	}

	c.AddConnectCallback(func(e ircmsg.Message) {
		if _, ok := c.AcknowledgedCaps()["sasl"]; !ok {
			status("SCRAM login skipped: the server did not accept the sasl capability.")
			return
		}
		client, err := scram.SHA256.NewClient(user, pass, "")
		if err != nil {
			status("SCRAM setup failed: " + err.Error())
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
			status("Logged in via SCRAM-SHA-256.")
			conv = nil
		}
	})
	for _, num := range []string{"904", "905", "906", "907"} {
		c.AddCallback(num, func(e ircmsg.Message) {
			if conv != nil {
				status("SCRAM login failed: " + lastParamOf(e))
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
