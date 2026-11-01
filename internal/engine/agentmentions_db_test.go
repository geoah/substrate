package engine

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// The final reply's record links land on its row as mentions: a record that
// does not exist is missing, one nothing in the run carried is unread, and
// the run's result carries the same list.
func TestTheFinalReplyRecordsWhatItLinks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	widget := crewPackage + "/widget"
	for _, id := range []string{"w-read", "w-unread"} {
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: widget, ID: id, Properties: map[string]any{"name": id},
		}); err != nil {
			t.Fatalf("put %s: %v", id, err)
		}
	}
	reply := "Found [the read one](substrate://" + widget + "/w-read), " +
		"[the other](ref:" + widget + "/w-unread) and [a ghost](substrate://" + widget + "/w-ghost). " +
		"Again: [read](substrate://" + widget + "/w-read). " +
		"`[example](substrate://" + widget + "/w-code)`"
	fake.script("arch",
		fakeTurn{calls: []fakeCall{{"query", `{"kind": "` + widget + `", "id": "w-read"}`}}},
		fakeTurn{content: reply},
	)
	res, err := ds.CallAgent(ctx, crewPackage+"/archivist", "find widgets")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	want := []substrate.AgentMention{
		{Kind: widget, ID: "w-read", Text: "the read one"},
		{Kind: widget, ID: "w-unread", Text: "the other", Unread: true},
		{Kind: widget, ID: "w-ghost", Text: "a ghost", Missing: true, Unread: true},
	}
	if len(res.Mentions) != len(want) {
		t.Fatalf("result mentions: %+v", res.Mentions)
	}
	for i, m := range want {
		if res.Mentions[i] != m {
			t.Errorf("result mention %d = %+v, want %+v", i, res.Mentions[i], m)
		}
	}
	rows := assistantRows(t, ds, res.Thread)
	final := rows[len(rows)-1]
	stored, _ := final["mentions"].([]any)
	if len(stored) != len(want) {
		t.Fatalf("stored mentions: %+v", final["mentions"])
	}
	for i, m := range want {
		got, _ := stored[i].(map[string]any)
		if got["kind"] != m.Kind || got["id"] != m.ID || got["text"] != m.Text ||
			(got["missing"] == true) != m.Missing || (got["unread"] == true) != m.Unread {
			t.Errorf("stored mention %d = %+v, want %+v", i, got, m)
		}
	}
	// The tool-calling turn's row carries none.
	if _, ok := rows[0]["mentions"]; ok {
		t.Fatalf("a tool-calling turn stamped mentions: %+v", rows[0])
	}
}
