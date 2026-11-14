package vocabulary_test

// The package header's `interface:` (decision record 0265): the loader admits
// a function interface this server runs, stores nothing for an absent key,
// and refuses a version it does not run with that reason. Every admission
// door loads a closure through the same loader, so the refusal is the same at
// the apply door, an install and the boot. That the row carries it is the
// engine's, tested over HTTP in internal/testenv/functioninterface_db_test.go.

import (
	"fmt"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

func TestAPackageNamesItsFunctionInterface(t *testing.T) {
	t.Run("an interface this server runs loads as written", func(t *testing.T) {
		for _, v := range vocabulary.FunctionInterfaces {
			r, err := loadStability(t, stabilityPackage(fmt.Sprintf("  interface: %d\n", v)))
			if err != nil {
				t.Fatalf("interface %d: %v", v, err)
			}
			g, _ := r.PackageByName("g.example.com/g")
			if g.Interface != v {
				t.Fatalf("Interface = %d, want %d", g.Interface, v)
			}
		}
	})

	t.Run("an absent key stores nothing", func(t *testing.T) {
		r, err := loadStability(t, stabilityPackage(""))
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		g, _ := r.PackageByName("g.example.com/g")
		if g.Interface != 0 {
			t.Fatalf("Interface = %d, want 0: an absent key is not defaulted in", g.Interface)
		}
	})

	t.Run("a version this server does not run is refused with that reason", func(t *testing.T) {
		next := vocabulary.FunctionInterface + 1
		_, err := loadStability(t, stabilityPackage(fmt.Sprintf("  interface: %d\n", next)))
		wantContains(t, err, fmt.Sprintf(
			"package g.example.com/g: data.interface: the package's code is written against function interface %d, and this server runs interface %d alone",
			next, vocabulary.FunctionInterface))
	})

	t.Run("a value that is no version is refused", func(t *testing.T) {
		for _, body := range []string{
			"  interface: 0\n", "  interface: -1\n", "  interface: \"1\"\n",
			"  interface: 1.5\n", "  interface: [1]\n",
		} {
			_, err := loadStability(t, stabilityPackage(body))
			wantContains(t, err, "data.interface: must be a whole number of at least 1")
		}
	})
}

// THE INTERFACE IS THE PACKAGE'S, like its label: a function is declared in a
// package and runs against the interface its package names.
func TestInterfaceIsAPackageHeaderKeyAlone(t *testing.T) {
	for _, short := range []string{
		vocabulary.DocAuthority, vocabulary.DocKind, vocabulary.DocTrait,
		vocabulary.DocPropertyType, vocabulary.DocRecordMapping, vocabulary.DocFunction,
		vocabulary.DocAgent, vocabulary.DocBundle,
	} {
		if vocabulary.DeclarationDataKeys(short)["interface"] {
			t.Errorf("%s admits `interface`; the function interface is named on a package header and nowhere else", short)
		}
	}
	if !vocabulary.DeclarationDataKeys(vocabulary.DocPackage)["interface"] {
		t.Error("the package header does not admit `interface`")
	}
}
