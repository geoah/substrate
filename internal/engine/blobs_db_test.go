package engine_test

// The content-addressed blob store: bytes land under a derived
// digest, the blob manifest is an ordinary record, a blob-ref renders the
// manifest and never the bytes, reads are repository-scoped, and an unreferenced
// blob is GC-collectable.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const blobPackage = "attachments.example.substrate.reamde.dev/attachments"

// blobDocType declares a type carrying a blob-ref property (scalar or repeated).
func blobDocDocs(prop string, repeated bool) []map[string]any {
	pdef := map[string]any{"type": "blobref"}
	if repeated {
		pdef["repeated"] = true
	}
	return []map[string]any{
		vocabulary.PackageManifest(blobPackage, 0),
		vocabulary.KindManifest(blobPackage,
			map[string]any{"singular": "doc"},
			map[string]any{"properties": map[string]any{prop: pdef}}),
	}
}

func TestBlobPutStoresMintsAndStreams(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	data := []byte("the untransformed provider payload")

	info, err := ds.PutBlob(ctx, owner, substrate.BlobUpload{MediaType: "text/plain"}, data, "")
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
	ent := mustGet(t, ds, "substrate.reamde.dev/core/blob", info.Digest)
	if ent.Kind != "substrate.reamde.dev/core/blob" {
		t.Fatalf("manifest type = %q", ent.Kind)
	}
	if ent.Properties["status"] != "stored" {
		t.Fatalf("manifest status = %v", ent.Properties["status"])
	}
	if ent.Properties["mediaType"] != "text/plain" {
		t.Fatalf("manifest mediaType = %v", ent.Properties["mediaType"])
	}

	// GET streams the exact bytes.
	got, raw, err := ds.GetBlob(ctx, info.Digest)
	if err != nil {
		t.Fatalf("get blob: %v", err)
	}
	if !bytes.Equal(raw, data) {
		t.Fatalf("streamed bytes differ: %q", raw)
	}
	if got.MediaType != "text/plain" {
		t.Fatalf("get mediaType = %q", got.MediaType)
	}
}

func TestBlobDedupOnSameBytes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	data := []byte("same bytes, same blob")

	a, err := ds.PutBlob(ctx, owner, substrate.BlobUpload{MediaType: "application/octet-stream"}, data, "")
	if err != nil {
		t.Fatalf("put a: %v", err)
	}
	before := maxSeq(t, ds)
	b, err := ds.PutBlob(ctx, owner, substrate.BlobUpload{MediaType: "application/octet-stream"}, data, "")
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
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	_, err := ds.PutBlob(ctx, owner, substrate.BlobUpload{MediaType: "text/plain"}, []byte("hello"),
		substrate.BlobDigestPrefix+"0000000000000000000000000000000000000000000000000000000000000000")
	wantErr(t, err, substrate.ErrValidation, "digest mismatch")
}

func TestBlobRefRendersManifestNotBytes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, blobDocDocs("attachment", false)); err != nil {
		t.Fatalf("install doc type: %v", err)
	}
	info, err := ds.PutBlob(ctx, owner, substrate.BlobUpload{MediaType: "image/png"}, []byte("\x89PNG fake bytes"), "")
	if err != nil {
		t.Fatalf("put blob: %v", err)
	}

	doc := mustPut(t, ds, owner, substrate.PutInput{
		Kind:       blobPackage + "/doc",
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
	if m["mediaType"] != "image/png" {
		t.Fatalf("manifest mediaType = %v", m["mediaType"])
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
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, blobDocDocs("attachment", false)); err != nil {
		t.Fatalf("install doc type: %v", err)
	}
	_, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: blobPackage + "/doc",
		Properties: map[string]any{
			"attachment": substrate.BlobDigestPrefix + "1111111111111111111111111111111111111111111111111111111111111111",
		},
	})
	wantErr(t, err, substrate.ErrValidation, "unknown blob ref")
}

