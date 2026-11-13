package engine

// Every door that proves the factors and then acts holds the proof to the
// act (factorsStand): a request held between the two while the factors
// change acts on nothing. Each door has its test here, held by the
// after-proof seam while another request changes the password. A mint that
// stands on a token holds that token to authenticating, its expiry included.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

const proofDoorPassword = "a-password-proved-before"

// heldDoor is a service whose after-proof seam holds one door: proved closes
// once a request of that door has proved its factors, and the request acts
// once release is closed.
type heldDoor struct {
	s        *service
	proved   chan struct{}
	release  chan struct{}
	repoName string
}

func openHeldDoor(t *testing.T, door string) *heldDoor {
	t.Helper()
	ctx := context.Background()
	h := &heldDoor{proved: make(chan struct{}), release: make(chan struct{}), repoName: testdb.Repository(t)}
	svc, err := OpenForTest(t, ctx, MigratedDSN(t), WithDataRoot(t.TempDir()), WithCredentialKey(TestCredentialKey),
		WithKindsFS(SeedKinds), WithTestAfterProof(func(name string) {
			if name != door {
				return
			}
			close(h.proved)
			<-h.release
		}))
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	h.s = svc.(*service)
	if _, err := h.s.Register(ctx, substrate.RegisterInput{
		Repository: h.repoName, Password: proofDoorPassword, SkipTOTP: true, Label: "registered",
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	return h
}

func (h *heldDoor) in() substrate.LoginInput {
	return substrate.LoginInput{Repository: h.repoName, Password: proofDoorPassword, Label: "held"}
}

// run starts act, waits until its request proved the factors, changes the
// password from another request, lets the held one act, and answers its
// error.
func (h *heldDoor) run(t *testing.T, act func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- act() }()
	select {
	case <-h.proved:
	case err := <-done:
		t.Fatalf("the request ended before it proved its factors: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("the request never proved its factors")
	}
	if _, err := h.s.SignOutEverywhere(context.Background(), h.in(), "a-password-changed-meanwhile"); err != nil {
		t.Fatalf("the password change and sign out everywhere meanwhile: %v", err)
	}
	close(h.release)
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("the held request never finished")
		return nil
	}
}

func (h *heldDoor) liveTokens(t *testing.T) int {
	t.Helper()
	ds, err := h.s.Dataset(context.Background(), h.repoName)
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := ds.Tokens(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return len(tokens)
}

func wantFactorsChanged(t *testing.T, door string, err error) {
	t.Helper()
	if !errors.Is(err, substrate.ErrAuth) && !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("%s held across a password change and a sign out everywhere: %v, want it refused", door, err)
	}
}

// The finding: a sign-in that proved the old password, held, mints nothing
// after the password changed and every token ended.
func TestASignInHeldAfterItsProofMintsNothingAfterASignOut(t *testing.T) {
	t.Parallel()
	h := openHeldDoor(t, "login")
	err := h.run(t, func() error {
		_, _, err := h.s.Login(context.Background(), h.in())
		return err
	})
	wantFactorsChanged(t, "a sign-in", err)
	if n := h.liveTokens(t); n != 0 {
		t.Fatalf("%d tokens stand after the sign out everywhere, want none", n)
	}
}

func TestAPasswordChangeHeldAfterItsProofChangesNothing(t *testing.T) {
	t.Parallel()
	h := openHeldDoor(t, "password")
	err := h.run(t, func() error {
		return h.s.ChangePassword(context.Background(), h.in(), "a-password-from-the-old-proof")
	})
	wantFactorsChanged(t, "a password change", err)
	if _, _, err := h.s.Login(context.Background(), substrate.LoginInput{
		Repository: h.repoName, Password: "a-password-changed-meanwhile",
	}); err != nil {
		t.Fatalf("the change made meanwhile does not stand: %v", err)
	}
}

func TestASecondFactorHeldAfterItsProofEnrollsNothing(t *testing.T) {
	t.Parallel()
	h := openHeldDoor(t, "totp")
	seed, err := NewTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	code, err := TOTPCode(seed, TOTPStep(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	err = h.run(t, func() error {
		return h.s.ReenrollTOTP(context.Background(), h.in(), seed, code)
	})
	wantFactorsChanged(t, "a second factor's enrollment", err)
}

func TestASignOutHeldAfterItsProofEndsNothing(t *testing.T) {
	t.Parallel()
	h := openHeldDoor(t, "signout")
	done := make(chan error, 1)
	go func() {
		_, err := h.s.SignOutEverywhere(context.Background(), h.in(), "")
		done <- err
	}()
	<-h.proved
	// The factors change meanwhile, and a sign-in under the new ones stands.
	if err := h.s.ChangePassword(context.Background(), h.in(), "a-password-changed-meanwhile"); err != nil {
		t.Fatalf("the password change meanwhile: %v", err)
	}
	if _, _, err := h.s.Login(context.Background(), substrate.LoginInput{
		Repository: h.repoName, Password: "a-password-changed-meanwhile", Label: "new",
	}); err != nil {
		t.Fatalf("a sign-in under the new password: %v", err)
	}
	before := h.liveTokens(t)
	close(h.release)
	wantFactorsChanged(t, "a sign out everywhere", <-done)
	if n := h.liveTokens(t); n != before {
		t.Fatalf("%d tokens stand after the refused sign-out, want the %d before it", n, before)
	}
}

func TestADeleteHeldAfterItsProofErasesNothing(t *testing.T) {
	t.Parallel()
	h := openHeldDoor(t, "delete")
	err := h.run(t, func() error {
		return h.s.DeleteRepository(context.Background(), h.in())
	})
	wantFactorsChanged(t, "a repository's delete", err)
	if _, err := h.s.repositoryByAuthority(context.Background(), h.repoName); err != nil {
		t.Fatalf("the repository is gone after a refused delete: %v", err)
	}
	if _, err := h.s.Dataset(context.Background(), h.repoName); err != nil {
		t.Fatalf("the repository does not open after a refused delete: %v", err)
	}
}

// A mint standing on a token holds it to authenticating: one authenticated
// before the token's time ran out mints nothing after.
func TestAMintStandingOnAnExpiredTokenMintsNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := newRaceDataset(t)
	soon := time.Now().Add(1500 * time.Millisecond).UTC().Truncate(time.Second).Add(time.Second)
	asker, _, err := ds.MintToken(ctx, substrate.MintTokenInput{Label: "short", ExpiresAt: &soon})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	onAsker := substrate.WithPrincipal(ctx, asker.ID)
	if _, _, err := ds.MintToken(onAsker, substrate.MintTokenInput{Label: "while it stands"}); err != nil {
		t.Fatalf("a mint on a token not yet expired: %v", err)
	}
	time.Sleep(time.Until(soon) + 200*time.Millisecond)
	if _, _, err := ds.MintToken(onAsker, substrate.MintTokenInput{Label: "after it expired"}); !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("a mint on an expired token = %v, want ErrAuth", err)
	}
}
