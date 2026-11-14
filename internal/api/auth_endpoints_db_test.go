package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// The door end to end on the engine, as the B3 "done when" states it:
// register → login → write → mint → revoke → change the password with a token
// alone and see it refused. The engine verifies the password and the code;
// the door's own gate, pacing and request shape are auth_endpoints_test.go's
// and token_guard_test.go's.

// authPassword is the password every account these tests register holds.
const authPassword = "correct-horse-battery-staple"

// authStep is one TOTP step. A code is spent once, so a request presenting
// another code of the same account moves the clock this far first; the step
// is longer than the door's pacing interval, so the move refills that too.
const authStep = 30 * time.Second

// authAccount is an account the engine registered underneath the door.
type authAccount struct {
	repository string
	// seed is the second factor's seed, "" for an account without one.
	seed string
	// token is the bearer the registration handed back.
	token string
}

// authRegister registers repository on the engine, with a second factor when
// factor is set, and moves the clock one TOTP step past the step the
// registration's code spent.
func authRegister(t *testing.T, env *dbEnv, repository string, factor bool) authAccount {
	t.Helper()
	ctx := context.Background()
	in := substrate.RegisterInput{Identity: repository, Repository: repository, Password: authPassword, SkipTOTP: !factor, Label: "test"}
	if factor {
		enrollment, err := env.svc.BeginRegistration(ctx, repository)
		if err != nil {
			t.Fatalf("enroll %s: %v", repository, err)
		}
		in.TOTPSecret, in.TOTPCode = enrollment.Secret, env.code(t, enrollment.Secret)
	}
	out, err := env.svc.Register(ctx, in)
	if err != nil {
		t.Fatalf("register %s: %v", repository, err)
	}
	env.clock.advance(authStep)
	return authAccount{repository: repository, seed: in.TOTPSecret, token: out.Secret}
}

// login is the login body for the account under password, with the factor's
// current code.
func (a authAccount) login(t *testing.T, env *dbEnv, password string) map[string]any {
	t.Helper()
	return loginBody(a.repository, password, env.code(t, a.seed))
}

// authRegisterAtTheDoor walks the registration gesture at the door: the
// enrollment, then the commit with a code from its seed. It answers the
// commit.
func authRegisterAtTheDoor(t *testing.T, env *dbEnv, repository string) *httptest.ResponseRecorder {
	t.Helper()
	rec := env.do(t, http.MethodPost, registerEnrolPath, "", map[string]any{
		"inviteCode": testInviteCode, "identity": repository,
	})
	wantStatus(t, rec, http.StatusOK)
	enrollment := decodeJSON[substrate.TOTPEnrollment](t, rec)
	return env.do(t, http.MethodPost, registerPath, "", map[string]any{
		"inviteCode": testInviteCode, "identity": repository, "password": authPassword,
		"totpSecret": enrollment.Secret, "totpCode": env.code(t, enrollment.Secret),
	})
}

func TestRegisterThenLogin(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withoutRepository(), withNameHost("example.com"))
	label := testdb.RepositoryLabel(t)

	// Step one issues the enrollment and writes nothing.
	rec := env.do(t, http.MethodPost, registerEnrolPath, "", map[string]any{
		"inviteCode": testInviteCode, "identity": label,
	})
	wantStatus(t, rec, http.StatusOK)
	enrollment := decodeJSON[substrate.TOTPEnrollment](t, rec)
	if enrollment.Secret == "" || enrollment.URI == "" {
		t.Fatalf("enrollment = %+v", enrollment)
	}
	if _, err := env.svc.Dataset(context.Background(), label+".example.com"); err == nil {
		t.Fatal("the enrollment created the repository")
	}

	// Step two commits, and registration ends logged in.
	env.clock.advance(defaultAuthInterval + time.Millisecond)
	rec = env.do(t, http.MethodPost, registerPath, "", map[string]any{
		"inviteCode": testInviteCode, "identity": label,
		"password":   authPassword,
		"totpSecret": enrollment.Secret, "totpCode": env.code(t, enrollment.Secret),
		"label": "console",
	})
	wantStatus(t, rec, http.StatusCreated)
	out := decodeJSON[substrate.Registered](t, rec)
	if out.Secret == "" || out.Token.Label != "console" {
		t.Fatalf("registration response = %+v", out)
	}
	// A bare repository label is completed under the host the request reached
	// (httptest requests carry `example.com`), and the caller learns the
	// authority it got here.
	if out.Repository != label+".example.com" {
		t.Fatalf("registered repository = %q, want the label completed under the request host", out.Repository)
	}

	// The token registration handed back is an ordinary bearer.
	wantStatus(t, env.do(t, http.MethodGet, recordsPath, out.Secret, nil),
		http.StatusOK)

	// Login mints another — sessions ARE token records. The registration
	// spent its code, so the login presents the next step's.
	env.clock.advance(authStep)
	rec = env.do(t, http.MethodPost, loginPath, "",
		loginBody(label, authPassword, env.code(t, enrollment.Secret)))
	wantStatus(t, rec, http.StatusCreated)
	if login := decodeJSON[substrate.MintedToken](t, rec); login.Secret == out.Secret {
		t.Fatal("login handed back the registration's secret")
	}
}

