package engine_test

// A webhook trigger's declared signature is checked at the door, before the
// request becomes a blob, a ledger entry or an invocation (decision 0164). A
// refused request leaves the repository as it found it.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	hookSigningSecret = "a-signing-secret-the-provider-shares"
	hookSecretID      = "hooks/signing"
)

// signedSource is a webhook arm whose provider signs `{timestamp}.{body}`
// with HMAC-SHA256, base64, the timestamp in seconds: Standard Webhooks'
// shape, without its id.
func signedSource(key, secret string) map[string]any {
	arm := map[string]any{
		"signature": map[string]any{
			"algorithm":       "hmac-sha256",
			"signatureHeader": "X-Hook-Signature",
			"timestampHeader": "X-Hook-Timestamp",
			"maxSkew":         "PT5M",
			"message":         "{timestamp}.{body}",
			"encoding":        "base64",
			"secret":          secret,
		},
	}
	if key != "" {
		arm["key"] = key
	}
	return map[string]any{"webhook": arm}
}

// signedHook is a JSON request signed under secret at the instant at.
func signedHook(body, secret string, at time.Time) substrate.WebhookRequest {
	stamp := strconv.FormatInt(at.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(stamp + "." + body))
	req := jsonHook(body, "signed")
	req.Headers["x-hook-timestamp"] = stamp
	req.Headers["x-hook-signature"] = base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return req
}

// storedTrace is what a delivery leaves in the repository: blob manifests,
// ledger rows and `delivery` changelog entries, read from the tamperer's seat.
type storedTrace struct{ blobs, failures, deliveries int }

