package vocabulary_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

const home = "ada.example.com"

// The repository's name is another spelling of `~` in a reference, and only
// there: a reference under another authority, or one whose authority merely
// starts with the name, is left as it is (decision record 0259).
func TestLocalizeRefFoldsTheNameOnlyAsAWholeAuthority(t *testing.T) {
	for in, want := range map[string]string{
		"ada.example.com/tasks/task":            "~/tasks/task",
		"ada.example.com/tasks":                 "~/tasks",
		"ada.example.com/*":                     "~/*",
		"ada.example.com/tasks/task/01jbx":      "~/tasks/task/01jbx",
		"~/tasks/task":                          "~/tasks/task",
		"ada.example.com":                       "ada.example.com",
		"ada.example.com.evil/tasks/task":       "ada.example.com.evil/tasks/task",
		"bada.example.com/tasks/task":           "bada.example.com/tasks/task",
		"providers.substrate.reamde.dev/google": "providers.substrate.reamde.dev/google",
	} {
		if got := vocabulary.LocalizeRef(in, home); got != want {
			t.Errorf("LocalizeRef(%q) = %q, want %q", in, got, want)
		}
	}
	if got := vocabulary.LocalizeRef("ada.example.com/tasks/task", ""); got != "ada.example.com/tasks/task" {
		t.Errorf("an empty name folded %q", got)
	}
}

// A record path folds its kind, and its id where the kind's records are named
// by a reference: a pointer at the repository's own agent is a pointer at the
// `~` one. A data record's id is the writer's and stays.
func TestLocalizePathFoldsADeclarationsIDToo(t *testing.T) {
	for in, want := range map[string]string{
		"substrate.reamde.dev/core/agent/ada.example.com/chat/assistant": "substrate.reamde.dev/core/agent/~/chat/assistant",
		"ada.example.com/tasks/task/ada.example.com/x":                   "~/tasks/task/ada.example.com/x",
		"substrate.reamde.dev/core/contact/ada.example.com/x":            "substrate.reamde.dev/core/contact/ada.example.com/x",
		"~/tasks/task/01jbx": "~/tasks/task/01jbx",
	} {
		if got := vocabulary.LocalizePath(in, home); got != want {
			t.Errorf("LocalizePath(%q) = %q, want %q", in, got, want)
		}
	}
}

// In a copied function's source or agent's prompt, a kind reference spelled
// under the sample's authority moves to `~`, and nothing else does: an
// address keeps the name, wherever it stands in it (a URL's host or path,
// userinfo, a port, an email address), and so do a bare mention, a mention
// with fewer than two words after it, and an actor text.
func TestLocalizeKindRefsMovesOnlyASpelledKindReference(t *testing.T) {
	const sample = "samples.substrate.reamde.dev"
	for in, want := range map[string]string{
		`host.effects.put("samples.substrate.reamde.dev/tasks/task", x)`:      `host.effects.put("~/tasks/task", x)`,
		"kind samples.substrate.reamde.dev/readinglist/page plus its id":      "kind ~/readinglist/page plus its id",
		"list samples.substrate.reamde.dev/llm/scratchpad.":                   "list ~/llm/scratchpad.",
		"read:samples.substrate.reamde.dev/tasks/task":                        "read:samples.substrate.reamde.dev/tasks/task",
		"fetch https://samples.substrate.reamde.dev/feed/items":               "fetch https://samples.substrate.reamde.dev/feed/items",
		"fetch https://user@samples.substrate.reamde.dev/feed/items":          "fetch https://user@samples.substrate.reamde.dev/feed/items",
		"fetch https://example.com/samples.substrate.reamde.dev/a/b":          "fetch https://example.com/samples.substrate.reamde.dev/a/b",
		"samples.substrate.reamde.dev:8080/feed/items":                        "samples.substrate.reamde.dev:8080/feed/items",
		"samples.substrate.reamde.dev/feed":                                   "samples.substrate.reamde.dev/feed",
		"samples.substrate.reamde.dev/feed/items/more":                        "samples.substrate.reamde.dev/feed/items/more",
		"samples.substrate.reamde.dev/feed/items.json":                        "samples.substrate.reamde.dev/feed/items.json",
		"samples.substrate.reamde.dev/Feed/items":                             "samples.substrate.reamde.dev/Feed/items",
		"write to person@samples.substrate.reamde.dev":                        "write to person@samples.substrate.reamde.dev",
		"samples.substrate.reamde.dev":                                        "samples.substrate.reamde.dev",
		"bundle:samples.substrate.reamde.dev:tasks":                           "bundle:samples.substrate.reamde.dev:tasks",
		"notsamples.substrate.reamde.dev/tasks/task":                          "notsamples.substrate.reamde.dev/tasks/task",
		"substrate.reamde.dev/core/function/samples.substrate.reamde.dev/x/y": "substrate.reamde.dev/core/function/samples.substrate.reamde.dev/x/y",
	} {
		if got := vocabulary.LocalizeKindRefs(in, sample); got != want {
			t.Errorf("LocalizeKindRefs(%q) = %q, want %q", in, got, want)
		}
	}
}

