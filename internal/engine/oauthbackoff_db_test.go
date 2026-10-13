package engine_test

// The refresh paths' guards against one credential or one endpoint costing
// more than it should: a refusal backs the credential off for the loop and
// for every invocation alike (#913), callers waiting behind a refresh that
// lands take its token instead of calling the provider again, a refresh that
// loses its compare-and-swap says so, and each repository's pass runs under a
// budget.

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
)

// The lines the refresh paths log, as the tests below count them.
const (
	backoffLine  = "oauth refresh refused; no refresh of this credential is sent"
	retryLine    = "oauth refresh failed"
	lostSwapLine = "the refreshed token is not stored"
)

// installBackoffBundle stands up the mail bundle (docs, or the standard
// closure when nil) against a provider at providerURL, its calls riding hc,
// with the service logging into logs when one is given and the OAuth clock
// on the returned TestClock.
func installBackoffBundle(t *testing.T, docs []map[string]any, providerURL string, config map[string]any, hc *http.Client, logs *lockedLog) (substrate.Service, substrate.Dataset, *substrate.Record, *engine.TestClock) {
	t.Helper()
	opts := []engine.Option{
		engine.WithOAuth("test-state-key", "https://substrate.example/api/v1/substrate.reamde.dev/core/oauth/callback", hc),
		engine.WithCredentialKey(engine.TestCredentialKey),
	}
	if logs != nil {
		opts = append(opts, engine.WithLogger(slog.New(slog.NewTextHandler(logs, nil))))
	}
	svc, ds := newDataset(t, opts...)
	clock := &engine.TestClock{}
	engine.SetOAuthClock(svc, clock.Now)
	if docs == nil {
		docs = mbStandardDocs()
	}
	mbPointOAuthAt(docs, providerURL)
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), owner, docs); err != nil {
		t.Fatalf("install bundle: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: mbConfigType, Properties: config})
	account := mustPut(t, ds, owner, substrate.PutInput{
		Kind: mbAccountType, Properties: map[string]any{"address": "geo@example.com", "enabledMail": true},
	})
	return svc, ds, account, clock
}

// echoAccount runs the bundle's echo function and returns the one account
// entry its config carried.
func echoAccount(t *testing.T, ds substrate.Dataset) map[string]any {
	t.Helper()
	out, _, err := ds.CallFunction(context.Background(), substrate.ActorAPI, mbEchoFn, map[string]any{})
	if err != nil {
		t.Errorf("call: %v", err)
		return nil
	}
	cfg, _ := out.(map[string]any)["config"].(map[string]any)
	accounts, _ := cfg["accounts"].([]any)
	if len(accounts) != 1 {
		t.Errorf("accounts: %v", cfg["accounts"])
		return nil
	}
	acc, _ := accounts[0].(map[string]any)
	return acc
}

func (p *fakeProvider) refuseWith(status int, code string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.refuseStatus, p.refuseCode = status, code
}

func (p *fakeProvider) calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.refreshCalls
}

