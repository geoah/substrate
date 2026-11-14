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
	rrPackage = "ada.example.com/crew"
	rrKind    = rrPackage + "/membership"
)

// rrDocs is a package whose membership kind points at a person with one link
// property of the name given.
func rrDocs(version int64, link string) []map[string]any {
	return []map[string]any{
		vocabulary.PackageManifest(rrPackage, version),
		vocabulary.KindManifest(rrPackage, map[string]any{"singular": "person"}, map[string]any{
			"properties": map[string]any{"name": map[string]any{"type": "string"}},
		}),
		vocabulary.KindManifest(rrPackage, map[string]any{"singular": "membership"}, map[string]any{
			"properties": map[string]any{"person": map[string]any{
				"type": "reference", "kind": rrPackage + "/person",
				"properties": map[string]any{link: map[string]any{"type": "string"}},
			}},
		}),
	}
}

// `repository` is reserved in a reference value (the 1.0 review, #1313, F4):
// the apply door refuses a link property of that name, naming why. A stored
// declaration a binary before the reservation admitted is refused at the next
// open the way every stored closure the loader refuses is: its package is set
// aside, quarantined with the reason, and the rest of the repository serves.
// Applying the package with the link property renamed clears it, and a
// reference that still carries the old key is rewritten under the new one.
func TestAReferenceLinkPropertyNamedRepositoryIsRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, dsn := newService(t)
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}

	_, err = ds.ApplyVocabularyDocuments(ctx, owner, rrDocs(1, vocabulary.ReferenceRepositoryField))
	if err == nil || !strings.Contains(err.Error(), `"repository" is the reserved key naming the repository the referent lives in`) {
		t.Fatalf("a link property named repository: err = %v, want the reservation named", err)
	}

	// What a binary before the reservation would have left: the declaration
	// and a record whose reference carries link data under the name. Written
	// under another link name, then renamed in the stored rows.
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, rrDocs(1, "source")); err != nil {
		t.Fatalf("declare the package: %v", err)
	}
	person := mustPut(t, ds, owner, substrate.PutInput{Kind: rrPackage + "/person", Properties: map[string]any{"name": "Ada"}})
	personRef := rrPackage + "/person/" + person.ID
	member := mustPut(t, ds, owner, substrate.PutInput{Kind: rrKind, Properties: map[string]any{
		"person": map[string]any{"ref": personRef, "source": "hr"},
	}})
	res, err := scopedDB(t, dsn, testdb.Repository(t)).ExecContext(ctx, `UPDATE records
		SET props = jsonb_set(props, '{person}', (props->'person') - 'source' || jsonb_build_object('repository', props->'person'->'source'))
		WHERE kind = $1 AND id = $2`, rrKind, member.ID)
	if err != nil {
		t.Fatalf("store the old link data: %v", err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		t.Fatalf("store the old link data: %d rows, %v; want the one record", n, err)
	}
	row := mustGet(t, ds, "substrate.reamde.dev/core/kind", rrKind)
	props := map[string]any{}
	for k, v := range row.Properties {
		props[k] = v
	}
	props["properties"] = map[string]any{"person": map[string]any{
		"type": "reference", "kind": rrPackage + "/person",
		"properties": map[string]any{vocabulary.ReferenceRepositoryField: map[string]any{"type": "string"}},
	}}
	if err := planter(t, ds).PlantDeclarationRow(ctx, "substrate.reamde.dev/core/kind", rrKind, props); err != nil {
		t.Fatalf("store the old shape: %v", err)
	}
	_ = svc.Close()

	svc2 := mustReopen(t, dsn, engine.DataRootOf(svc))
	ds2, err := svc2.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("a repository with one refused stored closure must still open: %v", err)
	}
	pkg := mustGet(t, ds2, "substrate.reamde.dev/core/package", rrPackage)
	if pkg.Properties["quarantined"] != true || !strings.Contains(fmt.Sprint(pkg.Properties["quarantineReason"]), `"repository" is the reserved key`) {
		t.Fatalf("the package holding the reserved name is not set aside with the reason: %v", pkg.Properties)
	}
	if _, err := ds2.KindByRef(ctx, rrKind); err == nil {
		t.Fatal("the set-aside package's kind still resolves")
	}
	mustPut(t, ds2, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "still served"}})

	// The way back, with the link data in place: apply the package with the
	// link property renamed, then rewrite each reference that still carries
	// the old key.
	if _, err := ds2.ApplyVocabularyDocuments(ctx, owner, rrDocs(2, "origin")); err != nil {
		t.Fatalf("apply the package with the link property renamed: %v", err)
	}
	if pkg := mustGet(t, ds2, "substrate.reamde.dev/core/package", rrPackage); pkg.Properties["quarantined"] == true {
		t.Fatalf("renaming the link property did not clear the quarantine: %v", pkg.Properties)
	}
	held := mustGet(t, ds2, rrKind, member.ID)
	if v, _ := held.Properties["person"].(map[string]any); v["repository"] != "hr" {
		t.Fatalf("the stored link data = %v, want the old key until the reference is rewritten", held.Properties["person"])
	}
	mustPatch(t, ds2, owner, rrKind, member.ID, substrate.PatchInput{Properties: map[string]any{
		"person": map[string]any{"ref": personRef, "origin": "hr"},
	}})
	got, _ := mustGet(t, ds2, rrKind, member.ID).Properties["person"].(map[string]any)
	if _, stale := got["repository"]; stale || got["origin"] != "hr" {
		t.Fatalf("the rewritten reference = %v, want the link data under origin alone", got)
	}
}
