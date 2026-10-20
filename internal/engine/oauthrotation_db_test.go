package engine_test

// A refresh against a provider that rotates refresh tokens (WHOOP) and the
// refusals that follow when a rotated token is lost (#913, 0180): the call
// and the store outlive the caller, a store that fails keeps the token in
// memory until it lands, a refusal of a replaced grant records nothing, and
// a refusal that lasts an hour parks the credential and asks for a
// reconnect while a failure that may pass never does.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The lines the refresh paths log, as the tests below count them.
const (
	unstoredLine       = "is kept in memory and stands in for the stored one until a store lands"
	parkedRepeatedLine = "oauth refresh refused repeatedly; the refresh loop skips this credential"
	replacedLine       = "oauth refresh refused, but a reconnect replaced the credential first"
	keptDroppedLine    = "a rotated token that was never stored was replaced by a reconnect; it is dropped"
)

// mbTokenSource hands back which access token its one account resolved to:
// the n of at-n, since the output redacts the token itself.
const mbTokenSource = `
def main(input, host):
    tok = input["config"]["accounts"][0].get("token") or ""
    return {"effects": [], "output": {"issued": tok.removeprefix("at-")}}
`

// withTokenErrorProps declares the account kind's tokenError and
// tokenErrorAt, the slots a refresh failure's reason lands in.
func withTokenErrorProps(docs []map[string]any) []map[string]any {
	for _, d := range docs {
		if meta, _ := d["metadata"].(map[string]any); d["kind"] == vocabulary.CoreKind(vocabulary.DocKind) && meta["id"] == mbAccountType {
			props, _ := d["data"].(map[string]any)["properties"].(map[string]any)
			props["tokenError"] = map[string]any{"type": "string", "writer": "oauth"}
			props["tokenErrorAt"] = map[string]any{"type": "datetime", "writer": "oauth"}
		}
	}
	return docs
}

func (p *fakeProvider) sentTokens() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.sent...)
}

func (p *fakeProvider) refreshCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.refreshes
}

func wantTokenStatus(t *testing.T, ds substrate.Dataset, account *substrate.Record, want string) *substrate.Record {
	t.Helper()
	got := mustGet(t, ds, account.Kind, account.ID)
	if got.Properties["tokenStatus"] != want {
		t.Fatalf("tokenStatus = %v, want %s", got.Properties["tokenStatus"], want)
	}
	return got
}

// A refresh whose caller ends while the provider is answering still stores
// the token the provider rotated: the next pass refreshes with it, and the
// provider, which spent the old refresh token, answers.
func TestOAuthRefreshStoresARotatedTokenAfterItsCallerEnds(t *testing.T) {
	t.Parallel()
	p := newBarrierProvider(t)
	svc, ds, account, _ := installBackoffBundle(t, nil, p.ts.URL, p.configProps(), p.ts.Client(), nil)
	p.mu.Lock()
	p.expiresIn = 120 // inside the 10m refresh window
	p.rotate = true
	p.mu.Unlock()
	connectOAuthAccount(t, svc, ds, account)
	hold := make(chan struct{})
	release := sync.OnceFunc(func() { close(hold) })
	t.Cleanup(release)
	p.mu.Lock()
	p.holdRefresh = hold
	p.arrived = make(chan string, 8)
	p.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := ds.RefreshOAuthTokens(ctx)
		done <- err
	}()
	select {
	case <-p.arrived:
	case <-time.After(30 * time.Second):
		t.Fatal("the refresh never reached the provider")
	}
	// The caller ends while the provider holds the answer, as a shutdown or
	// a canceled invocation would.
	cancel()
	release()
	if err := <-done; err != nil && !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("the canceled pass: %v", err)
	}
	p.mu.Lock()
	p.holdRefresh = nil
	p.mu.Unlock()

	n, err := ds.RefreshOAuthTokens(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("the pass after the canceled one: refreshed %d (%v), want 1", n, err)
	}
	p.mu.Lock()
	refreshes, current := p.refreshes, p.current
	p.mu.Unlock()
	if refreshes != 2 || current != "rt-3" {
		t.Fatalf("%d provider refreshes, current refresh token %s; want 2 and rt-3: the rotated rt-2 was not stored", refreshes, current)
	}
	wantTokenStatus(t, ds, account, "connected")
}

