package engine

// The pure half of repository migration 1 and the runner's ledger check,
// without a database: the old resolution rule reproduced from stored
// documents, and the three refusals over a ledger.

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

func kindDoc(id string, data map[string]any) vocabulary.Document {
	a, p, _ := vocabulary.SplitKindRef(id)
	data["authority"], data["package"] = a, p
	return vocabulary.Document{Kind: vocabulary.DocKind, ID: id, Data: data}
}

func traitDoc(id string) vocabulary.Document {
	a, p, _ := vocabulary.SplitKindRef(id)
	return vocabulary.Document{Kind: vocabulary.DocTrait, ID: id, Data: map[string]any{"authority": a, "package": p, "properties": map[string]any{"score": "int"}}}
}

func functionDoc(id string, perms map[string]any) vocabulary.Document {
	a, p, _ := vocabulary.SplitKindRef(id)
	return vocabulary.Document{Kind: vocabulary.DocFunction, ID: id, Data: map[string]any{"authority": a, "package": p, "permissions": perms}}
}

func ref(kind string) map[string]any { return map[string]any{"type": "reference", "kind": kind} }

func TestQualifyDocumentReproducesTheOldRule(t *testing.T) {
	docs := map[string]vocabulary.Document{}
	add := func(d vocabulary.Document) { docs[d.Kind+"\x00"+d.ID] = d }
	add(kindDoc("ada.example.com/people/person", map[string]any{}))
	add(kindDoc("samples.example.com/people/person", map[string]any{}))
	add(kindDoc("ada.example.com/tasks/project", map[string]any{}))
	add(kindDoc("ada.example.com/tasks/task", map[string]any{
		"traits": []any{"temporal(point: dueAt)", "occurrencelog", "ranked(point)", "zzz"},
		"properties": map[string]any{
			"project":  ref("project"), // in package
			"assignee": ref("person"),  // two carry the word: left, the old loader refused it too
			"tag":      ref("tag"),     // the one kind anywhere
			"anything": ref("any"),     // unconstrained stays
			"pinned":   ref("ada.example.com/tasks/project"),
			"held":     map[string]any{"type": "reference", "trait": "ranked"},
			"bag": map[string]any{"type": "object", "fields": map[string]any{
				"deep": map[string]any{"type": "object", "fields": map[string]any{"inner": ref("project")}},
			}},
			"name": map[string]any{"type": "string"},
		},
	}))
	add(kindDoc("bo.example.com/labels/tag", map[string]any{}))
	add(traitDoc("substrate.reamde.dev/core/temporal"))
	add(traitDoc("ada.example.com/scheduling/occurrencelog"))
	add(traitDoc("ada.example.com/tasks/ranked"))
	add(traitDoc("substrate.reamde.dev/core/ranked")) // shadowed by the package's own
	add(functionDoc("ada.example.com/tasks/harvest", map[string]any{
		"writes": []any{"tag", "project", "person", "ada.example.com/*", "*"},
		"reads":  map[string]any{"kinds": []any{"project"}},
	}))
	ix := indexDeclaredNames(docs)

	task, n := qualifyDocument(docs[vocabulary.DocKind+"\x00ada.example.com/tasks/task"], ix)
	// Three kind pins, one trait pin and three bindings.
	if n != 7 {
		t.Fatalf("rewrote %d names, want 7: %v", n, task.Data)
	}
	props := task.Data["properties"].(map[string]any)
	for path, want := range map[string]string{
		"project":  "ada.example.com/tasks/project",
		"assignee": "person",
		"tag":      "bo.example.com/labels/tag",
		"anything": "any",
		"pinned":   "ada.example.com/tasks/project",
	} {
		if got := props[path].(map[string]any)["kind"]; got != want {
			t.Errorf("%s.kind = %v, want %s", path, got, want)
		}
	}
	if got := props["held"].(map[string]any)["trait"]; got != "ada.example.com/tasks/ranked" {
		t.Errorf("held.trait = %v, want the package's own ranked", got)
	}
	inner := props["bag"].(map[string]any)["fields"].(map[string]any)["deep"].(map[string]any)["fields"].(map[string]any)["inner"].(map[string]any)
	if inner["kind"] != "ada.example.com/tasks/project" {
		t.Errorf("bag.deep.inner.kind = %v", inner["kind"])
	}
	wantTraits := []any{"substrate.reamde.dev/core/temporal(point: dueAt)", "ada.example.com/scheduling/occurrencelog", "ada.example.com/tasks/ranked(point)", "zzz"}
	if got := task.Data["traits"].([]any); len(got) != 4 || got[0] != wantTraits[0] || got[1] != wantTraits[1] || got[2] != wantTraits[2] || got[3] != wantTraits[3] {
		t.Errorf("traits = %v, want %v", got, wantTraits)
	}
	// The input document is untouched: the rewrite is on a copy.
	if docs[vocabulary.DocKind+"\x00ada.example.com/tasks/task"].Data["properties"].(map[string]any)["project"].(map[string]any)["kind"] != "project" {
		t.Fatal("the stored document was mutated")
	}

	fn, n := qualifyDocument(docs[vocabulary.DocFunction+"\x00ada.example.com/tasks/harvest"], ix)
	if n != 3 {
		t.Fatalf("rewrote %d allowlist entries, want 3: %v", n, fn.Data)
	}
	perms := fn.Data["permissions"].(map[string]any)
	wantWrites := []any{"bo.example.com/labels/tag", "ada.example.com/tasks/project", "person", "ada.example.com/*", "*"}
	for i, w := range perms["writes"].([]any) {
		if w != wantWrites[i] {
			t.Errorf("writes[%d] = %v, want %v", i, w, wantWrites[i])
		}
	}
	if got := perms["reads"].(map[string]any)["kinds"].([]any)[0]; got != "ada.example.com/tasks/project" {
		t.Errorf("reads.kinds[0] = %v", got)
	}

	// A document with nothing bare comes back as it was, with zero.
	if _, n := qualifyDocument(docs[vocabulary.DocKind+"\x00ada.example.com/tasks/project"], ix); n != 0 {
		t.Fatalf("a document with no pin rewrote %d names", n)
	}
	if _, n := qualifyDocument(docs[vocabulary.DocTrait+"\x00ada.example.com/tasks/ranked"], ix); n != 0 {
		t.Fatalf("a trait document rewrote %d names", n)
	}
}

