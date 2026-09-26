package engine

// A TRIGGER'S READ IS BOUNDED BY ITS KINDS (#637). The dispatcher read every
// changelog entry past a record trigger's cursor and filtered by kind in Go,
// so a replay of a trigger over one small kind in a large repository walked
// the whole changelog, 200 entries a batch. The read now names the kinds the
// source can match, one seq-ordered branch per kind, and a short batch moves
// the scan position to the head it read first: the entries of other kinds are
// never read, and the cursor still reaches head.

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

const (
	readWidgetKind  = "widgets.test.dev/widgets/widget"
	readTaskKind    = "samples.substrate.reamde.dev/tasks/task"
	readProjectKind = "samples.substrate.reamde.dev/tasks/project"
)

// seedReadBacklog writes `noise` tasks with the counted entries of each other
// kind spread evenly among them.
func seedReadBacklog(t *testing.T, ds *dataset, noise int, spread map[string]int) {
	t.Helper()
	ctx := context.Background()
	put := func(kind, id string) {
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: kind, ID: id, Properties: map[string]any{"name": id},
		}); err != nil {
			t.Fatalf("put %s: %v", kind, err)
		}
	}
	written := map[string]int{}
	for i := range noise {
		put(readTaskKind, fmt.Sprintf("noise-%d", i))
		for kind, n := range spread {
			// Entry j of the kind lands after noise entry j*noise/n.
			for written[kind] < n && written[kind]*noise/n <= i {
				put(kind, fmt.Sprintf("%s-%d", kind[strings.LastIndex(kind, "/")+1:], written[kind]))
				written[kind]++
			}
		}
	}
}

// drainRead reads a source from seq 0 to head the way the dispatcher does,
// batch after batch, and fails when a batch holds a kind outside `want`, is
// out of seq order, or covers the wrong scan position.
func drainRead(t *testing.T, ds *dataset, pats []string, want map[string]bool, head int64) []substrate.Change {
	t.Helper()
	tr := &trigger{ID: "probe", Record: &recordSource{Kinds: pats}}
	var all []substrate.Change
	var after int64
	for after < head {
		changes, scanned, err := ds.changesPast(context.Background(), tr, after)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		for i, ch := range changes {
			if !want[ch.Kind] {
				t.Fatalf("the read returned an entry of %s (seq %d), a kind the source cannot match", ch.Kind, ch.Seq)
			}
			if ch.Seq <= after || (i > 0 && ch.Seq <= changes[i-1].Seq) {
				t.Fatalf("entry seq %d is out of order past %d", ch.Seq, after)
			}
		}
		switch {
		case len(changes) == triggerBatch && scanned != changes[len(changes)-1].Seq:
			t.Fatalf("a full batch covers through %d, want its last entry %d", scanned, changes[len(changes)-1].Seq)
		case len(changes) < triggerBatch && scanned != head:
			t.Fatalf("a short batch covers through %d, want the head %d", scanned, head)
		}
		all = append(all, changes...)
		after = scanned
	}
	return all
}

var (
	sortNode    = regexp.MustCompile(`(?m)^\s*(->\s+)?(Incremental )?Sort \(`)
	indexRows   = regexp.MustCompile(`Index (Only )?Scan.*rows=(\d+)`)
	indexKindIx = "changelog_kind_seq_idx"
)

// assertBoundedPlan fails unless the plan walks the kind index with no Sort,
// filters no entry out of a wider scan, and reads at most one batch from each
// index scan.
func assertBoundedPlan(t *testing.T, plan string) {
	t.Helper()
	if !strings.Contains(plan, indexKindIx) || sortNode.MatchString(plan) || strings.Contains(plan, "Rows Removed by Filter") {
		t.Fatalf("the read is not bounded by its kinds:\n%s", plan)
	}
	for _, m := range indexRows.FindAllStringSubmatch(plan, -1) {
		if n, _ := strconv.Atoi(m[2]); n > triggerBatch {
			t.Fatalf("an index scan read %d entries, more than one batch of %d:\n%s", n, triggerBatch, plan)
		}
	}
}

// querier is what explainPlan runs on: the pool or one transaction.
type querier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func explainPlan(t *testing.T, ctx context.Context, q querier, query string, args ...any) string {
	t.Helper()
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var plan []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(plan, "\n")
}