// A refreshed token whose store fails is kept in memory: the invocation that
// refreshed it gets its access token, no later call asks the provider while
// it is held, and the first call after the store recovers stores it. The
// refresh after that sends the rotated refresh token and is answered.
func TestOAuthRefreshKeepsAnUnstoredRotatedTokenUntilItLands(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := newFakeProvider(t)
	logs := &lockedLog{}
	docs := mbDocs(nil,
		mbConfigTypeDoc(), mbAccountTypeDoc(), mbItemTypeDoc(), mbMessageTypeDoc(),
		mbFnDoc("fresh", mbFreshSource))
	svc, ds, account, _ := installBackoffBundle(t, docs, p.ts.URL, p.configProps(), p.ts.Client(), logs)
	p.expiresIn = 30 // due for an invocation
	p.rotate = true
	connectOAuthAccount(t, svc, ds, account)
	p.mu.Lock()
	p.expiresIn = 600 // the fresh token is not due for an invocation
	p.mu.Unlock()
	fresh := func(when string) {
		t.Helper()
		out, _, err := ds.CallFunction(ctx, substrate.ActorAPI, mbPackage+"/fresh", map[string]any{})
		if err != nil {
			t.Fatalf("%s: call: %v", when, err)
		}
		if out.(map[string]any)["fresh"] != true {
			t.Fatalf("%s: the invocation was not handed the refreshed token: %v", when, out)
		}
	}
	wantKept := func(when string, lines int) {
		t.Helper()
		if got := strings.Count(logs.String(), unstoredLine); got != lines {
			t.Fatalf("%s: the kept token was logged %d times, want %d:\n%s", when, got, lines, logs.String())
		}
		if got := engine.OAuthUnstoredTokens(ds); got != 1 {
			t.Fatalf("%s: %d tokens kept in memory, want 1", when, got)
		}
		if got := p.refreshCount(); got != 1 {
			t.Fatalf("%s: %d provider refreshes, want 1", when, got)
		}
	}

	engine.BreakSealedStore(ds)
	fresh("the invocation whose store fails")
	wantKept("after the failed store", 1)

	if n, err := ds.RefreshOAuthTokens(ctx); err != nil || n != 0 {
		t.Fatalf("a pass with the store still broken: refreshed %d (%v), want 0", n, err)
	}
	wantKept("after a pass with the store still broken", 2)
	fresh("an invocation with the store still broken")
	wantKept("after an invocation with the store still broken", 3)

	engine.RepairSealedStore(ds)
	if n, err := ds.RefreshOAuthTokens(ctx); err != nil || n != 1 {
		t.Fatalf("the pass after the repair: refreshed %d (%v), want 1", n, err)
	}
	if got := engine.OAuthUnstoredTokens(ds); got != 0 {
		t.Fatalf("%d tokens kept in memory after the store recovered, want 0", got)
	}
	if got := p.refreshCount(); got != 1 {
		t.Fatalf("storing the kept token asked the provider: %d refreshes, want 1", got)
	}
	wantTokenStatus(t, ds, account, "connected")

	// The stored token expires inside the loop's window, so the next pass
	// refreshes it, with rt-2: the provider refuses any other.
	if n, err := ds.RefreshOAuthTokens(ctx); err != nil || n != 1 {
		t.Fatalf("the pass after the kept token landed: refreshed %d (%v), want 1", n, err)
	}
	if got, want := strings.Join(p.sentTokens(), ","), "rt-1,rt-2"; got != want {
		t.Fatalf("refresh tokens sent: %s, want %s", got, want)
	}
	wantTokenStatus(t, ds, account, "connected")
}

// A store that fails once is tried again, and the token lands without being
// kept in memory.
func TestOAuthRefreshStoreRetriesBeforeKeepingTheToken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := newFakeProvider(t)
	logs := &lockedLog{}
	svc, ds, account, _ := installBackoffBundle(t, nil, p.ts.URL, p.configProps(), p.ts.Client(), logs)
	p.expiresIn = 120
	p.rotate = true
	connectOAuthAccount(t, svc, ds, account)
	engine.BreakSealedStoreTimes(ds, 1)
	if n, err := ds.RefreshOAuthTokens(ctx); err != nil || n != 1 {
		t.Fatalf("the pass: refreshed %d (%v), want 1", n, err)
	}
	if strings.Contains(logs.String(), unstoredLine) {
		t.Fatalf("a store that landed on its second attempt kept the token in memory:\n%s", logs.String())
	}
	if got := engine.OAuthUnstoredTokens(ds); got != 0 {
		t.Fatalf("%d tokens kept in memory, want 0", got)
	}
	if n, err := ds.RefreshOAuthTokens(ctx); err != nil || n != 1 {
		t.Fatalf("the next pass: refreshed %d (%v), want 1: rt-2 was not stored", n, err)
	}
}

