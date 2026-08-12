package engine

import (
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// TestCoalesceChangesKeysByTypeAndID pins codex regress #6: trigger coalescing
// keys pending changes by the FULL (type, id) identity, not the bare id. Two
// matched types that happen to share an id are two distinct records — keying
// on id alone dropped one delivery while the cursor advanced past it.
func TestCoalesceChangesKeysByTypeAndID(t *testing.T) {
	// Two changes, identical ids, different types: both must survive coalescing.
	both := coalesceChanges([]substrate.Change{
		{Seq: 1, Kind: "a.substrate.reamde.dev/widget", RecordID: "dup"},
		{Seq: 2, Kind: "b.substrate.reamde.dev/gadget", RecordID: "dup"},
	})
	if len(both) != 2 {
		t.Fatalf("coalesced two distinct (type, id) records into %d — a delivery was dropped", len(both))
	}

	// Same (type, id) repeated: coalescing keeps the LAST, as before.
	one := coalesceChanges([]substrate.Change{
		{Seq: 1, Kind: "a.substrate.reamde.dev/widget", RecordID: "dup"},
		{Seq: 2, Kind: "a.substrate.reamde.dev/widget", RecordID: "dup"},
	})
	if len(one) != 1 || one[0].Seq != 2 {
		t.Fatalf("same-identity coalescing = %+v, want the last (seq 2) only", one)
	}
}