func traceOf(t *testing.T, dsn string) storedTrace {
	t.Helper()
	var s storedTrace
	db := rawDB(t, dsn)
	if err := db.QueryRow(`SELECT count(*) FROM records WHERE kind = 'substrate.reamde.dev/core/blob'`).Scan(&s.blobs); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM trigger_failures`).Scan(&s.failures); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM changelog WHERE op = 'delivery'`).Scan(&s.deliveries); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestWebhookSignatureIsCheckedBeforeAnythingIsStored(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var invoked atomic.Int32
	svc, ds, dsn := newDatasetWithDSN(t, engine.WithTestInvokeHook(func(function string) {
		if strings.HasSuffix(function, "/hookecho") {
			invoked.Add(1)
		}
	}))
	secretRef := vocabulary.RecordPath("substrate.reamde.dev/core/secret", hookSecretID)
	connector := fnConnector(
		[]enginetest.Trigger{
			hookTrigger("hook-signed", signedSource("", secretRef), "hookecho", true),
			hookTrigger("hook-both", signedSource(hookKey, secretRef), "hookecho", true),
			hookTrigger("hook-nosecret", signedSource("", vocabulary.RecordPath("substrate.reamde.dev/core/secret", "hooks/missing")), "hookecho", true),
			hookTrigger("hook-emptysecret", signedSource("", vocabulary.RecordPath("substrate.reamde.dev/core/secret", "hooks/empty")), "hookecho", true),
		},
		pyFn("hookecho", map[string]any{}, []any{widgetType}, hookEchoSource),
	)
	if err := enginetest.Install(ctx, ds, owner, connector); err != nil {
		t.Fatalf("register connector: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/secret", ID: hookSecretID, Properties: map[string]any{"value": hookSigningSecret}})
	mustPut(t, ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/secret", ID: "hooks/empty", Properties: map[string]any{"displayName": "unfilled"}})
	authority := testdb.Repository(t)
	now := time.Now()

	stale := signedHook(`{"n":"stale"}`, hookSigningSecret, now.Add(-10*time.Minute))
	early := signedHook(`{"n":"early"}`, hookSigningSecret, now.Add(10*time.Minute))
	altered := signedHook(`{"n":"altered"}`, hookSigningSecret, now)
	altered.Body = []byte(`{"n":"tampered"}`)
	unsigned := jsonHook(`{"n":"unsigned"}`, "unsigned")
	noStamp := signedHook(`{"n":"nostamp"}`, hookSigningSecret, now)
	delete(noStamp.Headers, "x-hook-timestamp")
	garbled := signedHook(`{"n":"garbled"}`, hookSigningSecret, now)
	garbled.Headers["x-hook-signature"] = "not base64 at all"
	multipart := substrate.WebhookRequest{
		Method: "POST", ContentType: "multipart/form-data",
		Headers: signedHook("", hookSigningSecret, now).Headers,
		Parts:   []substrate.WebhookPart{{Name: "audio", Filename: "a.m4a", MediaType: "audio/mp4", Data: []byte("bytes")}},
	}

	// A keyless trigger refuses with the door's one 404, so a POST cannot
	// tell that the repository has the trigger installed; one whose key the
	// request carried refuses with the signature's 401.
	for _, tc := range []struct {
		name, trigger, reason string
		req                   substrate.WebhookRequest
	}{
		{"an unsigned request", "hook-signed", "no signature", unsigned},
		{"a signature under another secret", "hook-signed", "signature does not match", signedHook(`{"n":"forged"}`, "another-secret-entirely", now)},
		{"an altered body", "hook-signed", "signature does not match", altered},
		{"a signature that does not decode", "hook-signed", "signature does not match", garbled},
		{"a timestamp ten minutes old", "hook-signed", "timestamp outside maxSkew", stale},
		{"a timestamp ten minutes ahead", "hook-signed", "timestamp outside maxSkew", early},
		{"no timestamp", "hook-signed", "no readable timestamp", noStamp},
		{"a multipart body", "hook-signed", "multipart body", multipart},
		{"a secret record that does not exist", "hook-nosecret", "signing secret unresolvable", signedHook(`{"n":"x"}`, hookSigningSecret, now)},
		{"a secret record with no value", "hook-emptysecret", "signing secret unresolvable", signedHook(`{"n":"x"}`, hookSigningSecret, now)},
		{"the right key and no signature", "hook-both", "no signature", jsonHook(`{"n":"keyonly"}`, "k")},
		{"the right key and a signature under another secret", "hook-both", "signature does not match", signedHook(`{"n":"keyforged"}`, "another-secret-entirely", now)},
		{"the right key and a timestamp ten minutes old", "hook-both", "timestamp outside maxSkew", signedHook(`{"n":"keystale"}`, hookSigningSecret, now.Add(-10*time.Minute))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := traceOf(t, dsn)
			key, want := "", substrate.ErrNotFound
			if tc.trigger == "hook-both" {
				key, want = hookKey, substrate.ErrAuth
			}
			_, err := engine.ReceiveWebhookSync(ctx, svc, authority, tc.trigger, key, tc.req)
			if !errors.Is(err, want) || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("err = %v, want %v for %q", err, want, tc.reason)
			}
			if after := traceOf(t, dsn); after != before {
				t.Fatalf("a refused request stored something: before %+v, after %+v", before, after)
			}
			if n := invoked.Load(); n != 0 {
				t.Fatalf("a refused request invoked the callable %d times", n)
			}
		})
	}

	// The key is checked first and still answers the door's one 404, so a
	// good signature does not make a keyed endpoint probeable.
	if _, err := engine.ReceiveWebhookSync(ctx, svc, authority, "hook-both", "wrong-key-wrong-key", signedHook(`{"n":"k"}`, hookSigningSecret, now)); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("a wrong key with a good signature: err = %v, want ErrNotFound", err)
	}

	// The controls: the same trigger, signed and on time, delivers; and one
	// carrying both gates delivers with both.
	before := traceOf(t, dsn)
	fid, err := engine.ReceiveWebhookSync(ctx, svc, authority, "hook-signed", "", signedHook(`{"n":"signed"}`, hookSigningSecret, now.Add(-time.Minute)))
	if err != nil {
		t.Fatalf("a signed request was refused: %v", err)
	}
	if got := hookEcho(t, ds, "hook-echo"); got["name"] != `{"n":"signed"}` || got["record"] != fid {
		t.Fatalf("the signed delivery echoed %v", got)
	}
	if after := traceOf(t, dsn); after.deliveries <= before.deliveries {
		t.Fatalf("the admitted request appended no delivery: before %+v, after %+v", before, after)
	}
	if invoked.Load() != 1 {
		t.Fatalf("the admitted request invoked the callable %d times, want 1", invoked.Load())
	}
	if _, err := engine.ReceiveWebhookSync(ctx, svc, authority, "hook-both", hookKey, signedHook(`{"n":"both"}`, hookSigningSecret, now)); err != nil {
		t.Fatalf("the right key and a good signature were refused: %v", err)
	}
	if got := hookEcho(t, ds, "hook-echo")["name"]; got != `{"n":"both"}` {
		t.Fatalf("the keyed, signed delivery echoed %v", got)
	}
}