// A refusal of a grant a reconnect replaced while the refusal was in flight
// records nothing: the account stays connected, no backoff is armed for the
// new grant, and the next pass refreshes it.
func TestOAuthRefusalOfAReplacedCredentialStampsNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := newBarrierProvider(t)
	logs := &lockedLog{}
	svc, ds, account, _ := installBackoffBundle(t, withTokenErrorProps(mbStandardDocs()), p.ts.URL, p.configProps(), p.ts.Client(), logs)
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
	connectOAuthAccount(t, svc, ds, account)
	p.mu.Lock()
	p.refuseHeld = true
	p.mu.Unlock()
	release()
	if r := <-done; r.err != nil || r.n != 0 {
		t.Fatalf("the refused pass: refreshed %d (%v), want 0", r.n, r.err)
	}
	got := wantTokenStatus(t, ds, account, "connected")
	for _, name := range []string{"tokenError", "tokenErrorAt"} {
		if v, ok := got.Properties[name]; ok {
			t.Fatalf("%s = %v after a refusal of the replaced grant, want none", name, v)
		}
	}
	out := logs.String()
	if !strings.Contains(out, replacedLine) || strings.Contains(out, backoffLine) {
		t.Fatalf("the refusal of a replaced grant was logged as a backoff, or not at all:\n%s", out)
	}

	p.mu.Lock()
	p.refuseHeld, p.holdRefresh = false, nil
	p.mu.Unlock()
	if n, err := ds.RefreshOAuthTokens(ctx); err != nil || n != 1 {
		t.Fatalf("the pass after the reconnect: refreshed %d (%v), want 1: the new grant was backed off", n, err)
	}
	p.mu.Lock()
	refreshes := p.refreshes
	p.mu.Unlock()
	if refreshes != 2 {
		t.Fatalf("%d provider refreshes, want 2", refreshes)
	}
}

// refusalSince lifts the instant a repeated-refusal reason names.
func refusalSince(t *testing.T, reason string) time.Time {
	t.Helper()
	_, rest, ok := strings.Cut(reason, " since ")
	if !ok {
		t.Fatalf("tokenError %q names no first refusal", reason)
	}
	at, _, _ := strings.Cut(rest, ",")
	since, err := time.Parse(time.RFC3339, at)
	if err != nil {
		t.Fatalf("tokenError %q: the first refusal %q is not RFC 3339: %v", reason, at, err)
	}
	return since
}

// wantParkedReason checks the account carries the repeated-refusal reason
// for refusals refusals since first.
func wantParkedReason(t *testing.T, ds substrate.Dataset, account *substrate.Record, refusals int, first time.Time) {
	t.Helper()
	got := wantTokenStatus(t, ds, account, "erroring")
	reason, _ := got.Properties["tokenError"].(string)
	for _, want := range []string{fmt.Sprintf("refused %d times since ", refusals), "invalid_request", "reconnect"} {
		if !strings.Contains(reason, want) {
			t.Fatalf("tokenError = %q, want it to hold %q", reason, want)
		}
	}
	if since := refusalSince(t, reason); since.Before(first.Add(-2*time.Second)) || since.After(first.Add(2*time.Second)) {
		t.Fatalf("tokenError names the first refusal at %v, want about %v", since, first)
	}
}

