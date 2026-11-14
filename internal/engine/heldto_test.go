package engine

import (
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The pinned block a thread stores is a snapshot of reads, held as every
// read is: replayed only to the same work, while its reads' own scope
// answers for each pinned kind what it answered when the block was taken.
// Another person's or another app's work, or a scope that answers
// otherwise, takes it again.
func TestAPinnedBlockIsReplayedOnlyToWorkHeldTheSameWay(t *testing.T) {
	ag := &vocabulary.Agent{Definition: map[string]any{"context": map[string]any{"pins": []any{"x"}}}}
	reads := "x.example.com/p/note=true;"
	as := func(person, answer string, tokens ...string) string {
		return captureKey(holders{onBehalfOf: substrate.PersonActor(person), tokens: tokens}, answer)
	}
	stored := pinnedProps(ag, false, as("ada", reads), "the block", time.Unix(0, 0))
	if block, ok := storedPinned(ag, false, as("ada", reads), stored); !ok || block != "the block" {
		t.Fatalf("the block for the work that captured it: %q %v", block, ok)
	}
	for what, captured := range map[string]string{
		"another person's":                    as("bo", reads),
		"the same person's app's":             as("ada", reads, "token-of-an-app"),
		"the same person's, the scope denies": as("ada", "x.example.com/p/note=false;"),
	} {
		if _, ok := storedPinned(ag, false, captured, stored); ok {
			t.Errorf("the block ada's work captured is replayed to %s work", what)
		}
	}
	if as("ada", reads, "t1", "t2") != as("ada", reads, "t2", "t1") {
		t.Error("the holders' order changes what a block was captured under")
	}
}
