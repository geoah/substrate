package engine_test

// THE COMPACTION PASS (compactpass.go, decision 0300): entries past their
// kind's retention become one state entry per record, entries inside it are
// untouched, and the history replays into the fold it replayed into before.

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const passPkg = "pass.example.substrate.reamde.dev/pass"

type passer interface {
	CompactionPass(ctx context.Context) (engine.CompactionPassReport, error)
}

// passLine is one line of the history as the test holds it.
type passLine struct {
	kind, id string
	ts       time.Time
	hash     []byte
	state    bool
}

func passHistory(t *testing.T, db *sql.DB) map[int64]passLine {
	t.Helper()
	rows, err := db.Query(`
		SELECT seq, kind, record_id, ts, hash,
			jsonb_path_exists(payload, '$.fold[*] ? (@.kind == "state")')
		FROM changelog`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	out := map[int64]passLine{}
	for rows.Next() {
		var seq int64
		var l passLine
		if err := rows.Scan(&seq, &l.kind, &l.id, &l.ts, &l.hash, &l.state); err != nil {
			t.Fatal(err)
		}
		out[seq] = l
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// Done when 1: kinds of each retention, written to over a span longer than
// the longest finite one (the pass's clock is a seam): after the pass every
// record's latest state is there, entries inside retention are untouched,
// entries past it are one state entry per record, and a rebuild gives the
// same fold, table by table.
func TestThePassCompactsWhatIsPastEachKindsRetention(t *testing.T) {
	const week, month = 7 * 24 * time.Hour, 30 * 24 * time.Hour
	// Each kind's declared retention (decision 0282); `forever` states none
	// and keeps everything, and `overridden` declares a month that the
	// repository's own `core/retention` record makes a week.
	retention := map[string]string{
		"brief": "none", "weekly": "P7D", "mirror": "P30D", "overridden": "P30D",
	}
	var clock time.Time
	svc, ds, dsn := newDatasetWithDSN(t,
		engine.WithCompactionInterval(-1),
		engine.WithTestCompactionClock(func() time.Time { return clock }))
	ctx := context.Background()
	docs := []map[string]any{vocabulary.PackageManifest(passPkg, 0)}
	for _, name := range []string{"brief", "weekly", "mirror", "overridden", "forever"} {
		data := map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}
		if r, ok := retention[name]; ok {
			data["retention"] = r
		}
		docs = append(docs, vocabulary.KindManifest(passPkg, map[string]any{"singular": name}, data))
	}
	if _, err := ds.(interface {
		ApplyVocabularyDocuments(context.Context, substrate.Actor, []map[string]any) ([]*substrate.Record, error)
	}).ApplyVocabularyDocuments(ctx, owner, docs); err != nil {
		t.Fatalf("declare the kinds: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/retention", ID: "overridden",
		Properties: map[string]any{
			"appliesTo": map[string]any{"ref": "substrate.reamde.dev/core/kind/" + passPkg + "/overridden"},
			"retention": "P7D",
		},
	})
	write := func(round string) {
		for _, name := range []string{"brief", "weekly", "mirror", "overridden", "forever"} {
			for _, id := range []string{"a", "b"} {
				rec := mustPut(t, ds, owner, substrate.PutInput{
					Kind: passPkg + "/" + name, ID: fid(name + id),
					Properties: map[string]any{"name": round},
				})
				mustPatch(t, ds, owner, rec.Kind, rec.ID, substrate.PatchInput{Properties: map[string]any{"name": round + ", again"}})
			}
		}
	}
	write("old")
	// One record of each kind is written only before the boundary.
	for _, name := range []string{"brief", "weekly", "mirror", "overridden", "forever"} {
		mustPut(t, ds, owner, substrate.PutInput{Kind: passPkg + "/" + name, ID: fid(name + "-quiet"), Properties: map[string]any{"name": "x"}})
	}
	time.Sleep(5 * time.Millisecond)
	boundary := time.Now().UTC()
	time.Sleep(5 * time.Millisecond)
	write("new")

	rebuild := func() {
		t.Helper()
		if _, err := svc.(rebuilder).RebuildRepository(ctx, testdb.Repository(t)); err != nil {
			t.Fatalf("rebuild: %v", err)
		}
	}
	rebuild()
	want := engine.FoldRows(t, ds)
	db := rawDB(t, dsn)
	before := passHistory(t, db)

	// pass runs a pass at now and holds the history to it: past says, per
	// kind, whether the entries after the boundary are past the retention
	// too; the entries before it are past it for every kind but `forever`
	// and the ones listed as not.
	pass := func(now time.Time, oldKept, newPast map[string]bool) {
		t.Helper()
		clock = now
		report, err := ds.(passer).CompactionPass(ctx)
		if err != nil || !report.Finished {
			t.Fatalf("the pass at %s: %+v, %v", now, report, err)
		}
		after := passHistory(t, db)
		for seq, l := range before {
			if !strings.HasPrefix(l.kind, passPkg+"/") {
				continue
			}
			kind := strings.TrimPrefix(l.kind, passPkg+"/")
			old := l.ts.Before(boundary)
			inside := kind == "forever" || (old && oldKept[kind]) || (!old && !newPast[kind])
			if now, ok := after[seq]; inside && (!ok || string(now.hash) != string(l.hash)) {
				t.Errorf("at %s, seq %d (%s, %s) inside its retention changed: kept %v", clock.Sub(boundary), seq, kind, l.id, ok)
			}
		}
		for _, name := range []string{"brief", "weekly", "mirror", "overridden"} {
			if oldKept[name] {
				continue
			}
			for _, id := range []string{name + "a", name + "b", name + "-quiet"} {
				var states, past int
				for _, l := range after {
					if l.kind != passPkg+"/"+name || l.id != fid(id) {
						continue
					}
					switch {
					case l.state:
						states++
					case l.ts.Before(boundary) || newPast[name]:
						past++
					}
				}
				if states != 1 || past != 0 {
					t.Errorf("at %s, %s %s: %d state entries and %d entries past its retention, want 1 and 0", clock.Sub(boundary), name, id, states, past)
				}
			}
		}
		rebuild()
		if diff := engine.DiffFoldTables(want, engine.FoldRows(t, ds)); len(diff) > 0 {
			t.Fatalf("at %s, the compacted history replays into another fold:\n%s", clock.Sub(boundary), strings.Join(diff, "\n"))
		}
	}
	// A week after the boundary: the weekly kinds' entries before it are
	// past their retention, the mirror's are not, and every entry of the
	// brief kind but each record's latest state is.
	pass(boundary.Add(week), map[string]bool{"mirror": true}, map[string]bool{"brief": true})
	// A month after: the mirror's entries before the boundary too, and the
	// weekly kinds' entries after it.
	pass(boundary.Add(month), nil, map[string]bool{"brief": true, "weekly": true, "overridden": true})
	// A second pass at the same clock finds its marks where it left them.
	removed := len(passHistory(t, db))
	if again, err := ds.(passer).CompactionPass(ctx); err != nil || again.Removed != 0 || len(passHistory(t, db)) != removed {
		t.Fatalf("a second pass at the same clock: %+v, %v", again, err)
	}
}

// The horizon: a record purged longer ago than deletions are kept is
// forgotten whole, the repository's horizon moves to the line its deletion
// was on, and the fold replays as before. What a trigger's cursor does is
// compact_internal_db_test.go's.
func TestThePassForgetsAnOldDeletionAndMovesTheHorizon(t *testing.T) {
	t.Parallel()
	var clock time.Time
	svc, ds, dsn := newDatasetWithDSN(t,
		engine.WithCompactionInterval(-1), engine.WithDeletionsKept(time.Hour), engine.WithTestPurgeKeepsHistory(),
		engine.WithTestCompactionClock(func() time.Time { return clock }))
	ctx := context.Background()
	db := rawDB(t, dsn)
	const task = "samples.substrate.reamde.dev/tasks/task"
	gone := mustPut(t, ds, owner, substrate.PutInput{Kind: task, Properties: map[string]any{"name": "gone"}})
	mustPatch(t, ds, owner, gone.Kind, gone.ID, substrate.PatchInput{Properties: map[string]any{"name": "gone, again"}})
	if _, err := ds.Delete(ctx, owner, gone.Kind, gone.ID, substrate.DeleteInput{Purge: true}); err != nil {
		t.Fatalf("purge: %v", err)
	}
	var deletion int64
	if err := db.QueryRow(`SELECT max(seq) FROM changelog WHERE record_id = $1`, gone.ID).Scan(&deletion); err != nil {
		t.Fatal(err)
	}
	kept := mustPut(t, ds, owner, substrate.PutInput{Kind: task, Properties: map[string]any{"name": "kept"}})
	rebuild := func() {
		t.Helper()
		if _, err := svc.(rebuilder).RebuildRepository(ctx, testdb.Repository(t)); err != nil {
			t.Fatalf("rebuild: %v", err)
		}
	}
	lines := func() int {
		t.Helper()
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM changelog WHERE record_id = $1`, gone.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	clock = time.Now().UTC().Add(2 * time.Hour)
	rebuild()
	want := engine.FoldRows(t, ds)
	report, err := ds.(passer).CompactionPass(ctx)
	if err != nil || report.Forgotten != 1 {
		t.Fatalf("the pass forgot %d records (%v), want the purged one", report.Forgotten, err)
	}
	if n := lines(); n != 0 {
		t.Fatalf("the forgotten record still has %d lines", n)
	}
	head, err := ds.Head(ctx)
	if err != nil || head.Horizon != deletion {
		t.Fatalf("the horizon is %d (%v), want the deletion's line %d", head.Horizon, err, deletion)
	}
	if mustGet(t, ds, kept.Kind, kept.ID) == nil {
		t.Fatal("the kept record went")
	}
	rebuild()
	if diff := engine.DiffFoldTables(want, engine.FoldRows(t, ds)); len(diff) > 0 {
		t.Fatalf("forgetting a deletion moved the fold:\n%s", strings.Join(diff, "\n"))
	}
}

// A record a hold that ends kept part of is compacted the rest of the way
// by the first pass after the hold goes, though nothing writes to it again
// and the kind's mark moved past it: a task whose creation's line a parked
// failure names keeps that line and its state, waits as held, and once the
// failure goes is one state entry, the fold as it was.
func TestARecordAParkedFailureHeldIsCompactedOnceItGoes(t *testing.T) {
	t.Parallel()
	const task = "samples.substrate.reamde.dev/tasks/task"
	var clock time.Time
	svc, ds, dsn := newDatasetWithDSN(t,
		engine.WithCompactionInterval(-1), engine.WithDeletionsKept(0),
		engine.WithTestCompactionClock(func() time.Time { return clock }),
		engine.WithTestCompactionRetention(func(kind string) (bool, time.Duration) {
			return kind != task, time.Hour
		}))
	ctx := context.Background()
	db := rawDB(t, dsn)
	held := mustPut(t, ds, owner, substrate.PutInput{Kind: task, Properties: map[string]any{"name": "held"}})
	var created int64
	if err := db.QueryRow(`SELECT max(seq) FROM changelog WHERE record_id = $1`, held.ID).Scan(&created); err != nil {
		t.Fatal(err)
	}
	mustPatch(t, ds, owner, held.Kind, held.ID, substrate.PatchInput{Properties: map[string]any{"name": "held, again"}})
	mustPatch(t, ds, owner, held.Kind, held.ID, substrate.PatchInput{Properties: map[string]any{"name": "held, once more"}})
	mustPut(t, ds, owner, substrate.PutInput{Kind: task, Properties: map[string]any{"name": "after"}})
	lines := func() int {
		t.Helper()
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM changelog WHERE record_id = $1`, held.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	waiting := func() int64 {
		t.Helper()
		st, err := svc.(interface {
			CompactionStatus(context.Context, string) (engine.CompactionStatus, error)
		}).CompactionStatus(ctx, testdb.Repository(t))
		if err != nil {
			t.Fatal(err)
		}
		for _, k := range st.Kinds {
			if k.Kind == task {
				return k.Held
			}
		}
		return 0
	}
	rebuild := func() {
		t.Helper()
		if _, err := svc.(rebuilder).RebuildRepository(ctx, testdb.Repository(t)); err != nil {
			t.Fatalf("rebuild: %v", err)
		}
	}
	clock = time.Now().UTC().Add(2 * time.Hour)
	rebuild()
	want := engine.FoldRows(t, ds)

	scoped, err := engine.OpenScopedDB(dsn, testdb.Repository(t), engine.RoleApp)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = scoped.Close() }()
	if _, err := scoped.ExecContext(ctx, `
		INSERT INTO trigger_failures (id, trigger_id, seq, record_id, attempts, last_error, parked_at)
		VALUES (1, 'parked', $1, $2, 1, 'parked over the creation', now())`, created, held.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.(passer).CompactionPass(ctx); err != nil {
		t.Fatal(err)
	}
	if n := lines(); n != 2 {
		t.Fatalf("the held task has %d lines after the first pass, want its creation's and its state's", n)
	}
	if n := waiting(); n != 1 {
		t.Fatalf("%d tasks wait on a hold, want the held one", n)
	}
	if _, err := scoped.ExecContext(ctx, `DELETE FROM trigger_failures WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.(passer).CompactionPass(ctx); err != nil {
		t.Fatal(err)
	}
	if n := lines(); n != 1 {
		t.Fatalf("the held task has %d lines once the failure went, want one state", n)
	}
	if n := waiting(); n != 0 {
		t.Fatalf("%d tasks still wait on a hold", n)
	}
	rebuild()
	if diff := engine.DiffFoldTables(want, engine.FoldRows(t, ds)); len(diff) > 0 {
		t.Fatalf("the compacted history replays into another fold:\n%s", strings.Join(diff, "\n"))
	}
}

// heldFixture is a repository whose tasks keep an hour of history, on the
// pass's clock two hours on, with a seat to park failures over lines from.
type heldFixture struct {
	svc   substrate.Service
	ds    substrate.Dataset
	db    *sql.DB
	park  *sql.DB
	clock *time.Time
}

func newHeldFixture(t *testing.T, opts ...engine.Option) *heldFixture {
	t.Helper()
	const task = "samples.substrate.reamde.dev/tasks/task"
	f := &heldFixture{clock: new(time.Time)}
	svc, ds, dsn := newDatasetWithDSN(t, append([]engine.Option{
		engine.WithCompactionInterval(-1),
		engine.WithTestCompactionClock(func() time.Time { return *f.clock }),
		engine.WithTestCompactionRetention(func(kind string) (bool, time.Duration) {
			return kind != task, time.Hour
		}),
	}, opts...)...)
	park, err := engine.OpenScopedDB(dsn, testdb.Repository(t), engine.RoleApp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = park.Close() })
	f.svc, f.ds, f.db, f.park = svc, ds, rawDB(t, dsn), park
	return f
}

// newestLine is the seq of the newest line addressed to a record.
func (f *heldFixture) newestLine(t *testing.T, id string) int64 {
	t.Helper()
	var seq int64
	if err := f.db.QueryRow(`SELECT max(seq) FROM changelog WHERE record_id = $1`, id).Scan(&seq); err != nil {
		t.Fatal(err)
	}
	return seq
}

func (f *heldFixture) lines(t *testing.T, id string) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(`SELECT count(*) FROM changelog WHERE record_id = $1`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// parkOver parks a failure over a line, which a retry rebuilds its envelope
// from: a hold that ends, here when release takes it away.
func (f *heldFixture) parkOver(t *testing.T, id int, seq int64, record string) {
	t.Helper()
	if _, err := f.park.Exec(`
		INSERT INTO trigger_failures (id, trigger_id, seq, record_id, attempts, last_error, parked_at)
		VALUES ($1, 'parked', $2, $3, 1, 'parked over the line', now())`, id, seq, record); err != nil {
		t.Fatal(err)
	}
}

func (f *heldFixture) release(t *testing.T, ids ...int) {
	t.Helper()
	if _, err := f.park.Exec(`DELETE FROM trigger_failures WHERE id = ANY($1)`, ids); err != nil {
		t.Fatal(err)
	}
}

func (f *heldFixture) pass(t *testing.T) engine.CompactionPassReport {
	t.Helper()
	report, err := f.ds.(passer).CompactionPass(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func (f *heldFixture) status(t *testing.T) engine.CompactionStatus {
	t.Helper()
	st, err := f.svc.(interface {
		CompactionStatus(context.Context, string) (engine.CompactionStatus, error)
	}).CompactionStatus(context.Background(), testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func (f *heldFixture) held(t *testing.T) engine.CompactedKind {
	t.Helper()
	for _, k := range f.status(t).Kinds {
		if k.Kind == "samples.substrate.reamde.dev/tasks/task" {
			return k
		}
	}
	return engine.CompactedKind{}
}

func (f *heldFixture) rebuildRows(t *testing.T) map[string][]string {
	t.Helper()
	if _, err := f.svc.(rebuilder).RebuildRepository(context.Background(), testdb.Repository(t)); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	return engine.FoldRows(t, f.ds)
}

// A record whose every line in reach a parked failure names has no line for
// its state, and is held all the same: once the failures go, the next pass
// compacts it, with no further write to it and the kind's mark past it.
func TestARecordWhoseEveryLineIsHeldIsCompactedOnceTheyGo(t *testing.T) {
	t.Parallel()
	f := newHeldFixture(t)
	const task = "samples.substrate.reamde.dev/tasks/task"
	held := mustPut(t, f.ds, owner, substrate.PutInput{Kind: task, Properties: map[string]any{"name": "held"}})
	created := f.newestLine(t, held.ID)
	mustPatch(t, f.ds, owner, held.Kind, held.ID, substrate.PatchInput{Properties: map[string]any{"name": "held, again"}})
	patched := f.newestLine(t, held.ID)
	mustPut(t, f.ds, owner, substrate.PutInput{Kind: task, Properties: map[string]any{"name": "after"}})
	*f.clock = time.Now().UTC().Add(2 * time.Hour)
	want := f.rebuildRows(t)
	f.parkOver(t, 1, created, held.ID)
	f.parkOver(t, 2, patched, held.ID)
	f.pass(t)
	if n := f.lines(t, held.ID); n != 2 {
		t.Fatalf("the held task has %d lines, want both", n)
	}
	if k := f.held(t); k.Held != 1 {
		t.Fatalf("%d tasks wait on a hold, want the held one", k.Held)
	}
	f.release(t, 1, 2)
	f.pass(t)
	if n := f.lines(t, held.ID); n != 1 {
		t.Fatalf("the held task has %d lines once the failures went, want one state", n)
	}
	if k := f.held(t); k.Held != 0 {
		t.Fatalf("%d tasks still wait on a hold", k.Held)
	}
	if diff := engine.DiffFoldTables(want, f.rebuildRows(t)); len(diff) > 0 {
		t.Fatalf("the compacted history replays into another fold:\n%s", strings.Join(diff, "\n"))
	}
}

// A purged record whose every line a parked failure names has no line for a
// state, and is not forgotten,
// and the pass's mark for deletions stays below it: once the failures go,
// the next pass forgets it and moves the horizon to its deletion.
func TestADeletionWhoseEveryLineIsHeldIsForgottenOnceTheyGo(t *testing.T) {
	t.Parallel()
	f := newHeldFixture(t, engine.WithDeletionsKept(time.Hour), engine.WithTestPurgeKeepsHistory())
	const task = "samples.substrate.reamde.dev/tasks/task"
	gone := mustPut(t, f.ds, owner, substrate.PutInput{Kind: task, Properties: map[string]any{"name": "gone"}})
	created := f.newestLine(t, gone.ID)
	if _, err := f.ds.Delete(context.Background(), owner, gone.Kind, gone.ID, substrate.DeleteInput{Purge: true}); err != nil {
		t.Fatalf("purge: %v", err)
	}
	deletion := f.newestLine(t, gone.ID)
	mustPut(t, f.ds, owner, substrate.PutInput{Kind: task, Properties: map[string]any{"name": "after"}})
	*f.clock = time.Now().UTC().Add(2 * time.Hour)
	want := f.rebuildRows(t)
	before := f.lines(t, gone.ID)
	var parked []int
	rows, err := f.db.Query(`SELECT seq FROM changelog WHERE record_id = $1 ORDER BY seq`, gone.ID)
	if err != nil {
		t.Fatal(err)
	}
	var seqs []int64
	for rows.Next() {
		var seq int64
		if err := rows.Scan(&seq); err != nil {
			t.Fatal(err)
		}
		seqs = append(seqs, seq)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}
	if len(seqs) < 2 || seqs[0] != created || seqs[len(seqs)-1] != deletion {
		t.Fatalf("the purged task's lines are %v: the test proves nothing", seqs)
	}
	for i, seq := range seqs {
		f.parkOver(t, i+1, seq, gone.ID)
		parked = append(parked, i+1)
	}
	if report := f.pass(t); report.Forgotten != 0 {
		t.Fatalf("the pass forgot %d records while failures name every line of the purged one", report.Forgotten)
	}
	if n := f.lines(t, gone.ID); n != before {
		t.Fatalf("the purged task has %d lines, %d before", n, before)
	}
	if st := f.status(t); st.ForgotUpTo >= deletion {
		t.Fatalf("deletions looked at up to %d, want below the held deletion's line %d", st.ForgotUpTo, deletion)
	}
	f.release(t, parked...)
	if report := f.pass(t); report.Forgotten != 1 {
		t.Fatalf("the pass after the failures went forgot %d, want the purged task", report.Forgotten)
	}
	if n := f.lines(t, gone.ID); n != 0 {
		t.Fatalf("the forgotten task still has %d lines", n)
	}
	if st := f.status(t); st.Horizon != deletion {
		t.Fatalf("the horizon is %d, want the deletion's line %d", st.Horizon, deletion)
	}
	if diff := engine.DiffFoldTables(want, f.rebuildRows(t)); len(diff) > 0 {
		t.Fatalf("forgetting the deletion moved the fold:\n%s", strings.Join(diff, "\n"))
	}
}

// Held records are retried one batch a pass, in turn: with more held than
// a batch and the first batch's holds lasting, a pass retries those and no
// more, and the next goes on to the rest, whose holds ended. The status
// says how many passes a round of retries takes, and that the pass ended.
func TestHeldRecordsAreRetriedOneBatchAPassInTurn(t *testing.T) {
	t.Parallel()
	f := newHeldFixture(t)
	const task = "samples.substrate.reamde.dev/tasks/task"
	type heldTask struct {
		id      string
		created int64
	}
	var tasks []heldTask
	for i := range 30 {
		r := mustPut(t, f.ds, owner, substrate.PutInput{Kind: task, Properties: map[string]any{"name": "held " + strconv.Itoa(i)}})
		created := f.newestLine(t, r.ID)
		mustPatch(t, f.ds, owner, r.Kind, r.ID, substrate.PatchInput{Properties: map[string]any{"name": "held, again"}})
		tasks = append(tasks, heldTask{r.ID, created})
	}
	mustPut(t, f.ds, owner, substrate.PutInput{Kind: task, Properties: map[string]any{"name": "after"}})
	slices.SortFunc(tasks, func(a, b heldTask) int { return strings.Compare(a.id, b.id) })
	for i, h := range tasks {
		f.parkOver(t, i+1, h.created, h.id)
	}
	*f.clock = time.Now().UTC().Add(2 * time.Hour)
	f.pass(t)
	if k := f.held(t); k.Held != 30 || k.HeldRound != 2 {
		t.Fatalf("%d tasks wait on a hold, a round in %d passes; want 30 in 2", k.Held, k.HeldRound)
	}
	// The last five, in the order the retries go, are released; the first
	// twenty-five stay held.
	f.release(t, 26, 27, 28, 29, 30)
	f.pass(t)
	for _, h := range tasks[25:] {
		if n := f.lines(t, h.id); n != 2 {
			t.Fatalf("task %s has %d lines after the pass that retried the first batch, want both: the retries took more than a batch", h.id, n)
		}
	}
	f.pass(t)
	for _, h := range tasks[25:] {
		if n := f.lines(t, h.id); n != 1 {
			t.Fatalf("task %s has %d lines after the next pass, want one state", h.id, n)
		}
	}
	if k := f.held(t); k.Held != 25 {
		t.Fatalf("%d tasks wait on a hold, want the 25 still held", k.Held)
	}
	if st := f.status(t); st.LastPass == nil || st.LastPassEnded {
		t.Fatalf("the status says the last pass ended at %v, on its budget %v; want a finished pass", st.LastPass, st.LastPassEnded)
	}
}

// A deletion an effect that stays holds is not forgotten in part. A purged
// record whose creation's line a parked failure names keeps every line,
// since without its creation the kept line would make the row again on a
// rebuild; the pass's mark stays below its deletion, and once the failure
// goes the next pass forgets it whole.
func TestADeletionAKeptLineHoldsIsForgottenOnceItGoes(t *testing.T) {
	t.Parallel()
	var clock time.Time
	svc, ds, dsn := newDatasetWithDSN(t,
		engine.WithCompactionInterval(-1), engine.WithDeletionsKept(time.Hour), engine.WithTestPurgeKeepsHistory(),
		engine.WithTestCompactionClock(func() time.Time { return clock }))
	ctx := context.Background()
	db := rawDB(t, dsn)
	const task = "samples.substrate.reamde.dev/tasks/task"
	gone := mustPut(t, ds, owner, substrate.PutInput{Kind: task, Properties: map[string]any{"name": "gone"}})
	lineOf := func(query string) int64 {
		t.Helper()
		var seq int64
		if err := db.QueryRow(query, gone.ID).Scan(&seq); err != nil {
			t.Fatal(err)
		}
		return seq
	}
	created := lineOf(`SELECT max(seq) FROM changelog WHERE record_id = $1`)
	mustPatch(t, ds, owner, gone.Kind, gone.ID, substrate.PatchInput{Properties: map[string]any{"name": "gone, again"}})
	if _, err := ds.Delete(ctx, owner, gone.Kind, gone.ID, substrate.DeleteInput{Purge: true}); err != nil {
		t.Fatalf("purge: %v", err)
	}
	deletion := lineOf(`SELECT max(seq) FROM changelog WHERE record_id = $1`)
	mustPut(t, ds, owner, substrate.PutInput{Kind: task, Properties: map[string]any{"name": "after"}})
	lines := func() int64 { return lineOf(`SELECT count(*) FROM changelog WHERE record_id = $1`) }
	rebuild := func() {
		t.Helper()
		if _, err := svc.(rebuilder).RebuildRepository(ctx, testdb.Repository(t)); err != nil {
			t.Fatalf("rebuild: %v", err)
		}
	}
	clock = time.Now().UTC().Add(2 * time.Hour)
	rebuild()
	want := engine.FoldRows(t, ds)
	before := lines()

	// The failure the rebuild below folds away again.
	scoped, err := engine.OpenScopedDB(dsn, testdb.Repository(t), engine.RoleApp)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = scoped.Close() }()
	if _, err := scoped.ExecContext(ctx, `
		INSERT INTO trigger_failures (id, trigger_id, seq, record_id, attempts, last_error, parked_at)
		VALUES (1, 'parked', $1, $2, 1, 'parked over the creation', now())`, created, gone.ID); err != nil {
		t.Fatal(err)
	}
	report, err := ds.(passer).CompactionPass(ctx)
	if err != nil || report.Forgotten != 0 {
		t.Fatalf("the pass forgot %d records (%v), want none while a failure names the creation", report.Forgotten, err)
	}
	if n := lines(); n != before {
		t.Fatalf("the held record has %d lines, %d before", n, before)
	}
	var mark int64
	if err := db.QueryRow(`SELECT forgot_upto FROM compaction_state`).Scan(&mark); err != nil || mark >= deletion {
		t.Fatalf("the pass's mark is at %d (%v), want below the held deletion's line %d", mark, err, deletion)
	}
	rebuild()
	if diff := engine.DiffFoldTables(want, engine.FoldRows(t, ds)); len(diff) > 0 {
		t.Fatalf("a held deletion moved the fold:\n%s", strings.Join(diff, "\n"))
	}

	if report, err := ds.(passer).CompactionPass(ctx); err != nil || report.Forgotten != 1 {
		t.Fatalf("the pass after the failure went forgot %d records (%v), want the purged one", report.Forgotten, err)
	}
	if n := lines(); n != 0 {
		t.Fatalf("the forgotten record still has %d lines", n)
	}
	if head, err := ds.Head(ctx); err != nil || head.Horizon != deletion {
		t.Fatalf("the horizon is %d (%v), want the deletion's line %d", head.Horizon, err, deletion)
	}
	rebuild()
	if diff := engine.DiffFoldTables(want, engine.FoldRows(t, ds)); len(diff) > 0 {
		t.Fatalf("forgetting the deletion moved the fold:\n%s", strings.Join(diff, "\n"))
	}
}

// Done when 3: the pass stopped at each of its stages, as a server killed
// there would stop it, then run again, ends where a pass that was never
// stopped ends, and loses nothing: the fold replays as before, each record
// is its state, the old deletion is forgotten and the horizon moved.
func TestAPassStoppedAtEachStageIsFinishedByTheNext(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{
		engine.CompactionInIndex, engine.CompactionInBatch, engine.CompactionAfterBatch,
		engine.CompactionBeforeMark, engine.CompactionToForget, engine.CompactionInForget,
	} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			armed := true
			killed := errors.New("test: the server stopped here")
			clock := time.Now().UTC().Add(2 * time.Hour)
			svc, ds, dsn := newDatasetWithDSN(t,
				engine.WithCompactionInterval(-1), engine.WithDeletionsKept(time.Hour), engine.WithTestPurgeKeepsHistory(),
				engine.WithTestCompactionClock(func() time.Time { return clock }),
				engine.WithTestCompactionRetention(func(kind string) (bool, time.Duration) { return kind != passPkg+"/brief", 0 }),
				engine.WithTestCompactionFault(func(s string) error {
					if armed && s == stage {
						armed = false
						return killed
					}
					return nil
				}))
			ctx := context.Background()
			if _, err := ds.(interface {
				ApplyVocabularyDocuments(context.Context, substrate.Actor, []map[string]any) ([]*substrate.Record, error)
			}).ApplyVocabularyDocuments(ctx, owner, []map[string]any{
				vocabulary.PackageManifest(passPkg, 0),
				vocabulary.KindManifest(passPkg, map[string]any{"singular": "brief"},
					map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
			}); err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{"a", "b", "c"} {
				for _, name := range []string{"one", "two", "three"} {
					mustPut(t, ds, owner, substrate.PutInput{Kind: passPkg + "/brief", ID: fid(id), Properties: map[string]any{"name": name}})
				}
			}
			const task = "samples.substrate.reamde.dev/tasks/task"
			gone := mustPut(t, ds, owner, substrate.PutInput{Kind: task, Properties: map[string]any{"name": "gone"}})
			if _, err := ds.Delete(ctx, owner, gone.Kind, gone.ID, substrate.DeleteInput{Purge: true}); err != nil {
				t.Fatal(err)
			}
			mustPut(t, ds, owner, substrate.PutInput{Kind: task, Properties: map[string]any{"name": "after"}})
			rebuild := func() {
				t.Helper()
				if _, err := svc.(rebuilder).RebuildRepository(ctx, testdb.Repository(t)); err != nil {
					t.Fatalf("rebuild: %v", err)
				}
			}
			rebuild()
			want := engine.FoldRows(t, ds)
			db := rawDB(t, dsn)
			// A repository from before the index, which the pass derives
			// first (a new one is indexed from its creation).
			if _, err := db.Exec(`UPDATE compaction_state SET foreign_upto = 0, foreign_done = false`); err != nil {
				t.Fatal(err)
			}
			if _, err := ds.(passer).CompactionPass(ctx); !errors.Is(err, killed) {
				t.Fatalf("the pass stopped at %q: %v, want the stop", stage, err)
			}
			if report, err := ds.(passer).CompactionPass(ctx); err != nil || !report.Finished {
				t.Fatalf("the next pass: %+v, %v", report, err)
			}
			for _, id := range []string{"a", "b", "c"} {
				var n int
				if err := db.QueryRow(`SELECT count(*) FROM changelog WHERE kind = $1 AND record_id = $2`, passPkg+"/brief", fid(id)).Scan(&n); err != nil || n != 1 {
					t.Errorf("brief %s holds %d lines (%v), want its state alone", id, n, err)
				}
			}
			var left int
			if err := db.QueryRow(`SELECT count(*) FROM changelog WHERE record_id = $1`, gone.ID).Scan(&left); err != nil || left != 0 {
				t.Errorf("the old deletion keeps %d lines (%v), want it forgotten", left, err)
			}
			if head, err := ds.Head(ctx); err != nil || head.Horizon == 0 {
				t.Errorf("the horizon is %d (%v), want the forgotten deletion's line", head.Horizon, err)
			}
			if again, err := ds.(passer).CompactionPass(ctx); err != nil || again.Removed != 0 || again.Forgotten != 0 {
				t.Errorf("a pass after: %+v, %v, want nothing left to do", again, err)
			}
			rebuild()
			if diff := engine.DiffFoldTables(want, engine.FoldRows(t, ds)); len(diff) > 0 {
				t.Fatalf("stopped at %q, the history replays into another fold:\n%s", stage, strings.Join(diff, "\n"))
			}
		})
	}
}

// Done when 2, measured: writes beside a pass. A batch holds the changelog
// lock a write waits for, and the pass sleeps as long as each batch took, so
// a write waits for at most one batch. It reports the latencies and holds
// the 99th percentile beside a pass to three times the one without, plus a
// tenth of a second. Set SUBSTRATE_COMPACTION_TIMING to run it: a timing on
// a shared machine is a measurement, not a check CI runs.
func TestWritesBesideAPassKeepTheirLatency(t *testing.T) {
	if os.Getenv("SUBSTRATE_COMPACTION_TIMING") == "" {
		t.Skip("set SUBSTRATE_COMPACTION_TIMING to time writes beside a pass")
	}
	_, ds, _ := newDatasetWithDSN(t, engine.WithCompactionInterval(-1),
		engine.WithTestCompactionRetention(func(kind string) (bool, time.Duration) { return kind != passPkg+"/brief", 0 }))
	ctx := context.Background()
	if _, err := ds.(interface {
		ApplyVocabularyDocuments(context.Context, substrate.Actor, []map[string]any) ([]*substrate.Record, error)
	}).ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(passPkg, 0),
		vocabulary.KindManifest(passPkg, map[string]any{"singular": "brief"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
	}); err != nil {
		t.Fatal(err)
	}
	for i := range 600 {
		for v := range 5 {
			mustPut(t, ds, owner, substrate.PutInput{
				Kind: passPkg + "/brief", ID: fid("b" + strconv.Itoa(i)),
				Properties: map[string]any{"name": strconv.Itoa(v)},
			})
		}
	}
	const task = "samples.substrate.reamde.dev/tasks/task"
	writes := func(n int) []time.Duration {
		out := make([]time.Duration, 0, n)
		for i := range n {
			started := time.Now()
			mustPut(t, ds, owner, substrate.PutInput{Kind: task, Properties: map[string]any{"name": "w" + strconv.Itoa(i)}})
			out = append(out, time.Since(started))
		}
		slices.Sort(out)
		return out
	}
	pct := func(d []time.Duration, p float64) time.Duration { return d[int(float64(len(d)-1)*p)] }
	alone := writes(300)
	done := make(chan engine.CompactionPassReport, 1)
	go func() {
		report, err := ds.(passer).CompactionPass(ctx)
		if err != nil {
			t.Error(err)
		}
		done <- report
	}()
	beside := writes(300)
	report := <-done
	t.Logf("writes alone:       p50 %s p99 %s max %s", pct(alone, .5), pct(alone, .99), alone[len(alone)-1])
	t.Logf("writes beside pass: p50 %s p99 %s max %s", pct(beside, .5), pct(beside, .99), beside[len(beside)-1])
	t.Logf("the pass: %d records, %d entries removed, in %s", report.Records, report.Removed, report.Took)
	if bound := 3*pct(alone, .99) + 100*time.Millisecond; pct(beside, .99) > bound {
		t.Fatalf("p99 beside a pass is %s, over the bound %s", pct(beside, .99), bound)
	}
}
