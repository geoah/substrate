package engine_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

func TestRepositoryProvisioningAndProjections(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	open := func() substrate.Service {
		svc, err := engine.Open(ctx, dsn, engine.WithDataRoot(t.TempDir()), engine.WithCredentialKey(engine.TestCredentialKey),
			engine.WithKindsDir("../../kinds/substrate.reamde.dev/core"))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		return svc
	}
	svc := open()
	info, err := svc.CreateRepository(ctx, "geoah", "geoah.example.com")
	if err != nil {
		t.Fatalf("create repository: %v", err)
	}
	if info.ID == "" || info.Name != "geoah" {
		t.Fatalf("repository = %+v", info)
	}
	if _, err := svc.CreateRepository(ctx, "geoah", "geoah.example.com"); err == nil {
		t.Fatal("expected a duplicate-repository error")
	}
	if _, err := svc.CreateRepository(ctx, "Bad Name", "bad.example.com"); err == nil {
		t.Fatal("expected a repository-name validation error")
	}

	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	importVocabulary(t, ds)
	// The control plane is a TABLE now, not a repository: one row per user,
	// carrying the username and the day they arrived and nothing else — the
	// auth material is sealed rows behind the credential record.
	repos, err := svc.Repositories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].Name != "geoah" || repos[0].ID != info.ID {
		t.Fatalf("repositories = %+v", repos)
	}
	self, err := ds.Get(ctx, "substrate.reamde.dev/core/repository", info.ID)
	if err != nil {
		t.Fatalf("the repository's own description: %v", err)
	}
	for _, gone := range []string{"totpSecret", "totpStep", "totpFails", "totpLockedUntil"} {
		if _, leaked := self.Properties[gone]; leaked {
			t.Fatalf("the bootstrap credential survived as %q: %v", gone, self.Properties)
		}
	}
	// Type and actor projections are records in every dataset.
	types, err := ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{Kinds: []string{"substrate.reamde.dev/core/kind"}}, First: 200,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(types.Records) < 20 {
		t.Fatalf("type projections = %d", len(types.Records))
	}
	msgType, err := ds.Get(ctx, "substrate.reamde.dev/core/kind", "samples.substrate.reamde.dev/messaging/conversationmessage")
	if err != nil {
		t.Fatalf("type record by identity: %v", err)
	}
	names, _ := msgType.Properties["names"].(map[string]any)
	// A sample package INSTALLS: only the seeded core package is builtin.
	if names["singular"] != "conversationmessage" || msgType.Properties["source"] != "installed" {
		t.Fatalf("kind projection = %v", msgType.Properties)
	}
	// The row's properties ARE the declaration: no blob beside them.
	if msgType.Properties["properties"] == nil || msgType.Properties["definition"] != nil {
		t.Fatalf("kind projection is not the typed shape: %v", msgType.Properties)
	}
	actors, err := ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{Kinds: []string{"substrate.reamde.dev/core/actor"}}, First: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	// The shipped set is core's four: the three doors (api, console, substratectl)
	// and the engine's own hand. A connector's actor is minted at dispatch
	// (`connector:<name>`) and is never declared.
	if len(actors.Records) != 4 {
		t.Fatalf("actor projections = %v", ids(actors.Records))
	}
	ti, err := ds.KindByRef(ctx, "samples.substrate.reamde.dev/calendar/calendarevent")
	if err != nil || ti.Identity != "samples.substrate.reamde.dev/calendar/calendarevent" {
		t.Fatalf("KindByRef = %+v %v", ti, err)
	}

	// Reconciliation is a no-op on restart.
	seq := maxSeq(t, ds)
	_ = svc.Close()
	svc2 := open()
	t.Cleanup(func() { _ = svc2.Close() })
	ds2, err := svc2.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	if got := maxSeq(t, ds2); got != seq {
		rows := changesSince(t, ds2, seq)
		t.Fatalf("restart re-wrote projections: %+v", rows)
	}

	// Tokens: mint once, authenticate, revoke. The secret carries NO username
	// segment — the hash lookup is what finds the repository.
	tok, secret, err := ds2.MintToken(ctx, "cli", nil)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if !strings.HasPrefix(secret, "substrate_tok_") || strings.Count(secret, "_") != 2 {
		t.Fatalf("secret = %q, want substrate_tok_<hex>", secret)
	}
	if strings.Contains(secret, "geoah") {
		t.Fatalf("secret leaks the username: %q", secret)
	}
	authDS, info2, err := svc2.Authenticate(ctx, secret)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if info2.ID != tok.ID || authDS.Repository().Name != "geoah" {
		t.Fatalf("authenticated as %+v / %s", info2, authDS.Repository().Name)
	}
	if _, _, err := svc2.Authenticate(ctx, "substrate_tok_deadbeef"); err == nil {
		t.Fatal("expected an auth error")
	}
	if _, _, err := svc2.Authenticate(ctx, "not-a-token"); err == nil {
		t.Fatal("expected an auth error")
	}
	// Revoking IS deleting the record, through the ordinary surface.
	if _, err := ds2.Delete(ctx, owner, "substrate.reamde.dev/core/token", tok.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, _, err := svc2.Authenticate(ctx, secret); err == nil {
		t.Fatal("a revoked token should not authenticate")
	}
}

