package api

// Codex regression round 2 (server, API surface):
//   #1  the bundle lifecycle is gated behind the engine's exclusive fence
//   #4  a successful uninstall acks with a tombstone, never an error
//   #16 a malformed trailing closer {}} is bad_request
//
// The bundle lifecycle runs on the engine (regress_db_test.go); the decode is
// the handler's own.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---- strict REST decode trailing closer (#16) -------------------------------

// TestStrictDecodeRejectsTrailingCloser pins codex regress #16: a body like
// {}} must be bad_request. Decoder.More() missed it; a required io.EOF second
// decode catches the stray closer.
func TestStrictDecodeRejectsTrailingCloser(t *testing.T) {
	env := newStubEnv(t, nil)
	req := httptest.NewRequest(http.MethodPost, recordsPath, strings.NewReader("{}}"))
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("Authorization", "Bearer "+stubToken)
	rec := httptest.NewRecorder()
	env.h.ServeHTTP(rec, req)
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
}