// A signed request posted again inside its timestamp's window is the same
// delivery: the door answers the first one's fire id and stores nothing, so
// a captured request replayed, or a provider's resend of one whose answer it
// lost, runs once. A request with another body is another signature and
// another delivery.
func TestWebhookSignatureRepeatIsOneDelivery(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var invoked atomic.Int32
	svc, ds, dsn := newDatasetWithDSN(t, engine.WithTestInvokeHook(func(function string) {
		if strings.HasSuffix(function, "/hookecho") {
			invoked.Add(1)
		}
	}))
	secretRef := vocabulary.RecordPath("substrate.reamde.dev/core/secret", hookSecretID)
	connector := fnConnector(
		[]enginetest.Trigger{hookTrigger("hook-once", signedSource("", secretRef), "hookecho", true)},
		pyFn("hookecho", map[string]any{}, []any{widgetType}, hookEchoSource),
	)
	if err := enginetest.Install(ctx, ds, owner, connector); err != nil {
		t.Fatalf("register connector: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/secret", ID: hookSecretID, Properties: map[string]any{"value": hookSigningSecret}})
	authority := testdb.Repository(t)
	now := time.Now()

	captured := signedHook(`{"n":"once"}`, hookSigningSecret, now)
	first, err := engine.ReceiveWebhookSync(ctx, svc, authority, "hook-once", "", captured)
	if err != nil {
		t.Fatalf("the first delivery: %v", err)
	}
	before := traceOf(t, dsn)
	for i := range 3 {
		again, err := engine.ReceiveWebhookSync(ctx, svc, authority, "hook-once", "", signedHook(`{"n":"once"}`, hookSigningSecret, now))
		if err != nil || again != first {
			t.Fatalf("repeat %d: fire %q (%v), want the first delivery's %q", i, again, err, first)
		}
	}
	if after := traceOf(t, dsn); after != before {
		t.Fatalf("a repeat stored something: before %+v, after %+v", before, after)
	}
	if n := invoked.Load(); n != 1 {
		t.Fatalf("the callable ran %d times, want once", n)
	}
	other, err := engine.ReceiveWebhookSync(ctx, svc, authority, "hook-once", "", signedHook(`{"n":"other"}`, hookSigningSecret, now))
	if err != nil || other == first {
		t.Fatalf("another body under the same timestamp: fire %q (%v), want a delivery of its own", other, err)
	}
	if n := invoked.Load(); n != 2 {
		t.Fatalf("the callable ran %d times, want twice", n)
	}
}

// doorLog is a log sink the test reads while the service's background loops
// still write to it.
type doorLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *doorLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *doorLog) lines(substr string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, line := range strings.Split(l.buf.String(), "\n") {
		if strings.Contains(line, substr) {
			out = append(out, line)
		}
	}
	return out
}

// Anyone can reach the unresolvable-secret check with a fresh timestamp and
// 32 random bytes, so the door logs it once per trigger per minute and the
// next line counts the refusals it did not log.
func TestWebhookUnresolvableSecretIsLoggedOncePerMinute(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var logs doorLog
	var clock atomic.Int64
	clock.Store(time.Now().UnixNano())
	now := func() time.Time { return time.Unix(0, clock.Load()).UTC() }
	svc, ds, _ := newHookDatasetWithOptions(t,
		[]engine.Option{engine.WithLogger(slog.New(slog.NewTextHandler(&logs, nil))), engine.WithTestWebhookClock(now)},
		nil, pyFn("hookecho", map[string]any{}, []any{widgetType}, hookEchoSource))
	putHookTrigger(t, ds, "hook-unfilled", "hookecho", signedSource("", vocabulary.RecordPath("substrate.reamde.dev/core/secret", "hooks/missing")))
	authority := testdb.Repository(t)
	const warning = "signing secret does not resolve"
	probe := func() {
		t.Helper()
		_, err := engine.ReceiveWebhookSync(ctx, svc, authority, "hook-unfilled", "", signedHook(`{"n":"probe"}`, "a-guess", now()))
		if !errors.Is(err, substrate.ErrNotFound) || !strings.Contains(err.Error(), "signing secret unresolvable") {
			t.Fatalf("err = %v, want the door's refusal for an unresolvable secret", err)
		}
	}
	for range 5 {
		probe()
	}
	if got := logs.lines(warning); len(got) != 1 {
		t.Fatalf("five refusals inside a minute logged %d lines, want 1: %q", len(got), got)
	}
	clock.Add(int64(time.Minute))
	probe()
	got := logs.lines(warning)
	if len(got) != 2 || !strings.Contains(got[1], "unlogged=4") {
		t.Fatalf("a refusal a minute later logged %q, want a second line counting the 4 unlogged", got)
	}
}

