package engine

// A webhook trigger's declared provider signature (decision 0164): the door
// checks it before the request becomes a blob or a ledger entry, so a request
// the provider did not sign leaves nothing behind, and the secret it is
// checked against stays in the host. A callable verifies only what the door
// cannot (decision 0045's rule, narrowed).

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	// webhookSignatureHMACSHA256 is the one algorithm the door computes.
	webhookSignatureHMACSHA256 = "hmac-sha256"
	// webhookSecretOAuthClient names the client secret of the callable's
	// bundle's OAuth client input as the signing key: the value the OAuth
	// facility already holds, which no function is handed.
	webhookSecretOAuthClient = "oauthClientSecret"
	// defaultWebhookMaxSkew is how far a signed timestamp may sit from the
	// door's receipt time when the trigger names no maxSkew.
	defaultWebhookMaxSkew = 5 * time.Minute
	// maxWebhookMaxSkew bounds a declared maxSkew: past an hour the
	// timestamp no longer stops a captured request from being replayed.
	maxWebhookMaxSkew = time.Hour
	// webhookTimestampMillisFloor is the smallest timestamp read as
	// milliseconds: 10^11 seconds is the year 5138 and 10^11 milliseconds is
	// 1973, so the two units cannot be confused for a real request.
	webhookTimestampMillisFloor = 100_000_000_000
)

// webhookSignatureKeys is the signature block's closed key set.
var webhookSignatureKeys = map[string]bool{
	"algorithm": true, "signatureHeader": true, "timestampHeader": true, "maxSkew": true,
	"message": true, "encoding": true, "secret": true,
}

// webhookSignature is one parsed `source.webhook.signature`.
type webhookSignature struct {
	// Header and TimestampHeader are lowercased, as the door's names are.
	// TimestampHeader is empty when the provider signs no timestamp.
	Header          string
	TimestampHeader string
	MaxSkew         time.Duration
	// Message is the signed bytes' recipe: literal text, the timestamp
	// header's value and the body, in the declared order.
	Message []signedPart
	// Encoding is how the signature header spells the MAC: hex or base64.
	Encoding string
	// OAuthClient names the callable's bundle's OAuth client secret as the
	// key; otherwise SecretRecord is the id of the core `secret` record
	// holding it.
	OAuthClient  bool
	SecretRecord string
}

// signedPart is one piece of the signed message: a literal, or a field.
type signedPart struct {
	literal string
	field   string // "", "timestamp" or "body"
}

// parseWebhookSignature reads `source.webhook.signature`, or nil when the arm
// declares none. Every field the door cannot act on is refused here, so a
// trigger that lands is one the door can check.
func parseWebhookSignature(raw any) (*webhookSignature, error) {
	if raw == nil {
		return nil, nil
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("source.webhook.signature: a map of algorithm, signatureHeader, timestampHeader, maxSkew, message, encoding and secret")
	}
	for k := range m {
		if !webhookSignatureKeys[k] {
			return nil, fmt.Errorf("source.webhook.signature: unknown key %q", k)
		}
	}
	str := func(name string) (string, error) {
		v, has := m[name]
		if !has || v == nil {
			return "", nil
		}
		s, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("source.webhook.signature.%s: a string, got %T", name, v)
		}
		return s, nil
	}
	sig := &webhookSignature{MaxSkew: defaultWebhookMaxSkew}

	algorithm, err := str("algorithm")
	if err != nil {
		return nil, err
	}
	if algorithm != webhookSignatureHMACSHA256 {
		return nil, fmt.Errorf("source.webhook.signature.algorithm: %q is the one algorithm, got %q", webhookSignatureHMACSHA256, algorithm)
	}

	header, err := str("signatureHeader")
	if err != nil {
		return nil, err
	}
	if header == "" {
		return nil, fmt.Errorf("source.webhook.signature.signatureHeader: required, the header the provider puts the signature in")
	}
	if sig.Header, err = signatureHeaderName("signatureHeader", header); err != nil {
		return nil, err
	}

	stamp, err := str("timestampHeader")
	if err != nil {
		return nil, err
	}
	if stamp != "" {
		if sig.TimestampHeader, err = signatureHeaderName("timestampHeader", stamp); err != nil {
			return nil, err
		}
		if sig.TimestampHeader == sig.Header {
			return nil, fmt.Errorf("source.webhook.signature.timestampHeader: the same header as signatureHeader")
		}
	}

	skew, err := str("maxSkew")
	if err != nil {
		return nil, err
	}
	if skew != "" {
		if sig.TimestampHeader == "" {
			return nil, fmt.Errorf("source.webhook.signature.maxSkew: needs a timestampHeader to measure")
		}
		d, err := vocabulary.ParseISODuration(skew)
		if err != nil {
			return nil, fmt.Errorf("source.webhook.signature.maxSkew: %w", err)
		}
		if d < time.Second || d > maxWebhookMaxSkew {
			return nil, fmt.Errorf("source.webhook.signature.maxSkew: between PT1S and PT1H, got %s", skew)
		}
		sig.MaxSkew = d
	}

	message, err := str("message")
	if err != nil {
		return nil, err
	}
	if message == "" {
		message = "{body}"
	}
	if sig.Message, err = parseSignedMessage(message); err != nil {
		return nil, err
	}
	signsStamp := false
	for _, p := range sig.Message {
		if p.field == "timestamp" {
			signsStamp = true
		}
	}
	// A timestamp outside the MAC proves nothing: a replayer rewrites it.
	if signsStamp != (sig.TimestampHeader != "") {
		return nil, fmt.Errorf("source.webhook.signature.message: names {timestamp} exactly when a timestampHeader is declared, so the timestamp the door checks is one the provider signed")
	}

	encoding, err := str("encoding")
	if err != nil {
		return nil, err
	}
	if encoding != "hex" && encoding != "base64" {
		return nil, fmt.Errorf("source.webhook.signature.encoding: hex or base64, got %q", encoding)
	}
	sig.Encoding = encoding

	secret, err := str("secret")
	if err != nil {
		return nil, err
	}
	switch id, isRecord := strings.CutPrefix(secret, kindSecret+"/"); {
	case secret == webhookSecretOAuthClient:
		sig.OAuthClient = true
	case isRecord && id != "":
		sig.SecretRecord = id
	default:
		return nil, fmt.Errorf("source.webhook.signature.secret: %s/<id> or %s, got %q", kindSecret, webhookSecretOAuthClient, secret)
	}
	return sig, nil
}

