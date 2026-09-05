package catalog_test

// SUGGESTED MAPPINGS against a REAL engine (decision record 0049, phase 5 of
// docs/plans/providers-and-samples.md): a sample ships one mapping per
// provider it knows, onto a kind of its own, and the door keeps only the ones
// this repository can resolve.
//
// The three rounds are the whole story, and the middle one is the point. On a
// fresh repository the mapping is dropped and reported WAITING. Installing the
// provider does not land it: it lands mirrors, and the mapping is the sample's
// declaration, so the state is READY. Importing the sample AGAIN lands it, and
// only then does a mirror row mint the subject and the map rules project onto
// it.
//
// The mirror rows here are written directly, which is what a sync body's
// effects do to them. The provider fake, uv and the OAuth flow are proven in
// internal/engine, where the linear suite also drives this mapping through the
// real sync; what needs proving here is that the SHIPPED closures reach each
// other through the door.

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

// suggestedState is one shipped closure's READ of one mapping's state, by the
// id this repository holds it under.
func suggestedState(t *testing.T, b *catalog.Bundle, ds substrate.Dataset, id string) substrate.SuggestedMapping {
	t.Helper()
	states, err := b.SuggestedMappingStates(context.Background(), ds)
	if err != nil {
		t.Fatalf("suggested mapping states: %v", err)
	}
	return reported(t, states, id)
}

// reported picks one mapping out of a door's own report.
func reported(t *testing.T, report []substrate.SuggestedMapping, id string) substrate.SuggestedMapping {
	t.Helper()
	for _, sm := range report {
		if sm.ID == id {
			return sm
		}
	}
	t.Fatalf("no suggested mapping %s in %+v", id, report)
	return substrate.SuggestedMapping{}
}

// subjectOf reads the record a mirror row's subject slot names: the kind and
// the id, both empty where the slot is still empty.
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

