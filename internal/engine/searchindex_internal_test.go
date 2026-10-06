package engine

import (
	"io"
	"log/slog"
	"testing"
	"time"
)

// Once the service's shutdown has waited its drain budget, closing a dataset
// whose reindex has not returned does not wait a second budget of its own:
// a shutdown closing many repositories would otherwise wait once for each.
func TestClosingADatasetAfterTheShutdownDrainDoesNotWaitForTheReindex(t *testing.T) {
	t.Parallel()
	svc := &service{bg: newBackground(), log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	svc.stopBackground(time.Second)
	canceled := false
	ds := &dataset{svc: svc}
	ds.reindex.cancel = func() { canceled = true }
	// Never closed: the reindex outlived the shutdown's budget.
	ds.reindex.done = make(chan struct{})
	started := time.Now()
	ds.stopSearchReindex(true)
	if took := time.Since(started); took > backgroundDrainTimeout/2 {
		t.Fatalf("closing the dataset waited %s for a reindex the shutdown already waited for", took)
	}
	if !canceled {
		t.Fatal("closing the dataset did not cancel its reindex")
	}
}
