package engine

// EVERY PLACE A RECORD'S VALUE LIVES (decisions 0304 and 0305): each shape
// below puts a value of one record somewhere besides its own lines, and a
// permanent delete of the record leaves none of it in any table, the stored
// Idempotency-Key answers or any file under the data root, the archive
// included, once the next archive pass has published.

import (
	"context"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	shapesPackage = "shapes.connectors.substrate.reamde.dev/shapes"
	shapesThing   = shapesPackage + "/thing"
	shapesContact = shapesPackage + "/contact"
)

// installShapes registers a thing that holds a secret, and a contact mapped
// onto the shipped person by its email.
func installShapes(t *testing.T, d *dataset) {
	t.Helper()
	if err := enginetest.Install(context.Background(), d, substrate.ActorSystem, enginetest.Manifest{
		Name: "shapes", Authority: shapesPackage,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(shapesPackage, 1),
			vocabulary.KindManifest(shapesPackage, map[string]any{"singular": "thing"},
				map[string]any{"properties": map[string]any{
					"name":  map[string]any{"type": "string"},
					"token": map[string]any{"type": "secret"},
				}}),
			vocabulary.KindManifest(shapesPackage, map[string]any{"singular": "contact"},
				map[string]any{"properties": map[string]any{
					"email": map[string]any{"type": "email"},
					"name":  map[string]any{"type": "string"},
					"person": map[string]any{
						"type": "reference", "kind": subjPerson,
						"required": true, "mustExist": true, "subject": true,
					},
				}}),
		},
	}); err != nil {
		t.Fatalf("register the shapes: %v", err)
	}
	if err := enginetest.DeclareMappings(context.Background(), d,
		enginetest.PeopleMapping("shapesperson", map[string]any{
			"from": shapesContact, "property": "person",
			"match": []any{map[string]any{"from": "email", "to": "emails"}},
			"map": map[string]any{
				"name":   map[string]any{"path": "name"},
				"emails": map[string]any{"path": "email", "merge": "union"},
			},
		})); err != nil {
		t.Fatalf("declare the contact mapping: %v", err)
	}
}

// valueShape opens a repository, builds one shape in it and returns the
// record to delete permanently and every value the shape put anywhere.
type valueShape struct {
	name  string
	open  func(t *testing.T) *dataset
	build func(t *testing.T, d *dataset) (eref, []string)
}

func shapePut(t *testing.T, d *dataset, in substrate.PutInput) *substrate.Record {
	t.Helper()
	rec, err := d.Put(context.Background(), substrate.ActorPerson, in)
	if err != nil {
		t.Fatalf("put %s: %v", in.Kind, err)
	}
	return rec
}

func shapesDataset(t *testing.T) *dataset {
	d := openInternalDataset(t, WithArchiveInterval(time.Hour))
	installShapes(t, d)
	return d
}

func shapeRef(r *substrate.Record) eref { return eref{Kind: r.Kind, ID: r.ID} }

