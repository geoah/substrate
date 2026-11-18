package pushrelay

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/apns"
	"github.com/geoah/substrate/internal/pushwire"
	"github.com/geoah/substrate/internal/pushwire/pushwiretest"
)

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *clock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}
func (c *clock) Add(d time.Duration) { c.Set(c.Now().Add(d)) }

// fakeSender stands in for one Apple environment in the tests that are not
// about HTTP/2: it records each notification and answers what it is told.
type fakeSender struct {
	mu     sync.Mutex
	sent   []apns.Notification
	answer func(apns.Notification) (apns.Response, error)
}

func (f *fakeSender) Send(_ context.Context, n apns.Notification) (apns.Response, error) {
	f.mu.Lock()
	f.sent = append(f.sent, n)
	answer := f.answer
	f.mu.Unlock()
	if answer != nil {
		return answer(n)
	}
	return apns.Response{Status: http.StatusOK, ID: "00000000-0000-0000-0000-00000000000a"}, nil
}

func (f *fakeSender) count() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.sent) }

// harness is a relay on the fixtures' keys, a fake clock and a fake Apple
// per environment, logging every line at every level into logs.
type harness struct {
	t      *testing.T
	keys   pushwiretest.Keys
	signer ed25519.PrivateKey
	clock  *clock
	relay  *Relay
	prod   *fakeSender
	dev    *fakeSender
	logs   *bytes.Buffer
}

func newHarness(t *testing.T, configure ...func(*Config)) *harness {
	t.Helper()
	keys := pushwiretest.LoadKeys(t)
	sealer, err := NewSealer(keys.SealKeyBytes(t), nil, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{
		t: t, keys: keys, signer: keys.Signer(t), clock: &clock{now: keys.Time()},
		prod: &fakeSender{}, dev: &fakeSender{}, logs: &bytes.Buffer{},
	}
	cfg := Config{
		Topics: []string{keys.Topic},
		Apple:  map[string]Sender{pushwire.EnvironmentProduction: h.prod, pushwire.EnvironmentDevelopment: h.dev},
		Sealer: sealer,
		Now:    h.clock.Now,
		Log:    slog.New(slog.NewJSONHandler(h.logs, &slog.HandlerOptions{Level: slog.LevelDebug, ReplaceAttr: dropTime})),
	}
	for _, f := range configure {
		f(&cfg)
	}
	if h.relay, err = New(cfg); err != nil {
		t.Fatal(err)
	}
	return h
}

// dropTime keeps the wall clock out of the captured lines, so a scan for a
// value cannot match a timestamp.
func dropTime(groups []string, a slog.Attr) slog.Attr {
	if len(groups) == 0 && a.Key == slog.TimeKey {
		return slog.Attr{}
	}
	return a
}

func (h *harness) do(method, path string, body []byte, header map[string]string) *httptest.ResponseRecorder {
	h.t.Helper()
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	r.RemoteAddr = "198.51.100.7:40000"
	for k, v := range header {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.relay.ServeHTTP(w, r)
	return w
}

// install is one registration made through the relay.
type install struct {
	token, generation, pushKey string
}

func (h *harness) register(token, environment string) install {
	h.t.Helper()
	in := install{token: token, generation: pushwire.Encode(pushwiretest.Stream("gen "+token, 16))}
	body, _ := json.Marshal(pushwire.RegisterRequest{
		Platform: pushwire.PlatformAPNs, Token: token, Topic: h.keys.Topic, Environment: environment,
		PublicKey: h.keys.PublicKey, Generation: in.generation,
	})
	w := h.do(http.MethodPost, pushwire.PathRegister, body, nil)
	if w.Code != http.StatusOK {
		h.t.Fatalf("register = %d %s", w.Code, w.Body)
	}
	var resp pushwire.RegisterResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	in.pushKey = resp.PushKey
	return in
}

var tokenSeq int

func newToken() string {
	tokenSeq++
	return fmt.Sprintf("%064x", tokenSeq)
}

// pushBody is a push to in, signed by key at t.
func (h *harness) signed(in install, event string, badge int, eventID string, key ed25519.PrivateKey, at time.Time) ([]byte, string) {
	body, _ := json.Marshal(pushwire.PushRequest{PushKey: in.pushKey, Event: event, Thread: "8f2c41d07a", Badge: badge, EventID: eventID})
	return body, pushwire.Sign(key, at, body)
}

func (h *harness) push(body []byte, signature string) *httptest.ResponseRecorder {
	h.t.Helper()
	return h.do(http.MethodPost, pushwire.PathPush, body, map[string]string{pushwire.SignatureHeader: signature})
}

func eventID(label string) string { return pushwire.Encode(pushwiretest.Stream(label, 48)) }

// wantRefusal fails unless w is the refusal f.
func wantRefusal(t *testing.T, w *httptest.ResponseRecorder, f Refusal) {
	t.Helper()
	var e pushwire.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil || w.Code != f.Status || e.Error.Code != f.Code {
		t.Fatalf("answered %d %s, want %d %s", w.Code, w.Body, f.Status, f.Code)
	}
}

func wantSent(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != `{"status":"sent"}` {
		t.Fatalf("answered %d %s, want 200 sent", w.Code, w.Body)
	}
}

// fakeApple is Apple over HTTP/2 with TLS, for the tests that run the real
// client: it records each request and answers 200.
type fakeApple struct {
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []appleRequest
}

type appleRequest struct {
	path    string
	headers map[string]string
	body    []byte
}

func newFakeApple(t *testing.T) *fakeApple {
	t.Helper()
	a := &fakeApple{}
	a.srv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			t.Errorf("Apple spoken to over %s", r.Proto)
		}
		body, _ := io.ReadAll(r.Body)
		h := map[string]string{}
		for k := range r.Header {
			if k := strings.ToLower(k); strings.HasPrefix(k, "apns-") {
				h[k] = r.Header.Get(k)
			}
		}
		a.mu.Lock()
		a.reqs = append(a.reqs, appleRequest{path: r.URL.Path, headers: h, body: body})
		a.mu.Unlock()
		w.Header().Set("apns-id", "00000000-0000-0000-0000-00000000000b")
	}))
	a.srv.EnableHTTP2 = true
	a.srv.StartTLS()
	t.Cleanup(a.srv.Close)
	return a
}