// signatureHeaderName holds a signature header to the header-name alphabet and
// lowercases it. A credential header the door strips before the service sees
// a request could never be read, so naming one is refused.
func signatureHeaderName(field, name string) (string, error) {
	if name == "" {
		return "", nil
	}
	if !webhookHeaderPattern.MatchString(name) {
		return "", fmt.Errorf("source.webhook.signature.%s: a header name, 1 to 64 characters of [A-Za-z0-9-]", field)
	}
	lower := strings.ToLower(name)
	if webhookUndeliverableHeaders[lower] {
		return "", fmt.Errorf("source.webhook.signature.%s: %q never reaches the door's check", field, lower)
	}
	return lower, nil
}

// parseSignedMessage reads the message template: literal text with `{body}`
// exactly once and `{timestamp}` at most once. A brace that opens no known
// field is refused rather than read as a literal, so a typo cannot sign the
// wrong bytes.
func parseSignedMessage(s string) ([]signedPart, error) {
	var parts []signedPart
	bodies, stamps := 0, 0
	var lit strings.Builder
	for i := 0; i < len(s); {
		switch s[i] {
		case '{':
			end := strings.IndexByte(s[i:], '}')
			if end < 0 {
				return nil, fmt.Errorf("source.webhook.signature.message: an unclosed {")
			}
			field := s[i+1 : i+end]
			switch field {
			case "body":
				bodies++
			case "timestamp":
				stamps++
			default:
				return nil, fmt.Errorf("source.webhook.signature.message: {%s} is not a field; {timestamp} and {body} are", field)
			}
			if lit.Len() > 0 {
				parts = append(parts, signedPart{literal: lit.String()})
				lit.Reset()
			}
			parts = append(parts, signedPart{field: field})
			i += end + 1
		case '}':
			return nil, fmt.Errorf("source.webhook.signature.message: a } that closes nothing")
		default:
			lit.WriteByte(s[i])
			i++
		}
	}
	if lit.Len() > 0 {
		parts = append(parts, signedPart{literal: lit.String()})
	}
	if bodies != 1 || stamps > 1 {
		return nil, fmt.Errorf("source.webhook.signature.message: {body} exactly once and {timestamp} at most once")
	}
	return parts, nil
}

// checkWebhookSecretSource refuses, at write time, a trigger whose signature
// names the OAuth client secret when its callable's package is no bundle with
// an `oauth2` block: there is no client secret to resolve, and every request
// would be refused.
func checkWebhookSecretSource(reg *vocabulary.Registry, t *trigger, pkg string) error {
	if t.WebhookSignature == nil || !t.WebhookSignature.OAuthClient {
		return nil
	}
	if b, ok := reg.BundleOf(pkg); ok && b.OAuth2 != nil {
		return nil
	}
	return fmt.Errorf("source.webhook.signature.secret: %s names the OAuth client of the callable's bundle, and %s declares no oauth2 block",
		webhookSecretOAuthClient, pkg)
}

// errWebhookSignature is the door's answer to a request its trigger's
// signature check refuses: a missing or wrong signature, a stale or missing
// timestamp, a multipart body, or a key the host cannot resolve. One terse
// answer, because the sender is unauthenticated; the reason is logged for an
// unresolvable key alone, which is the operator's to fix.
var errWebhookSignature = fmt.Errorf("%w: webhook signature refused", substrate.ErrAuth)

