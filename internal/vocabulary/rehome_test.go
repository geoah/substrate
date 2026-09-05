package vocabulary

import (
	"reflect"
	"strings"
	"testing"
)

const placeholder = "samples.substrate.reamde.dev"

func rehomed(t *testing.T, docs []map[string]any, from, to string) []map[string]any {
	t.Helper()
	out, err := RehomeAuthority(docs, from, to)
	if err != nil {
		t.Fatalf("rehome: %v", err)
	}
	return out
}

// The rehome reaches every string a document carries, not a named list of
// fields: an id, the declared authority, a reference pin, an entry in
// `installs`, a function's `writes`, a trigger selector, a mapping's `to`,
// and the authority a function's SOURCE spells inside its own text, which is
// the one a field-by-field walk would leave behind.
func TestRehomeAuthorityReachesEveryString(t *testing.T) {
	docs := []map[string]any{{
		"kind":     "substrate.reamde.dev/core/kind",
		"metadata": map[string]any{"id": placeholder + "/tasks/task"},
		"data": map[string]any{
			"authority": placeholder,
			"package":   "tasks",
			"installs":  []any{placeholder + "/tasks/task"},
			"requires":  []any{placeholder + "/people"},
			"writes":    []any{placeholder + "/tasks/task"},
			"to":        placeholder + "/people/person",
			"source":    "TASK = \"" + placeholder + "/tasks/task\"\n",
			"kindDescriptions": map[string]any{
				placeholder + "/tasks/task": "something to do",
			},
		},
	}}
	got := rehomed(t, docs, placeholder, "ada.example.com")
	want := []map[string]any{{
		"kind":     "substrate.reamde.dev/core/kind",
		"metadata": map[string]any{"id": "ada.example.com/tasks/task"},
		"data": map[string]any{
			"authority": "ada.example.com",
			"package":   "tasks",
			"installs":  []any{"ada.example.com/tasks/task"},
			"requires":  []any{"ada.example.com/people"},
			"writes":    []any{"ada.example.com/tasks/task"},
			"to":        "ada.example.com/people/person",
			"source":    "TASK = \"ada.example.com/tasks/task\"\n",
			"kindDescriptions": map[string]any{
				"ada.example.com/tasks/task": "something to do",
			},
		},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rehomed to\n%#v\nwant\n%#v", got, want)
	}
	// The originals are untouched: the catalog holds one parsed copy of each
	// shipped closure and serves every repository from it.
	if id := docs[0]["metadata"].(map[string]any)["id"]; id != placeholder+"/tasks/task" {
		t.Errorf("the source document was rewritten in place: %v", id)
	}
}

// The core references a sample document carries are NOT the sample's
// authority, and rewriting one would point every declaration at a meta-kind
// that does not exist. Neither is a longer name that merely contains it.
func TestRehomeAuthorityLeavesOtherAuthoritiesAlone(t *testing.T) {
	docs := []map[string]any{{
		"kind": "substrate.reamde.dev/core/kind",
		"data": map[string]any{
			"mirror": "providers.substrate.reamde.dev/linear/issue",
			// Neither of these IS the authority: one is a longer name that
			// merely ends with it, the other a longer name that starts with it.
			"notMine":  "not" + placeholder,
			"deeper":   placeholder + ".example",
			"realPin":  placeholder + "/people/person",
			"withPort": placeholder + ":8080",
		},
	}}
	data, _ := rehomed(t, docs, placeholder, "ada.example.com")[0]["data"].(map[string]any)
	for key, want := range map[string]string{
		"mirror":   "providers.substrate.reamde.dev/linear/issue",
		"notMine":  "not" + placeholder,
		"deeper":   placeholder + ".example",
		"realPin":  "ada.example.com/people/person",
		"withPort": "ada.example.com:8080",
	} {
		if data[key] != want {
			t.Errorf("%s = %v, want %v", key, data[key], want)
		}
	}
}

// The import refuses on what this reports, so it has to find a mention
// wherever one hides, including a map KEY and a nested list, and it has to
// apply the SAME boundary the rewrite does. A string the walk deliberately
// left alone must not be reported: a document naming
// `notsamples.substrate.reamde.dev` would be refused forever, since no rewrite
// will ever touch it.
func TestAuthorityMentionsNamesTheDocumentsThatStillCarryIt(t *testing.T) {
	docs := []map[string]any{
		{
			"kind":     "substrate.reamde.dev/core/kind",
			"metadata": map[string]any{"id": "ada.example.com/tasks/task"},
			"data":     map[string]any{"authority": "ada.example.com"},
		},
		{
			"kind":     "substrate.reamde.dev/core/function",
			"metadata": map[string]any{"id": "ada.example.com/tasks/sync"},
			"data":     map[string]any{"rules": map[string]any{placeholder + "/tasks/task": "kept"}},
		},
		{
			"kind":     "substrate.reamde.dev/core/agent",
			"metadata": map[string]any{"id": "ada.example.com/tasks/triage"},
			"data":     map[string]any{"tools": []any{[]any{placeholder + "/tasks/task"}}},
		},
		// Two other authorities that merely contain this one's name. The walk
		// leaves both, so the refusal must leave both too.
		{
			"kind":     "substrate.reamde.dev/core/kind",
			"metadata": map[string]any{"id": "ada.example.com/tasks/mirror"},
			"data": map[string]any{
				"notMine": "not" + placeholder + "/tasks/task",
				"deeper":  placeholder + ".example/tasks/task",
			},
		},
	}
	got := AuthorityMentions(docs, placeholder)
	want := []string{"ada.example.com/tasks/sync", "ada.example.com/tasks/triage"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mentions = %v, want %v", got, want)
	}
	if left := AuthorityMentions(rehomed(t, docs, placeholder, "ada.example.com"), placeholder); len(left) != 0 {
		t.Errorf("the rehome left mentions behind: %v", left)
	}
}

// A map holding both a key that mentions the placeholder and the key it would
// be rewritten to has two entries for one name. Writing them into one map
// drops whichever landed first, silently, so the rehome refuses instead.
func TestRehomeAuthorityRefusesAKeyCollision(t *testing.T) {
	docs := []map[string]any{{
		"kind":     "substrate.reamde.dev/core/recordmapping",
		"metadata": map[string]any{"id": placeholder + "/tasks/fromlinear"},
		"data": map[string]any{
			"map": map[string]any{
				placeholder + "/tasks/task":  "from the sample",
				"ada.example.com/tasks/task": "already the repository's",
			},
		},
	}}
	_, err := RehomeAuthority(docs, placeholder, "ada.example.com")
	if err == nil {
		t.Fatal("the rehome collapsed two keys into one without saying so")
	}
	if !strings.Contains(err.Error(), "ada.example.com/tasks/task") {
		t.Errorf("the refusal does not name the colliding key: %v", err)
	}
}
