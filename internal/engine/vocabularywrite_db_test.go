package engine_test

// Schema is records: the batch apply verb, admission refusals,
// commit-is-activation, concurrency, refuse-with-instances, and the one-time
// stored-manifest promotion.

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

func applier(t *testing.T, ds substrate.Dataset) substrate.VocabularyApplier {
	t.Helper()
	sa, ok := ds.(substrate.VocabularyApplier)
	if !ok {
		t.Fatal("dataset does not implement the schema apply seam")
	}
	return sa
}

const (
	swAuthority = "widgets.example.substrate.reamde.dev"
	swPackage   = swAuthority + "/widgets"
)

func swTypeDoc(singular, plural string, props map[string]any) map[string]any {
	return vocabulary.KindManifest(swPackage,
		map[string]any{"singular": singular, "plural": plural},
		map[string]any{"properties": props})
}

// The batch is one transaction: a single bad document fails the whole apply
// with the loader's full problem list, and nothing lands.
func TestSchemaApplyBatchAllOrNone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	before := maxSeq(t, ds)

	_, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(swPackage, 0),
		swTypeDoc("widget", "widgets", map[string]any{"name": map[string]any{"type": "string"}}),
		swTypeDoc("gadget", "gadgets", map[string]any{"weird": map[string]any{"type": "nosuchkind"}}),
	})
	if err == nil {
		t.Fatal("a batch with a bad document must fail whole")
	}
	wantErr(t, err, substrate.ErrValidation, "bad batch")
	var ve *substrate.ValidationError
	if !asValidationErr(err, &ve) || len(ve.Problems) == 0 {
		t.Fatalf("expected the loader's problem list, got %v", err)
	}
	if !strings.Contains(fmt.Sprint(ve.Problems), "nosuchkind") {
		t.Fatalf("problems should name the offending kind: %v", ve.Problems)
	}
	// Nothing landed: no rows, no changelog, no registry change.
	if got := maxSeq(t, ds); got != before {
		t.Fatalf("failed batch wrote %+v", changesSince(t, ds, before))
	}
	if _, err := ds.Get(ctx, "substrate.reamde.dev/core/package", swPackage); err == nil {
		t.Fatal("failed batch left the authority row behind")
	}
	if _, err := ds.KindByRef(ctx, swPackage+"/widget"); err == nil {
		t.Fatal("failed batch installed the good type")
	}
}

func asValidationErr(err error, target **substrate.ValidationError) bool {
	for err != nil {
		if ve, ok := err.(*substrate.ValidationError); ok {
			*target = ve
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// A schema write is activation: changelog rows land with the commit, the new
// type is immediately writable, the rows are the registry a reopen rebuilds
// from — and the reopen itself is silent.
func TestSchemaApplyActivatesOnCommit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	open := func() substrate.Service {
		svc, err := engine.Open(ctx, dsn, engine.WithCredentialKey(engine.TestCredentialKey),
			engine.WithKindsDir("../../kinds/substrate.reamde.dev/core"))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		return svc
	}
	svc := open()
	if _, err := svc.CreateRepository(ctx, "geoah", "geoah.example.com"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	before := maxSeq(t, ds)

	ents, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(swPackage, 0),
		swTypeDoc("widget", "widgets", map[string]any{"name": map[string]any{"type": "string"}}),
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(ents) != 2 {
		t.Fatalf("applied records = %v", ids(ents))
	}

	// The changelog carries the deploy (the Stream shows it).
	rows := changesSince(t, ds, before)
	seen := map[string]bool{}
	for _, ch := range rows {
		seen[ch.Kind] = true
		if ch.Actor != owner {
			t.Fatalf("schema change attributed to %q", ch.Actor)
		}
	}
	if !seen["substrate.reamde.dev/core/package"] || !seen["substrate.reamde.dev/core/kind"] {
		t.Fatalf("schema changes missing from the changelog: %+v", rows)
	}

	// Commit is activation: the type is writable NOW, no reopen, no restart.
	w := mustPut(t, ds, owner, substrate.PutInput{
		Kind: swPackage + "/widget", Properties: map[string]any{"name": "roof"},
	})
	if w.Properties["name"] != "roof" {
		t.Fatalf("widget = %v", w.Properties)
	}
	// And the declarations are ordinary, queryable records.
	row := mustGet(t, ds, "substrate.reamde.dev/core/kind", swPackage+"/widget")
	names, _ := row.Properties["names"].(map[string]any)
	if row.Kind != "substrate.reamde.dev/core/kind" || names["plural"] != "widgets" ||
		row.Properties["source"] != "installed" {
		t.Fatalf("declaration record = %v", row.Properties)
	}
	// The row's properties ARE the declaration: the authored keys, no blob.
	if row.Properties["properties"] == nil {
		t.Fatalf("declaration record carries no properties: %v", row.Properties)
	}

	// Reopen: the registry rebuilds FROM the rows — same types, no writes.
	seq := maxSeq(t, ds)
	_ = svc.Close()
	svc2 := open()
	t.Cleanup(func() { _ = svc2.Close() })
	ds2, err := svc2.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	if got := maxSeq(t, ds2); got != seq {
		t.Fatalf("reopen wrote %+v", changesSince(t, ds2, seq))
	}
	ti, err := ds2.KindByRef(ctx, swPackage+"/widget")
	if err != nil || ti.Source != "installed" {
		t.Fatalf("rebuilt type = %+v %v", ti, err)
	}
	if got := mustGet(t, ds2, w.Kind, w.ID); got.Properties["name"] != "roof" {
		t.Fatalf("data lost across reopen: %v", got.Properties)
	}
}

// A changed function activates on commit: the next dispatcher pass runs the
// NEW body, no restart, and the new function's cursor starts at head.
func TestSchemaApplySwapsFunctionsLive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	sa := applier(t, ds)
	ops, ok := ds.(fnOps)
	if !ok {
		t.Fatal("dataset does not implement the functions seam")
	}

	fnData := func(title string) map[string]any {
		return map[string]any{
			"authority":   swAuthority,
			"package":     "widgets",
			"description": "mirrors widgets into tasks",
			"runtime":     vocabulary.RuntimePython,
			"permissions": map[string]any{"writes": []any{"samples.substrate.reamde.dev/tasks/task"}},
			"source": `
def main(input, host):
    c = input["envelope"]["change"]
    return {"effects": [{"action": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
                         "id": "t-" + c["id"], "properties": {"name": "` + title + `"}}]}
`,
		}
	}
	if _, err := sa.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(swPackage, 0),
		swTypeDoc("widget", "widgets", map[string]any{"name": map[string]any{"type": "string"}}),
		vocabulary.FunctionManifest(swPackage, "mirror", fnData("v1")),
	}); err != nil {
		t.Fatalf("apply v1: %v", err)
	}
	// The subscription is a trigger RECORD, written like any other data row.
	if _, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/trigger", ID: "on-mirror." + swPackage,
		Properties: map[string]any{
			"enabled": true,
			"source": map[string]any{"record": map[string]any{
				"kinds": []any{swPackage + "/widget"}, "ops": []any{"create", "update"},
			}},
			"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", swPackage+"/mirror"),
		},
	}); err != nil {
		t.Fatalf("put trigger: %v", err)
	}

	a := mustPut(t, ds, owner, substrate.PutInput{Kind: swPackage + "/widget", Properties: map[string]any{"name": "a"}})
	process(t, ops)
	if got := mustGet(t, ds, taskType, "t-"+a.ID); got.Title != "v1" {
		t.Fatalf("v1 delivery title = %q", got.Title)
	}

	// Swap the body. The write commits, the pointer publishes, the very next
	// delivery runs v2 — the restart-to-activate hazard is dead.
	if _, err := sa.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.FunctionManifest(swPackage, "mirror", fnData("v2")),
	}); err != nil {
		t.Fatalf("apply v2: %v", err)
	}
	b := mustPut(t, ds, owner, substrate.PutInput{Kind: swPackage + "/widget", Properties: map[string]any{"name": "b"}})
	process(t, ops)
	if got := mustGet(t, ds, taskType, "t-"+b.ID); got.Title != "v2" {
		t.Fatalf("post-swap delivery title = %q (the old registry answered)", got.Title)
	}
	// The function is queryable as a record, its declaration in its properties.
	fnRow := mustGet(t, ds, "substrate.reamde.dev/core/function", swPackage+"/mirror")
	if fnRow.Kind != "substrate.reamde.dev/core/function" || fnRow.Properties["source"] == nil ||
		fnRow.Properties["runtime"] == nil {
		t.Fatalf("function record = %v", fnRow.Properties)
	}
}