var valueShapes = []valueShape{
	{"the source of a mapping", shapesDataset, func(t *testing.T, d *dataset) (eref, []string) {
		const name = "erasable-mapped-name"
		person := shapePut(t, d, substrate.PutInput{Kind: subjPerson, Properties: map[string]any{"emails": []any{"ada@example.com"}}})
		contact := shapePut(t, d, substrate.PutInput{Kind: shapesContact, Properties: map[string]any{"email": "ada@example.com", "name": name}})
		var got *string
		if err := d.db.QueryRow(`SELECT props->>'name' FROM records WHERE kind = $1 AND id = $2`, person.Kind, person.ID).Scan(&got); err != nil || got == nil || *got != name {
			t.Fatalf("the contact did not give the person its name (%v): the shape proves nothing", err)
		}
		// The scan asks the search index too (valuesLeft): here it holds the
		// name, in the contact's row and the person's copy.
		var indexed int
		if err := d.db.QueryRow(`SELECT count(*) FROM records WHERE fts @@ plainto_tsquery('english', $1)`, name).Scan(&indexed); err != nil || indexed == 0 {
			t.Fatalf("the search index holds the name in %d rows (%v): the scan's question proves nothing", indexed, err)
		}
		return shapeRef(contact), []string{name}
	}},
	{"a merge winner and its loser", shapesDataset, func(t *testing.T, d *dataset) (eref, []string) {
		winner := shapePut(t, d, substrate.PutInput{Kind: shapesThing, Properties: map[string]any{"name": "erasable-the-winner"}})
		loser := shapePut(t, d, substrate.PutInput{Kind: shapesThing, Properties: map[string]any{"name": "erasable-the-loser"}})
		if _, err := d.Merge(context.Background(), substrate.ActorPerson, substrate.MergeInput{Kind: shapesThing, Winner: winner.ID, Loser: loser.ID}); err != nil {
			t.Fatalf("merge: %v", err)
		}
		return shapeRef(winner), []string{"erasable-the-winner", "erasable-the-loser"}
	}},
	{"a record whose merge and split records hold its moved values", shapesDataset, func(t *testing.T, d *dataset) (eref, []string) {
		const note = "erasable-overwritten-note"
		ctx := context.Background()
		thing := shapePut(t, d, substrate.PutInput{
			Kind: shapesThing, Properties: map[string]any{"name": "a thing"},
			Annotations: map[string]any{"owner/note": map[string]any{"why": note}},
		})
		other := shapePut(t, d, substrate.PutInput{
			Kind: shapesThing, Properties: map[string]any{"name": "another"},
			Annotations: map[string]any{"owner/note": map[string]any{"why": "another's note"}},
		})
		merge, err := d.Merge(ctx, substrate.ActorPerson, substrate.MergeInput{Kind: shapesThing, Winner: thing.ID, Loser: other.ID})
		if err != nil {
			t.Fatalf("merge: %v", err)
		}
		if _, err := d.Split(ctx, substrate.ActorPerson, substrate.SplitInput{Merge: merge.ID}); err != nil {
			t.Fatalf("split: %v", err)
		}
		return shapeRef(thing), []string{note}
	}},
	{"a change request's target", shapesDataset, func(t *testing.T, d *dataset) (eref, []string) {
		const asked = "erasable-asked-name"
		thing := shapePut(t, d, substrate.PutInput{Kind: shapesThing, Properties: map[string]any{"name": "a thing"}})
		shapePut(t, d, substrate.PutInput{Kind: kindPatchRequest, Properties: map[string]any{
			"diff": map[string]any{"properties": map[string]any{"name": asked}}, "target": vocabulary.RecordPath(thing.Kind, thing.ID),
		}})
		return shapeRef(thing), []string{asked}
	}},
	{"a change request on a record merged into it", shapesDataset, func(t *testing.T, d *dataset) (eref, []string) {
		const asked = "erasable-asked-of-the-loser"
		winner := shapePut(t, d, substrate.PutInput{Kind: shapesThing, Properties: map[string]any{"name": "the winner"}})
		loser := shapePut(t, d, substrate.PutInput{Kind: shapesThing, Properties: map[string]any{"name": "the loser"}})
		request := shapePut(t, d, substrate.PutInput{Kind: kindPatchRequest, Properties: map[string]any{
			"diff": map[string]any{"properties": map[string]any{"name": asked}}, "target": vocabulary.RecordPath(loser.Kind, loser.ID),
		}})
		if _, err := d.Merge(context.Background(), substrate.ActorPerson, substrate.MergeInput{Kind: shapesThing, Winner: winner.ID, Loser: loser.ID}); err != nil {
			t.Fatalf("merge: %v", err)
		}
		var target string
		if err := d.db.QueryRow(`SELECT dst FROM refs WHERE src_kind = $1 AND src = $2 AND dst_kind = $3`, request.Kind, request.ID, shapesThing).Scan(&target); err != nil || target != loser.ID {
			t.Fatalf("the request points at %q (%v), want the loser %s: the shape proves nothing", target, err, loser.ID)
		}
		return shapeRef(winner), []string{asked}
	}},
	{"a sealed value", shapesDataset, func(t *testing.T, d *dataset) (eref, []string) {
		thing := shapePut(t, d, substrate.PutInput{Kind: shapesThing, Properties: map[string]any{"name": "a thing", "token": "erasable-sealed"}})
		var ref string
		if err := d.db.QueryRow(`SELECT ref FROM sealed WHERE record_id = $1`, thing.ID).Scan(&ref); err != nil {
			t.Fatalf("the thing holds no sealed value: %v", err)
		}
		return shapeRef(thing), []string{ref, hex.EncodeToString([]byte(ref))}
	}},
	{"a stored Idempotency-Key answer", shapesDataset, func(t *testing.T, d *dataset) (eref, []string) {
		const name = "erasable-keyed-name"
		thing := shapePut(t, d, substrate.PutInput{Kind: shapesThing, Properties: map[string]any{"name": "a thing"}})
		keyed := substrate.WithIdempotencyKey(context.Background(), "a-keyed-patch")
		if _, err := d.Patch(keyed, substrate.ActorPerson, thing.Kind, thing.ID, substrate.PatchInput{Properties: map[string]any{"name": name}}); err != nil {
			t.Fatal(err)
		}
		return shapeRef(thing), []string{name}
	}},
	{"an embedding and a queued one", shapesDataset, func(t *testing.T, d *dataset) (eref, []string) {
		const hash = "erasable-embedded-text"
		thing := shapePut(t, d, substrate.PutInput{Kind: shapesThing, Properties: map[string]any{"name": "a thing"}})
		for _, q := range []string{
			`INSERT INTO embeddings (record_kind, record_id, property, chunk, text_hash) VALUES ($1, $2, 'name', 0, '` + hash + `')`,
			`INSERT INTO embed_queue (record_kind, record_id, property) VALUES ($1, $2, '` + hash + `')`,
		} {
			if _, err := d.db.Exec(q, thing.Kind, thing.ID); err != nil {
				t.Fatal(err)
			}
		}
		return shapeRef(thing), []string{hash}
	}},
	{"an observed point", func(t *testing.T) *dataset { return openInternalDataset(t, WithArchiveInterval(time.Hour)) }, func(t *testing.T, d *dataset) (eref, []string) {
		const value = "61.7531"
		if err := enginetest.PutShippedUnits(context.Background(), d, "1/min"); err != nil {
			t.Fatal(err)
		}
		metric := vocabulary.PackageObservations + "/metric"
		shapePut(t, d, substrate.PutInput{Kind: metric, ID: fid("mine/pulse"), Properties: map[string]any{
			"name": "Pulse", "unit": map[string]any{"ref": vocabulary.KindUnit + "/1/min"}, "form": "gauge",
		}})
		obs := shapePut(t, d, substrate.PutInput{Kind: vocabulary.PackageObservations + "/observation", ID: fid("pulse/1"), Properties: map[string]any{
			"metric": map[string]any{"ref": metric + "/" + fid("mine/pulse")}, "form": "gauge", "hold": "PT0S",
			"value": map[string]any{"value": value, "unit": "/min"}, "at": "2026-10-07T06:41:12Z",
		}})
		var points int
		if err := d.db.QueryRow(`SELECT count(*) FROM observed_segments WHERE record_id = $1`, obs.ID).Scan(&points); err != nil || points == 0 {
			t.Fatalf("the observation is in no observed segment (%v): the shape proves nothing", err)
		}
		return shapeRef(obs), []string{value}
	}},
	{"a parked failure and a drain on its lines", func(t *testing.T) *dataset {
		return supersedeDatasetOn(t, openInternalDataset(t, WithArchiveInterval(time.Hour)), nowUTC().Add(time.Hour), "create")
	}, func(t *testing.T, d *dataset) (eref, []string) {
		const name, cursor = "erasable-parked-job", "erasable-drain-cursor"
		ctx := context.Background()
		job := shapePut(t, d, substrate.PutInput{Kind: supersedeJob, ID: fid("parked"), Properties: map[string]any{"name": name}})
		processOnce(t, d)
		failures, err := d.TriggerFailures(ctx, supersedeRec)
		if err != nil || len(failures) != 1 {
			t.Fatalf("%s parked %+v (%v), want the job's delivery", supersedeRec, failures, err)
		}
		seq := failures[0].Seq
		if err := d.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
			id, err := tx.reserveSeq()
			if err != nil {
				return err
			}
			owner := pagedOwner{triggerID: supersedeRec, kind: pagedKindRecord, identity: strconv.FormatInt(seq, 10)}
			if err := tx.pageTx(owner, d.recordChainKey(supersedeRec, seq), map[string]any{"after": cursor}, 1, 1, 1, 1, tx.now); err != nil {
				return err
			}
			return tx.appendDeliveryAt(supersedeRec, id)
		}); err != nil {
			t.Fatalf("plant the drain: %v", err)
		}
		return shapeRef(job), []string{name, cursor}
	}},
	{"a drain that saved its cursor before it parked", func(t *testing.T) *dataset {
		return supersedeDatasetOn(t, openInternalDataset(t, WithArchiveInterval(time.Hour)), nowUTC().Add(time.Hour), "create")
	}, checkpointedDrain(false)},
	{"a drain whose saved cursor its trigger's ledger state keeps", func(t *testing.T) *dataset {
		return supersedeDatasetOn(t, openInternalDataset(t, WithArchiveInterval(time.Hour)), nowUTC().Add(time.Hour), "create")
	}, checkpointedDrain(true)},
}

