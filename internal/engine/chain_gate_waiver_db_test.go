package engine_test

// The prior-reseal waiver, held across the new epoch gate (#252). A reseal
// that legitimately rewrites a secret value below the activation boundary
// moves the very head the activation epoch signed, so the boundary no longer
// matches; a SIGNED reseal epoch is what excuses the mismatch. Now that
// reseal's and rebuild's gates consult the epoch anchor, that waiver must
// still hold, or a repository resealed once could never be resealed or
// verified-rebuilt again.

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
)

func TestResealAndRebuildDoNotRefuseAPriorReseal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, db, dsn := newSealingDatasetDSN(t)
	sgPutProvider(t, ds, sgPlainKey)
	ref := storedAPIKeyRef(t, db)

	// The legacy pre-store, pre-chain shape: plaintext in the fold and the
	// changelog, no sealed row, no hashes, no signing state. The reopen's
	// backfill notarizes it and activation lands at head+1, so the secret sits
	// in the unsigned prefix.
	const legacy = "sk-legacy-55555"
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
	svc, err := engine.Open(ctx, dsn, engine.WithKindsDir("../../kinds/substrate.reamde.dev/core"),
		engine.WithCredentialKey(engine.TestCredentialKey))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if _, err := svc.Dataset(ctx, "geoah"); err != nil {
		t.Fatalf("reopen dataset: %v", err)
	}

	// The legitimate reseal: it rewrites the secret in the prefix and re-chains
	// from there, moving the head the activation epoch signed and minting the
	// signed reseal epoch that records the move.
	report, err := svc.(resealer).ResealRepository(ctx, "geoah")
	if err != nil {
		t.Fatalf("legitimate reseal: %v", err)
	}
	if report.Entries == 0 {
		t.Fatalf("the reseal rewrote nothing, so it did not move the boundary: %+v", report)
	}
	after := mustVerify(t, svc, "geoah")
	resealedBelowBoundary := false
	for _, ep := range after.Epochs {
		if ep.Reason == "reseal" && ep.FromSeq <= after.SignedFrom-1 {
			resealedBelowBoundary = true
		}
	}
	if !resealedBelowBoundary {
		t.Fatalf("the reseal did not touch the unsigned prefix, so the waiver is not exercised: %+v", after.Epochs)
	}

	// The waiver holds through the new gate: a second reseal and a verified
	// rebuild both pass, rather than reading the moved boundary as tampering.
	if _, err := svc.(resealer).ResealRepository(ctx, "geoah"); err != nil {
		t.Fatalf("a second reseal falsely refused a prior-resealed repository: %v", err)
	}
	if _, err := svc.(rebuilder).RebuildRepository(ctx, "geoah"); err != nil {
		t.Fatalf("verified rebuild falsely refused a prior-resealed repository: %v", err)
	}

	// And verify itself is clean but for the unsigned prefix the reseal cannot
	// sign: no boundary finding survives the waiver.
	final := mustVerify(t, svc, "geoah")
	for _, f := range final.Findings {
		if strings.Contains(f, "unsigned history has been rewritten") {
			t.Fatalf("the waiver did not suppress the boundary finding on a sanctioned reseal: %s", f)
		}
	}
}
