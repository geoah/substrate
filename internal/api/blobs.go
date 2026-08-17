package api

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/geoah/substrate/internal/substrate"
)

// maxBlobBody bounds one uploaded blob. Blobs are archives and attachments,
// not streams; a larger payload belongs in a chunked design this build does
// not have.
const maxBlobBody = 64 << 20

// maxConcurrentBlobPuts is the route-level concurrency bound on PUT /blobs.
// Each in-flight PUT can hold a full body in memory (hashing, the byte slice,
// driver encoding, Postgres), so an unbounded number of parallel 64 MiB
// uploads is a memory-exhaustion DoS even with the per-request cap. The
// weighted semaphore (weight 1 per request) caps how many bodies are resident
// at once; the rest queue holding only their request goroutine.
const maxConcurrentBlobPuts = 8

// blobPutSem is the process-wide PUT /blobs admission semaphore. A buffered
// channel is a counting (weighted, weight-1) semaphore: a token per slot.
var blobPutSem = make(chan struct{}, maxConcurrentBlobPuts)

// putBlob is PUT /blobs and PUT /blobs/{digest}: it stores the request body in
// the repository's content-addressed byte store under the derived digest, verifying
// it against a supplied one, and mints/settles the blob manifest at
// status=stored. The same bytes always dedup to the same blob.
func (h *handler) putBlob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bs, ok := DatasetFrom(ctx).(substrate.BlobStore)
	if !ok {
		writeUnsupported(w, "this service has no blob store")
		return
	}
	// Admission bound: acquire a slot before allocating the body, so waiting
	// requests hold only their goroutine, not a full 64 MiB buffer.
	select {
	case blobPutSem <- struct{}{}:
		defer func() { <-blobPutSem }()
	case <-ctx.Done():
		writeUnavailable(w, time.Second, "blob store is busy, retry")
		return
	}

	// Stream the body to a bounded temp file while hashing, instead of
	// io.ReadAll holding the whole slow-loris transfer resident.
	// The hard size cap still applies via MaxBytesReader.
	data, sum, err := drainBlobBody(w, r)
	if err != nil {
		if errors.Is(err, errBlobTooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, codeBadRequest, "blob body too large or unreadable")
			return
		}
		writeError(w, http.StatusInternalServerError, codeInternal, "could not receive blob body")
		return
	}
	// Fail fast on a claimed-digest mismatch before touching the byte store —
	// the engine re-derives and re-verifies authoritatively.
	wantDigest := chi.URLParam(r, "digest")
	if wantDigest != "" && wantDigest != sum {
		writeError(w, http.StatusBadRequest, codeBadRequest, "digest mismatch — the bytes do not hash to the addressed digest")
		return
	}

	info, err := bs.PutBlob(ctx, ActorFrom(ctx), substrate.BlobUpload{
		Name:     uploadName(r),
		MimeType: r.Header.Get("Content-Type"),
	}, data, wantDigest)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	w.Header().Set("Location", "/api/"+APIVersion+"/blobs/"+info.Digest)
	writeJSON(w, http.StatusCreated, info)
}

// uploadName reads the display name a PUT gives its bytes: `?name=` first,
// because a query parameter is what a curl or a browser fetch can set without
// ceremony, and the `filename` of a Content-Disposition header second, because
// that is what an upload form already sends. Neither is required — a blob
// without a name is a blob addressed by its digest, which is what it always
// was. The engine validates; this only finds the string.
func uploadName(r *http.Request) string {
	if name := r.URL.Query().Get("name"); name != "" {
		return name
	}
	cd := r.Header.Get("Content-Disposition")
	if cd == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(cd)
	if err != nil {
		return ""
	}
	return params["filename"]
}

// errBlobTooLarge distinguishes a body that overran the cap (or read error)
// from an internal temp-storage failure.
var errBlobTooLarge = errors.New("blob body too large or unreadable")

// drainBlobBody streams the request body to a bounded temp file while computing
// its sha-256, enforcing the hard size cap, then reads it back for the byte
// store. Streaming keeps the receive phase at a fixed buffer instead of holding
// the whole (possibly slow) transfer in memory.
func drainBlobBody(w http.ResponseWriter, r *http.Request) (data []byte, digest string, err error) {
	tmp, err := os.CreateTemp("", "substrate-blob-*")
	if err != nil {
		return nil, "", err
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	limited := http.MaxBytesReader(w, r.Body, maxBlobBody)
	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, hasher), limited); err != nil {
		return nil, "", errBlobTooLarge
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, "", err
	}
	data, err = io.ReadAll(tmp)
	if err != nil {
		return nil, "", err
	}
	return data, substrate.BlobDigestPrefix + hex.EncodeToString(hasher.Sum(nil)), nil
}

// getBlob is GET /blobs/{digest}: it streams a blob's bytes, repository-scoped by
// the authenticated dataset. A digest another repository stored is simply absent
// here — a cross-repository read is a not-found, never a leak.
func (h *handler) getBlob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bs, ok := DatasetFrom(ctx).(substrate.BlobStore)
	if !ok {
		writeUnsupported(w, "this service has no blob store")
		return
	}
	info, data, err := bs.GetBlob(ctx, chi.URLParam(r, "digest"))
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	// The mime type is OPTIONAL on the manifest, so the read falls back to the
	// honest "some bytes" rather than guessing from the content.
	ct := info.MimeType
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	// A named blob says its name back, so a browser save keeps the filename.
	// `inline`: the name is metadata, not an instruction to download.
	if info.Name != "" {
		w.Header().Set("Content-Disposition",
			mime.FormatMediaType("inline", map[string]string{"filename": info.Name}))
	}
	w.Header().Set("ETag", `"`+info.Digest+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
