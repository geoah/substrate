package engine

// Every door that proves the factors and then acts holds the proof to the
// act: the act runs under the identity's row, the lock every change of the
// factors takes, and checks there that they are the ones proved
// (factorsStandOn). A request held between the two while the factors change
// acts on nothing. Each door has its test here, held by the after-proof seam
// once the request has spent its proof. A mint that stands on a token holds
// that token to authenticating: present, not revoked, not expired, and its
// person still one who may open the repository.

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

const (
	proofDoorPassword = "a-password-proved-before"
	changedPassword   = "a-password-changed-meanwhile"
)

// heldDoor is a service whose after-proof seam holds one door: proved closes
// once a request of that door has proved its factors, and the request acts
// once release is closed. It holds nothing until armed, which the setup's
// own registration needs. name is the person's and their repository's.
type heldDoor struct {
	s       *service
	proved  chan struct{}
	release chan struct{}
	armed   atomic.Bool
	name    string
}

func openHeldDoor(t *testing.T, door string) *heldDoor {
	t.Helper()
	ctx := context.Background()
	h := &heldDoor{proved: make(chan struct{}), release: make(chan struct{}), name: testdb.Repository(t)}
	svc, err := OpenForTest(t, ctx, MigratedDSN(t), WithDataRoot(t.TempDir()), WithCredentialKey(TestCredentialKey),
		WithKindsFS(SeedKinds), WithTestAfterProof(func(name string) {
			if name != door || !h.armed.Swap(false) {
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
		Identity: h.name, Repository: h.name, Password: proofDoorPassword, SkipTOTP: true, Label: "registered",
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	h.armed.Store(true)
	return h
}

func (h *heldDoor) in() substrate.LoginInput {
	return substrate.LoginInput{Identity: h.name, Password: proofDoorPassword, Label: "held"}
}

// run starts act, waits until its request proved the factors, runs change
// from another request, lets the held one act, and answers its error.
func (h *heldDoor) run(t *testing.T, act func() error, change func()) error {
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
	change()
	close(h.release)
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("the held request never finished")
		return nil
	}
}

// signOutWithANewPassword is the change of the finding: the password changes
// and every token ends.
func (h *heldDoor) signOutWithANewPassword(t *testing.T) func() {
	return func() {
		if _, err := h.s.SignOutEverywhere(context.Background(), h.in(), changedPassword); err != nil {
			t.Fatalf("the password change and sign out everywhere meanwhile: %v", err)
		}
	}
}

func (h *heldDoor) liveTokens(t *testing.T, repository string) int {
	t.Helper()
	ds, err := h.s.Dataset(context.Background(), repository)
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
		t.Fatalf("%s held across a change of the factors: %v, want it refused", door, err)
	}
}

// The finding: a sign-in that proved the old password, held, mints nothing
// after the password changed and every token ended.
func TestASignInHeldAfterItsProofMintsNothingAfterASignOut(t *testing.T) {
	t.Parallel()
	h := openHeldDoor(t, "login")
	err := h.run(t, func() error {
		_, err := h.s.Login(context.Background(), h.in())
		return err
	}, h.signOutWithANewPassword(t))
	wantFactorsChanged(t, "a sign-in", err)
	if n := h.liveTokens(t, h.name); n != 0 {
		t.Fatalf("%d tokens stand after the sign out everywhere, want none", n)
	}
}

// An operator's reset is a change of the factors like any other.
func TestASignInHeldAfterItsProofMintsNothingAfterAReset(t *testing.T) {
	t.Parallel()
	h := openHeldDoor(t, "login")
	err := h.run(t, func() error {
		_, err := h.s.Login(context.Background(), h.in())
		return err
	}, func() {
		if _, err := h.s.ResetUser(context.Background(), h.name, "an-operator-issued-passphrase", ""); err != nil {
			t.Fatalf("the reset meanwhile: %v", err)
		}
	})
	wantFactorsChanged(t, "a sign-in", err)
	if n := h.liveTokens(t, h.name); n != 1 {
		t.Fatalf("%d tokens stand after the reset, want the registration's alone", n)
	}
}

// The repository a sign-in chose is held to the person still opening it.
func TestASignInHeldAfterItsProofMintsNothingInARepositoryItsPersonLeft(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := openHeldDoor(t, "login")
	second := testdb.RepositoryLabel(t) + "2.example.com"
	repo, err := h.s.CreateRepository(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	person, err := h.s.identityByName(ctx, h.s.maint, h.name)
	if err != nil {
		t.Fatal(err)
	}
	if err := linkIdentity(ctx, h.s.maint, person.ID, repo.ID); err != nil {
		t.Fatal(err)
	}
	in := h.in()
	in.Repository = second
	err = h.run(t, func() error {
		_, err := h.s.Login(ctx, in)
		return err
	}, func() {
		if _, err := h.s.maint.ExecContext(ctx, `DELETE FROM identity_repositories WHERE identity = $1 AND repository_id = $2`, person.ID, repo.ID); err != nil {
			t.Fatalf("take the person out of the second repository: %v", err)
		}
	})
	if !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("a sign-in held while its person left the repository it chose: %v, want ErrForbidden", err)
	}
	if n := h.liveTokens(t, second); n != 0 {
		t.Fatalf("%d tokens in the repository the person left, want none", n)
	}
}

func TestAPasswordChangeHeldAfterItsProofChangesNothing(t *testing.T) {
	t.Parallel()
	h := openHeldDoor(t, "password")
	err := h.run(t, func() error {
		return h.s.ChangePassword(context.Background(), h.in(), "a-password-from-the-old-proof")
	}, h.signOutWithANewPassword(t))
	wantFactorsChanged(t, "a password change", err)
	if _, err := h.s.Login(context.Background(), substrate.LoginInput{Identity: h.name, Password: changedPassword}); err != nil {
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
	}, h.signOutWithANewPassword(t))
	wantFactorsChanged(t, "a second factor's enrollment", err)
	person, err := h.s.Identity(context.Background(), identityIDOf(t, h.s, h.name))
	if err != nil || person.SecondFactor {
		t.Fatalf("the held enrollment wrote a second factor: %+v (%v)", person, err)
	}
}

func TestASignOutHeldAfterItsProofEndsNothing(t *testing.T) {
	t.Parallel()
	h := openHeldDoor(t, "signout")
	var before int
	err := h.run(t, func() error {
		_, err := h.s.SignOutEverywhere(context.Background(), h.in(), "")
		return err
	}, func() {
		// The factors change meanwhile, and a sign-in under the new ones stands.
		if err := h.s.ChangePassword(context.Background(), h.in(), changedPassword); err != nil {
			t.Fatalf("the password change meanwhile: %v", err)
		}
		if _, err := h.s.Login(context.Background(), substrate.LoginInput{Identity: h.name, Password: changedPassword, Label: "new"}); err != nil {
			t.Fatalf("a sign-in under the new password: %v", err)
		}
		before = h.liveTokens(t, h.name)
	})
	wantFactorsChanged(t, "a sign out everywhere", err)
	if n := h.liveTokens(t, h.name); n != before {
		t.Fatalf("%d tokens stand after the refused sign-out, want the %d before it", n, before)
	}
}

func TestADeleteHeldAfterItsProofErasesNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := openHeldDoor(t, "delete")
	in := h.in()
	in.Repository = h.name
	err := h.run(t, func() error {
		return h.s.DeleteRepository(ctx, in)
	}, h.signOutWithANewPassword(t))
	wantFactorsChanged(t, "a repository's delete", err)
	if _, err := h.s.repositoryByName(ctx, h.name); err != nil {
		t.Fatalf("the repository is gone after a refused delete: %v", err)
	}
	if _, err := h.s.Dataset(ctx, h.name); err != nil {
		t.Fatalf("the repository does not open after a refused delete: %v", err)
	}
	if _, err := h.s.identityByName(ctx, h.s.maint, h.name); err != nil {
		t.Fatalf("the person is gone after a refused delete: %v", err)
	}
}

