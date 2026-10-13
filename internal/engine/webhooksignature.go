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
	"sync"
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
	// webhookSecretWarnEvery is how often one trigger's unresolvable signing
	// secret is logged. Any sender reaches that check with a fresh timestamp
	// and 32 random bytes, so a line per request would let it fill the log.
	webhookSecretWarnEvery = time.Minute
	// maxWebhookReplays bounds the signatures one repository's door
	// remembers. Past it the one closest to leaving its window is forgotten
	// early, which reopens that one request to a replay.
	maxWebhookReplays = 4096
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

// errWebhookSignature is the door's answer to a request carrying its keyed
// trigger's key that the trigger's signature check refuses: a missing or
// wrong signature, a stale or missing timestamp, a multipart body, or a
// signing key the trigger's configuration does not supply. One terse answer,
// because the sender is unauthenticated; the reason is logged for an
// unresolvable signing key alone, which is the operator's to fix. A store
// that fails to answer while the key is read is no refusal: it reaches the
// caller as itself, a 5xx at the door and a retry on delivery.
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
// trigger declaring none passes with a zero presentedSignature. A refusal is
// errWebhookSignature only when the trigger carries a key, which the request
// has already matched; on a keyless trigger it is the door's one
// errWebhookRefused, because a distinct answer would tell anyone who posts
// that the repository exists and has the trigger installed.
func (ds *dataset) checkWebhookSignature(ctx context.Context, tr *trigger, req substrate.WebhookRequest, at time.Time) (presentedSignature, error) {
	p, reason, err := ds.webhookSignatureRefusal(ctx, tr, req, at)
	if err != nil {
		return presentedSignature{}, err
	}
	if reason == "" {
		return p, nil
	}
	if tr.WebhookKey == "" {
		return presentedSignature{}, fmt.Errorf("%w (signature refused: %s)", errWebhookRefused, reason)
	}
	return presentedSignature{}, refuseSignature(reason)
}

// webhookSignatureRefusal is the reason the trigger's declared signature
// refuses the request received at at, one of the signatureRefused words, or
// "" when it verifies or none is declared, with what the request presented.
// The MAC compare is constant-time. err is a store fault met while resolving
// the key, which decides nothing about the request.
func (ds *dataset) webhookSignatureRefusal(ctx context.Context, tr *trigger, req substrate.WebhookRequest, at time.Time) (presentedSignature, string, error) {
	sig := tr.WebhookSignature
	if sig == nil {
		return presentedSignature{}, "", nil
	}
	p, reason := sig.presented(req, at)
	if reason != "" {
		return p, reason, nil
	}
	key, err := ds.webhookSigningKey(ctx, tr)
	if errors.Is(err, errSigningKeyUnresolved) {
		if due, unlogged := ds.webhookSecretWarnings.due(tr.ID, ds.svc.webhookNow()); due {
			ds.svc.log.Warn("substrate: webhook refused, its trigger's signing secret does not resolve",
				"repository", ds.Repository().ID, "trigger", logSafeID(tr.ID), "error", err, "unlogged", unlogged)
		}
		return p, signatureRefusedSecret, nil
	}
	if err != nil {
		return p, "", fmt.Errorf("substrate: webhook %s: resolve the signing secret: %w", logSafeID(tr.ID), err)
	}
	if !hmac.Equal(sig.expected(key, p.stamp, req.Body), p.mac) {
		return p, signatureRefusedInvalid, nil
	}
	return p, "", nil
}

// presentedSignature is what a request presents to its trigger's declared
// signature: the decoded MAC, the timestamp header's value as sent, and the
// instant it names, zero when the declaration signs no timestamp.
type presentedSignature struct {
	mac      []byte
	stamp    string
	signedAt time.Time
}