// Two concurrent schema writes serialize under the per-repository schema-write
// mutex (both land; neither validates against a stale base), and data writes
// flow while schema writes run.
func TestSchemaWritesSerializeDataWritesFlow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	sa := applier(t, ds)

	if _, err := sa.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(swPackage, 0),
	}); err != nil {
		t.Fatalf("apply authority: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for _, ty := range []string{"alpha", "beta", "gamma", "delta"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := sa.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
				swTypeDoc(ty, ty+"s", map[string]any{"name": map[string]any{"type": "string"}}),
			})
			if err != nil {
				errs <- fmt.Errorf("apply %s: %w", ty, err)
			}
		}()
	}
	// Data writes during the schema writes: they must not wait on the schema
	// mutex or the candidate compile.
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := ds.Put(ctx, owner, substrate.PutInput{
				Kind: "task", Properties: map[string]any{"name": fmt.Sprintf("t%d", i)},
			})
			if err != nil {
				errs <- fmt.Errorf("data write %d: %w", i, err)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	// Serialization means every type survived: no batch clobbered another.
	for _, ty := range []string{"alpha", "beta", "gamma", "delta"} {
		if _, err := ds.KindByRef(ctx, swPackage+"/"+ty); err != nil {
			t.Fatalf("type %s lost to a concurrent schema write: %v", ty, err)
		}
	}
}

// Deleting a type with live instances refuses, counted in the same
// transaction; with the instances gone the delete admits, the row tombstones,
// and the identity leaves the registry.
func TestSchemaDeleteRefusesWithInstances(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	sa := applier(t, ds)

	if _, err := sa.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(swPackage, 0),
		swTypeDoc("widget", "widgets", map[string]any{"name": map[string]any{"type": "string"}}),
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	w := mustPut(t, ds, owner, substrate.PutInput{
		Kind: swPackage + "/widget", Properties: map[string]any{"name": "keep"},
	})

	_, err := ds.Delete(ctx, owner, "substrate.reamde.dev/core/kind", swPackage+"/widget")
	if err == nil {
		t.Fatal("type deletion with live instances must refuse")
	}
	wantErr(t, err, substrate.ErrGuard, "refuse-with-instances")
	if !strings.Contains(err.Error(), "1 live") {
		t.Fatalf("refusal should carry the transactional count: %v", err)
	}
	// Still installed, still writable.
	if _, err := ds.KindByRef(ctx, swPackage+"/widget"); err != nil {
		t.Fatalf("refused delete uninstalled the type: %v", err)
	}

	if _, err := ds.Delete(ctx, owner, w.Kind, w.ID); err != nil {
		t.Fatalf("delete instance: %v", err)
	}
	gone, err := ds.Delete(ctx, owner, "substrate.reamde.dev/core/kind", swPackage+"/widget")
	if err != nil {
		t.Fatalf("delete type: %v", err)
	}
	if gone.DeletedAt == nil {
		t.Fatalf("schema row not tombstoned: %+v", gone)
	}
	if _, err := ds.KindByRef(ctx, swPackage+"/widget"); err == nil {
		t.Fatal("deleted type still resolves")
	}
	// History orphans by design: the instance's changelog rows remain.
	found := false
	for _, ch := range changesSince(t, ds, 0) {
		if ch.RecordID == w.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("the dropped type's history should stay readable")
	}
}

