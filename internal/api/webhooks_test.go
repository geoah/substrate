package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// webhookSvc wraps the fake service with a webhook door that admits every
// trigger but the ones in refused, recording what it was handed and
// answering a canned outcome.
type webhookSvc struct {
	*fakeService
	mu      sync.Mutex
	calls   []webhookCall
	fid     string
	err     error
	refused map[string]bool
	// maxBody is the inline bound a trigger declares; one not named here
	// reads the default.
	maxBody map[string]int64
	// signatureOnly names the triggers with no key and a declared signature.
	signatureOnly map[string]bool
}

func (s *webhookSvc) CheckWebhook(_ context.Context, _, trigger, _ string) (substrate.WebhookTarget, error) {
	if s.refused[trigger] {
		return substrate.WebhookTarget{}, substrate.ErrNotFound
	}
	out := substrate.WebhookTarget{MaxBody: substrate.WebhookInlineDefault, SignatureOnly: s.signatureOnly[trigger]}
	if n := s.maxBody[trigger]; n > 0 {
		out.MaxBody = n
	}
	return out, nil
}

type webhookCall struct {
	authority, trigger, key string
	req                     substrate.WebhookRequest
}

func (s *webhookSvc) ReceiveWebhook(_ context.Context, authority, trigger, key string, req substrate.WebhookRequest) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, webhookCall{authority, trigger, key, req})
	return s.fid, s.err
}

func webhookHandler(fid string, err error) (*webhookSvc, http.Handler) {
	svc := &webhookSvc{fakeService: newFakeService(), fid: fid, err: err}
	return svc, New(Config{Service: svc})
}

func postHook(h http.Handler, path string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.RemoteAddr = "10.0.0.1:1234"
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func (s *webhookSvc) last(t *testing.T) webhookCall {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.calls) == 0 {
		t.Fatal("the door never reached the service")
	}
	return s.calls[len(s.calls)-1]
}

