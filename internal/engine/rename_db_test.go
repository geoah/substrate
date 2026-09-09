package engine_test

// A property rename is ordinary record writes (decision 0063): a declaration
// naming its previous name with `renamedFrom` is admitted while live records
// carry that name, and the apply moves each record's value inside the apply's
// transaction, one changelog entry per record. These hold what travels with
// the value (the manager row, the offer rows, the embed queue, a secret's
// sealed ref), what refuses the rename (a reader of the old name, a rename that
// also narrows), and that a fresh replay reproduces the renamed records.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	rnPackage = "rename.example.substrate.reamde.dev/rn"
	rnGizmo   = rnPackage + "/gizmo"
	rnSource  = rnPackage + "/gizmosource"
)

// rnBaseProps is the gizmo before the rename: a plain string, an embeddable
// text, a secret and a property the rename leaves alone.
func rnBaseProps() map[string]any {
	return map[string]any{
		"size":  map[string]any{"type": "string", "fts": true},
		"blurb": map[string]any{"type": "markdown", "embed": true},
		"token": map[string]any{"type": "secret"},
		"note":  map[string]any{"type": "string"},
	}
}

// rnRenamedProps is the gizmo after it: three properties renamed, one kept.
func rnRenamedProps() map[string]any {
	return map[string]any{
		"dimensions": map[string]any{"type": "string", "fts": true, "renamedFrom": "size"},
		"summary":    map[string]any{"type": "markdown", "embed": true, "renamedFrom": "blurb"},
		"apiKey":     map[string]any{"type": "secret", "renamedFrom": "token"},
		"note":       map[string]any{"type": "string"},
	}
}

// rnClosure is the package under test: the gizmo with the given properties
// and kind-level keys, a source kind describing a gizmo, and the mapping that
// projects the source's `size` onto the gizmo property `target` names. extra
// documents land in the same package.
func rnClosure(gizmoProps, gizmoData map[string]any, target string, extra ...map[string]any) []map[string]any {
	data := map[string]any{"properties": gizmoProps}
	for k, v := range gizmoData {
		data[k] = v
	}
	docs := []map[string]any{
		vocabulary.PackageManifest(rnPackage, 0),
		vocabulary.KindManifest(rnPackage, map[string]any{"singular": "gizmo"}, data),
		vocabulary.KindManifest(rnPackage, map[string]any{"singular": "gizmosource"},
			map[string]any{"properties": map[string]any{
				"size": map[string]any{"type": "string"},
				"gizmo": map[string]any{
					"type": "reference", "kind": rnGizmo,
					"required": true, "mustExist": true, "subject": true,
				},
			}}),
		vocabulary.MappingManifest(rnPackage, "gizmosourcegizmo", map[string]any{
			"from": rnSource, "to": rnGizmo, "property": "gizmo",
			"map": map[string]any{target: map[string]any{"path": "size"}},
		}),
	}
	return append(docs, extra...)
}

func rnApply(t *testing.T, ds substrate.Dataset, docs []map[string]any) error {
	t.Helper()
	_, err := applier(t, ds).ApplyVocabularyDocuments(context.Background(), owner, docs)
	return err
}

// rnStillDeclaresSize asserts a refused rename left the stored declaration and
// the record it names exactly as they were.
func rnStillDeclaresSize(t *testing.T, ds substrate.Dataset, id string) {
	t.Helper()
	ty, err := ds.Get(context.Background(), "substrate.reamde.dev/core/kind", rnGizmo)
	if err != nil {
		t.Fatalf("read the stored gizmo kind: %v", err)
	}
	declared, _ := ty.Properties["properties"].(map[string]any)
	if _, ok := declared["size"]; !ok {
		t.Fatalf("a refused rename moved the declaration: %v", declared)
	}
	if got := mustGet(t, ds, rnGizmo, id); got.Properties["size"] != "big" || got.Properties["dimensions"] != nil {
		t.Fatalf("a refused rename moved a value: %v", got.Properties)
	}
}