// M5: WHAT PROJECTION STORES IS WHAT THE AUTHOR WROTE. The declaration a
// registry object carries is a read-only view of the document, and the projection
// serializes it into the row — so a value the loader normalized on the way past
// would be stored as though it had been authored, and every reader downstream
// (the console's editors, a client's `get -o yaml | apply -f`) would be looking at
// the substrate's opinion instead of the author's.
//
// An enum's values are the case that used to move: authored as bare scalars, they
// were rewritten into {value, label} objects in place. The kind's own
// `properties` is the meta-kind's one json leaf, so nothing about the row needs
// them rewritten — and the stored row now says exactly what the manifest said.
//
// A PROPERTY TYPE's values are the one exception, and the meta-kind's declaration
// is the reason: core's `propertytype` types them as a repeated {value, label}
// object, so a bare scalar could not be stored at all. The loader normalizes a
// COPY (internal/vocabulary, TestTheParseNeverMutatesTheDocument), and that is the
// whole of the exception.
func TestProjectionStoresTheAuthoredDeclaration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(swPackage, 0),
		{
			"kind":     vocabulary.CoreKind(vocabulary.DocPropertyType),
			"metadata": map[string]any{"id": swPackage + "/grade"},
			"data": map[string]any{
				"authority": swAuthority,
				"package":   "widgets",
				"base":      "enum",
				"values":    []any{"good", "bad"},
			},
		},
		swTypeDoc("widget", "widgets", map[string]any{
			"status": map[string]any{
				"type":   "enum",
				"values": []any{"draft", "live"},
			},
		}),
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	row := mustGet(t, ds, "substrate.reamde.dev/core/kind", swPackage+"/widget")
	props, _ := row.Properties["properties"].(map[string]any)
	status, _ := props["status"].(map[string]any)
	values, _ := status["values"].([]any)
	if len(values) != 2 {
		t.Fatalf("the stored enum values = %#v", status["values"])
	}
	for i, v := range values {
		if _, rewritten := v.(map[string]any); rewritten {
			t.Fatalf("the stored declaration rewrote value %d into %#v — the row must carry what the author wrote", i, v)
		}
	}
	if values[0] != "draft" || values[1] != "live" {
		t.Fatalf("the stored enum values = %#v", values)
	}

	// The property type's row carries the objects its own declaration types.
	pt := mustGet(t, ds, "substrate.reamde.dev/core/propertytype", swPackage+"/grade")
	ptValues, _ := pt.Properties["values"].([]any)
	if len(ptValues) != 2 {
		t.Fatalf("the stored property type values = %#v", pt.Properties["values"])
	}
	first, isObject := ptValues[0].(map[string]any)
	if !isObject || first["value"] != "good" {
		t.Fatalf("a property type's stored values must be {value, label} objects: %#v", ptValues[0])
	}
}

// KindInfo.Definition IS THE AUTHORED DECLARATION, and it reads the same before
// and after a restart. A kind that pins no version of its own has the authority's
// stamped onto its row; the row is what a reopen rebuilds the registry from, so a
// definition that carried the stored properties verbatim would gain a `version`
// nobody authored the moment the process restarted — one declaration, two answers,
// and any client diffing or fingerprinting it (gql.RegistryKey) would see a change
// that never happened.
func TestKindInfoDefinitionSurvivesAReload(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	open := func() substrate.Service {
		svc, err := engine.Open(ctx, dsn, engine.WithCredentialKey(engine.TestCredentialKey), engine.WithKindsDir("../../kinds/substrate.reamde.dev/core"))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		t.Cleanup(func() { _ = svc.Close() })
		return svc
	}
	svc := open()
	if _, err := svc.CreateRepository(ctx, "geoah", "geoah.example.com"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	// No `version:` on the declaration — the authority's is what the row gets.
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(swPackage, 3),
		swTypeDoc("widget", "widgets", map[string]any{"name": map[string]any{"type": "string"}}),
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	before, err := ds.KindByRef(ctx, swPackage+"/widget")
	if err != nil {
		t.Fatalf("read the kind: %v", err)
	}
	if _, stamped := before.Definition["version"]; stamped {
		t.Fatalf("the rendered declaration carries the engine's version: %v", before.Definition)
	}
	if before.Version != 3 {
		t.Fatalf("the stamped version is not on the KindInfo: %+v", before)
	}
	// The ROW carries it, because a boot upgrade diffs on it.
	row := mustGet(t, ds, "substrate.reamde.dev/core/kind", swPackage+"/widget")
	if v, _ := vocabulary.VersionValue(row.Properties["version"]); v != 3 {
		t.Fatalf("the row carries no stamped version: %v", row.Properties)
	}
	_ = svc.Close()

	// The reopen rebuilds the registry from that row.
	svc2 := open()
	ds2, err := svc2.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	after, err := ds2.KindByRef(ctx, swPackage+"/widget")
	if err != nil {
		t.Fatalf("read the kind again: %v", err)
	}
	if !reflect.DeepEqual(before.Definition, after.Definition) {
		t.Fatalf("the declaration moved across a restart:\n before %#v\n after  %#v",
			before.Definition, after.Definition)
	}
	if after.Version != 3 {
		t.Fatalf("the version did not survive the reload: %+v", after)
	}
	// A declaration that PINS its own version keeps reading it off KindInfo.
	if _, err := applier(t, ds2).ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.KindManifest(swPackage,
			map[string]any{"singular": "gadget", "plural": "gadgets"},
			map[string]any{"version": 21, "properties": map[string]any{"name": map[string]any{"type": "string"}}}),
	}); err != nil {
		t.Fatalf("apply the pinned declaration: %v", err)
	}
	pinned, err := ds2.KindByRef(ctx, swPackage+"/gadget")
	if err != nil {
		t.Fatalf("read the pinned kind: %v", err)
	}
	if pinned.Version != 21 {
		t.Fatalf("the pinned version is not on the KindInfo: %+v", pinned)
	}
}

