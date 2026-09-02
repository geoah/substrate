package api

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// webhookSvc wraps the fake service with the substrate.WebhookReceiver half
// the door asserts at runtime, recording what it was handed and answering a
// canned outcome.
type webhookSvc struct {
	*fakeService
	mu    sync.Mutex
	calls []webhookCall
	fid   string
	err   error
}

type webhookCall struct {
	owner, trigger, key string
	req                 substrate.WebhookRequest
}

func (s *webhookSvc) ReceiveWebhook(_ context.Context, owner, trigger, key string, req substrate.WebhookRequest) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, webhookCall{owner, trigger, key, req})
	return s.fid, s.err
}

var _ substrate.WebhookReceiver = (*webhookSvc)(nil)

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
	rec := postHook(h, "/webhooks/geoah/gh-issues?a=b&a=c", body, map[string]string{
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
	if c.owner != "geoah" || c.trigger != "gh-issues" || c.key != "0123456789abcdefXYZ" {
		t.Fatalf("addressed (%s, %s, %s)", c.owner, c.trigger, c.key)
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
	postHook(h, "/webhooks/geoah/t/pathkey-0123456789?key=querykey", nil, map[string]string{"Authorization": "Bearer headerkey"})
	if c := svc.last(t); c.key != "pathkey-0123456789" || c.trigger != "t" {
		t.Fatalf("path key lost: %+v", c)
	}
	postHook(h, "/webhooks/geoah/t?key=querykey-0123456789&x=1", nil, map[string]string{"Authorization": "Bearer headerkey"})
	c := svc.last(t)
	if c.key != "querykey-0123456789" {
		t.Fatalf("query key lost: %+v", c)
	}
	if _, has := c.req.Query["key"]; has || c.req.Query["x"][0] != "1" {
		t.Fatalf("query = %v", c.req.Query)
	}
	postHook(h, "/webhooks/geoah/t", nil, map[string]string{"Authorization": "bearer headerkey-0123456789"})
	if c := svc.last(t); c.key != "headerkey-0123456789" {
		t.Fatalf("header key lost: %+v", c)
	}
	postHook(h, "/webhooks/geoah/t", nil, nil)
	if c := svc.last(t); c.key != "" || c.req.Body != nil {
		t.Fatalf("an unkeyed empty POST = %+v", c)
	}
}

// Every service refusal is one 404, and a service without the seam says so.
func TestWebhookRefusals(t *testing.T) {
	_, h := webhookHandler("", substrate.ErrNotFound)
	rec := postHook(h, "/webhooks/geoah/nope", []byte("{}"), nil)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "not_found") {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	plain := New(Config{Service: newFakeService()})
	if rec := postHook(plain, "/webhooks/geoah/nope", []byte("{}"), nil); rec.Code != http.StatusNotImplemented {
		t.Fatalf("no seam: status = %d", rec.Code)
	}
}

// The bounds: a non-multipart body past 1 MiB is 413, headers past 16 KiB are
// 431, and neither reaches the service.
func TestWebhookBounds(t *testing.T) {
	svc, h := webhookHandler("f", nil)
	rec := postHook(h, "/webhooks/geoah/t", bytes.Repeat([]byte("x"), maxWebhookInline+1), map[string]string{"Content-Type": "text/plain"})
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize body: status = %d", rec.Code)
	}
	rec = postHook(h, "/webhooks/geoah/t", nil, map[string]string{"X-Big": strings.Repeat("h", maxWebhookHeaderBytes)})
	if rec.Code != http.StatusRequestHeaderFieldsTooLarge {
		t.Fatalf("oversize headers: status = %d", rec.Code)
	}
	if len(svc.calls) != 0 {
		t.Fatalf("a refused request reached the service: %d calls", len(svc.calls))
	}
	if rec := postHook(h, "/webhooks/geoah/t", bytes.Repeat([]byte("x"), maxWebhookInline), map[string]string{"Content-Type": "text/plain"}); rec.Code != http.StatusAccepted {
		t.Fatalf("a body at the cap: status = %d", rec.Code)
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

	rec := postHook(h, "/webhooks/geoah/pebble-webhook", buf.Bytes(), map[string]string{
		"Content-Type": mw.FormDataContentType(), "X-Pebble-Mode": "note",
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
	if c.req.Headers["x-pebble-mode"] != "note" {
		t.Fatalf("headers = %v", c.req.Headers)
	}
}
