package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func TestRESTUnknownKindIs404(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	rec := env.do(t, http.MethodGet, "/api/v1/samples.substrate.reamde.dev/people/widgets/p1", env.token, nil)
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
	if msg := decodeJSON[substrate.ErrorEnvelope](t, rec).Error.Message; !strings.Contains(msg, "unknown kind samples.substrate.reamde.dev/people/widgets") {
		t.Fatalf("message = %q, want the kind named", msg)
	}
}

func TestRESTCRUD(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))

	created := createRecord(t, env.testEnv, env.token, personKind, map[string]any{"title": "Ada", "name": "Ada"})
	if created.Kind != personKind {
		t.Fatalf("POST must stamp the body's kind, got %q", created.Kind)
	}
	if got := env.get(t, personKind, created.ID); got == nil {
		t.Fatalf("the create did not land under %s", personKind)
	}

	rec := env.do(t, http.MethodGet, peoplePath+"/"+created.ID, env.token, nil)
	wantStatus(t, rec, http.StatusOK)

	// PUT to a fresh id CREATES it, so the status is 201: the code
	// reports what the write did, not which method carried it.
	fixed := mintedID("fixed1")
	rec = env.do(t, http.MethodPut, peoplePath+"/"+fixed, env.token, map[string]any{"properties": map[string]any{"name": "Grace"}})
	wantStatus(t, rec, http.StatusCreated)
	if got := env.get(t, personKind, fixed); got == nil || got.Properties["name"] != "Grace" {
		t.Fatalf("PUT must address the path id, stored %+v", got)
	}
	// A second PUT to the same id is an UPDATE, so it is 200.
	rec = env.do(t, http.MethodPut, peoplePath+"/"+fixed, env.token, map[string]any{"properties": map[string]any{"name": "Grace H"}})
	wantStatus(t, rec, http.StatusOK)

	rec = env.do(t, http.MethodPatch, peoplePath+"/"+created.ID, env.token,
		map[string]any{"properties": map[string]any{"pronouns": "she/her"}})
	wantStatus(t, rec, http.StatusOK)
	patched := decodeJSON[substrate.Record](t, rec)
	if patched.Properties["pronouns"] != "she/her" || patched.Properties["name"] != "Ada" {
		t.Fatalf("patched props = %v", patched.Properties)
	}

	rec = env.do(t, http.MethodDelete, peoplePath+"/"+created.ID, env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	deleted := decodeJSON[substrate.Record](t, rec)
	if deleted.DeletedAt == nil {
		t.Fatal("delete must be a soft delete (deletedAt set)")
	}

	rec = env.do(t, http.MethodGet, peoplePath+"/nope", env.token, nil)
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
}

// restContactActor is the connector that syncs restContacts' contacts.
const restContactActor = substrate.Actor("connector:contacts")

// restContacts installs, under the repository's own authority, a contact
// kind a connector syncs and the people package's mapping of it onto a
// person: matched by email, offering its display name as the person's name.
func restContacts(t *testing.T, env *dbEnv) string {
	t.Helper()
	ctx := context.Background()
	pkg := env.repository + "/contacts"
	contact := "~/contacts/contact"
	if _, err := env.ds.ApplyVocabularyDocuments(ctx, substrate.ActorSystem, []map[string]any{
		vocabulary.PackageManifest(pkg, 1),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "contact"}, map[string]any{
			"displayTemplate": "{displayName}",
			"properties": map[string]any{
				"displayName": map[string]any{"type": "string"},
				"email":       map[string]any{"type": "email"},
				"person": map[string]any{
					"type": "reference", "kind": personKind,
					"required": true, "mustExist": true, "subject": true,
				},
			},
		}),
	}); err != nil {
		t.Fatalf("install the contact source: %v", err)
	}
	if err := enginetest.DeclareMappings(ctx, env.ds, enginetest.PeopleMapping("contactperson", map[string]any{
		"from": contact, "property": "person",
		"match": []any{map[string]any{"from": "email", "to": "emails"}},
		"map": map[string]any{
			"name":   map[string]any{"path": "displayName"},
			"emails": map[string]any{"path": "email", "merge": "union"},
		},
	})); err != nil {
		t.Fatalf("declare the contact mapping: %v", err)
	}
	return contact
}