// A registration's proof is a new seed's code, and no earlier factors can
// change under it; what can is its name, which one registration holds.
func TestARegistrationHeldAfterItsProofCreatesNothingOnceItsNameIsTaken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := openHeldDoor(t, "register")
	label := testdb.RepositoryLabel(t)
	name, first, second := label+"2.example.com", label+"3.example.com", label+"4.example.com"
	err := h.run(t, func() error {
		_, err := h.s.Register(ctx, substrate.RegisterInput{
			Identity: name, Repository: first, Password: proofDoorPassword, SkipTOTP: true,
		})
		return err
	}, func() {
		if _, err := h.s.Register(ctx, substrate.RegisterInput{
			Identity: name, Repository: second, Password: proofDoorPassword, SkipTOTP: true,
		}); err != nil {
			t.Fatalf("the registration meanwhile: %v", err)
		}
	})
	if !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("a registration held while another took its name: %v, want the name refused as taken", err)
	}
	if _, err := h.s.repositoryByName(ctx, first); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the refused registration's repository: %v, want none", err)
	}
	person, err := h.s.identityByName(ctx, h.s.maint, name)
	if err != nil {
		t.Fatal(err)
	}
	repos, err := h.s.identityRepositories(ctx, h.s.maint, person.ID)
	if err != nil || len(repos) != 1 || repos[0].Authority != second {
		t.Fatalf("the name's person opens %+v (%v), want the second registration's alone", repos, err)
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

// A mint standing on a token holds its person to opening the repository: a
// request authenticated before the person left mints nothing after, for
// `POST /tokens` and an app's code exchange alike.
func TestAMintStandingOnATokenWhosePersonLeftMintsNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := openHeldDoor(t, "none")
	session, err := h.s.Login(ctx, substrate.LoginInput{Identity: h.name, Password: proofDoorPassword, Label: "session"})
	if err != nil {
		t.Fatal(err)
	}
	d, err := h.s.Dataset(ctx, h.name)
	if err != nil {
		t.Fatal(err)
	}
	ds := d.(*dataset)
	onSession := substrate.WithPrincipal(ctx, session.Token.ID)
	if _, _, err := ds.MintToken(onSession, substrate.MintTokenInput{Label: "while the person may open it"}); err != nil {
		t.Fatalf("a mint on a token whose person may open the repository: %v", err)
	}
	unlinkPerson(t, h.s, h.name)
	if _, _, err := ds.MintToken(onSession, substrate.MintTokenInput{Label: "after the person left"}); !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("a mint on a token whose person left = %v, want ErrAuth", err)
	}
}