// A signature declaration is held at write time: a field the door cannot act
// on, or `oauthClientSecret` on a callable whose bundle declares no oauth2
// block, never lands.
func TestWebhookSignatureIsRefusedAtWriteTime(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newHookDataset(t, nil, pyFn("hookecho", map[string]any{}, []any{widgetType}, hookEchoSource))
	callable := vocabulary.RecordPath("substrate.reamde.dev/core/function", fnPackage+"/hookecho")
	good := func() map[string]any {
		return signedSource("", vocabulary.RecordPath("substrate.reamde.dev/core/secret", hookSecretID))["webhook"].(map[string]any)["signature"].(map[string]any)
	}
	put := func(sig map[string]any) error {
		_, err := ds.Put(ctx, owner, substrate.PutInput{
			Kind: "substrate.reamde.dev/core/trigger", ID: "hook-declared",
			Properties: map[string]any{
				"source":   map[string]any{"webhook": map[string]any{"signature": sig}},
				"callable": callable,
			},
		})
		return err
	}
	if err := put(good()); err != nil {
		t.Fatalf("a good declaration was refused: %v", err)
	}
	for name, edit := range map[string]func(map[string]any){
		"oauthClientSecret without an oauth2 bundle": func(s map[string]any) { s["secret"] = "oauthClientSecret" },
		"another algorithm":                          func(s map[string]any) { s["algorithm"] = "hmac-sha1" },
		"no signature header":                        func(s map[string]any) { delete(s, "signatureHeader") },
		"an encoding the door does not read":         func(s map[string]any) { s["encoding"] = "base32" },
		"an unsigned timestamp":                      func(s map[string]any) { s["message"] = "{body}" },
		"a timestamp field with no header":           func(s map[string]any) { delete(s, "timestampHeader"); delete(s, "maxSkew") },
		"a skew past an hour":                        func(s map[string]any) { s["maxSkew"] = "PT2H" },
		"a secret named by a bare id":                func(s map[string]any) { s["secret"] = hookSecretID },
		"a message without the body":                 func(s map[string]any) { s["message"] = "{timestamp}" },
		"an unknown message field":                   func(s map[string]any) { s["message"] = "{timestamp}.{payload}" },
		"an unknown key":                             func(s map[string]any) { s["prefix"] = "sha256=" },
	} {
		sig := good()
		edit(sig)
		if err := put(sig); err == nil {
			t.Errorf("%s: landed", name)
		}
	}
}

// `secret: oauthClientSecret` checks the request against the client secret of
// the callable's bundle's OAuth client input, which the host resolves and no
// function is handed. A hex, millisecond-stamped scheme here, WHOOP's minus
// its base64.
func TestWebhookSignatureUnderTheOAuthClientSecret(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, p, _ := installOAuthBundle(t)
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/trigger", ID: "mail-hook",
		Properties: map[string]any{
			"enabled": true,
			"source": map[string]any{"webhook": map[string]any{"signature": map[string]any{
				"algorithm":       "hmac-sha256",
				"signatureHeader": "x-mail-signature",
				"timestampHeader": "x-mail-timestamp",
				"message":         "{timestamp}{body}",
				"encoding":        "hex",
				"secret":          "oauthClientSecret",
			}}},
			"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", mbEchoFn),
		},
	})
	clientSecret, _ := p.configProps()["clientSecret"].(string)
	sign := func(secret string) substrate.WebhookRequest {
		body := `{"mail":"arrived"}`
		stamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(stamp + body))
		return substrate.WebhookRequest{
			Method: "POST", ContentType: "application/json",
			Headers: map[string]string{
				"content-type": "application/json", "x-mail-timestamp": stamp,
				"x-mail-signature": hex.EncodeToString(mac.Sum(nil)),
			},
			Body: []byte(body),
		}
	}
	authority := ds.Repository().Authority
	if _, err := engine.ReceiveWebhookSync(ctx, svc, authority, "mail-hook", "", sign("not-the-client-secret")); !errors.Is(err, substrate.ErrNotFound) || !strings.Contains(err.Error(), "signature does not match") {
		t.Fatalf("a request under another secret: err = %v, want the door's refusal, signature does not match", err)
	}
	fid, err := engine.ReceiveWebhookSync(ctx, svc, authority, "mail-hook", "", sign(clientSecret))
	if err != nil {
		t.Fatalf("a request under the client secret was refused: %v", err)
	}
	if n := okRuns(t, ds, fid); n != 1 {
		t.Fatalf("OK runs under %s = %d, want 1", fid, n)
	}
}

