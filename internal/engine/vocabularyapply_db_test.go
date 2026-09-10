package engine

// A vocabulary apply prepares every added or changed function body before it
// activates anything, so an uncompilable body fails admission and the batch
// leaves no half-installed package behind.

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func TestSchemaApplyRejectsUnpreparableBody(t *testing.T) {
	t.Parallel()
	// A vocabulary apply prepares every added or changed body BEFORE
	// activation — invalid python fails admission with the register error,
	// and nothing activates.
	ds := newRaceDataset(t)
	ctx := context.Background()
	const badPackage = "broken.test.dev/broken"
	manifest := func(source string) enginetest.Manifest {
		return enginetest.Manifest{
			Name: "broken", Authority: badPackage,
			Manifests: []map[string]any{
				vocabulary.PackageManifest(badPackage, 0),
				vocabulary.ActorManifest(badPackage, vocabulary.PackageActor(badPackage)),
				vocabulary.FunctionManifest(badPackage, "mangle", map[string]any{
					"description": "a body that must compile at registration",
					"runtime":     vocabulary.RuntimePython,
					"source":      source,
					"permissions": map[string]any{"writes": []any{raceWidget}},
				}),
			},
		}
	}
	err := enginetest.Install(ctx, ds, substrate.ActorAPI,
		manifest("def main(input, host)\n    return {}\n")) // missing colon
	if err == nil || !strings.Contains(err.Error(), "failed to prepare") {
		t.Fatalf("invalid source admitted: %v", err)
	}
	if _, ok := ds.registry().PackageByName(badPackage); ok {
		t.Fatal("the failed batch activated")
	}
	// The corrected body admits.
	if err := enginetest.Install(ctx, ds, substrate.ActorAPI,
		manifest("def main(input, host):\n    return {}\n")); err != nil {
		t.Fatalf("valid source refused: %v", err)
	}
	if _, ok := ds.registry().PackageByName(badPackage); !ok {
		t.Fatal("the corrected batch did not activate")
	}
}
