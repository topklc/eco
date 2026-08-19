package irc

import (
	"errors"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ergochat/irc-go/ircevent"
	"github.com/ergochat/irc-go/ircmsg"
	"github.com/toprakkilic/eco/internal/model"
)

// ErrEmptyTarget is returned when a send has nowhere to go.
var ErrEmptyTarget = errors.New("irc: empty target")

// Handle is the receipt for an outgoing command: what was written, and the
// labels its echoes will carry.
//
// This is the whole point of the labelled-response ledger. The store creates
// one pending message per label and resolves it when an event arrives with
// that label - gaining the server's msgid and server-time - instead of
// matching the echo's text, which cannot tell two identical messages apart
// and breaks outright when the server rewrites the body (relays, filters,
// always-on multiclient).
type Handle struct {
	// Target is the raw target the command was addressed to.
	Target string
	// Lines is what actually went on the wire, after multiline splitting and
	// truncation.
	Lines []string
	// Labels holds one label per independently resolvable echo: exactly one
	// for a multiline batch, one per line when the message had to be split
	// without one, and none at all when the server does not do
	// labeled-response.
	Labels []string
	// Truncated reports that the message was cut to the server's advertised
	// multiline line limit.
	Truncated bool
	// Multiline reports that the lines went out as one BATCH and will come
	// back as one merged message.
	Multiline bool
}

// Label returns the primary label, "" when labels are not in use.
func (h Handle) Label() string {
	if len(h.Labels) == 0 {
		return ""
	}
	return h.Labels[0]
}

// batchSeq numbers our outgoing BATCH references.
var batchSeq atomic.Uint64

// live returns the current connection, or ErrNotConnected.
func (c *Client) live() (*ircevent.Connection, error) {
	conn := c.connection()
	if conn == nil || !conn.Connected() {
		return nil, ErrNotConnected
	}
	return conn, nil
}

// labelled reports whether outgoing commands may carry a label. The spec
// makes labeled-response depend on batch and message-tags; asking for all
// three keeps eco from tagging a server that never agreed to read tags.
func (c *Client) labelled() bool {
	caps := c.caps.Load()
	return caps.Enabled("labeled-response") && caps.Enabled("message-tags") && caps.Enabled("batch")
}

// label allocates a label for an outgoing command, or "" when the server does
// not support correlating one.
func (c *Client) label(s Sent) string {
	if !c.labelled() {
		return ""
	}
	return c.ledger.next(s)
}

func labelTags(label string) map[string]string {
	if label == "" {
		return nil
	}
	return map[string]string{"label": label}
}

// sendBody writes a command whose last parameter is a message body, forcing
// it into the trailing position. The encoder only insists on that when the
// body would otherwise be ambiguous; doing it always is what every other
// client does, and it keeps a body that happens to look like a parameter from
// depending on the server's tolerance.
func sendBody(conn *ircevent.Connection, tags map[string]string, command string, params ...string) error {
	msg := ircmsg.MakeMessage(tags, "", command, params...)
	msg.ForceTrailing()
	return conn.SendIRCMessage(msg)
}

// Send writes one command with no label. It is the escape hatch for commands
// eco has no typed method for; everything it knows about goes through the
// methods below so the reply can be correlated.
func (c *Client) Send(command string, params ...string) error {
	conn, err := c.live()
	if err != nil {
		return err
	}
	return conn.Send(command, params...)
}

// SendRaw writes an already-formatted protocol line.
func (c *Client) SendRaw(line string) error {
	conn, err := c.live()
	if err != nil {
		return err
	}
	return conn.SendRaw(line)
}

// SendMessage sends text to target as PRIVMSG, splitting on newlines.
//
// With draft/multiline negotiated the lines go out as one BATCH and come back
// as one message; without it each line is sent and labelled separately, so
// the store still resolves every one of them.
func (c *Client) SendMessage(target, text string) (Handle, error) {
	return c.sendText(target, text, "PRIVMSG")
}

