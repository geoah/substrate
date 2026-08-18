package engine_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// newDatasetWithDB provisions a repository and also hands back a raw connection to
// its schema, for assertions about what is actually stored under a redaction.
func newDatasetWithDB(t *testing.T, opts ...engine.Option) (substrate.Dataset, *sql.DB, string) {
	t.Helper()
	dsn := testdb.NewSchema(t)
	ctx := context.Background()
	all := []engine.Option{
		engine.WithKindsDir("../../kinds/core.substrate.reamde.dev"),
		engine.WithCredentialKey(engine.TestCredentialKey),
	}
	all = append(all, opts...)
	svc, err := engine.Open(ctx, dsn, all...)
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if _, err := svc.CreateRepository(ctx, "geoah"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	importVocabulary(t, ds)
	raw, err := engine.OpenScopedDB(dsn, testdb.RepositoryID(t, dsn, "geoah"), engine.RoleApp)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	return ds, raw, dsn
}

// secretCRD is a connector CRD with a secret property: the only realistic
// place a caller can write one through the generic surface.
func installSecretCRD(t *testing.T, ds substrate.Dataset) string {
	t.Helper()
	const authority = "gmail.google.connectors.substrate.reamde.dev"
	if err := enginetest.Install(context.Background(), ds, substrate.ActorSystem, enginetest.Manifest{
		Name: "google.gmail", Authority: authority,
		Manifests: []map[string]any{
			vocabulary.AuthorityManifest(authority, 1),
			vocabulary.ActorManifest(authority, "connector:gmail"),
			vocabulary.KindManifest(authority,
				map[string]any{"singular": "accountconfig", "plural": "accountconfigs"},
				map[string]any{"properties": map[string]any{
					"label":  map[string]any{"type": "string"},
					"apiKey": map[string]any{"type": "secret"},
				}}),
		},
	}); err != nil {
		t.Fatalf("register connector: %v", err)
	}
	return "gmail.google.connectors.substrate.reamde.dev/accountconfig"
}

// A deletion is an ordinary write: nothing ranks writers, so any actor may
// clear any property.
func TestAnyActorMayDeleteAProperty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	c := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "Alexandros Papas"},
	})
	before := maxSeq(t, ds)

	got := mustPatch(t, ds, slack, c.Kind, c.ID, substrate.PatchInput{
		Properties: map[string]any{"name": nil},
	})
	if _, still := got.Properties["name"]; still {
		t.Fatalf("a delete was blocked: %v", got.Properties)
	}
	rows := changesSince(t, ds, before)
	if len(rows) != 1 {
		t.Fatalf("expected one row, got %d", len(rows))
	}
	if _, dropped := rows[0].Payload["dropped"]; dropped {
		t.Fatalf("no write is dropped any more: %+v", rows[0].Payload)
	}

	// And the owner may clear it too.
	if _, err := ds.Patch(ctx, owner, c.Kind, c.ID, substrate.PatchInput{
		Properties: map[string]any{"displayName": nil},
	}); err != nil {
		t.Fatalf("owner delete: %v", err)
	}
}

// Re-submitting identical data any number of times produces zero changelog
// rows.
func TestResyncIsSilentUnderAnyActor(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)

	c := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "Alexandros Papas"},
	})
	mustPatch(t, ds, slack, c.Kind, c.ID, substrate.PatchInput{Properties: map[string]any{"name": "alex"}})
	v := mustGet(t, ds, c.Kind, c.ID).Version
	before := maxSeq(t, ds)

	for i := range 5 {
		got := mustPatch(t, ds, slack, c.Kind, c.ID, substrate.PatchInput{
			Properties: map[string]any{"name": "alex"},
		})
		if got.Version != v {
			t.Fatalf("sync %d bumped version %d → %d", i, v, got.Version)
		}
	}
	if rows := changesSince(t, ds, before); len(rows) != 0 {
		t.Fatalf("5 identical writes wrote %d changelog rows", len(rows))
	}
}