func TestRenameMovesTheValueOfEveryLiveRecord(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, dsn := newDatasetWithDSN(t)
	if err := rnApply(t, ds, rnClosure(rnBaseProps(), nil, "size")); err != nil {
		t.Fatalf("install the package: %v", err)
	}
	// Three live gizmos: one the owner wrote with every renamed property, one
	// without the renamed properties at all, and one a source record's
	// mapping wrote, whose `size` a connector's actor manages at the machine
	// tier and whose offer row names it.
	full := mustPut(t, ds, owner, substrate.PutInput{
		Kind: rnGizmo, Properties: map[string]any{
			"size": "big", "blurb": "alpha prose", "token": "s3cret", "note": "kept",
		},
	})
	bare := mustPut(t, ds, owner, substrate.PutInput{
		Kind: rnGizmo, Properties: map[string]any{"note": "only a note"},
	})
	lib := substrate.Actor("connector:library")
	source := mustPut(t, ds, lib, substrate.PutInput{
		Kind: rnSource, ID: "src:1", Properties: map[string]any{"size": "small"},
	})
	mappedKind, mappedID, _ := vocabulary.SplitRecordPath(refPathValue(mustGet(t, ds, source.Kind, source.ID), "gizmo"))
	if mappedKind != rnGizmo {
		t.Fatalf("the source's subject is a %s", mappedKind)
	}
	if got := mustGet(t, ds, rnGizmo, mappedID); got.Properties["size"] != "small" {
		t.Fatalf("the mapping did not project size: %v", got.Properties)
	}
	db := rawDB(t, dsn)
	var offers int
	if err := db.QueryRow(`SELECT count(*) FROM property_offers WHERE record_kind = $1 AND record_id = $2 AND property = 'size'`,
		rnGizmo, mappedID).Scan(&offers); err != nil || offers != 1 {
		t.Fatalf("offers for size before the rename = %d, %v", offers, err)
	}
	var libStamp time.Time
	if err := db.QueryRow(`SELECT updated_at FROM property_managers WHERE record_kind = $1 AND record_id = $2 AND property = 'size'`,
		rnGizmo, mappedID).Scan(&libStamp); err != nil {
		t.Fatalf("read the connector's manager stamp: %v", err)
	}
	// The blurb's vectors, as the worker would have stored them: one chunk of
	// the text, hashed, under the repository's embeddings provider.
	emb := newFakeEmbedServer(t)
	installEmbedProvider(t, ds, "vectors", emb.srv.URL, "text-embedding-3-small")
	blurbHash := sha256.Sum256([]byte("alpha prose"))
	if _, err := db.Exec(`INSERT INTO embeddings (repository, record_kind, record_id, property, chunk, text_hash, provider, model)
		VALUES ($1, $2, $3, 'blurb', 0, $4, 'vectors', 'text-embedding-3-small')`,
		testdb.Repository(t), rnGizmo, full.ID, hex.EncodeToString(blurbHash[:])); err != nil {
		t.Fatalf("store the blurb's vector row: %v", err)
	}
	// The value is indexed under the old name before the rename.
	if hits, err := searchHits(ds.Search(ctx, substrate.SearchInput{Q: "big", Mode: substrate.SearchLexical})); err != nil || len(hits) != 1 || hits[0].Record.ID != full.ID {
		t.Fatalf("lexical search for the fts value before the rename = %v, %v", hits, err)
	}
	head := maxSeq(t, ds)

	// THE RENAME, with the mapping rewired onto the new name in the same
	// apply: the one batch a mapping onto the renamed property can land in.
	if err := rnApply(t, ds, rnClosure(rnRenamedProps(), nil, "dimensions")); err != nil {
		t.Fatalf("the rename must land: %v", err)
	}

	// Every live record reads the value under the new name only.
	after := mustGet(t, ds, rnGizmo, full.ID)
	for old, renamed := range map[string]string{"size": "dimensions", "blurb": "summary", "token": "apiKey"} {
		if _, still := after.Properties[old]; still {
			t.Fatalf("%q is still on the record after the rename: %v", old, after.Properties)
		}
		if _, has := after.Properties[renamed]; !has {
			t.Fatalf("%q did not land on the record: %v", renamed, after.Properties)
		}
	}
	if after.Properties["dimensions"] != "big" || after.Properties["summary"] != "alpha prose" || after.Properties["note"] != "kept" {
		t.Fatalf("the values did not move intact: %v", after.Properties)
	}
	if after.Properties["apiKey"] == "s3cret" {
		t.Fatal("the secret reads back in plaintext")
	}
	if after.Version == full.Version {
		t.Fatal("a rewritten record must move its version")
	}
	// The rewrite was validated against the renamed declaration, so the row
	// carries its version (decision 0060), as a write through apply would.
	if want := kindVersion(t, ds, rnGizmo); after.KindVersion != want || after.KindVersion == full.KindVersion {
		t.Fatalf("a rewritten record carries kindVersion %d, want %d (was %d)", after.KindVersion, want, full.KindVersion)
	}
	if got := mustGet(t, ds, rnGizmo, bare.ID); got.Version != bare.Version || got.Properties["dimensions"] != nil {
		t.Fatalf("a record without the old property was rewritten: %+v", got)
	}
	if got := mustGet(t, ds, rnGizmo, mappedID); got.Properties["dimensions"] != "small" || got.Properties["size"] != nil {
		t.Fatalf("the mapped gizmo did not move: %v", got.Properties)
	}

	// The manager rows moved with their actor and tier: the owner's on the
	// record it wrote, the connector's at the machine tier on the mapped one.
	managerOf := func(id, property string) (actor, tier string) {
		t.Helper()
		err := db.QueryRow(`SELECT actor, tier FROM property_managers WHERE record_kind = $1 AND record_id = $2 AND property = $3`,
			rnGizmo, id, property).Scan(&actor, &tier)
		if err != nil {
			return "", ""
		}
		return actor, tier
	}
	if actor, tier := managerOf(full.ID, "dimensions"); actor != string(owner) || tier != string(substrate.TierOwner) {
		t.Fatalf("the owner's manager row did not move: %q at %q", actor, tier)
	}
	if actor, tier := managerOf(mappedID, "dimensions"); actor != string(lib) || tier != string(substrate.TierMachine) {
		t.Fatalf("the connector's manager row did not move: %q at %q", actor, tier)
	}
	// The row keeps the time its actor last had a change accepted: the rename
	// moved the row, it did not write the value.
	var movedStamp time.Time
	if err := db.QueryRow(`SELECT updated_at FROM property_managers WHERE record_kind = $1 AND record_id = $2 AND property = 'dimensions'`,
		rnGizmo, mappedID).Scan(&movedStamp); err != nil {
		t.Fatalf("read the moved manager stamp: %v", err)
	}
	if !movedStamp.Equal(libStamp) {
		t.Fatalf("the moved manager row is stamped %s, want the original %s", movedStamp, libStamp)
	}
	for _, id := range []string{full.ID, mappedID} {
		if actor, _ := managerOf(id, "size"); actor != "" {
			t.Fatalf("a manager row for the old name survives on %s: %q", id, actor)
		}
	}

	// The offer row and the embed queue follow the value.
	var offerProps []string
	rows, err := db.Query(`SELECT property FROM property_offers WHERE record_kind = $1 AND record_id = $2 ORDER BY property`, rnGizmo, mappedID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			t.Fatal(err)
		}
		offerProps = append(offerProps, p)
	}
	_ = rows.Close()
	if strings.Join(offerProps, ",") != "dimensions" {
		t.Fatalf("offers after the rename = %v, want dimensions alone", offerProps)
	}
	var queued []string
	rows, err = db.Query(`SELECT property FROM embed_queue WHERE record_kind = $1 AND record_id = $2 ORDER BY property`, rnGizmo, full.ID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			t.Fatal(err)
		}
		queued = append(queued, p)
	}
	_ = rows.Close()
	if strings.Join(queued, ",") != "summary" {
		t.Fatalf("embed queue after the rename = %v, want summary alone", queued)
	}
	// The vectors moved with the text: the same row, keyed under the new name,
	// and the worker finds every chunk's hash in place and buys nothing.
	var vectorProps []string
	rows, err = db.Query(`SELECT property || ':' || text_hash FROM embeddings WHERE record_kind = $1 AND record_id = $2 ORDER BY property`, rnGizmo, full.ID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			t.Fatal(err)
		}
		vectorProps = append(vectorProps, p)
	}
	_ = rows.Close()
	if strings.Join(vectorProps, ",") != "summary:"+hex.EncodeToString(blurbHash[:]) {
		t.Fatalf("embeddings after the rename = %v, want the blurb's row under summary alone", vectorProps)
	}
	if n, err := ds.ProcessEmbedQueue(ctx, 10); err != nil || n != 1 {
		t.Fatalf("draining the re-enqueued summary = %d, %v; want the one job applied", n, err)
	}
	if emb.calls != 0 {
		t.Fatalf("the drain bought %d embeddings for text whose vectors moved with it", emb.calls)
	}
	// `fts` follows the value: the rewritten row indexes it under the new
	// declaration at the fold, and the apply re-derives the kind's index
	// whole (reprojectFTS), so lexical search still finds it.
	if hits, err := searchHits(ds.Search(ctx, substrate.SearchInput{Q: "big", Mode: substrate.SearchLexical})); err != nil || len(hits) != 1 || hits[0].Record.ID != full.ID {
		t.Fatalf("lexical search for the renamed fts value = %v, %v", hits, err)
	}

	// One entry per renamed record, a patch that says what moved it.
	var entries int
	if err := db.QueryRow(`SELECT count(*) FROM changelog WHERE seq > $1 AND kind = $2 AND op = 'patch' AND payload->'renamed' IS NOT NULL`,
		head, rnGizmo).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if entries != 2 {
		t.Fatalf("the rename wrote %d record entries, want 2 (one per record carrying the old name)", entries)
	}

	// The record keeps working under the new names: the sealed ref moved with
	// the secret, so a later write carries it, and the rewired mapping
	// projects the source onto the new name.
	mustPatch(t, ds, owner, rnGizmo, full.ID, substrate.PatchInput{Properties: map[string]any{"note": "patched"}})
	if got := mustGet(t, ds, rnGizmo, full.ID); got.Properties["apiKey"] == nil || got.Properties["note"] != "patched" {
		t.Fatalf("the record does not write under the renamed declaration: %v", got.Properties)
	}
	mustPut(t, ds, lib, substrate.PutInput{Kind: rnSource, ID: "src:1", Properties: map[string]any{"size": "medium"}})
	if got := mustGet(t, ds, rnGizmo, mappedID); got.Properties["dimensions"] != "medium" {
		t.Fatalf("the rewired mapping did not project onto the new name: %v", got.Properties)
	}
	if _, err := ds.Put(ctx, owner, substrate.PutInput{Kind: rnGizmo, Properties: map[string]any{"size": "x"}}); err == nil {
		t.Fatal("the old name must be undeclared after the rename")
	}

	// A fresh replay reproduces the renamed records: the rename is values in
	// the changelog, never a fold-time conversion.
	before := foldOf(t, ds)
	rb, ok := svc.(rebuilder)
	if !ok {
		t.Fatal("the service cannot rebuild a repository")
	}
	if _, err := rb.RebuildRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if got := foldOf(t, ds); string(got) != string(before) {
		t.Fatalf("the rebuilt fold is not the fold\n%s", firstDifference(before, got))
	}
}

