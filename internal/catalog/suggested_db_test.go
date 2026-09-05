package catalog_test

// SUGGESTED MAPPINGS against a REAL engine (decision record 0049, phase 5 of
// docs/plans/providers-and-samples.md): a sample ships one mapping per
// provider it knows, onto a kind of its own, and the import keeps only the
// ones whose provider this repository holds.
//
// The two rounds are the whole story. On a fresh repository the mapping is
// dropped and reported `waiting`, and the kinds land without it. Install the
// provider, import the sample AGAIN, and the mapping lands: a mirror row
// written from then on mints the subject and the map rules project onto it.
//
// The mirror rows here are written directly, which is what a sync body's
// effects do to them. The provider fake, uv and the OAuth flow are proven in
// internal/engine; what needs proving here is that the SHIPPED closures reach
// each other through the door.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	mappingKind = "substrate.reamde.dev/core/recordmapping"

	githubProviderID = "providers.substrate.reamde.dev/github"
	githubAccount    = githubProviderID + "/account"
	githubUser       = githubProviderID + "/user"

	linearProviderID = "providers.substrate.reamde.dev/linear"
	linearAccount    = linearProviderID + "/account"
	linearIssue      = linearProviderID + "/issue"
)

// suggestedState is one shipped closure's report for one mapping id.
func suggestedState(t *testing.T, b *catalog.Bundle, ds substrate.Dataset, id string) substrate.SuggestedMapping {
	t.Helper()
	states, err := b.SuggestedMappingStates(context.Background(), ds)
	if err != nil {
		t.Fatalf("suggested mapping states: %v", err)
	}
	for _, sm := range states {
		if sm.ID == id {
			return sm
		}
	}
	t.Fatalf("%s ships no suggested mapping %s: %+v", b.ID, id, states)
	return substrate.SuggestedMapping{}
}

// subjectOf reads the record a mirror row's subject slot names: the kind and
// the id, empty where the slot is still empty.
func subjectOf(t *testing.T, ds substrate.Dataset, kind, id, property string) (string, string) {
	t.Helper()
	row, err := ds.Get(context.Background(), kind, id)
	if err != nil {
		t.Fatalf("get %s/%s: %v", kind, id, err)
	}
	path, _ := row.Properties[property].(string)
	if path == "" {
		if m, ok := row.Properties[property].(map[string]any); ok {
			path, _ = m[vocabulary.ReferenceValueKey].(string)
		}
	}
	if path == "" {
		return "", ""
	}
	subjectKind, subjectID, ok := vocabulary.SplitRecordPath(path)
	if !ok {
		t.Fatalf("%s/%s: %s = %q, which is not a record path", kind, id, property, path)
	}
	return subjectKind, subjectID
}

// The people sample's github mapping: dropped on a repository with no GitHub,
// landed on the import after the provider is installed, and projecting from
// then on.
func TestASuggestedMappingWaitsForItsProviderAndLandsOnReimport(t *testing.T) {
	ds := newDataset(t)
	c := loadCatalog(t)
	ctx := context.Background()
	people, ok := c.ByID(peopleSampleID)
	if !ok {
		t.Fatalf("no %s in the shipped catalog", peopleSampleID)
	}
	const suggested = peopleSampleID + "/githubuserperson"

	// ROUND ONE: a fresh repository. Every suggested mapping is waiting, and
	// each says which provider package on.
	importSamples(t, c, ds, peopleSampleID)
	sm := suggestedState(t, people, ds, suggested)
	if sm.State != substrate.SuggestedMappingWaiting || sm.Package != githubProviderID {
		t.Fatalf("state = %+v, want waiting for %s", sm, githubProviderID)
	}
	if sm.From != githubUser || sm.To != peopleSampleID+"/person" {
		t.Errorf("the report does not name both ends: %+v", sm)
	}
	// The kinds landed and the mapping did not: an import onto an absent
	// provider is not a refusal, it is a smaller closure.
	if _, err := ds.KindByRef(ctx, homeAuthority+"/people/person"); err != nil {
		t.Fatalf("the people kinds did not land: %v", err)
	}
	landedMapping := homeAuthority + "/people/githubuserperson"
	if _, err := ds.Get(ctx, mappingKind, landedMapping); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the suggested mapping landed with no provider to read: %v", err)
	}

	// ROUND TWO: install the provider, and the same import lands the mapping.
	if _, err := c.Install(ctx, substrate.ActorAPI, githubProviderID, ds); err != nil {
		t.Fatalf("install %s: %v", githubProviderID, err)
	}
	if sm := suggestedState(t, people, ds, suggested); sm.State != substrate.SuggestedMappingLanded {
		t.Fatalf("state after installing the provider = %+v, want landed", sm)
	}
	importSamples(t, c, ds, peopleSampleID)
	if _, err := ds.Get(ctx, mappingKind, landedMapping); err != nil {
		t.Fatalf("the mapping did not land on the re-import: %v", err)
	}

	// A synced user mirror now mints the person and the map rules project
	// onto it: the name, the login as the friendly name, and the address
	// unioned onto the person's own list.
	account, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: githubAccount, ID: "acct"})
	if err != nil {
		t.Fatalf("write the account: %v", err)
	}
	user, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: githubUser, ID: "user-ada",
		Properties: map[string]any{
			"account": githubAccount + "/" + account.ID,
			"login":   "ada",
			"name":    "Ada Lovelace",
			"email":   "ada@example.com",
		},
	})
	if err != nil {
		t.Fatalf("write the user mirror: %v", err)
	}
	personKind, personID := subjectOf(t, ds, githubUser, user.ID, "person")
	if personID == "" {
		t.Fatal("the user mirror landed with an empty person slot, so the mapping did not mint")
	}
	if personKind != homeAuthority+"/people/person" {
		t.Fatalf("the mapping minted a %s, not this repository's person", personKind)
	}
	person, err := ds.Get(ctx, personKind, personID)
	if err != nil {
		t.Fatalf("get the minted person: %v", err)
	}
	if person.Properties["name"] != "Ada Lovelace" || person.Properties["displayName"] != "ada" {
		t.Errorf("the map rules did not project: %+v", person.Properties)
	}
	if emails := storedStrings(person.Properties["emails"]); len(emails) != 1 || emails[0] != "ada@example.com" {
		t.Errorf("emails = %v, want the union of the profile's address", person.Properties["emails"])
	}
}

