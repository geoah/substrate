package engine

// The compaction pass and the archive: after ten passes over a growing
// repository the archive holds what the table holds, and what each archive
// pass uploads is the pieces a compaction changed, not the history.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/archive"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// countingStore is a store that counts the bytes of the pieces put in it.
type countingStore struct {
	archive.Store
	mu     sync.Mutex
	pieces int64
}

func (c *countingStore) Put(ctx context.Context, key string, body io.ReadSeeker, size int64) error {
	if strings.Contains(key, "/changelog/") {
		c.mu.Lock()
		c.pieces += size
		c.mu.Unlock()
	}
	return c.Store.Put(ctx, key, body, size)
}

func (c *countingStore) take() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := c.pieces
	c.pieces = 0
	return n
}

// Done when 4.
func TestTheArchiveAfterTenPassesHoldsTheTableAndUploadsWhatChanged(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir, err := archive.NewDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := &countingStore{Store: dir}
	const pkg = "ten.example.substrate.reamde.dev/ten"
	d := openInternalDataset(t, WithArchive(store), WithArchiveInterval(time.Hour), WithArchivePieceBytes(16<<10),
		WithCompactionInterval(-1),
		WithTestCompactionRetention(func(kind string) (bool, time.Duration) { return kind != pkg+"/brief", 0 }))
	if _, err := d.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "brief"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
	}); err != nil {
		t.Fatal(err)
	}
	put := func(kind, id, name string) {
		if _, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: kind, ID: id, Properties: map[string]any{"name": name}}); err != nil {
			t.Fatal(err)
		}
	}
	// Records the pass compacts, written first, under a long history it
	// keeps: their first lines sit in the oldest pieces.
	for i := range 5 {
		put(pkg+"/brief", fid("brief"+string(rune('a'+i))), "first")
	}
	for i := range 400 {
		put(ledgerTask, fid("task"+string(rune('a'+i%26))+string(rune('a'+i/26))), "background")
	}
	if err := d.ArchivePass(ctx); err != nil {
		t.Fatal(err)
	}
	whole := store.take()
	for round := range 10 {
		for i := range 5 {
			put(pkg+"/brief", fid("brief"+string(rune('a'+i))), "round "+string(rune('0'+round)))
		}
		for i := range 20 {
			put(ledgerTask, fid("more"+string(rune('a'+round))+string(rune('a'+i))), "more")
		}
		if _, err := d.CompactionPass(ctx); err != nil {
			t.Fatalf("round %d: the pass: %v", round, err)
		}
		if err := d.ArchivePass(ctx); err != nil {
			t.Fatalf("round %d: the archive pass: %v", round, err)
		}
		uploaded := store.take()
		// The first round takes the lines out of the oldest pieces; every
		// round writes the pieces holding the last round's lines and the
		// new tail. Either way, a part of the history and not the whole.
		if uploaded*3 > whole {
			t.Errorf("round %d uploaded %d bytes of pieces, the whole history is %d: more than a third", round, uploaded, whole)
		}
	}
	report, err := d.svc.VerifyRepository(ctx, testdb.Repository(t))
	if err != nil || !report.OK || report.ArchiveUnverified != "" || report.ArchiveHead != report.Head {
		t.Fatalf("after ten passes the archive is not the table: %v %+v", err, report)
	}
}

