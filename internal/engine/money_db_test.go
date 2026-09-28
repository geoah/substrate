package engine_test

// The money datatype against Postgres: what a write stores and a read returns,
// the title a template renders from it, the filter that compares within one
// currency, the order that compares the exact number, and the bound a
// declaration change may not raise over live rows.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const moneyPackage = "money.example.substrate.reamde.dev/money"

func moneyValue(amount int64, currency string) map[string]any {
	return map[string]any{"amount": amount, "currency": currency}
}

func itemManifest(price map[string]any) map[string]any {
	return vocabulary.KindManifest(moneyPackage,
		map[string]any{"singular": "item"},
		map[string]any{
			"displayTemplate": "{name}: {price}",
			"properties": map[string]any{
				"name":  map[string]any{"type": "string"},
				"price": price,
			},
		})
}

// installPricedItems declares one kind with a money property and seeds it
// with prices whose order differs from their amounts' order and from their
// text, in currencies of three minor units, inserted scrambled.
func installPricedItems(t *testing.T) substrate.Dataset {
	t.Helper()
	_, ds := newDataset(t)
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), owner, []map[string]any{
		vocabulary.PackageManifest(moneyPackage, 0),
		itemManifest(map[string]any{"type": "money"}),
	}); err != nil {
		t.Fatalf("install the money kind: %v", err)
	}
	for name, price := range map[string]map[string]any{
		"ten":     moneyValue(1005, "EUR"),  // 10.05
		"cheap":   moneyValue(99, "USD"),    // 0.99
		"hundred": moneyValue(10010, "EUR"), // 100.10
		"nine":    moneyValue(9500, "KWD"),  // 9.500
		"two":     moneyValue(2, "JPY"),     // 2
		"refund":  moneyValue(-250, "EUR"),  // -2.50
		"precise": moneyValue(1999, "EUR"),  // 19.99
	} {
		mustPut(t, ds, owner, substrate.PutInput{
			Kind: moneyPackage + "/item", Properties: map[string]any{"name": name, "price": price},
		})
	}
	return ds
}

func pricedNames(t *testing.T, ds substrate.Dataset, q substrate.Query) []string {
	t.Helper()
	q.Filter.Kinds = []string{moneyPackage + "/item"}
	if q.First == 0 {
		q.First = 50
	}
	page, err := ds.List(context.Background(), q)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	out := make([]string, 0, len(page.Records))
	for _, r := range page.Records {
		out = append(out, r.Properties["name"].(string))
	}
	return out
}

func TestMoneyStoresReadsAndTitles(t *testing.T) {
	t.Parallel()
	ds := installPricedItems(t)
	rec := mustPut(t, ds, owner, substrate.PutInput{
		Kind:       moneyPackage + "/item",
		Properties: map[string]any{"name": "coffee", "price": moneyValue(350, "EUR")},
	})
	got, err := ds.Get(context.Background(), rec.Kind, rec.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if s := vocabulary.FormatMoney(got.Properties["price"]); s != "3.50 EUR" {
		t.Fatalf("read back %#v, want 3.50 EUR", got.Properties["price"])
	}
	if got.Title != "coffee: 3.50 EUR" {
		t.Fatalf("title = %q, want %q", got.Title, "coffee: 3.50 EUR")
	}
	for name, bad := range map[string]any{
		"a bare number":    19.99,
		"a decimal string": "19.99",
		"a fraction":       map[string]any{"amount": 3.5, "currency": "EUR"},
		"a lowercase code": moneyValue(350, "eur"),
		"an unknown code":  moneyValue(350, "XYZ"),
		"a scale":          map[string]any{"amount": 350, "currency": "EUR", "decimals": 2},
	} {
		_, err := ds.Put(context.Background(), owner, substrate.PutInput{
			Kind: moneyPackage + "/item", Properties: map[string]any{"name": "bad", "price": bad},
		})
		if !errors.Is(err, substrate.ErrValidation) {
			t.Fatalf("%s: err = %v, want a validation error", name, err)
		}
	}
}

// An order compares the exact number each value denotes, the currency's minor
// unit placing the point: 9500 KWD fils is 9.500 and sits between 2 yen and
// 10.05 EUR, never where 9500 would.
// One-row pages put a cursor at every boundary, so the numeric key survives
// its round trip through the cursor as text.
func TestOrderByMoneyComparesTheExactNumber(t *testing.T) {
	t.Parallel()
	ds := installPricedItems(t)
	want := []string{"refund", "cheap", "two", "nine", "ten", "precise", "hundred"}
	var got []string
	after := ""
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("walk did not terminate")
		}
		page, err := ds.List(context.Background(), substrate.Query{
			Filter:  substrate.Filter{Kinds: []string{moneyPackage + "/item"}},
			OrderBy: []substrate.Order{{Property: "price"}},
			First:   1,
			After:   after,
		})
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		for _, r := range page.Records {
			got = append(got, r.Properties["name"].(string))
		}
		if page.Cursor == "" {
			break
		}
		after = page.Cursor
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ordered %v, want %v", got, want)
	}
}

