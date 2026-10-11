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
