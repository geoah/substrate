package engine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The repository's own hands move to `~` by position (decision record 0259):
// the map holds every package, function and agent the history ever declared
// under the name, a deleted one included, and every actor record the history
// holds whose hand carries the name; a line's actor and onBehalfOf, a mark's
// hands, a package's declaredBy link and an actor record's own id,
// name and title move by it; a person's text holding an id stays; a second
// pass changes nothing; and an id whose hand carries the name and that the
// history declares nowhere stops the conversion, naming its seq.
func TestTheRepositorysOwnHandsMoveToTheLocalNamespace(t *testing.T) {
	const home = localizeHome
	id := func(a substrate.Actor) string { return vocabulary.ActorID(a) }
	tasks := substrate.BundleActor(home, "tasks")
	remind := substrate.FunctionActor(home, "tasks", "remind")
	helper := substrate.AgentActor(home, "tasks", "helper")
	other := substrate.BundleActor(home, "other")
	person := substrate.ActorPerson
	moves := actorMovesOf(home, []declarationVersion{
		{Seq: 2, Ref: kindPackage, ID: home + "/tasks"},
		{Seq: 2, Ref: kindFunction, ID: home + "/tasks/remind"},
		{Seq: 2, Ref: kindAgent, ID: home + "/tasks/helper"},
		// Deleted long ago: its hand still moves.
		{Seq: 4, Ref: kindAgent, ID: home + "/tasks/helper", Deleted: true},
		{Seq: 2, Ref: kindPackage, ID: "pub.example.com/elsewhere"},
	}, []declarationVersion{
		// An actor record no declaration of the history names.
		{Seq: 3, Ref: kindActor, ID: id(other), Properties: vocabulary.ActorProperties(other, "")},
	})
	local := func(a substrate.Actor) string {
		l, ok := localHand(a, home)
		if !ok {
			t.Fatalf("%s is no hand of the repository's own", a)
		}
		return id(l)
	}
	for _, a := range []substrate.Actor{tasks, remind, helper, other} {
		if got := moves.moves[id(a)]; got != local(a) || got == id(a) {
			t.Errorf("%s moves to %q, want the id of its hand under ~", a, got)
		}
	}
	if _, ok := moves.moves[id(substrate.BundleActor("pub.example.com", "elsewhere"))]; ok {
		t.Error("another authority's package moves")
	}

	refs := localizeRefs(t)
	run := func(e *changelogfile.Entry) {
		t.Helper()
		if err := localizeLine(home, refs, moves)(e); err != nil {
			t.Fatal(err)
		}
	}
	payload := func(v any) json.RawMessage {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	line := changelogfile.Entry{
		Seq: 7, Txn: 7, Kind: kindPackage, RecordID: home + "/tasks", Op: "put",
		Actor: id(remind), OnBehalfOf: id(person),
		Payload: payload(map[string]any{"fold": []any{
			map[string]any{"kind": "record", "ref": kindPackage, "id": home + "/tasks", "delta": map[string]any{"set": map[string]any{
				substrate.PackageDeclaredBy: map[string]any{"ref": kindActor + "/" + id(helper)},
				"description":               "written by " + id(helper),
			}}},
			map[string]any{"kind": "mark", "ref": kindPackage, "id": home + "/tasks", "property": "description", "pinned": true, "setBy": id(tasks), "onBehalfOf": id(helper)},
		}}),
	}
	run(&line)
	if line.Actor != local(remind) || line.OnBehalfOf != id(person) {
		t.Errorf("the line's actor %s on behalf of %s, want %s on behalf of the person", line.Actor, line.OnBehalfOf, local(remind))
	}
	got := string(line.Payload)
	for _, want := range []string{`"ref":"` + kindActor + "/" + local(helper) + `"`, `"setBy":"` + local(tasks) + `"`, `"onBehalfOf":"` + local(helper) + `"`, `"written by ` + id(helper) + `"`} {
		if !strings.Contains(got, want) {
			t.Errorf("the payload %s holds no %s", got, want)
		}
	}
	again := line
	run(&again)
	if string(again.Payload) != string(line.Payload) || again.Actor != line.Actor {
		t.Errorf("a second pass moved the line:\n first  %s\n second %s", line.Payload, again.Payload)
	}

	record := changelogfile.Entry{
		Seq: 8, Txn: 8, Kind: kindActor, RecordID: id(tasks), Op: "put", Actor: id(substrate.ActorSystem),
		Payload: payload(map[string]any{"fold": []any{map[string]any{
			"kind": "record", "ref": kindActor, "id": id(tasks),
			"delta": map[string]any{"title": home + "/tasks", "set": vocabulary.ActorProperties(tasks, "")},
		}}}),
	}
	run(&record)
	var rec struct {
		Fold []struct {
			ID    string `json:"id"`
			Delta struct {
				Title string         `json:"title"`
				Set   map[string]any `json:"set"`
			} `json:"delta"`
		} `json:"fold"`
	}
	if err := json.Unmarshal(record.Payload, &rec); err != nil {
		t.Fatal(err)
	}
	f := rec.Fold[0]
	link, _ := f.Delta.Set[vocabulary.DocPackage].(map[string]any)
	if record.RecordID != local(tasks) || f.ID != local(tasks) || f.Delta.Title != "~/tasks" || f.Delta.Set["name"] != "~/tasks" ||
		link["ref"] != kindPackage+"/~/tasks" {
		t.Errorf("the actor record's line: id %s, effect %+v; want the hand under ~, named ~/tasks, linking ~/tasks", record.RecordID, f)
	}
	if key, ok := vocabulary.ActorKeyFrom(f.Delta.Set); !ok || vocabulary.ActorID(key) != local(tasks) {
		t.Errorf("the moved record reads back as %s, whose id is not its own", key)
	}

	gone := substrate.FunctionActor(home, "gone", "away")
	stray := changelogfile.Entry{Seq: 9, Txn: 9, Kind: kindPackage, RecordID: home + "/tasks", Op: "put", Actor: id(gone)}
	if err := localizeLine(home, refs, moves)(&stray); err == nil || !strings.Contains(err.Error(), "seq 9") {
		t.Errorf("a hand of the repository's own the history declares nowhere: %v, want the conversion stopped at seq 9", err)
	}
}
