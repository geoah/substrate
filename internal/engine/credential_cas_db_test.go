package engine

// Concurrent credential changes must not lose a rotation or move the TOTP
// replay step backwards. These are INTERNAL tests: they drive
// verifyFactors / rewriteCredential / consumeTOTPStep directly so the racy
// window between "read the material" and "write the new material" is
// reproducible rather than left to goroutine timing.

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

func openCredentialService(t *testing.T) (*service, Repository) {
	t.Helper()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	svcIface, err := Open(ctx, dsn, WithKindsDir("../../kinds/core.substrate.reamde.dev"))
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
		Username: "geoah", Password: "correct-horse-battery-staple",
		TOTPSecret: seed, TOTPCode: code, Label: "cli",
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	repo, err := s.repositoryByUsername(ctx, "geoah")
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
		username: "geoah", passwordHash: hash,
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
		username: "geoah", passwordHash: matB.passwordHash,
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
	won, err := s.consumeTOTPStep(ctx, repo.ID, mat.totpRef, advanced)
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
		username: "geoah", passwordHash: hash,
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
			Username: "geoah", Password: "correct-horse-battery-staple", TOTPCode: codeA,
		}, newPassword)
	}()
	go func() {
		defer wg.Done()
		errB = s.ReenrollTOTP(ctx, substrate.LoginInput{
			Username: "geoah", Password: "correct-horse-battery-staple", TOTPCode: codeB,
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
