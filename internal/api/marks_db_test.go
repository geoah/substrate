package api

// The marks over HTTP, on the real engine (decision 0284): the plan's worked
// example read back field for field, two accounts of one provider as two
// alternatives, and a pinned required property released to sync.

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// marksSync is the function that syncs both address books: one hand, two
// accounts.
var marksSync = substrate.FunctionActor("providers.example.com", "book", "syncbook")

// marksKinds declares, under the repository's own authority, a contact kind
// with a required name and an address-book entry kind mapped onto it: the
// entry's name, nickname, job title and emails, the emails as a union.
func marksKinds(t *testing.T, env *dbEnv) (contact, entry string) {
	t.Helper()
	pkg := env.repository + "/book"
	contact, entry = "~/book/contact", "~/book/entry"
	if _, err := env.ds.ApplyVocabularyDocuments(context.Background(), substrate.ActorSystem, []map[string]any{
		vocabulary.PackageManifest(pkg, 1),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "contact"}, map[string]any{
			"displayTemplate": "{name}",
			"properties": map[string]any{
				"name":     map[string]any{"type": "string", "required": true, "default": "unnamed"},
				"nickname": map[string]any{"type": "string"},
				"jobTitle": map[string]any{"type": "string"},
				"emails":   map[string]any{"type": "email", "repeated": true},
			},
		}),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "entry"}, map[string]any{
			"displayTemplate": "{name}",
			"properties": map[string]any{
				"name":     map[string]any{"type": "string"},
				"jobTitle": map[string]any{"type": "string"},
				"emails":   map[string]any{"type": "email", "repeated": true},
				"contact": map[string]any{
					"type": "reference", "kind": contact, "required": true, "mustExist": true, "subject": true,
				},
			},
		}),
		vocabulary.MappingManifest(pkg, "entrycontact", map[string]any{
			"from": entry, "to": contact, "property": "contact",
			"match": []any{map[string]any{"from": "emails", "to": "emails"}},
			"map": map[string]any{
				"name":     map[string]any{"path": "name"},
				"jobTitle": map[string]any{"path": "jobTitle"},
				"emails":   map[string]any{"path": "emails", "merge": "union"},
			},
		}),
	}); err != nil {
		t.Fatalf("declare the address book: %v", err)
	}
	return contact, entry
}

// marksSyncEntry writes one address book's entry as the sync's hand, linked
// to contact where one is named.
func marksSyncEntry(t *testing.T, env *dbEnv, entry, id string, props map[string]any) *substrate.Record {
	t.Helper()
	rec, err := env.ds.Put(context.Background(), marksSync, substrate.PutInput{Kind: entry, ID: id, Properties: props})
	if err != nil {
		t.Fatalf("sync %s: %v", id, err)
	}
	return rec
}

