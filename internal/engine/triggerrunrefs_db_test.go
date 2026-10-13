package engine

// A trigger's runs are found through refs, not through each run's own
// `trigger` property. Row level security runs a clause it cannot prove
// leakproof only on the rows its policy admits, and `->>` on jsonb is not
// leakproof, so the app role could not use the run kind's [trigger, status]
// index and filtered every run of every trigger: in the prune every settling
// transaction runs under the changelog lock, and in the failing-trigger read.

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func TestTriggerRunsAreReadThroughRefs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openCoreDataset(t)
	callable := vocabulary.RecordPath(kindFunction, "fn")
	putRuns := func(trigger, status string, n int) {
		t.Helper()
		if err := ds.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
			for range n {
				if err := tx.putSystemRun(runRecord{
					trigger: trigger, callable: callable, mode: runner.ModeRecord,
					status: status, attempt: 1, startedAt: tx.now, errMsg: "boom",
				}, false); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Fatalf("put %d %s runs of %s: %v", n, status, trigger, err)
		}
	}
	prune := func(trigger string) {
		t.Helper()
		if err := ds.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error { return tx.pruneRuns(trigger) }); err != nil {
			t.Fatalf("prune %s: %v", trigger, err)
		}
	}
	live := func(trigger string) int {
		t.Helper()
		var n int
		if err := ds.db.QueryRowContext(ctx, `
			SELECT count(*) FROM records WHERE kind = $1 AND deleted_at IS NULL AND `+referencePathSQL("props", "trigger")+` = $2`,
			typeTriggerRun, vocabulary.RecordPath(typeTrigger, trigger)).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	parked := func(trigger string) int64 {
		t.Helper()
		reads, err := readFailing(ctx, ds.db, []string{trigger})
		if err != nil || len(reads) != 1 {
			t.Fatalf("read failing %s: %v (%d reads)", trigger, err, len(reads))
		}
		return reads[0].count
	}

	putRuns("kept", runStatusOK, runRetention)
	putRuns("failing", runStatusParked, 3)
	if got := parked("failing"); got != 3 {
		t.Fatalf("the failing trigger counts %d parked runs, want 3", got)
	}

	// The read's source seeks refs_dst_idx under the app role's policy and
	// reads no run's properties to find the trigger's runs.
	tx, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SET LOCAL enable_seqscan = off`); err != nil {
		t.Fatal(err)
	}
	plan := explainPlan(t, ctx, tx, `EXPLAIN (FORMAT TEXT) SELECT run.id `+triggerRunsSQL(true, "$1", "$2", "$3", "$3"),
		typeTriggerRun, typeTrigger, "kept")
	if !strings.Contains(plan, "refs_dst_idx") || strings.Contains(plan, "props") {
		t.Fatalf("the runs of one trigger are not found through refs_dst_idx alone:\n%s", plan)
	}
	_ = tx.Rollback()

	// With the runs' refs rows gone, as a declaration change the
	// re-derivation has not reached would leave them, refs alone decide: the
	// parked runs are not found, and the prune finds only the runs written
	// since, so it tombstones none of the older ones.
	if _, err := ds.db.ExecContext(ctx, `DELETE FROM refs WHERE src_kind = $1`, typeTriggerRun); err != nil {
		t.Fatalf("drop the runs' refs rows: %v", err)
	}
	putRuns("kept", runStatusOK, 5)
	prune("kept")
	if got := live("kept"); got != runRetention+5 {
		t.Fatalf("with refs deciding, the prune left %d runs, want all %d: it did not read refs", got, runRetention+5)
	}
	if got := parked("failing"); got != 0 {
		t.Fatalf("with refs deciding, the failing trigger counts %d parked runs, want 0: the read did not use refs", got)
	}

	// While the re-derivation owes the run kind's `trigger` refs rows, each
	// run's own property decides.
	if _, err := ds.db.ExecContext(ctx, `
		INSERT INTO index_reprojections (kind, refs, refs_properties, fts) VALUES ($1, true, ARRAY['trigger'], false)`,
		typeTriggerRun); err != nil {
		t.Fatalf("owe a re-derivation: %v", err)
	}
	if got := parked("failing"); got != 3 {
		t.Fatalf("with refs owed, the failing trigger counts %d parked runs, want 3 off its properties", got)
	}
	prune("kept")
	if got := live("kept"); got != runRetention {
		t.Fatalf("with refs owed, the prune left %d runs, want the %d of the retention", got, runRetention)
	}
}
