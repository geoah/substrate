// Package pushrelay is the publisher's push relay (docs/plans/push.md,
// section 1): it seals what an install registers into a push key and keeps
// nothing, accepts a push only when it is signed by the key sealed inside
// that push key, and builds every notification it hands Apple from an
// event, a thread, a count and an opaque id, so no caller chooses words.
package pushrelay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/geoah/substrate/internal/apns"
	"github.com/geoah/substrate/internal/peer"
	"github.com/geoah/substrate/internal/pushwire"
)

// Sender sends to one Apple environment; *apns.Client is one.
type Sender interface {
	Send(ctx context.Context, n apns.Notification) (apns.Response, error)
}

// Config is what the relay serves with.
type Config struct {
	// Topics are the bundle ids it pushes to.
	Topics []string
	// Apple holds a sender for each environment it holds a key for, and no
	// other.
	Apple map[string]Sender
	// Sealer seals and opens push keys.
	Sealer *Sealer
	// Limits are the allowances; DefaultLimits when zero.
	Limits Limits
	// TrustedProxies are the proxies whose X-Forwarded-For names the caller.
	TrustedProxies []netip.Prefix
	// Now is time.Now when nil.
	Now func() time.Time
	// Log is slog.Default() when nil.
	Log *slog.Logger
	// Metrics is a fresh set when nil.
	Metrics *Metrics
}

// Relay is the relay's HTTP handler.
type Relay struct {
	cfg    Config
	limits *limiter

	// admitting guards the replay window and the charge of the install and
	// total limits, so a signature is looked up, the limits are charged and
	// the signature is held as one step at one clock.
	admitting sync.Mutex
	replays   *replays

	ready atomic.Bool
}

// New makes a relay. It answers /readyz 503 until MarkReady. It refuses a
// total limit whose replay window would not fit MaxReplays.
func New(cfg Config) (*Relay, error) {
	if cfg.Sealer == nil || len(cfg.Topics) == 0 || len(cfg.Apple) == 0 {
		return nil, errors.New("pushrelay: a relay needs a seal key, a topic and an Apple key")
	}
	for env := range cfg.Apple {
		if !slices.Contains(pushwire.Environments, env) {
			return nil, errors.New("pushrelay: an Apple environment is production or development")
		}
	}
	if cfg.Limits == (Limits{}) {
		cfg.Limits = DefaultLimits
	}
	if cfg.Limits.Total > MaxTotal {
		return nil, errors.New("pushrelay: the total limit is over " + strconv.Itoa(MaxTotal) + " a minute, more than the replay window holds")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if cfg.Metrics == nil {
		cfg.Metrics = NewMetrics()
	}
	return &Relay{cfg: cfg, limits: newLimiter(cfg.Limits), replays: newReplays(ReplayCapacity(cfg.Limits.Total))}, nil
}

// MarkReady makes /readyz answer 200: every environment's key parsed and a
// provider token was minted for it.
func (rl *Relay) MarkReady() { rl.ready.Store(true) }

// The routes' names, in metrics and logs.
const (
	routeRegister = "register"
	routePush     = "push"
	routeOther    = "other"
)

// codeRegistered labels a register the relay answered with a push key.
const codeRegistered = "registered"

// ServeHTTP routes by hand, so every refusal, a wrong route's and a wrong
// method's included, is the one envelope. Every request but a health check
// is charged to its caller first.
func (rl *Relay) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/healthz":
		health(w, r, true)
		return
	case "/readyz":
		health(w, r, rl.ready.Load())
		return
	}
	call := &call{relay: rl, w: w, r: r, route: routeOther, now: rl.cfg.Now()}
	call.caller = peer.Host(peer.Address(r, rl.cfg.TrustedProxies))
	switch r.URL.Path {
	case pushwire.PathRegister:
		call.route = routeRegister
	case pushwire.PathPush:
		call.route = routePush
	}
	if ok, wait := rl.limits.take(limitCaller, callerKey(call.caller), call.now); !ok {
		call.callerLimited(limitCaller, wait)
		return
	}
	if call.route == routeOther {
		call.refuse(NotFound)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		call.refuse(MethodNotAllowed)
		return
	}
	body, ok := call.body()
	if !ok {
		return
	}
	if call.route == routeRegister {
		call.register(body)
	} else {
		call.push(body)
	}
}

// callerKey is what the per-caller limits key on: an IPv4 address, or the
// /64 an IPv6 address is in, since one network holds that many addresses
// and hands them out freely.
func callerKey(addr string) string {
	a, err := netip.ParseAddr(addr)
	if err != nil {
		return addr
	}
	a = a.Unmap().WithZone("")
	if a.Is4() {
		return a.String()
	}
	p, _ := a.Prefix(64)
	return p.String()
}