// A refusal other than `invalid_grant` that lasts until the backoff step
// reaches an hour (the seventh) parks the credential like `invalid_grant`:
// the account says how many times since when and asks for a reconnect, the
// loop stops asking, and a reconnect lists it again.
func TestOAuthRepeatedRefusalsParkTheCredential(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := newFakeProvider(t)
	logs := &lockedLog{}
	svc, ds, account, clock := installBackoffBundle(t, withTokenErrorProps(mbStandardDocs()), p.ts.URL, p.configProps(), p.ts.Client(), logs)
	p.expiresIn = 120 // inside the 10m refresh window
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

	first := clock.Now()
	step := time.Minute
	for i := 1; i <= 6; i++ {
		pass("a refusal before the park", 0, i)
		if strings.Contains(logs.String(), parkedRepeatedLine) {
			t.Fatalf("refusal %d parked the credential:\n%s", i, logs.String())
		}
		clock.Advance(step + time.Second)
		step *= 2
	}
	pass("the seventh refusal", 0, 7)
	wantParkedReason(t, ds, account, 7, first)
	if got := strings.Count(logs.String(), parkedRepeatedLine); got != 1 {
		t.Fatalf("the park logged %d times, want 1:\n%s", got, logs.String())
	}

	clock.Advance(65 * time.Minute)
	pass("a pass after the park", 0, 7)
	pass("another pass after the park", 0, 7)

	p.refuseWith(0, "")
	connectOAuthAccount(t, svc, ds, account)
	pass("the pass after a reconnect", 1, 8)
	got := wantTokenStatus(t, ds, account, "connected")
	if v, ok := got.Properties["tokenError"]; ok {
		t.Fatalf("tokenError = %v after a reconnect, want it cleared", v)
	}
}

// The seventh refusal may come from an invocation, which does not park: the
// next pass finds the credential backed off by an hour and parks it without
// asking the provider.
func TestOAuthAnInvocationsSeventhRefusalParksAtTheNextPass(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := newFakeProvider(t)
	logs := &lockedLog{}
	svc, ds, account, clock := installBackoffBundle(t, withTokenErrorProps(mbStandardDocs()), p.ts.URL, p.configProps(), p.ts.Client(), logs)
	p.expiresIn = 30 // due for the loop and for an invocation alike
	connectOAuthAccount(t, svc, ds, account)
	p.refuseWith(http.StatusBadRequest, "invalid_request")

	first := clock.Now()
	step := time.Minute
	for i := 1; i <= 6; i++ {
		if n, err := ds.RefreshOAuthTokens(ctx); err != nil || n != 0 {
			t.Fatalf("refusal %d: refreshed %d (%v), want 0", i, n, err)
		}
		clock.Advance(step + time.Second)
		step *= 2
	}
	if acc := echoAccount(t, ds); acc == nil || acc["tokenError"] == nil {
		t.Fatalf("the invocation's refusal: account entry %v, want a tokenError", acc)
	}
	if got := p.calls(); got != 7 {
		t.Fatalf("%d provider refresh calls, want 7", got)
	}
	if strings.Contains(logs.String(), parkedRepeatedLine) {
		t.Fatalf("an invocation parked the credential:\n%s", logs.String())
	}
	if n, err := ds.RefreshOAuthTokens(ctx); err != nil || n != 0 {
		t.Fatalf("the pass after the invocation: refreshed %d (%v), want 0", n, err)
	}
	if got := p.calls(); got != 7 {
		t.Fatalf("the parking pass asked the provider: %d calls, want 7", got)
	}
	wantParkedReason(t, ds, account, 7, first)

	// Once the backoff ends an invocation asks the provider again and is
	// refused: the account keeps the count, the first refusal and the ask
	// instead of the bare answer.
	clock.Advance(65 * time.Minute)
	if acc := echoAccount(t, ds); acc == nil || acc["tokenError"] == nil {
		t.Fatalf("the invocation after the park: account entry %v, want a tokenError", acc)
	}
	if got := p.calls(); got != 8 {
		t.Fatalf("the invocation after the park: %d provider refresh calls, want 8", got)
	}
	wantParkedReason(t, ds, account, 8, first)
}