// GET one carries the marks; lists never do. The owner names a person
// through the API, a connector's contact offers another name, and the
// owner's name is pinned with the contact's beside it as an alternative,
// keyed by the contact record.
func TestRESTGetOneCarriesTheMarks(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	contact := restContacts(t, env)

	sam := createRecord(t, env.testEnv, env.token, personKind, map[string]any{"name": "Sam", "emails": []any{"sam@acme.com"}})
	if _, err := env.ds.Put(context.Background(), restContactActor, substrate.PutInput{
		Kind: contact, ID: mintedID("c-sam"),
		Properties: map[string]any{"displayName": "Samuel Jones", "email": "sam@acme.com"},
	}); err != nil {
		t.Fatalf("sync the contact: %v", err)
	}

	rec := env.do(t, http.MethodGet, peoplePath+"/"+sam.ID, env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	var got map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	raw, ok := got["marks"]
	if !ok {
		t.Fatalf("GET one lost its marks: %s", rec.Body.String())
	}
	var marks map[string]substrate.PropertyMark
	if err := json.Unmarshal(raw, &marks); err != nil {
		t.Fatal(err)
	}
	name := marks["name"]
	person := vocabulary.RecordPath(corePackage+"/actor", vocabulary.ActorID(substrate.ActorPerson))
	if !name.Pinned || name.SetBy == nil || name.SetBy.Ref != person || len(name.Alternatives) != 1 ||
		name.Alternatives[0].DerivedFrom == nil || name.Alternatives[0].DerivedFrom.Ref != contact+"/"+mintedID("c-sam") ||
		name.Alternatives[0].Value != "Samuel Jones" {
		t.Fatalf("marks = %+v, want the owner's name pinned and the contact's offered beside it", marks)
	}

	// The list projection stays lean: no marks on a list read.
	rec = env.do(t, http.MethodGet, recordsOf(t, personKind), env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	if strings.Contains(rec.Body.String(), `"marks"`) {
		t.Fatalf("list carries marks: %s", rec.Body.String())
	}
}

// A record carries what it points at; what points BACK is a list under
// `filter.referencing`, and the target is matched by its canonical id and
// every former one, so a pointer written before a merge still counts.
func TestRESTReferencingFollowsAMergedTarget(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	member, _ := recordsStaff(t, env)

	env.put(t, personKind, "winner", map[string]any{"name": "Sam"})
	env.put(t, personKind, "loser", map[string]any{"name": "S."})
	env.put(t, member, "p2", map[string]any{"name": "Ada", "manager": map[string]any{"ref": personKind + "/loser"}})
	if _, err := env.ds.Merge(context.Background(), substrate.ActorSystem, substrate.MergeInput{
		Kind: personKind, Winner: "winner", Loser: "loser",
	}); err != nil {
		t.Fatalf("merge: %v", err)
	}

	// The record body never carries its pointers-back.
	rec := env.do(t, http.MethodGet, peoplePath+"/winner", env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	if body := rec.Body.String(); strings.Contains(body, "referencing") || strings.Contains(body, "matches") {
		t.Fatalf("the record body carries a reverse read: %s", body)
	}

	rec = env.do(t, http.MethodGet, filterPath(t, substrate.Filter{
		Referencing: &substrate.Referencing{Ref: personKind + "/winner"},
	}), env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	page := decodeJSON[substrate.Page](t, rec)
	// The merge's own record names the winner too; beside it, the one
	// pointer is the one written at the loser.
	var pointers []string
	for _, r := range page.Records {
		if r.Kind != corePackage+"/recordmerge" {
			pointers = append(pointers, r.Kind+"/"+r.ID)
		}
	}
	if len(pointers) != 1 || pointers[0] != member+"/p2" || len(page.Records) != 2 {
		t.Fatalf("referencing page = %v beside %d records, want p2 and the merge", pointers, len(page.Records))
	}
	if got := page.Matches[member+"/p2"]; len(got) != 1 || got[0].Property != "manager" {
		t.Fatalf("matches = %+v", page.Matches)
	}
}

// The engine refuses a write whose revision falls below the stored one, on
// the record path's PUT and PATCH alike, as `validation` narrowed by the
// reason `stale_revision`, and leaves the stored value; removing the revision
// is no stale write and carries no reason (decision 0162). The envelope for
// every write door is TestRESTStaleRevisionIsValidationWithItsReason.
func TestRESTStaleRevisionIsRefusedWithItsReason(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	if err := enginetest.InstallCatalogues(context.Background(), env.ds, "health"); err != nil {
		t.Fatalf("install the health catalogue: %v", err)
	}
	const observation = "substrate.reamde.dev/observations/observation"
	weighing := func(value string, revision int) map[string]any {
		return map[string]any{
			"metric": map[string]any{"ref": "substrate.reamde.dev/observations/metric/health/body_mass"},
			"form":   "gauge", "value": map[string]any{"value": value, "unit": "kg"},
			"at": "2026-10-07T06:41:12+03:00", "utcOffset": 180, "revision": revision,
		}
	}
	env.put(t, observation, "w", weighing("72.40", 3))
	path := "/api/v1/" + vocabulary.RecordPath(observation, "w")

	for _, w := range []struct {
		method string
		body   map[string]any
	}{
		{http.MethodPut, map[string]any{"properties": weighing("71.00", 2)}},
		{http.MethodPatch, map[string]any{"properties": map[string]any{"revision": 2, "value": map[string]any{"value": "71.00", "unit": "kg"}}}},
	} {
		rec := env.do(t, w.method, path, env.token, w.body)
		wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)
		p := decodeJSON[substrate.ErrorEnvelope](t, rec).Error
		if p.Reason != substrate.ReasonStaleRevision || !strings.Contains(p.Message, "below the stored revision") {
			t.Fatalf("%s: reason %q, message %q; want stale_revision naming the stored revision", w.method, p.Reason, p.Message)
		}
	}
	if got := env.get(t, observation, "w").Properties["value"].(map[string]any)["value"]; got != "72.40" {
		t.Fatalf("a stale write landed: value %v", got)
	}

	rec := env.do(t, http.MethodPatch, path, env.token, map[string]any{"properties": map[string]any{"revision": nil}})
	wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)
	if strings.Contains(rec.Body.String(), `"reason"`) {
		t.Fatalf("a removed revision answered a reason: %s", rec.Body.String())
	}
}

// Issue 553 on the engine: accepting a change request whose diff re-asserts
// the stored value applies no change, and the API answers ONE refusal, 409
// conflict naming that reason, never a validation failure and never the words
// "version conflict"; a stale ifVersion is still the version conflict.
func TestRESTFailedAcceptOfANoOpDiffIsConflict(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	const request = "substrate.reamde.dev/core/recordpatchrequest"
	env.put(t, personKind, "p1", map[string]any{"name": "Ada"})
	noop := env.put(t, request, "noop", map[string]any{
		"diff":   map[string]any{"properties": map[string]any{"name": "Ada"}},
		"target": vocabulary.RecordPath(personKind, "p1"),
	})

	rec := env.do(t, http.MethodPatch, "/api/v1/"+vocabulary.RecordPath(request, "noop"), env.token,
		map[string]any{"properties": map[string]any{"decision": "accepted"}, "ifVersion": noop.Version})
	wantErrorCode(t, rec, http.StatusConflict, codeConflict)
	body := decodeJSON[substrate.ErrorEnvelope](t, rec)
	if !strings.Contains(body.Error.Message, "applied no change") {
		t.Fatalf("message does not name the reason: %q", body.Error.Message)
	}
	if strings.Contains(body.Error.Message, "version conflict") {
		t.Fatalf("a failed accept must not claim a version conflict: %q", body.Error.Message)
	}
	if len(body.Error.Problems) != 0 {
		t.Fatalf("a failed accept is not a validation refusal: %v", body.Error.Problems)
	}

	stale := env.put(t, personKind, "p1", map[string]any{"name": "Ada Lovelace"}).Version - 1
	rec = env.do(t, http.MethodPatch, peoplePath+"/p1", env.token,
		map[string]any{"properties": map[string]any{"name": "Ada"}, "ifVersion": stale})
	wantErrorCode(t, rec, http.StatusConflict, codeConflict)
	if got := decodeJSON[substrate.ErrorEnvelope](t, rec); !strings.Contains(got.Error.Message, "version conflict") {
		t.Fatalf("a stale ifVersion still reads as a version conflict: %q", got.Error.Message)
	}
}
