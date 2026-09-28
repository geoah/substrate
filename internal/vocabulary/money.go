package vocabulary

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
)

// The money value shape lives with the datatype it belongs to (DatatypeMoney):
// the engine's write coercion, its filters and ordering, and the title
// renderer all read these names, so the stored shape has one home.
//
// A money value is `{amount: 1999, currency: "EUR"}`: an INTEGER count of the
// currency's minor units and the ISO 4217 code it counts. The currency's minor
// unit says where the decimal point sits, so that value is 19.99 EUR, and
// `{amount: 1999, currency: "JPY"}` is 1999 yen. The amount is an integer so
// it survives the float64 doors exactly (decision 0012).
const (
	MoneyAmount   = "amount"
	MoneyCurrency = "currency"
)

// currencyDecimals is ISO 4217's minor unit per active alphabetic code: how
// many of an amount's digits follow the decimal point. The codes with no minor
// unit (the precious metals, XDR, the bond units, XTS, XXX) are left out,
// because a count of minor units means nothing there.
//
// The console carries a copy (web/console/src/lib/money.ts, CURRENCY_DECIMALS)
// that TestConsoleCurrenciesMatchTheEngine holds to this one.
var currencyDecimals = map[string]int{
	// No minor unit in use.
	"BIF": 0, "CLP": 0, "DJF": 0, "GNF": 0, "ISK": 0, "JPY": 0, "KMF": 0,
	"KRW": 0, "PYG": 0, "RWF": 0, "UGX": 0, "UYI": 0, "VND": 0, "VUV": 0,
	"XAF": 0, "XOF": 0, "XPF": 0,
	// Thousandths.
	"BHD": 3, "IQD": 3, "JOD": 3, "KWD": 3, "LYD": 3, "OMR": 3, "TND": 3,
	// Ten-thousandths: the two unidades de fomento.
	"CLF": 4, "UYW": 4,
	// Hundredths, which is every other active code.
	"AED": 2, "AFN": 2, "ALL": 2, "AMD": 2, "ANG": 2, "AOA": 2, "ARS": 2,
	"AUD": 2, "AWG": 2, "AZN": 2, "BAM": 2, "BBD": 2, "BDT": 2, "BGN": 2,
	"BMD": 2, "BND": 2, "BOB": 2, "BOV": 2, "BRL": 2, "BSD": 2, "BTN": 2,
	"BWP": 2, "BYN": 2, "BZD": 2, "CAD": 2, "CDF": 2, "CHE": 2, "CHF": 2,
	"CHW": 2, "CNY": 2, "COP": 2, "COU": 2, "CRC": 2, "CUP": 2, "CVE": 2,
	"CZK": 2, "DKK": 2, "DOP": 2, "DZD": 2, "EGP": 2, "ERN": 2, "ETB": 2,
	"EUR": 2, "FJD": 2, "FKP": 2, "GBP": 2, "GEL": 2, "GHS": 2, "GIP": 2,
	"GMD": 2, "GTQ": 2, "GYD": 2, "HKD": 2, "HNL": 2, "HTG": 2, "HUF": 2,
	"IDR": 2, "ILS": 2, "INR": 2, "IRR": 2, "JMD": 2, "KES": 2, "KGS": 2,
	"KHR": 2, "KPW": 2, "KYD": 2, "KZT": 2, "LAK": 2, "LBP": 2, "LKR": 2,
	"LRD": 2, "LSL": 2, "MAD": 2, "MDL": 2, "MGA": 2, "MKD": 2, "MMK": 2,
	"MNT": 2, "MOP": 2, "MRU": 2, "MUR": 2, "MVR": 2, "MWK": 2, "MXN": 2,
	"MXV": 2, "MYR": 2, "MZN": 2, "NAD": 2, "NGN": 2, "NIO": 2, "NOK": 2,
	"NPR": 2, "NZD": 2, "PAB": 2, "PEN": 2, "PGK": 2, "PHP": 2, "PKR": 2,
	"PLN": 2, "QAR": 2, "RON": 2, "RSD": 2, "RUB": 2, "SAR": 2, "SBD": 2,
	"SCR": 2, "SDG": 2, "SEK": 2, "SGD": 2, "SHP": 2, "SLE": 2, "SOS": 2,
	"SRD": 2, "SSP": 2, "STN": 2, "SVC": 2, "SYP": 2, "SZL": 2, "THB": 2,
	"TJS": 2, "TMT": 2, "TOP": 2, "TRY": 2, "TTD": 2, "TWD": 2, "TZS": 2,
	"UAH": 2, "USD": 2, "USN": 2, "UYU": 2, "UZS": 2, "VED": 2, "VES": 2,
	"WST": 2, "XCD": 2, "XCG": 2, "YER": 2, "ZAR": 2, "ZMW": 2, "ZWG": 2,
}

// CurrencyDecimals answers a known currency's minor unit: 2 for EUR, 0 for
// JPY, 3 for KWD. An unknown code answers false, and a money value naming one
// is refused, because nothing could say where its decimal point sits.
func CurrencyDecimals(code string) (int, bool) {
	d, ok := currencyDecimals[code]
	return d, ok
}

// Currencies lists every known code, sorted.
func Currencies() []string {
	out := make([]string, 0, len(currencyDecimals))
	for c := range currencyDecimals {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// MoneyDecimal renders an amount of minor units at a scale as its exact decimal
// digits: 1999 at 2 is "19.99", -5 at 2 is "-0.05", 7 at 0 is "7".
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
	currency, _ := m[MoneyCurrency].(string)
	decimals, known := CurrencyDecimals(currency)
	if !known {
		return ""
	}
	return MoneyDecimal(amount, decimals) + " " + currency
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
