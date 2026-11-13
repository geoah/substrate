package catalog_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/geoah/substrate/internal/authority"
	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// AN OLDER SERVER INSTALLS THE LAST VERSION IT UNDERSTANDS (decision record
// 0268). The publisher lists version 1, written for this server's dialect,
// and version 2, written for the next one: the install lands version 1, a
// refresh passes version 2 over, and version 3, written for this server's
// dialect again, is the upgrade a refresh takes. Version 4 states the next
// dialect in its header alone, and a refresh passes it over too.
func TestAnOlderServerInstallsTheLastVersionItUnderstands(t *testing.T) {
	ds := newDataset(t)
	ctx := context.Background()
	const id = "publisher.example/notes"
	dir := t.TempDir()
	docPath := filepath.Join(dir, "authority.json")
	var versions []authority.AuthorityPackageVersion
	// release lists version v, its header stating dialect; listed says
	// whether the document lists it too.
	release := func(v int, dialect int64, listed bool) {
		t.Helper()
		body := fmt.Sprintf("kind: substrate.reamde.dev/core/package\nmetadata:\n  id: %s\ndata:\n  authority: publisher.example\n  package: notes\n  version: %d\n  dialect: %d\n---\nkind: substrate.reamde.dev/core/bundle\nmetadata:\n  id: %s\ndata:\n  authority: publisher.example\n  package: notes\n  description: Notes.\n  installs: [publisher.example/notes/note]\n---\nkind: substrate.reamde.dev/core/kind\nmetadata:\n  id: publisher.example/notes/note\ndata:\n  authority: publisher.example\n  package: notes\n  names:\n    singular: note\n  properties:\n    name:\n      type: string\n", id, v, dialect, id)
		file := fmt.Sprintf("v%d.yaml", v)
		if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(body))
		entry := authority.AuthorityPackageVersion{Version: v, Closure: file, SHA256: hex.EncodeToString(sum[:])}
		if listed {
			entry.Dialect = int(dialect)
		}
		versions = append(versions, entry)
		raw, _ := json.Marshal(authority.AuthorityDocument{Authority: "publisher.example", Packages: []authority.AuthorityPackage{{Package: "notes", Versions: versions}}})
		if err := os.WriteFile(docPath, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	shipped := func() string {
		t.Helper()
		row, err := ds.Get(ctx, "substrate.reamde.dev/core/package", id)
		if err != nil {
			t.Fatalf("the package row: %v", err)
		}
		return fmt.Sprint(row.Properties["shippedVersion"])
	}

	release(1, vocabulary.Dialect, true)
	release(2, vocabulary.Dialect+1, true)
	c, err := catalog.LoadWithReader(authority.NewReader(authority.Options{LocalDocuments: map[string]string{"publisher.example": docPath}}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Install(ctx, substrate.ActorAPI, id, ds); err != nil {
		t.Fatalf("install: %v", err)
	}
	if got := shipped(); got != "1" {
		t.Fatalf("the install landed version %s, want 1, the last this server understands", got)
	}

	result, err := c.RefreshPackage(ctx, ds, id)
	if err != nil || result.Outcome == "upgraded" || shipped() != "1" {
		t.Fatalf("a refresh with only a newer dialect's version listed = %+v %v, version %s; want nothing taken", result, err, shipped())
	}

	release(3, vocabulary.Dialect, true)
	result, err = c.RefreshPackage(ctx, ds, id)
	if err != nil || result.Outcome != "upgraded" || shipped() != "3" {
		t.Fatalf("a refresh with version 3 listed = %+v %v, version %s; want it upgraded to 3", result, err, shipped())
	}

	// Version 4 needs the next dialect and its document entry does not say
	// so: the refresh fetches it, finds its header's dialect, and passes it
	// over the same way.
	release(4, vocabulary.Dialect+1, false)
	result, err = c.RefreshPackage(ctx, ds, id)
	if err != nil || result.Outcome == "upgraded" || shipped() != "3" {
		t.Fatalf("a refresh with version 4's dialect only in its header = %+v %v, version %s; want nothing taken", result, err, shipped())
	}
}
