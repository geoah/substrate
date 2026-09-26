package engine

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// Close does not wait forever on a connection somebody still holds: a task
// or a handler that outlived the shutdown would otherwise keep the process
// from exiting. It gives up after its wait and says what was still held.
func TestClosingTheRepositoryPoolGivesUpOnAHeldConnection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool, err := openRepositoryPool(ctx, MigratedDSN(t), "", MinRepositoryConnections)
	if err != nil {
		t.Fatal(err)
	}
	held, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	started := time.Now()
	closeRepositoryPool(pool, 100*time.Millisecond, slog.New(slog.NewTextHandler(&logs, nil)))
	if took := time.Since(started); took > 5*time.Second {
		t.Fatalf("closing the pool waited %s on a held connection", took)
	}
	if !strings.Contains(logs.String(), "acquired=1") {
		t.Fatalf("the give-up does not say what was held: %s", logs.String())
	}
	held.Release()
}
