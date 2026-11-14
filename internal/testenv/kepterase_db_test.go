package testenv_test

// VALIDATION ITEM 2 OVER HISTORY A RELEASE WROTE. Every erasure test of the
// engine writes its history through today's write path; this one erases in
// the kept v0.123.1 directory's import, whose lines 0.x wrote and the
// conversion steps rewrote, so a value in a shape only an old release wrote,
// or only a step left, is erased or the test fails. It holds what a client
// and an operator see, not how the engine erases: the delete's and the
// forget's replies, the reads, the change feed, every store the repository
// writes to, the verify and the rebuild, and the next start.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/testenv"
)

// keptErasedTask is the task the test deletes permanently: v0.123.1 created
// it and patched it three times, so its values sit on several lines.
// keptErasedValues are the values it held that nothing else in the kept
// directory or the seeded packages spells: its name and current
// description, which its record and its history hold, and the description
// and url it replaced and cleared, which only its history holds.
const keptErasedTask = "00000000000000000000000002"

var keptErasedValues = []string{"Ship the fold", "values, replayable", "carry the values, not just the names", "https://example.com/1"}

// keptForgottenWinner is the task the test forgets: it won a merge
// v0.123.1 wrote, of keptMergedLoser, and the merge stands, so its history
// is its creation and the merge's entry, and a read of the loser answers it.
const (
	keptForgottenWinner = "00000000000000000000000005"
	keptMergedLoser     = "00000000000000000000000006"
)

// keptClock is the compaction pass's clock: the day v0.123.1 wrote the kept
// history.
var keptClock = time.Date(2026, time.November, 13, 1, 0, 0, 0, time.UTC)

