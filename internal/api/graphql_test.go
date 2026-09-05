package api

import (
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/graphql-go/graphql"

	"github.com/geoah/substrate/internal/gql"
	"github.com/geoah/substrate/internal/substrate"
)

const graphqlPath = "/api/v1/graphql"

type gqlResponse struct {
	Data   map[string]any `json:"data"`
	Errors []struct {
		Message    string         `json:"message"`
		Extensions map[string]any `json:"extensions"`
	} `json:"errors"`
}

func (e *testEnv) gql(t *testing.T, token, query string, vars map[string]any) gqlResponse {
	t.Helper()
	rec := e.do(t, http.MethodPost, graphqlPath, token, map[string]any{"query": query, "variables": vars})
	wantStatus(t, rec, http.StatusOK)
	out := decodeJSON[gqlResponse](t, rec)
	if len(out.Errors) > 0 {
		t.Fatalf("graphql errors: %v (query %s)", out.Errors, query)
	}
	return out
}

// The schema cache key must move when a type's DECLARATION moves, not only
// when types come and go: schema is records now, and a property added
// through the record path activates on commit — GraphQL must not keep
// serving the old shape until an unrelated type add/remove.
func TestRegistryKeyTracksDefinitions(t *testing.T) {
	widget := func(props map[string]any) []substrate.KindInfo {
		return []substrate.KindInfo{{
			Identity: "example.substrate.reamde.dev/example/widget", Version: 1, Plural: "widgets",
			Definition: map[string]any{"properties": props},
		}}
	}
	base := widget(map[string]any{"name": map[string]any{"type": "string"}})
	same := widget(map[string]any{"name": map[string]any{"type": "string"}})
	extended := widget(map[string]any{
		"name":  map[string]any{"type": "string"},
		"count": map[string]any{"type": "float"},
	})
	if gql.RegistryKey(base) != gql.RegistryKey(same) {
		t.Fatal("identical registries must share a key")
	}
	if gql.RegistryKey(base) == gql.RegistryKey(extended) {
		t.Fatal("a definition change must move the registry key, or the cached GraphQL schema serves the old shape")
	}
}

func TestGraphQLSchemaBuildsFromTheRegistry(t *testing.T) {
	schema, err := gql.BuildSchema(testTypes())
	if err != nil {
		t.Fatalf("buildSchema: %v", err)
	}
	names := map[string]bool{}
	for name := range schema.TypeMap() {
		names[name] = true
	}
	for _, want := range []string{"Record", "Temporal", "HasStatus", "Person", "Task", "Book", "GenericRecord"} {
		if !names[want] {
			t.Errorf("schema is missing type %q", want)
		}
	}
}

// D1: Record.history is addressed by FULL identity, not the bare
// id. An id can repeat across types, so a history read must be scoped to (type,
// id) — the changelog rows of a DIFFERENT type sharing the same id must not
// leak into this record's audit trail.
func TestGraphQLHistoryIsScopedByType(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]

	// A person materialized at id "shared", plus changelog rows for the SAME id
	// under two types — the collision A9 removes.
	ds.records["shared"] = &substrate.Record{
		ID: "shared", Kind: "samples.substrate.reamde.dev/people/person",
		Properties: map[string]any{"title": "Ada"}, Version: 1,
	}
	ds.commit(substrate.Change{
		TS: time.Unix(1, 0).UTC(), Actor: substrate.ActorAPI, Op: substrate.OpPut,
		RecordID: "shared", Kind: "samples.substrate.reamde.dev/people/person",
	})
	ds.commit(substrate.Change{
		TS: time.Unix(2, 0).UTC(), Actor: substrate.ActorAPI, Op: substrate.OpPut,
		RecordID: "shared", Kind: "samples.substrate.reamde.dev/tasks/task",
	})

	res := env.gql(t, tok,
		`query ($kind: String!, $id: ID!) {
			record(kind: $kind, id: $id) { id kind history { seq kind recordId } }
		}`,
		map[string]any{"kind": "samples.substrate.reamde.dev/people/person", "id": "shared"})

	record, _ := res.Data["record"].(map[string]any)
	history, _ := record["history"].([]any)
	if len(history) != 1 {
		t.Fatalf("history has %d rows, want 1 — the task-typed row sharing id %q leaked into the person's history", len(history), "shared")
	}
	row, _ := history[0].(map[string]any)
	if row["kind"] != "samples.substrate.reamde.dev/people/person" {
		t.Fatalf("history row kind = %v, want the person kind only", row["kind"])
	}
}

