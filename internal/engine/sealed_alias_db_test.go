package engine

// Two secret properties on one record must not share a sealed row (#233). The
// write path recognizes a carried ref PER-PROPERTY, so a value naming the row a
// DIFFERENT property owns is refused rather than stored verbatim. Without the
// gate, property B adopts property A's ref, and rotating A runs
// `DELETE FROM sealed` on the row B still points at, erasing B's material.
// This is an INTERNAL test: it reaches ds.openSecretValue to prove B's material
// survives, which the wire redacts.

import (
	"context"
	"errors"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const aliasVaultPackage = "alias.example.substrate.reamde.dev/alias"

func TestSecretPropsDoNotAliasOneSealedRow(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	ctx := context.Background()
	ds := openInternalDataset(t)

	vaultKind := aliasVaultPackage + "/vault"
	docs := []map[string]any{
		vocabulary.PackageManifest(aliasVaultPackage, 0),
		vocabulary.KindManifest(aliasVaultPackage,
			map[string]any{"singular": "vault", "plural": "vaults"},
			map[string]any{"properties": map[string]any{
				"name":    map[string]any{"type": "string"},
				"secretA": map[string]any{"type": "secret"},
				"secretB": map[string]any{"type": "secret"},
			}}),
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, docs); err != nil {
		t.Fatalf("declare vault kind: %v", err)
	}

	readRefs := func() (string, string) {
		t.Helper()
		var a, b string
		if err := ds.db.QueryRow(
			`SELECT props->>'secretA', props->>'secretB' FROM records WHERE kind = $1 AND id = 'v1'`,
			vaultKind).Scan(&a, &b); err != nil {
			t.Fatalf("read stored refs: %v", err)
		}
		return a, b
	}
	sealedRows := func() int {
		t.Helper()
		var n int
		if err := ds.db.QueryRow(
			`SELECT count(*) FROM sealed WHERE record_kind = $1 AND record_id = 'v1'`,
			vaultKind).Scan(&n); err != nil {
			t.Fatalf("count sealed rows: %v", err)
		}
		return n
	}

	mustPutInternal(t, ds, substrate.PutInput{
		Kind: vaultKind, ID: "v1",
		Properties: map[string]any{"name": "v", "secretA": "value-A", "secretB": "value-B"},
	})
	refA, refB := readRefs()
	if refA == "" || refB == "" || refA == refB {
		t.Fatalf("two secret properties did not get two distinct sealed refs: %q %q", refA, refB)
	}
	if n := sealedRows(); n != 2 {
		t.Fatalf("want 2 sealed rows, got %d", n)
	}

	// The attack: write secretB as a value naming secretA's sealed row. The
	// unfixed write treats any ref of this record as a carried ref and stores it
	// verbatim, aliasing B onto A's row.
	_, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: vaultKind, ID: "v1",
		Properties: map[string]any{"secretB": refA},
	})
	if !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("aliasing secretB onto secretA's sealed ref was not refused: %v", err)
	}

	// The refused write moved no ref: B still owns its own row.
	if afterA, afterB := readRefs(); afterA != refA || afterB != refB {
		t.Fatalf("refused write moved a ref: A %q->%q B %q->%q", refA, afterA, refB, afterB)
	}

	// Rotating A must not erase B. With no alias, A's rotation deletes only A's
	// old row; B keeps its ref and still opens to its original value.
	mustPutInternal(t, ds, substrate.PutInput{
		Kind: vaultKind, ID: "v1",
		Properties: map[string]any{"secretA": "value-A-rotated"},
	})
	if _, afterRotB := readRefs(); afterRotB != refB {
		t.Fatalf("secretB's ref moved after rotating secretA: %q -> %q", refB, afterRotB)
	}
	if got, err := ds.openSecretValue(ctx, refB); err != nil || got != "value-B" {
		t.Fatalf("secretB no longer opens to its original value: got %q err %v", got, err)
	}
	if n := sealedRows(); n != 2 {
		t.Fatalf("want 2 sealed rows after rotating secretA, got %d", n)
	}
}
