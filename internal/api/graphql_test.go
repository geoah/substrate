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
		Message string `json:"message"`
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
			Identity: "example.substrate.reamde.dev/widget", Version: "v1alpha1", Plural: "widgets",
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
		ID: "shared", Kind: "people.substrate.reamde.dev/person",
		Properties: map[string]any{"title": "Ada"}, Version: 1,
	}
	ds.commit(substrate.Change{
		TS: time.Unix(1, 0).UTC(), Actor: substrate.ActorAPI, Op: substrate.OpPut,
		RecordID: "shared", Kind: "people.substrate.reamde.dev/person",
	})
	ds.commit(substrate.Change{
		TS: time.Unix(2, 0).UTC(), Actor: substrate.ActorAPI, Op: substrate.OpPut,
		RecordID: "shared", Kind: "tasks.substrate.reamde.dev/task",
	})

	res := env.gql(t, tok,
		`query ($kind: String!, $id: ID!) {
			record(kind: $kind, id: $id) { id kind history { seq kind recordId } }
		}`,
		map[string]any{"kind": "people.substrate.reamde.dev/person", "id": "shared"})

	record, _ := res.Data["record"].(map[string]any)
	history, _ := record["history"].([]any)
	if len(history) != 1 {
		t.Fatalf("history has %d rows, want 1 — the task-typed row sharing id %q leaked into the person's history", len(history), "shared")
	}
	row, _ := history[0].(map[string]any)
	if row["kind"] != "people.substrate.reamde.dev/person" {
		t.Fatalf("history row kind = %v, want the person kind only", row["kind"])
	}
}

func TestGraphQLPutPatchRecordRoundTrip(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")

	put := env.gql(t, tok, `mutation ($in: JSON!) { put(input: $in) { id kind title ... on Person { name company } } }`,
		map[string]any{"in": map[string]any{
			"kind": "people.substrate.reamde.dev/person",
			// Everything authored is a property: `title` sits in the map with
			// the declared ones.
			"properties": map[string]any{"title": "Ada", "name": "Ada"},
		}})
	created, _ := put.Data["put"].(map[string]any)
	if created["title"] != "Ada" || created["name"] != "Ada" {
		t.Fatalf("put returned %v", created)
	}
	id, _ := created["id"].(string)

	patched := env.gql(t, tok, `mutation ($id: ID!, $in: JSON!) { patch(kind: "people.substrate.reamde.dev/person", id: $id, input: $in) { id version ... on Person { company } } }`,
		map[string]any{"id": id, "in": map[string]any{"properties": map[string]any{"company": "Analytical"}}})
	got, _ := patched.Data["patch"].(map[string]any)
	if got["company"] != "Analytical" {
		t.Fatalf("patch returned %v", got)
	}

	one := env.gql(t, tok, `query ($id: ID!) { record(kind: "people.substrate.reamde.dev/person", id: $id) { id kind title labels } }`, map[string]any{"id": id})
	ent, _ := one.Data["record"].(map[string]any)
	if ent["id"] != id || ent["kind"] != "people.substrate.reamde.dev/person" {
		t.Fatalf("record returned %v", ent)
	}
}

// A reference property renders through the GraphQL Reference
// object type: the stored {kind, id} record reference survives put and reads back
// through its subfields — the GraphQL half of the wire round-trip.
func TestGraphQLReferenceRoundTrip(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")

	put := env.gql(t, tok,
		`mutation ($in: JSON!) { put(input: $in) { id ... on Person { manager { kind id } } } }`,
		map[string]any{"in": map[string]any{
			"kind": "people.substrate.reamde.dev/person",
			"properties": map[string]any{
				"title":   "Ada",
				"manager": map[string]any{"kind": "people.substrate.reamde.dev/person", "id": "boss1"},
			},
		}})
	created, _ := put.Data["put"].(map[string]any)
	mgr, ok := created["manager"].(map[string]any)
	if !ok {
		t.Fatalf("manager did not render as a Reference: %v", created)
	}
	if mgr["kind"] != "people.substrate.reamde.dev/person" || mgr["id"] != "boss1" {
		t.Fatalf("manager reference = %v", mgr)
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
		"in": map[string]any{"kind": "people.substrate.reamde.dev/person", "properties": map[string]any{"title": "Ada", "name": "Ada"}},
	})
	env.gql(t, tok, `mutation ($in: JSON!) { put(input: $in) { id } }`, map[string]any{
		"in": map[string]any{"kind": "tasks.substrate.reamde.dev/task", "properties": map[string]any{"title": "ship it", "status": "open"}},
	})

	res := env.gql(t, tok, `query ($f: JSON, $o: JSON) {
		records(filter: $f, orderBy: $o, first: 10) {
			cursor
			nodes { id kind title ... on Task { status } }
		}
	}`, map[string]any{
		"f": map[string]any{"kinds": []string{"tasks.substrate.reamde.dev/task"}},
		"o": []map[string]any{{"property": "created_at", "desc": true}},
	})
	conn, _ := res.Data["records"].(map[string]any)
	nodes, _ := conn["nodes"].([]any)
	if len(nodes) != 1 {
		t.Fatalf("nodes = %v", nodes)
	}
	node, _ := nodes[0].(map[string]any)
	if node["kind"] != "tasks.substrate.reamde.dev/task" || node["status"] != "open" {
		t.Fatalf("node = %v", node)
	}
	if len(ds.lastQuery.OrderBy) != 1 || !ds.lastQuery.OrderBy[0].Desc {
		t.Fatalf("orderBy did not reach the dataset: %+v", ds.lastQuery.OrderBy)
	}
}