func health(w http.ResponseWriter, r *http.Request, ok bool) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if !ok {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, "not ready\n")
		return
	}
	_, _ = io.WriteString(w, "ok\n")
}

// call is one request: what it has been found to be so far, which is what
// its log line says. Only a value the relay checked against its own lists
// is set here: an event or an environment the wire's closed set admitted, a
// topic the relay serves.
type call struct {
	relay  *Relay
	w      http.ResponseWriter
	r      *http.Request
	route  string
	now    time.Time
	caller string

	topic, environment, event, key string
}

// logAttrs are what every line about this call may carry: never a push
// key, Apple's token, a public key, an event id, a thread or a badge.
func (c *call) logAttrs(status int, code string) []any {
	attrs := []any{"route", c.route, "status", status, "code", code}
	for _, kv := range [][2]string{{"topic", c.topic}, {"environment", c.environment}, {"event", c.event}} {
		if kv[1] != "" {
			attrs = append(attrs, kv[0], kv[1])
		}
	}
	if c.key != "" {
		attrs = append(attrs, "key", Fingerprint(c.key))
	}
	return attrs
}

// Fingerprint names a push key in a log line: the first 12 hex characters
// of its SHA-256, which links the pushes of one registration and opens
// nothing.
func Fingerprint(pushKey string) string {
	sum := sha256.Sum256([]byte(pushKey))
	return hex.EncodeToString(sum[:])[:12]
}

// refuse answers a refusal made before Apple is called, always with its
// own code: a server deletes a device on key_refused or key_expired, and an
// app stops on topic_refused, whatever else the relay is going through.
// Each is charged to the refused total, which bounds the log lines: past
// it the refusal is counted and not logged.
func (c *call) refuse(f Refusal, extra ...any) {
	if ok, _ := c.relay.limits.takeGlobal(limitRefusedTotal, c.now); !ok {
		c.relay.cfg.Metrics.limited.WithLabelValues(limitRefusedTotal).Inc()
		c.write(f)
		return
	}
	level := slog.LevelInfo
	if c.route == routeOther {
		level = slog.LevelDebug
	}
	attrs := append(c.logAttrs(f.Status, f.Code), "caller", c.caller)
	if f.Code == pushwire.CodeBadRequest {
		attrs = append(attrs, "reason", f.Message)
	}
	c.relay.cfg.Log.Log(c.r.Context(), level, "push relay refused a request", append(attrs, extra...)...)
	c.write(f)
}

// limited refuses a request a shared limit holds: the install's, or one of
// the totals. It is a refusal like any other.
func (c *call) limited(limit string, wait time.Duration) {
	c.relay.cfg.Metrics.limited.WithLabelValues(limit).Inc()
	c.w.Header().Set("Retry-After", strconv.Itoa(retryAfter(wait)))
	c.refuse(RateLimited, "limit", limit)
}

// callerLimited refuses a request its own caller's limit holds. It is not
// charged to the refused total and is logged at debug alone, so one caller
// past its allowance can neither spend the other callers' share of the log
// nor fill the log itself.
func (c *call) callerLimited(limit string, wait time.Duration) {
	c.relay.cfg.Metrics.limited.WithLabelValues(limit).Inc()
	c.w.Header().Set("Retry-After", strconv.Itoa(retryAfter(wait)))
	c.relay.cfg.Log.Debug("push relay held a caller to its limit",
		append(c.logAttrs(RateLimited.Status, RateLimited.Code), "caller", c.caller, "limit", limit)...)
	c.write(RateLimited)
}

func (c *call) write(f Refusal) {
	c.relay.cfg.Metrics.requests.WithLabelValues(c.route, f.Code).Inc()
	c.w.Header().Set("Content-Type", "application/json")
	c.w.WriteHeader(f.Status)
	_, _ = c.w.Write(f.Body())
}

func (c *call) ok(code string, v any) {
	c.relay.cfg.Metrics.requests.WithLabelValues(c.route, code).Inc()
	b, _ := json.Marshal(v)
	c.w.Header().Set("Content-Type", "application/json")
	_, _ = c.w.Write(b)
}

// body reads at most MaxBody bytes, refusing a body over it before reading
// further.
func (c *call) body() ([]byte, bool) {
	if c.r.ContentLength > pushwire.MaxBody {
		c.refuse(TooLarge)
		return nil, false
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.w, c.r.Body, pushwire.MaxBody))
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		c.refuse(TooLarge)
		return nil, false
	case err != nil:
		c.refuse(BadRequest("the body could not be read"))
		return nil, false
	}
	return body, true
}