// While a line of the history is known not to verify, nothing at or above it
// is compacted (damage.go). A line of a task R whose `record_id` the damage
// changed is one no batch of R reads; a reader notes it (the backfill, the
// door, an archive pass that refuses it, whose note outlives the failed
// pass), and the pass then compacts what lies below it and leaves every
// column of every row from it up as it was, R's lines and a later purged
// task's deletion among them. Once the row is written back, the next pass
// drops the note, compacts R and forgets the deletion by itself, a rebuild
// gives the fold from before the damage, and the archive holds the table.
func TestThePassWaitsBelowALineKnownNotToVerify(t *testing.T) {
	t.Parallel()
	for _, noter := range []string{"the backfill", "the door", "the archive pass"} {
		t.Run(noter, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			dir, err := archive.NewDir(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			clock := time.Now().UTC()
			d := openInternalDataset(t, WithArchive(dir), WithArchiveInterval(time.Hour),
				WithCompactionInterval(-1), WithDeletionsKept(time.Hour),
				WithTestCompactionClock(func() time.Time { return clock }),
				WithTestCompactionRetention(func(kind string) (bool, time.Duration) { return kind != ledgerTask, time.Hour }))
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
			status := func() CompactionStatus {
				t.Helper()
				st, err := d.svc.CompactionStatus(ctx, testdb.Repository(t))
				if err != nil {
					t.Fatal(err)
				}
				return st
			}
			pass := func() {
				t.Helper()
				if _, err := d.CompactionPass(ctx); err != nil {
					t.Fatalf("pass: %v", err)
				}
			}
			below := put("below")
			patch(below, map[string]any{"name": "below, again"})
			r := put("r")
			patch(r, map[string]any{"url": "https://example.com/set-on-the-damaged-line"})
			bad := lines(r)[1]
			patch(r, map[string]any{"name": "r, last"})
			gone := put("gone")
			patch(gone, map[string]any{"name": "gone, again"})
			if _, err := d.Delete(ctx, substrate.ActorPerson, ledgerTask, gone, substrate.DeleteInput{Purge: true}); err != nil {
				t.Fatalf("purge: %v", err)
			}
			deletion := slices.Max(lines(gone))
			put("the head")
			if noter != "the backfill" {
				// The index is whole before the damage: a pass inside every
				// retention runs the backfill and compacts nothing.
				pass()
			}
			want := FoldRows(t, d)
			if _, err := d.db.ExecContext(ctx, `UPDATE changelog SET record_id = $2 WHERE seq = $1`, bad, below); err != nil {
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
			clock = clock.Add(2 * time.Hour)
			switch noter {
			case "the door":
				report, err := d.svc.CompactRepository(ctx, testdb.Repository(t), 0)
				if err != nil || report.Damaged != bad {
					t.Fatalf("the door stopped at %d (%v), want %d", report.Damaged, err, bad)
				}
			case "the archive pass":
				if err := d.ArchivePass(ctx); !errors.Is(err, ErrChangelogDiverged) || !strings.Contains(err.Error(), fmt.Sprintf("seq %d:", bad)) {
					t.Fatalf("an archive pass over the damaged row: %v, want it refused at seq %d", err, bad)
				}
				if got := status().Damaged; !slices.Equal(got, []int64{bad}) {
					t.Fatalf("after the failed archive pass the notes are %v, want [%d]", got, bad)
				}
			}
			for round := range 2 {
				pass()
				if got := status().Damaged; !slices.Equal(got, []int64{bad}) {
					t.Fatalf("pass %d: the notes are %v, want [%d]", round, got, bad)
				}
				if after := from(); !maps.Equal(after, before) {
					for seq, row := range after {
						if before[seq] != row {
							t.Errorf("seq %d is now %s, was %s", seq, row, before[seq])
						}
					}
					t.Fatalf("pass %d changed rows from the damaged line %d up (%d rows, %d before)", round, bad, len(after), len(before))
				}
				if got := lines(below); len(got) != 2 || got[1] != bad {
					t.Fatalf("pass %d: the task below has lines %v, want its state and the damaged line %d", round, got, bad)
				}
				if st := status(); st.Horizon >= deletion || st.ForgotUpTo >= bad {
					t.Fatalf("pass %d: the horizon is %d and deletions are looked at up to %d, want both below seq %d", round, st.Horizon, st.ForgotUpTo, bad)
				}
			}
			if _, err := d.db.ExecContext(ctx, `UPDATE changelog SET record_id = $2 WHERE seq = $1`, bad, r); err != nil {
				t.Fatal(err)
			}
			report, err := d.CompactionPass(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if got := status().Damaged; len(got) != 0 {
				t.Fatalf("after the repair the notes are %v, want none", got)
			}
			if got := lines(r); len(got) != 1 {
				t.Fatalf("after the repair R has lines %v, want one state", got)
			}
			if report.Forgotten != 1 || len(lines(gone)) != 0 || status().Horizon < deletion {
				t.Fatalf("after the repair the pass forgot %d records, the purged one has lines %v and the horizon is %d, want it forgotten and the horizon at %d",
					report.Forgotten, lines(gone), status().Horizon, deletion)
			}
			if _, err := d.svc.RebuildRepository(ctx, testdb.Repository(t)); err != nil {
				t.Fatalf("rebuild: %v", err)
			}
			if diff := DiffFoldTables(want, FoldRows(t, d)); len(diff) > 0 {
				t.Fatalf("after the repair the history replays into another fold:\n%s", strings.Join(diff, "\n"))
			}
			if err := d.ArchivePass(ctx); err != nil {
				t.Fatalf("the archive pass after the repair: %v", err)
			}
			verified, err := d.svc.VerifyRepository(ctx, testdb.Repository(t))
			if err != nil || !verified.OK || verified.ArchiveUnverified != "" || verified.ArchiveHead != verified.Head {
				t.Fatalf("after the repair the archive is not the table: %v %+v", err, verified)
			}
		})
	}
}

// The pass reads only its batch's lines, and the rest of each transaction
// they touch: a line of a record the pass keeps whole, damaged inside a
// transaction whose end line belongs to a task the pass compacts and whose
// next write left that end removable, is met by the batch's own read, which
// notes it and stops below it, so every line of the transaction keeps its
// `txn`.
func TestThePassHoldsTheTransactionsItReadsPartOf(t *testing.T) {
	t.Parallel()
	for _, damage := range []struct{ name, sql string }{
		{"the stored checksum", `UPDATE changelog SET hash = sha256(hash) WHERE seq = $1`},
		{"its transaction taken off it", `UPDATE changelog SET txn = NULL WHERE seq = $1`},
	} {
		t.Run(damage.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			const pkg = "held.example.substrate.reamde.dev/held"
			clock := time.Now().UTC()
			d := openInternalDataset(t, WithArchiveInterval(time.Hour), WithCompactionInterval(-1), WithDeletionsKept(0),
				WithTestCompactionClock(func() time.Time { return clock }),
				WithTestCompactionRetention(func(kind string) (bool, time.Duration) { return kind != ledgerTask, time.Hour }))
			if _, err := d.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
				vocabulary.PackageManifest(pkg, 0),
				vocabulary.KindManifest(pkg, map[string]any{"singular": "kept"},
					map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
			}); err != nil {
				t.Fatal(err)
			}
			ender, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": "ender"}})
			if err != nil {
				t.Fatal(err)
			}
			var first, kept string
			if err := d.inTx(ctx, substrate.ActorPerson, false, func(tx *txn) error {
				rec, err := tx.put(substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": "first"}})
				if err != nil {
					return err
				}
				first = rec.ID
				if rec, err = tx.put(substrate.PutInput{Kind: pkg + "/kept", Properties: map[string]any{"name": "kept whole"}}); err != nil {
					return err
				}
				kept = rec.ID
				_, err = tx.patch(eref{Kind: ledgerTask, ID: ender.ID}, substrate.PatchInput{Properties: map[string]any{"name": "ender, in the transaction"}})
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := d.Patch(ctx, substrate.ActorPerson, ledgerTask, ender.ID, substrate.PatchInput{Properties: map[string]any{"name": "ender, last"}}); err != nil {
				t.Fatal(err)
			}
			frame := func() map[int64]sql.NullInt64 {
				t.Helper()
				rows, err := d.db.QueryContext(ctx, `SELECT seq, txn FROM changelog WHERE record_id = ANY($1)`, []string{first, kept, ender.ID})
				if err != nil {
					t.Fatal(err)
				}
				out := map[int64]sql.NullInt64{}
				for rows.Next() {
					var seq int64
					var txn sql.NullInt64
					if err := rows.Scan(&seq, &txn); err != nil {
						t.Fatal(err)
					}
					out[seq] = txn
				}
				if err := errors.Join(rows.Err(), rows.Close()); err != nil {
					t.Fatal(err)
				}
				return out
			}
			// The index is whole before the damage, so the batch's read is
			// the one that meets it.
			if _, err := d.CompactionPass(ctx); err != nil {
				t.Fatal(err)
			}
			clock = clock.Add(2 * time.Hour)
			var bad int64
			if err := d.db.QueryRowContext(ctx, `SELECT seq FROM changelog WHERE record_id = $1`, kept).Scan(&bad); err != nil {
				t.Fatal(err)
			}
			if _, err := d.db.ExecContext(ctx, damage.sql, bad); err != nil {
				t.Fatal(err)
			}
			before := frame()
			if _, err := d.CompactionPass(ctx); err != nil {
				t.Fatal(err)
			}
			if after := frame(); !maps.Equal(after, before) {
				t.Fatalf("the transaction's lines are now %v, want %v: the pass framed them around seq %d", after, before, bad)
			}
			st, err := d.svc.CompactionStatus(ctx, testdb.Repository(t))
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(st.Damaged, []int64{bad}) {
				t.Fatalf("the notes are %v, want [%d]", st.Damaged, bad)
			}
		})
	}
}

// A batch reads a transaction's other lines only where it re-frames it: a
// task whose line sits inside a long transaction of records the pass keeps
// whole, not at its end, is compacted without a read of the rest of it, so a
// damaged line there is neither noted nor touched by the pass.
func TestABatchReadsNoLineOfATransactionItDoesNotReframe(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const pkg = "long.example.substrate.reamde.dev/long"
	clock := time.Now().UTC()
	d := openInternalDataset(t, WithArchiveInterval(time.Hour), WithCompactionInterval(-1), WithDeletionsKept(0),
		WithTestCompactionClock(func() time.Time { return clock }),
		WithTestCompactionRetention(func(kind string) (bool, time.Duration) { return kind != ledgerTask, time.Hour }))
	if _, err := d.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "kept"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
	}); err != nil {
		t.Fatal(err)
	}
	task, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": "task"}})
	if err != nil {
		t.Fatal(err)
	}
	var kept []string
	if err := d.inTx(ctx, substrate.ActorPerson, false, func(tx *txn) error {
		for i := range 8 {
			if i == 4 {
				if _, err := tx.patch(eref{Kind: ledgerTask, ID: task.ID}, substrate.PatchInput{Properties: map[string]any{"name": "task, in the transaction"}}); err != nil {
					return err
				}
			}
			rec, err := tx.put(substrate.PutInput{Kind: pkg + "/kept", Properties: map[string]any{"name": "kept whole"}})
			if err != nil {
				return err
			}
			kept = append(kept, rec.ID)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Patch(ctx, substrate.ActorPerson, ledgerTask, task.ID, substrate.PatchInput{Properties: map[string]any{"name": "task, last"}}); err != nil {
		t.Fatal(err)
	}
	// The index is whole before the damage.
	if _, err := d.CompactionPass(ctx); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(2 * time.Hour)
	var bad int64
	var row string
	if err := d.db.QueryRowContext(ctx, `SELECT seq FROM changelog WHERE record_id = $1`, kept[1]).Scan(&bad); err != nil {
		t.Fatal(err)
	}
	if _, err := d.db.ExecContext(ctx, `UPDATE changelog SET hash = sha256(hash) WHERE seq = $1`, bad); err != nil {
		t.Fatal(err)
	}
	if err := d.db.QueryRowContext(ctx, `SELECT row_to_json(c)::text FROM changelog c WHERE seq = $1`, bad).Scan(&row); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CompactionPass(ctx); err != nil {
		t.Fatal(err)
	}
	var lines int
	if err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM changelog WHERE record_id = $1`, task.ID).Scan(&lines); err != nil || lines != 1 {
		t.Fatalf("the task has %d lines (%v), want one state", lines, err)
	}
	st, err := d.svc.CompactionStatus(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Damaged) != 0 {
		t.Fatalf("the pass noted %v: it read lines of a transaction it does not re-frame", st.Damaged)
	}
	var after string
	if err := d.db.QueryRowContext(ctx, `SELECT row_to_json(c)::text FROM changelog c WHERE seq = $1`, bad).Scan(&after); err != nil || after != row {
		t.Fatalf("the damaged row is now %s (%v), was %s", after, err, row)
	}
}

// A batch finds its records' lines by the foreign-effects index, so it
// compacts nothing past what the index covers while its derivation has not
// finished, read in the batch's own transaction, whatever else bounds it.
func TestABatchStaysInsideTheIndex(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := openInternalDataset(t, WithArchiveInterval(time.Hour), WithCompactionInterval(-1))
	rec, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": "indexed"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"past the index", "past it again"} {
		if _, err := d.Patch(ctx, substrate.ActorPerson, ledgerTask, rec.ID, substrate.PatchInput{Properties: map[string]any{"name": name}}); err != nil {
			t.Fatal(err)
		}
	}
	var first int64
	if err := d.db.QueryRowContext(ctx, `SELECT min(seq) FROM changelog WHERE record_id = $1`, rec.ID).Scan(&first); err != nil {
		t.Fatal(err)
	}
	if _, err := d.db.ExecContext(ctx, `
		INSERT INTO compaction_state (foreign_upto, foreign_done) VALUES ($1, false)
		ON CONFLICT (repository) DO UPDATE SET foreign_upto = EXCLUDED.foreign_upto, foreign_done = false`, first); err != nil {
		t.Fatal(err)
	}
	var report CompactReport
	if err := d.withCompactionTx(ctx, true, func(tx *txn) error {
		var err error
		report, err = tx.compactRecords(1<<62, []eref{{Kind: ledgerTask, ID: rec.ID}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var lines int
	if err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM changelog WHERE record_id = $1`, rec.ID).Scan(&lines); err != nil {
		t.Fatal(err)
	}
	if report.UpTo != first || lines != 3 {
		t.Fatalf("the batch compacted up to %d and left the task %d lines, want up to the index's %d and all three", report.UpTo, lines, first)
	}
}

// A window larger than one pass is finished over several (compactKind): with
// batches of three records and a budget of two batches a pass, each pass
// takes the window on from where the last one stopped, the window completes,
// the old deletion behind it is forgotten, and no record is read twice by
// the window: the purged one is read once more, by its forgetting.
func TestAWindowLargerThanAPassIsFinishedOverSeveral(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var mu sync.Mutex
	read := map[string]int{}
	batches := 0
	clock := time.Now().UTC()
	d := openInternalDataset(t, WithCompactionInterval(-1), WithDeletionsKept(time.Hour),
		WithTestCompactionRetention(func(kind string) (bool, time.Duration) { return kind != ledgerTask, 0 }),
		WithTestCompactionClock(func() time.Time { return clock }),
		WithTestCompactionBatches(3, 0, func(kind string, ids []string, _ int64, _ int) {
			mu.Lock()
			defer mu.Unlock()
			for _, id := range ids {
				read[id]++
			}
		}),
		WithTestCompactionFault(func(stage string) error {
			if stage != compactionAfterBatch {
				return nil
			}
			mu.Lock()
			defer mu.Unlock()
			if batches++; batches%2 == 0 {
				return context.DeadlineExceeded
			}
			return nil
		}))
	put := func(name string) string {
		t.Helper()
		rec, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": name}})
		if err != nil {
			t.Fatal(err)
		}
		return rec.ID
	}
	gone := put("gone")
	if _, err := d.Delete(ctx, substrate.ActorPerson, ledgerTask, gone, substrate.DeleteInput{Purge: true}); err != nil {
		t.Fatal(err)
	}
	var tasks []string
	for i := range 20 {
		id := put(fmt.Sprintf("task %d", i))
		if _, err := d.Patch(ctx, substrate.ActorPerson, ledgerTask, id, substrate.PatchInput{Properties: map[string]any{"name": fmt.Sprintf("task %d, again", i)}}); err != nil {
			t.Fatal(err)
		}
		tasks = append(tasks, id)
	}
	put("the head")
	lines := func(id string) int {
		t.Helper()
		var n int
		if err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM changelog WHERE record_id = $1`, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	clock = time.Now().UTC().Add(2 * time.Hour)
	passes, lastAfter := 0, ""
	for ; passes < 20; passes++ {
		report, err := d.CompactionPass(ctx)
		if err != nil {
			t.Fatalf("pass %d: %v", passes+1, err)
		}
		if report.Finished {
			passes++
			break
		}
		var after string
		var upTo int64
		if err := d.db.QueryRowContext(ctx, `SELECT window_after, window_upto FROM compaction_marks WHERE kind = $1`, ledgerTask).Scan(&after, &upTo); err != nil {
			t.Fatalf("the window after pass %d: %v", passes+1, err)
		}
		if upTo != 0 && after <= lastAfter {
			t.Fatalf("pass %d left the window at %q, not past %q where the last one stopped", passes+1, after, lastAfter)
		}
		lastAfter = after
	}
	if passes < 3 || passes >= 20 {
		t.Fatalf("the window took %d passes, want several and an end", passes)
	}
	for _, id := range tasks {
		if read[id] > 1 {
			t.Errorf("record %s was read in %d batches, want one", id, read[id])
		}
	}
	if read[gone] != 2 {
		t.Errorf("the purged task was read in %d batches, want its window's and its forgetting's", read[gone])
	}
	for _, id := range tasks {
		if n := lines(id); n != 1 {
			t.Errorf("task %s keeps %d lines, want its state's one", id, n)
		}
	}
	if n := lines(gone); n != 0 {
		t.Errorf("the purged task behind the window keeps %d lines, want it forgotten", n)
	}
}

// A batch ends at its line bound as well as its records (compactWindow), and
// a record longer than the bound alone is compacted in steps of it
// (compactLong), each step reading no more than the bound's lines past the
// state the step before left: the fold a rebuild of the result gives is the
// fold before.
func TestABatchIsBoundedByLinesAndALongRecordIsCompactedInSteps(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const bound = 10
	type batch struct {
		ids  []string
		upTo int64
	}
	var mu sync.Mutex
	var seen []batch
	clock := time.Now().UTC()
	d := openInternalDataset(t, WithCompactionInterval(-1),
		WithTestCompactionRetention(func(kind string) (bool, time.Duration) { return kind != ledgerTask, 0 }),
		WithTestCompactionClock(func() time.Time { return clock }),
		WithTestCompactionBatches(0, bound, func(kind string, ids []string, upTo int64, _ int) {
			mu.Lock()
			defer mu.Unlock()
			seen = append(seen, batch{slices.Clone(ids), upTo})
		}))
	write := func(name string, patches int) string {
		t.Helper()
		rec, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": name}})
		if err != nil {
			t.Fatal(err)
		}
		for i := range patches {
			if _, err := d.Patch(ctx, substrate.ActorPerson, ledgerTask, rec.ID, substrate.PatchInput{Properties: map[string]any{"name": fmt.Sprintf("%s %d", name, i)}}); err != nil {
				t.Fatal(err)
			}
		}
		return rec.ID
	}
	long := write("long", 34)
	short := map[string]bool{}
	for i := range 3 {
		short[write(fmt.Sprintf("short %d", i), 3)] = true
	}
	write("the head", 0)
	var longLines []int64
	rows, err := d.db.QueryContext(ctx, `SELECT seq FROM changelog WHERE record_id = $1 ORDER BY seq`, long)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var seq int64
		if err := rows.Scan(&seq); err != nil {
			t.Fatal(err)
		}
		longLines = append(longLines, seq)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}
	want := FoldRows(t, d)
	clock = time.Now().UTC().Add(2 * time.Hour)
	if report, err := d.CompactionPass(ctx); err != nil || !report.Finished {
		t.Fatalf("the pass: %+v (%v)", report, err)
	}
	var steps []int64
	for _, b := range seen {
		switch {
		case slices.Contains(b.ids, long):
			if len(b.ids) != 1 {
				t.Errorf("the long record was compacted in a batch of %v, want alone", b.ids)
			}
			steps = append(steps, b.upTo)
		case short[b.ids[0]]:
			if len(b.ids) > 2 {
				t.Errorf("a batch of %d short records holds %d lines, past the bound of %d", len(b.ids), 4*len(b.ids), bound)
			}
		}
	}
	if len(steps) < len(longLines)/bound {
		t.Fatalf("the long record of %d lines was compacted in %d steps %v, want one every %d lines", len(longLines), len(steps), steps, bound)
	}
	for i, step := range steps[:len(steps)-1] {
		prev := int64(0)
		if i > 0 {
			prev = steps[i-1]
		}
		n := 0
		for _, seq := range longLines {
			if seq > prev && seq <= step {
				n++
			}
		}
		if n > bound {
			t.Errorf("step %d took %d of the record's lines, past the bound of %d", i+1, n, bound)
		}
	}
	var n int
	if err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM changelog WHERE record_id = $1`, long).Scan(&n); err != nil || n != 1 {
		t.Fatalf("the long record keeps %d lines (%v), want its state's one", n, err)
	}
	if _, err := d.svc.RebuildRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if diff := DiffFoldTables(want, FoldRows(t, d)); len(diff) > 0 {
		t.Fatalf("the compacted history replays into another fold:\n%s", strings.Join(diff, "\n"))
	}
}

// A pass compacts no further than the retention in force at that pass
// (compactKind): a window larger than one pass, stopped part way, and then
// the kind's retention lengthened, or kept whole for a pass and then a time
// again, is finished up to the new cutoff alone. A record the first pass
// reached is as that pass left it; one it had not keeps every line the new
// retention keeps, until the clock moves past them and a later pass
// compacts them.
func TestAResumedWindowKeepsTheRetentionInForce(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name    string
		keepAll bool
	}{
		{"the retention lengthened", false},
		{"kept whole for a pass, then a time again", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			var mu sync.Mutex
			firstPass, budget, batches := true, true, 0
			all, keep := false, time.Hour
			reached := map[string]bool{}
			clock := time.Now().UTC().Add(2 * time.Hour)
			d := openInternalDataset(t, WithCompactionInterval(-1),
				WithTestCompactionRetention(func(kind string) (bool, time.Duration) {
					mu.Lock()
					defer mu.Unlock()
					if kind != ledgerTask {
						return true, 0
					}
					return all, keep
				}),
				WithTestCompactionClock(func() time.Time {
					mu.Lock()
					defer mu.Unlock()
					return clock
				}),
				WithTestCompactionBatches(3, 0, func(kind string, ids []string, _ int64, _ int) {
					mu.Lock()
					defer mu.Unlock()
					if kind == ledgerTask && firstPass {
						for _, id := range ids {
							reached[id] = true
						}
					}
				}),
				WithTestCompactionFault(func(stage string) error {
					mu.Lock()
					defer mu.Unlock()
					if stage != compactionAfterBatch || !budget {
						return nil
					}
					if batches++; batches%2 == 0 {
						return context.DeadlineExceeded
					}
					return nil
				}))
			write := func(name string) string {
				t.Helper()
				rec, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": name}})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := d.Patch(ctx, substrate.ActorPerson, ledgerTask, rec.ID, substrate.PatchInput{Properties: map[string]any{"name": name + ", again"}}); err != nil {
					t.Fatal(err)
				}
				return rec.ID
			}
			older, newer := map[string]bool{}, map[string]bool{}
			for i := range 8 {
				older[write(fmt.Sprintf("older %d", i))] = true
			}
			mid := time.Now().UTC()
			for i := range 8 {
				newer[write(fmt.Sprintf("newer %d", i))] = true
			}
			write("the head")
			pass := func() CompactionPassReport {
				t.Helper()
				report, err := d.CompactionPass(ctx)
				if err != nil {
					t.Fatal(err)
				}
				return report
			}
			if report := pass(); report.Finished {
				t.Fatal("the first pass finished its window: the test proves nothing")
			}
			mu.Lock()
			firstPass, budget = false, false
			all = c.keepAll
			mu.Unlock()
			if c.keepAll {
				if report := pass(); !report.Finished {
					t.Fatal("a pass over a kind kept whole did not finish")
				}
			}
			mu.Lock()
			// The retention reaches back to the newer records' start: the
			// older ones are past it, the newer ones inside it.
			all, keep = false, clock.Sub(mid)
			mu.Unlock()
			if report := pass(); !report.Finished {
				t.Fatal("the pass after the retention changed did not finish")
			}
			lines := func(id string) int {
				t.Helper()
				var n int
				if err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM changelog WHERE record_id = $1`, id).Scan(&n); err != nil {
					t.Fatal(err)
				}
				return n
			}
			unreachedNewer := 0
			for id := range older {
				if n := lines(id); n != 1 {
					t.Errorf("an older record keeps %d lines, want its state's one", n)
				}
			}
			for id := range newer {
				want := 2
				if reached[id] {
					want = 1
				} else {
					unreachedNewer++
				}
				if n := lines(id); n != want {
					t.Errorf("a newer record the first pass reached (%v) keeps %d lines, want %d", reached[id], n, want)
				}
			}
			if unreachedNewer == 0 {
				t.Fatal("the first pass reached every newer record: the test proves nothing")
			}
			// Once the stretch between the lowered cutoff and the old one is
			// past the retention too, the records it spared are compacted,
			// none skipped.
			mu.Lock()
			clock = clock.Add(keep)
			mu.Unlock()
			if report := pass(); !report.Finished {
				t.Fatal("the pass after the clock moved did not finish")
			}
			for id := range newer {
				if n := lines(id); n != 1 {
					t.Errorf("once its lines are past the retention, a newer record the first pass reached (%v) keeps %d lines, want its state's one", reached[id], n)
				}
			}
		})
	}
}

// A held record's retry compacts no further than the retention in force at
// its pass either (compactKind): a record whose newest line a parked failure
// names is compacted below it and held, and once the failure is gone and the
// retention lengthened, its retry goes no further than the new cutoff, so
// the line the longer retention keeps stays.
func TestAHeldRecordKeepsTheRetentionInForce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var mu sync.Mutex
	keep := time.Hour
	clock := time.Now().UTC().Add(2 * time.Hour)
	d := openInternalDataset(t, WithCompactionInterval(-1),
		WithTestCompactionRetention(func(kind string) (bool, time.Duration) {
			mu.Lock()
			defer mu.Unlock()
			return kind != ledgerTask, keep
		}),
		WithTestCompactionClock(func() time.Time { return clock }))
	rec, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": "held"}})
	if err != nil {
		t.Fatal(err)
	}
	patch := func(name string) int64 {
		t.Helper()
		if _, err := d.Patch(ctx, substrate.ActorPerson, ledgerTask, rec.ID, substrate.PatchInput{Properties: map[string]any{"name": name}}); err != nil {
			t.Fatal(err)
		}
		var seq int64
		if err := d.db.QueryRowContext(ctx, `SELECT max(seq) FROM changelog WHERE record_id = $1`, rec.ID).Scan(&seq); err != nil {
			t.Fatal(err)
		}
		return seq
	}
	patch("held, again")
	mid := time.Now().UTC()
	newest := patch("held, inside the longer retention")
	if _, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": "the head"}}); err != nil {
		t.Fatal(err)
	}
	lines := func() int {
		t.Helper()
		var n int
		if err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM changelog WHERE record_id = $1`, rec.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if _, err := d.db.ExecContext(ctx, `
		INSERT INTO trigger_failures (id, trigger_id, seq, record_id, attempts, last_error, parked_at)
		VALUES (1, 'parked', $1, $2, 1, 'parked over the newest line', now())`, newest, rec.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CompactionPass(ctx); err != nil {
		t.Fatal(err)
	}
	var held int
	if err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM compaction_held WHERE record_id = $1`, rec.ID).Scan(&held); err != nil || held != 1 || lines() != 2 {
		t.Fatalf("the parked failure left the record held %d (%v) with %d lines, want held with its parked line and a state below it: the test proves nothing", held, err, lines())
	}
	if _, err := d.db.ExecContext(ctx, `DELETE FROM trigger_failures WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	keep = clock.Sub(mid)
	mu.Unlock()
	if _, err := d.CompactionPass(ctx); err != nil {
		t.Fatal(err)
	}
	if n := lines(); n != 2 {
		t.Fatalf("the held record keeps %d lines after its retry, want its older lines' state and the line the longer retention keeps", n)
	}
}

// boundedPass is a repository whose pass sees every ledger task batch, with
// the line bound at ten: the lines each of its transactions read of each
// record, by pass.
type boundedPass struct {
	d     *dataset
	mu    sync.Mutex
	pass  int
	reads []boundedRead
	clock time.Time
}

type boundedRead struct {
	pass  int
	ids   []string
	lines int
}

const passBound = 10

func newBoundedPass(t *testing.T, opts ...Option) *boundedPass {
	t.Helper()
	b := &boundedPass{clock: time.Now().UTC().Add(2 * time.Hour)}
	b.d = openInternalDataset(t, append([]Option{
		WithCompactionInterval(-1),
		WithTestCompactionRetention(func(kind string) (bool, time.Duration) { return kind != ledgerTask, time.Hour }),
		WithTestCompactionClock(func() time.Time { return b.clock }),
		WithTestCompactionBatches(0, passBound, func(kind string, ids []string, _ int64, lines int) {
			if kind != ledgerTask {
				return
			}
			b.mu.Lock()
			defer b.mu.Unlock()
			b.reads = append(b.reads, boundedRead{b.pass, slices.Clone(ids), lines})
		}),
	}, opts...)...)
	return b
}

func (b *boundedPass) run(t *testing.T) CompactionPassReport {
	t.Helper()
	b.mu.Lock()
	b.pass++
	b.mu.Unlock()
	report, err := b.d.CompactionPass(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return report
}

// of is every read of a pass that took id.
func (b *boundedPass) of(pass int, id string) []boundedRead {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []boundedRead
	for _, r := range b.reads {
		if r.pass == pass && slices.Contains(r.ids, id) {
			out = append(out, r)
		}
	}
	return out
}

func (b *boundedPass) lines(t *testing.T, id string) int {
	t.Helper()
	var n int
	if err := b.d.db.QueryRowContext(context.Background(), `SELECT count(*) FROM changelog WHERE record_id = $1`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (b *boundedPass) held(t *testing.T, id string) bool {
	t.Helper()
	var n int
	if err := b.d.db.QueryRowContext(context.Background(), `SELECT count(*) FROM compaction_held WHERE record_id = $1`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}

// A record the pass cannot compact within the line bound is passed by
// (compactLong): what it keeps (the lines parked failures name and every
// line under them, or purges that are not its last) is read again by each
// step, so after a step that leaves it keeping more than the bound the pass
// notes it held and the window moves past it, no transaction reading more
// than twice the bound of it, and a later pass reads a bounded number of its
// lines and stops. Once nothing pins it, it is compacted in steps.
func TestARecordThePassCannotCompactWithinTheBoundIsPassedBy(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		write func(t *testing.T, b *boundedPass) string
	}{
		{"a parked failure's line every few lines", func(t *testing.T, b *boundedPass) string {
			ctx := context.Background()
			rec, err := b.d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": "pinned"}})
			if err != nil {
				t.Fatal(err)
			}
			for i := range 49 {
				if _, err := b.d.Patch(ctx, substrate.ActorPerson, ledgerTask, rec.ID, substrate.PatchInput{Properties: map[string]any{"name": fmt.Sprintf("pinned %d", i)}}); err != nil {
					t.Fatal(err)
				}
			}
			rows, err := b.d.db.QueryContext(ctx, `SELECT seq FROM changelog WHERE record_id = $1 ORDER BY seq`, rec.ID)
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
			for i := 4; i < len(seqs); i += 5 {
				if _, err := b.d.db.ExecContext(ctx, `
					INSERT INTO trigger_failures (id, trigger_id, seq, record_id, attempts, last_error, parked_at)
					VALUES ($1, 'parked', $2, $3, 1, 'parked', now())`, i, seqs[i], rec.ID); err != nil {
					t.Fatal(err)
				}
			}
			return rec.ID
		}},
		{"a record purged and written again", func(t *testing.T, b *boundedPass) string {
			ctx := context.Background()
			id := vocabulary.DerivedMintedID("purged", t.Name())
			for i := range 16 {
				if _, err := b.d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, ID: id, Properties: map[string]any{"name": fmt.Sprintf("round %d", i)}}); err != nil {
					t.Fatal(err)
				}
				if _, err := b.d.Delete(ctx, substrate.ActorPerson, ledgerTask, id, substrate.DeleteInput{Purge: true}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := b.d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, ID: id, Properties: map[string]any{"name": "the last round"}}); err != nil {
				t.Fatal(err)
			}
			return id
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			b := newBoundedPass(t)
			long := c.write(t, b)
			var short []string
			for i := range 3 {
				rec, err := b.d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": fmt.Sprintf("short %d", i)}})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := b.d.Patch(ctx, substrate.ActorPerson, ledgerTask, rec.ID, substrate.PatchInput{Properties: map[string]any{"name": "again"}}); err != nil {
					t.Fatal(err)
				}
				short = append(short, rec.ID)
			}
			if _, err := b.d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": "the head"}}); err != nil {
				t.Fatal(err)
			}
			if n := b.lines(t, long); n < 4*passBound {
				t.Fatalf("the long record has %d lines, want several times the bound", n)
			}
			within := func(pass int) int {
				t.Helper()
				total := 0
				for _, r := range b.of(pass, long) {
					if r.lines > 2*passBound {
						t.Errorf("pass %d: a transaction read %d lines of the long record's batch, past twice the bound", pass, r.lines)
					}
					total += r.lines
				}
				return total
			}
			if report := b.run(t); !report.Finished {
				t.Fatalf("the first pass did not finish its window: %+v", report)
			}
			within(1)
			if !b.held(t, long) {
				t.Fatalf("the long record is not held after the pass that could not compact it")
			}
			var windowUpTo int64
			if err := b.d.db.QueryRowContext(ctx, `SELECT window_upto FROM compaction_marks WHERE kind = $1`, ledgerTask).Scan(&windowUpTo); err != nil || windowUpTo != 0 {
				t.Fatalf("the window stands at %d (%v) after the first pass, want it moved past the long record and done", windowUpTo, err)
			}
			for _, id := range short {
				if n := b.lines(t, id); n != 1 {
					t.Errorf("a short record beside the long one keeps %d lines, want its state's one", n)
				}
			}
			b.run(t)
			if total := within(2); total == 0 || total > 3*passBound {
				t.Errorf("the second pass read %d lines of the long record, want a bounded few", total)
			}
			if c.name != "a parked failure's line every few lines" {
				return
			}
			if _, err := b.d.db.ExecContext(ctx, `DELETE FROM trigger_failures`); err != nil {
				t.Fatal(err)
			}
			b.run(t)
			within(3)
			if steps := len(b.of(3, long)); steps < 2 {
				t.Errorf("the long record was compacted in %d transactions once nothing pinned it, want steps", steps)
			}
			if n := b.lines(t, long); n != 1 {
				t.Errorf("the long record keeps %d lines once nothing pinned it, want its state's one", n)
			}
		})
	}
}

// Held records are retried within the line bound (compactHeld): records each
// within the bound but together past it are retried one line-bounded batch
// a pass, none reading past the bound, each of them within the passes the
// list's length needs, and the retries start over from the first once they
// reach the last.
func TestHeldRecordsAreRetriedWithinTheBound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := newBoundedPass(t)
	var held []string
	failure := 0
	for i := range 4 {
		rec, err := b.d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": fmt.Sprintf("held %d", i)}})
		if err != nil {
			t.Fatal(err)
		}
		for j := range 7 {
			if _, err := b.d.Patch(ctx, substrate.ActorPerson, ledgerTask, rec.ID, substrate.PatchInput{Properties: map[string]any{"name": fmt.Sprintf("held %d %d", i, j)}}); err != nil {
				t.Fatal(err)
			}
			if j == 2 || j == 6 {
				var seq int64
				if err := b.d.db.QueryRowContext(ctx, `SELECT max(seq) FROM changelog WHERE record_id = $1`, rec.ID).Scan(&seq); err != nil {
					t.Fatal(err)
				}
				failure++
				if _, err := b.d.db.ExecContext(ctx, `
					INSERT INTO trigger_failures (id, trigger_id, seq, record_id, attempts, last_error, parked_at)
					VALUES ($1, 'parked', $2, $3, 1, 'parked', now())`, failure, seq, rec.ID); err != nil {
					t.Fatal(err)
				}
			}
		}
		held = append(held, rec.ID)
	}
	if _, err := b.d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": "the head"}}); err != nil {
		t.Fatal(err)
	}
	b.run(t)
	total := 0
	for _, id := range held {
		if !b.held(t, id) {
			t.Fatalf("record %s is not held after the first pass: the test proves nothing", id)
		}
		total += b.lines(t, id)
	}
	if total <= passBound {
		t.Fatalf("the held records keep %d lines together, want past the bound: the test proves nothing", total)
	}
	slices.Sort(held)
	retried := map[string]int{}
	var took [][]string
	for range len(held) + 1 {
		b.run(t)
		var ids []string
		b.mu.Lock()
		for _, r := range b.reads {
			if r.pass == b.pass {
				ids = append(ids, r.ids...)
				if r.lines > passBound {
					t.Errorf("a retry of held records %v read %d lines, past the bound", r.ids, r.lines)
				}
			}
		}
		b.mu.Unlock()
		for _, id := range ids {
			if _, ok := retried[id]; !ok {
				retried[id] = len(took)
			}
		}
		took = append(took, ids)
	}
	for _, id := range held {
		if pass, ok := retried[id]; !ok || pass >= len(held) {
			t.Errorf("held record %s was not retried within the %d passes the list needs: %v", id, len(held), took)
		}
	}
	if len(took[0]) >= len(held) {
		t.Fatalf("one pass retried every held record: the test proves nothing: %v", took)
	}
	end := slices.IndexFunc(took, func(ids []string) bool { return slices.Contains(ids, held[len(held)-1]) })
	switch {
	case end < 0 || end+1 >= len(took):
		t.Errorf("no pass after the one that retried the last held record: %v", took)
	case len(took[end+1]) == 0 || took[end+1][0] != held[0]:
		t.Errorf("the pass after the one that retried the last held record retried %v, want the list from its first, %s", took[end+1], held[0])
	}
}

// A line known not to verify, found between two passes of one window below
// the window's cutoff and below the old mark, takes the mark below it, and
// once the line is written back every record the window had not finished is
// compacted.
func TestDamageFoundInsideAWindowTakesTheMarkBelowIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var mu sync.Mutex
	budget, batches := false, 0
	b := newBoundedPass(t, WithTestCompactionFault(func(stage string) error {
		mu.Lock()
		defer mu.Unlock()
		if stage != compactionAfterBatch || !budget {
			return nil
		}
		if batches++; batches%2 == 0 {
			return context.DeadlineExceeded
		}
		return nil
	}))
	write := func(name string) string {
		t.Helper()
		rec, err := b.d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": name}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := b.d.Patch(ctx, substrate.ActorPerson, ledgerTask, rec.ID, substrate.PatchInput{Properties: map[string]any{"name": name + ", again"}}); err != nil {
			t.Fatal(err)
		}
		return rec.ID
	}
	first := write("before the first window")
	write("the first window's head")
	if report := b.run(t); !report.Finished {
		t.Fatal("the first window did not finish")
	}
	var damaged int64
	if err := b.d.db.QueryRowContext(ctx, `SELECT max(seq) FROM changelog WHERE record_id = $1`, first).Scan(&damaged); err != nil {
		t.Fatal(err)
	}
	var later []string
	for i := range 12 {
		later = append(later, write(fmt.Sprintf("later %d", i)))
	}
	write("the head")
	mu.Lock()
	budget = true
	mu.Unlock()
	if report := b.run(t); report.Finished {
		t.Fatal("the second window finished in one pass: the test proves nothing")
	}
	var mark, windowUpTo int64
	if err := b.d.db.QueryRowContext(ctx, `SELECT upto, window_upto FROM compaction_marks WHERE kind = $1`, ledgerTask).Scan(&mark, &windowUpTo); err != nil || damaged >= mark || windowUpTo <= mark {
		t.Fatalf("mark %d, window to %d (%v): want a window in progress above a mark above line %d", mark, windowUpTo, err, damaged)
	}
	var actor string
	if err := b.d.db.QueryRowContext(ctx, `SELECT actor FROM changelog WHERE seq = $1`, damaged).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if _, err := b.d.db.ExecContext(ctx, `UPDATE changelog SET actor = 'not who wrote it' WHERE seq = $1`, damaged); err != nil {
		t.Fatal(err)
	}
	if err := b.d.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error { return t.noteDamage(damaged, "a test") }); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	budget = false
	mu.Unlock()
	b.run(t)
	if err := b.d.db.QueryRowContext(ctx, `SELECT upto, window_upto FROM compaction_marks WHERE kind = $1`, ledgerTask).Scan(&mark, &windowUpTo); err != nil || mark >= damaged || windowUpTo != 0 {
		t.Fatalf("after the damage was found the mark is %d and the window to %d (%v), want the mark below line %d and no window", mark, windowUpTo, err, damaged)
	}
	if _, err := b.d.db.ExecContext(ctx, `UPDATE changelog SET actor = $2 WHERE seq = $1`, damaged, actor); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		b.run(t)
	}
	for _, id := range later {
		if n := b.lines(t, id); n != 1 {
			t.Errorf("a record the window had not finished keeps %d lines after the repair, want its state's one", n)
		}
	}
}

