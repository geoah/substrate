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