// A comparison holds within the operand's currency, where one minor unit is
// one minor unit.
func TestFilterByMoneyComparesWithinACurrency(t *testing.T) {
	t.Parallel()
	ds := installPricedItems(t)
	byPrice := []substrate.Order{{Property: "price"}}
	for _, tc := range []struct {
		name string
		cond substrate.Cond
		want []string
	}{
		{"gte stays in its currency", substrate.Cond{Gte: moneyValue(950, "EUR")}, []string{"ten", "precise", "hundred"}},
		{"a range", substrate.Cond{Gt: moneyValue(0, "EUR"), Lt: moneyValue(2000, "EUR")}, []string{"ten", "precise"}},
		{"eq", substrate.Cond{Eq: moneyValue(1999, "EUR")}, []string{"precise"}},
		{"in", substrate.Cond{In: []any{moneyValue(99, "USD"), moneyValue(2, "JPY")}}, []string{"cheap", "two"}},
		{"another currency", substrate.Cond{Lte: moneyValue(10000, "USD")}, []string{"cheap"}},
		{"thousandths", substrate.Cond{Gte: moneyValue(9500, "KWD")}, []string{"nine"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := pricedNames(t, ds, substrate.Query{
				Filter:  substrate.Filter{Properties: map[string]substrate.Cond{"price": tc.cond}},
				OrderBy: byPrice,
			})
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("filtered to %v, want %v", got, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		name string
		cond substrate.Cond
		want string
	}{
		{"a bare number has no currency", substrate.Cond{Gte: "9.50"}, "compares against a money value"},
		{"a bad operand", substrate.Cond{Eq: moneyValue(1, "euro")}, "ISO 4217"},
		{"prefix", substrate.Cond{Prefix: "1"}, "is money"},
		{"match", substrate.Cond{Match: "ten"}, "is money"},
		{"contains", substrate.Cond{Contains: moneyValue(1, "EUR")}, "contains is for a list"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := ds.List(context.Background(), substrate.Query{Filter: substrate.Filter{
				Kinds:      []string{moneyPackage + "/item"},
				Properties: map[string]substrate.Cond{"price": tc.cond},
			}})
			if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want a validation error naming %q", err, tc.want)
			}
		})
	}
}

// A raised `min` is refused while a live row holds a smaller value, compared
// by the number the value denotes: -2.50 EUR is below 0, 0.99 USD is not.
func TestRaisingAMoneyMinIsRefusedOverSmallerRows(t *testing.T) {
	t.Parallel()
	ds := installPricedItems(t)
	_, err := ds.ApplyVocabularyDocuments(context.Background(), owner, []map[string]any{
		itemManifest(map[string]any{"type": "money", "min": 0}),
	})
	if err == nil || !strings.Contains(err.Error(), "requires values >= 0 while 1 live records hold a smaller one") {
		t.Fatalf("err = %v, want the raised min refused over the one refund", err)
	}
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), owner, []map[string]any{
		itemManifest(map[string]any{"type": "money", "min": -3}),
	}); err != nil {
		t.Fatalf("a min every row meets: %v", err)
	}
}

// A declared default is a money value the write path admits, stored on a
// create that leaves the property out.
func TestMoneyDefaultFillsACreate(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	ctx := context.Background()
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(moneyPackage, 0),
		itemManifest(map[string]any{"type": "money", "default": moneyValue(0, "EUR")}),
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	rec := mustPut(t, ds, owner, substrate.PutInput{
		Kind: moneyPackage + "/item", Properties: map[string]any{"name": "free"},
	})
	if s := vocabulary.FormatMoney(rec.Properties["price"]); s != "0.00 EUR" {
		t.Fatalf("price = %#v, want the default 0.00 EUR", rec.Properties["price"])
	}
	_, err := ds.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		itemManifest(map[string]any{"type": "money", "default": moneyValue(0, "euro")}),
	})
	if err == nil || !strings.Contains(err.Error(), "ISO 4217") {
		t.Fatalf("err = %v, want a default no write could store refused", err)
	}
}