// SendNotice sends text to target as NOTICE. Notices are never batched: a
// multiline notice is not a thing servers expect.
func (c *Client) SendNotice(target, text string) (Handle, error) {
	conn, err := c.live()
	if err != nil {
		return Handle{}, err
	}
	if target == "" {
		return Handle{}, ErrEmptyTarget
	}
	lines := splitLines(text)
	h := Handle{Target: target, Lines: lines}
	for _, ln := range lines {
		label := c.label(Sent{Kind: CmdMessage, Target: target, Lines: 1})
		if label != "" {
			h.Labels = append(h.Labels, label)
		}
		if err := sendBody(conn, labelTags(label), "NOTICE", target, ln); err != nil {
			return h, err
		}
	}
	return h, nil
}

// SendAction sends a CTCP ACTION (/me). A single action cannot contain a line
// break - the newline would make the protocol line unsendable - so they are
// folded to spaces.
func (c *Client) SendAction(target, text string) (Handle, error) {
	conn, err := c.live()
	if err != nil {
		return Handle{}, err
	}
	if target == "" {
		return Handle{}, ErrEmptyTarget
	}
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\n", " "), "\r", " ")
	body := "\x01ACTION " + text + "\x01"
	label := c.label(Sent{Kind: CmdMessage, Target: target, Lines: 1})
	h := Handle{Target: target, Lines: []string{body}}
	if label != "" {
		h.Labels = []string{label}
	}
	return h, sendBody(conn, labelTags(label), "PRIVMSG", target, body)
}

func (c *Client) sendText(target, text, command string) (Handle, error) {
	conn, err := c.live()
	if err != nil {
		return Handle{}, err
	}
	if target == "" {
		return Handle{}, ErrEmptyTarget
	}
	lines := splitLines(text)
	h := Handle{Target: target}

	limits, multiline := c.MultilineLimits()
	if len(lines) > 1 && multiline {
		if maxLines := limits.Lines(); len(lines) > maxLines {
			lines = lines[:maxLines]
			h.Truncated = true
		}
	}
	h.Lines = lines

	if len(lines) > 1 && multiline {
		h.Multiline = true
		label := c.label(Sent{Kind: CmdMessage, Target: target, Lines: len(lines)})
		if label != "" {
			h.Labels = []string{label}
		}
		return h, c.sendMultiline(conn, target, command, lines, label)
	}

	for _, ln := range lines {
		label := c.label(Sent{Kind: CmdMessage, Target: target, Lines: 1})
		if label != "" {
			h.Labels = append(h.Labels, label)
		}
		if err := sendBody(conn, labelTags(label), command, target, ln); err != nil {
			return h, err
		}
	}
	return h, nil
}

// sendMultiline writes one draft/multiline BATCH. The label rides on the
// BATCH command, which is where the server puts the msgid of the assembled
// message on the way back.
func (c *Client) sendMultiline(conn *ircevent.Connection, target, command string, lines []string, label string) error {
	ref := "eco" + strconv.FormatUint(batchSeq.Add(1), 10)
	typ := "draft/multiline"
	if c.Enabled("multiline") && !c.Enabled("draft/multiline") {
		typ = "multiline"
	}
	if err := conn.SendWithTags(labelTags(label), "BATCH", "+"+ref, typ, target); err != nil {
		return err
	}
	tags := map[string]string{"batch": ref}
	for _, ln := range lines {
		if err := sendBody(conn, tags, command, target, ln); err != nil {
			return err
		}
	}
	return conn.Send("BATCH", "-"+ref)
}

// splitLines splits a composed message into wire lines. An empty line cannot
// be sent as a PRIVMSG parameter, so it becomes a single space - the blank
// line is part of what the user typed and dropping it silently reflows their
// message.
func splitLines(text string) []string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i, ln := range lines {
		if ln == "" {
			lines[i] = " "
		}
	}
	return lines
}

// SendTyping sends a draft/typing TAGMSG. It is a no-op without the
// capability rather than an error: typing state is decoration, and a caller
// should not have to check first.
func (c *Client) SendTyping(target string, state model.TypingState) error {
	if target == "" {
		return ErrEmptyTarget
	}
	if !c.Enabled("message-tags") || !c.Enabled("draft/typing") {
		return nil
	}
	conn, err := c.live()
	if err != nil {
		return err
	}
	return conn.SendWithTags(map[string]string{"+typing": state.String()}, "TAGMSG", target)
}