// rnViewers are two kinds whose displayTemplate reads a gizmo property through
// a reference: one pinned at the gizmo kind, one pinned at nothing, which a
// stored value may still point at a gizmo. pinned and loose name the property
// each template reads.
func rnViewers(pinned, loose string) []map[string]any {
	return []map[string]any{
		vocabulary.KindManifest(rnPackage, map[string]any{"singular": "viewer"},
			map[string]any{
				"displayTemplate": "{gizmo." + pinned + "}",
				"properties": map[string]any{
					"gizmo": map[string]any{"type": "reference", "kind": rnGizmo},
				},
			}),
		vocabulary.KindManifest(rnPackage, map[string]any{"singular": "looseviewer"},
			map[string]any{
				"displayTemplate": "{subject." + loose + "}",
				"properties": map[string]any{
					"subject": map[string]any{"type": "reference"},
				},
			}),
	}
}

// A rename is refused while anything the candidate cannot rewrite still reads
// the old name: a mapping onto it, the kind's own displayTemplate, and another
// kind's displayTemplate reading through a reference that can resolve to the
// kind, pinned at it or not.
func TestRenameRefusesAReaderOfTheOldName(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	if err := rnApply(t, ds, rnClosure(rnBaseProps(), nil, "size", rnViewers("size", "size")...)); err != nil {
		t.Fatalf("install the package: %v", err)
	}
	full := mustPut(t, ds, owner, substrate.PutInput{
		Kind: rnGizmo, Properties: map[string]any{"size": "big"},
	})
	refused := func(t *testing.T, err error, fragment string) {
		t.Helper()
		if err == nil {
			t.Fatal("a rename with a reader of the old name must refuse")
		}
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("the refusal must name %q, got: %v", fragment, err)
		}
		rnStillDeclaresSize(t, ds, full.ID)
	}

	t.Run("a template reads through a reference pinned at the kind", func(t *testing.T) {
		refused(t, rnApply(t, ds, rnClosure(rnRenamedProps(), nil, "dimensions", rnViewers("size", "dimensions")...)),
			`kind `+rnPackage+`/viewer: displayTemplate {gizmo.size} reads property "size" of `+rnGizmo+`, which this apply renames to "dimensions"; rewrite the template first`)
	})
	t.Run("a template reads through a reference pinned at nothing", func(t *testing.T) {
		refused(t, rnApply(t, ds, rnClosure(rnRenamedProps(), nil, "dimensions", rnViewers("dimensions", "size")...)),
			`kind `+rnPackage+`/looseviewer: displayTemplate {subject.size} reads property "size" of `+rnGizmo)
	})
	// Both templates move to the new name, so only the mapping and the kind's
	// own template can refuse below.
	renamedViewers := rnViewers("dimensions", "dimensions")
	t.Run("a mapping still maps onto the old name", func(t *testing.T) {
		refused(t, rnApply(t, ds, rnClosure(rnRenamedProps(), nil, "size", renamedViewers...)),
			`declares no property "size"`)
	})
	t.Run("the kind's own displayTemplate names the old name", func(t *testing.T) {
		refused(t, rnApply(t, ds, rnClosure(rnRenamedProps(), map[string]any{"displayTemplate": "{size}"}, "dimensions", renamedViewers...)),
			`displayTemplate: {size}: gizmo declares no property "size"`)
	})
	// With every reader rewritten the same rename lands, and both templates
	// render the renamed property through their reference.
	if err := rnApply(t, ds, rnClosure(rnRenamedProps(), map[string]any{"displayTemplate": "{dimensions}"}, "dimensions", renamedViewers...)); err != nil {
		t.Fatalf("the rename must land once nothing reads the old name: %v", err)
	}
	if got := mustGet(t, ds, rnGizmo, full.ID); got.Title != "big" {
		t.Fatalf("the title did not re-render under the renamed template: %q", got.Title)
	}
	for _, kind := range []string{rnPackage + "/viewer", rnPackage + "/looseviewer"} {
		prop := "gizmo"
		if strings.HasSuffix(kind, "looseviewer") {
			prop = "subject"
		}
		v := mustPut(t, ds, owner, substrate.PutInput{
			Kind: kind, Properties: map[string]any{prop: rnGizmo + "/" + full.ID},
		})
		if v.Title != "big" {
			t.Fatalf("%s's template does not read the renamed property: %q", kind, v.Title)
		}
	}
}

