package engine

// A REPLAY'S SPEED DOES NOT REST ON WHAT THE PLANNER BELIEVES ABOUT A TABLE
// IT IS FILLING (rebuild.go planEveryStatement, repodir.go refold). Two
// checks, each inside the replay's own transaction:
//
//   - Postgres counts what a transaction itself read
//     (pg_stat_xact_user_tables): at the end of each replay every fold table
//     is held to rows read in table scans no more than a few times the rows
//     the replay wrote there. A replay held to a generic plan made while a
//     table was empty reads every row it has written for every line it
//     folds: rows squared over two.
//   - Those counts do not see an index scan on the wrong index, which walks
//     its entries inside the btree. So at the first page of each replay and
//     at its end, every statement the fold runs once a line to find a row by
//     its key is EXPLAINed there and held to its table's primary key. A
//     statement a replay runs once a line that is not listed fails the test
//     until it is.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// planNotes is how many notes the history holds: far past the five
// executions after which Postgres keeps a generic plan, and enough that a
// table scan a line shows in the counts.
const planNotes = 3000

// planWorld is a repository whose history holds planNotes notes, each
// following the one before, some rewritten and some deleted, archived, in its
// own database.
type planWorld struct {
	dsn, root, repo, id string
	// note is the notes' kind as it is stored, and sample one note's id,
	// which the EXPLAINs below look up.
	note, sample string
}

func (w planWorld) archive(t *testing.T) Option {
	t.Helper()
	return WithArchive(archiveDir(t, filepath.Join(w.root, "archive")))
}

func newPlanWorld(t *testing.T) planWorld {
	t.Helper()
	return newPlanWorldIn(t, MigratedDSN(t), t.TempDir(), testdb.Repository(t), planNotes)
}

