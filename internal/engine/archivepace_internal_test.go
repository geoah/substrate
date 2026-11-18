package engine

import (
	"context"
	"testing"
	"testing/synctest"
	"time"
)

// A pass pauses for the agents in flight with its snapshot held, and an index
// build waits out that snapshot, so a pass never pauses while a build runs,
// and a pause a build starts during ends at the next poll: an agent's
// declaration whose build waited on the pass, while the pass waited on the
// agent, ended at the agent's deadline with nothing applied (#1496).
func TestAnArchivePassDoesNotPauseWhileAnIndexBuildRuns(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		s := &service{archiveYieldMax: time.Hour}
		s.invocationStarted()
		p := s.newArchivePacer("r")

		s.indexBuildStarted()
		if err := p.yield(ctx); err != nil {
			t.Fatalf("yield beside a build: %v", err)
		}
		if p.paused != 0 {
			t.Fatalf("the pass paused %s while a build ran", p.paused)
		}
		s.indexBuildEnded()

		done := make(chan error, 1)
		go func() { done <- p.yield(ctx) }()
		synctest.Wait()
		select {
		case err := <-done:
			t.Fatalf("yield returned (%v) while an agent ran and no build did", err)
		default:
		}

		// A build starts while the pass pauses: the pause ends at the next
		// poll, one poll after it began.
		s.indexBuildStarted()
		defer s.indexBuildEnded()
		time.Sleep(archiveYieldPoll)
		synctest.Wait()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("yield: %v", err)
			}
		default:
			t.Fatal("a pause went on past the poll after a build started")
		}
		if p.paused != archiveYieldPoll {
			t.Fatalf("the pass paused %s, want one poll, %s", p.paused, archiveYieldPoll)
		}
	})
}
