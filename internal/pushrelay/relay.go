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
	cfg     Config
	limits  *limiter
	replays *replays
	ready   atomic.Bool
}

// New makes a relay. It answers /readyz 503 until MarkReady.
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
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if cfg.Metrics == nil {
		cfg.Metrics = NewMetrics()
	}
	return &Relay{cfg: cfg, limits: newLimiter(cfg.Limits), replays: newReplays(maxReplays)}, nil
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
// method's included, is the one envelope.
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
	if !strings.HasPrefix(r.URL.Path, "/v1/") {
		call.refuse(NotFound, nil)
		return
	}
	if ok, wait := rl.limits.take(limitCaller, call.caller, call.now); !ok {
		call.limited(limitCaller, wait)
		return
	}
	switch r.URL.Path {
	case pushwire.PathRegister:
		call.route = routeRegister
	case pushwire.PathPush:
		call.route = routePush
	default:
		call.refuse(NotFound, nil)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		call.refuse(MethodNotAllowed, nil)
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
// its log line says.
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

// refuse answers a refusal made before Apple is called. Each one is charged
// to the refused total; past it the refusal is answered rate_limited and
// not logged, so a flood of bad requests costs the logs a bounded amount.
func (c *call) refuse(f Refusal, err error) {
	if ok, wait := c.relay.limits.takeGlobal(limitRefusedTotal, c.now); !ok {
		c.relay.cfg.Metrics.limited.WithLabelValues(limitRefusedTotal).Inc()
		c.w.Header().Set("Retry-After", strconv.Itoa(retryAfter(wait)))
		c.write(RateLimited)
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
	if err != nil {
		attrs = append(attrs, "error", err.Error())
	}
	c.relay.cfg.Log.Log(c.r.Context(), level, "push relay refused a request", attrs...)
	c.write(f)
}

// limited refuses a request a limit holds.
func (c *call) limited(limit string, wait time.Duration) {
	c.relay.cfg.Metrics.limited.WithLabelValues(limit).Inc()
	c.w.Header().Set("Retry-After", strconv.Itoa(retryAfter(wait)))
	c.refuse(RateLimited, errors.New(limit+" limit"))
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
		c.refuse(TooLarge, nil)
		return nil, false
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.w, c.r.Body, pushwire.MaxBody))
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		c.refuse(TooLarge, nil)
		return nil, false
	case err != nil:
		c.refuse(BadRequest("the body could not be read"), nil)
		return nil, false
	}
	return body, true
}

func badRequest(err error) Refusal {
	return BadRequest(strings.TrimPrefix(err.Error(), pushwire.ErrBadRequest.Error()+": "))
}

func (c *call) register(body []byte) {
	if ok, wait := c.relay.limits.take(limitRegister, c.caller, c.now); !ok {
		c.limited(limitRegister, wait)
		return
	}
	if ok, wait := c.relay.limits.takeGlobal(limitRegisterTotal, c.now); !ok {
		c.limited(limitRegisterTotal, wait)
		return
	}
	req, err := pushwire.DecodeRegister(body)
	if err != nil {
		c.refuse(badRequest(err), nil)
		return
	}
	c.topic, c.environment = req.Topic, req.Environment
	if !slices.Contains(c.relay.cfg.Topics, req.Topic) {
		c.refuse(TopicRefused, nil)
		return
	}
	if c.relay.cfg.Apple[req.Environment] == nil {
		c.refuse(EnvironmentRefused, nil)
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
		c.refuse(badRequest(err), nil)
		return
	}
	c.event, c.key = req.Event, req.PushKey
	k, err := c.relay.cfg.Sealer.Open(req.PushKey)
	if err != nil {
		c.refuse(KeyRefused, nil)
		return
	}
	c.topic, c.environment = k.Topic, k.Environment
	if c.now.Sub(time.Unix(k.Issued, 0)) > pushwire.KeyLifetime {
		c.refuse(KeyExpired, nil)
		return
	}
	if !slices.Contains(c.relay.cfg.Topics, k.Topic) {
		c.refuse(TopicRefused, nil)
		return
	}
	sender := c.relay.cfg.Apple[k.Environment]
	if sender == nil {
		c.refuse(EnvironmentRefused, nil)
		return
	}
	pub, _ := pushwire.Decode(k.PublicKey)
	sig, err := pushwire.ParseSignature(c.r.Header.Get(pushwire.SignatureHeader))
	if err == nil {
		err = sig.Verify(pub, c.now, body)
	}
	switch {
	case errors.Is(err, pushwire.ErrStale):
		c.refuse(StaleSignature, nil)
		return
	case err != nil:
		c.refuse(SignatureRefused, nil)
		return
	}
	if !c.relay.replays.accept(sig.Sig, sig.T.Add(pushwire.SignatureWindow), c.now) {
		c.refuse(Replayed, nil)
		return
	}
	if ok, wait := c.relay.limits.take(limitInstall, c.relay.cfg.Sealer.InstallKey(k.Token), c.now); !ok {
		c.limited(limitInstall, wait)
		return
	}
	if ok, wait := c.relay.limits.takeGlobal(limitTotal, c.now); !ok {
		c.limited(limitTotal, wait)
		return
	}
	n := pushwire.ApplePush(req, k.Generation, c.now)
	c.send(sender, k, n)
}

// The reasons Apple gives that are the publisher's configuration, not the
// install's: the key, or a topic the key may not send to.
var configurationReasons = []string{apns.ReasonInvalidProviderToken, apns.ReasonExpiredProviderToken, "TopicDisallowed", "BadTopic"}

// The reasons that say the install is gone.
var goneReasons = []string{"BadDeviceToken", "DeviceTokenNotForTopic"}

func (c *call) send(sender Sender, k PushKey, n pushwire.AppleNotification) {
	started := time.Now()
	resp, err := sender.Send(c.r.Context(), apns.Notification{DeviceToken: k.Token, Headers: n.Headers(k.Topic), Payload: n.Payload})
	reason := appleReason(resp.Reason)
	c.relay.cfg.Metrics.appleAnswer(k.Environment, resp.Status, reason, time.Since(started).Seconds())
	log := c.relay.cfg.Log
	attrs := func(f Refusal) []any {
		return append(c.logAttrs(f.Status, f.Code), "apple_status", resp.Status, "apple_reason", reason, "apns_id", resp.ID)
	}
	switch {
	case err != nil:
		log.Warn("push relay could not reach Apple", append(attrs(Upstream), "error", err.Error())...)
		c.write(Upstream)
	case resp.Status == http.StatusOK:
		log.Debug("push relay sent a push", c.logAttrs(http.StatusOK, pushwire.StatusSent)...)
		c.ok(pushwire.StatusSent, pushwire.PushResponse{Status: pushwire.StatusSent})
	case resp.Status == http.StatusGone, resp.Status == http.StatusBadRequest && slices.Contains(goneReasons, resp.Reason):
		log.Info("Apple says the install is gone", attrs(Gone)...)
		c.write(Gone)
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

// appleReason keeps a reason Apple wrote when it is a reason's shape, so a
// metric label stays one of Apple's words.
func appleReason(s string) string {
	if len(s) > 64 {
		return "other"
	}
	for i := range len(s) {
		if (s[i] < 'a' || s[i] > 'z') && (s[i] < 'A' || s[i] > 'Z') {
			return "other"
		}
	}
	return s
}