func TestGraphQLPutPatchRecordRoundTrip(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")

	put := env.gql(t, tok, `mutation ($in: JSON!) { put(input: $in) { id kind title ... on Person { name company } } }`,
		map[string]any{"in": map[string]any{
			"kind": "samples.substrate.reamde.dev/people/person",
			// Everything authored is a property: `title` sits in the map with
			// the declared ones.
			"properties": map[string]any{"title": "Ada", "name": "Ada"},
		}})
	created, _ := put.Data["put"].(map[string]any)
	if created["title"] != "Ada" || created["name"] != "Ada" {
		t.Fatalf("put returned %v", created)
	}
	id, _ := created["id"].(string)

	patched := env.gql(t, tok, `mutation ($id: ID!, $in: JSON!) { patch(kind: "samples.substrate.reamde.dev/people/person", id: $id, input: $in) { id version ... on Person { company } } }`,
		map[string]any{"id": id, "in": map[string]any{"properties": map[string]any{"company": "Analytical"}}})
	got, _ := patched.Data["patch"].(map[string]any)
	if got["company"] != "Analytical" {
		t.Fatalf("patch returned %v", got)
	}

	one := env.gql(t, tok, `query ($id: ID!) { record(kind: "samples.substrate.reamde.dev/people/person", id: $id) { id kind title labels } }`, map[string]any{"id": id})
	ent, _ := one.Data["record"].(map[string]any)
	if ent["id"] != id || ent["kind"] != "samples.substrate.reamde.dev/people/person" {
		t.Fatalf("record returned %v", ent)
	}
}

// A reference property renders through its generated GraphQL object type, and
// it does so with NO link properties declared: `manager` is a plain reference,
// and it still selects as `{ ref }`. The write sends the bare path (the
// shorthand) and the read hands back the object, which is the round trip
// decision 0044 asks for.
func TestGraphQLReferenceRoundTrip(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")

	const path = "samples.substrate.reamde.dev/people/person/boss1"
	put := env.gql(t, tok,
		`mutation ($in: JSON!) { put(input: $in) { id ... on Person { manager { ref } } } }`,
		map[string]any{"in": map[string]any{
			"kind": "samples.substrate.reamde.dev/people/person",
			"properties": map[string]any{
				"title":   "Ada",
				"manager": path,
			},
		}})
	created, _ := put.Data["put"].(map[string]any)
	ref, _ := created["manager"].(map[string]any)
	if ref == nil || ref["ref"] != path {
		t.Fatalf("manager reference = %v, want an object holding ref %q", created["manager"], path)
	}
}

// A reference is an OBJECT even with no link properties, so selecting it bare
// is the query error: the mirror of the old rule, and the check that the
// one-shape decision reaches the schema and not only the stored value.
func TestGraphQLReferenceRequiresASubSelection(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")

	res := env.gqlRaw(t, tok,
		`query { records(first: 1) { nodes { ... on Person { manager } } } }`, nil)
	if len(res.Errors) == 0 {
		t.Fatalf("a bare selection of a reference object must be a query error, got %+v", res.Data)
	}
	if !strings.Contains(res.Errors[0].Message, "must have a sub selection") {
		t.Fatalf("error = %q", res.Errors[0].Message)
	}
}

// The retired {kind, id} pair is not what the object carries: a client asking
// for it is told at query time rather than handed two nulls.
func TestGraphQLReferenceRefusesTheRetiredPair(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")

	res := env.gqlRaw(t, tok,
		`query { records(first: 1) { nodes { ... on Person { manager { kind id } } } } }`, nil)
	if len(res.Errors) == 0 {
		t.Fatalf("selecting kind/id on a reference must be a query error, got %+v", res.Data)
	}
	if !strings.Contains(res.Errors[0].Message, `Cannot query field "kind"`) {
		t.Fatalf("error = %q", res.Errors[0].Message)
	}
}

