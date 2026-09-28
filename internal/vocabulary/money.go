package vocabulary

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// The money value shape lives with the datatype it belongs to (DatatypeMoney):
// the engine's write coercion, its filters and ordering, and the title
// renderer all read these names, so the stored shape has one home.
//
// A money value is `{amount: 1999, currency: "EUR", decimals: 2}`: an INTEGER
// count of minor units, the currency it counts, and how many of the amount's
// digits sit after the decimal point. That value is 19.99 EUR. The amount is an
// integer so it survives the float64 doors exactly (decision 0012), and the
// scale is data, never derived from the currency: a fuel price at 3 decimals
// and a ledger line at 2 are both euros.
const (
	MoneyAmount   = "amount"
	MoneyCurrency = "currency"
	MoneyDecimals = "decimals"
)

// MaxMoneyDecimals is the finest scale a money value may declare. 18 is the
// finest in use (an ether counts wei); no ISO 4217 currency uses more than 4.
const MaxMoneyDecimals = 18

// reCurrency is an ISO 4217 alphabetic code: three capital letters. The list
// of assigned codes changes and a private one (`XTS`, a ledger's own) is
// legitimate, so the grammar is the contract and the code list is not.
var reCurrency = regexp.MustCompile(`^[A-Z]{3}$`)

// ValidCurrency reports whether s is spelled as an ISO 4217 alphabetic code.
func ValidCurrency(s string) bool { return reCurrency.MatchString(s) }

// MoneyDecimal renders an amount of minor units at a scale as its exact decimal
// digits: 1999 at 2 is "19.99", -5 at 2 is "-0.05", 7 at 0 is "7". The scale is
// kept, so 1990 at 2 is "19.90".
func MoneyDecimal(amount int64, decimals int) string {
	digits, neg := strings.CutPrefix(strconv.FormatInt(amount, 10), "-")
	if decimals > 0 {
		if len(digits) <= decimals {
			digits = strings.Repeat("0", decimals-len(digits)+1) + digits
		}
		digits = digits[:len(digits)-decimals] + "." + digits[len(digits)-decimals:]
	}
	if neg {
		return "-" + digits
	}
	return digits
}

// FormatMoney renders a STORED money value the way a title shows it: the exact
// decimal and the currency, "19.99 EUR". A value that is not a money shape
// renders "", which is how every other unrenderable value reads in a title.
func FormatMoney(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	amount, ok := moneyInt(m[MoneyAmount])
	if !ok {
		return ""
	}
	decimals, ok := moneyInt(m[MoneyDecimals])
	if !ok || decimals < 0 || decimals > MaxMoneyDecimals {
		return ""
	}
	currency, _ := m[MoneyCurrency].(string)
	if !ValidCurrency(currency) {
		return ""
	}
	return MoneyDecimal(amount, int(decimals)) + " " + currency
}

// moneyInt reads one integer member of a stored value in every shape the
// decoders hand over: int64 from a live write, float64 from the jsonb
// read-back, json.Number from a replayed delta.
func moneyInt(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		if n != math.Trunc(n) || math.Abs(n) > 1<<53 {
			return 0, false
		}
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	}
	return 0, false
}
