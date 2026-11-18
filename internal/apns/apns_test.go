package apns

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// clock is a fake time the tests move by hand.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func p8(t *testing.T, key any) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

// apple is a fake Apple: an HTTP/2 server with TLS that checks each
// request's provider token against the one key it trusts and answers what
// the test says.
type apple struct {
	t      *testing.T
	srv    *httptest.Server
	pub    *ecdsa.PublicKey
	keyID  string
	teamID string
	answer func(token string, r *http.Request) (int, string)
	seen   atomic.Int32
}

func newApple(t *testing.T, key *ecdsa.PrivateKey, keyID string, answer func(token string, r *http.Request) (int, string)) *apple {
	t.Helper()
	a := &apple{t: t, pub: &key.PublicKey, keyID: keyID, teamID: "TEAM000001", answer: answer}
	a.srv = httptest.NewUnstartedServer(http.HandlerFunc(a.serve))
	a.srv.EnableHTTP2 = true
	a.srv.StartTLS()
	t.Cleanup(a.srv.Close)
	return a
}

func (a *apple) serve(w http.ResponseWriter, r *http.Request) {
	a.seen.Add(1)
	if r.ProtoMajor != 2 {
		a.t.Errorf("Apple was spoken to over %s, not HTTP/2", r.Proto)
	}
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "bearer ")
	if !ok || !a.verify(tok) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"reason":"InvalidProviderToken"}`)
		return
	}
	status, reason := http.StatusOK, ""
	if a.answer != nil {
		status, reason = a.answer(tok, r)
	}
	w.Header().Set("apns-id", "00000000-0000-0000-0000-000000000001")
	w.WriteHeader(status)
	if reason != "" {
		_ = json.NewEncoder(w).Encode(map[string]string{"reason": reason})
	}
}

// verify checks a provider token the way Apple does: the header, the
// claims and the ES256 signature under the key it trusts.
func (a *apple) verify(tok string) bool {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return false
	}
	var header struct{ Alg, Kid string }
	var claims struct {
		Iss string `json:"iss"`
		Iat int64  `json:"iat"`
	}
	h, _ := base64.RawURLEncoding.DecodeString(parts[0])
	c, _ := base64.RawURLEncoding.DecodeString(parts[1])
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	if json.Unmarshal(h, &header) != nil || json.Unmarshal(c, &claims) != nil || len(sig) != 64 {
		return false
	}
	if header.Alg != "ES256" || header.Kid != a.keyID || claims.Iss != a.teamID || claims.Iat == 0 {
		return false
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	return ecdsa.Verify(a.pub, sum[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:]))
}

func (a *apple) client(t *testing.T, key *ecdsa.PrivateKey, keyID string, now func() time.Time, mints *atomic.Int32) *Client {
	t.Helper()
	pool := x509.NewCertPool()
	pool.AddCert(a.srv.Certificate())
	c, err := New(Config{
		BaseURL: a.srv.URL, TeamID: a.teamID, KeyID: keyID, Key: key,
		Transport: NewTransport(&tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}),
		Now:       now,
		OnMint: func() {
			if mints != nil {
				mints.Add(1)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

var note = Notification{
	DeviceToken: strings.Repeat("ab", 32),
	Headers:     map[string]string{"apns-topic": "dev.example.app", "apns-push-type": "alert", "apns-priority": "10"},
	Payload:     []byte(`{"aps":{"badge":1}}`),
}

func TestParseKeyTakesAP8AndNothingElse(t *testing.T) {
	if _, err := ParseKey(p8(t, newKey(t))); err != nil {
		t.Fatalf("a P-256 .p8: %v", err)
	}
	p384, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	for name, b := range map[string][]byte{
		"P-384":      p8(t, p384),
		"RSA":        p8(t, rsaKey),
		"not PEM":    []byte("not a key"),
		"a cert PEM": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte{1}}),
		"bad PKCS#8": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte{1, 2, 3}}),
	} {
		if _, err := ParseKey(b); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}

// The provider token is the shape Apple reads, signed by the key, and Apple
// accepts it over HTTP/2.
func TestAProviderTokenIsSignedByTheKeyAndSent(t *testing.T) {
	key := newKey(t)
	a := newApple(t, key, "KEY0000001", nil)
	c := a.client(t, key, "KEY0000001", nil, nil)
	tok, err := c.Token()
	if err != nil {
		t.Fatal(err)
	}
	if !a.verify(tok) {
		t.Fatalf("Apple's check refuses the token %s", tok)
	}
	resp, err := c.Send(context.Background(), note)
	if err != nil || resp.Status != http.StatusOK || resp.ID == "" {
		t.Fatalf("Send = %+v, %v", resp, err)
	}
}

func TestAProviderTokenIsReusedFor50Minutes(t *testing.T) {
	key := newKey(t)
	clk := &clock{now: time.Unix(1_800_000_000, 0)}
	var mints atomic.Int32
	c := newApple(t, key, "KEY0000001", nil).client(t, key, "KEY0000001", clk.Now, &mints)
	first, _ := c.Token()
	clk.Add(TokenLifetime - time.Second)
	if again, _ := c.Token(); again != first || mints.Load() != 1 {
		t.Fatalf("a token 49:59 old was replaced (mints %d)", mints.Load())
	}
	clk.Add(time.Second)
	if next, _ := c.Token(); next == first || mints.Load() != 2 {
		t.Fatalf("a token 50 minutes old was reused (mints %d)", mints.Load())
	}
}

// Many sends waiting on a mint take the one token it mints.
func TestMintingIsOneAtATime(t *testing.T) {
	key := newKey(t)
	var mints atomic.Int32
	c := newApple(t, key, "KEY0000001", nil).client(t, key, "KEY0000001", nil, &mints)
	var wg sync.WaitGroup
	tokens := make([]string, 50)
	for i := range tokens {
		wg.Go(func() { tokens[i], _ = c.Token() })
	}
	wg.Wait()
	for _, tok := range tokens {
		if tok != tokens[0] {
			t.Fatal("concurrent sends got different tokens")
		}
	}
	if mints.Load() != 1 {
		t.Fatalf("%d mints for one token", mints.Load())
	}
}

// Many ExpiredProviderToken answers to one token cause one mint, and each
// of those sends goes once more with the new token.
func TestManyExpiredAnswersToOneTokenMintOnce(t *testing.T) {
	key := newKey(t)
	const n = 20
	var first atomic.Value
	arrived := make(chan struct{}, n)
	release := make(chan struct{})
	a := newApple(t, key, "KEY0000001", func(tok string, _ *http.Request) (int, string) {
		if tok == first.Load() {
			arrived <- struct{}{}
			<-release
			return http.StatusForbidden, ReasonExpiredProviderToken
		}
		return http.StatusOK, ""
	})
	var mints atomic.Int32
	c := a.client(t, key, "KEY0000001", nil, &mints)
	tok, _ := c.Token()
	first.Store(tok)

	var wg sync.WaitGroup
	results := make([]Response, n)
	for i := range n {
		wg.Go(func() {
			var err error
			if results[i], err = c.Send(context.Background(), note); err != nil {
				t.Error(err)
			}
		})
	}
	for range n {
		select {
		case <-arrived:
		case <-time.After(5 * time.Second):
			t.Fatal("the sends did not all reach Apple with the first token")
		}
	}
	close(release)
	wg.Wait()
	for _, r := range results {
		if r.Status != http.StatusOK {
			t.Fatalf("a send ended %v, want 200 after the new token", r)
		}
	}
	if mints.Load() != 2 {
		t.Fatalf("%d mints, want the first and one replacement", mints.Load())
	}
}

// An ExpiredProviderToken answered to a token that was already replaced
// mints nothing more.
func TestAnExpiredAnswerToAnOlderTokenMintsNothing(t *testing.T) {
	key := newKey(t)
	clk := &clock{now: time.Unix(1_800_000_000, 0)}
	var mints atomic.Int32
	c := newApple(t, key, "KEY0000001", nil).client(t, key, "KEY0000001", clk.Now, &mints)
	old, _ := c.Token()
	clk.Add(TokenLifetime)
	current, _ := c.Token()
	c.expire(old)
	if again, _ := c.Token(); again != current || mints.Load() != 2 {
		t.Fatalf("an answer to the older token replaced the current one (mints %d)", mints.Load())
	}
	c.expire(current)
	if next, _ := c.Token(); next == current || mints.Load() != 3 {
		t.Fatalf("an answer to the current token did not replace it (mints %d)", mints.Load())
	}
}

// Each environment has its own key, its own token and its own host: a
// client never sends one environment's token to the other's host.
func TestEachEnvironmentHasItsOwnKeyTokenAndHost(t *testing.T) {
	prodKey, devKey := newKey(t), newKey(t)
	prod := newApple(t, prodKey, "PROD000001", nil)
	dev := newApple(t, devKey, "DEV0000001", nil)
	pc := prod.client(t, prodKey, "PROD000001", nil, nil)
	dc := dev.client(t, devKey, "DEV0000001", nil, nil)
	for _, c := range []*Client{pc, dc} {
		if r, err := c.Send(context.Background(), note); err != nil || r.Status != http.StatusOK {
			t.Fatalf("Send = %v, %v", r, err)
		}
	}
	pt, _ := pc.Token()
	dt, _ := dc.Token()
	if pt == dt || !prod.verify(pt) || prod.verify(dt) || !dev.verify(dt) || dev.verify(pt) {
		t.Fatal("the two environments share a token or a key")
	}
	if prod.seen.Load() != 1 || dev.seen.Load() != 1 {
		t.Fatalf("hosts saw %d and %d sends", prod.seen.Load(), dev.seen.Load())
	}
}

func TestSendReadsWhatAppleAnswered(t *testing.T) {
	key := newKey(t)
	for _, tc := range []struct {
		status int
		reason string
	}{
		{http.StatusOK, ""},
		{http.StatusGone, "Unregistered"},
		{http.StatusBadRequest, "BadDeviceToken"},
		{http.StatusForbidden, ReasonInvalidProviderToken},
		{http.StatusTooManyRequests, "TooManyRequests"},
		{http.StatusServiceUnavailable, "ServiceUnavailable"},
	} {
		a := newApple(t, key, "KEY0000001", func(string, *http.Request) (int, string) { return tc.status, tc.reason })
		r, err := a.client(t, key, "KEY0000001", nil, nil).Send(context.Background(), note)
		if err != nil || r.Status != tc.status || r.Reason != tc.reason || r.ID == "" {
			t.Errorf("%d %s: Send = %+v, %v", tc.status, tc.reason, r, err)
		}
	}
}

func TestTheRequestCarriesTheHeadersAndPayload(t *testing.T) {
	key := newKey(t)
	var got *http.Request
	var body []byte
	a := newApple(t, key, "KEY0000001", func(_ string, r *http.Request) (int, string) {
		got = r
		body, _ = io.ReadAll(r.Body)
		return http.StatusOK, ""
	})
	if _, err := a.client(t, key, "KEY0000001", nil, nil).Send(context.Background(), note); err != nil {
		t.Fatal(err)
	}
	if got.Method != http.MethodPost || got.URL.Path != "/3/device/"+note.DeviceToken || string(body) != string(note.Payload) {
		t.Fatalf("%s %s %s", got.Method, got.URL.Path, body)
	}
	for k, v := range note.Headers {
		if got.Header.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, got.Header.Get(k), v)
		}
	}
}

// A send that gets no answer is an error, never a status: the host gone, a
// deadline passed.
func TestASendWithNoAnswerIsAnError(t *testing.T) {
	key := newKey(t)
	block := make(chan struct{})
	a := newApple(t, key, "KEY0000001", func(string, *http.Request) (int, string) { <-block; return http.StatusOK, "" })
	c := a.client(t, key, "KEY0000001", nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := c.Send(ctx, note); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a send past its deadline = %v", err)
	}
	close(block)
	a.srv.Close()
	if _, err := c.Send(context.Background(), note); err == nil {
		t.Error("a send to a closed host answered")
	}
}

func TestARedirectIsNotFollowed(t *testing.T) {
	key := newKey(t)
	a := newApple(t, key, "KEY0000001", nil)
	a.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/elsewhere" {
			t.Error("the redirect was followed")
		}
		http.Redirect(w, r, "/elsewhere", http.StatusTemporaryRedirect)
	})
	r, err := a.client(t, key, "KEY0000001", nil, nil).Send(context.Background(), note)
	if err != nil || r.Status != http.StatusTemporaryRedirect {
		t.Fatalf("Send = %+v, %v", r, err)
	}
}