func TestGraphQLRecordsUsesTheJSONFilter(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]

	// title is a PROPERTY, not a top-level put field — the strict
	// GraphQL input decoder (codex regress #9) refuses it at the top level, so
	// it rides inside properties like every authored value.
	env.gql(t, tok, `mutation ($in: JSON!) { put(input: $in) { id } }`, map[string]any{
		"in": map[string]any{"kind": "samples.substrate.reamde.dev/people/person", "properties": map[string]any{"title": "Ada", "name": "Ada"}},
	})
	env.gql(t, tok, `mutation ($in: JSON!) { put(input: $in) { id } }`, map[string]any{
		"in": map[string]any{"kind": "samples.substrate.reamde.dev/tasks/task", "properties": map[string]any{"title": "ship it", "status": "open"}},
	})

	res := env.gql(t, tok, `query ($f: JSON, $o: JSON) {
		records(filter: $f, orderBy: $o, first: 10) {
			cursor
			nodes { id kind title ... on Task { status } }
		}
	}`, map[string]any{
		"f": map[string]any{"kinds": []string{"samples.substrate.reamde.dev/tasks/task"}},
		"o": []map[string]any{{"property": "created_at", "desc": true}},
	})
	conn, _ := res.Data["records"].(map[string]any)
	nodes, _ := conn["nodes"].([]any)
	if len(nodes) != 1 {
		t.Fatalf("nodes = %v", nodes)
	}
	node, _ := nodes[0].(map[string]any)
	if node["kind"] != "samples.substrate.reamde.dev/tasks/task" || node["status"] != "open" {
		t.Fatalf("node = %v", node)
	}
	if len(ds.lastQuery.OrderBy) != 1 || !ds.lastQuery.OrderBy[0].Desc {
		t.Fatalf("orderBy did not reach the dataset: %+v", ds.lastQuery.OrderBy)
	}
}

// `filter` is a JSON scalar, so its DESCRIPTION is the whole grammar a client
// that has only introspection can read. A tester's agent, holding the
// `graphql` tool and nothing else, guessed the shape, got a bare unknown-field
// refusal, and told its user the substrate "rejects date-field filtering" —
// which it never did. The description carries the date range that works.
func TestGraphQLFilterArgumentDescribesItsGrammar(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	res := env.gql(t, tok, `{ __type(name: "Query") { fields { name args { name description } } } }`, nil)

	args := map[string]string{}
	typ, _ := res.Data["__type"].(map[string]any)
	fields, _ := typ["fields"].([]any)
	for _, f := range fields {
		fm, _ := f.(map[string]any)
		if fm["name"] != "records" {
			continue
		}
		list, _ := fm["args"].([]any)
		for _, a := range list {
			am, _ := a.(map[string]any)
			name, _ := am["name"].(string)
			args[name], _ = am["description"].(string)
		}
	}
	for _, name := range []string{"filter", "orderBy", "first", "after"} {
		if args[name] == "" {
			t.Fatalf("records(%s) is introspectable but undescribed: %v", name, args)
		}
	}
	// The grammar's three load-bearing parts: where a kind goes, where a
	// predicate goes, and that a timestamp is a comparable instant.
	for _, want := range []string{"kinds", "properties", "gte", "RFC3339", "at"} {
		if !strings.Contains(args["filter"], want) {
			t.Fatalf("filter description does not mention %q: %s", want, args["filter"])
		}
	}
	if !strings.Contains(args["orderBy"], "property") {
		t.Fatalf("orderBy description does not name its key: %s", args["orderBy"])
	}
}

// The day-agenda query the filter description hands a caller, run: the
// example is executed rather than asserted about, so a description that stops
// working is a failing test and not a client's afternoon.
func TestGraphQLDayRangeFilterReachesTheDataset(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]
	env.gql(t, tok, `{
		records(filter: {kinds: ["samples.substrate.reamde.dev/tasks/task"],
		                 properties: {at: {gte: "2026-08-15T00:00:00Z", lt: "2026-08-16T00:00:00Z"}}},
		        orderBy: [{property: "at"}]) { nodes { id } }
	}`, nil)

	at, ok := ds.lastQuery.Filter.Properties["at"]
	if !ok {
		t.Fatalf("the range predicate did not reach the dataset: %+v", ds.lastQuery.Filter)
	}
	if at.Gte != "2026-08-15T00:00:00Z" || at.Lt != "2026-08-16T00:00:00Z" {
		t.Fatalf("bounds = %+v", at)
	}
	if len(ds.lastQuery.OrderBy) != 1 || ds.lastQuery.OrderBy[0].Property != "at" {
		t.Fatalf("orderBy = %+v", ds.lastQuery.OrderBy)
	}
}