// A token authenticates while the person it names may open its repository.
// One naming a person of another server (a restore brings those back) is
// refused, and the open does not hand it to this server's person. One
// naming nobody is from before identities and stands until the open names
// it.
func TestATokenWhosePersonMayNotOpenItsRepositoryDoesNotAuthenticate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := openHeldDoor(t, "none")
	session, err := h.s.Login(ctx, substrate.LoginInput{Identity: h.name, Password: proofDoorPassword, Label: "session"})
	if err != nil {
		t.Fatal(err)
	}
	d, err := h.s.Dataset(ctx, h.name)
	if err != nil {
		t.Fatal(err)
	}
	ds := d.(*dataset)
	stranger, strangerSecret, err := ds.MintToken(ctx, substrate.MintTokenInput{Label: "restored", Identity: "idn-of-another-server"})
	if err != nil {
		t.Fatal(err)
	}
	_, unnamedSecret, err := ds.MintToken(ctx, substrate.MintTokenInput{Label: "from before identities"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.s.Authenticate(ctx, session.Secret); err != nil {
		t.Fatalf("the person's own token: %v", err)
	}
	if _, _, err := h.s.Authenticate(ctx, unnamedSecret); err != nil {
		t.Fatalf("a token naming nobody: %v", err)
	}
	if _, _, err := h.s.Authenticate(ctx, strangerSecret); !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("a token naming another server's person = %v, want ErrAuth", err)
	}

	// The next open names the token naming nobody, and leaves the stranger's.
	h.s.mu.Lock()
	delete(h.s.datasets, ds.info.ID)
	h.s.mu.Unlock()
	ds.close()
	d, err = h.s.Dataset(ctx, h.name)
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := d.Tokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	person := identityIDOf(t, h.s, h.name)
	for _, tok := range tokens {
		want := person
		if tok.ID == stranger.ID {
			want = "idn-of-another-server"
		}
		if tok.Identity != want {
			t.Fatalf("after the open token %s (%s) acts for %q, want %q", tok.ID, tok.Label, tok.Identity, want)
		}
	}
	if _, _, err := h.s.Authenticate(ctx, strangerSecret); !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("the stranger's token after the open = %v, want ErrAuth", err)
	}

	// The person taken out of the repository by the server: the first
	// person's actor names another, as a restore's history can, and the
	// rebuild that replays it takes out of the index the person no actor
	// names, under the cache's fence. The person's token, cached a moment
	// before, stops at once; nothing empties the cache by hand.
	if _, _, err := h.s.Authenticate(ctx, session.Secret); err != nil || !h.s.tokens.cached(hashToken(session.Secret)) {
		t.Fatalf("the person's token before: %v, cached %v; want it standing and cached", err, h.s.tokens.cached(hashToken(session.Secret)))
	}
	d, err = h.s.Dataset(ctx, h.name)
	if err != nil {
		t.Fatal(err)
	}
	ds = d.(*dataset)
	if err := ds.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
		return tx.namePersonActor(substrate.ActorPerson, "idn-of-another-server", h.name)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.RebuildRepository(ctx, h.name); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.s.Authenticate(ctx, session.Secret); !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("a token whose person no actor names = %v, want ErrAuth", err)
	}
}