// The people sample's github mapping through its three states: dropped and
// waiting on a repository with no GitHub, READY once the provider is there,
// landed on the import after it, and projecting from then on.
func TestASuggestedMappingWaitsThenIsReadyThenLands(t *testing.T) {
	ds := newDataset(t)
	c := loadCatalog(t)
	ctx := context.Background()
	people, ok := c.ByID(peopleSampleID)
	if !ok {
		t.Fatalf("no %s in the shipped catalog", peopleSampleID)
	}
	// Every id the report names is the one this repository would HOLD:
	// rehomed, because that is the record its state is read from.
	landedMapping := homeAuthority + "/people/githubuserperson"

	// ROUND ONE: a fresh repository. The mapping is waiting, and says which
	// provider package on.
	importSamples(t, c, ds, peopleSampleID)
	sm := suggestedState(t, people, ds, landedMapping)
	if sm.State != substrate.SuggestedMappingWaiting || sm.Package != githubProviderID {
		t.Fatalf("state = %+v, want waiting for %s", sm, githubProviderID)
	}
	if sm.From != githubUser || sm.To != homeAuthority+"/people/person" {
		t.Errorf("the report does not name both ends as they land here: %+v", sm)
	}
	// The kinds landed and the mapping did not: an import onto an absent
	// provider is not a refusal, it is a smaller closure.
	if _, err := ds.KindByRef(ctx, homeAuthority+"/people/person"); err != nil {
		t.Fatalf("the people kinds did not land: %v", err)
	}
	if _, err := ds.Get(ctx, mappingKind, landedMapping); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the suggested mapping landed with no provider to read: %v", err)
	}

	// ROUND TWO: install the provider. THE MAPPING STILL HAS NOT LANDED.
	// Installing GitHub writes mirrors and nothing else, and the mapping is
	// the sample's own declaration, so the state is READY. Reading the
	// provider's presence instead would report `landed` here and tell the
	// reader a GitHub user reaches their person while no such declaration
	// exists.
	if _, _, err := c.Install(ctx, substrate.ActorAPI, githubProviderID, ds); err != nil {
		t.Fatalf("install %s: %v", githubProviderID, err)
	}
	if sm := suggestedState(t, people, ds, landedMapping); sm.State != substrate.SuggestedMappingReady {
		t.Fatalf("state after installing the provider = %+v, want ready", sm)
	}
	if _, err := ds.Get(ctx, mappingKind, landedMapping); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("installing the provider landed the sample's mapping: %v", err)
	}
	user, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: githubUser, ID: "before",
		Properties: map[string]any{
			"account": githubAccount + "/" + mustAccount(t, ds, githubAccount),
			"login":   "grace", "email": "grace@example.com",
		},
	})
	if err != nil {
		t.Fatalf("write a user mirror before the mapping: %v", err)
	}
	if _, id := subjectOf(t, ds, githubUser, user.ID, "person"); id != "" {
		t.Fatalf("a mirror synced before the mapping landed points at %s", id)
	}

	// ROUND THREE: the re-import, which is what lands it. The door's own
	// report says landed, because the mapping was in the batch that
	// committed, and a read afterwards agrees.
	_, report, err := c.Import(ctx, substrate.ActorAPI, peopleSampleID, ds)
	if err != nil {
		t.Fatalf("re-import %s: %v", peopleSampleID, err)
	}
	if got := reported(t, report, landedMapping); got.State != substrate.SuggestedMappingLanded {
		t.Fatalf("the door reported %+v, want landed", got)
	}
	if _, err := ds.Get(ctx, mappingKind, landedMapping); err != nil {
		t.Fatalf("the mapping did not land on the re-import: %v", err)
	}
	if sm := suggestedState(t, people, ds, landedMapping); sm.State != substrate.SuggestedMappingLanded {
		t.Fatalf("state after the re-import = %+v, want landed", sm)
	}

	// A user mirror written from now on mints the person and the map rules
	// project onto it: the name, the login as the friendly name, and the
	// address unioned onto the person's own list.
	fresh, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: githubUser, ID: "user-ada",
		Properties: map[string]any{
			"account": githubAccount + "/acct",
			"login":   "ada",
			"name":    "Ada Lovelace",
			"email":   "ada@example.com",
		},
	})
	if err != nil {
		t.Fatalf("write the user mirror: %v", err)
	}
	personKind, personID := subjectOf(t, ds, githubUser, fresh.ID, "person")
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
	landedMapping := homeAuthority + "/tasks/linearissuetask"

	if _, _, err := c.Install(ctx, substrate.ActorAPI, linearProviderID, ds); err != nil {
		t.Fatalf("install %s: %v", linearProviderID, err)
	}
	// The provider is here and the mapping fits it, so it is READY before the
	// import and LANDED after it, in the door's own report.
	if sm := suggestedState(t, tasks, ds, landedMapping); sm.State != substrate.SuggestedMappingReady {
		t.Fatalf("state = %+v, want ready with the provider installed", sm)
	}
	importSamples(t, c, ds, peopleSampleID, schedulingSample)
	_, report, err := c.Import(ctx, substrate.ActorAPI, tasksSampleID, ds)
	if err != nil {
		t.Fatalf("import %s: %v", tasksSampleID, err)
	}
	if got := reported(t, report, landedMapping); got.State != substrate.SuggestedMappingLanded {
		t.Fatalf("the door reported %+v, want landed", got)
	}
	if _, err := ds.Get(ctx, mappingKind, landedMapping); err != nil {
		t.Fatalf("the issue mapping did not land: %v", err)
	}

	issue, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: linearIssue, ID: "issue-1",
		Properties: map[string]any{
			"account":       linearAccount + "/" + mustAccount(t, ds, linearAccount),
			"title":         "Ship the mappings",
			"identifier":    "ENG-1",
			"url":           "https://linear.app/acme/issue/ENG-1",
			"assigneeEmail": "ada@example.com",
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
	// The issue's OTHER subject slot is the people sample's mapping, landed by
	// the import above, and it minted a person for the assignee address: one
	// mirror kind reaching two of this repository's kinds through two slots,
	// one mapping each (record 0049). A shell person is the honest answer to
	// an address no person carries yet, and the projection's own rule
	// (zero-or-several candidates mint) is what produces it.
	personKind, personID := subjectOf(t, ds, linearIssue, issue.ID, "assignee")
	if personID == "" {
		t.Fatal("the assignee slot is empty, so the people sample's issue mapping did not mint")
	}
	if personKind != homeAuthority+"/people/person" {
		t.Fatalf("the assignee slot points at %s, not this repository's person", personKind)
	}
	person, err := ds.Get(ctx, personKind, personID)
	if err != nil {
		t.Fatalf("get the minted person: %v", err)
	}
	// A SHELL, and empty on purpose. The issue DOES carry an assignee address,
	// so the probe runs; no person in this repository holds that address yet,
	// and zero candidates mint rather than guess. What lands is bare, because
	// the issue mapping carries a `match` block and no `map` block: an issue
	// describes work, and nothing of it belongs on the human, not even the
	// address the probe matched on. Prominence stays at its initial `utility`
	// until an address book or the owner promotes the person.
	if got := storedStrings(person.Properties["emails"]); len(got) != 0 {
		t.Errorf("the minted person carries %v; the issue mapping maps nothing", got)
	}
	if person.Properties["name"] != nil {
		t.Errorf("the issue mapping copied %v onto the person; an issue describes work, not a human", person.Properties["name"])
	}
	if person.Properties["prominence"] != "utility" {
		t.Errorf("prominence = %v, want the initial utility", person.Properties["prominence"])
	}
}