// A store that fails while the door reads the signing secret decides nothing
// about the request. The door answers it as the store's failure, never the
// signature refusal a sender reads as final, and stores nothing. A stored
// request whose recheck meets the same failure is a transient fault: a retry
// by hand answers the store's error, the dispatcher's resume spends every
// attempt on it before it parks, and neither is parked as refused. Both run
// once the store answers again. The fault is the `sealed` table renamed away
// under the secret record's value.
func TestWebhookSigningSecretStoreFaultIsNotARefusal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, dsn := newHookDatasetWithDSN(t, nil, pyFn("hookecho", map[string]any{}, []any{widgetType}, hookEchoSource))
	mustPut(t, ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/secret", ID: hookSecretID, Properties: map[string]any{"value": hookSigningSecret}})
	putHookTrigger(t, ds, "hook-fault", "hookecho", keptSignatureSource(true, "PT5M"))
	putHookTrigger(t, ds, "hook-fault-stored", "hookecho", keptSignatureSource(false, ""))
	authority := testdb.Repository(t)
	byHand, err := engine.ReceiveWebhookHeld(ctx, svc, authority, "hook-fault-stored", "", signedHook(`{"n":"by-hand"}`, hookSigningSecret, time.Now()))
	if err != nil {
		t.Fatalf("receive the stored request: %v", err)
	}
	resumed, err := engine.ReceiveWebhookHeld(ctx, svc, authority, "hook-fault-stored", "", signedHook(`{"n":"resumed"}`, hookSigningSecret, time.Now()))
	if err != nil {
		t.Fatalf("receive the stored request: %v", err)
	}
	putHookTrigger(t, ds, "hook-fault-stored", "hookecho", keptSignatureSource(true, "PT5M"))

	db := rawDB(t, dsn)
	if _, err := db.Exec(`ALTER TABLE sealed RENAME TO sealed_unreachable`); err != nil {
		t.Fatal(err)
	}
	before := traceOf(t, dsn)
	_, err = engine.ReceiveWebhookSync(ctx, svc, authority, "hook-fault", "", signedHook(`{"n":"fault"}`, hookSigningSecret, time.Now()))
	if err == nil || errors.Is(err, substrate.ErrAuth) || errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("a store fault at the door: err = %v, want the store's error, neither a refusal nor the door's 404", err)
	}
	if after := traceOf(t, dsn); after != before {
		t.Fatalf("the faulted request stored something: before %+v, after %+v", before, after)
	}
	_, err = ds.RetryTriggerFailure(ctx, "hook-fault-stored", failuresByFire(t, ds, "hook-fault-stored")[byHand].ID)
	if err == nil || strings.Contains(err.Error(), "webhook signature refused at delivery") {
		t.Fatalf("a store fault on a retry by hand: err = %v, want the store's error, not the refusal", err)
	}
	dispatch(t, ds)
	deadline := time.Now().Add(30 * time.Second)
	var row substrate.TriggerFailure
	for {
		row = failuresByFire(t, ds, "hook-fault-stored")[resumed]
		if row.LastError != engine.WebhookPendingError {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the dispatcher pass never settled the pending entry")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if row.Attempts != 3 || strings.Contains(row.LastError, "refused at delivery") {
		t.Fatalf("the resume parked as %+v, want three attempts spent on the store's error", row)
	}

	if _, err := db.Exec(`ALTER TABLE sealed_unreachable RENAME TO sealed`); err != nil {
		t.Fatal(err)
	}
	for _, fid := range []string{byHand, resumed} {
		if _, err := ds.RetryTriggerFailure(ctx, "hook-fault-stored", failuresByFire(t, ds, "hook-fault-stored")[fid].ID); err != nil {
			t.Fatalf("%s, once the store answers: %v", fid, err)
		}
		if got := hookEcho(t, ds, "hook-echo"); got["record"] != fid {
			t.Fatalf("the retry of %s echoed %v", fid, got)
		}
	}
	fid, err := engine.ReceiveWebhookSync(ctx, svc, authority, "hook-fault", "", signedHook(`{"n":"recovered"}`, hookSigningSecret, time.Now()))
	if err != nil {
		t.Fatalf("the door, once the store answers: %v", err)
	}
	if got := hookEcho(t, ds, "hook-echo"); got["record"] != fid {
		t.Fatalf("the recovered delivery echoed %v", got)
	}
}

// putHookTrigger writes a webhook trigger on fn the way the owner does,
// outside any bundle, so a test can rewrite its source between deliveries.
func putHookTrigger(t *testing.T, ds substrate.Dataset, id, fn string, source map[string]any) {
	t.Helper()
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/trigger", ID: id,
		Properties: map[string]any{
			"enabled":  true,
			"source":   source,
			"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", fnPackage+"/"+fn),
		},
	})
}

// keptSignatureSource is a webhook arm that keeps the signature headers, so a
// request stored under it holds what a later declaration checks. signed adds
// that declaration under the hookSecretID record, with maxSkew skew.
func keptSignatureSource(signed bool, skew string) map[string]any {
	arm := map[string]any{"headers": []any{"x-github-event", "x-hook-signature", "x-hook-timestamp"}}
	if signed {
		sig := signedSource("", vocabulary.RecordPath("substrate.reamde.dev/core/secret", hookSecretID))["webhook"].(map[string]any)["signature"].(map[string]any)
		sig["maxSkew"] = skew
		arm["signature"] = sig
	}
	return map[string]any{"webhook": arm}
}

// failuresByFire is a trigger's ledger rows keyed by fire id.
func failuresByFire(t *testing.T, ds substrate.Dataset, trigger string) map[string]substrate.TriggerFailure {
	t.Helper()
	rows, err := ds.TriggerFailures(context.Background(), trigger)
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]substrate.TriggerFailure, len(rows))
	for _, r := range rows {
		out[r.FireID] = r
	}
	return out
}

