package engine_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	rsPackage = "ada.example.com/team"
	rsPerson  = rsPackage + "/person"
	rsMember  = rsPackage + "/member"
)

// rsDocs is a package whose member kind points at a person, with the link
// properties given.
func rsDocs(version int64, links map[string]any) []map[string]any {
	return []map[string]any{
		vocabulary.PackageManifest(rsPackage, version),
		vocabulary.KindManifest(rsPackage, map[string]any{"singular": "person"}, map[string]any{
			"properties": map[string]any{"name": map[string]any{"type": "string"}},
		}),
		vocabulary.KindManifest(rsPackage, map[string]any{"singular": "member"}, map[string]any{
			"properties": map[string]any{"person": map[string]any{
				"type": "reference", "kind": rsPerson, "properties": links,
			}},
		}),
	}
}

// link is one link property's declaration.
func link(required bool) map[string]any {
	d := map[string]any{"type": "string"}
	if required {
		d["required"] = true
	}
	return d
}

// The footer's steps for a reference link property named `repository`
// (step 14's BREAKING CHANGE), walked as they are written, for one that is
// required and one that is not, beside a second link property whose data
// must come through. On the binary a person runs before upgrading, the steps
// for before the upgrade run end to end. Against the change, the first of
// them is refused, which is why they are run before it, and the steps for
// after the upgrade run from the shape the older binary left.
func TestTheFootersStepsForALinkPropertyNamedRepository(t *testing.T) {
	t.Parallel()
	for _, required := range []bool{false, true} {
		t.Run(fmt.Sprintf("required %v", required), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			svc, dsn := newService(t)
			repo := testdb.Repository(t)
			if _, err := svc.CreateRepository(ctx, repo); err != nil {
				t.Fatal(err)
			}
			ds, err := svc.Dataset(ctx, repo)
			if err != nil {
				t.Fatal(err)
			}
			_, err = ds.ApplyVocabularyDocuments(ctx, owner, rsDocs(1, map[string]any{"repository": link(required), "note": link(false)}))
			if err != nil {
				if !strings.Contains(err.Error(), `"repository" is the reserved key`) {
					t.Fatalf("declaring the link property: %v, want admitted before the change or the reservation after it", err)
				}
				afterTheUpgrade(t, svc, dsn, repo, required)
				return
			}
			beforeTheUpgrade(t, svc, dsn, repo, required)
		})
	}
}

// beforeTheUpgrade walks the footer's step 4 on a binary that admits the
// link property, from a reference that carries it, read back before and
// after a reopen.
func beforeTheUpgrade(t *testing.T, svc substrate.Service, dsn, repo string, required bool) {
	t.Helper()
	ctx := context.Background()
	ds, err := svc.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	person := mustPut(t, ds, owner, substrate.PutInput{Kind: rsPerson, Properties: map[string]any{"name": "Ada"}})
	ref := rsPerson + "/" + person.ID
	member := mustPut(t, ds, owner, substrate.PutInput{Kind: rsMember, Properties: map[string]any{
		"person": map[string]any{"ref": ref, "repository": "hr", "note": "keep"},
	}})
	wantOldShape(t, ds, member.ID, ref, "written")
	_ = svc.Close()
	svc2 := mustReopen(t, dsn, engine.DataRootOf(svc))
	t.Cleanup(func() { _ = svc2.Close() })
	if ds, err = svc2.Dataset(ctx, repo); err != nil {
		t.Fatal(err)
	}
	wantOldShape(t, ds, member.ID, ref, "reopened")
	if required {
		// A replacement that copies `required` refuses while the stored values
		// lack it, so the steps add it optional.
		_, err := ds.ApplyVocabularyDocuments(ctx, owner, rsDocs(2, map[string]any{"repository": link(true), "origin": link(true), "note": link(false)}))
		if err == nil || !strings.Contains(err.Error(), `adds link property "origin" as required while 1 live references lack it`) {
			t.Fatalf("a required replacement beside a required repository: %v, want it refused while the values lack it", err)
		}
		// What the footer said before: the new name beside `repository`, then
		// the values rewritten without it. A required `repository` refuses
		// the rewrite, so the steps make it optional first.
		if _, err := ds.ApplyVocabularyDocuments(ctx, owner, rsDocs(2, map[string]any{"repository": link(true), "origin": link(false), "note": link(false)})); err != nil {
			t.Fatalf("the new name beside a required repository: %v", err)
		}
		_, err = ds.Patch(ctx, owner, rsMember, member.ID, substrate.PatchInput{Properties: map[string]any{
			"person": map[string]any{"ref": ref, "origin": "hr", "note": "keep"},
		}})
		if err == nil || !strings.Contains(err.Error(), `"repository" requires a value`) {
			t.Fatalf("the rewrite under a required repository: %v, want the refusal the finding names", err)
		}
	}
	// a. `repository` made optional, and the replacement added beside it,
	// optional.
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, rsDocs(3, map[string]any{"repository": link(false), "origin": link(false), "note": link(false)})); err != nil {
		t.Fatalf("step a: %v", err)
	}
	// b. Each reference value written whole: its ref, every other link
	// property copied as it is, and `repository`'s value under the
	// replacement's name, with no `repository`.
	mustPatch(t, ds, owner, rsMember, member.ID, substrate.PatchInput{Properties: map[string]any{
		"person": map[string]any{"ref": ref, "origin": "hr", "note": "keep"},
	}})
	// c. The declaration without `repository`.
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, rsDocs(4, map[string]any{"origin": link(false), "note": link(false)})); err != nil {
		t.Fatalf("step c: %v", err)
	}
	// d. `required` restored, on the replacement, where `repository` had it.
	if required {
		if _, err := ds.ApplyVocabularyDocuments(ctx, owner, rsDocs(5, map[string]any{"origin": link(true), "note": link(false)})); err != nil {
			t.Fatalf("step d: %v", err)
		}
	}
	wantMoved(t, ds, member.ID, ref)
}