// A mis-spelled filter key is the CALLER's error, and the refusal has to say
// so twice over: classified `validation` rather than `internal` (a server that
// reports itself broken is a server a client stops trying), and naming the
// keys the argument does take.
func TestGraphQLBadFilterIsAValidationErrorNamingTheKeys(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	rec := env.do(t, http.MethodPost, graphqlPath, tok, map[string]any{
		"query": `query ($f: JSON) { records(filter: $f) { nodes { id } } }`,
		// The shape an agent guesses for a day agenda: the predicate written
		// straight onto the filter instead of under `properties`.
		"variables": map[string]any{"f": map[string]any{"at": map[string]any{"gte": "2026-08-15T00:00:00Z"}}},
	})
	wantStatus(t, rec, http.StatusOK)
	out := decodeJSON[gqlResponse](t, rec)
	if len(out.Errors) != 1 {
		t.Fatalf("errors = %+v", out.Errors)
	}
	if code, _ := out.Errors[0].Extensions["code"].(string); code != codeValidation {
		t.Fatalf("a caller's misspelling is not the server's fault: code = %q (%+v)", code, out.Errors[0])
	}
	for _, want := range []string{`unknown field "at"`, "properties", "kinds"} {
		if !strings.Contains(out.Errors[0].Message, want) {
			t.Fatalf("refusal does not mention %q: %s", want, out.Errors[0].Message)
		}
	}
}

func TestGraphQLNeedsAuth(t *testing.T) {
	env := newTestEnv(t)
	rec := env.do(t, http.MethodPost, graphqlPath, "", map[string]any{"query": "{ __typename }"})
	wantErrorCode(t, rec, http.StatusUnauthorized, codeAuth)
}

// The request's escape-hatch field is the GraphQL-over-HTTP spec's
// `extensions` — a rename sweep once shipped it as `bundles`, which made the
// strict decoder refuse every spec-compliant client that sent the real key.
func TestGraphQLRequestExtensionsKeyIsSpelledExtensions(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	rec := env.do(t, http.MethodPost, graphqlPath, tok, map[string]any{
		"query": "{ __typename }", "extensions": map[string]any{"trace": true},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("spec `extensions` key refused: %d %s", rec.Code, rec.Body.String())
	}
	rec = env.do(t, http.MethodPost, graphqlPath, tok, map[string]any{
		"query": "{ __typename }", "bundles": map[string]any{},
	})
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
}

func TestGraphQLSchemaIsCachedPerRegistryFingerprint(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]

	env.gql(t, tok, `{ __typename }`, nil)
	env.gql(t, tok, `{ __typename }`, nil)

	// Installing a type changes the fingerprint and must rebuild.
	ds.types = append(ds.types, substrate.KindInfo{
		Identity: "beeper.connectors.substrate.reamde.dev/beeper/thread", Name: "thread",
		Authority: "beeper.connectors.substrate.reamde.dev", Version: 1, Plural: "threads",
		Source:     "installed",
		Definition: map[string]any{"plural": "threads", "properties": map[string]any{"subject": map[string]any{"type": "string"}}},
	})
	// An installed type is ALWAYS authority-prefixed: the beeper
	// bundle's thread is Beeper_Thread, never the bare Thread.
	res := env.gql(t, tok, `{ __type(name: "Beeper_Thread") { name } }`, nil)
	typ, _ := res.Data["__type"].(map[string]any)
	if typ == nil || typ["name"] != "Beeper_Thread" {
		t.Fatalf("installed type did not reach the schema under its prefixed name: %v", res.Data)
	}
}

