package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The door's guards: a per-IP, per-username
// and GLOBAL rate limit and a bounded body —
// all in front of the service, so a refused request never reaches it.

const (
	registerPath      = "/register"
	registerEnrolPath = "/register/enroll"
	loginPath         = "/login"
)

func loginBody(username, password, code string) map[string]any {
	return map[string]any{"username": username, "password": password, "totpCode": code}
}

func TestLoginRateLimitIgnoresSpoofedClientIPHeaders(t *testing.T) {
	env := newTestEnv(t)
	body := loginBody("geoah", "wrong", "000000")
	for i := range 30 {
		env.do(t, http.MethodPost, loginPath, "", body,
			"True-Client-IP", fmt.Sprintf("203.0.113.%d", i),
			"X-Real-IP", fmt.Sprintf("198.51.100.%d", i),
			"X-Forwarded-For", fmt.Sprintf("192.0.2.%d", i))
	}
	if env.svc.loginCalls != 1 {
		t.Fatalf("Login called %d times; client-supplied IP headers must not vary the limiter key", env.svc.loginCalls)
	}
}

func TestLoginRateLimitIsAlsoPerUsernameAcrossPeers(t *testing.T) {
	env := newTestEnv(t)
	body := loginBody("geoah", "wrong", "000000")
	for i := range 20 {
		env.doFrom(t, fmt.Sprintf("10.0.%d.%d:4321", i, i), http.MethodPost, loginPath, "", body)
	}
	if env.svc.loginCalls != 1 {
		t.Fatalf("Login called %d times; one username must be limited across peers", env.svc.loginCalls)
	}
}

// The GLOBAL key is what makes a spray across many usernames cost what a run
// at one costs: unknown spellings additionally share a single bucket, so they
// buy neither map growth nor extra attempts.
func TestLoginRateLimitIsGlobal(t *testing.T) {
	env := newTestEnv(t)
	for i := range 20 {
		env.doFrom(t, fmt.Sprintf("10.1.%d.%d:4321", i, i), http.MethodPost, loginPath, "",
			loginBody(strings.Repeat("x", 64)+fmt.Sprint(i), "wrong", "000000"))
	}
	if env.svc.loginCalls != 1 {
		t.Fatalf("Login called %d times; a spray across usernames must share the global bucket", env.svc.loginCalls)
	}
}

func TestAuthBodyIsBounded(t *testing.T) {
	env := newTestEnv(t)
	rec := env.do(t, http.MethodPost, loginPath, "", map[string]any{
		"username": "geoah", "password": strings.Repeat("a", 2<<20), "totpCode": "000000",
	})
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
	if env.svc.loginCalls != 0 {
		t.Fatalf("Login called %d times; an oversized body must be refused", env.svc.loginCalls)
	}
}

func TestRateLimiterMapIsBounded(t *testing.T) {
	clock := &testClock{}
	l := newRateLimiter(defaultAuthInterval, clock.now)
	for i := range 20 * maxLimiterEntries {
		l.allow(costRequest, fmt.Sprintf("10.0.0.%d|geoah", i))
	}
	if len(l.buckets) > maxLimiterEntries {
		t.Fatalf("limiter holds %d entries, want at most %d", len(l.buckets), maxLimiterEntries)
	}
}

// The two-call registration gesture is ONE attempt: `/register/enroll`
// immediately followed by `/register` is what the console and substratectl both do,
// so the pacing must not 429 the commit half a second after the enroll. The
// SAME caller's SECOND gesture still waits the full interval — the discount is
// inside one gesture, not a doubling of the rate — and that pacing is the
// per-(IP, username) bucket's, not the substrate-wide one (which is a
// distributed-spray cap, not a per-user pacer).
func TestRegistrationGestureIsOneAttempt(t *testing.T) {
	env := newTestEnv(t)
	enroll := func(user string) int {
		return env.do(t, http.MethodPost, registerEnrolPath, "", map[string]any{
			"inviteCode": testInviteCode, "username": user,
		}).Code
	}
	if code := enroll("ada"); code != http.StatusOK {
		t.Fatalf("enroll = %d, want 200", code)
	}
	// No clock advance at all: the commit half follows immediately.
	rec := env.do(t, http.MethodPost, registerPath, "", map[string]any{
		"inviteCode": testInviteCode, "username": "ada",
		"password":   "correct-horse-battery-staple",
		"totpSecret": "SEED", "totpCode": fakeCode("ada"),
	})
	wantStatus(t, rec, http.StatusCreated)

	// The SAME caller's second gesture in the same interval is refused: the
	// (IP, username) bucket spent its whole allowance across the pair's halves,
	// so it is reached (429) before the endpoint ever runs.
	if code := enroll("ada"); code != http.StatusTooManyRequests {
		t.Fatalf("a second gesture from the same caller inside the interval = %d, want 429", code)
	}
}

