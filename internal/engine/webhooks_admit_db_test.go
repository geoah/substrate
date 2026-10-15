package engine

// The webhook door's admission (webhooks.go admitWebhook) is one changelog
// transaction: the request's blob bytes are staged before it, holding no
// lock, and their manifests commit beside the pending entry.

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// doorTx is one committed transaction the webhook door made before its
// answer: the record entries it appended and its last seq.
type doorTx struct {
	entries []changeEntry
	maxSeq  int64
}

// recordDoorTxs collects every transaction committed on a context
// withForegroundWrites marked, which in these tests is the door's alone.
func recordDoorTxs(ds *dataset) func() []doorTx {
	var (
		mu  sync.Mutex
		txs []doorTx
	)
	ds.mu.Lock()
	ds.beforeSignal = func(tx *txn) {
		if foreground, _ := tx.ctx.Value(foregroundWritesKey{}).(bool); !foreground {
			return
		}
		mu.Lock()
		txs = append(txs, doorTx{entries: slices.Clone(tx.entries), maxSeq: tx.maxSeq})
		mu.Unlock()
	}
	ds.mu.Unlock()
	return func() []doorTx {
		mu.Lock()
		defer mu.Unlock()
		out := txs
		txs = nil
		return out
	}
}

func jsonDelivery(body string) substrate.WebhookRequest {
	return substrate.WebhookRequest{
		Method: "POST", ContentType: "application/json",
		Headers: map[string]string{"content-type": "application/json"},
		Body:    []byte(body),
	}
}

// A delivery commits its blob manifests, its pending entry and its delivery
// entry in one transaction, so the sender waits on the changelog lock once:
// for a raw body, for a multipart request whose file part and inline value
// both go to the blob store, and for a redelivery whose blob is already
// stored. The manifests keep the webhook actor, the delivery entry is the
// transaction's last, and its seq is the pending row's id.
func TestAWebhookDeliveryCommitsInOneTransaction(t *testing.T) {
	t.Parallel()
	ds := newRaceDataset(t)
	installLedgerConnector(t, ds, nowUTC().Add(-time.Hour))
	ctx := context.Background()
	waitGateIdle(t, &ds.gate)
	doorTxs := recordDoorTxs(ds)

	body := `{"say":"one transaction"}`
	cases := []struct {
		name  string
		req   substrate.WebhookRequest
		blobs []string
	}{
		{"a raw body", jsonDelivery(body), []string{blobDigest([]byte(body))}},
		{"a multipart request", substrate.WebhookRequest{
			Method: "POST", ContentType: "multipart/form-data",
			Parts: []substrate.WebhookPart{
				{Name: "note", Value: "an inline value"},
				{Name: "file", Filename: "a.txt", MediaType: "text/plain", Data: []byte("a file part")},
			},
		}, []string{blobDigest([]byte("an inline value")), blobDigest([]byte("a file part"))}},
		{"a redelivery of a stored body", jsonDelivery(body), nil},
	}
	for _, c := range cases {
		fid, err := ds.svc.receiveWebhook(ctx, testdb.Repository(t), ledgerHook, "", c.req, webhookFireHeld)
		if err != nil {
			t.Fatalf("%s: receive: %v", c.name, err)
		}
		txs := doorTxs()
		if len(txs) != 1 {
			t.Fatalf("%s: the door committed %d transactions, want 1", c.name, len(txs))
		}
		tx := txs[0]
		var manifests []string
		for _, e := range tx.entries {
			if e.kind != kindBlob {
				t.Fatalf("%s: the door's transaction wrote %s %s", c.name, e.kind, e.id)
			}
			if e.seq >= tx.maxSeq {
				t.Fatalf("%s: manifest entry %d is not before the delivery entry %d", c.name, e.seq, tx.maxSeq)
			}
			var actor string
			if err := ds.db.QueryRowContext(ctx, `SELECT actor FROM changelog WHERE seq = $1`, e.seq).Scan(&actor); err != nil {
				t.Fatal(err)
			}
			if actor != string(actorWebhook) {
				t.Fatalf("%s: manifest entry %d names actor %q, want %q", c.name, e.seq, actor, actorWebhook)
			}
			manifests = append(manifests, e.id)
		}
		slices.Sort(manifests)
		want := slices.Sorted(slices.Values(c.blobs))
		if !slices.Equal(manifests, want) {
			t.Fatalf("%s: the transaction settled manifests %v, want %v", c.name, manifests, want)
		}
		for _, digest := range c.blobs {
			info, _, err := ds.GetBlob(ctx, digest)
			if err != nil || info.Status != substrate.BlobStored {
				t.Fatalf("%s: blob %s after the delivery: %+v, %v", c.name, digest, info, err)
			}
		}
		failures, err := ds.TriggerFailures(ctx, ledgerHook)
		if err != nil {
			t.Fatal(err)
		}
		i := slices.IndexFunc(failures, func(f substrate.TriggerFailure) bool { return f.FireID == fid })
		if i < 0 {
			t.Fatalf("%s: no pending entry for fire %s", c.name, fid)
		}
		if failures[i].ID != tx.maxSeq {
			t.Fatalf("%s: the pending entry is %d, its delivery entry %d", c.name, failures[i].ID, tx.maxSeq)
		}
	}
}

