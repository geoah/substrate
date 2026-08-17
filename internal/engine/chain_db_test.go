package engine_test

// The chain, end to end: every committed entry carries a hash linking to the
// previous one; verify recomputes the lot from the stored bytes and names the
// first touched seq on any tamper; the backfill stamps pre-chain history at
// first open and records the epoch; signing makes removal detectable and
// unsigned appends impossible; and the two sanctioned rewrites (reseal) and
// refusals (rebuild over tampered history) behave as documented.

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

type chainVerifier interface {
	VerifyRepository(ctx context.Context, username string) (engine.VerifyReport, error)
	VerifyRepositoryPinned(ctx context.Context, username string, pins engine.VerifyPins) (engine.VerifyReport, error)
}

type forceRebuilder interface {
	RebuildRepositoryUnverified(ctx context.Context, username string) (engine.RebuildReport, error)
}

// rawDB opens the DSN's own superuser connection: the tamperer's seat, which
// bypasses row level security on purpose.
func rawDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// newChainDataset is newDataset with the DSN kept: these tests need the
// tamperer's seat beside the front door.
func newChainDataset(t *testing.T, opts ...engine.Option) (substrate.Service, substrate.Dataset, string) {
	t.Helper()
	svc, dsn := newService(t, opts...)
	ctx := context.Background()
	if _, err := svc.CreateRepository(ctx, "geoah"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	importVocabulary(t, ds, "tasks")
	return svc, ds, dsn
}

func mustVerify(t *testing.T, svc substrate.Service, username string) engine.VerifyReport {
	t.Helper()
	report, err := svc.(chainVerifier).VerifyRepository(context.Background(), username)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	return report
}

func findingContaining(report engine.VerifyReport, substr string) bool {
	for _, f := range report.Findings {
		if strings.Contains(f, substr) {
			return true
		}
	}
	return false
}

func TestChainEveryEntryHashedAndVerifies(t *testing.T) {
	t.Parallel()
	svc, ds, dsn := newChainDataset(t)
	ctx := context.Background()
	task := mustPut(t, ds, owner, substrate.PutInput{
		Kind:       "tasks.substrate.reamde.dev/task",
		Properties: map[string]any{"name": "Ship it"},
	})
	mustPatch(t, ds, owner, "tasks.substrate.reamde.dev/task", task.ID, substrate.PatchInput{
		Properties: map[string]any{"name": "Ship it now"},
	})
	if _, err := ds.Delete(ctx, owner, "tasks.substrate.reamde.dev/task", task.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	db := rawDB(t, dsn)
	var total, hashed int64
	if err := db.QueryRow(`SELECT count(*), count(hash) FROM changelog`).Scan(&total, &hashed); err != nil {
		t.Fatalf("count: %v", err)
	}
	if total == 0 || hashed != total {
		t.Fatalf("%d of %d entries carry a hash; every one must", hashed, total)
	}
	report := mustVerify(t, svc, "geoah")
	if !report.OK {
		t.Fatalf("a fresh repository does not verify: %+v", report.Findings)
	}
	if report.Entries != total || report.HeadHash == "" {
		t.Fatalf("report reads %d entries, head hash %q; want %d entries and a head hash",
			report.Entries, report.HeadHash, total)
	}
	// The wire carries the hash as a hex receipt.
	changes := changesSince(t, ds, 0)
	for _, ch := range changes {
		if len(ch.Hash) != 64 {
			t.Fatalf("seq %d rides the wire with hash %q; want 64 hex chars", ch.Seq, ch.Hash)
		}
	}
}

func TestChainNamesEveryTamper(t *testing.T) {
	t.Parallel()
	svc, ds, dsn := newChainDataset(t)
	mustPut(t, ds, owner, substrate.PutInput{Kind: "tasks.substrate.reamde.dev/task", Properties: map[string]any{"name": "a"}})
	mustPut(t, ds, owner, substrate.PutInput{Kind: "tasks.substrate.reamde.dev/task", Properties: map[string]any{"name": "b"}})
	db := rawDB(t, dsn)

	var mid int64
	if err := db.QueryRow(`SELECT max(seq) - 1 FROM changelog`).Scan(&mid); err != nil {
		t.Fatalf("pick a seq: %v", err)
	}
	for name, tamper := range map[string]string{
		"payload": `UPDATE changelog SET payload = jsonb_set(payload, '{forged}', 'true') WHERE seq = $1`,
		"actor":   `UPDATE changelog SET actor = 'console' WHERE seq = $1`,
		// The principal is hashed like every other column, so rewriting the
		// token an entry names is a tamper the verifier catches — which is
		// what makes the stamped attribution worth anything.
		"principal": `UPDATE changelog SET principal = 'tok-forged' WHERE seq = $1`,
		"op":        `UPDATE changelog SET op = 'patch' WHERE seq = $1`,
		"ts":        `UPDATE changelog SET ts = ts + interval '1 second' WHERE seq = $1`,
	} {
		t.Run(name, func(t *testing.T) {
			var before []byte
			if err := db.QueryRow(`SELECT payload::text FROM changelog WHERE seq = $1`, mid).Scan(&before); err != nil {
				t.Fatalf("snapshot: %v", err)
			}
			var tsBefore, actorBefore, opBefore, principalBefore string
			if err := db.QueryRow(`SELECT ts::text, actor, op, principal FROM changelog WHERE seq = $1`, mid).
				Scan(&tsBefore, &actorBefore, &opBefore, &principalBefore); err != nil {
				t.Fatalf("snapshot: %v", err)
			}
			if _, err := db.Exec(tamper, mid); err != nil {
				t.Fatalf("tamper: %v", err)
			}
			report := mustVerify(t, svc, "geoah")
			if report.OK || !findingContaining(report, "hash mismatch") {
				t.Fatalf("a %s tamper at seq %d was not named: %+v", name, mid, report.Findings)
			}
			// Put it back so the subtests stay independent — every hashed
			// column to exactly the value the entry was written with.
			if _, err := db.Exec(`UPDATE changelog SET payload = $2::jsonb, ts = $3::timestamptz, actor = $4, op = $5, principal = $6 WHERE seq = $1`,
				mid, before, tsBefore, actorBefore, opBefore, principalBefore); err != nil {
				t.Fatalf("restore: %v", err)
			}
			if report := mustVerify(t, svc, "geoah"); !report.OK {
				t.Fatalf("restore did not restore: %+v", report.Findings)
			}
		})
	}
}

func TestChainDeletionReadsAsGapAndRebuildRefuses(t *testing.T) {
	t.Parallel()
	svc, ds, dsn := newChainDataset(t)
	mustPut(t, ds, owner, substrate.PutInput{Kind: "tasks.substrate.reamde.dev/task", Properties: map[string]any{"name": "a"}})
	mustPut(t, ds, owner, substrate.PutInput{Kind: "tasks.substrate.reamde.dev/task", Properties: map[string]any{"name": "b"}})
	db := rawDB(t, dsn)
	var mid int64
	if err := db.QueryRow(`SELECT max(seq) - 1 FROM changelog`).Scan(&mid); err != nil {
		t.Fatalf("pick a seq: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM changelog WHERE seq = $1`, mid); err != nil {
		t.Fatalf("delete: %v", err)
	}
	report := mustVerify(t, svc, "geoah")
	if report.OK || !findingContaining(report, "gap") {
		t.Fatalf("a deleted entry was not named as a gap: %+v", report.Findings)
	}
	// Tampered history refuses to become the live fold — and the explicit
	// escape hatch installs it while saying so.
	if _, err := svc.(rebuilder).RebuildRepository(context.Background(), "geoah"); err == nil {
		t.Fatal("rebuild accepted a chain that does not verify")
	}
	forced, err := svc.(forceRebuilder).RebuildRepositoryUnverified(context.Background(), "geoah")
	if err != nil {
		t.Fatalf("forced rebuild: %v", err)
	}
	if !forced.Unverified {
		t.Fatal("the forced rebuild's report does not carry the unverified mark")
	}
}

func TestChainTailTruncationIsTheDocumentedLimit(t *testing.T) {
	t.Parallel()
	svc, ds, dsn := newChainDataset(t)
	mustPut(t, ds, owner, substrate.PutInput{Kind: "tasks.substrate.reamde.dev/task", Properties: map[string]any{"name": "a"}})
	before := mustVerify(t, svc, "geoah")
	mustPut(t, ds, owner, substrate.PutInput{Kind: "tasks.substrate.reamde.dev/task", Properties: map[string]any{"name": "b"}})
	after := mustVerify(t, svc, "geoah")
	db := rawDB(t, dsn)
	if _, err := db.Exec(`DELETE FROM changelog WHERE seq = (SELECT max(seq) FROM changelog)`); err != nil {
		t.Fatalf("truncate the tail: %v", err)
	}
	// A from-scratch walk cannot see a truncated tail: the chain verifies.
	// What catches it is a REMEMBERED head — the receipt the reports hand out
	// — which is exactly what the docs tell an operator to write down.
	truncated := mustVerify(t, svc, "geoah")
	if !truncated.OK {
		t.Fatalf("tail truncation should verify internally: %+v", truncated.Findings)
	}
	if truncated.HeadHash == after.HeadHash {
		t.Fatal("the head did not move; the truncation test cut nothing")
	}
	if truncated.HeadHash != before.HeadHash {
		t.Fatal("cutting the tail did not rewind the head to the earlier receipt")
	}
	// And PINNED, the same receipt turns the invisible truncation into a
	// finding: this is the enforced version of "write the head down".
	pinned, err := svc.(chainVerifier).VerifyRepositoryPinned(context.Background(), "geoah",
		engine.VerifyPins{HeadSeq: after.Head, HeadHash: after.HeadHash})
	if err != nil {
		t.Fatalf("pinned verify: %v", err)
	}
	if pinned.OK || !findingContaining(pinned, "truncated") {
		t.Fatalf("the pinned head did not catch the truncation: %+v", pinned.Findings)
	}
	// A pinned key on an unsigned repository is a mismatch too: pins hold
	// whatever the caller knows.
	keyed, err := svc.(chainVerifier).VerifyRepositoryPinned(context.Background(), "geoah",
		engine.VerifyPins{PublicKey: "deadbeef"})
	if err != nil {
		t.Fatalf("pinned verify: %v", err)
	}
	if keyed.OK || !findingContaining(keyed, "public key") {
		t.Fatalf("a pinned public key mismatch was not named: %+v", keyed.Findings)
	}
}

func TestChainBackfillStampsLegacyHistory(t *testing.T) {
	t.Parallel()
	svc, ds, dsn := newChainDataset(t)
	ctx := context.Background()
	mustPut(t, ds, owner, substrate.PutInput{Kind: "tasks.substrate.reamde.dev/task", Properties: map[string]any{"name": "old world"}})
	db := rawDB(t, dsn)
	// Wind the chain back: a store written before the chain (and before
	// signing) existed — all-zero signatures, no signing state at all.
	if _, err := db.Exec(`UPDATE changelog SET hash = NULL, sig = decode(repeat('00', 64), 'hex')`); err != nil {
		t.Fatalf("wind back: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM chain_epochs`); err != nil {
		t.Fatalf("wind back epochs: %v", err)
	}
	if _, err := db.Exec(`UPDATE repositories SET signing_key = NULL, signing_public = NULL, signed_from_seq = NULL`); err != nil {
		t.Fatalf("wind back signing: %v", err)
	}
	// Plant one raw legacy entry with numbers a float64 cannot carry: the
	// backfill and the verifier must agree on its canonical form.
	repoID := testdb.RepositoryID(t, dsn, "geoah")
	if _, err := db.Exec(`
		INSERT INTO changelog (repository, seq, ts, actor, principal, op, record_id, kind, payload, sig)
		VALUES ($1, (SELECT max(seq) + 1 FROM changelog WHERE repository = $1), now(), 'api', 'invalid', 'put', 'x', 'task',
			'{"n": 9007199254740993, "f": 1.50, "e": 1e3, "neg": -0.0}'::jsonb, decode(repeat('00', 64), 'hex'))`, repoID); err != nil {
		t.Fatalf("plant a legacy entry: %v", err)
	}

	// Wound back, the store is what a pre-signing release left behind, and
	// verify says so by name before anything touches it.
	never := mustVerify(t, svc, "geoah")
	if never.OK || !findingContaining(never, "never activated") {
		t.Fatalf("a never-activated repository verified clean: %+v", never.Findings)
	}

	// A fresh service on the same store: first open backfills, activates
	// signing on the settled head, then serves.
	svc2, err := engine.Open(ctx, dsn, engine.WithKindsDir("../../kinds/core.substrate.reamde.dev"),
		engine.WithCredentialKey("test-cred-key"))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = svc2.Close() })
	if _, err := svc2.Dataset(ctx, "geoah"); err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	var unhashed int64
	if err := db.QueryRow(`SELECT count(*) FROM changelog WHERE hash IS NULL`).Scan(&unhashed); err != nil {
		t.Fatalf("count: %v", err)
	}
	if unhashed != 0 {
		t.Fatalf("%d entries still unhashed after the first open", unhashed)
	}
	report := mustVerify(t, svc2, "geoah")
	if report.SignedFrom == 0 {
		t.Fatalf("the keyed reopen did not activate signing: %+v", report)
	}
	// The hashes check, so the backfill did its job. The entries below the
	// activation seq still carry no signature, and that is a finding now:
	// nothing sanctioned can sign the past, so nothing vouches for it.
	if report.UnsignedEntries != report.SignedFrom-1 {
		t.Fatalf("%d unsigned entries below activation seq %d; want %d",
			report.UnsignedEntries, report.SignedFrom, report.SignedFrom-1)
	}
	if report.OK || !findingContaining(report, "carry no signature") {
		t.Fatalf("an unsigned prefix was not named: %+v", report.Findings)
	}
	for _, f := range report.Findings {
		if strings.Contains(f, "hash") {
			t.Fatalf("the backfill left a hash finding: %s", f)
		}
	}
	backfilled := false
	for _, ep := range report.Epochs {
		if ep.Reason == "backfill" {
			backfilled = true
		}
	}
	if !backfilled {
		t.Fatal("no backfill epoch was recorded: attested history has no stated beginning")
	}
}

func TestChangelogSigningSignsAndDetectsRemoval(t *testing.T) {
	t.Parallel()
	// No option: signing is mandatory, and a keyed host activates on its own.
	svc, ds, dsn := newChainDataset(t)
	mustPut(t, ds, owner, substrate.PutInput{
		Kind:       "tasks.substrate.reamde.dev/task",
		Properties: map[string]any{"name": "signed"},
	})

	report := mustVerify(t, svc, "geoah")
	if !report.OK {
		t.Fatalf("a signed repository does not verify: %+v", report.Findings)
	}
	if report.SignedFrom == 0 || report.PublicKey == "" {
		t.Fatalf("signing state missing from the report: %+v", report)
	}
	activated := false
	for _, ep := range report.Epochs {
		if ep.Reason == "activate" {
			activated = true
			if !ep.Signed || ep.SigOK == nil || !*ep.SigOK {
				t.Fatalf("the activation epoch is not signed and verified: %+v", ep)
			}
		}
	}
	if !activated {
		t.Fatal("no activation epoch recorded")
	}
	db := rawDB(t, dsn)
	var signed, covered int64
	if err := db.QueryRow(`
		SELECT count(*) FILTER (WHERE sig <> decode(repeat('00', 64), 'hex')), count(*)
		FROM changelog WHERE seq >= $1`,
		report.SignedFrom).Scan(&signed, &covered); err != nil {
		t.Fatalf("count: %v", err)
	}
	if covered == 0 || signed != covered {
		t.Fatalf("%d of %d covered entries carry a real signature", signed, covered)
	}
	// A fresh repository activates before its seed, so nothing predates the
	// guarantee.
	if report.SignedFrom != 1 || report.UnsignedEntries != 0 {
		t.Fatalf("a fresh repository is signed from seq %d with %d unsigned entries; want seq 1 and none",
			report.SignedFrom, report.UnsignedEntries)
	}
	// Signature removal is the downgrade the durable mark exists to catch:
	// with sig NOT NULL, stripping one means writing the all-zero value.
	if _, err := db.Exec(`UPDATE changelog SET sig = decode(repeat('00', 64), 'hex') WHERE seq = (SELECT max(seq) FROM changelog)`); err != nil {
		t.Fatalf("strip a signature: %v", err)
	}
	stripped := mustVerify(t, svc, "geoah")
	if stripped.OK || !findingContaining(stripped, "all-zero signature") {
		t.Fatalf("a stripped signature was not named: %+v", stripped.Findings)
	}
}

func TestSigningActivationIsOneWay(t *testing.T) {
	t.Parallel()
	// Every repository is signed from seq 1 and the activation mark is
	// durable, so the only way to reach "activated but cannot sign" is a
	// stored seed that will not open — what a rotated or restored-wrong
	// credential key looks like from inside. Reads keep working and every
	// write refuses: the guarantee does not quietly shed.
	svc, ds, dsn := newChainDataset(t)
	ctx := context.Background()
	mustPut(t, ds, owner, substrate.PutInput{
		Kind:       "tasks.substrate.reamde.dev/task",
		Properties: map[string]any{"name": "signed"},
	})
	db := rawDB(t, dsn)
	var sig []byte
	if err := db.QueryRow(`SELECT sig FROM changelog WHERE seq = (SELECT max(seq) FROM changelog)`).Scan(&sig); err != nil {
		t.Fatalf("read sig: %v", err)
	}
	if len(sig) != 64 || string(sig) == string(make([]byte, 64)) {
		t.Fatalf("an append is not signed (%d bytes)", len(sig))
	}
	_ = svc.Close()

	// Sealed framing ('s'), then bytes the credential key cannot open.
	if _, err := db.Exec(
		`UPDATE repositories SET signing_key = decode('73' || repeat('ff', 60), 'hex')`); err != nil {
		t.Fatalf("spoil the stored seed: %v", err)
	}
	svc2, err := engine.Open(ctx, dsn,
		engine.WithKindsDir("../../kinds/core.substrate.reamde.dev"),
		engine.WithCredentialKey("test-cred-key"))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = svc2.Close() })
	ds2, err := svc2.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("open dataset with an unopenable seed: %v", err)
	}
	if _, err := ds2.Put(ctx, owner, substrate.PutInput{
		Kind:       "tasks.substrate.reamde.dev/task",
		Properties: map[string]any{"name": "must refuse"},
	}); err == nil ||
		!strings.Contains(err.Error(), "signing key is unavailable") {
		t.Fatalf("a host that cannot sign appended anyway: %v", err)
	}
	if _, err := svc2.(resealer).ResealRepository(ctx, "geoah"); err == nil {
		t.Fatal("a reseal without the signing key did not refuse")
	}
	// The durable mark stands: nothing about a missing key deactivates it.
	after := mustVerify(t, svc2, "geoah")
	if after.SignedFrom != 1 || after.UnsignedEntries != 0 {
		t.Fatalf("the activation mark moved: signed from %d, %d unsigned entries",
			after.SignedFrom, after.UnsignedEntries)
	}
}

