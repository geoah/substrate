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
// root that still holds one of the values: a changelog line, a row of any
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
	_ = filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		for _, v := range values {
			if strings.Contains(string(raw), v) || strings.Contains(filepath.Base(path), v) {
				rel, _ := filepath.Rel(root, path)
				out = append(out, "file "+rel+" holds "+v)
			}
		}
		return nil
	})
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

// A permanent delete reaches what the record owns, its whole ownership chain
// included, the change requests that target it and the records merged into
// it, the labels and annotations a merge line carries of the pair among them
// (decision 0305): none of their values is left in the table, the key store,
// the data root or, after the pass and an archive pass, the archive, the
// reply names the records the request purged, and a rebuild gives the same
// fold.
func TestAPermanentDeleteReachesWhatTheRecordOwnsAndWhatAsksToChangeIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := openInternalDataset(t, WithArchiveInterval(time.Hour))
	const pkg = "owned.example.substrate.reamde.dev/owned"
	if _, err := d.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "box"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "item"},
			map[string]any{"properties": map[string]any{
				"name": map[string]any{"type": "string"},
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
	const boxed, owned, deeper, merged, asked = "erasable-box", "erasable-owned-item", "erasable-owned-part", "erasable-merged-box", "erasable-change-request"
	const label, loserNote, winnerNote = "erasable-moved-label", "erasable-moved-note", "erasable-overwritten-note"
	box := put(substrate.PutInput{
		Kind: pkg + "/box", Properties: map[string]any{"name": boxed},
		Annotations: map[string]any{"owner/note": map[string]any{"why": winnerNote}},
	})
	item := put(substrate.PutInput{Kind: pkg + "/item", Properties: map[string]any{
		"name": owned, "box": map[string]any{"ref": vocabulary.RecordPath(box.Kind, box.ID)},
	}})
	part := put(substrate.PutInput{Kind: pkg + "/part", Properties: map[string]any{
		"name": deeper, "item": map[string]any{"ref": vocabulary.RecordPath(item.Kind, item.ID)},
	}})
	loser := put(substrate.PutInput{
		Kind: pkg + "/box", Properties: map[string]any{"name": merged},
		Labels:      map[string]any{"owner/kept": label},
		Annotations: map[string]any{"owner/note": map[string]any{"why": loserNote}},
	})
	if _, err := d.Merge(ctx, substrate.ActorPerson, substrate.MergeInput{Kind: box.Kind, Winner: box.ID, Loser: loser.ID}); err != nil {
		t.Fatalf("merge: %v", err)
	}
	put(substrate.PutInput{Kind: kindPatchRequest, Properties: map[string]any{
		"diff":   map[string]any{"properties": map[string]any{"name": asked}},
		"target": vocabulary.RecordPath(box.Kind, box.ID),
	}})
	put(substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": "a bystander"}})
	if err := d.ArchivePass(ctx); err != nil {
		t.Fatal(err)
	}
	values := []string{boxed, owned, deeper, merged, asked, label, loserNote, winnerNote}
	for _, v := range values {
		if left := valuesLeft(t, d, v); len(left) == 0 {
			t.Fatalf("%q is nowhere before the delete: the test proves nothing", v)
		}
	}
	deleted, err := d.Delete(ctx, substrate.ActorPerson, box.Kind, box.ID, substrate.DeleteInput{Purge: true})
	if err != nil {
		t.Fatalf("permanent delete: %v", err)
	}
	for _, r := range []*substrate.Record{item, loser} {
		if !slices.Contains(deleted.Erasure.Purged, vocabulary.RecordPath(r.Kind, r.ID)) {
			t.Fatalf("the delete's reply names %v as purged, want %s among them", deleted.Erasure.Purged, r.ID)
		}
	}
	// The part is what the item owns: a later batch, the pass's, takes it.
	if _, err := d.CompactionPass(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Get(ctx, part.Kind, part.ID); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("after the pass the part still reads: %v", err)
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

// boxesPackage is a box that owns items, each of which owns parts, through
// `onDelete: cascade`: a closure two levels deep.
const boxesPackage = "batches.example.substrate.reamde.dev/batches"

// declareBoxes declares boxesPackage and returns its name.
func declareBoxes(t *testing.T, d *dataset) string {
	t.Helper()
	pkg := boxesPackage
	if _, err := d.ApplyVocabularyDocuments(context.Background(), substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "box"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "item"},
			map[string]any{"properties": map[string]any{
				"name": map[string]any{"type": "string"},
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
	return pkg
}

// A permanent delete is bounded by the record, not by what it owns: a box
// that owns more items than a batch, each owning parts, is erased in the
// request with one batch of its items, and the reply says that at least so
// many remain and that the pass erases them. The rest go in the pass's
// batches, each its own transaction, a write committing between two of
// them while records are still to go. An owned item written meanwhile, and
// a new item that names the purged box while its intent stands, go with
// the rest; the lines of what went are the engine's, acting for the hand
// the delete acted for; and nothing of any of it is left.
func TestAPermanentDeleteTakesWhatItOwnsInBatches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var d *dataset
	var batchesDone atomic.Int32
	var wroteBetween atomic.Bool
	d = openInternalDataset(t, WithArchiveInterval(time.Hour), WithCompactionInterval(-1), WithTestErasureBatch(10),
		WithTestCompactionFault(func(stage string) error {
			// The second batch is the pass's first: a write between it and
			// the next must not wait on it.
			if stage != erasureBatchDone || batchesDone.Add(1) != 2 {
				return nil
			}
			wctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if _, err := d.Put(wctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": "written between two batches"}}); err != nil {
				return err
			}
			wroteBetween.Store(true)
			return nil
		}))
	pkg := declareBoxes(t, d)
	put := func(in substrate.PutInput) *substrate.Record {
		t.Helper()
		rec, err := d.Put(ctx, substrate.ActorPerson, in)
		if err != nil {
			t.Fatal(err)
		}
		return rec
	}
	box := put(substrate.PutInput{Kind: pkg + "/box", Properties: map[string]any{"name": "erasable-box-of-many"}})
	boxRef := map[string]any{"ref": vocabulary.RecordPath(box.Kind, box.ID)}
	var values []string
	var items []*substrate.Record
	for i := range 15 {
		name := fmt.Sprintf("erasable-item-%02d", i)
		item := put(substrate.PutInput{Kind: pkg + "/item", Properties: map[string]any{"name": name, "box": boxRef}})
		items, values = append(items, item), append(values, name)
		for j := range 2 {
			part := fmt.Sprintf("erasable-part-%02d-%d", i, j)
			put(substrate.PutInput{Kind: pkg + "/part", Properties: map[string]any{
				"name": part, "item": map[string]any{"ref": vocabulary.RecordPath(item.Kind, item.ID)},
			}})
			values = append(values, part)
		}
	}
	if err := d.ArchivePass(ctx); err != nil {
		t.Fatal(err)
	}
	asked := substrate.WithOnBehalfOf(ctx, substrate.ActorPerson)
	deleted, err := d.Delete(asked, substrate.ActorPerson, box.Kind, box.ID, substrate.DeleteInput{Purge: true})
	if err != nil {
		t.Fatalf("permanent delete: %v", err)
	}
	e := deleted.Erasure
	if e == nil || !e.Pending || e.Owned == nil || e.Owned.Batch != 10 || e.Owned.Remaining < 5 || len(e.Purged) != 10 ||
		!strings.Contains(e.Held, "batches of 10") {
		t.Fatalf("the delete's erasure is %+v (owned %+v), want it pending with ten items purged and at least five owned to go", e, e.Owned)
	}
	var live []*substrate.Record
	for _, item := range items {
		got, err := d.Get(ctx, item.Kind, item.ID)
		switch {
		case errors.Is(err, substrate.ErrNotFound):
		case err != nil:
			t.Fatal(err)
		default:
			live = append(live, got)
		}
	}
	if len(live) != 5 {
		t.Fatalf("%d items read after the request, want the five its batch did not reach, as they were", len(live))
	}
	// Meanwhile: an owned item its batch has not reached is written, and a
	// new item names the purged box.
	const meanwhile, after = "erasable-written-meanwhile", "erasable-new-while-the-intent-stands"
	if _, err := d.Patch(ctx, substrate.ActorPerson, live[0].Kind, live[0].ID, substrate.PatchInput{Properties: map[string]any{"name": meanwhile}}); err != nil {
		t.Fatal(err)
	}
	put(substrate.PutInput{Kind: pkg + "/item", Properties: map[string]any{"name": after, "box": boxRef}})
	values = append(values, meanwhile, after)
	st, err := d.svc.CompactionStatus(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	if i := slices.IndexFunc(st.Erasures, func(w WaitingErasure) bool { return w.ID == box.ID }); i < 0 ||
		st.Erasures[i].Owned == nil || st.Erasures[i].Owned.Purged != 10 || st.Erasures[i].Owned.Remaining < 6 {
		t.Fatalf("the status shows %+v, want the box's closure with ten gone and at least six to go", st.Erasures)
	}
	for pass := 0; ; pass++ {
		if _, err := d.CompactionPass(ctx); err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
		var pending int
		if err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM compaction_erasures`).Scan(&pending); err != nil {
			t.Fatal(err)
		}
		if pending == 0 {
			break
		}
		if pass == 5 {
			t.Fatalf("%d erasures still pending after six passes", pending)
		}
	}
	if !wroteBetween.Load() {
		t.Fatal("no write ran between two batches: the test proves nothing about the lock")
	}
	var boxActor, boxFor string
	if err := d.db.QueryRowContext(ctx, `SELECT actor, on_behalf_of FROM changelog WHERE record_id = $1 AND op = 'gc'`, box.ID).Scan(&boxActor, &boxFor); err != nil {
		t.Fatal(err)
	}
	// The hand the delete acted for: the one its line names, or, where it
	// acted for itself, its own.
	asker := boxFor
	if asker == "" {
		asker = boxActor
	}
	rows, err := d.db.QueryContext(ctx, `SELECT DISTINCT actor, on_behalf_of FROM changelog WHERE op = 'gc' AND kind = ANY($1)`, []string{pkg + "/item", pkg + "/part"})
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var actor, onBehalfOf string
		if err := rows.Scan(&actor, &onBehalfOf); err != nil {
			t.Fatal(err)
		}
		if actor == boxActor || onBehalfOf != asker {
			t.Errorf("an owned record's gc line is by %s for %s, want the engine's for %s, whom the delete acted for", actor, onBehalfOf, asker)
		}
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}
	if err := d.ArchivePass(ctx); err != nil {
		t.Fatal(err)
	}
	if left := valuesLeft(t, d, values...); len(left) > 0 {
		t.Fatalf("after the passes and an archive pass, values remain:\n%s", strings.Join(left, "\n"))
	}
	want := FoldRows(t, d)
	if _, err := d.svc.RebuildRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatal(err)
	}
	if diff := DiffFoldTables(want, FoldRows(t, d)); len(diff) > 0 {
		t.Fatalf("the erased history replays into another fold:\n%s", strings.Join(diff, "\n"))
	}
}

// A create at an id whose permanent delete is still taking what the record
// owned is refused until the closure is done, at every level: the batches
// find what it owned by the id, so the new record would lose what it owns to
// them. It is refused as ErrErasing, which an engine caller may retry, and
// the message names the deleted record and at least how many it still owns,
// counted no further than a batch and one. Once the passes finish, the id
// takes a record again, and what that one owns stays.
func TestACreateAtAnIdWhoseClosureStandsWaitsForIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := openInternalDataset(t, WithArchiveInterval(time.Hour), WithCompactionInterval(-1), WithTestErasureBatch(2))
	pkg := declareBoxes(t, d)
	put := func(in substrate.PutInput) (*substrate.Record, error) {
		return d.Put(ctx, substrate.ActorPerson, in)
	}
	must := func(rec *substrate.Record, err error) *substrate.Record {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return rec
	}
	box := must(put(substrate.PutInput{Kind: pkg + "/box", Properties: map[string]any{"name": "a box of five"}}))
	ref := func(r *substrate.Record) map[string]any {
		return map[string]any{"ref": vocabulary.RecordPath(r.Kind, r.ID)}
	}
	for i := range 6 {
		item := must(put(substrate.PutInput{Kind: pkg + "/item", Properties: map[string]any{"name": fmt.Sprintf("item %d", i), "box": ref(box)}}))
		must(put(substrate.PutInput{Kind: pkg + "/part", Properties: map[string]any{"name": "a part", "item": ref(item)}}))
	}
	deleted, err := d.Delete(ctx, substrate.ActorPerson, box.Kind, box.ID, substrate.DeleteInput{Purge: true})
	if err != nil {
		t.Fatal(err)
	}
	// Four items are left, and the count stops at a batch and one.
	if deleted.Erasure == nil || !deleted.Erasure.Pending || len(deleted.Erasure.Purged) != 2 ||
		deleted.Erasure.Owned == nil || deleted.Erasure.Owned.Remaining != 3 {
		t.Fatalf("the delete's erasure is %+v, want two items purged and at least three to go", deleted.Erasure)
	}
	refused := func(kind, id string, owns int) {
		t.Helper()
		_, err := put(substrate.PutInput{Kind: kind, ID: id, Properties: map[string]any{"name": "again"}})
		if !errors.Is(err, substrate.ErrErasing) || !errors.Is(err, substrate.ErrUnavailable) ||
			!strings.Contains(err.Error(), vocabulary.RecordPath(kind, id)) || !strings.Contains(err.Error(), fmt.Sprintf("at least %d of what it owned", owns)) {
			t.Fatalf("a create at %s %s answered %v, want it refused as still being erased, naming it and the %d it owns", kind, id, err, owns)
		}
	}
	refused(box.Kind, box.ID, 3)
	// One level down: an item the request's batch purged owns its part,
	// which a later batch takes.
	purgedItem := deleted.Erasure.Purged[0][strings.LastIndex(deleted.Erasure.Purged[0], "/")+1:]
	refused(pkg+"/item", purgedItem, 1)
	// A record with no closure behind its id is created as ever.
	must(put(substrate.PutInput{Kind: pkg + "/box", Properties: map[string]any{"name": "another box"}}))
	for pass := 0; ; pass++ {
		if _, err := d.CompactionPass(ctx); err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
		var owned bool
		if err := d.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM compaction_erasures WHERE owned)`).Scan(&owned); err != nil {
			t.Fatal(err)
		}
		if !owned {
			break
		}
		if pass == 5 {
			t.Fatal("a closure still stands after six passes")
		}
	}
	again := must(put(substrate.PutInput{Kind: box.Kind, ID: box.ID, Properties: map[string]any{"name": "the box again"}}))
	item := must(put(substrate.PutInput{Kind: pkg + "/item", ID: purgedItem, Properties: map[string]any{"name": "an item again", "box": ref(again)}}))
	if _, err := d.CompactionPass(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Get(ctx, item.Kind, item.ID); err != nil {
		t.Fatalf("what the new box owns did not stay: %v", err)
	}
}

// The sweep never collects a record a standing closure still has to erase:
// it would write a plain `collected` line and no intent, out of the
// closure's reach, and the record's history would keep its values. A box
// owns two items, the second deleted before; the request's batch takes the
// first, a sweep runs between the batches, and the pass's batch takes the
// second and erases it.
func TestTheSweepLeavesARecordAClosureHasStillToErase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := openInternalDataset(t, WithArchiveInterval(time.Hour), WithCompactionInterval(-1), WithTestErasureBatch(1))
	pkg := declareBoxes(t, d)
	put := func(in substrate.PutInput) *substrate.Record {
		t.Helper()
		rec, err := d.Put(ctx, substrate.ActorPerson, in)
		if err != nil {
			t.Fatal(err)
		}
		return rec
	}
	box := put(substrate.PutInput{Kind: pkg + "/box", Properties: map[string]any{"name": "a box"}})
	boxRef := map[string]any{"ref": vocabulary.RecordPath(box.Kind, box.ID)}
	put(substrate.PutInput{Kind: pkg + "/item", Properties: map[string]any{"name": "the first item", "box": boxRef}})
	const name = "erasable-the-second-item"
	second := put(substrate.PutInput{Kind: pkg + "/item", Properties: map[string]any{"name": name, "box": boxRef}})
	if _, err := d.Delete(ctx, substrate.ActorPerson, second.Kind, second.ID, substrate.DeleteInput{}); err != nil {
		t.Fatal(err)
	}
	deleted, err := d.Delete(ctx, substrate.ActorPerson, box.Kind, box.ID, substrate.DeleteInput{Purge: true})
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Erasure == nil || !deleted.Erasure.Pending || slices.Contains(deleted.Erasure.Purged, vocabulary.RecordPath(second.Kind, second.ID)) {
		t.Fatalf("the delete's erasure is %+v, want the second item left to the pass", deleted.Erasure)
	}
	if _, err := d.RunGC(ctx); err != nil {
		t.Fatal(err)
	}
	var tombstone int
	if err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM records WHERE id = $1 AND deleted_at IS NOT NULL`, second.ID).Scan(&tombstone); err != nil || tombstone != 1 {
		t.Fatalf("the sweep collected the item the closure has still to erase (%v)", err)
	}
	for pass := 0; ; pass++ {
		if _, err := d.CompactionPass(ctx); err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
		var pending int
		if err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM compaction_erasures`).Scan(&pending); err != nil {
			t.Fatal(err)
		}
		if pending == 0 {
			break
		}
		if pass == 5 {
			t.Fatalf("%d erasures still pending after six passes", pending)
		}
	}
	if err := d.ArchivePass(ctx); err != nil {
		t.Fatal(err)
	}
	if left := valuesLeft(t, d, name); len(left) > 0 {
		t.Fatalf("values of the item the sweep met remain:\n%s", strings.Join(left, "\n"))
	}
}

// The transaction that takes a closure's intent off its line holds above a
// line known not to verify, as every erasure does, when the line is noted
// after the closure began: a box owns two items, a batch a transaction; a
// bystander's line below the box's history is damaged and noted once the
// pass's batch has taken the second item; and the look that would take the
// box's intent off its line keeps it, and the line, as they were.
func TestTheLastTransactionOfAClosureHoldsAboveAKnownCut(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var d *dataset
	var batches atomic.Int32
	var bystander int64
	d = openInternalDataset(t, WithArchiveInterval(time.Hour), WithCompactionInterval(-1), WithTestErasureBatch(1),
		WithTestCompactionFault(func(stage string) error {
			// The second batch is the pass's.
			if stage != erasureBatchDone || batches.Add(1) != 2 {
				return nil
			}
			if _, err := d.db.ExecContext(ctx, `UPDATE changelog SET hash = sha256(hash) WHERE seq = $1`, bystander); err != nil {
				return err
			}
			_, err := d.db.ExecContext(ctx, `INSERT INTO changelog_damage (seq, noted_by) VALUES ($1, 'test')`, bystander)
			return err
		}))
	pkg := declareBoxes(t, d)
	put := func(in substrate.PutInput) *substrate.Record {
		t.Helper()
		rec, err := d.Put(ctx, substrate.ActorPerson, in)
		if err != nil {
			t.Fatal(err)
		}
		return rec
	}
	by := put(substrate.PutInput{Kind: pkg + "/box", Properties: map[string]any{"name": "a bystander"}})
	if err := d.db.QueryRowContext(ctx, `SELECT max(seq) FROM changelog WHERE record_id = $1`, by.ID).Scan(&bystander); err != nil {
		t.Fatal(err)
	}
	box := put(substrate.PutInput{Kind: pkg + "/box", Properties: map[string]any{"name": "a box"}})
	for _, name := range []string{"the first item", "the second item"} {
		put(substrate.PutInput{Kind: pkg + "/item", Properties: map[string]any{
			"name": name, "box": map[string]any{"ref": vocabulary.RecordPath(box.Kind, box.ID)},
		}})
	}
	if _, err := d.Delete(ctx, substrate.ActorPerson, box.Kind, box.ID, substrate.DeleteInput{Purge: true}); err != nil {
		t.Fatal(err)
	}
	var line int64
	if err := d.db.QueryRowContext(ctx, `SELECT line FROM compaction_erasures WHERE record_id = $1`, box.ID).Scan(&line); err != nil {
		t.Fatalf("no intent for the box: %v", err)
	}
	var carried string
	if err := d.db.QueryRowContext(ctx, `SELECT payload::text FROM changelog WHERE seq = $1`, line).Scan(&carried); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CompactionPass(ctx); err != nil {
		t.Fatal(err)
	}
	if batches.Load() < 2 {
		t.Fatal("the pass ran no batch: the test proves nothing")
	}
	var items int
	if err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM records WHERE kind = $1`, pkg+"/item").Scan(&items); err != nil || items != 0 {
		t.Fatalf("%d items left (%v): the closure's batches did not finish, so the test proves nothing", items, err)
	}
	var now string
	if err := d.db.QueryRowContext(ctx, `SELECT payload::text FROM changelog WHERE seq = $1`, line).Scan(&now); err != nil {
		t.Fatal(err)
	}
	var pending int
	if err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM compaction_erasures WHERE record_id = $1`, box.ID).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending == 0 || now != carried {
		t.Fatalf("the box's intent is gone or its line %d changed above the noted line %d", line, bystander)
	}
}