// A registration may skip the second factor: no enrollment, no code, and the
// account signs in with its password alone until it enrolls one. That the
// login takes no code is what shows the skip reached the engine: an account
// with a factor answers a codeless login totp_required.
func TestRegistrationMaySkipTheSecondFactor(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withoutRepository(), withNameHost("example.com"))
	label := testdb.RepositoryLabel(t)
	rec := env.do(t, http.MethodPost, registerPath, "", map[string]any{
		"inviteCode": testInviteCode, "identity": label,
		"password": authPassword, "skipTotp": true,
	})
	wantStatus(t, rec, http.StatusCreated)
	if out := decodeJSON[substrate.Registered](t, rec); out.Secret == "" {
		t.Fatalf("registration response = %+v", out)
	}

	env.clock.advance(defaultAuthInterval + time.Millisecond)
	rec = env.do(t, http.MethodPost, loginPath, "", loginBody(label, authPassword, ""))
	wantStatus(t, rec, http.StatusCreated)

	// The credential changes take the password alone too: the door no longer
	// demands a code the account does not have.
	env.clock.advance(defaultAuthInterval + time.Millisecond)
	rec = env.do(t, http.MethodPost, "/password", "", map[string]any{
		"identity": label, "password": authPassword,
		"newPassword": "a-new-correct-horse",
	})
	wantStatus(t, rec, http.StatusOK)
}

// THE TWO-STEP SIGN-IN: a right password with no code, on an account that has
// a second factor, answers totp_required, so a client asks for the code. A
// wrong password with no code is the one `auth` answer it always was, so the
// new code says nothing to a caller who does not hold the password.
func TestLoginWithoutACodeAsksForOne(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withoutRepository())
	acct := authRegister(t, env, testdb.Repository(t), true)

	rec := env.do(t, http.MethodPost, loginPath, "", loginBody(acct.repository, authPassword, ""))
	wantErrorCode(t, rec, http.StatusUnauthorized, codeTOTPRequired)

	env.clock.advance(defaultAuthInterval + time.Millisecond)
	rec = env.do(t, http.MethodPost, loginPath, "", loginBody(acct.repository, "wrong", ""))
	wantErrorCode(t, rec, http.StatusUnauthorized, codeAuth)

	env.clock.advance(defaultAuthInterval + time.Millisecond)
	rec = env.do(t, http.MethodPost, "/password", "", map[string]any{
		"identity": acct.repository, "password": authPassword,
		"newPassword": "a-new-correct-horse",
	})
	wantErrorCode(t, rec, http.StatusUnauthorized, codeTOTPRequired)
}

// A repository name carrying a dot IS the authority: the host is not appended
// to it, and the engine's answer is echoed verbatim.
func TestRegistrationKeepsADottedRepositoryName(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withoutRepository())
	name := testdb.RepositoryLabel(t) + ".example.org"
	rec := authRegisterAtTheDoor(t, env, " "+name+" ")
	wantStatus(t, rec, http.StatusCreated)
	out := decodeJSON[substrate.Registered](t, rec)
	if out.Repository != name {
		t.Fatalf("registered repository = %q, want the trimmed name the request sent", out.Repository)
	}
	ds, err := env.svc.Dataset(context.Background(), name)
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	if got := ds.Repository().Authority; got != name {
		t.Fatalf("the repository was created under %q", got)
	}
}

// With NO invite code configured the door reads none: a registration carrying
// an empty code, or any code at all, is admitted. This is the local substrate;
// discovery says so (TestDiscoveryReportsNoInviteRequiredWithNoInviteCode) so a
// client stops asking. Both halves reach the engine: the commit's code is
// checked against the seed the enrollment handed out.
func TestRegistrationAdmitsWithoutACodeWhenNoneIsConfigured(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withoutRepository(), withNameHost("example.com"), withConfig(func(c *Config) { c.InviteCode = "" }))
	label := testdb.RepositoryLabel(t)
	rec := env.do(t, http.MethodPost, registerEnrolPath, "", map[string]any{
		"inviteCode": "", "identity": label,
	})
	wantStatus(t, rec, http.StatusOK)
	enrollment := decodeJSON[substrate.TOTPEnrollment](t, rec)
	env.clock.advance(defaultAuthInterval + time.Millisecond)
	rec = env.do(t, http.MethodPost, registerPath, "", map[string]any{
		"inviteCode": "anything-at-all", "identity": label,
		"password":   authPassword,
		"totpSecret": enrollment.Secret, "totpCode": env.code(t, enrollment.Secret),
	})
	wantStatus(t, rec, http.StatusCreated)
	if _, err := env.svc.Dataset(context.Background(), label+".example.com"); err != nil {
		t.Fatalf("the registration created no repository: %v", err)
	}
}