func (a *fakeApple) client(t *testing.T) *apns.Client {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pool := x509.NewCertPool()
	pool.AddCert(a.srv.Certificate())
	c, err := apns.New(apns.Config{
		BaseURL: a.srv.URL, TeamID: "TEAM000001", KeyID: "KEY0000001", Key: key,
		Transport: apns.NewTransport(&tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}),
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func sameJSON(a, b []byte) bool {
	var x, y any
	return json.Unmarshal(a, &x) == nil && json.Unmarshal(b, &y) == nil && reflect.DeepEqual(x, y)
}

// register.json: the relay, sealing with the fixtures' nonces at the
// fixtures' time, answers the very push key the fixture holds.
func TestRegisterAnswersTheFixture(t *testing.T) {
	var f pushwiretest.Register
	pushwiretest.Read(t, "register.json", &f)
	h := newHarness(t, func(c *Config) {
		keys := pushwiretest.LoadKeys(t)
		c.Sealer, _ = NewSealer(keys.SealKeyBytes(t), nil, pushwiretest.Nonces())
	})
	h.clock.Set(time.Unix(h.keys.RegisteredAt, 0))
	w := h.do(http.MethodPost, pushwire.PathRegister, f.Request, map[string]string{"Content-Type": "application/json"})
	if w.Code != f.Response.Status || !sameJSON(w.Body.Bytes(), f.Response.Body) {
		t.Fatalf("register = %d %s, want %d %s", w.Code, w.Body, f.Response.Status, f.Response.Body)
	}
}

// Every push fixture, sent through the relay and the real Apple client to an
// HTTP/2 Apple, arrives as the fixture says: the device, the headers and the
// payload, in the fixture's environment.
func TestEveryPushFixtureReachesAppleAsWritten(t *testing.T) {
	for _, name := range pushwiretest.PushNames {
		t.Run(name, func(t *testing.T) {
			f := pushwiretest.LoadPush(t, name)
			prod, dev := newFakeApple(t), newFakeApple(t)
			h := newHarness(t, func(c *Config) {
				c.Apple = map[string]Sender{pushwire.EnvironmentProduction: prod.client(t), pushwire.EnvironmentDevelopment: dev.client(t)}
			})
			h.clock.Set(time.Unix(f.Now, 0))
			w := h.push([]byte(f.Body), f.Signature)
			if w.Code != f.Response.Status || !sameJSON(w.Body.Bytes(), f.Response.Body) {
				t.Fatalf("push = %d %s, want %d %s", w.Code, w.Body, f.Response.Status, f.Response.Body)
			}
			got, other := prod, dev
			if f.Apple.Environment == pushwire.EnvironmentDevelopment {
				got, other = dev, prod
			}
			if len(got.reqs) != 1 || len(other.reqs) != 0 {
				t.Fatalf("Apple saw %d requests in %s and %d in the other", len(got.reqs), f.Apple.Environment, len(other.reqs))
			}
			r := got.reqs[0]
			if r.path != f.Apple.Path || !reflect.DeepEqual(r.headers, f.Apple.Headers) || !sameJSON(r.body, f.Apple.Payload) {
				t.Fatalf("Apple got %s %v %s\nwant %s %v %s", r.path, r.headers, r.body, f.Apple.Path, f.Apple.Headers, f.Apple.Payload)
			}
		})
	}
}

// The two cards on one thread both reach Apple, neither with a collapse id.
func TestTwoCardsOnOneThreadBothReachApple(t *testing.T) {
	h := newHarness(t)
	for _, name := range []string{"push-request-card-1.json", "push-request-card-2.json"} {
		f := pushwiretest.LoadPush(t, name)
		wantSent(t, h.push([]byte(f.Body), f.Signature))
	}
	if h.prod.count() != 2 {
		t.Fatalf("Apple got %d", h.prod.count())
	}
	for _, n := range h.prod.sent {
		if _, ok := n.Headers["apns-collapse-id"]; ok || n.Headers["apns-push-type"] != "alert" {
			t.Errorf("a card's headers: %v", n.Headers)
		}
	}
}

// Every refusal in errors.json is what the relay answers for it.
func TestErrorsJSONIsTheRelaysRefusals(t *testing.T) {
	var rows []pushwiretest.Refusal
	pushwiretest.Read(t, "errors.json", &rows)
	byStatusCode := map[string]pushwiretest.Refusal{}
	for _, r := range rows {
		var e pushwire.ErrorResponse
		_ = json.Unmarshal(r.Body, &e)
		byStatusCode[strconv.Itoa(r.Status)+e.Error.Code] = r
	}
	for _, f := range []Refusal{
		StaleSignature, SignatureRefused, KeyRefused, KeyExpired, TopicRefused, EnvironmentRefused,
		Replayed, Gone, TooLarge, RateLimited, Upstream, Internal, BadRequest("eventId is missing"),
	} {
		r, ok := byStatusCode[strconv.Itoa(f.Status)+f.Code]
		if !ok || !sameJSON(r.Body, f.Body()) {
			t.Errorf("errors.json has no row for %d %s %q", f.Status, f.Code, f.Message)
		}
	}
	// And a push missing its event id gets that row's very envelope.
	h := newHarness(t)
	in := h.register(newToken(), pushwire.EnvironmentProduction)
	body := []byte(`{"pushKey":"` + in.pushKey + `","event":"reply","thread":"8f2c41d07a","badge":1}`)
	w := h.push(body, pushwire.Sign(h.signer, h.clock.Now(), body))
	if w.Code != http.StatusBadRequest || !sameJSON(w.Body.Bytes(), BadRequest("eventId is missing").Body()) {
		t.Fatalf("a push missing its event id = %d %s", w.Code, w.Body)
	}
}

// Apple's every answer, and no answer, becomes the plan's.
func TestEveryAppleAnswerBecomesThePlans(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		reason string
		err    error
		want   *Refusal
	}{
		{"sent", 200, "", nil, nil},
		{"unregistered", 410, "Unregistered", nil, &Gone},
		{"an expired token", 410, "ExpiredToken", nil, &Gone},
		{"a bad device token", 400, "BadDeviceToken", nil, &Gone},
		{"a token for another topic", 400, "DeviceTokenNotForTopic", nil, &Gone},
		{"the key refused", 403, "InvalidProviderToken", nil, &Upstream},
		{"a provider token expired twice", 403, "ExpiredProviderToken", nil, &Upstream},
		{"a topic the key may not send to", 400, "TopicDisallowed", nil, &Upstream},
		{"a bad topic", 400, "BadTopic", nil, &Upstream},
		{"too many requests", 429, "TooManyRequests", nil, &Upstream},
		{"Apple's 500", 500, "InternalServerError", nil, &Upstream},
		{"Apple's 503", 503, "ServiceUnavailable", nil, &Upstream},
		{"no answer", 0, "", context.DeadlineExceeded, &Upstream},
		{"a refused connection", 0, "", errors.New("connection refused"), &Upstream},
		{"a payload too large", 413, "PayloadTooLarge", nil, &Internal},
		{"an unknown 400", 400, "BadCollapseId", nil, &Internal},
		{"an unknown 403", 403, "Forbidden", nil, &Internal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.prod.answer = func(apns.Notification) (apns.Response, error) {
				return apns.Response{Status: tc.status, Reason: tc.reason, ID: "id"}, tc.err
			}
			in := h.register(newToken(), pushwire.EnvironmentProduction)
			w := h.push(h.signed(in, pushwire.EventReply, 1, eventID(tc.name), h.signer, h.clock.Now()))
			if tc.want == nil {
				wantSent(t, w)
				return
			}
			wantRefusal(t, w, *tc.want)
		})
	}
}