func TestRepositoryDatasetIsolation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)
	for _, name := range []string{"alpha", "beta"} {
		if _, err := svc.CreateRepository(ctx, name, name+".example.com"); err != nil {
			t.Fatal(err)
		}
	}
	a, err := svc.Dataset(ctx, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.Dataset(ctx, "beta")
	if err != nil {
		t.Fatal(err)
	}
	importVocabulary(t, a, "tasks")
	task := mustPut(t, a, owner, substrate.PutInput{Kind: "task", Properties: map[string]any{"name": "alpha only"}})
	if _, err := b.Get(ctx, task.Kind, task.ID); err == nil {
		t.Fatal("datasets must be isolated")
	}
	if _, err := svc.Dataset(ctx, "nosuch"); err == nil {
		t.Fatal("expected not found")
	}
}

// Schema rows persist the parsed definition only: no schema kind
// stores sourceYAML, authored or derived — the source view re-renders the
// definition. Rows written before the record converge through a one-time
// gated boot pass, and every boot after it writes nothing.
func TestSchemaRowsStoreNoSourceYAML(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	open := func() substrate.Service {
		svc, err := engine.Open(ctx, dsn, engine.WithDataRoot(t.TempDir()), engine.WithCredentialKey(engine.TestCredentialKey),
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
	// A builtin projection carries the parsed declaration and no verbatim text.
	req, err := ds.Get(ctx, "substrate.reamde.dev/core/kind", "substrate.reamde.dev/core/recordpatchrequest")
	if err != nil {
		t.Fatalf("kind projection: %v", err)
	}
	if req.Properties["properties"] == nil {
		t.Fatalf("kind projection lost its declaration: %v", req.Properties)
	}
	if _, has := req.Properties["sourceYAML"]; has {
		t.Fatalf("builtin projection stores sourceYAML (record 61 removed it): %v", req.Properties["sourceYAML"])
	}

	// An installed pkg's rows store none either.
	const pkg = "srcless.example.substrate.reamde.dev/srcless"
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.KindManifest(pkg,
			map[string]any{"singular": "widget", "plural": "widgets"},
			map[string]any{"properties": map[string]any{
				"label": map[string]any{"type": "string"},
			}}),
	}); err != nil {
		t.Fatalf("install pkg: %v", err)
	}
	widget := mustGet(t, ds, "substrate.reamde.dev/core/kind", pkg+"/widget")
	if _, has := widget.Properties["sourceYAML"]; has {
		t.Fatal("installed projection stores sourceYAML (record 61 removed it)")
	}

	// A generic write smuggling the retired property is REFUSED, not ignored: a
	// dropped edit is a write that reads as obeyed.
	_, err = ds.Put(ctx, owner, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/kind", ID: pkg + "/widget",
		Properties: map[string]any{
			"pkg": pkg, "names": widget.Properties["names"],
			"properties": widget.Properties["properties"],
			"sourceYAML": "# smuggled bytes",
		},
	})
	wantErr(t, err, substrate.ErrValidation, "retired property")
	if _, has := mustGet(t, ds, "substrate.reamde.dev/core/kind", pkg+"/widget").Properties["sourceYAML"]; has {
		t.Fatal("a write-supplied sourceYAML was stored")
	}
}