func TestCheckRepositoryMigrationsRefusesEveryDivergence(t *testing.T) {
	carried := []repositoryMigration{{Version: 1, Name: "one"}, {Version: 2, Name: "two"}}
	if err := checkRepositoryMigrations("r", carried, map[int]string{1: "one"}); err != nil {
		t.Fatalf("a ledger behind the binary refused: %v", err)
	}
	if err := checkRepositoryMigrations("r", carried, map[int]string{1: "one", 2: "two"}); err != nil {
		t.Fatalf("a ledger at the binary refused: %v", err)
	}
	err := checkRepositoryMigrations("r", carried, map[int]string{1: "one", 2: "two", 3: "three"})
	if !errors.Is(err, ErrRepositoryMigrationsNewer) || !strings.Contains(err.Error(), "3 (three)") {
		t.Fatalf("a newer ledger: %v", err)
	}
	err = checkRepositoryMigrations("r", carried, map[int]string{1: "uno"})
	if err == nil || !strings.Contains(err.Error(), `recorded "uno", this binary carries "one"`) {
		t.Fatalf("a renamed migration: %v", err)
	}
	err = checkRepositoryMigrations("r", carried, map[int]string{2: "two"})
	if err == nil || !strings.Contains(err.Error(), "pending below the highest") || !strings.Contains(err.Error(), "1 (one)") {
		t.Fatalf("a gap: %v", err)
	}
	// Every divergence at once, not the first one met.
	err = checkRepositoryMigrations("r", carried, map[int]string{1: "uno", 3: "three"})
	if !errors.Is(err, ErrRepositoryMigrationsNewer) || !strings.Contains(err.Error(), "another name") || !strings.Contains(err.Error(), "pending below") {
		t.Fatalf("three divergences named one: %v", err)
	}
}

// The runner's list and the files agree: versions 1..N in order, and each
// migration's name is its file's suffix, so the ledger row names a file in the
// tree.
func TestRepositoryMigrationsMatchTheirFiles(t *testing.T) {
	files, err := filepath.Glob("repomigration_[0-9][0-9][0-9][0-9]_*.go")
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]bool{}
	for _, f := range files {
		name := strings.TrimSuffix(strings.TrimPrefix(f, "repomigration_"), ".go")
		byName[name] = true
	}
	if len(files) != len(repositoryMigrations) {
		t.Fatalf("%d migration files, %d registered: %v vs %+v", len(files), len(repositoryMigrations), files, repositoryMigrations)
	}
	for i, m := range repositoryMigrations {
		if m.Version != i+1 {
			t.Errorf("migration %d is registered at position %d", m.Version, i)
		}
		if m.Run == nil {
			t.Errorf("migration %d (%s) has no Run", m.Version, m.Name)
		}
		// The file is repomigration_NNNN_<name>.go: the name is what follows
		// the four digits and their underscore.
		base := filepath.Base(files[i])
		if got := strings.TrimSuffix(base[len("repomigration_0000_"):], ".go"); got != m.Name {
			t.Errorf("migration %d is named %q, its file is %s", m.Version, m.Name, base)
		}
	}
}
