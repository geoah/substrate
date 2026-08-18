package engine_test

// The store-backed secret gate: a secret-typed property's stored value is a
// ref into the sealed store, every kind alike, and the material never
// touches the records fold or the append-only changelog. Alongside: the
// change feed redacts what it cannot know, a re-pasted secret is a no-op
// that neither mints a delta nor steals attribution, rotation erases the old
// material, a pasted ref-shaped string is material like any other, `digest`
// redacts without indirection, and `repository reseal` moves legacy values
// into the store so a rebuild folds the changelog to byte-identical rows.

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	sgProviderKind = "core.substrate.reamde.dev/llmprovider"
	sgPlainKey     = "sk-plain-12345"
)

// newSealingDataset provisions a keyed repository plus a raw *sql.DB on the
// same schema, so a test can read what the database actually stores.
func newSealingDataset(t *testing.T) (substrate.Service, substrate.Dataset, *sql.DB) {
	t.Helper()
	svc, ds, db, _ := newSealingDatasetDSN(t)
	return svc, ds, db
}

// newSealingDatasetDSN is newSealingDataset with the DSN kept, for the tests
// that reopen the same store under a fresh service.
func newSealingDatasetDSN(t *testing.T) (substrate.Service, substrate.Dataset, *sql.DB, string) {
	t.Helper()
	svc, dsn := newService(t, engine.WithCredentialKey(engine.TestCredentialKey))
	ctx := context.Background()
	if _, err := svc.CreateRepository(ctx, "geoah"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return svc, ds, db, dsn
}

func sgPutProvider(t *testing.T, ds substrate.Dataset, key string) *substrate.Record {
	t.Helper()
	return mustPut(t, ds, owner, substrate.PutInput{
		Kind: sgProviderKind, ID: "prov",
		Properties: map[string]any{
			"name": "prov", "wire": "openai",
			"baseURL": "https://llm.example.com/v1", "apiKey": key,
		},
	})
}

// storedAPIKeyRef reads the raw stored value, bypassing every redaction.
func storedAPIKeyRef(t *testing.T, db *sql.DB) string {
	t.Helper()
	var v string
	if err := db.QueryRow(`SELECT props->>'apiKey' FROM records WHERE kind = $1 AND id = 'prov'`,
		sgProviderKind).Scan(&v); err != nil {
		t.Fatalf("read stored apiKey: %v", err)
	}
	return v
}

// sealedRowsOf counts the sealed-store rows a record owns.
func sealedRowsOf(t *testing.T, db *sql.DB, kind, id string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sealed WHERE record_kind = $1 AND record_id = $2`,
		kind, id).Scan(&n); err != nil {
		t.Fatalf("count sealed rows: %v", err)
	}
	return n
}

// mustHaveNoPlaintext asserts a value appears nowhere in records or changelog.
func mustHaveNoPlaintext(t *testing.T, db *sql.DB, value string) {
	t.Helper()
	for _, q := range []string{
		`SELECT count(*) FROM records WHERE props::text LIKE '%' || $1 || '%'`,
		`SELECT count(*) FROM changelog WHERE payload::text LIKE '%' || $1 || '%'`,
	} {
		var n int
		if err := db.QueryRow(q, value).Scan(&n); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if n != 0 {
			t.Fatalf("plaintext %q survives: %s", value, q)
		}
	}
}

func TestSecretMovesIntoTheStore(t *testing.T) {
	t.Parallel()
	_, ds, db := newSealingDataset(t)
	rec := sgPutProvider(t, ds, sgPlainKey)

	// The wire shows the sentinel, never the value or the ref.
	if got := rec.Properties["apiKey"]; got != "<redacted>" {
		t.Fatalf("put returned the apiKey unredacted: %v", got)
	}
	// The records fold holds a ref; the material sits sealed in the store.
	ref := storedAPIKeyRef(t, db)
	if !strings.HasPrefix(ref, "secret:") {
		t.Fatalf("stored apiKey is not a sealed-store ref: %q", ref)
	}
	var payload []byte
	if err := db.QueryRow(`SELECT payload FROM sealed WHERE ref = $1`, ref).Scan(&payload); err != nil {
		t.Fatalf("the ref has no sealed row: %v", err)
	}
	if len(payload) == 0 {
		t.Fatal("sealed payload is empty")
	}
	// The store binds a payload to its row under the 'a' framing (ADR 0023);
	// 'p' would be plaintext.
	if payload[0] != 'a' {
		t.Fatalf("sealed payload is not bound-sealed (marker %q)", payload[0])
	}
	// Neither the fold nor the append-only log ever held the material.
	mustHaveNoPlaintext(t, db, sgPlainKey)
}

func TestChangeFeedRedactsSensitiveValues(t *testing.T) {
	t.Parallel()
	_, ds, _ := newSealingDataset(t)
	sgPutProvider(t, ds, sgPlainKey)

	changes, err := ds.Changes(context.Background(), 0, substrate.ChangeFilter{}, 500)
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	found := false
	for _, ch := range changes {
		effects, _ := ch.Payload["fold"].([]any)
		for _, e := range effects {
			op, _ := e.(map[string]any)
			if op["ref"] != sgProviderKind {
				continue
			}
			delta, _ := op["delta"].(map[string]any)
			set, _ := delta["set"].(map[string]any)
			if v, ok := set["apiKey"]; ok {
				found = true
				if v != "<redacted>" {
					t.Fatalf("change feed shows the apiKey delta as %v", v)
				}
			}
		}
	}
	if !found {
		t.Fatal("no llmprovider apiKey delta found in the feed")
	}
}

func TestSecretRepasteIsANoOp(t *testing.T) {
	t.Parallel()
	_, ds, db := newSealingDataset(t)
	first := sgPutProvider(t, ds, sgPlainKey)
	refOnce := storedAPIKeyRef(t, db)

	// The same document again: the plaintext matches the stored material, so
	// the ref stays, no delta is minted, and no attribution moves.
	second := sgPutProvider(t, ds, sgPlainKey)
	if second.Version != first.Version {
		t.Fatalf("re-pasting the same secret bumped the version: %d -> %d", first.Version, second.Version)
	}
	if again := storedAPIKeyRef(t, db); again != refOnce {
		t.Fatal("re-pasting the same secret re-stored the material")
	}
	// A rotation lands, and the OLD material is erased, not retired.
	third := sgPutProvider(t, ds, "sk-rotated-67890")
	if third.Version == first.Version {
		t.Fatal("a rotated secret did not bump the version")
	}
	if after := storedAPIKeyRef(t, db); after == refOnce {
		t.Fatal("rotation kept the old ref")
	}
	if n := sealedRowsOf(t, db, sgProviderKind, "prov"); n != 1 {
		t.Fatalf("rotation left %d sealed rows, want 1 (the old material erased)", n)
	}
	mustHaveNoPlaintext(t, db, sgPlainKey)
}

func TestPastedRefShapedStringIsMaterial(t *testing.T) {
	t.Parallel()
	_, ds, db := newSealingDataset(t)
	// A writer pastes something shaped like a ref. It names no sealed row of
	// this record, so it is material like any other paste: stored under a
	// FRESH ref, never trusted as an address.
	const pasted = "secret:00000000000000000000000000000000"
	sgPutProvider(t, ds, pasted)

	ref := storedAPIKeyRef(t, db)
	if ref == pasted {
		t.Fatal("a pasted ref-shaped string was stored as an address")
	}
	var payload []byte
	if err := db.QueryRow(`SELECT payload FROM sealed WHERE ref = $1`, ref).Scan(&payload); err != nil {
		t.Fatalf("the fresh ref has no sealed row: %v", err)
	}
}

func TestDigestRedactsWithoutIndirection(t *testing.T) {
	t.Parallel()
	_, ds, db := newSealingDataset(t)
	const auth = "digests.example.substrate.reamde.dev"
	docs := []map[string]any{
		vocabulary.AuthorityManifest(auth, 0),
		vocabulary.KindManifest(auth,
			map[string]any{"singular": "artifact", "plural": "artifacts"},
			map[string]any{"properties": map[string]any{
				"name":        map[string]any{"type": "string"},
				"fingerprint": map[string]any{"type": "digest"},
			}}),
	}
	if _, err := applier(t, ds).ApplyVocabularyDocuments(context.Background(), owner, docs); err != nil {
		t.Fatalf("declare digest kind: %v", err)
	}

	// Not a SHA-256: refused at coercion.
	_, err := ds.Put(context.Background(), owner, substrate.PutInput{
		Kind: auth + "/artifact", ID: "a1",
		Properties: map[string]any{"fingerprint": "not-a-digest"},
	})
	wantErr(t, err, substrate.ErrValidation, "malformed digest")

	sum := strings.Repeat("ab", 32)
	rec := mustPut(t, ds, owner, substrate.PutInput{
		Kind: auth + "/artifact", ID: "a1",
		Properties: map[string]any{"name": "one", "fingerprint": sum},
	})
	if got := rec.Properties["fingerprint"]; got != "<redacted>" {
		t.Fatalf("digest not redacted on the wire: %v", got)
	}
	// Stored as the value itself: the engine compares digests in SQL.
	var stored string
	if err := db.QueryRow(`SELECT props->>'fingerprint' FROM records WHERE kind = $1 AND id = 'a1'`,
		auth+"/artifact").Scan(&stored); err != nil {
		t.Fatalf("read stored fingerprint: %v", err)
	}
	if stored != sum {
		t.Fatalf("digest was rewritten at rest: %q", stored)
	}
}

func TestDisplayTemplateRefusesSensitiveProps(t *testing.T) {
	t.Parallel()
	_, ds, _ := newSealingDataset(t)
	const auth = "leaky.example.substrate.reamde.dev"
	docs := []map[string]any{
		vocabulary.AuthorityManifest(auth, 0),
		vocabulary.KindManifest(auth,
			map[string]any{"singular": "leak", "plural": "leaks"},
			map[string]any{
				"displayTemplate": "{apiKey}",
				"properties": map[string]any{
					"apiKey": map[string]any{"type": "secret"},
				},
			}),
	}
	_, err := applier(t, ds).ApplyVocabularyDocuments(context.Background(), owner, docs)
	wantErr(t, err, substrate.ErrValidation, "secret in a display template")
}

type resealer interface {
	ResealRepository(ctx context.Context, username string) (engine.ResealReport, error)
}

func TestResealMovesLegacyValuesIntoTheStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, db, dsn := newSealingDatasetDSN(t)
	sgPutProvider(t, ds, sgPlainKey)
	ref := storedAPIKeyRef(t, db)

	// Plant the pre-store layout: raw plaintext in the records fold and in
	// every changelog delta, and no sealed row, exactly what an earlier
	// release left behind. An earlier release also predates the CHAIN, so
	// the honest legacy state has no hashes either: wipe them and let the
	// reopen's backfill notarize the planted bytes, exactly as a real
	// upgrade would — reseal itself verifies first and refuses a chain that
	// reads as tampered.
	const legacy = "sk-legacy-99999"
	if _, err := db.Exec(`UPDATE records SET props = jsonb_set(props, '{apiKey}', to_jsonb($1::text))
		WHERE kind = $2 AND id = 'prov'`, legacy, sgProviderKind); err != nil {
		t.Fatalf("plant legacy record: %v", err)
	}
	if _, err := db.Exec(`UPDATE changelog SET payload = replace(payload::text, $1, $2)::jsonb
		WHERE payload::text LIKE '%' || $1 || '%'`, ref, legacy); err != nil {
		t.Fatalf("plant legacy changelog: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM sealed WHERE ref = $1`, ref); err != nil {
		t.Fatalf("drop the store row: %v", err)
	}
	if _, err := db.Exec(`UPDATE changelog SET hash = NULL, sig = decode(repeat('00', 64), 'hex')`); err != nil {
		t.Fatalf("wind the chain back: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM chain_epochs`); err != nil {
		t.Fatalf("wind the epochs back: %v", err)
	}
	if _, err := db.Exec(`UPDATE repositories SET signing_key = NULL, signing_public = NULL, signed_from_seq = NULL`); err != nil {
		t.Fatalf("wind the signing state back: %v", err)
	}
	_ = svc.Close()
	svc, err := engine.Open(ctx, dsn, engine.WithKindsDir("../../kinds/core.substrate.reamde.dev"),
		engine.WithCredentialKey(engine.TestCredentialKey))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if _, err := svc.Dataset(ctx, "geoah"); err != nil {
		t.Fatalf("reopen dataset: %v", err)
	}

	report, err := svc.(resealer).ResealRepository(ctx, "geoah")
	if err != nil {
		t.Fatalf("reseal: %v", err)
	}
	if report.Records == 0 || report.Entries == 0 {
		t.Fatalf("reseal touched nothing: %+v", report)
	}
	mustHaveNoPlaintext(t, db, legacy)

	// Records and changelog agree on ONE ref, so the fold is byte-identical
	// to a replay of the rewritten log.
	migrated := storedAPIKeyRef(t, db)
	if !strings.HasPrefix(migrated, "secret:") {
		t.Fatalf("migrated value is not a ref: %q", migrated)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM changelog WHERE payload::text LIKE '%' || $1 || '%'`,
		migrated).Scan(&n); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if n == 0 {
		t.Fatal("no changelog delta points at the migrated ref")
	}

	// Idempotent: a second pass rewrites nothing.
	again, err := svc.(resealer).ResealRepository(ctx, "geoah")
	if err != nil {
		t.Fatalf("second reseal: %v", err)
	}
	if again.Records != 0 || again.Entries != 0 || again.SealedRows != 0 {
		t.Fatalf("reseal is not idempotent: %+v", again)
	}

	// A rebuild folds the rewritten deltas to the same bytes.
	if _, err := svc.(rebuilder).RebuildRepository(ctx, "geoah"); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if after := storedAPIKeyRef(t, db); after != migrated {
		t.Fatalf("rebuild diverged from the migrated fold: %q != %q", after, migrated)
	}
	mustHaveNoPlaintext(t, db, legacy)
}
