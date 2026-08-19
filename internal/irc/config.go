package irc

import (
	"context"
	"crypto/tls"
	"math"
	"net"
	"time"

	"github.com/toprakkilic/eco/internal/model"
	"github.com/toprakkilic/eco/internal/proto"
)

// SASL mechanism names, spelled the way the wire spells them.
const (
	MechPlain    = "PLAIN"
	MechExternal = "EXTERNAL"
	MechScram    = "SCRAM-SHA-256"
)

// SASL is the authentication half of a network's configuration.
//
// PLAIN and EXTERNAL are performed inside registration. SCRAM-SHA-256 runs
// after registration, under this package's control, because that is the only
// place it can run - and because sequencing it explicitly is what lets
// ConnReady wait for authentication to settle instead of racing it.
type SASL struct {
	// Mechanism is "" (no SASL), MechPlain, MechExternal or MechScram.
	Mechanism string
	// User and Pass are the credentials. EXTERNAL (CertFP) needs neither:
	// it authenticates with the TLS client certificate in TLSConfig.
	User string
	Pass string
	// Optional downgrades a failed authentication from fatal to a warning.
	// The default is fatal, because silently continuing unauthenticated is
	// how a client ends up with no cloak, no +R channels, and no explanation.
	Optional bool
}

// Enabled reports whether authentication was configured at all.
func (s SASL) Enabled() bool {
	switch s.Mechanism {
	case MechPlain, MechScram:
		return s.User != ""
	case MechExternal:
		return true
	}
	return false
}

// external reports whether the mechanism authenticates with a client
// certificate rather than a password.
func (s SASL) external() bool { return s.Mechanism == MechExternal }

// scram reports whether the mechanism runs after registration.
func (s SASL) scram() bool { return s.Mechanism == MechScram && s.User != "" }

// Backoff is the reconnect schedule: a geometric ladder with proportional
// jitter, replacing v1's flat 30-second retry.
//
// The zero value is usable and means the defaults below.
type Backoff struct {
	// Initial is the wait after the first failed attempt.
	Initial time.Duration
	// Max caps the ladder.
	Max time.Duration
	// Factor multiplies the wait per consecutive failure.
	Factor float64
	// Jitter is the fraction of the computed delay the result may vary by,
	// in both directions: 0.25 means the delay lands in [0.75d, 1.25d].
	// Jitter is what keeps a whole network's worth of clients from
	// reconnecting in lockstep after a server restart.
	Jitter float64
}

// Backoff defaults. They are exported so a test can assert against the
// schedule instead of restating it.
const (
	DefaultBackoffInitial = 1 * time.Second
	DefaultBackoffMax     = 5 * time.Minute
	DefaultBackoffFactor  = 2.0
	DefaultBackoffJitter  = 0.25
)

func (b Backoff) withDefaults() Backoff {
	if b.Initial <= 0 {
		b.Initial = DefaultBackoffInitial
	}
	if b.Max <= 0 {
		b.Max = DefaultBackoffMax
	}
	if b.Factor < 1 {
		b.Factor = DefaultBackoffFactor
	}
	if b.Jitter < 0 {
		b.Jitter = 0
	}
	if b.Jitter > 1 {
		b.Jitter = 1
	}
	return b
}

// Delay returns the wait before attempt+1, given that `attempt` consecutive
// attempts have failed (attempt is 1-based; anything below 1 is treated as 1).
// rnd must return a value in [0,1); a nil rnd means no jitter.
func (b Backoff) Delay(attempt int, rnd func() float64) time.Duration {
	b = b.withDefaults()
	if attempt < 1 {
		attempt = 1
	}
	// pow on float64 rather than repeated multiplication: the ladder is
	// capped immediately afterwards, so overflow is not reachable.
	d := float64(b.Initial) * math.Pow(b.Factor, float64(attempt-1))
	if d > float64(b.Max) || math.IsInf(d, 1) {
		d = float64(b.Max)
	}
	if b.Jitter > 0 && rnd != nil {
		// rnd()*2-1 spreads over [-1,1); 0.5 is the exact centre, which is
		// why a test that wants the un-jittered ladder injects rnd = 0.5.
		d *= 1 + b.Jitter*(rnd()*2-1)
	}
	if d < 0 {
		d = 0
	}
	return time.Duration(d)
}