// checkpointedDrain parks a drain of a job's delivery after a page saved
// its cursor, so the park re-states the chain's row cursor and all on a
// line of the trigger's; compacted, the trigger's delivery lines are then
// replaced by a ledger state that keeps the row, cursor whole.
func checkpointedDrain(compacted bool) func(t *testing.T, d *dataset) (eref, []string) {
	return func(t *testing.T, d *dataset) (eref, []string) {
		name, cursor := "erasable-checkpointed-job", "erasable-checkpointed-cursor"
		if compacted {
			name, cursor = "erasable-ledgered-job", "erasable-ledgered-cursor"
		}
		ctx := context.Background()
		job := shapePut(t, d, substrate.PutInput{Kind: supersedeJob, ID: fid("checkpointed"), Properties: map[string]any{"name": name}})
		processOnce(t, d)
		failures, err := d.TriggerFailures(ctx, supersedeRec)
		if err != nil || len(failures) != 1 {
			t.Fatalf("%s parked %+v (%v), want the job's delivery", supersedeRec, failures, err)
		}
		f := failures[0]
		owner := pagedOwner{triggerID: supersedeRec, kind: pagedKindRecord, identity: strconv.FormatInt(f.Seq, 10)}
		for _, step := range []func(tx *txn) error{
			func(tx *txn) error {
				return tx.pageTx(owner, d.recordChainKey(supersedeRec, f.Seq), map[string]any{"after": cursor}, 1, 1, 1, 1, tx.now)
			},
			func(tx *txn) error {
				return tx.parkTx(supersedeRec, foldFailure{
					ID: foldInt(f.ID), Seq: foldInt(f.Seq), RecordID: f.RecordID,
					Attempts: foldInt(f.Attempts + 1), LastError: f.LastError, ParkedAt: tx.now,
				})
			},
		} {
			if err := d.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
				id, err := tx.reserveSeq()
				if err != nil {
					return err
				}
				if err := step(tx); err != nil {
					return err
				}
				return tx.appendDeliveryAt(supersedeRec, id)
			}); err != nil {
				t.Fatalf("plant the drain: %v", err)
			}
		}
		where := "page"
		if compacted {
			where = "ledger"
			head, err := d.Head(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := d.withCompactionTx(ctx, true, func(tx *txn) error {
				_, err := tx.compactRecords(head.Seq, []eref{{Kind: typeTrigger, ID: supersedeRec}})
				return err
			}); err != nil {
				t.Fatalf("compact the trigger: %v", err)
			}
		}
		var holding []string
		rows, err := d.db.QueryContext(ctx, `SELECT DISTINCT e->>'kind' FROM changelog c, jsonb_array_elements(c.payload->'fold') e
			WHERE c.kind = $1 AND strpos(e::text, $2) > 0`, typeTrigger, cursor)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var kind string
			if err := rows.Scan(&kind); err != nil {
				t.Fatal(err)
			}
			holding = append(holding, kind)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			t.Fatal(err)
		}
		if len(holding) != 1 || holding[0] != where {
			t.Fatalf("the cursor is held by the trigger's %v effects, want its %s alone: the shape proves nothing", holding, where)
		}
		return shapeRef(job), []string{name, cursor}
	}
}