func TestAnErasureOverAKeptDirectoryLeavesNoValueAndTheRestAsTheReleaseServedIt(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(keptRoot, "v0.123.1")
	root := t.TempDir()
	copyKept(t, filepath.Join(dir, changelogfile.RepositoriesDir), filepath.Join(root, changelogfile.RepositoriesDir))
	dsn := testdb.NewSchema(t)
	// The pass runs when the test says, by a clock at the release's own
	// time, so nothing of the kept history is past a retention or the
	// deletion period whenever the test runs.
	opts := []testenv.Option{testenv.WithDSN(dsn), testenv.WithEngineOptions(
		engine.WithCompactionInterval(-1),
		engine.WithTestCompactionClock(func() time.Time { return keptClock }))}
	k := checkKeptDirectoryIn(t, dir, root, keptConversions, opts...)
	e := k.e
	ds, err := e.Service.Dataset(ctx, k.fixture.Repository)
	if err != nil {
		t.Fatal(err)
	}
	archive := ds.(interface{ ArchivePass(context.Context) error })
	pass := ds.(interface {
		CompactionPass(context.Context) (engine.CompactionPassReport, error)
	})
	operator := e.Service.(engine.Operator)
	erased := keptFind(t, k.got, keptErasedTask)
	winner := keptFind(t, k.got, keptForgottenWinner)
	loser := keptFind(t, k.got, keptMergedLoser)

	// What the delete must take is there to take, in the history and in the
	// archive, and the record carries marks the conversion made.
	if err := archive.ArchivePass(ctx); err != nil {
		t.Fatal(err)
	}
	for _, v := range keptErasedValues {
		left := keptValuesLeft(t, dsn, root, []string{v})
		inFile := slices.ContainsFunc(left, func(s string) bool { return strings.HasPrefix(s, "the file ") })
		if !inFile || len(left) < 2 {
			t.Fatalf("before the delete the scan finds %q only in %v: it must find it in the database and in the archive, or it proves nothing", v, left)
		}
	}
	if n := keptLinesHolding(t, dsn, ds.Repository().ID, keptErasedValues); n < 2 {
		t.Fatalf("the task's values sit on %d line(s) of the imported history, want the release's several", n)
	}
	if marks, _ := erased.Read["marks"].(map[string]any); len(marks) == 0 {
		t.Fatalf("%s carries no marks: the delete would not reach what conversion step 5 made of who set its values", erased.Path)
	}
	winnerFeed := keptFeed(t, e, winner)
	if len(winnerFeed) < 2 {
		t.Fatalf("%s's feed holds %v, want its creation and the merge's entry", winner.Path, winnerFeed)
	}

	// The permanent delete, finished at once where it waits for a trigger.
	before := keptHead(t, e)
	status, body := e.Do(http.MethodDelete, "/api/v1/"+erased.Path+"?purge=true", nil)
	if status != http.StatusOK {
		t.Fatalf("permanently delete %s: %d %s", erased.Path, status, body)
	}
	var deleted struct {
		Erasure *substrate.Erasure `json:"erasure"`
	}
	if err := json.Unmarshal(body, &deleted); err != nil {
		t.Fatalf("decode the delete's reply %s: %v", body, err)
	}
	if deleted.Erasure == nil || deleted.Erasure.Pending {
		keptErase(t, e, pass, erased, true)
	}
	if status, body := e.Do(http.MethodGet, "/api/v1/"+erased.Path, nil); status != http.StatusNotFound {
		t.Fatalf("a read of the deleted task: %d %s, want 404", status, body)
	}

	// The forget of the merge's winner keeps what it is now and erases what
	// it was before.
	if erasure := keptErase(t, e, pass, winner, false); erasure != nil {
		t.Logf("%s: the forget leaves the copies %+v", winner.Path, erasure.Kept)
	}
	if status, body := e.Do(http.MethodGet, "/api/v1/"+winner.Path, nil); status != http.StatusOK {
		t.Fatalf("a read of the forgotten winner: %d %s", status, body)
	} else if d := keptDiff(winner.Read, keptDecode(t, body)); d != "" {
		t.Errorf("the forget changed what %s is now: %s", winner.Path, d)
	}
	if status, body := e.Do(http.MethodGet, "/api/v1/"+loser.Path, nil); status != http.StatusOK {
		t.Errorf("a read of the merged loser after the winner's forget: %d %s, want the winner", status, body)
	} else if id := fmt.Sprint(keptDecode(t, body)["id"]); id != keptForgottenWinner {
		t.Errorf("a read of the merged loser after the winner's forget answers %s, want the winner", id)
	}
	newest := slices.Max(winnerFeed)
	if after := keptFeed(t, e, winner); slices.ContainsFunc(after, func(seq int64) bool { return seq < newest }) {
		t.Errorf("after the forget %s's feed holds %v, an entry from before its newest %d", winner.Path, after, newest)
	}

	wrote := map[int64]bool{}
	for seq := before + 1; seq <= keptHead(t, e); seq++ {
		wrote[seq] = true
	}
	if err := archive.ArchivePass(ctx); err != nil {
		t.Fatal(err)
	}
	if left := keptValuesLeft(t, dsn, root, keptErasedValues); len(left) > 0 {
		t.Errorf("after the permanent delete and the next archive pass, values remain:\n  %s", strings.Join(left, "\n  "))
	}

	// Every other record of the owner's is what the import served, and the
	// history the two left verifies and replays into the same records.
	now := keptRead(t, e)
	for path, was := range k.got {
		if path == erased.Path || !strings.HasPrefix(path, "~/") {
			continue
		}
		if d := keptDiff(was.Read, now[path].Read); d != "" {
			t.Errorf("%s: the delete and the forget changed it: %s", path, d)
		}
	}
	if _, ok := now[erased.Path]; ok {
		t.Errorf("%s is still listed", erased.Path)
	}
	if report, err := operator.VerifyRepository(ctx, k.fixture.Repository); err != nil || !report.OK {
		t.Fatalf("verify the erased history: %+v (%v)", report, err)
	}
	if _, err := operator.RebuildRepository(ctx, k.fixture.Repository); err != nil {
		t.Fatal(err)
	}
	rebuilt := keptRead(t, e)
	for path := range keptPaths(now, rebuilt) {
		if d := keptDiff(now[path].Read, rebuilt[path].Read); d != "" {
			t.Errorf("%s: the erased history rebuilds into another record: %s", path, d)
		}
	}

	// The next start holds every record but the deleted one to what the
	// release served, as the import's own check does.
	e.Stop()
	checkKeptDirectoryAfter(t, dir, root, keptConversions, wrote, opts...)
}

// keptHead is the head of the repository's history the records route
// answers with.
func keptHead(t *testing.T, e *testenv.Env) int64 {
	t.Helper()
	var page struct {
		Head int64 `json:"head"`
	}
	e.MustJSON(http.MethodGet, "/api/v1/records?first=1", nil, &page)
	return page.Head
}

// keptFind is the record of id among the import's own kinds' records.
func keptFind(t *testing.T, got map[string]keptRecord, id string) keptRecord {
	t.Helper()
	for path, rec := range got {
		if strings.HasPrefix(path, "~/tasks/task/") && strings.HasSuffix(path, "/"+id) {
			return rec
		}
	}
	t.Fatalf("the import serves no task %s", id)
	return keptRecord{}
}