// The substrate-wide bucket is a DISTRIBUTED-spray cap, not a per-user pacer:
// two honest users logging in within one interval, from two IPs, must BOTH
// reach the service. Sizing that global bucket at a single request's cost used
// to funnel the whole substrate into one attempt per interval — the second
// honest login got a 429 for the first's sake.
func TestGlobalBucketDoesNotThrottleHonestConcurrentUsers(t *testing.T) {
	env := newTestEnv(t)
	env.svc.addRepository("ada")
	env.svc.passwords["ada"] = "correct-horse-battery-staple"

	// geoah and ada, different peers, no clock advance between them.
	wantStatus(t, env.doFrom(t, "10.0.0.1:1", http.MethodPost, loginPath, "",
		loginBody("geoah", env.svc.passwords["geoah"], fakeCode("geoah"))), http.StatusCreated)
	wantStatus(t, env.doFrom(t, "10.0.0.2:1", http.MethodPost, loginPath, "",
		loginBody("ada", env.svc.passwords["ada"], fakeCode("ada"))), http.StatusCreated)
	if env.svc.loginCalls != 2 {
		t.Fatalf("Login called %d times; two honest users in one interval must both reach the service", env.svc.loginCalls)
	}
}

// The global bucket still BOUNDS a distributed spray: a run across many valid
// usernames from many peers is capped substrate-wide, so the cap survives the
// decoupling that let honest concurrency through.
func TestGlobalBucketBoundsADistributedSpray(t *testing.T) {
	env := newTestEnv(t)
	// Each attempt is a distinct valid username from a distinct peer, so only
	// the global bucket can refuse them. globalBurst full-cost attempts drain
	// it; the next is refused.
	attempts := globalAllowance/costRequest + 4
	refused := 0
	for i := range attempts {
		rec := env.doFrom(t, fmt.Sprintf("10.9.%d.%d:1", i/256, i%256), http.MethodPost, loginPath, "",
			loginBody(fmt.Sprintf("user%02d", i), "wrong", "000000"))
		if rec.Code == http.StatusTooManyRequests {
			refused++
		}
	}
	if refused == 0 {
		t.Fatalf("a %d-wide distributed spray was never globally capped", attempts)
	}
}

// The pair's discount does not loosen login: one attempt per interval, still.
func TestLoginKeepsOneAttemptPerInterval(t *testing.T) {
	env := newTestEnv(t)
	good := loginBody("geoah", env.svc.passwords["geoah"], fakeCode("geoah"))
	wantStatus(t, env.do(t, http.MethodPost, loginPath, "", good), http.StatusCreated)
	// Half an interval later — enough for a paired call, not for a request.
	env.clock.advance(defaultAuthInterval / 2)
	rec := env.do(t, http.MethodPost, loginPath, "", good)
	wantErrorCode(t, rec, http.StatusTooManyRequests, codeRateLimited)
	env.clock.advance(defaultAuthInterval/2 + time.Millisecond)
	wantStatus(t, env.do(t, http.MethodPost, loginPath, "", good), http.StatusCreated)
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
	env := newTestEnv(t)

	// A stranger sprays a username that does not exist.
	stranger := loginBody("victim", "wrong-password", "000000")
	for range 20 {
		wantErrorCode(t, env.do(t, http.MethodPost, loginPath, "", stranger),
			http.StatusUnauthorized, codeAuth)
		env.clock.advance(defaultAuthInterval + time.Millisecond)
	}

	// The door is still open to an unrelated, honest user.
	good := loginBody("geoah", env.svc.passwords["geoah"], fakeCode("geoah"))
	wantStatus(t, env.do(t, http.MethodPost, loginPath, "", good), http.StatusCreated)

	// Now geoah fails repeatedly against their own account...
	env.clock.advance(defaultAuthInterval + time.Millisecond)
	ownBad := loginBody("geoah", "wrong-password", "000000")
	for range 20 {
		wantErrorCode(t, env.do(t, http.MethodPost, loginPath, "", ownBad),
			http.StatusUnauthorized, codeAuth)
		env.clock.advance(defaultAuthInterval + time.Millisecond)
	}

	// ...and the right credentials still work on the very next attempt.
	wantStatus(t, env.do(t, http.MethodPost, loginPath, "", good), http.StatusCreated)
}
