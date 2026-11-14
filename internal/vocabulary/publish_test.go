package vocabulary

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Publishing moves a local package's own documents whole to the authority it
// is published under, and another local package's only where it names the
// package; the author's text stays as written, and a name in the package of
// another local package is a problem, not a move (decision record 0289).
func TestPublishDocumentsMovesThePackageAndWhatNamesIt(t *testing.T) {
	const to, home = "pub.example.com", "ada.example.com"
	docs := []map[string]any{
		PackageManifest("~/recipes", 1),
		KindManifest("~/recipes", map[string]any{"singular": "recipe"}, map[string]any{
			"description": "a recipe in ~/recipes/recipe, as the author wrote",
			"properties": map[string]any{
				"main": map[string]any{"type": "reference", "kind": "~/recipes/ingredient"},
			},
		}),
		FunctionManifest("~/recipes", "add", map[string]any{
			"runtime":     RuntimePython,
			"permissions": map[string]any{"writes": []any{map[string]any{"ref": CoreKind(DocKind) + "/~/recipes/recipe"}}},
			"source":      `put("~/recipes/recipe"); put("` + home + `/recipes/recipe"); url("https://example.com/~/recipes/x")`,
		}),
		KindManifest("~/notes", map[string]any{"singular": "note"}, map[string]any{
			"properties": map[string]any{"recipe": map[string]any{"type": "reference", "kind": "~/recipes/recipe"}},
		}),
		KindManifest("~/tasks", map[string]any{"singular": "task"}, map[string]any{
			"properties": map[string]any{"name": map[string]any{"type": "string"}},
		}),
	}
	out, problems, err := PublishDocuments(docs, "recipes", to, []string{home})
	if err != nil || len(problems) != 0 {
		t.Fatalf("publish: %v %v", problems, err)
	}
	byID := map[string]map[string]any{}
	for _, d := range out {
		byID[mapPathString(d, "metadata", "id")] = d
	}
	if _, ok := byID["~/tasks/task"]; ok {
		t.Error("a package that does not name the published one was moved")
	}
	for _, id := range []string{to + "/recipes", to + "/recipes/recipe", to + "/recipes/add", "~/notes/note"} {
		if byID[id] == nil {
			t.Fatalf("no document %s among %v", id, keysOf(byID))
		}
	}
	kind := byID[to+"/recipes/recipe"]
	if got := mapPath(kind, "data", "authority"); got != to {
		t.Errorf("the kind's authority is %v", got)
	}
	if got := mapPath(kind, "data", "properties", "main", "kind"); got != to+"/recipes/ingredient" {
		t.Errorf("the kind's reference pin is %v", got)
	}
	if got := mapPath(kind, "data", "description"); !strings.Contains(got.(string), "~/recipes/recipe") {
		t.Errorf("the author's text moved: %v", got)
	}
	fn := byID[to+"/recipes/add"]
	if got := mapPath(fn, "data", "permissions", "writes"); !reflect.DeepEqual(got, []any{map[string]any{"ref": CoreKind(DocKind) + "/" + to + "/recipes/recipe"}}) {
		t.Errorf("the function's grant is %v", got)
	}
	src := mapPathString(fn, "data", "source")
	if !strings.Contains(src, `put("`+to+`/recipes/recipe"); put("`+to+`/recipes/recipe")`) || !strings.Contains(src, "https://example.com/~/recipes/x") {
		t.Errorf("the function's source is %q: both spellings of the kind move, the URL stays", src)
	}
	if got := mapPath(byID["~/notes/note"], "data", "properties", "recipe", "kind"); got != to+"/recipes/recipe" {
		t.Errorf("another package's pin at the published kind is %v", got)
	}
}