// presented reads the request's signature under the declaration, received
// at at, or names the refusal: every check that needs no key, so a request
// that fails one costs no key lookup.
func (sig *webhookSignature) presented(req substrate.WebhookRequest, at time.Time) (presentedSignature, string) {
	var p presentedSignature
	// The door parses a multipart body into parts and keeps no raw bytes,
	// so there is nothing byte-exact to compute a MAC over.
	if req.Parts != nil {
		return p, signatureRefusedMultipart
	}
	provided := strings.TrimSpace(req.Headers[sig.Header])
	if provided == "" {
		return p, signatureRefusedMissing
	}
	if sig.TimestampHeader != "" {
		p.stamp = strings.TrimSpace(req.Headers[sig.TimestampHeader])
		signedAt, ok := parseWebhookTimestamp(p.stamp)
		if !ok {
			return p, signatureRefusedTimestamp
		}
		if at.Sub(signedAt).Abs() > sig.MaxSkew {
			return p, signatureRefusedStale
		}
		p.signedAt = signedAt
	}
	var err error
	if sig.Encoding == "hex" {
		p.mac, err = hex.DecodeString(provided)
	} else {
		p.mac, err = base64.StdEncoding.DecodeString(provided)
	}
	if err != nil || len(p.mac) != sha256.Size {
		return p, signatureRefusedInvalid
	}
	return p, ""
}

// expected is the MAC the provider computes under key over the declared
// message: its literals, the timestamp header's value as sent and the body.
func (sig *webhookSignature) expected(key, stamp string, body []byte) []byte {
	mac := hmac.New(sha256.New, []byte(key))
	for _, part := range sig.Message {
		switch part.field {
		case "body":
			mac.Write(body)
		case "timestamp":
			mac.Write([]byte(stamp))
		default:
			mac.Write([]byte(part.literal))
		}
	}
	return mac.Sum(nil)
}

// webhookSecretWarnings paces the warning for a trigger whose signing secret
// does not resolve to one line per trigger per webhookSecretWarnEvery, each
// line counting the refusals since the last one that went unlogged.
type webhookSecretWarnings struct {
	mu       sync.Mutex
	last     map[string]time.Time
	unlogged map[string]int
}

// due reports whether the trigger's refusal at now is logged, and how many
// of its refusals since its last line were not.
func (w *webhookSecretWarnings) due(triggerID string, now time.Time) (bool, int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.last == nil {
		w.last, w.unlogged = map[string]time.Time{}, map[string]int{}
	}
	if last, ok := w.last[triggerID]; ok && now.Sub(last) < webhookSecretWarnEvery {
		w.unlogged[triggerID]++
		return false, 0
	}
	n := w.unlogged[triggerID]
	w.last[triggerID] = now
	delete(w.unlogged, triggerID)
	return true, n
}

// webhookReplays is the door's memory of the signatures it admitted under a
// declaration that signs a timestamp, each kept until that timestamp leaves
// maxSkew, after which the door refuses the request as stale anyway. A
// repeat inside the window is the same delivery: a captured request posted
// again, or a provider resending one whose answer it lost. Memory only, so a
// restart forgets it; a body-only declaration has no window to bound it by
// and is never recorded.
type webhookReplays struct {
	mu   sync.Mutex
	seen map[string]*webhookReplay
}

// webhookReplay is one admitted signature: the fire it became, empty while
// the admission that claimed it is still storing the request, and the
// instant its timestamp leaves maxSkew.
type webhookReplay struct {
	fire  string
	until time.Time
}

// webhookReplayKey names one signature under one trigger.
func webhookReplayKey(triggerID string, mac []byte) string {
	return triggerID + "\x00" + string(mac)
}

// claim records key as admitted until until, received at now, and returns
// the claim; or, when an admission inside its window already holds key,
// returns nil with that admission's fire id, empty while it is in flight.
func (r *webhookReplays) claim(key string, until, now time.Time) (*webhookReplay, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if held, ok := r.seen[key]; ok && !now.After(held.until) {
		return nil, held.fire
	}
	if r.seen == nil {
		r.seen = map[string]*webhookReplay{}
	}
	if len(r.seen) >= maxWebhookReplays {
		r.evictLocked(now)
	}
	c := &webhookReplay{until: until}
	r.seen[key] = c
	return c, ""
}

// evictLocked drops every entry whose window has closed and, when none has,
// the one closest to closing.
func (r *webhookReplays) evictLocked(now time.Time) {
	soonest := ""
	for k, e := range r.seen {
		if now.After(e.until) {
			delete(r.seen, k)
			continue
		}
		if soonest == "" || e.until.Before(r.seen[soonest].until) {
			soonest = k
		}
	}
	if len(r.seen) >= maxWebhookReplays {
		delete(r.seen, soonest)
	}
}

// admitted names the fire a claim became, once its request is stored.
func (r *webhookReplays) admitted(c *webhookReplay, fire string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c.fire = fire
}