// Reading redacts a secret; writing the redaction back must not store it.
func TestSecretRoundTripLeavesStoredValue(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, raw, dsn := newDatasetWithDB(t)
	ty := installSecretCRD(t, ds)

	cfg := mustPut(t, ds, gmail, substrate.PutInput{
		Kind: ty, Properties: map[string]any{"label": "work", "apiKey": "hunter2"},
	})
	read := mustGet(t, ds, cfg.Kind, cfg.ID)
	if read.Properties["apiKey"] != engine.Redacted {
		t.Fatalf("secret not redacted on read: %v", read.Properties)
	}

	before := maxSeq(t, ds)
	// The naive round trip: read a record, change one field, write it back.
	back := mustPut(t, ds, gmail, substrate.PutInput{
		Kind: ty, ID: cfg.ID,
		Properties: map[string]any{"label": "personal", "apiKey": read.Properties["apiKey"]},
	})
	if back.Properties["label"] != "personal" {
		t.Fatalf("round trip lost the ordinary property: %v", back.Properties)
	}
	// The stored form is SEALED, so the round-trip assertion opens it under
	// the repository's own DEK to prove the credential survived intact.
	if got := storedSecret(t, raw, dsn, cfg.ID); got != "hunter2" {
		t.Fatalf("round trip destroyed the credential: stored %q", got)
	}
	rows := changesSince(t, ds, before)
	if len(rows) != 1 {
		t.Fatalf("expected one row for the label change, got %d", len(rows))
	}
	if props, _ := rows[0].Payload["properties"].([]any); len(props) != 1 || props[0] != "label" {
		t.Fatalf("redaction sentinel counted as a write: %+v", rows[0].Payload)
	}

	// A real new secret still lands.
	if _, err := ds.Put(ctx, gmail, substrate.PutInput{
		Kind: ty, ID: cfg.ID, Properties: map[string]any{"apiKey": "hunter3"},
	}); err != nil {
		t.Fatalf("rotate secret: %v", err)
	}
	if got := storedSecret(t, raw, dsn, cfg.ID); got != "hunter3" {
		t.Fatalf("secret rotation did not land: %q", got)
	}
}

// storedSecret reads the raw stored apiKey ref, follows it into the sealed
// store, and opens the payload under the repository's DEK — the key lifted
// out of the control plane under the host credential key, the way an
// operator's tooling would.
func storedSecret(t *testing.T, raw *sql.DB, dsn, id string) string {
	t.Helper()
	var ref string
	if err := raw.QueryRow(
		`SELECT props->>'apiKey' FROM records WHERE id = $1`, id).Scan(&ref); err != nil {
		t.Fatalf("read stored secret ref: %v", err)
	}
	if !strings.HasPrefix(ref, "secret:") {
		t.Fatalf("stored secret is not a sealed-store ref: %q", ref)
	}
	var payload []byte
	var kind, rid string
	if err := raw.QueryRow(
		`SELECT payload, record_kind, record_id FROM sealed WHERE ref = $1`, ref).
		Scan(&payload, &kind, &rid); err != nil {
		t.Fatalf("read sealed payload for %s: %v", ref, err)
	}
	// The scoped app pool cannot see the control plane (that is the
	// isolation working); the DEK read takes the schema's own connection.
	cp, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open control-plane connection: %v", err)
	}
	defer func() { _ = cp.Close() }()
	var wrapped []byte
	var repoID string
	if err := cp.QueryRow(`SELECT id, dek FROM repositories LIMIT 1`).Scan(&repoID, &wrapped); err != nil {
		t.Fatalf("read wrapped dek: %v", err)
	}
	if len(wrapped) == 0 || wrapped[0] != 'a' {
		t.Fatalf("a keyed host must wrap the DEK bound-sealed, not plain-marked or unbound")
	}
	hostKey := engine.TestCredentialKeyBytes
	dek, err := engine.OpenPayloadWithKey(hostKey, wrapped, engine.DEKAAD(repoID))
	if err != nil {
		t.Fatalf("unwrap the DEK: %v", err)
	}
	plain, err := engine.OpenPayloadWithKey(dek, payload, engine.SealedAAD(ref, kind, rid))
	if err != nil {
		t.Fatalf("open payload under the DEK: %v", err)
	}
	return string(plain)
}