func TestRegisterHoldsItsBounds(t *testing.T) {
	h := newHarness(t, func(c *Config) {
		c.Apple = map[string]Sender{pushwire.EnvironmentDevelopment: &fakeSender{}}
	})
	good := pushwire.RegisterRequest{
		Platform: pushwire.PlatformAPNs, Token: newToken(), Topic: h.keys.Topic, Environment: pushwire.EnvironmentDevelopment,
		PublicKey: h.keys.PublicKey, Generation: pushwire.Encode(make([]byte, 16)),
	}
	reg := func(r pushwire.RegisterRequest) *httptest.ResponseRecorder {
		b, _ := json.Marshal(r)
		return h.do(http.MethodPost, pushwire.PathRegister, b, nil)
	}
	if w := reg(good); w.Code != http.StatusOK {
		t.Fatalf("register = %d %s", w.Code, w.Body)
	}
	r := good
	r.Topic = "dev.example.other"
	wantRefusal(t, reg(r), TopicRefused)
	r = good
	r.Environment = pushwire.EnvironmentProduction
	wantRefusal(t, reg(r), EnvironmentRefused)
	r = good
	r.Environment = "sandbox"
	wantRefusal(t, reg(r), BadRequest(""))
	r = good
	r.Token = "xyz"
	wantRefusal(t, reg(r), BadRequest(""))
	r = good
	r.Platform = "fcm"
	wantRefusal(t, reg(r), BadRequest(""))
}

