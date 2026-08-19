package engine

// The reseal and verified-rebuild gates against the unsigned prefix (#252).
// Below signed_from_seq every entry carries the all-zero signature, so a
// database-only attacker rewrites one entry and re-chains the prefix, and the
// chain walk alone checks clean. Only the activation epoch's signed head still
// names the rewrite, and both doors that install or re-sign history must
// consult it: without the anchor a reseal would notarize the tampering into
// signed history, and a verified rebuild would install it as the fold.

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

// tamperUnsignedPrefix rewrites one entry below signed_from_seq and re-chains
// every stored hash so the chain walk alone passes: the exact shape only the
// epoch anchor names. No key is needed, which is what an unsigned prefix is
// worth.
func tamperUnsignedPrefix(t *testing.T, repoID string, raw *sql.DB) {
	t.Helper()
	ctx := context.Background()
	if _, err := raw.Exec(`UPDATE changelog SET actor = 'console' WHERE seq = 1`); err != nil {
		t.Fatalf("rewrite seq 1: %v", err)
	}
	rows, err := scanChainPageWithMarks(ctx, raw, 0, 10000)
	if err != nil {
		t.Fatalf("read the prefix: %v", err)
	}
	prev := zeroHash
	for _, r := range rows {
		h, err := entryHash(repoID, r.entry, prev)
		if err != nil {
			t.Fatalf("re-chain seq %d: %v", r.entry.Seq, err)
		}
		if _, err := raw.Exec(`UPDATE changelog SET hash = $2 WHERE seq = $1`, r.entry.Seq, h[:]); err != nil {
			t.Fatalf("stamp seq %d: %v", r.entry.Seq, err)
		}
		prev = h
	}
}

// A clean chain passes both gates; a rewritten-and-re-chained unsigned prefix
// is refused by both, where before the epoch anchor was added it returned nil.
func TestResealAndVerifiedRebuildRefuseARewrittenUnsignedPrefix(t *testing.T) {
	t.Parallel()
	svc, repoID, raw, _ := upgradedRepository(t)
	ctx := context.Background()

	// The gate passes over the untouched store: neither door refuses a clean
	// chain, so a refusal below is the anchor firing, not a false positive.
	if _, err := svc.ResealRepository(ctx, "geoah"); err != nil {
		t.Fatalf("reseal refused a clean chain: %v", err)
	}
	if _, err := svc.RebuildRepository(ctx, "geoah"); err != nil {
		t.Fatalf("verified rebuild refused a clean chain: %v", err)
	}

	tamperUnsignedPrefix(t, repoID, raw)

	// The chain walk alone still passes: the re-chained prefix leaves no hash
	// finding, so this exercises the epoch-only path and not a broken chain.
	report, err := svc.VerifyRepository(ctx, "geoah")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if report.OK {
		t.Fatal("verify passed a rewritten unsigned prefix: the epoch anchor is not firing")
	}
	for _, f := range report.Findings {
		if strings.Contains(f, "hash mismatch") {
			t.Fatalf("the re-chain left a hash finding, so this does not exercise the epoch-only path: %s", f)
		}
	}

	// Both gates now consult the anchor and REFUSE the rewrite. Reseal is
	// checked first: it rolls back on refusal and leaves the tampered bytes in
	// place for the rebuild check.
	if _, err := svc.ResealRepository(ctx, "geoah"); err == nil ||
		!strings.Contains(err.Error(), "reseal refuses") {
		t.Fatalf("reseal did not refuse the rewritten prefix: %v", err)
	}
	if _, err := svc.RebuildRepository(ctx, "geoah"); err == nil ||
		!strings.Contains(err.Error(), "rebuild refuses") {
		t.Fatalf("verified rebuild did not refuse the rewritten prefix: %v", err)
	}
}
