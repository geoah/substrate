package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// A list cursor carries the head of the history it was minted in, and that
// head is the watch handoff. A cursor minted before an import replaced the
// changelog would hand the client a head the new history never reached, so
// List refuses a cursor whose generation is not the dataset's (decision 0053).
func TestListRefusesACursorFromAnotherHistory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openCursorDataset(t)
	const widgetType = "widgets.test.dev/widgets/widget"
	for _, name := range []string{"one", "two"} {
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": name}}); err != nil {
			t.Fatalf("put %s: %v", name, err)
		}
	}
	q := substrate.Query{Filter: substrate.Filter{Kinds: []string{widgetType}}, First: 1}
	first, err := ds.List(ctx, q)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if first.Cursor == "" || first.Generation != ds.generation || first.Head == 0 {
		t.Fatalf("first page = cursor %q, head %d, generation %q; want a continuation under %q", first.Cursor, first.Head, first.Generation, ds.generation)
	}
	tok, err := decodeKeyset(first.Cursor)
	if err != nil {
		t.Fatal(err)
	}
	if tok.G != ds.generation {
		t.Fatalf("the cursor carries generation %q, want the dataset's %q", tok.G, ds.generation)
	}

	// The same cursor, as a client that saved it before a restore would
	// resend it: the head it carries belongs to a history this dataset does
	// not hold, so the page is refused rather than handed a stale head.
	q.After = encodeKeyset(tok.O, tok.K, tok.H, "another-history")
	if _, err := ds.List(ctx, q); !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("a cursor from another generation listed: %v", err)
	}
	// And the genuine continuation still walks.
	q.After = first.Cursor
	second, err := ds.List(ctx, q)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(second.Records) != 1 || second.Head != first.Head || second.Generation != ds.generation {
		t.Fatalf("second page = %d records, head %d, generation %q; want one record under the first page's head %d", len(second.Records), second.Head, second.Generation, first.Head)
	}
}