// No bearer, a JSON body: the door answers 202 with the fire id and hands the
// service the transport facts, byte-exact body included, credential headers
// excluded.
func TestWebhookAcceptsAndHandsOverTheRequest(t *testing.T) {
	svc, h := webhookHandler("hook-abc", nil)
	body := []byte(`{"action":"opened","n":1}`)
	rec := postHook(h, "/webhooks/geoah.example.com/gh-issues?a=b&a=c", body, map[string]string{
		"Content-Type":   "application/json; charset=utf-8",
		"X-GitHub-Event": "issues",
		"Authorization":  "Bearer 0123456789abcdefXYZ",
		"Cookie":         "session=nope",
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out["fire"] != "hook-abc" {
		t.Fatalf("body = %s", rec.Body.String())
	}
	c := svc.last(t)
	if c.authority != "geoah.example.com" || c.trigger != "gh-issues" || c.key != "0123456789abcdefXYZ" {
		t.Fatalf("addressed (%s, %s, %s)", c.authority, c.trigger, c.key)
	}
	if c.req.Method != http.MethodPost || c.req.ContentType != "application/json" {
		t.Fatalf("method/type = %s %s", c.req.Method, c.req.ContentType)
	}
	if !bytes.Equal(c.req.Body, body) || c.req.Parts != nil {
		t.Fatalf("body = %q, parts = %v", c.req.Body, c.req.Parts)
	}
	if c.req.Headers["x-github-event"] != "issues" || c.req.Headers["content-type"] != "application/json; charset=utf-8" {
		t.Fatalf("headers = %v", c.req.Headers)
	}
	for _, denied := range []string{"authorization", "cookie"} {
		if _, has := c.req.Headers[denied]; has {
			t.Fatalf("%s reached the service", denied)
		}
	}
	if got := c.req.Query["a"]; len(got) != 2 || got[0] != "b" || got[1] != "c" {
		t.Fatalf("query = %v", c.req.Query)
	}
}

// The key rides wherever the sender can put it: the trailing path segment
// wins, then `?key=`, then a bearer header; the query key never reaches the
// callable's view of the query.
func TestWebhookKeyFromPathQueryOrHeader(t *testing.T) {
	svc, h := webhookHandler("f", nil)
	postHook(h, "/webhooks/geoah.example.com/t/pathkey-0123456789?key=querykey", nil, map[string]string{"Authorization": "Bearer headerkey"})
	if c := svc.last(t); c.key != "pathkey-0123456789" || c.trigger != "t" {
		t.Fatalf("path key lost: %+v", c)
	}
	postHook(h, "/webhooks/geoah.example.com/t?key=querykey-0123456789&x=1", nil, map[string]string{"Authorization": "Bearer headerkey"})
	c := svc.last(t)
	if c.key != "querykey-0123456789" {
		t.Fatalf("query key lost: %+v", c)
	}
	if _, has := c.req.Query["key"]; has || c.req.Query["x"][0] != "1" {
		t.Fatalf("query = %v", c.req.Query)
	}
	postHook(h, "/webhooks/geoah.example.com/t", nil, map[string]string{"Authorization": "bearer headerkey-0123456789"})
	if c := svc.last(t); c.key != "headerkey-0123456789" {
		t.Fatalf("header key lost: %+v", c)
	}
	postHook(h, "/webhooks/geoah.example.com/t", nil, nil)
	if c := svc.last(t); c.key != "" || c.req.Body != nil {
		t.Fatalf("an unkeyed empty POST = %+v", c)
	}
}

// Every service refusal is one 404: no such trigger, wrong key and disabled
// must not be told apart.
func TestWebhookRefusals(t *testing.T) {
	_, h := webhookHandler("", substrate.ErrNotFound)
	rec := postHook(h, "/webhooks/geoah.example.com/nope", []byte("{}"), nil)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "not_found") {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
}

// A request the trigger's declared signature refuses is a 401 whose body says
// no more than that: the reason the service wrapped (a stale timestamp here)
// never reaches the unauthenticated sender.
func TestWebhookSignatureRefusalIsATerse401(t *testing.T) {
	refusal := fmt.Errorf("%w: webhook signature refused (timestamp outside maxSkew)", substrate.ErrAuth)
	_, h := webhookHandler("", refusal)
	rec := postHook(h, "/webhooks/geoah.example.com/signed", []byte("{}"), nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var out substrate.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("body = %s: %v", rec.Body.String(), err)
	}
	if out.Error.Code != "auth" || out.Error.Message != "webhook signature refused" {
		t.Fatalf("error = %+v, want auth and the fixed message", out.Error)
	}
}

// A trigger with no key and a declared signature is not known to exist
// until the service verifies the signature, so every refusal the door makes
// while reading the request answers exactly as a missing trigger does: a
// multipart body (signed or broken), a body past the bound and headers past
// the cap. A trigger whose sender proved itself, or that is open to all,
// keeps the transport's own answer.
func TestWebhookKeylessSignedTriggerHidesTransportRefusals(t *testing.T) {
	svc, h := webhookHandler("f", nil)
	svc.refused = map[string]bool{"missing": true}
	svc.signatureOnly = map[string]bool{"signed": true}
	var form bytes.Buffer
	mw := multipart.NewWriter(&form)
	_ = mw.WriteField("n", "1")
	_ = mw.Close()
	for name, c := range map[string]struct {
		body    []byte
		headers map[string]string
		open    int
	}{
		"multipart with no boundary": {[]byte("x"), map[string]string{"Content-Type": "multipart/form-data"}, http.StatusBadRequest},
		"a whole multipart body":     {form.Bytes(), map[string]string{"Content-Type": mw.FormDataContentType()}, http.StatusAccepted},
		"a body past the bound":      {bytes.Repeat([]byte("x"), substrate.WebhookInlineDefault+1), map[string]string{"Content-Type": "text/plain"}, http.StatusRequestEntityTooLarge},
		"headers past the cap":       {nil, map[string]string{"X-Big": strings.Repeat("h", maxWebhookHeaderBytes)}, http.StatusRequestHeaderFieldsTooLarge},
	} {
		missing := postHook(h, "/webhooks/geoah.example.com/missing", c.body, c.headers)
		signed := postHook(h, "/webhooks/geoah.example.com/signed", c.body, c.headers)
		if missing.Code != http.StatusNotFound {
			t.Fatalf("%s: a missing trigger answered %d", name, missing.Code)
		}
		if signed.Code != missing.Code || signed.Body.String() != missing.Body.String() || signed.Header().Get("Connection") != "close" {
			t.Errorf("%s: the keyless signed trigger answered %d %q (Connection %q), a missing one %d %q",
				name, signed.Code, signed.Body.String(), signed.Header().Get("Connection"), missing.Code, missing.Body.String())
		}
		if open := postHook(h, "/webhooks/geoah.example.com/open", c.body, c.headers); open.Code != c.open {
			t.Errorf("%s: an open trigger answered %d, want %d", name, open.Code, c.open)
		}
	}
	for _, call := range svc.calls {
		if call.trigger == "signed" {
			t.Fatalf("a refused request to the keyless signed trigger reached the service: %+v", call.req)
		}
	}
}

// While every delivery slot is held, a keyless signed trigger reaches the
// slot and is told the door is busy, so a missing trigger answers the same
// 503 then, and so does a multipart request to the signed one, which the
// door refuses before the slot. The answers match in status, body, headers
// and connection handling.
func TestWebhookBusyDoorHidesKeylessSignedTriggers(t *testing.T) {
	svc, h := webhookHandler("f", nil)
	svc.refused = map[string]bool{"missing": true}
	svc.signatureOnly = map[string]bool{"signed": true}
	for range cap(webhookSem) {
		webhookSem <- struct{}{}
	}
	defer func() {
		for range cap(webhookSem) {
			<-webhookSem
		}
	}()
	for name, headers := range map[string]map[string]string{
		"inline":    {"Content-Type": "application/json"},
		"multipart": {"Content-Type": "multipart/form-data; boundary=xyz"},
	} {
		missing := postHook(h, "/webhooks/geoah.example.com/missing", []byte("{}"), headers)
		signed := postHook(h, "/webhooks/geoah.example.com/signed", []byte("{}"), headers)
		if signed.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: the keyless signed trigger answered %d with every slot held, want 503", name, signed.Code)
		}
		if diff := webhookAnswerDiff(missing, signed); diff != "" {
			t.Errorf("%s, every slot held: %s", name, diff)
		}
	}
	if len(svc.calls) != 0 {
		t.Fatalf("a request to a busy door reached the service: %d calls", len(svc.calls))
	}
}

// A keyless signed trigger whose signature check refuses an unsigned request
// answers it as a missing trigger does, down to closing the connection: an
// empty POST that reaches ReceiveWebhook cannot be told from one that never
// got past CheckWebhook.
func TestWebhookKeylessSignatureRefusalMatchesAMissingTrigger(t *testing.T) {
	svc, h := webhookHandler("", substrate.ErrNotFound)
	svc.refused = map[string]bool{"missing": true}
	svc.signatureOnly = map[string]bool{"signed": true}
	missing := postHook(h, "/webhooks/geoah.example.com/missing", nil, nil)
	signed := postHook(h, "/webhooks/geoah.example.com/signed", nil, nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("a missing trigger answered %d", missing.Code)
	}
	if diff := webhookAnswerDiff(missing, signed); diff != "" {
		t.Error(diff)
	}
	if svc.last(t).trigger != "signed" {
		t.Fatal("the signed trigger's request never reached ReceiveWebhook")
	}
}

// webhookAnswerDiff names how two of the door's answers differ, or is empty.
// X-Request-Id is fresh on every answer, so it is left out.
func webhookAnswerDiff(a, b *httptest.ResponseRecorder) string {
	if a.Code != b.Code || a.Body.String() != b.Body.String() {
		return fmt.Sprintf("answers %d %q and %d %q", a.Code, a.Body.String(), b.Code, b.Body.String())
	}
	ha, hb := a.Header().Clone(), b.Header().Clone()
	ha.Del("X-Request-Id")
	hb.Del("X-Request-Id")
	if !reflect.DeepEqual(ha, hb) {
		return fmt.Sprintf("headers %v and %v", ha, hb)
	}
	return ""
}

// A body the service holds too large for what the trigger calls (an agent
// reads at most 1 MiB) is a 413, as the door's own caps are. The trigger
// declares 8 MiB, so the door reads the 2 MiB body and the 413 is the
// service's ErrTooLarge, carrying its message, not the door's own bound.
func TestWebhookTooLargeForTheCallableIs413(t *testing.T) {
	const refusal = "an agent callable reads at most 1 MiB"
	svc, h := webhookHandler("", fmt.Errorf("%w: %s", substrate.ErrTooLarge, refusal))
	svc.maxBody = map[string]int64{"to-agent": 8 << 20}
	rec := postHook(h, "/webhooks/geoah.example.com/to-agent", bytes.Repeat([]byte("x"), 2<<20), map[string]string{"Content-Type": "text/plain"})
	if rec.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rec.Body.String(), "bad_request") || !strings.Contains(rec.Body.String(), refusal) {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if got := len(svc.last(t).req.Body); got != 2<<20 {
		t.Fatalf("the service got %d bytes, want the whole 2 MiB body", got)
	}
}

// A repository whose webhook spool is full answers unavailable, which the
// door says as 503 with Retry-After, so the sender redelivers later.
func TestWebhookSpoolFullIsUnavailableWithRetryAfter(t *testing.T) {
	_, h := webhookHandler("", fmt.Errorf("%w: the webhook spool of this repository is full; retry later", substrate.ErrUnavailable))
	rec := postHook(h, "/webhooks/geoah.example.com/t", []byte("{}"), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("status = %d, Retry-After = %q: %s", rec.Code, rec.Header().Get("Retry-After"), rec.Body.String())
	}
}

// The bounds: a non-multipart body past the trigger's inline bound (1 MiB
// when it declares none) is 413, headers past 16 KiB are 431, and neither
// reaches the service.
func TestWebhookBounds(t *testing.T) {
	svc, h := webhookHandler("f", nil)
	rec := postHook(h, "/webhooks/geoah.example.com/t", bytes.Repeat([]byte("x"), substrate.WebhookInlineDefault+1), map[string]string{"Content-Type": "text/plain"})
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize body: status = %d", rec.Code)
	}
	rec = postHook(h, "/webhooks/geoah.example.com/t", nil, map[string]string{"X-Big": strings.Repeat("h", maxWebhookHeaderBytes)})
	if rec.Code != http.StatusRequestHeaderFieldsTooLarge {
		t.Fatalf("oversize headers: status = %d", rec.Code)
	}
	if len(svc.calls) != 0 {
		t.Fatalf("a refused request reached the service: %d calls", len(svc.calls))
	}
	if rec := postHook(h, "/webhooks/geoah.example.com/t", bytes.Repeat([]byte("x"), substrate.WebhookInlineDefault), map[string]string{"Content-Type": "text/plain"}); rec.Code != http.StatusAccepted {
		t.Fatalf("a body at the cap: status = %d", rec.Code)
	}
}

