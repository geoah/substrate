package engine_test

// THE COMPACTION (compact.go, decision 0296) and the fold: a history
// compacted, every record up to its newest entry, replays into the fold it
// replayed into before, row for row, versions and times included; and a
// watch resumed from inside what it replaced misses no record. The archive,
// an import and an older binary are compact_internal_db_test.go's.

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

type compactor interface {
	CompactRepository(ctx context.Context, repository string, upTo int64) (engine.CompactReport, error)
}

func compactTo(t *testing.T, svc substrate.Service, upTo int64) engine.CompactReport {
	t.Helper()
	report, err := svc.(compactor).CompactRepository(context.Background(), testdb.Repository(t), upTo)
	if err != nil {
		t.Fatalf("compact up to %d: %v", upTo, err)
	}
	return report
}

// writeChangedDeclaration declares a kind, writes under it, declares it
// again with a property more, and writes under both versions.
func writeChangedDeclaration(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	ctx := context.Background()
	const pkg = "compact.example.substrate.reamde.dev/compact"
	note := func(props map[string]any) []map[string]any {
		return []map[string]any{
			vocabulary.PackageManifest(pkg, 0),
			vocabulary.KindManifest(pkg, map[string]any{"singular": "note"}, map[string]any{"properties": props}),
		}
	}
	name := map[string]any{"type": "string"}
	if _, err := ds.(interface {
		ApplyVocabularyDocuments(context.Context, substrate.Actor, []map[string]any) ([]*substrate.Record, error)
	}).ApplyVocabularyDocuments(ctx, owner, note(map[string]any{"name": name})); err != nil {
		t.Fatalf("declare the note kind: %v", err)
	}
	first := mustPut(t, ds, owner, substrate.PutInput{Kind: pkg + "/note", Properties: map[string]any{"name": "first"}})
	if _, err := ds.(interface {
		ApplyVocabularyDocuments(context.Context, substrate.Actor, []map[string]any) ([]*substrate.Record, error)
	}).ApplyVocabularyDocuments(ctx, owner, note(map[string]any{"name": name, "topic": name})); err != nil {
		t.Fatalf("declare the note kind again: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: pkg + "/note", Properties: map[string]any{"name": "second", "topic": "later"}})
	mustPatch(t, ds, owner, first.Kind, first.ID, substrate.PatchInput{Properties: map[string]any{"topic": "after the change"}})
}

// historyCount is the changelog's entries and its head.
func historyCount(t *testing.T, db *sql.DB) (n, head int64) {
	t.Helper()
	if err := db.QueryRow(`SELECT count(*), coalesce(max(seq), 0) FROM changelog`).Scan(&n, &head); err != nil {
		t.Fatal(err)
	}
	return n, head
}

// Done when 1, and 4: a repository with every kind of row a rebuild fills,
// compacted first up to a seq in the middle (records changed after it keep
// their later entries, on top of a state taken as of then) and then every
// record up to its newest entry, replays each time into the fold the
// history replayed into before it was compacted: every table a rebuild
// fills, read from the schema, row for row, versions and times included. A
// table the fixture leaves empty fails by name. The fold it is held to is
// the uncompacted history's replay, which TestRebuildReproducesTheFold holds
// to the live fold: a history with no state entry runs no new code.
func TestACompactedHistoryReplaysToTheSameFold(t *testing.T) {
	// Not parallel: WriteDeliveryHistory sets the engine's drain caps.
	var stop atomic.Bool
	svc, ds, dsn := newDatasetWithDSN(t, engine.WithTestCompactionFault(func(stage string) error {
		if stage == engine.ErasureBetween && stop.Load() {
			return errors.New("stopped between the forget and its erasure")
		}
		return nil
	}))
	writeSomeHistory(t, ds)
	installPeopleSourcesWithDir(t, ds)
	writeMappedHistory(t, ds)
	writeObservedHistory(t, ds)
	writeChangedDeclaration(t, ds)
	engine.WriteDeliveryHistory(t, ds)
	// An erasure that waits: a forget stopped between its transactions
	// leaves its intent, which no compaction takes and the replay keeps.
	forgotten := mustPut(t, ds, owner, substrate.PutInput{Kind: "samples.substrate.reamde.dev/tasks/task", Properties: map[string]any{"name": "forgotten later"}})
	stop.Store(true)
	if _, err := ds.Forget(context.Background(), owner, forgotten.Kind, forgotten.ID, substrate.ForgetInput{}); err == nil {
		t.Fatal("the forget was not stopped between its transactions")
	}
	stop.Store(false)
	db := rawDB(t, dsn)

	rebuild := func() {
		t.Helper()
		if _, err := svc.(rebuilder).RebuildRepository(context.Background(), testdb.Repository(t)); err != nil {
			t.Fatalf("rebuild: %v", err)
		}
	}
	rebuild()
	want := engine.FoldRows(t, ds)
	for _, table := range engine.FoldTables() {
		if len(want[table]) == 0 {
			t.Errorf("the fixture fills no row of %s: a compaction would prove nothing about it", table)
		}
	}
	if t.Failed() {
		t.FailNow()
	}
	entries, head := historyCount(t, db)

	for _, upTo := range []int64{head / 2, 0} {
		report := compactTo(t, svc, upTo)
		if report.Records == 0 || report.Effects == 0 {
			t.Fatalf("compacting up to %d replaced nothing: %+v", upTo, report)
		}
		rebuild()
		if diff := engine.DiffFoldTables(want, engine.FoldRows(t, ds)); len(diff) > 0 {
			t.Fatalf("compacted up to %d, the history replays into another fold:\n%s", report.UpTo, strings.Join(diff, "\n"))
		}
	}
	n, h := historyCount(t, db)
	if n >= entries || h != head {
		t.Fatalf("the compacted history holds %d entries to head %d; before, %d to head %d", n, h, entries, head)
	}
	// A second compaction finds nothing more to replace.
	if report := compactTo(t, svc, 0); report.Removed != 0 {
		t.Fatalf("a second compaction removed %d entries: %+v", report.Removed, report)
	}
}

// Done when 5, the watch: a watch resumed from inside the range a
// compaction replaced reads each record the uncompacted history changed
// after its position, once, and none it did not.
func TestAWatchResumedInsideACompactedRangeSeesEveryRecordItWouldHaveOnce(t *testing.T) {
	t.Parallel()
	svc, ds, _ := newDatasetWithDSN(t)
	const task = "samples.substrate.reamde.dev/tasks/task"
	put := func(name string) *substrate.Record {
		return mustPut(t, ds, owner, substrate.PutInput{Kind: task, Properties: map[string]any{"name": name}})
	}
	a, b, c, still := put("a"), put("b"), put("c"), put("untouched")
	mustPatch(t, ds, owner, a.Kind, a.ID, substrate.PatchInput{Properties: map[string]any{"name": "a1"}})
	resume := maxSeq(t, ds)
	mustPatch(t, ds, owner, a.Kind, a.ID, substrate.PatchInput{Properties: map[string]any{"name": "a2"}})
	mustPatch(t, ds, owner, a.Kind, a.ID, substrate.PatchInput{Properties: map[string]any{"name": "a3"}})
	mustPatch(t, ds, owner, b.Kind, b.ID, substrate.PatchInput{Properties: map[string]any{"name": "b1"}})
	d := put("d")
	mustPatch(t, ds, owner, d.Kind, d.ID, substrate.PatchInput{Properties: map[string]any{"name": "d1"}})
	if _, err := ds.Delete(context.Background(), owner, c.Kind, c.ID, substrate.DeleteInput{}); err != nil {
		t.Fatal(err)
	}

	read := func() map[string]int {
		t.Helper()
		seen := map[string]int{}
		for after := resume; ; {
			page, err := ds.Changes(context.Background(), after, substrate.ChangeFilter{}, 100)
			if err != nil {
				t.Fatal(err)
			}
			if len(page) == 0 {
				return seen
			}
			for _, ch := range page {
				after = ch.Seq
				for _, rec := range ch.Affected {
					if rec.Kind == task {
						seen[rec.ID]++
					}
				}
			}
		}
	}
	before := read()
	report := compactTo(t, svc, 0)
	if report.Removed == 0 {
		t.Fatalf("the compaction removed nothing: %+v", report)
	}
	after := read()
	for id := range before {
		if after[id] != 1 {
			t.Errorf("record %s, changed after the watch's position, is read %d times after the compaction, want once", id, after[id])
		}
	}
	for id, n := range after {
		if before[id] == 0 {
			t.Errorf("record %s, unchanged after the watch's position, is read %d times after the compaction", id, n)
		}
	}
	if before[still.ID] != 0 || after[still.ID] != 0 {
		t.Errorf("the untouched record is read %d times before and %d after", before[still.ID], after[still.ID])
	}
}

// writeFunc is an io.Writer that is a function.
type writeFunc func([]byte) (int, error)

func (f writeFunc) Write(p []byte) (int, error) { return f(p) }

// lineSums is every line of a changelog table by seq, as its checksum.
func lineSums(t *testing.T, db *sql.DB) map[int64]string {
	t.Helper()
	rows, err := db.Query(`SELECT seq, encode(hash, 'hex') FROM changelog`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	out := map[int64]string{}
	for rows.Next() {
		var seq int64
		var sum string
		if err := rows.Scan(&seq, &sum); err != nil {
			t.Fatal(err)
		}
		out[seq] = sum
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// restoredExport extracts an export into a fresh data root and boots a
// server over an empty database on it, returning the repository, that
// database and the root.
func restoredExport(t *testing.T, archive []byte) (substrate.Dataset, string, string) {
	t.Helper()
	root := t.TempDir()
	extractExport(t, root, archive)
	dsn := engine.MigratedDSN(t)
	svc := mustReopen(t, dsn, root)
	t.Cleanup(func() { _ = svc.Close() })
	ds, err := svc.Dataset(context.Background(), testdb.Repository(t))
	if err != nil {
		t.Fatalf("open the restored repository: %v", err)
	}
	return ds, dsn, root
}

// An export streams every line as of its point, whatever a compaction
// commits after the pin: one committed before the history is streamed, and
// one committed between two of its pages, below a head they leave as it
// was. The server restored from it holds the table's lines at the pin, each
// with its checksum.
func TestAnExportStreamsTheHistoryAsOfItsPoint(t *testing.T) {
	// Not parallel: the history is read eight rows at a time.
	engine.SetArchivePageRows(t, 8)
	for _, during := range []bool{false, true} {
		name := "a compaction after the pin"
		if during {
			name = "a compaction between two pages"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			svc, ds, dsn := newDatasetWithDSN(t, engine.WithArchivePieceBytes(1))
			writeSomeHistory(t, ds)
			writeChangedDeclaration(t, ds)
			db := rawDB(t, dsn)
			pinned := lineSums(t, db)
			_, head := historyCount(t, db)
			ex, err := ds.(interface {
				Export(context.Context) (substrate.Export, error)
			}).Export(ctx)
			if err != nil {
				t.Fatalf("export: %v", err)
			}
			compact := func() {
				t.Helper()
				if report := compactTo(t, svc, head-1); report.Removed == 0 {
					t.Fatalf("the compaction removed nothing: %+v", report)
				}
			}
			compacted := !during
			if compacted {
				compact()
			}
			var buf bytes.Buffer
			if _, err := ex.WriteTo(writeFunc(func(p []byte) (int, error) {
				// The first piece's header: a page of the history is read,
				// and the next is not.
				if !compacted && bytes.Contains(p, []byte(".ndjson")) {
					compacted = true
					compact()
				}
				return buf.Write(p)
			})); err != nil {
				t.Fatalf("stream the export: %v", err)
			}
			if !compacted {
				t.Fatal("the export streamed no piece: the test proves nothing")
			}
			if now := lineSums(t, db); len(now) >= len(pinned) {
				t.Fatalf("the table holds %d lines after the compaction, %d at the pin: it proves nothing", len(now), len(pinned))
			}
			_, restored, _ := restoredExport(t, buf.Bytes())
			got := lineSums(t, rawDB(t, restored))
			if len(got) != len(pinned) {
				t.Fatalf("the export holds %d lines, the table held %d at its point", len(got), len(pinned))
			}
			for seq, sum := range pinned {
				if got[seq] != sum {
					t.Fatalf("seq %d is %q in the export, %q in the table at its point", seq, got[seq], sum)
				}
			}
		})
	}
}

// A server restored from an export fires no webhook request twice. A
// request the export's copy of the spool holds while its point holds its
// delivery entries too (A: recorded and fired, its file not yet deleted),
// then a compaction after the pin that replaces those entries with the
// trigger's ledger: the export streams them as of its point, and the
// restored server's drain finds A recorded and records nothing.
func TestAServerRestoredFromAnExportFiresNoRequestTwice(t *testing.T) {
	noSpoolBeforeTheGrace(t)
	ctx := context.Background()
	svc, ds, dsn, root, _ := newHeldHookDataset(t, true)
	dir := spoolDirOf(t, root, ds.Repository().ID)
	naming := func(db *sql.DB, fid string) int {
		t.Helper()
		var n int
		if err := db.QueryRow(`
			SELECT count(*) FROM changelog WHERE op = 'delivery'
			  AND jsonb_path_exists(payload, '$.** ? (@.fireId == $fid)', jsonb_build_object('fid', $1::text))`, fid).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	fire := func(say string) string {
		t.Helper()
		fid, err := engine.ReceiveWebhookSpooled(ctx, svc, testdb.Repository(t), "hook-held", "", jsonHook(`{"say":"`+say+`"}`, "held"))
		if err != nil {
			t.Fatalf("receive %s: %v", say, err)
		}
		return fid
	}
	fidA := fire("a")
	spooled := spooledFiles(t, dir)
	if len(spooled) != 1 {
		t.Fatalf("the spool holds %d requests, want A's", len(spooled))
	}
	dispatch(t, ds)
	awaitHookEcho(t, ds, fidA)
	awaitSpoolEmpty(t, ds, dir)
	// The drain deletes a file after it records the request: the export is
	// pinned in between.
	for name, data := range spooled {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ex, err := ds.(interface {
		Export(context.Context) (substrate.Export, error)
	}).Export(ctx)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	for name := range spooled {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	fidB := fire("b")
	dispatch(t, ds)
	awaitHookEcho(t, ds, fidB)
	awaitSpoolEmpty(t, ds, dir)
	if report := compactTo(t, svc, 0); report.Removed == 0 {
		t.Fatalf("the compaction removed nothing: %+v", report)
	}
	if n := naming(rawDB(t, dsn), fidA); n != 0 {
		t.Fatalf("the compaction left %d delivery entries naming A: it proves nothing", n)
	}

	var buf bytes.Buffer
	if _, err := ex.WriteTo(&buf); err != nil {
		t.Fatalf("stream the export: %v", err)
	}
	restored, restoredDSN, restoredRoot := restoredExport(t, buf.Bytes())
	held := naming(rawDB(t, restoredDSN), fidA)
	if held == 0 {
		t.Fatal("the export holds A's spooled request and no delivery entry naming it: its drain fires A again")
	}
	dispatch(t, restored)
	awaitSpoolEmpty(t, restored, spoolDirOf(t, restoredRoot, restored.Repository().ID))
	if n := naming(rawDB(t, restoredDSN), fidA); n != held {
		t.Fatalf("the restored server recorded A again: %d delivery entries name it, %d before its drain", n, held)
	}
}