// reopen closes the service's dataset and opens it again, as a restart of
// the repository does, in the same process: the token cache stays warm.
func reopen(t *testing.T, s *service, ds *dataset) {
	t.Helper()
	s.mu.Lock()
	delete(s.datasets, ds.info.ID)
	s.mu.Unlock()
	ds.close()
	if _, err := s.Dataset(context.Background(), ds.info.ID); err != nil {
		t.Fatal(err)
	}
}

// A repository's deletion takes its people out of it and the deleter's
// identity with their last repository, under the cache's fence: a token
// cached a moment before stops at once, and the person's token in a
// repository they are still in stands. Nothing empties the cache by hand.
func TestARepositoryDeletedTakesItsPeopleOutWithAWarmCache(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := openHeldDoor(t, "none")
	other := testdb.RepositoryLabel(t) + "2.example.com"
	if _, err := h.s.CreateRepository(ctx, other); err != nil {
		t.Fatal(err)
	}
	if err := h.s.AddPerson(ctx, h.name, other, scope.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	signIn := func(repository string) string {
		t.Helper()
		m, err := h.s.Login(ctx, substrate.LoginInput{Identity: h.name, Repository: repository, Password: proofDoorPassword, Label: "session"})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := h.s.Authenticate(ctx, m.Secret); err != nil || !h.s.tokens.cached(hashToken(m.Secret)) {
			t.Fatalf("a session in %s: %v, cached %v", repository, err, h.s.tokens.cached(hashToken(m.Secret)))
		}
		return m.Secret
	}
	inOther, inOwn := signIn(other), signIn(h.name)
	in := h.in()
	in.Repository = other
	if err := h.s.DeleteRepository(ctx, in); err != nil {
		t.Fatalf("delete %s: %v", other, err)
	}
	if _, _, err := h.s.Authenticate(ctx, inOther); !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("a token of the deleted repository = %v, want ErrAuth", err)
	}
	if _, _, err := h.s.Authenticate(ctx, inOwn); err != nil {
		t.Fatalf("the person's token in the repository they are still in: %v", err)
	}
	in.Repository = h.name
	if err := h.s.DeleteRepository(ctx, in); err != nil {
		t.Fatalf("delete %s: %v", h.name, err)
	}
	if _, _, err := h.s.Authenticate(ctx, inOwn); !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("a token of the person whose last repository went = %v, want ErrAuth", err)
	}
	if _, err := h.s.identityByName(ctx, h.s.maint, h.name); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the person after their last repository went: %v, want none", err)
	}
}

