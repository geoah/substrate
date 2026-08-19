package api

// #14: PUT /blobs is concurrency-bounded. Without the route-level semaphore an
// authenticated caller could open an unbounded number of parallel 64 MiB uploads
// and exhaust memory before the per-request cap helps. This drives more requests
// than the cap and asserts the number of concurrently in-flight uploads never
// exceeds it.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// blockingBlobDS is a blob-capable dataset whose PutBlob blocks until released,
// recording the peak number of concurrent in-flight uploads.
type blockingBlobDS struct {
	*fakeDataset
	inflight int32
	maxSeen  int32
	started  chan struct{}
	release  chan struct{}
}

func (b *blockingBlobDS) PutBlob(_ context.Context, _ substrate.Actor, up substrate.BlobUpload, data []byte, _ string) (*substrate.BlobInfo, error) {
	n := atomic.AddInt32(&b.inflight, 1)
	for {
		m := atomic.LoadInt32(&b.maxSeen)
		if n <= m || atomic.CompareAndSwapInt32(&b.maxSeen, m, n) {
			break
		}
	}
	b.started <- struct{}{}
	<-b.release
	atomic.AddInt32(&b.inflight, -1)
	return &substrate.BlobInfo{
		Digest: substrate.BlobDigestPrefix + strings.Repeat("a", 64),
		Size:   int64(len(data)), Name: up.Name, MediaType: up.MediaType,
		Status: substrate.BlobStored,
	}, nil
}

func (b *blockingBlobDS) GetBlob(_ context.Context, _ string) (*substrate.BlobInfo, []byte, error) {
	return nil, nil, substrate.ErrNotFound
}

var _ substrate.BlobStore = (*blockingBlobDS)(nil)

func TestBlobPutIsConcurrencyBounded(t *testing.T) {
	ds := &blockingBlobDS{
		fakeDataset: newFakeDataset("geoah"),
		started:     make(chan struct{}, 64),
		release:     make(chan struct{}),
	}
	h := &handler{}

	total := maxConcurrentBlobPuts + 4
	var wg sync.WaitGroup
	for range total {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := withRequestAuth(context.Background(), ds, substrate.TokenInfo{}, substrate.ActorAPI)
			req := httptest.NewRequest(http.MethodPut, "/api/"+APIVersion+"/blobs",
				strings.NewReader("payload")).WithContext(ctx)
			req.Header.Set("Content-Type", "text/plain")
			h.putBlob(httptest.NewRecorder(), req)
		}()
	}

	// Exactly the cap should reach PutBlob; the rest block on the semaphore.
	for i := range maxConcurrentBlobPuts {
		select {
		case <-ds.started:
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d of %d permitted puts started", i, maxConcurrentBlobPuts)
		}
	}
	// No further upload may start while the cap is held.
	select {
	case <-ds.started:
		t.Fatalf("a put ran beyond the concurrency cap of %d", maxConcurrentBlobPuts)
	case <-time.After(300 * time.Millisecond):
	}

	close(ds.release) // let everyone drain
	wg.Wait()

	if got := atomic.LoadInt32(&ds.maxSeen); got != int32(maxConcurrentBlobPuts) {
		t.Fatalf("peak concurrent puts = %d, want exactly %d", got, maxConcurrentBlobPuts)
	}
}
