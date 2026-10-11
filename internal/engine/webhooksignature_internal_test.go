package engine

import (
	"testing"
	"time"
)

// whoopSignatureDecl is WHOOP's scheme as a trigger declares it.
func whoopSignatureDecl() map[string]any {
	return map[string]any{
		"algorithm":       "hmac-sha256",
		"signatureHeader": "X-WHOOP-Signature",
		"timestampHeader": "X-WHOOP-Signature-Timestamp",
		"message":         "{timestamp}{body}",
		"encoding":        "base64",
		"secret":          "oauthClientSecret",
	}
}

// source.webhook.signature parses to what the door checks: headers
// lowercased, the default skew, the message split into its parts, and the
// secret named either way.
func TestWebhookSignatureDeclarationIsAdmittedAtParse(t *testing.T) {
	t.Parallel()
	sig, err := parseWebhookSignature(whoopSignatureDecl())
	if err != nil {
		t.Fatalf("WHOOP's declaration was refused: %v", err)
	}
	if sig.Header != "x-whoop-signature" || sig.TimestampHeader != "x-whoop-signature-timestamp" {
		t.Fatalf("headers = %q, %q", sig.Header, sig.TimestampHeader)
	}
	if sig.MaxSkew != 5*time.Minute || !sig.OAuthClient || sig.SecretRecord != "" || sig.Encoding != "base64" {
		t.Fatalf("parsed %+v", sig)
	}
	if len(sig.Message) != 2 || sig.Message[0].field != "timestamp" || sig.Message[1].field != "body" {
		t.Fatalf("message parts = %+v", sig.Message)
	}

	github, err := parseWebhookSignature(map[string]any{
		"algorithm": "hmac-sha256", "signatureHeader": "X-Hub-Signature-Hex",
		"encoding": "hex", "secret": "substrate.reamde.dev/core/secret/webhooks/github",
	})
	if err != nil {
		t.Fatalf("a body-only declaration was refused: %v", err)
	}
	if github.SecretRecord != "webhooks/github" || github.OAuthClient || len(github.Message) != 1 || github.Message[0].field != "body" {
		t.Fatalf("parsed %+v", github)
	}

	stamped, err := parseWebhookSignature(map[string]any{
		"algorithm": "hmac-sha256", "signatureHeader": "x-sig", "timestampHeader": "x-ts",
		"maxSkew": "PT30S", "message": "v1:{timestamp}:{body}", "encoding": "hex",
		"secret": "oauthClientSecret",
	})
	if err != nil {
		t.Fatalf("a literal-carrying message was refused: %v", err)
	}
	if stamped.MaxSkew != 30*time.Second || len(stamped.Message) != 4 || stamped.Message[0].literal != "v1:" || stamped.Message[2].literal != ":" {
		t.Fatalf("parsed %+v", stamped)
	}

	if sig, err := parseWebhookSignature(nil); sig != nil || err != nil {
		t.Fatalf("an absent block = %v, %v", sig, err)
	}

	for name, edit := range map[string]func(map[string]any){
		"not a map":                 nil,
		"an unknown key":            func(m map[string]any) { m["prefix"] = "sha256=" },
		"no algorithm":              func(m map[string]any) { delete(m, "algorithm") },
		"sha1":                      func(m map[string]any) { m["algorithm"] = "hmac-sha1" },
		"no signature header":       func(m map[string]any) { delete(m, "signatureHeader") },
		"a bad header name":         func(m map[string]any) { m["signatureHeader"] = "x sig" },
		"the authorization header":  func(m map[string]any) { m["signatureHeader"] = "Authorization" },
		"one header for both":       func(m map[string]any) { m["timestampHeader"] = "x-whoop-signature" },
		"an unsigned timestamp":     func(m map[string]any) { m["message"] = "{body}" },
		"a {timestamp} with no hdr": func(m map[string]any) { delete(m, "timestampHeader") },
		"maxSkew without a stamp":   func(m map[string]any) { delete(m, "timestampHeader"); m["message"] = "{body}"; m["maxSkew"] = "PT1M" },
		"maxSkew past an hour":      func(m map[string]any) { m["maxSkew"] = "PT61M" },
		"maxSkew under a second":    func(m map[string]any) { m["maxSkew"] = "PT0S" },
		"maxSkew not a duration":    func(m map[string]any) { m["maxSkew"] = "five minutes" },
		"two bodies":                func(m map[string]any) { m["message"] = "{timestamp}{body}{body}" },
		"no body":                   func(m map[string]any) { m["message"] = "{timestamp}" },
		"an unknown field":          func(m map[string]any) { m["message"] = "{timestamp}{payload}" },
		"an unclosed brace":         func(m map[string]any) { m["message"] = "{timestamp}{body" },
		"a stray brace":             func(m map[string]any) { m["message"] = "{timestamp}}{body}" },
		"no encoding":               func(m map[string]any) { delete(m, "encoding") },
		"base32":                    func(m map[string]any) { m["encoding"] = "base32" },
		"no secret":                 func(m map[string]any) { delete(m, "secret") },
		"a bare secret id":          func(m map[string]any) { m["secret"] = "webhooks/github" },
		"another kind's record":     func(m map[string]any) { m["secret"] = "substrate.reamde.dev/core/setting/webhooks/github" },
		"a secret path with no id":  func(m map[string]any) { m["secret"] = "substrate.reamde.dev/core/secret/" },
		"a secret that is a number": func(m map[string]any) { m["secret"] = 7 },
	} {
		t.Run(name, func(t *testing.T) {
			var raw any = "hmac-sha256"
			if edit != nil {
				m := whoopSignatureDecl()
				edit(m)
				raw = m
			}
			if sig, err := parseWebhookSignature(raw); err == nil {
				t.Fatalf("admitted as %+v", sig)
			}
		})
	}
}

