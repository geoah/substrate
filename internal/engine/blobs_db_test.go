package engine_test

// The content-addressed blob store: bytes land under a derived
// digest, the blob manifest is an ordinary record, a blob-ref renders the
// manifest and never the bytes, reads are repository-scoped, and an unreferenced
// blob is GC-collectable.

import (
	"bytes"
	"context"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// blobStore is the engine's byte-store seam, asserted at runtime the way the
// API layer reaches it (it is deliberately off the frozen substrate.Dataset).
type blobStore interface {
	PutBlob(ctx context.Context, actor substrate.Actor, mimeType string, data []byte, wantDigest string) (*substrate.BlobInfo, error)
	GetBlob(ctx context.Context, digest string) (*substrate.BlobInfo, []byte, error)
}

func blobStoreOf(t *testing.T, ds substrate.Dataset) blobStore {
	t.Helper()
	bs, ok := ds.(blobStore)
	if !ok {
		t.Fatal("dataset does not implement the blob store seam")
	}
	return bs
}

const blobAuthority = "attachments.example.substrate.reamde.dev"

// blobDocType declares a type carrying a blob-ref property (scalar or repeated).
func blobDocDocs(prop string, repeated bool) []map[string]any {
	pdef := map[string]any{"type": "blobref"}
	if repeated {
		pdef["repeated"] = true
	}
	return []map[string]any{
		vocabulary.AuthorityManifest(blobAuthority, ""),
		vocabulary.KindManifest(blobAuthority,
			map[string]any{"singular": "doc", "plural": "docs"},
			map[string]any{"properties": map[string]any{prop: pdef}}),
	}
}

func TestBlobPutStoresMintsAndStreams(t *testing.T) {
	ctx := context.Background()
	_, ds := newDataset(t)
	bs := blobStoreOf(t, ds)
	data := []byte("the untransformed provider payload")

	info, err := bs.PutBlob(ctx, owner, "text/plain", data, "")
	if err != nil {
		t.Fatalf("put blob: %v", err)
	}
	if info.Status != substrate.BlobStored {
		t.Fatalf("status = %q, want stored", info.Status)
	}
	if info.Size != int64(len(data)) {
		t.Fatalf("size = %d, want %d", info.Size, len(data))
	}
	if info.Digest[:len(substrate.BlobDigestPrefix)] != substrate.BlobDigestPrefix {
		t.Fatalf("digest %q lacks the %q prefix", info.Digest, substrate.BlobDigestPrefix)
	}

	// The manifest is an ordinary record, id == digest, status stored.
	ent := mustGet(t, ds, "core.substrate.reamde.dev/blob", info.Digest)
	if ent.Kind != "core.substrate.reamde.dev/blob" {
		t.Fatalf("manifest type = %q", ent.Kind)
	}
	if ent.Properties["status"] != "stored" {
		t.Fatalf("manifest status = %v", ent.Properties["status"])
	}
	if ent.Properties["mimeType"] != "text/plain" {
		t.Fatalf("manifest mimeType = %v", ent.Properties["mimeType"])
	}

	// GET streams the exact bytes.
	got, raw, err := bs.GetBlob(ctx, info.Digest)
	if err != nil {
		t.Fatalf("get blob: %v", err)
	}
	if !bytes.Equal(raw, data) {
		t.Fatalf("streamed bytes differ: %q", raw)
	}
	if got.MimeType != "text/plain" {
		t.Fatalf("get mimeType = %q", got.MimeType)
	}
}

func TestBlobDedupOnSameBytes(t *testing.T) {
	ctx := context.Background()
	_, ds := newDataset(t)
	bs := blobStoreOf(t, ds)
	data := []byte("same bytes, same blob")

	a, err := bs.PutBlob(ctx, owner, "application/octet-stream", data, "")
	if err != nil {
		t.Fatalf("put a: %v", err)
	}
	before := maxSeq(t, ds)
	b, err := bs.PutBlob(ctx, owner, "application/octet-stream", data, "")
	if err != nil {
		t.Fatalf("put b: %v", err)
	}
	if a.Digest != b.Digest {
		t.Fatalf("dedup broke: %q != %q", a.Digest, b.Digest)
	}
	// A re-store of identical bytes writes no changelog row (no-op suppression).
	if after := maxSeq(t, ds); after != before {
		t.Fatalf("re-storing identical bytes wrote %+v", changesSince(t, ds, before))
	}
}

func TestBlobDigestMismatchRefused(t *testing.T) {
	ctx := context.Background()
	_, ds := newDataset(t)
	bs := blobStoreOf(t, ds)
	_, err := bs.PutBlob(ctx, owner, "text/plain", []byte("hello"),
		substrate.BlobDigestPrefix+"0000000000000000000000000000000000000000000000000000000000000000")
	wantErr(t, err, substrate.ErrValidation, "digest mismatch")
}

func TestBlobRefRendersManifestNotBytes(t *testing.T) {
	ctx := context.Background()
	_, ds := newDataset(t)
	bs := blobStoreOf(t, ds)
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, blobDocDocs("attachment", false)); err != nil {
		t.Fatalf("install doc type: %v", err)
	}
	info, err := bs.PutBlob(ctx, owner, "image/png", []byte("\x89PNG fake bytes"), "")
	if err != nil {
		t.Fatalf("put blob: %v", err)
	}

	doc := mustPut(t, ds, owner, substrate.PutInput{
		Kind:       blobAuthority + "/doc",
		Properties: map[string]any{"attachment": info.Digest},
	})

	got := mustGet(t, ds, doc.Kind, doc.ID)
	m, ok := got.Properties["attachment"].(map[string]any)
	if !ok {
		t.Fatalf("attachment did not resolve to a manifest: %T %v", got.Properties["attachment"], got.Properties["attachment"])
	}
	if m["digest"] != info.Digest {
		t.Fatalf("manifest digest = %v", m["digest"])
	}
	if m["mimeType"] != "image/png" {
		t.Fatalf("manifest mimeType = %v", m["mimeType"])
	}
	if m["status"] != "stored" {
		t.Fatalf("manifest status = %v", m["status"])
	}
	// Never the bytes inline.
	if _, hasBytes := m["bytes"]; hasBytes {
		t.Fatal("manifest leaked bytes")
	}
}