// A request stored while its trigger declared no signature carries no mark
// that the door verified it. Once the trigger declares one, a hand's retry
// of a pending entry, a retry of a parked one and the dispatcher's resume
// each refuse an unsigned request and park it naming the signature, and the
// callable never runs it.
func TestWebhookSignatureDeclaredLaterRefusesAStoredUnsignedRequest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var invoked atomic.Int32
	svc, ds, _ := newDatasetWithDSN(t, engine.WithTestInvokeHook(func(function string) {
		if strings.HasSuffix(function, "/hookgated") {
			invoked.Add(1)
		}
	}))
	connector := fnConnector(nil, pyFn("hookgated", map[string]any{
		"permissions": map[string]any{"reads": map[string]any{"kinds": []any{widgetType}}},
	}, []any{widgetType}, hookGatedSource))
	if err := enginetest.Install(ctx, ds, owner, connector); err != nil {
		t.Fatalf("register connector: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/secret", ID: hookSecretID, Properties: map[string]any{"value": hookSigningSecret}})
	putHookTrigger(t, ds, "hook-later", "hookgated", keptSignatureSource(false, ""))
	authority := testdb.Repository(t)

	// Two pending entries, and one a failed fire parked: all unsigned.
	retried, err := engine.ReceiveWebhookHeld(ctx, svc, authority, "hook-later", "", jsonHook(`{"n":"unsigned-before-upgrade"}`, "held"))
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	resumed, err := engine.ReceiveWebhookHeld(ctx, svc, authority, "hook-later", "", jsonHook(`{"n":"resumed"}`, "held"))
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	parked, err := engine.ReceiveWebhookSync(ctx, svc, authority, "hook-later", "", jsonHook(`{"n":"parked"}`, "gate"))
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	ran := invoked.Load()
	if ran == 0 {
		t.Fatal("the gated fire never ran")
	}

	// The upgrade: the trigger declares the signature, and the gate the body
	// waits on opens, so only the signature stands between a request and a
	// run.
	putHookTrigger(t, ds, "hook-later", "hookgated", keptSignatureSource(true, "PT5M"))
	mustPut(t, ds, owner, substrate.PutInput{Kind: widgetType, ID: "gate", Properties: map[string]any{"name": "open"}})

	rows := failuresByFire(t, ds, "hook-later")
	for _, fid := range []string{retried, parked} {
		_, err := ds.RetryTriggerFailure(ctx, "hook-later", rows[fid].ID)
		if !errors.Is(err, substrate.ErrParked) || !strings.Contains(err.Error(), "source.webhook.signature") {
			t.Fatalf("retry of the unsigned %s: err = %v, want ErrParked naming the signature", fid, err)
		}
	}
	dispatch(t, ds)
	deadline := time.Now().Add(30 * time.Second)
	for {
		row, ok := failuresByFire(t, ds, "hook-later")[resumed]
		if !ok || row.LastError != engine.WebhookPendingError {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the dispatcher pass never settled the pending entry")
		}
		time.Sleep(50 * time.Millisecond)
	}
	rows = failuresByFire(t, ds, "hook-later")
	for _, fid := range []string{retried, resumed, parked} {
		row, ok := rows[fid]
		if !ok || !strings.Contains(row.LastError, "source.webhook.signature") {
			t.Fatalf("entry %s = %+v (present %v), want parked naming the signature", fid, row, ok)
		}
	}
	if _, err := ds.Get(ctx, widgetType, "gated-echo"); err == nil {
		t.Fatal("an unsigned stored request ran after the trigger declared a signature")
	}
	if n := invoked.Load(); n != ran {
		t.Fatalf("the callable ran %d more times after the declaration", n-ran)
	}
}

