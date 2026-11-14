package engine

// THE ERASURE (erase.go, decision 0304): a permanent delete leaves no value
// of the record in the log, the fold or the archive.

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// valuesLeft names every place under the repository's database and data
// root that still holds one of the values (a file it cannot read fails the
// test): a changelog line, a row of any
// table in the repository's schema (its text), a stored Idempotency-Key
// outcome (bytes), and any file under the data root, the archive's included,
// by its contents or its name.
func valuesLeft(t *testing.T, d *dataset, values ...string) []string {
	t.Helper()
	ctx := context.Background()
	var out []string
	tables, err := d.db.QueryContext(ctx, `
		SELECT table_name FROM information_schema.tables
		WHERE table_schema = current_schema() AND table_type = 'BASE TABLE' ORDER BY table_name`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for tables.Next() {
		var n string
		if err := tables.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	if err := errors.Join(tables.Err(), tables.Close()); err != nil {
		t.Fatal(err)
	}
	for _, table := range names {
		for _, v := range values {
			var n int
			if err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM `+table+` x WHERE x::text LIKE '%' || $1 || '%'`, v).Scan(&n); err != nil {
				t.Fatalf("scan %s: %v", table, err)
			}
			if n > 0 {
				out = append(out, table+" holds "+v)
			}
		}
	}
	for _, v := range values {
		var n int
		if err := d.db.QueryRowContext(ctx, `
			SELECT count(*) FROM idempotency_keys WHERE position(convert_to($1, 'UTF8') in coalesce(outcome, ''::bytea)) > 0`, v).Scan(&n); err != nil {
			t.Fatalf("scan idempotency outcomes: %v", err)
		}
		if n > 0 {
			out = append(out, "an Idempotency-Key outcome holds "+v)
		}
	}
	root := d.svc.dataRoot
	if err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, v := range values {
			if strings.Contains(string(raw), v) || strings.Contains(filepath.Base(path), v) {
				rel, _ := filepath.Rel(root, path)
				out = append(out, "file "+rel+" holds "+v)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("a file under the data root does not read, so the scan cannot say what it holds: %v", err)
	}
	return out
}

func TestAPermanentDeleteLeavesNoValueOfTheRecordInTheLog(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := openInternalDataset(t, WithArchiveInterval(time.Hour))
	const name, again, note = "erasable-first-name", "erasable-second-name", "erasable-note"
	rec, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": name}})
	if err != nil {
		t.Fatal(err)
	}
	keyed := substrate.WithIdempotencyKey(ctx, "erasable-patch")
	patch := substrate.PatchInput{
		Properties:  map[string]any{"name": again},
		Annotations: map[string]any{"owner/note": map[string]any{"why": note}},
	}
	if _, err := d.Patch(keyed, substrate.ActorPerson, ledgerTask, rec.ID, patch); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": "a bystander"}}); err != nil {
		t.Fatal(err)
	}
	if err := d.ArchivePass(ctx); err != nil {
		t.Fatal(err)
	}
	if left := valuesLeft(t, d, name, again, note); len(left) == 0 {
		t.Fatal("no value of the record is anywhere before its delete: the test proves nothing")
	}
	if _, err := d.Delete(ctx, substrate.ActorPerson, ledgerTask, rec.ID, substrate.DeleteInput{Purge: true}); err != nil {
		t.Fatalf("permanent delete: %v", err)
	}
	if err := d.ArchivePass(ctx); err != nil {
		t.Fatal(err)
	}
	if left := valuesLeft(t, d, name, again, note); len(left) > 0 {
		t.Fatalf("after the permanent delete and the next archive pass, values remain:\n%s", strings.Join(left, "\n"))
	}
	want := FoldRows(t, d)
	if _, err := d.svc.RebuildRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatal(err)
	}
	if diff := DiffFoldTables(want, FoldRows(t, d)); len(diff) > 0 {
		t.Fatalf("the erased history replays into another fold:\n%s", strings.Join(diff, "\n"))
	}
	var pending int
	if err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM compaction_erasures`).Scan(&pending); err != nil || pending != 0 {
		t.Fatalf("%d erasures still pending (%v)", pending, err)
	}
	// The key's effect stays run-once: a retry is told its outcome went with
	// the history, and nothing runs again.
	if _, err := d.Patch(keyed, substrate.ActorPerson, ledgerTask, rec.ID, patch); !errors.Is(err, substrate.ErrFirstAttemptLanded) || !strings.Contains(err.Error(), "erased") {
		t.Fatalf("a retry of the keyed patch after the erasure: %v, want it told the outcome was erased", err)
	}
}

// While a line of the history is known not to verify, nothing at or above it
// is erased (damage.go). A line of a task whose `record_id` the damage
// changed is one no read of the task meets; the index's derivation notes it.
// A permanent delete then deletes the task and holds its history's erasure,
// and a forget of the live task holds its own, intent kept and nothing of
// the history edited, and the reply and a forget asked to finish name the
// line. Once the row is written back, the next pass drops the note and
// finishes the erasure: no value it erases is left in the table or, after an
// archive pass, in the archive.
func TestAnErasureWaitsBelowALineKnownNotToVerify(t *testing.T) {
	t.Parallel()
	const before, url, after = "erasable-before", "https://example.com/erasable-on-the-damaged-line", "erasable-after"
	for _, door := range []struct {
		name  string
		erase func(d *dataset, id string) (*substrate.Erasure, error)
		gone  []string
		stay  []string
	}{
		{"a permanent delete", func(d *dataset, id string) (*substrate.Erasure, error) {
			rec, err := d.Delete(context.Background(), substrate.ActorPerson, ledgerTask, id, substrate.DeleteInput{Purge: true})
			if err != nil {
				return nil, err
			}
			return rec.Erasure, nil
		}, []string{before, url, after}, nil},
		{"a forget", func(d *dataset, id string) (*substrate.Erasure, error) {
			return d.Forget(context.Background(), substrate.ActorPerson, ledgerTask, id, substrate.ForgetInput{})
		}, []string{before}, []string{url, after}},
	} {
		t.Run(door.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			d := openInternalDataset(t, WithArchiveInterval(time.Hour), WithCompactionInterval(-1),
				WithTestCompactionRetention(func(string) (bool, time.Duration) { return true, 0 }))
			rec, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": before}})
			if err != nil {
				t.Fatal(err)
			}
			patch := func(props map[string]any) int64 {
				t.Helper()
				if _, err := d.Patch(ctx, substrate.ActorPerson, ledgerTask, rec.ID, substrate.PatchInput{Properties: props}); err != nil {
					t.Fatal(err)
				}
				var seq int64
				if err := d.db.QueryRowContext(ctx, `SELECT max(seq) FROM changelog WHERE record_id = $1`, rec.ID).Scan(&seq); err != nil {
					t.Fatal(err)
				}
				return seq
			}
			patch(map[string]any{"name": before + ", again"})
			bad := patch(map[string]any{"url": url})
			patch(map[string]any{"name": after})
			bystander, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": "a bystander"}})
			if err != nil {
				t.Fatal(err)
			}
			if err := d.ArchivePass(ctx); err != nil {
				t.Fatal(err)
			}
			if left := valuesLeft(t, d, door.gone...); len(left) == 0 {
				t.Fatal("no value the erasure erases is anywhere before it: the test proves nothing")
			}
			if _, err := d.db.ExecContext(ctx, `UPDATE changelog SET record_id = $2 WHERE seq = $1`, bad, bystander.ID); err != nil {
				t.Fatal(err)
			}
			// The pass's derivation of its index meets the line: a repository
			// from before the index, which a new one is not.
			if _, err := d.db.ExecContext(ctx, `UPDATE compaction_state SET foreign_upto = 0, foreign_done = false`); err != nil {
				t.Fatal(err)
			}
			if _, err := d.CompactionPass(ctx); err != nil {
				t.Fatal(err)
			}
			if st, err := d.svc.CompactionStatus(ctx, testdb.Repository(t)); err != nil || !slices.Equal(st.Damaged, []int64{bad}) {
				t.Fatalf("the notes are %v (%v), want [%d]", st.Damaged, err, bad)
			}
			rows := func(upTo int64) map[int64]string {
				t.Helper()
				got, err := d.db.QueryContext(ctx, `SELECT seq, row_to_json(c)::text FROM changelog c WHERE seq <= $1`, upTo)
				if err != nil {
					t.Fatal(err)
				}
				out := map[int64]string{}
				for got.Next() {
					var seq int64
					var row string
					if err := got.Scan(&seq, &row); err != nil {
						t.Fatal(err)
					}
					out[seq] = row
				}
				if err := errors.Join(got.Err(), got.Close()); err != nil {
					t.Fatal(err)
				}
				return out
			}
			head, err := tableChangelogHead(ctx, d.db)
			if err != nil {
				t.Fatal(err)
			}
			history := rows(head)
			held := func(e *substrate.Erasure, from string) {
				t.Helper()
				if e == nil || !e.Pending || e.Damaged != bad || !strings.Contains(e.Held, fmt.Sprintf("seq %d does not verify", bad)) {
					t.Fatalf("%s: the erasure is %+v, want it pending and held on seq %d", from, e, bad)
				}
			}
			erasure, err := door.erase(d, rec.ID)
			if err != nil {
				t.Fatalf("%s: %v", door.name, err)
			}
			held(erasure, "the reply")
			if got := rows(head); !maps.Equal(got, history) {
				t.Fatalf("the held erasure changed the history before it: %d rows, %d before", len(got), len(history))
			}
			finished, err := d.Forget(ctx, substrate.ActorPerson, ledgerTask, rec.ID, substrate.ForgetInput{Finish: true})
			if err != nil {
				t.Fatalf("forget: %v", err)
			}
			held(finished, "a forget asked to finish it")
			if _, err := d.db.ExecContext(ctx, `UPDATE changelog SET record_id = $2 WHERE seq = $1`, bad, rec.ID); err != nil {
				t.Fatal(err)
			}
			report, err := d.CompactionPass(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if report.Erased < 1 {
				t.Fatalf("the pass after the repair finished %d erasures, want the task's", report.Erased)
			}
			var pending, noted int
			if err := d.db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM compaction_erasures), (SELECT count(*) FROM changelog_damage)`).Scan(&pending, &noted); err != nil || pending != 0 || noted != 0 {
				t.Fatalf("after the repair %d erasures are pending and %d lines noted (%v), want none", pending, noted, err)
			}
			if err := d.ArchivePass(ctx); err != nil {
				t.Fatalf("the archive pass after the repair: %v", err)
			}
			if left := valuesLeft(t, d, door.gone...); len(left) > 0 {
				t.Fatalf("after the repair, the pass and an archive pass, values remain:\n%s", strings.Join(left, "\n"))
			}
			for _, v := range door.stay {
				if left := valuesLeft(t, d, v); len(left) == 0 {
					t.Fatalf("the current value %q went with the erasure", v)
				}
			}
			want := FoldRows(t, d)
			if _, err := d.svc.RebuildRepository(ctx, testdb.Repository(t)); err != nil {
				t.Fatal(err)
			}
			if diff := DiffFoldTables(want, FoldRows(t, d)); len(diff) > 0 {
				t.Fatalf("the erased history replays into another fold:\n%s", strings.Join(diff, "\n"))
			}
		})
	}
}

// What remains to erase is part of what the delete commits, in the history:
// a permanent delete cut short between its two transactions leaves its
// intent, and whatever happens to the process and the database then, the
// next pass finishes the erasure and no value of the record is left, in the
// table or, after an archive pass, in the archive. The cases: the process
// stopped between them; a rebuild between them; and an archive published
// between them restored after the original finished, which is what a
// database restored from before the second transaction reads. A restore
// starts an archive life of its own, and the life it read, which no server
// continues, keeps what it held as any copy taken before does.
func TestAnErasureCutShortBetweenItsTransactionsIsFinishedByThePass(t *testing.T) {
	t.Parallel()
	const first, second = "crash-erasable-first", "crash-erasable-second"
	for _, cut := range []string{"stopped between them", "rebuilt between them", "restored from an archive published between them"} {
		t.Run(cut, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			var stop atomic.Bool
			d := openInternalDataset(t, WithArchiveInterval(time.Hour), WithCompactionInterval(-1),
				WithTestCompactionFault(func(stage string) error {
					if stage == erasureBetween && stop.Load() {
						return errors.New("stopped between the delete and its erasure")
					}
					return nil
				}))
			rec, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": first}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := d.Patch(ctx, substrate.ActorPerson, ledgerTask, rec.ID, substrate.PatchInput{Properties: map[string]any{"name": second}}); err != nil {
				t.Fatal(err)
			}
			if err := d.ArchivePass(ctx); err != nil {
				t.Fatal(err)
			}
			stop.Store(true)
			if _, err := d.Delete(ctx, substrate.ActorPerson, ledgerTask, rec.ID, substrate.DeleteInput{Purge: true}); err == nil {
				t.Fatal("the delete was not stopped between its transactions: the test proves nothing")
			}
			stop.Store(false)
			pending := func(d *dataset) int {
				t.Helper()
				var n int
				if err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM compaction_erasures`).Scan(&n); err != nil {
					t.Fatal(err)
				}
				return n
			}
			if pending(d) == 0 || len(valuesLeft(t, d, first, second)) == 0 {
				t.Fatal("the delete stopped between its transactions left no intent or no value: the test proves nothing")
			}
			target, source := d, []string(nil)
			switch cut {
			case "rebuilt between them":
				if _, err := d.svc.RebuildRepository(ctx, testdb.Repository(t)); err != nil {
					t.Fatal(err)
				}
				if pending(d) == 0 {
					t.Fatal("the rebuild lost the intent")
				}
			case "restored from an archive published between them":
				if err := d.ArchivePass(ctx); err != nil {
					t.Fatal(err)
				}
				root := archiveTaken(t, d)
				lives, err := filepath.Glob(filepath.Join(root, "archive", "repositories", "*", "a*"))
				if err != nil || len(lives) == 0 {
					t.Fatalf("the archive taken holds no life (%v): the test proves nothing", err)
				}
				for _, life := range lives {
					source = append(source, filepath.Base(life))
				}
				if _, err := d.CompactionPass(ctx); err != nil {
					t.Fatal(err)
				}
				if pending(d) != 0 {
					t.Fatal("the original's pass left the erasure")
				}
				if target, err = restoredFrom(t, root, WithCompactionInterval(-1)); err != nil {
					t.Fatalf("restore: %v", err)
				}
				if pending(target) == 0 || len(valuesLeft(t, target, first, second)) == 0 {
					t.Fatal("the restored repository holds no intent or no value: the test proves nothing")
				}
			}
			report, err := target.CompactionPass(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if report.Erased != 1 || pending(target) != 0 {
				t.Fatalf("the pass finished %d erasures and left %d, want the record's finished", report.Erased, pending(target))
			}
			if err := target.ArchivePass(ctx); err != nil {
				t.Fatal(err)
			}
			left := slices.DeleteFunc(valuesLeft(t, target, first, second), func(place string) bool {
				return slices.ContainsFunc(source, func(life string) bool { return strings.Contains(place, "/"+life+"/") })
			})
			if len(left) > 0 {
				t.Fatalf("after the pass and an archive pass, values remain:\n%s", strings.Join(left, "\n"))
			}
		})
	}
}

// A permanent delete reaches the change requests that target the record and
// the records merged into it, the labels and annotations a merge line
// carries of the pair among them (decision 0305): none of their values is
// left in the table, the key store, the data root or, after an archive pass,
// the archive; the reply names the records it purged with the record; and a
// rebuild gives the same fold.
func TestAPermanentDeleteReachesWhatAsksToChangeTheRecordAndWhatWasMergedIntoIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := openInternalDataset(t, WithArchiveInterval(time.Hour))
	const pkg = "reach.example.substrate.reamde.dev/reach"
	if _, err := d.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "box"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
	}); err != nil {
		t.Fatal(err)
	}
	put := func(in substrate.PutInput) *substrate.Record {
		t.Helper()
		rec, err := d.Put(ctx, substrate.ActorPerson, in)
		if err != nil {
			t.Fatal(err)
		}
		return rec
	}
	const boxed, merged, asked = "erasable-box", "erasable-merged-box", "erasable-change-request"
	const label, loserNote, winnerNote = "erasable-moved-label", "erasable-moved-note", "erasable-overwritten-note"
	box := put(substrate.PutInput{
		Kind: pkg + "/box", Properties: map[string]any{"name": boxed},
		Annotations: map[string]any{"owner/note": map[string]any{"why": winnerNote}},
	})
	loser := put(substrate.PutInput{
		Kind: pkg + "/box", Properties: map[string]any{"name": merged},
		Labels:      map[string]any{"owner/kept": label},
		Annotations: map[string]any{"owner/note": map[string]any{"why": loserNote}},
	})
	if _, err := d.Merge(ctx, substrate.ActorPerson, substrate.MergeInput{Kind: box.Kind, Winner: box.ID, Loser: loser.ID}); err != nil {
		t.Fatalf("merge: %v", err)
	}
	request := put(substrate.PutInput{Kind: kindPatchRequest, Properties: map[string]any{
		"diff":   map[string]any{"properties": map[string]any{"name": asked}},
		"target": vocabulary.RecordPath(box.Kind, box.ID),
	}})
	put(substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": "a bystander"}})
	if err := d.ArchivePass(ctx); err != nil {
		t.Fatal(err)
	}
	values := []string{boxed, merged, asked, label, loserNote, winnerNote}
	for _, v := range values {
		if left := valuesLeft(t, d, v); len(left) == 0 {
			t.Fatalf("%q is nowhere before the delete: the test proves nothing", v)
		}
	}
	deleted, err := d.Delete(ctx, substrate.ActorPerson, box.Kind, box.ID, substrate.DeleteInput{Purge: true})
	if err != nil {
		t.Fatalf("permanent delete: %v", err)
	}
	if deleted.Erasure == nil || deleted.Erasure.Pending {
		t.Fatalf("the delete's erasure is %+v, want it done", deleted.Erasure)
	}
	for _, r := range []*substrate.Record{loser, request} {
		if !slices.Contains(deleted.Erasure.Purged, vocabulary.RecordPath(r.Kind, r.ID)) {
			t.Fatalf("the delete's reply names %v as purged, want %s among them", deleted.Erasure.Purged, r.ID)
		}
	}
	if err := d.ArchivePass(ctx); err != nil {
		t.Fatal(err)
	}
	if left := valuesLeft(t, d, values...); len(left) > 0 {
		t.Fatalf("after the permanent delete and an archive pass, values remain:\n%s", strings.Join(left, "\n"))
	}
	want := FoldRows(t, d)
	if _, err := d.svc.RebuildRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatal(err)
	}
	if diff := DiffFoldTables(want, FoldRows(t, d)); len(diff) > 0 {
		t.Fatalf("the erased history replays into another fold:\n%s", strings.Join(diff, "\n"))
	}
}

