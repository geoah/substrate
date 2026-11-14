package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/vocabulary"
	"github.com/geoah/substrate/kinds"
)

// localizeCase is one line before and after the conversion.
type localizeCase struct {
	name                string
	kind, id            string
	payload             string
	wantKind, wantID    string
	wantPayload         string
	wantPayloadSameText bool
}

const localizeHome = "ada.example.com"

// localizeRefs are the reference positions the cases below are converted
// with: every kind the binary seeds, as the history's seeded declarations
// hold them (seq 1), and the repository's own task kind (two versions, seq 2
// and 5) and a published account kind (seq 3), as their declaration lines
// spell them, under the name. The cases' lines are at seq 7.
func localizeRefs(t *testing.T) referencePositions {
	t.Helper()
	reg, err := vocabulary.LoadFS(kinds.Seed())
	if err != nil {
		t.Fatal(err)
	}
	var versions []declarationVersion
	for _, k := range reg.Kinds() {
		versions = append(versions, declarationVersion{Seq: 1, Ref: kindKind, ID: k.Identity, Properties: k.Definition})
	}
	task := func(seq int64, props map[string]any) declarationVersion {
		return declarationVersion{Seq: seq, Ref: kindKind, ID: "ada.example.com/tasks/task", Properties: map[string]any{
			"authority": "ada.example.com", "package": "tasks", "properties": props,
		}}
	}
	versions = append(versions,
		task(2, map[string]any{
			"name":    map[string]any{"type": "string"},
			"payload": map[string]any{"type": "json"},
			"project": map[string]any{"type": "reference", "kind": "ada.example.com/tasks/project"},
		}),
		declarationVersion{Seq: 3, Ref: kindKind, ID: "providers.substrate.reamde.dev/google/account", Properties: map[string]any{
			"properties": map[string]any{"owner": map[string]any{"type": "reference", "kind": "ada.example.com/people/person"}, "note": map[string]any{"type": "string"}},
		}},
		// A later version adds a list of references and an object holding one.
		task(5, map[string]any{
			"name":     map[string]any{"type": "string"},
			"payload":  map[string]any{"type": "json"},
			"project":  map[string]any{"type": "reference", "kind": "ada.example.com/tasks/project"},
			"blockers": map[string]any{"type": "reference", "kind": "ada.example.com/tasks/task", "repeated": true},
			"review": map[string]any{"type": "object", "fields": map[string]any{
				"by":   map[string]any{"type": "reference", "kind": "ada.example.com/people/person"},
				"note": map[string]any{"type": "string"},
			}},
		}),
	)
	return referencePositionsOf(versions, localizeHome)
}

// A line is read against the declaration of its kind that governs it: the
// one in force at its seq, or, where none is, the first written later in its
// own transaction. No other version is read: a property that was a reference
// when the line was written moves and the same property retyped to json since
// stays as written in a later line; a declaration after a deletion says
// nothing of a line before it was written; and a line no declaration governs
// stops the conversion, naming the kind and the seq.
func TestLocalizeLineReadsTheDeclarationThatGovernsIt(t *testing.T) {
	note := func(seq int64, deleted bool, link map[string]any) declarationVersion {
		v := declarationVersion{Seq: seq, Ref: kindKind, ID: "ada.example.com/notes/note", Deleted: deleted}
		if !deleted {
			v.Properties = map[string]any{"properties": map[string]any{"link": link}}
		}
		return v
	}
	reference := map[string]any{"type": "reference", "kind": "ada.example.com/notes/note"}
	asJSON := map[string]any{"type": "json"}
	refs := referencePositionsOf([]declarationVersion{
		note(10, false, reference),
		note(20, false, asJSON),
		note(30, true, nil),
		note(40, false, reference),
	}, localizeHome)
	line := func(seq, txn int64) (string, error) {
		e := &changelogfile.Entry{
			Seq: seq, Txn: txn, Kind: "ada.example.com/notes/note", RecordID: "n1",
			Payload: []byte(`{"fold":[{"delta":{"set":{"link":{"ref":"ada.example.com/notes/note/n2"}}},"id":"n1","kind":"record","ref":"ada.example.com/notes/note","version":1}]}`),
		}
		err := localizeLine(localizeHome, refs, actorMoves{})(e)
		return string(e.Payload), err
	}
	for _, c := range []struct {
		seq, txn int64
		want     string
	}{
		{15, 15, `"~/notes/note/n2"`},               // a reference when written
		{25, 25, `"ada.example.com/notes/note/n2"`}, // json since, whatever came before or after
		{5, 12, `"~/notes/note/n2"`},                // its transaction declares the kind at 10
		{45, 45, `"~/notes/note/n2"`},               // declared again after the deletion
	} {
		got, err := line(c.seq, c.txn)
		if err != nil || !strings.Contains(got, `"link":{"ref":`+c.want+`}`) {
			t.Errorf("a line at seq %d (transaction to %d): %s, %v; want its link %s", c.seq, c.txn, got, err, c.want)
		}
	}
	for _, c := range []struct{ seq, txn int64 }{
		{5, 5},   // before any declaration, and its transaction declares none
		{35, 35}, // after the deletion, before the kind is declared again
	} {
		if got, err := line(c.seq, c.txn); err == nil || !strings.Contains(err.Error(), fmt.Sprintf("seq %d writes a record of ~/notes/note", c.seq)) {
			t.Errorf("a line at seq %d no declaration governs: %s, %v; want the conversion stopped, naming the kind and the seq", c.seq, got, err)
		}
	}
}

