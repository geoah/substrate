package api

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
)

// blobPut uploads body to target through the door under env's token and
// answers the manifest the engine stored.
func blobPut(t *testing.T, env *dbEnv, target, body string, headers map[string]string) substrate.BlobInfo {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, target, strings.NewReader(body))
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("Authorization", "Bearer "+env.token)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	env.h.ServeHTTP(rec, req)
	wantStatus(t, rec, http.StatusCreated)
	return decodeJSON[substrate.BlobInfo](t, rec)
}

// A blob whose stored bytes no longer hash to its digest is refused before
// the status line: `500 internal` with a message naming the digest (never the
// masked "internal error", which would hide which blob is damaged), none of
// the blob's own headers, and one ERROR line naming the digest. The bytes are
// damaged where the engine keeps them, under the data root.
//
// Serial: captureLogs replaces the process's default logger.
func TestBlobReadOfCorruptBytesAnswers500NamingTheDigest(t *testing.T) {
	root := t.TempDir()
	env := newDBEnv(t, withEngine(engine.WithDataRoot(root)))
	digest := blobPut(t, env, "/api/"+APIVersion+"/blobs", "payload", map[string]string{"Content-Type": "text/plain"}).Digest
	stored := filepath.Join(root, "repositories", env.ds.Repository().ID, "blobs", digest)
	if err := os.WriteFile(stored, []byte("PAYLOAD"), 0o600); err != nil {
		t.Fatalf("damage the stored bytes: %v", err)
	}
	logs := captureLogs(t)
	route := "/api/" + APIVersion + "/blobs/{digest}"

	rec := env.do(t, http.MethodGet, "/api/"+APIVersion+"/blobs/"+digest, env.token, nil)
	wantErrorCode(t, rec, http.StatusInternalServerError, codeInternal)
	problem := decodeJSON[substrate.ErrorEnvelope](t, rec)
	if !strings.Contains(problem.Error.Message, digest) {
		t.Fatalf("the problem message %q does not name %s", problem.Error.Message, digest)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want the problem body's application/json", got)
	}
	if rec.Header().Get("ETag") != "" || rec.Header().Get("Content-Disposition") != "" {
		t.Fatalf("the refusal carries the blob's headers: %v", rec.Header())
	}
	wantOneErrorLog(t, logs, "request failed", map[string]string{
		"method": http.MethodGet, "route": route, "repository": env.ds.Repository().ID,
	})
	if line := logs.at(slog.LevelError)[0]; !strings.Contains(line.attrs["error"], digest) {
		t.Fatalf("the ERROR line does not name %s: %v", digest, line.attrs)
	}
}

// A blob the engine stored answers a read whose If-None-Match names its ETag
// with a 304 and no body, any other read with a 200 and its bytes, both with
// the ETag and a private, immutable Cache-Control; a conditional read of a
// digest the repository never stored is not found.
func TestBlobConditionalReadOfAStoredBlob(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	digest := blobPut(t, env, "/api/"+APIVersion+"/blobs", "payload", map[string]string{"Content-Type": "text/plain"}).Digest
	path := "/api/" + APIVersion + "/blobs/" + digest
	etag := `"` + digest + `"`

	for _, match := range []string{etag, "W/" + etag, `"blob-sha256-other", ` + etag, "*"} {
		rec := env.do(t, http.MethodGet, path, env.token, nil, "If-None-Match", match)
		wantStatus(t, rec, http.StatusNotModified)
		if rec.Body.Len() != 0 || rec.Header().Get("ETag") != etag || rec.Header().Get("Cache-Control") != blobCacheControl ||
			rec.Header().Get("Vary") != "Authorization" {
			t.Fatalf("If-None-Match %s: headers %v, body %q", match, rec.Header(), rec.Body)
		}
	}

	rec := env.do(t, http.MethodGet, path, env.token, nil, "If-None-Match", `"blob-sha256-other"`)
	wantStatus(t, rec, http.StatusOK)
	if rec.Body.String() != "payload" || rec.Header().Get("ETag") != etag || rec.Header().Get("Cache-Control") != blobCacheControl ||
		rec.Header().Get("Vary") != "Authorization" {
		t.Fatalf("a non-matching read: headers %v, body %q", rec.Header(), rec.Body)
	}

	other := substrate.BlobDigestPrefix + strings.Repeat("f", 64)
	rec = env.do(t, http.MethodGet, "/api/"+APIVersion+"/blobs/"+other, env.token, nil, "If-None-Match", `"`+other+`"`)
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
}