// THE BOUNDARY (decision 0305): a permanent delete erases the record it is
// asked for, and what it owns is deleted with it as a purge always deleted
// it (decision 0122): tombstoned by the cascade, for the sweep, its history
// its own. A box owns two items, one live and one deleted before, each
// written twice with two properties, and the live one owns a part. After
// the permanent delete no value of the box is left anywhere and no intent
// names what it owned; both items are tombstoned and the sweep collects
// them, tombstoning the part; every line of what it owned is still there as
// it was; and a put at the box's id creates a fresh record at once, before
// the sweep runs as after it.
func TestAPermanentDeleteLeavesWhatTheRecordOwnsToTheSweep(t *testing.T) {
	t.Parallel()
	for _, beforeSweep := range []bool{true, false} {
		name := "a put after the sweep"
		if beforeSweep {
			name = "a put before the sweep"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			leavesWhatItOwnsToTheSweep(t, beforeSweep)
		})
	}
}

func leavesWhatItOwnsToTheSweep(t *testing.T, putBeforeSweep bool) {
	ctx := context.Background()
	d := openInternalDataset(t, WithArchiveInterval(time.Hour))
	const pkg = "boundary.example.substrate.reamde.dev/boundary"
	if _, err := d.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "box"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "item"},
			map[string]any{"properties": map[string]any{
				"name": map[string]any{"type": "string"},
				"note": map[string]any{"type": "string"},
				"box":  map[string]any{"type": "reference", "kind": pkg + "/box", "onDelete": "cascade"},
			}}),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "part"},
			map[string]any{"properties": map[string]any{
				"name": map[string]any{"type": "string"},
				"item": map[string]any{"type": "reference", "kind": pkg + "/item", "onDelete": "cascade"},
			}}),
	}); err != nil {
		t.Fatal(err)
	}
	put := func(in substrate.PutInput) *substrate.Record {
		t.Helper()
		rec, err := d.Put(ctx, substrate.ActorPerson, in)
		if err != nil {
			t.Fatal(err)
		}
		return rec
	}
	const boxed = "erasable-the-box"
	box := put(substrate.PutInput{Kind: pkg + "/box", Properties: map[string]any{"name": boxed}})
	boxRef := map[string]any{"ref": vocabulary.RecordPath(box.Kind, box.ID)}
	// Each item written twice, two properties each time.
	item := func(tag string) *substrate.Record {
		t.Helper()
		rec := put(substrate.PutInput{Kind: pkg + "/item", Properties: map[string]any{
			"name": "kept-" + tag + "-first-name", "note": "kept-" + tag + "-first-note", "box": boxRef,
		}})
		if _, err := d.Patch(ctx, substrate.ActorPerson, rec.Kind, rec.ID, substrate.PatchInput{Properties: map[string]any{
			"name": "kept-" + tag + "-second-name", "note": "kept-" + tag + "-second-note",
		}}); err != nil {
			t.Fatal(err)
		}
		return rec
	}
	live, gone := item("live"), item("gone")
	part := put(substrate.PutInput{Kind: pkg + "/part", Properties: map[string]any{
		"name": "kept-the-part", "item": map[string]any{"ref": vocabulary.RecordPath(live.Kind, live.ID)},
	}})
	if _, err := d.Delete(ctx, substrate.ActorPerson, gone.Kind, gone.ID, substrate.DeleteInput{}); err != nil {
		t.Fatal(err)
	}
	ids := []string{live.ID, gone.ID, part.ID}
	history := func() map[int64]string {
		t.Helper()
		rows, err := d.db.QueryContext(ctx, `SELECT seq, row_to_json(c)::text FROM changelog c WHERE record_id = ANY($1)`, ids)
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
	before := history()
	keptAll := func(when string) {
		t.Helper()
		now := history()
		for seq, row := range before {
			if now[seq] != row {
				t.Fatalf("%s, the line %d of what the box owned is %q, want it as it was: %q", when, seq, now[seq], row)
			}
		}
		var intents int
		if err := d.db.QueryRowContext(ctx, `
			SELECT (SELECT count(*) FROM compaction_erasures WHERE record_id = ANY($1))
			     + (SELECT count(*) FROM changelog, jsonb_array_elements(payload -> 'fold') e
			        WHERE e ->> 'kind' = 'erase' AND e ->> 'id' = ANY($1))`, ids).Scan(&intents); err != nil || intents != 0 {
			t.Fatalf("%s, %d intents name what the box owned (%v), want none", when, intents, err)
		}
	}
	again := func() {
		t.Helper()
		fresh, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: box.Kind, ID: box.ID, Properties: map[string]any{"name": "a box again"}})
		if err != nil || fresh.Version != 1 {
			t.Fatalf("a put at the deleted id answered %+v (%v), want a fresh record at version 1", fresh, err)
		}
	}

	deleted, err := d.Delete(ctx, substrate.ActorPerson, box.Kind, box.ID, substrate.DeleteInput{Purge: true})
	if err != nil {
		t.Fatalf("permanent delete: %v", err)
	}
	if deleted.Erasure == nil || deleted.Erasure.Pending || len(deleted.Erasure.Purged) != 0 || deleted.Erasure.Tombstoned != 1 {
		t.Fatalf("the delete's erasure is %+v, want it done, nothing purged beside the box, the live item tombstoned", deleted.Erasure)
	}
	keptAll("after the delete")
	for _, r := range []*substrate.Record{live, gone} {
		row, err := d.loadRowDB(ctx, eref{Kind: r.Kind, ID: r.ID})
		if err != nil || row == nil || row.DeletedAt == nil {
			t.Fatalf("the item %s is %+v (%v) after the delete, want it tombstoned for the sweep", r.ID, row, err)
		}
	}
	if row, err := d.loadRowDB(ctx, eref{Kind: part.Kind, ID: part.ID}); err != nil || row == nil || row.DeletedAt != nil {
		t.Fatalf("the part is %+v (%v) after the delete, want it live until the sweep collects its item", row, err)
	}
	if putBeforeSweep {
		again()
	}
	if _, err := d.RunGC(ctx); err != nil {
		t.Fatal(err)
	}
	for _, r := range []*substrate.Record{live, gone} {
		if row, err := d.loadRowDB(ctx, eref{Kind: r.Kind, ID: r.ID}); err != nil || row != nil {
			t.Fatalf("the sweep left the item %s as %+v (%v), want it collected", r.ID, row, err)
		}
	}
	if row, err := d.loadRowDB(ctx, eref{Kind: part.Kind, ID: part.ID}); err != nil || (row != nil && row.DeletedAt == nil) {
		t.Fatalf("the part is %+v (%v) after the sweep collected its item, want it tombstoned or collected", row, err)
	}
	keptAll("after the sweep")
	if !putBeforeSweep {
		again()
	}
	if err := d.ArchivePass(ctx); err != nil {
		t.Fatal(err)
	}
	if left := valuesLeft(t, d, boxed); len(left) > 0 {
		t.Fatalf("values of the box remain:\n%s", strings.Join(left, "\n"))
	}
}

