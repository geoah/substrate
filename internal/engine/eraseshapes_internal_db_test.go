package engine

// ONE PATH (decision 0305): a record a closure's batch reaches is erased
// exactly as a record deleted directly is. Each shape below is built twice in
// one repository, once under a holder kept and once under a holder deleted:
// the first record is permanently deleted itself, the second is reached by
// its holder's batch. Either way no value of it is left anywhere, and every
// table holds as many rows naming each record of the shape, the holder's own
// rows aside.

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	shapesPackage = "shapes.connectors.substrate.reamde.dev/shapes"
	shapesHolder  = shapesPackage + "/holder"
	shapesThing   = shapesPackage + "/thing"
	shapesPiece   = shapesPackage + "/piece"
	shapesContact = shapesPackage + "/contact"
)

// installShapes registers a holder that owns things and contacts, a piece a
// thing owns, and a contact mapped onto the shipped person by its email.
func installShapes(t *testing.T, d *dataset) {
	t.Helper()
	holder := map[string]any{"type": "reference", "kind": shapesHolder, "onDelete": "cascade"}
	if err := enginetest.Install(context.Background(), d, substrate.ActorSystem, enginetest.Manifest{
		Name: "shapes", Authority: shapesPackage,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(shapesPackage, 1),
			vocabulary.KindManifest(shapesPackage, map[string]any{"singular": "holder"},
				map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
			vocabulary.KindManifest(shapesPackage, map[string]any{"singular": "thing"},
				map[string]any{"properties": map[string]any{
					"name":   map[string]any{"type": "string"},
					"token":  map[string]any{"type": "secret"},
					"holder": holder,
				}}),
			vocabulary.KindManifest(shapesPackage, map[string]any{"singular": "piece"},
				map[string]any{"properties": map[string]any{
					"name":  map[string]any{"type": "string"},
					"thing": map[string]any{"type": "reference", "kind": shapesThing, "onDelete": "cascade"},
				}}),
			vocabulary.KindManifest(shapesPackage, map[string]any{"singular": "contact"},
				map[string]any{"properties": map[string]any{
					"email":  map[string]any{"type": "email"},
					"name":   map[string]any{"type": "string"},
					"holder": holder,
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

// eraseShape builds one shape under holder, its values marked with mark,
// and returns the record to erase, every value the shape put anywhere, and
// the records of the shape by their role.
type eraseShape struct {
	name  string
	build func(t *testing.T, d *dataset, holder *substrate.Record, mark string) (*substrate.Record, []string, map[string]string)
}

func shapePut(t *testing.T, d *dataset, in substrate.PutInput) *substrate.Record {
	t.Helper()
	rec, err := d.Put(context.Background(), substrate.ActorPerson, in)
	if err != nil {
		t.Fatalf("put %s: %v", in.Kind, err)
	}
	return rec
}

func refTo(r *substrate.Record) map[string]any {
	return map[string]any{"ref": vocabulary.RecordPath(r.Kind, r.ID)}
}

var eraseShapes = []eraseShape{
	{"a source of a mapping", func(t *testing.T, d *dataset, holder *substrate.Record, mark string) (*substrate.Record, []string, map[string]string) {
		// The person's own email, which stays: no value of the contact.
		mail := strings.TrimPrefix(mark, "erasable-") + "@example.com"
		person := shapePut(t, d, substrate.PutInput{Kind: subjPerson, Properties: map[string]any{"emails": []any{mail}}})
		contact := shapePut(t, d, substrate.PutInput{Kind: shapesContact, Properties: map[string]any{
			"email": mail, "name": mark, "holder": refTo(holder),
		}})
		var name *string
		if err := d.db.QueryRow(`SELECT props->>'name' FROM records WHERE kind = $1 AND id = $2`, person.Kind, person.ID).Scan(&name); err != nil || name == nil || *name != mark {
			t.Fatalf("the contact did not give the person its name (%v): the shape proves nothing", err)
		}
		return contact, []string{mark}, map[string]string{"source": contact.ID, "target": person.ID}
	}},
	{"a merge winner whose loser a child still names", func(t *testing.T, d *dataset, holder *substrate.Record, mark string) (*substrate.Record, []string, map[string]string) {
		winner := shapePut(t, d, substrate.PutInput{Kind: shapesThing, Properties: map[string]any{"name": "a winner", "holder": refTo(holder)}})
		loser := shapePut(t, d, substrate.PutInput{Kind: shapesThing, Properties: map[string]any{"name": "a loser"}})
		piece := shapePut(t, d, substrate.PutInput{Kind: shapesPiece, Properties: map[string]any{"name": mark, "thing": refTo(loser)}})
		if _, err := d.Merge(context.Background(), substrate.ActorPerson, substrate.MergeInput{Kind: shapesThing, Winner: winner.ID, Loser: loser.ID}); err != nil {
			t.Fatalf("merge: %v", err)
		}
		return winner, []string{mark}, map[string]string{"winner": winner.ID, "loser": loser.ID, "piece": piece.ID}
	}},
	{"a record whose merge and split records hold its moved values", func(t *testing.T, d *dataset, holder *substrate.Record, mark string) (*substrate.Record, []string, map[string]string) {
		ctx := context.Background()
		thing := shapePut(t, d, substrate.PutInput{
			Kind: shapesThing, Properties: map[string]any{"name": "a thing", "holder": refTo(holder)},
			Annotations: map[string]any{"owner/note": map[string]any{"why": mark}},
		})
		other := shapePut(t, d, substrate.PutInput{
			Kind: shapesThing, Properties: map[string]any{"name": "another"},
			Annotations: map[string]any{"owner/note": map[string]any{"why": "another's note"}},
		})
		merge, err := d.Merge(ctx, substrate.ActorPerson, substrate.MergeInput{Kind: shapesThing, Winner: thing.ID, Loser: other.ID})
		if err != nil {
			t.Fatalf("merge: %v", err)
		}
		split, err := d.Split(ctx, substrate.ActorPerson, substrate.SplitInput{Merge: merge.ID})
		if err != nil {
			t.Fatalf("split: %v", err)
		}
		return thing, []string{mark}, map[string]string{"thing": thing.ID, "other": other.ID, "merge": merge.ID, "split": split.ID}
	}},
	{"a record a change request targets", func(t *testing.T, d *dataset, holder *substrate.Record, mark string) (*substrate.Record, []string, map[string]string) {
		thing := shapePut(t, d, substrate.PutInput{Kind: shapesThing, Properties: map[string]any{"name": "a thing", "holder": refTo(holder)}})
		req := shapePut(t, d, substrate.PutInput{Kind: kindPatchRequest, Properties: map[string]any{
			"diff": map[string]any{"properties": map[string]any{"name": mark}}, "target": vocabulary.RecordPath(thing.Kind, thing.ID),
		}})
		return thing, []string{mark}, map[string]string{"thing": thing.ID, "request": req.ID}
	}},
	{"a sealed value", func(t *testing.T, d *dataset, holder *substrate.Record, mark string) (*substrate.Record, []string, map[string]string) {
		thing := shapePut(t, d, substrate.PutInput{Kind: shapesThing, Properties: map[string]any{"name": "a thing", "token": mark, "holder": refTo(holder)}})
		var ref string
		if err := d.db.QueryRow(`SELECT ref FROM sealed WHERE record_id = $1`, thing.ID).Scan(&ref); err != nil {
			t.Fatalf("the thing holds no sealed value: %v", err)
		}
		return thing, []string{ref, hex.EncodeToString([]byte(ref))}, map[string]string{"thing": thing.ID}
	}},
	{"a stored Idempotency-Key answer", func(t *testing.T, d *dataset, holder *substrate.Record, mark string) (*substrate.Record, []string, map[string]string) {
		thing := shapePut(t, d, substrate.PutInput{Kind: shapesThing, Properties: map[string]any{"name": "a thing", "holder": refTo(holder)}})
		keyed := substrate.WithIdempotencyKey(context.Background(), "key-"+strings.TrimPrefix(mark, "erasable-"))
		if _, err := d.Patch(keyed, substrate.ActorPerson, thing.Kind, thing.ID, substrate.PatchInput{Properties: map[string]any{"name": mark}}); err != nil {
			t.Fatal(err)
		}
		return thing, []string{mark}, map[string]string{"thing": thing.ID}
	}},
	{"a child deleted before", func(t *testing.T, d *dataset, holder *substrate.Record, mark string) (*substrate.Record, []string, map[string]string) {
		thing := shapePut(t, d, substrate.PutInput{Kind: shapesThing, Properties: map[string]any{"name": mark, "holder": refTo(holder)}})
		if _, err := d.Delete(context.Background(), substrate.ActorPerson, thing.Kind, thing.ID, substrate.DeleteInput{}); err != nil {
			t.Fatal(err)
		}
		return thing, []string{mark}, map[string]string{"thing": thing.ID}
	}},
}

func TestEveryShapeIsErasedTheSameDirectlyAndByABatch(t *testing.T) {
	t.Parallel()
	for _, shape := range eraseShapes {
		t.Run(shape.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			d := openInternalDataset(t, WithArchiveInterval(time.Hour), WithCompactionInterval(-1), WithTestErasureBatch(1))
			installShapes(t, d)
			kept := shapePut(t, d, substrate.PutInput{Kind: shapesHolder, Properties: map[string]any{"name": "the holder kept"}})
			deleted := shapePut(t, d, substrate.PutInput{Kind: shapesHolder, Properties: map[string]any{"name": "the holder deleted"}})
			direct, directValues, directRoles := shape.build(t, d, kept, "erasable-direct")
			_, batchValues, batchRoles := shape.build(t, d, deleted, "erasable-batch")
			values := append(slices.Clone(directValues), batchValues...)
			if err := d.ArchivePass(ctx); err != nil {
				t.Fatal(err)
			}
			if left := valuesLeft(t, d, values...); len(left) == 0 {
				t.Fatal("no value of the shape is anywhere before the deletes: the test proves nothing")
			}
			if _, err := d.Delete(ctx, substrate.ActorPerson, direct.Kind, direct.ID, substrate.DeleteInput{Purge: true}); err != nil {
				t.Fatalf("the permanent delete of the record: %v", err)
			}
			if _, err := d.Delete(ctx, substrate.ActorPerson, deleted.Kind, deleted.ID, substrate.DeleteInput{Purge: true}); err != nil {
				t.Fatalf("the permanent delete of its holder: %v", err)
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
				if pass == 9 {
					t.Fatalf("%d erasures still pending after ten passes", pending)
				}
			}
			if err := d.ArchivePass(ctx); err != nil {
				t.Fatal(err)
			}
			if left := valuesLeft(t, d, values...); len(left) > 0 {
				t.Fatalf("values remain:\n%s", strings.Join(left, "\n"))
			}
			for role, id := range directRoles {
				got, want := footprint(t, d, batchRoles[role]), footprint(t, d, id)
				if diff := diffFootprints(want, got); diff != "" {
					t.Errorf("the %s reached by a batch left other rows than the one deleted directly:\n%s", role, diff)
				}
			}
		})
	}
}

// footprint is, by table, how many rows of the repository's schema name id.
func footprint(t *testing.T, d *dataset, id string) map[string]int {
	t.Helper()
	ctx := context.Background()
	rows, err := d.db.QueryContext(ctx, `
		SELECT table_name FROM information_schema.tables
		WHERE table_schema = current_schema() AND table_type = 'BASE TABLE' ORDER BY table_name`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, n)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}
	out := map[string]int{}
	for _, table := range tables {
		var n int
		if err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM `+table+` x WHERE x::text LIKE '%' || $1 || '%'`, id).Scan(&n); err != nil {
			t.Fatalf("scan %s: %v", table, err)
		}
		if n > 0 {
			out[table] = n
		}
	}
	return out
}

func diffFootprints(want, got map[string]int) string {
	var out []string
	for table, n := range want {
		if got[table] != n {
			out = append(out, fmt.Sprintf("%s: %d rows, %d directly", table, got[table], n))
		}
	}
	for table, n := range got {
		if _, ok := want[table]; !ok {
			out = append(out, fmt.Sprintf("%s: %d rows, none directly", table, n))
		}
	}
	slices.Sort(out)
	return strings.Join(out, "\n")
}
