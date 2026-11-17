package engine

import (
	"bytes"
	"cmp"
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
	"github.com/geoah/substrate/internal/substrate"
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

// A kind declared before decision 0098 may bind a trait by its bare word,
// and the conversion reads the binding as the loader of the day resolved
// it: the declaring package's trait of that name, else core's, else the one
// trait any package declares under it. A word two other packages declare
// stays unresolved, and a line of the kind stops the conversion.
func TestABareTraitBindingIsReadAsTheLoaderResolvedIt(t *testing.T) {
	trait := func(id, prop string) declarationVersion {
		return declarationVersion{Seq: 1, Ref: kindTrait, ID: id, Properties: map[string]any{"properties": map[string]any{
			prop: map[string]any{"type": "reference", "kind": "ada.example.com/notes/note"},
		}}}
	}
	kind := func(id string, traits ...any) declarationVersion {
		return declarationVersion{Seq: 2, Ref: kindKind, ID: id, Properties: map[string]any{"traits": traits}}
	}
	refs := referencePositionsOf([]declarationVersion{
		trait(vocabulary.PackageCore+"/oauth2", "account"),
		trait("ada.example.com/notes/oauth2", "owner"),
		trait("other.example.com/memory/attention", "about"),
		trait("a.example.com/x/feedback", "from"),
		trait("b.example.com/y/feedback", "to"),
		kind("ada.example.com/notes/note", "oauth2", "attention"),
		kind("ada.example.com/tasks/task", "oauth2(point: dueAt)"),
		kind("ada.example.com/tasks/log", "feedback"),
	}, localizeHome)
	for _, c := range []struct {
		kind       string
		want, lack []string
	}{
		{"~/notes/note", []string{"owner", "about"}, []string{"account"}}, // its own package's oauth2 over core's
		{"~/tasks/task", []string{"account"}, []string{"owner"}},          // core's, with a variant after the word
	} {
		got, err := refs.at(c.kind, 3, 3, "")
		if err != nil {
			t.Fatalf("a line of %s: %v", c.kind, err)
		}
		for _, name := range c.want {
			if _, ok := got[name]; !ok {
				t.Errorf("a line of %s holds no reference at %s, which a trait it binds bare declares: %v", c.kind, name, slices.Sorted(maps.Keys(got)))
			}
		}
		for _, name := range c.lack {
			if _, ok := got[name]; ok {
				t.Errorf("a line of %s holds a reference at %s, from a trait its bare binding did not resolve to", c.kind, name)
			}
		}
	}
	if _, err := refs.at("~/tasks/log", 3, 3, ""); err == nil || !strings.Contains(err.Error(), "binds feedback") {
		t.Errorf("a line of a kind binding a word two packages declare: %v, want the conversion stopped", err)
	}
}

// A repository migration runs before the stored vocabulary loads, so a
// record of a kind the binary ships may precede the history's first
// declaration of that kind, and of a trait it binds: the line is read by the
// first version. A repository's own kind has no such writer, and its line
// before any declaration still stops the conversion.
func TestAShippedKindsLineBeforeItsDeclarationIsReadByTheFirstVersion(t *testing.T) {
	unit := vocabulary.PackageCore + "/unit"
	temporal := vocabulary.PackageCore + "/temporal"
	refs := referencePositionsOf([]declarationVersion{
		{Seq: 10, Ref: kindTrait, ID: temporal, Properties: map[string]any{"properties": map[string]any{
			"source": map[string]any{"type": "reference", "kind": "ada.example.com/notes/note"},
		}}},
		{Seq: 11, Ref: kindKind, ID: unit, Properties: map[string]any{
			"properties": map[string]any{"base": map[string]any{"type": "reference", "kind": unit}},
			"traits":     []any{temporal + "(point)"},
		}},
		{Seq: 11, Ref: kindKind, ID: "ada.example.com/notes/note", Properties: map[string]any{}},
	}, localizeHome)
	got, err := refs.at(unit, 5, 6, vocabulary.ActorID(substrate.ActorSeed))
	if err != nil {
		t.Fatalf("a line of %s one transaction before its declaration: %v", unit, err)
	}
	if _, ok := got["base"]; !ok {
		t.Errorf("the line holds no reference at base, which the first version declares: %v", slices.Sorted(maps.Keys(got)))
	}
	if _, ok := got["source"]; !ok {
		t.Errorf("the line holds no reference at source, which the first version of the trait it binds declares: %v", slices.Sorted(maps.Keys(got)))
	}
	if _, err := refs.at("~/notes/note", 5, 6, vocabulary.ActorID(substrate.ActorSeed)); err == nil || !strings.Contains(err.Error(), "no declaration of ~/notes/note governs it") {
		t.Errorf("a line of an own kind before any declaration of it: %v, want the conversion stopped", err)
	}
}

// A bare binding resolves against the traits that govern the line, as the
// loader reloaded at each declaration resolved it then, never against a
// trait declared after the line's transaction. A trait the declaring package
// adds later takes the binding from then on; a second package declaring the
// word later leaves the earlier lines resolved and stops the conversion at
// the later ones; and a trait declared in the kind's own transaction (one
// vocabulary batch, which the loader resolved after) governs every line of
// that transaction and after. A shipped trait the history declares only
// later takes no part even in a line the engine wrote, which the shipped
// kind's first version may read: the word meant what the history held.
func TestABareTraitBindingIsReadByTheTraitsThatGovernTheLine(t *testing.T) {
	trait := func(seq int64, id, prop string) declarationVersion {
		return declarationVersion{Seq: seq, Ref: kindTrait, ID: id, Properties: map[string]any{"properties": map[string]any{
			prop: map[string]any{"type": "reference", "kind": "ada.example.com/notes/note"},
		}}}
	}
	kind := func(seq int64, id, binds string) declarationVersion {
		return declarationVersion{Seq: seq, Ref: kindKind, ID: id, Properties: map[string]any{"traits": []any{binds}}}
	}
	refs := referencePositionsOf([]declarationVersion{
		trait(1, "other.example.com/people/owner", "account"),
		trait(1, "a.example.com/x/feedback", "from"),
		trait(1, "other.example.com/people/pinned", "elsewhere"),
		kind(2, "ada.example.com/tasks/task", "owner"),
		kind(2, "ada.example.com/tasks/log", "feedback"),
		trait(5, "ada.example.com/tasks/owner", "owner"),
		trait(5, "b.example.com/y/feedback", "to"),
		// One batch, its transaction seq 10 to 12: the kind, a line of it,
		// then the trait it binds.
		kind(10, "ada.example.com/notes/note", "pinned"),
		trait(12, "ada.example.com/notes/pinned", "pinnedBy"),
		trait(1, "other.example.com/x/label", "labelledBy"),
		kind(2, "ada.example.com/tasks/tag", "label"),
		trait(30, vocabulary.PackageCore+"/label", "coreLabel"),
	}, localizeHome)
	system := vocabulary.ActorID(substrate.ActorSystem)
	for _, c := range []struct {
		kind       string
		seq, txn   int64
		actor      string
		want, lack string
	}{
		{"~/tasks/task", 3, 3, "", "account", "owner"},           // only another package's owner governs the line
		{"~/tasks/task", 6, 6, "", "owner", "account"},           // its own package's owner since seq 5
		{"~/tasks/log", 3, 3, "", "from", "to"},                  // one feedback before the second is declared
		{"~/notes/note", 11, 12, "", "pinnedBy", "elsewhere"},    // its package's pinned, later in the batch
		{"~/notes/note", 20, 20, "", "pinnedBy", "elsewhere"},    //
		{"~/tasks/tag", 5, 5, system, "labelledBy", "coreLabel"}, // core's label is declared at 30
		{"~/tasks/tag", 31, 31, "", "coreLabel", "labelledBy"},   // and is core's from then on
	} {
		got, err := refs.at(c.kind, c.seq, c.txn, c.actor)
		if err != nil {
			t.Errorf("a line of %s at seq %d: %v", c.kind, c.seq, err)
			continue
		}
		if _, ok := got[c.want]; !ok {
			t.Errorf("a line of %s at seq %d holds no reference at %s: %v", c.kind, c.seq, c.want, slices.Sorted(maps.Keys(got)))
		}
		if _, ok := got[c.lack]; ok {
			t.Errorf("a line of %s at seq %d holds a reference at %s, from a trait that did not govern it", c.kind, c.seq, c.lack)
		}
	}
	if _, err := refs.at("~/tasks/log", 6, 6, ""); err == nil || !strings.Contains(err.Error(), "binds feedback") {
		t.Errorf("a line of a kind binding a word two packages declare by then: %v, want the conversion stopped", err)
	}
}

// The memo boundTrait keeps answers every line as reading the candidates
// afresh would, across a history in which many packages declare the same word
// at different points, one is deleted and core declares it last, and it
// reads the candidates once per stretch between their versions, not once per
// line.
func TestABareTraitBindingsMemoAnswersAsTheCandidatesDo(t *testing.T) {
	trait := func(seq int64, id string, deleted bool) declarationVersion {
		return declarationVersion{Seq: seq, Ref: kindTrait, ID: id, Deleted: deleted, Properties: map[string]any{"properties": map[string]any{}}}
	}
	versions := []declarationVersion{{Seq: 1, Ref: kindKind, ID: "ada.example.com/tasks/task", Properties: map[string]any{"traits": []any{"owner"}}}}
	for i := range 40 {
		versions = append(versions, trait(int64(10+10*i), fmt.Sprintf("p%d.example.com/people/owner", i), false))
	}
	versions = append(versions, trait(15, "p0.example.com/people/owner", true), trait(450, vocabulary.PackageCore+"/owner", false))
	slices.SortFunc(versions, func(a, b declarationVersion) int { return cmp.Compare(a.Seq, b.Seq) })
	refs := referencePositionsOf(versions, localizeHome)
	asked := 0
	for seq := int64(1); seq <= 500; seq++ {
		for _, txn := range []int64{seq, seq + 3, seq + 12} {
			asked++
			got := refs.boundTrait("owner", "~/tasks/task", seq, txn)
			if want := refs.resolveBare("owner", "~/tasks", seq, txn); got != want {
				t.Fatalf("a line at seq %d in a transaction ending at %d: the memo answers %s, the candidates %s", seq, txn, got, want)
			}
		}
	}
	if held := len(refs.bare.memo); held*10 > asked {
		t.Errorf("the memo holds %d answers for %d lines asked: it reads the candidates per line", held, asked)
	}
}

// A row outside history, which carries no line, is read by its kind's last
// version as the history leaves it: the traits it binds as they stand after
// every line, a trait its own batch declared after it included, and, for a
// kind the history deleted, as they stood when it was deleted.
func TestARecordOutsideHistoryIsReadByTheDeclarationsAsTheyStand(t *testing.T) {
	trait := func(seq int64, id, prop string) declarationVersion {
		return declarationVersion{Seq: seq, Ref: kindTrait, ID: id, Properties: map[string]any{"properties": map[string]any{
			prop: map[string]any{"type": "reference", "kind": "ada.example.com/notes/note"},
		}}}
	}
	kind := func(seq int64, id string, binds ...any) declarationVersion {
		return declarationVersion{Seq: seq, Ref: kindKind, ID: id, Properties: map[string]any{"traits": binds}}
	}
	refs := referencePositionsOf([]declarationVersion{
		kind(10, "ada.example.com/notes/note", "pinned"),
		kind(10, "ada.example.com/notes/page", "ada.example.com/notes/pinned"),
		trait(12, "ada.example.com/notes/pinned", "pinnedBy"),
		kind(20, "ada.example.com/notes/draft", "starred"),
		trait(21, "ada.example.com/notes/starred", "starredBy"),
		{Seq: 30, Ref: kindKind, ID: "ada.example.com/notes/draft", Deleted: true},
		{Seq: 31, Ref: kindTrait, ID: "ada.example.com/notes/starred", Deleted: true},
	}, localizeHome)
	for _, c := range []struct{ kind, want string }{
		{"~/notes/note", "pinnedBy"},   // bound bare, declared later in the kind's batch
		{"~/notes/page", "pinnedBy"},   // bound in full, the same
		{"~/notes/draft", "starredBy"}, // the kind and then its trait deleted since
	} {
		if got := refs.lastDeclared(c.kind); got[c.want] == nil {
			t.Errorf("a row of %s outside history holds no reference at %s: %v", c.kind, c.want, slices.Sorted(maps.Keys(got)))
		}
	}
}

// The first version of a shipped kind reads a line before it only where the
// engine wrote that line: the seed or the system hand, as the line names it
// when step 6 reads it, an actor record's id (conversion step 3). A line
// another hand wrote before any declaration of the kind, a published
// package's or the person's, still stops the conversion.
func TestAShippedKindsLineBeforeItsDeclarationIsReadOnlyWhereTheEngineWroteIt(t *testing.T) {
	unit := vocabulary.PackageCore + "/unit"
	workout := vocabulary.AuthorityPublisher + "/health/workout"
	reference := map[string]any{"type": "reference", "kind": unit}
	refs := referencePositionsOf([]declarationVersion{
		{Seq: 11, Ref: kindKind, ID: unit, Properties: map[string]any{"properties": map[string]any{"base": reference}}},
		{Seq: 40, Ref: kindKind, ID: workout, Properties: map[string]any{"properties": map[string]any{"unit": reference}}},
	}, localizeHome)
	for _, actor := range []substrate.Actor{substrate.ActorSeed, substrate.ActorSystem} {
		got, err := refs.at(unit, 5, 6, vocabulary.ActorID(actor))
		if err != nil {
			t.Errorf("a line of %s by %s before its declaration: %v", unit, actor, err)
		} else if _, ok := got["base"]; !ok {
			t.Errorf("a line of %s by %s holds no reference at base: %v", unit, actor, slices.Sorted(maps.Keys(got)))
		}
	}
	for _, c := range []struct {
		kind  string
		actor substrate.Actor
	}{
		{unit, substrate.ActorPerson},
		{unit, substrate.BundleActor(vocabulary.AuthorityPublisher, "health")},
		{workout, substrate.ActorPerson},
		{workout, substrate.BundleActor(vocabulary.AuthorityPublisher, "health")},
	} {
		if _, err := refs.at(c.kind, 5, 6, vocabulary.ActorID(c.actor)); err == nil || !strings.Contains(err.Error(), "no declaration of "+c.kind+" governs it") {
			t.Errorf("a line of %s by %s before any declaration of it: %v, want the conversion stopped", c.kind, c.actor, err)
		}
	}
	// A line from before the actor links, its hand spelled, as step 6 is
	// handed it: through every step below it, so its actor is the id step 3
	// linked.
	line := func(spelled string) error {
		e := &changelogfile.Entry{
			Seq: 5, Txn: 6, Op: "put", Kind: workout, RecordID: "w1", Actor: spelled, HasPrincipal: true,
			Payload: []byte(`{"fold":[{"delta":{"set":{"unit":{"ref":"` + unit + `/kg"}}},"id":"w1","kind":"record","ref":"` + workout + `","version":1}]}`),
		}
		for _, step := range conversionSteps {
			if step.Dialect >= localNamespaceDialect {
				break
			}
			if err := step.Line(e); err != nil {
				t.Fatalf("step %d: %v", step.Dialect, err)
			}
		}
		return localizeLine(localizeHome, refs, actorMoves{})(e)
	}
	for _, spelled := range []string{string(substrate.ActorSeed), string(substrate.ActorSystem)} {
		if err := line(spelled); err != nil {
			t.Errorf("a line of %s %s wrote before its declaration: %v", workout, spelled, err)
		}
	}
	for _, spelled := range []string{"console", string(substrate.BundleActor(vocabulary.AuthorityPublisher, "health"))} {
		if err := line(spelled); err == nil || !strings.Contains(err.Error(), "seq 5 writes a record of "+workout) {
			t.Errorf("a line of %s %s wrote before its declaration: %v, want the conversion stopped", workout, spelled, err)
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
			if _, err := refs.at(vocabulary.LocalizeRef(ef.Ref, m.Authority), e.Seq, e.Txn, e.Actor); err != nil {
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
