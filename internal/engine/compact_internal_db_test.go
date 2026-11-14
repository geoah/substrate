package engine

// THE COMPACTION (compact.go, decision 0296), from inside the engine: the
// archive's next manifest after a compaction, an import of it into an empty
// server, verify over a piece awaiting a rewrite, a compaction beside the
// archive's copy of the spool, an effect kept with the effects under it, a
// binary from before the state entries, and a trigger replayed from the
// start of a compacted history. The fold half is compact_db_test.go,
// which reads the tables through FoldRows below.

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// FoldRows reads every table a rebuild fills (FoldTables), each from the columns the
// schema gives it, one JSON text per row, sorted, once the background pass
// owes nothing. Two columns are ids a replay may allocate otherwise and are
// read by what they name: an observed stream's id is left out, and a
// `stream` column reads as the stream's key.
func FoldRows(t testing.TB, ds substrate.Dataset) map[string][]string {
	t.Helper()
	DrainIndexReprojection(t, ds)
	d := ds.(*dataset)
	ctx := context.Background()
	out := map[string][]string{}
	for _, table := range FoldTables() {
		var cols []string
		rows, err := d.db.QueryContext(ctx, `
			SELECT column_name FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = $1`, table)
		if err != nil {
			t.Fatalf("read the columns of %s: %v", table, err)
		}
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				t.Fatal(err)
			}
			cols = append(cols, c)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			t.Fatal(err)
		}
		if len(cols) == 0 {
			t.Fatalf("the schema has no table %s", table)
		}
		expr := `to_jsonb(x) - 'repository'`
		if slices.Contains(cols, "stream") {
			expr = `(` + expr + ` - 'stream') || jsonb_build_object('stream', (SELECT ` + snapshotStream + `
				FROM observed_streams s WHERE s.id = x.stream))`
		}
		if table == "observed_streams" {
			expr += ` - 'id'`
		}
		rows, err = d.db.QueryContext(ctx, `SELECT (`+expr+`)::text FROM `+table+` x`)
		if err != nil {
			t.Fatalf("read %s: %v", table, err)
		}
		list := []string{}
		for rows.Next() {
			var row string
			if err := rows.Scan(&row); err != nil {
				t.Fatal(err)
			}
			list = append(list, row)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			t.Fatal(err)
		}
		slices.Sort(list)
		out[table] = list
	}
	return out
}

// DiffFoldTables names, table by table, where got is not want: the first row
// each holds that the other does not.
func DiffFoldTables(want, got map[string][]string) []string {
	var out []string
	for _, table := range slices.Sorted(func(yield func(string) bool) {
		for k := range want {
			if !yield(k) {
				return
			}
		}
	}) {
		w, g := want[table], got[table]
		if slices.Equal(w, g) {
			continue
		}
		missing, extra := "", ""
		for _, r := range w {
			if !slices.Contains(g, r) {
				missing = r
				break
			}
		}
		for _, r := range g {
			if !slices.Contains(w, r) {
				extra = r
				break
			}
		}
		out = append(out, fmt.Sprintf("%s: %d rows, want %d\n  a row it lacks: %s\n  a row it holds: %s", table, len(g), len(w), missing, extra))
	}
	return out
}

// WriteDeliveryHistory gives a repository every row the delivery tables
// hold: the ledger connector's four triggers (installLedgerConnector), a
// record trigger's runs, a paged drain parked at its second page, a webhook
// request parked with its envelope, a schedule that fired, and a record
// trigger part way through its backlog. It sets the drains' page cap and the
// schedule's drain for its own length, so its caller runs alone.
func WriteDeliveryHistory(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	defer withMaxPages(2)()
	defer withScheduleDrain(1)()
	ctx := context.Background()
	d := ds.(*dataset)
	startsAt := nowUTC().Add(-3*time.Hour - 30*time.Minute).Truncate(time.Minute)
	installLedgerConnector(t, d, startsAt)
	w, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerWidget, Properties: map[string]any{"name": "one"}})
	if err != nil {
		t.Fatalf("put widget: %v", err)
	}
	for _, name := range []string{"one, again", "one, once more"} {
		if _, err := d.Patch(ctx, substrate.ActorPerson, ledgerWidget, w.ID, substrate.PatchInput{Properties: map[string]any{"name": name}}); err != nil {
			t.Fatalf("patch widget: %v", err)
		}
	}
	if _, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerGadget, Properties: map[string]any{"name": "big"}}); err != nil {
		t.Fatalf("put gadget: %v", err)
	}
	if _, err := d.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	if len(failureIDs(t, d, ledgerPager)) != 1 {
		t.Fatal("the paged drain did not park")
	}
	req := substrate.WebhookRequest{
		Method: "POST", ContentType: "application/json",
		Headers: map[string]string{"x-github-event": "parked", "content-type": "application/json"},
		Body:    []byte(`{"say":"later"}`),
	}
	if _, err := d.svc.receiveWebhook(ctx, testdb.Repository(t), ledgerHook, "", req, webhookFireInline); err != nil {
		t.Fatalf("receive: %v", err)
	}
	if len(failureIDs(t, d, ledgerHook)) != 1 {
		t.Fatal("the webhook did not park")
	}
	if _, err := d.db.ExecContext(ctx, `UPDATE trigger_schedule SET fired_at = $2 WHERE trigger_id = $1`,
		ledgerHourly, startsAt.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	processRecordTriggers(t, d)
	// Two widgets the mirror has not reached: its cursor stands behind them.
	for _, name := range []string{"backlog one", "backlog two"} {
		if _, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerWidget, Properties: map[string]any{"name": name}}); err != nil {
			t.Fatalf("put widget: %v", err)
		}
	}
}

// historyRow is a changelog row's columns a compaction never changes.
type historyRow struct {
	ts                        time.Time
	actor, op, recordID, kind string
}

// historyRows reads every row of the repository's changelog by seq.
func historyRows(t *testing.T, d *dataset) map[int64]historyRow {
	t.Helper()
	rows, err := d.db.QueryContext(context.Background(), `SELECT seq, ts, actor, op, record_id, kind FROM changelog`)
	if err != nil {
		t.Fatal(err)
	}
	out := map[int64]historyRow{}
	for rows.Next() {
		var seq int64
		var r historyRow
		if err := rows.Scan(&seq, &r.ts, &r.actor, &r.op, &r.recordID, &r.kind); err != nil {
			t.Fatal(err)
		}
		out[seq] = r
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}
	return out
}

// archiveTaken copies the repository's archive, at the head once a pass has
// run, into the archive of a fresh data root, leaving the service running.
func archiveTaken(t *testing.T, d *dataset) string {
	t.Helper()
	if err := d.ArchivePass(context.Background()); err != nil {
		t.Fatalf("archive pass: %v", err)
	}
	rel := filepath.Join("archive", changelogfile.RepositoriesDir, d.Repository().ID)
	root := t.TempDir()
	dst := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(dst, os.DirFS(filepath.Join(d.svc.dataRoot, rel))); err != nil {
		t.Fatal(err)
	}
	return root
}