// A process that stops between staging a delivery's bytes and committing
// its transaction leaves objects no manifest names and nothing else: no
// changelog entry, no pending row, no readable blob. The orphan sweep
// removes them once they are older than BlobUploadGrace. Not parallel: it
// sets the grace.
func TestAWebhookStagedButNotRecordedLeavesASweepableOrphan(t *testing.T) {
	prev := BlobUploadGrace
	t.Cleanup(func() { BlobUploadGrace = prev })
	BlobUploadGrace = time.Hour

	ds := newRaceDataset(t)
	installLedgerConnector(t, ds, nowUTC().Add(-time.Hour))
	ctx := context.Background()
	tr, _, err := ds.triggerByID(ctx, ledgerHook)
	if err != nil {
		t.Fatal(err)
	}
	head := func() int64 {
		var seq int64
		if err := ds.db.QueryRowContext(ctx, `SELECT coalesce(max(seq), 0) FROM changelog`).Scan(&seq); err != nil {
			t.Fatal(err)
		}
		return seq
	}
	before := head()

	body := []byte(`{"say":"staged, then the process stopped"}`)
	digest := blobDigest(body)
	if _, err := ds.stageWebhook(ctx, tr.WebhookHeaders, jsonDelivery(string(body)), "hook-staged", nowUTC(), ds.stageBlobs(actorWebhook)); err != nil {
		t.Fatalf("stage: %v", err)
	}
	store, err := ds.blobBytes()
	if err != nil {
		t.Fatal(err)
	}
	if held, err := store.Exists(ctx, digest); err != nil || !held {
		t.Fatalf("the staged bytes are not in the store: %v, %v", held, err)
	}
	if after := head(); after != before {
		t.Fatalf("staging appended to the changelog: head %d, was %d", after, before)
	}
	err = ds.db.QueryRowContext(ctx, `SELECT 1 FROM records WHERE id = $1`, digest).Scan(new(int))
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("staging wrote a manifest: %v", err)
	}
	if _, _, err := ds.GetBlob(ctx, digest); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("a staged blob read back before its manifest committed: %v", err)
	}
	if ids := failureIDs(t, ds, ledgerHook); len(ids) != 0 {
		t.Fatalf("staging left pending entries %v", ids)
	}

	// Inside the grace the sweep leaves the bytes for the transaction that
	// is about to name them.
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("gc: %v", err)
	}
	if held, err := store.Exists(ctx, digest); err != nil || !held {
		t.Fatalf("the sweep took staged bytes inside the grace: %v, %v", held, err)
	}

	BlobUploadGrace = 0
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("gc: %v", err)
	}
	if held, err := store.Exists(ctx, digest); err != nil || held {
		t.Fatalf("the orphan sweep left the staged bytes: %v, %v", held, err)
	}
}

// Bytes the sweep deletes between the stage and the transaction (bytes that
// were already in the store, older than the grace, with no manifest) are put
// back under the digest lock before the manifest says `stored`, so the
// delivery commits and its blob reads.
func TestAWebhookRecordPutsBackBytesSweptAfterTheStage(t *testing.T) {
	t.Parallel()
	ds := newRaceDataset(t)
	installLedgerConnector(t, ds, nowUTC().Add(-time.Hour))
	ctx := context.Background()
	tr, _, err := ds.triggerByID(ctx, ledgerHook)
	if err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"say":"swept before the commit"}`)
	digest := blobDigest(body)
	blobs := ds.stageBlobs(actorWebhook)
	row, err := ds.stageWebhook(ctx, tr.WebhookHeaders, jsonDelivery(string(body)), "hook-swept", nowUTC(), blobs)
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	store, err := ds.blobBytes()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, digest); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.recordWebhook(ctx, tr.ID, row, blobs, 0); err != nil {
		t.Fatalf("record: %v", err)
	}
	if _, data, err := ds.GetBlob(ctx, digest); err != nil || string(data) != string(body) {
		t.Fatalf("the delivery's blob after the record: %q, %v", data, err)
	}
}
