package engine

// The database-only attacker, replayed against the placeholder era
// (adversarial review, third pass — Codex): below signed_from_seq every
// signature is the placeholder, so the chain there needs no secret, and the
// activation epoch's SIGNED heads are the one anchor the key reaches into
// that prefix. These tests rewrite the prefix and move the mark the way an
// attacker with full database access would, and hold verify to naming both —
// and hold the crash repair (ensureActivationEpoch) to refusing to notarize
// either.

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// upgradedRepository builds the placeholder-era shape: a repository whose
// whole history predates the chain and signing, reopened under a key so the
// backfill stamps hashes and activation lands at head+1. It returns the
// service, the repository id, a raw (RLS-free) connection, and the dsn.
func upgradedRepository(t *testing.T) (*service, string, *sql.DB, string) {
	t.Helper()
	ctx := context.Background()
	ds := openInternalDataset(t)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: "tasks.substrate.reamde.dev/task", Properties: map[string]any{"title": "old world"},
	}); err != nil {
		t.Fatalf("put: %v", err)
	}
	dsn := ds.svc.dsn
	raw, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("raw connection: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	for _, wind := range []string{
		`UPDATE changelog SET hash = NULL, sig = decode(repeat('00', 64), 'hex')`,
		`DELETE FROM chain_epochs`,
		`UPDATE repositories SET signing_key = NULL, signing_public = NULL, signed_from_seq = NULL`,
	} {
		if _, err := raw.Exec(wind); err != nil {
			t.Fatalf("wind back (%s): %v", wind, err)
		}
	}
	_ = ds.svc.Close()
	svc2, err := Open(ctx, dsn,
		WithKindsDir("../../kinds/core.substrate.reamde.dev"),
		WithCredentialKey("test-cred-key"))
	if err != nil {
		t.Fatalf("reopen keyed: %v", err)
	}
	t.Cleanup(func() { _ = svc2.Close() })
	if _, err := svc2.Dataset(ctx, "geoah"); err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	return svc2.(*service), testdb.RepositoryID(t, dsn, "geoah"), raw, dsn
}

func reportFinding(t *testing.T, svc *service, substr string) VerifyReport {
	t.Helper()
	report, err := svc.VerifyRepository(context.Background(), "geoah")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if substr == "" {
		return report
	}
	for _, f := range report.Findings {
		if strings.Contains(f, substr) {
			return report
		}
	}
	t.Fatalf("no finding contains %q: %+v", substr, report.Findings)
	return report
}

// A rewritten-and-re-chained placeholder prefix is byte-consistent — every
// hash checks, every placeholder is where placeholders live — and ONLY the
// activation epoch's signed heads still know the history it activated over.
func TestVerifyNamesARewrittenPlaceholderPrefix(t *testing.T) {
	t.Parallel()
	svc, repoID, raw, dsn := upgradedRepository(t)
	ctx := context.Background()
	if before := reportFinding(t, svc, ""); !before.OK || before.PlaceholderSigs == 0 {
		t.Fatalf("the upgraded repository does not verify clean before the attack: %+v", before)
	}

	// The attack: change one placeholder entry, then re-chain the whole
	// prefix so every stored hash checks again. No key is needed for any of
	// it — that is the point of the placeholder era.
	if _, err := raw.Exec(`UPDATE changelog SET actor = 'console' WHERE seq = 1`); err != nil {
		t.Fatalf("rewrite: %v", err)
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
	reportFinding(t, svc, "placeholder history has been rewritten")

	// The crash repair must not launder it: the epoch is intact, so nothing
	// is repaired, and verify keeps naming the rewrite after a reopen.
	_ = svc.Close()
	svc2, err := Open(ctx, dsn,
		WithKindsDir("../../kinds/core.substrate.reamde.dev"),
		WithCredentialKey("test-cred-key"))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = svc2.Close() })
	if _, err := svc2.Dataset(ctx, "geoah"); err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	reportFinding(t, svc2.(*service), "placeholder history has been rewritten")
}

// A moved activation mark is named twice — the epoch disagrees with it, and a
// mark beyond the head covers nothing — and the crash repair refuses to mint
// a fresh epoch for it, on this open and every one after.
func TestVerifyNamesAMovedActivationMarkAndRepairRefuses(t *testing.T) {
	t.Parallel()
	svc, _, raw, dsn := upgradedRepository(t)
	ctx := context.Background()

	// The store itself refuses the two spellings of "inactive".
	if _, err := raw.Exec(`UPDATE repositories SET signed_from_seq = 0`); err == nil {
		t.Fatal("signed_from_seq = 0 was stored; the CHECK is gone")
	}
	if _, err := raw.Exec(`UPDATE repositories SET signed_from_seq = signed_from_seq + 100`); err != nil {
		t.Fatalf("move the mark: %v", err)
	}
	reportFinding(t, svc, "disagrees with the repository's activation mark")
	reportFinding(t, svc, "beyond the head")

	// A keyed reopen runs ensureActivationEpoch; an activate epoch exists and
	// disagrees, so the repair must refuse rather than sign the moved mark.
	_ = svc.Close()
	svc2, err := Open(ctx, dsn,
		WithKindsDir("../../kinds/core.substrate.reamde.dev"),
		WithCredentialKey("test-cred-key"))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = svc2.Close() })
	if _, err := svc2.Dataset(ctx, "geoah"); err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	var activates int
	if err := raw.QueryRow(`SELECT count(*) FROM chain_epochs WHERE reason = 'activate'`).Scan(&activates); err != nil {
		t.Fatalf("count activate epochs: %v", err)
	}
	if activates != 1 {
		t.Fatalf("the repair minted an epoch for a moved mark: %d activate epochs", activates)
	}
	reportFinding(t, svc2.(*service), "disagrees with the repository's activation mark")
}