// newPlanWorldIn is newPlanWorld in a database and data root already
// holding others, with notes notes.
func newPlanWorldIn(t *testing.T, dsn, root, repo string, notes int) planWorld {
	t.Helper()
	ctx := context.Background()
	w := planWorld{dsn: dsn, root: root, repo: repo}
	svc, err := OpenForTest(t, ctx, w.dsn, WithDataRoot(w.root), w.archive(t), WithArchiveInterval(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	info, err := svc.CreateRepository(ctx, w.repo)
	if err != nil {
		t.Fatal(err)
	}
	w.id = info.ID
	d, err := svc.Dataset(ctx, w.repo)
	if err != nil {
		t.Fatal(err)
	}
	ds := d.(*dataset)
	pkg := w.repo + "/notes"
	note := pkg + "/note"
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "note"}, map[string]any{
			"properties": map[string]any{
				"name":    map[string]any{"type": "string"},
				"follows": map[string]any{"type": "reference", "kind": note},
			},
		}),
	}); err != nil {
		t.Fatalf("declare the notes: %v", err)
	}
	id := func(n int) string { return vocabulary.DerivedMintedID("plans", strconv.Itoa(n)) }
	w.sample = id(notes / 2)
	for from := 0; from < notes; from += 500 {
		if err := ds.inTx(ctx, substrate.ActorPerson, true, func(t *txn) error {
			for n := from; n < min(from+500, notes); n++ {
				props := map[string]any{"name": fmt.Sprintf("note %d", n)}
				if n > 0 {
					props["follows"] = map[string]any{"ref": note + "/" + id(n-1)}
				}
				rec, err := t.put(substrate.PutInput{Kind: note, ID: id(n), Properties: props})
				if err != nil {
					return err
				}
				w.note = rec.Kind
			}
			return nil
		}); err != nil {
			t.Fatalf("write the notes: %v", err)
		}
	}
	// As the scale harness's history does: a note rewritten now and then,
	// and one deleted, so a replay runs every path a line of the fold takes.
	if err := ds.inTx(ctx, substrate.ActorPerson, true, func(t *txn) error {
		for n := range notes {
			switch {
			case n%7 == 3:
				if _, err := t.patch(eref{Kind: w.note, ID: id(n)}, substrate.PatchInput{Properties: map[string]any{"name": fmt.Sprintf("note %d, again", n)}}); err != nil {
					return err
				}
			case n%31 == 5:
				if _, err := t.softDelete(eref{Kind: w.note, ID: id(n)}); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("rewrite and delete notes: %v", err)
	}
	if err := ds.ArchivePass(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	return w
}

// foldKeyedStatements are the statements the fold runs once a line to find a
// row by its key, each with the table whose primary key must serve it and its
// parameters for a note. A replay that runs another statement once a line,
// other than an insert, fails until it is listed here.
var foldKeyedStatements = []struct {
	sql, table string
	// keys are the columns after `repository` the primary key's Index Cond
	// must hold for the lookup to be one probe.
	keys []string
	args func(w planWorld) []any
}{
	{
		sql:   `SELECT id, kind, title, states, at, ends_at, due_at, props, labels, version, kind_version, created_at, updated_at, deleted_at, to_jsonb(finalizers) FROM records WHERE kind = $1 AND id = $2`,
		table: "records",
		keys:  []string{"kind", "id"},
		args:  func(w planWorld) []any { return []any{w.note, w.sample} },
	},
	{
		sql:   `UPDATE records SET deleted_at = $3, version = version + 1, updated_at = $3 WHERE kind = $1 AND id = $2 AND deleted_at IS NULL RETURNING version`,
		table: "records",
		keys:  []string{"kind", "id"},
		args:  func(w planWorld) []any { return []any{w.note, w.sample, time.Now()} },
	},
	{
		sql:   `DELETE FROM refs WHERE src_kind = $1 AND src = $2`,
		table: "refs",
		keys:  []string{"src_kind", "src"},
		args:  func(w planWorld) []any { return []any{w.note, w.sample} },
	},
	// A state entry's replay states its record's annotations and marks
	// again (compact.go restateSideRows).
	{
		sql:   `DELETE FROM annotations WHERE record_kind = $1 AND record_id = $2`,
		table: "annotations",
		keys:  []string{"record_kind", "record_id"},
		args:  func(w planWorld) []any { return []any{w.note, w.sample} },
	},
	{
		sql:   `DELETE FROM property_marks WHERE record_kind = $1 AND record_id = $2`,
		table: "property_marks",
		keys:  []string{"record_kind", "record_id"},
		args:  func(w planWorld) []any { return []any{w.note, w.sample} },
	},
}

// foldOncePerLine is how many times a replay of the plan world runs a
// statement before the test counts it as run once a line: the world's
// history is thousands of lines, and what a replay runs once or twice (a
// clear, a count, a derivation) stays far below.
const foldOncePerLine = 100

var foldSpaces = regexp.MustCompile(`\s+`)

func foldStatement(q string) string { return strings.TrimSpace(foldSpaces.ReplaceAllString(q, " ")) }

// replayWatch is what a test holds every replay of one start to.
type replayWatch struct {
	t *testing.T
	w planWorld

	mu      sync.Mutex
	replays []xactReads
	// planned is where each replay's keyed statements were planned, at its
	// first page and at its end, by replay and moment.
	planned []string
	firsts  map[int]bool
	ran     map[string]int
}

func newReplayWatch(t *testing.T, w planWorld) *replayWatch {
	return &replayWatch{t: t, w: w, firsts: map[int]bool{}, ran: map[string]int{}}
}

// options are the seams the watch reads a replay through.
func (r *replayWatch) options() []Option {
	return []Option{
		WithTestReplay(r.page, r.end),
		WithTestReplaySQL(func(q string) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.ran[foldStatement(q)]++
		}),
	}
}

func (r *replayWatch) page(_ int64, tx *sql.Tx) {
	r.mu.Lock()
	replay := len(r.replays) + 1
	first := !r.firsts[replay]
	r.firsts[replay] = true
	r.mu.Unlock()
	if first {
		r.plan(tx, fmt.Sprintf("replay %d, first page", replay))
	}
}

func (r *replayWatch) end(tx *sql.Tx) {
	r.mu.Lock()
	replay := len(r.replays) + 1
	r.mu.Unlock()
	r.plan(tx, fmt.Sprintf("replay %d, end", replay))
	reads := readXact(r.t, tx)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.replays = append(r.replays, reads)
}

// planCheckPages is the fewest pages a table holds, inside the replay, before
// its keyed statements are held to its primary key: on a table that small any
// plan reads a page or two, and the planner may take another index for it.
const planCheckPages = 32

// plan EXPLAINs every keyed statement on the replay's own connection, inside
// its transaction, and records which indexes of its table the plan reads.
func (r *replayWatch) plan(tx *sql.Tx, when string) {
	for _, s := range foldKeyedStatements {
		var pages int64
		if err := tx.QueryRow(`SELECT pg_relation_size($1::regclass) / current_setting('block_size')::int`, s.table).Scan(&pages); err != nil {
			r.t.Errorf("%s: the size of %s: %v", when, s.table, err)
			continue
		}
		if pages < planCheckPages {
			continue
		}
		var raw []byte
		if err := tx.QueryRow(`EXPLAIN (FORMAT JSON) `+s.sql, s.args(r.w)...).Scan(&raw); err != nil {
			r.t.Errorf("%s: explain %q: %v", when, s.sql, err)
			continue
		}
		var plan []map[string]any
		if err := json.Unmarshal(raw, &plan); err != nil {
			r.t.Errorf("%s: read the plan of %q: %v", when, s.sql, err)
			continue
		}
		paths := planAccess(plan[0]["Plan"], s.table)
		r.mu.Lock()
		r.planned = append(r.planned, fmt.Sprintf("%s: %s on %v", when, s.sql[:min(len(s.sql), 40)], paths))
		r.mu.Unlock()
		if err := keyedAccess(paths, s.table+"_pkey", s.keys); err != nil {
			r.t.Errorf("%s: %q: %v: the planner believes the table smaller than it is", when, s.sql, err)
		}
	}
}

// accessPath is how a plan reads one table: the scan, and for an index
// scan the index and its Index Cond.
type accessPath struct {
	scan, index, cond string
}

func (p accessPath) String() string {
	if p.index == "" {
		return p.scan
	}
	return p.scan + " using " + p.index + " (" + p.cond + ")"
}

// planAccess is every way plan reads table. A bitmap heap scan names the
// table and its bitmap index scans below it name the index, so each of
// those is the path.
func planAccess(plan any, table string) []accessPath {
	node, ok := plan.(map[string]any)
	if !ok {
		return nil
	}
	var out []accessPath
	kind, _ := node["Node Type"].(string)
	subs, _ := node["Plans"].([]any)
	switch {
	case kind == "Bitmap Heap Scan" && node["Relation Name"] == table:
		for _, sub := range subs {
			out = append(out, bitmapIndexes(sub)...)
		}
		return out
	case (kind == "Seq Scan" || kind == "Index Scan" || kind == "Index Only Scan") && node["Relation Name"] == table:
		index, _ := node["Index Name"].(string)
		cond, _ := node["Index Cond"].(string)
		out = append(out, accessPath{scan: kind, index: index, cond: cond})
	}
	for _, sub := range subs {
		out = append(out, planAccess(sub, table)...)
	}
	return out
}

// bitmapIndexes is every bitmap index scan below a bitmap heap scan.
func bitmapIndexes(plan any) []accessPath {
	node, ok := plan.(map[string]any)
	if !ok {
		return nil
	}
	if node["Node Type"] == "Bitmap Index Scan" {
		index, _ := node["Index Name"].(string)
		cond, _ := node["Index Cond"].(string)
		return []accessPath{{scan: "Bitmap Index Scan", index: index, cond: cond}}
	}
	var out []accessPath
	subs, _ := node["Plans"].([]any)
	for _, sub := range subs {
		out = append(out, bitmapIndexes(sub)...)
	}
	return out
}

// keyedAccess holds a key lookup's paths to one probe: at least one path,
// and every one an index scan of the primary key whose Index Cond holds each
// key column after `repository`, not a scan the key narrows only as a filter.
func keyedAccess(paths []accessPath, pkey string, keys []string) error {
	if len(paths) == 0 {
		return fmt.Errorf("no recognized access path")
	}
	for _, p := range paths {
		if p.index != pkey {
			return fmt.Errorf("reads through %s, not %s", p, pkey)
		}
		for _, k := range keys {
			if !strings.Contains(p.cond, "("+k+" = ") {
				return fmt.Errorf("reads through %s, whose Index Cond does not hold %s", p, k)
			}
		}
	}
	return nil
}

// hold holds the start's replays to their counts and to the statements the
// test lists.
func (r *replayWatch) hold(t *testing.T, want int) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.replays) != want {
		t.Fatalf("%d replay(s) ran, want %d", len(r.replays), want)
	}
	for _, p := range r.planned {
		t.Log(p)
	}
	for i, reads := range r.replays {
		for _, table := range foldTables {
			c := reads[table]
			if c.seqScans == 0 && c.idxScans == 0 && c.written == 0 {
				continue
			}
			t.Logf("replay %d, %s: %d table scan(s) read %d row(s), %d index scan(s), %d row(s) written",
				i+1, table, c.seqScans, c.seqRead, c.idxScans, c.written)
			if bound := planScanBound*c.written + planScanSlack; c.seqRead > bound {
				t.Errorf("replay %d read %d rows of %s in table scans, past %d for the %d it wrote: a statement kept a plan made while the table was smaller",
					i+1, c.seqRead, table, bound, c.written)
			}
		}
	}
	listed := map[string]bool{}
	for _, s := range foldKeyedStatements {
		listed[foldStatement(s.sql)] = true
	}
	var unlisted []string
	for q, n := range r.ran {
		if n >= foldOncePerLine && !listed[q] && !strings.HasPrefix(q, "INSERT ") {
			unlisted = append(unlisted, fmt.Sprintf("%d× %s", n, q))
		}
	}
	sort.Strings(unlisted)
	for _, q := range unlisted {
		t.Errorf("a replay runs a statement once a line that foldKeyedStatements does not list, so nothing holds its plan: %s", q)
	}
}