// The generic verbs write schema records THROUGH admission: put/patch with a
// definition work and activate; a write into shipped vocabulary refuses; a
// definition that breaks the closure refuses with the problem list.
func TestGenericWritesRouteThroughAdmission(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	sa := applier(t, ds)

	if _, err := sa.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(swPackage, 0),
		swTypeDoc("widget", "widgets", map[string]any{"name": map[string]any{"type": "string"}}),
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	// PUT with an extended declaration: the new property is usable at once. The
	// row's properties ARE the declaration — there is no `definition` blob to
	// reach into — so what a reader gets back is what a writer sends.
	row := mustGet(t, ds, "substrate.reamde.dev/core/kind", swPackage+"/widget")
	if _, blob := row.Properties["definition"]; blob {
		t.Fatalf("a declaration row still carries a definition blob: %v", row.Properties)
	}
	declared, _ := row.Properties["properties"].(map[string]any)
	if declared == nil {
		t.Fatalf("the declaration row carries no properties: %v", row.Properties)
	}
	declared["count"] = map[string]any{"type": "float"}
	if _, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/kind", ID: swPackage + "/widget",
		Properties: map[string]any{
			"authority": swAuthority,
			"package":   "widgets", "names": row.Properties["names"],
			"properties": declared,
		},
	}); err != nil {
		t.Fatalf("generic put of a declaration record: %v", err)
	}
	if e := mustPut(t, ds, owner, substrate.PutInput{
		Kind: swPackage + "/widget", Properties: map[string]any{"name": "n", "count": 3},
	}); e.Properties["count"] == nil {
		t.Fatalf("new property not live: %v", e.Properties)
	}

	// The `definition` blob is REFUSED, naming the properties that carry the
	// declaration instead: obeying it would store a declaration no reader looks
	// at, and dropping it would tell the client an edit landed.
	declared["note"] = map[string]any{"type": "text"}
	_, blobErr := ds.Put(ctx, owner, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/kind", ID: swPackage + "/widget",
		Properties: map[string]any{"definition": map[string]any{
			"authority": swAuthority,
			"package":   "widgets", "names": row.Properties["names"], "properties": declared,
		}},
	})
	wantErr(t, blobErr, substrate.ErrValidation, "a write carrying a definition blob")
	if !strings.Contains(fmt.Sprint(blobErr), "props.definition") ||
		!strings.Contains(fmt.Sprint(blobErr), "properties") {
		t.Fatalf("the refusal must name the blob and the typed properties: %v", blobErr)
	}
	if e := mustPut(t, ds, owner, substrate.PutInput{
		Kind: swPackage + "/widget", Properties: map[string]any{"name": "n2"},
	}); e.Properties["note"] != nil {
		t.Fatalf("the refused blob still landed: %v", e.Properties)
	}
	// A NULL under that key is refused too. A null deletes a property everywhere
	// else in this dialect, but there is nothing left to delete here — no row
	// carries a blob — so a client naming the key at all is working from a
	// document this substrate stopped storing.
	_, nullErr := ds.Put(ctx, owner, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/kind", ID: swPackage + "/widget",
		Properties: map[string]any{
			"authority": swAuthority,
			"package":   "widgets", "names": row.Properties["names"],
			"properties": declared, "definition": nil,
		},
	})
	wantErr(t, nullErr, substrate.ErrValidation, "a write carrying a null definition")
	if !strings.Contains(fmt.Sprint(nullErr), "props.definition") {
		t.Fatalf("the refusal must name the blob: %v", nullErr)
	}

	// A declaration that breaks the closure refuses whole.
	declared["bad"] = map[string]any{"type": "nosuchkind"}
	_, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/kind", ID: swPackage + "/widget",
		Properties: map[string]any{
			"authority": swAuthority,
			"package":   "widgets", "names": row.Properties["names"], "properties": declared,
		},
	})
	wantErr(t, err, substrate.ErrValidation, "closure-breaking declaration")

	// The shipped vocabulary is the embedded tree's to change. The seeded core
	// package is the whole of it: a sample package installs, so `core` is what
	// a request meets the refusal on.
	tokenRow := mustGet(t, ds, "substrate.reamde.dev/core/kind", "substrate.reamde.dev/core/token")
	_, err = ds.Put(ctx, owner, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/kind", ID: "substrate.reamde.dev/core/token",
		Properties: map[string]any{
			"authority": "substrate.reamde.dev", "package": "core", "names": tokenRow.Properties["names"],
			"properties": tokenRow.Properties["properties"],
		},
	})
	wantErr(t, err, substrate.ErrForbidden, "builtin package write")
	_, err = ds.Delete(ctx, owner, "substrate.reamde.dev/core/kind", "substrate.reamde.dev/core/token")
	wantErr(t, err, substrate.ErrForbidden, "builtin package delete")
}

// THE ENGINE'S PROPERTIES ARE NOT THE WRITER'S. A declaration write that
// supplies a managed property round-trips when it echoes the stored value and is
// REFUSED when it invents one — silently replacing it would tell the client its
// edit landed. Two more refusals sit beside it: the retired blob and the typed
// properties in one write (obeying either discards the other's edits), and an
// undeclared property, which is a typo rather than a change that vanishes.
func TestDeclarationWritesRefuseWhatTheEngineOwns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	sa := applier(t, ds)
	if _, err := sa.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(swPackage, 0),
		swTypeDoc("widget", "widgets", map[string]any{"name": map[string]any{"type": "string"}}),
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	row := mustGet(t, ds, "substrate.reamde.dev/core/kind", swPackage+"/widget")
	declared, _ := row.Properties["properties"].(map[string]any)
	base := func() map[string]any {
		return map[string]any{
			"authority": swAuthority,
			"package":   "widgets", "names": row.Properties["names"], "properties": declared,
		}
	}

	// The round trip: `get -o yaml | apply -f` echoes every property a read hands
	// back — the stamped `source` and `version` among them, and the derived
	// `title` — and must land.
	echoed := base()
	echoed["source"] = row.Properties["source"]
	echoed["version"] = row.Properties["version"]
	echoed["title"] = row.Title
	if _, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/kind", ID: swPackage + "/widget", Properties: echoed,
	}); err != nil {
		t.Fatalf("a read echoed straight back must apply: %v", err)
	}

	// The ORIGIN is the engine's: a kind may pin its own `version` (the loader
	// admits `data.version`, which is why echoing one above landed), and nothing
	// authors where a declaration came from.
	origin := base()
	origin["source"] = "builtin"
	_, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/kind", ID: swPackage + "/widget", Properties: origin,
	})
	wantErr(t, err, substrate.ErrValidation, "managed origin mismatch")
	if !strings.Contains(err.Error(), "props.source") {
		t.Fatalf("the refusal must name the property: %v", err)
	}
	if got := mustGet(t, ds, "substrate.reamde.dev/core/kind", swPackage+"/widget"); got.Properties["source"] != row.Properties["source"] {
		t.Fatalf("the refused write moved the origin to %v", got.Properties["source"])
	}

	// A TRAIT's version is the engine's whole and simple: its document admits no
	// `version:` at all, so an invented one is refused and the stored one echoes.
	if _, err := sa.ApplyVocabularyDocuments(ctx, owner, []map[string]any{{
		"kind":     "substrate.reamde.dev/core/trait",
		"metadata": map[string]any{"id": swPackage + "/spanned"},
		"data": map[string]any{
			"authority":  swAuthority,
			"package":    "widgets",
			"properties": map[string]any{"span": "string"},
		},
	}}); err != nil {
		t.Fatalf("apply a trait: %v", err)
	}
	traitRow := mustGet(t, ds, "substrate.reamde.dev/core/trait", swPackage+"/spanned")
	_, err = ds.Put(ctx, owner, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/trait", ID: swPackage + "/spanned",
		Properties: map[string]any{
			"authority": swAuthority,
			"package":   "widgets", "properties": traitRow.Properties["properties"],
			"version": 99,
		},
	})
	wantErr(t, err, substrate.ErrValidation, "managed version mismatch")
	if !strings.Contains(err.Error(), "props.version") {
		t.Fatalf("the refusal must name the property: %v", err)
	}
	if got := mustGet(t, ds, "substrate.reamde.dev/core/trait", swPackage+"/spanned"); got.Properties["version"] != traitRow.Properties["version"] {
		t.Fatalf("the refused write moved the version to %v", got.Properties["version"])
	}
	if _, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/trait", ID: swPackage + "/spanned",
		Properties: map[string]any{
			"authority": swAuthority,
			"package":   "widgets", "properties": traitRow.Properties["properties"],
			"version": traitRow.Properties["version"], "title": traitRow.Title,
		},
	}); err != nil {
		t.Fatalf("a trait read echoed straight back must apply: %v", err)
	}

	// Both spellings in one write: refused, naming what it carried twice.
	both := base()
	both["definition"] = map[string]any{
		"authority": swAuthority,
		"package":   "widgets", "names": row.Properties["names"], "properties": declared,
	}
	_, err = ds.Put(ctx, owner, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/kind", ID: swPackage + "/widget", Properties: both,
	})
	wantErr(t, err, substrate.ErrValidation, "definition and typed properties")
	if !strings.Contains(err.Error(), "definition") || !strings.Contains(err.Error(), "properties") {
		t.Fatalf("the refusal must name both spellings: %v", err)
	}

	// A typo is refused rather than dropped.
	typo := base()
	typo["displayTemplat"] = "{name}"
	_, err = ds.Put(ctx, owner, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/kind", ID: swPackage + "/widget", Properties: typo,
	})
	wantErr(t, err, substrate.ErrValidation, "undeclared property")

	// A PATCH of an ordinary property is unaffected: the merge carries the stored
	// managed values, which are the engine's own answer by definition.
	if _, err := ds.Patch(ctx, owner, "substrate.reamde.dev/core/kind", swPackage+"/widget",
		substrate.PatchInput{Properties: map[string]any{"description": "the widget kind"}}); err != nil {
		t.Fatalf("patch an ordinary property: %v", err)
	}
}

