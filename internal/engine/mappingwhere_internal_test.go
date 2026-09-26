package engine

// A mapping's where (#581, record 0106) binds only the properties it names to
// its one-row query. The source's own write asks before its secrets are
// sealed, so a property the where does not name must not reach Postgres. No
// database: the extraction is plain Go.

import (
	"reflect"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

func TestWherePropsBindsOnlyTheNamedProperties(t *testing.T) {
	m := &vocabulary.Mapping{WhereOrder: []string{"state", "author"}}
	got := whereProps(m, map[string]any{
		"state":  "open",
		"secret": "ghs_plaintext",
		"title2": "unrelated",
	})
	want := map[string]any{"state": "open"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("whereProps = %v, want %v", got, want)
	}
	if got := whereProps(m, nil); got == nil || len(got) != 0 {
		t.Fatalf("whereProps(nil) = %v, want an empty object", got)
	}
}
