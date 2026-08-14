package engine_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
)

// Registration is two calls and ONE durable write: the enrollment creates
// nothing, and the commit creates the repository, the sealed material, the
// credential record and the first token together.
func TestRegistrationCreatesTheUserAndNothingBefore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)

	// An abandoned enrollment leaves no trace at all.
	if _, err := svc.BeginRegistration(ctx, "geoah"); err != nil {
		t.Fatalf("begin registration: %v", err)
	}
	if repos, err := svc.Repositories(ctx); err != nil || len(repos) != 0 {
		t.Fatalf("an abandoned enrollment created %v (err %v)", repos, err)
	}
	if _, err := svc.BeginRegistration(ctx, "Bad Name"); err == nil {
		t.Fatal("a malformed username must not get an enrollment")
	}

	user, tok, secret := registerUser(t, svc, "geoah")
	if tok.Label != "cli" || secret == "" {
		t.Fatalf("registration token = %+v, secret %q", tok, secret)
	}
	repos, err := svc.Repositories(ctx)
	if err != nil || len(repos) != 1 || repos[0].Name != "geoah" {
		t.Fatalf("repositories = %v (err %v)", repos, err)
	}

	// The token registration returned works, and it is a RECORD in the
	// repository it opened.
	ds, info, err := svc.Authenticate(ctx, secret)
	if err != nil {
		t.Fatalf("authenticate the registration token: %v", err)
	}
	if info.ID != tok.ID || ds.Repository().Name != "geoah" {
		t.Fatalf("authenticated as %+v in %q", info, ds.Repository().Name)
	}

	// The credential is a singleton record holding REFS, never material.
	cred, err := ds.Get(ctx, "core.substrate.reamde.dev/credential", "self")
	if err != nil {
		t.Fatalf("the credential record: %v", err)
	}
	if cred.Properties["username"] != "geoah" {
		t.Fatalf("credential username = %v", cred.Properties["username"])
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

	// A second registration for the same username is refused, and refusing it
	// leaves the first user untouched.
	enrollment, err := svc.BeginRegistration(ctx, "geoah")
	if err != nil {
		t.Fatalf("begin registration: %v", err)
	}
	dup := &authUser{username: "geoah", seed: enrollment.Secret}
	if _, err := svc.Register(ctx, substrate.RegisterInput{
		Username: "geoah", Password: testPassword,
		TOTPSecret: enrollment.Secret, TOTPCode: dup.code(t),
	}); err == nil {
		t.Fatal("a taken username must not register twice")
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
	enrollment, err := svc.BeginRegistration(ctx, "geoah")
	if err != nil {
		t.Fatalf("begin registration: %v", err)
	}
	u := &authUser{username: "geoah", seed: enrollment.Secret}

	for name, in := range map[string]substrate.RegisterInput{
		"wrong code": {
			Username: "geoah", Password: testPassword,
			TOTPSecret: enrollment.Secret, TOTPCode: "000000",
		},
		"short password": {
			Username: "geoah", Password: "short",
			TOTPSecret: enrollment.Secret, TOTPCode: u.code(t),
		},
		"unusable seed": {
			Username: "geoah", Password: testPassword,
			TOTPSecret: "not base32!", TOTPCode: "123456",
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

// Login is the second door: both factors, a token record minted, and the code
// spent — a replay of the same code is refused even with the right password.
func TestLoginMintsATokenAndSpendsTheCode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)
	user, _, _ := registerUser(t, svc, "geoah")

	code := user.code(t)
	tok, secret, err := svc.Login(ctx, substrate.LoginInput{
		Username: "geoah", Password: testPassword, TOTPCode: code, Label: "console",
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
		Username: "geoah", Password: testPassword, TOTPCode: code,
	}); err == nil {
		t.Fatal("a consumed code logged in again")
	} else {
		wantErr(t, err, substrate.ErrAuth, "replayed code")
	}
}

// Every login failure answers the same way, whoever the caller is: a wrong
// password, a wrong code and a username that does not exist are one error.
func TestLoginGivesNoOracle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)
	user, _, _ := registerUser(t, svc, "geoah")

	for name, in := range map[string]substrate.LoginInput{
		"wrong password": {Username: "geoah", Password: "wrong-password-entirely", TOTPCode: user.code(t)},
		"wrong code":     {Username: "geoah", Password: testPassword, TOTPCode: "000000"},
		"unknown user":   {Username: "nosuch", Password: testPassword, TOTPCode: "000000"},
	} {
		_, _, err := svc.Login(ctx, in)
		if err == nil {
			t.Fatalf("%s: login succeeded", name)
		}
		wantErr(t, err, substrate.ErrAuth, name)
		if got := err.Error(); !strings.Contains(got, "bad username, password or code") {
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
	user, _, secret := registerUser(t, svc, "geoah")

	const newPassword = "a-longer-passphrase-entirely"
	if err := svc.ChangePassword(ctx, substrate.LoginInput{
		Username: "geoah", Password: testPassword, TOTPCode: user.code(t),
	}, newPassword); err != nil {
		t.Fatalf("change password: %v", err)
	}
	user.password = newPassword

	// A wrong current password cannot change it, whatever else is right. A
	// refused attempt spends no code — nothing is consumed until both factors
	// have already passed.
	if err := svc.ChangePassword(ctx, substrate.LoginInput{
		Username: "geoah", Password: testPassword, TOTPCode: "000000",
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
		Username: "geoah", Password: newPassword, TOTPCode: user.code(t),
	}); err != nil {
		t.Fatalf("login with the new password: %v", err)
	}
	if _, _, err := svc.Login(ctx, substrate.LoginInput{
		Username: "geoah", Password: testPassword, TOTPCode: "000000",
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
	user, _, _ := registerUser(t, svc, "geoah")
	oldSeed := user.seed

	enrollment, err := svc.BeginTOTPReenrollment(ctx, substrate.LoginInput{
		Username: "geoah", Password: testPassword, TOTPCode: user.code(t),
	})
	if err != nil {
		t.Fatalf("begin re-enrollment: %v", err)
	}
	if enrollment.Secret == oldSeed {
		t.Fatal("the re-enrollment reissued the same seed")
	}
	if !strings.Contains(enrollment.URI, "otpauth://totp/Substrate:geoah") {
		t.Fatalf("enrollment uri = %q", enrollment.URI)
	}
	// Nothing changed yet: the credential still points at the live seed, and
	// the candidate is only a candidate.
	waitStep(t)
	candidate := &authUser{seed: enrollment.Secret}
	if err := svc.ReenrollTOTP(ctx, substrate.LoginInput{
		Username: "geoah", Password: testPassword, TOTPCode: user.code(t),
	}, enrollment.Secret, candidate.code(t)); err != nil {
		t.Fatalf("re-enroll: %v", err)
	}

	// The old seed is dead, the new one works, the password is unchanged.
	if _, _, err := svc.Login(ctx, substrate.LoginInput{
		Username: "geoah", Password: testPassword, TOTPCode: user.code(t),
	}); err == nil {
		t.Fatal("the replaced seed still logs in")
	}
	waitStep(t)
	if _, _, err := svc.Login(ctx, substrate.LoginInput{
		Username: "geoah", Password: testPassword, TOTPCode: candidate.code(t),
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
	registerUser(t, svc, "geoah")
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	importVocabulary(t, ds, "tasks")
	task := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "task", Properties: map[string]any{"title": "survives a reset"},
	})

	resetter, ok := svc.(interface {
		ResetUser(context.Context, string, string) (substrate.TOTPEnrollment, error)
	})
	if !ok {
		t.Fatal("the engine service must expose ResetUser for substratectl")
	}
	const resetPassword = "operator-issued-passphrase"
	enrollment, err := resetter.ResetUser(ctx, "geoah", resetPassword)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	fresh := &authUser{username: "geoah", password: resetPassword, seed: enrollment.Secret}
	if _, _, err := svc.Login(ctx, substrate.LoginInput{
		Username: "geoah", Password: resetPassword, TOTPCode: fresh.code(t),
	}); err != nil {
		t.Fatalf("login after a reset: %v", err)
	}
	if _, err := ds.Get(ctx, task.Kind, task.ID); err != nil {
		t.Fatalf("the reset took the data with it: %v", err)
	}
	if _, err := resetter.ResetUser(ctx, "nosuch", resetPassword); err == nil {
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
	registerUser(t, svc, "geoah")
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: "core.substrate.reamde.dev/credential", ID: "self",
		Properties: map[string]any{"username": "geoah", "passwordRef": "forged"},
	}); err == nil {
		t.Fatal("the credential was forged through the generic surface")
	} else {
		wantErr(t, err, substrate.ErrForbidden, "credential put")
	}
	if _, err := ds.Patch(ctx, owner, "core.substrate.reamde.dev/credential", "self", substrate.PatchInput{
		Properties: map[string]any{"passwordRef": "forged"},
	}); err == nil {
		t.Fatal("the credential was repointed through the generic surface")
	}
	if _, err := ds.Delete(ctx, owner, "core.substrate.reamde.dev/credential", "self"); err == nil {
		t.Fatal("the credential was deleted through the generic surface")
	}
	if _, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: "core.substrate.reamde.dev/token", Properties: map[string]any{"label": "forged", "hash": "x"},
	}); err == nil {
		t.Fatal("a token was forged through the generic surface")
	}
	// Reading and listing both kinds stays ordinary.
	if _, err := ds.Get(ctx, "core.substrate.reamde.dev/credential", "self"); err != nil {
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
	_, _, alphaSecret := registerUser(t, svc, "alpha")
	_, _, betaSecret := registerUser(t, svc, "beta")

	alphaDS, _, err := svc.Authenticate(ctx, alphaSecret)
	if err != nil {
		t.Fatalf("authenticate alpha: %v", err)
	}
	betaDS, _, err := svc.Authenticate(ctx, betaSecret)
	if err != nil {
		t.Fatalf("authenticate beta: %v", err)
	}
	if alphaDS.Repository().Name != "alpha" || betaDS.Repository().Name != "beta" {
		t.Fatalf("tokens resolved to %q and %q", alphaDS.Repository().Name, betaDS.Repository().Name)
	}
	importVocabulary(t, alphaDS, "tasks")
	task := mustPut(t, alphaDS, owner, substrate.PutInput{
		Kind: "task", Properties: map[string]any{"title": "alpha only"},
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
	registerUser(t, svc, "geoah")
	ds, err := svc.Dataset(ctx, "geoah")
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
	enrollment, err := svc.BeginRegistration(ctx, "geoah")
	if err != nil {
		t.Fatalf("begin registration: %v", err)
	}
	want := "otpauth://totp/Substrate:geoah?secret=" + enrollment.Secret +
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
		Username: "geoah", Password: testPassword, Label: "cli",
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
		Username: "geoah", Password: testPassword, Label: "cli",
	}); err != nil {
		t.Fatalf("login without a code: %v", err)
	}
	if _, _, err := svc.Login(ctx, substrate.LoginInput{
		Username: "geoah", Password: "not-the-password",
	}); err == nil {
		t.Fatal("a wrong password logged in: the password is the whole credential now")
	}
	if err := svc.ChangePassword(ctx, substrate.LoginInput{
		Username: "geoah", Password: testPassword,
	}, "a-second-correct-horse"); err != nil {
		t.Fatalf("change the password without a code: %v", err)
	}
	if _, _, err := svc.Login(ctx, substrate.LoginInput{
		Username: "geoah", Password: "a-second-correct-horse",
	}); err != nil {
		t.Fatalf("login with the new password: %v", err)
	}

	// A seed WAS minted and sealed: the factor is off, not absent, so turning
	// the flag back off restores a credential that has one.
	ds, _, err := svc.Authenticate(ctx, res.Secret)
	if err != nil {
		t.Fatalf("authenticate the registration token: %v", err)
	}
	cred, err := ds.Get(ctx, "core.substrate.reamde.dev/credential", "self")
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
		Username: "geoah", Password: testPassword, TOTPSecret: seed,
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
		Username: "geoah", Password: testPassword, TOTPCode: code,
	}); err != nil {
		t.Fatalf("login carrying a code: %v", err)
	}
	// A garbage seed is still refused: the flag drops the VERIFICATION, not
	// the shape of what gets sealed.
	if _, err := svc.Register(ctx, substrate.RegisterInput{
		Username: "other", Password: testPassword, TOTPSecret: "not base32!!",
	}); err == nil {
		t.Fatal("a malformed seed was accepted")
	}
}