// Outside a copied declaration's code texts, the sample's authority is read
// only where the grammar names something: prose, a default and a URL in a
// declaration keep it as written, and a function's source moves only its
// kind references.
func TestACopyReadsTheSamplesNameOnlyWhereItNamesSomething(t *testing.T) {
	const sample = "samples.substrate.reamde.dev"
	docs := []map[string]any{
		{"kind": "substrate.reamde.dev/core/kind", "metadata": map[string]any{"id": sample + "/feeds/feed"}, "data": map[string]any{
			"authority": sample, "package": "feeds",
			"description": "a feed, as https://" + sample + "/feeds serves one; mail person@" + sample,
			"properties": map[string]any{
				"url":    map[string]any{"type": "string", "default": sample + ":8080/feed/items"},
				"author": map[string]any{"type": "reference", "kind": sample + "/people/person"},
			},
		}},
		{"kind": "substrate.reamde.dev/core/function", "metadata": map[string]any{"id": sample + "/feeds/fetch"}, "data": map[string]any{
			"authority": sample, "package": "feeds",
			"permissions": map[string]any{"writes": []any{sample + "/feeds/feed"}, "network": []any{sample}},
			"source":      `URL = "https://` + sample + `/feed/items"` + "\n" + `host.effects.put("` + sample + `/feeds/feed", i)`,
		}},
	}
	got, err := vocabulary.LocalizeAuthority(docs, sample, home)
	if err != nil {
		t.Fatal(err)
	}
	kind, fn := got[0]["data"].(map[string]any), got[1]["data"].(map[string]any)
	props := kind["properties"].(map[string]any)
	if got[0]["metadata"].(map[string]any)["id"] != "~/feeds/feed" || kind["authority"] != "~" ||
		props["author"].(map[string]any)["kind"] != "~/people/person" {
		t.Errorf("the kind's names did not move: %v", got[0])
	}
	if kind["description"] != docs[0]["data"].(map[string]any)["description"] ||
		props["url"].(map[string]any)["default"] != sample+":8080/feed/items" {
		t.Errorf("the kind's text moved: %v", kind)
	}
	if w := fn["permissions"].(map[string]any)["writes"].([]any)[0]; w != "~/feeds/feed" {
		t.Errorf("the function's grant is %v, want ~/feeds/feed", w)
	}
	if n := fn["permissions"].(map[string]any)["network"].([]any)[0]; n != sample {
		t.Errorf("the function's network host is %v, want it as written", n)
	}
	if want := `URL = "https://` + sample + `/feed/items"` + "\n" + `host.effects.put("~/feeds/feed", i)`; fn["source"] != want {
		t.Errorf("the source is\n%s\nwant\n%s", fn["source"], want)
	}
}

