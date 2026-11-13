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
	c, err := catalog.LoadWithReader(authority.NewReader(authority.Options{LocalDocuments: map[string]string{"publisher.example": docPath}}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Install(ctx, substrate.ActorPerson, id, ds); err != nil {
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
	if _, _, err := c.Install(ctx, substrate.ActorPerson, id, ds); !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("held narrowing: %v", err)
	}
	row, err := ds.Get(ctx, "substrate.reamde.dev/core/package", id)
	if err != nil || row.Properties["publishedFrom"] == nil {
		t.Fatalf("provenance: %+v %v", row, err)
	}
	// A same-version rewrite and a lower version both fail inside admission.
	versions = nil
	release(2, "")
	if _, _, err := c.Install(ctx, substrate.ActorPerson, id, ds); !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("mutable release: %v", err)
	}
	versions = nil
	release(1, "")
	if _, _, err := c.Install(ctx, substrate.ActorPerson, id, ds); !errors.Is(err, substrate.ErrConflict) {
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
	_, err = ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: "substrate.reamde.dev/core/token", ID: app.ID, Properties: map[string]any{"appPackages": map[string]any{id: 9}}})
	if err == nil {
		t.Fatal("generic put forged build facts")
	}
	taken, err := ds.TakeOverPackage(ctx, substrate.ActorPerson, id, id)
	if err != nil || taken.Properties["source"] != "installed" || taken.Properties["takenOverFrom"] != "publisher.example" || taken.Properties["publishedFrom"] != nil {
		t.Fatalf("takeover: %+v %v", taken, err)
	}
}