func TestLocalizeLineMovesOwnKindsAndLeavesTheRest(t *testing.T) {
	cases := []localizeCase{
		{
			name: "an own kind: a reference, a list of references and an object's reference move; a json property and a string shaped like them stay",
			kind: "ada.example.com/tasks/task", id: "01jbx9m2qz7k4c5v8n3w6t1hrd",
			payload:  `{"created":true,"fold":[{"delta":{"created":true,"set":{"blockers":[{"ref":"ada.example.com/tasks/task/t2"}],"name":"ada.example.com/tasks/notes","payload":{"ref":"ada.example.com/tasks/task/t3","nested":{"ref":"ada.example.com/tasks/task/t4"}},"project":{"ref":"ada.example.com/tasks/project/01jbx9m2qz7k4c5v8n3w6t1hre"},"review":{"by":{"ref":"ada.example.com/people/person/p1"},"note":"ada.example.com/people/person/p1"}}},"id":"01jbx9m2qz7k4c5v8n3w6t1hrd","kind":"record","ref":"ada.example.com/tasks/task","version":1},{"id":"01jbx9m2qz7k4c5v8n3w6t1hrd","kind":"mark","pinned":true,"property":"name","ref":"ada.example.com/tasks/task"}],"properties":["name","project"]}`,
			wantKind: "~/tasks/task", wantID: "01jbx9m2qz7k4c5v8n3w6t1hrd",
			wantPayload: `{"created":true,"fold":[{"delta":{"created":true,"set":{"blockers":[{"ref":"~/tasks/task/t2"}],"name":"ada.example.com/tasks/notes","payload":{"ref":"ada.example.com/tasks/task/t3","nested":{"ref":"ada.example.com/tasks/task/t4"}},"project":{"ref":"~/tasks/project/01jbx9m2qz7k4c5v8n3w6t1hre"},"review":{"by":{"ref":"~/people/person/p1"},"note":"ada.example.com/people/person/p1"}}},"id":"01jbx9m2qz7k4c5v8n3w6t1hrd","kind":"record","ref":"~/tasks/task","version":1},{"id":"01jbx9m2qz7k4c5v8n3w6t1hrd","kind":"mark","pinned":true,"property":"name","ref":"~/tasks/task"}],"properties":["name","project"]}`,
		},
		{
			name: "a built-in kind: an approval's grant and its agent reference move; its json arguments and its tool name stay",
			kind: "substrate.reamde.dev/llm/approval", id: "ap1",
			payload:  `{"fold":[{"delta":{"set":{"agent":{"ref":"substrate.reamde.dev/core/agent/ada.example.com/crew/scribe"},"arguments":{"ref":"ada.example.com/tasks/task/t1","kind":"ada.example.com/tasks/task"},"tool":"ada.example.com/tasks/stamp","writes":["ada.example.com/tasks/task","providers.substrate.reamde.dev/google/*"]}},"id":"ap1","kind":"record","ref":"substrate.reamde.dev/llm/approval","version":1}]}`,
			wantKind: "substrate.reamde.dev/llm/approval", wantID: "ap1",
			wantPayload: `{"fold":[{"delta":{"set":{"agent":{"ref":"substrate.reamde.dev/core/agent/~/crew/scribe"},"arguments":{"ref":"ada.example.com/tasks/task/t1","kind":"ada.example.com/tasks/task"},"tool":"ada.example.com/tasks/stamp","writes":["~/tasks/task","providers.substrate.reamde.dev/google/*"]}},"id":"ap1","kind":"record","ref":"substrate.reamde.dev/llm/approval","version":1}]}`,
		},
		{
			name: "a declaration: its id and the keys that name kinds move; its title, which the line keeps as the engine rendered it then, its description, its source and an authority row's own name stay",
			kind: "substrate.reamde.dev/core/function", id: "ada.example.com/tasks/remind",
			payload:  `{"fold":[{"delta":{"title":"ada.example.com/tasks/remind","set":{"authority":"ada.example.com","description":"writes ada.example.com/tasks/task","permissions":{"reads":{"kinds":["ada.example.com/people/*"]},"writes":["ada.example.com/tasks/task"]},"source":"host.records.put(\"ada.example.com/tasks/task\", fetch(\"https://ada.example.com/feed\"))"}},"id":"ada.example.com/tasks/remind","kind":"record","ref":"substrate.reamde.dev/core/function","version":1}]}`,
			wantKind: "substrate.reamde.dev/core/function", wantID: "~/tasks/remind",
			wantPayload: `{"fold":[{"delta":{"title":"ada.example.com/tasks/remind","set":{"authority":"~","description":"writes ada.example.com/tasks/task","permissions":{"reads":{"kinds":["~/people/*"]},"writes":["~/tasks/task"]},"source":"host.records.put(\"ada.example.com/tasks/task\", fetch(\"https://ada.example.com/feed\"))"}},"id":"~/tasks/remind","kind":"record","ref":"substrate.reamde.dev/core/function","version":1}]}`,
		},
		{
			name: "a kind's declaration: a property the author named source keeps its name, and its pin moves",
			kind: "substrate.reamde.dev/core/kind", id: "ada.example.com/notes/note",
			payload:  `{"fold":[{"delta":{"set":{"authority":"ada.example.com","package":"notes","properties":{"label":{"description":"from ada.example.com/x","type":"string"},"source":{"kind":"ada.example.com/notes/note","type":"reference"}}}},"id":"ada.example.com/notes/note","kind":"record","ref":"substrate.reamde.dev/core/kind","version":1}]}`,
			wantKind: "substrate.reamde.dev/core/kind", wantID: "~/notes/note",
			wantPayload: `{"fold":[{"delta":{"set":{"authority":"~","package":"notes","properties":{"label":{"description":"from ada.example.com/x","type":"string"},"source":{"kind":"~/notes/note","type":"reference"}}}},"id":"~/notes/note","kind":"record","ref":"substrate.reamde.dev/core/kind","version":1}]}`,
		},
		{
			name: "an app token's scopes",
			kind: "substrate.reamde.dev/core/token", id: "01jbx9k0token000000000000a",
			payload:  `{"fold":[{"delta":{"set":{"label":"ada.example.com/tasks","scopes":["read:ada.example.com/tasks/*","write:implements(ada.example.com/tasks/dated)","read:providers.substrate.reamde.dev/google/*"]}},"id":"01jbx9k0token000000000000a","kind":"record","ref":"substrate.reamde.dev/core/token","version":1}]}`,
			wantKind: "substrate.reamde.dev/core/token", wantID: "01jbx9k0token000000000000a",
			wantPayload: `{"fold":[{"delta":{"set":{"label":"ada.example.com/tasks","scopes":["read:~/tasks/*","write:implements(~/tasks/dated)","read:providers.substrate.reamde.dev/google/*"]}},"id":"01jbx9k0token000000000000a","kind":"record","ref":"substrate.reamde.dev/core/token","version":1}]}`,
		},
		{
			name: "a setting's id moves and its value stays",
			kind: "substrate.reamde.dev/core/setting", id: "ada.example.com/readinglist/denyDomains",
			payload:  `{"fold":[{"delta":{"set":{"type":"string","value":"ada.example.com/private"}},"id":"ada.example.com/readinglist/denyDomains","kind":"record","ref":"substrate.reamde.dev/core/setting","version":1}]}`,
			wantKind: "substrate.reamde.dev/core/setting", wantID: "~/readinglist/denyDomains",
			wantPayload: `{"fold":[{"delta":{"set":{"type":"string","value":"ada.example.com/private"}},"id":"~/readinglist/denyDomains","kind":"record","ref":"substrate.reamde.dev/core/setting","version":1}]}`,
		},
		{
			name: "a trigger's watched kinds move; its guard and its arguments stay",
			kind: "substrate.reamde.dev/core/trigger", id: "tr1",
			payload:  `{"fold":[{"delta":{"set":{"arguments":{"kind":"ada.example.com/tasks/task"},"source":{"record":{"kinds":["ada.example.com/tasks/task"],"when":"record.kind == 'ada.example.com/tasks/task'"}}}},"id":"tr1","kind":"record","ref":"substrate.reamde.dev/core/trigger","version":1}]}`,
			wantKind: "substrate.reamde.dev/core/trigger", wantID: "tr1",
			wantPayload: `{"fold":[{"delta":{"set":{"arguments":{"kind":"ada.example.com/tasks/task"},"source":{"record":{"kinds":["~/tasks/task"],"when":"record.kind == 'ada.example.com/tasks/task'"}}}},"id":"tr1","kind":"record","ref":"substrate.reamde.dev/core/trigger","version":1}]}`,
		},
		{
			name: "a change request's target kind moves; its diff and rationale stay",
			kind: "substrate.reamde.dev/core/recordpatchrequest", id: "rq1",
			payload:  `{"fold":[{"delta":{"set":{"diff":{"note":"ada.example.com/tasks/task"},"rationale":"see ada.example.com/x","targetKind":"ada.example.com/tasks/task"}},"id":"rq1","kind":"record","ref":"substrate.reamde.dev/core/recordpatchrequest","version":1}]}`,
			wantKind: "substrate.reamde.dev/core/recordpatchrequest", wantID: "rq1",
			wantPayload: `{"fold":[{"delta":{"set":{"diff":{"note":"ada.example.com/tasks/task"},"rationale":"see ada.example.com/x","targetKind":"~/tasks/task"}},"id":"rq1","kind":"record","ref":"substrate.reamde.dev/core/recordpatchrequest","version":1}]}`,
		},
		{
			name: "a published package's record keeps its kind, and moves only its reference to an own record",
			kind: "providers.substrate.reamde.dev/google/account", id: "acct",
			payload:  `{"fold":[{"delta":{"set":{"owner":{"ref":"ada.example.com/people/person/p1"},"note":"ada.example.com/x"}},"id":"acct","kind":"record","ref":"providers.substrate.reamde.dev/google/account","version":1}]}`,
			wantKind: "providers.substrate.reamde.dev/google/account", wantID: "acct",
			wantPayload: `{"fold":[{"delta":{"set":{"note":"ada.example.com/x","owner":{"ref":"~/people/person/p1"}}},"id":"acct","kind":"record","ref":"providers.substrate.reamde.dev/google/account","version":1}]}`,
		},
		{
			name: "the repository record keeps the repository's name, which is what it records",
			kind: "substrate.reamde.dev/core/repository", id: "rnd-0abcdefghjkmnpqrstvwxyz00",
			payload:  `{"fold":[{"delta":{"set":{"authority":"ada.example.com","id":"rnd-0abcdefghjkmnpqrstvwxyz00"}},"id":"rnd-0abcdefghjkmnpqrstvwxyz00","kind":"record","ref":"substrate.reamde.dev/core/repository","version":1}]}`,
			wantKind: "substrate.reamde.dev/core/repository", wantID: "rnd-0abcdefghjkmnpqrstvwxyz00",
			wantPayloadSameText: true,
		},
		{
			name: "the repository's own authority row describes a publisher, and stays",
			kind: "substrate.reamde.dev/core/authority", id: "ada.example.com",
			payload:  `{"fold":[{"delta":{"set":{"authority":"ada.example.com","description":"Ada's"}},"id":"ada.example.com","kind":"record","ref":"substrate.reamde.dev/core/authority","version":1}]}`,
			wantKind: "substrate.reamde.dev/core/authority", wantID: "ada.example.com",
			wantPayloadSameText: true,
		},
		{
			name: "a trigger whose own id spells the name keeps it: its id is its writer's",
			kind: "substrate.reamde.dev/core/trigger", id: "ada.example.com/daily",
			payload:  `{"fold":[{"id":"ada.example.com/daily","kind":"tombstone","ref":"substrate.reamde.dev/core/trigger","version":2}]}`,
			wantKind: "substrate.reamde.dev/core/trigger", wantID: "ada.example.com/daily",
			wantPayloadSameText: true,
		},
		{
			name: "a mark's source records move; its hands are ids the actor map moves",
			kind: "substrate.reamde.dev/core/contact", id: "c1",
			payload:  `{"fold":[{"derivedFrom":["ada.example.com/tasks/task/t1","providers.substrate.reamde.dev/google/person/p1"],"id":"c1","kind":"mark","property":"name","ref":"substrate.reamde.dev/core/contact"},{"kind":"resync","rows":{"marks":[{"derivedFrom":["ada.example.com/tasks/task/t2"],"id":"t3","kind":"ada.example.com/tasks/task","property":"name"}]},"scope":[{"id":"t3","kind":"ada.example.com/tasks/task"}]}]}`,
			wantKind: "substrate.reamde.dev/core/contact", wantID: "c1",
			wantPayload: `{"fold":[{"derivedFrom":["~/tasks/task/t1","providers.substrate.reamde.dev/google/person/p1"],"id":"c1","kind":"mark","property":"name","ref":"substrate.reamde.dev/core/contact"},{"kind":"resync","rows":{"marks":[{"derivedFrom":["~/tasks/task/t2"],"id":"t3","kind":"~/tasks/task","property":"name"}]},"scope":[{"id":"t3","kind":"~/tasks/task"}]}]}`,
		},
		{
			name: "another authority that merely starts with the name stays",
			kind: "ada.example.com.evil/tasks/task", id: "x",
			payload:  `{"fold":[{"id":"x","kind":"tombstone","ref":"ada.example.com.evil/tasks/task","version":2}]}`,
			wantKind: "ada.example.com.evil/tasks/task", wantID: "x",
			wantPayloadSameText: true,
		},
	}
	refs := localizeRefs(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := &changelogfile.Entry{Seq: 7, Txn: 7, Kind: c.kind, RecordID: c.id, Actor: "bundle:ada.example.com:tasks", Payload: json.RawMessage(c.payload)}
			if err := localizeLine(localizeHome, refs, actorMoves{})(e); err != nil {
				t.Fatal(err)
			}
			if e.Kind != c.wantKind || e.RecordID != c.wantID {
				t.Errorf("kind, id = %q, %q; want %q, %q", e.Kind, e.RecordID, c.wantKind, c.wantID)
			}
			if e.Actor != "bundle:ada.example.com:tasks" || e.Seq != 7 || e.Txn != 7 {
				t.Errorf("the conversion moved what it does not own: actor %q seq %d txn %d", e.Actor, e.Seq, e.Txn)
			}
			want := c.wantPayload
			if c.wantPayloadSameText {
				want = c.payload
			}
			if !sameJSON(t, string(e.Payload), want) {
				t.Errorf("payload:\n got  %s\n want %s", e.Payload, want)
			}

			// Idempotent: a line already converted comes back as it is.
			again := *e
			if err := localizeLine(localizeHome, refs, actorMoves{})(&again); err != nil {
				t.Fatal(err)
			}
			if again.Kind != e.Kind || again.RecordID != e.RecordID || string(again.Payload) != string(e.Payload) {
				t.Errorf("a second pass changed the line:\n first  %s %s %s\n second %s %s %s",
					e.Kind, e.RecordID, e.Payload, again.Kind, again.RecordID, again.Payload)
			}
		})
	}
}