// xactReads is what one replay's transaction read and wrote in each fold
// table, as Postgres counted it.
type xactReads map[string]struct{ seqScans, seqRead, idxScans, written int64 }

func readXact(t *testing.T, tx *sql.Tx) xactReads {
	t.Helper()
	rows, err := tx.Query(`
		SELECT relname, seq_scan, seq_tup_read, coalesce(idx_scan, 0), n_tup_ins + n_tup_upd + n_tup_del
		FROM pg_stat_xact_user_tables
		WHERE schemaname = current_schema() AND relname = ANY (string_to_array($1, ','))`, strings.Join(foldTables, ","))
	if err != nil {
		t.Errorf("read the replay's own counts: %v", err)
		return nil
	}
	defer func() { _ = rows.Close() }()
	out := xactReads{}
	for rows.Next() {
		var name string
		var r struct{ seqScans, seqRead, idxScans, written int64 }
		if err := rows.Scan(&name, &r.seqScans, &r.seqRead, &r.idxScans, &r.written); err != nil {
			t.Errorf("read the replay's own counts: %v", err)
			return nil
		}
		out[name] = r
	}
	if err := rows.Err(); err != nil {
		t.Errorf("read the replay's own counts: %v", err)
		return nil
	}
	return out
}

const (
	// A replay scans a table whole a few times whatever plan it holds: the
	// clear before it, a count, a derivation after it.
	planScanBound = 4
	// A fresh plan picks a table scan while a table is a few pages, where it
	// costs less than an index probe: a few hundred rows a statement, read
	// until the table grows past that, whatever the history's length. Held to
	// a plan made while the table was empty, the replay of planNotes notes
	// reads about 4.8 million.
	planScanSlack = 50000
)

