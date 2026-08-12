package engine_test

// Phase-1 adversarial-review regressions for the blob store (findings #5, #6,
// #9, #10). Each test reproduces the reviewer's exact scenario and pins the fix.

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
)

// #5: a write validates a blob-ref while its record is still uncommitted; a GC
// sweep whose snapshot predates that reference must not be able to leave a
// committed blob-ref dangling. The exclusive per-digest lock serializes the
// sweep behind the still-in-flight reference (held SHARED), and the in-tx
// re-check re-observes the truth: either the reference wins and the blob
// survives, or the sweep wins and the reference is REFUSED — never a committed
// reference to a collected blob.
func TestBlobGCCannotDangleUncommittedRef(t *testing.T) {
	prev := engine.BlobUploadGrace
	engine.BlobUploadGrace = 0
	t.Cleanup(func() { engine.BlobUploadGrace = prev })

	ctx := context.Background()
	ds, raw := newDatasetWithDB(t)
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, blobDocDocs("attachment", false)); err != nil {
		t.Fatalf("install doc type: %v", err)
	}
	bs := blobStoreOf(t, ds)
	blob, err := bs.PutBlob(ctx, owner, substrate.BlobUpload{MimeType: "text/plain"}, []byte("racy payload"), "")
	if err != nil {
		t.Fatalf("put blob: %v", err)
	}

	// Simulate a writer mid-validation of a reference to `blob`: hold the digest
	// lock SHARED in an uncommitted transaction, exactly as validateBlobRefs does.
	tx1, err := raw.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := tx1.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock_shared(hashtext($1)::bigint)`, ds.Repository().ID+"|blob|"+blob.Digest); err != nil {
		t.Fatalf("shared lock: %v", err)
	}

	// GC starts now: its snapshot sees `blob` unreferenced, so it marks it a
	// victim and reaches for the EXCLUSIVE digest lock — which must block behind
	// tx1's shared hold instead of deleting.
	gcDone := make(chan error, 1)
	go func() { _, e := ds.RunGC(ctx); gcDone <- e }()

	// While GC is blocked, a real reference to `blob` is written.
	refDone := make(chan error, 1)
	go func() {
		_, e := ds.Put(ctx, owner, substrate.PutInput{
			Kind: blobAuthority + "/doc", ID: "racedoc",
			Properties: map[string]any{"attachment": blob.Digest},
		})
		refDone <- e
	}()

	time.Sleep(500 * time.Millisecond) // let GC and the reference both queue on the lock

	// Release the simulated in-flight writer; GC and the reference now resolve.
	if err := tx1.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	if e := <-gcDone; e != nil {
		t.Fatalf("gc: %v", e)
	}
	refErr := <-refDone

	// The invariant: a COMMITTED reference may never point at a collected blob.
	if refErr == nil {
		if _, _, err := bs.GetBlob(ctx, blob.Digest); err != nil {
			t.Fatalf("DANGLING blobref: the reference committed but its blob was collected: %v", err)
		}
	} else if !errors.Is(refErr, substrate.ErrValidation) {
		t.Fatalf("reference failed for the wrong reason (want a clean validation refusal): %v", refErr)
	}
}

// #6: a freshly uploaded, not-yet-referenced blob must survive GC long enough
// to become referenced — the unreferenced-upload grace. Past the grace the same
// orphan is collectable.
func TestBlobUploadGraceSparesFreshBlob(t *testing.T) {
	prev := engine.BlobUploadGrace
	t.Cleanup(func() { engine.BlobUploadGrace = prev })

	ctx := context.Background()
	_, ds := newDataset(t)
	bs := blobStoreOf(t, ds)

	engine.BlobUploadGrace = time.Hour
	blob, err := bs.PutBlob(ctx, owner, substrate.BlobUpload{MimeType: "text/plain"}, []byte("fresh, not yet referenced"), "")
	if err != nil {
		t.Fatalf("put blob: %v", err)
	}
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("gc within grace: %v", err)
	}
	if _, _, err := bs.GetBlob(ctx, blob.Digest); err != nil {
		t.Fatalf("the grace must spare a freshly uploaded blob, but it was collected: %v", err)
	}

	engine.BlobUploadGrace = 0
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("gc past grace: %v", err)
	}
	if _, _, err := bs.GetBlob(ctx, blob.Digest); err == nil {
		t.Fatal("past the grace, an unreferenced blob must be collected")
	}
}

// #9: the generic record API may not forge a blob manifest, and a dedup PUT
// returns the FIRST writer's authoritative mime, not the second request's claim.
func TestBlobManifestForgeRefusedAndDedupAuthoritative(t *testing.T) {
	ctx := context.Background()
	_, ds := newDataset(t)
	bs := blobStoreOf(t, ds)

	forged := substrate.BlobDigestPrefix + strings.Repeat("2", 64)
	_, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: "core.substrate.reamde.dev/blob", ID: forged,
		Properties: map[string]any{
			"digest": forged, "size": 99999,
			"mimeType": "text/plain", "status": "stored",
		},
	})
	wantErr(t, err, substrate.ErrForbidden, "a forged generic blob manifest must be refused")

	data := []byte("same bytes, different claimed mime")
	first, err := bs.PutBlob(ctx, owner, substrate.BlobUpload{MimeType: "text/plain"}, data, "")
	if err != nil {
		t.Fatalf("put first: %v", err)
	}
	second, err := bs.PutBlob(ctx, owner, substrate.BlobUpload{MimeType: "image/png"}, data, "")
	if err != nil {
		t.Fatalf("put second (dedup): %v", err)
	}
	if second.Digest != first.Digest {
		t.Fatalf("dedup broke: %q != %q", second.Digest, first.Digest)
	}
	if second.MimeType != "text/plain" {
		t.Fatalf("dedup PUT returned %q, want the authoritative text/plain", second.MimeType)
	}
	got, _, err := bs.GetBlob(ctx, first.Digest)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.MimeType != "text/plain" {
		t.Fatalf("GET mime = %q, want text/plain", got.MimeType)
	}
}

// #10: an unreferenced STORED blob's manifest is TOMBSTONED by the blob sweep
// (not hard-deleted), so a reader can observe the promised tombstone; ordinary
// record GC hard-deletes that tombstone on a later pass.
func TestBlobGCTombstonesStoredManifest(t *testing.T) {
	prev := engine.BlobUploadGrace
	engine.BlobUploadGrace = 0
	t.Cleanup(func() { engine.BlobUploadGrace = prev })

	ctx := context.Background()
	ds, raw := newDatasetWithDB(t)
	bs := blobStoreOf(t, ds)
	blob, err := bs.PutBlob(ctx, owner, substrate.BlobUpload{MimeType: "text/plain"}, []byte("orphan to tombstone"), "")
	if err != nil {
		t.Fatalf("put blob: %v", err)
	}

	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("gc: %v", err)
	}

	// Bytes are hard-deleted immediately.
	if _, _, err := bs.GetBlob(ctx, blob.Digest); err == nil {
		t.Fatal("orphan bytes survived gc")
	}
	// The manifest row is still present with deleted_at set — the tombstone.
	var deletedAt sql.NullTime
	err = raw.QueryRowContext(ctx, `SELECT deleted_at FROM records WHERE id = $1`, blob.Digest).Scan(&deletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		t.Fatal("the manifest was hard-deleted in the blob sweep; the contract requires a tombstone")
	}
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if !deletedAt.Valid {
		t.Fatal("the manifest was not tombstoned")
	}

	// Ordinary record GC hard-deletes the tombstone on a later pass.
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("second gc: %v", err)
	}
	err = raw.QueryRowContext(ctx, `SELECT 1 FROM records WHERE id = $1`, blob.Digest).Scan(new(int))
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("the tombstoned manifest was not hard-deleted by later GC: %v", err)
	}
}