// badRequest is the 400 for a body the wire refused. The wire's messages
// are fixed, so the logged reason repeats nothing the caller wrote.
func badRequest(err error) Refusal {
	return BadRequest(strings.TrimPrefix(err.Error(), pushwire.ErrBadRequest.Error()+": "))
}

func (c *call) register(body []byte) {
	if ok, wait := c.relay.limits.take(limitRegister, callerKey(c.caller), c.now); !ok {
		c.callerLimited(limitRegister, wait)
		return
	}
	if ok, wait := c.relay.limits.takeGlobal(limitRegisterTotal, c.now); !ok {
		c.limited(limitRegisterTotal, wait)
		return
	}
	req, err := pushwire.DecodeRegister(body)
	if err != nil {
		c.refuse(badRequest(err))
		return
	}
	c.environment = req.Environment
	if !slices.Contains(c.relay.cfg.Topics, req.Topic) {
		c.refuse(TopicRefused)
		return
	}
	c.topic = req.Topic
	if c.relay.cfg.Apple[req.Environment] == nil {
		c.refuse(EnvironmentRefused)
		return
	}
	key, err := c.relay.cfg.Sealer.Seal(PushKey{
		Platform: req.Platform, Token: strings.ToLower(req.Token), Topic: req.Topic, Environment: req.Environment,
		PublicKey: req.PublicKey, Generation: req.Generation, Issued: c.now.Unix(),
	})
	if err != nil {
		c.relay.cfg.Log.Error("push relay could not seal a push key", append(c.logAttrs(http.StatusInternalServerError, pushwire.CodeInternal), "error", err.Error())...)
		c.write(Internal)
		return
	}
	c.key = key
	c.relay.cfg.Log.Debug("push relay registered an install", c.logAttrs(http.StatusOK, codeRegistered)...)
	c.ok(codeRegistered, pushwire.RegisterResponse{PushKey: key})
}

func (c *call) push(body []byte) {
	req, err := pushwire.DecodePush(body)
	if err != nil {
		c.refuse(badRequest(err))
		return
	}
	c.event, c.key = req.Event, req.PushKey
	k, err := c.relay.cfg.Sealer.Open(req.PushKey)
	if err != nil {
		c.refuse(KeyRefused)
		return
	}
	c.environment = k.Environment
	issued := time.Unix(k.Issued, 0)
	// A key from the future was sealed under a clock far ahead of this one,
	// or under a seal key someone else holds.
	if issued.Sub(c.now) > pushwire.SignatureWindow {
		c.refuse(KeyRefused)
		return
	}
	if c.now.Sub(issued) > pushwire.KeyLifetime {
		c.refuse(KeyExpired)
		return
	}
	if !slices.Contains(c.relay.cfg.Topics, k.Topic) {
		c.refuse(TopicRefused)
		return
	}
	c.topic = k.Topic
	sender := c.relay.cfg.Apple[k.Environment]
	if sender == nil {
		c.refuse(EnvironmentRefused)
		return
	}
	pub, _ := pushwire.Decode(k.PublicKey)
	sig, err := pushwire.ParseSignature(c.r.Header.Get(pushwire.SignatureHeader))
	if err == nil {
		err = sig.Verify(pub, c.relay.cfg.Now(), body)
	}
	switch {
	case errors.Is(err, pushwire.ErrStale):
		c.refuse(StaleSignature)
		return
	case err != nil:
		c.refuse(SignatureRefused)
		return
	}
	if !c.admit(sig, k) {
		return
	}
	c.send(sender, k, pushwire.ApplePush(req, k.Generation, c.relay.cfg.Now()))
}

// admit takes a verified push to Apple or refuses it, as one step under the
// admission lock and at one clock, the one the window is pruned by: the
// signature still inside the window, not held already, room to hold it,
// the install's and the total limits charged, and only then the signature
// held. A request refused here holds no entry, so no request that never
// reached Apple takes a delivered push's place, and no entry is dropped
// while its signature could still be accepted.
func (c *call) admit(sig pushwire.Signature, k PushKey) bool {
	rl := c.relay
	rl.admitting.Lock()
	now := rl.cfg.Now()
	rl.replays.prune(now)
	var (
		refusal    *Refusal
		limit      string
		wait       time.Duration
		installKey = rl.cfg.Sealer.InstallKey(k.Token)
	)
	switch d := now.Sub(sig.T); {
	case d > pushwire.SignatureWindow || d < -pushwire.SignatureWindow:
		refusal = &StaleSignature
	case rl.replays.seen(sig.Sig):
		refusal = &Replayed
	case rl.replays.full():
		limit = limitReplayWindow
	default:
		var ok bool
		if ok, wait = rl.limits.take(limitInstall, installKey, now); !ok {
			limit = limitInstall
		} else if ok, wait = rl.limits.takeGlobal(limitTotal, now); !ok {
			limit = limitTotal
		} else {
			rl.replays.add(sig.Sig, sig.T.Add(pushwire.SignatureWindow))
		}
	}
	rl.admitting.Unlock()
	switch {
	case refusal != nil:
		c.refuse(*refusal)
		return false
	case limit != "":
		c.limited(limit, wait)
		return false
	}
	return true
}

