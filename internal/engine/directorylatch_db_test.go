package engine

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/geoah/substrate/internal/substrate"
)

// beginWatch is a pgx tracer that closes seen when a statement sent under a
// context carrying key begins a transaction.
type beginWatch struct {
	key  any
	seen chan struct{}
	once sync.Once
}

func (w *beginWatch) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if ctx.Value(w.key) != nil && strings.HasPrefix(strings.ToLower(strings.TrimSpace(d.SQL)), "begin") {
		w.once.Do(func() { close(w.seen) })
	}
	return ctx
}

func (w *beginWatch) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

type secondWriteKey struct{}

// A write held at its commit, with the changelog lines prepared and writerMu
// held, does not keep the next write from its door: the second write takes a
// pool connection and begins its transaction while the first is held, and
// waits for the changelog lock there (#835).
func TestDirectoryLatchLetsAWriteBeginDuringAnotherCommit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var armed atomic.Bool
	held, release := make(chan struct{}), make(chan struct{})
	watch := &beginWatch{key: secondWriteKey{}, seen: make(chan struct{})}
	ds := openInternalDataset(t,
		WithTestQueryTracer(watch),
		WithTestCommitFault(func(stage string) error {
			if stage == commitAfterPrepare && armed.CompareAndSwap(true, false) {
				close(held)
				<-release
			}
			return nil
		}))
	put := func(ctx context.Context, name string) error {
		_, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": name}})
		return err
	}

	armed.Store(true)
	first := make(chan error, 1)
	go func() { first <- put(ctx, "held at its commit") }()
	select {
	case <-held:
	case <-time.After(time.Minute):
		t.Fatal("the first write never reached its commit")
	}
	second := make(chan error, 1)
	go func() { second <- put(context.WithValue(ctx, secondWriteKey{}, true), "waits behind it") }()
	select {
	case <-watch.seen:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("the second write did not begin its transaction while the first held its commit: its door waited for writerMu")
	}
	close(release)
	for name, ch := range map[string]chan error{"first": first, "second": second} {
		select {
		case err := <-ch:
			if err != nil {
				t.Fatalf("the %s write: %v", name, err)
			}
		case <-time.After(time.Minute):
			t.Fatalf("the %s write never returned", name)
		}
	}
}