// The inline bound is the trigger's: 1 MiB for one that declares no
// maxBody, and up to 8 MiB for one that does (a Health Auto Export delivery
// of a day at minute grouping passes 1 MiB, and the app never re-sends a
// part the door refused). Pinned in bytes rather than through the
// constants, so moving either fails here.
func TestWebhookInlineBoundIsTheTriggers(t *testing.T) {
	svc, h := webhookHandler("f", nil)
	svc.maxBody = map[string]int64{"large": 8 << 20}
	asJSON := map[string]string{"Content-Type": "application/json"}
	if rec := postHook(h, "/webhooks/geoah.example.com/t", bytes.Repeat([]byte("x"), 1<<20), asJSON); rec.Code != http.StatusAccepted {
		t.Fatalf("1 MiB to a trigger that declares no maxBody: status = %d: %s", rec.Code, rec.Body.String())
	}
	over := postHook(h, "/webhooks/geoah.example.com/t", bytes.Repeat([]byte("x"), 1<<20+1), asJSON)
	if over.Code != http.StatusRequestEntityTooLarge || !strings.Contains(over.Body.String(), "exceeds 1 MiB") {
		t.Fatalf("a byte past 1 MiB to a trigger that declares no maxBody: status = %d: %s", over.Code, over.Body.String())
	}
	under := postHook(h, "/webhooks/geoah.example.com/large", bytes.Repeat([]byte("x"), 8<<20-1), asJSON)
	if under.Code != http.StatusAccepted {
		t.Fatalf("a body one byte under 8 MiB to a trigger that declares 8 MiB: status = %d: %s", under.Code, under.Body.String())
	}
	if got := len(svc.last(t).req.Body); got != 8<<20-1 {
		t.Fatalf("the service got %d bytes, want %d", got, 8<<20-1)
	}
	over = postHook(h, "/webhooks/geoah.example.com/large", bytes.Repeat([]byte("x"), 8<<20+1), asJSON)
	if over.Code != http.StatusRequestEntityTooLarge || !strings.Contains(over.Body.String(), "exceeds 8 MiB") {
		t.Fatalf("a byte past 8 MiB: status = %d: %s", over.Code, over.Body.String())
	}
	if len(svc.calls) != 2 {
		t.Fatalf("a refused body reached the service: %d calls, want 2", len(svc.calls))
	}
}