// A package's document that moved (the record names another URL than the
// one the package was installed from) holds every upgrade for the owner:
// a publisher that moved its files and a DNS answer that should not be
// followed unseen read the same, so nothing upgrades by itself.
func TestARefreshDoesNotFollowADocumentThatMoved(t *testing.T) {
	ds := newDataset(t)
	ctx := context.Background()
	const id = "publisher.example/notes"
	closure := func(v int) string {
		return fmt.Sprintf("kind: substrate.reamde.dev/core/package\nmetadata:\n  id: %s\ndata:\n  authority: publisher.example\n  package: notes\n  version: %d\n---\nkind: substrate.reamde.dev/core/bundle\nmetadata:\n  id: %s\ndata:\n  authority: publisher.example\n  package: notes\n  description: Notes.\n  installs: [publisher.example/notes/note]\n---\nkind: substrate.reamde.dev/core/kind\nmetadata:\n  id: publisher.example/notes/note\ndata:\n  authority: publisher.example\n  package: notes\n  names:\n    singular: note\n  properties:\n    name:\n      type: string\n", id, v, id)
	}
	document := func(dir string, versions ...int) string {
		t.Helper()
		var listed []authority.AuthorityPackageVersion
		for _, v := range versions {
			file := fmt.Sprintf("v%d.yaml", v)
			body := closure(v)
			if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256([]byte(body))
			listed = append(listed, authority.AuthorityPackageVersion{Version: v, Closure: file, SHA256: hex.EncodeToString(sum[:])})
		}
		raw, _ := json.Marshal(authority.AuthorityDocument{Authority: "publisher.example", Packages: []authority.AuthorityPackage{{Package: "notes", Versions: listed}}})
		path := filepath.Join(dir, "authority.json")
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	docs := map[string]string{"publisher.example": document(t.TempDir(), 1)}
	c, err := catalog.LoadWithReader(authority.NewReader(authority.Options{LocalDocuments: docs}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Install(ctx, substrate.ActorPerson, id, ds); err != nil {
		t.Fatal(err)
	}
	docs["publisher.example"] = document(t.TempDir(), 1, 2)
	got, err := c.RefreshPackage(ctx, ds, id)
	if err != nil || got.Outcome != "upgrade-needs-review" || !strings.Contains(got.Reason, "moved") {
		t.Fatalf("refresh after the document moved = %+v, %v; want it held for review", got, err)
	}
	row, err := ds.Get(ctx, "substrate.reamde.dev/core/package", id)
	if err != nil || fmt.Sprint(row.Properties["shippedVersion"]) != "1" {
		t.Fatalf("the moved document upgraded the package: %v, %v", row.Properties["shippedVersion"], err)
	}
}

// A package a later binary ships is never upgraded from the document it was
// installed from: the binary wins on a refresh as on an install.
func TestARefreshLeavesAPackageTheBinaryNowShipsToTheCatalog(t *testing.T) {
	ds := newDataset(t)
	ctx := context.Background()
	const id = "publisher.example/notes"
	closure := func(v int) string {
		return fmt.Sprintf("kind: substrate.reamde.dev/core/package\nmetadata:\n  id: %s\ndata:\n  authority: publisher.example\n  package: notes\n  version: %d\n---\nkind: substrate.reamde.dev/core/bundle\nmetadata:\n  id: %s\ndata:\n  authority: publisher.example\n  package: notes\n  description: Notes.\n  installs: [publisher.example/notes/note]\n---\nkind: substrate.reamde.dev/core/kind\nmetadata:\n  id: publisher.example/notes/note\ndata:\n  authority: publisher.example\n  package: notes\n  names:\n    singular: note\n  properties:\n    name:\n      type: string\n", id, v, id)
	}
	dir := t.TempDir()
	var listed []authority.AuthorityPackageVersion
	for _, v := range []int{1, 2} {
		file, body := fmt.Sprintf("v%d.yaml", v), closure(v)
		if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(body))
		listed = append(listed, authority.AuthorityPackageVersion{Version: v, Closure: file, SHA256: hex.EncodeToString(sum[:])})
	}
	install := authority.AuthorityDocument{Authority: "publisher.example", Packages: []authority.AuthorityPackage{{Package: "notes", Versions: listed[:1]}}}
	raw, _ := json.Marshal(install)
	path := filepath.Join(dir, "authority.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	docs := map[string]string{"publisher.example": path}
	c, err := catalog.LoadWithReader(authority.NewReader(authority.Options{LocalDocuments: docs}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Install(ctx, substrate.ActorPerson, id, ds); err != nil {
		t.Fatal(err)
	}
	install.Packages[0].Versions = listed
	raw, _ = json.Marshal(install)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	later, err := catalog.LoadWithReader(authority.NewReader(authority.Options{LocalDocuments: docs}), []string{id})
	if err != nil {
		t.Fatal(err)
	}
	got, err := later.RefreshPackage(ctx, ds, id)
	if err != nil || got.Outcome != "upgrade-needs-review" || !strings.Contains(got.Reason, "ships with this server") {
		t.Fatalf("refresh of a package the binary now ships = %+v, %v; want it left to the catalog", got, err)
	}
	row, err := ds.Get(ctx, "substrate.reamde.dev/core/package", id)
	if err != nil || fmt.Sprint(row.Properties["shippedVersion"]) != "1" {
		t.Fatalf("the document upgraded a package the binary ships: %v, %v", row.Properties["shippedVersion"], err)
	}
}

// The catalog loads a published closure through the one loader, so a
// `writer:` from before actor types refuses the install, naming the type
// that replaced it (decision 0290).
func TestAnInstallRefusesARetiredWriterNamingItsReplacement(t *testing.T) {
	for old, instead := range map[string]string{
		"owner":     "write person",
		"connector": "write [function, agent]",
		"oauth":     "write system",
	} {
		t.Run(old, func(t *testing.T) {
			ds := newDataset(t)
			ctx := context.Background()
			const id = "publisher.example/notes"
			dir := t.TempDir()
			body := fmt.Sprintf("kind: substrate.reamde.dev/core/package\nmetadata:\n  id: %s\ndata:\n  authority: publisher.example\n  package: notes\n  version: 1\n---\nkind: substrate.reamde.dev/core/bundle\nmetadata:\n  id: %s\ndata:\n  authority: publisher.example\n  package: notes\n  description: Notes.\n  installs: [publisher.example/notes/note]\n---\nkind: substrate.reamde.dev/core/kind\nmetadata:\n  id: publisher.example/notes/note\ndata:\n  authority: publisher.example\n  package: notes\n  names:\n    singular: note\n  properties:\n    name:\n      type: string\n    cursor:\n      type: string\n      writer: %s\n", id, id, old)
			if err := os.WriteFile(filepath.Join(dir, "v1.yaml"), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256([]byte(body))
			raw, _ := json.Marshal(authority.AuthorityDocument{Authority: "publisher.example", Packages: []authority.AuthorityPackage{{Package: "notes", Versions: []authority.AuthorityPackageVersion{{Version: 1, Closure: "v1.yaml", SHA256: hex.EncodeToString(sum[:])}}}}})
			docPath := filepath.Join(dir, "authority.json")
			if err := os.WriteFile(docPath, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			c, err := catalog.LoadWithReader(authority.NewReader(authority.Options{LocalDocuments: map[string]string{"publisher.example": docPath}}), nil)
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = c.Install(ctx, substrate.ActorPerson, id, ds)
			if err == nil {
				t.Fatalf("the catalog installed writer: %s", old)
			}
			if want := `writer "` + old + `" is retired: ` + instead; !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal does not say %q: %v", want, err)
			}
		})
	}
}

// A retention is compared by what each kind keeps, not by its spelling or by
// where it is stated (decision 0282). The automatic refresh leaves a release
// that keeps a kind's history for less time to the owner, its preview naming
// each kind, and takes one that keeps the same or longer: a header gaining a
// default shortens every kind that states none, a respelling changes
// nothing, a header losing its default lengthens, and a kind gaining its own
// shorter key shortens that kind.
func TestARefreshLeavesAShorterRetentionToTheOwner(t *testing.T) {
	ds := newDataset(t)
	ctx := context.Background()
	const id = "publisher.example/journal"
	dir := t.TempDir()
	docPath := filepath.Join(dir, "authority.json")
	var versions []authority.AuthorityPackageVersion
	release := func(v int, header, kind string) {
		t.Helper()
		body := fmt.Sprintf("kind: substrate.reamde.dev/core/package\nmetadata:\n  id: %s\ndata:\n  authority: publisher.example\n  package: journal\n%s  version: %d\n---\nkind: substrate.reamde.dev/core/bundle\nmetadata:\n  id: %s\ndata:\n  authority: publisher.example\n  package: journal\n  description: A journal.\n  installs: [publisher.example/journal/entry]\n---\nkind: substrate.reamde.dev/core/kind\nmetadata:\n  id: publisher.example/journal/entry\ndata:\n  authority: publisher.example\n  package: journal\n%s  names:\n    singular: entry\n  properties:\n    name:\n      type: string\n", id, header, v, id, kind)
		file := fmt.Sprintf("v%d.yaml", v)
		if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(body))
		versions = append(versions, authority.AuthorityPackageVersion{Version: v, Closure: file, SHA256: hex.EncodeToString(sum[:])})
		raw, _ := json.Marshal(authority.AuthorityDocument{Authority: "publisher.example", Packages: []authority.AuthorityPackage{{Package: "journal", Versions: versions}}})
		if err := os.WriteFile(docPath, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	release(1, "", "")
	c, err := catalog.LoadWithReader(authority.NewReader(authority.Options{LocalDocuments: map[string]string{"publisher.example": docPath}}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Install(ctx, substrate.ActorPerson, id, ds); err != nil {
		t.Fatal(err)
	}
	held := func() string {
		t.Helper()
		row, err := ds.Get(ctx, "substrate.reamde.dev/core/package", id)
		if err != nil {
			t.Fatal(err)
		}
		return fmt.Sprint(row.Properties["shippedVersion"])
	}
	refresh := func(want string) catalog.RefreshResult {
		t.Helper()
		got, err := c.RefreshPackage(ctx, ds, id)
		if err != nil || got.Outcome != want {
			t.Fatalf("refresh = %+v, %v, want %s", got, err, want)
		}
		return got
	}
	const entry = "publisher.example/journal/entry"

	// A header gaining a default: the entry kept all and would keep 30 days.
	release(2, "  retention: P30D\n", "")
	if got := refresh("upgrade-needs-review"); !strings.Contains(got.Reason, "for less time") || !strings.Contains(got.Reason, entry) {
		t.Fatalf("the refresh's reason does not name the shortening: %q", got.Reason)
	}
	if v := held(); v != "1" {
		t.Fatalf("the shorter release landed by itself: version %s", v)
	}
	plan, err := c.Upgrade(ctx, id, ds, nil)
	if err != nil || !plan.ReviewRequired || len(plan.RetentionShortens) != 1 ||
		plan.RetentionShortens[0] != (substrate.RetentionChange{Kind: entry, From: "all", To: "P30D"}) {
		t.Fatalf("the preview = %+v, %v, want the entry from all to P30D under review", plan, err)
	}
	if _, _, err := c.Install(ctx, substrate.ActorPerson, id, ds); err != nil {
		t.Fatalf("the owner takes the release: %v", err)
	}
	// A respelling keeps the same 30 days; a header losing its default keeps
	// all: both land by themselves.
	release(3, "  retention: PT720H\n", "")
	refresh("upgraded")
	release(4, "", "")
	refresh("upgraded")
	if v := held(); v != "4" {
		t.Fatalf("held version %s after the safe releases, want 4", v)
	}
	// A kind gaining its own shorter key.
	release(5, "", "  retention: P7D\n")
	refresh("upgrade-needs-review")
	if v := held(); v != "4" {
		t.Fatalf("the kind's shorter key landed by itself: version %s", v)
	}
}