// The tasks sample's linear mapping, installed the other way round: the
// provider is there before the import, so the mapping lands on the FIRST
// import and a synced issue mints a task.
func TestImportingTasksWithLinearInstalledLandsTheIssueMapping(t *testing.T) {
	ds := newDataset(t)
	c := loadCatalog(t)
	ctx := context.Background()
	tasks, ok := c.ByID(tasksSampleID)
	if !ok {
		t.Fatalf("no %s in the shipped catalog", tasksSampleID)
	}
	const suggested = tasksSampleID + "/linearissuetask"

	if _, err := c.Install(ctx, substrate.ActorAPI, linearProviderID, ds); err != nil {
		t.Fatalf("install %s: %v", linearProviderID, err)
	}
	if sm := suggestedState(t, tasks, ds, suggested); sm.State != substrate.SuggestedMappingLanded {
		t.Fatalf("state = %+v, want landed with the provider installed", sm)
	}
	importSamples(t, c, ds, peopleSampleID, schedulingSample, tasksSampleID)
	if _, err := ds.Get(ctx, mappingKind, homeAuthority+"/tasks/linearissuetask"); err != nil {
		t.Fatalf("the issue mapping did not land: %v", err)
	}

	account, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: linearAccount, ID: "acct"})
	if err != nil {
		t.Fatalf("write the account: %v", err)
	}
	issue, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: linearIssue, ID: "issue-1",
		Properties: map[string]any{
			"account":    linearAccount + "/" + account.ID,
			"title":      "Ship the mappings",
			"identifier": "ENG-1",
			"url":        "https://linear.app/acme/issue/ENG-1",
		},
	})
	if err != nil {
		t.Fatalf("write the issue mirror: %v", err)
	}
	taskKind, taskID := subjectOf(t, ds, linearIssue, issue.ID, "task")
	if taskID == "" {
		t.Fatal("the issue mirror landed with an empty task slot, so the mapping did not mint")
	}
	if taskKind != homeAuthority+"/tasks/task" {
		t.Fatalf("the mapping minted a %s, not this repository's task", taskKind)
	}
	task, err := ds.Get(ctx, taskKind, taskID)
	if err != nil {
		t.Fatalf("get the minted task: %v", err)
	}
	if task.Properties["name"] != "Ship the mappings" {
		t.Errorf("the heading did not project: %+v", task.Properties)
	}
	if got, _ := task.Properties["url"].(string); !strings.HasSuffix(got, "/ENG-1") {
		t.Errorf("url = %v, want the issue's own link", task.Properties["url"])
	}
	// The issue's OTHER subject slot is the people sample's mapping, and it
	// is empty here: the issue carries no assignee address, so the probe
	// extracts nothing and nothing links. One mirror kind, two slots, one
	// mapping each (record 0049).
	if _, assignee := subjectOf(t, ds, linearIssue, issue.ID, "assignee"); assignee == "" {
		t.Log("the assignee slot is empty, as an issue with no assignee address should be")
	}
}

// storedStrings reads a repeated string-family property.
func storedStrings(v any) []string {
	list, repeated := v.([]any)
	if !repeated {
		list = []any{v}
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}
