package push

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/authority"
	"github.com/geoah/substrate/internal/build"
	"github.com/geoah/substrate/internal/egress"
	"github.com/geoah/substrate/internal/pushwire"
	"github.com/geoah/substrate/internal/pushwire/pushwiretest"
	"github.com/geoah/substrate/internal/substrate"
)

// keySigner signs with one Ed25519 key; it seals nothing.
type keySigner struct{ key ed25519.PrivateKey }

func newKeySigner() keySigner {
	seed := sha256.Sum256([]byte("push test signer"))
	return keySigner{key: ed25519.NewKeyFromSeed(seed[:])}
}

func (s keySigner) PublicKey() ed25519.PublicKey { return s.key.Public().(ed25519.PublicKey) }
func (s keySigner) Sign(t time.Time, body []byte) string {
	return pushwire.Sign(s.key, t, body)
}
func (keySigner) SealEvent(substrate.PushEvent) (string, error) { return "", errors.New("unused") }
func (keySigner) OpenEvent(string) (substrate.PushEvent, error) {
	return substrate.PushEvent{}, substrate.ErrNotFound
}

var testRequest = pushwire.PushRequest{
	PushKey: "v1.Qm9vbGVhbi4uLg", Event: pushwire.EventReply, Thread: "8f2c41d07a", Badge: 3,
	EventID: strings.Repeat("e", 40),
}

// The request is the push's JSON, signed so the relay's own check accepts
// it, named and typed as the plan says, at the relay's push address.
func TestSendSignsTheBody(t *testing.T) {
	now := time.Unix(1763459123, 0)
	signer := newKeySigner()
	var got struct {
		path, ua, ct, sig string
		body              []byte
	}
	relay := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path, got.ua, got.ct, got.sig = r.URL.Path, r.UserAgent(), r.Header.Get("Content-Type"), r.Header.Get(pushwire.SignatureHeader)
		got.body, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(pushwire.PushResponse{Status: pushwire.StatusSent})
	}))
	defer relay.Close()
	c := New(Options{HTTP: relay.Client(), Now: func() time.Time { return now }})
	answer := c.Send(context.Background(), relay.URL+"/prefix/", signer, testRequest)
	if answer.Outcome != Sent || answer.Status != http.StatusOK || answer.Err != nil {
		t.Fatalf("answered %+v", answer)
	}
	want, _ := json.Marshal(testRequest)
	if !bytes.Equal(got.body, want) {
		t.Fatalf("sent %s, want %s", got.body, want)
	}
	if got.path != "/prefix"+pushwire.PathPush {
		t.Fatalf("posted to %s", got.path)
	}
	if got.ua != "substrate/"+build.Version() || got.ct != "application/json" {
		t.Fatalf("User-Agent %q, Content-Type %q", got.ua, got.ct)
	}
	if err := pushwire.Verify(got.sig, signer.PublicKey(), now, got.body); err != nil {
		t.Fatalf("the relay's check refuses the signature: %v", err)
	}
	// Signed over the bytes sent, so another body does not verify.
	if err := pushwire.Verify(got.sig, signer.PublicKey(), now, append(got.body, ' ')); !errors.Is(err, pushwire.ErrRefused) {
		t.Fatalf("a changed body verified: %v", err)
	}
}