// A reference that carries link data projects as an object type of its own:
// the path under `ref`, the declared link properties typed beside it, and
// `target` resolving the referent through the registry.
func TestGraphQLReferenceHistoryAndCapabilityInterfaces(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]

	at := time.Unix(1_700_000_500, 0).UTC()
	ds.records["team1"] = &substrate.Record{
		ID: "team1", Kind: "samples.substrate.reamde.dev/people/person", Title: "Analytical",
		Properties: map[string]any{"name": "Analytical"},
	}
	ds.records["msg1"] = &substrate.Record{
		ID: "msg1", Kind: "samples.substrate.reamde.dev/messaging/conversationmessage", Title: "hi", At: &at,
		Properties: map[string]any{
			"text":   "hi",
			"author": map[string]any{"ref": "samples.substrate.reamde.dev/people/person/team1", "since": 2020},
		},
	}
	ds.changes = append(ds.changes, substrate.Change{
		Seq: 1, TS: at, Actor: substrate.ActorAPI, Op: substrate.OpPut,
		RecordID: "msg1", Kind: "samples.substrate.reamde.dev/messaging/conversationmessage",
	})

	res := env.gql(t, tok, `{
		record(kind: "samples.substrate.reamde.dev/messaging/conversationmessage", id: "msg1") {
			id
			... on Temporal { at endsAt }
			... on Conversationmessage {
				author { ref since target { id ... on Person { name } } }
			}
			history(first: 5) { seq op }
		}
	}`, nil)
	ent, _ := res.Data["record"].(map[string]any)
	if ent["at"] == nil {
		t.Fatalf("Temporal.at not resolved: %v", ent)
	}
	author, _ := ent["author"].(map[string]any)
	target, _ := author["target"].(map[string]any)
	if author["ref"] != "samples.substrate.reamde.dev/people/person/team1" || author["since"] != float64(2020) {
		t.Fatalf("author = %v", author)
	}
	if target["id"] != "team1" || target["name"] != "Analytical" {
		t.Fatalf("author target = %v", target)
	}
	history, _ := ent["history"].([]any)
	if len(history) != 1 {
		t.Fatalf("history = %v", history)
	}
}

// The generated object is named from (kind, property) and never depends on
// which other kinds are in the registry; a plain reference stays the Reference
// scalar rather than growing an object of its own.
func TestGraphQLLinkDataReferenceIsItsOwnType(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")

	res := env.gql(t, tok, `{
		message: __type(name: "Conversationmessage") { fields { name type { name kind } } }
		ref: __type(name: "ConversationmessageAuthorReference") {
			fields { name type { name kind ofType { name } } }
		}
		person: __type(name: "Person") { fields { name type { name kind } } }
	}`, nil)

	refType, _ := res.Data["ref"].(map[string]any)
	if refType == nil {
		t.Fatalf("no generated type for the link-carrying reference: %v", res.Data)
	}
	got := map[string]string{}
	for _, f := range refType["fields"].([]any) {
		field, _ := f.(map[string]any)
		typ, _ := field["type"].(map[string]any)
		name, _ := typ["name"].(string)
		if name == "" { // NON_NULL wrapper
			inner, _ := typ["ofType"].(map[string]any)
			name, _ = inner["name"].(string)
		}
		got[field["name"].(string)] = name
	}
	for field, want := range map[string]string{"ref": "Reference", "since": "Int", "target": "Record"} {
		if got[field] != want {
			t.Fatalf("%s field %q = %q, want %q", "ConversationmessageAuthorReference", field, got[field], want)
		}
	}

	// A reference that declares NO link data generates a type too (0044): the
	// value has one shape, so the schema has one shape, and adding a link
	// property later adds a field instead of replacing a scalar with an object.
	person, _ := res.Data["person"].(map[string]any)
	var seen bool
	for _, f := range person["fields"].([]any) {
		field, _ := f.(map[string]any)
		if field["name"] != "manager" {
			continue
		}
		seen = true
		typ, _ := field["type"].(map[string]any)
		if typ["name"] != "PersonManagerReference" || typ["kind"] != "OBJECT" {
			t.Fatalf("a reference without link data must still be its own object type: %v", typ)
		}
	}
	if !seen {
		t.Fatal("Person declares no manager field")
	}
}

