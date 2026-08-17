package vocabulary_test

// THE CONSOLE'S COPY OF THE KEY GRAMMAR, held to this package's.
//
// A keyed map's key contract is one grammar asked in four places: the loader's
// CheckKey, the narrowing guard's SQL, and the console's own client-side check
// (web/console/src/lib/record-schema.ts, KEY_PATTERNS). KeyPatternRegexp is the
// seam that keeps the first three from drifting, and this is the fourth, which
// cannot import Go and so spells the pattern out as a JavaScript regex literal.
//
// It needs a gate because the failure is SILENT and asymmetric: a console
// pattern that is tighter than the server's refuses a key on the line that the
// substrate would have stored, and one that is looser lets an author type a key
// that only fails as a 422 after the round trip. Neither stops a build and
// neither stops a test that does not exist. The generated-types conformance
// suite used to hold a third copy of this grammar (corekinds.KeyPattern*Regexp,
// TestKeyContractsAgree); that whole pipeline was deleted in #217, and the
// console's copy would have been left with nothing checking it.
//
// The console is allowed to know FEWER contracts than the loader — an unknown
// pattern leaves the key unchecked there and the server is still the authority —
// so this checks the patterns it does know, and that it still knows the two that
// exist.

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

// consoleRecordSchema is the console module that carries the copy.
const consoleRecordSchema = "../../web/console/src/lib/record-schema.ts"

// keyPatternEntry matches one `name: /regex/,` entry of the KEY_PATTERNS
// object, across the line break prettier inserts before a long literal.
var keyPatternEntry = regexp.MustCompile(`(?m)^\s*(\w+):\s*\n?\s*/(.+)/,?\s*$`)

func TestConsoleKeyPatternsMatchTheLoader(t *testing.T) {
	src, err := os.ReadFile(consoleRecordSchema)
	if err != nil {
		t.Fatalf("read the console's copy: %v", err)
	}
	block := keyPatternsBlock(t, string(src))
	found := map[string]string{}
	for _, m := range keyPatternEntry.FindAllStringSubmatch(block, -1) {
		// A JavaScript regex literal escapes the delimiter; the Go source does
		// not, and that is the only difference the two spellings are allowed.
		found[m[1]] = strings.ReplaceAll(m[2], `\/`, `/`)
	}
	if len(found) == 0 {
		t.Fatalf("read no pattern out of KEY_PATTERNS; the block's shape changed:\n%s", block)
	}
	for name, got := range found {
		want := vocabulary.KeyPatternRegexp(name)
		if want == "" {
			t.Errorf("the console checks keys against %q, which this package declares no contract for", name)
			continue
		}
		if got != want {
			t.Errorf("keyPattern %s: the console has %q, the loader %q — a key one admits and the other refuses is a write that only fails after the round trip",
				name, got, want)
		}
	}
	// The console must still know the contracts that exist. Without this, an
	// emptied map would pass every check above by having nothing to disagree
	// with, which is precisely the drift this test is for.
	for _, name := range []string{vocabulary.KeyPatternCamel, vocabulary.KeyPatternKindRef} {
		if _, known := found[name]; !known {
			t.Errorf("the console no longer checks the %s contract; every key under it now reaches the server unchecked", name)
		}
	}
}

// keyPatternsBlock cuts the KEY_PATTERNS object out of the module, so a regex
// literal elsewhere in the file cannot be read as one of its entries.
func keyPatternsBlock(t *testing.T, src string) string {
	t.Helper()
	const open = "const KEY_PATTERNS: Record<string, RegExp> = {"
	i := strings.Index(src, open)
	if i < 0 {
		t.Fatalf("%s no longer declares KEY_PATTERNS; move this test to wherever the grammar went", consoleRecordSchema)
	}
	rest := src[i+len(open):]
	j := strings.Index(rest, "\n}")
	if j < 0 {
		t.Fatalf("KEY_PATTERNS is not closed by a line-leading brace; the block's shape changed")
	}
	return rest[:j]
}
