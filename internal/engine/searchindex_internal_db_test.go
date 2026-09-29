package engine

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

const (
	reindexPerson = "samples.substrate.reamde.dev/people/person"
	reindexTask   = "samples.substrate.reamde.dev/tasks/task"
	// reindexRows is what staleSearchIndex writes of each kind; at a page of
	// reindexPage rows each kind spans several pages.
	reindexRows = 7
	reindexPage = 3
)

// staleSearchIndex creates a repository with rows of two kinds, then stands
// for one indexed under older rules: every row's `fts` holds one word no
// derivation produces, and the stored version is 1. It returns the DSN and
// data root a reopen needs.
func staleSearchIndex(t *testing.T) (dsn, root, repo string) {
	t.Helper()
	ctx := context.Background()
	dsn, root, repo = MigratedDSN(t), t.TempDir(), testdb.Repository(t)
	svc, err := OpenForTest(t, ctx, dsn, WithDataRoot(root))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.Close() }()
	if _, err := svc.CreateRepository(ctx, repo); err != nil {
		t.Fatal(err)
	}
	d, err := svc.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	importVocabulary(t, d, "tasks")
	ds := d.(*dataset)
	for i := range reindexRows {
		mustPutInternal(t, ds, substrate.PutInput{
			Kind: reindexPerson, ID: fmt.Sprintf("p%d", i),
			Properties: map[string]any{"name": fmt.Sprintf("José %d", i), "emails": []any{fmt.Sprintf("p%d@inbox.example", i)}},
		})
		mustPutInternal(t, ds, substrate.PutInput{
			Kind: reindexTask, ID: fmt.Sprintf("t%d", i),
			Properties: map[string]any{"name": fmt.Sprintf("Read https://docs.example.com/page%d", i)},
		})
	}
	db := ds.db
	if _, err := db.ExecContext(ctx, `UPDATE records SET fts = to_tsvector('english', 'plantedstale')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE search_index SET version = 1`); err != nil {
		t.Fatal(err)
	}
	return dsn, root, repo
}

// searchIndexVersionOf reads the stored version through the dataset's pool.
func searchIndexVersionOf(t *testing.T, ds *dataset) int {
	t.Helper()
	var v int
	if err := ds.db.QueryRowContext(context.Background(), `SELECT version FROM search_index`).Scan(&v); err != nil {
		t.Fatalf("read the search index version: %v", err)
	}
	return v
}

// underivedRows names every row whose `fts` is not what rederiveFTS derives
// for it now under the published registry.
func underivedRows(t *testing.T, ds *dataset) []string {
	t.Helper()
	ctx := context.Background()
	rows, err := ds.scanRows(ctx, ds.db, `SELECT `+recordCols+` FROM records ORDER BY kind, id`, nil)
	if err != nil {
		t.Fatal(err)
	}
	reg := ds.registry()
	var out []string
	for _, row := range rows {
		bands := ftsBandsUnder(reg, row)
		var same bool
		if err := ds.db.QueryRowContext(ctx, `
			SELECT coalesce(fts = setweight(to_tsvector('english', $3), 'A') ||
			                      setweight(to_tsvector('english', $4), 'B') ||
			                      setweight(to_tsvector('english', $5), 'C'), false)
			FROM records WHERE kind = $1 AND id = $2`,
			row.Kind, row.ID, bands[0], bands[1], bands[2]).Scan(&same); err != nil {
			t.Fatal(err)
		}
		if !same {
			out = append(out, row.Kind+"/"+row.ID)
		}
	}
	return out
}