// Redact withdraws one of our own messages (draft/message-redaction).
func (c *Client) Redact(target, msgid, reason string) error {
	conn, err := c.live()
	if err != nil {
		return err
	}
	if target == "" || msgid == "" {
		return ErrEmptyTarget
	}
	label := c.label(Sent{Kind: CmdOther, Target: target})
	if reason == "" {
		return conn.SendWithTags(labelTags(label), "REDACT", target, msgid)
	}
	return sendBody(conn, labelTags(label), "REDACT", target, msgid, reason)
}

// MarkRead moves the server-side read marker for target (draft/read-marker).
// A zero time clears it, which the wire spells "*".
func (c *Client) MarkRead(target string, at time.Time) error {
	conn, err := c.live()
	if err != nil {
		return err
	}
	if target == "" {
		return ErrEmptyTarget
	}
	sel := "*"
	if !at.IsZero() {
		sel = "timestamp=" + at.UTC().Format(historyTimeLayout)
	}
	return conn.Send("MARKREAD", target, sel)
}

// Join joins a channel, with a key when one is known. Rejoining a +k channel
// after a reconnect needs the key, which v1 tracked and then failed to use.
func (c *Client) Join(channel, key string) (Handle, error) {
	conn, err := c.live()
	if err != nil {
		return Handle{}, err
	}
	if channel == "" {
		return Handle{}, ErrEmptyTarget
	}
	label := c.label(Sent{Kind: CmdJoin, Target: channel})
	h := Handle{Target: channel}
	if label != "" {
		h.Labels = []string{label}
	}
	params := []string{channel}
	if key != "" {
		params = append(params, key)
	}
	return h, conn.SendWithTags(labelTags(label), "JOIN", params...)
}

// Part leaves a channel. The label is what lets a 403/442 answer be attributed
// to this command instead of to some unrelated one.
func (c *Client) Part(channel, reason string) (Handle, error) {
	conn, err := c.live()
	if err != nil {
		return Handle{}, err
	}
	if channel == "" {
		return Handle{}, ErrEmptyTarget
	}
	label := c.label(Sent{Kind: CmdPart, Target: channel})
	h := Handle{Target: channel}
	if label != "" {
		h.Labels = []string{label}
	}
	if reason == "" {
		return h, conn.SendWithTags(labelTags(label), "PART", channel)
	}
	return h, sendBody(conn, labelTags(label), "PART", channel, reason)
}

// SetNick asks for a different nick.
func (c *Client) SetNick(nick string) error { return c.Send("NICK", nick) }

// SetTopic sets a channel topic; an empty topic clears it, which the wire
// spells as an empty trailing parameter.
func (c *Client) SetTopic(channel, topic string) error {
	conn, err := c.live()
	if err != nil {
		return err
	}
	return sendBody(conn, nil, "TOPIC", channel, topic)
}

// Kick removes a user from a channel.
func (c *Client) Kick(channel, nick, reason string) error {
	conn, err := c.live()
	if err != nil {
		return err
	}
	if reason == "" {
		return conn.Send("KICK", channel, nick)
	}
	return sendBody(conn, nil, "KICK", channel, nick, reason)
}

// Invite invites a user to a channel.
func (c *Client) Invite(nick, channel string) error { return c.Send("INVITE", nick, channel) }

// Mode issues a MODE command; with no arguments it queries.
func (c *Client) Mode(target string, args ...string) error {
	return c.Send("MODE", append([]string{target}, args...)...)
}

// Names asks for a channel's member list.
func (c *Client) Names(channel string) error { return c.Send("NAMES", channel) }

// Whois asks about a user. The label is how the reply finds its way back to
// the buffer that asked, replacing v1's single global whoisTarget slot.
func (c *Client) Whois(nick string) (Handle, error) {
	conn, err := c.live()
	if err != nil {
		return Handle{}, err
	}
	label := c.label(Sent{Kind: CmdWhois, Target: nick})
	h := Handle{Target: nick}
	if label != "" {
		h.Labels = []string{label}
	}
	return h, conn.SendWithTags(labelTags(label), "WHOIS", nick)
}