// A refresh refused with `400 invalid_request` (#913's Whoop answer) is not
// sent again until the backoff ends, by the loop or by an invocation, which
// is answered with the refusal instead. The step starts at a minute and
// doubles, one log line per step; a reconnect starts it over, and a refresh
// that lands clears it.
func TestOAuthBackoffSkipsARefusedRefresh(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := newFakeProvider(t)
	logs := &lockedLog{}
	svc, ds, account, clock := installBackoffBundle(t, nil, p.ts.URL, p.configProps(), p.ts.Client(), logs)
	p.expiresIn = 30 // due for the loop and for an invocation alike
	connectOAuthAccount(t, svc, ds, account)
	p.refuseWith(http.StatusBadRequest, "invalid_request")

	pass := func(when string, wantRefreshed, wantCalls int) {
		t.Helper()
		n, err := ds.RefreshOAuthTokens(ctx)
		if err != nil || n != wantRefreshed {
			t.Fatalf("%s: refreshed %d (%v), want %d", when, n, err, wantRefreshed)
		}
		if got := p.calls(); got != wantCalls {
			t.Fatalf("%s: %d provider refresh calls, want %d", when, got, wantCalls)
		}
	}
	invoke := func(when string, wantCalls int) {
		t.Helper()
		acc := echoAccount(t, ds)
		reason, _ := acc["tokenError"].(string)
		if !strings.Contains(reason, `provider answered 400, error code "invalid_request"`) {
			t.Fatalf("%s: account entry %v, want the refusal as its tokenError", when, acc)
		}
		if got := p.calls(); got != wantCalls {
			t.Fatalf("%s: %d provider refresh calls, want %d", when, got, wantCalls)
		}
	}
	wantLines := func(when string, want int) {
		t.Helper()
		out := logs.String()
		if got := strings.Count(out, backoffLine); got != want {
			t.Fatalf("%s: the backoff logged %d times, want %d:\n%s", when, got, want, out)
		}
		if strings.Contains(out, retryLine) {
			t.Fatalf("%s: a refusal that backs off was also logged as a failure to retry:\n%s", when, out)
		}
	}

	pass("the refused pass", 0, 1)
	pass("a pass inside the first step", 0, 1)
	invoke("an invocation inside the first step", 1)
	wantLines("inside the first step", 1)
	if got := mustGet(t, ds, account.Kind, account.ID); got.Properties["tokenStatus"] != "erroring" {
		t.Fatalf("tokenStatus = %v, want erroring", got.Properties["tokenStatus"])
	}

	clock.Advance(time.Minute + time.Second)
	pass("the pass after the first step", 0, 2)
	clock.Advance(time.Minute + time.Second)
	pass("a pass inside the doubled step", 0, 2)
	invoke("an invocation inside the doubled step", 2)
	clock.Advance(time.Minute)
	invoke("the invocation after the doubled step", 3)
	pass("a pass inside the third step", 0, 3)
	wantLines("after three steps", 3)

	// A reconnect writes a new generation, and the backoff starts over: the
	// next pass asks at once, and the one after the first step asks again.
	connectOAuthAccount(t, svc, ds, account)
	pass("the pass after a reconnect", 0, 4)
	clock.Advance(time.Minute + time.Second)
	pass("the first step after a reconnect", 0, 5)

	// The provider recovers: the pass after the step refreshes and the
	// account reads connected again.
	p.refuseWith(0, "")
	clock.Advance(2*time.Minute + time.Second)
	pass("the pass after the provider recovers", 1, 6)
	if got := mustGet(t, ds, account.Kind, account.ID); got.Properties["tokenStatus"] != "connected" {
		t.Fatalf("tokenStatus = %v, want connected", got.Properties["tokenStatus"])
	}
}

// A 429 and a 408 may pass on their own, so they arm no backoff and every
// pass asks again; a 400 on the same credential does back off.
func TestOAuthBackoffLeavesThrottlingToTheNextPass(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := newFakeProvider(t)
	svc, ds, account, _ := installBackoffBundle(t, nil, p.ts.URL, p.configProps(), p.ts.Client(), nil)
	p.expiresIn = 120
	connectOAuthAccount(t, svc, ds, account)
	pass := func(when string, wantCalls int) {
		t.Helper()
		if n, err := ds.RefreshOAuthTokens(ctx); err != nil || n != 0 {
			t.Fatalf("%s: refreshed %d (%v), want 0", when, n, err)
		}
		if got := p.calls(); got != wantCalls {
			t.Fatalf("%s: %d provider refresh calls, want %d", when, got, wantCalls)
		}
	}
	p.refuseWith(http.StatusTooManyRequests, "")
	pass("the first 429", 1)
	pass("the second 429", 2)
	p.refuseWith(http.StatusRequestTimeout, "")
	pass("the first 408", 3)
	pass("the second 408", 4)
	p.refuseWith(http.StatusBadRequest, "invalid_request")
	pass("the first 400", 5)
	pass("the pass after a 400", 5)
}

// Concurrent refreshes of one credential make one provider call when the
// first lands: the loop and two invocations reach an expiring token
// together, the first refresh holds the credential's turn, and the others
// wait and then take the token it stored instead of sending the same refresh
// token again. A provider that rotates refresh tokens would refuse the
// second, or honor it and leave the store holding a spent one. The fresh
// token is short-lived on purpose: five minutes is inside the loop's ten
// minute window, and thirty seconds inside an invocation's one minute, so
// neither caller's own rule would spare it.
func TestOAuthSingleflightRefreshesACredentialOnce(t *testing.T) {
	t.Parallel()
	for _, lifetime := range []int{300, 30} {
		t.Run(fmt.Sprintf("a fresh token of %ds", lifetime), func(t *testing.T) {
			t.Parallel()
			singleflightOnce(t, lifetime)
		})
	}
}

// mbFreshSource reports whether the one account's injected token is the
// refreshed one the barrier provider issues, `at-2`: the token itself never
// leaves the invocation unscrubbed.
const mbFreshSource = `
def main(input, host):
    tokens = [a.get("token") for a in input["config"]["accounts"]]
    return {"effects": [], "output": {"fresh": tokens == ["at-2"]}}
`