// A stored request the declared signature covers still runs once the skew
// window has passed. One the door verified carries that mark and is not
// checked again; one stored before the declaration is checked from its kept
// body and headers against the time the door received it, not the retry's
// clock, and one of those whose signature does not match is refused.
func TestWebhookSignatureStoredRequestRunsPastItsSkew(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// The door receives and the sender signs 3 s before the retries run, past
	// the PT2S skew measured against the retry's clock, with no wait.
	now := time.Now().Add(-3 * time.Second)
	door := engine.WithTestWebhookClock(func() time.Time { return now })
	svc, ds, _ := newHookDatasetWithOptions(t, []engine.Option{door}, nil, pyFn("hookecho", map[string]any{}, []any{widgetType}, hookEchoSource))
	mustPut(t, ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/secret", ID: hookSecretID, Properties: map[string]any{"value": hookSigningSecret}})
	putHookTrigger(t, ds, "hook-verified", "hookecho", keptSignatureSource(true, "PT2S"))
	putHookTrigger(t, ds, "hook-legacy", "hookecho", keptSignatureSource(false, ""))
	authority := testdb.Repository(t)

	verified, err := engine.ReceiveWebhookHeld(ctx, svc, authority, "hook-verified", "", signedHook(`{"n":"verified"}`, hookSigningSecret, now))
	if err != nil {
		t.Fatalf("receive the signed request: %v", err)
	}
	legacy, err := engine.ReceiveWebhookHeld(ctx, svc, authority, "hook-legacy", "", signedHook(`{"n":"legacy"}`, hookSigningSecret, now))
	if err != nil {
		t.Fatalf("receive the legacy request: %v", err)
	}
	forged, err := engine.ReceiveWebhookHeld(ctx, svc, authority, "hook-legacy", "", signedHook(`{"n":"forged"}`, "another-secret-entirely", now))
	if err != nil {
		t.Fatalf("receive the forged request: %v", err)
	}
	putHookTrigger(t, ds, "hook-legacy", "hookecho", keptSignatureSource(true, "PT2S"))

	// Past the declared skew: measured against the retry's clock, every one
	// of these timestamps is stale.

	retry := func(trigger, fid string) error {
		row, ok := failuresByFire(t, ds, trigger)[fid]
		if !ok {
			t.Fatalf("no entry for %s under %s", fid, trigger)
		}
		_, err := ds.RetryTriggerFailure(ctx, trigger, row.ID)
		return err
	}
	if err := retry("hook-verified", verified); err != nil {
		t.Fatalf("a request the door verified, retried past its skew: %v", err)
	}
	if got := hookEcho(t, ds, "hook-echo"); got["name"] != `{"n":"verified"}` || got["record"] != verified {
		t.Fatalf("the verified retry echoed %v", got)
	}
	if err := retry("hook-legacy", legacy); err != nil {
		t.Fatalf("a stored request that verifies, retried past its skew: %v", err)
	}
	if got := hookEcho(t, ds, "hook-echo"); got["name"] != `{"n":"legacy"}` || got["record"] != legacy {
		t.Fatalf("the legacy retry echoed %v", got)
	}
	if err := retry("hook-legacy", forged); !errors.Is(err, substrate.ErrParked) || !strings.Contains(err.Error(), "signature does not match") {
		t.Fatalf("a stored request signed under another secret: err = %v, want ErrParked, signature does not match", err)
	}
	if got := hookEcho(t, ds, "hook-echo"); got["record"] != legacy {
		t.Fatalf("the forged request ran: %v", got)
	}
}

// The door's mark is the only proof a stored request carries when its
// trigger keeps no signature headers, the shape WHOOP ships: the recheck
// has no signature to read. A verified request whose fire fails and parks
// keeps the mark through the park, and a retry by hand long after maxSkew
// runs it.
func TestWebhookSignatureMarkSurvivesAParkAndARetry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// The door receives and the sender signs 3 s before the retry runs, past
	// maxSkew, with no wait.
	received := time.Now().Add(-3 * time.Second)
	door := engine.WithTestWebhookClock(func() time.Time { return received })
	svc, ds, _ := newHookDatasetWithOptions(t, []engine.Option{door}, nil, pyFn("hookgated", map[string]any{
		"permissions": map[string]any{"reads": map[string]any{"kinds": []any{widgetType}}},
	}, []any{widgetType}, hookGatedSource))
	mustPut(t, ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/secret", ID: hookSecretID, Properties: map[string]any{"value": hookSigningSecret}})
	source := signedSource("", vocabulary.RecordPath("substrate.reamde.dev/core/secret", hookSecretID))
	source["webhook"].(map[string]any)["signature"].(map[string]any)["maxSkew"] = "PT2S"
	putHookTrigger(t, ds, "hook-unkept", "hookgated", source)
	authority := testdb.Repository(t)

	fid, err := engine.ReceiveWebhookSync(ctx, svc, authority, "hook-unkept", "", signedHook(`{"n":"marked"}`, hookSigningSecret, received))
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	row, ok := failuresByFire(t, ds, "hook-unkept")[fid]
	if !ok || !strings.Contains(row.LastError, "gate closed") {
		t.Fatalf("the first fire left %+v (present %v), want parked on the closed gate", row, ok)
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: widgetType, ID: "gate", Properties: map[string]any{"name": "open"}})
	if _, err := ds.RetryTriggerFailure(ctx, "hook-unkept", row.ID); err != nil {
		t.Fatalf("a verified request with no kept signature, retried past maxSkew: %v", err)
	}
	if got := hookEcho(t, ds, "gated-echo"); got["record"] != fid {
		t.Fatalf("the retry echoed %v", got)
	}
}

