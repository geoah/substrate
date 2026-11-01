package engine_test

// An app's token stores blobs when it writes a kind that declares a blob-ref
// (docs/apps.md): the API admits the upload and hands the engine the
// token's scopes on the context and the app's own actor, and the engine
// stores the bytes and their manifest under that actor as it would the
// owner's.

import (
	"bytes"
	"context"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

func TestABlobStoresUnderAnAppsActor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newCoreDataset(t)
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, blobDocDocs("images", true)); err != nil {
		t.Fatalf("declare the doc kind: %v", err)
	}
	ownerBytes := []byte("the owner's scan")
	first, err := ds.PutBlob(ctx, owner, substrate.BlobUpload{Name: "scan.png", MediaType: "image/png"}, ownerBytes, "")
	if err != nil {
		t.Fatalf("the owner's upload: %v", err)
	}
	app := substrate.WithScopes(ctx, []string{"write:" + blobPackage + "/*"})
	actor := substrate.AppActor("notes.example.org")

	data := []byte("an image the app shared")
	before := maxSeq(t, ds)
	info, err := ds.PutBlob(app, actor, substrate.BlobUpload{Name: "shared.png", MediaType: "image/png"}, data, "")
	if err != nil {
		t.Fatalf("the app's upload: %v", err)
	}
	if info.Status != substrate.BlobStored || info.Name != "shared.png" || info.Size != int64(len(data)) {
		t.Fatalf("the app's upload = %+v", info)
	}
	rows := changesSince(t, ds, before)
	if len(rows) == 0 {
		t.Fatal("the app's upload wrote no manifest")
	}
	for _, row := range rows {
		if row.Kind != "substrate.reamde.dev/core/blob" || row.RecordID != info.Digest || row.Actor != actor {
			t.Fatalf("the app's upload wrote %s/%s as %q, want the manifest as %s", row.Kind, row.RecordID, row.Actor, actor)
		}
	}
	if _, got, err := ds.GetBlob(ctx, info.Digest); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("reading the app's blob back: %v, %q", err, got)
	}

	// A record the app writes points at it, and its read resolves the
	// manifest.
	doc, err := ds.Put(app, actor, substrate.PutInput{
		Kind: blobPackage + "/doc", Properties: map[string]any{"images": []any{info.Digest}},
	})
	if err != nil {
		t.Fatalf("the app's record naming its blob: %v", err)
	}
	images, _ := mustGet(t, ds, doc.Kind, doc.ID).Properties["images"].([]any)
	if m, _ := firstOf(images).(map[string]any); m["digest"] != info.Digest || m["name"] != "shared.png" {
		t.Fatalf("the record's images read %v", images)
	}

	// Bytes the owner stored first are the owner's blob still: the app's
	// upload writes nothing, and the engine answers what the store holds, so
	// the API is the layer that answers an app with what it sent.
	before = maxSeq(t, ds)
	again, err := ds.PutBlob(app, actor, substrate.BlobUpload{Name: "mine.png", MediaType: "image/png"}, ownerBytes, "")
	if err != nil {
		t.Fatalf("the app's upload of the owner's bytes: %v", err)
	}
	if again.Digest != first.Digest || again.Name != "scan.png" {
		t.Fatalf("the app's upload of the owner's bytes = %+v", again)
	}
	if after := maxSeq(t, ds); after != before {
		t.Fatalf("the app's upload of the owner's bytes wrote %+v", changesSince(t, ds, before))
	}
	if m := mustGet(t, ds, "substrate.reamde.dev/core/blob", first.Digest); m.Properties["name"] != "scan.png" {
		t.Fatalf("the owner's manifest is named %v", m.Properties["name"])
	}
}

func firstOf(items []any) any {
	if len(items) == 0 {
		return nil
	}
	return items[0]
}