// The filter grammar must not become a decryption oracle.
func TestSecretPropertiesAreNotFilterable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	if _, _, err := ds.MintToken(ctx, "cli", nil); err != nil {
		t.Fatalf("mint: %v", err)
	}

	for name, q := range map[string]substrate.Query{
		"eq on a typed collection": {Filter: substrate.Filter{
			Kinds:      []string{"core.substrate.reamde.dev/token"},
			Properties: map[string]substrate.Cond{"hash": {Eq: "abc"}},
		}},
		"prefix binary search": {Filter: substrate.Filter{
			Kinds:      []string{"core.substrate.reamde.dev/token"},
			Properties: map[string]substrate.Cond{"hash": {Prefix: "a"}},
		}},
		"untyped filter": {Filter: substrate.Filter{
			Properties: map[string]substrate.Cond{"hash": {In: []any{"a", "b"}}},
		}},
		"exists probe on another type's secret": {Filter: substrate.Filter{
			Properties: map[string]substrate.Cond{"passwordRef": {Exists: ptr(true)}},
		}},
		"ordering oracle": {
			Filter:  substrate.Filter{Kinds: []string{"core.substrate.reamde.dev/token"}},
			OrderBy: []substrate.Order{{Property: "hash"}},
		},
	} {
		if _, err := ds.List(ctx, q); err == nil {
			t.Fatalf("%s: secret exposed through the query grammar", name)
		} else {
			wantErr(t, err, substrate.ErrValidation, name)
		}
	}

	// Ordinary properties still filter.
	if _, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{
		Kinds:      []string{"core.substrate.reamde.dev/token"},
		Properties: map[string]substrate.Cond{"label": {Eq: "cli"}},
	}}); err != nil {
		t.Fatalf("ordinary filter broke: %v", err)
	}
}

// Two people holding one email address is LEGAL AND INERT:
// nothing matches by value, so nothing fuses and nothing collides.
func TestSharedEmailDoesNotFuse(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)

	a := mustPut(t, ds, people, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "Alex", "emails": []any{"alex@acme.com"}},
	})
	b := mustPut(t, ds, people, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "Alexa", "emails": []any{"alex@acme.com"}},
	})
	if a.ID == b.ID {
		t.Fatal("a shared address fused two people")
	}
	if got := mustGet(t, ds, a.Kind, a.ID); got.Properties["name"] != "Alex" {
		t.Fatalf("a = %v", got.Properties)
	}
	if got := mustGet(t, ds, b.Kind, b.ID); got.Properties["name"] != "Alexa" {
		t.Fatalf("b = %v", got.Properties)
	}
}

// An accepted mutation request CAS's against the target version its diff was computed
// against, even when the diff itself carries no ifVersion.
func TestApplyDiffChecksTargetVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	task := mustPut(t, ds, owner, substrate.PutInput{Kind: "task", Properties: map[string]any{"name": "Draft the memo"}})
	req := mustPut(t, ds, engram, substrate.PutInput{
		Kind: "recordpatchrequest",
		Properties: map[string]any{
			"diff": map[string]any{"properties": map[string]any{"description": "due Friday"}},
		},
		Edges: []substrate.EdgeInput{{Rel: "target", To: substrate.EdgeRef{Kind: "tasks.substrate.reamde.dev/task", ID: task.ID}}},
	})

	// The owner moves on before deciding.
	newer := mustPatch(t, ds, owner, task.Kind, task.ID, substrate.PatchInput{Properties: map[string]any{"description": "due Monday"}})
	if newer.Version == task.Version {
		t.Fatal("owner write did not bump the version")
	}

	if _, err := ds.Patch(ctx, owner, req.Kind, req.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: ptr(req.Version),
	}); err == nil {
		t.Fatal("stale request clobbered a newer owner write")
	} else {
		wantErr(t, err, substrate.ErrConflict, "stale applyDiff")
	}
	if got := mustGet(t, ds, task.Kind, task.ID); got.Properties["description"] != "due Monday" {
		t.Fatalf("target changed despite the conflict: %v", got.Properties)
	}
	after := mustGet(t, ds, req.Kind, req.ID)
	if after.Properties["decision"] != "proposed" {
		t.Fatalf("failed transition should roll back: %v", after.Properties)
	}
	if after.Annotations["substrate/conflict"] == nil {
		t.Fatalf("conflict annotation missing: %v", after.Annotations)
	}

	// Re-syncing the same request (same target, same diff) must not rebase it
	// onto the newer target.
	resynced := mustPut(t, ds, engram, substrate.PutInput{
		Kind: "recordpatchrequest", ID: req.ID,
		Properties: map[string]any{
			"diff": map[string]any{"properties": map[string]any{"description": "due Friday"}},
		},
		Edges: []substrate.EdgeInput{{Rel: "target", To: substrate.EdgeRef{Kind: "tasks.substrate.reamde.dev/task", ID: task.ID}}},
	})
	if _, err := ds.Patch(ctx, owner, req.Kind, req.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: ptr(resynced.Version),
	}); err == nil {
		t.Fatal("re-submitting the request rebased it onto the newer target")
	}

	// A request computed against the current version still applies.
	fresh := mustPut(t, ds, engram, substrate.PutInput{
		Kind: "recordpatchrequest",
		Properties: map[string]any{
			"diff": map[string]any{"properties": map[string]any{"description": "due Tuesday"}},
		},
		Edges: []substrate.EdgeInput{{Rel: "target", To: substrate.EdgeRef{Kind: "tasks.substrate.reamde.dev/task", ID: task.ID}}},
	})
	mustPatch(t, ds, owner, fresh.Kind, fresh.ID, substrate.PatchInput{Properties: map[string]any{"decision": "accepted"}, IfVersion: ptr(fresh.Version)})
	if got := mustGet(t, ds, task.Kind, task.ID); got.Properties["description"] != "due Tuesday" {
		t.Fatalf("fresh request did not land: %v", got.Properties)
	}
}