// A key issued 90 days ago is the last day it is accepted.
func TestAKeyPast90DaysIsExpired(t *testing.T) {
	h := newHarness(t)
	in := h.register(newToken(), pushwire.EnvironmentProduction)
	h.clock.Add(pushwire.KeyLifetime)
	wantSent(t, h.push(h.signed(in, pushwire.EventReply, 1, eventID("at 90 days"), h.signer, h.clock.Now())))
	h.clock.Add(time.Second)
	wantRefusal(t, h.push(h.signed(in, pushwire.EventReply, 1, eventID("past 90 days"), h.signer, h.clock.Now())), KeyExpired)
}

func TestAKeyAnotherRelaySealedIsRefused(t *testing.T) {
	h := newHarness(t)
	other, _ := NewSealer(sealKey("another relay"), nil, rand.Reader)
	key, _ := other.Seal(PushKey{
		Platform: pushwire.PlatformAPNs, Token: newToken(), Topic: h.keys.Topic, Environment: pushwire.EnvironmentProduction,
		PublicKey: h.keys.PublicKey, Generation: pushwire.Encode(make([]byte, 16)), Issued: h.clock.Now().Unix(),
	})
	wantRefusal(t, h.push(h.signed(install{pushKey: key}, pushwire.EventReply, 1, eventID("x"), h.signer, h.clock.Now())), KeyRefused)
}

// A topic or an environment the relay stops serving refuses the keys it
// sealed for it.
func TestAKeyForATopicOrEnvironmentNoLongerServedIsRefused(t *testing.T) {
	h := newHarness(t)
	in := h.register(newToken(), pushwire.EnvironmentDevelopment)
	topicless := newHarness(t, func(c *Config) { c.Topics = []string{"dev.example.other"} })
	wantRefusal(t, topicless.push(h.signed(in, pushwire.EventReply, 1, eventID("a"), h.signer, h.clock.Now())), TopicRefused)
	devless := newHarness(t, func(c *Config) { c.Apple = map[string]Sender{pushwire.EnvironmentProduction: &fakeSender{}} })
	wantRefusal(t, devless.push(h.signed(in, pushwire.EventReply, 1, eventID("b"), h.signer, h.clock.Now())), EnvironmentRefused)
}