func TestBlobGetIsRepositoryScoped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds := newDataset(t)
	info, err := ds.PutBlob(ctx, owner, substrate.BlobUpload{MediaType: "text/plain"}, []byte("repository A secret archive"), "")
	if err != nil {
		t.Fatalf("put blob: %v", err)
	}
	// A second repository cannot read repository A's blob: the digest is simply absent
	// in its schema.
	if _, err := svc.CreateRepository(ctx, "otheruser.example.com"); err != nil {
		t.Fatalf("create repository B: %v", err)
	}
	dsB, err := svc.Dataset(ctx, "otheruser.example.com")
	if err != nil {
		t.Fatalf("open repository B: %v", err)
	}
	_, _, err = dsB.GetBlob(ctx, info.Digest)
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
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, blobDocDocs("attachment", false)); err != nil {
		t.Fatalf("install doc type: %v", err)
	}

	kept, err := ds.PutBlob(ctx, owner, substrate.BlobUpload{MediaType: "text/plain"}, []byte("referenced payload"), "")
	if err != nil {
		t.Fatalf("put kept: %v", err)
	}
	orphan, err := ds.PutBlob(ctx, owner, substrate.BlobUpload{MediaType: "text/plain"}, []byte("orphan payload"), "")
	if err != nil {
		t.Fatalf("put orphan: %v", err)
	}
	// Only `kept` is referenced by a live record.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind:       blobPackage + "/doc",
		Properties: map[string]any{"attachment": kept.Digest},
	})

	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("run gc: %v", err)
	}

	// The referenced blob survives, bytes and manifest.
	if _, _, err := ds.GetBlob(ctx, kept.Digest); err != nil {
		t.Fatalf("referenced blob collected: %v", err)
	}
	if _, err := ds.Get(ctx, "substrate.reamde.dev/core/blob", kept.Digest); err != nil {
		t.Fatalf("referenced manifest collected: %v", err)
	}
	// The orphan's bytes are hard-deleted at once; its manifest is TOMBSTONED,
	// and ordinary record GC hard-deletes the tombstone on a later pass.
	if _, _, err := ds.GetBlob(ctx, orphan.Digest); err == nil {
		t.Fatal("orphan bytes survived gc")
	}
	orphanManifest, err := ds.Get(ctx, "substrate.reamde.dev/core/blob", orphan.Digest)
	if err != nil {
		t.Fatalf("orphan manifest should be tombstoned, not vanished: %v", err)
	}
	if orphanManifest.DeletedAt == nil {
		t.Fatal("orphan manifest was not tombstoned")
	}
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("second gc: %v", err)
	}
	if _, err := ds.Get(ctx, "substrate.reamde.dev/core/blob", orphan.Digest); err == nil {
		t.Fatal("tombstoned orphan manifest survived a later gc")
	}
}

// A blob carries an optional NAME beside its optional mime type: the manifest
// holds it, a blob-ref resolves it, and the read hands it back. The digest is
// still the identity, so a second upload of the same bytes under a different
// name gets the FIRST name back rather than renaming the blob.
func TestBlobNameIsDescriptiveAndFirstWins(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, blobDocDocs("attachment", false)); err != nil {
		t.Fatalf("install doc type: %v", err)
	}
	data := []byte("%PDF-1.7 fake bytes")

	info, err := ds.PutBlob(ctx, owner,
		substrate.BlobUpload{Name: "invoice.pdf", MediaType: "application/pdf"}, data, "")
	if err != nil {
		t.Fatalf("put blob: %v", err)
	}
	if info.Name != "invoice.pdf" {
		t.Fatalf("name = %q, want invoice.pdf", info.Name)
	}
	ent := mustGet(t, ds, "substrate.reamde.dev/core/blob", info.Digest)
	if ent.Properties["name"] != "invoice.pdf" {
		t.Fatalf("manifest name = %v", ent.Properties["name"])
	}

	// The same bytes under another name are the same blob, still named as the
	// first upload named it — the store never lies about what it holds.
	again, err := ds.PutBlob(ctx, owner,
		substrate.BlobUpload{Name: "copy.pdf", MediaType: "application/pdf"}, data, "")
	if err != nil {
		t.Fatalf("re-put blob: %v", err)
	}
	if again.Digest != info.Digest || again.Name != "invoice.pdf" {
		t.Fatalf("re-put = %+v, want the first blob back", again)
	}

	// A blob-ref resolves the name with the rest of the manifest.
	doc := mustPut(t, ds, owner, substrate.PutInput{
		Kind:       blobPackage + "/doc",
		Properties: map[string]any{"attachment": info.Digest},
	})
	got := mustGet(t, ds, doc.Kind, doc.ID)
	m, ok := got.Properties["attachment"].(map[string]any)
	if !ok {
		t.Fatalf("attachment did not resolve to a manifest: %v", got.Properties["attachment"])
	}
	if m["name"] != "invoice.pdf" {
		t.Fatalf("resolved manifest name = %v", m["name"])
	}

	// And the read says it too.
	read, _, err := ds.GetBlob(ctx, info.Digest)
	if err != nil {
		t.Fatalf("get blob: %v", err)
	}
	if read.Name != "invoice.pdf" {
		t.Fatalf("read name = %q", read.Name)
	}
}

