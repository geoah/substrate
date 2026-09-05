package engine_test

// Ordering by a declared DATETIME sorts as an instant. validate.go normalizes
// every write to one UTC RFC 3339 layout, but RFC3339Nano trims trailing zeros
// in the fraction, so a mixed-precision set does not sort as text:
// "09:00:00.5Z" lands before "09:00:00Z" ('.' is 0x2E, 'Z' is 0x5A) while the
// half-second instant comes after. The filter half (condJSON) already casts to
// timestamptz; the order now applies the same cast, and because the order
// expression is also the projected cursor key, the keyset walk is tested
// across the boundary the text sort got wrong, not just the first page.

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const datetimePackage = "when.example.substrate.reamde.dev/when"

// firedAtChronological is the chronological order of the fixture set, which
// differs from its text order (.25Z, .5Z, 00Z, 01Z) at the whole-second row.
var firedAtChronological = []string{
	"2026-08-15T09:00:00Z",
	"2026-08-15T09:00:00.25Z",
	"2026-08-15T09:00:00.5Z",
	"2026-08-15T09:00:01Z",
}

func installPings(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	ctx := context.Background()
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(datetimePackage, 0),
		vocabulary.KindManifest(datetimePackage,
			map[string]any{"singular": "ping", "plural": "pings"},
			map[string]any{"properties": map[string]any{
				"firedAt": map[string]any{"type": "datetime"},
			}}),
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	// Inserted scrambled, so neither insertion order nor created_at can fake
	// the assertion.
	for _, at := range []string{
		firedAtChronological[2], firedAtChronological[0],
		firedAtChronological[3], firedAtChronological[1],
	} {
		mustPut(t, ds, owner, substrate.PutInput{
			Kind:       datetimePackage + "/ping",
			Properties: map[string]any{"firedAt": at},
		})
	}
}

func TestOrderByADatetimePropertySortsChronologically(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	installPings(t, ds)

	page, err := ds.List(ctx, substrate.Query{
		Filter:  substrate.Filter{Kinds: []string{datetimePackage + "/ping"}},
		OrderBy: []substrate.Order{{Property: "firedAt"}},
		First:   50,
	})
	if err != nil {
		t.Fatalf("list ordered by a datetime: %v", err)
	}
	var got []string
	for _, r := range page.Records {
		s, _ := r.Properties["firedAt"].(string)
		got = append(got, s)
	}
	if len(got) != len(firedAtChronological) {
		t.Fatalf("got %d rows, want %d", len(got), len(firedAtChronological))
	}
	for i := range firedAtChronological {
		if got[i] != firedAtChronological[i] {
			t.Fatalf("ordered %v, want %v", got, firedAtChronological)
		}
	}
}

// TestKeysetWalkOverADatetimeOrderPagesAcrossPrecision is the round trip the
// cast has to survive: the order expression is projected as the cursor key
// (a timestamptz rendered to text), passed back as a bound parameter, and
// compared against the native expression in seekPredicate. One-row pages put
// a cursor at every boundary, including the whole-second/fractional one the
// text sort got wrong.
func TestKeysetWalkOverADatetimeOrderPagesAcrossPrecision(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	installPings(t, ds)

	var got []string
	after := ""
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("walk did not terminate")
		}
		page, err := ds.List(ctx, substrate.Query{
			Filter:  substrate.Filter{Kinds: []string{datetimePackage + "/ping"}},
			OrderBy: []substrate.Order{{Property: "firedAt"}},
			First:   1,
			After:   after,
		})
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		for _, r := range page.Records {
			s, _ := r.Properties["firedAt"].(string)
			got = append(got, s)
		}
		if page.Cursor == "" {
			break
		}
		after = page.Cursor
	}
	if len(got) != len(firedAtChronological) {
		t.Fatalf("walk saw %d rows %v, want %d", len(got), got, len(firedAtChronological))
	}
	for i := range firedAtChronological {
		if got[i] != firedAtChronological[i] {
			t.Fatalf("walked %v, want %v", got, firedAtChronological)
		}
	}
}
