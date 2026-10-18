package engine

import (
	"bytes"
	"crypto/hmac"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// whoopSignatureDecl is WHOOP's scheme as a trigger declares it.
func whoopSignatureDecl() map[string]any {
	return map[string]any{
		"algorithm":       "hmac-sha256",
		"signatureHeader": "X-WHOOP-Signature",
		"timestampHeader": "X-WHOOP-Signature-Timestamp",
		"timestampUnit":   "milliseconds",
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
	if sig.Header != "x-whoop-signature" || sig.TimestampHeader != "x-whoop-signature-timestamp" || sig.TimestampUnit != "milliseconds" {
		t.Fatalf("headers = %q, %q in %q", sig.Header, sig.TimestampHeader, sig.TimestampUnit)
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
		"algorithm": "hmac-sha256", "signatureHeader": "x-sig", "timestampHeader": "x-ts", "timestampUnit": "seconds",
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
		"a {timestamp} with no hdr": func(m map[string]any) { delete(m, "timestampHeader"); delete(m, "timestampUnit") },
		"maxSkew without a stamp": func(m map[string]any) {
			delete(m, "timestampHeader")
			delete(m, "timestampUnit")
			m["message"] = "{body}"
			m["maxSkew"] = "PT1M"
		},
		"a stamp with no unit":      func(m map[string]any) { delete(m, "timestampUnit") },
		"a unit off the list":       func(m map[string]any) { m["timestampUnit"] = "microseconds" },
		"a unit that is a number":   func(m map[string]any) { m["timestampUnit"] = 1000 },
		"a unit without a stamp":    func(m map[string]any) { delete(m, "timestampHeader"); m["message"] = "{body}" },
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

// signatureHeaderName refuses an empty name like any other off the header
// alphabet: its callers handle an absent header before they ask, so an empty
// one reaching it is a mistake to refuse, never a header to accept.
func TestSignatureHeaderNameRefusesAnEmptyName(t *testing.T) {
	t.Parallel()
	if got, err := signatureHeaderName("signatureHeader", ""); err == nil {
		t.Fatalf("an empty header name read as %q", got)
	}
	if got, err := signatureHeaderName("signatureHeader", "X-Sig"); err != nil || got != "x-sig" {
		t.Fatalf("X-Sig = %q, %v", got, err)
	}
}

// A signed timestamp is Unix time in the declared unit, in canonical
// digits, and nothing else: the value's size never picks the unit.
func TestWebhookTimestampReadsTheDeclaredUnit(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		in, unit string
		want     time.Time
	}{
		{"1791720000", "seconds", at},
		{"1791720000000", "milliseconds", at},
		{"1791720000250", "milliseconds", at.Add(250 * time.Millisecond)},
		{"1791720000000", "seconds", time.Unix(1791720000000, 0).UTC()},
		{"1791720000", "milliseconds", time.UnixMilli(1791720000).UTC()},
	} {
		got, ok := parseWebhookTimestamp(c.in, c.unit)
		if !ok || !got.Equal(c.want) {
			t.Errorf("%s in %s = %v, %v, want %v", c.in, c.unit, got, ok, c.want)
		}
	}
	for _, in := range []string{"", "-1791720000", "1791720000.5", "2026-10-11T12:00:00Z", "12345678901234567", " 1791720000", "01791720000", "0"} {
		if got, ok := parseWebhookTimestamp(in, "seconds"); ok {
			t.Errorf("%q read as %v", in, got)
		}
	}
	if got, ok := parseWebhookTimestamp("1791720000", ""); ok {
		t.Errorf("a timestamp with no unit read as %v", got)
	}
}

// A captured request cannot be re-split into another one under the same
// MAC. With `{timestamp}{body}`, the header `1791720000` and the body
// `123456` sign the same bytes as `1791720000123` and `456`; with
// `{body}{timestamp}`, the body `1230` and `1791720000` sign the same bytes
// as `123` and `01791720000`. The declared unit and the canonical digits
// leave exactly one split of each pair inside maxSkew.
func TestWebhookSignatureMessageSplitsOneWay(t *testing.T) {
	t.Parallel()
	at := time.Unix(1791720000, 0).UTC()
	for _, c := range []struct {
		name, message, unit string
		admitted, resplit   [2]string // timestamp header, body
	}{
		{
			"seconds, timestamp first", "{timestamp}{body}", "seconds",
			[2]string{"1791720000", "123456"},
			[2]string{"1791720000123", "456"},
		},
		{
			"milliseconds, timestamp first", "{timestamp}{body}", "milliseconds",
			[2]string{"1791720000123", "456"},
			[2]string{"1791720000", "123456"},
		},
		{
			"seconds, body first", "{body}{timestamp}", "seconds",
			[2]string{"1791720000", "1230"},
			[2]string{"01791720000", "123"},
		},
	} {
		sig, err := parseWebhookSignature(map[string]any{
			"algorithm": "hmac-sha256", "signatureHeader": "x-sig", "timestampHeader": "x-ts",
			"timestampUnit": c.unit, "message": c.message, "encoding": "hex", "secret": "oauthClientSecret",
		})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		mac := hex.EncodeToString(sig.expected("k", c.admitted[0], []byte(c.admitted[1])))
		if forged := hex.EncodeToString(sig.expected("k", c.resplit[0], []byte(c.resplit[1]))); forged != mac {
			t.Fatalf("%s: the pair signs different bytes, so it tests nothing", c.name)
		}
		request := func(split [2]string) substrate.WebhookRequest {
			return substrate.WebhookRequest{Headers: map[string]string{"x-sig": mac, "x-ts": split[0]}, Body: []byte(split[1])}
		}
		if _, reason := sig.presented(request(c.admitted), at); reason != "" {
			t.Errorf("%s: the signed request was refused: %s", c.name, reason)
		}
		if _, reason := sig.presented(request(c.resplit), at); reason == "" {
			t.Errorf("%s: the re-split request %q + %q was admitted under the same MAC", c.name, c.resplit[0], c.resplit[1])
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

// The door's replay record holds an admitted signature until its window
// closes: a repeat reads the first admission's fire, or nothing while that
// one is in flight; a released claim is forgotten; a closed window admits
// again; and at the bound the entry closest to closing goes first.
func TestWebhookReplaysHoldASignatureForItsWindow(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	var r webhookReplays
	key := webhookReplayKey("hook", []byte("mac-one"))

	c, _ := r.claim(key, now.Add(time.Minute), now)
	if c == nil {
		t.Fatal("the first claim was refused")
	}
	if again, fire := r.claim(key, now.Add(time.Minute), now); again != nil || fire != "" {
		t.Fatalf("a repeat in flight = %v, %q, want held with no fire yet", again, fire)
	}
	r.admitted(c, "hook-first")
	if again, fire := r.claim(key, now.Add(time.Minute), now.Add(time.Minute)); again != nil || fire != "hook-first" {
		t.Fatalf("a repeat at the window's last instant = %v, %q, want the first fire", again, fire)
	}
	if again, _ := r.claim(webhookReplayKey("other-hook", []byte("mac-one")), now.Add(time.Minute), now); again == nil {
		t.Fatal("the same MAC under another trigger was held")
	}
	if again, _ := r.claim(key, now.Add(3*time.Minute), now.Add(2*time.Minute)); again == nil {
		t.Fatal("a repeat after the window closed was held")
	}

	released := webhookReplayKey("hook", []byte("mac-two"))
	c, _ = r.claim(released, now.Add(time.Minute), now)
	r.release(released, c)
	if again, _ := r.claim(released, now.Add(time.Minute), now); again == nil {
		t.Fatal("a released claim was still held")
	}

	var bounded webhookReplays
	soonest := webhookReplayKey("hook", []byte("closes-first"))
	bounded.claim(soonest, now.Add(time.Second), now)
	for i := range maxWebhookReplays - 1 {
		bounded.claim(webhookReplayKey("hook", []byte{byte(i), byte(i >> 8), 'x'}), now.Add(time.Hour), now)
	}
	bounded.claim(webhookReplayKey("hook", []byte("one-past")), now.Add(time.Hour), now)
	if len(bounded.seen) != maxWebhookReplays {
		t.Fatalf("the record holds %d, want the bound %d", len(bounded.seen), maxWebhookReplays)
	}
	if _, held := bounded.seen[soonest]; held {
		t.Fatal("the entry closest to closing survived the bound")
	}
}

// rfc4231Case2 is RFC 4231's HMAC-SHA-256 test case 2: the key "Jefe" over
// "what do ya want for nothing?". stampedVector is the same key over
// "1791720000." and that data, computed with `openssl dgst -sha256 -hmac`.
const (
	rfc4231Case2       = "5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843"
	rfc4231Case2Base64 = "W9zBRr9gdU5qBCQmCJV1x1oAPwidJzmDnexYuWTsOEM="
	stampedVector      = "kAnNc/Ehuxxya6XNAqkxMt9aA1zXryYgdzzQ+t7NX0Y="
	rfc4231Data        = "what do ya want for nothing?"
)

// The door computes standard HMAC-SHA256 over the declared message, pinned
// to vectors from outside this package rather than to its own recipe: RFC
// 4231 as a body-only message and split across a literal and the body, and
// an openssl vector over a signed timestamp. It reads the signature header
// as a provider sends it, hex in either case or base64.
func TestWebhookSignatureMatchesKnownVectors(t *testing.T) {
	t.Parallel()
	at := time.Unix(1791720000, 0).UTC()
	for _, c := range []struct {
		name, message, encoding, header, stamp, body string
	}{
		{"a body-only message, lowercase hex", "{body}", "hex", rfc4231Case2, "", rfc4231Data},
		{"a body-only message, uppercase hex", "{body}", "hex", strings.ToUpper(rfc4231Case2), "", rfc4231Data},
		{"a body-only message, base64", "{body}", "base64", rfc4231Case2Base64, "", rfc4231Data},
		{"a literal and the body", "what do ya want {body}", "hex", rfc4231Case2, "", "for nothing?"},
		{"a signed timestamp", "{timestamp}.{body}", "base64", stampedVector, "1791720000", rfc4231Data},
	} {
		decl := map[string]any{
			"algorithm": "hmac-sha256", "signatureHeader": "x-sig", "message": c.message,
			"encoding": c.encoding, "secret": "oauthClientSecret",
		}
		req := substrate.WebhookRequest{Headers: map[string]string{"x-sig": c.header}, Body: []byte(c.body)}
		if c.stamp != "" {
			decl["timestampHeader"] = "x-ts"
			decl["timestampUnit"] = "seconds"
			req.Headers["x-ts"] = c.stamp
		}
		sig, err := parseWebhookSignature(decl)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		p, reason := sig.presented(req, at)
		if reason != "" {
			t.Fatalf("%s: refused before the key: %s", c.name, reason)
		}
		if !hmac.Equal(sig.expected("Jefe", p.stamp, req.Body), p.mac) {
			t.Fatalf("%s: computed %s, the vector is %s", c.name, hex.EncodeToString(sig.expected("Jefe", p.stamp, req.Body)), c.header)
		}
	}
}

// The checks that need no key, at their edges: a timestamp exactly maxSkew
// from the receipt passes on either side and one millisecond further is
// stale, and a signature that decodes to other than 32 bytes is refused.
func TestWebhookSignaturePresentedAtItsEdges(t *testing.T) {
	t.Parallel()
	sig, err := parseWebhookSignature(map[string]any{
		"algorithm": "hmac-sha256", "signatureHeader": "x-sig", "timestampHeader": "x-ts", "timestampUnit": "milliseconds",
		"maxSkew": "PT5M", "message": "{timestamp}{body}", "encoding": "hex", "secret": "oauthClientSecret",
	})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	request := func(signedAt time.Time, mac string) substrate.WebhookRequest {
		return substrate.WebhookRequest{Headers: map[string]string{
			"x-sig": mac, "x-ts": strconv.FormatInt(signedAt.UnixMilli(), 10),
		}}
	}
	for _, c := range []struct {
		name     string
		signedAt time.Time
		mac      string
		want     string
	}{
		{"exactly maxSkew before", at.Add(-5 * time.Minute), rfc4231Case2, ""},
		{"exactly maxSkew after", at.Add(5 * time.Minute), rfc4231Case2, ""},
		{"a millisecond past maxSkew before", at.Add(-5*time.Minute - time.Millisecond), rfc4231Case2, signatureRefusedStale},
		{"a millisecond past maxSkew after", at.Add(5*time.Minute + time.Millisecond), rfc4231Case2, signatureRefusedStale},
		{"31 bytes", at, rfc4231Case2[:62], signatureRefusedInvalid},
		{"33 bytes", at, rfc4231Case2 + "00", signatureRefusedInvalid},
		{"an odd hex digit", at, rfc4231Case2 + "0", signatureRefusedInvalid},
	} {
		if _, got := sig.presented(request(c.signedAt, c.mac), at); got != c.want {
			t.Errorf("%s: refusal %q, want %q", c.name, got, c.want)
		}
	}
}

// The line for an unresolvable signing secret carries a fixed reason and
// nothing else the resolution met. A reason built from a secret's value (the
// type cannot stop a conversion) logs as "unknown", and the error the
// resolution wrapped never reaches the line at all.
func TestUnresolvedSigningKeyLogCarriesNoSecret(t *testing.T) {
	t.Parallel()
	const secret = "whsec_SENTINEL_9f8e7d"
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	for _, why := range []signingKeyUnresolved{
		signingKeyNoOAuth2, signingKeyNoOAuthClient, signingKeyNoSecretRecord,
		signingKeySecretUnreadable, signingKeySecretEmpty,
		signingKeyUnresolved("the value " + secret + " does not open"),
	} {
		wrapped := fmt.Errorf("%w: %s", why, secret)
		var got signingKeyUnresolved
		if !errors.As(wrapped, &got) || !errors.Is(wrapped, errSigningKeyUnresolved) {
			t.Fatalf("%q does not read back as an unresolved signing key", wrapped)
		}
		logUnresolvedSigningKey(log, "geoah.example.com", "hook-signed", got, 0)
	}
	if strings.Contains(buf.String(), secret) {
		t.Fatalf("the secret reached the log:\n%s", buf.String())
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 6 {
		t.Fatalf("logged %d lines, want 6:\n%s", len(lines), buf.String())
	}
	if !strings.Contains(lines[2], `reason="the secret record does not exist"`) {
		t.Fatalf("a declared reason logged as %q", lines[2])
	}
	if !strings.Contains(lines[5], "reason=unknown") {
		t.Fatalf("a reason built from a value logged as %q, want unknown", lines[5])
	}
}

// The door reuses a trigger's resolved signing key for webhookKeyTTL, under
// the trigger version it was resolved for: a later read inside the window
// hits, one at its end misses, and one for an edited trigger misses at once.
func TestWebhookKeyCacheHoldsAKeyForItsWindow(t *testing.T) {
	var c webhookKeyCache
	now := time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)
	if _, ok := c.get("t", 3, now); ok {
		t.Fatal("an empty cache answered")
	}
	c.put("t", 3, "k1", now)
	if key, ok := c.get("t", 3, now.Add(webhookKeyTTL-time.Millisecond)); !ok || key != "k1" {
		t.Fatalf("inside the window: %q, %v", key, ok)
	}
	if _, ok := c.get("t", 3, now.Add(webhookKeyTTL)); ok {
		t.Fatal("the key outlived its window")
	}
	if _, ok := c.get("t", 4, now); ok {
		t.Fatal("an edited trigger read the key resolved for its old version")
	}
	c.put("u", 1, "k2", now.Add(webhookKeyTTL))
	if _, has := c.keys["t"]; has {
		t.Fatal("an expired entry outlived the next put")
	}
}