// record(id) exposes the property provenance as a JSON field; records in a
// list resolve it null, because only single-record reads assemble it.
func TestGraphQLPropertyMeta(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]

	at := time.Unix(1_700_000_000, 0).UTC()
	ds.records["p1"] = &substrate.Record{
		ID: "p1", Kind: "samples.substrate.reamde.dev/people/person",
		Properties: map[string]any{"name": "Sam"},
	}
	ds.meta["p1"] = map[string]substrate.PropertyMeta{
		"name": {Manager: "owner", UpdatedAt: at, Alternatives: []substrate.PropertyAlternative{
			{Actor: "google.connectors.substrate.reamde.dev/google/people", Value: "Samuel Jones", UpdatedAt: at},
		}},
	}

	res := env.gql(t, tok, `{ record(kind: "samples.substrate.reamde.dev/people/person", id: "p1") { id propertyMeta } }`, nil)
	ent, _ := res.Data["record"].(map[string]any)
	meta, _ := ent["propertyMeta"].(map[string]any)
	name, _ := meta["name"].(map[string]any)
	if name["manager"] != "owner" {
		t.Fatalf("propertyMeta = %#v", ent["propertyMeta"])
	}
	alts, _ := name["alternatives"].([]any)
	if len(alts) != 1 {
		t.Fatalf("alternatives = %#v", name["alternatives"])
	}

	list := env.gql(t, tok, `{ records(first: 10) { nodes { id propertyMeta } } }`, nil)
	conn, _ := list.Data["records"].(map[string]any)
	for _, n := range conn["nodes"].([]any) {
		node, _ := n.(map[string]any)
		if node["propertyMeta"] != nil {
			t.Fatalf("a list node carries propertyMeta: %v", node)
		}
	}
}

// Incoming references do not inflate GraphQL record reads. The dedicated REST
// resource owns their pagination.
func TestGraphQLIncomingIsNotOnRecord(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]

	ds.records["p1"] = &substrate.Record{
		ID: "p1", Kind: "samples.substrate.reamde.dev/people/person",
		Properties: map[string]any{"name": "Sam"},
	}
	ds.incoming["p1"] = []substrate.IncomingReference{
		{Property: "person", From: substrate.IncomingSource{
			ID: "people-c1001", Kind: "google.connectors.substrate.reamde.dev/google/contact", Title: "Samuel Jones",
		}},
	}

	// The GraphQL Record type has NO `incoming` field (record 57; removed at
	// the v1 freeze, ticket 004): selecting it is a validation error, not a
	// null. Query it raw (the gql helper rejects any error) and assert the
	// field cannot be selected.
	rec := env.do(t, http.MethodPost, graphqlPath, tok, map[string]any{
		"query": `{ record(kind: "samples.substrate.reamde.dev/people/person", id: "p1") { id incoming } }`,
	})
	res := decodeJSON[gqlResponse](t, rec)
	if len(res.Errors) == 0 {
		t.Fatalf("expected a validation error for the removed `incoming` field, got %+v", res.Data)
	}

	// The record still reads fine without it.
	ok := env.gql(t, tok, `{ record(kind: "samples.substrate.reamde.dev/people/person", id: "p1") { id } }`, nil)
	ent, _ := ok.Data["record"].(map[string]any)
	if ent["id"] != "p1" {
		t.Fatalf("record = %#v", ent)
	}
}