func singleflightOnce(t *testing.T, lifetime int) {
	ctx := context.Background()
	p := newBarrierProvider(t)
	docs := mbDocs(nil,
		mbConfigTypeDoc(), mbAccountTypeDoc(), mbItemTypeDoc(), mbMessageTypeDoc(),
		mbFnDoc("fresh", mbFreshSource))
	svc, ds, account, _ := installBackoffBundle(t, docs, p.ts.URL, p.configProps(), p.ts.Client(), nil)
	p.mu.Lock()
	p.expiresIn = 30 // due for the loop and for an invocation alike
	p.mu.Unlock()
	connectOAuthAccount(t, svc, ds, account)
	hold := make(chan struct{})
	release := sync.OnceFunc(func() { close(hold) })
	t.Cleanup(release)
	p.mu.Lock()
	p.expiresIn = lifetime
	p.holdRefresh = hold
	p.arrived = make(chan string, 8)
	p.mu.Unlock()

	invocations := make(chan any, 2)
	invoke := func() {
		out, _, err := ds.CallFunction(ctx, substrate.ActorAPI, mbPackage+"/fresh", map[string]any{})
		if err != nil {
			t.Errorf("call: %v", err)
		}
		invocations <- out
	}
	go invoke()
	select {
	case <-p.arrived:
	case <-time.After(30 * time.Second):
		t.Fatal("the first refresh never reached the provider")
	}
	go invoke()
	type passResult struct {
		n   int
		err error
	}
	loop := make(chan passResult, 1)
	go func() {
		n, err := ds.RefreshOAuthTokens(ctx)
		loop <- passResult{n, err}
	}()
	secondRequest := func(grant string) {
		release()
		t.Fatalf("a second %s request reached the provider while the first refresh of the credential was in flight", grant)
	}
	// The first refresh is released only once the second invocation and the
	// loop both wait for the credential's turn (three hands: one holding, two
	// waiting), so each has read the token the first refresh replaces.
	queued := time.After(30 * time.Second)
	for engine.OAuthRefreshHands(ds) < 3 {
		select {
		case grant := <-p.arrived:
			secondRequest(grant)
		case <-queued:
			release()
			t.Fatalf("the second invocation and the loop never queued for the credential's turn: %d hands", engine.OAuthRefreshHands(ds))
		case <-time.After(10 * time.Millisecond):
		}
	}
	select {
	case grant := <-p.arrived:
		secondRequest(grant)
	case <-time.After(time.Second):
	}
	release()

	for range 2 {
		if out := <-invocations; out == nil || out.(map[string]any)["fresh"] != true {
			t.Fatalf("an invocation was not handed the refreshed token: %v", out)
		}
	}
	if r := <-loop; r.err != nil || r.n != 0 {
		t.Fatalf("the loop pass that waited: refreshed %d (%v), want 0: another hand had just stored a token", r.n, r.err)
	}
	p.mu.Lock()
	refreshes := p.refreshes
	p.mu.Unlock()
	if refreshes != 1 {
		t.Fatalf("%d provider refreshes, want 1", refreshes)
	}
}

// A token an invocation or a reconnect replaced after the loop listed it is
// not refreshed by that pass: the pass lists two credentials, the first
// holds it at the provider, and a reconnect stores an hour-long token for
// the second before the pass reads it.
func TestOAuthRefreshPassSkipsATokenReplacedSinceItsList(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := newBarrierProvider(t)
	svc, ds, first, _ := installBackoffBundle(t, nil, p.ts.URL, p.configProps(), p.ts.Client(), nil)
	second := mustPut(t, ds, owner, substrate.PutInput{
		Kind: mbAccountType, Properties: map[string]any{"address": "second@example.com", "enabledMail": true},
	})
	// Both inside the loop's window, the first listed first.
	p.mu.Lock()
	p.expiresIn = 60
	p.mu.Unlock()
	connectOAuthAccount(t, svc, ds, first)
	p.mu.Lock()
	p.expiresIn = 120
	p.mu.Unlock()
	connectOAuthAccount(t, svc, ds, second)

	hold := make(chan struct{})
	release := sync.OnceFunc(func() { close(hold) })
	t.Cleanup(release)
	p.mu.Lock()
	p.holdRefresh = hold
	p.arrived = make(chan string, 8)
	p.mu.Unlock()
	type passResult struct {
		n   int
		err error
	}
	done := make(chan passResult, 1)
	go func() {
		n, err := ds.RefreshOAuthTokens(ctx)
		done <- passResult{n, err}
	}()
	select {
	case <-p.arrived:
	case <-time.After(30 * time.Second):
		t.Fatal("the first refresh never reached the provider")
	}
	p.mu.Lock()
	p.expiresIn = 3600
	p.mu.Unlock()
	connectOAuthAccount(t, svc, ds, second)
	release()
	if r := <-done; r.err != nil || r.n != 1 {
		t.Fatalf("the pass: refreshed %d (%v), want 1, the first credential alone", r.n, r.err)
	}
	p.mu.Lock()
	refreshes := p.refreshes
	p.mu.Unlock()
	if refreshes != 1 {
		t.Fatalf("%d provider refreshes, want 1: the pass refreshed a token a reconnect had just replaced", refreshes)
	}
}