// restoredFrom boots a server over an empty database on root's archive.
func restoredFrom(t *testing.T, root string, opts ...Option) (*dataset, error) {
	t.Helper()
	ctx := context.Background()
	svc, err := OpenForTest(t, ctx, MigratedDSN(t), append([]Option{
		WithDataRoot(root), WithCredentialKey(TestCredentialKey), WithKindsFS(SeedKinds), WithArchiveInterval(time.Hour),
	}, opts...)...)
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { _ = svc.Close() })
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		return nil, err
	}
	return ds.(*dataset), nil
}

// writeRecordHistory writes tasks with several versions, a label cleared, an
// annotation, a delete and a restore, and a merge, so a compaction meets the
// record effects, a former-id trail and a resync.
func writeRecordHistory(t *testing.T, d *dataset) {
	t.Helper()
	ctx := context.Background()
	put := func(id, name string) {
		if _, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{
			Kind: ledgerTask, ID: id, Properties: map[string]any{"name": name},
			Labels: map[string]any{"owner/pinned": true},
		}); err != nil {
			t.Fatalf("put %s: %v", id, err)
		}
	}
	patch := func(id string, in substrate.PatchInput) {
		if _, err := d.Patch(ctx, substrate.ActorPerson, ledgerTask, id, in); err != nil {
			t.Fatalf("patch %s: %v", id, err)
		}
	}
	put(fid("a"), "first")
	put(fid("b"), "second")
	put(fid("c"), "third")
	patch(fid("a"), substrate.PatchInput{
		Properties: map[string]any{"name": "first, again"}, Labels: map[string]any{"owner/pinned": nil},
		Annotations: map[string]any{"owner/note": map[string]any{"why": "kept"}},
	})
	patch(fid("a"), substrate.PatchInput{Properties: map[string]any{"status": "done"}})
	if _, err := d.Delete(ctx, substrate.ActorPerson, ledgerTask, fid("c"), substrate.DeleteInput{}); err != nil {
		t.Fatal(err)
	}
	put(fid("c"), "third, back")
	if _, err := d.Merge(ctx, substrate.ActorPerson, substrate.MergeInput{Kind: ledgerTask, Winner: fid("a"), Loser: fid("b")}); err != nil {
		t.Fatalf("merge: %v", err)
	}
	patch(fid("a"), substrate.PatchInput{Properties: map[string]any{"name": "first, merged"}})
}

// Done when 2 and 3: the history is smaller, no seq moved, the archive's
// next manifest holds the lines the table holds and nothing it replaced, and
// the compacted archive imports into an empty server as the fold the
// archive taken before the compaction imports into.
func TestACompactedArchiveImportsAsTheArchiveBeforeItDid(t *testing.T) {
	ctx := context.Background()
	d := openInternalDataset(t, WithArchiveInterval(time.Hour))
	writeRecordHistory(t, d)
	WriteDeliveryHistory(t, d)

	beforeRoot := archiveTaken(t, d)
	before := historyRows(t, d)
	head, err := tableChangelogHead(ctx, d.db)
	if err != nil {
		t.Fatal(err)
	}
	report, err := d.svc.CompactRepository(ctx, testdb.Repository(t), 0)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if report.Records == 0 || report.Triggers == 0 || report.Removed == 0 {
		t.Fatalf("the compaction replaced too little to prove anything: %+v", report)
	}
	after := historyRows(t, d)
	if len(after) >= len(before) || len(before)-len(after) != report.Removed {
		t.Fatalf("the history holds %d entries, %d before, and the report says %d went", len(after), len(before), report.Removed)
	}
	for seq, row := range after {
		was, ok := before[seq]
		if !ok || !was.ts.Equal(row.ts) || was.actor != row.actor || was.op != row.op || was.recordID != row.recordID || was.kind != row.kind {
			t.Fatalf("seq %d is %+v after the compaction, %+v (%v) before: a seq moved", seq, row, was, ok)
		}
	}
	if _, ok := after[head]; !ok {
		t.Fatalf("the compaction removed the head, seq %d", head)
	}
	if report, err := d.svc.VerifyRepository(ctx, testdb.Repository(t)); err != nil || !report.OK {
		t.Fatalf("verify after the compaction: %v %+v", err, report.Findings)
	}

	afterRoot := archiveTaken(t, d)
	// The next manifest holds exactly the lines the table holds.
	restored, err := restoredFrom(t, afterRoot)
	if err != nil {
		t.Fatalf("restore the compacted archive: %v", err)
	}
	got := historyRows(t, restored)
	if len(got) != len(after) {
		t.Fatalf("the compacted archive holds %d entries, the table %d", len(got), len(after))
	}
	for seq := range got {
		if _, ok := after[seq]; !ok {
			t.Fatalf("the compacted archive holds seq %d, which the compaction replaced", seq)
		}
	}
	if report, err := restored.svc.VerifyRepository(ctx, testdb.Repository(t)); err != nil || !report.OK {
		t.Fatalf("verify the restored repository: %v %+v", err, report.Findings)
	}
	beforeRestored, err := restoredFrom(t, beforeRoot)
	if err != nil {
		t.Fatalf("restore the archive taken before the compaction: %v", err)
	}
	if n := len(historyRows(t, beforeRestored)); n != len(before) {
		t.Fatalf("the archive taken before the compaction holds %d entries, want %d", n, len(before))
	}
	if diff := DiffFoldTables(FoldRows(t, beforeRestored), FoldRows(t, restored)); len(diff) > 0 {
		t.Fatalf("the compacted archive imports into another fold than the archive before it:\n%s", strings.Join(diff, "\n"))
	}
}