// Config is everything a Client needs. It is copied by New; changing it
// afterwards has no effect.
type Config struct {
	// Network is the stable id stamped on every event this client emits. It
	// is what makes the stream routable in a multi-network client.
	Network model.NetworkID

	// Server is a hostname, or a ws:// / wss:// URL for the IRCv3 websocket
	// transport (internal/irc/transport owns that case).
	Server string
	// Port is ignored for websocket URLs.
	Port string
	// TLS selects a TLS connection. It is ignored for websocket URLs, where
	// the scheme decides.
	TLS bool
	// TLSConfig overrides the default (ServerName from Server, MinVersion
	// 1.2). CertFP authentication puts its client certificate here.
	TLSConfig *tls.Config

	// ServerPass is the PASS command's argument.
	ServerPass string

	Nick     string
	User     string
	RealName string

	SASL SASL

	// Caps is the capability wish list. nil means proto.DefaultCapRequest().
	Caps []string

	Backoff Backoff

	QuitMessage string
	Version     string

	// Timeout bounds each registration step and the keepalive PING.
	Timeout time.Duration
	// KeepAlive is the PING interval; it must be at least Timeout.
	KeepAlive time.Duration

	// EventBuffer sizes the event channel. A full channel applies
	// backpressure to the reader goroutine rather than dropping events.
	EventBuffer int

	// EmitRaw adds a model.Raw event for every inbound line, for the raw
	// protocol view. Outgoing lines are not mirrored: the sender already
	// holds them, and emitting from the caller's goroutine would break the
	// single-emitter ordering guarantee.
	EmitRaw bool

	// Debug lets the underlying library log to stdout. Off by default, which
	// also silences its "unrecognized label" chatter - eco correlates labels
	// itself, so the library is expected not to recognise them.
	Debug bool

	// AllowPanic lets a panic in the decoder escape instead of being caught
	// and logged. Tests set it so a decode bug fails loudly; production leaves
	// it false, because dropping one line beats taking the window down.
	AllowPanic bool

	// --- test seams; production leaves these nil ---

	// Dial replaces the dialer, including the websocket transport.
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
	// Clock supplies "now" for events without a server-time tag. It is read
	// from more than one goroutine, so it must be safe for concurrent use:
	// model.FixedClock is, model.StepClock is not.
	Clock model.Clock
	// Timer replaces the backoff sleep.
	Timer func(d time.Duration) <-chan time.Time
	// Rand is the jitter source; it must return a value in [0,1).
	Rand func() float64
}

// Client defaults.
const (
	defaultEventBuffer = 1024
	defaultTimeout     = 30 * time.Second
	defaultKeepAlive   = 2 * time.Minute
	// The library's own reconnect loop is never allowed to fire: this
	// Client drives reconnection so the schedule is ours and testable.
	// Loop() is only ever entered after Quit(), where the delay is unused.
	noAutoReconnect = 24 * time.Hour
)

func (c Config) withDefaults() Config {
	if c.Nick == "" {
		c.Nick = "eco"
	}
	if c.User == "" {
		c.User = c.Nick
	}
	if c.RealName == "" {
		c.RealName = c.Nick
	}
	if c.Caps == nil {
		c.Caps = proto.DefaultCapRequest()
	}
	if c.Version == "" {
		c.Version = "eco"
	}
	if c.Timeout <= 0 {
		c.Timeout = defaultTimeout
	}
	if c.KeepAlive < c.Timeout {
		c.KeepAlive = defaultKeepAlive
	}
	if c.KeepAlive < c.Timeout {
		c.KeepAlive = c.Timeout
	}
	if c.EventBuffer <= 0 {
		c.EventBuffer = defaultEventBuffer
	}
	if c.Clock == nil {
		c.Clock = model.SystemClock()
	}
	if c.Timer == nil {
		c.Timer = time.After
	}
	c.Backoff = c.Backoff.withDefaults()
	return c
}