// A 503 and a provider that cannot be reached may pass on their own: they
// arm no backoff, every pass asks again, and nothing parks however long they
// last.
func TestOAuthTransientFailuresNeverPark(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := newFakeProvider(t)
	logs := &lockedLog{}
	svc, ds, account, clock := installBackoffBundle(t, nil, p.ts.URL, p.configProps(), p.ts.Client(), logs)
	p.expiresIn = 120
	connectOAuthAccount(t, svc, ds, account)
	p.mu.Lock()
	p.unavailable = true
	p.mu.Unlock()
	step := time.Minute
	for i := 1; i <= 8; i++ {
		if n, err := ds.RefreshOAuthTokens(ctx); err != nil || n != 0 {
			t.Fatalf("503 pass %d: refreshed %d (%v), want 0", i, n, err)
		}
		if got := p.calls(); got != i {
			t.Fatalf("503 pass %d: %d provider refresh calls, want %d", i, got, i)
		}
		clock.Advance(step + time.Second)
		step *= 2
	}
	// The provider cannot be reached at all: a transport error.
	p.ts.Close()
	for i := 1; i <= 8; i++ {
		if n, err := ds.RefreshOAuthTokens(ctx); err != nil || n != 0 {
			t.Fatalf("unreachable pass %d: refreshed %d (%v), want 0", i, n, err)
		}
		clock.Advance(step + time.Second)
	}
	out := logs.String()
	if got := strings.Count(out, retryLine); got != 16 {
		t.Fatalf("%d passes logged a failure to retry, want 16: a pass skipped the credential:\n%s", got, out)
	}
	if strings.Contains(out, parkedRepeatedLine) || strings.Contains(out, backoffLine) {
		t.Fatalf("a failure that may pass backed off or parked the credential:\n%s", out)
	}
	wantTokenStatus(t, ds, account, "erroring")
}

// A process that lost its writer lease sends no refresh: the provider would
// spend the refresh token and the store of the fresh one would be refused,
// the way the owner's WHOOP grant was lost (#913). Once the lease is back
// the refresh sends the stored refresh token and lands.
func TestOAuthRefreshSendsNothingWhileTheWriterLeaseIsLost(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := newFakeProvider(t)
	svc, ds, account, _ := installBackoffBundle(t, nil, p.ts.URL, p.configProps(), p.ts.Client(), nil)
	p.expiresIn = 30 // due for the loop and for an invocation alike
	p.rotate = true
	connectOAuthAccount(t, svc, ds, account)
	p.mu.Lock()
	p.expiresIn = 3600
	p.mu.Unlock()

	restore := engine.LoseWriterLease(svc)
	t.Cleanup(restore)
	if n, err := ds.RefreshOAuthTokens(ctx); !errors.Is(err, engine.ErrWriterLeaseLost) || n != 0 {
		t.Fatalf("a pass without the lease: refreshed %d (%v), want 0 and the lease refusal", n, err)
	}
	// An invocation resolving the due token does not ask either, whether it
	// is refused before or at the token.
	_, _, _ = ds.CallFunction(ctx, substrate.ActorAPI, mbEchoFn, map[string]any{})
	if got := p.calls(); got != 0 {
		t.Fatalf("%d provider refresh calls without the lease, want 0", got)
	}
	restore()

	if n, err := ds.RefreshOAuthTokens(ctx); err != nil || n != 1 {
		t.Fatalf("the pass after the lease came back: refreshed %d (%v), want 1", n, err)
	}
	if got, want := strings.Join(p.sentTokens(), ","), "rt-1"; got != want {
		t.Fatalf("refresh tokens sent: %s, want %s", got, want)
	}
	if got := engine.OAuthUnstoredTokens(ds); got != 0 {
		t.Fatalf("%d tokens kept in memory, want 0", got)
	}
	wantTokenStatus(t, ds, account, "connected")
}

// A provider call that runs out its bound is a failure the account shows:
// the store's own bound starts after the call, so the stamp still has the
// time to write `erroring`.
func TestOAuthRefreshThatRunsOutItsCallBoundStampsErroring(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := newBarrierProvider(t)
	logs := &lockedLog{}
	svc, ds, account, _ := installBackoffBundle(t, withTokenErrorProps(mbStandardDocs()), p.ts.URL, p.configProps(), p.ts.Client(), logs)
	p.mu.Lock()
	p.expiresIn = 120 // inside the 10m refresh window
	p.mu.Unlock()
	connectOAuthAccount(t, svc, ds, account)
	hold := make(chan struct{})
	t.Cleanup(sync.OnceFunc(func() { close(hold) }))
	p.mu.Lock()
	p.holdRefresh = hold
	p.mu.Unlock()
	engine.SetOAuthCallBudget(svc, 300*time.Millisecond)

	if n, err := ds.RefreshOAuthTokens(ctx); err != nil || n != 0 {
		t.Fatalf("the pass whose call ran out its bound: refreshed %d (%v), want 0", n, err)
	}
	got := wantTokenStatus(t, ds, account, "erroring")
	if reason, _ := got.Properties["tokenError"].(string); reason == "" {
		t.Fatalf("tokenError is empty after a call that ran out its bound")
	}
	if !strings.Contains(logs.String(), retryLine) {
		t.Fatalf("the failure was not logged as one to retry:\n%s", logs.String())
	}
}