// A DELETED SPELLING IS NAMED AT BOTH DOORS. A declaration arrives here as
// PROPERTIES and at the YAML door as a document, and a writer sending the
// retired `emit` deserves the same sentence either way: "not declared on
// substrate.reamde.dev/core/function" tells them nothing about where the grant
// went. Every retired grant key is checked on PUT and on PATCH, since a patch
// carrying one is the same mistake with a smaller payload.
func TestDeclarationWritesNameTheDeletedSpellings(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	sa := applier(t, ds)
	const fnKind = "substrate.reamde.dev/core/function"
	const agentKind = "substrate.reamde.dev/core/agent"
	fnID, agentID := swPackage+"/mirror", swPackage+"/thinker"
	if _, err := sa.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(swPackage, 0),
		swTypeDoc("widget", "widgets", map[string]any{"name": map[string]any{"type": "string"}}),
		vocabulary.FunctionManifest(swPackage, "mirror", map[string]any{
			"authority": swAuthority,
			"package":   "widgets", "description": "mirrors widgets", "runtime": vocabulary.RuntimePython,
			"permissions": map[string]any{"writes": []any{swPackage + "/widget"}},
			"source":      "def main(input, host): return {}",
		}),
		vocabulary.AgentManifest(swPackage, "thinker", map[string]any{
			"description": "thinks", "prompt": "be useful",
			"provider": "default", "model": "gpt-5",
			"permissions": map[string]any{"writes": []any{swPackage + "/widget"}},
		}),
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	fnRow := mustGet(t, ds, fnKind, fnID)
	agentRow := mustGet(t, ds, agentKind, agentID)

	for name, tc := range map[string]struct {
		kind, id string
		base     *substrate.Record
		prop     string
		value    any
		want     string
	}{
		"a function's emit": {
			fnKind, fnID, fnRow, "emit",
			[]any{swPackage + "/widget"},
			"props.emit: the deleted key, replaced by permissions.writes",
		},
		"a function's reads": {
			fnKind, fnID, fnRow, "reads",
			map[string]any{"kinds": []any{swPackage + "/widget"}},
			"props.reads: the deleted key, replaced by permissions.reads",
		},
		"a function's call": {
			fnKind, fnID, fnRow, "call",
			[]any{swPackage + "/other"},
			"props.call: the deleted key, replaced by permissions.call",
		},
		"a function's network": {
			fnKind, fnID, fnRow, "network",
			[]any{"api.example.com"},
			"props.network: the deleted key, replaced by permissions.network",
		},
		"a function's mutations": {
			fnKind, fnID, fnRow, "mutations",
			[]any{"merge"},
			"props.mutations: the deleted key, replaced by permissions.mutations",
		},
		"a function's capability wrapper": {
			fnKind, fnID, fnRow, "capabilities",
			map[string]any{"emit": []any{swPackage + "/widget"}},
			"props.capabilities: the deleted key, replaced by permissions",
		},
		"an agent's emit": {
			agentKind, agentID, agentRow, "emit",
			[]any{swPackage + "/widget"},
			"props.emit: the deleted key, replaced by permissions.writes",
		},
		"an agent's reads": {
			agentKind, agentID, agentRow, "reads",
			map[string]any{"kinds": []any{swPackage + "/widget"}},
			"props.reads: the deleted key, replaced by permissions.reads",
		},
	} {
		t.Run(name, func(t *testing.T) {
			// PUT: the whole declaration, plus the retired key beside it.
			props := map[string]any{}
			for k, v := range tc.base.Properties {
				props[k] = v
			}
			props[tc.prop] = tc.value
			_, err := ds.Put(ctx, owner, substrate.PutInput{Kind: tc.kind, ID: tc.id, Properties: props})
			wantErr(t, err, substrate.ErrValidation, "a put carrying "+tc.prop)
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the put refusal must name the replacement, got: %v", err)
			}
			// PATCH: the retired key alone.
			_, err = ds.Patch(ctx, owner, tc.kind, tc.id,
				substrate.PatchInput{Properties: map[string]any{tc.prop: tc.value}})
			wantErr(t, err, substrate.ErrValidation, "a patch carrying "+tc.prop)
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the patch refusal must name the replacement, got: %v", err)
			}
			// A NULL is refused too, by presence: no row carries one of these keys,
			// so there is nothing a delete marker could be clearing.
			_, err = ds.Patch(ctx, owner, tc.kind, tc.id,
				substrate.PatchInput{Properties: map[string]any{tc.prop: nil}})
			wantErr(t, err, substrate.ErrValidation, "a patch nulling "+tc.prop)
		})
	}

	// The tool entry's own retired key is the loader's, since `tools` is a
	// declared property and the entry inside it is what moved.
	tools := map[string]any{}
	for k, v := range agentRow.Properties {
		tools[k] = v
	}
	tools["tools"] = []any{map[string]any{"callable": vocabulary.HostFunctionGraphQL}}
	_, err := ds.Put(ctx, owner, substrate.PutInput{Kind: agentKind, ID: agentID, Properties: tools})
	wantErr(t, err, substrate.ErrValidation, "a tool entry naming its callable")
	if !strings.Contains(err.Error(), `key "callable" is deleted — function`) {
		t.Fatalf("the refusal must name the entry's replacement, got: %v", err)
	}
}

