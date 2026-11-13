package api

// An agent stream a full token opened ends with its token: the revoke is a
// changelog write, the stream reads its token again and ends with an error
// event whose code is `auth` (decision 0272).

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// holdingChat runs a chat turn until its stream ends: a turn that holds
// without a model, which the engine cannot be made to run.
type holdingChat struct {
	substrate.Dataset
	started chan struct{}
}

func (d holdingChat) ChatAgent(ctx context.Context, _ substrate.Actor, _ string, _ substrate.ChatInput, emit func(substrate.AgentEvent)) (*substrate.AgentResult, error) {
	emit(substrate.AgentEvent{Kind: substrate.AgentEventThread, Thread: "tA"})
	close(d.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestAFullTokensAgentStreamEndsWithItsToken(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	started := make(chan struct{})
	env.wrap = func(ds substrate.Dataset) substrate.Dataset { return holdingChat{Dataset: ds, started: started} }
	_, info, err := env.svc.Authenticate(context.Background(), env.token)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	other := env.mint(t, substrate.MintTokenInput{Label: "other"})

	req := httptest.NewRequest(http.MethodPost, helperChatPath, bytes.NewReader([]byte(`{"message":"hi"}`)))
	req.Header.Set("Authorization", "Bearer "+env.token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		env.h.ServeHTTP(rec, req)
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the chat turn never started")
	}

	wantStatus(t, env.do(t, http.MethodDelete, "/tokens/"+info.ID, other, nil), http.StatusOK)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the agent stream ran on after its token was revoked")
	}
	var last substrate.AgentEvent
	for _, line := range strings.Split(strings.TrimSpace(rec.Body.String()), "\n") {
		_ = json.Unmarshal([]byte(line), &last)
	}
	if last.Kind != substrate.AgentEventError || last.Code != codeAuth {
		t.Fatalf("the stream ended with %+v, want an error event with the code auth (body %s)", last, rec.Body.String())
	}
}

// startingChat holds a chat turn before its first event, as a turn that is
// still starting its model does.
type startingChat struct {
	substrate.Dataset
	started, release chan struct{}
}

func (d startingChat) ChatAgent(ctx context.Context, _ substrate.Actor, _ string, _ substrate.ChatInput, emit func(substrate.AgentEvent)) (*substrate.AgentResult, error) {
	close(d.started)
	select {
	case <-d.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	emit(substrate.AgentEvent{Kind: substrate.AgentEventThread, Thread: "tA"})
	<-ctx.Done()
	return nil, ctx.Err()
}

// A token revoked while its agent stream is still starting ends the request
// with an ordinary refusal, before any event: the stream does not wait for a
// first event to notice, and that event is never delivered.
func TestAnAgentStreamWhoseTokenDiesBeforeItsFirstEventIsRefused(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	env.wrap = func(ds substrate.Dataset) substrate.Dataset {
		return startingChat{Dataset: ds, started: started, release: release}
	}
	_, info, err := env.svc.Authenticate(context.Background(), env.token)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	other := env.mint(t, substrate.MintTokenInput{Label: "other"})
	req := httptest.NewRequest(http.MethodPost, helperChatPath, bytes.NewReader([]byte(`{"message":"hi"}`)))
	req.Header.Set("Authorization", "Bearer "+env.token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		env.h.ServeHTTP(rec, req)
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the chat turn never started")
	}
	wantStatus(t, env.do(t, http.MethodDelete, "/tokens/"+info.ID, other, nil), http.StatusOK)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the agent stream waited on after its token was revoked")
	}
	wantErrorCode(t, rec, http.StatusUnauthorized, codeAuth)
}

// revokingWriter revokes the request's token the first time a body write
// reaches it, and moves the clock past the lease's read window: a long
// transfer whose token dies while a client is still reading.
type revokingWriter struct {
	*httptest.ResponseRecorder
	revoke  func()
	revoked bool
}

func (w *revokingWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseRecorder.Write(p)
	if !w.revoked {
		w.revoked = true
		w.revoke()
	}
	return n, err
}

func (w *revokingWriter) Unwrap() http.ResponseWriter { return w.ResponseRecorder }

// serveAborting serves req and reports whether the handler cut the response
// (http.ErrAbortHandler), which a client reads as a body that ended early.
func serveAborting(env *dbEnv, w http.ResponseWriter, req *http.Request) (aborted bool) {
	defer func() {
		if p := recover(); p != nil {
			if err, ok := p.(error); !ok || !errors.Is(err, http.ErrAbortHandler) {
				panic(p)
			}
			aborted = true
		}
	}()
	env.h.ServeHTTP(w, req)
	return false
}

// revokeDuring is a revokingWriter for env's token, revoked by another token.
func revokeDuring(t *testing.T, env *dbEnv) *revokingWriter {
	t.Helper()
	_, info, err := env.svc.Authenticate(context.Background(), env.token)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	other := env.mint(t, substrate.MintTokenInput{Label: "other"})
	return &revokingWriter{ResponseRecorder: httptest.NewRecorder(), revoke: func() {
		wantStatus(t, env.do(t, http.MethodDelete, "/tokens/"+info.ID, other, nil), http.StatusOK)
		env.clock.advance(2 * leaseTransferEvery)
	}}
}

// An export is the whole repository, and its token dying mid-archive stops
// it (decision 0272): the archive is cut before its last entry, the point.
func TestAnExportStopsWhenItsTokenIsRevoked(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	w := revokeDuring(t, env)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/export", nil)
	req.Header.Set("Authorization", "Bearer "+env.token)
	if !serveAborting(env, w, req) {
		t.Fatalf("the export ran to its end after its token was revoked (%d bytes)", w.Body.Len())
	}
	if !w.revoked {
		t.Fatal("the export wrote no body before it was cut")
	}
	if bytes.Contains(w.Body.Bytes(), []byte("snapshot.json")) {
		t.Fatal("the cut archive carries its last entry")
	}
}

// A large blob's bytes stop when its token dies while a client is still
// reading them.
func TestABlobsBytesStopWhenItsTokenIsRevoked(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	data := bytes.Repeat([]byte("substrate "), 4*blobChunk/10)
	sum := sha256.Sum256(data)
	digest := "blob-sha256-" + hex.EncodeToString(sum[:])
	blobPut(t, env, "/api/v1/blobs/"+digest, string(data), map[string]string{"Content-Type": "application/octet-stream"})
	w := revokeDuring(t, env)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/blobs/"+digest, nil)
	req.Header.Set("Authorization", "Bearer "+env.token)
	if !serveAborting(env, w, req) {
		t.Fatalf("the blob's %d bytes all went out after its token was revoked", w.Body.Len())
	}
	if w.Body.Len() >= len(data) {
		t.Fatalf("%d of %d bytes went out, want the body cut", w.Body.Len(), len(data))
	}
}

// heldOnce holds the first call it is asked to hold until release closes,
// with held closed once that call is waiting: a store or a pin still working
// while the token dies.
type heldOnce struct {
	first         atomic.Bool
	held, release chan struct{}
}

func newHeldOnce() *heldOnce {
	h := &heldOnce{held: make(chan struct{}), release: make(chan struct{})}
	h.first.Store(true)
	return h
}

func (h *heldOnce) hold() {
	if h.first.CompareAndSwap(true, false) {
		close(h.held)
		<-h.release
	}
}

// serveRevokingWhileHeld serves req on env, waits for hold to hold its
// call, revokes env's token from another, releases the call and answers
// the response. The request ends within five seconds either way.
func serveRevokingWhileHeld(t *testing.T, env *dbEnv, hold *heldOnce, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	_, info, err := env.svc.Authenticate(context.Background(), env.token)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	other := env.mint(t, substrate.MintTokenInput{Label: "other"})
	ctx, cancel := context.WithTimeout(req.Context(), 5*time.Second)
	defer cancel()
	req = req.WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+env.token)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			if p := recover(); p != nil {
				if err, ok := p.(error); !ok || !errors.Is(err, http.ErrAbortHandler) {
					panic(p)
				}
			}
		}()
		env.h.ServeHTTP(rec, req)
	}()
	select {
	case <-hold.held:
	case <-done:
		t.Fatalf("the request ended before the call it holds: %d %s", rec.Code, rec.Body.String())
	case <-time.After(5 * time.Second):
		t.Fatal("the request never reached the call it holds")
	}
	wantStatus(t, env.do(t, http.MethodDelete, "/tokens/"+info.ID, other, nil), http.StatusOK)
	close(hold.release)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the request never ended")
	}
	return rec
}

