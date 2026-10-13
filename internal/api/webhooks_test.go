package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

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

// A body the service holds too large for what the trigger calls (an agent
// reads at most 1 MiB) is a 413, as the door's own caps are.
func TestWebhookTooLargeForTheCallableIs413(t *testing.T) {
	_, h := webhookHandler("", fmt.Errorf("%w: an agent callable reads at most 1 MiB", substrate.ErrTooLarge))
	rec := postHook(h, "/webhooks/geoah.example.com/to-agent", bytes.Repeat([]byte("x"), 2<<20), map[string]string{"Content-Type": "text/plain"})
	if rec.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rec.Body.String(), "bad_request") {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
}

// The bounds: a non-multipart body past 8 MiB is 413, headers past 16 KiB are
// 431, and neither reaches the service.
func TestWebhookBounds(t *testing.T) {
	svc, h := webhookHandler("f", nil)
	rec := postHook(h, "/webhooks/geoah.example.com/t", bytes.Repeat([]byte("x"), maxWebhookInline+1), map[string]string{"Content-Type": "text/plain"})
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
	if rec := postHook(h, "/webhooks/geoah.example.com/t", bytes.Repeat([]byte("x"), maxWebhookInline), map[string]string{"Content-Type": "text/plain"}); rec.Code != http.StatusAccepted {
		t.Fatalf("a body at the cap: status = %d", rec.Code)
	}
}

// The inline cap is 8 MiB, the function frame a body rides in: a Health
// Auto Export delivery of a day at minute grouping passes 1 MiB, and the app
// never re-sends a part the door refused. Pinned in bytes rather than through
// the constant, so lowering the constant fails here.
func TestWebhookInlineCapIsEightMiB(t *testing.T) {
	svc, h := webhookHandler("f", nil)
	under := postHook(h, "/webhooks/geoah.example.com/t", bytes.Repeat([]byte("x"), 8<<20-1), map[string]string{"Content-Type": "application/json"})
	if under.Code != http.StatusAccepted {
		t.Fatalf("a body one byte under 8 MiB: status = %d: %s", under.Code, under.Body.String())
	}
	if got := len(svc.last(t).req.Body); got != 8<<20-1 {
		t.Fatalf("the service got %d bytes, want %d", got, 8<<20-1)
	}
	over := postHook(h, "/webhooks/geoah.example.com/t", bytes.Repeat([]byte("x"), 8<<20+1), map[string]string{"Content-Type": "application/json"})
	if over.Code != http.StatusRequestEntityTooLarge || !strings.Contains(over.Body.String(), "exceeds 8 MiB") {
		t.Fatalf("a body one byte past 8 MiB: status = %d: %s", over.Code, over.Body.String())
	}
	if len(svc.calls) != 1 {
		t.Fatalf("the refused body reached the service: %d calls", len(svc.calls))
	}
}

// A sender that trickles its body holds its slot for webhookReadTimeout at
// most. Eight such connections take every slot and turn the next delivery
// away with 503; once the deadline passes each is answered 408 and the door
// admits again, although the eight are still open.
func TestWebhookTrickledBodyGivesUpItsSlot(t *testing.T) {
	defer func(d time.Duration) { webhookReadTimeout = d }(webhookReadTimeout)
	webhookReadTimeout = 3 * time.Second
	_, h := webhookHandler("f", nil)
	srv := httptest.NewServer(h)
	defer srv.Close()
	post := func() int {
		t.Helper()
		resp, err := srv.Client().Post(srv.URL+"/webhooks/geoah.example.com/t", "application/json", strings.NewReader("{}"))
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	trickles := make([]net.Conn, maxConcurrentWebhooks)
	for i := range trickles {
		c, err := net.Dial("tcp", srv.Listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = c.Close() }()
		if _, err := fmt.Fprint(c, "POST /webhooks/geoah.example.com/t HTTP/1.1\r\nHost: example.com\r\n"+
			"Content-Type: application/json\r\nContent-Length: 1000\r\n\r\n{"); err != nil {
			t.Fatal(err)
		}
		trickles[i] = c
	}
	for deadline := time.Now().Add(webhookReadTimeout / 2); post() != http.StatusServiceUnavailable; {
		if time.Now().After(deadline) {
			t.Fatal("the trickling connections never held every slot")
		}
		time.Sleep(10 * time.Millisecond)
	}
	for deadline := time.Now().Add(4 * webhookReadTimeout); ; time.Sleep(50 * time.Millisecond) {
		if code := post(); code == http.StatusAccepted {
			break
		} else if time.Now().After(deadline) {
			t.Fatalf("the door still answers %d while the trickles stay open", code)
		}
	}
	for i, c := range trickles {
		_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
		line, err := bufio.NewReader(c).ReadString('\n')
		if err != nil || !strings.HasPrefix(line, "HTTP/1.1 408") {
			t.Fatalf("trickle %d was answered %q (%v), want 408", i, line, err)
		}
	}
}

// A multipart request is held to four times the inline cap, so its body
// gets four times the inline read deadline: one sending rate carries either
// bound in time. A body whose second half arrives two deadlines after its
// headers is answered 408 when it is JSON and 202 when it is multipart.
func TestWebhookMultipartBodyGetsItsBoundsShareOfTime(t *testing.T) {
	defer func(d time.Duration) { webhookReadTimeout = d }(webhookReadTimeout)
	webhookReadTimeout = time.Second
	_, h := webhookHandler("hook-slow", nil)
	srv := httptest.NewServer(h)
	defer srv.Close()
	var form bytes.Buffer
	mw := multipart.NewWriter(&form)
	_ = mw.WriteField("transcription", strings.Repeat("buy milk ", 64))
	_ = mw.Close()
	slow := func(contentType string, body []byte, finish bool) string {
		t.Helper()
		c, err := net.Dial("tcp", srv.Listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = c.Close() }()
		half := len(body) / 2
		if _, err := fmt.Fprintf(c, "POST /webhooks/geoah.example.com/t HTTP/1.1\r\nHost: example.com\r\n"+
			"Content-Type: %s\r\nContent-Length: %d\r\n\r\n%s", contentType, len(body), body[:half]); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * webhookReadTimeout)
		if finish {
			if _, err := c.Write(body[half:]); err != nil {
				t.Fatal(err)
			}
		}
		_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
		line, err := bufio.NewReader(c).ReadString('\n')
		if err != nil {
			t.Fatalf("%s: read the answer: %v", contentType, err)
		}
		return line
	}
	if line := slow("application/json", []byte(`{"transcription":"buy milk"}`), false); !strings.HasPrefix(line, "HTTP/1.1 408") {
		t.Fatalf("a JSON body past its deadline was answered %q, want 408", line)
	}
	if line := slow(mw.FormDataContentType(), form.Bytes(), true); !strings.HasPrefix(line, "HTTP/1.1 202") {
		t.Fatalf("a multipart body inside four deadlines was answered %q, want 202", line)
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
