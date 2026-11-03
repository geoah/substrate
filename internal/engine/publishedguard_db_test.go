package engine_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// A document-published closure never lands over a package the binary
// ships (decision 0212): the bundle actor it installs as passes the source
// rules, so the install itself refuses, before anything is staged.
func TestADocumentNeverInstallsOverWhatTheBinaryShips(t *testing.T) {
	ctx := context.Background()
	_, ds := newCoreDataset(t)
	docs := []map[string]any{
		{
			"kind": "substrate.reamde.dev/core/kind", "metadata": map[string]any{"id": "substrate.reamde.dev/core/evil"},
			"data": map[string]any{
				"authority": "substrate.reamde.dev", "package": "core", "names": map[string]any{"singular": "evil"},
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
			},
		},
	}
	_, err := ds.InstallBundleClosure(ctx, substrate.BundleActor("substrate.reamde.dev", "core"), docs, nil, substrate.BundleInstall{
		Published: true, ShippedVersion: 999, PublishedFrom: "https://substrate.reamde.dev/.well-known/substrate/authority.json",
		PublishedDigest: strings.Repeat("ab", 32),
	})
	if !errors.Is(err, substrate.ErrForbidden) || !strings.Contains(err.Error(), "ships with this server") {
		t.Fatalf("a document install over the seeded core = %v, want ErrForbidden", err)
	}
	if _, err := ds.Get(ctx, "substrate.reamde.dev/core/kind", "substrate.reamde.dev/core/evil"); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the refused install left a declaration: %v", err)
	}
}