// The rest of the schema meta-model is projected too: the authority's own header,
// its capabilities and its custom property types, each with the text it was
// declared in. Without these the console can render
// `traits: [temporal(point)]` and `asin: {type: asin}` with nothing to
// look up — and after the vocabulary split, the capability a type binds almost
// never lives in the type's own authority.
func TestSchemaMetaModelProjections(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	// The shelf fixture brings the meta-model members the shipped vocabulary
	// no longer carries: custom property types and a record mapping.
	installShelf(t, ds)
	const shelf = enginetest.ShelfPackage

	pkg, err := ds.Get(ctx, "substrate.reamde.dev/core/package", "samples.substrate.reamde.dev/people")
	if err != nil {
		t.Fatalf("package projection: %v", err)
	}
	if pkg.Kind != "substrate.reamde.dev/core/package" {
		t.Fatalf("package projection type = %q", pkg.Kind)
	}
	// A sample package INSTALLS: only the seeded core package is builtin.
	if v, _ := vocabulary.VersionValue(pkg.Properties["version"]); v != 4 || pkg.Properties["source"] != "installed" {
		t.Fatalf("package projection = %v", pkg.Properties)
	}
	// A vocabulary package declares no actors of its own; core's four are the
	// shipped set, connector actors install with their connector.
	if actors, _ := pkg.Properties["actors"].([]any); len(actors) != 0 {
		t.Fatalf("package actors = %v", pkg.Properties["actors"])
	}
	core, err := ds.Get(ctx, "substrate.reamde.dev/core/package", "substrate.reamde.dev/core")
	if err != nil {
		t.Fatalf("core package projection: %v", err)
	}
	if actors, _ := core.Properties["actors"].([]any); len(actors) != 4 {
		t.Fatalf("core actors = %v", core.Properties["actors"])
	}
	// No schema row stores verbatim text: the parsed facts are
	// the row, and the source view re-renders the definition.
	if _, has := pkg.Properties["sourceYAML"]; has {
		t.Fatalf("package projection stores sourceYAML (record 61 removed it): %v", pkg.Properties)
	}

	capa, err := ds.Get(ctx, "substrate.reamde.dev/core/trait", "substrate.reamde.dev/core/temporal")
	if err != nil {
		t.Fatalf("capability projection: %v", err)
	}
	if capa.Kind != "substrate.reamde.dev/core/trait" ||
		capa.Properties["authority"] != "substrate.reamde.dev" || capa.Properties["package"] != "core" {
		t.Fatalf("capability projection = %v", capa.Properties)
	}
	if _, has := capa.Properties["sourceYAML"]; has {
		t.Fatalf("capability projection stores sourceYAML (record 61 removed it): %v", capa.Properties)
	}
	if _, ok := capa.Properties["oneOf"].([]any); !ok {
		t.Fatalf("the trait's variants are not the declared list: %v", capa.Properties)
	}

	dt, err := ds.Get(ctx, "substrate.reamde.dev/core/propertytype", shelf+"/asin")
	if err != nil {
		t.Fatalf("datatype projection: %v", err)
	}
	if dt.Kind != "substrate.reamde.dev/core/propertytype" || dt.Properties["base"] != "string" {
		t.Fatalf("datatype projection = %v", dt.Properties)
	}
	if _, has := dt.Properties["sourceYAML"]; has {
		t.Fatalf("datatype projection stores sourceYAML (record 61 removed it): %v", dt.Properties)
	}
	if !strings.Contains(fmt.Sprint(dt.Properties["pattern"]), "^B0") {
		t.Fatalf("the refinement's pattern = %v", dt.Properties)
	}

	// Mappings mirror like the rest of the meta-model: one
	// record per loaded mapping, reachable through the ordinary collection
	// machinery.
	mp, err := ds.Get(ctx, "substrate.reamde.dev/core/recordmapping", shelf+"/bookeditionwork")
	if err != nil {
		t.Fatalf("mapping projection: %v", err)
	}
	if mp.Kind != "substrate.reamde.dev/core/recordmapping" ||
		refPathValue(mp, "from") != vocabulary.RecordPath("substrate.reamde.dev/core/kind", shelf+"/bookedition") ||
		refPathValue(mp, "to") != vocabulary.RecordPath("substrate.reamde.dev/core/kind", shelf+"/book") ||
		mp.Properties["property"] != "work" {
		t.Fatalf("mapping projection = %v", mp.Properties)
	}
	// Mappings shed their sourceYAML first; record 61 extended
	// the removal to every schema kind. The projection writes an explicit
	// null so older rows converge, and null against absence is a no-op.
	if _, has := mp.Properties["sourceYAML"]; has {
		t.Fatalf("mapping projection carries sourceYAML; records 58/61 removed it")
	}
	if ti, err := ds.KindByRef(ctx, "substrate.reamde.dev/core/recordmapping"); err != nil ||
		ti.Identity != "substrate.reamde.dev/core/recordmapping" {
		t.Fatalf("recordmapping collection = %+v %v", ti, err)
	}
	page, err := ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{Kinds: []string{"substrate.reamde.dev/core/recordmapping"}}, First: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 1 || page.Records[0].ID != shelf+"/bookeditionwork" {
		t.Fatalf("mapping mirrors = %v", ids(page.Records))
	}

	// The meta-model rows are real records now (schema is records), but a
	// write reaches them only through admission: a shapeless put — no
	// identity, no declaration — is refused, never stored.
	for _, ty := range []string{
		"substrate.reamde.dev/core/package", "substrate.reamde.dev/core/trait",
		"substrate.reamde.dev/core/propertytype", "substrate.reamde.dev/core/recordmapping",
	} {
		if _, err := ds.Put(ctx, owner, substrate.PutInput{
			Kind: ty, Properties: map[string]any{"name": "evil"},
		}); err == nil {
			t.Fatalf("%s accepted a shapeless write", ty)
		}
	}
}