// Every refusal the relay answers is classified by the plan's table, read
// from the wire's own fixtures.
func TestEveryRelayAnswerIsClassified(t *testing.T) {
	want := map[string]Outcome{
		pushwire.CodeBadRequest:         Failed,
		pushwire.CodeStaleSignature:     Failed,
		pushwire.CodeSignatureRefused:   Delete,
		pushwire.CodeKeyRefused:         Delete,
		pushwire.CodeKeyExpired:         Delete,
		pushwire.CodeTopicRefused:       Delete,
		pushwire.CodeEnvironmentRefused: Delete,
		pushwire.CodeReplayed:           Failed,
		pushwire.CodeGone:               Delete,
		pushwire.CodeRateLimited:        Limited,
		pushwire.CodeUpstream:           Failed,
		pushwire.CodeInternal:           Failed,
	}
	for _, code := range pushwire.Codes {
		if _, ok := want[code]; !ok {
			t.Errorf("code %s has no expected outcome", code)
		}
	}
	var refusals []pushwiretest.Refusal
	pushwiretest.Read(t, "errors.json", &refusals)
	for _, ref := range refusals {
		var env pushwire.ErrorResponse
		if err := json.Unmarshal(ref.Body, &env); err != nil {
			t.Fatal(err)
		}
		a := classify(ref.Status, ref.Body)
		if a.Outcome != want[env.Error.Code] || a.Code != env.Error.Code || a.Status != ref.Status {
			t.Errorf("%d %s classified %+v, want %s", ref.Status, env.Error.Code, a, want[env.Error.Code])
		}
	}
	for _, c := range []struct {
		status int
		body   string
		want   Outcome
	}{
		{http.StatusOK, `{"status":"sent"}`, Sent},
		{http.StatusTooManyRequests, `not json`, Limited},
		{http.StatusGone, `not json`, Failed},
		{http.StatusBadGateway, ``, Failed},
		{http.StatusForbidden, `{"error":{"code":"something_new"}}`, Failed},
	} {
		if a := classify(c.status, []byte(c.body)); a.Outcome != c.want {
			t.Errorf("%d %q classified %s, want %s", c.status, c.body, a.Outcome, c.want)
		}
	}
}

// A relay that answers a redirect is not followed: the push fails and the
// address it names is never asked.
func TestARedirectIsNotFollowed(t *testing.T) {
	var followed atomic.Bool
	target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { followed.Store(true) }))
	defer target.Close()
	relay := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+pushwire.PathPush, http.StatusTemporaryRedirect)
	}))
	defer relay.Close()
	c := New(Options{HTTP: egress.CredentialClient(relay.Client().Transport, Timeout)})
	a := c.Send(context.Background(), relay.URL, newKeySigner(), testRequest)
	var redirect *egress.RedirectError
	if a.Outcome != Failed || !errors.As(a.Err, &redirect) {
		t.Fatalf("answered %+v", a)
	}
	if followed.Load() {
		t.Fatal("the redirect was followed")
	}
}

// The default client dials public addresses alone: a relay on loopback is
// never reached.
func TestTheDefaultClientRefusesAPrivateAddress(t *testing.T) {
	t.Setenv("SUBSTRATE_EGRESS_ALLOW", "")
	var reached atomic.Bool
	relay := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached.Store(true) }))
	defer relay.Close()
	a := New(Options{}).Send(context.Background(), relay.URL, newKeySigner(), testRequest)
	if a.Outcome != Failed || a.Err == nil {
		t.Fatalf("answered %+v", a)
	}
	if reached.Load() {
		t.Fatal("a loopback relay was reached")
	}
}

// At most 4 KiB goes out, and at most 4 KiB of the answer is read.
func TestTheBoundsOfARequest(t *testing.T) {
	var asked atomic.Int32
	relay := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked.Add(1)
		w.WriteHeader(http.StatusGone)
		// The envelope sits past the first 4 KiB, so a reader held to the
		// bound never reads its code.
		_, _ = w.Write(bytes.Repeat([]byte(" "), maxAnswer))
		_, _ = w.Write([]byte(`{"error":{"code":"gone","message":"x"}}`))
	}))
	defer relay.Close()
	c := New(Options{HTTP: relay.Client()})
	big := testRequest
	big.PushKey = strings.Repeat("k", pushwire.MaxBody)
	if a := c.Send(context.Background(), relay.URL, newKeySigner(), big); a.Outcome != Failed || a.Err == nil {
		t.Fatalf("a body past %d bytes answered %+v", pushwire.MaxBody, a)
	}
	if asked.Load() != 0 {
		t.Fatal("a body past the bound was sent")
	}
	a := c.Send(context.Background(), relay.URL, newKeySigner(), testRequest)
	if a.Outcome != Failed || a.Code != "" || a.Status != http.StatusGone {
		t.Fatalf("an answer past 4 KiB was read whole: %+v", a)
	}
}

// A relay that is not one is refused before anything is sent.
func TestAnInvalidRelayIsRefused(t *testing.T) {
	for _, relay := range []string{"http://push.example.com", "https://push.example.com?x=1", "push.example.com"} {
		if a := New(Options{}).Send(context.Background(), relay, newKeySigner(), testRequest); a.Outcome != Failed || a.Err == nil {
			t.Fatalf("%s answered %+v", relay, a)
		}
	}
}

