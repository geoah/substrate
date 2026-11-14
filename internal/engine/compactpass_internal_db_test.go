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
// column of every row from it up as it was, R's lines among them, and looks
// for deletions to forget no further. (A later purged task is forgotten at
// its permanent delete, before the damage: erase.go.) Once the row is
// written back, the next pass drops the note and compacts R by itself, a
// rebuild gives the fold from before the damage, and the archive holds the
// table.
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
			if noter == "the backfill" {
				// A repository from before the index: the pass derives it
				// first (a new one is indexed from its creation).
				if _, err := d.db.ExecContext(ctx, `UPDATE compaction_state SET foreign_upto = 0, foreign_done = false`); err != nil {
					t.Fatal(err)
				}
			} else {
				// A pass inside every retention compacts nothing.
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
				if st := status(); st.ForgotUpTo >= bad {
					t.Fatalf("pass %d: deletions are looked at up to %d, want below seq %d", round, st.ForgotUpTo, bad)
				}
			}
			if _, err := d.db.ExecContext(ctx, `UPDATE changelog SET record_id = $2 WHERE seq = $1`, bad, r); err != nil {
				t.Fatal(err)
			}
			pass()
			if got := status().Damaged; len(got) != 0 {
				t.Fatalf("after the repair the notes are %v, want none", got)
			}
			if got := lines(r); len(got) != 1 {
				t.Fatalf("after the repair R has lines %v, want one state", got)
			}
			if len(lines(gone)) != 0 || status().Horizon < deletion || status().ForgotUpTo < bad {
				t.Fatalf("after the repair the purged task has lines %v, the horizon is %d and deletions are looked at up to %d, want none, at least %d and past %d",
					lines(gone), status().Horizon, status().ForgotUpTo, deletion, bad)
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
