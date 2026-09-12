package engine

// The sealed store from the inside. Its one key (decision 0059): a repository
// refuses a plain payload and one sealed under the host key, and a wrong or
// damaged DEK wrap is reported by key id. Its addressing: a payload binds to
// the row it was written at, and two secret properties on one record never
// share a sealed row. And the credential material's compare-and-swap: a
// rotation is never lost and the TOTP replay step never moves backwards.
//
// INTERNAL tests throughout: they plant bytes in the sealed table, damage the
// control-plane row, and drive verifyFactors / rewriteCredential /
// consumeTOTPStep directly, so the window between reading material and
// writing it back is reproducible rather than left to goroutine timing.

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// plantSealed overwrites one sealed row's payload through the scoped pool.
func plantSealed(t *testing.T, ctx context.Context, ds *dataset, ref string, payload []byte) {
	t.Helper()
	if _, err := ds.db.ExecContext(ctx, `UPDATE sealed SET payload = $1 WHERE ref = $2`, payload, ref); err != nil {
		t.Fatalf("plant payload at %s: %v", ref, err)
	}
}

// hostSealed seals raw under the service's host key, bound to the row, as a
// pre-DEK release sealed it.
func hostSealed(t *testing.T, ds *dataset, raw, aad []byte) []byte {
	t.Helper()
	aead, err := aeadOf(ds.svc.credKey)
	if err != nil || aead == nil {
		t.Fatalf("host key aead: %v", err)
	}
	out, err := sealWith(aead, raw, aad)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// repoKeyRow reads the control-plane row's key columns.
func repoKeyRow(t *testing.T, ctx context.Context, svc *service, repoID string) (hasDEK bool, keyID string) {
	t.Helper()
	var id *string
	if err := svc.maint.QueryRowContext(ctx,
		`SELECT dek IS NOT NULL, dek_key_id FROM repositories WHERE id = $1`, repoID).
		Scan(&hasDEK, &id); err != nil {
		t.Fatalf("read the repository row: %v", err)
	}
	if id != nil {
		keyID = *id
	}
	return hasDEK, keyID
}

func TestARepositoryRefusesAPlainOrHostKeyedPayload(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	ctx := context.Background()
	ds := openInternalDataset(t)
	repoID := ds.scope.Repository

	// Born with a DEK, and the wrap names this host's key.
	hasDEK, keyID := repoKeyRow(t, ctx, ds.svc, repoID)
	if !hasDEK || keyID != hostKeyID(TestCredentialKeyBytes) {
		t.Fatalf("a fresh repository is not born with a DEK under the host key: dek=%v keyID=%q", hasDEK, keyID)
	}
	m, err := changelogfile.ReadManifest(ds.dir)
	if err != nil || m.DEKKeyID != keyID {
		t.Fatalf("the manifest does not carry the key id: %+v, %v", m, err)
	}

	ref := putProviderSecret(t, ds, "a", "secret-a")
	aad := sealedAAD(ref, bindingProviderKind, "a")

	// A plain payload is refused on every read path, by framing.
	plantSealed(t, ctx, ds, ref, append([]byte{credPlain}, []byte("secret-a")...))
	if got, err := ds.openSecretValue(ctx, ref); err == nil || !strings.Contains(err.Error(), "'p'") {
		t.Fatalf("the dataset read a plain payload: %q, %v", got, err)
	}
	if got, err := ds.svc.openSealed(ctx, repoID, ref); err == nil || !strings.Contains(err.Error(), "'p'") {
		t.Fatalf("the maintenance read opened a plain payload: %q, %v", got, err)
	}

	// A payload sealed under the host key is refused too, naming the key
	// expected: the host key is never tried on a repository's payload.
	plantSealed(t, ctx, ds, ref, hostSealed(t, ds, []byte("secret-a"), aad))
	if got, err := ds.openSecretValue(ctx, ref); err == nil || !strings.Contains(err.Error(), "repository DEK") {
		t.Fatalf("the dataset opened a host-key payload: %q, %v", got, err)
	}
	if got, err := ds.svc.openSealed(ctx, repoID, ref); err == nil || !strings.Contains(err.Error(), "repository DEK") {
		t.Fatalf("the maintenance read opened a host-key payload: %q, %v", got, err)
	}
}

// A host booting with the wrong SUBSTRATE_CREDENTIAL_KEY is told which key the
// wraps name and which key it holds, by id, beside the repository.
func TestWrongHostKeyIsNamedById(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	ctx := context.Background()
	ds := openInternalDataset(t)
	repoID := ds.scope.Repository
	dsn, root := ds.svc.dsn, ds.svc.dataRoot
	_ = ds.svc.Close()

	other := make([]byte, 32)
	if _, err := rand.Read(other); err != nil {
		t.Fatal(err)
	}
	_, err := OpenForTest(t, ctx, dsn,
		WithDataRoot(root),
		WithKindsDir(SeedKindsDir),
		WithCredentialKey(base64.StdEncoding.EncodeToString(other)))
	if err == nil {
		t.Fatal("a host with the wrong key booted over the database")
	}
	msg := err.Error()
	for _, want := range []string{"SUBSTRATE_CREDENTIAL_KEY", repoID, hostKeyID(TestCredentialKeyBytes), hostKeyID(other)} {
		if !strings.Contains(msg, want) {
			t.Fatalf("the refusal does not name %q: %v", want, err)
		}
	}
}

// A wrap that names this host's key and still does not open is damage, and
// the boot must say so rather than send the operator hunting for another key.
func TestBootNamesADamagedWrapUnderTheSameKey(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	ctx := context.Background()
	ds := openInternalDataset(t)
	repoID := ds.scope.Repository
	dsn, root := ds.svc.dsn, ds.svc.dataRoot
	// Flip the wrap's last byte, the GCM tag's, and leave the id it names.
	if _, err := ds.svc.maint.ExecContext(ctx,
		`UPDATE repositories SET dek = set_byte(dek, length(dek) - 1, (get_byte(dek, length(dek) - 1) + 1) % 256) WHERE id = $1`, repoID); err != nil {
		t.Fatalf("damage the wrap: %v", err)
	}
	_ = ds.svc.Close()

	_, err := OpenForTest(t, ctx, dsn,
		WithDataRoot(root),
		WithKindsDir(SeedKindsDir),
		WithCredentialKey(TestCredentialKey))
	if err == nil {
		t.Fatal("a damaged wrap booted")
	}
	msg := err.Error()
	for _, want := range []string{repoID, hostKeyID(TestCredentialKeyBytes), "damaged", "keep it"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("the refusal does not say %q: %v", want, err)
		}
	}
	if strings.Contains(msg, "written under a different one") {
		t.Fatalf("a damaged wrap under the right key was blamed on the key: %v", err)
	}
}

