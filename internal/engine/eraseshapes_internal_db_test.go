package engine

// EVERY PLACE A RECORD'S VALUE LIVES (decisions 0304 and 0305): each shape
// below puts a value of one record somewhere besides its own lines, and a
// permanent delete of the record leaves none of it in any table, the stored
// Idempotency-Key answers or any file under the data root, the archive
// included, once the next archive pass has published.

import (
	"context"
	"encoding/hex"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
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