type docs struct {
	doc authority.AuthorityDocument
	err error
	n   int
}

func (d *docs) Read(_ context.Context, name string, fresh bool) (authority.Source, error) {
	d.n++
	if fresh {
		return authority.Source{}, errors.New("the relay is read from the cache")
	}
	if d.err != nil {
		return authority.Source{}, d.err
	}
	if name != d.doc.Authority {
		return authority.Source{}, fmt.Errorf("no document for %s", name)
	}
	return authority.Source{Document: d.doc}, nil
}

const clientID = "https://apps.example.com/apps/chat/client.json"

func TestRelayIsTheVerifiedAppsDocuments(t *testing.T) {
	appToken := substrate.TokenInfo{ID: "tok1", ClientID: clientID, AppAuthority: "example.com"}
	withPush := authority.AuthorityDocument{Authority: "example.com", Apps: []authority.AuthorityApp{
		{ClientID: clientID, Push: &authority.AppPush{Relay: "https://push.example.com/"}},
	}}
	d := &docs{doc: withPush}
	relay, err := Relay(context.Background(), d, appToken)
	if err != nil || relay != "https://push.example.com/" {
		t.Fatalf("relay %q, %v", relay, err)
	}
	for name, c := range map[string]struct {
		tok  substrate.TokenInfo
		docs *docs
		want error
		read bool
	}{
		"a password sign-in": {substrate.TokenInfo{ID: "tok1"}, &docs{doc: withPush}, ErrNoRelay, false},
		"no client id":       {substrate.TokenInfo{ID: "tok1", AppAuthority: "example.com"}, &docs{doc: withPush}, ErrNoRelay, false},
		"an unverified app":  {substrate.TokenInfo{ID: "tok1", ClientID: clientID}, &docs{doc: withPush}, ErrNoRelay, false},
		"a local app":        {substrate.TokenInfo{ID: "tok1", ClientID: clientID, AppAuthority: "example.com", AppLocal: true}, &docs{doc: withPush}, ErrNoRelay, false},
		"an app the document does not list": {
			substrate.TokenInfo{ID: "tok1", ClientID: "https://apps.example.com/other.json", AppAuthority: "example.com"},
			&docs{doc: withPush}, ErrNoRelay, true,
		},
		"an app with no push": {appToken, &docs{doc: authority.AuthorityDocument{
			Authority: "example.com",
			Apps:      []authority.AuthorityApp{{ClientID: clientID}},
		}}, ErrNoRelay, true},
		"an unreadable document": {appToken, &docs{err: errors.New("dns")}, ErrUnreadable, true},
	} {
		got, err := Relay(context.Background(), c.docs, c.tok)
		if !errors.Is(err, c.want) || got != "" {
			t.Errorf("%s: relay %q, %v, want %v", name, got, err, c.want)
		}
		if read := c.docs.n > 0; read != c.read {
			t.Errorf("%s: read the document %d times", name, c.docs.n)
		}
	}
	if _, err := Relay(context.Background(), nil, appToken); !errors.Is(err, ErrNoRelay) {
		t.Fatalf("no reader: %v", err)
	}
}

// pushStore counts the badge over its threads.
type pushStore struct {
	substrate.PushStore
	threads []*substrate.Record
}

func (s pushStore) PushBadgeThreads(context.Context, string) ([]*substrate.Record, error) {
	return s.threads, nil
}

func TestTheBadgeIsWhatTheViewReadsAndStopsAt999(t *testing.T) {
	var threads []*substrate.Record
	for i := range 1500 {
		threads = append(threads, &substrate.Record{ID: fmt.Sprint(i)})
	}
	all := func(context.Context, *substrate.Record) (bool, error) { return true, nil }
	if n, err := Badge(context.Background(), pushStore{threads: threads}, all, "p"); err != nil || n != substrate.PushBadgeMax {
		t.Fatalf("badge %d, %v", n, err)
	}
	odd := func(_ context.Context, r *substrate.Record) (bool, error) { return len(r.ID)%2 == 1, nil }
	want := 0
	for _, th := range threads[:20] {
		if len(th.ID)%2 == 1 {
			want++
		}
	}
	if n, _ := Badge(context.Background(), pushStore{threads: threads[:20]}, odd, "p"); n != want {
		t.Fatalf("badge %d, want %d", n, want)
	}
}
