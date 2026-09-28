package vocabulary_test

// THE CONSOLE'S COPY OF THE CURRENCY TABLE, held to this package's.
//
// A money value's currency places its decimal point, so the minor unit per
// code is asked in two places: CurrencyDecimals here, and the console's
// CURRENCY_DECIMALS (web/console/src/lib/money.ts), which parses what a person
// types and lists what they may pick. A console that knows a code the engine
// does not offers a currency every write of it refuses; one that places the
// point differently turns "19.99" into an amount ten or a hundred times off,
// and the server stores it without complaint. So the two must be equal, code
// for code.

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

const consoleMoney = "../../web/console/src/lib/money.ts"

var currencyEntry = regexp.MustCompile(`(?m)^\s*([A-Z]{3}):\s*(\d+),?\s*$`)

func TestConsoleCurrenciesMatchTheEngine(t *testing.T) {
	src, err := os.ReadFile(consoleMoney)
	if err != nil {
		t.Fatalf("read the console's copy: %v", err)
	}
	const open = "export const CURRENCY_DECIMALS: Record<string, number> = {"
	i := strings.Index(string(src), open)
	if i < 0 {
		t.Fatalf("%s no longer declares CURRENCY_DECIMALS; move this test to wherever the table went", consoleMoney)
	}
	rest := string(src)[i+len(open):]
	j := strings.Index(rest, "\n}")
	if j < 0 {
		t.Fatalf("CURRENCY_DECIMALS is not closed by a line-leading brace; the block's shape changed")
	}
	found := map[string]int{}
	for _, m := range currencyEntry.FindAllStringSubmatch(rest[:j], -1) {
		d, _ := strconv.Atoi(m[2])
		found[m[1]] = d
	}
	for _, code := range vocabulary.Currencies() {
		want, _ := vocabulary.CurrencyDecimals(code)
		got, ok := found[code]
		switch {
		case !ok:
			t.Errorf("%s: the engine admits it and the console does not offer it", code)
		case got != want:
			t.Errorf("%s: the console places the point after %d digits, the engine after %d", code, got, want)
		}
		delete(found, code)
	}
	for code := range found {
		t.Errorf("%s: the console offers it and the engine refuses every write of it", code)
	}
}
