package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/blobbytes"
	"github.com/geoah/substrate/internal/substrate"
)

// A blob whose stored bytes no longer hash to its digest is refused before
// the status line: `500 internal` with a message naming the digest (never the
// masked "internal error", which would hide which blob is damaged), none of
// the blob's own headers, and one ERROR line naming the digest.
func TestBlobReadOfCorruptBytesAnswers500NamingTheDigest(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	logs := captureLogs(t)

	digest := substrate.BlobDigestPrefix + strings.Repeat("c", 64)
	ds.errs["GetBlob"] = fmt.Errorf("%w: blobbytes: the stored bytes do not match their digest: %s hashes to %s",
		substrate.ErrCorrupt, digest, substrate.BlobDigestPrefix+strings.Repeat("d", 64))
	route := "/api/" + APIVersion + "/blobs/{digest}"

	rec := env.do(t, http.MethodGet, "/api/"+APIVersion+"/blobs/"+digest, tok, nil)
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
		"method": http.MethodGet, "route": route, "repository": fakeRepository,
	})
	if line := logs.at(slog.LevelError)[0]; !strings.Contains(line.attrs["error"], digest) {
		t.Fatalf("the ERROR line does not name %s: %v", digest, line.attrs)
	}
}

// The store bounds a read of a manifest with no size by the upload cap, so the
// two must stay one number: a larger cap here would store blobs that read
// back refused.
func TestBlobUploadCapIsTheStoreReadBound(t *testing.T) {
	if maxBlobBody != blobbytes.MaxUnsizedRead {
		t.Fatalf("the upload cap is %d bytes, the store reads at most %d", maxBlobBody, blobbytes.MaxUnsizedRead)
	}
}
