package catalog

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/geoah/substrate/internal/authority"
)

// The authority document `.mise/closures.py --authority` writes for a local
// server (apps/tasks/README.md: SUBSTRATE_INSECURE_AUTHORITY_DOCS) names each
// package by its bare name and each closure by a file name beside it. Only
// the server reads it, so nothing else would notice the two drifting apart:
// it is written here from the tree and resolved as the server resolves an
// app's `package` detail, through a reader given it as a local document.
func TestTheDevAuthorityDocumentResolvesAsTheServerReadsIt(t *testing.T) {
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv is not on PATH: the closures are built by .mise/closures.py in the environment .mise/python.sh has uv make")
	}
	dir := t.TempDir()
	cmd := exec.Command(filepath.Join("..", "..", ".mise", "python.sh"), filepath.Join("..", "..", ".mise", "closures.py"),
		"--out", dir, "--authority", "substrate.reamde.dev", "tasks")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("closures.py --authority: %v\n%s", err, out)
	}
	reader := authority.NewReader(authority.Options{LocalDocuments: map[string]string{
		"substrate.reamde.dev": filepath.Join(dir, "authority.json"),
	}})
	c, err := LoadWithReader(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.Resolve(context.Background(), "substrate.reamde.dev/tasks", 1)
	if err != nil {
		t.Fatalf("resolve the tasks package from the dev document: %v", err)
	}
	if b.Version < 1 || len(b.Kinds()) != 2 {
		t.Fatalf("resolved substrate.reamde.dev/tasks at %d with %d kinds, want 2 kinds", b.Version, len(b.Kinds()))
	}
	for _, kind := range b.Kinds() {
		if kind.Identity != "substrate.reamde.dev/tasks/task" && kind.Identity != "substrate.reamde.dev/tasks/list" {
			t.Fatalf("the dev document's closure declares %s", kind.Identity)
		}
	}
}
