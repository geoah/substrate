package testenv_test

// `body` is an ordinary property (decision 0273): a check, an order, a filter
// and a search answer it exactly as they answer a property of another name.
// Each test declares one kind twice, `letter` carrying `body` and `notice`
// carrying `content` with the same declaration, and holds the two to the same
// answers over HTTP against the real engine.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/testenv"
)

const bodyAuthority = "bodyproperty.example.com"

// twinKinds declares package pkg with a letter whose `body` and a notice
// whose `content` are both declared as decl, each beside a `name`.
func twinKinds(pkg, decl string) string {
	ref := bodyAuthority + "/" + pkg
	kind := func(singular, prop string) string {
		return `
kind: substrate.reamde.dev/core/kind
metadata:
  id: ` + ref + `/` + singular + `
data:
  authority: ` + bodyAuthority + `
  package: ` + pkg + `
  names:
    singular: ` + singular + `
  displayTemplate: "{name}"
  properties:
    name:
      type: string
      description: what the record is called
    ` + prop + `:
` + decl
	}
	return `
kind: substrate.reamde.dev/core/package
metadata:
  id: ` + ref + `
data:
  authority: ` + bodyAuthority + `
  package: ` + pkg + `
---` + kind("letter", "body") + `---` + kind("notice", "content")
}

// twin is one of the two kinds and the name its text property goes by.
type twin struct{ kind, prop string }

func twinsOf(pkg string) []twin {
	ref := bodyAuthority + "/" + pkg
	return []twin{{ref + "/letter", "body"}, {ref + "/notice", "content"}}
}

// create writes one record through the API and answers the status and the
// error code, if any.
func create(t *testing.T, e *testenv.Env, kind string, props map[string]any) (int, string) {
	t.Helper()
	status, raw := e.Do(http.MethodPost, "/api/v1/records", map[string]any{"kind": kind, "properties": props})
	var out struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &out)
	return status, out.Error.Code
}

// A body declared `required`, with a `pattern`, or as a number with a `min`
// refuses the write that breaks it, as a property of another name does. A
// text property takes no `min`, whatever its name: the bound is a number's.
func TestBodyTakesTheChecksEveryPropertyTakes(t *testing.T) {
	e := testenv.Start(t, testenv.WithRepository(bodyAuthority))
	for _, c := range []struct {
		pkg, decl     string
		refused, kept any
	}{
		{"required", "      type: text\n      required: true\n", nil, "a text"},
		{"pattern", "      type: text\n      pattern: \"^[A-Z]\"\n", "lowercase first", "Uppercase first"},
		{"minimum", "      type: int\n      min: 5\n", 3, 7},
	} {
		e.ApplyVocabularyYAML(twinKinds(c.pkg, c.decl))
		for _, tw := range twinsOf(c.pkg) {
			props := map[string]any{"name": "refused"}
			if c.refused != nil {
				props[tw.prop] = c.refused
			}
			if status, code := create(t, e, tw.kind, props); status != http.StatusUnprocessableEntity || code != "validation" {
				t.Errorf("%s: %s %v answered %d %s, want 422 validation", c.pkg, tw.prop, c.refused, status, code)
			}
			if status, code := create(t, e, tw.kind, map[string]any{"name": "kept", tw.prop: c.kept}); status != http.StatusCreated {
				t.Errorf("%s: %s %v answered %d %s, want 201", c.pkg, tw.prop, c.kept, status, code)
			}
		}
	}
}