// The reasons Apple gives that are the publisher's configuration, not the
// install's: the key, or a topic the key may not send to.
var configurationReasons = []string{apns.ReasonInvalidProviderToken, apns.ReasonExpiredProviderToken, "TopicDisallowed", "BadTopic"}

// reasonWrongEnvironment is a key issued for one Apple environment
// configured for the other.
const reasonWrongEnvironment = "BadEnvironmentKeyIdInToken"

// The reasons that say the install is gone. BadDeviceToken is also a token
// of the other environment: the device is deleted, and the install
// registers again in its own when it next opens.
var goneReasons = []string{"BadDeviceToken", "DeviceTokenNotForTopic"}

// appleReasons are the reasons Apple documents; any other is counted as
// `other`, so the metric's labels are a finite set.
var appleReasons = map[string]bool{
	"BadCollapseId": true, "BadDeviceToken": true, "BadExpirationDate": true, "BadMessageId": true,
	"BadPriority": true, "BadTopic": true, "DeviceTokenNotForTopic": true, "DuplicateHeaders": true,
	"IdleTimeout": true, "InvalidPushType": true, "MissingDeviceToken": true, "MissingTopic": true,
	"PayloadEmpty": true, "TopicDisallowed": true, "BadCertificate": true, "BadCertificateEnvironment": true,
	"ExpiredProviderToken": true, "Forbidden": true, "InvalidProviderToken": true, "MissingProviderToken": true,
	reasonWrongEnvironment: true, "BadPath": true, "MethodNotAllowed": true, "ExpiredToken": true,
	"Unregistered": true, "PayloadTooLarge": true, "TooManyProviderTokenUpdates": true, "TooManyRequests": true,
	"InternalServerError": true, "ServiceUnavailable": true, "Shutdown": true,
}

// appleReason is the metric label for a reason Apple gave: the reason when
// Apple documents it, empty for none, and `other` for anything else.
func appleReason(s string) string {
	switch {
	case s == "":
		return ""
	case appleReasons[s]:
		return s
	default:
		return "other"
	}
}

func (c *call) send(sender Sender, k PushKey, n pushwire.AppleNotification) {
	started := c.relay.cfg.Now()
	resp, err := sender.Send(c.r.Context(), apns.Notification{DeviceToken: k.Token, Headers: n.Headers(k.Topic), Payload: n.Payload})
	reason := appleReason(resp.Reason)
	c.relay.cfg.Metrics.appleAnswer(k.Environment, resp.Status, reason, c.relay.cfg.Now().Sub(started).Seconds())
	log := c.relay.cfg.Log
	attrs := func(f Refusal) []any {
		return append(c.logAttrs(f.Status, f.Code), "apple_status", resp.Status, "apple_reason", reason, "apns_id", resp.ID)
	}
	switch {
	case err != nil:
		// An apns error's text is a fixed category, never the request's
		// URL, which holds the device token.
		log.Warn("push relay could not reach Apple", append(attrs(Upstream), "error", err.Error())...)
		c.write(Upstream)
	case resp.Status == http.StatusOK:
		log.Debug("push relay sent a push", c.logAttrs(http.StatusOK, pushwire.StatusSent)...)
		c.ok(pushwire.StatusSent, pushwire.PushResponse{Status: pushwire.StatusSent})
	case resp.Status == http.StatusGone, resp.Status == http.StatusBadRequest && slices.Contains(goneReasons, resp.Reason):
		log.Info("Apple says the install is gone", attrs(Gone)...)
		c.write(Gone)
	case resp.Status == http.StatusForbidden && resp.Reason == reasonWrongEnvironment:
		log.Error("Apple refused the key configured for this environment: it was issued for the other one", attrs(Upstream)...)
		c.write(Upstream)
	case slices.Contains(configurationReasons, resp.Reason) && (resp.Status == http.StatusForbidden || resp.Status == http.StatusBadRequest):
		log.Error("Apple refused the publisher's configuration: its key, key id, team id or topic", attrs(Upstream)...)
		c.write(Upstream)
	case resp.Status == http.StatusTooManyRequests, resp.Status >= 500:
		log.Warn("Apple could not take the push", attrs(Upstream)...)
		c.write(Upstream)
	default:
		log.Error("Apple answered what the relay does not know", attrs(Internal)...)
		c.write(Internal)
	}
}