// The stamp checks the credential's generation inside its own transaction: a
// reconnect that lands after the refused refresh found its credential
// unchanged, and before the stamp, keeps its `connected`.
func TestOAuthRefusalStampChecksTheGenerationUnderTheLock(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := newFakeProvider(t)
	svc, ds, account, _ := installBackoffBundle(t, withTokenErrorProps(mbStandardDocs()), p.ts.URL, p.configProps(), p.ts.Client(), nil)
	p.expiresIn = 120 // inside the 10m refresh window
	connectOAuthAccount(t, svc, ds, account)
	p.refuseWith(http.StatusBadRequest, "invalid_request")
	reconnected := false
	engine.SetOAuthBeforeStamp(svc, func() {
		if !reconnected {
			reconnected = true
			connectOAuthAccount(t, svc, ds, account)
		}
	})

	if n, err := ds.RefreshOAuthTokens(ctx); err != nil || n != 0 {
		t.Fatalf("the refused pass: refreshed %d (%v), want 0", n, err)
	}
	if !reconnected {
		t.Fatal("the refusal never reached the stamp")
	}
	got := wantTokenStatus(t, ds, account, "connected")
	for _, name := range []string{"tokenError", "tokenErrorAt"} {
		if v, ok := got.Properties[name]; ok {
			t.Fatalf("%s = %v after a refusal of the replaced grant, want none", name, v)
		}
	}

	engine.SetOAuthBeforeStamp(svc, nil)
	p.refuseWith(0, "")
	if n, err := ds.RefreshOAuthTokens(ctx); err != nil || n != 1 {
		t.Fatalf("the pass after the reconnect: refreshed %d (%v), want 1: the new grant was backed off", n, err)
	}
}

// A token kept in memory whose credential a reconnect replaced is dropped,
// not stored over the new grant, and the refresh after it sends the
// reconnect's refresh token.
func TestOAuthReconnectDropsAKeptToken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := newFakeProvider(t)
	logs := &lockedLog{}
	docs := mbDocs(nil,
		mbConfigTypeDoc(), mbAccountTypeDoc(), mbItemTypeDoc(), mbMessageTypeDoc(),
		mbFnDoc("fresh", mbFreshSource))
	svc, ds, account, _ := installBackoffBundle(t, docs, p.ts.URL, p.configProps(), p.ts.Client(), logs)
	p.expiresIn = 30 // due for an invocation
	p.rotate = true
	connectOAuthAccount(t, svc, ds, account)

	engine.BreakSealedStore(ds)
	if _, _, err := ds.CallFunction(ctx, substrate.ActorAPI, mbPackage+"/fresh", map[string]any{}); err != nil {
		t.Fatalf("the invocation whose store fails: %v", err)
	}
	if got := engine.OAuthUnstoredTokens(ds); got != 1 {
		t.Fatalf("%d tokens kept in memory, want 1", got)
	}
	engine.RepairSealedStore(ds)

	p.mu.Lock()
	p.expiresIn = 120 // the reconnect's token is inside the loop's window
	p.mu.Unlock()
	connectOAuthAccount(t, svc, ds, account)
	if n, err := ds.RefreshOAuthTokens(ctx); err != nil || n != 1 {
		t.Fatalf("the pass after the reconnect: refreshed %d (%v), want 1", n, err)
	}
	if !strings.Contains(logs.String(), keptDroppedLine) {
		t.Fatalf("the kept token was not dropped with a warning:\n%s", logs.String())
	}
	if got := engine.OAuthUnstoredTokens(ds); got != 0 {
		t.Fatalf("%d tokens kept in memory after the reconnect, want 0", got)
	}
	// rt-1 of the first grant, then rt-1 of the reconnect's: the kept rt-2
	// was never sent or stored.
	if got, want := strings.Join(p.sentTokens(), ","), "rt-1,rt-1"; got != want {
		t.Fatalf("refresh tokens sent: %s, want %s", got, want)
	}
	wantTokenStatus(t, ds, account, "connected")
}