// List asks for the channel list.
func (c *Client) List(args ...string) (Handle, error) {
	conn, err := c.live()
	if err != nil {
		return Handle{}, err
	}
	label := c.label(Sent{Kind: CmdList})
	h := Handle{}
	if label != "" {
		h.Labels = []string{label}
	}
	return h, conn.SendWithTags(labelTags(label), "LIST", args...)
}

// --- the command table's verbs ------------------------------------------------
//
// Everything internal/command can put on the wire is a method here. The table
// formats no protocol of its own - that is what keeps ircevent behind this
// package (blueprint §5.3, "all wire writes live in internal/irc"), and what
// lets ~90% of the commands be tested against a fake wire instead of a server.

// Privmsg sends a PRIVMSG and discards the receipt. It is for messages nobody
// typed into a buffer - a services command, a CTCP request - which have no
// pending local row waiting for a labelled echo to resolve it.
func (c *Client) Privmsg(target, text string) error {
	_, err := c.SendMessage(target, text)
	return err
}

// Notice sends a NOTICE and discards the receipt, for the same reason Privmsg
// does: /notice is not echoed into the buffer it was typed in.
func (c *Client) Notice(target, text string) error {
	_, err := c.SendNotice(target, text)
	return err
}

// Ctcp sends a CTCP request (/ctcp nick VERSION). The \x01 framing and the
// upper-casing happen here rather than at the call site: both are protocol.
func (c *Client) Ctcp(target, request string) error {
	return c.Privmsg(target, "\x01"+strings.ToUpper(request)+"\x01")
}

// Topic asks for a channel's current topic. SetTopic changes it.
func (c *Client) Topic(channel string) error { return c.Send("TOPIC", channel) }

// Away marks us away with a reason; an empty reason marks us back.
func (c *Client) Away(reason string) error {
	if reason == "" {
		return c.Send("AWAY")
	}
	conn, err := c.live()
	if err != nil {
		return err
	}
	return sendBody(conn, nil, "AWAY", reason)
}

// SetName changes the realname on a live connection (setname capability).
func (c *Client) SetName(realname string) error {
	conn, err := c.live()
	if err != nil {
		return err
	}
	return sendBody(conn, nil, "SETNAME", realname)
}

// Register asks the server to register an account for us
// (draft/account-registration). An account of "*" means "this nickname", and an
// email of "*" means the server should not ask for one.
func (c *Client) Register(account, email, password string) error {
	conn, err := c.live()
	if err != nil {
		return err
	}
	return sendBody(conn, nil, "REGISTER", account, email, password)
}

// Verify completes a registration with the code the server sent out.
func (c *Client) Verify(account, code string) error { return c.Send("VERIFY", account, code) }

// Oper authenticates as a server operator.
func (c *Client) Oper(name, password string) error { return c.Send("OPER", name, password) }

// Who asks about a nick, a channel or a mask.
func (c *Client) Who(target string) error { return c.Send("WHO", target) }

// Whowas asks about a nick that has already left.
func (c *Client) Whowas(nick string) error { return c.Send("WHOWAS", nick) }

// Knock asks for an invitation to an invite-only channel.
func (c *Client) Knock(channel string) error { return c.Send("KNOCK", channel) }

// Wallops broadcasts to everyone wearing the +w mode (operators).
func (c *Client) Wallops(text string) error {
	conn, err := c.live()
	if err != nil {
		return err
	}
	return sendBody(conn, nil, "WALLOPS", text)
}

// Kill disconnects another user (operators).
func (c *Client) Kill(nick, reason string) error {
	conn, err := c.live()
	if err != nil {
		return err
	}
	if reason == "" {
		return conn.Send("KILL", nick)
	}
	return sendBody(conn, nil, "KILL", nick, reason)
}

