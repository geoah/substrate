package catalog_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/authority"
	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
)

func TestPublishedInstallRefreshTakeover(t *testing.T) {
	ds := newDataset(t)
	ctx := context.Background()
	const id = "publisher.example/notes"
	dir := t.TempDir()
	docPath := filepath.Join(dir, "authority.json")
	var versions []authority.AuthorityPackageVersion
	release := func(v int, extra string) {
		t.Helper()
		body := fmt.Sprintf("kind: substrate.reamde.dev/core/package\nmetadata:\n  id: %s\ndata:\n  authority: publisher.example\n  package: notes\n  version: %d\n---\nkind: substrate.reamde.dev/core/bundle\nmetadata:\n  id: %s\ndata:\n  authority: publisher.example\n  package: notes\n  description: Notes.\n  installs: [publisher.example/notes/note]\n---\nkind: substrate.reamde.dev/core/kind\nmetadata:\n  id: publisher.example/notes/note\ndata:\n  authority: publisher.example\n  package: notes\n  names:\n    singular: note\n  properties:\n    name:\n      type: string\n%s", id, v, id, extra)
		if v == 3 {
			body = strings.Replace(body, "    name:\n", "    heading:\n", 1)
		}
		file := fmt.Sprintf("v%d.yaml", v)
		if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(body))
		versions = append(versions, authority.AuthorityPackageVersion{Version: v, Closure: file, SHA256: hex.EncodeToString(sum[:])})
		raw, _ := json.Marshal(authority.AuthorityDocument{Authority: "publisher.example", Packages: []authority.AuthorityPackage{{Package: "notes", Versions: versions}}})
		if err := os.WriteFile(docPath, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	release(1, "")
	c, err := catalog.LoadWithReader(authority.NewReader(authority.Options{LocalDocuments: map[string]string{"publisher.example": docPath}}))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Install(ctx, substrate.ActorAPI, id, ds); err != nil {
		t.Fatal(err)
	}
	app, _, err := ds.MintToken(ctx, substrate.MintTokenInput{Label: "Notes", ClientID: "https://app.example/client.json", ClientName: "Notes", AppPackages: map[string]int64{id: 1}, AppSoftwareVersion: "1.0", AppAddons: []substrate.AppAddon{{Package: "publisher.example/addon", Version: 1, AddonFor: id}}})
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := ds.MintToken(ctx, substrate.MintTokenInput{Label: "Other", ClientID: "https://other.example/client.json", AppPackages: map[string]int64{id: 1}})
	if err != nil {
		t.Fatal(err)
	}
	release(2, "    summary:\n      type: string\n")
	release(3, "")

	result, err := c.RefreshPackage(ctx, ds, id)
	if err != nil || result.Outcome != "upgraded" {
		t.Fatalf("refresh: %+v %v", result, err)
	}

	if err := ds.RecordAppBuild(substrate.WithPrincipal(ctx, app.ID), map[string]int64{id: 2}, "2.0"); err != nil {
		t.Fatal(err)
	}
	tokens, err := ds.Tokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range tokens {
		if token.ID == app.ID && (token.AppPackages[id] != 2 || token.AppSoftwareVersion != "2.0" || token.AppCheckedAt == nil || len(token.AppAddons) != 1) {
			t.Fatalf("build not recorded: %+v", token)
		}
		if token.ID == other.ID && token.AppPackages[id] != 1 {
			t.Fatal("another token changed")
		}
	}
	plan, err := c.Upgrade(ctx, id, ds, nil)
	if err != nil || len(plan.Holders) != 2 || !plan.ReviewRequired {
		t.Fatalf("narrowing: %+v %v", plan, err)
	}
	if _, _, err := c.Install(ctx, substrate.ActorAPI, id, ds); !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("held narrowing: %v", err)
	}
	row, err := ds.Get(ctx, "substrate.reamde.dev/core/package", id)
	if err != nil || row.Properties["publishedFrom"] == nil {
		t.Fatalf("provenance: %+v %v", row, err)
	}
	// A same-version rewrite and a lower version both fail inside admission.
	versions = nil
	release(2, "")
	if _, _, err := c.Install(ctx, substrate.ActorAPI, id, ds); !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("mutable release: %v", err)
	}
	versions = nil
	release(1, "")
	if _, _, err := c.Install(ctx, substrate.ActorAPI, id, ds); !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("downgrade: %v", err)
	}
	if err := os.Remove(docPath); err != nil {
		t.Fatal(err)
	}
	if _, err := c.RefreshPackage(ctx, ds, id); err == nil {
		t.Fatal("lapsed publisher was not reported")
	}
	if _, err := ds.Get(ctx, "substrate.reamde.dev/core/package", id); err != nil {
		t.Fatal("lapsed publisher removed package")
	}
	if err := ds.RecordAppBuild(ctx, map[string]int64{id: 9}, "forged"); !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("missing principal: %v", err)
	}
	_, err = ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: "substrate.reamde.dev/core/token", ID: app.ID, Properties: map[string]any{"appPackages": map[string]any{id: 9}}})
	if err == nil {
		t.Fatal("generic put forged build facts")
	}
	taken, err := ds.TakeOverPackage(ctx, substrate.ActorAPI, id, id)
	if err != nil || taken.Properties["source"] != "installed" || taken.Properties["takenOverFrom"] != "publisher.example" || taken.Properties["publishedFrom"] != nil {
		t.Fatalf("takeover: %+v %v", taken, err)
	}
}
