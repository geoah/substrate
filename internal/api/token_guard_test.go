package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The door's guards: a per-IP, per-username
// and GLOBAL rate limit, a consecutive-failure lockout, and a bounded body —
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

// Failures compound: past the threshold the caller is locked out for longer
// than the interval, and a SUCCESS clears the run.
func TestLoginLockoutAfterConsecutiveFailures(t *testing.T) {
	env := newTestEnv(t)
	bad := loginBody("geoah", "wrong-password", "000000")
	for range lockoutThreshold {
		rec := env.do(t, http.MethodPost, loginPath, "", bad)
		wantErrorCode(t, rec, http.StatusUnauthorized, codeAuth)
		env.clock.advance(defaultAuthInterval + time.Millisecond)
	}
	// Spacing no longer buys an attempt: the run itself is the refusal.
	rec := env.do(t, http.MethodPost, loginPath, "", bad)
	wantErrorCode(t, rec, http.StatusTooManyRequests, codeRateLimited)
	retry, err := strconv.Atoi(rec.Header().Get("Retry-After"))
	if err != nil || retry < 1 {
		t.Fatalf("Retry-After = %q, want a positive integer", rec.Header().Get("Retry-After"))
	}
	calls := env.svc.loginCalls

	// Past the lockout a good credential is admitted, and it clears the run.
	env.clock.advance(lockoutBase + time.Minute)
	good := loginBody("geoah", env.svc.passwords["geoah"], fakeCode("geoah"))
	wantStatus(t, env.do(t, http.MethodPost, loginPath, "", good), http.StatusCreated)
	if env.svc.loginCalls != calls+1 {
		t.Fatalf("the locked-out request reached the service")
	}
	env.clock.advance(defaultAuthInterval + time.Millisecond)
	wantStatus(t, env.do(t, http.MethodPost, loginPath, "", good), http.StatusCreated)
}

// A lockout is one caller's or one username's, NEVER the substrate's. The
// global key is the rate bucket's alone: feeding it to the lockout let five
// unauthenticated failures against a username nobody has — from IPs nobody
// shares — take login, registration and both credential changes offline for
// every user, doubling to the hour cap on every further request and clearing
// for nobody, since only a request that gets PAST the lockout can clear it.
func TestOneUsernamesFailuresDoNotLockOutEverybodyElse(t *testing.T) {
	env := newTestEnv(t)
	bad := loginBody("nobody-by-that-name", "wrong-password", "000000")
	for i := range lockoutThreshold {
		env.doFrom(t, fmt.Sprintf("203.0.113.%d", i), http.MethodPost, loginPath, "", bad)
		env.clock.advance(defaultAuthInterval + time.Millisecond)
	}
	good := loginBody("geoah", env.svc.passwords["geoah"], fakeCode("geoah"))
	rec := env.doFrom(t, "198.51.100.7", http.MethodPost, loginPath, "", good)
	wantStatus(t, rec, http.StatusCreated)
}

// /register/enroll proves the shared invite code and authenticates NOBODY, so
// it must not clear a username's login lockout. Treating it as a success let an
// invite-code holder POST /register/enroll{username: victim} to reset the
// victim's consecutive-failure run and keep the exponential lockout pinned at
// its floor — many more login guesses per hour. The run must be UNCHANGED by an
// enroll: after one, a further failure still escalates rather than starting
// over. (The lockout is keyed by the username string, present or not, so the
// mechanism is the same whichever the victim.)
func TestRegisterEnrollDoesNotResetTheLoginLockout(t *testing.T) {
	env := newTestEnv(t)
	const victim = "victim"
	bad := loginBody(victim, "wrong-password", "000000")

	// Run the consecutive failures up to the threshold: the last one locks.
	for range lockoutThreshold {
		env.do(t, http.MethodPost, loginPath, "", bad)
		env.clock.advance(defaultAuthInterval + time.Millisecond)
	}
	// Step past the lockout window: unlocked by time, but the run still stands.
	env.clock.advance(lockoutBase + time.Millisecond)

	// The attacker holds the invite code and tries to wipe the run via enroll.
	wantStatus(t, env.do(t, http.MethodPost, registerEnrolPath, "", map[string]any{
		"inviteCode": testInviteCode, "username": victim,
	}), http.StatusOK)

	// One more failed login. With the run intact this is the SIXTH failure, so
	// it re-locks (a longer window); had the enroll reset it, this would be the
	// first failure and lock nothing.
	env.clock.advance(defaultAuthInterval + time.Millisecond)
	env.do(t, http.MethodPost, loginPath, "", bad)

	// The rate buckets are full (only 5s passed) yet the next attempt is
	// refused: the refusal is the LOCKOUT, which proves the run survived enroll.
	env.clock.advance(defaultAuthInterval + time.Millisecond)
	wantErrorCode(t, env.do(t, http.MethodPost, loginPath, "", bad),
		http.StatusTooManyRequests, codeRateLimited)
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

func TestLockoutMapIsBounded(t *testing.T) {
	clock := &testClock{t: time.Unix(1_700_000_000, 0).UTC()}
	l := newLockout(clock.now)
	for i := range 20 * maxLimiterEntries {
		l.fail(fmt.Sprintf("10.0.0.%d|geoah", i))
	}
	if len(l.entries) > maxLimiterEntries {
		t.Fatalf("lockout holds %d entries, want at most %d", len(l.entries), maxLimiterEntries)
	}
}

func TestLockoutBackoffIsExponentialAndCapped(t *testing.T) {
	clock := &testClock{t: time.Unix(1_700_000_000, 0).UTC()}
	l := newLockout(clock.now)
	for range lockoutThreshold - 1 {
		l.fail("k")
	}
	if locked, _ := l.locked("k"); locked {
		t.Fatal("locked before the threshold")
	}
	l.fail("k")
	locked, wait := l.locked("k")
	if !locked || wait != lockoutBase {
		t.Fatalf("first lockout = %v (locked %v), want %v", wait, locked, lockoutBase)
	}
	l.fail("k")
	if _, wait := l.locked("k"); wait != 2*lockoutBase {
		t.Fatalf("second lockout = %v, want %v", wait, 2*lockoutBase)
	}
	for range 20 {
		l.fail("k")
	}
	if _, wait := l.locked("k"); wait != lockoutCap {
		t.Fatalf("runaway lockout = %v, want the %v cap", wait, lockoutCap)
	}
	l.succeed("k")
	if locked, _ := l.locked("k"); locked {
		t.Fatal("a success must clear the run")
	}
}
