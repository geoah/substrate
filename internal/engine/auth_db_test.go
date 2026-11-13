package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// Registration is two calls and ONE durable write: the enrollment creates
// nothing, and the commit creates the repository, the sealed material, the
// credential record and the first token together.
func TestRegistrationCreatesTheUserAndNothingBefore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)

	// An abandoned enrollment leaves no trace at all.
	if _, err := svc.BeginRegistration(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("begin registration: %v", err)
	}
	if repos, err := svc.Repositories(ctx); err != nil || len(repos) != 0 {
		t.Fatalf("an abandoned enrollment created %v (err %v)", repos, err)
	}
	if _, err := svc.BeginRegistration(ctx, "Bad Name"); err == nil {
		t.Fatal("a malformed repository name must not get an enrollment")
	}

	user, tok, secret := registerUser(t, svc, testdb.Repository(t))
	if tok.Label != "cli" || secret == "" {
		t.Fatalf("registration token = %+v, secret %q", tok, secret)
	}
	repos, err := svc.Repositories(ctx)
	if err != nil || len(repos) != 1 || !vocabulary.ValidRepositoryID(repos[0].ID) || repos[0].Authority != testdb.Repository(t) {
		t.Fatalf("repositories = %v (err %v), want one under a minted id named %s", repos, err, testdb.Repository(t))
	}

	// The token registration returned works, and it is a RECORD in the
	// repository it opened.
	ds, info, err := svc.Authenticate(ctx, secret)
	if err != nil {
		t.Fatalf("authenticate the registration token: %v", err)
	}
	if info.ID != tok.ID || ds.Repository().ID != repos[0].ID || ds.Repository().Authority != testdb.Repository(t) {
		t.Fatalf("authenticated as %+v in %+v", info, ds.Repository())
	}
	// The repository's self-description carries the authority it owns, so a
	// client that only speaks the record API can learn where its kinds live.
	self, err := ds.Get(ctx, "substrate.reamde.dev/core/repository", ds.Repository().ID)
	if err != nil {
		t.Fatalf("the repository record: %v", err)
	}
	if self.Properties["authority"] != testdb.Repository(t) || self.Properties["name"] != testdb.Repository(t) {
		t.Fatalf("repository record = %v", self.Properties)
	}

	// The credential is a singleton record holding REFS, never material.
	cred, err := ds.Get(ctx, "substrate.reamde.dev/core/credential", "self")
	if err != nil {
		t.Fatalf("the credential record: %v", err)
	}
	if cred.Properties["repository"] != testdb.Repository(t) {
		t.Fatalf("credential repository = %v", cred.Properties["repository"])
	}
	for _, ref := range []string{"passwordRef", "totpRef"} {
		if cred.Properties[ref] != "<redacted>" {
			t.Fatalf("%s must read back redacted, got %v", ref, cred.Properties[ref])
		}
	}
	// Nothing crackable reached the changelog: no entry payload carries the seed or
	// an argon2id hash.
	for _, c := range changesSince(t, ds, 0) {
		raw, err := json.Marshal(c.Payload)
		if err != nil {
			t.Fatal(err)
		}
		blob := strings.ToLower(string(raw))
		// The seed itself, and the "$argon2id$" a stored hash opens with. (The
		// credential KIND's own declaration names argon2id in a description,
		// which is documentation, not material.)
		if strings.Contains(blob, strings.ToLower(user.seed)) || strings.Contains(blob, "$argon2id$") {
			t.Fatalf("auth material reached the changelog: %s", raw)
		}
	}

	// A second registration for the same repository is refused, and refusing it
	// leaves the first user untouched.
	enrollment, err := svc.BeginRegistration(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("begin registration: %v", err)
	}
	dup := &authUser{repository: testdb.Repository(t), seed: enrollment.Secret}
	if _, err := svc.Register(ctx, substrate.RegisterInput{
		Repository: testdb.Repository(t), Password: testPassword,
		TOTPSecret: enrollment.Secret, TOTPCode: dup.code(t),
	}); err == nil {
		t.Fatal("a taken repository must not register twice")
	}
	if repos, err := svc.Repositories(ctx); err != nil || len(repos) != 1 {
		t.Fatalf("the refused registration left %v (err %v)", repos, err)
	}
	if _, _, err := svc.Authenticate(ctx, secret); err != nil {
		t.Fatalf("the first user's token died with the refused registration: %v", err)
	}
}