// A permanent delete is refused whole (409, nothing purged, no intent
// written) by a finalizer on any record it would take with the record: a
// record merged into it, beside the merge's own hold; the merge's own
// record; a split's; a change request that targets it. The merge's own hold
// on its loser is the one it takes past: that loser goes with the winner.
func TestAPermanentDeleteIsRefusedWholeByAHoldOnARecordItWouldTake(t *testing.T) {
	t.Parallel()
	const hold = "holds.example.substrate.reamde.dev/holds/teardown"
	for _, c := range []struct {
		name    string
		refused bool
		shape   func(t *testing.T, d *dataset, winner *substrate.Record, put func(substrate.PutInput) *substrate.Record) eref
	}{
		{"a loser held beside the merge's own hold", true, func(t *testing.T, d *dataset, winner *substrate.Record, put func(substrate.PutInput) *substrate.Record) eref {
			loser := put(substrate.PutInput{Kind: winner.Kind, Properties: map[string]any{"name": "a loser"}})
			addHold(t, d, eref{Kind: loser.Kind, ID: loser.ID}, hold)
			mergeInto(t, d, winner, loser)
			return eref{Kind: loser.Kind, ID: loser.ID}
		}},
		{"the merge's own record held", true, func(t *testing.T, d *dataset, winner *substrate.Record, put func(substrate.PutInput) *substrate.Record) eref {
			loser := put(substrate.PutInput{Kind: winner.Kind, Properties: map[string]any{"name": "a loser"}})
			merge := mergeInto(t, d, winner, loser)
			addHold(t, d, merge, hold)
			return merge
		}},
		{"a split's own record held", true, func(t *testing.T, d *dataset, winner *substrate.Record, put func(substrate.PutInput) *substrate.Record) eref {
			loser := put(substrate.PutInput{Kind: winner.Kind, Properties: map[string]any{"name": "a loser"}})
			merge := mergeInto(t, d, winner, loser)
			split, err := d.Split(context.Background(), substrate.ActorPerson, substrate.SplitInput{Merge: merge.ID})
			if err != nil {
				t.Fatalf("split: %v", err)
			}
			addHold(t, d, eref{Kind: split.Kind, ID: split.ID}, hold)
			return eref{Kind: split.Kind, ID: split.ID}
		}},
		{"a change request held", true, func(t *testing.T, d *dataset, winner *substrate.Record, put func(substrate.PutInput) *substrate.Record) eref {
			req := put(substrate.PutInput{Kind: kindPatchRequest, Properties: map[string]any{
				"diff": map[string]any{"properties": map[string]any{"name": "renamed"}}, "target": vocabulary.RecordPath(winner.Kind, winner.ID),
			}})
			addHold(t, d, eref{Kind: req.Kind, ID: req.ID}, hold)
			return eref{Kind: req.Kind, ID: req.ID}
		}},
		{"a loser held by the merge alone", false, func(t *testing.T, d *dataset, winner *substrate.Record, put func(substrate.PutInput) *substrate.Record) eref {
			loser := put(substrate.PutInput{Kind: winner.Kind, Properties: map[string]any{"name": "a loser"}})
			mergeInto(t, d, winner, loser)
			return eref{Kind: loser.Kind, ID: loser.ID}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			d := openInternalDataset(t, WithArchiveInterval(time.Hour))
			const pkg = "holds.example.substrate.reamde.dev/holds"
			if _, err := d.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
				vocabulary.PackageManifest(pkg, 0),
				vocabulary.KindManifest(pkg, map[string]any{"singular": "box"},
					map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
			}); err != nil {
				t.Fatal(err)
			}
			put := func(in substrate.PutInput) *substrate.Record {
				t.Helper()
				rec, err := d.Put(ctx, substrate.ActorPerson, in)
				if err != nil {
					t.Fatal(err)
				}
				return rec
			}
			winner := put(substrate.PutInput{Kind: pkg + "/box", Properties: map[string]any{"name": "the winner"}})
			other := c.shape(t, d, winner, put)
			before := mustGetInternal(t, d, winner.Kind, winner.ID)
			head := headOf(t, d)
			_, err := d.Delete(ctx, substrate.ActorPerson, winner.Kind, winner.ID, substrate.DeleteInput{Purge: true})
			if !c.refused {
				if err != nil {
					t.Fatalf("the permanent delete of a winner whose loser the merge alone holds: %v", err)
				}
				if row, err := d.loadRowDB(ctx, other); err != nil || row != nil {
					t.Fatalf("the loser is %+v (%v), want it gone with the winner", row, err)
				}
				return
			}
			if !errors.Is(err, substrate.ErrConflict) || !strings.Contains(err.Error(), other.ID) {
				t.Fatalf("the permanent delete answered %v, want a conflict naming %s", err, other.ID)
			}
			if got := mustGetInternal(t, d, winner.Kind, winner.ID); got.DeletedAt != nil || got.Version != before.Version {
				t.Fatalf("the refused delete changed the winner: %+v", got)
			}
			if row, err := d.loadRowDB(ctx, other); err != nil || row == nil {
				t.Fatalf("the held record is %+v (%v) after the refusal, want it there", row, err)
			}
			var intents int
			if err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM compaction_erasures`).Scan(&intents); err != nil || intents != 0 {
				t.Fatalf("%d intents after the refusal (%v), want none", intents, err)
			}
			if again := headOf(t, d); again != head {
				t.Fatalf("the refused delete wrote %d lines, want none", again-head)
			}
		})
	}
}

// addHold adds a finalizer to a record as the engine's own hand, which a
// substrate-managed record (a merge's, a split's) takes.
func addHold(t *testing.T, d *dataset, ref eref, hold string) {
	t.Helper()
	if err := d.inTx(context.Background(), substrate.ActorSystem, true, func(tx *txn) error {
		_, err := tx.patch(ref, substrate.PatchInput{AddFinalizers: []string{hold}})
		return err
	}); err != nil {
		t.Fatalf("hold %s %s: %v", ref.Kind, ref.ID, err)
	}
}

// mergeInto merges loser into winner and returns the merge's own record.
func mergeInto(t *testing.T, d *dataset, winner, loser *substrate.Record) eref {
	t.Helper()
	merge, err := d.Merge(context.Background(), substrate.ActorPerson, substrate.MergeInput{Kind: winner.Kind, Winner: winner.ID, Loser: loser.ID})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	return eref{Kind: merge.Kind, ID: merge.ID}
}

// headOf is the repository's last seq.
func headOf(t *testing.T, d *dataset) int64 {
	t.Helper()
	var head int64
	if err := d.db.QueryRowContext(context.Background(), `SELECT coalesce(max(seq), 0) FROM changelog`).Scan(&head); err != nil {
		t.Fatal(err)
	}
	return head
}

// An intent stays until its erasure and no reader sees it: a permanent
// delete of a record a change request targets, stopped between its
// transactions, leaves its last line, the request's, with an `erase` effect
// for each, one of them naming a record the line is not addressed to; a whole-history compaction keeps
// that line and the rows; the change the line projects and the function
// envelope built from it are byte for byte what they are with the effects
// taken off; and the next pass finishes the erasures.
func TestAnIntentStaysUntilItsErasureAndNoReaderSeesIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var stop atomic.Bool
	d := openInternalDataset(t, WithArchiveInterval(time.Hour), WithCompactionInterval(-1),
		WithTestCompactionFault(func(stage string) error {
			if stage == erasureBetween && stop.Load() {
				return errors.New("stopped between the delete and its erasure")
			}
			return nil
		}))
	const pkg = "intent.example.substrate.reamde.dev/intent"
	if _, err := d.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "box"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "item"},
			map[string]any{"properties": map[string]any{
				"name": map[string]any{"type": "string"},
				"box":  map[string]any{"type": "reference", "kind": pkg + "/box", "onDelete": "cascade"},
			}}),
	}); err != nil {
		t.Fatal(err)
	}
	rec, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: pkg + "/box", Properties: map[string]any{"name": "a box"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: kindPatchRequest, Properties: map[string]any{
		"diff": map[string]any{"properties": map[string]any{"name": "a box, renamed"}}, "target": vocabulary.RecordPath(rec.Kind, rec.ID),
	}}); err != nil {
		t.Fatal(err)
	}
	stop.Store(true)
	if _, err := d.Delete(ctx, substrate.ActorPerson, rec.Kind, rec.ID, substrate.DeleteInput{Purge: true}); err == nil {
		t.Fatal("the delete was not stopped between its transactions: the test proves nothing")
	}
	stop.Store(false)
	var line int64
	if err := d.db.QueryRowContext(ctx, `SELECT max(line) FROM compaction_erasures`).Scan(&line); err != nil {
		t.Fatalf("no intent is pending: %v", err)
	}
	intents := func() int {
		t.Helper()
		var n int
		if err := d.db.QueryRowContext(ctx, `
			SELECT count(*) FROM changelog, jsonb_array_elements(payload->'fold') e
			WHERE seq = $1 AND e->>'kind' = 'erase'`, line).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	var foreign int
	if err := d.db.QueryRowContext(ctx, `
		SELECT count(*) FROM changelog, jsonb_array_elements(payload->'fold') e
		WHERE seq = $1 AND e->>'kind' = 'erase' AND e->>'id' <> record_id`, line).Scan(&foreign); err != nil || foreign == 0 {
		t.Fatalf("the line carries no intent for a record it is not addressed to (%v): the test proves nothing", err)
	}
	if _, err := d.svc.CompactRepository(ctx, testdb.Repository(t), 0); err != nil {
		t.Fatal(err)
	}
	var pending int
	if err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM compaction_erasures`).Scan(&pending); err != nil || pending == 0 || intents() == 0 {
		t.Fatalf("a whole-history compaction took the intent: %d rows pending (%v), %d effects on its line", pending, err, intents())
	}
	stripped := func(ch substrate.Change) substrate.Change {
		effects, _ := ch.Payload[foldPayloadKey].([]any)
		var kept []any
		for _, e := range effects {
			if op, _ := e.(map[string]any); op["kind"] != string(foldErase) {
				kept = append(kept, e)
			}
		}
		payload := maps.Clone(ch.Payload)
		payload[foldPayloadKey] = kept
		ch.Payload = payload
		return ch
	}
	read := func(ch substrate.Change) (string, string) {
		t.Helper()
		env, err := d.deliveryEnvelope(ctx, ch)
		if err != nil {
			t.Fatal(err)
		}
		projected := ch
		projected.Payload = maps.Clone(ch.Payload)
		projectAffected(&projected)
		e, _ := json.Marshal(env)
		p, _ := json.Marshal(projected)
		return string(e), string(p)
	}
	ch, err := d.changeAt(ctx, line)
	if err != nil {
		t.Fatal(err)
	}
	withEnv, withChange := read(ch)
	withoutEnv, withoutChange := read(stripped(ch))
	if withEnv != withoutEnv {
		t.Fatalf("the envelope reads the intent:\n%s\n%s", withEnv, withoutEnv)
	}
	if withChange != withoutChange {
		t.Fatalf("the change reads the intent:\n%s\n%s", withChange, withoutChange)
	}
	report, err := d.CompactionPass(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM compaction_erasures`).Scan(&pending); err != nil || pending != 0 || report.Erased < 2 {
		t.Fatalf("the pass finished %d erasures and left %d (%v), want the box's and the request's", report.Erased, pending, err)
	}
}

// A permanent delete leaves no sealed value of the record: its rows in
// `sealed` go, and so do the archive's objects of them after its next pass,
// so neither the ref the record held nor its hex is anywhere.
func TestAPermanentDeleteLeavesNoSealedValueOfTheRecord(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := openInternalDataset(t, WithArchiveInterval(time.Hour))
	const pkg = "sealed.example.substrate.reamde.dev/sealed"
	if _, err := d.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "vault"},
			map[string]any{"properties": map[string]any{
				"label": map[string]any{"type": "string"},
				"token": map[string]any{"type": "secret"},
			}}),
	}); err != nil {
		t.Fatal(err)
	}
	rec, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: pkg + "/vault", Properties: map[string]any{
		"label": "a vault", "token": "erasable-sealed-secret",
	}})
	if err != nil {
		t.Fatal(err)
	}
	var ref string
	if err := d.db.QueryRowContext(ctx, `SELECT ref FROM sealed WHERE record_id = $1`, rec.ID).Scan(&ref); err != nil {
		t.Fatalf("the vault holds no sealed value: %v", err)
	}
	if err := d.ArchivePass(ctx); err != nil {
		t.Fatal(err)
	}
	traces := []string{ref, hex.EncodeToString([]byte(ref))}
	if left := valuesLeft(t, d, traces...); len(left) == 0 {
		t.Fatal("the sealed value's ref is nowhere before the delete: the test proves nothing")
	}
	if _, err := d.Delete(ctx, substrate.ActorPerson, rec.Kind, rec.ID, substrate.DeleteInput{Purge: true}); err != nil {
		t.Fatalf("permanent delete: %v", err)
	}
	if err := d.ArchivePass(ctx); err != nil {
		t.Fatal(err)
	}
	if left := valuesLeft(t, d, traces...); len(left) > 0 {
		t.Fatalf("after the permanent delete and an archive pass, the sealed value remains:\n%s", strings.Join(left, "\n"))
	}
}

// An enabled record trigger that has not been delivered the deletion and is
// delivering holds a permanent delete's erasure, and the reply names it:
// once it is delivered the deletion the pass finishes the erasure, and a
// forget with finish finishes it at once, past the trigger.
func TestAnErasureWaitsForADeliveringTrigger(t *testing.T) {
	t.Parallel()
	for _, end := range []string{"delivered", "finished now"} {
		t.Run(end, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			d := openInternalDataset(t, WithArchiveInterval(time.Hour), WithCompactionInterval(-1))
			installLedgerConnector(t, d, nowUTC().Add(-time.Hour))
			const value = "erasable-behind-a-trigger"
			w, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerWidget, Properties: map[string]any{"name": value}})
			if err != nil {
				t.Fatal(err)
			}
			stand := func(seq int64) {
				t.Helper()
				if err := d.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
					if err := tx.setCursorTx(ledgerMirror, seq); err != nil {
						return err
					}
					return tx.settleDelivery(ledgerMirror)
				}); err != nil {
					t.Fatal(err)
				}
			}
			head, err := tableChangelogHead(ctx, d.db)
			if err != nil {
				t.Fatal(err)
			}
			stand(head)
			deleted, err := d.Delete(ctx, substrate.ActorPerson, ledgerWidget, w.ID, substrate.DeleteInput{Purge: true})
			if err != nil {
				t.Fatalf("permanent delete: %v", err)
			}
			if e := deleted.Erasure; e == nil || !e.Pending || !slices.ContainsFunc(e.Waits, func(w substrate.ErasureWait) bool { return w.Trigger == ledgerMirror }) {
				t.Fatalf("the delete's erasure is %+v, want it waiting for trigger %s", e, ledgerMirror)
			}
			if left := valuesLeft(t, d, value); len(left) == 0 {
				t.Fatal("the held erasure erased the history all the same")
			}
			switch end {
			case "delivered":
				head, err := tableChangelogHead(ctx, d.db)
				if err != nil {
					t.Fatal(err)
				}
				stand(head)
				report, err := d.CompactionPass(ctx)
				if err != nil || report.Erased != 1 {
					t.Fatalf("the pass after the delivery finished %d erasures (%v), want the widget's", report.Erased, err)
				}
			case "finished now":
				e, err := d.Forget(ctx, substrate.ActorPerson, ledgerWidget, w.ID, substrate.ForgetInput{Finish: true})
				if err != nil || e.Pending {
					t.Fatalf("a forget with finish: %+v (%v), want the erasure done", e, err)
				}
			}
			if left := valuesLeft(t, d, value); len(left) > 0 {
				t.Fatalf("after the erasure, values remain:\n%s", strings.Join(left, "\n"))
			}
		})
	}
}

// An erasure is whole or held: a delete of a box a change request targets,
// stopped between its transactions, leaves the box's intent on the
// request's later gc line, above the box's own history. That line damaged, the next pass
// holds the box's erasure on it before it edits anything: every row is as
// it was, the intents stay, and the line is noted.
func TestAnErasureHeldOnADamagedIntentLineEditsNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var stop atomic.Bool
	d := openInternalDataset(t, WithArchiveInterval(time.Hour), WithCompactionInterval(-1),
		WithTestCompactionFault(func(stage string) error {
			if stage == erasureBetween && stop.Load() {
				return errors.New("stopped between the delete and its erasure")
			}
			return nil
		}))
	const pkg = "damagedintent.example.substrate.reamde.dev/damagedintent"
	if _, err := d.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "box"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "item"},
			map[string]any{"properties": map[string]any{
				"name": map[string]any{"type": "string"},
				"box":  map[string]any{"type": "reference", "kind": pkg + "/box", "onDelete": "cascade"},
			}}),
	}); err != nil {
		t.Fatal(err)
	}
	box, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: pkg + "/box", Properties: map[string]any{"name": "erasable-damaged-box"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: kindPatchRequest, Properties: map[string]any{
		"diff": map[string]any{"properties": map[string]any{"name": "a box, renamed"}}, "target": vocabulary.RecordPath(box.Kind, box.ID),
	}}); err != nil {
		t.Fatal(err)
	}
	stop.Store(true)
	if _, err := d.Delete(ctx, substrate.ActorPerson, box.Kind, box.ID, substrate.DeleteInput{Purge: true}); err == nil {
		t.Fatal("the delete was not stopped between its transactions: the test proves nothing")
	}
	stop.Store(false)
	var upTo, line int64
	if err := d.db.QueryRowContext(ctx, `SELECT upto, line FROM compaction_erasures WHERE record_id = $1`, box.ID).Scan(&upTo, &line); err != nil {
		t.Fatalf("no intent for the box: %v", err)
	}
	if line <= upTo {
		t.Fatalf("the box's intent is on seq %d, at or below its history's end %d: the test proves nothing", line, upTo)
	}
	if _, err := d.db.ExecContext(ctx, `UPDATE changelog SET hash = sha256(hash) WHERE seq = $1`, line); err != nil {
		t.Fatal(err)
	}
	rows := func() map[int64]string {
		t.Helper()
		got, err := d.db.QueryContext(ctx, `SELECT seq, row_to_json(c)::text FROM changelog c`)
		if err != nil {
			t.Fatal(err)
		}
		out := map[int64]string{}
		for got.Next() {
			var seq int64
			var row string
			if err := got.Scan(&seq, &row); err != nil {
				t.Fatal(err)
			}
			out[seq] = row
		}
		if err := errors.Join(got.Err(), got.Close()); err != nil {
			t.Fatal(err)
		}
		return out
	}
	before := rows()
	if _, err := d.CompactionPass(ctx); err != nil {
		t.Fatal(err)
	}
	if after := rows(); !maps.Equal(after, before) {
		t.Fatalf("the held erasure changed the history: %d rows, %d before", len(after), len(before))
	}
	var pending int
	if err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM compaction_erasures`).Scan(&pending); err != nil || pending < 2 {
		t.Fatalf("%d intents pending (%v), want the box's and the request's kept", pending, err)
	}
	if st, err := d.svc.CompactionStatus(ctx, testdb.Repository(t)); err != nil || !slices.Contains(st.Damaged, line) {
		t.Fatalf("the notes are %v (%v), want seq %d among them", st.Damaged, err, line)
	}
}

// A merge and a split line carry copies of the pair's values beside their
// fold (`moved`: the labels and annotations moved, the winner's annotations
// the loser's overwrote). Where such a line outlives the erasure, because
// the other record of the pair lives on, the copies of the erased one go:
// a box merged into another, split back out, and the other permanently
// deleted leaves no value of the deleted box.
func TestAPermanentDeleteStripsTheMergeCopiesOfItsValues(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := openInternalDataset(t, WithArchiveInterval(time.Hour))
	const pkg = "mergecopy.example.substrate.reamde.dev/mergecopy"
	if _, err := d.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "box"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
	}); err != nil {
		t.Fatal(err)
	}
	const overwritten = "erasable-overwritten-by-the-merge"
	winner, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{
		Kind: pkg + "/box", Properties: map[string]any{"name": "the deleted box"},
		Annotations: map[string]any{"owner/note": map[string]any{"why": overwritten}},
	})
	if err != nil {
		t.Fatal(err)
	}
	loser, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{
		Kind: pkg + "/box", Properties: map[string]any{"name": "the box that lives on"},
		Annotations: map[string]any{"owner/note": map[string]any{"why": "the box that lives on, its note"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	merge, err := d.Merge(ctx, substrate.ActorPerson, substrate.MergeInput{Kind: winner.Kind, Winner: winner.ID, Loser: loser.ID})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if _, err := d.Split(ctx, substrate.ActorPerson, substrate.SplitInput{Merge: merge.ID}); err != nil {
		t.Fatalf("split: %v", err)
	}
	var copies int
	if err := d.db.QueryRowContext(ctx, `
		SELECT count(*) FROM changelog WHERE op IN ('merge', 'split') AND (payload->'moved')::text LIKE '%' || $1 || '%'`, overwritten).Scan(&copies); err != nil || copies == 0 {
		t.Fatalf("no merge or split line carries the overwritten note (%v): the test proves nothing", err)
	}
	if _, err := d.Delete(ctx, substrate.ActorPerson, winner.Kind, winner.ID, substrate.DeleteInput{Purge: true}); err != nil {
		t.Fatalf("permanent delete: %v", err)
	}
	if err := d.ArchivePass(ctx); err != nil {
		t.Fatal(err)
	}
	if left := valuesLeft(t, d, overwritten); len(left) > 0 {
		t.Fatalf("after the permanent delete and an archive pass, the deleted box's note remains:\n%s", strings.Join(left, "\n"))
	}
	if got, err := d.Get(ctx, loser.Kind, loser.ID); err != nil || got.DeletedAt != nil {
		t.Fatalf("the box that lives on is %+v (%v), want it live", got, err)
	}
}
