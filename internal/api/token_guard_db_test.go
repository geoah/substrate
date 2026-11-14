package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// The door's pacing in front of real accounts: what it lets through, the
// engine verifies and answers.

// The two-call registration gesture is ONE attempt: `/register/enroll`
// immediately followed by `/register` is what the console and substratectl both do,
// so the pacing must not 429 the commit half a second after the enroll. The
// SAME caller's SECOND gesture still waits the full interval — the discount is
// inside one gesture, not a doubling of the rate — and that pacing is the
// per-(IP, username) bucket's, not the substrate-wide one (which is a
// distributed-spray cap, not a per-user pacer).
func TestRegistrationGestureIsOneAttempt(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withoutRepository(), withNameHost("example.com"))
	label := testdb.RepositoryLabel(t)
	enroll := func(user string) *httptest.ResponseRecorder {
		return env.do(t, http.MethodPost, registerEnrolPath, "", map[string]any{
			"inviteCode": testInviteCode, "identity": user,
		})
	}
	first := enroll(label)
	if first.Code != http.StatusOK {
		t.Fatalf("enroll = %d, want 200", first.Code)
	}
	enrollment := decodeJSON[substrate.TOTPEnrollment](t, first)
	// No clock advance at all: the commit half follows immediately.
	rec := env.do(t, http.MethodPost, registerPath, "", map[string]any{
		"inviteCode": testInviteCode, "identity": label,
		"password":   authPassword,
		"totpSecret": enrollment.Secret, "totpCode": env.code(t, enrollment.Secret),
	})
	wantStatus(t, rec, http.StatusCreated)

	// The SAME caller's second gesture in the same interval is refused: the
	// (IP, repository) bucket spent its whole allowance across the pair's halves,
	// so it is reached (429) before the endpoint ever runs.
	if code := enroll(label).Code; code != http.StatusTooManyRequests {
		t.Fatalf("a second gesture from the same caller inside the interval = %d, want 429", code)
	}
}

// The substrate-wide bucket is a DISTRIBUTED-spray cap, not a per-user pacer:
// two honest users logging in within one interval, from two IPs, must BOTH
// reach the service, which each answers with a minted token. Sizing that
// global bucket at a single request's cost used to funnel the whole substrate
// into one attempt per interval — the second honest login got a 429 for the
// first's sake.
func TestGlobalBucketDoesNotThrottleHonestConcurrentUsers(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withoutRepository())
	geoah := authRegister(t, env, testdb.Repository(t), true)
	ada := authRegister(t, env, testdb.RepositoryLabel(t)+"2.example.com", true)

	// geoah and ada, different peers, no clock advance between them.
	wantStatus(t, env.doFrom(t, "10.0.0.1:1", http.MethodPost, loginPath, "",
		geoah.login(t, env, authPassword)), http.StatusCreated)
	wantStatus(t, env.doFrom(t, "10.0.0.2:1", http.MethodPost, loginPath, "",
		ada.login(t, env, authPassword)), http.StatusCreated)
}

// The pair's discount does not loosen login: one attempt per interval, still.
func TestLoginKeepsOneAttemptPerInterval(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withoutRepository())
	acct := authRegister(t, env, testdb.Repository(t), true)
	wantStatus(t, env.do(t, http.MethodPost, loginPath, "", acct.login(t, env, authPassword)), http.StatusCreated)
	// Half an interval later — enough for a paired call, not for a request.
	env.clock.advance(defaultAuthInterval / 2)
	rec := env.do(t, http.MethodPost, loginPath, "", acct.login(t, env, authPassword))
	wantErrorCode(t, rec, http.StatusTooManyRequests, codeRateLimited)
	env.clock.advance(defaultAuthInterval/2 + time.Millisecond)
	// The first login spent its step's code, and the verifier admits the next
	// step's one step early.
	next, err := engine.TOTPCode(acct.seed, engine.TOTPStep(env.clock.now())+1)
	if err != nil {
		t.Fatalf("totp code: %v", err)
	}
	wantStatus(t, env.do(t, http.MethodPost, loginPath, "", loginBody(acct.repository, authPassword, next)), http.StatusCreated)
}

// The removed lockout, as a standing test. Two halves, both of which the old
// exponential lockout failed:
//
//   - a stranger hammering ANY username must not take the door offline for
//     everyone. The lockout's key set once included a substrate-wide key, so
//     five bad requests — no credentials needed — refused login, registration
//     and both credential changes for every user, doubling to an hour cap and
//     unclearable, since only a request that got past the lock cleared the run.
//   - a user's own failures must not lock the user out, because the key is a
//     username anybody may name.
func TestFailuresNeverLockAnybodyOut(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withoutRepository())
	acct := authRegister(t, env, testdb.Repository(t), true)

	// A stranger sprays a username that does not exist.
	stranger := loginBody("victim", "wrong-password", "000000")
	for range 20 {
		wantErrorCode(t, env.do(t, http.MethodPost, loginPath, "", stranger),
			http.StatusUnauthorized, codeAuth)
		env.clock.advance(defaultAuthInterval + time.Millisecond)
	}

	// The door is still open to an unrelated, honest user.
	wantStatus(t, env.do(t, http.MethodPost, loginPath, "", acct.login(t, env, authPassword)), http.StatusCreated)

	// Now the owner fails repeatedly against their own account...
	env.clock.advance(defaultAuthInterval + time.Millisecond)
	ownBad := loginBody(acct.repository, "wrong-password", "000000")
	for range 20 {
		wantErrorCode(t, env.do(t, http.MethodPost, loginPath, "", ownBad),
			http.StatusUnauthorized, codeAuth)
		env.clock.advance(defaultAuthInterval + time.Millisecond)
	}

	// ...and the right credentials still work on the very next attempt.
	wantStatus(t, env.do(t, http.MethodPost, loginPath, "", acct.login(t, env, authPassword)), http.StatusCreated)
}