// release forgets a claim whose request was not stored, so the sender's
// next try is admitted.
func (r *webhookReplays) release(key string, c *webhookReplay) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seen[key] == c {
		delete(r.seen, key)
	}
}

// errWebhookReplayInFlight answers a repeat of a signature whose first
// admission is still storing the request: 503, so the sender retries and
// then reads the first one's fire id.
var errWebhookReplayInFlight = fmt.Errorf("%w: the same signed webhook request is being recorded, retry", substrate.ErrUnavailable)

// webhookRepeat is admitWebhook's answer to a repeat of an admitted
// signature: the fire the first admission became, which the door answers
// again with nothing stored.
type webhookRepeat struct{ fire string }

func (r webhookRepeat) Error() string { return "webhook repeats the admitted request " + r.fire }

// requestSignatureVerified is the stored request's key saying the door
// verified the trigger's declared signature before it stored the request.
// The door alone writes it, and only after the check passed: a sender's
// request becomes `method`, `contentType`, `headers`, `query` and `body` or
// `parts`, never a key of the request map itself.
const requestSignatureVerified = "signatureVerified"

// errWebhookUnverified parks a stored webhook request that the trigger's
// signature, declared after the door stored it, does not cover. It is
// deterministic: the stored bytes verify on no later attempt either.
var errWebhookUnverified = errors.New("webhook signature refused at delivery")

// checkStoredWebhookSignature holds a stored webhook request to the signature
// its trigger declares NOW, before the callable runs it: a resumed pending
// entry, a retry by hand and the rerun of an interrupted agent delivery all
// pass here. A request the door verified carries requestSignatureVerified
// and runs, however long after its timestamp. One stored while the trigger
// declared no signature is verified from its kept body and headers against
// the time the door received it (webhookReceivedAt), never the retry's
// clock, so a genuinely signed request stored before the upgrade still runs
// and a forged or unsigned one is refused. failure is the request's ledger
// row, whose id is the seq of the entry that admitted it. A wake carries no
// request and is authenticated already.
func (ds *dataset) checkStoredWebhookSignature(ctx context.Context, tr *trigger, envelope map[string]any, failure int64) error {
	if !tr.Webhook || tr.WebhookSignature == nil {
		return nil
	}
	req, _ := envelope["request"].(map[string]any)
	if req == nil {
		return nil
	}
	if verified, _ := req[requestSignatureVerified].(bool); verified {
		return nil
	}
	refuse := func(reason string) error {
		return fmt.Errorf("%w: the request was stored before its trigger declared source.webhook.signature, and its kept body and headers do not verify (%s)",
			errWebhookUnverified, reason)
	}
	fire, _ := envelope["fire"].(map[string]any)
	at, ok, err := ds.webhookReceivedAt(ctx, tr.ID, failure, fire)
	if err != nil {
		return err
	}
	if !ok {
		return refuse("no receipt time")
	}
	stored, err := ds.storedWebhookRequest(ctx, req)
	if err != nil {
		return err
	}
	_, reason, err := ds.webhookSignatureRefusal(ctx, tr, stored, at)
	if err != nil {
		return err
	}
	if reason != "" {
		return refuse(reason)
	}
	return nil
}

// webhookReceivedAt is the instant the door received a stored request, which
// a millisecond signature timestamp is measured against to the millisecond.
// `fire.at` carries it with its fraction of a second. An envelope stored
// before `fire.at` kept that fraction names only the whole second, so the
// receipt the door recorded in the request's admission entry (the pending
// row it folded, at the seq that is the row's id, under the same fire id)
// stands in for it when it falls inside that second. Without one, the
// receipt is the start of the named second (receiptOf).
func (ds *dataset) webhookReceivedAt(ctx context.Context, triggerID string, failure int64, fire map[string]any) (time.Time, bool, error) {
	stamp, _ := fire["at"].(string)
	fid, _ := fire["id"].(string)
	at, ok := receiptOf(stamp, time.Time{})
	if !ok || !at.Equal(at.Truncate(time.Second)) || failure <= 0 || fid == "" {
		return at, ok, nil
	}
	ch, err := ds.changeAt(ctx, failure)
	if errors.Is(err, substrate.ErrNotFound) {
		return at, true, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	ops, err := foldOpsOf(ch)
	if err != nil {
		return time.Time{}, false, err
	}
	for _, op := range ops {
		if op.Kind == foldPark && op.ID == triggerID && op.Failure != nil &&
			op.Failure.FireID == fid && op.Failure.LastError == pendingWebhookError {
			at, ok = receiptOf(stamp, op.Failure.ParkedAt)
			return at, ok, nil
		}
	}
	return at, true, nil
}

// receiptOf reads a stored `fire.at`. A stamp with a fraction of a second is
// the receipt itself. A whole-second stamp is recorded when recorded falls
// inside that second, and otherwise the start of the second: a stamp written
// before `fire.at` kept its fraction was truncated, never rounded, so the
// receipt was no earlier than that.
func receiptOf(stamp string, recorded time.Time) (time.Time, bool) {
	at, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		return time.Time{}, false
	}
	at = at.UTC()
	if at.Equal(at.Truncate(time.Second)) && !recorded.IsZero() && recorded.UTC().Truncate(time.Second).Equal(at) {
		return recorded.UTC(), true
	}
	return at, true
}

