package engine

// #10 (internal half): a PENDING manifest — a reference created ahead of an
// upload that never arrived — has no byte row, so the old byte-table-only sweep
// left it immortal. The union sweep (manifests ∪ byte rows) collects it once its
// last reference is gone.

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

func TestBlobGCCollectsPendingManifestOrphan(t *testing.T) {
	prev := BlobUploadGrace
	BlobUploadGrace = 0
	t.Cleanup(func() { BlobUploadGrace = prev })

	ctx := context.Background()
	ds := openInternalDataset(t)

	digest := substrate.BlobDigestPrefix + strings.Repeat("b", 64)
	// A pending manifest with no bytes and no referrer (born pending; only the
	// byte store may enter `stored`).
	if _, err := ds.putInternal(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: kindBlob, ID: digest,
		Properties: map[string]any{
			blobPropDigest:    digest,
			blobPropCreatedBy: string(substrate.ActorAPI),
		},
	}); err != nil {
		t.Fatalf("create pending manifest: %v", err)
	}
	// It occupies no byte row, so the old byte-table-only sweep would never see
	// it. The union sweep must collect it: tombstone now, hard-delete later.
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("gc: %v", err)
	}
	var deletedAt sql.NullTime
	err := ds.db.QueryRowContext(ctx, `SELECT deleted_at FROM records WHERE id = $1`, digest).Scan(&deletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		t.Fatal("the pending manifest orphan was never collected — it is immortal")
	}
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if !deletedAt.Valid {
		t.Fatal("the pending manifest orphan survived gc — it is immortal")
	}

	// Ordinary record GC hard-deletes the tombstone on a later pass.
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("second gc: %v", err)
	}
	if err := ds.db.QueryRowContext(ctx, `SELECT 1 FROM records WHERE id = $1`, digest).Scan(new(int)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("the tombstoned pending manifest was not hard-deleted by later GC: %v", err)
	}
}