// The password-factor rule's other half: with both current factors presented
// directly in the body, the change goes through, and a wrong current password
// is refused whatever else is right. A bearer token alone is refused at the
// door (TestCredentialChangesRefuseABearerTokenAlone).
func TestCredentialChangeTakesBothCurrentFactors(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withoutRepository())
	acct := authRegister(t, env, testdb.Repository(t), true)
	const changed = "a-brand-new-passphrase"

	rec := env.do(t, http.MethodPost, "/password", "", map[string]any{
		"identity": acct.repository, "password": authPassword, "totpCode": env.code(t, acct.seed),
		"newPassword": changed,
	})
	wantStatus(t, rec, http.StatusOK)

	// The new password is the account's now, and the old one is not.
	env.clock.advance(authStep)
	wantStatus(t, env.do(t, http.MethodPost, loginPath, "", acct.login(t, env, changed)), http.StatusCreated)
	env.clock.advance(authStep)
	wantErrorCode(t, env.do(t, http.MethodPost, loginPath, "", acct.login(t, env, authPassword)),
		http.StatusUnauthorized, codeAuth)

	// A wrong current password is refused whatever else is right.
	env.clock.advance(authStep)
	rec = env.do(t, http.MethodPost, "/password", "", map[string]any{
		"identity": acct.repository, "password": authPassword, "totpCode": env.code(t, acct.seed),
		"newPassword": "another-passphrase-again",
	})
	wantErrorCode(t, rec, http.StatusUnauthorized, codeAuth)
}

// Deleting the repository takes both factors, erases it, and frees the name
// for the next registration.
func TestDeleteRepositoryTakesBothFactorsAndFreesTheName(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withoutRepository())
	ctx := context.Background()
	acct := authRegister(t, env, testdb.Repository(t), true)

	rec := env.do(t, http.MethodPost, "/repository/delete", "", map[string]any{
		"identity": acct.repository, "repository": acct.repository, "password": "wrong", "totpCode": env.code(t, acct.seed),
	})
	wantErrorCode(t, rec, http.StatusUnauthorized, codeAuth)
	if _, err := env.svc.Dataset(ctx, acct.repository); err != nil {
		t.Fatalf("a refused delete erased the repository: %v", err)
	}
	// While it stands, the name is held.
	env.clock.advance(authStep)
	wantErrorCode(t, authRegisterAtTheDoor(t, env, acct.repository), http.StatusUnprocessableEntity, codeValidation)

	env.clock.advance(authStep)
	rec = env.do(t, http.MethodPost, "/repository/delete", "", map[string]any{
		"identity": acct.repository, "repository": acct.repository, "password": authPassword, "totpCode": env.code(t, acct.seed),
	})
	wantStatus(t, rec, http.StatusNoContent)
	if _, err := env.svc.Dataset(ctx, acct.repository); err == nil {
		t.Fatal("the repository survived its delete")
	}
	wantErrorCode(t, env.do(t, http.MethodGet, recordsPath, acct.token, nil), http.StatusUnauthorized, codeAuth)

	env.clock.advance(authStep)
	wantStatus(t, authRegisterAtTheDoor(t, env, acct.repository), http.StatusCreated)
}

