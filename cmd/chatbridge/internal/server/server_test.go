package server

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/cmd/chatbridge/internal/contract"
	"github.com/geoah/substrate/cmd/chatbridge/internal/store"
)

const testToken = "substrate_tok_test"

// door is the webhook door, the instance row and the parked list of a
// substrate, as far as the pusher reaches them.
type door struct {
	t       *testing.T
	srv     *httptest.Server
	mu      sync.Mutex
	batches []contract.Batch
	files   [][]string
	applied int64
	parked  []Parked
	fires   int
	// apply decides what the function does with a delivery: apply it,
	// park it, or leave it filing.
	apply func(n int, b contract.Batch) string
}

func newDoor(t *testing.T) *door {
	d := &door{t: t, apply: func(int, contract.Batch) string { return "apply" }}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhooks/example.com/chatbridge-events", d.deliver)
	mux.HandleFunc("GET /api/v1/records", d.records)
	mux.HandleFunc("GET /api/v1/substrate.reamde.dev/core/trigger/chatbridge-events/parked", func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		defer d.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"items": d.parked})
	})
	d.srv = httptest.NewServer(mux)
	t.Cleanup(d.srv.Close)
	return d
}

func (d *door) deliver(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+testToken {
		http.Error(w, `{"error":{"code":"not_found","message":"no"}}`, http.StatusNotFound)
		return
	}
	mt, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/form-data" {
		d.t.Errorf("content type %q", r.Header.Get("Content-Type"))
		return
	}
	mr := multipartReader(r.Body, params["boundary"])
	var batch contract.Batch
	var files []string
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			d.t.Errorf("reading a part: %v", err)
			return
		}
		body, _ := io.ReadAll(p)
		if p.FormName() == contract.BatchPart {
			// The door hands a part inline only with no filename and a text
			// media type.
			if p.FileName() != "" || p.Header.Get("Content-Type") != contract.BatchContentType {
				d.t.Errorf("the batch part has filename %q and type %q", p.FileName(), p.Header.Get("Content-Type"))
			}
			if err := json.Unmarshal(body, &batch); err != nil {
				d.t.Errorf("the batch is not JSON: %v", err)
			}
			continue
		}
		if p.FileName() == "" {
			d.t.Errorf("file part %q has no filename", p.FormName())
		}
		files = append(files, p.FormName())
	}
	d.mu.Lock()
	d.fires++
	fire := fmt.Sprintf("hook-%d", d.fires)
	d.batches = append(d.batches, batch)
	d.files = append(d.files, files)
	n := len(d.batches)
	switch d.apply(n, batch) {
	case "apply":
		if batch.First <= d.applied+1 {
			d.applied = max(d.applied, batch.Last)
		}
	case "park":
		d.parked = append(d.parked, Parked{ID: int64(n), FireID: fire, Attempts: 3, LastError: "refused"})
	}
	d.mu.Unlock()
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"fire": fire})
}

func (d *door) records(w http.ResponseWriter, r *http.Request) {
	var f Filter
	_ = json.Unmarshal([]byte(r.URL.Query().Get("filter")), &f)
	d.mu.Lock()
	applied := d.applied
	d.mu.Unlock()
	recs := []Record{}
	if len(f.Kinds) == 1 && f.Kinds[0] == KindInstance {
		recs = append(recs, Record{ID: "inst", Kind: KindInstance, Properties: map[string]any{
			"instanceId": f.Properties["instanceId"]["eq"], "appliedSeq": applied,
		}})
	}
	_ = json.NewEncoder(w).Encode(Page{Records: recs})
}

func (d *door) snapshot() []contract.Batch {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]contract.Batch(nil), d.batches...)
}