// A function's uninstall does NOT touch delivery state any more: the cursor
// belongs to the TRIGGER record, which outlives the callable. While the
// callable is gone the dispatcher skips the trigger loudly and its cursor
// stands still; a reinstall of the identity resumes from where it stood —
// the interim change delivers late, never lost, and never as an implicit
// backfill of anything older.
func TestTriggerOutlivesItsCallable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	sa := applier(t, ds)
	ops, ok := ds.(fnOps)
	if !ok {
		t.Fatal("dataset does not implement the automation seam")
	}

	fnData := map[string]any{
		"authority":   swAuthority,
		"package":     "widgets",
		"description": "mirrors widgets into tasks",
		"runtime":     vocabulary.RuntimePython,
		"permissions": map[string]any{"writes": []any{"samples.substrate.reamde.dev/tasks/task"}},
		"source": `
def main(input, host):
    c = input["envelope"]["change"]
    return {"effects": [{"action": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
                         "id": "t-" + c["id"], "properties": {"name": "mirrored"}}]}
`,
	}
	if _, err := sa.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(swPackage, 0),
		swTypeDoc("widget", "widgets", map[string]any{"name": map[string]any{"type": "string"}}),
		vocabulary.FunctionManifest(swPackage, "mirror", fnData),
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	const triggerID = "on-mirror.widgets.example.substrate.reamde.dev"
	if _, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/trigger", ID: triggerID,
		Properties: map[string]any{
			"source":   map[string]any{"record": map[string]any{"kinds": []any{swPackage + "/widget"}}},
			"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", swPackage+"/mirror"),
		},
	}); err != nil {
		t.Fatalf("put trigger: %v", err)
	}
	a := mustPut(t, ds, owner, substrate.PutInput{Kind: swPackage + "/widget", Properties: map[string]any{"name": "a"}})
	process(t, ops)
	if _, err := ds.Get(ctx, taskType, "t-"+a.ID); err != nil {
		t.Fatalf("first incarnation never delivered: %v", err)
	}

	// The function dies; a widget changes while the callable is gone. The
	// trigger is skipped — no delivery, no cursor motion, no park.
	if _, err := ds.Delete(ctx, owner, "substrate.reamde.dev/core/function", swPackage+"/mirror"); err != nil {
		t.Fatalf("delete function: %v", err)
	}
	b := mustPut(t, ds, owner, substrate.PutInput{Kind: swPackage + "/widget", Properties: map[string]any{"name": "b"}})
	process(t, ops)
	if _, err := ds.Get(ctx, taskType, "t-"+b.ID); err == nil {
		t.Fatal("a trigger with no callable delivered")
	}
	st := statusOf(t, ops, triggerID)
	if st.Error == "" || st.Lag == 0 {
		t.Fatalf("callable-less trigger status: %+v", st)
	}

	// Reinstall: the trigger resumes from where it stood — the interim
	// change delivers late, not lost.
	if _, err := sa.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.FunctionManifest(swPackage, "mirror", fnData),
	}); err != nil {
		t.Fatalf("reinstall: %v", err)
	}
	process(t, ops)
	if _, err := ds.Get(ctx, taskType, "t-"+b.ID); err != nil {
		t.Fatalf("the outage's backlog did not deliver after reinstall: %v", err)
	}
	if st := statusOf(t, ops, triggerID); st.Lag != 0 || st.Error != "" {
		t.Fatalf("post-reinstall status: %+v", st)
	}
}

// An actor's id does not embed its authority (alone among the schema kinds), so a
// document may CLAIM any authority. Redeclaring a shipped actor into an installed
// authority must refuse — otherwise the projection overwrites the shipped row and
// the write silently unwinds at the next boot.
func TestBuiltinActorRowsRefuseRedeclaration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	sa := applier(t, ds)

	if _, err := sa.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(swPackage, 0),
	}); err != nil {
		t.Fatalf("apply authority: %v", err)
	}

	// Through the batch verb.
	_, err := sa.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.ActorManifest(swPackage, "api"),
	})
	wantErr(t, err, substrate.ErrForbidden, "batch redeclares a shipped actor")

	// Through the generic put.
	_, err = ds.Put(ctx, owner, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/actor", ID: "substrate",
		Properties: map[string]any{"authority": swAuthority, "package": "widgets"},
	})
	wantErr(t, err, substrate.ErrForbidden, "put hijacks a shipped actor")

	// Through a patch that retargets the row's authority.
	_, err = ds.Patch(ctx, owner, "substrate.reamde.dev/core/actor", "api", substrate.PatchInput{
		Properties: map[string]any{"authority": swAuthority, "package": "widgets"},
	})
	wantErr(t, err, substrate.ErrForbidden, "patch retargets a shipped actor")

	// The shipped rows are untouched.
	for _, id := range []string{"api", "console", "substratectl", "substrate"} {
		row := mustGet(t, ds, "substrate.reamde.dev/core/actor", id)
		if row.Properties["authority"] != "substrate.reamde.dev" || row.Properties["package"] != "core" ||
			row.Properties["source"] != "builtin" {
			t.Fatalf("shipped actor row %s = %v", id, row.Properties)
		}
	}
}

