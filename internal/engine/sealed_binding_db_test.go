package engine

// A sealed payload binds to the address it was written at (ADR 0023): moving a
// row's ciphertext onto another row fails the open, and the reseal migration
// leaves an already-bound store byte-identical. These are INTERNAL tests: they
// reach openSecretValue and rekeySealedStore directly and plant bytes in the
// sealed table the way an attacker with table write access would.

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

const bindingProviderKind = "substrate.reamde.dev/core/llmprovider"

// putProviderSecret writes one llmprovider row with a secret apiKey and returns
// the sealed-store ref the property now holds.
func putProviderSecret(t *testing.T, ds *dataset, id, key string) string {
	t.Helper()
	mustPutInternal(t, ds, substrate.PutInput{
		Kind: bindingProviderKind, ID: id,
		Properties: map[string]any{
			"label": id, "wire": "openai",
			"baseURL": "https://llm.example.com/v1", "apiKey": key,
		},
	})
	var ref string
	if err := ds.db.QueryRow(
		`SELECT props->>'apiKey' FROM records WHERE kind = $1 AND id = $2`,
		bindingProviderKind, id).Scan(&ref); err != nil {
		t.Fatalf("read apiKey ref for %s: %v", id, err)
	}
	if !strings.HasPrefix(ref, secretRefPrefix) {
		t.Fatalf("apiKey did not store a sealed-store ref: %q", ref)
	}
	return ref
}

// TestSealedPayloadDoesNotOpenAtAnotherRow moves one row's ciphertext onto a
// second row and asserts the open fails there while the origin still opens: the
// binding turns a confidentiality break into an availability one.
func TestSealedPayloadDoesNotOpenAtAnotherRow(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	ctx := context.Background()
	ds := openInternalDataset(t)

	refA := putProviderSecret(t, ds, "a", "secret-a")
	refB := putProviderSecret(t, ds, "b", "secret-b")

	if got, err := ds.openSecretValue(ctx, refA); err != nil || got != "secret-a" {
		t.Fatalf("row A does not open to its own material: got %q err %v", got, err)
	}

	var payloadA []byte
	if err := ds.db.QueryRow(`SELECT payload FROM sealed WHERE ref = $1`, refA).Scan(&payloadA); err != nil {
		t.Fatalf("read payload A: %v", err)
	}
	if len(payloadA) == 0 || payloadA[0] != credBoundSealed {
		t.Fatalf("a fresh sealed row is not bound-framed: %q", payloadA)
	}

	// Move A's ciphertext onto B's row: the bytes change, but B's own
	// (ref, record_kind, record_id) stay, so the open computes B's binding.
	if _, err := ds.db.ExecContext(ctx, `UPDATE sealed SET payload = $1 WHERE ref = $2`, payloadA, refB); err != nil {
		t.Fatalf("plant A's bytes at row B: %v", err)
	}

	if got, err := ds.openSecretValue(ctx, refB); err == nil {
		t.Fatalf("the moved payload decrypted at row B: %q", got)
	}
	if got, err := ds.openSecretValue(ctx, refA); err != nil || got != "secret-a" {
		t.Fatalf("row A stopped opening after the move: got %q err %v", got, err)
	}
}

// TestResealIsIdempotentOnBoundStore re-keys twice: the first pass upgrades a
// planted unbound legacy row and leaves the rest, the second moves nothing and
// leaves every payload byte-identical.
func TestResealIsIdempotentOnBoundStore(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	ctx := context.Background()
	ds := openInternalDataset(t)

	refA := putProviderSecret(t, ds, "a", "secret-a")
	putProviderSecret(t, ds, "b", "secret-b")

	// Plant an unbound `s`-framed payload at row A, as a pre-binding release
	// wrote it: DEK-sealed with no additional data.
	aead, err := aeadOf(ds.dek)
	if err != nil || aead == nil {
		t.Fatalf("build DEK aead: %v", err)
	}
	legacy, err := sealWith(aead, []byte("secret-a"), nil)
	if err != nil {
		t.Fatalf("seal legacy payload: %v", err)
	}
	if legacy[0] != credSealed {
		t.Fatalf("legacy payload is not unbound-framed: %q", legacy)
	}
	if _, err := ds.db.ExecContext(ctx, `UPDATE sealed SET payload = $1 WHERE ref = $2`, legacy, refA); err != nil {
		t.Fatalf("plant legacy payload at row A: %v", err)
	}

	// First re-key: only the legacy row moves; the bound rows are skipped.
	if n := rekeySealed(t, ctx, ds); n != 1 {
		t.Fatalf("first re-key moved %d rows, want 1 (the legacy)", n)
	}
	before := snapshotSealed(t, ctx, ds)

	// Second re-key on an all-bound store: nothing moves, nothing changes.
	if n := rekeySealed(t, ctx, ds); n != 0 {
		t.Fatalf("second re-key moved %d rows, want 0", n)
	}
	after := snapshotSealed(t, ctx, ds)
	if len(before) != len(after) {
		t.Fatalf("row count changed under an idempotent re-key: %d -> %d", len(before), len(after))
	}
	for ref, payload := range before {
		if !bytes.Equal(payload, after[ref]) {
			t.Fatalf("row %s changed under an idempotent re-key", ref)
		}
	}

	if got, err := ds.openSecretValue(ctx, refA); err != nil || got != "secret-a" {
		t.Fatalf("row A does not open after the re-key: got %q err %v", got, err)
	}
}