func newPusher(t *testing.T, d *door, st *store.Store) *Pusher {
	t.Helper()
	c, err := NewClient(d.srv.URL, func() string { return testToken }, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &Pusher{
		Store: st, Client: c, Instance: "inst-1",
		WebhookURL: func() string { return d.srv.URL + "/webhooks/example.com/chatbridge-events" },
		Log:        slog.New(slog.DiscardHandler),
		Poll:       10 * time.Millisecond, Backoff: 10 * time.Millisecond, MaxBackoff: 20 * time.Millisecond,
	}
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func appendEvents(t *testing.T, st *store.Store, n int, media ...string) {
	t.Helper()
	err := st.Do(context.Background(), func(tx *store.Tx) error {
		for i := range n {
			ev := &contract.ChatDelete{Head: contract.Head{Type: contract.TypeChatDelete, Network: "whatsapp", Account: "1"}, Chat: fmt.Sprint(i)}
			if _, err := tx.Append(context.Background(), ev, media...); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestPusherKeepsOneBatchInFlight(t *testing.T) {
	d := newDoor(t)
	hold := make(chan struct{})
	d.apply = func(n int, _ contract.Batch) string {
		if n == 1 {
			go func() { <-hold; d.mu.Lock(); d.applied = 200; d.mu.Unlock() }()
			return "file"
		}
		return "apply"
	}
	st := openStore(t)
	appendEvents(t, st, 250)
	p := newPusher(t, d, st)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = p.Run(ctx) }()

	waitFor(t, "the first batch", func() bool { return len(d.snapshot()) == 1 })
	time.Sleep(100 * time.Millisecond)
	if b := d.snapshot(); len(b) != 1 {
		t.Fatalf("%d batches left before the first applied", len(b))
	}
	first := d.snapshot()[0]
	if first.Version != contract.Version || first.Instance != "inst-1" || first.First != 1 || first.Last != 200 || len(first.Events) != 200 {
		t.Fatalf("first batch %d..%d of %d", first.First, first.Last, len(first.Events))
	}
	close(hold)
	waitFor(t, "the second batch", func() bool { return len(d.snapshot()) == 2 })
	second := d.snapshot()[1]
	if second.First != 201 || second.Last != 250 {
		t.Fatalf("second batch %d..%d", second.First, second.Last)
	}
	waitFor(t, "the outbox to drain", func() bool {
		n, _ := st.Depth(context.Background(), 0)
		return n == 0
	})
}

func TestPusherResendsAParkedBatch(t *testing.T) {
	d := newDoor(t)
	d.apply = func(n int, _ contract.Batch) string {
		if n == 1 {
			return "park"
		}
		return "apply"
	}
	st := openStore(t)
	appendEvents(t, st, 3)
	p := newPusher(t, d, st)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = p.Run(ctx) }()
	waitFor(t, "the resend to apply", func() bool {
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.applied == 3
	})
	b := d.snapshot()
	if len(b) != 2 || b[0].First != b[1].First || b[0].Last != b[1].Last {
		t.Fatalf("batches %+v", b)
	}
	if p.Metrics.BatchesParked.Load() != 1 || p.Metrics.Resends.Load() != 1 {
		t.Fatalf("parked %d resent %d", p.Metrics.BatchesParked.Load(), p.Metrics.Resends.Load())
	}
}

func TestPusherCarriesTheFilesABatchNames(t *testing.T) {
	d := newDoor(t)
	st := openStore(t)
	info, err := st.PutMedia(context.Background(), []byte("jpeg bytes"), "image/jpeg", "photo.jpg")
	if err != nil {
		t.Fatal(err)
	}
	appendEvents(t, st, 2, info.ID)
	p := newPusher(t, d, st)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = p.Run(ctx) }()
	waitFor(t, "the batch", func() bool { return len(d.snapshot()) == 1 })
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.files[0]) != 1 || d.files[0][0] != info.ID {
		t.Fatalf("files %v, want one part named %s", d.files[0], info.ID)
	}
}

func TestAuthorizeRunsPKCEOnALoopbackRedirect(t *testing.T) {
	var challenge string
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(Metadata{
			Issuer: srv.URL, AuthorizationEndpoint: srv.URL + "/oauth/authorize",
			TokenEndpoint: srv.URL + "/oauth/token", AuthorizationDetailsTypes: []string{"package", "account", "webhook"},
		})
	})
	mux.HandleFunc("POST /oauth/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if base64.RawURLEncoding.EncodeToString(sum[:]) != challenge || r.PostForm.Get("code") != "the-code" ||
			r.PostForm.Get("client_id") != ClientID {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": testToken, "token_type": "Bearer", "scope": strings.Join(Scopes, " ") + " webhook:" + Trigger,
			"expires_in": 2592000, "repository": "example.com", "token_id": "tok1",
			"authorization_details": []Detail{
				{Type: "package", Package: Package, Version: 1},
				{Type: "webhook", Trigger: Trigger, URL: srv.URL + "/webhooks/example.com/" + Trigger},
			},
		})
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	auth, err := Authorize(context.Background(), AuthorizeOptions{
		Server: srv.URL,
		Wait:   10 * time.Second,
		Show: func(u string) {
			parsed, _ := url.Parse(u)
			q := parsed.Query()
			challenge = q.Get("code_challenge")
			var details []map[string]any
			_ = json.Unmarshal([]byte(q.Get("authorization_details")), &details)
			if q.Get("client_id") != ClientID || q.Get("code_challenge_method") != "S256" || q.Get("response_type") != "code" ||
				q.Get("scope") != strings.Join(Scopes, " ") || len(details) != 2 || details[0]["package"] != Package ||
				details[0]["version"] != float64(1) || details[1]["trigger"] != Trigger {
				t.Errorf("authorization request %v", q)
			}
			redirect, _ := url.Parse(q.Get("redirect_uri"))
			if redirect.Hostname() != "127.0.0.1" {
				t.Errorf("redirect %s is not loopback", redirect)
			}
			cb := redirect.String() + "?" + url.Values{"code": {"the-code"}, "state": {q.Get("state")}, "iss": {srv.URL}}.Encode()
			go func() {
				resp, err := http.Get(cb)
				if err == nil {
					_ = resp.Body.Close()
				}
			}()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if auth.Token != testToken || auth.Repository != "example.com" || auth.TokenID != "tok1" || auth.ExpiresAt == nil ||
		auth.WebhookURL != srv.URL+"/webhooks/example.com/"+Trigger {
		t.Fatalf("auth %+v", auth)
	}
}

func TestSplitPath(t *testing.T) {
	kind, id, ok := SplitPath(KindChat + "/abc")
	if !ok || kind != KindChat || id != "abc" {
		t.Fatalf("%q %q %v", kind, id, ok)
	}
	if _, _, ok := SplitPath(KindChat); ok {
		t.Fatal("a kind alone split")
	}
}

func multipartReader(r io.Reader, boundary string) *multipart.Reader {
	return multipart.NewReader(r, boundary)
}
