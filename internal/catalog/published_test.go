package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/geoah/substrate/internal/authority"
	"github.com/geoah/substrate/internal/substrate"
)

func publishedFixture(t *testing.T, versions ...int) (*Catalog, string) {
	t.Helper()
	dir := t.TempDir()
	p := authority.AuthorityPackage{Package: "notes"}
	for _, v := range versions {
		body := []byte(publishedClosure("example.com/notes", v))
		sum := sha256.Sum256(body)
		name := string(rune('a'+v)) + ".yaml"
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil {
			t.Fatal(err)
		}
		p.Versions = append(p.Versions, authority.AuthorityPackageVersion{Version: v, Closure: name, SHA256: hex.EncodeToString(sum[:])})
	}
	raw, _ := json.Marshal(authority.AuthorityDocument{Authority: "example.com", Packages: []authority.AuthorityPackage{p}})
	path := filepath.Join(dir, "authority.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadWithReader(authority.NewReader(authority.Options{LocalDocuments: map[string]string{"example.com": path}}))
	if err != nil {
		t.Fatal(err)
	}
	return c, path
}

func TestPublishedResolution(t *testing.T) {
	c, _ := publishedFixture(t, 1, 3, 2)
	b, err := c.Resolve(context.Background(), "example.com/notes", 2)
	if err != nil || b.Version != 3 || !b.Origin().FromFile {
		t.Fatalf("resolve: %+v %v", b, err)
	}
	if _, err := c.Resolve(context.Background(), "example.com/notes", 4); !errors.Is(err, ErrNotPublished) {
		t.Fatal(err)
	}
	for _, id := range []string{"providers.substrate.reamde.dev/x", "samples.substrate.reamde.dev/x"} {
		if _, err := c.Resolve(context.Background(), id, 0); !errors.Is(err, ErrNotPublished) {
			t.Fatal(err)
		}
	}
	binary, err := Load(ProviderRoot(fstest.MapFS{"notes/bundle.yaml": {Data: []byte(publishedClosure("example.com/notes", 1))}}))
	if err != nil {
		t.Fatal(err)
	}
	binary.reader = c.reader
	b, err = binary.Resolve(context.Background(), "example.com/notes", 9)
	if err != nil || b.Version != 1 || b.Origin() != nil {
		t.Fatalf("binary must win: %+v %v", b, err)
	}
}

func TestPublishedClosureBoundaries(t *testing.T) {
	raw := publishedClosure("example.com/notes", 2)
	for _, tc := range []struct {
		name, body string
		version    int64
	}{
		{"wrong version", raw, 3},
		{"two bundles", raw + "---\n" + raw, 2},
		{"foreign declaration", raw + "---\nkind: substrate.reamde.dev/core/kind\nmetadata:\n  id: other.example/tasks/task\ndata:\n  authority: other.example\n  package: tasks\n", 2},
		{"token injection", raw + "---\nkind: substrate.reamde.dev/core/token\nmetadata:\n  id: example.com/notes/token\ndata:\n  properties: {}\n", 2},
		{"foreign data", raw + "---\nkind: other.example/tasks/task\nmetadata:\n  id: x\ndata:\n  properties: {}\n", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseClosure([]byte(tc.body), "example.com/notes", tc.version); err == nil {
				t.Fatal("admitted invalid closure")
			}
		})
	}
}

func TestPublishedComparisonAndHolders(t *testing.T) {
	makeBundle := func(body string) *Bundle {
		b, err := ParseClosure([]byte(body), "example.com/notes", 1)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	raw := publishedClosure("example.com/notes", 1)
	old := makeBundle(raw)
	next := makeBundle(strings.Replace(raw, "installs: [example.com/notes/item]", "installs: [example.com/notes/item, example.com/notes/note]", 1) + "---\nkind: substrate.reamde.dev/core/kind\nmetadata:\n  id: example.com/notes/note\ndata:\n  authority: example.com\n  package: notes\n  names:\n    singular: note\n  properties:\n    name:\n      type: string\n")
	if narrows, add := CompareClosures(old, next); narrows || !add {
		t.Fatal(narrows, add)
	}
	if narrows, add := CompareClosures(next, old); !narrows || add {
		t.Fatal(narrows, add)
	}
	edited := makeBundle(strings.Replace(raw, "The notes package.", "Changed.", 1))
	if _, add := CompareClosures(old, edited); add {
		t.Fatal("changed bundle description needs review")
	}
	fn := &Bundle{vocabularyDocs: []map[string]any{{"kind": "substrate.reamde.dev/core/function", "metadata": map[string]any{"id": "example.com/notes/run"}, "data": map[string]any{"source": "old"}}}}
	fnNext := &Bundle{vocabularyDocs: []map[string]any{{"kind": "substrate.reamde.dev/core/function", "metadata": map[string]any{"id": "example.com/notes/run"}, "data": map[string]any{"source": "new"}}}}
	if _, add := CompareClosures(fn, fnNext); add {
		t.Fatal("function change was automatic")
	}
	now := time.Now()
	past := now.Add(-time.Hour)
	holders := BuildHolders([]substrate.TokenInfo{
		{ClientID: "https://example.com/app.json", ClientName: "Notes", AppSoftwareVersion: "1.0", AppPackages: map[string]int64{"example.com/notes": 1}},
		{AppPackages: map[string]int64{"example.com/notes": 1}, ExpiresAt: &past},
		{AppPackages: map[string]int64{"example.com/notes": 2}},
	}, "example.com/notes", 2, now)
	if len(holders) != 1 || holders[0].ClientName != "Notes" {
		t.Fatal(holders)
	}
}

type ownPublishedDataset struct{ substrate.Dataset }

func (ownPublishedDataset) Repository() substrate.RepositoryInfo {
	return substrate.RepositoryInfo{Authority: "example.com"}
}

func TestOwnAuthorityNeverReadsDocument(t *testing.T) {
	c, path := publishedFixture(t, 1)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	_, err := c.ResolveFor(context.Background(), "example.com/notes", 1, ownPublishedDataset{})
	if !errors.Is(err, ErrNotPublished) || !strings.Contains(err.Error(), "own authority") {
		t.Fatal(err)
	}
}

func publishedClosure(id string, version int) string {
	body := requiringClosure(id, version)
	a, p, _ := strings.Cut(id, "/")
	return body + "  installs: [" + id + "/item]\n---\nkind: substrate.reamde.dev/core/kind\nmetadata:\n  id: " + id + "/item\ndata:\n  authority: " + a + "\n  package: " + p + "\n  names:\n    singular: item\n  properties:\n    name:\n      type: string\n"
}
