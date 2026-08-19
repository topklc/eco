package proto

import (
	"strings"
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/model"
)

func TestDefaultCapRequestIsFresh(t *testing.T) {
	a := DefaultCapRequest()
	b := DefaultCapRequest()
	if len(a) == 0 {
		t.Fatal("no capabilities requested")
	}
	a[0] = "mutated"
	if b[0] == "mutated" {
		t.Error("DefaultCapRequest hands out a shared slice")
	}
	// The capabilities the rest of v2 is built on must be in the list.
	for _, must := range []string{"batch", "labeled-response", "message-tags", "server-time", "multi-prefix", "echo-message", "sasl", "cap-notify"} {
		if !contains(b, must) {
			t.Errorf("%q missing from the default request", must)
		}
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestParseCapTokens(t *testing.T) {
	cases := []struct {
		in   string
		want []model.Cap
	}{
		{"", nil},
		{"multi-prefix", []model.Cap{{Name: "multi-prefix"}}},
		{
			"sasl=PLAIN,EXTERNAL draft/multiline=max-bytes=4096,max-lines=24",
			[]model.Cap{
				{Name: "sasl", Value: "PLAIN,EXTERNAL"},
				{Name: "draft/multiline", Value: "max-bytes=4096,max-lines=24"},
			},
		},
		{"  spaced   out  ", []model.Cap{{Name: "spaced"}, {Name: "out"}}},
		{"empty=", []model.Cap{{Name: "empty", Value: ""}}},
	}
	for _, tc := range cases {
		got := ParseCapTokens(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("ParseCapTokens(%q) = %+v, want %+v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("ParseCapTokens(%q)[%d] = %+v, want %+v", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}

// TestCapNegotiation walks a whole negotiation: a split LS, a REQ, an ACK with
// a partial refusal, then NEW and DEL after registration.
func TestCapNegotiation(t *testing.T) {
	cs := NewCapSet([]string{"batch", "sasl", "labeled-response", "draft/multiline", "server-time"})

	// First LS line is a continuation: nothing may be requested yet, because
	// the next line could offer something we want.
	if got := cs.LS(ParseCapTokens("batch server-time"), true); got != nil {
		t.Errorf("continuation LS returned %v, want nothing", got)
	}
	if cs.Complete() {
		t.Error("advertisement should not be complete after a continuation line")
	}

	// Final line completes the advertisement.
	want := []string{"batch", "draft/multiline", "labeled-response", "sasl", "server-time"}
	got := cs.LS(ParseCapTokens("sasl=PLAIN,EXTERNAL draft/multiline=max-lines=24 labeled-response away-notify"), false)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("LS request = %v, want %v", got, want)
	}
	if !cs.Complete() {
		t.Error("advertisement should be complete")
	}
	// away-notify was advertised but not wanted, so it is not requested.
	if contains(got, "away-notify") {
		t.Error("requested a capability we do not want")
	}
	if _, ok := cs.Advertised("away-notify"); !ok {
		t.Error("an unwanted capability is still advertised")
	}

	// The server ACKs most of them and NAKs one.
	cs.Ack([]string{"batch", "sasl", "labeled-response", "server-time"})
	cs.Nak([]string{"draft/multiline"})

	for _, name := range []string{"batch", "sasl", "labeled-response", "server-time"} {
		if !cs.Enabled(name) {
			t.Errorf("%q should be enabled", name)
		}
	}
	if cs.Enabled("draft/multiline") {
		t.Error("a NAKed capability must not be enabled")
	}
	// A NAKed capability must not be asked for again.
	if got := cs.ToRequest(); len(got) != 0 {
		t.Errorf("ToRequest after a full ACK/NAK = %v, want nothing", got)
	}
	// Values survive the ACK.
	if v, _ := cs.Value("sasl"); v != "PLAIN,EXTERNAL" {
		t.Errorf("sasl value = %q", v)
	}

	// CAP NEW offers something we wanted all along.
	if got := cs.New(ParseCapTokens("draft/multiline=max-lines=8 unwanted")); strings.Join(got, ",") != "draft/multiline" {
		t.Errorf("NEW request = %v, want [draft/multiline]", got)
	}
	cs.Ack([]string{"draft/multiline"})
	if lim, ok := cs.Multiline(); !ok || lim.MaxLines != 8 {
		t.Errorf("multiline limits = %+v, %v", lim, ok)
	}

	// CAP DEL takes it away again and says so.
	lost := cs.Del([]string{"draft/multiline", "never-had-it"})
	if strings.Join(lost, ",") != "draft/multiline" {
		t.Errorf("DEL reported %v, want [draft/multiline]", lost)
	}
	if cs.Enabled("draft/multiline") {
		t.Error("a DELed capability must not stay enabled")
	}
	if _, ok := cs.Advertised("draft/multiline"); ok {
		t.Error("a DELed capability must not stay advertised")
	}
}

func TestCapAckDisable(t *testing.T) {
	cs := NewCapSet([]string{"echo-message"})
	cs.LS(ParseCapTokens("echo-message"), false)
	cs.Ack([]string{"echo-message"})
	if !cs.Enabled("echo-message") {
		t.Fatal("should be enabled")
	}
	// "CAP ACK :-echo-message" confirms a disable we asked for.
	cs.Ack([]string{"-echo-message"})
	if cs.Enabled("echo-message") {
		t.Error("ACK -name must disable the capability")
	}
	// It is still advertised, so it can be asked for again.
	if got := cs.ToRequest(); strings.Join(got, ",") != "echo-message" {
		t.Errorf("ToRequest = %v", got)
	}
}

func TestCapReset(t *testing.T) {
	cs := NewCapSet([]string{"batch"})
	cs.LS(ParseCapTokens("batch"), false)
	cs.Ack([]string{"batch"})
	cs.Reset()

	if cs.Enabled("batch") || cs.Complete() {
		t.Error("Reset must clear the negotiated and advertised state")
	}
	if len(cs.AvailableNames()) != 0 || len(cs.EnabledNames()) != 0 {
		t.Error("Reset left state behind")
	}
	// The wanted set survives: it is configuration, not connection state.
	if strings.Join(cs.Wanted(), ",") != "batch" {
		t.Errorf("Reset lost the wanted set: %v", cs.Wanted())
	}
}

func TestCapClone(t *testing.T) {
	cs := NewCapSet([]string{"batch", "sasl"})
	cs.LS(ParseCapTokens("batch sasl=PLAIN"), false)
	cs.Ack([]string{"batch"})

	c := cs.Clone()
	c.Ack([]string{"sasl"})
	c.Del([]string{"batch"})

	if !cs.Enabled("batch") {
		t.Error("mutating the clone disabled a capability on the original")
	}
	if cs.Enabled("sasl") {
		t.Error("mutating the clone enabled a capability on the original")
	}
}

func TestCapSortedAccessors(t *testing.T) {
	cs := NewCapSet([]string{"zeta", "alpha", "mid"})
	cs.LS(ParseCapTokens("zeta alpha mid"), false)
	cs.Ack([]string{"zeta", "alpha", "mid"})

	for _, got := range [][]string{cs.Wanted(), cs.AvailableNames(), cs.EnabledNames()} {
		if strings.Join(got, ",") != "alpha,mid,zeta" {
			t.Errorf("accessor not sorted: %v", got)
		}
	}
}

func TestCapValues(t *testing.T) {
	cases := []struct {
		in   string
		want map[string]string
	}{
		{"", map[string]string{}},
		{"max-lines=24", map[string]string{"max-lines": "24"}},
		{"max-bytes=4096,max-lines=24", map[string]string{"max-bytes": "4096", "max-lines": "24"}},
		{"preload,duration=100", map[string]string{"preload": "", "duration": "100"}},
		{",,a=1,,", map[string]string{"a": "1"}},
	}
	for _, tc := range cases {
		got := CapValues(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("CapValues(%q) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for k, v := range tc.want {
			if got[k] != v {
				t.Errorf("CapValues(%q)[%q] = %q, want %q", tc.in, k, got[k], v)
			}
		}
	}
}

func TestParseMultiline(t *testing.T) {
	cases := []struct {
		in        string
		bytes     int
		lines     int
		effective int
	}{
		{"", 0, 0, DefaultMultilineMaxLines},
		{"max-bytes=4096,max-lines=24", 4096, 24, 24},
		{"max-lines=3", 0, 3, 3},
		{"max-bytes=4096", 4096, 0, DefaultMultilineMaxLines},
		{"max-lines=notanumber", 0, 0, DefaultMultilineMaxLines},
	}
	for _, tc := range cases {
		got := ParseMultiline(tc.in)
		if got.MaxBytes != tc.bytes || got.MaxLines != tc.lines {
			t.Errorf("ParseMultiline(%q) = %+v, want bytes %d lines %d", tc.in, got, tc.bytes, tc.lines)
		}
		if got.Lines() != tc.effective {
			t.Errorf("ParseMultiline(%q).Lines() = %d, want %d", tc.in, got.Lines(), tc.effective)
		}
	}
}

func TestMultilineNotEnabled(t *testing.T) {
	cs := NewCapSet(nil)
	if _, ok := cs.Multiline(); ok {
		t.Error("multiline reported as enabled when it is not")
	}
}

func TestSASLMechanisms(t *testing.T) {
	cs := NewCapSet([]string{"sasl"})
	cs.LS(ParseCapTokens("sasl=plain,external,SCRAM-SHA-256"), false)
	cs.Ack([]string{"sasl"})

	mechs, ok := cs.SASLMechanisms()
	if !ok {
		t.Fatal("sasl should be enabled")
	}
	if strings.Join(mechs, ",") != "PLAIN,EXTERNAL,SCRAM-SHA-256" {
		t.Errorf("mechanisms = %v", mechs)
	}
	for _, m := range []string{"PLAIN", "plain", "SCRAM-SHA-256"} {
		if !cs.SupportsSASL(m) {
			t.Errorf("SupportsSASL(%q) = false", m)
		}
	}
	if cs.SupportsSASL("ANONYMOUS") {
		t.Error("SupportsSASL accepted an unadvertised mechanism")
	}
}

func TestSASLWithoutMechanismList(t *testing.T) {
	// A server may enable sasl without naming mechanisms; then anything goes.
	cs := NewCapSet([]string{"sasl"})
	cs.LS(ParseCapTokens("sasl"), false)
	cs.Ack([]string{"sasl"})

	mechs, ok := cs.SASLMechanisms()
	if !ok || len(mechs) != 0 {
		t.Fatalf("mechanisms = %v, %v", mechs, ok)
	}
	if !cs.SupportsSASL("SCRAM-SHA-256") {
		t.Error("with no advertised list, every mechanism is worth trying")
	}
}

func TestSASLNotEnabled(t *testing.T) {
	cs := NewCapSet(nil)
	if _, ok := cs.SASLMechanisms(); ok {
		t.Error("sasl reported as enabled when it is not")
	}
	if cs.SupportsSASL("PLAIN") {
		t.Error("SupportsSASL true with sasl disabled")
	}
}

func TestParseSTSPolicy(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    STSPolicy
		wantErr bool
	}{
		{
			name: "plaintext connection is told where to reconnect",
			in:   "port=6697",
			want: STSPolicy{Port: 6697, HasPort: true},
		},
		{
			name: "secure connection is told how long to remember",
			in:   "duration=2592000",
			want: STSPolicy{Duration: 30 * 24 * time.Hour, HasDuration: true},
		},
		{
			name: "preload flag",
			in:   "duration=100,preload",
			want: STSPolicy{Duration: 100 * time.Second, HasDuration: true, Preload: true},
		},
		{
			name: "duration zero is a revocation, not an absent duration",
			in:   "duration=0",
			want: STSPolicy{Duration: 0, HasDuration: true},
		},
		{
			name: "empty value",
			in:   "",
			want: STSPolicy{},
		},
		{
			name:    "bad port is reported",
			in:      "port=nope",
			want:    STSPolicy{},
			wantErr: true,
		},
		{
			name:    "out-of-range port is reported",
			in:      "port=70000",
			want:    STSPolicy{},
			wantErr: true,
		},
		{
			name:    "negative duration is reported",
			in:      "duration=-1",
			want:    STSPolicy{},
			wantErr: true,
		},
		{
			name:    "a bad field does not discard the good one",
			in:      "port=oops,duration=60",
			want:    STSPolicy{Duration: time.Minute, HasDuration: true},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseSTSPolicy(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("ParseSTSPolicy(%q) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}

	// Revocation must be distinguishable from "no duration advertised".
	rev, _ := ParseSTSPolicy("duration=0")
	if !rev.Revocation() {
		t.Error("duration=0 is a revocation")
	}
	none, _ := ParseSTSPolicy("port=6697")
	if none.Revocation() {
		t.Error("an absent duration is not a revocation")
	}
}

func TestCapSetSTS(t *testing.T) {
	cs := NewCapSet(DefaultCapRequest())
	// sts is never requested, only advertised - so it must be readable from the
	// advertised set, not the enabled one.
	cs.LS(ParseCapTokens("sts=duration=2592000,preload batch"), false)
	cs.Ack([]string{"batch"})

	p, ok, err := cs.STS()
	if err != nil || !ok {
		t.Fatalf("STS() = %+v, %v, %v", p, ok, err)
	}
	if !p.HasDuration || p.Duration != 30*24*time.Hour || !p.Preload {
		t.Errorf("policy = %+v", p)
	}
	if cs.Enabled("sts") {
		t.Error("sts is not a capability we request")
	}

	none := NewCapSet(nil)
	none.LS(ParseCapTokens("batch"), false)
	if _, ok, _ := none.STS(); ok {
		t.Error("STS() reported a policy that was never advertised")
	}
}
