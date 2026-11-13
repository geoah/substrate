package vocabulary_test

// The package header's `dialect:` (decision record 0268): the loader admits a
// dialect this server reads, stores nothing for an absent key, and refuses a
// newer one naming both numbers. That a key set changed only with the number
// is dialectkeys_test.go's.

import (
	"fmt"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

func TestAPackageStatesTheDialectItNeeds(t *testing.T) {
	t.Run("a dialect this server reads loads as written", func(t *testing.T) {
		for v := int64(1); v <= vocabulary.Dialect; v++ {
			r, err := loadStability(t, stabilityPackage(fmt.Sprintf("  dialect: %d\n", v)))
			if err != nil {
				t.Fatalf("dialect %d: %v", v, err)
			}
			g, _ := r.PackageByName("g.example.com/g")
			if g.Dialect != v {
				t.Fatalf("Dialect = %d, want %d", g.Dialect, v)
			}
		}
	})

	t.Run("an absent key stores nothing", func(t *testing.T) {
		r, err := loadStability(t, stabilityPackage(""))
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		g, _ := r.PackageByName("g.example.com/g")
		if g.Dialect != 0 {
			t.Fatalf("Dialect = %d, want 0", g.Dialect)
		}
	})

	t.Run("a newer dialect is refused naming both", func(t *testing.T) {
		_, err := loadStability(t, stabilityPackage(fmt.Sprintf("  dialect: %d\n", vocabulary.Dialect+1)))
		wantContains(t, err, fmt.Sprintf(
			"package g.example.com/g: data.dialect: the package needs vocabulary dialect %d, and this server reads dialect %d and below",
			vocabulary.Dialect+1, vocabulary.Dialect))
	})

	t.Run("a value that is no dialect is refused", func(t *testing.T) {
		for _, body := range []string{"  dialect: 0\n", "  dialect: \"3\"\n", "  dialect: [3]\n"} {
			_, err := loadStability(t, stabilityPackage(body))
			wantContains(t, err, "data.dialect: must be a whole number of at least 1")
		}
	})

	t.Run("the package header is the one document that admits it", func(t *testing.T) {
		for _, short := range []string{
			vocabulary.DocAuthority, vocabulary.DocKind, vocabulary.DocFunction, vocabulary.DocBundle,
		} {
			if vocabulary.DeclarationDataKeys(short)["dialect"] {
				t.Errorf("%s admits `dialect`", short)
			}
		}
	})
}
