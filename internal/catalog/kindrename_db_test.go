package catalog_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/authority"
	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A PUBLISHER MAY NOT RENAME A KIND (decision record 0269). Version 1 of a
// published package declares `note`, and a repository installs it and holds a
// note. Version 2 declares `memo` with `movedFrom: …/note`. The refresh holds
// it for review, the upgrade preview blocks it, and the install, which every
// door a published package arrives through runs, refuses it with what to do
// instead. The note stays a note.
func TestARenameIsRefusedAtThePublishersDoor(t *testing.T) {
	ds := newDataset(t)
	ctx := context.Background()
	const id = "publisher.example/notes"
	const reason = "a kind is renamed only by the packages this server ships"
	noteID := vocabulary.DerivedMintedID("rename", "n1")
	dir := t.TempDir()
	docPath := filepath.Join(dir, "authority.json")
	var versions []authority.AuthorityPackageVersion
	kind := func(name, extra string) string {
		return fmt.Sprintf("---\nkind: substrate.reamde.dev/core/kind\nmetadata:\n  id: publisher.example/notes/%s\ndata:\n  authority: publisher.example\n  package: notes\n%s  names:\n    singular: %s\n  properties:\n    name:\n      type: string\n", name, extra, name)
	}
	release := func(v int, installs, kinds string) {
		t.Helper()
		body := fmt.Sprintf("kind: substrate.reamde.dev/core/package\nmetadata:\n  id: %s\ndata:\n  authority: publisher.example\n  package: notes\n  version: %d\n---\nkind: substrate.reamde.dev/core/bundle\nmetadata:\n  id: %s\ndata:\n  authority: publisher.example\n  package: notes\n  description: Notes.\n  installs: [%s]\n%s", id, v, id, installs, kinds)
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

	release(1, "publisher.example/notes/note", kind("note", ""))
	c, err := catalog.LoadWithReader(authority.NewReader(authority.Options{LocalDocuments: map[string]string{"publisher.example": docPath}}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Install(ctx, substrate.ActorAPI, id, ds); err != nil {
		t.Fatalf("install version 1: %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: "publisher.example/notes/note", ID: noteID, Properties: map[string]any{"name": "kept"},
	}); err != nil {
		t.Fatalf("write a note: %v", err)
	}

	release(2, "publisher.example/notes/note, publisher.example/notes/memo",
		kind("note", "")+kind("memo", "  movedFrom: publisher.example/notes/note\n"))

	result, err := c.RefreshPackage(ctx, ds, id)
	if err != nil || result.Outcome != "upgrade-needs-review" {
		t.Fatalf("refresh = %+v %v, want the rename held for review", result, err)
	}
	plan, err := c.Upgrade(ctx, id, ds, nil)
	if err != nil || plan == nil || !strings.Contains(strings.Join(plan.Blockers, "; "), reason) {
		t.Fatalf("the upgrade preview = %+v %v, want it blocked naming the reason", plan, err)
	}
	_, _, err = c.Install(ctx, substrate.ActorAPI, id, ds)
	if err == nil || !strings.Contains(err.Error(), reason) || !strings.Contains(err.Error(), "deprecated: true") {
		t.Fatalf("installing the renaming version = %v, want it refused saying what to do instead", err)
	}

	note, err := ds.Get(ctx, "publisher.example/notes/note", noteID)
	if err != nil || note.Properties["name"] != "kept" {
		t.Fatalf("the note after the refused rename: %+v %v", note, err)
	}
	if _, err := ds.KindByRef(ctx, "publisher.example/notes/memo"); err == nil {
		t.Fatal("the refused version landed its new kind")
	}
	row, err := ds.Get(ctx, "substrate.reamde.dev/core/package", id)
	if err != nil || fmt.Sprint(row.Properties["shippedVersion"]) != "1" {
		t.Fatalf("the package moved to version %v: %v", row.Properties["shippedVersion"], err)
	}
}

// A RENAME IS REFUSED WHERE THE REPOSITORY HOLDS NO OLD KIND. A publisher's
// first release declares `memo` with `movedFrom: …/note`, and this repository
// never held `note`: the engine would have nothing to move and store the key
// inert, but the publisher's package renames a kind all the same, which
// other repositories hold under its name. The install refuses it in the
// engine's words, and nothing lands (decision 0269).
func TestARenameIsRefusedWhereTheRepositoryHoldsNoOldKind(t *testing.T) {
	ds := newDataset(t)
	ctx := context.Background()
	const id = "publisher.example/notes"
	dir := t.TempDir()
	docPath := filepath.Join(dir, "authority.json")
	body := fmt.Sprintf("kind: substrate.reamde.dev/core/package\nmetadata:\n  id: %[1]s\ndata:\n  authority: publisher.example\n  package: notes\n  version: 1\n---\nkind: substrate.reamde.dev/core/bundle\nmetadata:\n  id: %[1]s\ndata:\n  authority: publisher.example\n  package: notes\n  description: Notes.\n  installs: [%[1]s/memo]\n---\nkind: substrate.reamde.dev/core/kind\nmetadata:\n  id: %[1]s/memo\ndata:\n  authority: publisher.example\n  package: notes\n  movedFrom: %[1]s/note\n  names:\n    singular: memo\n  properties:\n    name:\n      type: string\n", id)
	if err := os.WriteFile(filepath.Join(dir, "v1.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(body))
	raw, _ := json.Marshal(authority.AuthorityDocument{Authority: "publisher.example", Packages: []authority.AuthorityPackage{{
		Package:  "notes",
		Versions: []authority.AuthorityPackageVersion{{Version: 1, Closure: "v1.yaml", SHA256: hex.EncodeToString(sum[:])}},
	}}})
	if err := os.WriteFile(docPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := catalog.LoadWithReader(authority.NewReader(authority.Options{LocalDocuments: map[string]string{"publisher.example": docPath}}), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = c.Install(ctx, substrate.ActorAPI, id, ds)
	if err == nil || !strings.Contains(err.Error(), "a kind is renamed only by the packages this server ships") {
		t.Fatalf("installing a release that renames a kind this repository never held = %v, want it refused", err)
	}
	if _, err := ds.KindByRef(ctx, id+"/memo"); err == nil {
		t.Fatal("the refused release landed its kind")
	}
}

// A PUBLISHED KIND STAYS. Version 1 declares `note`; version 2 drops it and
// declares `memo` beside nothing, without movedFrom, and no note was ever
// written here, so no live record would have stopped it before. The refresh
// holds it, the upgrade preview blocks it naming the kind, and the install
// refuses it: a published kind is not the publisher's to remove (decision
// 0269).
func TestARemovedKindIsRefusedAtThePublishersDoor(t *testing.T) {
	ds := newDataset(t)
	ctx := context.Background()
	const id = "publisher.example/notes"
	dir := t.TempDir()
	docPath := filepath.Join(dir, "authority.json")
	var versions []authority.AuthorityPackageVersion
	release := func(v int, kind string) {
		t.Helper()
		body := fmt.Sprintf("kind: substrate.reamde.dev/core/package\nmetadata:\n  id: %[1]s\ndata:\n  authority: publisher.example\n  package: notes\n  version: %[2]d\n---\nkind: substrate.reamde.dev/core/bundle\nmetadata:\n  id: %[1]s\ndata:\n  authority: publisher.example\n  package: notes\n  description: Notes.\n  installs: [%[1]s/%[3]s]\n---\nkind: substrate.reamde.dev/core/kind\nmetadata:\n  id: %[1]s/%[3]s\ndata:\n  authority: publisher.example\n  package: notes\n  names:\n    singular: %[3]s\n  properties:\n    name:\n      type: string\n", id, v, kind)
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
	release(1, "note")
	c, err := catalog.LoadWithReader(authority.NewReader(authority.Options{LocalDocuments: map[string]string{"publisher.example": docPath}}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Install(ctx, substrate.ActorAPI, id, ds); err != nil {
		t.Fatalf("install version 1: %v", err)
	}
	release(2, "memo")

	result, err := c.RefreshPackage(ctx, ds, id)
	if err != nil || result.Outcome != "upgrade-needs-review" {
		t.Fatalf("refresh = %+v %v, want the removal held", result, err)
	}
	plan, err := c.Upgrade(ctx, id, ds, nil)
	if err != nil || plan == nil || !strings.Contains(strings.Join(plan.Blockers, "; "), "removes "+id+"/note") {
		t.Fatalf("the upgrade preview = %+v %v, want it blocked naming the removed kind", plan, err)
	}
	_, _, err = c.Install(ctx, substrate.ActorAPI, id, ds)
	if err == nil || !strings.Contains(err.Error(), "removes "+id+"/note") {
		t.Fatalf("installing the version that drops note = %v, want it refused", err)
	}
	if _, err := ds.KindByRef(ctx, id+"/note"); err != nil {
		t.Fatalf("the published kind went: %v", err)
	}
}