// A published package is installed where the other local packages are not,
// so naming one is refused, line by line.
func TestPublishDocumentsRefusesAPackageThatNamesAnotherLocalOne(t *testing.T) {
	docs := []map[string]any{
		PackageManifest("~/recipes", 1),
		KindManifest("~/recipes", map[string]any{"singular": "recipe"}, map[string]any{
			"properties": map[string]any{"task": map[string]any{"type": "reference", "kind": "~/tasks/task"}},
		}),
		FunctionManifest("~/recipes", "add", map[string]any{"runtime": RuntimePython, "source": `put("~/notes/note")`}),
	}
	_, problems, err := PublishDocuments(docs, "recipes", "pub.example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(problems, "\n")
	if len(problems) != 2 || !strings.Contains(joined, "~/tasks") || !strings.Contains(joined, "~/notes") {
		t.Fatalf("problems: %v, want ~/tasks and ~/notes named", problems)
	}
}

func keysOf(m map[string]map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// Every position class a declaration names something at has the publish's
// answer, so a class added later is a choice its author makes.
func TestEveryNamingPositionHasAPublishAnswer(t *testing.T) {
	names := func(pos position) bool {
		switch pos {
		case posWord, posText, posObject, posNamed:
			return false
		}
		return true
	}
	for set, keys := range declarationPositions {
		for key, c := range keys {
			if !names(c.pos) {
				continue
			}
			if _, ok := publishDeclarationAnswers[c.pos]; !ok {
				t.Errorf("%s.%s names something at a position class (%d) the publish gives no answer: add it to publishDeclarationAnswers", set, key, c.pos)
			}
		}
	}
}

// Code moves with the publish wherever it is, a bundle's shared modules
// beside a function's source, and a sample's copy moves both; a pattern over
// every package of `~` in another of the repository's packages keeps the
// published package beside it.
func TestPublishMovesCodeAndKeepsAPatternOverTheLocalNamespace(t *testing.T) {
	const to = "pub.example.com"
	docs := []map[string]any{
		PackageManifest("~/recipes", 1),
		KindManifest("~/recipes", map[string]any{"singular": "recipe"}, map[string]any{
			"properties": map[string]any{"name": map[string]any{"type": "string"}},
		}),
		BundleManifest("~/recipes", map[string]any{
			"installs": []any{CoreKind(DocKind) + "/~/recipes/recipe"},
			"modules":  map[string]any{"shared.py": `KIND = "~/recipes/recipe"`},
		}),
		FunctionManifest("~/tasks", "sweep", map[string]any{
			"runtime":     RuntimePython,
			"permissions": map[string]any{"writes": []any{"~/*"}},
			"source":      `def main(input, host): return {}`,
		}),
	}
	out, problems, err := PublishDocuments(docs, "recipes", to, nil)
	if err != nil || len(problems) != 0 {
		t.Fatalf("publish: %v %v", problems, err)
	}
	byID := map[string]map[string]any{}
	for _, d := range out {
		byID[mapPathString(d, "metadata", "id")] = d
	}
	if got := mapPathString(byID[to+"/recipes"], "data", "modules", "shared.py"); got != `KIND = "`+to+`/recipes/recipe"` {
		t.Errorf("the bundle's module is %q, want the kind it names moved", got)
	}
	if got := mapPath(byID["~/tasks/sweep"], "data", "permissions", "writes"); !reflect.DeepEqual(got, []any{"~/*", to + "/recipes/*"}) {
		t.Errorf("the other package's grant is %v, want ~/* with the published package beside it", got)
	}

	copied, err := localizeCopiedDocument(BundleManifest("samples.example.com/recipes", map[string]any{
		"modules": map[string]any{"shared.py": `KIND = "samples.example.com/recipes/recipe"`},
	}), "samples.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got := mapPathString(copied, "data", "modules", "shared.py"); got != `KIND = "~/recipes/recipe"` {
		t.Errorf("the copied module is %q, want the sample's kind moved to ~", got)
	}
}

// An agent's prompt is its author's prose: the publish leaves it as
// written, and lists every prompt that still names the package; a sample's
// copy moves the kinds a prompt names, as before.
func TestAPromptIsLeftByAPublishAndListed(t *testing.T) {
	prompt := "Keep ~/recipes/recipe tidy."
	docs := []map[string]any{
		PackageManifest("~/recipes", 1),
		AgentManifest("~/recipes", "chef", map[string]any{"prompt": prompt}),
		AgentManifest("~/notes", "scribe", map[string]any{"prompt": "Read ~/recipes/recipe when asked."}),
		AgentManifest("~/notes", "clerk", map[string]any{"prompt": "Nothing of the recipes."}),
	}
	out, _, err := PublishDocuments(docs, "recipes", "pub.example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range out {
		if mapPathString(d, "metadata", "id") == "pub.example.com/recipes/chef" && mapPathString(d, "data", "prompt") != prompt {
			t.Errorf("the publish rewrote the prompt: %q", mapPathString(d, "data", "prompt"))
		}
	}
	want := []string{CoreKind(DocAgent) + "/~/notes/scribe", CoreKind(DocAgent) + "/~/recipes/chef"}
	// The package by its name alone, and under a name the repository used
	// to answer to, is named as surely as a kind of it.
	docs = append(docs,
		AgentManifest("~/notes", "porter", map[string]any{"prompt": "Use `~/recipes`."}),
		AgentManifest("~/notes", "usher", map[string]any{"prompt": "Read former.example.com/recipes/recipe/r1."}))
	want = append(want, CoreKind(DocAgent)+"/~/notes/porter", CoreKind(DocAgent)+"/~/notes/usher")
	sort.Strings(want)
	if got := PublishTexts(docs, "recipes", "pub.example.com", []string{"former.example.com"}); !reflect.DeepEqual(got, want) {
		t.Errorf("the listed prompts are %v, want %v", got, want)
	}
	copied, err := localizeCopiedDocument(AgentManifest("samples.example.com/recipes", "chef", map[string]any{
		"prompt": "Keep samples.example.com/recipes/recipe tidy.",
	}), "samples.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got := mapPathString(copied, "data", "prompt"); got != prompt {
		t.Errorf("the copied prompt is %q, want %q", got, prompt)
	}
}

// A published closure that names `~` names the installing repository's own
// namespace, at a reference position or in code.
func TestLocalNamesInAPublishedClosure(t *testing.T) {
	docs := []map[string]any{
		KindManifest("pub.example.com/recipes", map[string]any{"singular": "recipe"}, map[string]any{
			"properties": map[string]any{"note": map[string]any{"type": "reference", "kind": "~/notes/note"}},
		}),
		FunctionManifest("pub.example.com/recipes", "add", map[string]any{
			"runtime": RuntimePython, "source": `put("~/tasks/task"); url("https://example.com/~/x")`,
		}),
		AgentManifest("pub.example.com/recipes", "chef", map[string]any{"prompt": "Mind ~/notes/note."}),
	}
	if got, want := LocalNamesIn(docs), []string{"~/notes/note", "~/tasks"}; !reflect.DeepEqual(got, want) {
		t.Errorf("LocalNamesIn = %v, want %v (a prompt is prose and not read)", got, want)
	}
}

// NamesPackage is the one test of whether a value names a local package:
// under `~` or a name the repository answers to, whole, as a package, a
// kind or a record path, in a value or a key at any depth, at a name
// boundary inside a text; and never a name that only resembles it, nor the
// published name itself.
func TestNamesPackageFindsThePackageUnderEverySpelling(t *testing.T) {
	spellings := []string{"ada.example.com", "former.example.com"}
	for _, c := range []struct {
		v    any
		want bool
	}{
		{"~/recipes", true},
		{"~/recipes/recipe", true},
		{"~/recipes/*", true},
		{"former.example.com/recipes", true},
		{"former.example.com/recipes/recipe/r1", true},
		{CoreKind(DocAgent) + "/~/recipes/chef", true},
		{"Use `~/recipes`.", true},
		{"use ~/recipes.", true},
		{map[string]any{"records": map[string]any{"~/recipes/recipe/r1": map[string]any{}}}, true},
		{map[string]any{"a": []any{map[string]any{"b": map[string]any{"former.example.com/recipes/recipe": 1}}}}, true},
		{[]any{"x", map[string]any{"into": "~/recipes/recipe"}}, true},
		{"~/recipes2", false},
		{"~/recipes-old", false},
		{"x~/recipes", false},
		{"~~/recipes", false},
		{"notformer.example.com/recipes", false},
		{"sub.former.example.com/recipes", false},
		{"ada.example.com/recipes/recipe", false},
		{"~/notes/note", false},
		{map[string]any{"recipes": "~/notes"}, false},
	} {
		if got := NamesPackage(c.v, "recipes", "ada.example.com", spellings); got != c.want {
			t.Errorf("NamesPackage(%#v) = %v, want %v", c.v, got, c.want)
		}
	}
}