// A converted line still encodes, and one the conversion does not change
// encodes to the bytes it had, so the runner rewrites only the lines it
// moved.
func TestLocalizeLineLeavesAnUnmovedLineByteForByte(t *testing.T) {
	e := changelogfile.Entry{
		Seq: 3, Txn: 3, TS: time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC), Kind: "substrate.reamde.dev/core/contact", RecordID: "c1", Actor: "api", Op: "put",
		Payload: json.RawMessage(`{"fold":[{"delta":{"set":{"name":"Bo <bo@example.org>","weight":1.5E0}},"id":"c1","kind":"record","ref":"substrate.reamde.dev/core/contact","version":1E0}]}`),
	}
	before, _, err := changelogfile.Encode(e)
	if err != nil {
		t.Fatal(err)
	}
	if err := localizeLine(localizeHome, localizeRefs(t), actorMoves{})(&e); err != nil {
		t.Fatal(err)
	}
	after, _, err := changelogfile.Encode(e)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("a line naming nothing of the repository's changed:\n before %s\n after  %s", before, after)
	}

	// The payload's own bytes too, however an older release spelled them:
	// the runner rewrites a line whose checksum moved, and this one's has no
	// right to.
	odd := `{ "fold": [ {"ref":"substrate.reamde.dev/core/contact","kind":"record","id":"c1","delta":{"set":{"name":"Bo"}}} ] }`
	e.Payload = json.RawMessage(odd)
	if err := localizeLine(localizeHome, localizeRefs(t), actorMoves{})(&e); err != nil {
		t.Fatal(err)
	}
	if string(e.Payload) != odd {
		t.Errorf("an unmoved payload was written again:\n before %s\n after  %s", odd, e.Payload)
	}
}

