package engine

// The agent plumbing's pure halves: what a call input renders as, the
// seeded provider row's pricing table, and a sub-agent row's write summary.

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// The summary counts records, not entries, and lists at most
// subagentWritesKinds kinds in first-write order, counting the rest, so a
// chain that writes many kinds cannot grow the parent's row without bound.
func TestSubagentWritesCountsRecordsAndCapsKinds(t *testing.T) {
	var entries []changeEntry
	var want []any
	for i := range subagentWritesKinds + 5 {
		kind := fmt.Sprintf("cap.test.dev/cap/k%02d", i)
		// Two entries on one record, then a second record of the same kind.
		entries = append(entries,
			changeEntry{seq: int64(3 * i), op: substrate.OpPut, kind: kind, id: "a"},
			changeEntry{seq: int64(3*i + 1), op: substrate.OpPatch, kind: kind, id: "a"},
			changeEntry{seq: int64(3*i + 2), op: substrate.OpPut, kind: kind, id: "b"},
		)
		if i < subagentWritesKinds {
			want = append(want, kind)
		}
	}
	got := subagentWrites("th1", entries)
	if got["thread"] != "th1" || got["records"] != 2*(subagentWritesKinds+5) || got["moreKinds"] != 5 {
		t.Fatalf("summary = %v, want thread th1, %d records, moreKinds 5", got, 2*(subagentWritesKinds+5))
	}
	if !reflect.DeepEqual(got["kinds"], want) {
		t.Fatalf("kinds = %v, want the first %d in write order", got["kinds"], subagentWritesKinds)
	}

	// At the cap exactly, nothing overflows.
	got = subagentWrites("th1", entries[:3*subagentWritesKinds])
	if _, held := got["moreKinds"]; held || len(got["kinds"].([]any)) != subagentWritesKinds {
		t.Fatalf("summary at the cap = %v", got)
	}

	// Nothing written is a zero count and no kinds.
	got = subagentWrites("th1", nil)
	if got["records"] != 0 {
		t.Fatalf("empty summary = %v", got)
	}
	if _, held := got["kinds"]; held {
		t.Fatalf("empty summary lists kinds: %v", got)
	}
}

// An EMPTY input is refused exactly like a nil one: some wires reject an empty
// user text block outright (anthropic 400s), so letting one through would
// settle a thread on an error after its rows had already landed.
func TestAgentUserContentRefusesAnEmptyInput(t *testing.T) {
	for _, in := range []any{nil, ""} {
		if _, err := agentUserContent(in); !errors.Is(err, substrate.ErrValidation) {
			t.Fatalf("agentUserContent(%#v) = %v, want a validation refusal", in, err)
		}
	}
	got, err := agentUserContent("do the thing")
	if err != nil || got != "do the thing" {
		t.Fatalf("agentUserContent(string) = %q, %v", got, err)
	}
	got, err = agentUserContent(map[string]any{"a": 1})
	if err != nil || got != `{"a":1}` {
		t.Fatalf("agentUserContent(map) = %q, %v", got, err)
	}
}
