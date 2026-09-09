package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
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
	if err != nil || len(repos) != 1 || repos[0].ID != testdb.Repository(t) || repos[0].Authority != testdb.Repository(t) {
		t.Fatalf("repositories = %v (err %v)", repos, err)
	}

	// The token registration returned works, and it is a RECORD in the
	// repository it opened.
	ds, info, err := svc.Authenticate(ctx, secret)
	if err != nil {
		t.Fatalf("authenticate the registration token: %v", err)
	}
	if info.ID != tok.ID || ds.Repository().ID != testdb.Repository(t) {
		t.Fatalf("authenticated as %+v in %q", info, ds.Repository().ID)
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
			TOTPSecret: enrollment.Secret, TOTPCode: u.code(t),
		},
		"one-label authority": {
			Password:   testPassword,
			TOTPSecret: enrollment.Secret, TOTPCode: u.code(t), Repository: testdb.Repository(t),
		},
		"uppercase authority": {
			Password:   testPassword,
			TOTPSecret: enrollment.Secret, TOTPCode: u.code(t), Repository: "Geoah.Example.com",
		},
		"publisher authority": {
			Password:   testPassword,
			TOTPSecret: enrollment.Secret, TOTPCode: u.code(t), Repository: "geoah.substrate.reamde.dev",
		},
		// DNS admits 253 bytes; a repository id, which the authority now is
		// (decision record 0052), admits MaxIDLen.
		"authority longer than a record id": {
			Password:   testPassword,
			TOTPSecret: enrollment.Secret, TOTPCode: u.code(t),
			Repository: strings.Repeat("a", 60) + "." + strings.Repeat("b", 60) + ".example.com",
		},
	} {
		if _, err := svc.Register(ctx, in); err == nil {
			t.Fatalf("%s: registration was accepted", name)
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
	if err != nil || len(repos) != 1 || repos[0].ID != authority {
		t.Fatalf("repositories = %+v (%v), want %s alone", repos, err, authority)
	}
	// The winner's repository opens, describes itself, and its changelog and
	// directory are intact: the loser erased nothing of the winner's.
	ds, err := svc.Dataset(ctx, authority)
	if err != nil {
		t.Fatalf("the winner's repository does not open: %v", err)
	}
	self, err := ds.Get(ctx, "substrate.reamde.dev/core/repository", authority)
	if err != nil || self.Properties["authority"] != authority {
		t.Fatalf("the winner's self-description: %+v, %v", self, err)
	}
	report := mustVerify(t, svc, authority)
	if !report.OK || report.Head == 0 || report.FileHead != report.Head {
		t.Fatalf("the winner's changelog does not verify: %+v", report)
	}
	dir := filepath.Join(engine.DataRootOf(svc), changelogfile.RepositoriesDir, authority)
	m, err := changelogfile.ReadManifest(dir)
	if err != nil || m.Authority != authority {
		t.Fatalf("the winner's manifest: %+v, %v", m, err)
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
		Kind: "task", Properties: map[string]any{"name": "survives a reset"},
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
	if alphaDS.Repository().ID != "alpha.example.com" || betaDS.Repository().ID != "beta.example.com" {
		t.Fatalf("tokens resolved to %q and %q", alphaDS.Repository().ID, betaDS.Repository().ID)
	}
	importVocabulary(t, alphaDS, "tasks")
	task := mustPut(t, alphaDS, owner, substrate.PutInput{
		Kind: "task", Properties: map[string]any{"name": "alpha only"},
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
	_, secret, err := ds.MintToken(ctx, "scripted", nil)
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

	// A seed WAS minted and sealed: the factor is off, not absent, so turning
	// the flag back off restores a credential that has one.
	ds, _, err := svc.Authenticate(ctx, res.Secret)
	if err != nil {
		t.Fatalf("authenticate the registration token: %v", err)
	}
	cred, err := ds.Get(ctx, "substrate.reamde.dev/core/credential", "self")
	if err != nil {
		t.Fatalf("the credential record: %v", err)
	}
	if cred.Properties["totpRef"] != "<redacted>" {
		t.Fatalf("no sealed second factor behind the credential: %v", cred.Properties)
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