// required:true on an edge is enforced when the record is created.
func TestRequiredEdgesEnforcedOnCreate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	if err := enginetest.InstallAccountType(context.Background(), ds, substrate.ActorAPI); err != nil {
		t.Fatalf("install account type: %v", err)
	}

	_, err := ds.Put(ctx, slack, substrate.PutInput{
		Kind: "conversationmessage", ID: "slack:T1:C1:1",
		Properties: map[string]any{"text": "orphan"},
	})
	if err == nil {
		t.Fatal("message created with neither conversation nor author")
	}
	wantErr(t, err, substrate.ErrValidation, "missing required edges")
	if !strings.Contains(err.Error(), "author") || !strings.Contains(err.Error(), "conversation") {
		t.Fatalf("error should name the missing rels: %v", err)
	}

	acc := mustPut(t, ds, owner, substrate.PutInput{
		Kind: enginetest.AccountType, ID: "slack:T1",
		Properties: map[string]any{"provider": "slack"},
	})
	conv := mustPut(t, ds, slack, substrate.PutInput{
		Kind: "conversation", ID: "slack:T1:C1",
		Properties: map[string]any{"kind": "channel", "name": "general", "account": enginetest.AccountType + "/" + acc.ID},
	})
	author := mustPut(t, ds, slack, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "Alex"},
	})
	msg := mustPut(t, ds, slack, substrate.PutInput{
		Kind: "conversationmessage", ID: "slack:T1:C1:1",
		Properties: map[string]any{"text": "hello"},
		Edges: []substrate.EdgeInput{
			{Rel: "conversation", To: substrate.EdgeRef{ID: conv.ID}},
			{Rel: "author", To: substrate.EdgeRef{ID: author.ID}},
		},
	})

	// A patch that does not touch edges must not fail.
	if _, err := ds.Patch(ctx, slack, msg.Kind, msg.ID, substrate.PatchInput{
		Properties: map[string]any{"text": "hello there"},
	}); err != nil {
		t.Fatalf("edge-free patch rejected: %v", err)
	}
	// An edge naming an id nothing holds is a not-found, never a silent stub:
	// there is no resolve-by-value any more.
	if _, err := ds.Put(ctx, slack, substrate.PutInput{
		Kind: "conversationmessage", ID: "slack:T1:C1:2",
		Edges: []substrate.EdgeInput{
			{Rel: "conversation", To: substrate.EdgeRef{ID: "slack:T1:C2"}},
			{Rel: "author", To: substrate.EdgeRef{ID: author.ID}},
		},
	}); err == nil {
		t.Fatal("an edge at an id nothing holds must not create one")
	} else {
		wantErr(t, err, substrate.ErrNotFound, "edge at an unheld id")
	}
}