// Copying a sample moves its references into `~`, sets its declarations'
// authority to `~` and leaves the sample authority's own authority document
// behind. Admitting the repository's own declarations under its name folds
// them the same way and keeps its own authority document as written.
func TestLocalizeAuthorityCopiesASampleAndAdmitsTheOwnersDeclarations(t *testing.T) {
	const sample = "samples.substrate.reamde.dev"
	docs := []map[string]any{
		{"kind": "substrate.reamde.dev/core/authority", "metadata": map[string]any{"id": sample}, "data": map[string]any{"description": "the samples"}},
		{"kind": "substrate.reamde.dev/core/kind", "metadata": map[string]any{"id": sample + "/tasks/task"}, "data": map[string]any{
			"authority": sample, "package": "tasks",
			"properties": map[string]any{"owner": map[string]any{"type": "reference", "kind": sample + "/people/person"}},
		}},
	}
	got, err := vocabulary.LocalizeAuthority(docs, sample, home)
	if err != nil {
		t.Fatal(err)
	}
	want := []map[string]any{
		{"kind": "substrate.reamde.dev/core/kind", "metadata": map[string]any{"id": "~/tasks/task"}, "data": map[string]any{
			"authority": "~", "package": "tasks",
			"properties": map[string]any{"owner": map[string]any{"type": "reference", "kind": "~/people/person"}},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the copy:\n got  %v\n want %v", got, want)
	}

	own := []map[string]any{
		{"kind": "substrate.reamde.dev/core/authority", "metadata": map[string]any{"id": home}, "data": map[string]any{"description": "Ada's"}},
		{"kind": "substrate.reamde.dev/core/package", "metadata": map[string]any{"id": home + "/notes"}, "data": map[string]any{"authority": home, "package": "notes"}},
	}
	got, err = vocabulary.LocalizeAuthority(own, home, home)
	if err != nil {
		t.Fatal(err)
	}
	if got[0]["metadata"].(map[string]any)["id"] != home || got[1]["metadata"].(map[string]any)["id"] != "~/notes" ||
		got[1]["data"].(map[string]any)["authority"] != "~" {
		t.Errorf("the owner's declarations: %v", got)
	}
}

// Admitting the owner's own declarations moves the name only at the keys that
// name something: a declaration's id, its `authority`, a reference's pin, a
// kind's traits, a grant's kinds, an agent's tools and sub-agents. The
// owner's text of the same shape stays as written: a description, a
// property's default, an agent's prompt, a function's source, a mapping's
// filter (decision record 0259).
func TestTheOwnersDeclarationsMoveOnlyWhereTheyNameSomething(t *testing.T) {
	docs := []map[string]any{
		{"kind": "substrate.reamde.dev/core/kind", "metadata": map[string]any{"id": home + "/notes/note"}, "data": map[string]any{
			"authority": home, "package": "notes", "description": "see ada.example.com/notes/note",
			"traits": []any{home + "/notes/dated(start)"},
			"properties": map[string]any{
				"about": map[string]any{"type": "reference", "kind": home + "/people/person", "description": "ada.example.com/people/person"},
				"where": map[string]any{"type": "string", "default": home + "/somewhere"},
			},
		}},
		{"kind": "substrate.reamde.dev/core/function", "metadata": map[string]any{"id": home + "/notes/stamp"}, "data": map[string]any{
			"authority": home, "package": "notes",
			"source":      `host.records.put("ada.example.com/notes/note", {})`,
			"permissions": map[string]any{"writes": []any{home + "/notes/note"}, "reads": map[string]any{"kinds": []any{home + "/people/*"}}, "call": []any{home + "/notes/other"}},
		}},
		{"kind": "substrate.reamde.dev/core/agent", "metadata": map[string]any{"id": home + "/notes/scribe"}, "data": map[string]any{
			"authority": home, "package": "notes", "prompt": "write to ada.example.com/notes/note",
			"tools": []any{map[string]any{"function": home + "/notes/stamp", "description": "ada.example.com/notes/stamp"}},
			"subagents": []any{
				home + "/notes/helper",
				map[string]any{"ref": home + "/notes/digger", "background": true},
				map[string]any{"ref": "substrate.reamde.dev/core/agent/" + home + "/notes/stored"},
			},
		}},
		{"kind": "substrate.reamde.dev/core/recordmapping", "metadata": map[string]any{"id": home + "/notes/fromcontact"}, "data": map[string]any{
			"authority": home, "package": "notes", "from": "substrate.reamde.dev/core/contact", "to": home + "/people/person",
			"where": map[string]any{"kind": home + "/people/person"},
		}},
	}
	got, err := vocabulary.LocalizeAuthority(docs, home, home)
	if err != nil {
		t.Fatal(err)
	}
	want := []map[string]any{
		{"kind": "substrate.reamde.dev/core/kind", "metadata": map[string]any{"id": "~/notes/note"}, "data": map[string]any{
			"authority": "~", "package": "notes", "description": "see ada.example.com/notes/note",
			"traits": []any{"~/notes/dated(start)"},
			"properties": map[string]any{
				"about": map[string]any{"type": "reference", "kind": "~/people/person", "description": "ada.example.com/people/person"},
				"where": map[string]any{"type": "string", "default": home + "/somewhere"},
			},
		}},
		{"kind": "substrate.reamde.dev/core/function", "metadata": map[string]any{"id": "~/notes/stamp"}, "data": map[string]any{
			"authority": "~", "package": "notes",
			"source":      `host.records.put("ada.example.com/notes/note", {})`,
			"permissions": map[string]any{"writes": []any{"~/notes/note"}, "reads": map[string]any{"kinds": []any{"~/people/*"}}, "call": []any{"~/notes/other"}},
		}},
		{"kind": "substrate.reamde.dev/core/agent", "metadata": map[string]any{"id": "~/notes/scribe"}, "data": map[string]any{
			"authority": "~", "package": "notes", "prompt": "write to ada.example.com/notes/note",
			"tools": []any{map[string]any{"function": "~/notes/stamp", "description": "ada.example.com/notes/stamp"}},
			"subagents": []any{
				"~/notes/helper",
				map[string]any{"ref": "~/notes/digger", "background": true},
				map[string]any{"ref": "substrate.reamde.dev/core/agent/~/notes/stored"},
			},
		}},
		{"kind": "substrate.reamde.dev/core/recordmapping", "metadata": map[string]any{"id": "~/notes/fromcontact"}, "data": map[string]any{
			"authority": "~", "package": "notes", "from": "substrate.reamde.dev/core/contact", "to": "~/people/person",
			"where": map[string]any{"kind": home + "/people/person"},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the owner's declarations:\n got  %v\n want %v", got, want)
	}
}

// The grammar decides, not a key's spelling: a property the author named
// `source`, `label`, `where` or `default` is a property, and its reference pin
// moves to `~`; a function's `source` is its text, and stays as written.
func TestADeclarationIsReadByItsGrammar(t *testing.T) {
	docs := []map[string]any{
		{"kind": "substrate.reamde.dev/core/kind", "metadata": map[string]any{"id": home + "/notes/note"}, "data": map[string]any{
			"authority": home, "package": "notes",
			"properties": map[string]any{
				"source":  map[string]any{"type": "reference", "kind": home + "/notes/note"},
				"label":   map[string]any{"type": "reference", "kind": home + "/notes/tag", "description": "see ada.example.com/notes/tag"},
				"where":   map[string]any{"type": "object", "fields": map[string]any{"default": map[string]any{"type": "reference", "kind": home + "/places/place"}}},
				"default": map[string]any{"type": "string", "default": home + "/notes/note"},
			},
		}},
		{"kind": "substrate.reamde.dev/core/function", "metadata": map[string]any{"id": home + "/notes/stamp"}, "data": map[string]any{
			"authority": home, "package": "notes", "source": `host.records.put("ada.example.com/notes/note", {})`,
		}},
	}
	got, err := vocabulary.LocalizeAuthority(docs, home, home)
	if err != nil {
		t.Fatal(err)
	}
	props := got[0]["data"].(map[string]any)["properties"].(map[string]any)
	for _, c := range []struct {
		got  any
		want string
	}{
		{props["source"].(map[string]any)["kind"], "~/notes/note"},
		{props["label"].(map[string]any)["kind"], "~/notes/tag"},
		{props["label"].(map[string]any)["description"], "see ada.example.com/notes/tag"},
		{props["where"].(map[string]any)["fields"].(map[string]any)["default"].(map[string]any)["kind"], "~/places/place"},
		{props["default"].(map[string]any)["default"], home + "/notes/note"},
		{got[1]["data"].(map[string]any)["source"], `host.records.put("ada.example.com/notes/note", {})`},
	} {
		if c.got != c.want {
			t.Errorf("got %v, want %v", c.got, c.want)
		}
	}
}

// Two `requiresAtLeast` entries that become one package under `~` are
// refused: keeping either would drop the other's floor.
func TestTwoSpellingsOfOneRequiredPackageAreRefused(t *testing.T) {
	data := map[string]any{"requires": []any{"~/tasks"}, "requiresAtLeast": map[string]any{home + "/tasks": 10, "~/tasks": 1}}
	if got, err := vocabulary.LocalizeDeclaration(vocabulary.CoreKind(vocabulary.DocBundle), data, home); err == nil || !strings.Contains(err.Error(), "requiresAtLeast names ~/tasks twice") {
		t.Fatalf("two floors for one package: %v, %v; want them refused", got, err)
	}
	docs := []map[string]any{{"kind": "substrate.reamde.dev/core/bundle", "metadata": map[string]any{"id": home + "/crew"}, "data": data}}
	if _, err := vocabulary.LocalizeAuthority(docs, home, home); err == nil {
		t.Fatal("admitting a bundle with two floors for one package succeeded")
	}
	one, err := vocabulary.LocalizeDeclaration(vocabulary.CoreKind(vocabulary.DocBundle), map[string]any{"requiresAtLeast": map[string]any{home + "/tasks": 10}}, home)
	if err != nil || one.(map[string]any)["requiresAtLeast"].(map[string]any)["~/tasks"] != 10 {
		t.Errorf("one floor under the name: %v, %v; want it under ~", one, err)
	}
}

// Copying a sample keeps a URL's host as written, whatever the sample's
// authority and the repository's name: a function fetching from the sample's
// host still fetches from it.
func TestASampleCopyKeepsAURLsHost(t *testing.T) {
	const sample = "samples.substrate.reamde.dev"
	docs := []map[string]any{{"kind": "substrate.reamde.dev/core/function", "metadata": map[string]any{"id": sample + "/feeds/pull"}, "data": map[string]any{
		"authority": sample, "package": "feeds",
		"source": `fetch("https://samples.substrate.reamde.dev/feed"); host.records.put("samples.substrate.reamde.dev/feeds/item", {})`,
	}}}
	got, err := vocabulary.LocalizeAuthority(docs, sample, home)
	if err != nil {
		t.Fatal(err)
	}
	if source := got[0]["data"].(map[string]any)["source"]; source != `fetch("https://samples.substrate.reamde.dev/feed"); host.records.put("~/feeds/item", {})` {
		t.Errorf("the copy's source = %v, want the URL's host kept and the kind under ~", source)
	}
}