// Registration refuses a code that does not match the enrollment, a password
// under the policy, and a seed that is not a usable TOTP secret — and each
// refusal creates nothing.
func TestRegistrationRefusals(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)
	enrollment, err := svc.BeginRegistration(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("begin registration: %v", err)
	}
	u := &authUser{repository: testdb.Repository(t), seed: enrollment.Secret}
	// The authority cases carry a code the enrollment accepts, so each one is
	// refused for its authority and never for the code: u.code hands every
	// call a later step, which the clock may or may not have reached.
	valid := u.code(t)

	for name, in := range map[string]substrate.RegisterInput{
		"wrong code": {
			Password:   testPassword,
			TOTPSecret: enrollment.Secret, TOTPCode: "000000",
		},
		"short password": {
			Password:   "short",
			TOTPSecret: enrollment.Secret, TOTPCode: u.code(t),
		},
		"unusable seed": {
			Password:   testPassword,
			TOTPSecret: "not base32!", TOTPCode: "123456",
		},
		// The authority is required, DNS-shaped, and never under the
		// publisher's namespace (decision record 0046).
		"no authority": {
			Password:   testPassword,
			TOTPSecret: enrollment.Secret, TOTPCode: valid,
		},
		"one-label authority": {
			Password:   testPassword,
			TOTPSecret: enrollment.Secret, TOTPCode: valid, Repository: testdb.RepositoryLabel(t),
		},
		"uppercase authority": {
			Password:   testPassword,
			TOTPSecret: enrollment.Secret, TOTPCode: valid, Repository: "Geoah.Example.com",
		},
		"publisher authority": {
			Password:   testPassword,
			TOTPSecret: enrollment.Secret, TOTPCode: valid, Repository: "geoah.substrate.reamde.dev",
		},
		// DNS admits 253 bytes; a repository id, which the authority now is
		// (decision record 0052), admits MaxIDLen.
		"authority longer than a record id": {
			Password:   testPassword,
			TOTPSecret: enrollment.Secret, TOTPCode: valid,
			Repository: strings.Repeat("a", 60) + "." + strings.Repeat("b", 60) + ".example.com",
		},
	} {
		_, err := svc.Register(ctx, in)
		if err == nil {
			t.Fatalf("%s: registration was accepted", name)
		}
		if strings.HasSuffix(name, "authority") && !errors.Is(err, substrate.ErrValidation) {
			t.Fatalf("%s: refused with %v, want the authority refused as invalid", name, err)
		}
		if repos, err := svc.Repositories(ctx); err != nil || len(repos) != 0 {
			t.Fatalf("%s: a failed registration created %v", name, repos)
		}
	}
}

// Two repositories cannot own one authority: a kind reference names its
// authority and nothing else, so a shared one would be two homes for one name.
func TestRegistrationRefusesATakenAuthority(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)
	registerUser(t, svc, testdb.Repository(t))

	enrollment, err := svc.BeginRegistration(ctx, "ada.example.com")
	if err != nil {
		t.Fatalf("begin registration: %v", err)
	}
	u := &authUser{repository: "ada.example.com", seed: enrollment.Secret}
	_, err = svc.Register(ctx, substrate.RegisterInput{
		Password:   testPassword,
		TOTPSecret: enrollment.Secret, TOTPCode: u.code(t),
		Repository: testdb.Repository(t),
	})
	if err == nil || !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "already owned") {
		t.Fatalf("a taken authority registered: %v", err)
	}
	if repos, err := svc.Repositories(ctx); err != nil || len(repos) != 1 {
		t.Fatalf("the refused registration created %v (err %v)", repos, err)
	}
}

// Two registrations of one repository at the same moment: one wins, the other
// is refused as a taken authority, and the winner is whole. The authority is
// the scope the seed writes under, so without the registration lock both
// would seed rows under it and the loser's cleanup would erase the winner's
// rows, changelog and directory as its own.
func TestConcurrentRegistrationsForOneAuthorityKeepTheWinner(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)
	const authority = "shared.example.com"
	inputs := make([]substrate.RegisterInput, 2)
	for i := range inputs {
		enrollment, err := svc.BeginRegistration(ctx, authority)
		if err != nil {
			t.Fatalf("begin registration %d: %v", i, err)
		}
		u := &authUser{repository: authority, seed: enrollment.Secret}
		inputs[i] = substrate.RegisterInput{
			Repository: authority, Password: testPassword,
			TOTPSecret: enrollment.Secret, TOTPCode: u.code(t),
		}
	}
	errs := make([]error, len(inputs))
	var wg sync.WaitGroup
	for i := range inputs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = svc.Register(ctx, inputs[i])
		}()
	}
	wg.Wait()

	won, losers := 0, 0
	for i, err := range errs {
		switch {
		case err == nil:
			won++
		case errors.Is(err, substrate.ErrValidation) && strings.Contains(err.Error(), "already owned"):
			losers++
		default:
			t.Fatalf("registration %d: not the taken-authority refusal: %v", i, err)
		}
	}
	if won != 1 || losers != 1 {
		t.Fatalf("want one winner and one refused registration, got %v", errs)
	}
	repos, err := svc.Repositories(ctx)
	if err != nil || len(repos) != 1 || repos[0].Authority != authority {
		t.Fatalf("repositories = %+v (%v), want %s alone", repos, err, authority)
	}
	id := repos[0].ID
	// The winner's repository opens, describes itself, and its changelog and
	// directory are intact: the loser erased nothing of the winner's.
	ds, err := svc.Dataset(ctx, authority)
	if err != nil {
		t.Fatalf("the winner's repository does not open: %v", err)
	}
	self, err := ds.Get(ctx, "substrate.reamde.dev/core/repository", id)
	if err != nil || self.Properties["authority"] != authority {
		t.Fatalf("the winner's self-description: %+v, %v", self, err)
	}
	report := mustVerify(t, svc, authority)
	if !report.OK || report.Head == 0 {
		t.Fatalf("the winner's changelog does not verify: %+v", report)
	}
	if err := engine.ArchivePass(ctx, ds); err != nil {
		t.Fatalf("archive the winner: %v", err)
	}
	if m, _, err := changelogfile.ReadArchiveManifestDir(filepath.Join(engine.DataRootOf(svc), "archive", changelogfile.RepositoriesDir, id)); err != nil || m.Authority != authority {
		t.Fatalf("the winner's archive manifest: %+v, %v", m, err)
	}
}

