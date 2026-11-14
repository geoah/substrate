package engine

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// linkedLine is a history line as the actor links conversion leaves it: the
// actor an id, acting for onBehalfOf.
func linkedLine(actor, onBehalfOf, kind, payload string) changelogfile.Entry {
	return changelogfile.Entry{
		Seq: 9, TS: time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC), Txn: 9,
		Actor: actor, OnBehalfOf: onBehalfOf,
		Op: "patch", RecordID: "r1", Kind: kind, Payload: json.RawMessage(payload),
	}
}

// marked is one line through the marks conversion, its payload decoded.
func marked(t *testing.T, e changelogfile.Entry) (changelogfile.Entry, map[string]any) {
	t.Helper()
	if err := markLine(&e); err != nil {
		t.Fatalf("markLine: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		t.Fatalf("decode %s: %v", e.Payload, err)
	}
	return e, payload
}

func effectAt(t *testing.T, payload map[string]any, i int) map[string]any {
	t.Helper()
	effects, _ := payload[foldPayloadKey].([]any)
	if i >= len(effects) {
		t.Fatalf("no effect %d in %v", i, payload)
	}
	return effects[i].(map[string]any)
}

// EVERY MANAGER ROW BECOMES A MARK, its tier resolved once into `pinned`. A
// hand's tier pins and names the hand; the machine tier pins nothing, and
// names its actor only where the line itself wrote it; the release is an
// unmark; and a stamp the row carried is when the value was set.
func TestMarkLineTurnsEveryManagerIntoAMark(t *testing.T) {
	person := vocabulary.ActorID(substrate.ActorPerson)
	app := vocabulary.ActorID(substrate.AppActor("chat.example.com"))
	sync := vocabulary.ActorID(substrate.FunctionActor("providers.substrate.reamde.dev", "google", "synccontacts"))
	pkg := vocabulary.ActorID(substrate.BundleActor("providers.substrate.reamde.dev", "google"))
	system := vocabulary.ActorID(substrate.ActorSystem)
	const task = "ada.example.com/tasks/task"

	t.Run("a line's own effects", func(t *testing.T) {
		_, payload := marked(t, linkedLine(app, person, task,
			`{"fold":[
			  {"kind":"manager","ref":"`+task+`","id":"r1","property":"name","actor":"`+app+`","tier":"owner","token":"tok1"},
			  {"kind":"manager","ref":"`+task+`","id":"r1","property":"done","actor":"`+app+`","tier":"machine"},
			  {"kind":"manager","ref":"`+task+`","id":"r1","property":"subject","actor":"`+pkg+`","tier":"machine"},
			  {"kind":"manager","ref":"`+task+`","id":"r1","property":"notes"},
			  {"kind":"manager","ref":"`+task+`","id":"r1","property":"title","actor":"`+app+`","tier":"owner","updatedAt":"2026-11-01T08:00:00Z"}],
			  "properties":["name"]}`))
		for i, want := range []map[string]any{
			{"kind": "mark", "ref": task, "id": "r1", "property": "name", "pinned": true, "setBy": app, "onBehalfOf": person},
			{"kind": "mark", "ref": task, "id": "r1", "property": "done", "setBy": app, "onBehalfOf": person},
			{"kind": "mark", "ref": task, "id": "r1", "property": "subject"},
			{"kind": "unmark", "ref": task, "id": "r1", "property": "notes"},
			{"kind": "mark", "ref": task, "id": "r1", "property": "title", "pinned": true, "setBy": app, "onBehalfOf": person, "setAt": "2026-11-01T08:00:00Z"},
		} {
			if got := effectAt(t, payload, i); !reflect.DeepEqual(got, want) {
				t.Errorf("effect %d = %v, want %v", i, got, want)
			}
		}
	})

	t.Run("a recompute's", func(t *testing.T) {
		_, payload := marked(t, linkedLine(system, "", task,
			`{"fold":[{"kind":"manager","ref":"`+task+`","id":"r1","property":"name","actor":"`+sync+`","tier":"machine"}],
			  "managers":{"name":"`+sync+`"},"mechanism":"mapping","triggeredBy":"`+sync+`"}`))
		want := map[string]any{"kind": "mark", "ref": task, "id": "r1", "property": "name"}
		if got := effectAt(t, payload, 0); !reflect.DeepEqual(got, want) {
			t.Errorf("effect = %v, want %v", got, want)
		}
		if _, ok := payload["managers"]; ok {
			t.Errorf("the payload keeps its managers: %v", payload)
		}
	})

	t.Run("a merge's resync, moved set and record", func(t *testing.T) {
		_, payload := marked(t, linkedLine(person, "", kindRecordMerge,
			`{"fold":[
			  {"kind":"resync","scope":[{"kind":"`+task+`","id":"w"}],"rows":{"managers":[
			    {"kind":"`+task+`","id":"w","property":"name","actor":"`+person+`","tier":"owner","token":"tok1","updatedAt":"2026-10-01T00:00:00Z"},
			    {"kind":"`+task+`","id":"w","property":"done","actor":"`+sync+`","tier":"machine","updatedAt":"2026-10-02T00:00:00Z"}]}},
			  {"kind":"record","ref":"`+kindRecordMerge+`","id":"m1","delta":{"set":{"moved":{"managers":[
			    {"property":"name","actor":"`+person+`","tier":"owner","token":"tok1"}]}}}}],
			  "moved":{"managers":[{"property":"done","actor":"`+sync+`","tier":"machine"}],"formerIds":["l"]}}`))
		rows := effectAt(t, payload, 0)["rows"].(map[string]any)
		wantRows := []any{
			map[string]any{"kind": task, "id": "w", "property": "name", "pinned": true, "setBy": person, "setAt": "2026-10-01T00:00:00Z"},
			map[string]any{"kind": task, "id": "w", "property": "done", "setAt": "2026-10-02T00:00:00Z"},
		}
		if _, left := rows["managers"]; left || !reflect.DeepEqual(rows["marks"], wantRows) {
			t.Errorf("resync rows = %v, want marks %v", rows, wantRows)
		}
		moved := payload["moved"].(map[string]any)
		if want := []any{map[string]any{"property": "done"}}; !reflect.DeepEqual(moved["marks"], want) || moved["managers"] != nil {
			t.Errorf("payload moved = %v", moved)
		}
		set := effectAt(t, payload, 1)["delta"].(map[string]any)["set"].(map[string]any)
		want := []any{map[string]any{"property": "name", "pinned": true, "setBy": person}}
		if got := set["moved"].(map[string]any)["marks"]; !reflect.DeepEqual(got, want) {
			t.Errorf("record moved = %v, want %v", set["moved"], want)
		}
	})

	t.Run("a split record's kept result", func(t *testing.T) {
		_, payload := marked(t, linkedLine(person, "", kindRecordSplit,
			`{"fold":[{"kind":"record","ref":"`+kindRecordSplit+`","id":"s1","delta":{"set":{"result":
			  {"fold":[{"kind":"manager","ref":"`+task+`","id":"w","property":"name","actor":"`+person+`","tier":"bundle"}]}}}}]}`))
		result := effectAt(t, payload, 0)["delta"].(map[string]any)["set"].(map[string]any)["result"].(map[string]any)
		want := map[string]any{"kind": "mark", "ref": task, "id": "w", "property": "name", "pinned": true, "setBy": person}
		if got := effectAt(t, result, 0); !reflect.DeepEqual(got, want) {
			t.Errorf("result effect = %v, want %v", got, want)
		}
	})
}

// A BODY'S MARK CONVERTS LIKE ANY PROPERTY'S. A line from before dialect 4
// whose record effect carries `body` as a delta field, and whose manager
// effect names it, leaves the chain's steps 4 and 5, in the chain's order,
// with the value in `set` or its clear in `del`, and the manager row a mark
// on `body` or its release an unmark.
func TestABodyLineTakesTheBodyStepThenTheMarksStep(t *testing.T) {
	t.Parallel()
	person := vocabulary.ActorID(substrate.ActorPerson)
	const note = "ada.example.com/notes/note"
	through := func(t *testing.T, payload string) map[string]any {
		t.Helper()
		canon, err := changelogfile.CanonicalJSON([]byte(payload))
		if err != nil {
			t.Fatalf("the fixture is not a payload: %v", err)
		}
		e := linkedLine(person, "", note, string(canon))
		for _, s := range conversionSteps {
			// A repository's step reads its repository, which one line
			// does not carry.
			if s.Dialect <= 3 || s.Line == nil {
				continue
			}
			if err := s.Line(&e); err != nil {
				t.Fatalf("step %d, %s: %v", s.Dialect, s.Name, err)
			}
		}
		var out map[string]any
		if err := json.Unmarshal(e.Payload, &out); err != nil {
			t.Fatalf("decode %s: %v", e.Payload, err)
		}
		return out
	}

	t.Run("a value", func(t *testing.T) {
		payload := through(t, `{"fold":[
		  {"kind":"record","ref":"`+note+`","id":"r1","version":2,"delta":{"body":"the falcon dives","set":{"name":"Falcon"}}},
		  {"kind":"manager","ref":"`+note+`","id":"r1","property":"body","actor":"`+person+`","tier":"owner"},
		  {"kind":"manager","ref":"`+note+`","id":"r1","property":"name","actor":"`+person+`","tier":"owner"}],
		  "properties":["body","name"]}`)
		delta := effectAt(t, payload, 0)["delta"].(map[string]any)
		if want := map[string]any{"set": map[string]any{"body": "the falcon dives", "name": "Falcon"}}; !reflect.DeepEqual(delta, want) {
			t.Errorf("delta = %v, want %v", delta, want)
		}
		for i, want := range []map[string]any{
			{"kind": "mark", "ref": note, "id": "r1", "property": "body", "pinned": true, "setBy": person},
			{"kind": "mark", "ref": note, "id": "r1", "property": "name", "pinned": true, "setBy": person},
		} {
			if got := effectAt(t, payload, i+1); !reflect.DeepEqual(got, want) {
				t.Errorf("effect %d = %v, want %v", i+1, got, want)
			}
		}
	})

	t.Run("a clear", func(t *testing.T) {
		payload := through(t, `{"fold":[
		  {"kind":"record","ref":"`+note+`","id":"r1","version":3,"delta":{"body":""}},
		  {"kind":"manager","ref":"`+note+`","id":"r1","property":"body"}],
		  "properties":["body"]}`)
		delta := effectAt(t, payload, 0)["delta"].(map[string]any)
		if want := map[string]any{"del": []any{"body"}}; !reflect.DeepEqual(delta, want) {
			t.Errorf("delta = %v, want %v", delta, want)
		}
		if got, want := effectAt(t, payload, 1), map[string]any{"kind": "unmark", "ref": note, "id": "r1", "property": "body"}; !reflect.DeepEqual(got, want) {
			t.Errorf("effect 1 = %v, want %v", got, want)
		}
	})
}

// A MANAGER ROW GOES WHERE ITS VALUE WENT. For each property an earlier step
// takes out of every line of its kind, or moves (takenOutBelow), a line as
// that step leaves it, with the manager row standing on the old name, as an
// effect and as a resync's row: a row whose value was taken out goes, one
// whose value moved becomes a mark on the new name, every other becomes a
// mark, and the converted line converts to itself.
func TestAManagerRowGoesWhereAStepBelowTookItsValue(t *testing.T) {
	t.Parallel()
	person := vocabulary.ActorID(substrate.ActorPerson)
	for _, gone := range takenOutBelow {
		t.Run(gone.kind+"."+gone.property, func(t *testing.T) {
			row := func(property string) string {
				return `"id":"r1","property":"` + property + `","actor":"` + person + `","tier":"owner"`
			}
			set := `"kept":"a value"`
			if gone.movedTo != "" {
				set += `,"` + gone.movedTo + `":"the moved value"`
			}
			e, payload := marked(t, linkedLine(person, "", gone.kind,
				`{"fold":[
				  {"kind":"record","ref":"`+gone.kind+`","id":"r1","delta":{"set":{`+set+`}}},
				  {"kind":"manager","ref":"`+gone.kind+`",`+row(gone.property)+`},
				  {"kind":"manager","ref":"`+gone.kind+`",`+row("kept")+`},
				  {"kind":"resync","scope":[{"kind":"`+gone.kind+`","id":"r1"}],"rows":{"managers":[
				    {"kind":"`+gone.kind+`",`+row(gone.property)+`},
				    {"kind":"`+gone.kind+`",`+row("kept")+`}]}}],
				  "properties":["`+gone.property+`","kept"]}`))
			mark := func(property string) map[string]any {
				return map[string]any{"kind": "mark", "ref": gone.kind, "id": "r1", "property": property, "pinned": true, "setBy": person}
			}
			resyncRow := func(property string) any {
				return map[string]any{"kind": gone.kind, "id": "r1", "property": property, "pinned": true, "setBy": person}
			}
			wantEffects := []map[string]any{mark("kept")}
			wantRows := []any{resyncRow("kept")}
			if gone.movedTo != "" {
				wantEffects = []map[string]any{mark(gone.movedTo), mark("kept")}
				wantRows = []any{resyncRow(gone.movedTo), resyncRow("kept")}
			}
			effects, _ := payload[foldPayloadKey].([]any)
			if len(effects) != len(wantEffects)+2 {
				t.Fatalf("%d effects, want the record, %d marks and the resync: %s", len(effects), len(wantEffects), e.Payload)
			}
			for i, want := range wantEffects {
				if got := effectAt(t, payload, i+1); !reflect.DeepEqual(got, want) {
					t.Errorf("effect %d = %v, want %v", i+1, got, want)
				}
			}
			rows := effectAt(t, payload, len(wantEffects)+1)["rows"].(map[string]any)
			if !reflect.DeepEqual(rows["marks"], wantRows) {
				t.Errorf("resync rows = %v, want %v", rows, wantRows)
			}
			again := e
			if err := markLine(&again); err != nil || !bytes.Equal(again.Payload, e.Payload) {
				t.Errorf("the converted line converted again changed (%v):\n%s\n%s", err, e.Payload, again.Payload)
			}
		})
	}
}

// A converted line comes back unchanged, so a history whose manifest names
// the old dialect over converted lines converts again to the same bytes.
func TestMarkLineLeavesAConvertedLineAlone(t *testing.T) {
	person := vocabulary.ActorID(substrate.ActorPerson)
	e, _ := marked(t, linkedLine(person, "", "ada.example.com/tasks/task",
		`{"fold":[{"kind":"manager","ref":"ada.example.com/tasks/task","id":"r1","property":"name","actor":"`+person+`","tier":"owner"}],
		  "moved":{"managers":[]},"managers":{"name":"`+person+`"}}`))
	once, _, err := changelogfile.Encode(e)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := marked(t, e)
	twice, _, err := changelogfile.Encode(again)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(once, twice) {
		t.Fatalf("a second pass moved the line:\n  %s\n  %s", once, twice)
	}
}

// THE KEPT DIRECTORY CONVERTS ITS MARKS. Every line of the release's kept
// history, through every step of the chain in its order, holds no manager
// row, no tier, no recompute's managers and no mark on a property a step
// below took out of it. What each stored tier resolved to is counted: the
// differences from the directory as kept.
func TestTheKeptDirectoryConvertsItsMarks(t *testing.T) {
	segments, err := filepath.Glob("../testenv/testdata/kept/*/repositories/*/changelog/*.ndjson")
	if err != nil || len(segments) == 0 {
		t.Fatalf("no kept segment: %v", err)
	}
	tiers := map[string]int{}
	counts := map[string]int{}
	for _, path := range segments {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
			e, _, err := changelogfile.Decode(line)
			if err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			before := e.Payload
			for _, step := range conversionSteps {
				if step.Line == nil {
					continue
				}
				if err := step.Line(&e); err != nil {
					t.Fatalf("seq %d, step %d: %v", e.Seq, step.Dialect, err)
				}
			}
			out, _, err := changelogfile.Encode(e)
			if err != nil {
				t.Fatalf("seq %d: %v", e.Seq, err)
			}
			// A declaration's line holds marks too: a package's `actors`
			// is one a step below takes out.
			if dropped, moved := countTakenOut(before); dropped+moved > 0 {
				counts["dropped with the value a step below took out"] += dropped
				counts["moved with the value a step below moved"] += moved
				var payload map[string]any
				if err := json.Unmarshal(e.Payload, &payload); err != nil {
					t.Fatalf("seq %d: %v", e.Seq, err)
				}
				for _, raw := range asItems(payload[foldPayloadKey]) {
					effect, _ := raw.(map[string]any)
					if _, gone := takenOut(effect["ref"], effect["property"]); effect["kind"] == "mark" && gone {
						t.Errorf("seq %d marks %v, which a step below took out of the line: %s", e.Seq, effect["property"], out)
					}
				}
			}
			if vocabulary.IsDeclarationRef(e.Kind) {
				continue
			}
			countTiers(before, tiers)
			var payload map[string]any
			if err := json.Unmarshal(e.Payload, &payload); err != nil {
				t.Fatalf("seq %d: %v", e.Seq, err)
			}
			if _, ok := payload["managers"]; ok {
				t.Errorf("seq %d keeps a recompute's managers: %s", e.Seq, out)
			}
			for _, raw := range asItems(payload[foldPayloadKey]) {
				effect, _ := raw.(map[string]any)
				switch effect["kind"] {
				case "manager":
					t.Errorf("seq %d keeps a manager effect: %s", e.Seq, out)
				case "mark":
					switch {
					case effect["pinned"] == true:
						counts["pinned"]++
					case effect["setBy"] != nil:
						counts["unpinned, setBy kept"]++
					default:
						counts["unpinned, no setBy"]++
					}
					if _, ok := effect["tier"]; ok {
						t.Errorf("seq %d keeps a tier: %s", e.Seq, out)
					}
				case "unmark":
					counts["unmark"]++
				case "resync":
					if rows, _ := effect["rows"].(map[string]any); rows["managers"] != nil {
						t.Errorf("seq %d keeps a resync's managers: %s", e.Seq, out)
					}
				}
			}
		}
	}
	if counts["pinned"]+counts["unpinned, setBy kept"]+counts["unpinned, no setBy"] == 0 {
		t.Fatalf("the kept directory converted no manager into a mark: %v", counts)
	}
	t.Logf("stored tiers %v became %v", tiers, counts)
}

// countTakenOut counts a dialect-1 line's manager effects on a property a
// step below takes out of the line, and on one it moves.
func countTakenOut(raw json.RawMessage) (dropped, moved int) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return 0, 0
	}
	for _, item := range asItems(payload[foldPayloadKey]) {
		effect, _ := item.(map[string]any)
		if effect["kind"] != "manager" {
			continue
		}
		switch to, ok := takenOut(effect["ref"], effect["property"]); {
		case ok && to == "":
			dropped++
		case ok:
			moved++
		}
	}
	return dropped, moved
}

// countTiers counts the tiers a dialect-1 line's manager effects hold.
func countTiers(raw json.RawMessage, into map[string]int) {
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil {
		return
	}
	for _, item := range asItems(payload[foldPayloadKey]) {
		if effect, _ := item.(map[string]any); effect["kind"] == "manager" {
			tier, _ := effect["tier"].(string)
			if effect["actor"] == nil || effect["actor"] == "" {
				tier = "(release)"
			}
			into[tier]++
		}
	}
}