// Two restore resets of one repository, naming two people, give it one
// person: the second waits on the first's lock, reads that the repository
// has a person, and is refused.
func TestTwoResetsOfARestoredRepositoryGiveItOnePerson(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := openHeldDoor(t, "reset")
	repo, err := h.s.repositoryByName(ctx, h.name)
	if err != nil {
		t.Fatal(err)
	}
	// A restore without its people, whose actor names nobody: no identity
	// to give back, so each reset names one of its own.
	if _, err := h.s.maint.ExecContext(ctx, `DELETE FROM identities WHERE name = $1`, h.name); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.maint.ExecContext(ctx, `UPDATE records SET props = props - 'identity'
		WHERE repository = $1 AND kind = $2 AND id = $3`, repo.ID, kindActor, actorID(substrate.ActorPerson)); err != nil {
		t.Fatal(err)
	}
	label := testdb.RepositoryLabel(t)
	first, second := label+"3.example.com", label+"4.example.com"
	done := make(chan error, 1)
	go func() {
		_, err := h.s.ResetUser(ctx, h.name, "an-operator-issued-passphrase", first)
		done <- err
	}()
	select {
	case <-h.proved:
	case err := <-done:
		t.Fatalf("the first reset ended before its check: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("the first reset never reached its check")
	}
	secondDone := make(chan error, 1)
	go func() {
		_, err := h.s.ResetUser(ctx, h.name, "another-operator-issued-passphrase", second)
		secondDone <- err
	}()
	// The second reset is waiting on the first's lock of the repository's
	// row, in the database, before the first lets go.
	// The owner's connection sees every session's activity, which the
	// maintenance role's does not.
	owner, err := sql.Open("pgx", h.s.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Close() }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting int
		if err := owner.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock'
			  AND query LIKE '%FROM repositories WHERE id = $1 FOR UPDATE%'`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			break
		}
		select {
		case err := <-secondDone:
			t.Fatalf("the second reset finished while the first held the repository: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("the second reset never waited on the repository's lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(h.release)
	if err := <-done; err != nil {
		t.Fatalf("the first reset: %v", err)
	}
	select {
	case err := <-secondDone:
		if !errors.Is(err, substrate.ErrConflict) {
			t.Fatalf("the second reset = %v, want ErrConflict", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the second reset never finished")
	}
	people, err := repositoryIdentities(ctx, h.s.maint, repo.ID)
	if err != nil || len(people) != 1 {
		t.Fatalf("the repository's people after two resets: %v (%v), want one", people, err)
	}
	if _, err := h.s.identityByName(ctx, h.s.maint, second); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the refused reset's person: %v, want none", err)
	}
}

func identityIDOf(t *testing.T, s *service, name string) string {
	t.Helper()
	row, err := s.identityByName(context.Background(), s.maint, name)
	if err != nil {
		t.Fatal(err)
	}
	return row.ID
}

// unlinkPerson takes a person out of the repository named after them, as
// nothing at 1.0 does but an operator or a later release may.
func unlinkPerson(t *testing.T, s *service, name string) {
	t.Helper()
	if _, err := s.maint.ExecContext(context.Background(), `
		DELETE FROM identity_repositories WHERE identity = (SELECT id FROM identities WHERE name = $1)
		AND repository_id = (SELECT id FROM repositories WHERE authority = $1)`, name); err != nil {
		t.Fatalf("take %s out of its repository: %v", name, err)
	}
}

// A repository restored without its people refuses the tokens it carried,
// whose person this server does not hold, until the operator's reset gives it
// its person back under the id its first person's actor names. An actor
// naming a person this server holds is people_internal_db_test.go's.
func TestAResetGivesARestoredRepositoryThePersonItsActorNames(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := openHeldDoor(t, "none")
	session, err := h.s.Login(ctx, substrate.LoginInput{Identity: h.name, Password: proofDoorPassword, Label: "session"})
	if err != nil {
		t.Fatal(err)
	}
	person := identityIDOf(t, h.s, h.name)
	// A restore into a server without the identity: the token was never
	// looked up here, so nothing is cached.
	if _, err := h.s.maint.ExecContext(ctx, `DELETE FROM identities WHERE id = $1`, person); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.s.Authenticate(ctx, session.Secret); !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("a restored token before the reset = %v, want ErrAuth", err)
	}
	if _, err := h.s.ResetUser(ctx, h.name, "an-operator-issued-passphrase", ""); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if got := identityIDOf(t, h.s, h.name); got != person {
		t.Fatalf("the reset gave the repository %s, want the person its actor names, %s", got, person)
	}
	if _, _, err := h.s.Authenticate(ctx, session.Secret); err != nil {
		t.Fatalf("a restored token after the reset: %v", err)
	}
}

// A sign out everywhere that changes no factor ends the tokens that exist
// when it commits and leaves the factors standing: a sign-in that proved
// them is a new sign-in, whenever it completes, as one sent a moment later
// would be.
func TestASignInHeldAcrossASignOutThatChangesNoFactorIsANewSignIn(t *testing.T) {
	t.Parallel()
	h := openHeldDoor(t, "login")
	err := h.run(t, func() error {
		_, err := h.s.Login(context.Background(), h.in())
		return err
	}, func() {
		if _, err := h.s.SignOutEverywhere(context.Background(), h.in(), ""); err != nil {
			t.Fatalf("the sign out everywhere meanwhile: %v", err)
		}
	})
	if err != nil {
		t.Fatalf("a sign-in held across a sign out everywhere that changed no factor: %v, want it to complete", err)
	}
	if n := h.liveTokens(t, h.name); n != 1 {
		t.Fatalf("%d tokens stand, want the held sign-in's alone", n)
	}
}

// A rename proved before the factors changed, held while they change,
// renames nothing: the rename's transaction holds the person's row to the
// version it proved.
func TestARenameHeldAfterItsProofRenamesNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := openHeldDoor(t, "rename")
	in := h.in()
	in.Repository = h.name
	to := testdb.RepositoryLabel(t) + "-renamed.example.com"
	err := h.run(t, func() error {
		_, err := h.s.RenameRepository(ctx, in, to)
		return err
	}, h.signOutWithANewPassword(t))
	wantFactorsChanged(t, "a repository's rename", err)
	if _, err := h.s.repositoryByName(ctx, h.name); err != nil {
		t.Fatalf("the repository does not answer to its name after a refused rename: %v", err)
	}
	if _, err := h.s.repositoryByName(ctx, to); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the new name answers after a refused rename: %v", err)
	}
}

// A sign-in that picked its repository before a rename, held while the
// rename commits, answers the name the repository has when it answers, with
// a token of that repository.
func TestASignInHeldAcrossARenameAnswersTheNameItHasNow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := openHeldDoor(t, "login")
	in := h.in()
	in.Repository = h.name
	to := testdb.RepositoryLabel(t) + "-renamed.example.com"
	var minted substrate.MintedToken
	err := h.run(t, func() error {
		var err error
		minted, err = h.s.Login(ctx, in)
		return err
	}, func() {
		if _, err := h.s.RenameRepository(ctx, in, to); err != nil {
			t.Fatalf("the rename meanwhile: %v", err)
		}
	})
	if err != nil {
		t.Fatalf("a sign-in held across a rename: %v", err)
	}
	if minted.Repository != to {
		t.Errorf("the sign-in answers %s, want %s, the name the repository has", minted.Repository, to)
	}
	if n := h.liveTokens(t, to); n != 2 {
		t.Errorf("the renamed repository holds %d tokens, want the registration's and the sign-in's", n)
	}
}