// Both descriptors are OPTIONAL: bytes with neither still store, and the
// manifest says nothing rather than claiming an empty name or type.
func TestBlobNameAndMediaTypeAreOptional(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	info, err := ds.PutBlob(ctx, owner, substrate.BlobUpload{}, []byte("anonymous bytes"), "")
	if err != nil {
		t.Fatalf("put blob: %v", err)
	}
	if info.Name != "" || info.MediaType != "" {
		t.Fatalf("unnamed blob = %+v", info)
	}
	ent := mustGet(t, ds, "substrate.reamde.dev/core/blob", info.Digest)
	if _, ok := ent.Properties["name"]; ok {
		t.Fatalf("manifest claims a name: %v", ent.Properties["name"])
	}
	if _, ok := ent.Properties["mediaType"]; ok {
		t.Fatalf("manifest claims a mime type: %v", ent.Properties["mediaType"])
	}
}

// A name names the blob; it does not address anything. A path separator or a
// control character is refused rather than quietly rewritten, so nothing that
// renders the name can be talked into reading it as a location.
func TestBlobNameRefusesAPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	for _, name := range []string{"../../etc/passwd", `dir\file.pdf`, "line\nbreak.pdf"} {
		_, err := ds.PutBlob(ctx, owner, substrate.BlobUpload{Name: name}, []byte("payload "+name), "")
		wantErr(t, err, substrate.ErrValidation, "blob name")
	}
}

// A blob-ref reads as its manifest and stores as the digest, so the read shape
// applied back must land as a no-op and stay a string in storage: the GC's
// referencedDigests selects `jsonb_typeof = 'string'`, and an object stored
// there would let it collect a referenced blob. The manifest's own keys are not
// writable through the property: a wrong mediaType in the object changes
// nothing.
func TestBlobRefReadShapeAppliesBackUnchanged(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		prop     string
		repeated bool
	}{
		{"single", "attachment", false},
		{"repeated", "attachments", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			_, ds, dsn := newDatasetWithDSN(t)
			if _, err := ds.ApplyVocabularyDocuments(ctx, owner, blobDocDocs(tc.prop, tc.repeated)); err != nil {
				t.Fatalf("install doc kind: %v", err)
			}
			a, err := ds.PutBlob(ctx, owner, substrate.BlobUpload{MediaType: "image/png", Name: "a.png"}, []byte("first"), "")
			if err != nil {
				t.Fatalf("put blob a: %v", err)
			}
			b, err := ds.PutBlob(ctx, owner, substrate.BlobUpload{MediaType: "image/png", Name: "b.png"}, []byte("second"), "")
			if err != nil {
				t.Fatalf("put blob b: %v", err)
			}
			// The read shape, with the descriptive keys deliberately wrong:
			// only the digest is the reference.
			readShape := func(digest string) map[string]any {
				return map[string]any{
					"digest": digest, "name": "renamed.txt", "mediaType": "text/plain",
					"size": json.Number("1"), "status": "pending",
				}
			}
			var written any = readShape(a.Digest)
			if tc.repeated {
				written = []any{readShape(a.Digest), b.Digest}
			}
			doc := mustPut(t, ds, owner, substrate.PutInput{
				Kind:       blobPackage + "/doc",
				Properties: map[string]any{tc.prop: written},
			})

			read := mustGet(t, ds, doc.Kind, doc.ID)
			var manifests []map[string]any
			if tc.repeated {
				for _, item := range read.Properties[tc.prop].([]any) {
					manifests = append(manifests, item.(map[string]any))
				}
			} else {
				manifests = append(manifests, read.Properties[tc.prop].(map[string]any))
			}
			if manifests[0]["digest"] != a.Digest || manifests[0]["name"] != "a.png" ||
				manifests[0]["mediaType"] != "image/png" || manifests[0]["status"] != "stored" {
				t.Fatalf("the read did not resolve the stored manifest: %#v", manifests[0])
			}
			if tc.repeated && manifests[1]["digest"] != b.Digest {
				t.Fatalf("second manifest = %#v", manifests[1])
			}

			// The read applied back verbatim is a no-op, and reads the same.
			again := mustPut(t, ds, owner, substrate.PutInput{
				Kind: doc.Kind, ID: doc.ID, Properties: read.Properties,
			})
			if again.Version != doc.Version {
				t.Fatalf("re-applying the read shape moved the version %d -> %d", doc.Version, again.Version)
			}
			final := mustGet(t, ds, doc.Kind, doc.ID)
			if !reflect.DeepEqual(final.Properties[tc.prop], read.Properties[tc.prop]) {
				t.Fatalf("the read changed across get | put | get:\n%#v\n%#v", read.Properties[tc.prop], final.Properties[tc.prop])
			}

			// Storage holds the digest string, never the object.
			var stored string
			q := `SELECT jsonb_typeof(props -> $1) FROM records WHERE id = $2`
			if tc.repeated {
				q = `SELECT string_agg(DISTINCT jsonb_typeof(e), ',') FROM records, jsonb_array_elements(props -> $1) e WHERE id = $2`
			}
			if err := rawDB(t, dsn).QueryRow(q, tc.prop, doc.ID).Scan(&stored); err != nil {
				t.Fatalf("read stored shape: %v", err)
			}
			if stored != "string" {
				t.Fatalf("stored jsonb type = %q, want string", stored)
			}
		})
	}
}