// storedWebhookRequest rebuilds what the signature covers from a stored
// request: its kept headers and its raw body, read back from the blob store
// when the entry references it. A multipart request comes back with its
// parts marked present and no body, which the check refuses.
func (ds *dataset) storedWebhookRequest(ctx context.Context, req map[string]any) (substrate.WebhookRequest, error) {
	out := substrate.WebhookRequest{Headers: map[string]string{}}
	if headers, ok := req["headers"].(map[string]any); ok {
		for name, v := range headers {
			if s, ok := v.(string); ok {
				out.Headers[strings.ToLower(name)] = s
			}
		}
	}
	if _, multipart := req["parts"]; multipart {
		out.Parts = []substrate.WebhookPart{}
		return out, nil
	}
	body, _ := req["body"].(map[string]any)
	switch {
	case body == nil:
	case body["blob"] != nil:
		digest, _ := body["blob"].(string)
		_, data, err := ds.GetBlob(ctx, digest)
		if err != nil {
			return out, fmt.Errorf("substrate: stored webhook body %s: %w", digest, err)
		}
		out.Body = data
	case body["base64"] != nil:
		enc, _ := body["base64"].(string)
		data, err := base64.StdEncoding.DecodeString(enc)
		if err != nil {
			return out, fmt.Errorf("substrate: stored webhook body: %w", err)
		}
		out.Body = data
	default:
		text, _ := body["text"].(string)
		out.Body = []byte(text)
	}
	return out, nil
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

// errSigningKeyUnresolved marks a signing key the trigger's configuration
// does not supply: no oauth2 block or client record, no secret record, an
// empty value, or a sealed value that does not open. The door refuses the
// request on it. Any other error from webhookSigningKey is the store failing
// to answer, which refuses nothing.
var errSigningKeyUnresolved = errors.New("signing secret unresolvable")

// webhookSigningKey resolves the trigger's signing secret inside the host: the
// OAuth client secret of the callable's bundle, or the value of a core
// `secret` record. Neither is handed to the callable by this path.
func (ds *dataset) webhookSigningKey(ctx context.Context, tr *trigger) (string, error) {
	unresolved := func(format string, args ...any) error {
		return fmt.Errorf("%w: "+format, append([]any{errSigningKeyUnresolved}, args...)...)
	}
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
			return "", unresolved("the callable's package %s declares no oauth2 block", pkg)
		}
		_, secret, err := ds.oauthClientOf(ctx, b)
		switch {
		case errors.Is(err, substrate.ErrGuard), errors.Is(err, substrate.ErrNotFound), errors.Is(err, errSecretUnreadable):
			return "", unresolved("%w", err)
		case err != nil:
			return "", err
		}
		return secret, nil
	}
	row, err := ds.loadRowDB(ctx, eref{Kind: kindSecret, ID: sig.SecretRecord})
	if err != nil {
		return "", err
	}
	if row == nil || row.DeletedAt != nil {
		return "", unresolved("no secret record %s", logSafeID(sig.SecretRecord))
	}
	value, err := ds.openSecretValue(ctx, propString(row, propSettingValue))
	switch {
	case errors.Is(err, errSecretUnreadable):
		return "", unresolved("%w", err)
	case err != nil:
		return "", err
	}
	if value == "" {
		return "", unresolved("the secret record holds no value")
	}
	return value, nil
}
