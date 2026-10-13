package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

// conditionalBlobDS stores one blob and counts the manifest reads and the
// byte reads it answers.
type conditionalBlobDS struct {
	*fakeDataset
	digest      string
	stats, gets atomic.Int32
}

func (b *conditionalBlobDS) info(digest string) (*substrate.BlobInfo, error) {
	if digest != b.digest {
		return nil, fmt.Errorf("%w: blob %s", substrate.ErrNotFound, digest)
	}
	return &substrate.BlobInfo{Digest: digest, Size: 7, MediaType: "text/plain", Status: substrate.BlobStored}, nil
}

func (b *conditionalBlobDS) StatBlob(_ context.Context, digest string) (*substrate.BlobInfo, error) {
	b.stats.Add(1)
	return b.info(digest)
}

func (b *conditionalBlobDS) GetBlob(_ context.Context, digest string) (*substrate.BlobInfo, []byte, error) {
	b.gets.Add(1)
	info, err := b.info(digest)
	if err != nil {
		return nil, nil, err
	}
	return info, []byte("payload"), nil
}

// A read whose If-None-Match names the blob's ETag is a 304 answered from the
// manifest, with no bytes read; any other read is the 200 with the bytes.
// Both carry the ETag and a private, immutable Cache-Control, and a plain read
// costs no manifest read beside the one GetBlob makes.
func TestBlobConditionalReadAnswers304WithoutTheBytes(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	digest := substrate.BlobDigestPrefix + strings.Repeat("e", 64)
	ds := &conditionalBlobDS{fakeDataset: env.svc.datasets[fakeRepository], digest: digest}
	env.svc.wrap = func(*fakeDataset) substrate.Dataset { return ds }
	path := "/api/" + APIVersion + "/blobs/" + digest
	etag := `"` + digest + `"`

	for _, match := range []string{etag, "W/" + etag, `"blob-sha256-other", ` + etag, "*"} {
		rec := env.do(t, http.MethodGet, path, tok, nil, "If-None-Match", match)
		wantStatus(t, rec, http.StatusNotModified)
		if rec.Body.Len() != 0 || rec.Header().Get("ETag") != etag || rec.Header().Get("Cache-Control") != blobCacheControl ||
			rec.Header().Get("Vary") != "Authorization" {
			t.Fatalf("If-None-Match %s: headers %v, body %q", match, rec.Header(), rec.Body)
		}
	}
	if n := ds.gets.Load(); n != 0 {
		t.Fatalf("a 304 read the bytes %d times", n)
	}

	rec := env.do(t, http.MethodGet, path, tok, nil, "If-None-Match", `"blob-sha256-other"`)
	wantStatus(t, rec, http.StatusOK)
	if rec.Body.String() != "payload" || rec.Header().Get("Cache-Control") != blobCacheControl ||
		rec.Header().Get("Vary") != "Authorization" {
		t.Fatalf("a non-matching read: headers %v, body %q", rec.Header(), rec.Body)
	}

	other := substrate.BlobDigestPrefix + strings.Repeat("f", 64)
	rec = env.do(t, http.MethodGet, "/api/"+APIVersion+"/blobs/"+other, tok, nil, "If-None-Match", `"`+other+`"`)
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)

	stats := ds.stats.Load()
	wantStatus(t, env.do(t, http.MethodGet, path, tok, nil), http.StatusOK)
	if ds.stats.Load() != stats {
		t.Fatal("an unconditional read made a second manifest read")
	}
}

// blockingGetDS is a blob store whose GetBlob blocks until released,
// recording the peak number of reads in flight.
type blockingGetDS struct {
	*fakeDataset
	inflight, peak atomic.Int32
	started        chan struct{}
	release        chan struct{}
}

func (b *blockingGetDS) GetBlob(_ context.Context, digest string) (*substrate.BlobInfo, []byte, error) {
	n := b.inflight.Add(1)
	defer b.inflight.Add(-1)
	for {
		p := b.peak.Load()
		if n <= p || b.peak.CompareAndSwap(p, n) {
			break
		}
	}
	b.started <- struct{}{}
	<-b.release
	return &substrate.BlobInfo{Digest: digest, Status: substrate.BlobStored}, []byte("payload"), nil
}