// Two secret properties on one record must not share a sealed row. The write
// path recognizes a carried ref PER-PROPERTY, so a value naming the row a
// DIFFERENT property owns is refused rather than stored verbatim. Without the
// gate, property B adopts property A's ref, and rotating A runs
// `DELETE FROM sealed` on the row B still points at, erasing B's material.
// The test reaches ds.openSecretValue to prove B's material survives, which
// the wire redacts.
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
			map[string]any{"singular": "vault"},
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

// A sealed payload binds to the address it was written at (ADR 0023): moving
// a row's ciphertext onto another row fails the open. The tests below reach
// openSecretValue directly and plant bytes in the sealed table the way an
// attacker with table write access would.
const bindingProviderKind = "substrate.reamde.dev/llm/provider"

// putProviderSecret writes one llm/provider row with a secret apiKey and returns
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

// Concurrent credential changes must not lose a rotation or move the TOTP
// replay step backwards. The cases below drive verifyFactors /
// rewriteCredential / consumeTOTPStep directly so the racy window between
// "read the material" and "write the new material" is reproducible rather
// than left to goroutine timing.
func openCredentialService(t *testing.T) (*service, Repository) {
	t.Helper()
	ctx := context.Background()
	dsn := MigratedDSN(t)
	svcIface, err := OpenForTest(t, ctx, dsn, WithDataRoot(t.TempDir()), WithCredentialKey(TestCredentialKey), WithKindsDir(SeedKindsDir))
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = svcIface.Close() })
	s := svcIface.(*service)

	seed, err := NewTOTPSecret()
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Register consuming an EARLIER step, so two later, still-valid adjacent
	// codes remain for a change to spend.
	base := TOTPStep(nowUTC())
	code, err := TOTPCode(seed, base-1)
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	if _, err := s.Register(ctx, substrate.RegisterInput{
		Repository: testdb.Repository(t), Password: "correct-horse-battery-staple",
		TOTPSecret: seed, TOTPCode: code, Label: "cli",
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	repo, err := s.repositoryByID(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	return s, repo
}

// TestCredentialRewriteCASPreventsLostUpdate reproduces the exact lost-update:
// a password change and a TOTP re-enrollment both read the SAME baseline
// credential, then both try to write. The compare-and-swap on the refs must
// turn the second write into a CONFLICT rather than a silent overwrite that
// reverts the first.
func TestCredentialRewriteCASPreventsLostUpdate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, repo := openCredentialService(t)
	ds, err := s.open(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}

	// Both writers read the SAME baseline — the racy window.
	matA, err := s.authMaterialOf(ctx, repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	matB, err := s.authMaterialOf(ctx, repo.ID)
	if err != nil {
		t.Fatal(err)
	}

	const newPassword = "a-fresh-passphrase-1"
	hash, err := hashPassword(newPassword)
	if err != nil {
		t.Fatal(err)
	}
	// A (password change) commits first, keeping the seed.
	if err := ds.rewriteCredential(ctx, credentialWrite{
		repository: testdb.Repository(t), passwordHash: hash,
		totp:              matA.totp,
		expectPasswordRef: matA.passwordRef, expectTotpRef: matA.totpRef, casEnabled: true,
	}); err != nil {
		t.Fatalf("A (password change) failed: %v", err)
	}

	// B (TOTP re-enroll) started from the SAME baseline and carries the OLD
	// password hash. Without the CAS it would overwrite A — reverting the
	// password. It must be refused as a conflict.
	newSeed, err := NewTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	err = ds.rewriteCredential(ctx, credentialWrite{
		repository: testdb.Repository(t), passwordHash: matB.passwordHash,
		totp:              totpMaterial{Secret: newSeed, Step: 1},
		expectPasswordRef: matB.passwordRef, expectTotpRef: matB.totpRef, casEnabled: true,
	})
	if !errors.Is(err, errCredentialConflict) {
		t.Fatalf("B's stale write = %v, want errCredentialConflict (A's rotation must not be silently lost)", err)
	}

	// A's change stands: the new password verifies and the seed is unchanged.
	final, err := s.authMaterialOf(ctx, repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !verifyPassword(final.passwordHash, newPassword) {
		t.Fatal("A's password change did not survive B's stale write")
	}
	if final.totp.Secret != matA.totp.Secret {
		t.Fatal("B's new seed landed despite the conflict — the credential was corrupted")
	}
}

// TestCredentialRewriteStepNeverRegresses: a concurrent login consumes a LATER
// code on the same seed AFTER a password change read the material. The password
// change keeps the seed, so its write must carry the seed's step FORWARD to the
// stored value — never back to the step it read — or the later code could be
// replayed.
func TestCredentialRewriteStepNeverRegresses(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, repo := openCredentialService(t)
	ds, err := s.open(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}

	// The password change reads the material at its (older) step.
	mat, err := s.authMaterialOf(ctx, repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	readStep := mat.totp.Step

	// A concurrent login lands a much later code on the same seed, advancing the
	// stored step under its own row lock.
	advanced := readStep + 25
	won, err := s.consumeTOTPStep(ctx, repo, mat.totpRef, advanced)
	if err != nil || !won {
		t.Fatalf("simulate a concurrent login: won=%v err=%v", won, err)
	}

	// The password change now writes, carrying the STALE step it read. The write
	// must bump it to the stored value, not regress to the stale one.
	hash, err := hashPassword("another-good-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if err := ds.rewriteCredential(ctx, credentialWrite{
		repository: testdb.Repository(t), passwordHash: hash,
		totp:              mat.totp, // carries readStep, which is now stale
		expectPasswordRef: mat.passwordRef, expectTotpRef: mat.totpRef, casEnabled: true,
	}); err != nil {
		t.Fatalf("password change: %v", err)
	}

	final, err := s.authMaterialOf(ctx, repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.totp.Step < advanced {
		t.Fatalf("the replay step regressed to %d, want >= %d (a spent code must not be re-enabled)",
			final.totp.Step, advanced)
	}
}

// TestConcurrentCredentialChangesNeitherSilentlyLost races a real password
// change against a real TOTP re-enrollment on the public service methods, with
// adjacent valid codes. Exactly one must win, and the winner's change must be
// durable — the other must fail (a conflict or a spent code), never silently
// overwrite the winner.
func TestConcurrentCredentialChangesNeitherSilentlyLost(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, repo := openCredentialService(t)

	seed := currentSeed(t, s, repo)
	base := TOTPStep(nowUTC())
	codeA, err := TOTPCode(seed, base) // password change spends this
	if err != nil {
		t.Fatal(err)
	}
	codeB, err := TOTPCode(seed, base+1) // re-enroll spends the adjacent one
	if err != nil {
		t.Fatal(err)
	}
	newSeed, err := NewTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	newSeedCode, err := TOTPCode(newSeed, base)
	if err != nil {
		t.Fatal(err)
	}

	const newPassword = "concurrent-new-passphrase"
	var errA, errB error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		errA = s.ChangePassword(ctx, substrate.LoginInput{
			Repository: testdb.Repository(t), Password: "correct-horse-battery-staple", TOTPCode: codeA,
		}, newPassword)
	}()
	go func() {
		defer wg.Done()
		errB = s.ReenrollTOTP(ctx, substrate.LoginInput{
			Repository: testdb.Repository(t), Password: "correct-horse-battery-staple", TOTPCode: codeB,
		}, newSeed, newSeedCode)
	}()
	wg.Wait()

	if (errA == nil) == (errB == nil) {
		t.Fatalf("exactly one change must win; got errA=%v errB=%v", errA, errB)
	}

	final, err := s.authMaterialOf(ctx, repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	passwordIsNew := verifyPassword(final.passwordHash, newPassword)
	seedIsNew := final.totp.Secret == newSeed
	switch {
	case errA == nil: // the password change won
		if !passwordIsNew || seedIsNew {
			t.Fatalf("the winning password change was not durable: passwordIsNew=%v seedIsNew=%v", passwordIsNew, seedIsNew)
		}
	default: // the re-enroll won
		if !seedIsNew || passwordIsNew {
			t.Fatalf("the winning re-enrollment was not durable: passwordIsNew=%v seedIsNew=%v", passwordIsNew, seedIsNew)
		}
	}
}

// currentSeed reads the repository's current TOTP seed off the sealed material.
func currentSeed(t *testing.T, s *service, repo Repository) string {
	t.Helper()
	mat, err := s.authMaterialOf(context.Background(), repo.ID)
	if err != nil {
		t.Fatalf("read material: %v", err)
	}
	return mat.totp.Secret
}
