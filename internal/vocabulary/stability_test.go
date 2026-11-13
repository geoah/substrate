package vocabulary_test

// The package header's `stability:` (decision record 0256): the loader holds
// the value to the closed label set, stores what the author wrote and nothing
// for an absent key, and admits the key on a package header alone. That the
// row carries it is the engine's, tested in
// internal/engine/packagestability_db_test.go.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// stabilityPackage is one package whose header carries the given extra lines
// beside one declared kind.
func stabilityPackage(header string) string {
	return `kind: substrate.reamde.dev/core/package
metadata:
  id: g.example.com/g
data:
  authority: g.example.com
  package: g
  version: 1
` + header + `---
kind: substrate.reamde.dev/core/kind
metadata:
  id: g.example.com/g/thing
data:
  authority: g.example.com
  package: g
  names:
    singular: thing
  properties:
    label:
      type: string
`
}

func loadStability(t *testing.T, bodies ...string) (*vocabulary.Registry, error) {
	t.Helper()
	fsys := fstest.MapFS{}
	for i, body := range bodies {
		fsys[string(rune('a'+i))+".yaml"] = &fstest.MapFile{Data: []byte(body)}
	}
	return vocabulary.LoadFS(fsys)
}

func TestAPackageStatesItsStability(t *testing.T) {
	t.Run("each label loads as written", func(t *testing.T) {
		for _, label := range substrate.Stabilities {
			r, err := loadStability(t, stabilityPackage("  stability: "+label+"\n"))
			if err != nil {
				t.Fatalf("%s: %v", label, err)
			}
			g, ok := r.PackageByName("g.example.com/g")
			if !ok {
				t.Fatalf("%s: package did not load", label)
			}
			if g.Stability != label {
				t.Fatalf("%s: Stability = %q", label, g.Stability)
			}
		}
	})

	t.Run("an absent key stores nothing", func(t *testing.T) {
		r, err := loadStability(t, stabilityPackage(""))
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		g, _ := r.PackageByName("g.example.com/g")
		if g.Stability != "" {
			t.Fatalf("Stability = %q, want empty: an absent key is not defaulted in", g.Stability)
		}
	})

	t.Run("an unknown value is refused, naming the three", func(t *testing.T) {
		const three = `"stable", "beta" or "experimental"`
		for body, want := range map[string]string{
			// The retired spelling of the weakest label is a value like any
			// other the set does not hold.
			"  stability: alpha\n":    `data.stability: "alpha" is not a stability — ` + three,
			"  stability: Stable\n":   `data.stability: "Stable" is not a stability — ` + three,
			"  stability: \"\"\n":     `data.stability: "" is not a stability — ` + three,
			"  stability: 1\n":        `data.stability: must be one of ` + three,
			"  stability: [stable]\n": `data.stability: must be one of ` + three,
		} {
			_, err := loadStability(t, stabilityPackage(body))
			wantContains(t, err, "package g.example.com/g: "+want)
			if !strings.Contains(err.Error(), `reads as "experimental"`) {
				t.Fatalf("%q: error = %v, want it to say what dropping the key reads as", body, err)
			}
		}
	})
}

// THE LABEL IS THE PACKAGE'S. An authority row publishes packages and a bundle
// document says how one installs; neither holds kinds to a promise, so the key
// on either is the closed key set's ordinary refusal.
func TestStabilityIsAPackageHeaderKeyAlone(t *testing.T) {
	t.Run("an authority row refuses it", func(t *testing.T) {
		_, err := loadStability(t, `kind: substrate.reamde.dev/core/authority
metadata:
  id: g.example.com
data:
  description: the publisher
  stability: stable
`, stabilityPackage(""))
		wantContains(t, err, `authority g.example.com: unknown key "stability"`)
	})

	t.Run("a bundle document refuses it", func(t *testing.T) {
		_, err := loadStability(t, stabilityPackage("")+`---
kind: substrate.reamde.dev/core/bundle
metadata:
  id: g.example.com/g
data:
  authority: g.example.com
  package: g
  description: one kind, so the closure is whole
  stability: stable
  installs:
    - g.example.com/g/thing
`)
		wantContains(t, err, `unknown key "stability"`)
	})

	t.Run("the package header is the one document that admits it", func(t *testing.T) {
		for _, short := range []string{
			vocabulary.DocAuthority, vocabulary.DocKind, vocabulary.DocTrait,
			vocabulary.DocPropertyType, vocabulary.DocRecordMapping, vocabulary.DocFunction,
			vocabulary.DocAgent, vocabulary.DocBundle,
		} {
			if vocabulary.DeclarationDataKeys(short)["stability"] {
				t.Errorf("%s admits `stability`; a label sits on a package and nowhere else", short)
			}
		}
		if !vocabulary.DeclarationDataKeys(vocabulary.DocPackage)["stability"] {
			t.Error("the package header does not admit `stability`")
		}
	})
}