func TestWatchSignal(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, ds := newDataset(t)
	ch := ds.WatchSignal(ctx)

	task := mustPut(t, ds, owner, substrate.PutInput{Kind: "task", Properties: map[string]any{"name": "watch me"}})
	select {
	case seq := <-ch:
		if seq <= 0 {
			t.Fatalf("signal seq = %d", seq)
		}
		changes, err := ds.Changes(ctx, seq-1, substrate.ChangeFilter{}, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(changes) == 0 || changes[len(changes)-1].RecordID != task.ID {
			t.Fatalf("signal did not point at the write: %+v", changes)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no watch signal")
	}
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			// A late coalesced signal may arrive before the close.
			if _, ok := <-ch; ok {
				t.Fatal("channel should close when the context ends")
			}
		}
	case <-time.After(3 * time.Second):
		t.Fatal("channel did not close")
	}
}

func TestChangesFilters(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	task := mustPut(t, ds, engram, substrate.PutInput{
		Kind: "task", Properties: map[string]any{"name": "t", "status": "proposed"},
	})
	mustPatch(t, ds, owner, task.Kind, task.ID, substrate.PatchInput{Properties: map[string]any{"status": "open"}})

	byActor, err := ds.Changes(ctx, 0, substrate.ChangeFilter{Actors: []substrate.Actor{engram}}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(byActor) != 1 || byActor[0].Actor != engram {
		t.Fatalf("actor filter = %+v", byActor)
	}
	byType, err := ds.Changes(ctx, 0, substrate.ChangeFilter{
		Kinds: []string{"task"}, Ops: []substrate.Op{substrate.OpPatch},
	}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(byType) != 1 || byType[0].Op != substrate.OpPatch {
		t.Fatalf("type/op filter = %+v", byType)
	}
	byRecord, err := ds.Changes(ctx, 0, substrate.ChangeFilter{RecordID: task.ID}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(byRecord) != 2 {
		t.Fatalf("record filter = %+v", byRecord)
	}
	if byRecord[0].Seq >= byRecord[1].Seq {
		t.Fatal("changes must be seq-ordered")
	}
}