// Login is the second door: both factors, a token record minted, and the code
// spent — a replay of the same code is refused even with the right password.
func TestLoginMintsATokenAndSpendsTheCode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)
	user, _, _ := registerUser(t, svc, testdb.Repository(t))

	code := user.code(t)
	tok, secret, err := svc.Login(ctx, substrate.LoginInput{
		Repository: testdb.Repository(t), Password: testPassword, TOTPCode: code, Label: "console",
	})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if tok.Label != "console" || !strings.HasPrefix(secret, "substrate_tok_") {
		t.Fatalf("login token = %+v, secret %q", tok, secret)
	}
	ds, _, err := svc.Authenticate(ctx, secret)
	if err != nil {
		t.Fatalf("the login token does not authenticate: %v", err)
	}
	// Sessions ARE token records: the login is visible as one, beside the
	// registration token.
	tokens, err := ds.Tokens(ctx)
	if err != nil {
		t.Fatalf("list tokens: %v", err)
	}
	if len(tokens) != 2 {
		t.Fatalf("tokens = %+v, want the registration and login records", tokens)
	}

	// The same code cannot be spent twice.
	if _, _, err := svc.Login(ctx, substrate.LoginInput{
		Repository: testdb.Repository(t), Password: testPassword, TOTPCode: code,
	}); err == nil {
		t.Fatal("a consumed code logged in again")
	} else {
		wantErr(t, err, substrate.ErrAuth, "replayed code")
	}
}

// Every login failure answers the same way, whoever the caller is: a wrong
// password, a wrong code and a repository that does not exist are one error.
func TestLoginGivesNoOracle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)
	user, _, _ := registerUser(t, svc, testdb.Repository(t))

	for name, in := range map[string]substrate.LoginInput{
		"wrong password": {Repository: testdb.Repository(t), Password: "wrong-password-entirely", TOTPCode: user.code(t)},
		"wrong code":     {Repository: testdb.Repository(t), Password: testPassword, TOTPCode: "000000"},
		"unknown user":   {Repository: "nosuch.example.com", Password: testPassword, TOTPCode: "000000"},
	} {
		_, _, err := svc.Login(ctx, in)
		if err == nil {
			t.Fatalf("%s: login succeeded", name)
		}
		wantErr(t, err, substrate.ErrAuth, name)
		if got := err.Error(); !strings.Contains(got, "bad repository, password or code") {
			t.Fatalf("%s: the refusal names which factor failed: %v", name, got)
		}
	}
}

// The password-factor rule at the engine: changing the password
// takes the CURRENT password and code, the new one works, the old one stops,
// and the second factor survives the change.
// Deleting a repository takes both factors, leaves nothing a login, a token
// or a lookup can reach, and frees the name for a new registration that
// inherits none of it.
func TestDeleteRepositoryErasesItAndFreesTheName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)
	repository := testdb.Repository(t)
	user, _, secret := registerUser(t, svc, repository)
	ds, err := svc.Dataset(ctx, repository)
	if err != nil {
		t.Fatalf("dataset: %v", err)
	}
	kept, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind:       "substrate.reamde.dev/llm/memory",
		Properties: map[string]any{"text": "the old repository's"},
	})
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	waitStep(t)
	if err := svc.DeleteRepository(ctx, substrate.LoginInput{
		Repository: repository, Password: "not-the-password", TOTPCode: user.code(t),
	}); !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("delete with a wrong password = %v, want ErrAuth", err)
	}
	if _, _, err := svc.Authenticate(ctx, secret); err != nil {
		t.Fatalf("a refused delete touched the repository: %v", err)
	}

	if err := svc.DeleteRepository(ctx, substrate.LoginInput{
		Repository: repository, Password: testPassword, TOTPCode: user.code(t),
	}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, _, err := svc.Authenticate(ctx, secret); !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("the deleted repository's token = %v, want ErrAuth", err)
	}
	if _, err := svc.Dataset(ctx, repository); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the deleted repository's dataset = %v, want ErrNotFound", err)
	}

	waitStep(t)
	registerUser(t, svc, repository)
	ds, err = svc.Dataset(ctx, repository)
	if err != nil {
		t.Fatalf("dataset of the new repository: %v", err)
	}
	if _, err := ds.Get(ctx, "substrate.reamde.dev/llm/memory", kept.ID); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the new repository holds the old one's record: %v", err)
	}
}