func TestAnImportIntoAnEmptyDatabaseKeepsItsPlans(t *testing.T) {
	t.Parallel()
	w := newPlanWorld(t)
	watch := newReplayWatch(t, w)
	began := time.Now()
	svc, err := OpenForTest(t, context.Background(), MigratedDSN(t), append([]Option{
		WithDataRoot(t.TempDir()), w.archive(t),
		WithArchiveInterval(time.Hour),
	}, watch.options()...)...)
	if err != nil {
		t.Fatalf("the restore: %v", err)
	}
	t.Logf("the restore took %s", time.Since(began).Round(time.Millisecond))
	t.Cleanup(func() { _ = svc.Close() })
	watch.hold(t, 2)
}

// The conversion at a start: the fold tables full of the old fold, both
// replays over them, and autovacuum's ANALYZE of the tables from another
// connection at the second replay's first page, as a minute's wait brings it.
func TestAConversionsReplaysKeepTheirPlans(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := newPlanWorld(t)
	db := rawDBOf(t, w.dsn)
	// Below the last step that refolds, so the start converts and replays.
	below := 0
	for _, s := range conversionSteps {
		if s.Refolds {
			below = s.Dialect - 1
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE changelog_dialect SET dialect = $2 WHERE repository = $1`, w.id, below); err != nil {
		t.Fatal(err)
	}
	watch := newReplayWatch(t, w)
	var analyzed bool
	page := watch.page
	watch2 := []Option{WithTestReplay(func(seq int64, tx *sql.Tx) {
		watch.mu.Lock()
		second := len(watch.replays) == 1
		watch.mu.Unlock()
		if second && !analyzed {
			analyzed = true
			if _, err := db.ExecContext(ctx, `ANALYZE refs, records`); err != nil {
				t.Errorf("analyze beside the replay: %v", err)
			}
		}
		page(seq, tx)
	}, watch.end)}
	began := time.Now()
	svc, err := OpenForTest(t, ctx, w.dsn, append(append([]Option{WithDataRoot(w.root), w.archive(t), WithArchiveInterval(time.Hour)},
		watch.options()...), watch2...)...)
	if err != nil {
		t.Fatalf("the start: %v", err)
	}
	t.Logf("the start took %s", time.Since(began).Round(time.Millisecond))
	t.Cleanup(func() { _ = svc.Close() })
	if !analyzed {
		t.Fatal("the tables were not analyzed beside the second replay")
	}
	watch.hold(t, 2)
}

// The state a build from before one-transaction refolds left a repository in
// when it stopped between its two replays: the first one committed, which
// emptied refs over the pages the old fold's rows still fill, analyzed, and
// the import-progress marker set. The next start finishes the import from
// there.
func TestARefoldAnOlderBuildLeftHalfDoneKeepsItsPlans(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := newPlanWorld(t)
	db := rawDBOf(t, w.dsn)
	for _, q := range []string{
		`DELETE FROM refs WHERE repository = $1`,
		`INSERT INTO import_progress (repository, file_head) SELECT $1, max(seq) FROM changelog WHERE repository = $1`,
	} {
		if _, err := db.ExecContext(ctx, q, w.id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `ANALYZE refs`); err != nil {
		t.Fatal(err)
	}
	watch := newReplayWatch(t, w)
	began := time.Now()
	svc, err := OpenForTest(t, ctx, w.dsn, append([]Option{WithDataRoot(w.root), w.archive(t), WithArchiveInterval(time.Hour)},
		watch.options()...)...)
	if err != nil {
		t.Fatalf("the start: %v", err)
	}
	t.Logf("the start took %s", time.Since(began).Round(time.Millisecond))
	t.Cleanup(func() { _ = svc.Close() })
	watch.hold(t, 2)
}

func TestTheOperatorsRebuildKeepsItsPlans(t *testing.T) {
	t.Parallel()
	w := newPlanWorld(t)
	watch := newReplayWatch(t, w)
	svc, err := OpenForTest(t, context.Background(), w.dsn, append([]Option{
		WithDataRoot(w.root), w.archive(t),
		WithArchiveInterval(time.Hour),
	}, watch.options()...)...)
	if err != nil {
		t.Fatalf("the start: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	began := time.Now()
	if _, err := svc.(*service).RebuildRepository(context.Background(), w.repo); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	t.Logf("the rebuild took %s", time.Since(began).Round(time.Millisecond))
	watch.hold(t, 1)
}

// logLines records every line the engine logs, with its table, for a test
// that holds what a start said.
type logLines struct {
	mu    sync.Mutex
	lines []string
}

func (l *logLines) Enabled(context.Context, slog.Level) bool { return true }

func (l *logLines) Handle(_ context.Context, r slog.Record) error {
	line := r.Message
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "table" {
			line += " table=" + a.Value.String()
		}
		return true
	})
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, line)
	return nil
}

func (l *logLines) WithAttrs([]slog.Attr) slog.Handler { return l }
func (l *logLines) WithGroup(string) slog.Handler      { return l }

// with is every line that holds part.
func (l *logLines) with(part string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, line := range l.lines {
		if strings.Contains(line, part) {
			out = append(out, line)
		}
	}
	return out
}

// olderBuildState leaves w as a build from before one-transaction refolds
// left it between its two replays: refs emptied over the old fold's pages
// and committed, analyzed, and the import-progress marker set.
func olderBuildState(t *testing.T, w planWorld) {
	t.Helper()
	db := rawDBOf(t, w.dsn)
	for _, q := range []string{
		`DELETE FROM refs WHERE repository = $1`,
		`INSERT INTO import_progress (repository, file_head) SELECT $1, max(seq) FROM changelog WHERE repository = $1`,
	} {
		if _, err := db.Exec(q, w.id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`ANALYZE refs`); err != nil {
		t.Fatal(err)
	}
}

// A reset another connection's lock holds up says so before it waits, gives
// up at its bound, and the replay goes ahead: the start finishes, the
// repository serves, and nothing is set aside. Speed is what the reset
// protects, not the fold.
func TestAResetHeldBehindALockGivesUpAndTheReplayGoesAhead(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := newPlanWorldIn(t, MigratedDSN(t), t.TempDir(), testdb.Repository(t), 600)
	olderBuildState(t, w)
	holder, err := rawDBOf(t, w.dsn).BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = holder.Rollback() })
	if _, err := holder.Exec(`LOCK TABLE refs IN SHARE UPDATE EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	logs := &logLines{}
	svc, err := OpenForTest(t, ctx, w.dsn, WithDataRoot(w.root), w.archive(t), WithArchiveInterval(time.Hour),
		WithLogger(slog.New(logs)), WithTestStatisticsLockTimeout(time.Second))
	if err != nil {
		t.Fatalf("the start: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if said := logs.with("vacuuming a fold table whose statistics say it is empty"); !slices.ContainsFunc(said, refsLine) {
		t.Errorf("the start did not say it was about to reset refs: %v", said)
	}
	if warned := logs.with("could not vacuum a fold table"); !slices.ContainsFunc(warned, refsLine) {
		t.Errorf("the start did not warn that the reset of refs could not run: %v", warned)
	}
	if r := svc.(*service).setAsideOf(w.id, ""); r != nil {
		t.Fatalf("the repository was set aside: %+v", r)
	}
	if _, err := svc.Dataset(ctx, w.repo); err != nil {
		t.Fatalf("the repository, served: %v", err)
	}
}

// Two repositories refolded in one start, beside a kind index that is sparse
// and correctly described (its statistics count a few entries over many
// pages, as a B-tree keeps its pages when most entries go): the reset is
// for a relation believed empty, so it vacuums at most once.
func TestTwoRefoldsInOneStartResetAtMostOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, root := MigratedDSN(t), t.TempDir()
	one := newPlanWorldIn(t, dsn, root, testdb.RepositoryLabel(t)+"a.example.com", 600)
	two := newPlanWorldIn(t, dsn, root, testdb.RepositoryLabel(t)+"b.example.com", 600)
	db := rawDBOf(t, dsn)
	// The sparse index: every note of both repositories purged but three,
	// and the table vacuumed, so the notes' kind index keeps its pages over
	// fewer entries than it has pages.
	if _, err := db.Exec(`DELETE FROM records WHERE kind IN ($1, $2) AND (repository, id) NOT IN (
		SELECT repository, id FROM records WHERE kind IN ($1, $2) ORDER BY repository, id LIMIT 3)`, one.note, two.note); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`VACUUM (ANALYZE) records`); err != nil {
		t.Fatal(err)
	}
	// Every other fold table analyzed too, so the only statistics that could
	// call for a reset are the sparse index's.
	if _, err := db.Exec(`ANALYZE ` + strings.Join(foldTables, ", ")); err != nil {
		t.Fatal(err)
	}
	var sparse int
	if err := db.QueryRow(`SELECT count(*) FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid
		WHERE i.indrelid = 'records'::regclass AND c.reltuples > 0 AND c.reltuples < pg_relation_size(c.oid) / current_setting('block_size')::int`).Scan(&sparse); err != nil {
		t.Fatal(err)
	}
	if sparse == 0 {
		t.Fatal("no index of records is sparse: the case does not hold what it says")
	}
	for _, w := range []planWorld{one, two} {
		if _, err := db.Exec(`INSERT INTO import_progress (repository, file_head) SELECT $1, max(seq) FROM changelog WHERE repository = $1`, w.id); err != nil {
			t.Fatal(err)
		}
	}
	logs := &logLines{}
	svc, err := OpenForTest(t, ctx, dsn, WithDataRoot(root), one.archive(t), WithArchiveInterval(time.Hour), WithLogger(slog.New(logs)))
	if err != nil {
		t.Fatalf("the start: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if refolds := logs.with("analyzed the fold tables after a replay"); len(refolds) < 2 {
		t.Fatalf("the start refolded %d repositories, want both", len(refolds))
	}
	vacuums := logs.with("vacuuming a fold table")
	if len(vacuums) > 1 {
		t.Fatalf("the start reset %d times, want at most one: %v", len(vacuums), vacuums)
	}
	if slices.ContainsFunc(vacuums, func(line string) bool { return strings.Contains(line, "table=records") }) {
		t.Fatalf("the start reset records, whose sparse index its statistics describe: %v", vacuums)
	}
	for _, w := range []planWorld{one, two} {
		if _, err := svc.Dataset(ctx, w.repo); err != nil {
			t.Fatalf("%s, served: %v", w.repo, err)
		}
	}
}

func refsLine(line string) bool { return strings.Contains(line, "table=refs") }

// A replay of a compacted history (compact.go): every note compacted into
// its state entry, archived, and rebuilt, the statements a state's replay
// runs once a record held as a line's are. The rebuild's pace per line is
// logged beside the same world's before the compaction.
func TestAReplayOfACompactedHistoryKeepsItsPlans(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := newPlanWorld(t)
	db := rawDBOf(t, w.dsn)
	lines := func() int64 {
		t.Helper()
		var n int64
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM changelog WHERE repository = $1`, w.id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	pace := func(svc substrate.Service) (int64, time.Duration) {
		t.Helper()
		n, began := lines(), time.Now()
		if _, err := svc.(*service).RebuildRepository(ctx, w.repo); err != nil {
			t.Fatalf("rebuild: %v", err)
		}
		return n, time.Since(began)
	}
	svc, err := OpenForTest(t, ctx, w.dsn, WithDataRoot(w.root), w.archive(t), WithArchiveInterval(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	whole, wholeTook := pace(svc)
	report, err := svc.(*service).CompactRepository(ctx, w.repo, 0)
	if err != nil || report.Records < planNotes {
		t.Fatalf("the compaction wrote %d states (%v), want one for every note", report.Records, err)
	}
	d, err := svc.Dataset(ctx, w.repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.(*dataset).ArchivePass(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}

	watch := newReplayWatch(t, w)
	svc, err = OpenForTest(t, ctx, w.dsn, append([]Option{WithDataRoot(w.root), w.archive(t), WithArchiveInterval(time.Hour)},
		watch.options()...)...)
	if err != nil {
		t.Fatalf("the start: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	compacted, compactedTook := pace(svc)
	t.Logf("a rebuild of %d lines took %s (%.0f lines a second); of the %d left after the compaction, %s (%.0f lines a second)",
		whole, wholeTook.Round(time.Millisecond), float64(whole)/wholeTook.Seconds(),
		compacted, compactedTook.Round(time.Millisecond), float64(compacted)/compactedTook.Seconds())
	watch.hold(t, 1)
}