func TestBlobRefUnknownDigestRefused(t *testing.T) {
	ctx := context.Background()
	_, ds := newDataset(t)
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, blobDocDocs("attachment", false)); err != nil {
		t.Fatalf("install doc type: %v", err)
	}
	_, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: blobAuthority + "/doc",
		Properties: map[string]any{
			"attachment": substrate.BlobDigestPrefix + "1111111111111111111111111111111111111111111111111111111111111111",
		},
	})
	wantErr(t, err, substrate.ErrValidation, "unknown blob ref")
}

func TestBlobGetIsRepositoryScoped(t *testing.T) {
	ctx := context.Background()
	svc, ds := newDataset(t)
	bs := blobStoreOf(t, ds)
	info, err := bs.PutBlob(ctx, owner, "text/plain", []byte("repository A secret archive"), "")
	if err != nil {
		t.Fatalf("put blob: %v", err)
	}
	// A second repository cannot read repository A's blob: the digest is simply absent
	// in its schema.
	if _, err := svc.CreateRepository(ctx, "otheruser"); err != nil {
		t.Fatalf("create repository B: %v", err)
	}
	dsB, err := svc.Dataset(ctx, "otheruser")
	if err != nil {
		t.Fatalf("open repository B: %v", err)
	}
	_, _, err = blobStoreOf(t, dsB).GetBlob(ctx, info.Digest)
	wantErr(t, err, substrate.ErrNotFound, "cross-repository blob read")
}

func TestBlobGCCollectsUnreferenced(t *testing.T) {
	// This test asserts immediate collection of a never-referenced orphan, so it
	// disables the unreferenced-upload grace.
	prev := engine.BlobUploadGrace
	engine.BlobUploadGrace = 0
	t.Cleanup(func() { engine.BlobUploadGrace = prev })

	ctx := context.Background()
	_, ds := newDataset(t)
	bs := blobStoreOf(t, ds)
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, blobDocDocs("attachment", false)); err != nil {
		t.Fatalf("install doc type: %v", err)
	}

	kept, err := bs.PutBlob(ctx, owner, "text/plain", []byte("referenced payload"), "")
	if err != nil {
		t.Fatalf("put kept: %v", err)
	}
	orphan, err := bs.PutBlob(ctx, owner, "text/plain", []byte("orphan payload"), "")
	if err != nil {
		t.Fatalf("put orphan: %v", err)
	}
	// Only `kept` is referenced by a live record.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind:       blobAuthority + "/doc",
		Properties: map[string]any{"attachment": kept.Digest},
	})

	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("run gc: %v", err)
	}

	// The referenced blob survives, bytes and manifest.
	if _, _, err := bs.GetBlob(ctx, kept.Digest); err != nil {
		t.Fatalf("referenced blob collected: %v", err)
	}
	if _, err := ds.Get(ctx, "core.substrate.reamde.dev/blob", kept.Digest); err != nil {
		t.Fatalf("referenced manifest collected: %v", err)
	}
	// The orphan's bytes are hard-deleted at once; its manifest is TOMBSTONED,
	// and ordinary record GC hard-deletes the tombstone on a later pass.
	if _, _, err := bs.GetBlob(ctx, orphan.Digest); err == nil {
		t.Fatal("orphan bytes survived gc")
	}
	orphanManifest, err := ds.Get(ctx, "core.substrate.reamde.dev/blob", orphan.Digest)
	if err != nil {
		t.Fatalf("orphan manifest should be tombstoned, not vanished: %v", err)
	}
	if orphanManifest.DeletedAt == nil {
		t.Fatal("orphan manifest was not tombstoned")
	}
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("second gc: %v", err)
	}
	if _, err := ds.Get(ctx, "core.substrate.reamde.dev/blob", orphan.Digest); err == nil {
		t.Fatal("tombstoned orphan manifest survived a later gc")
	}
}