// A deleted repository's archive goes with it: an archive with no row is a
// repository the next boot restores, so one left behind would bring back
// what its owner deleted.
func TestADeletedRepositoryIsNotRestoredFromItsArchive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, dsn := newService(t)
	repository := testdb.Repository(t)
	user, _, _ := registerUser(t, svc, repository)
	ds, err := svc.Dataset(ctx, repository)
	if err != nil {
		t.Fatalf("dataset: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/llm/memory", Properties: map[string]any{"text": "to be deleted"}})
	if err := engine.ArchivePass(ctx, ds); err != nil {
		t.Fatalf("archive pass: %v", err)
	}
	root := engine.DataRootOf(svc)
	archived := filepath.Join(root, "archive", changelogfile.RepositoriesDir, ds.Repository().ID)
	if _, _, err := changelogfile.ReadArchiveManifestDir(archived); err != nil {
		t.Fatalf("the repository has no archive to delete: %v", err)
	}

	waitStep(t)
	if err := svc.DeleteRepository(ctx, substrate.LoginInput{
		Repository: repository, Password: testPassword, TOTPCode: user.code(t),
	}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// The deletion mark stays, and nothing else: it is what keeps a
	// manifest a store applies late from restoring the repository.
	if err := filepath.WalkDir(archived, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() == changelogfile.DeletedMark {
			return err
		}
		return fmt.Errorf("the deleted repository's archive still holds %s", path)
	}); err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	svc2, err := reopen(t, dsn, root)
	if err != nil {
		t.Fatalf("boot after the delete: %v", err)
	}
	if _, err := svc2.Dataset(ctx, repository); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the deleted repository after a boot = %v, want ErrNotFound", err)
	}
}

// A server with the archive off still deletes a deleted repository's copy in
// the default archive under its data root, which the first boot with the
// archive on would restore.
func TestADeletedRepositoryIsNotRestoredFromTheDefaultArchiveWithTheArchiveOff(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, dsn := newService(t)
	repository := testdb.Repository(t)
	user, _, _ := registerUser(t, svc, repository)
	ds, err := svc.Dataset(ctx, repository)
	if err != nil {
		t.Fatalf("dataset: %v", err)
	}
	if err := engine.ArchivePass(ctx, ds); err != nil {
		t.Fatalf("archive pass: %v", err)
	}
	root := engine.DataRootOf(svc)
	archived := filepath.Join(root, "archive", changelogfile.RepositoriesDir, ds.Repository().ID)
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}

	off := reopenWith(t, dsn, root, engine.WithoutArchive())
	waitStep(t)
	if err := off.DeleteRepository(ctx, substrate.LoginInput{
		Repository: repository, Password: testPassword, TOTPCode: user.code(t),
	}); err != nil {
		t.Fatalf("delete with the archive off: %v", err)
	}
	if _, _, err := changelogfile.ReadArchiveManifestDir(archived); !errors.Is(err, changelogfile.ErrArchiveDeleted) {
		t.Fatalf("the default archive is not marked as the deleted repository's: %v", err)
	}
	if err := off.Close(); err != nil {
		t.Fatal(err)
	}
	on, err := reopen(t, dsn, root)
	if err != nil {
		t.Fatalf("boot with the archive on: %v", err)
	}
	if _, err := on.Dataset(ctx, repository); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the deleted repository after a boot with the archive on = %v, want ErrNotFound", err)
	}
}