// Installing a package whose singular collides with a shipped kind must NOT
// rename the shipped kind's GraphQL name. The shipped task keeps the bare
// "Task"; the installed task is package-prefixed "Alpha_Task". Both are
// resolvable and distinct — the pre-A14 first-claim scheme renamed the shipped
// type to "TasksTask" when the bundle sorted ahead of it.
func TestGraphQLNamesDoNotDependOnRegistryOrder(t *testing.T) {
	shipped := substrate.KindInfo{
		Identity: "substrate.reamde.dev/core/task", Name: "task",
		Authority: "substrate.reamde.dev", Package: "core",
		Version: 1, Plural: "tasks", Source: "builtin",
		Definition: map[string]any{"properties": map[string]any{"note": map[string]any{"type": "string"}}},
	}
	installed := substrate.KindInfo{
		Identity: "acme.example.com/alpha/task", Name: "task",
		Authority: "acme.example.com", Package: "alpha",
		Version: 1, Plural: "tasks", Source: "installed",
		Definition: map[string]any{"properties": map[string]any{"note": map[string]any{"type": "string"}}},
	}

	schema, err := gql.BuildSchema([]substrate.KindInfo{shipped, installed})
	if err != nil {
		t.Fatalf("buildSchema: %v", err)
	}
	tm := schema.TypeMap()
	bare, ok := tm["Task"].(*graphql.Object)
	if !ok {
		t.Fatalf("shipped task lost its bare name; have %v", sortedTypeNames(tm))
	}
	if bare.Description() != "substrate.reamde.dev/core/task" {
		t.Fatalf("bare Task resolves to %q, not the shipped kind", bare.Description())
	}
	prefixed, ok := tm["Alpha_Task"].(*graphql.Object)
	if !ok {
		t.Fatalf("installed task did not get its package-prefixed name; have %v", sortedTypeNames(tm))
	}
	if prefixed.Description() != "acme.example.com/alpha/task" {
		t.Fatalf("Alpha_Task resolves to %q", prefixed.Description())
	}
	// The order the two arrive in must not change either name.
	schema2, err := gql.BuildSchema([]substrate.KindInfo{installed, shipped})
	if err != nil {
		t.Fatalf("buildSchema (reordered): %v", err)
	}
	if _, ok := schema2.TypeMap()["Task"].(*graphql.Object); !ok {
		t.Fatal("shipped Task name depends on registry order")
	}
}

// A registry type whose computed name lands on a structural/interface name is
// refused at schema-build with a clear error, not silently renamed (A14).
func TestGraphQLReservedNameCollisionIsRefused(t *testing.T) {
	bad := substrate.KindInfo{
		Identity: "substrate.reamde.dev/core/change", Name: "change", Authority: coreAuthority,
		Version: 1, Plural: "changes", Source: "builtin",
		Definition: map[string]any{"properties": map[string]any{}},
	}
	_, err := gql.BuildSchema([]substrate.KindInfo{bad})
	if err == nil {
		t.Fatal("a type named Change must be refused — it collides with the structural Change type")
	}
	if !strings.Contains(err.Error(), "Change") {
		t.Fatalf("error should name the collision: %v", err)
	}
}

// TWO REFERENCE PROPERTIES CANNOT SHARE A GENERATED NAME.
// `<Kind><Property>Reference` is a pure function of (kind, property), so
// `task`.`noteX` and `taskNote`.`x` both spell `TaskNoteXReference`. The build
// refuses the pair: one generated object would otherwise carry one pair's link
// fields and serve both properties, silently, and nothing in the loader keeps
// kind names to the single lowercase word that made the collision unreachable.
func TestGraphQLReferenceNameCollisionIsRefused(t *testing.T) {
	reference := func(prop string) map[string]any {
		return map[string]any{"properties": map[string]any{
			prop: map[string]any{"type": "reference", "kind": "samples.substrate.reamde.dev/people/person"},
		}}
	}
	task := substrate.KindInfo{
		Identity: "samples.substrate.reamde.dev/tasks/task", Name: "task", Authority: "samples.substrate.reamde.dev/tasks",
		Version: 1, Plural: "tasks", Source: "builtin", Definition: reference("noteX"),
	}
	taskNote := substrate.KindInfo{
		Identity: "samples.substrate.reamde.dev/tasks/taskNote", Name: "taskNote", Authority: "samples.substrate.reamde.dev/tasks",
		Version: 1, Plural: "taskNotes", Source: "builtin", Definition: reference("x"),
	}

	_, err := gql.BuildSchema([]substrate.KindInfo{task, taskNote})
	if err == nil {
		t.Fatal("two reference properties minting one name must be refused")
	}
	for _, want := range []string{"TaskNoteXReference", "samples.substrate.reamde.dev/tasks/task.noteX"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the error must name %s: %v", want, err)
		}
	}

	// The same pair, one property renamed, builds: the refusal is the
	// collision and not the shape.
	taskNote.Definition = reference("y")
	if _, err := gql.BuildSchema([]substrate.KindInfo{task, taskNote}); err != nil {
		t.Fatalf("two distinct reference names must build: %v", err)
	}
}

