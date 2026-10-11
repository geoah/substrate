package engine_test

// A webhook trigger's declared signature is checked at the door, before the
// request becomes a blob, a ledger entry or an invocation (decision 0164). A
// refused request leaves the repository as it found it.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := traceOf(t, dsn)
			key := ""
			if tc.trigger == "hook-both" {
				key = hookKey
			}
			_, err := engine.ReceiveWebhookSync(ctx, svc, authority, tc.trigger, key, tc.req)
			if !errors.Is(err, substrate.ErrAuth) || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("err = %v, want ErrAuth for %q", err, tc.reason)
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
	if _, err := engine.ReceiveWebhookSync(ctx, svc, authority, "mail-hook", "", sign("not-the-client-secret")); !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("a request under another secret: err = %v, want ErrAuth", err)
	}
	fid, err := engine.ReceiveWebhookSync(ctx, svc, authority, "mail-hook", "", sign(clientSecret))
	if err != nil {
		t.Fatalf("a request under the client secret was refused: %v", err)
	}
	if n := okRuns(t, ds, fid); n != 1 {
		t.Fatalf("OK runs under %s = %d, want 1", fid, n)
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
	svc, ds := newHookDataset(t, nil, pyFn("hookecho", map[string]any{}, []any{widgetType}, hookEchoSource))
	mustPut(t, ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/secret", ID: hookSecretID, Properties: map[string]any{"value": hookSigningSecret}})
	putHookTrigger(t, ds, "hook-verified", "hookecho", keptSignatureSource(true, "PT2S"))
	putHookTrigger(t, ds, "hook-legacy", "hookecho", keptSignatureSource(false, ""))
	authority := testdb.Repository(t)
	now := time.Now()

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
	time.Sleep(3 * time.Second)

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

// atNextFraction sleeps until the wall clock next reaches frac past a whole
// second and returns that instant.
func atNextFraction(frac time.Duration) time.Time {
	now := time.Now()
	at := now.Truncate(time.Second).Add(frac)
	if !at.After(now) {
		at = at.Add(time.Second)
	}
	time.Sleep(time.Until(at))
	return at
}

// A stored request's skew is measured from the instant the door received
// it, to the millisecond, under `maxSkew: PT1S`. Received at X.900 and signed
// at X+1.500, a request is 0.6 s inside the window and runs; received at
// X.900 and signed at X-1.500, one is 1.4 s outside and is refused. Measured
// from the whole second X, both answers would flip. A third request's
// envelope is rewritten to whole seconds, the way a binary from before the
// fractional `fire.at` stored it, and its check reads the receipt instant the
// door recorded in the request's admission entry instead.
func TestWebhookSignatureStoredRequestSkewIsMeasuredToTheMillisecond(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, dsn := newHookDatasetWithDSN(t, nil, pyFn("hookecho", map[string]any{}, []any{widgetType}, hookEchoSource))
	mustPut(t, ds, owner, substrate.PutInput{Kind: "substrate.reamde.dev/core/secret", ID: hookSecretID, Properties: map[string]any{"value": hookSigningSecret}})
	putHookTrigger(t, ds, "hook-ms", "hookecho", keptSignatureSource(false, ""))
	authority := testdb.Repository(t)
	receive := func(body string, signedFrom time.Duration) string {
		t.Helper()
		received := atNextFraction(900 * time.Millisecond)
		fid, err := engine.ReceiveWebhookHeld(ctx, svc, authority, "hook-ms", "",
			signedHookMillis(body, hookSigningSecret, received.Add(signedFrom)))
		if err != nil {
			t.Fatalf("receive %s: %v", body, err)
		}
		return fid
	}
	inside := receive(`{"n":"inside"}`, 600*time.Millisecond)
	outside := receive(`{"n":"outside"}`, -1400*time.Millisecond)
	legacy := receive(`{"n":"legacy"}`, 600*time.Millisecond)

	db := rawDB(t, dsn)
	var stamp string
	if err := db.QueryRow(`SELECT payload->'fire'->>'at' FROM trigger_failures WHERE fire_id = $1`, inside).Scan(&stamp); err != nil {
		t.Fatal(err)
	}
	if at, err := time.Parse(time.RFC3339Nano, stamp); err != nil || at.Equal(at.Truncate(time.Second)) {
		t.Fatalf("the stored fire.at %q (%v) keeps no fraction of a second", stamp, err)
	}
	whole := failuresByFire(t, ds, "hook-ms")[legacy].ParkedAt.UTC().Truncate(time.Second).Format(time.RFC3339)
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