// GET /blobs is bounded like PUT: a read holds the whole blob while it checks
// the digest, so no more than maxConcurrentBlobGets run at once.
func TestBlobGetIsConcurrencyBounded(t *testing.T) {
	ds := &blockingGetDS{
		fakeDataset: newFakeDataset(fakeRepository),
		started:     make(chan struct{}, 64),
		release:     make(chan struct{}),
	}
	h := &handler{}
	var wg sync.WaitGroup
	for range maxConcurrentBlobGets + 4 {
		wg.Go(func() {
			ctx := withRequestAuth(context.Background(), ds, substrate.TokenInfo{}, substrate.ActorAPI)
			req := httptest.NewRequest(http.MethodGet, "/api/"+APIVersion+"/blobs/x", nil).WithContext(ctx)
			h.getBlob(httptest.NewRecorder(), req)
		})
	}
	for i := range maxConcurrentBlobGets {
		select {
		case <-ds.started:
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d of %d permitted reads started", i, maxConcurrentBlobGets)
		}
	}
	select {
	case <-ds.started:
		t.Fatalf("a read ran beyond the concurrency cap of %d", maxConcurrentBlobGets)
	case <-time.After(300 * time.Millisecond):
	}
	close(ds.release)
	wg.Wait()
	if got := ds.peak.Load(); got != maxConcurrentBlobGets {
		t.Fatalf("peak concurrent reads = %d, want exactly %d", got, maxConcurrentBlobGets)
	}
}

// bigBlobDS answers every read with the same large blob.
type bigBlobDS struct {
	*fakeDataset
	data []byte
}

func (b *bigBlobDS) GetBlob(_ context.Context, digest string) (*substrate.BlobInfo, []byte, error) {
	return &substrate.BlobInfo{Digest: digest, Size: int64(len(b.data)), Status: substrate.BlobStored}, b.data, nil
}

// A read whose client stops taking the bytes gives its slot back at the
// transfer deadline: otherwise eight such clients would hold every blob read
// on the server.
func TestBlobSlowReaderGivesItsSlotBack(t *testing.T) {
	shortBodyDeadlines(t)
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := &bigBlobDS{fakeDataset: env.svc.datasets[fakeRepository], data: make([]byte, 48<<20)}
	env.svc.wrap = func(*fakeDataset) substrate.Dataset { return ds }
	srv := httptest.NewServer(env.h)
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	digest := substrate.BlobDigestPrefix + strings.Repeat("a", 64)
	if _, err := fmt.Fprintf(conn, "GET /api/%s/blobs/%s HTTP/1.1\r\nHost: substrate.test\r\nAuthorization: Bearer %s\r\n\r\n", APIVersion, digest, tok); err != nil {
		t.Fatal(err)
	}
	waitFor := func(what string, done func() bool) {
		t.Helper()
		for deadline := time.Now().Add(3 * time.Second); !done(); time.Sleep(10 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatal(what)
			}
		}
	}
	waitFor("the read never took a slot", func() bool { return len(blobGetSem) == 1 })
	waitFor("a client that stopped reading still holds its slot", func() bool { return len(blobGetSem) == 0 })
}

// A GET that declares a body and never sends it is still answered, a 200
// and a 304 alike, gives its slot back and loses its connection: net/http
// reads a declared body before the answer goes out, and up to 256 KiB of it
// again after the handler returns, either of which would otherwise hold the
// slot, or the connection, for as long as the client waits.
func TestBlobGetWithAStalledBodyHoldsNoSlot(t *testing.T) {
	d := shortBodyDeadlines(t)
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	digest := substrate.BlobDigestPrefix + strings.Repeat("a", 64)
	big := &bigBlobDS{fakeDataset: env.svc.datasets[fakeRepository], data: make([]byte, 64<<10)}
	conditional := &conditionalBlobDS{fakeDataset: env.svc.datasets[fakeRepository], digest: digest}
	srv := httptest.NewServer(env.h)
	defer srv.Close()

	path := "/api/" + APIVersion + "/blobs/" + digest
	stalledGet := func(what string, headers map[string]string, want int) {
		t.Helper()
		got, conn, br := stallConn(t, srv, http.MethodGet, path, headers, "{", 3*time.Second)
		defer func() { _ = conn.Close() }()
		if got != want {
			t.Fatalf("%s with a stalled body got status %d within 3s, want %d (0 is no answer)", what, got, want)
		}
		_ = conn.SetReadDeadline(time.Now().Add(20 * d))
		_, err := br.ReadByte()
		var netErr net.Error
		if err == nil || (errors.As(err, &netErr) && netErr.Timeout()) {
			t.Fatalf("%s with a stalled body: the server kept the connection %s past its answer (read: %v), want it closed", what, 20*d, err)
		}
	}
	env.svc.wrap = func(*fakeDataset) substrate.Dataset { return conditional }
	stalledGet("a conditional GET", map[string]string{"Authorization": "Bearer " + tok, "If-None-Match": `"` + digest + `"`}, http.StatusNotModified)
	env.svc.wrap = func(*fakeDataset) substrate.Dataset { return big }
	stalledGet("a GET", map[string]string{"Authorization": "Bearer " + tok}, http.StatusOK)
	for deadline := time.Now().Add(3 * time.Second); len(blobGetSem) != 0; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("a GET with a stalled body still holds its slot")
		}
	}
}