func TestChangePasswordKeepsTheSecondFactor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)
	user, _, secret := registerUser(t, svc, testdb.Repository(t))

	const newPassword = "a-longer-passphrase-entirely"
	if err := svc.ChangePassword(ctx, substrate.LoginInput{
		Repository: testdb.Repository(t), Password: testPassword, TOTPCode: user.code(t),
	}, newPassword); err != nil {
		t.Fatalf("change password: %v", err)
	}
	user.password = newPassword

	// A wrong current password cannot change it, whatever else is right. A
	// refused attempt spends no code — nothing is consumed until both factors
	// have already passed.
	if err := svc.ChangePassword(ctx, substrate.LoginInput{
		Repository: testdb.Repository(t), Password: testPassword, TOTPCode: "000000",
	}, "yet-another-passphrase"); err == nil {
		t.Fatal("the old password changed the credential")
	}

	// Existing tokens survive a password change: a token is data access, the
	// credential is the account (this is the other half of RB-6 — the token
	// never had the power to do what just happened).
	if _, _, err := svc.Authenticate(ctx, secret); err != nil {
		t.Fatalf("a password change revoked a live token: %v", err)
	}

	// The new password logs in with the SAME seed, and the old one does not.
	waitStep(t)
	if _, _, err := svc.Login(ctx, substrate.LoginInput{
		Repository: testdb.Repository(t), Password: newPassword, TOTPCode: user.code(t),
	}); err != nil {
		t.Fatalf("login with the new password: %v", err)
	}
	if _, _, err := svc.Login(ctx, substrate.LoginInput{
		Repository: testdb.Repository(t), Password: testPassword, TOTPCode: "000000",
	}); err == nil {
		t.Fatal("the old password still logs in")
	}
}

// TOTP re-enrollment: the current factors AND one code from the candidate
// seed, then the old seed is dead and the password is untouched.
func TestReenrollTOTPSwapsTheSecondFactor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)
	user, _, _ := registerUser(t, svc, testdb.Repository(t))
	oldSeed := user.seed

	enrollment, err := svc.BeginTOTPReenrollment(ctx, substrate.LoginInput{
		Repository: testdb.Repository(t), Password: testPassword, TOTPCode: user.code(t),
	})
	if err != nil {
		t.Fatalf("begin re-enrollment: %v", err)
	}
	if enrollment.Secret == oldSeed {
		t.Fatal("the re-enrollment reissued the same seed")
	}
	if !strings.Contains(enrollment.URI, "otpauth://totp/Substrate:"+testdb.Repository(t)) {
		t.Fatalf("enrollment uri = %q", enrollment.URI)
	}
	// Nothing changed yet: the credential still points at the live seed, and
	// the candidate is only a candidate.
	waitStep(t)
	candidate := &authUser{seed: enrollment.Secret}
	if err := svc.ReenrollTOTP(ctx, substrate.LoginInput{
		Repository: testdb.Repository(t), Password: testPassword, TOTPCode: user.code(t),
	}, enrollment.Secret, candidate.code(t)); err != nil {
		t.Fatalf("re-enroll: %v", err)
	}

	// The old seed is dead, the new one works, the password is unchanged.
	if _, _, err := svc.Login(ctx, substrate.LoginInput{
		Repository: testdb.Repository(t), Password: testPassword, TOTPCode: user.code(t),
	}); err == nil {
		t.Fatal("the replaced seed still logs in")
	}
	waitStep(t)
	if _, _, err := svc.Login(ctx, substrate.LoginInput{
		Repository: testdb.Repository(t), Password: testPassword, TOTPCode: candidate.code(t),
	}); err != nil {
		t.Fatalf("login with the new seed: %v", err)
	}
}

// The operator's door for a user who lost both factors: fresh material, a new
// credential record, and the repository's data untouched.
func TestResetUser(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)
	registerUser(t, svc, testdb.Repository(t))
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	importVocabulary(t, ds, "tasks")
	task := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "samples.substrate.reamde.dev/tasks/task", Properties: map[string]any{"name": "survives a reset"},
	})

	resetter, ok := svc.(interface {
		ResetUser(context.Context, string, string) (substrate.TOTPEnrollment, error)
	})
	if !ok {
		t.Fatal("the engine service must expose ResetUser for substratectl")
	}
	const resetPassword = "operator-issued-passphrase"
	enrollment, err := resetter.ResetUser(ctx, testdb.Repository(t), resetPassword)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	fresh := &authUser{repository: testdb.Repository(t), password: resetPassword, seed: enrollment.Secret}
	if _, _, err := svc.Login(ctx, substrate.LoginInput{
		Repository: testdb.Repository(t), Password: resetPassword, TOTPCode: fresh.code(t),
	}); err != nil {
		t.Fatalf("login after a reset: %v", err)
	}
	if _, err := ds.Get(ctx, task.Kind, task.ID); err != nil {
		t.Fatalf("the reset took the data with it: %v", err)
	}
	if _, err := resetter.ResetUser(ctx, "nosuch.example.com", resetPassword); err == nil {
		t.Fatal("resetting a user who does not exist must fail")
	}
}