func TestGraphQLNeedsAuth(t *testing.T) {
	env := newTestEnv(t)
	rec := env.do(t, http.MethodPost, graphqlPath, "", map[string]any{"query": "{ __typename }"})
	wantErrorCode(t, rec, http.StatusUnauthorized, codeAuth)
}

func TestGraphQLSchemaIsCachedPerRegistryFingerprint(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]

	env.gql(t, tok, `{ __typename }`, nil)
	env.gql(t, tok, `{ __typename }`, nil)

	// Installing a type changes the fingerprint and must rebuild.
	ds.types = append(ds.types, substrate.KindInfo{
		Identity: "beeper.connectors.substrate.reamde.dev/thread", Name: "thread",
		Authority: "beeper.connectors.substrate.reamde.dev", Version: "v1alpha1", Plural: "threads",
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

func TestGraphQLEdgesHistoryAndCapabilityInterfaces(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]

	at := time.Unix(1_700_000_500, 0).UTC()
	ds.records["team1"] = &substrate.Record{
		ID: "team1", Kind: "people.substrate.reamde.dev/person", Title: "Analytical",
		Properties: map[string]any{"name": "Analytical"},
	}
	ds.records["msg1"] = &substrate.Record{
		ID: "msg1", Kind: "messaging.substrate.reamde.dev/conversationmessage", Title: "hi", At: &at,
		Properties: map[string]any{"text": "hi"},
		Edges: map[string][]substrate.EdgeTarget{
			"author": {{ID: "team1", Kind: "people.substrate.reamde.dev/person", Title: "Analytical", Properties: map[string]any{"since": 2020}}},
		},
	}
	ds.changes = append(ds.changes, substrate.Change{
		Seq: 1, TS: at, Actor: substrate.ActorAPI, Op: substrate.OpPut,
		RecordID: "msg1", Kind: "messaging.substrate.reamde.dev/conversationmessage",
	})

	res := env.gql(t, tok, `{
		record(kind: "messaging.substrate.reamde.dev/conversationmessage", id: "msg1") {
			id
			... on Temporal { at endsAt }
			edges(rel: "author") { rel properties target { id ... on Person { name } } }
			history(first: 5) { seq op }
		}
	}`, nil)
	ent, _ := res.Data["record"].(map[string]any)
	if ent["at"] == nil {
		t.Fatalf("Temporal.at not resolved: %v", ent)
	}
	edges, _ := ent["edges"].([]any)
	if len(edges) != 1 {
		t.Fatalf("edges = %v", edges)
	}
	edge, _ := edges[0].(map[string]any)
	target, _ := edge["target"].(map[string]any)
	if edge["rel"] != "author" || target["name"] != "Analytical" {
		t.Fatalf("edge = %v", edge)
	}
	history, _ := ent["history"].([]any)
	if len(history) != 1 {
		t.Fatalf("history = %v", history)
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
		ID: "p1", Kind: "people.substrate.reamde.dev/person",
		Properties: map[string]any{"name": "Sam"},
	}
	ds.meta["p1"] = map[string]substrate.PropertyMeta{
		"name": {Manager: "owner", UpdatedAt: at, Alternatives: []substrate.PropertyAlternative{
			{Actor: "google.connectors.substrate.reamde.dev/people", Value: "Samuel Jones", UpdatedAt: at},
		}},
	}

	res := env.gql(t, tok, `{ record(kind: "people.substrate.reamde.dev/person", id: "p1") { id propertyMeta } }`, nil)
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

// Reverse edges do not inflate GraphQL record reads. The dedicated REST
// resource owns their pagination.
func TestGraphQLIncomingIsNotOnRecord(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]

	ds.records["p1"] = &substrate.Record{
		ID: "p1", Kind: "people.substrate.reamde.dev/person",
		Properties: map[string]any{"name": "Sam"},
	}
	ds.incoming["p1"] = []substrate.IncomingEdge{
		{Rel: "person", From: substrate.EdgeTarget{
			ID: "people-c1001", Kind: "google.connectors.substrate.reamde.dev/contact", Title: "Samuel Jones",
		}},
	}

	// The GraphQL Record type has NO `incoming` field (record 57; removed at
	// the v1 freeze, ticket 004): selecting it is a validation error, not a
	// null. Query it raw (the gql helper rejects any error) and assert the
	// field cannot be selected.
	rec := env.do(t, http.MethodPost, graphqlPath, tok, map[string]any{
		"query": `{ record(kind: "people.substrate.reamde.dev/person", id: "p1") { id incoming } }`,
	})
	res := decodeJSON[gqlResponse](t, rec)
	if len(res.Errors) == 0 {
		t.Fatalf("expected a validation error for the removed `incoming` field, got %+v", res.Data)
	}

	// The record still reads fine without it.
	ok := env.gql(t, tok, `{ record(kind: "people.substrate.reamde.dev/person", id: "p1") { id } }`, nil)
	ent, _ := ok.Data["record"].(map[string]any)
	if ent["id"] != "p1" {
		t.Fatalf("record = %#v", ent)
	}
}

// Installing a bundle whose singular collides with a shipped type must NOT
// rename the shipped type's GraphQL name. The shipped task keeps
// the bare "Task"; the bundle's task is authority-prefixed "Alpha_Task". Both are
// resolvable and distinct — the pre-A14 first-claim scheme renamed the shipped
// type to "TasksTask" when the bundle sorted ahead of it.
func TestGraphQLNamesAreAFunctionOfIdentityNotTheRegistry(t *testing.T) {
	shipped := substrate.KindInfo{
		Identity: "tasks.substrate.reamde.dev/task", Name: "task", Authority: "tasks.substrate.reamde.dev",
		Version: "v1alpha1", Plural: "tasks", Source: "builtin",
		Definition: map[string]any{"properties": map[string]any{"note": map[string]any{"type": "string"}}},
	}
	installed := substrate.KindInfo{
		Identity: "alpha.bundles.substrate.reamde.dev/task", Name: "task", Authority: "alpha.bundles.substrate.reamde.dev",
		Version: "v1alpha1", Plural: "tasks", Source: "installed",
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
	if bare.Description() != "tasks.substrate.reamde.dev/task" {
		t.Fatalf("bare Task resolves to %q, not the shipped type", bare.Description())
	}
	prefixed, ok := tm["Alpha_Task"].(*graphql.Object)
	if !ok {
		t.Fatalf("installed task did not get its authority-prefixed name; have %v", sortedTypeNames(tm))
	}
	if prefixed.Description() != "alpha.bundles.substrate.reamde.dev/task" {
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
		Identity: "core.substrate.reamde.dev/change", Name: "change", Authority: coreAuthority,
		Version: "v1alpha1", Plural: "changes", Source: "builtin",
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

// Record.version and Change.seq are 64-bit (Long scalar): a value past 2^31
// round-trips through GraphQL instead of overflowing GraphQL's 32-bit Int.
func TestGraphQLLongScalarRoundTripsPast2e31(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]

	const bigVersion = int64(3_000_000_000) // > 2^31-1 (2_147_483_647)
	const bigSeq = int64(5_000_000_000)
	ds.records["big1"] = &substrate.Record{
		ID: "big1", Kind: "people.substrate.reamde.dev/person", Version: bigVersion,
		Properties: map[string]any{"name": "Ada"},
	}
	ds.changes = append(ds.changes, substrate.Change{
		Seq: bigSeq, TS: time.Unix(1, 0).UTC(), Actor: substrate.ActorAPI,
		Op: substrate.OpPut, RecordID: "big1", Kind: "people.substrate.reamde.dev/person",
	})

	res := env.gql(t, tok, `{ record(kind: "people.substrate.reamde.dev/person", id: "big1") { version history(first: 50) { seq } } }`, nil)
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
		Identity: "tools.substrate.reamde.dev/widget", Name: "widget", Authority: "tools.substrate.reamde.dev",
		Version: "v1alpha1", Plural: "widgets", Source: "builtin",
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
	ds.records["c1"] = &substrate.Record{ID: "c1", Kind: "people.substrate.reamde.dev/person", Title: "Ada Lovelace"}

	res := env.gql(t, tok, `{ search(q: "ada", mode: "hybrid", kinds: ["people.substrate.reamde.dev/person"], k: 5) { lexical record { id } } }`, nil)
	hits, _ := res.Data["search"].([]any)
	if len(hits) != 1 {
		t.Fatalf("hits = %v", hits)
	}
	if ds.lastSearch.Mode != substrate.SearchHybrid || ds.lastSearch.K != 5 || len(ds.lastSearch.Kinds) != 1 {
		t.Fatalf("search input = %+v", ds.lastSearch)
	}
}