// The seed rule: a shipped authority the tree no longer declares
// STAYS in a repository that already holds it. The v0 boot re-projected the
// embedded tree and pruned every row it no longer declared; that is deleted,
// because the repository's own changelog is the truth and nothing rewrites it from
// outside.
func TestOpenNeverPrunesShippedRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	open := func() substrate.Service {
		svc, err := engine.Open(ctx, dsn, engine.WithCredentialKey(engine.TestCredentialKey),
			engine.WithKindsDir("../../kinds/substrate.reamde.dev/core"))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		return svc
	}
	svc := open()
	if _, err := svc.CreateRepository(ctx, "geoah", "geoah.example.com"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	_ = svc.Close()

	// The fixture: rows a PREVIOUS binary's shipped tree seeded, for an authority
	// today's tree no longer declares. Typed, like any row this dialect stores —
	// a `definition` blob would be refused at the open by the dialect gate, which
	// is a different question from pruning.
	raw, err := engine.OpenScopedDB(dsn, testdb.RepositoryID(t, dsn, "geoah"), engine.RoleApp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	for _, row := range []struct{ id, typ, props string }{
		{
			"ghost.substrate.reamde.dev", "substrate.reamde.dev/core/authority",
			`{"name": "ghost.substrate.reamde.dev", "version": 1, "source": "builtin", "actors": []}`,
		},
		{
			"ghost.substrate.reamde.dev/ghost/color", "substrate.reamde.dev/core/propertytype",
			`{"name": "color", "authority": "ghost.substrate.reamde.dev", "package": "ghost", "base": "string", "version": 1}`,
		},
	} {
		if _, err := raw.ExecContext(ctx, `
			INSERT INTO records (id, kind, title, props) VALUES ($1, $2, $1, $3::jsonb)`,
			row.id, row.typ, row.props); err != nil {
			t.Fatalf("seed stale row %s: %v", row.id, err)
		}
	}

	svc2 := open()
	t.Cleanup(func() { _ = svc2.Close() })
	ds2, err := svc2.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	for id, typ := range map[string]string{
		"ghost.substrate.reamde.dev":             "substrate.reamde.dev/core/authority",
		"ghost.substrate.reamde.dev/ghost/color": "substrate.reamde.dev/core/propertytype",
	} {
		row := mustGet(t, ds2, typ, id)
		if row.DeletedAt != nil {
			t.Fatalf("open pruned the shipped row %s — re-assert-and-prune is dead (RB-8)", id)
		}
	}
	// And the authority is LIVE vocabulary, rebuilt from the rows like every other.
	if _, err := ds2.KindByRef(ctx, "ghost.substrate.reamde.dev/ghost/color"); err == nil {
		t.Log("the ghost authority's property type is not a record type; nothing to assert")
	}
	types, err := ds2.Kinds(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(types) == 0 {
		t.Fatal("the repository opened with no vocabulary at all")
	}
}

// swInstallPackage is the closure the install below admits, kept apart from
// swPackage so the whole-authority replace never prunes the widget kind its
// data documents are written against.
const swInstallPackage = "installer.example.substrate.reamde.dev/installer"

// The projection resolves a declaration row's kind against the CANDIDATE when
// this projection is what installs that kind's declaration, because the row is
// then newer than the stored declaration (engine.projectionKind). Every other
// write, that pass's own rows of a kind it leaves alone included, is held to the
// declarations the repository STORES: an ordinary put and an install's data
// documents are validated against the live registry, so an undeclared property
// is refused at both doors.
func TestCandidateResolutionIsTheProjectionsAlone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(swPackage, 0),
		swTypeDoc("widget", "widgets", map[string]any{"name": map[string]any{"type": "string"}}),
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	t.Run("an ordinary put", func(t *testing.T) {
		mustPut(t, ds, owner, substrate.PutInput{
			Kind: swPackage + "/widget", ID: "declared",
			Properties: map[string]any{"name": "ok"},
		})
		_, err := ds.Put(ctx, owner, substrate.PutInput{
			Kind: swPackage + "/widget", ID: "undeclared",
			Properties: map[string]any{"name": "ok", "color": "red"},
		})
		wantErr(t, err, substrate.ErrValidation, "a put carrying an undeclared property")
		if !strings.Contains(fmt.Sprint(err), "not declared") {
			t.Fatalf("the refusal must name the undeclared property: %v", err)
		}
		if _, err := ds.Get(ctx, swPackage+"/widget", "undeclared"); err == nil {
			t.Fatal("the refused put landed a row")
		}
	})

	t.Run("an install's data documents", func(t *testing.T) {
		inst, ok := ds.(substrate.BundleInstaller)
		if !ok {
			t.Fatal("dataset does not implement the closure-install seam")
		}
		closure := []map[string]any{vocabulary.PackageManifest(swInstallPackage, 0)}

		_, err := inst.InstallBundleClosure(ctx, owner, closure, []substrate.PutInput{{
			Kind: swPackage + "/widget", ID: "from-install",
			Properties: map[string]any{"name": "ok", "color": "red"},
		}})
		wantErr(t, err, substrate.ErrValidation, "an install's data document carrying an undeclared property")
		// The whole install rolled back with it: no data row, and no closure.
		if _, err := ds.Get(ctx, swPackage+"/widget", "from-install"); err == nil {
			t.Fatal("the refused data document landed a row")
		}
		if _, err := ds.Get(ctx, "substrate.reamde.dev/core/package", swInstallPackage); err == nil {
			t.Fatal("a refused data document must roll the closure back with it")
		}

		// The same install with a declared property admits both halves.
		if _, err := inst.InstallBundleClosure(ctx, owner, closure, []substrate.PutInput{{
			Kind: swPackage + "/widget", ID: "from-install",
			Properties: map[string]any{"name": "ok"},
		}}); err != nil {
			t.Fatalf("install: %v", err)
		}
		mustGet(t, ds, swPackage+"/widget", "from-install")
		mustGet(t, ds, "substrate.reamde.dev/core/package", swInstallPackage)
	})
}

// vocabularyRemover is the batch seam that upserts AND removes in one admission
// unit (vocabularyplant_internal_test.go): the shape no public door offers, and
// the one that can write a declaration row of a category the same batch drops.
type vocabularyRemover interface {
	ApplyVocabularyWithRemoval(ctx context.Context, actor substrate.Actor,
		docs []map[string]any, removeShort, removeID, removeAuthority string) error
}

const dkPackage = "dropkind.example.substrate.reamde.dev/dropkind"

// dkDocs is the bundle's whole closure: header, actor, the bundle listing what
// it installs, and the members. A re-apply REPLACES the authority, so a member
// left out of this list is a member the upgrade removes.
func dkDocs(members ...map[string]any) []map[string]any {
	installs := make([]any, 0, len(members))
	for _, m := range members {
		meta, _ := m["metadata"].(map[string]any)
		installs = append(installs, meta["id"])
	}
	return append([]map[string]any{
		vocabulary.PackageManifest(dkPackage, 0),
		vocabulary.ActorManifest(dkPackage, vocabulary.PackageActor(dkPackage)),
		vocabulary.BundleManifest(dkPackage, map[string]any{
			"description": "the drop-kind bundle",
			"installs":    installs,
		}),
	}, members...)
}

func dkKindDoc(singular, plural string) map[string]any {
	return vocabulary.KindManifest(dkPackage,
		map[string]any{"singular": singular, "plural": plural},
		map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}})
}