// listNames answers the `name` of every record a list of kind returns, in
// the list's order.
func listNames(t *testing.T, e *testenv.Env, kind string, props map[string]any, orderBy string) []string {
	t.Helper()
	f := map[string]any{"kinds": []string{kind}}
	if props != nil {
		f["properties"] = props
	}
	raw, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/records?filter=" + url.QueryEscape(string(raw)) + "&first=50"
	if orderBy != "" {
		path += "&orderBy=" + url.QueryEscape(orderBy)
	}
	status, body := e.Do(http.MethodGet, path, nil)
	if status != http.StatusOK {
		t.Fatalf("list %s %v %s: %d %s", kind, props, orderBy, status, body)
	}
	var page struct {
		Records []struct {
			Properties map[string]any `json:"properties"`
		} `json:"records"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(page.Records))
	for _, r := range page.Records {
		out = append(out, fmt.Sprint(r.Properties["name"]))
	}
	return out
}

// Sorting and filtering by `body` give the answers sorting and filtering by
// another long-text property give, operator by operator.
func TestBodySortsAndFiltersAsAnotherLongTextProperty(t *testing.T) {
	e := testenv.Start(t, testenv.WithRepository(bodyAuthority))
	e.ApplyVocabularyYAML(twinKinds("order", "      type: text\n"))
	texts := map[string]any{
		"d": "delta dives into the river",
		"a": "alpha arrives at noon",
		"c": "charlie climbs the ridge",
		"b": "bravo bends the bow",
		"e": nil,
	}
	twins := twinsOf("order")
	for _, tw := range twins {
		for name, text := range texts {
			props := map[string]any{"name": name}
			if text != nil {
				props[tw.prop] = text
			}
			if status, code := create(t, e, tw.kind, props); status != http.StatusCreated {
				t.Fatalf("create %s %s: %d %s", tw.kind, name, status, code)
			}
		}
	}
	for _, c := range []struct {
		what    string
		cond    map[string]any
		orderBy string
		want    []string
	}{
		{"ascending", nil, "%s", nil},
		{"descending", nil, "%s:desc", nil},
		{"eq", map[string]any{"eq": "bravo bends the bow"}, "", []string{"b"}},
		{"in", map[string]any{"in": []string{"alpha arrives at noon", "charlie climbs the ridge"}}, "", []string{"a", "c"}},
		{"prefix", map[string]any{"prefix": "del"}, "", []string{"d"}},
		{"gt", map[string]any{"gt": "bravo"}, "", []string{"b", "c", "d"}},
		{"match", map[string]any{"match": "climb*"}, "", []string{"c"}},
		{"exists", map[string]any{"exists": true}, "", []string{"a", "b", "c", "d"}},
		{"absent", map[string]any{"exists": false}, "", []string{"e"}},
	} {
		var answers [][]string
		for _, tw := range twins {
			var props map[string]any
			if c.cond != nil {
				props = map[string]any{tw.prop: c.cond}
			}
			orderBy := ""
			if c.orderBy != "" {
				orderBy = fmt.Sprintf(c.orderBy, tw.prop)
			}
			got := listNames(t, e, tw.kind, props, orderBy)
			if orderBy == "" {
				slices.Sort(got)
			}
			answers = append(answers, got)
		}
		if !slices.Equal(answers[0], answers[1]) {
			t.Errorf("%s: body answered %v and content %v", c.what, answers[0], answers[1])
		}
		if c.want != nil && !slices.Equal(answers[0], c.want) {
			t.Errorf("%s: answered %v, want %v", c.what, answers[0], c.want)
		}
		if c.orderBy != "" && len(answers[0]) != len(texts) {
			t.Errorf("%s: ordered %v, want every record", c.what, answers[0])
		}
	}
}

// A word in a record's body finds it as a word in another long-text property
// finds its record, ranked the same.
func TestBodyIsFoundAsAnotherLongTextPropertyIs(t *testing.T) {
	e := testenv.Start(t, testenv.WithRepository(bodyAuthority))
	e.ApplyVocabularyYAML(twinKinds("search", "      type: text\n"))
	twins := twinsOf("search")
	for _, tw := range twins {
		for name, text := range map[string]string{
			"one":   "a falcon stoops over the moor",
			"two":   "the falcon and the falcon's mate",
			"three": "nothing about birds of prey",
		} {
			if status, code := create(t, e, tw.kind, map[string]any{"name": name, tw.prop: text}); status != http.StatusCreated {
				t.Fatalf("create %s %s: %d %s", tw.kind, name, status, code)
			}
		}
	}
	var answers [][]string
	for _, tw := range twins {
		f, _ := json.Marshal(map[string]any{"kinds": []string{tw.kind}})
		status, body := e.Do(http.MethodGet, "/api/v1/records?q=falcon&mode=lexical&filter="+url.QueryEscape(string(f)), nil)
		if status != http.StatusOK {
			t.Fatalf("search %s: %d %s", tw.kind, status, body)
		}
		var page struct {
			Records []struct {
				Properties map[string]any `json:"properties"`
			} `json:"records"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, r := range page.Records {
			names = append(names, fmt.Sprint(r.Properties["name"]))
		}
		answers = append(answers, names)
	}
	if len(answers[0]) != 2 || !slices.Equal(answers[0], answers[1]) {
		t.Fatalf("a word found %s by body and %s by content, want the same two records in the same order",
			strings.Join(answers[0], ","), strings.Join(answers[1], ","))
	}
}
