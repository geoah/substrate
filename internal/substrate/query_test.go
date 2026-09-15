package substrate_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// A sort order has two spellings and ONE parser. The records route reads it
// off a query parameter and the runner's `list` host call reads it off a JSON
// frame, so a body that sorts the way a URL does must get the same orders, and
// the same refusal, as the URL.
func TestParseOrderByReadsBothSpellings(t *testing.T) {
	cases := []struct {
		raw  string
		want []substrate.Order
	}{
		{"", nil},
		{"at", []substrate.Order{{Property: "at"}}},
		{"at:asc", []substrate.Order{{Property: "at"}}},
		{"at:desc", []substrate.Order{{Property: "at", Desc: true}}},
		// The compact list: trimmed, case-insensitive on the direction, and an
		// empty part is nothing rather than a property named "".
		{" at : DESC , createdAt ,, ", []substrate.Order{{Property: "at", Desc: true}, {Property: "createdAt"}}},
		{`[{"property":"at","desc":true},{"property":"createdAt"}]`, []substrate.Order{{Property: "at", Desc: true}, {Property: "createdAt"}}},
		{`  [{"property":"dueAt"}]`, []substrate.Order{{Property: "dueAt"}}},
	}
	for _, c := range cases {
		got, err := substrate.ParseOrderBy(c.raw)
		if err != nil {
			t.Fatalf("ParseOrderBy(%q): %v", c.raw, err)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Fatalf("ParseOrderBy(%q) = %+v, want %+v", c.raw, got, c.want)
		}
	}
}

// Every refusal names `orderBy`, and the direction refusal names the property
// it could not sort: a caller reading the message must know which key to fix.
func TestParseOrderByRefusals(t *testing.T) {
	cases := []struct{ raw, names string }{
		{"at:down", "at"},
		{"at:desc,createdAt:sideways", "createdAt"},
		{`[{"prop":"at"}]`, `"prop"`},
		{`[{"property":"at"`, "orderBy"},
	}
	for _, c := range cases {
		_, err := substrate.ParseOrderBy(c.raw)
		if err == nil {
			t.Fatalf("ParseOrderBy(%q) was accepted", c.raw)
		}
		if !strings.HasPrefix(err.Error(), "orderBy: ") {
			t.Fatalf("ParseOrderBy(%q) error %q does not name orderBy", c.raw, err)
		}
		if !strings.Contains(err.Error(), c.names) {
			t.Fatalf("ParseOrderBy(%q) error %q does not name %q", c.raw, err, c.names)
		}
	}
}