func TestTheSignatureIsHeldToTheKeyTheWindowAndOnce(t *testing.T) {
	h := newHarness(t)
	in := h.register(newToken(), pushwire.EnvironmentProduction)
	now := h.clock.Now()

	body, sig := h.signed(in, pushwire.EventReply, 1, eventID("valid"), h.signer, now)
	wantSent(t, h.push(body, sig))
	wantRefusal(t, h.push(body, sig), Replayed)

	wantRefusal(t, h.push(h.signed(in, pushwire.EventReply, 1, eventID("behind"), h.signer, now.Add(-301*time.Second))), StaleSignature)
	wantRefusal(t, h.push(h.signed(in, pushwire.EventReply, 1, eventID("ahead"), h.signer, now.Add(301*time.Second))), StaleSignature)
	wantSent(t, h.push(h.signed(in, pushwire.EventReply, 1, eventID("300 behind"), h.signer, now.Add(-300*time.Second))))

	other := pushwiretest.LoadKeys(t).Other(t)
	wantRefusal(t, h.push(h.signed(in, pushwire.EventReply, 1, eventID("other key"), other, now)), SignatureRefused)

	body, sig = h.signed(in, pushwire.EventReply, 1, eventID("changed"), h.signer, now)
	wantRefusal(t, h.push(bytes.Replace(body, []byte(`"badge":1`), []byte(`"badge":2`), 1), sig), SignatureRefused)
	wantRefusal(t, h.push(body, ""), SignatureRefused)
	wantRefusal(t, h.push(body, "t=1,s=AAAA"), SignatureRefused)
	if h.prod.count() != 2 {
		t.Fatalf("Apple got %d pushes, want the two that verified", h.prod.count())
	}
}

// A signature dated 300 s ahead is kept until it could no longer be
// accepted: a repeat 590 s later is still a repeat.
func TestASignatureDatedAheadIsKeptAsLongAsItCouldBeAccepted(t *testing.T) {
	h := newHarness(t)
	in := h.register(newToken(), pushwire.EnvironmentProduction)
	body, sig := h.signed(in, pushwire.EventReply, 1, eventID("ahead"), h.signer, h.clock.Now().Add(300*time.Second))
	wantSent(t, h.push(body, sig))
	h.clock.Add(590 * time.Second)
	wantRefusal(t, h.push(body, sig), Replayed)
	h.clock.Add(11 * time.Second)
	wantRefusal(t, h.push(body, sig), StaleSignature)
}

// The window is bounded: past its size the entry that leaves soonest goes.
func TestTheReplayWindowIsBounded(t *testing.T) {
	r := newReplays(3)
	now := time.Unix(1_800_000_000, 0)
	for i, d := range []time.Duration{10, 30, 20} {
		if !r.accept([]byte{byte(i)}, now.Add(d*time.Second), now) {
			t.Fatal("a new signature was a repeat")
		}
	}
	if !r.accept([]byte{9}, now.Add(40*time.Second), now) || r.len() != 3 {
		t.Fatalf("the window holds %d", r.len())
	}
	if !r.accept([]byte{0}, now.Add(time.Minute), now) {
		t.Fatal("the soonest to leave was kept past the bound")
	}
	if r.accept([]byte{1}, now.Add(time.Minute), now) {
		t.Fatal("a later one was dropped first")
	}
	if !r.accept([]byte{1}, now.Add(time.Minute), now.Add(31*time.Second)) {
		t.Fatal("an entry past its time was still held")
	}
}

// Two pushes alike but for their event ids are two signed bodies, and both
// are sent.
func TestTwoPushesAlikeButForTheirEventIDsAreBothSent(t *testing.T) {
	h := newHarness(t)
	in := h.register(newToken(), pushwire.EnvironmentProduction)
	wantSent(t, h.push(h.signed(in, pushwire.EventReply, 3, eventID("one"), h.signer, h.clock.Now())))
	wantSent(t, h.push(h.signed(in, pushwire.EventReply, 3, eventID("two"), h.signer, h.clock.Now())))
	if h.prod.count() != 2 {
		t.Fatalf("Apple got %d", h.prod.count())
	}
}

