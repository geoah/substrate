package vocabulary

import (
	"reflect"
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
