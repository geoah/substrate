package engine_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
)

// The boot upgrade loads the shipped tree through the one loader, so a
// shipped kind whose `writer:` still says a word from before actor types
// (decision 0290) refuses the boot before any repository opens, naming the
// type that replaced it.
func TestTheBootUpgradeRefusesARetiredWriterNamingItsReplacement(t *testing.T) {
	t.Parallel()
	for old, instead := range map[string]string{
		"owner":     "write person",
		"connector": "write [function, agent]",
		"oauth":     "write system",
	} {
		t.Run(old, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			dsn := seededRepository(t)
			tree := shippedTree(t)
			addShippedKind(t, tree, corePackage, "gizmo")
			path := filepath.Join(tree, corePackage, "gizmo.yaml")
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, append(raw, []byte("    cursor:\n      type: string\n      writer: "+old+"\n")...), 0o600); err != nil {
				t.Fatal(err)
			}
			bumpPackageVersion(t, tree, corePackage, "100")
			_, err = engine.OpenForTest(t, ctx, dsn,
				engine.WithDataRoot(t.TempDir()), engine.WithCredentialKey(engine.TestCredentialKey), engine.WithKindsDir(tree))
			if err == nil {
				t.Fatalf("the boot loaded a shipped tree whose writer says %s", old)
			}
			if want := `writer "` + old + `" is retired: ` + instead; !strings.Contains(err.Error(), want) {
				t.Fatalf("the refusal does not say %q: %v", want, err)
			}
		})
	}
}