func limits(l Limits) func(*Config) { return func(c *Config) { c.Limits = l } }

var roomy = Limits{Install: 1000, Caller: 1000, Register: 1000, Total: 1000, RegisterTotal: 1000, RefusedTotal: 1000}

func with(f func(*Limits)) Limits { l := roomy; f(&l); return l }

func TestAnInstallsPushesAreLimitedOnlyOnceTheyVerify(t *testing.T) {
	h := newHarness(t, limits(with(func(l *Limits) { l.Install = 3 })))
	in := h.register(newToken(), pushwire.EnvironmentProduction)
	other := pushwiretest.LoadKeys(t).Other(t)
	// A stranger with the push key and no signing key spends nothing of the
	// install's allowance.
	for i := range 20 {
		wantRefusal(t, h.push(h.signed(in, pushwire.EventReply, 1, eventID(fmt.Sprint("stranger", i)), other, h.clock.Now())), SignatureRefused)
	}
	for i := range 3 {
		wantSent(t, h.push(h.signed(in, pushwire.EventReply, 1, eventID(fmt.Sprint("ok", i)), h.signer, h.clock.Now())))
	}
	w := h.push(h.signed(in, pushwire.EventReply, 1, eventID("fourth"), h.signer, h.clock.Now()))
	wantRefusal(t, w, RateLimited)
	if ra, _ := strconv.Atoi(w.Header().Get("Retry-After")); ra < 1 || ra > 20 {
		t.Fatalf("Retry-After %q", w.Header().Get("Retry-After"))
	}
	// A new registration of the same install shares its bucket.
	again := h.register(in.token, pushwire.EnvironmentProduction)
	wantRefusal(t, h.push(h.signed(again, pushwire.EventReply, 1, eventID("again"), h.signer, h.clock.Now())), RateLimited)
	// Another install has its own.
	fresh := h.register(newToken(), pushwire.EnvironmentProduction)
	wantSent(t, h.push(h.signed(fresh, pushwire.EventReply, 1, eventID("fresh"), h.signer, h.clock.Now())))
	// A minute refills it.
	h.clock.Add(time.Minute)
	wantSent(t, h.push(h.signed(in, pushwire.EventReply, 1, eventID("a minute later"), h.signer, h.clock.Now())))
}

func TestTheTotalPushesAreLimited(t *testing.T) {
	h := newHarness(t, limits(with(func(l *Limits) { l.Total = 2 })))
	for i := range 2 {
		in := h.register(newToken(), pushwire.EnvironmentProduction)
		wantSent(t, h.push(h.signed(in, pushwire.EventReply, 1, eventID(fmt.Sprint(i)), h.signer, h.clock.Now())))
	}
	in := h.register(newToken(), pushwire.EnvironmentProduction)
	wantRefusal(t, h.push(h.signed(in, pushwire.EventReply, 1, eventID("third"), h.signer, h.clock.Now())), RateLimited)
}

func TestACallersRequestsAreLimited(t *testing.T) {
	h := newHarness(t, limits(with(func(l *Limits) { l.Caller = 2 })))
	for range 2 {
		h.do(http.MethodPost, pushwire.PathPush, []byte(`{}`), nil)
	}
	wantRefusal(t, h.do(http.MethodPost, pushwire.PathPush, []byte(`{}`), nil), RateLimited)
	// Health is not /v1/ and is never limited.
	if w := h.do(http.MethodGet, "/healthz", nil, nil); w.Code != http.StatusOK {
		t.Fatalf("/healthz = %d", w.Code)
	}
	// Another caller has its own allowance.
	r := httptest.NewRequest(http.MethodPost, pushwire.PathPush, strings.NewReader(`{}`))
	r.RemoteAddr = "203.0.113.5:1000"
	w := httptest.NewRecorder()
	h.relay.ServeHTTP(w, r)
	wantRefusal(t, w, BadRequest(""))
}

