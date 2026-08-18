package config

import (
	"sync"
	"testing"
)

// TestSettingsSnapshotRace pins the contract the snapshot exists for:
// publishing new settings while other goroutines read must be race-free,
// and a reader always sees one coherent snapshot rather than a half-applied
// mix of old and new values (which the v1 globals could produce).
func TestSettingsSnapshotRace(t *testing.T) {
	Publish(defaults())
	var wg sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				s := Current()
				// the writer only ever publishes coherent pairs
				if s.ShowTimestamps != (s.TimeLayout == "15:04") {
					t.Errorf("torn snapshot: %+v", s)
					return
				}
			}
		}()
	}
	for i := 0; i < 200; i++ {
		s := defaults()
		if i%2 == 0 {
			s.ShowTimestamps, s.TimeLayout = false, "3:04 PM"
		}
		Publish(s)
	}
	close(stop)
	wg.Wait()
	Publish(defaults())
}