// A write validates a blob-ref while its record is still uncommitted; a GC
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
	ds, raw, _ := newDatasetWithDB(t)
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, blobDocDocs("attachment", false)); err != nil {
		t.Fatalf("install doc type: %v", err)
	}
	bs := ds
	blob, err := bs.PutBlob(ctx, owner, substrate.BlobUpload{MediaType: "text/plain"}, []byte("racy payload"), "")
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
		`SELECT pg_advisory_xact_lock_shared(`+engine.AdvisoryKeySQL+`)`, ds.Repository().ID+"|blob|"+blob.Digest); err != nil {
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
			Kind: blobPackage + "/doc", ID: "racedoc",
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

// A freshly uploaded, not-yet-referenced blob must survive GC long enough
// to become referenced — the unreferenced-upload grace. Past the grace the same
// orphan is collectable.
func TestBlobUploadGraceSparesFreshBlob(t *testing.T) {
	prev := engine.BlobUploadGrace
	t.Cleanup(func() { engine.BlobUploadGrace = prev })

	ctx := context.Background()
	_, ds := newDataset(t)
	bs := ds

	engine.BlobUploadGrace = time.Hour
	blob, err := bs.PutBlob(ctx, owner, substrate.BlobUpload{MediaType: "text/plain"}, []byte("fresh, not yet referenced"), "")
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

// The generic record API may not forge a blob manifest, and a dedup PUT
// returns the FIRST writer's authoritative mime, not the second request's claim.
func TestBlobManifestForgeRefusedAndDedupAuthoritative(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	bs := ds

	forged := substrate.BlobDigestPrefix + strings.Repeat("2", 64)
	_, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/blob", ID: forged,
		Properties: map[string]any{
			"digest": forged, "size": 99999,
			"mediaType": "text/plain", "status": "stored",
		},
	})
	wantErr(t, err, substrate.ErrForbidden, "a forged generic blob manifest must be refused")

	data := []byte("same bytes, different claimed mime")
	first, err := bs.PutBlob(ctx, owner, substrate.BlobUpload{MediaType: "text/plain"}, data, "")
	if err != nil {
		t.Fatalf("put first: %v", err)
	}
	second, err := bs.PutBlob(ctx, owner, substrate.BlobUpload{MediaType: "image/png"}, data, "")
	if err != nil {
		t.Fatalf("put second (dedup): %v", err)
	}
	if second.Digest != first.Digest {
		t.Fatalf("dedup broke: %q != %q", second.Digest, first.Digest)
	}
	if second.MediaType != "text/plain" {
		t.Fatalf("dedup PUT returned %q, want the authoritative text/plain", second.MediaType)
	}
	got, _, err := bs.GetBlob(ctx, first.Digest)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.MediaType != "text/plain" {
		t.Fatalf("GET mime = %q, want text/plain", got.MediaType)
	}
}

// An unreferenced STORED blob's manifest is TOMBSTONED by the blob sweep
// (not hard-deleted), so a reader can observe the promised tombstone; ordinary
// record GC hard-deletes that tombstone on a later pass.
func TestBlobGCTombstonesStoredManifest(t *testing.T) {
	prev := engine.BlobUploadGrace
	engine.BlobUploadGrace = 0
	t.Cleanup(func() { engine.BlobUploadGrace = prev })

	ctx := context.Background()
	ds, raw, _ := newDatasetWithDB(t)
	bs := ds
	blob, err := bs.PutBlob(ctx, owner, substrate.BlobUpload{MediaType: "text/plain"}, []byte("orphan to tombstone"), "")
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