// The inline deadline scales with the trigger's bound at the default's rate,
// 30 s per started MiB, so a trigger that declares no maxBody keeps 30 s.
func TestWebhookInlineDeadlineScalesWithTheBound(t *testing.T) {
	for bound, want := range map[int64]time.Duration{
		1:       30 * time.Second,
		1 << 20: 30 * time.Second,
		2 << 20: time.Minute,
		8 << 20: 4 * time.Minute,
	} {
		if got := inlineDeadline(bound); got != want {
			t.Errorf("inlineDeadline(%d) = %s, want %s", bound, got, want)
		}
	}
}

// multipart/form-data is decomposed here: text fields inline, a part with a
// filename or a non-text media type as bytes for the blob store, in order.
func TestWebhookMultipartParts(t *testing.T) {
	svc, h := webhookHandler("f", nil)
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("transcription", "buy milk")
	_ = mw.WriteField("recordedAt", "1756730000000")
	audio, _ := mw.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="audio"`},
		"Content-Type":        {"audio/mp4"},
	})
	_, _ = audio.Write([]byte{0, 1, 2, 3})
	doc, _ := mw.CreateFormFile("doc", "notes.txt")
	_, _ = doc.Write([]byte("a file that happens to be text"))
	typed, _ := mw.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="note"`},
		"Content-Type":        {"text/plain; charset=utf-8"},
	})
	_, _ = typed.Write([]byte("inline, despite the media type"))
	_ = mw.Close()

	rec := postHook(h, "/webhooks/geoah.example.com/pebble-webhook", buf.Bytes(), map[string]string{
		"Content-Type": mw.FormDataContentType(), "X-Pebble-Mode": "note", "X-Index-Trigger": "single-click-hold",
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	c := svc.last(t)
	if c.req.ContentType != "multipart/form-data" || c.req.Body != nil {
		t.Fatalf("type = %q, body = %q", c.req.ContentType, c.req.Body)
	}
	want := []substrate.WebhookPart{
		{Name: "transcription", Value: "buy milk"},
		{Name: "recordedAt", Value: "1756730000000"},
		{Name: "audio", MediaType: "audio/mp4", Data: []byte{0, 1, 2, 3}},
		{Name: "doc", Filename: "notes.txt", MediaType: "application/octet-stream", Data: []byte("a file that happens to be text")},
		{Name: "note", MediaType: "text/plain", Value: "inline, despite the media type"},
	}
	if len(c.req.Parts) != len(want) {
		t.Fatalf("parts = %+v", c.req.Parts)
	}
	for i, p := range want {
		got := c.req.Parts[i]
		if got.Name != p.Name || got.Filename != p.Filename || got.MediaType != p.MediaType || got.Value != p.Value || !bytes.Equal(got.Data, p.Data) {
			t.Errorf("part %d = %+v, want %+v", i, got, p)
		}
	}
	if c.req.Headers["x-pebble-mode"] != "note" || c.req.Headers["x-index-trigger"] != "single-click-hold" {
		t.Fatalf("headers = %v", c.req.Headers)
	}
}

// A request for no such webhook is refused before the door takes a delivery
// slot or reads a byte of its body: eight of them whose bodies never finish
// leave every slot free, and a real delivery behind them is accepted.
func TestWebhookUnknownTriggersHoldNoSlot(t *testing.T) {
	svc, h := webhookHandler("hook-real", nil)
	svc.refused = map[string]bool{"nothing": true}
	srv := httptest.NewServer(h)
	defer srv.Close()

	statuses := make(chan int, maxConcurrentWebhooks)
	for range maxConcurrentWebhooks {
		go func() {
			statuses <- stallBody(t, srv, http.MethodPost, "/webhooks/nobody.example.com/nothing", nil, 3*time.Second)
		}()
	}
	// Wait until the eight are answered or, without the check up front, until
	// they hold every slot.
	answered := 0
	for deadline := time.Now().Add(3 * time.Second); answered < maxConcurrentWebhooks && time.Now().Before(deadline); {
		select {
		case got := <-statuses:
			if got != http.StatusNotFound {
				t.Fatalf("a request for no such webhook got %d, want 404 before its body", got)
			}
			answered++
		case <-time.After(10 * time.Millisecond):
			if len(webhookSem) == maxConcurrentWebhooks {
				t.Fatalf("requests for no such webhook hold all %d delivery slots", maxConcurrentWebhooks)
			}
		}
	}
	if answered != maxConcurrentWebhooks {
		t.Fatalf("%d of %d requests for no such webhook were answered", answered, maxConcurrentWebhooks)
	}
	if rec := postHook(h, "/webhooks/geoah.example.com/t", []byte("{}"), nil); rec.Code != http.StatusAccepted {
		t.Fatalf("a real delivery behind them: status = %d: %s", rec.Code, rec.Body.String())
	}
	svc.mu.Lock()
	defer svc.mu.Unlock()
	for _, c := range svc.calls {
		if c.trigger == "nothing" {
			t.Fatal("a request the check refused reached ReceiveWebhook")
		}
	}
}

// A delivery whose sender stalls mid-body is answered at the deadline and
// gives its slot back: inline, multipart, and a multipart body whose closing
// boundary arrived but whose Content-Length promised more.
func TestWebhookStalledBodyGivesItsSlotBack(t *testing.T) {
	shortBodyDeadlines(t)
	svc, h := webhookHandler("f", nil)
	srv := httptest.NewServer(h)
	defer srv.Close()
	for name, c := range map[string]struct{ contentType, sent string }{
		"inline":           {"application/json", "{"},
		"multipart":        {"multipart/form-data; boundary=xyz", "-"},
		"closed multipart": {"multipart/form-data; boundary=xyz", "--xyz--\r\n"},
	} {
		t.Run(name, func(t *testing.T) {
			got := stallAfter(t, srv, http.MethodPost, "/webhooks/geoah.example.com/t", map[string]string{"Content-Type": c.contentType}, c.sent, 3*time.Second)
			if got != http.StatusBadRequest {
				t.Fatalf("a stalled body got status %d within 3s, want 400 (0 is no answer)", got)
			}
			if n := len(webhookSem); n != 0 {
				t.Fatalf("%d delivery slots still held after the answer", n)
			}
		})
	}
	if len(svc.calls) != 0 {
		t.Fatalf("a stalled delivery reached the service: %d calls", len(svc.calls))
	}
}

// slowHookSvc holds each delivery past the body deadline, reporting whether
// the request's context ended under it.
type slowHookSvc struct {
	*webhookSvc
	hold     time.Duration
	canceled chan bool
}

func (s *slowHookSvc) ReceiveWebhook(ctx context.Context, _, _, _ string, _ substrate.WebhookRequest) (string, error) {
	select {
	case <-ctx.Done():
		s.canceled <- true
		return "", ctx.Err()
	case <-time.After(s.hold):
		s.canceled <- false
	}
	return "hook-slow", nil
}

// An empty delivery (a ping, a bare POST) reaches the service with the body
// deadline cleared: net/http reads a bodiless connection in the background
// from the start, and a deadline left on that read would end the request's
// context while the service records the delivery.
func TestWebhookEmptyDeliveryOutlivesTheBodyDeadline(t *testing.T) {
	d := shortBodyDeadlines(t)
	svc := &slowHookSvc{webhookSvc: &webhookSvc{fakeService: newFakeService()}, hold: 4 * d, canceled: make(chan bool, 1)}
	srv := httptest.NewServer(New(Config{Service: svc}))
	defer srv.Close()
	resp, err := srv.Client().Post(srv.URL+"/webhooks/geoah.example.com/t", "application/json", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if <-svc.canceled {
		t.Fatalf("the delivery's context ended within %s of an empty body", 4*d)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}
}