// A held long record leaves the held list only in the step that finishes it
// (compactHeld): a record a pass passed by while parked failures pinned it,
// its failures settled, and every later pass stopped after one step, stays
// held at every stop, and the passes go on with it until it is finished.
func TestAHeldLongRecordStaysHeldUntilItIsFinished(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var mu sync.Mutex
	stop := false
	b := newBoundedPass(t, WithTestCompactionFault(func(stage string) error {
		mu.Lock()
		defer mu.Unlock()
		if stage == compactionAfterBatch && stop {
			return context.DeadlineExceeded
		}
		return nil
	}))
	rec, err := b.d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": "pinned"}})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 49 {
		if _, err := b.d.Patch(ctx, substrate.ActorPerson, ledgerTask, rec.ID, substrate.PatchInput{Properties: map[string]any{"name": fmt.Sprintf("pinned %d", i)}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.d.db.ExecContext(ctx, `
		INSERT INTO trigger_failures (id, trigger_id, seq, record_id, attempts, last_error, parked_at)
		SELECT row_number() OVER (ORDER BY seq), 'parked', seq, record_id, 1, 'parked', now()
		FROM (SELECT seq, record_id, row_number() OVER (ORDER BY seq) AS n FROM changelog WHERE record_id = $1) l
		WHERE n % 5 = 0`, rec.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := b.d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": "the head"}}); err != nil {
		t.Fatal(err)
	}
	b.run(t)
	if !b.held(t, rec.ID) {
		t.Fatal("the pinned record was not passed by and held: the test proves nothing")
	}
	if _, err := b.d.db.ExecContext(ctx, `DELETE FROM trigger_failures`); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	stop = true
	mu.Unlock()
	passes := 0
	for ; passes < 20 && b.lines(t, rec.ID) > 1; passes++ {
		b.run(t)
		if b.lines(t, rec.ID) > 1 && !b.held(t, rec.ID) {
			t.Fatalf("after pass %d stopped in its retry the record keeps %d lines and is on no held list", passes+1, b.lines(t, rec.ID))
		}
	}
	if passes < 2 || b.lines(t, rec.ID) != 1 {
		t.Fatalf("the record keeps %d lines after %d stopped passes, want it finished over several", b.lines(t, rec.ID), passes)
	}
	if b.held(t, rec.ID) {
		t.Error("the finished record is still held")
	}
}

// Forgetting an old deletion is held to the line bound (forgetDeletions): a
// purged record of a kind kept whole, several times the bound long and past
// the deletion period, is compacted in steps up to its purge and then
// forgotten, with every pass stopped after one step, no transaction reading
// more than twice the bound of it. The horizon reaches its purge's line only
// once it is gone, so a read of the changes after a seq below that line is
// refused only then.
func TestForgettingALongDeletionIsHeldToTheBound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := newBoundedPass(t, WithDeletionsKept(time.Hour),
		WithTestCompactionRetention(func(string) (bool, time.Duration) { return true, 0 }),
		WithTestCompactionFault(func(stage string) error {
			if stage == compactionAfterBatch {
				return context.DeadlineExceeded
			}
			return nil
		}))
	rec, err := b.d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": "long"}})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 4 * passBound {
		if _, err := b.d.Patch(ctx, substrate.ActorPerson, ledgerTask, rec.ID, substrate.PatchInput{Properties: map[string]any{"name": fmt.Sprintf("long %d", i)}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.d.Delete(ctx, substrate.ActorPerson, ledgerTask, rec.ID, substrate.DeleteInput{Purge: true}); err != nil {
		t.Fatal(err)
	}
	var purge int64
	if err := b.d.db.QueryRowContext(ctx, `SELECT max(seq) FROM changelog WHERE record_id = $1`, rec.ID).Scan(&purge); err != nil {
		t.Fatal(err)
	}
	if _, err := b.d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": "the head"}}); err != nil {
		t.Fatal(err)
	}
	refused := func() bool {
		t.Helper()
		head, err := b.d.Head(ctx)
		if err != nil {
			t.Fatal(err)
		}
		below := head.Horizon > purge-1
		_, err = b.d.Changes(ctx, purge-1, substrate.ChangeFilter{Kinds: []string{ledgerTask}}, 10)
		if stale := errors.Is(err, substrate.ErrStaleHistory); stale != below || (err != nil && !stale) {
			t.Fatalf("a read after seq %d answered %v with the horizon at %d", purge-1, err, head.Horizon)
		}
		return below
	}
	if refused() {
		t.Fatal("a reader below the purge is refused before any pass")
	}
	reads, passes := 0, 0
	for ; passes < 20 && b.lines(t, rec.ID) > 0; passes++ {
		b.run(t)
		for _, r := range b.of(b.pass, rec.ID) {
			reads++
			if r.lines > 2*passBound {
				t.Errorf("pass %d: a transaction read %d lines of the purged record, past twice the bound", b.pass, r.lines)
			}
		}
		if n := b.lines(t, rec.ID); n > 0 && refused() {
			t.Fatalf("after pass %d the purged record keeps %d lines and a reader below its purge is refused", b.pass, n)
		}
	}
	if n := b.lines(t, rec.ID); n != 0 {
		t.Fatalf("the purged record keeps %d lines after %d passes, want it forgotten", n, passes)
	}
	if passes < 2 || reads < 3 {
		t.Errorf("the purged record was forgotten over %d passes in %d transactions, want steps and then the forgetting", passes, reads)
	}
	if !refused() {
		t.Error("the purged record is forgotten and a reader below its purge is not refused")
	}
}
