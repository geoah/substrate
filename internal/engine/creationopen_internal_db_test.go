package engine

// A creation writes the new repository's directory AFTER the control-plane
// row, with a changelog writer of its own (createSeededRepository,
// reconcileRow). The row is what the background loops list, so an open of the
// repository can start while that writer holds the directory's flock, and the
// flock conflicts inside one process exactly as across two: the open has to
// wait for the creation, or it fails with ErrChangelogLocked.

import (
	"context"
	"sync"
	"testing"
)

// An open that finds the new row while the creation's catch-up has prepared
// the seed and not yet committed it waits for the creation and then opens.
// An open that does not wait scans the file, reads the prepared transaction
// as an incomplete tail, and is refused the cut by the creation's own writer:
// "cut incomplete tail: ... changelog writer lock".
func TestAnOpenDuringACreationsDirectoryWriteWaitsForIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := openBareService(t)
	const authority = "carol.example.com"

	opened := make(chan error, 1)
	parked := make(chan struct{})
	var parkOnce, fireOnce sync.Once
	fired := false
	s.testOpenWaits = func(id string) {
		if id == authority {
			parkOnce.Do(func() { close(parked) })
		}
	}
	s.testCatchUpPrepared = func(id string) {
		if id != authority {
			return
		}
		fireOnce.Do(func() {
			fired = true
			// What a background loop does once the row is listed: it opens
			// the repository by its id.
			go func() {
				_, err := s.Dataset(ctx, authority)
				opened <- err
			}()
			// The creation goes on once the open has either answered or is
			// waiting for the creation; it must not go on while the open
			// may still be reading the file.
			select {
			case err := <-opened:
				opened <- err
			case <-parked:
			}
		})
	}
	if _, err := s.CreateRepository(ctx, authority); err != nil {
		t.Fatalf("create the repository: %v", err)
	}
	// Read after CreateRepository returned, on the goroutine the seam ran on.
	if !fired {
		t.Fatal("the creation's directory write never prepared a catch-up; this test is not watching the window it means to")
	}
	if err := <-opened; err != nil {
		t.Fatalf("an open that found the new repository while its creation wrote the directory failed: %v", err)
	}
	s.mu.Lock()
	_, cached := s.datasets[authority]
	s.mu.Unlock()
	if !cached {
		t.Fatal("the open returned no error and cached no dataset")
	}
}
