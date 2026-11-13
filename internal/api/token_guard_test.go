package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// The door's guards: a per-IP, per-username
// and GLOBAL rate limit and a bounded body —
// all in front of the service, so a refused request never reaches it. What
// the pacing lets through to a real account is token_guard_db_test.go's.

const (
	registerPath      = "/register"
	registerEnrolPath = "/register/enroll"
	loginPath         = "/login"
)

func loginBody(identity, password, code string) map[string]any {
	return map[string]any{"identity": identity, "password": password, "totpCode": code}
}

// guardLoginSvc counts the logins that reach the service and refuses each,
// so a test of the door's pacing sees how many requests the limiter let
// through.
type guardLoginSvc struct {
	*stubService
	logins atomic.Int32
}

func (s *guardLoginSvc) Login(context.Context, substrate.LoginInput) (substrate.MintedToken, error) {
	s.logins.Add(1)
	return substrate.MintedToken{}, substrate.ErrAuth
}

func newGuardEnv(t *testing.T) (*testEnv, *guardLoginSvc) {
	t.Helper()
	svc := &guardLoginSvc{stubService: &stubService{}}
	return newStubEnvOn(t, svc), svc
}

func TestLoginRateLimitIgnoresSpoofedClientIPHeaders(t *testing.T) {
	env, svc := newGuardEnv(t)
	body := loginBody(stubRepository, "wrong", "000000")
	for i := range 30 {
		env.do(t, http.MethodPost, loginPath, "", body,
			"True-Client-IP", fmt.Sprintf("203.0.113.%d", i),
			"X-Real-IP", fmt.Sprintf("198.51.100.%d", i),
			"X-Forwarded-For", fmt.Sprintf("192.0.2.%d", i))
	}
	if n := svc.logins.Load(); n != 1 {
		t.Fatalf("Login called %d times; client-supplied IP headers must not vary the limiter key", n)
	}
}

func TestLoginRateLimitIsAlsoPerUsernameAcrossPeers(t *testing.T) {
	env, svc := newGuardEnv(t)
	body := loginBody(stubRepository, "wrong", "000000")
	for i := range 20 {
		env.doFrom(t, fmt.Sprintf("10.0.%d.%d:4321", i, i), http.MethodPost, loginPath, "", body)
	}
	if n := svc.logins.Load(); n != 1 {
		t.Fatalf("Login called %d times; one username must be limited across peers", n)
	}
}

// The GLOBAL key is what makes a spray across many usernames cost what a run
// at one costs: unknown spellings additionally share a single bucket, so they
// buy neither map growth nor extra attempts.
func TestLoginRateLimitIsGlobal(t *testing.T) {
	env, svc := newGuardEnv(t)
	for i := range 20 {
		env.doFrom(t, fmt.Sprintf("10.1.%d.%d:4321", i, i), http.MethodPost, loginPath, "",
			loginBody(strings.Repeat("x", 64)+fmt.Sprint(i), "wrong", "000000"))
	}
	if n := svc.logins.Load(); n != 1 {
		t.Fatalf("Login called %d times; a spray across usernames must share the global bucket", n)
	}
}

func TestAuthBodyIsBounded(t *testing.T) {
	env, svc := newGuardEnv(t)
	rec := env.do(t, http.MethodPost, loginPath, "", map[string]any{
		"identity": stubRepository, "password": strings.Repeat("a", 2<<20), "totpCode": "000000",
	})
	wantErrorCode(t, rec, http.StatusRequestEntityTooLarge, codeBadRequest)
	if n := svc.logins.Load(); n != 0 {
		t.Fatalf("Login called %d times; an oversized body must be refused", n)
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

// The global bucket still BOUNDS a distributed spray: a run across many valid
// usernames from many peers is capped substrate-wide, so the cap survives the
// decoupling that let honest concurrency through.
func TestGlobalBucketBoundsADistributedSpray(t *testing.T) {
	env, _ := newGuardEnv(t)
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