// signedHookMillis is signedHook with the timestamp in milliseconds.
func signedHookMillis(body, secret string, at time.Time) substrate.WebhookRequest {
	stamp := strconv.FormatInt(at.UnixMilli(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(stamp + "." + body))
	req := jsonHook(body, "signed")
	req.Headers["x-hook-timestamp"] = stamp
	req.Headers["x-hook-signature"] = base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return req
}

// A stored request's skew is measured from the instant the door received
// it, to the millisecond, under `maxSkew: PT1S`. The door's receipt clock is
// the test's (WithTestWebhookClock), so each request is received at exactly
// X.900 for a whole second X, however long the door's reads take. Received
// at X.900 and signed at X+1.500, a request is 0.6 s inside the window and
// runs; received at X.900 and signed at X-0.500, one is 1.4 s outside and is
// refused. Measured from the whole second X, both answers would flip. A
// third request's envelope is rewritten to whole seconds, the way a binary
// from before the fractional `fire.at` stored it, and its check reads the
// receipt instant the door recorded in the request's admission entry
// instead.
func TestWebhookSignatureStoredRequestSkewIsMeasuredToTheMillisecond(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var clock atomic.Int64
	door := engine.WithTestWebhookClock(func() time.Time { return time.Unix(0, clock.Load()).UTC() })
	svc, ds, dsn := newHookDatasetWithOptions(t, []engine.Option{door}, nil, pyFn("hookecho", map[string]any{}, []any{widgetType}, hookEchoSource))
	mustPut(t, ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/secret", ID: hookSecretID, Properties: map[string]any{"value": hookSigningSecret}})
	putHookTrigger(t, ds, "hook-ms", "hookecho", keptSignatureSource(false, ""))
	authority := testdb.Repository(t)
	// Whole seconds a minute back, so no receipt lies ahead of the wall clock.
	base := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	received := map[string]time.Time{}
	receive := func(body string, second int, signedFrom time.Duration) string {
		t.Helper()
		at := base.Add(time.Duration(second)*time.Second + 900*time.Millisecond)
		clock.Store(at.UnixNano())
		fid, err := engine.ReceiveWebhookHeld(ctx, svc, authority, "hook-ms", "",
			signedHookMillis(body, hookSigningSecret, at.Add(signedFrom)))
		if err != nil {
			t.Fatalf("receive %s: %v", body, err)
		}
		received[fid] = at
		return fid
	}
	inside := receive(`{"n":"inside"}`, 0, 600*time.Millisecond)
	outside := receive(`{"n":"outside"}`, 1, -1400*time.Millisecond)
	legacy := receive(`{"n":"legacy"}`, 2, 600*time.Millisecond)

	// The door recorded each receipt as the clock named it, in the row and
	// in the stored envelope.
	db := rawDB(t, dsn)
	rows := failuresByFire(t, ds, "hook-ms")
	for _, fid := range []string{inside, outside, legacy} {
		if got := rows[fid].ParkedAt; !got.Equal(received[fid]) {
			t.Fatalf("%s was recorded as received at %s, want %s", fid, got.UTC().Format(time.RFC3339Nano), received[fid].Format(time.RFC3339Nano))
		}
		var stamp string
		if err := db.QueryRow(`SELECT payload->'fire'->>'at' FROM trigger_failures WHERE fire_id = $1`, fid).Scan(&stamp); err != nil {
			t.Fatal(err)
		}
		if at, err := time.Parse(time.RFC3339Nano, stamp); err != nil || !at.Equal(received[fid]) {
			t.Fatalf("%s's stored fire.at is %q (%v), want %s", fid, stamp, err, received[fid].Format(time.RFC3339Nano))
		}
	}
	whole := received[legacy].Truncate(time.Second).Format(time.RFC3339)
	res, err := db.Exec(`UPDATE trigger_failures SET payload = jsonb_set(payload, '{fire,at}', to_jsonb($1::text)) WHERE fire_id = $2`, whole, legacy)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("rewrote %d rows, want the legacy request's", n)
	}
	putHookTrigger(t, ds, "hook-ms", "hookecho", keptSignatureSource(true, "PT1S"))

	retry := func(fid string) error {
		t.Helper()
		row, ok := failuresByFire(t, ds, "hook-ms")[fid]
		if !ok {
			t.Fatalf("no entry for %s", fid)
		}
		_, err := ds.RetryTriggerFailure(ctx, "hook-ms", row.ID)
		return err
	}
	if err := retry(inside); err != nil {
		t.Fatalf("signed 0.6 s after receipt: %v", err)
	}
	if got := hookEcho(t, ds, "hook-echo"); got["record"] != inside {
		t.Fatalf("the inside retry echoed %v", got)
	}
	if err := retry(outside); !errors.Is(err, substrate.ErrParked) || !strings.Contains(err.Error(), "timestamp outside maxSkew") {
		t.Fatalf("signed 1.4 s before receipt: err = %v, want ErrParked, timestamp outside maxSkew", err)
	}
	if err := retry(legacy); err != nil {
		t.Fatalf("signed 0.6 s after receipt, its envelope in whole seconds: %v", err)
	}
	if got := hookEcho(t, ds, "hook-echo"); got["record"] != legacy {
		t.Fatalf("the legacy retry echoed %v", got)
	}
}