func TestAPermanentDeleteLeavesNoValueOfTheRecordInAnyShape(t *testing.T) {
	t.Parallel()
	for _, shape := range valueShapes {
		t.Run(shape.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			d := shape.open(t)
			ref, values := shape.build(t, d)
			if err := d.ArchivePass(ctx); err != nil {
				t.Fatal(err)
			}
			for _, v := range values {
				if left := valuesLeft(t, d, v); len(left) == 0 {
					t.Fatalf("%q is nowhere before the delete: the shape proves nothing", v)
				}
			}
			deleted, err := d.Delete(ctx, substrate.ActorPerson, ref.Kind, ref.ID, substrate.DeleteInput{Purge: true})
			if err != nil {
				t.Fatalf("permanent delete: %v", err)
			}
			if deleted.Erasure == nil || deleted.Erasure.Pending {
				t.Fatalf("the delete's erasure is %+v, want it done", deleted.Erasure)
			}
			if err := d.ArchivePass(ctx); err != nil {
				t.Fatal(err)
			}
			if left := valuesLeft(t, d, values...); len(left) > 0 {
				t.Fatalf("values remain:\n%s", strings.Join(left, "\n"))
			}
		})
	}
}

// A stripped checkpoint is a line like any other afterwards: the history
// verifies, each line's checksum recomputed from its stored columns and the
// archive's copy held to its own, its transaction still framed, and a
// rebuild folds it into the fold the live tables hold. Once for a `page`
// effect's cursor, once for one in a compacted trigger's ledger state.
func TestAStrippedCheckpointVerifiesAndReplaysIntoTheSameFold(t *testing.T) {
	t.Parallel()
	for _, compacted := range []bool{false, true} {
		name := "a page effect's cursor"
		if compacted {
			name = "a cursor in a ledger state"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			d := supersedeDatasetOn(t, openInternalDataset(t, WithArchiveInterval(time.Hour)), nowUTC().Add(time.Hour), "create")
			ref, values := checkpointedDrain(compacted)(t, d)
			if _, err := d.Delete(ctx, substrate.ActorPerson, ref.Kind, ref.ID, substrate.DeleteInput{Purge: true}); err != nil {
				t.Fatalf("permanent delete: %v", err)
			}
			var digested int
			if err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM changelog WHERE kind = $1 AND strpos(payload::text, 'cursorSha256') > 0 AND strpos(payload::text, $2) = 0`,
				typeTrigger, values[1]).Scan(&digested); err != nil || digested == 0 {
				t.Fatalf("%d of the trigger's lines name a cursor by its digest after the strip (%v)", digested, err)
			}
			if err := d.ArchivePass(ctx); err != nil {
				t.Fatal(err)
			}
			report, err := d.svc.VerifyRepositoryWith(ctx, testdb.Repository(t), VerifyOptions{Recanonicalize: true})
			if err != nil || !report.OK {
				t.Fatalf("verify after the strip: %v, %+v", err, report.Findings)
			}
			// The trigger's cursor is left out, as the engine's own fold
			// snapshot leaves it out (rebuild.go foldSnapshot): the fixture's
			// dispatch moved its scan position past rows that did not match,
			// which is written outside the ledger by design (delivery.go), so
			// a rebuild gives the acknowledged position, before and after
			// the strip alike.
			want := FoldRows(t, d)
			if _, err := d.svc.RebuildRepository(ctx, testdb.Repository(t)); err != nil {
				t.Fatal(err)
			}
			got := FoldRows(t, d)
			delete(want, "trigger_cursors")
			delete(got, "trigger_cursors")
			if diff := DiffFoldTables(want, got); len(diff) > 0 {
				t.Fatalf("the stripped history replays into another fold:\n%s", strings.Join(diff, "\n"))
			}
		})
	}
}