// Record.version and Change.seq are 64-bit (Long scalar): a value past 2^31
// round-trips through GraphQL instead of overflowing GraphQL's 32-bit Int.
func TestGraphQLLongScalarRoundTripsPast2e31(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]

	const bigVersion = int64(3_000_000_000) // > 2^31-1 (2_147_483_647)
	const bigSeq = int64(5_000_000_000)
	ds.records["big1"] = &substrate.Record{
		ID: "big1", Kind: "samples.substrate.reamde.dev/people/person", Version: bigVersion,
		Properties: map[string]any{"name": "Ada"},
	}
	ds.changes = append(ds.changes, substrate.Change{
		Seq: bigSeq, TS: time.Unix(1, 0).UTC(), Actor: substrate.ActorAPI,
		Op: substrate.OpPut, RecordID: "big1", Kind: "samples.substrate.reamde.dev/people/person",
	})

	res := env.gql(t, tok, `{ record(kind: "samples.substrate.reamde.dev/people/person", id: "big1") { version history(first: 50) { seq } } }`, nil)
	ent, _ := res.Data["record"].(map[string]any)
	if got := int64(ent["version"].(float64)); got != bigVersion {
		t.Fatalf("version overflowed: got %d, want %d", got, bigVersion)
	}
	hist, _ := ent["history"].([]any)
	var sawSeq bool
	for _, h := range hist {
		row, _ := h.(map[string]any)
		if int64(row["seq"].(float64)) == bigSeq {
			sawSeq = true
		}
	}
	if !sawSeq {
		t.Fatalf("Change.seq %d did not round-trip through Long; history = %v", bigSeq, hist)
	}
}

// propertyType wraps repeated kinds in a LIST for every element type, and an
// object property is JSON, not String.
func TestGraphQLPropertyTypesListAndObject(t *testing.T) {
	widget := substrate.KindInfo{
		Identity: "tools.substrate.reamde.dev/tools/widget", Name: "widget", Authority: "tools.substrate.reamde.dev",
		Version: 1, Plural: "widgets", Source: "builtin",
		Definition: map[string]any{"properties": map[string]any{
			"scores":  map[string]any{"type": "int", "repeated": true},
			"tags":    map[string]any{"type": "string", "repeated": true},
			"address": map[string]any{"type": "object", "fields": map[string]any{"city": map[string]any{"type": "string"}}},
			"count":   map[string]any{"type": "int"},
		}},
	}
	schema, err := gql.BuildSchema([]substrate.KindInfo{widget})
	if err != nil {
		t.Fatalf("buildSchema: %v", err)
	}
	obj, ok := schema.TypeMap()["Widget"].(*graphql.Object)
	if !ok {
		t.Fatalf("Widget missing; have %v", sortedTypeNames(schema.TypeMap()))
	}
	fields := obj.Fields()

	scores, ok := fields["scores"].Type.(*graphql.List)
	if !ok || scores.OfType != graphql.Int {
		t.Fatalf("repeated int `scores` = %v, want [Int]", fields["scores"].Type)
	}
	tags, ok := fields["tags"].Type.(*graphql.List)
	if !ok || tags.OfType != graphql.String {
		t.Fatalf("repeated string `tags` = %v, want [String]", fields["tags"].Type)
	}
	if fields["address"].Type.Name() != "JSON" {
		t.Fatalf("object `address` = %v, want the JSON scalar (not String)", fields["address"].Type)
	}
	if fields["count"].Type != graphql.Int {
		t.Fatalf("scalar int `count` = %v, want Int", fields["count"].Type)
	}
}

func sortedTypeNames(tm map[string]graphql.Type) []string {
	out := make([]string, 0, len(tm))
	for n := range tm {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func TestGraphQLSearch(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]
	ds.records["c1"] = &substrate.Record{ID: "c1", Kind: "samples.substrate.reamde.dev/people/person", Title: "Ada Lovelace"}

	res := env.gql(t, tok, `{ search(q: "ada", mode: "hybrid", kinds: ["samples.substrate.reamde.dev/people/person"], k: 5) { lexical record { id } } }`, nil)
	hits, _ := res.Data["search"].([]any)
	if len(hits) != 1 {
		t.Fatalf("hits = %v", hits)
	}
	if ds.lastSearch.Mode != substrate.SearchHybrid || ds.lastSearch.K != 5 || len(ds.lastSearch.Kinds) != 1 {
		t.Fatalf("search input = %+v", ds.lastSearch)
	}
}