func TestATriggerReadReturnsOnlyItsKinds(t *testing.T) {
	t.Parallel()
	const (
		noise    = 1500
		widgets  = triggerBatch + 50
		projects = 30
	)
	ctx := context.Background()
	ds := openCursorDataset(t)
	seedReadBacklog(t, ds, noise, map[string]int{readWidgetKind: widgets, readProjectKind: projects})
	head := maxSeqOf(t, ds)
	onlyWidgets := map[string]bool{readWidgetKind: true}

	for _, pats := range [][]string{
		{readWidgetKind},
		{"widgets.test.dev/widgets/*"},
		{"widgets.test.dev/*"},
		// A kind named twice is read once.
		{readWidgetKind, "widgets.test.dev/*"},
	} {
		t.Run(strings.Join(pats, ","), func(t *testing.T) {
			if got := drainRead(t, ds, pats, onlyWidgets, head); len(got) != widgets {
				t.Fatalf("the drain read %d entries, want the %d widget entries once each", len(got), widgets)
			}
		})
	}

	t.Run("two kinds", func(t *testing.T) {
		want := map[string]bool{readWidgetKind: true, readProjectKind: true}
		if got := drainRead(t, ds, []string{readWidgetKind, readProjectKind}, want, head); len(got) != widgets+projects {
			t.Fatalf("the drain read %d entries, want %d", len(got), widgets+projects)
		}
	})

	t.Run("a kind no entry carries", func(t *testing.T) {
		tr := &trigger{ID: "probe", Record: &recordSource{Kinds: []string{"nobody.test.dev/*"}}}
		changes, scanned, err := ds.changesPast(ctx, tr, 0)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if len(changes) != 0 || scanned != head {
			t.Fatalf("read %d entries covering through %d, want none covering through head %d", len(changes), scanned, head)
		}
	})

	// The plan behind the read, on the statement changesPast runs: with the
	// table analyzed, each kind's branch walks its range of
	// changelog_kind_seq_idx from the cursor in seq order and stops at one
	// batch, and nothing sorts the kinds' remaining entries or filters out an
	// entry of another kind. Both plan shapes the pool can run are checked:
	// the custom plan of a one-off statement, and the generic plan of a
	// cached one.
	if _, err := ds.svc.admin.ExecContext(ctx, `ANALYZE changelog`); err != nil {
		t.Fatalf("analyze: %v", err)
	}
	for name, kinds := range map[string][]string{
		"one kind":  {readWidgetKind},
		"two kinds": {readWidgetKind, readProjectKind},
	} {
		query, args := triggerRead(kinds, 0, head)
		t.Run("the custom plan of "+name, func(t *testing.T) {
			assertBoundedPlan(t, explainPlan(t, ctx, ds.db, `EXPLAIN (ANALYZE, COSTS OFF, TIMING OFF) `+query, args...))
		})
		t.Run("the generic plan of "+name, func(t *testing.T) {
			tx, err := ds.db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback() }()
			stmt := "trigger_read_" + strconv.Itoa(len(kinds))
			if _, err := tx.ExecContext(ctx, `SET LOCAL plan_cache_mode = force_generic_plan`); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.ExecContext(ctx, `PREPARE `+stmt+` AS `+query); err != nil {
				t.Fatalf("prepare: %v", err)
			}
			defer func() { _, _ = tx.ExecContext(ctx, `DEALLOCATE `+stmt) }()
			vals := make([]string, len(args))
			for i, a := range args {
				if s, ok := a.(string); ok {
					vals[i] = "'" + s + "'"
				} else {
					vals[i] = fmt.Sprint(a)
				}
			}
			assertBoundedPlan(t, explainPlan(t, ctx, tx,
				`EXPLAIN (ANALYZE, COSTS OFF, TIMING OFF) EXECUTE `+stmt+`(`+strings.Join(vals, ", ")+`)`))
		})
	}

	t.Run("every kind", func(t *testing.T) {
		tr := &trigger{ID: "probe", Record: &recordSource{Kinds: []string{"*"}}}
		changes, scanned, err := ds.changesPast(ctx, tr, 0)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if len(changes) != triggerBatch {
			t.Fatalf("a `*` read returned %d entries, want a full batch of %d", len(changes), triggerBatch)
		}
		if last := changes[len(changes)-1].Seq; scanned != last {
			t.Fatalf("a full batch covers through %d, want its last entry %d", scanned, last)
		}
	})
}

// The replay the issue measured: the trigger rewound to zero over a backlog
// of other kinds delivers every widget and ends at head.
func TestAReplayOverOneKindDeliversItAndEndsAtHead(t *testing.T) {
	t.Parallel()
	const (
		noise   = triggerBatch + 50
		widgets = 3
	)
	ctx := context.Background()
	ds := openCursorDataset(t)
	const id = "on-mirror.widgets.test.dev/widgets"
	// Dispatch once so the cursor exists, then write the backlog and rewind.
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	seedReadBacklog(t, ds, noise, map[string]int{readWidgetKind: widgets})
	if err := ds.ReplayTrigger(ctx, id, 0); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	var mirrored int
	if err := ds.db.QueryRowContext(ctx, `
		SELECT count(*) FROM records WHERE kind = $1 AND id LIKE 't-%' AND deleted_at IS NULL`,
		readTaskKind).Scan(&mirrored); err != nil {
		t.Fatal(err)
	}
	if mirrored != widgets {
		t.Fatalf("the replay mirrored %d widgets, want %d", mirrored, widgets)
	}
	var cursor int64
	if err := ds.db.QueryRowContext(ctx, `SELECT seq FROM trigger_cursors WHERE trigger_id = $1`, id).Scan(&cursor); err != nil {
		t.Fatal(err)
	}
	if head := maxSeqOf(t, ds); cursor != head {
		t.Fatalf("the cursor is %d after the replay drained, want head %d", cursor, head)
	}
}