// Behind a proxy the operator lists, each client the proxy names has its own
// allowance.
func TestACallerBehindATrustedProxyIsTheClientItNames(t *testing.T) {
	h := newHarness(t, limits(with(func(l *Limits) { l.Caller = 1 })), func(c *Config) {
		c.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("198.51.100.0/24")}
	})
	for _, client := range []string{"203.0.113.1", "203.0.113.2"} {
		wantRefusal(t, h.do(http.MethodPost, pushwire.PathPush, []byte(`{}`), map[string]string{"X-Forwarded-For": client}), BadRequest(""))
	}
	wantRefusal(t, h.do(http.MethodPost, pushwire.PathPush, []byte(`{}`), map[string]string{"X-Forwarded-For": "203.0.113.1"}), RateLimited)
}

func TestRegistrationsAreLimited(t *testing.T) {
	h := newHarness(t, limits(with(func(l *Limits) { l.Register = 2 })))
	h.register(newToken(), pushwire.EnvironmentProduction)
	h.register(newToken(), pushwire.EnvironmentProduction)
	body, _ := json.Marshal(pushwire.RegisterRequest{})
	wantRefusal(t, h.do(http.MethodPost, pushwire.PathRegister, body, nil), RateLimited)

	h = newHarness(t, limits(with(func(l *Limits) { l.RegisterTotal = 1 })))
	h.register(newToken(), pushwire.EnvironmentProduction)
	r := httptest.NewRequest(http.MethodPost, pushwire.PathRegister, bytes.NewReader(body))
	r.RemoteAddr = "203.0.113.5:1000"
	w := httptest.NewRecorder()
	h.relay.ServeHTTP(w, r)
	wantRefusal(t, w, RateLimited)
}

// Past the refused total a refusal is answered rate_limited and logged no
// more, and a push that verifies is still sent.
func TestPastTheRefusedTotalARefusalIsRateLimitedAndASignedPushIsNot(t *testing.T) {
	h := newHarness(t, limits(with(func(l *Limits) { l.RefusedTotal = 2 })))
	in := h.register(newToken(), pushwire.EnvironmentProduction)
	for range 2 {
		wantRefusal(t, h.do(http.MethodPost, pushwire.PathPush, []byte(`{}`), nil), BadRequest(""))
	}
	logged := strings.Count(h.logs.String(), "refused a request")
	wantRefusal(t, h.do(http.MethodPost, pushwire.PathPush, []byte(`{}`), nil), RateLimited)
	if strings.Count(h.logs.String(), "refused a request") != logged {
		t.Error("a refusal past the refused total was logged")
	}
	wantSent(t, h.push(h.signed(in, pushwire.EventReply, 1, eventID("still sent"), h.signer, h.clock.Now())))
}

func TestABodyOver4KiBIsRefused(t *testing.T) {
	h := newHarness(t)
	big := []byte(`{"pushKey":"` + strings.Repeat("a", pushwire.MaxBody) + `"}`)
	wantRefusal(t, h.push(big, ""), TooLarge)
	// Without a length, as a chunked body arrives.
	r := httptest.NewRequest(http.MethodPost, pushwire.PathPush, io.MultiReader(bytes.NewReader(big)))
	r.ContentLength = -1
	r.RemoteAddr = "198.51.100.7:1"
	w := httptest.NewRecorder()
	h.relay.ServeHTTP(w, r)
	wantRefusal(t, w, TooLarge)
	if h.prod.count() != 0 {
		t.Fatal("a body over the bound reached Apple")
	}
}

func TestUnknownRoutesAndMethodsAreRefused(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{"/", "/v1/", "/v1/pushes", "/v2/push", "/push", "/metrics"} {
		wantRefusal(t, h.do(http.MethodPost, path, nil, nil), NotFound)
	}
	for _, path := range []string{pushwire.PathPush, pushwire.PathRegister} {
		for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
			w := h.do(method, path, nil, nil)
			wantRefusal(t, w, MethodNotAllowed)
			if w.Header().Get("Allow") != http.MethodPost {
				t.Errorf("%s %s: Allow %q", method, path, w.Header().Get("Allow"))
			}
		}
	}
}

func TestHealthAndReadiness(t *testing.T) {
	h := newHarness(t)
	if w := h.do(http.MethodGet, "/healthz", nil, nil); w.Code != http.StatusOK {
		t.Fatalf("/healthz = %d", w.Code)
	}
	if w := h.do(http.MethodGet, "/readyz", nil, nil); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz before ready = %d", w.Code)
	}
	h.relay.MarkReady()
	if w := h.do(http.MethodGet, "/readyz", nil, nil); w.Code != http.StatusOK {
		t.Fatalf("/readyz = %d", w.Code)
	}
	if w := h.do(http.MethodPost, "/healthz", nil, nil); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /healthz = %d", w.Code)
	}
	if h.prod.count()+h.dev.count() != 0 {
		t.Fatal("a health check called Apple")
	}
}