func TestResealRefusesTamperThenRechainsLegacy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, db, dsn := newSealingDatasetDSN(t)
	sgPutProvider(t, ds, sgPlainKey)
	ref := storedAPIKeyRef(t, db)

	// A hand rewrite of hashed history reads as tampering, and the reseal
	// REFUSES rather than laundering it into fresh hashes (and, on a signed
	// repository, fresh signatures).
	const legacy = "sk-legacy-77777"
	if _, err := db.Exec(`UPDATE changelog SET payload = replace(payload::text, $1, $2)::jsonb
		WHERE payload::text LIKE '%' || $1 || '%'`, ref, legacy); err != nil {
		t.Fatalf("plant legacy changelog: %v", err)
	}
	if _, err := db.Exec(`UPDATE records SET props = jsonb_set(props, '{apiKey}', to_jsonb($1::text))
		WHERE id = 'prov'`, legacy); err != nil {
		t.Fatalf("plant legacy record: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM sealed WHERE ref = $1`, ref); err != nil {
		t.Fatalf("drop the store row: %v", err)
	}
	planted := mustVerify(t, svc, "geoah")
	if planted.OK {
		t.Fatal("hand-rewritten history verified; the chain saw nothing")
	}
	if _, err := svc.(resealer).ResealRepository(ctx, "geoah"); err == nil ||
		!strings.Contains(err.Error(), "reseal refuses") {
		t.Fatalf("a reseal over tampered history did not refuse: %v", err)
	}

	// The honest legacy state predates the chain and signing both: no
	// hashes, all-zero signatures, no signing state. The backfill
	// notarizes the planted bytes at the next open, and only THEN does the
	// reseal — the one sanctioned rewrite — re-chain what it rewrites and
	// record the epoch that explains the moved head.
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
	svc2, err := engine.Open(ctx, dsn, engine.WithKindsDir("../../kinds/core.substrate.reamde.dev"),
		engine.WithCredentialKey("test-cred-key"))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = svc2.Close() })
	if _, err := svc2.Dataset(ctx, "geoah"); err != nil {
		t.Fatalf("reopen dataset: %v", err)
	}
	report, err := svc2.(resealer).ResealRepository(ctx, "geoah")
	if err != nil {
		t.Fatalf("reseal: %v", err)
	}
	if report.Entries == 0 {
		t.Fatalf("the reseal rewrote nothing: %+v", report)
	}
	// The chain verifies: the only finding left is the unsigned prefix the
	// reseal cannot sign and does not pretend to.
	after := mustVerify(t, svc2, "geoah")
	for _, f := range after.Findings {
		if !strings.Contains(f, "carry no signature") {
			t.Fatalf("the resealed chain does not verify: %+v", after.Findings)
		}
	}
	resealed := false
	for _, ep := range after.Epochs {
		if ep.Reason == "reseal" {
			resealed = true
			if ep.OldHead == "" || ep.NewHead == "" || ep.FromSeq == 0 {
				t.Fatalf("the reseal epoch does not explain the move: %+v", ep)
			}
		}
	}
	if !resealed {
		t.Fatal("no reseal epoch recorded: the sanctioned rewrite is indistinguishable from tampering")
	}
}
