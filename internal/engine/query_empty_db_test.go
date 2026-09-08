package engine_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// An empty collection answers `"records": []`, never `null`: the wire promises
// an array, the console's Page type is written to it, and a nil slice would
// serialize as the one value that type cannot hold.
func TestListEmptyCollectionSerializesAnArray(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{
		Kinds: []string{"samples.substrate.reamde.dev/tasks/task"},
	}})
	if err != nil {
		t.Fatalf("list an empty collection: %v", err)
	}
	if page.Records == nil {
		t.Fatalf("an empty page carries a nil Records slice; the wire needs []")
	}
	body, err := json.Marshal(page)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(body), `"records":[]`) {
		t.Fatalf("an empty page serialized as %s, want \"records\":[]", body)
	}
}