// Quit writes the user's own parting message. Close sends the configured one
// and tears the connection down; this is what /quit says first.
func (c *Client) Quit(reason string) error {
	conn, err := c.live()
	if err != nil {
		return err
	}
	return sendBody(conn, nil, "QUIT", reason)
}

// ServerQuery issues one of the informational commands whose entire reply is
// numerics eco renders straight into the network log (MOTD, TIME, VERSION,
// STATS, ISON, …). Which verbs may reach it is the command table's decision,
// not this one's - this is the shape of the write, not a policy.
func (c *Client) ServerQuery(command string, params ...string) error {
	return c.Send(strings.ToUpper(command), params...)
}

// historyTimeLayout is the timestamp format CHATHISTORY and MARKREAD use.
const historyTimeLayout = "2006-01-02T15:04:05.000Z"

// HistoryRequest is one CHATHISTORY query.
type HistoryRequest struct {
	// Target is the buffer to fetch, "*" for every target the server knows.
	Target string
	// Direction selects the subcommand: LATEST, BEFORE, AFTER or AROUND.
	Direction model.HistoryDirection
	// MsgID anchors the walk exactly; Time anchors it approximately. MsgID
	// wins when both are set, because a msgid is a position and a timestamp
	// is only a guess at one.
	MsgID string
	Time  time.Time
	// Limit is how many messages to ask for. Zero means the server's default
	// bounded by its advertised CHATHISTORY limit.
	Limit int
}

// History issues a CHATHISTORY request. The reply arrives as a chathistory
// BATCH, which eco delivers as one HistoryChunk carrying this request's
// direction and whether the server truncated it - neither of which the batch
// itself states, which is why the request is remembered against its label.
func (c *Client) History(req HistoryRequest) (Handle, error) {
	conn, err := c.live()
	if err != nil {
		return Handle{}, err
	}
	if req.Target == "" {
		return Handle{}, ErrEmptyTarget
	}
	sub := "LATEST"
	switch req.Direction {
	case model.HistoryBefore:
		sub = "BEFORE"
	case model.HistoryAfter:
		sub = "AFTER"
	case model.HistoryAround:
		sub = "AROUND"
	}
	limit := req.Limit
	if limit <= 0 {
		limit = defaultHistoryLimit
	}
	if maxLimit := c.props.Load().ChatHistoryLimit; maxLimit > 0 && limit > maxLimit {
		limit = maxLimit
	}

	label := c.label(Sent{Kind: CmdHistory, Target: req.Target,
		Direction: req.Direction, Limit: limit})
	h := Handle{Target: req.Target}
	if label != "" {
		h.Labels = []string{label}
	}
	return h, conn.SendWithTags(labelTags(label), "CHATHISTORY",
		sub, req.Target, historySelector(req), strconv.Itoa(limit))
}

// defaultHistoryLimit is what eco asks for when the caller does not say. It
// is capped again by the server's advertised CHATHISTORY limit.
const defaultHistoryLimit = 100

func historySelector(req HistoryRequest) string {
	switch {
	case req.MsgID != "":
		return "msgid=" + req.MsgID
	case !req.Time.IsZero():
		return "timestamp=" + req.Time.UTC().Format(historyTimeLayout)
	default:
		return "*"
	}
}

// HistoryTargets asks which conversations have activity in a time range -
// the way a client discovers private messages that arrived while it was
// offline. The reply is a chathistory-targets batch, delivered as its events.
func (c *Client) HistoryTargets(from, to time.Time, limit int) (Handle, error) {
	conn, err := c.live()
	if err != nil {
		return Handle{}, err
	}
	if limit <= 0 {
		limit = defaultHistoryLimit
	}
	label := c.label(Sent{Kind: CmdHistory, Target: "*", Limit: limit})
	h := Handle{Target: "*"}
	if label != "" {
		h.Labels = []string{label}
	}
	return h, conn.SendWithTags(labelTags(label), "CHATHISTORY", "TARGETS",
		"timestamp="+from.UTC().Format(historyTimeLayout),
		"timestamp="+to.UTC().Format(historyTimeLayout),
		strconv.Itoa(limit))
}