// TestResealRebindsLegacyDEKWrap proves reseal migrates the whole binding, not
// half: a store whose control-plane DEK wrap is the legacy unbound framing gets
// an address-bound wrap on reseal, still opens its own secrets, and its wrap
// moved into another repository's row fails to open.
func TestResealRebindsLegacyDEKWrap(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	ctx := context.Background()
	ds := openInternalDataset(t)
	s := ds.svc
	repoID := ds.scope.Repository

	// A secret to prove the store still opens after the migration.
	refA := putProviderSecret(t, ds, "a", "secret-a")

	// Wind the DEK wrap back to the unbound 's' framing a pre-binding release
	// wrote: the same DEK plaintext, host-keyed, with no address binding.
	hostAEAD, err := newAEAD(s.credKey)
	if err != nil {
		t.Fatalf("build host aead: %v", err)
	}
	legacyWrap, err := sealWith(hostAEAD, ds.dek, nil)
	if err != nil {
		t.Fatalf("seal legacy DEK wrap: %v", err)
	}
	if legacyWrap[0] != credSealed {
		t.Fatalf("legacy DEK wrap is not unbound-framed: %q", legacyWrap)
	}
	if _, err := s.maint.ExecContext(ctx, `UPDATE repositories SET dek = $1 WHERE id = $2`, legacyWrap, repoID); err != nil {
		t.Fatalf("plant legacy DEK wrap: %v", err)
	}

	if _, err := s.ResealRepository(ctx, "geoah"); err != nil {
		t.Fatalf("reseal: %v", err)
	}

	var wrapped []byte
	if err := s.maint.QueryRowContext(ctx, `SELECT dek FROM repositories WHERE id = $1`, repoID).Scan(&wrapped); err != nil {
		t.Fatalf("read DEK wrap: %v", err)
	}
	if len(wrapped) == 0 || wrapped[0] != credBoundSealed {
		t.Fatalf("reseal left the DEK wrap unbound: %q", wrapped)
	}

	// The rebound wrap opens for its own repository, and the store's secrets
	// still open.
	if _, err := s.unwrapDEK(wrapped, repoID); err != nil {
		t.Fatalf("rebound DEK wrap does not open for its own repository: %v", err)
	}
	if got, err := ds.openSecretValue(ctx, refA); err != nil || got != "secret-a" {
		t.Fatalf("secret does not open after reseal: got %q err %v", got, err)
	}

	// Moved into another repository's dek column it fails: the binding names
	// the repository.
	if _, err := s.unwrapDEK(wrapped, repoID+"-other"); err == nil {
		t.Fatal("the rebound DEK wrap opened under another repository's id")
	}

	// A second reseal leaves the already-bound wrap byte-identical.
	before := append([]byte(nil), wrapped...)
	if _, err := s.ResealRepository(ctx, "geoah"); err != nil {
		t.Fatalf("second reseal: %v", err)
	}
	var after []byte
	if err := s.maint.QueryRowContext(ctx, `SELECT dek FROM repositories WHERE id = $1`, repoID).Scan(&after); err != nil {
		t.Fatalf("read DEK wrap after second reseal: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("second reseal changed the already-bound DEK wrap")
	}
}

// rekeySealed runs one rekeySealedStore pass in a raw transaction and returns
// the number of rows it moved.
func rekeySealed(t *testing.T, ctx context.Context, ds *dataset) int {
	t.Helper()
	var n int
	if err := ds.inRawTx(ctx, func(tx *txn) error {
		var err error
		n, err = tx.rekeySealedStore()
		return err
	}); err != nil {
		t.Fatalf("re-key sealed store: %v", err)
	}
	return n
}

// snapshotSealed reads every sealed row's payload, keyed by ref.
func snapshotSealed(t *testing.T, ctx context.Context, ds *dataset) map[string][]byte {
	t.Helper()
	rows, err := ds.db.QueryContext(ctx, `SELECT ref, payload FROM sealed`)
	if err != nil {
		t.Fatalf("snapshot sealed store: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string][]byte{}
	for rows.Next() {
		var ref string
		var payload []byte
		if err := rows.Scan(&ref, &payload); err != nil {
			t.Fatalf("scan sealed row: %v", err)
		}
		out[ref] = payload
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate sealed rows: %v", err)
	}
	return out
}