// The fixed words a refusal's reason is, wrapped into errWebhookSignature
// for tests and never sent to the sender.
const (
	signatureRefusedMissing   = "no signature"
	signatureRefusedInvalid   = "signature does not match"
	signatureRefusedTimestamp = "no readable timestamp"
	signatureRefusedStale     = "timestamp outside maxSkew"
	signatureRefusedMultipart = "multipart body"
	signatureRefusedSecret    = "signing secret unresolvable"
)

func refuseSignature(reason string) error {
	return fmt.Errorf("%w (%s)", errWebhookSignature, reason)
}

// checkWebhookSignature verifies the request against the trigger's declared
// signature, measuring the timestamp against at, the door's receipt time. A
// trigger declaring none passes. The MAC compare is constant-time.
func (ds *dataset) checkWebhookSignature(ctx context.Context, tr *trigger, req substrate.WebhookRequest, at time.Time) error {
	sig := tr.WebhookSignature
	if sig == nil {
		return nil
	}
	// The door parses a multipart body into parts and keeps no raw bytes,
	// so there is nothing byte-exact to compute a MAC over.
	if req.Parts != nil {
		return refuseSignature(signatureRefusedMultipart)
	}
	provided := strings.TrimSpace(req.Headers[sig.Header])
	if provided == "" {
		return refuseSignature(signatureRefusedMissing)
	}
	var stamp string
	if sig.TimestampHeader != "" {
		stamp = strings.TrimSpace(req.Headers[sig.TimestampHeader])
		signedAt, ok := parseWebhookTimestamp(stamp)
		if !ok {
			return refuseSignature(signatureRefusedTimestamp)
		}
		if at.Sub(signedAt).Abs() > sig.MaxSkew {
			return refuseSignature(signatureRefusedStale)
		}
	}
	var got []byte
	var err error
	if sig.Encoding == "hex" {
		got, err = hex.DecodeString(provided)
	} else {
		got, err = base64.StdEncoding.DecodeString(provided)
	}
	if err != nil || len(got) != sha256.Size {
		return refuseSignature(signatureRefusedInvalid)
	}
	key, err := ds.webhookSigningKey(ctx, tr)
	if err != nil {
		ds.svc.log.Warn("substrate: webhook refused, its trigger's signing secret does not resolve",
			"repository", ds.Repository().ID, "trigger", logSafeID(tr.ID), "error", err)
		return refuseSignature(signatureRefusedSecret)
	}
	mac := hmac.New(sha256.New, []byte(key))
	for _, p := range sig.Message {
		switch p.field {
		case "body":
			mac.Write(req.Body)
		case "timestamp":
			mac.Write([]byte(stamp))
		default:
			mac.Write([]byte(p.literal))
		}
	}
	if !hmac.Equal(mac.Sum(nil), got) {
		return refuseSignature(signatureRefusedInvalid)
	}
	return nil
}

// parseWebhookTimestamp reads a signed timestamp: decimal Unix time, in
// seconds, or in milliseconds from webhookTimestampMillisFloor up.
func parseWebhookTimestamp(s string) (time.Time, bool) {
	if s == "" || len(s) > 16 {
		return time.Time{}, false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return time.Time{}, false
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	if n >= webhookTimestampMillisFloor {
		return time.UnixMilli(n).UTC(), true
	}
	return time.Unix(n, 0).UTC(), true
}

// webhookSigningKey resolves the trigger's signing secret inside the host: the
// OAuth client secret of the callable's bundle, or the value of a core
// `secret` record. Neither is handed to the callable by this path.
func (ds *dataset) webhookSigningKey(ctx context.Context, tr *trigger) (string, error) {
	sig := tr.WebhookSignature
	if sig.OAuthClient {
		pkg := ""
		switch {
		case tr.Callable != nil:
			pkg = tr.Callable.Package
		case tr.Agent != nil:
			pkg = tr.Agent.Package
		}
		b, ok := ds.registry().BundleOf(pkg)
		if !ok || b.OAuth2 == nil {
			return "", fmt.Errorf("the callable's package %s declares no oauth2 block", pkg)
		}
		_, secret, err := ds.oauthClientOf(ctx, b)
		return secret, err
	}
	row, err := ds.loadRowDB(ctx, eref{Kind: kindSecret, ID: sig.SecretRecord})
	if err != nil {
		return "", err
	}
	if row == nil || row.DeletedAt != nil {
		return "", fmt.Errorf("no secret record %s", logSafeID(sig.SecretRecord))
	}
	value, err := ds.openSecretValue(ctx, propString(row, propSettingValue))
	if err != nil {
		return "", err
	}
	if value == "" {
		return "", errors.New("the secret record holds no value")
	}
	return value, nil
}