func sameJSON(t *testing.T, a, b string) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal([]byte(a), &x); err != nil {
		t.Fatalf("%s: %v", a, err)
	}
	if err := json.Unmarshal([]byte(b), &y); err != nil {
		t.Fatalf("%s: %v", b, err)
	}
	xs, _ := json.Marshal(x)
	ys, _ := json.Marshal(y)
	return string(xs) == string(ys)
}

// Every line of the kept v0.123.1 history has a declaration that governs it,
// with the declarations built from that history's own declaration lines as
// the conversion runner hands them: each version the record as it stood
// after its line. The lines no declaration is in force at are the creation
// transaction's records of the core's declaration kinds, written before that
// transaction declares the kinds; the same-transaction rule governs them, and
// the conversion stops on none.
func TestEveryLineOfTheKeptHistoryHasAGoverningDeclaration(t *testing.T) {
	const repo = "../testenv/testdata/kept/v0.123.1/repositories/kept.example.com"
	m, err := changelogfile.ReadManifest(repo)
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(repo, "changelog", "*.ndjson"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no segments in %s: %v", repo, err)
	}
	slices.Sort(files)
	var lines []changelogfile.Entry
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
			e, _, err := changelogfile.Decode(line)
			if err != nil {
				t.Fatal(err)
			}
			lines = append(lines, e)
		}
	}
	state := map[string]map[string]any{}
	var versions []declarationVersion
	for _, e := range lines {
		if _, declaration := vocabularyRecordKinds[e.Kind]; !declaration {
			continue
		}
		var payload struct {
			Fold []struct {
				Kind  string `json:"kind"`
				ID    string `json:"id"`
				Delta struct {
					Set map[string]any `json:"set"`
					Del []string       `json:"del"`
				} `json:"delta"`
			} `json:"fold"`
		}
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		for _, ef := range payload.Fold {
			switch ef.Kind {
			case "record":
				props := map[string]any{}
				maps.Copy(props, state[ef.ID])
				maps.Copy(props, ef.Delta.Set)
				for _, name := range ef.Delta.Del {
					delete(props, name)
				}
				state[ef.ID] = props
				versions = append(versions, declarationVersion{Seq: e.Seq, Ref: e.Kind, ID: ef.ID, Properties: props})
			case "tombstone", "purge":
				delete(state, ef.ID)
				versions = append(versions, declarationVersion{Seq: e.Seq, Ref: e.Kind, ID: ef.ID, Deleted: true})
			}
		}
	}
	refs := referencePositionsOf(versions, m.Authority)
	authorityLines := 0
	for _, e := range lines {
		// Every record a line's effects set, read here apart from the
		// conversion, has a declaration that governs it, its kind read as
		// the conversion reads it.
		var payload struct {
			Fold []struct {
				Ref   string `json:"ref"`
				Delta struct {
					Set map[string]any `json:"set"`
				} `json:"delta"`
			} `json:"fold"`
		}
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		for _, ef := range payload.Fold {
			if len(ef.Delta.Set) == 0 {
				continue
			}
			if _, err := refs.at(vocabulary.LocalizeRef(ef.Ref, m.Authority), e.Seq, e.Txn); err != nil {
				t.Errorf("seq %d: %v", e.Seq, err)
			}
		}
		if e.Kind == kindAuthority {
			authorityLines++
		}
		if err := localizeLine(m.Authority, refs, actorMoves{})(&e); err != nil {
			t.Errorf("seq %d: %v", e.Seq, err)
		}
	}
	if authorityLines == 0 {
		t.Fatal("the kept history holds no authority line")
	}

	// Without the declaration that governs them, the authority lines, which
	// the conversion keeps as written, stop it as every other line would.
	var without []declarationVersion
	for _, v := range versions {
		if v.ID != kindAuthority {
			without = append(without, v)
		}
	}
	bare := referencePositionsOf(without, m.Authority)
	for _, e := range lines {
		if e.Kind != kindAuthority {
			continue
		}
		err := localizeLine(m.Authority, bare, actorMoves{})(&e)
		if err == nil || !strings.Contains(err.Error(), "no declaration of "+kindAuthority+" governs it") {
			t.Errorf("seq %d, its governing declaration removed: %v, want the conversion stopped", e.Seq, err)
		}
	}
}
