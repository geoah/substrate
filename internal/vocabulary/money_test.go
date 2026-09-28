package vocabulary_test

import (
	"encoding/json"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

func TestMoneyDecimalKeepsTheScale(t *testing.T) {
	for _, tc := range []struct {
		amount   int64
		decimals int
		want     string
	}{
		{1999, 2, "19.99"},
		{1990, 2, "19.90"},
		{5, 2, "0.05"},
		{-5, 2, "-0.05"},
		{-1999, 2, "-19.99"},
		{0, 2, "0.00"},
		{7, 0, "7"},
		{1, 18, "0.000000000000000001"},
		{1<<53 - 1, 2, "90071992547409.91"},
	} {
		if got := vocabulary.MoneyDecimal(tc.amount, tc.decimals); got != tc.want {
			t.Fatalf("MoneyDecimal(%d, %d) = %q, want %q", tc.amount, tc.decimals, got, tc.want)
		}
	}
}

// A stored value reaches the title renderer in every shape the decoders hand
// over: int64 from a live write, float64 from the jsonb read-back, json.Number
// from a replayed delta. All three must render the same title, or a rebuild
// writes a different title than the live fold did.
func TestFormatMoneyReadsEveryStoredShape(t *testing.T) {
	for _, v := range []any{
		map[string]any{"amount": int64(1999), "currency": "EUR", "decimals": int64(2)},
		map[string]any{"amount": float64(1999), "currency": "EUR", "decimals": float64(2)},
		map[string]any{"amount": json.Number("1999"), "currency": "EUR", "decimals": json.Number("2")},
	} {
		if got := vocabulary.FormatMoney(v); got != "19.99 EUR" {
			t.Fatalf("FormatMoney(%#v) = %q, want %q", v, got, "19.99 EUR")
		}
	}
	for _, v := range []any{
		nil,
		"19.99 EUR",
		map[string]any{"amount": 19.99, "currency": "EUR", "decimals": 2},
		map[string]any{"amount": 1999, "currency": "eur", "decimals": 2},
		map[string]any{"amount": 1999, "currency": "EUR"},
		map[string]any{"amount": 1999, "currency": "EUR", "decimals": 19},
	} {
		if got := vocabulary.FormatMoney(v); got != "" {
			t.Fatalf("FormatMoney(%#v) = %q, want nothing", v, got)
		}
	}
}

func TestMoneyDeclarations(t *testing.T) {
	t.Run("every container and a bound", func(t *testing.T) {
		ty := loadThing(t, `  properties:
    price: {type: money, min: 0, default: {amount: 0, currency: EUR, decimals: 2}}
    history: {type: money, repeated: true}
    byRegion: {type: money, keyed: true}
    line:
      type: object
      fields:
        unitPrice: money
    supplier:
      type: reference
      kind: any
      properties:
        quoted: {type: money}
`)
		price := ty.Props["price"]
		if price.Datatype != vocabulary.DatatypeMoney || price.Min == nil || *price.Min != 0 {
			t.Fatalf("price = %+v, want money with min 0", price)
		}
		if !ty.Props["history"].Repeated || !ty.Props["byRegion"].Keyed {
			t.Fatal("repeated and keyed money did not parse")
		}
		if f := ty.Props["line"].Fields["unitPrice"]; f == nil || f.Datatype != vocabulary.DatatypeMoney {
			t.Fatalf("object field = %+v, want money", f)
		}
		if l := ty.Props["supplier"].Properties["quoted"]; l == nil || l.Datatype != vocabulary.DatatypeMoney {
			t.Fatalf("link property = %+v, want money", l)
		}
	})
	t.Run("a refinement base", func(t *testing.T) {
		ty := loadThing(t, `  properties:
    price: {type: price}
---
kind: substrate.reamde.dev/core/propertytype
metadata: {id: g.example.com/g/price}
data: {authority: g.example.com, package: g, base: money, min: 0}
`)
		p := ty.Props["price"]
		if p.Datatype != vocabulary.DatatypeMoney || p.Refined != "price" || p.Min == nil {
			t.Fatalf("price = %+v, want a money refinement with min", p)
		}
	})
	t.Run("a pattern is refused", func(t *testing.T) {
		loadThingErr(t, `  properties:
    price: {type: money, pattern: "^EUR"}
`, "never a pattern or values")
	})
	t.Run("a value set is refused", func(t *testing.T) {
		loadThingErr(t, `  properties:
    price: {type: money, values: [eur, usd]}
`, "never a pattern or values")
	})
	t.Run("a default that is no money value is refused", func(t *testing.T) {
		loadThingErr(t, `  properties:
    price: {type: money, default: "19.99"}
`, "expected a money value")
	})
}