// plantedRows counts the rows still holding the planted index.
func plantedRows(t *testing.T, ds *dataset) int {
	t.Helper()
	var n int
	if err := ds.db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM records WHERE fts @@ to_tsquery('english', 'plantedstale')`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func waitReindex(t *testing.T, ds *dataset) {
	t.Helper()
	select {
	case <-SearchReindexDone(ds):
	case <-time.After(time.Minute):
		t.Fatal("the reindex did not return within a minute")
	}
}

// holdSecondPage is a reindex hook that closes reached before the second page
// and holds the reindex there until the dataset closes: one page has
// committed, the rest wait.
func holdSecondPage(reached chan struct{}) func(context.Context, string) error {
	var pages atomic.Int32
	return func(ctx context.Context, kind string) error {
		if pages.Add(1) != 2 {
			return nil
		}
		close(reached)
		<-ctx.Done()
		return ctx.Err()
	}
}

func openReindexing(t *testing.T, dsn, root, repo string, opts ...Option) (substrate.Service, *dataset) {
	t.Helper()
	svc, err := OpenForTest(t, context.Background(), dsn, append([]Option{WithDataRoot(root)}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	d, err := svc.Dataset(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	return svc, d.(*dataset)
}

// The open after a rules change answers at once and serves reads and writes
// while the reindex runs: a row keeps its old index until its page, a write
// in the middle lands its own, and at the end every row holds what a fresh
// derivation gives, the version is recorded, and the log names each kind.
func TestTheSearchReindexServesRequestsUntilItFinishes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, root, repo := staleSearchIndex(t)

	reached := make(chan struct{})
	release := make(chan struct{})
	var pages atomic.Int32
	hook := func(ctx context.Context, kind string) error {
		// Held before the second page: one page has committed, the rest wait.
		if pages.Add(1) != 2 {
			return nil
		}
		close(reached)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil
	}
	logs := &syncBuffer{}
	svc, err := OpenForTest(t, ctx, dsn, WithDataRoot(root),
		WithTestSearchReindex(reindexPage, hook),
		WithLogger(slog.New(slog.NewTextHandler(logs, nil))))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	// The open answers while the hook holds the reindex. An open that ran the
	// reindex itself would wait on the hook, so the deadline releases it
	// before failing, or the cleanup's Close would wait on the open.
	opened := make(chan substrate.Dataset, 1)
	go func() {
		d, err := svc.Dataset(ctx, repo)
		if err != nil {
			t.Error(err)
		}
		opened <- d
	}()
	var ds *dataset
	select {
	case d := <-opened:
		if d == nil {
			t.FailNow()
		}
		ds = d.(*dataset)
	case <-time.After(20 * time.Second):
		close(release)
		t.Fatal("the open waited for the reindex")
	}
	select {
	case <-reached:
	case <-time.After(time.Minute):
		t.Fatal("the reindex never reached its second page")
	}

	// Held mid-reindex: a list answers, a write lands, the old index is still
	// the one most rows carry, and the version is not recorded yet.
	listCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	page, err := ds.List(listCtx, substrate.Query{Filter: substrate.Filter{Kinds: []string{reindexPerson}}, First: 100})
	if err != nil {
		t.Fatalf("a records list during the reindex: %v", err)
	}
	if len(page.Records) != reindexRows {
		t.Fatalf("the list during the reindex answered %d persons, want %d", len(page.Records), reindexRows)
	}
	mustPutInternal(t, ds, substrate.PutInput{
		Kind: reindexPerson, ID: "p0",
		Properties: map[string]any{"name": "Zoë Written", "emails": []any{"zoe@midway.example"}},
	})
	if n := plantedRows(t, ds); n < reindexRows {
		t.Fatalf("%d rows still carry the old index mid-reindex, want at least %d", n, reindexRows)
	}
	if v := searchIndexVersionOf(t, ds); v != 1 {
		t.Fatalf("the version moved to %d before the reindex finished", v)
	}

	close(release)
	waitReindex(t, ds)
	if v := searchIndexVersionOf(t, ds); v != searchIndexVersion {
		t.Fatalf("the finished reindex recorded version %d, want %d", v, searchIndexVersion)
	}
	if stale := underivedRows(t, ds); len(stale) != 0 {
		t.Fatalf("rows whose fts is not a fresh derivation after the reindex: %v", stale)
	}
	if n := plantedRows(t, ds); n != 0 {
		t.Fatalf("%d rows still carry the old index after the reindex", n)
	}
	got := logs.String()
	for _, kind := range []string{reindexPerson, reindexTask} {
		if !logLine(got, `msg="substrate: re-derived the search index of one kind"`, "kind="+kind+" ") {
			t.Fatalf("the log has no per-kind line for %s:\n%s", kind, got)
		}
	}
	if !logLine(got, `msg="substrate: re-derived the search index" `, "to=2 ") {
		t.Fatalf("the log has no line for the finished reindex:\n%s", got)
	}
}

// logLine reports whether one line of logs carries every part.
func logLine(logs string, parts ...string) bool {
	for line := range strings.SplitSeq(logs, "\n") {
		found := true
		for _, p := range parts {
			if !strings.Contains(line, p) {
				found = false
				break
			}
		}
		if found {
			return true
		}
	}
	return false
}

func recordCount(t *testing.T, ds *dataset) int {
	t.Helper()
	var n int
	if err := ds.db.QueryRowContext(context.Background(), `SELECT count(*) FROM records`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A reindex the close interrupts records nothing, a read-only open leaves it
// alone, and the next writer's open runs it to the end.
func TestAnInterruptedSearchReindexCompletesAtTheNextOpen(t *testing.T) {
	t.Parallel()
	dsn, root, repo := staleSearchIndex(t)

	reached := make(chan struct{})
	svc, ds := openReindexing(t, dsn, root, repo, WithTestSearchReindex(reindexPage, holdSecondPage(reached)))
	select {
	case <-reached:
	case <-time.After(time.Minute):
		t.Fatal("the reindex never reached its second page")
	}
	closed := time.Now()
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(closed); took > backgroundDrainTimeout/2 {
		t.Fatalf("closing a dataset mid-reindex took %s", took)
	}
	waitReindex(t, ds)

	ro, rods := openReindexing(t, dsn, root, repo, WithDirectoryReadOnly())
	waitReindex(t, rods)
	if rods.reindexCancel != nil {
		t.Fatal("a read-only open started a reindex")
	}
	if v := searchIndexVersionOf(t, rods); v != 1 {
		t.Fatalf("the interrupted reindex recorded version %d, want 1", v)
	}
	if n, total := plantedRows(t, rods), recordCount(t, rods); n == 0 || n >= total {
		t.Fatalf("%d of %d rows carry the old index after the interruption, want some and not all", n, total)
	}
	_ = ro.Close()

	_, ds2 := openReindexing(t, dsn, root, repo, WithTestSearchReindex(reindexPage, nil))
	waitReindex(t, ds2)
	if v := searchIndexVersionOf(t, ds2); v != searchIndexVersion {
		t.Fatalf("the resumed reindex recorded version %d, want %d", v, searchIndexVersion)
	}
	if stale := underivedRows(t, ds2); len(stale) != 0 {
		t.Fatalf("rows whose fts is not a fresh derivation after the resumed reindex: %v", stale)
	}
}

// A rebuild stops the open's reindex rather than race it, and records the
// version itself: the replay indexed every row under this binary's rules.
func TestARebuildStopsTheSearchReindexAndRecordsTheVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, root, repo := staleSearchIndex(t)

	reached := make(chan struct{})
	svc, ds := openReindexing(t, dsn, root, repo, WithTestSearchReindex(reindexPage, holdSecondPage(reached)))
	select {
	case <-reached:
	case <-time.After(time.Minute):
		t.Fatal("the reindex never reached its second page")
	}
	if _, err := svc.(*service).RebuildRepository(ctx, repo); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	select {
	case <-SearchReindexDone(ds):
	default:
		t.Fatal("the rebuild returned with the reindex still running")
	}
	if v := searchIndexVersionOf(t, ds); v != searchIndexVersion {
		t.Fatalf("the rebuild recorded version %d, want %d", v, searchIndexVersion)
	}
	if stale := underivedRows(t, ds); len(stale) != 0 {
		t.Fatalf("rows whose fts is not a fresh derivation after the rebuild: %v", stale)
	}
}

// A rebuild that fails after it stopped the reindex starts it again, so the
// index is not left half re-derived until the next restart.
func TestAFailedRebuildStartsTheSearchReindexAgain(t *testing.T) {
	t.Parallel()
	dsn, root, repo := staleSearchIndex(t)

	// The rebuild's own context ends as the rebuild stops the reindex, so the
	// rebuild fails at its first statement after the stop.
	rebuildCtx, failRebuild := context.WithCancel(context.Background())
	defer failRebuild()
	reached := make(chan struct{})
	var pages atomic.Int32
	hook := func(ctx context.Context, kind string) error {
		if pages.Add(1) != 2 {
			return nil
		}
		close(reached)
		<-ctx.Done()
		failRebuild()
		return ctx.Err()
	}
	svc, ds := openReindexing(t, dsn, root, repo, WithTestSearchReindex(reindexPage, hook))
	select {
	case <-reached:
	case <-time.After(time.Minute):
		t.Fatal("the reindex never reached its second page")
	}
	if _, err := svc.(*service).RebuildRepository(rebuildCtx, repo); err == nil {
		t.Fatal("the rebuild succeeded on a canceled context")
	}
	waitReindex(t, ds)
	if v := searchIndexVersionOf(t, ds); v != searchIndexVersion {
		t.Fatalf("after the failed rebuild the reindex recorded version %d, want %d", v, searchIndexVersion)
	}
	if stale := underivedRows(t, ds); len(stale) != 0 {
		t.Fatalf("rows whose fts is not a fresh derivation after the restarted reindex: %v", stale)
	}
}

// A row a write holds when its page comes is skipped rather than waited on,
// and redone alone once the write lets go; the version waits for it.
func TestTheSearchReindexRedoesARowAWriteHeld(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, root, repo := staleSearchIndex(t)

	raw, err := OpenScopedDB(dsn, repo, RoleApp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	held, err := raw.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Rollback() }()
	if _, err := held.ExecContext(ctx, `SELECT 1 FROM records WHERE kind = $1 AND id = 'p1' FOR UPDATE`, reindexPerson); err != nil {
		t.Fatal(err)
	}

	_, ds := openReindexing(t, dsn, root, repo, WithTestSearchReindex(reindexPage, nil))
	planted := func() []string {
		t.Helper()
		rows, err := ds.db.QueryContext(ctx, `SELECT id FROM records WHERE kind = $1
			AND fts @@ to_tsquery('english', 'plantedstale') ORDER BY id`, reindexPerson)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rows.Close() }()
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, id)
		}
		return ids
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		if ids := planted(); len(ids) == 1 && ids[0] == "p1" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the reindex did not redo every person but the held one; still planted: %v", planted())
		}
		time.Sleep(20 * time.Millisecond)
	}
	select {
	case <-SearchReindexDone(ds):
		t.Fatal("the reindex finished while a row it had not redone was held")
	case <-time.After(200 * time.Millisecond):
	}
	if v := searchIndexVersionOf(t, ds); v != 1 {
		t.Fatalf("the version moved to %d with a row still held", v)
	}

	if err := held.Commit(); err != nil {
		t.Fatal(err)
	}
	waitReindex(t, ds)
	if v := searchIndexVersionOf(t, ds); v != searchIndexVersion {
		t.Fatalf("the reindex recorded version %d, want %d", v, searchIndexVersion)
	}
	if stale := underivedRows(t, ds); len(stale) != 0 {
		t.Fatalf("rows whose fts is not a fresh derivation after the reindex: %v", stale)
	}
}

// A page that fails is tried again after a pause, so one database error does
// not leave the index half re-derived until the next restart.
func TestASearchReindexPageThatFailsIsTriedAgain(t *testing.T) {
	t.Parallel()
	dsn, root, repo := staleSearchIndex(t)

	var pages atomic.Int32
	hook := func(ctx context.Context, kind string) error {
		if pages.Add(1) == 2 {
			return fmt.Errorf("the database went away for a moment")
		}
		return nil
	}
	logs := &syncBuffer{}
	_, ds := openReindexing(t, dsn, root, repo,
		WithTestSearchReindex(reindexPage, hook),
		WithLogger(slog.New(slog.NewTextHandler(logs, nil))))
	waitReindex(t, ds)
	if v := searchIndexVersionOf(t, ds); v != searchIndexVersion {
		t.Fatalf("the reindex recorded version %d after a failed page, want %d", v, searchIndexVersion)
	}
	if stale := underivedRows(t, ds); len(stale) != 0 {
		t.Fatalf("rows whose fts is not a fresh derivation after the reindex: %v", stale)
	}
	if got := logs.String(); !logLine(got, "level=ERROR", "trying it again", "the database went away for a moment") {
		t.Fatalf("the failed page was not logged:\n%s", got)
	}
}