// readingBlobs holds a blob's read, as a slow store does.
type readingBlobs struct {
	substrate.Dataset
	hold *heldOnce
}

func (d readingBlobs) GetBlob(ctx context.Context, digest string) (*substrate.BlobInfo, []byte, error) {
	d.hold.hold()
	return d.Dataset.GetBlob(ctx, digest)
}

// A blob's token is read after the store read its bytes and before the
// headers: a token revoked meanwhile gets an ordinary refusal and none of the
// bytes, however small the blob.
func TestABlobReadWhoseTokenDiesWhileTheStoreReadsSendsNoBytes(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	data := "a blob smaller than one chunk"
	sum := sha256.Sum256([]byte(data))
	digest := "blob-sha256-" + hex.EncodeToString(sum[:])
	blobPut(t, env, "/api/v1/blobs/"+digest, data, map[string]string{"Content-Type": "application/octet-stream"})
	hold := newHeldOnce()
	env.wrap = func(ds substrate.Dataset) substrate.Dataset { return readingBlobs{Dataset: ds, hold: hold} }
	rec := serveRevokingWhileHeld(t, env, hold, httptest.NewRequest(http.MethodGet, "/api/v1/blobs/"+digest, nil))
	wantErrorCode(t, rec, http.StatusUnauthorized, codeAuth)
	if strings.Contains(rec.Body.String(), data) {
		t.Fatal("the refused read carries the blob's bytes")
	}
}