// A provider OLDER than the sample was written against BLOCKS the mapping
// rather than refusing the import: the door drops it, says which provider to
// upgrade, and names what did not fit.
func TestASuggestedMappingIsBlockedByAnOlderProvider(t *testing.T) {
	ds := newDataset(t)
	c := loadCatalog(t)
	ctx := context.Background()
	tasks, ok := c.ByID(tasksSampleID)
	if !ok {
		t.Fatalf("no %s in the shipped catalog", tasksSampleID)
	}
	landedMapping := homeAuthority + "/tasks/linearissuetask"

	// A linear package as it stood at version 11: the `issue` mirror without
	// the `task` subject slot the tasks sample's mapping fills. Applied by
	// hand, which is the one way a repository can hold a provider's
	// declarations at a shape the shipped closure has moved past.
	applier, ok := ds.(substrate.VocabularyApplier)
	if !ok {
		t.Skip("this dataset cannot apply vocabulary documents")
	}
	older := []map[string]any{
		vocabulary.PackageManifest(linearProviderID, 11),
		vocabulary.KindManifest(linearProviderID,
			map[string]any{"singular": "issue"},
			map[string]any{
				"properties": map[string]any{
					"url":           map[string]any{"type": "url"},
					"assigneeEmail": map[string]any{"type": "email"},
					// The `task` slot is NOT here: that is the whole fixture.
					"assignee": map[string]any{
						"type": "reference", "mustExist": true, "subject": true,
					},
				},
			}),
	}
	if _, err := applier.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, older); err != nil {
		t.Fatalf("apply the older linear package: %v", err)
	}

	sm := suggestedState(t, tasks, ds, landedMapping)
	if sm.State != substrate.SuggestedMappingBlocked {
		t.Fatalf("state = %+v, want blocked against a provider with no `task` slot", sm)
	}
	if len(sm.Problems) == 0 {
		t.Fatal("a blocked mapping reports no problems, so the reader cannot tell what to fix")
	}
	if !strings.Contains(strings.Join(sm.Problems, " "), "task") {
		t.Errorf("the problems do not name the missing slot: %v", sm.Problems)
	}
	if sm.Package != linearProviderID {
		t.Errorf("package = %q, want the provider to upgrade", sm.Package)
	}

	// And the import goes THROUGH, without the mapping: a blocked mapping the
	// reader never wrote must not cost them the sample.
	importSamples(t, c, ds, peopleSampleID, schedulingSample, tasksSampleID)
	if _, err := ds.KindByRef(ctx, homeAuthority+"/tasks/task"); err != nil {
		t.Fatalf("the tasks kinds did not land: %v", err)
	}
	if _, err := ds.Get(ctx, mappingKind, landedMapping); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the blocked mapping landed: %v", err)
	}
}

// THE VERBATIM DOOR REPORTS WHAT IT WROTE. `install` of a sample lands the
// closure under the placeholder authority, unrehomed, so the mapping's
// declaration is there too, and a report naming the rehomed id would name a
// record nobody wrote. The door's spelling is the door's own.
func TestTheVerbatimInstallReportsTheShippedSpelling(t *testing.T) {
	ds := newDataset(t)
	c := loadCatalog(t)
	ctx := context.Background()
	people, ok := c.ByID(peopleSampleID)
	if !ok {
		t.Fatalf("no %s in the shipped catalog", peopleSampleID)
	}
	shippedMapping := peopleSampleID + "/githubuserperson"

	if _, _, err := c.Install(ctx, substrate.ActorAPI, githubProviderID, ds); err != nil {
		t.Fatalf("install %s: %v", githubProviderID, err)
	}
	_, report, err := c.Install(ctx, substrate.ActorAPI, peopleSampleID, ds)
	if err != nil {
		t.Fatalf("install %s verbatim: %v", peopleSampleID, err)
	}
	got := reported(t, report, shippedMapping)
	if got.State != substrate.SuggestedMappingLanded {
		t.Fatalf("the verbatim install reported %+v, want landed", got)
	}
	if got.To != peopleSampleID+"/person" {
		t.Errorf("to = %q, want the kind this door landed: %s/person", got.To, peopleSampleID)
	}
	// The id the report named is the record that exists, and the rehomed one
	// does not.
	if _, err := ds.Get(ctx, mappingKind, got.ID); err != nil {
		t.Fatalf("the reported mapping %s is not a record here: %v", got.ID, err)
	}
	if _, err := ds.Get(ctx, mappingKind, homeAuthority+"/people/githubuserperson"); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("a verbatim install wrote a rehomed mapping: %v", err)
	}
	// And a READ afterwards agrees, both fields: it prefers the rehomed
	// spelling, finds neither the kind nor the mapping there, and answers
	// with what this repository actually holds.
	state := suggestedState(t, people, ds, shippedMapping)
	if state.State != substrate.SuggestedMappingLanded || state.To != peopleSampleID+"/person" {
		t.Fatalf("the read = %+v, want landed onto %s/person", state, peopleSampleID)
	}
}

// mustAccount writes one account record of a provider's account kind and
// answers its id: every mirror row references the account it synced through.
func mustAccount(t *testing.T, ds substrate.Dataset, kind string) string {
	t.Helper()
	rec, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{Kind: kind, ID: "acct"})
	if err != nil {
		t.Fatalf("write the account %s: %v", kind, err)
	}
	return rec.ID
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