// A kept token that is itself due while the store still fails is refreshed
// with its own refresh token, and the token that refresh returns is kept in
// its place; once the store recovers, it lands and the next refresh sends it.
func TestOAuthAKeptTokenThatIsDueRefreshesWithItsOwnRefreshToken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := newFakeProvider(t)
	logs := &lockedLog{}
	docs := mbDocs(nil,
		mbConfigTypeDoc(), mbAccountTypeDoc(), mbItemTypeDoc(), mbMessageTypeDoc(),
		mbFnDoc("token", mbTokenSource))
	svc, ds, account, _ := installBackoffBundle(t, docs, p.ts.URL, p.configProps(), p.ts.Client(), logs)
	p.expiresIn = 30 // every token is due for an invocation
	p.rotate = true
	connectOAuthAccount(t, svc, ds, account)
	token := func(when, want string) {
		t.Helper()
		out, _, err := ds.CallFunction(ctx, substrate.ActorAPI, mbPackage+"/token", map[string]any{})
		if err != nil {
			t.Fatalf("%s: call: %v", when, err)
		}
		if got := out.(map[string]any)["issued"]; got != strings.TrimPrefix(want, "at-") {
			t.Fatalf("%s: the invocation was handed at-%v, want %s", when, got, want)
		}
	}

	engine.BreakSealedStore(ds)
	token("the first invocation with the store broken", "at-2")
	token("the second invocation with the store broken", "at-3")
	if got, want := strings.Join(p.sentTokens(), ","), "rt-1,rt-2"; got != want {
		t.Fatalf("refresh tokens sent with the store broken: %s, want %s", got, want)
	}
	if got := strings.Count(logs.String(), unstoredLine); got != 3 {
		t.Fatalf("the kept tokens were logged %d times, want 3:\n%s", got, logs.String())
	}
	if got := engine.OAuthUnstoredTokens(ds); got != 1 {
		t.Fatalf("%d tokens kept in memory, want 1", got)
	}

	engine.RepairSealedStore(ds)
	// The kept at-3 lands and, being due, is refreshed with rt-3.
	if n, err := ds.RefreshOAuthTokens(ctx); err != nil || n != 1 {
		t.Fatalf("the pass after the repair: refreshed %d (%v), want 1", n, err)
	}
	if got, want := strings.Join(p.sentTokens(), ","), "rt-1,rt-2,rt-3"; got != want {
		t.Fatalf("refresh tokens sent: %s, want %s", got, want)
	}
	if got := engine.OAuthUnstoredTokens(ds); got != 0 {
		t.Fatalf("%d tokens kept in memory after the repair, want 0", got)
	}
	wantTokenStatus(t, ds, account, "connected")
}

// Deleting an account revokes the token kept in memory for it, the live
// grant of a provider that rotates refresh tokens, beside the stored one,
// and drops it.
func TestOAuthTeardownRevokesAKeptToken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := newFakeProvider(t)
	docs := mbDocs(nil,
		mbConfigTypeDoc(), mbAccountTypeDoc(), mbItemTypeDoc(), mbMessageTypeDoc(),
		mbFnDoc("fresh", mbFreshSource))
	svc, ds, account, _ := installBackoffBundle(t, docs, p.ts.URL, p.configProps(), p.ts.Client(), nil)
	p.expiresIn = 30 // due for an invocation
	p.rotate = true
	connectOAuthAccount(t, svc, ds, account)

	engine.BreakSealedStore(ds)
	if _, _, err := ds.CallFunction(ctx, substrate.ActorAPI, mbPackage+"/fresh", map[string]any{}); err != nil {
		t.Fatalf("the invocation whose store fails: %v", err)
	}
	engine.RepairSealedStore(ds)
	if got := engine.OAuthUnstoredTokens(ds); got != 1 {
		t.Fatalf("%d tokens kept in memory, want 1", got)
	}

	if _, err := ds.Delete(ctx, owner, account.Kind, account.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete account: %v", err)
	}
	if released, err := ds.ProcessOAuthFinalizers(ctx); err != nil || released != 1 {
		t.Fatalf("finalizers: %d %v", released, err)
	}
	p.mu.Lock()
	revoked := strings.Join(p.revoked, ",")
	p.mu.Unlock()
	if revoked != "rt-1,rt-2" {
		t.Fatalf("revoked: %s, want rt-1 (stored) and rt-2 (kept)", revoked)
	}
	if got := engine.OAuthUnstoredTokens(ds); got != 0 {
		t.Fatalf("%d tokens kept in memory after the teardown, want 0", got)
	}
}