// A rename takes a name the stored kind does not declare: renaming `size` onto
// an existing `dimensions` would overwrite every record's `dimensions` with its
// `size` and collide the manager, offer and embedding rows keyed on the two
// names. The loader sees one document at a time, so admission refuses it.
func TestRenameRefusesADestinationTheStoredKindDeclares(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	props := rnBaseProps()
	props["dimensions"] = map[string]any{"type": "string"}
	if err := rnApply(t, ds, rnClosure(props, nil, "note")); err != nil {
		t.Fatalf("install the package: %v", err)
	}
	both := mustPut(t, ds, owner, substrate.PutInput{
		Kind: rnGizmo, Properties: map[string]any{"size": "big", "dimensions": "wide"},
	})
	err := rnApply(t, ds, rnClosure(rnRenamedProps(), nil, "note"))
	wantNarrowingGuard(t, err,
		`property "size" renamed to "dimensions", which the stored declaration already declares`)
	got := mustGet(t, ds, rnGizmo, both.ID)
	if got.Properties["size"] != "big" || got.Properties["dimensions"] != "wide" || got.Version != both.Version {
		t.Fatalf("a refused rename touched the record: %+v", got)
	}
}

// The destination must be empty on every LIVE record, not only undeclared: a
// record tombstoned while `dimensions` was declared, and restored after the
// declaration dropped it, carries the value undeclared, and the move would
// replace it. The guard counts, as every narrowing does.
func TestRenameRefusesADestinationALiveRecordCarries(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	// No mapping in this package: a mapped target's ids are server-assigned,
	// and the restore below puts at the record's own id.
	closure := func(props map[string]any) []map[string]any {
		return []map[string]any{
			vocabulary.PackageManifest(rnPackage, 0),
			vocabulary.KindManifest(rnPackage, map[string]any{"singular": "gizmo"},
				map[string]any{"properties": props}),
		}
	}
	props := rnBaseProps()
	props["dimensions"] = map[string]any{"type": "string"}
	if err := rnApply(t, ds, closure(props)); err != nil {
		t.Fatalf("install the package: %v", err)
	}
	both := mustPut(t, ds, owner, substrate.PutInput{
		Kind: rnGizmo, ID: "both", Properties: map[string]any{"size": "big", "dimensions": "wide"},
	})
	if _, err := ds.Delete(ctx, owner, rnGizmo, both.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("tombstone the record: %v", err)
	}
	// A tombstone is not counted, so the drop lands; the restore brings the
	// value back under a name the kind no longer declares.
	if err := rnApply(t, ds, closure(rnBaseProps())); err != nil {
		t.Fatalf("dropping dimensions with the only holder tombstoned must land: %v", err)
	}
	restored := mustPut(t, ds, owner, substrate.PutInput{
		Kind: rnGizmo, ID: both.ID, Properties: map[string]any{"note": "back"},
	})
	if restored.Properties["dimensions"] != "wide" {
		t.Fatalf("the restore did not revive the undeclared value the test needs: %v", restored.Properties)
	}

	err := rnApply(t, ds, closure(rnRenamedProps()))
	wantNarrowingGuard(t, err,
		`property "size" renamed to "dimensions" while 1 live records already carry a value under "dimensions"`)
	got := mustGet(t, ds, rnGizmo, both.ID)
	if got.Properties["size"] != "big" || got.Properties["dimensions"] != "wide" || got.Version != restored.Version {
		t.Fatalf("a refused rename touched the record: %+v", got)
	}

	// Cleared on the record, the same rename lands and the value moves.
	mustPatch(t, ds, owner, rnGizmo, both.ID, substrate.PatchInput{Properties: map[string]any{"dimensions": nil}})
	if err := rnApply(t, ds, closure(rnRenamedProps())); err != nil {
		t.Fatalf("the rename must land once no live record carries the destination: %v", err)
	}
	if got := mustGet(t, ds, rnGizmo, both.ID); got.Properties["dimensions"] != "big" {
		t.Fatalf("the value did not move: %v", got.Properties)
	}
}