// A signed timestamp is Unix time in seconds, or in milliseconds from 10^11
// up, and nothing else.
func TestWebhookTimestampReadsSecondsAndMilliseconds(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	for in, want := range map[string]time.Time{
		"1791720000":    at,
		"1791720000000": at,
		"1791720000250": at.Add(250 * time.Millisecond),
	} {
		got, ok := parseWebhookTimestamp(in)
		if !ok || !got.Equal(want) {
			t.Errorf("%s = %v, %v, want %v", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "-1791720000", "1791720000.5", "2026-10-11T12:00:00Z", "12345678901234567", " 1791720000"} {
		if got, ok := parseWebhookTimestamp(in); ok {
			t.Errorf("%q read as %v", in, got)
		}
	}
}

// A stored `fire.at` with a fraction of a second is the receipt. A
// whole-second one is the receipt the admission recorded when that falls
// inside the second, and otherwise the start of the second.
func TestStoredWebhookReceiptKeepsMilliseconds(t *testing.T) {
	t.Parallel()
	received := time.Date(2026, 10, 11, 12, 0, 0, 900_000_000, time.UTC)
	second := received.Truncate(time.Second)
	for _, c := range []struct {
		name     string
		stamp    string
		recorded time.Time
		want     time.Time
	}{
		{"a fractional stamp", "2026-10-11T12:00:00.9Z", time.Time{}, received},
		{"a fractional stamp beside a recorded receipt", "2026-10-11T12:00:00.9Z", received.Add(time.Millisecond), received},
		{"a whole-second stamp and its recorded receipt", "2026-10-11T12:00:00Z", received, received},
		{"a whole-second stamp alone", "2026-10-11T12:00:00Z", time.Time{}, second},
		{"a whole-second stamp and a receipt in another second", "2026-10-11T12:00:00Z", received.Add(time.Second), second},
	} {
		got, ok := receiptOf(c.stamp, c.recorded)
		if !ok || !got.Equal(c.want) {
			t.Errorf("%s: receiptOf(%q, %v) = %v, %v, want %v", c.name, c.stamp, c.recorded, got, ok, c.want)
		}
	}
	if _, ok := receiptOf("", time.Time{}); ok {
		t.Error("an empty stamp read as a receipt")
	}
}