// No log line, at any level, holds a push key, Apple's token, a public key,
// an event id, a thread or a badge, and a sent push's lines hold no caller
// address.
func TestNoLogLineHoldsWhatAPushCarries(t *testing.T) {
	h := newHarness(t)
	const thread, badge = "threadseventeen7x", 777
	token := strings.Repeat("c0ffee", 10) + "beef"
	in := h.register(token, pushwire.EnvironmentProduction)
	secrets := []string{in.pushKey, token, strings.ToUpper(token), h.keys.PublicKey, in.generation, thread, strconv.Itoa(badge)}
	send := func(event, label string, answer func(apns.Notification) (apns.Response, error)) {
		h.prod.answer = answer
		id := eventID(label)
		secrets = append(secrets, id)
		body, _ := json.Marshal(pushwire.PushRequest{PushKey: in.pushKey, Event: event, Thread: thread, Badge: badge, EventID: id})
		h.push(body, pushwire.Sign(h.signer, h.clock.Now(), body))
	}
	send(pushwire.EventReply, "sent", nil)
	send(pushwire.EventRead, "gone", func(apns.Notification) (apns.Response, error) {
		return apns.Response{Status: 410, Reason: "Unregistered", ID: "x"}, nil
	})
	send(pushwire.EventRequest, "bad key", func(apns.Notification) (apns.Response, error) {
		return apns.Response{Status: 403, Reason: "InvalidProviderToken", ID: "x"}, nil
	})
	send(pushwire.EventFailed, "unknown", func(apns.Notification) (apns.Response, error) {
		return apns.Response{Status: 418, Reason: "Teapot", ID: "x"}, nil
	})
	send(pushwire.EventReply, "no answer", func(apns.Notification) (apns.Response, error) {
		return apns.Response{}, errors.New("dial tcp: connection refused")
	})
	// Refusals: a bad signature, a replay, a stale one, a bad badge, a bad
	// thread, a refused key and a stranger's body.
	body, _ := json.Marshal(pushwire.PushRequest{PushKey: in.pushKey, Event: "reply", Thread: thread, Badge: badge, EventID: eventID("refused")})
	secrets = append(secrets, eventID("refused"))
	h.push(body, pushwire.Sign(pushwiretest.LoadKeys(t).Other(t), h.clock.Now(), body))
	h.push(body, pushwire.Sign(h.signer, h.clock.Now().Add(-time.Hour), body))
	h.push([]byte(strings.Replace(string(body), `"badge":777`, `"badge":7770`, 1)), "")
	h.push([]byte(strings.Replace(string(body), thread, thread+" x", 1)), "")
	h.push([]byte(strings.Replace(string(body), in.pushKey, in.pushKey[:40], 1)), "")
	h.do(http.MethodPost, pushwire.PathRegister, []byte(`{"platform":"apns","token":"`+token+`","topic":"other.topic","environment":"production","publicKey":"`+h.keys.PublicKey+`","generation":"`+in.generation+`"}`), nil)

	logs := h.logs.String()
	if logs == "" {
		t.Fatal("nothing was logged")
	}
	for _, s := range secrets {
		if strings.Contains(logs, s) {
			t.Errorf("a log line holds %.24s…", s)
		}
	}
	if strings.Contains(logs, `"badge"`) || strings.Contains(logs, `"thread"`) {
		t.Error("a log line has a badge or thread attribute")
	}
	for line := range strings.SplitSeq(strings.TrimSpace(logs), "\n") {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		if rec["code"] == pushwire.StatusSent && rec["caller"] != nil {
			t.Errorf("a sent push's line names its caller: %s", line)
		}
		if rec["code"] != pushwire.StatusSent && rec["code"] != codeRegistered && rec["msg"] == "push relay refused a request" && rec["caller"] == nil {
			t.Errorf("a refusal's line names no caller: %s", line)
		}
	}
	if !strings.Contains(logs, Fingerprint(in.pushKey)) {
		t.Error("no line names the push key by its fingerprint")
	}
}