// A rename moves values, nothing else: whatever else the new declaration
// changes is classified against the old one and refused with the count under
// the name the rows still carry.
func TestRenameThatAlsoNarrowsRefusesWithTheCount(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	// The mapping projects onto `note` here, the property the rename leaves
	// alone: a mapping onto the retyped property would refuse at the candidate
	// compile (a map path type-checks against both ends), ahead of the guard
	// under test.
	if err := rnApply(t, ds, rnClosure(rnBaseProps(), nil, "note")); err != nil {
		t.Fatalf("install the package: %v", err)
	}
	full := mustPut(t, ds, owner, substrate.PutInput{
		Kind: rnGizmo, Properties: map[string]any{"size": "big"},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: rnGizmo, Properties: map[string]any{"note": "no size"},
	})

	t.Run("retyped", func(t *testing.T) {
		props := rnRenamedProps()
		props["dimensions"] = map[string]any{"type": "int", "renamedFrom": "size"}
		wantNarrowingGuard(t, rnApply(t, ds, rnClosure(props, nil, "note")),
			`property "size" changes kind string → int`, "1 live records")
		rnStillDeclaresSize(t, ds, full.ID)
	})
	t.Run("made required", func(t *testing.T) {
		props := rnRenamedProps()
		props["dimensions"] = map[string]any{"type": "string", "required": true, "renamedFrom": "size"}
		wantNarrowingGuard(t, rnApply(t, ds, rnClosure(props, nil, "note")),
			`property "size" becomes required`, "1 live records")
		rnStillDeclaresSize(t, ds, full.ID)
	})
	t.Run("retyped and made required", func(t *testing.T) {
		// The retype strands the record holding a string, and `required`
		// strands the record lacking the property: both are counted, so the
		// record no non-integer count would see is not left invalid.
		props := rnRenamedProps()
		props["dimensions"] = map[string]any{"type": "int", "required": true, "renamedFrom": "size"}
		wantNarrowingGuard(t, rnApply(t, ds, rnClosure(props, nil, "note")),
			`property "size" changes kind string → int`, `property "size" becomes required`, "1 live records")
		rnStillDeclaresSize(t, ds, full.ID)
	})
	t.Run("the same shape lands", func(t *testing.T) {
		if err := rnApply(t, ds, rnClosure(rnRenamedProps(), nil, "note")); err != nil {
			t.Fatalf("a rename that narrows nothing must land: %v", err)
		}
		if got := mustGet(t, ds, rnGizmo, full.ID); got.Properties["dimensions"] != "big" {
			t.Fatalf("the value did not move: %v", got.Properties)
		}
	})
}
