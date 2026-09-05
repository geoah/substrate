package engine

// The trigger machinery's internal regressions: a schedule fire is idempotent
// under stable fire ids with missed ticks coalescing to one, and a host Call
// at the causal-depth cap refuses. (The dispatcher's per-repository
// independence and its self-actor exclusion are proved end to end, through the
// public surface, in functions_db_test.go.)

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func maxSeqOf(t *testing.T, ds *dataset) int64 {
	t.Helper()
	var seq int64
	if err := ds.db.QueryRowContext(context.Background(),
		`SELECT COALESCE(max(seq), 0) FROM changelog`).Scan(&seq); err != nil {
		t.Fatal(err)
	}
	return seq
}

func TestScheduleFireIdempotentAndCoalesced(t *testing.T) {
	t.Parallel()
	// Missed ticks coalesce to ONE fire with a stable id, the fire state
	// advances compare-and-swap in the delivery's transaction, and a second
	// pass at the same instant fires nothing.
	ctx := context.Background()
	ds := openCursorDataset(t)
	const pkg = "widgets.test.dev/widgets"

	_, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger,
		Properties: map[string]any{
			"source": map[string]any{"schedule": map[string]any{
				"recurrence": "FREQ=HOURLY", "timezone": "UTC",
			}},
			"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", pkg+"/hourly"),
		},
	})
	if err == nil {
		t.Fatal("a schedule trigger with an unknown callable landed")
	}

	// Install the fire-counting function, then the trigger.
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.FunctionManifest(pkg, "hourly", map[string]any{
			"description": "mints one task per fire",
			"runtime":     vocabulary.RuntimePython,
			"permissions": map[string]any{"writes": []any{"samples.substrate.reamde.dev/tasks/task"}},
			"source": `
def main(input, host):
    fire = input["envelope"]["fire"]
    return {"effects": [{"action": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
                         "id": "fire-" + fire["id"],
                         "properties": {"name": input["mode"] + " " + fire["at"]}}]}
`,
		}),
	}); err != nil {
		t.Fatalf("install function: %v", err)
	}
	tr, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger,
		Properties: map[string]any{
			"source": map[string]any{"schedule": map[string]any{
				"recurrence": "FREQ=HOURLY", "timezone": "UTC",
			}},
			"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", pkg+"/hourly"),
		},
	})
	if err != nil {
		t.Fatalf("put schedule trigger: %v", err)
	}

	// Nothing is due at creation: the fire state initialized at now.
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	if n := countLiveOf(t, ds, "samples.substrate.reamde.dev/tasks/task"); n != 0 {
		t.Fatalf("a fresh schedule backfilled: %d tasks", n)
	}

	// Rewind the fire state three-and-a-bit hours: several occurrences are
	// now overdue, and they must coalesce to exactly ONE fire — the newest.
	if _, err := ds.db.ExecContext(ctx, `
		UPDATE trigger_schedule SET fired_at = fired_at - interval '190 minutes'
		WHERE trigger_id = $1`, tr.ID); err != nil {
		t.Fatalf("rewind: %v", err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	if n := countLiveOf(t, ds, "samples.substrate.reamde.dev/tasks/task"); n != 1 {
		t.Fatalf("missed ticks did not coalesce: %d tasks", n)
	}
	// The fire id is the occurrence instant — stable, not a random mint.
	var fid string
	if err := ds.db.QueryRowContext(ctx, `
		SELECT props->>'fireId' FROM records
		WHERE kind = $1 AND props->>'status' = 'ok' AND deleted_at IS NULL
		ORDER BY created_at DESC LIMIT 1`, typeRun).Scan(&fid); err != nil {
		t.Fatalf("run row: %v", err)
	}
	at, err := time.Parse(time.RFC3339, fid)
	if err != nil {
		t.Fatalf("fire id %q is not an occurrence instant: %v", fid, err)
	}
	if since := time.Since(at); since < 0 || since > time.Hour {
		t.Fatalf("the coalesced fire is not the newest occurrence: %s", fid)
	}
	if _, err := ds.Get(ctx, "samples.substrate.reamde.dev/tasks/task", "fire-"+fid); err != nil {
		t.Fatalf("the body did not see the stable fire id: %v", err)
	}

	// Idempotence: the same instant fires nothing twice.
	before := maxSeqOf(t, ds)
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	if got := maxSeqOf(t, ds); got != before {
		t.Fatalf("a second pass re-fired: seq %d → %d", before, got)
	}
}

func countLiveOf(t *testing.T, ds *dataset, typeIdent string) int {
	t.Helper()
	var n int
	if err := ds.db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM records WHERE kind = $1 AND deleted_at IS NULL`, typeIdent).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCallAtDepthCapRefuses(t *testing.T) {
	t.Parallel()
	// The engine's half of the sub-call gates: a Call whose callee would sit
	// at the causal-depth cap refuses with the distinct error, and a Call to
	// a function already on the stack refuses as recursion.
	ds := openCursorDataset(t)
	ctx := context.Background()
	const ident = "widgets.test.dev/widgets/mirror"
	fn, err := ds.registry().ResolveFunction(ident)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	deep := &callBackend{
		inv: &invocation{ds: ds, stack: []string{"widgets.test.dev/widgets/root"}},
		fn:  fn, key: "k", causalDepth: causalDepthCap - 1,
	}
	if _, err := deep.Call(ctx, ident, nil); !errors.Is(err, errCausalDepth) {
		t.Fatalf("call at the cap returned %v, want errCausalDepth", err)
	}

	cyclic := &callBackend{
		inv: &invocation{ds: ds, stack: []string{ident}},
		fn:  fn, key: "k",
	}
	if _, err := cyclic.Call(ctx, ident, nil); err == nil || !strings.Contains(err.Error(), "recursion") {
		t.Fatalf("recursive call returned %v", err)
	}
}