// keptErase forgets rec's history, finished past every trigger, and while
// the reply says the erasure is held runs the compaction pass, as a
// server's next passes would: an imported history waits for the pass to
// derive its index. A deleted record whose erasure is done is one the
// forget no longer finds. It fails unless the erasure finishes within a few,
// and answers the forget's last reply, nil for a deleted record.
func keptErase(t *testing.T, e *testenv.Env, pass interface {
	CompactionPass(context.Context) (engine.CompactionPassReport, error)
}, rec keptRecord, deleted bool,
) *substrate.Erasure {
	t.Helper()
	for passes := 0; ; passes++ {
		status, body := e.Do(http.MethodPost, "/api/v1/"+rec.Path+"/forget", map[string]any{"finish": true})
		if deleted && status == http.StatusNotFound {
			t.Logf("%s: erased after %d compaction pass(es)", rec.Path, passes)
			return nil
		}
		if status != http.StatusOK {
			t.Fatalf("forget %s: %d %s", rec.Path, status, body)
		}
		var erasure substrate.Erasure
		if err := json.Unmarshal(body, &erasure); err != nil {
			t.Fatalf("decode the forget's reply %s: %v", body, err)
		}
		if !erasure.Pending {
			t.Logf("%s: erased after %d compaction pass(es)", rec.Path, passes)
			return &erasure
		}
		if passes == 10 {
			t.Fatalf("forget %s with finish, after %d passes: %s, still pending", rec.Path, passes, body)
		}
		if _, err := pass.CompactionPass(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
}

// keptFeed is the seq of every entry the change feed carries for rec.
func keptFeed(t *testing.T, e *testenv.Env, rec keptRecord) []int64 {
	t.Helper()
	q := url.Values{"first": {"500"}, "recordId": {fmt.Sprint(rec.Read["id"])}, "recordKind": {fmt.Sprint(rec.Read["kind"])}}
	var page substrate.ChangePage
	e.MustJSON(http.MethodGet, "/api/v1/changes?"+q.Encode(), nil, &page)
	if page.Cursor != 0 {
		t.Fatalf("%s's feed holds more than one page", rec.Path)
	}
	seqs := make([]int64, 0, len(page.Changes))
	for _, row := range page.Changes {
		seqs = append(seqs, row.Seq)
	}
	return seqs
}

// keptPaths is every path either read holds.
func keptPaths(a, b map[string]keptRecord) map[string]bool {
	out := map[string]bool{}
	for p := range a {
		out[p] = true
	}
	for p := range b {
		out[p] = true
	}
	return out
}

// keptLinesHolding is how many lines of repository's history hold one of
// values.
func keptLinesHolding(t *testing.T, dsn, repository string, values []string) int {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRow(`
		SELECT count(*) FROM changelog c WHERE repository = $1
		AND EXISTS (SELECT 1 FROM unnest($2::text[]) v WHERE strpos(c.payload::text, v) > 0)`, repository, values).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// keptValuesLeft names every place that holds one of values: a row of any
// table of the database's schema, as text; any bytea column, by its bytes,
// a stored Idempotency-Key outcome among them; and any file under the data
// root, the archive's included, by its contents or its name.
func keptValuesLeft(t *testing.T, dsn, root string, values []string) []string {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	quote := func(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }
	var out []string
	for _, table := range keptStrings(t, db, `
		SELECT table_name FROM information_schema.tables
		WHERE table_schema = current_schema() AND table_type = 'BASE TABLE' ORDER BY 1`) {
		for _, v := range values {
			var n int
			if err := db.QueryRow(`SELECT count(*) FROM `+quote(table)+` x WHERE strpos(x::text, $1) > 0`, v).Scan(&n); err != nil {
				t.Fatalf("scan %s: %v", table, err)
			}
			if n > 0 {
				out = append(out, fmt.Sprintf("%d row(s) of %s hold %q", n, table, v))
			}
		}
	}
	for _, col := range keptStrings(t, db, `
		SELECT c.table_name || '.' || c.column_name FROM information_schema.columns c
		JOIN information_schema.tables t ON t.table_schema = c.table_schema AND t.table_name = c.table_name
		WHERE c.table_schema = current_schema() AND t.table_type = 'BASE TABLE' AND c.data_type = 'bytea' ORDER BY 1`) {
		table, column, _ := strings.Cut(col, ".")
		for _, v := range values {
			var n int
			if err := db.QueryRow(`SELECT count(*) FROM `+quote(table)+` WHERE position(convert_to($1, 'UTF8') in coalesce(`+quote(column)+`, ''::bytea)) > 0`, v).Scan(&n); err != nil {
				t.Fatalf("scan %s: %v", col, err)
			}
			if n > 0 {
				out = append(out, fmt.Sprintf("%d value(s) of %s hold %q", n, col, v))
			}
		}
	}
	if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		for _, v := range values {
			if strings.Contains(string(raw), v) || strings.Contains(d.Name(), v) {
				out = append(out, fmt.Sprintf("the file %s holds %q", rel, v))
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("a file under the data root does not read, so the scan cannot say what it holds: %v", err)
	}
	return out
}

func keptStrings(t *testing.T, db *sql.DB, query string) []string {
	t.Helper()
	rows, err := db.Query(query)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}