// The two auth kinds are REFUSED on the generic surface: the
// credential cannot be written or deleted through it at all, and a token can
// only be deleted — which is what revoking is.
func TestAuthKindsRefuseGenericWrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)
	registerUser(t, svc, testdb.Repository(t))
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/credential", ID: "self",
		Properties: map[string]any{"repository": testdb.Repository(t), "passwordRef": "forged"},
	}); err == nil {
		t.Fatal("the credential was forged through the generic surface")
	} else {
		wantErr(t, err, substrate.ErrForbidden, "credential put")
	}
	if _, err := ds.Patch(ctx, owner, "substrate.reamde.dev/core/credential", "self", substrate.PatchInput{
		Properties: map[string]any{"passwordRef": "forged"},
	}); err == nil {
		t.Fatal("the credential was repointed through the generic surface")
	}
	if _, err := ds.Delete(ctx, owner, "substrate.reamde.dev/core/credential", "self", substrate.DeleteInput{}); err == nil {
		t.Fatal("the credential was deleted through the generic surface")
	}
	if _, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/token", Properties: map[string]any{"label": "forged", "hash": "x"},
	}); err == nil {
		t.Fatal("a token was forged through the generic surface")
	}
	// Reading and listing both kinds stays ordinary.
	if _, err := ds.Get(ctx, "substrate.reamde.dev/core/credential", "self"); err != nil {
		t.Fatalf("the credential must stay readable: %v", err)
	}
}

// The hash lookup is what scopes a request: a token minted in one repository
// opens that repository and no other, and two repositories' tokens never
// cross.
func TestTokenLookupScopesTheRequest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)
	_, _, alphaSecret := registerUser(t, svc, "alpha.example.com")
	_, _, betaSecret := registerUser(t, svc, "beta.example.com")

	alphaDS, _, err := svc.Authenticate(ctx, alphaSecret)
	if err != nil {
		t.Fatalf("authenticate alpha: %v", err)
	}
	betaDS, _, err := svc.Authenticate(ctx, betaSecret)
	if err != nil {
		t.Fatalf("authenticate beta: %v", err)
	}
	if alphaDS.Repository().ID != repositoryIDByName(t, svc, "alpha.example.com") ||
		betaDS.Repository().ID != repositoryIDByName(t, svc, "beta.example.com") ||
		alphaDS.Repository().ID == betaDS.Repository().ID {
		t.Fatalf("tokens resolved to %+v and %+v", alphaDS.Repository(), betaDS.Repository())
	}
	importVocabulary(t, alphaDS, "tasks")
	task := mustPut(t, alphaDS, owner, substrate.PutInput{
		Kind: "samples.substrate.reamde.dev/tasks/task", Properties: map[string]any{"name": "alpha only"},
	})
	if _, err := betaDS.Get(ctx, task.Kind, task.ID); err == nil {
		t.Fatal("beta's token reached alpha's data")
	}
}

// Authentication is a READ. A token no longer carries a last-used stamp, so
// no number of authentications appends anything to the changelog — the
// changelog is the user's data, not the substrate's access log.
func TestAuthenticationWritesNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)
	registerUser(t, svc, testdb.Repository(t))
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	_, secret, err := ds.MintToken(ctx, substrate.MintTokenInput{Label: "scripted"})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	seq := maxSeq(t, ds)
	for range 5 {
		if _, _, err := svc.Authenticate(ctx, secret); err != nil {
			t.Fatalf("authenticate: %v", err)
		}
	}
	if got := maxSeq(t, ds); got != seq {
		t.Fatalf("a burst of authentications wrote %d entries", got-seq)
	}
}

// The enrollment URI is the otpauth:// form a password manager imports, with
// the verifier's own parameters.
func TestEnrollmentURIShape(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)
	enrollment, err := svc.BeginRegistration(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("begin registration: %v", err)
	}
	want := "otpauth://totp/Substrate:" + testdb.Repository(t) + "?secret=" + enrollment.Secret +
		"&issuer=Substrate&algorithm=SHA1&digits=6&period=30"
	if enrollment.URI != want {
		t.Fatalf("uri = %q, want %q", enrollment.URI, want)
	}
	if _, err := engine.TOTPCode(enrollment.Secret, engine.TOTPStep(time.Now())); err != nil {
		t.Fatalf("the issued seed produces no code: %v", err)
	}
}

