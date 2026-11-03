package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func TestAddonInstallRequiresLiveConsentInTransaction(t *testing.T) {
	ds := openInternalDataset(t)
	ctx := context.Background()
	const pkg = "consent.example.com/addon"
	docs := []map[string]any{vocabulary.PackageManifest(pkg, 1), vocabulary.BundleManifest(pkg, map[string]any{"description": "Add-on"})}
	token, _, err := ds.MintToken(ctx, substrate.MintTokenInput{Label: "Approval"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.Delete(ctx, substrate.ActorConsole, kindToken, token.ID, substrate.DeleteInput{}); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	expired, _, err := ds.MintToken(ctx, substrate.MintTokenInput{Label: "Expired", ExpiresAt: &past})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"missing", token.ID, expired.ID} {
		if _, err := ds.InstallBundleClosure(ctx, substrate.ActorConsole, docs, nil, substrate.BundleInstall{Published: true, ApprovalTokenID: id}); !errors.Is(err, substrate.ErrForbidden) {
			t.Fatalf("consent %s: %v", id, err)
		}
		if _, err := ds.Get(ctx, kindPackage, pkg); !errors.Is(err, substrate.ErrNotFound) {
			t.Fatalf("refused install left package: %v", err)
		}
		if _, ok := ds.registry().PackageByName(pkg); ok {
			t.Fatal("refused install published registry")
		}
	}
	live, _, err := ds.MintToken(ctx, substrate.MintTokenInput{Label: "Live"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.InstallBundleClosure(ctx, substrate.ActorConsole, docs, nil, substrate.BundleInstall{Published: true, ApprovalTokenID: live.ID}); err != nil {
		t.Fatal(err)
	}
}