// A refresh that succeeds at the provider after a reconnect replaced the
// credential stores nothing, and says so: the provider may have spent the
// refresh token it sent.
func TestOAuthRefreshLosingItsSwapWarns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := newBarrierProvider(t)
	logs := &lockedLog{}
	svc, ds, account, _ := installBackoffBundle(t, nil, p.ts.URL, p.configProps(), p.ts.Client(), logs)
	p.mu.Lock()
	p.expiresIn = 120 // inside the 10m refresh window
	p.mu.Unlock()
	connectOAuthAccount(t, svc, ds, account)
	hold := make(chan struct{})
	release := sync.OnceFunc(func() { close(hold) })
	t.Cleanup(release)
	p.mu.Lock()
	p.holdRefresh = hold
	p.arrived = make(chan string, 8)
	p.mu.Unlock()

	type passResult struct {
		n   int
		err error
	}
	done := make(chan passResult, 1)
	go func() {
		n, err := ds.RefreshOAuthTokens(ctx)
		done <- passResult{n, err}
	}()
	select {
	case <-p.arrived:
	case <-time.After(30 * time.Second):
		t.Fatal("the refresh never reached the provider")
	}
	// The refresh is blocked at the provider; a reconnect lands meanwhile.
	connectOAuthAccount(t, svc, ds, account)
	release()
	r := <-done
	if r.err != nil || r.n != 0 {
		t.Fatalf("the pass that lost its swap: refreshed %d (%v), want 0", r.n, r.err)
	}
	if out := logs.String(); !strings.Contains(out, lostSwapLine) {
		t.Fatalf("a refresh that lost its compare-and-swap logged nothing:\n%s", out)
	}
}

// deadlineTransport records, per request path, how long the request's
// context had left when it was sent; -1 is no deadline.
type deadlineTransport struct {
	base http.RoundTripper
	mu   sync.Mutex
	left map[string]time.Duration
}

func (d *deadlineTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	left := time.Duration(-1)
	if dl, ok := r.Context().Deadline(); ok {
		left = time.Until(dl)
	}
	d.mu.Lock()
	d.left[r.URL.Path] = left
	d.mu.Unlock()
	return d.base.RoundTrip(r)
}

func (d *deadlineTransport) leftOn(path string) (time.Duration, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	left, ok := d.left[path]
	return left, ok
}

// The refresh pass and the finalizer pass each run one repository under a
// budget of their own, whatever context the caller passed: the process
// walks every repository in turn, and an endpoint answering slowly must not
// hold the rest of the walk.
func TestOAuthRefreshAndFinalizerPassesRunUnderABudget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := newFakeProvider(t)
	tr := &deadlineTransport{base: p.ts.Client().Transport, left: map[string]time.Duration{}}
	svc, ds, account, _ := installBackoffBundle(t, nil, p.ts.URL, p.configProps(), &http.Client{Transport: tr}, nil)
	p.expiresIn = 120 // inside the 10m refresh window
	connectOAuthAccount(t, svc, ds, account)
	wantBudget := func(what, path string) {
		t.Helper()
		left, ok := tr.leftOn(path)
		if !ok {
			t.Fatalf("%s sent nothing to %s", what, path)
		}
		if left <= 0 || left > 2*time.Minute {
			t.Fatalf("%s reached %s with %v left on its context, want a deadline inside two minutes", what, path, left)
		}
	}

	if n, err := ds.RefreshOAuthTokens(ctx); err != nil || n != 1 {
		t.Fatalf("refresh: %d %v", n, err)
	}
	wantBudget("the refresh pass", "/token")

	if _, err := ds.Delete(ctx, owner, account.Kind, account.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete account: %v", err)
	}
	if released, err := ds.ProcessOAuthFinalizers(ctx); err != nil || released != 1 {
		t.Fatalf("finalizers: %d %v", released, err)
	}
	wantBudget("the finalizer pass", "/revoke")
}
