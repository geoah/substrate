package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/geoah/substrate/internal/authority"
	"github.com/geoah/substrate/internal/domains"
	"github.com/geoah/substrate/internal/vocabulary"
	"github.com/geoah/substrate/kinds"
)

// The project's own packages come from the binary, whatever the project's
// name's DNS says (decision record 0292): with the `_substrate` record of
// substrate.reamde.dev pointing at another server's document, which lists a
// newer version of a package the binary ships, every package under the
// project's name that the binary ships resolves to the binary's closure, and
// the document is never read.
func TestTheProjectsOwnPackagesComeFromTheBinaryWhateverDNSSays(t *testing.T) {
	var fetched atomic.Int32
	elsewhere := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetched.Add(1)
		doc := authority.AuthorityDocument{Authority: vocabulary.AuthorityPublisher, Packages: []authority.AuthorityPackage{{
			Package: "health", Versions: []authority.AuthorityPackageVersion{{Version: 999, Closure: "https://" + r.Host + "/health.yaml", SHA256: "00"}},
		}}}
		_ = json.NewEncoder(w).Encode(doc)
	}))
	t.Cleanup(elsewhere.Close)
	dns := domains.Static{"_substrate." + vocabulary.AuthorityPublisher + ".": {"v=substrate1 doc=" + elsewhere.URL + "/authority.json"}}
	c, err := LoadWithReader(authority.NewReader(authority.Options{Resolver: dns, Client: elsewhere.Client()}), kinds.SeedPackages, Shipped()...)
	if err != nil {
		t.Fatal(err)
	}
	shipped := 0
	for _, b := range c.Bundles() {
		if b.Authority != vocabulary.AuthorityPublisher {
			continue
		}
		shipped++
		got, err := c.Resolve(context.Background(), b.ID, 1)
		if err != nil {
			t.Fatalf("resolve %s: %v", b.ID, err)
		}
		if got.Origin() != nil || got.Version != b.Version {
			t.Errorf("%s resolved to version %d from %+v, want the binary's %d", b.ID, got.Version, got.Origin(), b.Version)
		}
	}
	if shipped == 0 {
		t.Fatal("the binary ships no package under the project's name to resolve")
	}
	if n := fetched.Load(); n != 0 {
		t.Errorf("the project's document was read %d times while resolving packages the binary ships", n)
	}
}

// A package under the project's name that the binary does NOT ship is read
// from the project's authority document today, as any publisher's is
// (projectPackageFromDocument). This test states that behavior; whether the
// project's name publishes only what the binary ships is not decided.
func TestAProjectPackageTheBinaryDoesNotShipIsReadFromTheProjectDocument(t *testing.T) {
	dir := t.TempDir()
	const id = vocabulary.AuthorityPublisher + "/training"
	body := []byte(publishedClosure(id, 2))
	sum := sha256.Sum256(body)
	if err := os.WriteFile(filepath.Join(dir, "training.yaml"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(authority.AuthorityDocument{Authority: vocabulary.AuthorityPublisher, Packages: []authority.AuthorityPackage{{
		Package: "training", Versions: []authority.AuthorityPackageVersion{{Version: 2, Closure: "training.yaml", SHA256: hex.EncodeToString(sum[:])}},
	}}})
	path := filepath.Join(dir, "authority.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadWithReader(authority.NewReader(authority.Options{LocalDocuments: map[string]string{vocabulary.AuthorityPublisher: path}}), kinds.SeedPackages, Shipped()...)
	if err != nil {
		t.Fatal(err)
	}
	if _, ships := c.ByID(id); ships {
		t.Fatalf("the binary ships %s; the test needs a package it does not", id)
	}
	got, err := c.Resolve(context.Background(), id, 1)
	if err != nil {
		t.Fatalf("resolve %s: %v", id, err)
	}
	if got.Origin() == nil || got.Version != 2 {
		t.Fatalf("%s resolved to %+v (origin %+v), want version 2 from the project's document", id, got.Version, got.Origin())
	}
}

// A publisher's document that says where it moved puts that on the
// publisher of every package it lists (decision record 0293), and the
// package still resolves from this document: nothing follows the pointer.
func TestAPublisherThatMovedIsShownAndNotFollowed(t *testing.T) {
	dir := t.TempDir()
	const id = "old.example/notes"
	body := []byte(publishedClosure(id, 1))
	sum := sha256.Sum256(body)
	if err := os.WriteFile(filepath.Join(dir, "notes.yaml"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(authority.AuthorityDocument{Authority: "old.example", MovedTo: "new.example", Packages: []authority.AuthorityPackage{{
		Package: "notes", Versions: []authority.AuthorityPackageVersion{{Version: 1, Closure: "notes.yaml", SHA256: hex.EncodeToString(sum[:])}},
	}}})
	path := filepath.Join(dir, "authority.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadWithReader(authority.NewReader(authority.Options{LocalDocuments: map[string]string{"old.example": path}}), nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.Resolve(context.Background(), id, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Publisher == nil || got.Publisher.MovedTo != "new.example" || got.Publisher.Authority != "old.example" {
		t.Fatalf("the publisher reads %+v, want old.example saying it moved to new.example", got.Publisher)
	}
}
