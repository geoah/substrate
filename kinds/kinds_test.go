package kinds_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/vocabulary"
	"github.com/geoah/substrate/kinds"
)

// The binary ships the vocabulary, so the embed pattern is part of the
// contract: a directory the pattern misses is an authority that silently stops
// existing in production while every other test still passes.
func TestEveryManifestOnDiskIsEmbedded(t *testing.T) {
	var want []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".yaml" {
			return err
		}
		want = append(want, filepath.ToSlash(path))
		return nil
	})
	if err != nil {
		t.Fatalf("walk the tree on disk: %v", err)
	}
	var got []string
	err = fs.WalkDir(kinds.All(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".yaml" {
			return err
		}
		got = append(got, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk the embedded tree: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("embedded %d manifests, %d on disk", len(got), len(want))
	}
}

// Seed and Bundles partition the tree: every authority is in exactly one, and
// an authority directory that appeared in neither would be shipped and unread.
func TestSeedAndBundlesPartitionTheTree(t *testing.T) {
	seed := rootNames(t, kinds.Seed())
	if len(seed) != 1 || seed[0] != kinds.SeedAuthority {
		t.Fatalf("seed root = %v, want just %s", seed, kinds.SeedAuthority)
	}
	bundles := rootNames(t, kinds.Bundles())
	if len(bundles) == 0 {
		t.Fatal("no bundle authorities")
	}
	for _, name := range bundles {
		if name == kinds.SeedAuthority {
			t.Errorf("%s is in both halves", name)
		}
	}
	onDisk, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the tree on disk: %v", err)
	}
	var dirs int
	for _, e := range onDisk {
		if e.IsDir() {
			dirs++
		}
	}
	if len(seed)+len(bundles) != dirs {
		t.Errorf("seed(%d) + bundles(%d) != %d authorities on disk",
			len(seed), len(bundles), dirs)
	}
}

// The seed loads as a registry, and the bundles load as a catalog: the two
// views are handed to two different readers, and each has to be the shape its
// reader walks.
func TestBothViewsLoad(t *testing.T) {
	r, err := vocabulary.LoadFS(kinds.Seed())
	if err != nil {
		t.Fatalf("load the seed: %v", err)
	}
	if len(r.Authorities()) == 0 || len(r.Kinds()) == 0 {
		t.Fatalf("the seed registry is empty: %v", r.Authorities())
	}
	cat, err := catalog.Load(kinds.Bundles())
	if err != nil {
		t.Fatalf("load the shipped catalog: %v", err)
	}
	if len(cat.Bundles()) == 0 {
		t.Fatal("the shipped catalog is empty")
	}
}

// TestShippedCallableActorsAreDistinct: a function's or an agent's writing hand
// is `function:<name>` and the trigger loop guard keys on it,
// so two callables that share a LOCAL name share an actor — and each one's
// trigger silently drops the other's writes as though they were its own echo.
// The actor grammar has no room for the authority, so the shipped catalog has
// to keep its own names apart; installing two third-party bundles that collide
// is a known issue, shipping two that do is a bug.
func TestShippedCallableActorsAreDistinct(t *testing.T) {
	cat, err := catalog.Load(kinds.Bundles())
	if err != nil {
		t.Fatalf("load the shipped catalog: %v", err)
	}
	seen := map[string]string{}
	for _, b := range cat.Bundles() {
		callables := append(append([]string{}, b.Resources.Functions...), b.Resources.Agents...)
		for _, id := range callables {
			actor := "function:" + vocabulary.KindName(id)
			if prev, clash := seen[actor]; clash {
				t.Errorf("%s and %s both write as %s — one of them has to be renamed", prev, id, actor)
				continue
			}
			seen[actor] = id
		}
	}
}

func rootNames(t *testing.T, fsys fs.FS) []string {
	t.Helper()
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		t.Fatalf("read root: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