// resolvingHead holds a watch's read of the head, as resolving its cursor
// does.
type resolvingHead struct {
	substrate.Dataset
	hold *heldOnce
}

func (d resolvingHead) Head(ctx context.Context) (substrate.ChangelogHead, error) {
	d.hold.hold()
	return d.Dataset.Head(ctx)
}

// A watch reads its token before its first byte: one revoked while the
// cursor resolves is an ordinary refusal, with no 200 and no bookmark.
func TestAWatchWhoseTokenDiesWhileItsCursorResolvesIsRefused(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	hold := newHeldOnce()
	env.wrap = func(ds substrate.Dataset) substrate.Dataset { return resolvingHead{Dataset: ds, hold: hold} }
	rec := serveRevokingWhileHeld(t, env, hold, httptest.NewRequest(http.MethodGet, "/api/v1/changes?watch=1", nil))
	wantErrorCode(t, rec, http.StatusUnauthorized, codeAuth)
	if strings.Contains(rec.Body.String(), "bookmark") {
		t.Fatal("the refused watch carries a bookmark")
	}
}

// pinningExport holds an export once its point is pinned, as a large
// repository's pin takes its time.
type pinningExport struct {
	substrate.Dataset
	hold *heldOnce
}

func (d pinningExport) Export(ctx context.Context) (substrate.Export, error) {
	e, err := d.Dataset.Export(ctx)
	d.hold.hold()
	return e, err
}

// An export reads its token before its headers: one revoked while the point
// was pinned is an ordinary refusal, and the pin is let go, so the next
// export runs.
func TestAnExportWhoseTokenDiesWhileItsPointIsPinnedIsRefused(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	hold := newHeldOnce()
	env.wrap = func(ds substrate.Dataset) substrate.Dataset { return pinningExport{Dataset: ds, hold: hold} }
	next := env.mint(t, substrate.MintTokenInput{Label: "next"})
	rec := serveRevokingWhileHeld(t, env, hold, httptest.NewRequest(http.MethodGet, "/api/v1/export", nil))
	wantErrorCode(t, rec, http.StatusUnauthorized, codeAuth)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/export", nil)
	req.Header.Set("Authorization", "Bearer "+next)
	again := httptest.NewRecorder()
	env.h.ServeHTTP(again, req)
	if again.Code != http.StatusOK || !bytes.Contains(again.Body.Bytes(), []byte("snapshot.json")) {
		t.Fatalf("the export after the refused one: %d (%d bytes), want the whole archive: the pin was not let go", again.Code, again.Body.Len())
	}
}

// quietChat is startingChat whose changelog never signals: no write of the
// test's own, nor a delayed broadcast of one, reaches the stream.
type quietChat struct{ startingChat }

func (quietChat) WatchSignal(context.Context) <-chan int64 { return make(chan int64) }

// The first event of a stream whose token ran out while its turn was
// starting is never delivered. The token's time runs out without a write,
// the stream hears no changelog move, and the heartbeat is far off: the read
// before the first event is the only one that can see it.
func TestAnAgentStreamWhoseTokenRunsOutBeforeItsFirstEventSendsNone(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	started, release := make(chan struct{}), make(chan struct{})
	env.wrap = func(ds substrate.Dataset) substrate.Dataset {
		return quietChat{startingChat{Dataset: ds, started: started, release: release}}
	}
	expires := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	token := env.mint(t, substrate.MintTokenInput{Label: "short", ExpiresAt: &expires})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req := httptest.NewRequest(http.MethodPost, helperChatPath, bytes.NewReader([]byte(`{"message":"hi"}`))).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		env.h.ServeHTTP(rec, req)
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the chat turn never started")
	}
	env.clock.advance(2 * time.Hour)
	close(release)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the agent stream never ended")
	}
	wantErrorCode(t, rec, http.StatusUnauthorized, codeAuth)
}