// Re-enrollment is two steps: the current factors issue a candidate seed and
// write nothing, and the swap needs a code from the candidate beside them, so
// nothing changes on a seed nobody holds. A missing candidate code is the
// door's one refused-factor answer.
func TestTOTPReenrollmentIsTwoSteps(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withoutRepository())
	acct := authRegister(t, env, testdb.Repository(t), true)

	rec := env.do(t, http.MethodPost, "/totp/enroll", "", map[string]any{
		"identity": acct.repository, "password": authPassword, "totpCode": env.code(t, acct.seed),
	})
	wantStatus(t, rec, http.StatusOK)
	enrollment := decodeJSON[substrate.TOTPEnrollment](t, rec)
	if enrollment.Secret == "" {
		t.Fatalf("enrollment = %+v", enrollment)
	}

	env.clock.advance(authStep)
	rec = env.do(t, http.MethodPost, "/totp", "", map[string]any{
		"identity": acct.repository, "password": authPassword, "totpCode": env.code(t, acct.seed),
		"newTotpSecret": enrollment.Secret, "newTotpCode": "",
	})
	wantErrorCode(t, rec, http.StatusUnauthorized, codeAuth)

	// The refused swap left the current seed standing, so it proves the
	// current factors here.
	env.clock.advance(authStep)
	rec = env.do(t, http.MethodPost, "/totp", "", map[string]any{
		"identity": acct.repository, "password": authPassword, "totpCode": env.code(t, acct.seed),
		"newTotpSecret": enrollment.Secret, "newTotpCode": env.code(t, enrollment.Secret),
	})
	wantStatus(t, rec, http.StatusOK)

	// The candidate is the account's second factor now.
	env.clock.advance(authStep)
	rec = env.do(t, http.MethodPost, loginPath, "",
		loginBody(acct.repository, authPassword, env.code(t, enrollment.Secret)))
	wantStatus(t, rec, http.StatusCreated)
}

// Every refused factor answers the same way: which one was wrong, and whether
// the user exists at all, are not questions the door answers.
func TestLoginGivesNoExistenceOracle(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withoutRepository())
	acct := authRegister(t, env, testdb.Repository(t), true)
	badPassword := env.do(t, http.MethodPost, loginPath, "", acct.login(t, env, "wrong"))
	env.clock.advance(defaultAuthInterval + time.Millisecond)
	noUser := env.do(t, http.MethodPost, loginPath, "", loginBody("nosuch", "wrong", "000000"))

	wantErrorCode(t, badPassword, http.StatusUnauthorized, codeAuth)
	wantErrorCode(t, noUser, http.StatusUnauthorized, codeAuth)
	if badPassword.Body.String() != noUser.Body.String() {
		t.Fatalf("responses differ:\n%s\n%s", badPassword.Body.String(), noUser.Body.String())
	}
}

// THE DOOR WITH THE SECOND FACTOR OFF (SUBSTRATE_INSECURE_DISABLE_TOTP): the
// deployment says so in discovery so a client stops asking, the credential
// changes take the password ALONE — and the password-factor rule itself is
// untouched, because it was never about the code: a request with no password
// is still refused outright. The account holds a factor all the same: the
// switch drops the code's verdict, not the account's seed.
func TestTOTPDisabledDoorTakesThePasswordAlone(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withoutRepository(),
		withEngine(engine.WithInsecureDisableTOTP()),
		withConfig(func(c *Config) { c.TOTPDisabled = true }))
	acct := authRegister(t, env, testdb.Repository(t), true)

	rec := env.do(t, http.MethodGet, "/.well-known/substrate/server.json", "", nil)
	wantStatus(t, rec, http.StatusOK)
	doc := decodeJSON[map[string]any](t, rec)
	registration, _ := doc["registration"].(map[string]any)
	if registration == nil || registration["totpRequired"] != false {
		t.Fatalf("discovery must report the door's shape: %+v", doc["registration"])
	}

	// A password and no code changes the password.
	rec = env.do(t, http.MethodPost, "/password", "", map[string]any{
		"identity": acct.repository, "password": authPassword,
		"newPassword": "a-new-correct-horse",
	})
	wantStatus(t, rec, http.StatusOK)

	// A bearer token and no password is still refused with 403 — a token is
	// not evidence here and the missing factor is not why.
	env.clock.advance(defaultAuthInterval + time.Millisecond)
	rec = env.do(t, http.MethodPost, "/password", acct.token, map[string]any{
		"identity": acct.repository, "newPassword": "another-correct-horse",
	})
	wantStatus(t, rec, http.StatusForbidden)
}

// The default deployment is unchanged: discovery says a code is required, and
// a credential change without one, on an account that has a factor, is
// answered totp_required.
func TestDiscoveryRequiresTOTPByDefault(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withoutRepository())
	acct := authRegister(t, env, testdb.Repository(t), true)
	rec := env.do(t, http.MethodGet, "/.well-known/substrate/server.json", "", nil)
	doc := decodeJSON[map[string]any](t, rec)
	registration, _ := doc["registration"].(map[string]any)
	if registration == nil || registration["totpRequired"] != true {
		t.Fatalf("discovery must require the second factor by default: %+v", doc["registration"])
	}
	rec = env.do(t, http.MethodPost, "/password", "", map[string]any{
		"identity": acct.repository, "password": authPassword,
		"newPassword": "a-new-correct-horse",
	})
	wantErrorCode(t, rec, http.StatusUnauthorized, codeTOTPRequired)
}