func marksRead(t *testing.T, env *dbEnv, kind, id string) substrate.Record {
	t.Helper()
	rec := env.do(t, http.MethodGet, "/api/v1/"+kind+"/"+id, env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	return decodeJSON[substrate.Record](t, rec)
}

func actorPath(a substrate.Actor) string {
	return vocabulary.RecordPath(corePackage+"/actor", vocabulary.ActorID(a))
}

func linkRefs(links []substrate.RecordLink) []string {
	var out []string
	for _, l := range links {
		out = append(out, l.Ref)
	}
	return out
}

// THE WORKED EXAMPLE. Grace is one contact built from two address books,
// work and home, synced by one function. Ada fixed the name by hand, the Chat
// app added a nickname for her, and sync supplied the rest. Each mark reads
// back as the plan writes it.
func TestTheWorkedExampleReadsBackFieldForField(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	contact, entry := marksKinds(t, env)

	work := marksSyncEntry(t, env, entry, mintedID("work-c123"), map[string]any{
		"name": "G. Hopper", "jobTitle": "Rear Admiral", "emails": []any{"grace@navy.example"},
	})
	grace := marksRead(t, env, entry, work.ID)
	graceID := ""
	if ref, ok := grace.Properties["contact"].(map[string]any); ok {
		_, graceID, _ = vocabulary.SplitRecordPath(ref[vocabulary.ReferenceValueKey].(string))
	}
	if graceID == "" {
		t.Fatalf("the work entry linked no contact: %v", grace.Properties)
	}
	home := marksSyncEntry(t, env, entry, mintedID("home-c900"), map[string]any{
		"name": "Grace M. Hopper", "emails": []any{"grace@home.example"},
		"contact": map[string]any{vocabulary.ReferenceValueKey: vocabulary.RecordPath(contact, graceID)},
	})

	// Ada types the name in the console.
	path := "/api/v1/" + contact + "/" + graceID
	wantStatus(t, env.do(t, http.MethodPatch, path, env.token, map[string]any{
		"properties": map[string]any{"name": "Grace Hopper"},
	}), http.StatusOK)
	// The Chat app writes the nickname for her.
	app := appScopedToken(t, env, "read:"+contact, "write:"+contact)
	body, _ := json.Marshal(map[string]any{"properties": map[string]any{"nickname": "Amazing Grace"}})
	wantStatus(t, appScopedDo(t, env, http.MethodPatch, path, app, body, "application/json"), http.StatusOK)

	got := marksRead(t, env, contact, graceID)
	if got.Properties["name"] != "Grace Hopper" || got.Properties["nickname"] != "Amazing Grace" || got.Properties["jobTitle"] != "Rear Admiral" {
		t.Fatalf("Grace's values = %v", got.Properties)
	}
	workPath, homePath := vocabulary.RecordPath(entry, work.ID), vocabulary.RecordPath(entry, home.ID)
	person, chat := actorPath(substrate.ActorPerson), actorPath(substrate.AppActor("notes.example.org"))

	// name: Ada typed it, so it is pinned and set by her, and what each
	// address book says is an alternative of its own.
	name := got.Marks["name"]
	if !name.Pinned || name.SetBy == nil || name.SetBy.Ref != person || name.OnBehalfOf != nil || len(name.DerivedFrom) != 0 || name.SetAt.IsZero() {
		t.Fatalf("name mark = %+v, want Ada's pin", name)
	}
	alts := map[string]any{}
	for _, a := range name.Alternatives {
		if a.DerivedFrom == nil || a.UpdatedAt.IsZero() {
			t.Fatalf("an alternative names no source record or time: %+v", a)
		}
		alts[a.DerivedFrom.Ref] = a.Value
	}
	if len(alts) != 2 || alts[workPath] != "G. Hopper" || alts[homePath] != "Grace M. Hopper" {
		t.Fatalf("name alternatives = %v, want one per address book", alts)
	}
	// nickname: the Chat app wrote it for Ada.
	nick := got.Marks["nickname"]
	if !nick.Pinned || nick.SetBy == nil || nick.SetBy.Ref != chat || nick.OnBehalfOf == nil || nick.OnBehalfOf.Ref != person {
		t.Fatalf("nickname mark = %+v, want the app's pin on behalf of Ada", nick)
	}
	// jobTitle: sync took it from the work address book.
	job := got.Marks["jobTitle"]
	if job.Pinned || job.SetBy != nil || !slices.Equal(linkRefs(job.DerivedFrom), []string{workPath}) || len(job.Alternatives) != 0 {
		t.Fatalf("jobTitle mark = %+v, want derived from the work book", job)
	}
	// emails: sync merged both address books.
	want := []string{workPath, homePath}
	slices.Sort(want)
	emails := got.Marks["emails"]
	if emails.Pinned || emails.SetBy != nil || !slices.Equal(linkRefs(emails.DerivedFrom), want) {
		t.Fatalf("emails mark = %+v, want derived from both books", emails)
	}
	if stored, _ := got.Properties["emails"].([]any); len(stored) != 2 {
		t.Fatalf("emails = %v, want both books' addresses", got.Properties["emails"])
	}
}

// A PINNED REQUIRED PROPERTY IS RELEASED, and sync fills it from an
// alternative. Before the marks the release was a null patch, which a
// required property refuses; the null patch still is.
func TestAPinnedRequiredPropertyIsReleasedToSync(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	contact, entry := marksKinds(t, env)
	work := marksSyncEntry(t, env, entry, mintedID("work-c1"), map[string]any{
		"name": "G. Hopper", "emails": []any{"grace@navy.example"},
	})
	ref, _ := marksRead(t, env, entry, work.ID).Properties["contact"].(map[string]any)
	_, id, _ := vocabulary.SplitRecordPath(ref[vocabulary.ReferenceValueKey].(string))
	path := "/api/v1/" + contact + "/" + id
	wantStatus(t, env.do(t, http.MethodPatch, path, env.token, map[string]any{
		"properties": map[string]any{"name": "Grace Hopper"},
	}), http.StatusOK)
	// A fresher sync yields to the pin.
	marksSyncEntry(t, env, entry, work.ID, map[string]any{"name": "Rear Admiral Hopper", "emails": []any{"grace@navy.example"}})
	if got := marksRead(t, env, contact, id); got.Properties["name"] != "Grace Hopper" || !got.Marks["name"].Pinned {
		t.Fatalf("the sync replaced the pinned name: %v %+v", got.Properties["name"], got.Marks["name"])
	}
	// The old way to let go is refused on a required property.
	rec := env.do(t, http.MethodPatch, path, env.token, map[string]any{"properties": map[string]any{"name": nil}})
	if rec.Code == http.StatusOK {
		t.Fatalf("a null patch emptied a required property: %s", rec.Body.String())
	}

	rec = env.do(t, http.MethodPost, path+"/release", env.token, map[string]any{"properties": []any{"name"}})
	wantStatus(t, rec, http.StatusOK)
	got := decodeJSON[substrate.Record](t, rec)
	if got.Properties["name"] != "Rear Admiral Hopper" {
		t.Fatalf("the release left name %v, want sync's alternative", got.Properties["name"])
	}
	name := got.Marks["name"]
	if name.Pinned || !slices.Equal(linkRefs(name.DerivedFrom), []string{vocabulary.RecordPath(entry, work.ID)}) || len(name.Alternatives) != 0 {
		t.Fatalf("released name mark = %+v, want sync's, from the work entry", name)
	}

	// Pin keeps what sync wrote, and both actions are in the history.
	rec = env.do(t, http.MethodPost, path+"/pin", env.token, map[string]any{"properties": []any{"name"}})
	wantStatus(t, rec, http.StatusOK)
	if m := decodeJSON[substrate.Record](t, rec).Marks["name"]; !m.Pinned || len(m.DerivedFrom) != 1 {
		t.Fatalf("pinned name mark = %+v, want pinned and still derived from the entry", m)
	}
	changes, err := env.ds.ChangesBefore(context.Background(), 0, substrate.ChangeFilter{RecordID: id, Kinds: []string{contact}}, 20)
	if err != nil {
		t.Fatal(err)
	}
	var pinned, released bool
	for _, c := range changes {
		if names, ok := c.Payload["pinned"].([]any); ok && len(names) == 1 && names[0] == "name" {
			pinned = true
		}
		if names, ok := c.Payload["released"].([]any); ok && len(names) == 1 && names[0] == "name" {
			released = true
		}
	}
	if !pinned || !released {
		t.Fatalf("history holds the pin %v and the release %v, want both", pinned, released)
	}
	// A pin of nothing is refused, by name.
	rec = env.do(t, http.MethodPost, path+"/pin", env.token, map[string]any{"properties": []any{"noSuchProperty"}})
	if rec.Code != http.StatusUnprocessableEntity && rec.Code != http.StatusBadRequest {
		t.Fatalf("a pin of an undeclared property answered %d: %s", rec.Code, rec.Body.String())
	}
}