// THE SECOND FACTOR, OFF (WithInsecureDisableTOTP — the local-development
// escape hatch). A registration needs no seed and no code, every door after it
// takes the password alone, and a WRONG password is still refused: the factor
// that is gone is the only thing that is gone.
func TestInsecureDisableTOTPTakesThePasswordAlone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t, engine.WithInsecureDisableTOTP())

	res, err := svc.Register(ctx, substrate.RegisterInput{
		Repository: testdb.Repository(t), Password: testPassword, Label: "cli",
	})
	if err != nil {
		t.Fatalf("register without a second factor: %v", err)
	}
	if res.Secret == "" {
		t.Fatal("registration must still end logged in")
	}

	// Login, and the credential change behind the password-factor rule, both
	// without a code.
	if _, _, err := svc.Login(ctx, substrate.LoginInput{
		Repository: testdb.Repository(t), Password: testPassword, Label: "cli",
	}); err != nil {
		t.Fatalf("login without a code: %v", err)
	}
	if _, _, err := svc.Login(ctx, substrate.LoginInput{
		Repository: testdb.Repository(t), Password: "not-the-password",
	}); err == nil {
		t.Fatal("a wrong password logged in: the password is the whole credential now")
	}
	if err := svc.ChangePassword(ctx, substrate.LoginInput{
		Repository: testdb.Repository(t), Password: testPassword,
	}, "a-second-correct-horse"); err != nil {
		t.Fatalf("change the password without a code: %v", err)
	}
	if _, _, err := svc.Login(ctx, substrate.LoginInput{
		Repository: testdb.Repository(t), Password: "a-second-correct-horse",
	}); err != nil {
		t.Fatalf("login with the new password: %v", err)
	}

	// No seed was sent, so the account has no second factor: a seed nobody
	// holds would lock it out the day the flag comes off, where none lets it
	// sign in with its password and enroll one.
	ds, _, err := svc.Authenticate(ctx, res.Secret)
	if err != nil {
		t.Fatalf("authenticate the registration token: %v", err)
	}
	cred, err := ds.Get(ctx, "substrate.reamde.dev/core/credential", "self")
	if err != nil {
		t.Fatalf("the credential record: %v", err)
	}
	if _, ok := cred.Properties["totpRef"]; ok {
		t.Fatalf("a second factor nobody enrolled behind the credential: %v", cred.Properties)
	}
}

// A registration may SKIP the second factor (decision 0237): the credential
// carries no TOTP ref, the password alone signs in, and the owner enrolls one
// later through the same two calls that replace one. From then on a sign-in
// without a code is answered ErrTOTPRequired, but only once the password is
// right.
func TestRegistrationMaySkipTheSecondFactor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)
	repository := testdb.Repository(t)

	// The skip is explicit: a registration that lost its enrollment is
	// refused, and a skip that carries one is contradictory.
	if _, err := svc.Register(ctx, substrate.RegisterInput{
		Repository: repository, Password: testPassword,
	}); !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("a registration with no enrollment and no skip: %v, want a validation error", err)
	}
	if _, err := svc.Register(ctx, substrate.RegisterInput{
		Repository: repository, Password: testPassword, SkipTOTP: true, TOTPSecret: "JBSWY3DPEHPK3PXP",
	}); !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("a skip carrying a seed: %v, want a validation error", err)
	}

	res, err := svc.Register(ctx, substrate.RegisterInput{
		Repository: repository, Password: testPassword, SkipTOTP: true, Label: "cli",
	})
	if err != nil {
		t.Fatalf("register skipping the second factor: %v", err)
	}
	ds, _, err := svc.Authenticate(ctx, res.Secret)
	if err != nil {
		t.Fatalf("authenticate the registration token: %v", err)
	}
	cred, err := ds.Get(ctx, "substrate.reamde.dev/core/credential", "self")
	if err != nil {
		t.Fatalf("the credential record: %v", err)
	}
	if _, ok := cred.Properties["totpRef"]; ok {
		t.Fatalf("a skipped registration has a second factor: %v", cred.Properties)
	}

	if _, _, err := svc.Login(ctx, substrate.LoginInput{Repository: repository, Password: testPassword}); err != nil {
		t.Fatalf("login with the password alone: %v", err)
	}
	if _, _, err := svc.Login(ctx, substrate.LoginInput{Repository: repository, Password: "not-the-password"}); !errors.Is(err, substrate.ErrAuth) || errors.Is(err, substrate.ErrTOTPRequired) {
		t.Fatalf("a wrong password: %v, want the one auth answer", err)
	}

	// Enrolling one later: the current factors are the password alone.
	enrollment, err := svc.BeginTOTPReenrollment(ctx, substrate.LoginInput{Repository: repository, Password: testPassword})
	if err != nil {
		t.Fatalf("begin enrolling a second factor: %v", err)
	}
	step := engine.TOTPStep(time.Now())
	code, err := engine.TOTPCode(enrollment.Secret, step)
	if err != nil {
		t.Fatalf("code from the enrollment: %v", err)
	}
	if err := svc.ReenrollTOTP(ctx, substrate.LoginInput{Repository: repository, Password: testPassword},
		enrollment.Secret, code); err != nil {
		t.Fatalf("enroll a second factor: %v", err)
	}
	cred, err = ds.Get(ctx, "substrate.reamde.dev/core/credential", "self")
	if err != nil {
		t.Fatalf("the credential record: %v", err)
	}
	if cred.Properties["totpRef"] != "<redacted>" {
		t.Fatalf("no second factor behind the credential after enrolling one: %v", cred.Properties)
	}

	if _, _, err := svc.Login(ctx, substrate.LoginInput{Repository: repository, Password: testPassword}); !errors.Is(err, substrate.ErrTOTPRequired) {
		t.Fatalf("a right password with no code: %v, want ErrTOTPRequired", err)
	}
	if _, _, err := svc.Login(ctx, substrate.LoginInput{Repository: repository, Password: "not-the-password"}); errors.Is(err, substrate.ErrTOTPRequired) || !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("a wrong password with no code: %v, want the one auth answer and no hint", err)
	}
	// The enrolling code was consumed; the next step's code signs in.
	next, err := engine.TOTPCode(enrollment.Secret, step+1)
	if err != nil {
		t.Fatalf("next code: %v", err)
	}
	if _, _, err := svc.Login(ctx, substrate.LoginInput{Repository: repository, Password: testPassword, TOTPCode: next}); err != nil {
		t.Fatalf("login with both factors: %v", err)
	}
}

