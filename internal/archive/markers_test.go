package archive

import (
	"testing"
	"time"

	"github.com/toprakkilic/eco/internal/model"
)

// TestMergeMarker is the conflict table. ADR-3 says max(server, local): a
// marker only ever moves forward, so a MARKREAD from a phone that was behind —
// or a late local focus event — cannot un-read a buffer.
func TestMergeMarker(t *testing.T) {
	cases := []struct {
		name string
		cur  Marker
		in   Marker
		want Marker
	}{
		{
			name: "the first marker wins by default",
			cur:  Marker{}, in: Marker{MsgID: "a", At: at(5)},
			want: Marker{MsgID: "a", At: at(5)},
		},
		{
			name: "a later marker moves it forward",
			cur:  Marker{MsgID: "a", At: at(5)}, in: Marker{MsgID: "b", At: at(9)},
			want: Marker{MsgID: "b", At: at(9)},
		},
		{
			name: "an earlier marker is ignored — the other device was behind",
			cur:  Marker{MsgID: "b", At: at(9)}, in: Marker{MsgID: "a", At: at(5)},
			want: Marker{MsgID: "b", At: at(9)},
		},
		{
			name: "the same instant keeps the position and takes the sharper id",
			cur:  Marker{At: at(5)}, in: Marker{MsgID: "a", At: at(5)},
			want: Marker{MsgID: "a", At: at(5)},
		},
		{
			name: "the same instant does not replace an id we already have",
			cur:  Marker{MsgID: "a", At: at(5)}, in: Marker{MsgID: "z", At: at(5)},
			want: Marker{MsgID: "a", At: at(5)},
		},
		{
			name: "a marker with no timestamp only adds information",
			cur:  Marker{At: at(5)}, in: Marker{MsgID: "a"},
			want: Marker{MsgID: "a", At: at(5)},
		},
		{
			name: "a marker with no timestamp cannot move a positioned one",
			cur:  Marker{MsgID: "a", At: at(5)}, in: Marker{MsgID: "z"},
			want: Marker{MsgID: "a", At: at(5)},
		},
		{
			name: "a timestamped marker beats an id-only one",
			cur:  Marker{MsgID: "a"}, in: Marker{MsgID: "b", At: at(2)},
			want: Marker{MsgID: "b", At: at(2)},
		},
		{
			name: "nothing arriving changes nothing",
			cur:  Marker{MsgID: "a", At: at(5)}, in: Marker{},
			want: Marker{MsgID: "a", At: at(5)},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mergeMarker(tc.cur, tc.in); !got.Equal(tc.want) {
				t.Errorf("mergeMarker(%+v, %+v) = %+v, want %+v", tc.cur, tc.in, got, tc.want)
			}
		})
	}
}

// TestReadMarkerRoundTrip pins that the merge rule is what the disk applies,
// not just what a pure function says, and that a marker survives a restart —
// which is the whole point of storing it.
func TestReadMarkerRoundTrip(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)

	if m, err := db.ReadMarker(ctx, "#eco"); err != nil || !m.IsZero() {
		t.Fatalf("a buffer with no marker = %+v, %v, want the zero marker", m, err)
	}

	got, moved, err := db.SetReadMarker(ctx, "#eco", Marker{MsgID: "m5", At: at(5)})
	if err != nil || !moved {
		t.Fatalf("first SetReadMarker = %+v, moved=%v, err=%v", got, moved, err)
	}

	// The phone was behind: this must not un-read the buffer.
	got, moved, err = db.SetReadMarker(ctx, "#eco", Marker{MsgID: "m2", At: at(2)})
	if err != nil {
		t.Fatal(err)
	}
	if moved {
		t.Error("a stale marker reported that it moved the position")
	}
	if !got.Equal(Marker{MsgID: "m5", At: at(5)}) {
		t.Errorf("after a stale marker the position is %+v, want m5@%s", got, at(5))
	}

	// The phone caught up.
	got, moved, err = db.SetReadMarker(ctx, "#eco", Marker{MsgID: "m9", At: at(9)})
	if err != nil || !moved || !got.Equal(Marker{MsgID: "m9", At: at(9)}) {
		t.Fatalf("a newer marker = %+v, moved=%v, err=%v", got, moved, err)
	}

	// Restart.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := Open(db.Path(), Options{Clock: model.FixedClock(epoch)})
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	back, err := again.ReadMarker(ctx, "#eco")
	if err != nil {
		t.Fatal(err)
	}
	if !back.Equal(Marker{MsgID: "m9", At: at(9)}) {
		t.Errorf("after a restart the marker is %+v, want m9@%s", back, at(9))
	}
}

