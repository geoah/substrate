package catalog

import (
	"context"
	"slices"
	"testing"
	"testing/fstest"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

type addonDataset struct {
	heldBundles
	tokens []substrate.TokenInfo
	actors []substrate.Actor
}

func (d *addonDataset) Tokens(context.Context) ([]substrate.TokenInfo, error) { return d.tokens, nil }

func (d *addonDataset) InstallBundleClosure(_ context.Context, actor substrate.Actor, docs []map[string]any, _ []substrate.PutInput, opts substrate.BundleInstall) ([]*substrate.Record, error) {
	d.actors = append(d.actors, actor)
	for _, doc := range docs {
		if doc["kind"] == "substrate.reamde.dev/core/bundle" {
			metadata := doc["metadata"].(map[string]any)
			d.held[metadata["id"].(string)] = opts.ShippedVersion
		}
	}
	return nil, nil
}

func TestProviderInstallReplansLiveWaitingAddons(t *testing.T) {
	const provider = "example.com/provider"
	const addon = "example.com/addon"
	cat, err := Load(ProviderRoot(fstest.MapFS{
		"provider/bundle.yaml": {Data: []byte(requiringClosure(provider, 1))},
		"addon/bundle.yaml":    {Data: []byte(requiringClosure(addon, 1, provider, 1))},
	}))
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	waiting := []substrate.AppAddon{{Package: addon, Version: 1, AddonFor: provider}}
	ds := &addonDataset{heldBundles: heldBundles{held: map[string]int64{}}, tokens: []substrate.TokenInfo{
		{ID: "expired", ExpiresAt: &past, AppAddons: waiting},
		{ID: "live", Scopes: []string{"read:example.com/addon/*"}, AppAddons: waiting},
	}}
	var planned []string
	blocked := true
	cat.SetAddonPlanner(func(_ context.Context, _ substrate.Dataset, token substrate.TokenInfo, a substrate.AppAddon) *substrate.AuthorizationBlock {
		planned = append(planned, token.ID)
		if a.Package != addon || len(token.Scopes) != 1 {
			t.Fatal("lost approval")
		}
		if blocked {
			return &substrate.AuthorizationBlock{Reason: "reaches-past-scopes", Problem: "The add-on needs broader access."}
		}
		return nil
	})
	result, _, err := cat.Install(context.Background(), substrate.ActorAPI, provider, ds)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(planned, []string{"live"}) || len(result.AddonResults) != 1 || result.AddonResults[0].State != substrate.SetupWait || ds.held[addon] != 0 {
		t.Fatalf("blocked: %+v, %v", result.AddonResults, planned)
	}
	// Revocation removes the token from Tokens; no queued job keeps its grant.
	ds.tokens = nil
	planned = nil
	if _, _, err := cat.Install(context.Background(), substrate.ActorCLI, provider, ds); err != nil {
		t.Fatal(err)
	}
	if len(planned) != 0 || ds.held[addon] != 0 {
		t.Fatal("revoked approval ran")
	}
	ds.tokens = []substrate.TokenInfo{{ID: "live", Scopes: []string{"read:example.com/addon/*"}, AppAddons: waiting}}
	blocked = false
	result, _, err = cat.Install(context.Background(), substrate.ActorConsole, provider, ds)
	if err != nil {
		t.Fatal(err)
	}
	if ds.held[addon] != 1 || result.AddonResults[0].State != "installed" || ds.actors[len(ds.actors)-1] != substrate.BundleActor("example.com", "addon") {
		t.Fatalf("install: %+v %v", result.AddonResults, ds.actors)
	}
}
