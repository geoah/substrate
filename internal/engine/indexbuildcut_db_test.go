package engine

// A vocabulary apply's index work holds the archive passes off their pause
// (archivepace.go) and says, when its caller's context ends, which phase the
// end met (indexBuildCut). These run each through the apply door.

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// gardenNoteDocs are the two applies of these tests: a pad kind, which needs
// no index, and a note kind whose reference to it does. The documents spell
// the repository's full name; note is the kind as the engine names it, in
// the local namespace.
func gardenNoteDocs(t *testing.T) (pads, notes []map[string]any, note string) {
	t.Helper()
	pkg := testdb.Repository(t) + "/garden"
	pads = []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "pad"}, map[string]any{
			"description":     "A pad to write on.",
			"displayTemplate": "{name}",
			"properties": map[string]any{
				"name": map[string]any{"type": "string", "description": "what the pad is called"},
			},
		}),
	}
	notes = []map[string]any{
		vocabulary.KindManifest(pkg, map[string]any{"singular": "note"}, map[string]any{
			"description":     "A note written on a pad.",
			"displayTemplate": "{name}",
			"properties": map[string]any{
				"name": map[string]any{"type": "string", "description": "what the note is about"},
				"pad":  map[string]any{"type": "reference", "kind": pkg + "/pad", "description": "the pad it is on"},
			},
		}),
	}
	return pads, notes, "~/garden/note"
}

// panicOnStep is a log handler that panics at the vocabulary apply step it
// names while armed: a progress callback that panics, with no seam in the
// engine for it.
type panicOnStep struct {
	slog.Handler
	step     string
	armed    *atomic.Bool
	panicked *atomic.Bool
}

// Enabled is every level: the handler it wraps may discard every record,
// and a record a logger finds disabled never reaches Handle.
func (panicOnStep) Enabled(context.Context, slog.Level) bool { return true }

func (h panicOnStep) Handle(ctx context.Context, r slog.Record) error {
	if h.armed.Load() {
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == "step" && a.Value.String() == h.step {
				h.panicked.Store(true)
				panic("a progress callback panicked")
			}
			return true
		})
	}
	return h.Handler.Handle(ctx, r)
}

func (h panicOnStep) WithAttrs(attrs []slog.Attr) slog.Handler {
	h.Handler = h.Handler.WithAttrs(attrs)
	return h
}

func (h panicOnStep) WithGroup(name string) slog.Handler {
	h.Handler = h.Handler.WithGroup(name)
	return h
}

// A panic in the index work's progress callback leaves the archive passes
// pausing for agents again: a count left above zero, behind the HTTP
// recovery, would have kept every repository's passes from pausing for the
// life of the process.
func TestAPanicInAnIndexBuildsProgressLetsTheArchivePassesPauseAgain(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var armed, panicked atomic.Bool
	ds := openCoreDataset(t, WithLogger(slog.New(panicOnStep{
		Handler: slog.DiscardHandler, step: "building an index", armed: &armed, panicked: &panicked,
	})))
	pads, notes, note := gardenNoteDocs(t)
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, pads); err != nil {
		t.Fatalf("declare the pad: %v", err)
	}

	armed.Store(true)
	func() {
		defer func() { _ = recover() }()
		_, _ = ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, notes)
	}()
	armed.Store(false)
	if !panicked.Load() {
		t.Fatal("the apply built no index, so no progress callback ran")
	}
	if n := ds.svc.indexBuilds.Load(); n != 0 {
		t.Fatalf("index builds in flight after the panic = %d, want 0", n)
	}
	ds.svc.invocationStarted()
	defer ds.svc.invocationEnded()
	if !ds.svc.archiveYields() {
		t.Fatal("an archive pass does not pause for a running agent after the panic")
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, notes); err != nil {
		t.Fatalf("the same apply after the panic: %v", err)
	}
	if _, err := ds.resolveType(note); err != nil {
		t.Fatalf("the note kind is not declared: %v", err)
	}
}

// An apply queued behind another session's index build, whose context ends
// while it waits for the build lock that session refused it, says it was
// waiting for another build; one whose own build the end met says index
// preparation had not finished. Neither
// text carries the driver's error, which the cut still wraps, and neither
// declaration lands. The second apply, with nothing in the way, builds over
// what the cut build left.
func TestAnApplyWhoseIndexWorkItsContextEndedSaysWhereItStood(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openCoreDataset(t)
	pads, notes, note := gardenNoteDocs(t)
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, pads); err != nil {
		t.Fatalf("declare the pad: %v", err)
	}
	applyCut := func(phase indexCutPhase, want string) {
		t.Helper()
		actx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		_, err := ds.ApplyVocabularyDocuments(actx, substrate.ActorPerson, notes)
		var cut *indexBuildCut
		if !errors.As(err, &cut) {
			t.Fatalf("apply = %v, want the index work cut", err)
		}
		if cut.phase != phase || cut.kind != note {
			t.Fatalf("cut at phase %d of %s, want phase %d of %s", cut.phase, cut.kind, phase, note)
		}
		if err.Error() != want {
			t.Fatalf("apply's error = %q\nwant %q", err, want)
		}
		if cut.err == nil || !errors.Is(err, cut.err) {
			t.Fatal("the cut does not wrap the error the work ended in")
		}
		if _, err := ds.resolveType(note); err == nil {
			t.Fatal("the note kind landed although its index work was cut")
		}
	}

	// Another session holds the build lock.
	held, err := ds.svc.admin.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Close() }()
	if _, err := held.ExecContext(ctx, `SELECT pg_advisory_lock(`+indexBuildLockKeySQL+`)`); err != nil {
		t.Fatal(err)
	}
	applyCut(indexCutQueued, "substrate/engine: create index for "+note+
		": the call ended while it was waiting for another index build on the database to finish, so the declaration was not applied")
	if _, err := held.ExecContext(ctx, `SELECT pg_advisory_unlock(`+indexBuildLockKeySQL+`)`); err != nil {
		t.Fatal(err)
	}

	// A write in flight on records holds the build in its first wait.
	writer, err := ds.svc.admin.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = writer.Rollback() }()
	if _, err := writer.ExecContext(ctx, `LOCK TABLE records IN ROW EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	applyCut(indexCutPreparing, "substrate/engine: create index for "+note+
		": the call ended before index preparation finished, so the declaration was not applied")
	if err := writer.Rollback(); err != nil {
		t.Fatal(err)
	}

	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, notes); err != nil {
		t.Fatalf("the apply with nothing in the way: %v", err)
	}
	if _, err := ds.resolveType(note); err != nil {
		t.Fatalf("the note kind is not declared: %v", err)
	}
	if left, err := scratchIndexes(ctx, ds.svc.admin); err != nil || len(left) != 0 {
		t.Fatalf("scratch indexes after the apply = %v (%v), want none", left, err)
	}
}
