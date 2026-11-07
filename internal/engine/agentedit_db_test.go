package engine

// The edit built-in: a long text property read by line and changed by exact
// replacement against the version read, through the same patch `write`
// takes.

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

func TestAgentEditViewsAndReplacesText(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake, repo := openMossDataset(t)
	pad := repo + "/llm/scratchpad"
	rec, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: pad, Properties: map[string]any{
		"name": "plan", "note": "one\ntwo\nthree\ntwo again",
	}})
	if err != nil {
		t.Fatal(err)
	}
	edit := func(m map[string]any) fakeCall {
		m["kind"], m["id"], m["property"] = pad, rec.ID, "note"
		return fakeCall{"edit", toolArgs(t, m)}
	}
	fake.script(mossModel,
		fakeTurn{calls: []fakeCall{edit(map[string]any{"op": "view", "start": 2, "end": 3})}},
		fakeTurn{calls: []fakeCall{edit(map[string]any{"op": "replace", "ifVersion": rec.Version, "edits": []any{
			map[string]any{"old": "three", "new": "3"},
			map[string]any{"old": "two again", "new": "2 again"},
		}})}},
		fakeTurn{content: "edited"},
	)
	res, err := ds.CallAgent(ctx, mossID, "fix the note")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	rows := toolRows(t, ds, res.Thread)
	if len(rows) != 2 {
		t.Fatalf("tool rows = %d, want view and replace", len(rows))
	}
	if content, _ := rows[0]["content"].(string); !strings.Contains(content, `2\ttwo\n3\tthree\n`) || !strings.Contains(content, `"lines":4`) {
		t.Fatalf("view: %s", content)
	}
	if rows[1]["ok"] != true || len(changeKinds(rows[1])) != 1 {
		t.Fatalf("replace: ok %v, changes %v", rows[1]["ok"], rows[1]["changes"])
	}
	got, err := ds.Get(ctx, pad, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Properties["note"] != "one\ntwo\n3\n2 again" {
		t.Fatalf("note = %q", got.Properties["note"])
	}
}

// Every refusal leaves the text as it was: a stale version, an old that
// matches twice or nowhere, and a declaration a glob grant does not name.
func TestAgentEditRefusesAndAppliesNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake, repo := openMossDataset(t)
	pad := repo + "/llm/scratchpad"
	rec, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: pad, Properties: map[string]any{
		"name": "plan", "note": "a\nb\na",
	}})
	if err != nil {
		t.Fatal(err)
	}
	replace := func(kind, id, prop string, version int64, old string) fakeCall {
		return fakeCall{"edit", toolArgs(t, map[string]any{
			"op": "replace", "kind": kind, "id": id, "property": prop,
			"ifVersion": version, "edits": []any{map[string]any{"old": old, "new": "x"}},
		})}
	}
	cases := []struct {
		call fakeCall
		want string
	}{
		{replace(pad, rec.ID, "note", rec.Version+1, "b"), "view it again"},
		{replace(pad, rec.ID, "note", rec.Version, "a"), "matches 2 places"},
		{replace(pad, rec.ID, "note", rec.Version, "zzz"), "matches nothing"},
		{replace(kindAgent, mossID, "prompt", 1, "You are Moss"), "a glob never reaches one"},
	}
	turns := make([]fakeTurn, 0, len(cases)+1)
	for _, c := range cases {
		turns = append(turns, fakeTurn{calls: []fakeCall{c.call}})
	}
	fake.script(mossModel, append(turns, fakeTurn{content: "refused"})...)
	res, err := ds.CallAgent(ctx, mossID, "try them all")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	rows := toolRows(t, ds, res.Thread)
	for i, c := range cases {
		content, _ := rows[i]["content"].(string)
		if rows[i]["ok"] != false || !strings.Contains(content, c.want) {
			t.Fatalf("case %d: ok %v, %s; want a refusal naming %q", i, rows[i]["ok"], content, c.want)
		}
	}
	got, err := ds.Get(ctx, pad, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Properties["note"] != "a\nb\na" {
		t.Fatalf("a refused edit landed: %q", got.Properties["note"])
	}
}
