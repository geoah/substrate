package engine_test

// Search quality: each case is a query that answered badly before BM25F, the
// relaxed candidate set, the index variants and the purpose prior, and the
// hits it must answer with now.

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

func TestSearchQuality(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const (
		personKind = "samples.substrate.reamde.dev/people/person"
		taskKind   = "samples.substrate.reamde.dev/tasks/task"
	)
	_, ds := newVocabularyDataset(t, "people", "tasks")

	person := func(id, name, email string) {
		props := map[string]any{"name": name}
		if email != "" {
			props["emails"] = []any{email}
		}
		mustPut(t, ds, owner, substrate.PutInput{Kind: personKind, ID: id, Properties: props})
	}
	task := func(id, name, description string) {
		mustPut(t, ds, owner, substrate.PutInput{
			Kind: taskKind, ID: id,
			Properties: map[string]any{"name": name, "description": description},
		})
	}
	person("jose", "José Ramírez", "")
	person("anastasia", "Anastasia Pell", "anastasia@inbox.example")
	person("ansel", "Ansel Linscott", "")
	// Every person is born `utility`, as each synced contact is.
	person("ada-trigger", "Ada Trigger", "")
	task("rollback-plan", "Rollback plan", "Write the rollback plan for the migration.")
	task("rollback-notes", "Notes", "Ideas for the rollback, and the migration order.")
	// A long description that repeats the word saturates; the task titled by
	// it still ranks first.
	task("long", "Quarterly archive", strings.Repeat("rollback steps and the archive layout. ", 60))
	// Common words: every one of these holds `review`, one holds `ansel`.
	for _, id := range []string{"r1", "r2", "r3", "r4", "r5", "r6"} {
		task("review-"+id, "Review the queue "+id, "Weekly review.")
	}
	task("sync-ansel", "Sync with Ansel", "Agenda for Thursday.")
	// Core declares a kind named trigger and describes it in words. These
	// tasks name it in a long title and in a description.
	task("deploy-notes", "Deploy notes for the trigger on the staging cluster after the weekly review", "")
	task("deploy-site", "Deploy the site", "Set the trigger after review.")

	search := func(q string, purposes ...string) []string {
		t.Helper()
		res, err := ds.Search(ctx, substrate.SearchInput{Q: q, Mode: substrate.SearchLexical, Purposes: purposes, K: 10})
		if err != nil {
			t.Fatalf("search %q: %v", q, err)
		}
		out := make([]string, 0, len(res.Hits))
		for _, h := range res.Hits {
			out = append(out, h.Record.Kind+"/"+h.Record.ID)
		}
		return out
	}
	first := func(q, want string, purposes ...string) {
		t.Helper()
		if got := search(q, purposes...); len(got) == 0 || !strings.HasSuffix(got[0], want) {
			t.Errorf("search %q: first hit %v, want %s", q, got, want)
		}
	}

	t.Run("a folded spelling finds the accented one, and back", func(t *testing.T) {
		first("jose", "/jose")
		first("José", "/jose")
		first("ramirez", "/jose")
	})
	t.Run("an address's local part and domain find it", func(t *testing.T) {
		first("anastasia@inbox.example", "/anastasia")
		first("inbox.example", "/anastasia")
		// Before, the only lexeme was the whole address.
		got := search("inbox")
		if !slices.Contains(got, personKind+"/anastasia") {
			t.Errorf("search inbox = %v, want the person with that domain", got)
		}
	})
	t.Run("a prefix is not stemmed", func(t *testing.T) {
		// `ans` stems to `an`, and `an*` matched every word starting with it,
		// Anastasia among them.
		page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{personKind}, Search: "ans*"}})
		if err != nil {
			t.Fatal(err)
		}
		if got := pageIDs(page); !slices.Equal(got, []string{"ansel"}) {
			t.Errorf("filter.search ans* = %v, want [ansel]", got)
		}
		got := search("ans*", "primary")
		if len(got) == 0 || !strings.HasSuffix(got[0], "/ansel") && !strings.HasSuffix(got[0], "/sync-ansel") {
			t.Errorf("search ans* = %v, want Ansel first", got)
		}
		if slices.Contains(got, personKind+"/anastasia") {
			t.Errorf("search ans* = %v: Anastasia holds no word starting with ans", got)
		}
	})
	t.Run("a word no record holds still finds the others, whole matches first", func(t *testing.T) {
		got := search("rollback plan tuesday")
		if len(got) < 2 {
			t.Fatalf("search = %v, want the rollback tasks", got)
		}
		if !strings.HasSuffix(got[0], "/rollback-plan") {
			t.Errorf("search = %v, want the task holding both words first", got)
		}
		// The words as typed, all of them, match nothing: the list's search
		// arm is a predicate and keeps that.
		page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{Search: "rollback plan tuesday"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Records) != 0 {
			t.Errorf("filter.search matched %d rows, want none", len(page.Records))
		}
	})
	t.Run("the title outranks a long text that repeats the word", func(t *testing.T) {
		got := search("rollback")
		if i, j := slices.Index(got, taskKind+"/rollback-plan"), slices.Index(got, taskKind+"/long"); i < 0 || j < 0 || i > j {
			t.Errorf("search rollback = %v: the titled task must rank above the long one", got)
		}
	})
	t.Run("a rare word outweighs a common one", func(t *testing.T) {
		first("review ansel", "/sync-ansel")
	})
	t.Run("machinery ranks below data, and a purpose filter drops it", func(t *testing.T) {
		res, err := ds.Search(ctx, substrate.SearchInput{Q: "trigger", Mode: substrate.SearchLexical, K: 10})
		if err != nil {
			t.Fatal(err)
		}
		rank, raw := map[string]int{}, map[string]float64{}
		for i, h := range res.Hits {
			rank[h.Record.ID], raw[h.Record.ID] = i, h.Lexical
		}
		declared := "substrate.reamde.dev/core/trigger"
		ri, ok := rank[declared]
		if !ok {
			t.Fatalf("search trigger = %v, want core's trigger declaration among the hits", hitIDs(res.Hits))
		}
		// The long title scores below the declaration on words alone, so
		// only the purpose prior puts it above.
		if raw["deploy-notes"] >= raw[declared] {
			t.Errorf("the long-titled task scores %v, the declaration %v: the case no longer tests the prior", raw["deploy-notes"], raw[declared])
		}
		// A person born a moment ago is demoted below known people, not below
		// machinery.
		for _, id := range []string{"deploy-notes", "deploy-site", "ada-trigger"} {
			if r, ok := rank[id]; !ok || r > ri {
				t.Errorf("search trigger = %v: task %s must rank above the declaration", hitIDs(res.Hits), id)
			}
		}
		for _, id := range search("trigger", "primary", "supporting") {
			if strings.HasPrefix(id, "substrate.reamde.dev/core/") {
				t.Errorf("purposes primary+supporting returned %s", id)
			}
		}
		internal := search("trigger", "internal")
		if len(internal) == 0 {
			t.Fatal("purposes internal returned nothing; core's trigger declaration is an internal record")
		}
		for _, id := range internal {
			if !strings.HasPrefix(id, "substrate.reamde.dev/core/") {
				t.Errorf("purposes internal returned %s", id)
			}
		}
	})
	t.Run("an unknown purpose is refused by name", func(t *testing.T) {
		_, err := ds.Search(ctx, substrate.SearchInput{Q: "trigger", Purposes: []string{"hidden"}})
		if err == nil || !strings.Contains(err.Error(), `"hidden"`) {
			t.Errorf("err = %v, want the purpose named", err)
		}
		_, err = ds.List(ctx, substrate.Query{Filter: substrate.Filter{Purposes: []string{"hidden"}}})
		if err == nil || !strings.Contains(err.Error(), `"hidden"`) {
			t.Errorf("list err = %v, want the purpose named", err)
		}
	})
	t.Run("the list narrows by purpose", func(t *testing.T) {
		page, err := ds.List(ctx, substrate.Query{First: 200, Filter: substrate.Filter{Purposes: []string{"internal"}}})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Records) == 0 {
			t.Fatal("no internal records listed")
		}
		for _, r := range page.Records {
			if r.Kind == taskKind || r.Kind == personKind {
				t.Errorf("internal list holds %s/%s", r.Kind, r.ID)
			}
		}
		page, err = ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{taskKind}, Purposes: []string{"internal"}}})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Records) != 0 {
			t.Errorf("a primary kind narrowed to internal listed %d rows, want none", len(page.Records))
		}
	})
}

// A match in a record's description is not crushed where prose is rare. The
// band averages read only the rows holding the band; over every row, the seeded
// declarations (which carry no prose) put the prose average near zero, and a
// one-sentence description read as thirty times longer than usual.
func TestSearchScoresProseWhereProseIsRare(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newVocabularyDataset(t, "tasks")
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: "samples.substrate.reamde.dev/tasks/task", ID: "deploy-site",
		Properties: map[string]any{"name": "Deploy the site", "description": "Set the trigger after review."},
	})
	res, err := ds.Search(ctx, substrate.SearchInput{Q: "trigger", Mode: substrate.SearchLexical, K: 20})
	if err != nil {
		t.Fatal(err)
	}
	raw := map[string]float64{}
	for _, h := range res.Hits {
		raw[h.Record.ID] = h.Lexical
	}
	task, declared := raw["deploy-site"], raw["substrate.reamde.dev/core/trigger"]
	if declared == 0 || task < 0.4*declared {
		t.Errorf("the description match scores %v against the titled declaration's %v, want at least 0.4 of it", task, declared)
	}
}
