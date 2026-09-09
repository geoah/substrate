package engine

// A sealed payload binds to the address it was written at (ADR 0023): moving a
// row's ciphertext onto another row fails the open, and the re-key that
// recovery enrollment runs leaves an already-bound store byte-identical. These
// are INTERNAL tests: they reach openSecretValue directly
// and plant bytes in the sealed table the way an attacker with table write
// access would.

import (
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