// The seed a caller BRINGS is still stored when the factor is off, so a user
// who enrolled an authenticator ahead of time keeps it — and the code that
// comes with it is simply not checked.
func TestInsecureDisableTOTPKeepsASuppliedSeed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t, engine.WithInsecureDisableTOTP())

	seed, err := engine.NewTOTPSecret()
	if err != nil {
		t.Fatalf("mint a seed: %v", err)
	}
	if _, err := svc.Register(ctx, substrate.RegisterInput{
		Repository: testdb.Repository(t), Password: testPassword, TOTPSecret: seed,
		TOTPCode: "000000", Label: "cli",
	}); err != nil {
		t.Fatalf("register with a seed and a wrong code: %v", err)
	}
	// The seed is the one that was sent: a code from it re-enrolls nothing and
	// proves nothing here, but the material must be what the caller enrolled.
	code, err := engine.TOTPCode(seed, engine.TOTPStep(time.Now()))
	if err != nil {
		t.Fatalf("code from the seed: %v", err)
	}
	if _, _, err := svc.Login(ctx, substrate.LoginInput{
		Repository: testdb.Repository(t), Password: testPassword, TOTPCode: code,
	}); err != nil {
		t.Fatalf("login carrying a code: %v", err)
	}
	// A garbage seed is still refused: the flag drops the VERIFICATION, not
	// the shape of what gets sealed.
	if _, err := svc.Register(ctx, substrate.RegisterInput{
		Repository: "other.example.com", Password: testPassword, TOTPSecret: "not base32!!",
	}); err == nil {
		t.Fatal("a malformed seed was accepted")
	}
}

// A token's optional expiry, against the engine: a live one round-trips onto
// the TokenInfo and the stored record, and a passed one makes Authenticate
// fail with an auth error — server-enforced, no revoke step.
func TestTokenExpiryIsServerEnforced(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds := newDataset(t)

	future := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	info, secret, err := ds.MintToken(ctx, substrate.MintTokenInput{Label: "scripted", ExpiresAt: &future})
	if err != nil {
		t.Fatalf("mint with an expiry: %v", err)
	}
	if info.ExpiresAt == nil || !info.ExpiresAt.Equal(future) {
		t.Fatalf("minted expiresAt = %v, want %v", info.ExpiresAt, future)
	}

	_, got, err := svc.Authenticate(ctx, secret)
	if err != nil {
		t.Fatalf("authenticate live token: %v", err)
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(future) {
		t.Fatalf("authenticated expiresAt = %v, want %v", got.ExpiresAt, future)
	}
	if got.Label != "scripted" {
		t.Fatalf("authenticated label = %q", got.Label)
	}
	row := mustGet(t, ds, "substrate.reamde.dev/core/token", info.ID)
	if row.Properties["expiresAt"] == nil {
		t.Fatalf("token row missing expiresAt: %v", row.Properties)
	}
	// The hash is secret-typed: the read surface redacts it, so a token list
	// can never hand back the digest it authenticates against.
	if row.Properties["hash"] != "<redacted>" {
		t.Fatalf("token row exposed its hash: %v", row.Properties["hash"])
	}

	past := time.Now().Add(-time.Hour).UTC()
	_, expiredSecret, err := ds.MintToken(ctx, substrate.MintTokenInput{Label: "stale", ExpiresAt: &past})
	if err != nil {
		t.Fatalf("mint expired token: %v", err)
	}
	if _, _, err := svc.Authenticate(ctx, expiredSecret); err == nil {
		t.Fatal("an expired token must not authenticate")
	} else {
		wantErr(t, err, substrate.ErrAuth, "expired token")
	}
}
