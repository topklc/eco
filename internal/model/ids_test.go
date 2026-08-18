package model

import (
	"sync"
	"testing"
)

func TestBufferID(t *testing.T) {
	cases := []struct {
		name   string
		id     BufferID
		zero   bool
		server bool
		str    string
	}{
		{"zero", BufferID{}, true, false, "<none>"},
		{"server buffer", ServerBuffer("libera"), false, true, "libera/"},
		{"channel", BufferID{Network: "libera", Folded: "#go"}, false, false, "libera/#go"},
		{"query", BufferID{Network: "libera", Folded: "alice"}, false, false, "libera/alice"},
		// A folded name with no network is malformed, but must not be mistaken
		// for "no buffer".
		{"orphan", BufferID{Folded: "#go"}, false, false, "/#go"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.id.IsZero(); got != tc.zero {
				t.Errorf("IsZero = %v, want %v", got, tc.zero)
			}
			if got := tc.id.IsServer(); got != tc.server {
				t.Errorf("IsServer = %v, want %v", got, tc.server)
			}
			if got := tc.id.String(); got != tc.str {
				t.Errorf("String = %q, want %q", got, tc.str)
			}
		})
	}
}

// TestBufferIDIsAMapKey: the whole identity model depends on BufferID being
// comparable and usable as a map key.
func TestBufferIDIsAMapKey(t *testing.T) {
	m := map[BufferID]int{}
	a := BufferID{Network: "n1", Folded: "#go"}
	b := BufferID{Network: "n1", Folded: "#go"}
	c := BufferID{Network: "n2", Folded: "#go"}

	m[a] = 1
	m[b] = 2
	m[c] = 3
	if len(m) != 2 {
		t.Fatalf("map has %d entries, want 2", len(m))
	}
	if m[a] != 2 {
		t.Errorf("equal ids did not collide: %d", m[a])
	}
	// The same channel name on two networks must stay two buffers.
	if m[c] != 3 {
		t.Errorf("networks are not part of the identity: %d", m[c])
	}
}

func TestIDSeq(t *testing.T) {
	s := NewIDSeq(0)
	if s.Last() != 0 {
		t.Errorf("fresh sequence Last = %d, want 0", s.Last())
	}
	if got := s.Next(); got != 1 {
		t.Errorf("first id = %d, want 1 (0 means unassigned)", got)
	}
	if got := s.Next(); got != 2 {
		t.Errorf("second id = %d", got)
	}
	if s.Last() != 2 {
		t.Errorf("Last = %d, want 2", s.Last())
	}

	// Restoring from the archive continues rather than restarting.
	restored := NewIDSeq(500)
	if got := restored.Next(); got != 501 {
		t.Errorf("restored sequence first id = %d, want 501", got)
	}

	// Two sequences are independent: there is no package-level counter.
	a, b := NewIDSeq(0), NewIDSeq(0)
	a.Next()
	a.Next()
	if got := b.Next(); got != 1 {
		t.Errorf("second sequence started at %d, want 1", got)
	}
}

func TestIDSeqConcurrent(t *testing.T) {
	const goroutines, per = 8, 500
	s := NewIDSeq(0)

	var wg sync.WaitGroup
	seen := make([][]MessageID, goroutines)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			ids := make([]MessageID, per)
			for i := range ids {
				ids[i] = s.Next()
			}
			seen[g] = ids
		}(g)
	}
	wg.Wait()

	all := map[MessageID]bool{}
	for _, ids := range seen {
		for _, id := range ids {
			if id == 0 {
				t.Fatal("Next returned 0")
			}
			if all[id] {
				t.Fatalf("id %d issued twice", id)
			}
			all[id] = true
		}
	}
	if len(all) != goroutines*per {
		t.Errorf("issued %d distinct ids, want %d", len(all), goroutines*per)
	}
}
