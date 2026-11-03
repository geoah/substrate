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

func TestCompletedAddonClearsOnlyItsApprovingTokensWaitingEntry(t *testing.T) {
	ds := openInternalDataset(t)
	ctx := context.Background()
	waiting := []substrate.AppAddon{
		{Package: "app.example/first", Version: 1, AddonFor: "provider.example/data"},
		{Package: "app.example/second", Version: 1, AddonFor: "provider.example/data"},
	}
	mint := func() substrate.TokenInfo {
		t.Helper()
		token, _, err := ds.MintToken(ctx, substrate.MintTokenInput{Label: "App", ClientID: "https://app.example/client.json", AppAddons: waiting})
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	token, other := mint(), mint()
	if err := ds.CompleteAppAddon(ctx, token.ID, waiting[0].Package); err != nil {
		t.Fatal(err)
	}
	tokens, err := ds.Tokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range tokens {
		if got.ID == token.ID && (len(got.AppAddons) != 1 || got.AppAddons[0] != waiting[1]) {
			t.Fatalf("completed token: %+v", got)
		}
		if got.ID == other.ID && len(got.AppAddons) != 2 {
			t.Fatalf("another approval changed: %+v", got)
		}
	}
	before, err := ds.Get(ctx, kindToken, token.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := ds.CompleteAppAddon(ctx, token.ID, waiting[0].Package); err != nil {
		t.Fatal(err)
	}
	after, err := ds.Get(ctx, kindToken, token.ID)
	if err != nil || after.Version != before.Version {
		t.Fatalf("repeated completion wrote: %+v %v", after, err)
	}
	if err := ds.CompleteAppAddon(ctx, token.ID, waiting[1].Package); err != nil {
		t.Fatal(err)
	}
	tokens, err = ds.Tokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range tokens {
		if got.ID == token.ID && len(got.AppAddons) != 0 {
			t.Fatalf("last waiting entry remains: %+v", got)
		}
	}
}