// TestMarkerRoundsToMilliseconds pins that what the caller gets back is what
// the column can hold, so a comparison against a stored marker never fails on
// a nanosecond that was never written.
func TestMarkerRoundsToMilliseconds(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)

	precise := epoch.Add(1500*time.Millisecond + 999*time.Nanosecond)
	got, moved, err := db.SetReadMarker(ctx, "#eco", Marker{MsgID: "m1", At: precise})
	if err != nil || !moved {
		t.Fatal(err)
	}
	back, err := db.ReadMarker(ctx, "#eco")
	if err != nil {
		t.Fatal(err)
	}
	if !back.Equal(got) {
		t.Errorf("SetReadMarker returned %s but the disk holds %s", got.At, back.At)
	}
	if back.At.UnixMilli() != precise.UnixMilli() {
		t.Errorf("marker stored %d ms, want %d", back.At.UnixMilli(), precise.UnixMilli())
	}

	// Setting the same marker again is not a move.
	if _, moved, err := db.SetReadMarker(ctx, "#eco", Marker{MsgID: "m1", At: precise}); err != nil || moved {
		t.Errorf("re-setting the same marker reported moved=%v (%v)", moved, err)
	}
}

// TestMarkerOnAnUnknownBufferIsKept pins that a marker for a buffer the
// archive has never stored a message for still lands — a channel can be read
// on the phone before this client ever sees a line in it.
func TestMarkerOnAnUnknownBufferIsKept(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)

	if _, _, err := db.SetReadMarker(ctx, "#never-joined", Marker{At: at(3)}); err != nil {
		t.Fatal(err)
	}
	b, ok, err := db.Buffer(ctx, "#never-joined")
	if err != nil || !ok {
		t.Fatalf("Buffer = %+v, ok=%v, err=%v", b, ok, err)
	}
	if !b.Marker.Equal(Marker{At: at(3)}) {
		t.Errorf("marker = %+v, want @%s", b.Marker, at(3))
	}
	if !b.Logging {
		t.Error("a buffer created by a marker has logging off; the default is on")
	}
	if b.Kind != KindUnknown {
		t.Errorf("kind = %v, want KindUnknown until something says otherwise", b.Kind)
	}

	if _, _, err := db.SetReadMarker(ctx, "", Marker{At: at(3)}); err == nil {
		t.Error("SetReadMarker with no buffer name succeeded")
	}
}

// TestBufferMetaSurvives pins the rest of the buffers row: the settings the UI
// sets are the ones it reads back, and identity updates do not clobber them.
func TestBufferMetaSurvives(t *testing.T) {
	db := openTestDB(t)
	ctx := testCtx(t)

	if err := db.EnsureBuffer(ctx, BufferMeta{Name: "#eco", Display: "#eco", Kind: model.BufferChannel, Key: "sekrit"}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetNotifyLevel(ctx, "#eco", 3); err != nil {
		t.Fatal(err)
	}
	if err := db.SetPinned(ctx, "#eco", true); err != nil {
		t.Fatal(err)
	}
	if err := db.SetLogging(ctx, "#eco", false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.SetReadMarker(ctx, "#eco", Marker{MsgID: "m1", At: at(4)}); err != nil {
		t.Fatal(err)
	}

	// The server re-cases the channel: identity changes, settings do not.
	// Note the zero-valued Kind, which is model.BufferServer: a caller that
	// only knows the new display name must not be able to relabel a channel
	// as the status buffer by leaving a field out.
	if err := db.EnsureBuffer(ctx, BufferMeta{Name: "#eco", Display: "#ECO"}); err != nil {
		t.Fatal(err)
	}

	b, ok, err := db.Buffer(ctx, "#eco")
	if err != nil || !ok {
		t.Fatalf("Buffer = %v, %v", ok, err)
	}
	if b.Display != "#ECO" {
		t.Errorf("display = %q, want the new casing", b.Display)
	}
	if b.Kind != model.BufferChannel {
		t.Errorf("kind = %v, want channel to survive an identity update", b.Kind)
	}
	if b.Key != "sekrit" {
		t.Errorf("key = %q, want it to survive", b.Key)
	}
	if b.NotifyLevel != 3 || !b.Pinned || b.Logging {
		t.Errorf("settings = notify %d, pinned %v, logging %v; want 3, true, false", b.NotifyLevel, b.Pinned, b.Logging)
	}
	if !b.Marker.Equal(Marker{MsgID: "m1", At: at(4)}) {
		t.Errorf("marker = %+v, want m1@%s", b.Marker, at(4))
	}

	all, err := db.Buffers(ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("Buffers = %d rows, %v", len(all), err)
	}
	off, err := db.LoggingOff(ctx)
	if err != nil || !off["#eco"] {
		t.Errorf("LoggingOff = %v, %v", off, err)
	}
	if err := db.EnsureBuffer(ctx, BufferMeta{}); err == nil {
		t.Error("EnsureBuffer with no name succeeded")
	}

	// The status buffer is the one place kind server is writable, under the
	// name the store's empty-named buffer maps onto.
	if err := db.EnsureBuffer(ctx, BufferMeta{
		Name: FoldedName(model.ServerBuffer("libera")), Display: "libera", Kind: model.BufferServer,
	}); err != nil {
		t.Fatal(err)
	}
	sb, ok, err := db.Buffer(ctx, ServerBufferName)
	if err != nil || !ok || sb.Kind != model.BufferServer {
		t.Errorf("the status buffer = %+v, ok=%v, err=%v; want kind server under %q", sb, ok, err, ServerBufferName)
	}
	if _, ok, err := db.Buffer(ctx, "#nothing"); err != nil || ok {
		t.Errorf("Buffer of an unknown name = ok %v, %v; want a clean miss", ok, err)
	}
}