// afterTheUpgrade walks the footer's steps 2 and 3 on the change, from the
// shape a binary before it left: the declaration with a `repository` link
// property, and a reference whose value carries it. A set-aside package's
// declaration is applied whole again, so a `required` the new name carries
// is admitted while the stored values still carry the old key.
func afterTheUpgrade(t *testing.T, svc substrate.Service, dsn, repo string, required bool) {
	t.Helper()
	ctx := context.Background()
	ds, err := svc.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, rsDocs(1, map[string]any{"source": link(required), "note": link(false)})); err != nil {
		t.Fatalf("declare the package: %v", err)
	}
	person := mustPut(t, ds, owner, substrate.PutInput{Kind: rsPerson, Properties: map[string]any{"name": "Ada"}})
	ref := rsPerson + "/" + person.ID
	member := mustPut(t, ds, owner, substrate.PutInput{Kind: rsMember, Properties: map[string]any{
		"person": map[string]any{"ref": ref, "source": "hr", "note": "keep"},
	}})
	if _, err := scopedDB(t, dsn, repo).ExecContext(ctx, `UPDATE records
		SET props = jsonb_set(props, '{person}', (props->'person') - 'source' || jsonb_build_object('repository', props->'person'->'source'))
		WHERE kind = $1 AND id = $2`, rsMember, member.ID); err != nil {
		t.Fatalf("store the old link data: %v", err)
	}
	wantOldShape(t, ds, member.ID, ref, "planted")
	row := mustGet(t, ds, "substrate.reamde.dev/core/kind", rsMember)
	props := map[string]any{}
	for k, v := range row.Properties {
		props[k] = v
	}
	props["properties"] = map[string]any{"person": map[string]any{
		"type": "reference", "kind": rsPerson,
		"properties": map[string]any{"repository": link(required), "note": link(false)},
	}}
	if err := planter(t, ds).PlantDeclarationRow(ctx, "substrate.reamde.dev/core/kind", rsMember, props); err != nil {
		t.Fatalf("store the old shape: %v", err)
	}
	_ = svc.Close()

	svc2 := mustReopen(t, dsn, engine.DataRootOf(svc))
	t.Cleanup(func() { _ = svc2.Close() })
	ds2, err := svc2.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if pkg := mustGet(t, ds2, "substrate.reamde.dev/core/package", rsPackage); pkg.Properties["quarantined"] != true {
		t.Fatalf("the package is not set aside: %v", pkg.Properties)
	}
	// Set aside, the package serves no record, so the stored value is read
	// where it lies.
	var stored string
	if err := scopedDB(t, dsn, repo).QueryRowContext(ctx, `SELECT props->'person'->>'repository' FROM records WHERE kind = $1 AND id = $2`,
		rsMember, member.ID).Scan(&stored); err != nil || stored != "hr" {
		t.Fatalf("reopened, the stored reference carries repository %q (%v), want hr", stored, err)
	}
	// 2. The declaration with the link property under another name, as it
	// was declared, `required` included.
	if _, err := ds2.ApplyVocabularyDocuments(ctx, owner, rsDocs(2, map[string]any{"origin": link(required), "note": link(false)})); err != nil {
		t.Fatalf("step 2: %v", err)
	}
	if pkg := mustGet(t, ds2, "substrate.reamde.dev/core/package", rsPackage); pkg.Properties["quarantined"] == true {
		t.Fatalf("step 2 did not clear the quarantine: %v", pkg.Properties)
	}
	wantOldShape(t, ds2, member.ID, ref, "after step 2")
	// 3. Each reference value written whole: its ref, every other link
	// property copied as it is, and `repository`'s value under the new name.
	mustPatch(t, ds2, owner, rsMember, member.ID, substrate.PatchInput{Properties: map[string]any{
		"person": map[string]any{"ref": ref, "origin": "hr", "note": "keep"},
	}})
	wantMoved(t, ds2, member.ID, ref)
}

// wantOldShape holds the member's reference to the shape the steps start
// from: the value under `repository`, the other link data beside it, so the
// steps cannot pass on a value that never carried it.
func wantOldShape(t *testing.T, ds substrate.Dataset, id, ref, when string) {
	t.Helper()
	got, _ := mustGet(t, ds, rsMember, id).Properties["person"].(map[string]any)
	if got["ref"] != ref || got["repository"] != "hr" || got["note"] != "keep" || len(got) != 3 {
		t.Fatalf("%s, the reference = %v, want %s with repository hr and note keep", when, got, ref)
	}
}

// wantMoved holds the member's reference to the value under the new name,
// the other link data as it was, and no `repository`.
func wantMoved(t *testing.T, ds substrate.Dataset, id, ref string) {
	t.Helper()
	got, _ := mustGet(t, ds, rsMember, id).Properties["person"].(map[string]any)
	if _, stale := got["repository"]; stale || got["ref"] != ref || got["origin"] != "hr" || got["note"] != "keep" {
		t.Errorf("the reference = %v, want %s with origin hr and note keep, and no repository", got, ref)
	}
}