// A piece awaiting a compaction's rewrite is held to nothing in the table,
// and is still a piece a restore reads: verify reads it, holds it to the sum
// its manifest selects and its lines to theirs, and names one damaged.
func TestVerifyReadsAPieceAwaitingARewrite(t *testing.T) {
	ctx := context.Background()
	d := openInternalDataset(t, WithArchiveInterval(time.Hour))
	writeRecordHistory(t, d)
	if err := d.ArchivePass(ctx); err != nil {
		t.Fatalf("archive pass: %v", err)
	}
	if report, err := d.svc.CompactRepository(ctx, testdb.Repository(t), 0); err != nil || report.Removed == 0 {
		t.Fatalf("compact: %v %+v", err, report)
	}
	if report, err := d.svc.VerifyRepository(ctx, testdb.Repository(t)); err != nil || !report.OK {
		t.Fatalf("verify before the damage: %v %+v", err, report.Findings)
	}
	repoDir := filepath.Join(d.svc.dataRoot, "archive", changelogfile.RepositoriesDir, d.Repository().ID)
	m, _, err := changelogfile.ReadArchiveManifestDir(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	piece := m.Pieces[0].Name
	if err := os.WriteFile(filepath.Join(repoDir, m.Archive, piece), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := d.svc.VerifyRepository(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	if report.OK || !slices.ContainsFunc(report.Findings, func(f string) bool { return strings.Contains(f, piece) }) {
		t.Fatalf("a damaged piece awaiting a rewrite: no finding names %s: %+v", piece, report.Findings)
	}
}

// A compaction never commits between the archive pass's copy of the spool
// and the snapshot read after it. A request the copy holds that the drain
// then recorded and fired (A), and one fired after it (B), leave delivery
// entries a compaction replaces with the trigger's ledger; one committed in
// between took A's from the snapshot while the copy held A's file, and the
// server restored from that archive fired A again.
func TestACompactionNeverCommitsBetweenTheArchivesSpoolCopyAndItsSnapshot(t *testing.T) {
	ctx := context.Background()
	var window atomic.Pointer[func()]
	d := openInternalDataset(t, WithArchiveInterval(time.Hour), WithTestArchiveFault(func(stage string) error {
		if stage == archiveAfterSpool {
			if f := window.Swap(nil); f != nil {
				(*f)()
			}
		}
		return nil
	}))
	installLedgerConnector(t, d, nowUTC().Add(-time.Hour))
	if _, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerWidget, ID: fid("gate"), Properties: map[string]any{"name": "gate"}}); err != nil {
		t.Fatalf("open the gate: %v", err)
	}
	_, tr, err := d.svc.webhookTarget(ctx, testdb.Repository(t), ledgerHook, "")
	if err != nil {
		t.Fatal(err)
	}
	hook := func(say string) substrate.WebhookRequest {
		return substrate.WebhookRequest{
			Method: "POST", ContentType: "application/json",
			Headers: map[string]string{"x-github-event": "spooled", "content-type": "application/json"},
			Body:    []byte(`{"say":"` + say + `"}`),
		}
	}
	naming := func(d *dataset, fid string) int {
		t.Helper()
		var n int
		if err := d.db.QueryRowContext(ctx, `
			SELECT count(*) FROM changelog WHERE op = 'delivery'
			  AND jsonb_path_exists(payload, '$.** ? (@.fireId == $fid)', jsonb_build_object('fid', $1::text))`, fid).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	// A is answered and spooled, and not drained before the copy.
	name, fidA, err := d.spoolWebhook(ctx, tr, hook("a"), nowUTC(), false)
	if err != nil {
		t.Fatalf("spool A: %v", err)
	}
	compacted := make(chan error, 1)
	inWindow := func() {
		got, err := d.drainSpooled(withForegroundWrites(ctx), name)
		if err != nil || !got.recorded {
			t.Fatalf("drain A: %v %+v", err, got)
		}
		d.fireWebhook(ctx, tr, got.row)
		if _, err := d.svc.receiveWebhook(ctx, testdb.Repository(t), ledgerHook, "", hook("b"), webhookFireInline); err != nil {
			t.Fatalf("receive B: %v", err)
		}
		if ids := failureIDs(t, d, ledgerHook); len(ids) != 0 || d.spoolStats().Entries != 0 {
			t.Fatalf("A and B are not both done: parked %v, spooled %d", ids, d.spoolStats().Entries)
		}
		go func() {
			_, err := d.svc.CompactRepository(ctx, testdb.Repository(t), 0)
			compacted <- err
		}()
		// A compaction that does not wait for the snapshot commits here.
		select {
		case err := <-compacted:
			compacted <- err
		case <-time.After(2 * time.Second):
		}
	}
	window.Store(&inWindow)
	root := archiveTaken(t, d)
	if err := <-compacted; err != nil {
		t.Fatalf("compact: %v", err)
	}
	if n := naming(d, fidA); n != 0 {
		t.Fatalf("the compaction left %d delivery entries naming A: it proves nothing", n)
	}

	restored, err := restoredFrom(t, root)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if n := restored.spoolStats().Entries; n != 1 {
		t.Fatalf("the archive's copy of the spool holds %d requests, want A's", n)
	}
	held := naming(restored, fidA)
	if held == 0 {
		t.Fatal("the archive holds A's spooled request and no delivery entry naming it: a restore fires it again")
	}
	restored.kickSpoolDrain()
	deadline := time.Now().Add(30 * time.Second)
	for restored.spoolStats().Entries != 0 {
		if time.Now().After(deadline) {
			t.Fatal("the restored server never drained A's file")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if n := naming(restored, fidA); n != held {
		t.Fatalf("the restored server recorded A again: %d delivery entries name it, %d before its drain", n, held)
	}
}

// A family's effects below one a compaction keeps stay with it. A unit
// created, deleted on a line a parked failure names, and created again,
// compacted past all three, replays into the rows and the unit graph's
// revision the history replayed into before: the kept tombstone meets the
// row the creation under it made, as it did.
func TestAnEffectACompactionKeepsKeepsTheEffectsUnderIt(t *testing.T) {
	ctx := context.Background()
	d := openInternalDataset(t, WithArchiveInterval(time.Hour))
	const id = "zzkept"
	put := func(name string) {
		t.Helper()
		if _, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: vocabulary.KindUnit, ID: id, Properties: map[string]any{"name": name}}); err != nil {
			t.Fatalf("put the unit: %v", err)
		}
	}
	put("first")
	if _, err := d.Delete(ctx, substrate.ActorPerson, vocabulary.KindUnit, id, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete the unit: %v", err)
	}
	deletion, err := tableChangelogHead(ctx, d.db)
	if err != nil {
		t.Fatal(err)
	}
	put("back")
	rebuild := func() {
		t.Helper()
		if _, err := d.svc.RebuildRepository(ctx, testdb.Repository(t)); err != nil {
			t.Fatalf("rebuild: %v", err)
		}
	}
	rebuild()
	want := FoldRows(t, d)
	// The deletion's line is one a parked failure names, which a retry
	// builds its envelope from; the rebuild below folds the row away again.
	if _, err := d.db.ExecContext(ctx, `
		INSERT INTO trigger_failures (id, trigger_id, seq, record_id, attempts, last_error, parked_at)
		VALUES (1, 'parked', $1, $2, 1, 'parked over the deletion', now())`, deletion, id); err != nil {
		t.Fatal(err)
	}
	if report, err := d.svc.CompactRepository(ctx, testdb.Repository(t), 0); err != nil || report.Records == 0 {
		t.Fatalf("compact: %v %+v", err, report)
	}
	var kept bool
	if err := d.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM changelog WHERE seq = $1)`, deletion).Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if !kept {
		t.Fatalf("the compaction removed seq %d, which a parked failure names", deletion)
	}
	rebuild()
	if diff := DiffFoldTables(want, FoldRows(t, d)); len(diff) > 0 {
		t.Fatalf("the compacted history replays into another fold:\n%s", strings.Join(diff, "\n"))
	}
}

// Done when 6: a server from before the state entries, whose chain ends
// below their dialect, refuses a compacted repository by its dialect, from
// its database and from its archive alike.
func TestABinaryFromBeforeStateEntriesRefusesACompactedRepository(t *testing.T) {
	ctx := context.Background()
	dsn, root := MigratedDSN(t), t.TempDir()
	open := func(opts ...Option) (*service, error) {
		svc, err := OpenForTest(t, ctx, dsn, append([]Option{
			WithDataRoot(root), WithCredentialKey(TestCredentialKey), WithKindsFS(SeedKinds), WithArchiveInterval(time.Hour),
		}, opts...)...)
		if err != nil {
			return nil, err
		}
		return svc.(*service), nil
	}
	svc, err := open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatal(err)
	}
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	c, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: vocabulary.KindContact, Properties: map[string]any{"name": "Ada"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.Patch(ctx, substrate.ActorPerson, c.Kind, c.ID, substrate.PatchInput{Properties: map[string]any{"name": "Ada L."}}); err != nil {
		t.Fatal(err)
	}
	report, err := svc.CompactRepository(ctx, testdb.Repository(t), 0)
	if err != nil || report.Records == 0 {
		t.Fatalf("compact: %v %+v", err, report)
	}
	archived := archiveTaken(t, ds.(*dataset))
	_ = svc.Close()

	want := fmt.Sprintf("stores changelog dialect %d, this binary replays <= 1", MaxChangelogDialect())
	refused := func(what string, err error) {
		t.Helper()
		if !errors.Is(err, ErrChangelogDialectNewer) || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s under a binary from before the state entries: %v, want ErrChangelogDialectNewer naming %q", what, err, want)
		}
	}
	old, err := open(sourceChain())
	if err == nil {
		_, err = old.Dataset(ctx, testdb.Repository(t))
		_ = old.Close()
	}
	refused("the database", err)
	_, err = restoredFrom(t, archived, sourceChain())
	refused("the archive", err)
}

// installSeenConnector installs a widget kind and two record triggers over
// it, `on-seen` and `on-again` (disabled), whose functions each put one task
// per delivery, named for the change's op, its record and its seq.
func installSeenConnector(t *testing.T, d *dataset) {
	t.Helper()
	const pkg = "seen.test.dev/seen"
	callable := func(name string) string {
		return vocabulary.RecordPath("substrate.reamde.dev/core/function", pkg+"/"+name)
	}
	body := func(prefix string) string {
		return `
def main(input, host):
    ch = input["envelope"]["change"]
    return {"effects": [{"action": "put", "kind": "` + ledgerTask + `",
                         "id": "` + prefix + `-" + ch["id"] + "-" + str(ch["seq"]),
                         "properties": {"name": ch["op"]}}]}
`
	}
	fn := func(name, prefix string) map[string]any {
		return vocabulary.FunctionManifest(pkg, name, map[string]any{
			"description": "notes each delivery",
			"runtime":     vocabulary.RuntimePython,
			"permissions": map[string]any{"writes": []any{ledgerTask}},
			"source":      body(prefix),
		})
	}
	err := enginetest.Install(context.Background(), d, substrate.ActorPerson, enginetest.Manifest{
		Name: "seen", Authority: pkg,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(pkg, 0),
			vocabulary.KindManifest(pkg, map[string]any{"singular": "widget"},
				map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
			fn("seen", "seen"), fn("again", "again"),
		},
		Triggers: []enginetest.Trigger{
			{ID: "on-seen", Properties: map[string]any{
				"enabled": true, "source": map[string]any{"record": map[string]any{"kinds": []any{pkg + "/widget"}}},
				"callable": callable("seen"),
			}},
			{ID: "on-again", Properties: map[string]any{
				"enabled": false, "source": map[string]any{"record": map[string]any{"kinds": []any{pkg + "/widget"}}},
				"callable": callable("again"),
			}},
		},
	})
	if err != nil {
		t.Fatalf("install the connector: %v", err)
	}
}

// Done when 5, the trigger: one started from the beginning of a compacted
// history sees each live record once, in its latest state and as its
// creation where its state stands for it, then everything kept since.
func TestATriggerReplayedFromTheStartOfACompactedHistorySeesEachLiveRecordOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := openInternalDataset(t)
	installSeenConnector(t, d)
	const widget = "seen.test.dev/seen/widget"
	put := func(name string) string {
		w, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: widget, Properties: map[string]any{"name": name}})
		if err != nil {
			t.Fatal(err)
		}
		return w.ID
	}
	patch := func(id, name string) {
		if _, err := d.Patch(ctx, substrate.ActorPerson, widget, id, substrate.PatchInput{Properties: map[string]any{"name": name}}); err != nil {
			t.Fatal(err)
		}
	}
	many, gone, one := put("many"), put("gone"), put("one")
	for i := range 3 {
		patch(many, fmt.Sprintf("many %d", i))
		if _, err := d.ProcessTriggers(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.Delete(ctx, substrate.ActorPerson, widget, gone, substrate.DeleteInput{}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ProcessTriggers(ctx); err != nil {
		t.Fatal(err)
	}
	report, err := d.svc.CompactRepository(ctx, testdb.Repository(t), 0)
	if err != nil || report.Records == 0 || report.Removed == 0 {
		t.Fatalf("compact: %v %+v", err, report)
	}

	if _, err := d.Patch(ctx, substrate.ActorPerson, typeTrigger, "on-again", substrate.PatchInput{Properties: map[string]any{"enabled": true}}); err != nil {
		t.Fatalf("enable the replay: %v", err)
	}
	if err := d.ReplayTrigger(ctx, "on-again", 0); err != nil {
		t.Fatalf("replay from the beginning: %v", err)
	}
	if _, err := d.ProcessTriggers(ctx); err != nil {
		t.Fatal(err)
	}
	later := put("later")
	if _, err := d.ProcessTriggers(ctx); err != nil {
		t.Fatal(err)
	}

	seen := map[string][]string{}
	rows, err := d.db.QueryContext(ctx, `SELECT id, props->>'name' FROM records WHERE kind = $1 AND id LIKE 'again-%'`, ledgerTask)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id, op string
		if err := rows.Scan(&id, &op); err != nil {
			t.Fatal(err)
		}
		for _, w := range []string{many, gone, one, later} {
			if strings.HasPrefix(id, "again-"+w+"-") {
				seen[w] = append(seen[w], op)
			}
		}
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{many: vocabulary.FunctionOpCreate, one: vocabulary.FunctionOpCreate, later: vocabulary.FunctionOpCreate} {
		if got := seen[id]; len(got) != 1 || got[0] != want {
			t.Errorf("the replay saw %s as %v, want once, as %q", id, got, want)
		}
	}
	if got := seen[gone]; len(got) > 1 || (len(got) == 1 && got[0] != vocabulary.FunctionOpDelete) {
		t.Errorf("the replay saw the deleted %s as %v, want at most once, as a delete", gone, got)
	}
}

// The actors' rule (decision 0270): later work acts for whoever the line that
// created its own record names, and a run's settlement for whom the line that
// gave it the thread's lease names. A compaction replaces both lines with the
// thread's state entry, which carries the creating line's links and the mark
// on `leaseUntil`, and the work acts for the same person after it.
func TestWorkWhoseLinesACompactionReplacedActsForWhomTheyNamed(t *testing.T) {
	ds, fake := provisionJobAgents(t)
	fake.script("jlead", fakeTurn{content: "ready"})
	ctx := context.Background()
	person := substrate.WithOnBehalfOf(ctx, substrate.ActorPerson)
	opened, err := ds.CallAgent(person, jobsPackage+"/lead", "open")
	if err != nil {
		t.Fatal(err)
	}
	thread := opened.Thread
	ag, err := ds.registry().ResolveAgent(jobsPackage + "/lead")
	if err != nil {
		t.Fatal(err)
	}
	write := func(ctx context.Context, props map[string]any) {
		t.Helper()
		if err := ds.inTx(ctx, substrate.Actor(ag.Actor()), false, func(tx *txn) error {
			_, err := tx.patch(eref{Kind: typeThread, ID: thread}, substrate.PatchInput{Properties: props})
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	// The person's run takes the lease; then a write for no one, so neither
	// line is the thread's newest.
	write(person, map[string]any{"status": threadRunning, "leaseUntil": nowUTC().Add(time.Hour).Format(time.RFC3339Nano)})
	write(ctx, map[string]any{"title": "a title for no one"})
	reads := map[string]func() (context.Context, error){
		"its creating line": func() (context.Context, error) { return ds.actingFor(ctx, typeThread, thread) },
		"its run's lease":   func() (context.Context, error) { return ds.actingForRun(ctx, thread) },
	}
	for name, read := range reads {
		if got, err := read(); err != nil || substrate.OnBehalfOfFrom(got) != substrate.ActorPerson {
			t.Fatalf("before the compaction, %s acts for %q (%v), want the person", name, substrate.OnBehalfOfFrom(got), err)
		}
	}
	if _, err := ds.svc.CompactRepository(ctx, testdb.Repository(t), 0); err != nil {
		t.Fatal(err)
	}
	var first string
	if err := ds.db.QueryRowContext(ctx, `
		SELECT payload::text FROM changelog WHERE kind = $1 AND record_id = $2 ORDER BY seq LIMIT 1`, typeThread, thread).Scan(&first); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first, `"kind": "state"`) {
		t.Fatalf("the thread's first line after the compaction holds no state: the test proves nothing: %s", first)
	}
	for name, read := range reads {
		if got, err := read(); err != nil || substrate.OnBehalfOfFrom(got) != substrate.ActorPerson {
			t.Errorf("after the compaction, %s acts for %q (%v), want the person", name, substrate.OnBehalfOfFrom(got), err)
		}
	}
}

// The horizon and record triggers: an enabled trigger whose cursor stands
// below an old deletion holds it back, and the status names it; a disabled
// one holds nothing, and the pass starts it over from the beginning before it
// forgets, once: enabled again, it meets each live record once and the
// forgotten one not at all, and no later pass or walk sends it back. A
// replay into the stretch below the horizon starts from the beginning too.
func TestADisabledTriggerHoldsNoDeletionAndStartsOverBelowTheHorizon(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := openInternalDataset(t, WithCompactionInterval(-1), WithDeletionsKept(time.Hour),
		WithTestCompactionClock(func() time.Time { return time.Now().Add(2 * time.Hour) }))
	installSeenConnector(t, d)
	const widget = "seen.test.dev/seen/widget"
	put := func(name string) string {
		w, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: widget, Properties: map[string]any{"name": name}})
		if err != nil {
			t.Fatal(err)
		}
		return w.ID
	}
	live, gone := put("live"), put("gone")
	if _, err := d.Delete(ctx, substrate.ActorPerson, widget, gone, substrate.DeleteInput{Purge: true}); err != nil {
		t.Fatal(err)
	}
	put("after")
	var deletion int64
	if err := d.db.QueryRowContext(ctx, `SELECT max(seq) FROM changelog WHERE record_id = $1`, gone).Scan(&deletion); err != nil {
		t.Fatal(err)
	}
	if got := cursorSeq(t, d, "on-again"); got == 0 || got >= deletion {
		t.Fatalf("on-again stands at %d, want past 0 and below the deletion %d: the test proves nothing", got, deletion)
	}
	// on-seen is enabled and has not passed the deletion: it holds it back.
	st, err := d.svc.CompactionStatus(ctx, testdb.Repository(t))
	if err != nil || st.HeldBy == nil || st.HeldBy.Trigger != "on-seen" {
		t.Fatalf("the status names %+v (%v), want on-seen holding the horizon back", st.HeldBy, err)
	}
	if report, err := d.CompactionPass(ctx); err != nil || report.Forgotten != 0 {
		t.Fatalf("an enabled trigger behind the deletion: the pass forgot %d (%v)", report.Forgotten, err)
	}
	// Once it has delivered past it, only the disabled on-again stands
	// below, and holds nothing.
	if _, err := d.ProcessTriggers(ctx); err != nil {
		t.Fatal(err)
	}
	if report, err := d.CompactionPass(ctx); err != nil || report.Forgotten != 1 {
		t.Fatalf("with on-seen past the deletion: the pass forgot %d (%v), want the purged widget", report.Forgotten, err)
	}
	head, err := d.Head(ctx)
	if err != nil || head.Horizon == 0 {
		t.Fatalf("the horizon is %d (%v)", head.Horizon, err)
	}
	if got := cursorSeq(t, d, "on-again"); got != 0 {
		t.Fatalf("on-again, not enabled and below the forgotten deletion, stands at %d after the pass, want started over at 0", got)
	}
	// Enabled again, it walks from the beginning.
	if _, err := d.Patch(ctx, substrate.ActorPerson, typeTrigger, "on-again", substrate.PatchInput{Properties: map[string]any{"enabled": true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ProcessTriggers(ctx); err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	rows, err := d.db.QueryContext(ctx, `SELECT id FROM records WHERE kind = $1 AND id LIKE 'again-%'`, ledgerTask)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		for _, w := range []string{live, gone} {
			if strings.HasPrefix(id, "again-"+w+"-") {
				seen[w]++
			}
		}
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}
	if seen[live] != 1 || seen[gone] != 0 {
		t.Fatalf("on-again, started over, met the live widget %d times and the forgotten one %d, want once and never", seen[live], seen[gone])
	}
	// A recovery cut short, standing past 0 and below the horizon, goes on
	// from where it stood: no walk sends it back over what it delivered. Its
	// deliveries so far are purged first, so one made again shows as a task
	// made again.
	var liveLine int64
	if err := d.db.QueryRowContext(ctx, `SELECT min(seq) FROM changelog WHERE record_id = $1`, live).Scan(&liveLine); err != nil {
		t.Fatal(err)
	}
	if liveLine == 0 || liveLine >= head.Horizon {
		t.Fatalf("the live widget's line %d is not between 0 and the horizon %d: the test proves nothing", liveLine, head.Horizon)
	}
	againTasks := func() []string {
		t.Helper()
		rows, err := d.db.QueryContext(ctx, `SELECT id FROM records WHERE kind = $1 AND id LIKE 'again-%' AND deleted_at IS NULL`, ledgerTask)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, id)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			t.Fatal(err)
		}
		return ids
	}
	for _, id := range againTasks() {
		if _, err := d.Delete(ctx, substrate.ActorPerson, ledgerTask, id, substrate.DeleteInput{Purge: true}); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
		if err := tx.setCursorTx("on-again", liveLine); err != nil {
			return err
		}
		return tx.settleDelivery("on-again")
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CompactionPass(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ProcessTriggers(ctx); err != nil {
		t.Fatal(err)
	}
	if got := cursorSeq(t, d, "on-again"); got <= liveLine {
		t.Fatalf("on-again stands at %d after the walk, want past %d, where it stood", got, liveLine)
	}
	for _, id := range againTasks() {
		if seq, err := strconv.ParseInt(id[strings.LastIndexByte(id, '-')+1:], 10, 64); err != nil || seq <= liveLine {
			t.Fatalf("on-again delivered %s again: a change at or below seq %d, where it stood (%v)", id, liveLine, err)
		}
	}
	if err := d.ReplayTrigger(ctx, "on-seen", 1); err != nil {
		t.Fatal(err)
	}
	if got := cursorSeq(t, d, "on-seen"); got != 0 {
		t.Fatalf("a replay from seq 1, below the horizon %d, left on-seen at %d, want 0", head.Horizon, got)
	}
}

// The floor is read again under each forgetting batch's lock: an enabled
// trigger rewound below an old deletion after the pass chose what to forget,
// and before the batch ran, holds the deletion back, and the pass's mark
// stays below it.
func TestATriggerRewoundBeforeAForgettingBatchHoldsItBack(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var rewind atomic.Pointer[func()]
	d := openInternalDataset(t, WithCompactionInterval(-1), WithDeletionsKept(time.Hour), WithTestPurgeKeepsHistory(),
		WithTestCompactionClock(func() time.Time { return time.Now().Add(2 * time.Hour) }),
		WithTestCompactionFault(func(stage string) error {
			if stage == compactionToForget {
				if f := rewind.Swap(nil); f != nil {
					(*f)()
				}
			}
			return nil
		}))
	installSeenConnector(t, d)
	const widget = "seen.test.dev/seen/widget"
	w, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: widget, Properties: map[string]any{"name": "gone"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Delete(ctx, substrate.ActorPerson, widget, w.ID, substrate.DeleteInput{Purge: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: widget, Properties: map[string]any{"name": "after"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ProcessTriggers(ctx); err != nil {
		t.Fatal(err)
	}
	lines := func() int {
		var n int
		if err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM changelog WHERE record_id = $1`, w.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := lines()
	back := func() {
		if err := d.ReplayTrigger(ctx, "on-seen", 1); err != nil {
			t.Errorf("rewind on-seen: %v", err)
		}
	}
	rewind.Store(&back)
	report, err := d.CompactionPass(ctx)
	if err != nil || report.Forgotten != 0 {
		t.Fatalf("on-seen rewound below the deletion before the batch: the pass forgot %d (%v), want none", report.Forgotten, err)
	}
	if n := lines(); n != before {
		t.Fatalf("the purged widget has %d lines, %d before the pass", n, before)
	}
	st, err := d.svc.CompactionStatus(ctx, testdb.Repository(t))
	if err != nil || st.ForgotUpTo > 1 {
		t.Fatalf("deletions looked at up to %d (%v), want no further than on-seen at 1", st.ForgotUpTo, err)
	}
}

// The first pass of an archive that has no record yet, whose snapshot came
// before a compaction committed, publishes the history as it was, and the
// compaction's marks are kept: the next pass writes again what it changed,
// and the archive holds the table.
func TestAFirstArchivePassBesideACompactionKeepsItsMarks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var window atomic.Pointer[func()]
	d := openInternalDataset(t, WithArchiveInterval(time.Hour), WithTestArchiveFault(func(stage string) error {
		if stage == archiveAfterSnapshot {
			if f := window.Swap(nil); f != nil {
				(*f)()
			}
		}
		return nil
	}))
	writeRecordHistory(t, d)
	var records int
	if err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM archive_records`).Scan(&records); err != nil || records != 0 {
		t.Fatalf("the archive has %d records (%v) before its first pass: the test proves nothing", records, err)
	}
	compact := func() {
		if report, err := d.svc.CompactRepository(ctx, testdb.Repository(t), 0); err != nil || report.Removed == 0 {
			t.Errorf("compact: %v %+v", err, report)
		}
	}
	window.Store(&compact)
	if err := d.ArchivePass(ctx); err != nil {
		t.Fatalf("the first archive pass: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	if err := d.ArchivePass(ctx); err != nil {
		t.Fatalf("the next archive pass: %v", err)
	}
	if report, err := d.svc.VerifyRepository(ctx, testdb.Repository(t)); err != nil || !report.OK || report.ArchiveUnverified != "" {
		t.Fatalf("verify after the next pass: %v %+v %q", err, report.Findings, report.ArchiveUnverified)
	}
}

// The measurement a retention is chosen from, on the largest workload this
// suite builds: a provider-like mirror its sync refreshes, the sync's cursor
// record, a record trigger's deliveries with their runs and ledger entries,
// and a person's tasks. Set SUBSTRATE_COMPACTION_TIMING to print it.
func TestASyncShapedHistoryByKind(t *testing.T) {
	if os.Getenv("SUBSTRATE_COMPACTION_TIMING") == "" {
		t.Skip("set SUBSTRATE_COMPACTION_TIMING to measure a sync-shaped history")
	}
	ctx := context.Background()
	d := openInternalDataset(t, WithCompactionInterval(-1))
	startsAt := nowUTC().Add(-time.Hour).Truncate(time.Minute)
	installLedgerConnector(t, d, startsAt)
	const mirror = "mirror.example.substrate.reamde.dev/mirror"
	if _, err := d.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(mirror, 0),
		vocabulary.KindManifest(mirror, map[string]any{"singular": "message"}, map[string]any{"properties": map[string]any{
			"subject": map[string]any{"type": "string"}, "body": map[string]any{"type": "text"},
			"labels": map[string]any{"type": "string"}, "threadId": map[string]any{"type": "string", "fts": false},
		}}),
		vocabulary.KindManifest(mirror, map[string]any{"singular": "account"}, map[string]any{"properties": map[string]any{
			"cursor": map[string]any{"type": "string", "fts": false}, "syncedAt": map[string]any{"type": "string", "fts": false},
		}}),
	}); err != nil {
		t.Fatal(err)
	}
	body := strings.Repeat("A line of an email body the mirror holds. ", 40)
	for page := range 100 {
		for i := range 20 {
			id := fid("m" + strconv.Itoa(page) + "x" + strconv.Itoa(i))
			for refresh := range 3 {
				if _, err := d.Put(ctx, "function:mirror.example.substrate.reamde.dev:mirror:sync", substrate.PutInput{Kind: mirror + "/message", ID: id, Properties: map[string]any{
					"subject": "Subject " + id, "body": body, "labels": "inbox,refresh-" + strconv.Itoa(refresh), "threadId": "t" + strconv.Itoa(i),
				}}); err != nil {
					t.Fatal(err)
				}
			}
		}
		if _, err := d.Put(ctx, "function:mirror.example.substrate.reamde.dev:mirror:sync", substrate.PutInput{Kind: mirror + "/account", ID: fid("account"), Properties: map[string]any{
			"cursor": "page-" + strconv.Itoa(page), "syncedAt": nowUTC().Format(time.RFC3339Nano),
		}}); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 150 {
		if _, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerWidget, Properties: map[string]any{"name": "w" + strconv.Itoa(i)}}); err != nil {
			t.Fatal(err)
		}
		if i%10 == 9 {
			if _, err := d.ProcessTriggers(ctx); err != nil {
				t.Fatal(err)
			}
		}
	}
	for i := range 200 {
		id := fid("task" + strconv.Itoa(i))
		for _, name := range []string{"draft", "edited", "done"} {
			if _, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, ID: id, Properties: map[string]any{"name": name}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	kinds, err := d.svc.HistoryByKind(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	var entries, bytes int64
	for _, h := range kinds {
		entries, bytes = entries+h.Entries, bytes+h.Bytes
	}
	for _, h := range kinds {
		t.Logf("%-62s %7d entries %10d bytes %5.1f%%", h.Kind, h.Entries, h.Bytes, 100*float64(h.Bytes)/float64(bytes))
	}
	t.Logf("total %d entries, %d bytes", entries, bytes)
}

// A state entry keeps the fold's rule that a mark goes with its value
// (decision 0284), both ways: a property a line took out has no mark in the
// state, though its mark stood before the line, and a property written again
// keeps the mark it was written with through a second compaction. A rebuild
// after each gives the marks the live table holds.
func TestAStateKeepsAMarkOnlyBesideItsValue(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := openInternalDataset(t, WithArchiveInterval(time.Hour))
	rec, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{
		"name": "marked", "url": "https://example.com/marked",
	}})
	if err != nil {
		t.Fatal(err)
	}
	task := rec.ID
	marked := func() []string {
		t.Helper()
		rows, err := d.db.QueryContext(ctx, `SELECT property FROM property_marks WHERE record_kind = $1 AND record_id = $2 ORDER BY property`, ledgerTask, task)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for rows.Next() {
			var p string
			if err := rows.Scan(&p); err != nil {
				t.Fatal(err)
			}
			out = append(out, p)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if !slices.Contains(marked(), "url") {
		t.Fatalf("the written url carries no mark: %v", marked())
	}
	patch := func(props map[string]any) {
		t.Helper()
		if _, err := d.Patch(ctx, substrate.ActorPerson, ledgerTask, task, substrate.PatchInput{Properties: props}); err != nil {
			t.Fatal(err)
		}
	}
	compactAndRebuild := func() {
		t.Helper()
		want := FoldRows(t, d)
		if _, err := d.svc.CompactRepository(ctx, testdb.Repository(t), 0); err != nil {
			t.Fatal(err)
		}
		if _, err := d.svc.RebuildRepository(ctx, testdb.Repository(t)); err != nil {
			t.Fatal(err)
		}
		if diff := DiffFoldTables(want, FoldRows(t, d)); len(diff) > 0 {
			t.Fatalf("the compacted history replays into another fold:\n%s", strings.Join(diff, "\n"))
		}
	}
	patch(map[string]any{"url": nil})
	patch(map[string]any{"description": "after the url went"})
	compactAndRebuild()
	if slices.Contains(marked(), "url") {
		t.Fatalf("a url taken out before the state keeps a mark after it: %v", marked())
	}
	var state string
	if err := d.db.QueryRowContext(ctx, `
		SELECT payload::text FROM changelog WHERE kind = $1 AND record_id = $2 ORDER BY seq DESC LIMIT 1`, ledgerTask, task).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(state, `"kind": "state"`) || strings.Contains(state, `"property": "url"`) {
		t.Fatalf("the state's line is not a state without a mark on url: %s", state)
	}
	patch(map[string]any{"url": "https://example.com/again"})
	compactAndRebuild()
	if !slices.Contains(marked(), "url") {
		t.Fatalf("a url written again keeps no mark through the second compaction: %v", marked())
	}
}

// A compaction reads nothing of a line that does not verify but its seq, and
// stops below the first it meets, D: what lies below D compacts, and every
// column of every row from D up is what it was. D is a line of a record with
// lines on both sides of it, in the middle of a transaction whose end line
// another record's next write leaves removable, and the only line that sets
// the record's `url`. Once the row is written back, a rebuild gives the fold
// from before the damage, and the next compaction goes past it.
func TestACompactionStopsBelowALineThatDoesNotVerify(t *testing.T) {
	t.Parallel()
	for _, damage := range []struct{ name, sql string }{
		{"the stored checksum", `UPDATE changelog SET hash = sha256(hash) WHERE seq = $1`},
		{"a column edited under it", `UPDATE changelog SET payload = jsonb_set(payload, '{damaged}', 'true') WHERE seq = $1`},
		{"its transaction taken off it", `UPDATE changelog SET txn = NULL WHERE seq = $1`},
		{"its effect naming another record", `UPDATE changelog SET payload = jsonb_set(payload, '{fold,0,id}', to_jsonb($2::text)) WHERE seq = $1`},
	} {
		t.Run(damage.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			d := openInternalDataset(t, WithArchiveInterval(time.Hour))
			put := func(name string) string {
				t.Helper()
				rec, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": name}})
				if err != nil {
					t.Fatal(err)
				}
				return rec.ID
			}
			patch := func(id string, props map[string]any) {
				t.Helper()
				if _, err := d.Patch(ctx, substrate.ActorPerson, ledgerTask, id, substrate.PatchInput{Properties: props}); err != nil {
					t.Fatal(err)
				}
			}
			lines := func(id string) []int64 {
				t.Helper()
				rows, err := d.db.QueryContext(ctx, `SELECT seq FROM changelog WHERE record_id = $1 ORDER BY seq`, id)
				if err != nil {
					t.Fatal(err)
				}
				var out []int64
				for rows.Next() {
					var seq int64
					if err := rows.Scan(&seq); err != nil {
						t.Fatal(err)
					}
					out = append(out, seq)
				}
				if err := errors.Join(rows.Err(), rows.Close()); err != nil {
					t.Fatal(err)
				}
				return out
			}
			below := put("below")
			patch(below, map[string]any{"name": "below, again"})
			written, other, ender := put("written for"), put("other"), put("ender")
			var first string
			if err := d.inTx(ctx, substrate.ActorPerson, false, func(tx *txn) error {
				rec, err := tx.put(substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": "first"}})
				if err != nil {
					return err
				}
				first = rec.ID
				if _, err := tx.patch(eref{Kind: ledgerTask, ID: written}, substrate.PatchInput{Properties: map[string]any{"url": "https://example.com/set-on-the-damaged-line"}}); err != nil {
					return err
				}
				_, err = tx.patch(eref{Kind: ledgerTask, ID: ender}, substrate.PatchInput{Properties: map[string]any{"name": "ender, in the transaction"}})
				return err
			}); err != nil {
				t.Fatal(err)
			}
			patch(ender, map[string]any{"name": "ender, last"})
			patch(written, map[string]any{"name": "written for, last"})
			bad, end := lines(written)[1], lines(ender)[1]
			if a := lines(first)[0]; a != bad-1 || end != bad+1 {
				t.Fatalf("lines %d, %d, %d are not one transaction around the damaged line: the test proves nothing", a, bad, end)
			}
			want := FoldRows(t, d)
			var payload string
			var txn int64
			var hash []byte
			if err := d.db.QueryRowContext(ctx, `SELECT payload::text, txn, hash FROM changelog WHERE seq = $1`, bad).Scan(&payload, &txn, &hash); err != nil {
				t.Fatal(err)
			}
			args := []any{bad}
			if strings.Contains(damage.sql, "$2") {
				args = append(args, other)
			}
			if _, err := d.db.ExecContext(ctx, damage.sql, args...); err != nil {
				t.Fatal(err)
			}
			from := func() map[int64]string {
				t.Helper()
				rows, err := d.db.QueryContext(ctx, `SELECT seq, row_to_json(c)::text FROM changelog c WHERE seq >= $1`, bad)
				if err != nil {
					t.Fatal(err)
				}
				out := map[int64]string{}
				for rows.Next() {
					var seq int64
					var row string
					if err := rows.Scan(&seq, &row); err != nil {
						t.Fatal(err)
					}
					out[seq] = row
				}
				if err := errors.Join(rows.Err(), rows.Close()); err != nil {
					t.Fatal(err)
				}
				return out
			}
			before := from()
			report, err := d.svc.CompactRepository(ctx, testdb.Repository(t), 0)
			if err != nil {
				t.Fatalf("compact: %v", err)
			}
			if report.Damaged != bad || report.UpTo != bad-1 {
				t.Fatalf("the compaction names seq %d and compacted up to %d, want %d and %d", report.Damaged, report.UpTo, bad, bad-1)
			}
			if got := lines(below); len(got) != 1 {
				t.Fatalf("the record below the damaged line has lines %v, want one state", got)
			}
			if after := from(); !maps.Equal(after, before) {
				for seq, row := range after {
					if before[seq] != row {
						t.Errorf("seq %d is now %s, was %s", seq, row, before[seq])
					}
				}
				t.Fatalf("the compaction changed rows from the damaged line %d up (%d rows, %d before)", bad, len(after), len(before))
			}
			verified, err := d.svc.VerifyRepositoryWith(ctx, testdb.Repository(t), VerifyOptions{Recanonicalize: true})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.ContainsFunc(verified.Findings, func(f string) bool { return strings.Contains(f, fmt.Sprintf("seq %d:", bad)) }) {
				t.Fatalf("verify no longer names the damaged seq %d: %v", bad, verified.Findings)
			}
			if _, err := d.db.ExecContext(ctx, `UPDATE changelog SET payload = $2::jsonb, txn = $3, hash = $4 WHERE seq = $1`, bad, payload, txn, hash); err != nil {
				t.Fatal(err)
			}
			if _, err := d.svc.RebuildRepository(ctx, testdb.Repository(t)); err != nil {
				t.Fatalf("rebuild: %v", err)
			}
			if diff := DiffFoldTables(want, FoldRows(t, d)); len(diff) > 0 {
				t.Fatalf("with the row written back, the history replays into another fold:\n%s", strings.Join(diff, "\n"))
			}
			if report, err = d.svc.CompactRepository(ctx, testdb.Repository(t), 0); err != nil || report.Damaged != 0 {
				t.Fatalf("the compaction after the repair: %v, stopped at %d", err, report.Damaged)
			}
			if got := lines(written); len(got) != 1 {
				t.Fatalf("after the repair the record has lines %v, want one state", got)
			}
		})
	}
}

// A removed line that ended its transaction hands the end to the newest line
// of it that is left: every kept line of a three-line transaction whose end
// a compaction removes names the new end, and the history verifies.
func TestACompactionReframesWhatIsLeftOfATransaction(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := openInternalDataset(t, WithArchiveInterval(time.Hour))
	ender, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": "ender"}})
	if err != nil {
		t.Fatal(err)
	}
	var first, second string
	if err := d.inTx(ctx, substrate.ActorPerson, false, func(tx *txn) error {
		for _, id := range []*string{&first, &second} {
			rec, err := tx.put(substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": "kept"}})
			if err != nil {
				return err
			}
			*id = rec.ID
		}
		_, err := tx.patch(eref{Kind: ledgerTask, ID: ender.ID}, substrate.PatchInput{Properties: map[string]any{"name": "ender, in the transaction"}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Patch(ctx, substrate.ActorPerson, ledgerTask, ender.ID, substrate.PatchInput{Properties: map[string]any{"name": "ender, last"}}); err != nil {
		t.Fatal(err)
	}
	line := func(id string) (seq, txn int64) {
		t.Helper()
		if err := d.db.QueryRowContext(ctx, `SELECT seq, txn FROM changelog WHERE record_id = $1 ORDER BY seq LIMIT 1`, id).Scan(&seq, &txn); err != nil {
			t.Fatal(err)
		}
		return seq, txn
	}
	a, end := line(first)
	b, _ := line(second)
	if end != b+1 {
		t.Fatalf("the transaction ends at %d, want the ender's line after %d: the test proves nothing", end, b)
	}
	if _, err := d.svc.CompactRepository(ctx, testdb.Repository(t), 0); err != nil {
		t.Fatalf("compact: %v", err)
	}
	var left int
	if err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM changelog WHERE seq = $1`, end).Scan(&left); err != nil || left != 0 {
		t.Fatalf("the transaction's end line is still there (%d, %v): the test proves nothing", left, err)
	}
	for _, id := range []string{first, second} {
		if seq, txn := line(id); txn != b {
			t.Fatalf("seq %d names the end %d, want %d (the transaction was %d to %d)", seq, txn, b, a, end)
		}
	}
	verified, err := d.svc.VerifyRepositoryWith(ctx, testdb.Repository(t), VerifyOptions{Recanonicalize: true})
	if err != nil || !verified.OK {
		t.Fatalf("the compacted history does not verify: %v %+v", err, verified.Findings)
	}
}