// NO TRANSACTION PUBLISHES A VOCABULARY WITHOUT A KIND IT JUST WROTE ROWS OF.
//
// The dropped-kind guard counts live rows as the transaction FINDS them, and two
// writes after it create rows: an install's data documents (put against the
// still-live pre-commit registry) and the projection's own declaration rows. A
// bundle upgrade that removes a kind and ships a data document of that same kind
// would pass the opening count, create the row, prune the declaration and
// publish a registry that cannot resolve it. The guard runs again with every
// write behind it, so the row and the removal cannot both land.
func TestBundleUpgradeRefusesARowOfTheKindItRemoves(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newCoreDataset(t)
	inst, ok := ds.(substrate.BundleInstaller)
	if !ok {
		t.Fatal("dataset does not implement the closure-install seam")
	}
	actor := substrate.BundleActor(vocabulary.SplitPackageRef(dkPackage))

	if _, err := inst.InstallBundleClosure(ctx, actor,
		dkDocs(dkKindDoc("widget", "widgets"), dkKindDoc("gadget", "gadgets")), nil); err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := ds.KindByRef(ctx, dkPackage+"/widget"); err != nil {
		t.Fatalf("the installed widget kind: %v", err)
	}

	// The upgrade drops `widget` (no live rows, so the opening count admits it)
	// and carries a widget record in the same package.
	_, err := inst.InstallBundleClosure(ctx, actor, dkDocs(dkKindDoc("gadget", "gadgets")),
		[]substrate.PutInput{{
			Kind: dkPackage + "/widget", ID: "late", Properties: map[string]any{"name": "late"},
		}})
	wantErr(t, err, substrate.ErrGuard, "an upgrade that writes a row of the kind it removes")
	if !strings.Contains(err.Error(), dkPackage+"/widget") {
		t.Fatalf("the refusal must name the kind: %v", err)
	}
	// Whole rollback: the kind is still live, and the row it would have stranded
	// is not there.
	if _, err := ds.KindByRef(ctx, dkPackage+"/widget"); err != nil {
		t.Fatalf("the refused upgrade removed the kind anyway: %v", err)
	}
	if _, err := ds.Get(ctx, dkPackage+"/widget", "late"); err == nil {
		t.Fatal("the refused upgrade landed the row")
	}

	// And the same upgrade WITHOUT the row admits: this is refuse-while-stranded,
	// not refuse-forever.
	if _, err := inst.InstallBundleClosure(ctx, actor, dkDocs(dkKindDoc("gadget", "gadgets")), nil); err != nil {
		t.Fatalf("an upgrade that removes an unused kind must admit: %v", err)
	}
	if _, err := ds.KindByRef(ctx, dkPackage+"/widget"); err == nil {
		t.Fatal("the upgrade did not remove the unused kind")
	}
	if _, err := ds.KindByRef(ctx, dkPackage+"/gadget"); err != nil {
		t.Fatalf("the upgrade removed a member it keeps: %v", err)
	}
}

// The projection's own converse: a batch that stops declaring one of the
// META-kinds while projecting a declaration row OF that category. The row is
// validated against the retained live definition and lands, the prune then takes
// the definition, and the publish would leave the row unresolvable. The same
// final guard catches it.
func TestVocabularyApplyRefusesADeclarationRowOfTheMetaKindItRemoves(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newCoreDataset(t)
	const metaKind = "substrate.reamde.dev/core/propertytype"

	// A core-only repository declares no property types at all, so the opening
	// count is zero and the removal looks free.
	page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{metaKind}}, First: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 0 {
		t.Skipf("the seed now ships %d property types; this fixture needs a category with none", len(page.Records))
	}

	rm, ok := ds.(vocabularyRemover)
	if !ok {
		t.Fatal("dataset does not implement the upsert-and-remove seam")
	}
	// One batch: a property type declared into a new authority (its row's kind is
	// the meta-kind), and core's declaration of that meta-kind removed.
	err = rm.ApplyVocabularyWithRemoval(ctx, substrate.ActorSystem, []map[string]any{
		vocabulary.PackageManifest("mine.example.com/mine", 0),
		{
			"kind":     metaKind,
			"metadata": map[string]any{"id": "mine.example.com/mine/color"},
			"data":     map[string]any{"authority": "mine.example.com", "package": "mine", "base": "string"},
		},
	}, vocabulary.DocKind, metaKind, "substrate.reamde.dev/core")
	wantErr(t, err, substrate.ErrGuard, "a batch that projects a row of the meta-kind it removes")
	if !strings.Contains(err.Error(), metaKind) {
		t.Fatalf("the refusal must name the kind: %v", err)
	}
	// Nothing landed: the meta-kind is still live vocabulary and the row is not
	// there.
	if _, err := ds.KindByRef(ctx, metaKind); err != nil {
		t.Fatalf("the refused batch removed the meta-kind anyway: %v", err)
	}
	if _, err := ds.Get(ctx, metaKind, "mine.example.com/mine/color"); err == nil {
		t.Fatal("the refused batch landed the declaration row")
	}
}
