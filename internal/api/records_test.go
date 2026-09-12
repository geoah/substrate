package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// recordsPath is the one list route: every read of "some records" is a GET
// here, told apart by its parameters, and the one body-addressed create is a
// POST here.
const recordsPath = "/api/v1/records"

// peopleRecords is the person kind's list: the one-kind filter, spelled once
// for the tests that need it as a constant prefix.
var peopleRecords = recordsPath + "?filter=" + url.QueryEscape(`{"kinds":["`+personKind+`"]}`)

// filterPath spells a filter as the list route's `filter` parameter, with
// any further parameters appended as given ("first=5", "watch=1").
func filterPath(t *testing.T, f substrate.Filter, extra ...string) string {
	t.Helper()
	raw, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	path := recordsPath + "?filter=" + url.QueryEscape(string(raw))
	for _, p := range extra {
		path += "&" + p
	}
	return path
}

// recordsOf is the list of one kind: the spelling the collection path used
// to carry, as `filter.kinds`.
func recordsOf(t *testing.T, kind string, extra ...string) string {
	t.Helper()
	return filterPath(t, substrate.Filter{Kinds: []string{kind}}, extra...)
}

// createRecord POSTs one record of `kind` under a server-assigned id and
// returns what the route answered.
func createRecord(t *testing.T, env *testEnv, tok, kind string, props map[string]any) *substrate.Record {
	t.Helper()
	rec := env.do(t, http.MethodPost, recordsPath, tok, map[string]any{"kind": kind, "properties": props})
	wantStatus(t, rec, http.StatusCreated)
	e := decodeJSON[substrate.Record](t, rec)
	return &e
}

// wantMessage asserts the problem object's message names each fragment.
func wantMessage(t *testing.T, rec *httptest.ResponseRecorder, fragments ...string) {
	t.Helper()
	msg := decodeJSON[substrate.ErrorEnvelope](t, rec).Error.Message
	for _, want := range fragments {
		if !strings.Contains(msg, want) {
			t.Fatalf("message %q does not name %q", msg, want)
		}
	}
}

// --- the list ------------------------------------------------------------

// The list carries the whole grammar to the dataset unchanged: every filter
// arm, the sort, the page and the expansion.
func TestRecordsListCarriesTheGrammar(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]

	f := substrate.Filter{
		Kinds:      []string{personKind},
		Properties: map[string]substrate.Cond{"company": {Eq: "Analytical"}},
	}
	path := filterPath(t, f, "orderBy="+url.QueryEscape("at:desc,createdAt"), "first=7", "after=cur1",
		"expand="+url.QueryEscape("manager, author"), "withAnnotations=1")
	rec := env.do(t, http.MethodGet, path, tok, nil)
	wantStatus(t, rec, http.StatusOK)

	q := ds.lastQuery
	if len(q.Filter.Kinds) != 1 || q.Filter.Kinds[0] != personKind {
		t.Fatalf("filter.kinds = %v", q.Filter.Kinds)
	}
	if q.Filter.Properties["company"].Eq != "Analytical" {
		t.Fatalf("filter.properties lost: %+v", q.Filter.Properties)
	}
	want := []substrate.Order{{Property: "at", Desc: true}, {Property: "createdAt"}}
	if len(q.OrderBy) != 2 || q.OrderBy[0] != want[0] || q.OrderBy[1] != want[1] {
		t.Fatalf("orderBy = %+v, want %+v", q.OrderBy, want)
	}
	if q.First != 7 || q.After != "cur1" || !q.WithAnnotations {
		t.Fatalf("paging = first %d after %q withAnnotations %v", q.First, q.After, q.WithAnnotations)
	}
	if len(q.Expand) != 2 || q.Expand[0] != "manager" || q.Expand[1] != "author" {
		t.Fatalf("expand = %v, want the comma-separated names, trimmed", q.Expand)
	}
	page := decodeJSON[substrate.Page](t, rec)
	if page.Generation != ds.generation {
		t.Fatalf("page generation = %q, want %q", page.Generation, ds.generation)
	}
}

// With no filter at all the list is every record: there is no collection to
// scope it, so the route does not invent one.
func TestRecordsListWithoutAFilterIsEveryKind(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	ds.put(&substrate.Record{ID: "p1", Kind: personKind, Properties: map[string]any{}})
	ds.put(&substrate.Record{ID: "t1", Kind: "samples.substrate.reamde.dev/tasks/task", Properties: map[string]any{}})

	rec := env.do(t, http.MethodGet, recordsPath, tok, nil)
	wantStatus(t, rec, http.StatusOK)
	page := decodeJSON[substrate.Page](t, rec)
	if len(page.Records) != 2 {
		t.Fatalf("records = %d, want both kinds", len(page.Records))
	}
	if len(ds.lastQuery.Filter.Kinds) != 0 {
		t.Fatalf("the route narrowed an unfiltered list to %v", ds.lastQuery.Filter.Kinds)
	}
}

// filter.kinds resolves each reference against the repository's kinds, so an
// unknown one is the record path's 404, named, in every mode.
func TestRecordsUnknownKindInFilterIs404(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	const unknown = "samples.substrate.reamde.dev/people/widget"
	for _, extra := range [][]string{nil, {"q=ada"}, {"watch=1"}} {
		rec := env.do(t, http.MethodGet, recordsOf(t, unknown, extra...), tok, nil)
		wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
		if msg := decodeJSON[substrate.ErrorEnvelope](t, rec).Error.Message; !strings.Contains(msg, "unknown kind "+unknown) {
			t.Fatalf("%v: message = %q, want the kind named", extra, msg)
		}
	}
}

// The reverse read is a filter arm: `filter.referencing` narrows to the
// records pointing at one target, and the page's `matches` says from which
// property each one points. `property` narrows to one site.
func TestRecordsReferencingFillsMatches(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	const target = personKind + "/p1"
	ds.put(&substrate.Record{ID: "p1", Kind: personKind, Properties: map[string]any{"name": "Sam"}})
	ds.put(&substrate.Record{ID: "p2", Kind: personKind, Properties: map[string]any{
		"name": "Ada", "manager": map[string]any{"ref": target},
	}})
	ds.put(&substrate.Record{ID: "m1", Kind: "samples.substrate.reamde.dev/messaging/conversationmessage", Properties: map[string]any{
		"text": "hi", "author": map[string]any{"ref": target, "since": 3},
	}})
	ds.put(&substrate.Record{ID: "p3", Kind: personKind, Properties: map[string]any{"name": "Nobody"}})

	rec := env.do(t, http.MethodGet, filterPath(t, substrate.Filter{Referencing: &substrate.Referencing{Ref: target}}), tok, nil)
	wantStatus(t, rec, http.StatusOK)
	page := decodeJSON[substrate.Page](t, rec)
	if len(page.Records) != 2 {
		t.Fatalf("referencing page = %+v, want the two pointers", page.Records)
	}
	if got := page.Matches[personKind+"/p2"]; len(got) != 1 || got[0].Property != "manager" {
		t.Fatalf("matches for p2 = %+v", got)
	}
	if got := page.Matches["samples.substrate.reamde.dev/messaging/conversationmessage/m1"]; len(got) != 1 || got[0].Property != "author" {
		t.Fatalf("matches for m1 = %+v", got)
	}
	if q := ds.lastQuery.Filter.Referencing; q == nil || q.Ref != target {
		t.Fatalf("the dataset saw referencing = %+v", q)
	}

	rec = env.do(t, http.MethodGet, filterPath(t, substrate.Filter{
		Kinds:       []string{personKind},
		Referencing: &substrate.Referencing{Ref: target, Property: "manager"},
	}), tok, nil)
	wantStatus(t, rec, http.StatusOK)
	page = decodeJSON[substrate.Page](t, rec)
	if len(page.Records) != 1 || page.Records[0].ID != "p2" {
		t.Fatalf("property-narrowed page = %+v", page.Records)
	}
}

// `expand` hydrates the named reference properties one hop: the referents
// land in `included`, keyed by record path, each once; a dangling pointer has
// no entry.
func TestRecordsExpandFillsIncluded(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	ds.put(&substrate.Record{ID: "boss", Kind: personKind, Properties: map[string]any{"name": "Sam"}})
	ds.put(&substrate.Record{ID: "p2", Kind: personKind, Properties: map[string]any{
		"name": "Ada", "manager": map[string]any{"ref": personKind + "/boss"},
	}})
	ds.put(&substrate.Record{ID: "p3", Kind: personKind, Properties: map[string]any{
		"name": "Grace", "manager": map[string]any{"ref": personKind + "/boss"},
	}})
	ds.put(&substrate.Record{ID: "p4", Kind: personKind, Properties: map[string]any{
		"name": "Orphan", "manager": map[string]any{"ref": personKind + "/gone"},
	}})

	rec := env.do(t, http.MethodGet, recordsOf(t, personKind, "expand=manager"), tok, nil)
	wantStatus(t, rec, http.StatusOK)
	page := decodeJSON[substrate.Page](t, rec)
	if len(page.Included) != 1 {
		t.Fatalf("included = %+v, want the one referent once", page.Included)
	}
	if boss := page.Included[personKind+"/boss"]; boss == nil || boss.Properties["name"] != "Sam" {
		t.Fatalf("included[boss] = %+v", boss)
	}

	// Without `expand`, the key is absent, not an empty object.
	rec = env.do(t, http.MethodGet, recordsOf(t, personKind), tok, nil)
	wantStatus(t, rec, http.StatusOK)
	if strings.Contains(rec.Body.String(), `"included"`) {
		t.Fatalf("an unexpanded page carries included: %s", rec.Body.String())
	}
}

// A list cursor from another history is the changefeed's signal, 410
// `compacted` with the head to start over from, never a 422 the client reads
// as its own mistake.
func TestRecordsStaleCursorIs410Compacted(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	seedChanges(ds, 3)
	ds.errs["List"] = fmt.Errorf("after: %w", substrate.ErrStaleHistory)

	rec := env.do(t, http.MethodGet, recordsPath+"?after=old-cursor", tok, nil)
	wantErrorCode(t, rec, http.StatusGone, codeCompacted)
	problem := decodeJSON[substrate.ErrorEnvelope](t, rec).Error
	if problem.Head == nil || *problem.Head != 3 || problem.Generation != ds.generation {
		t.Fatalf("compacted problem = %+v, want head 3 and the live generation", problem)
	}
}

// A parameter the list does not honor is a bad_request naming it, and a
// misspelled filter key is refused rather than silently broadening the list.
func TestRecordsListRefusesUnknownParameters(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	for _, query := range []string{"?bogus=1", "?first=2&bogus=1", "?limit=5", "?from=3", "?generation=g"} {
		rec := env.do(t, http.MethodGet, recordsPath+query, tok, nil)
		wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
		msg := decodeJSON[substrate.ErrorEnvelope](t, rec).Error.Message
		key := strings.TrimPrefix(strings.SplitN(query, "=", 2)[0], "?")
		if strings.Contains(query, "&") {
			key = "bogus"
		}
		if !strings.Contains(msg, `"`+key+`"`) {
			t.Errorf("%s: message = %q, want %q named", query, msg, key)
		}
	}
	rec := env.do(t, http.MethodGet, recordsPath+"?filter=%7Bnot-json", tok, nil)
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
	rec = env.do(t, http.MethodGet, recordsPath+`?filter={"kind":"x"}`, tok, nil)
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
	if msg := decodeJSON[substrate.ErrorEnvelope](t, rec).Error.Message; !strings.Contains(msg, `"kind"`) {
		t.Fatalf("a misspelled filter key must be named: %q", msg)
	}
}

// --- the ranked read -----------------------------------------------------

// `q` switches the route to the ranked read: the query, its mode, the kind
// narrowing and the hit count reach Search, and the answer is the ranked
// page with the per-arm scores keyed by record path.
func TestRecordsRankedReadShapesTheSearch(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	ds.put(&substrate.Record{ID: "p1", Kind: personKind, Title: "Ada Lovelace", Properties: map[string]any{}})
	ds.put(&substrate.Record{ID: "p2", Kind: personKind, Title: "Grace Hopper", Properties: map[string]any{}})

	rec := env.do(t, http.MethodGet, recordsOf(t, personKind, "q=ada", "mode=Lexical", "first=5"), tok, nil)
	wantStatus(t, rec, http.StatusOK)
	in := ds.lastSearch
	if in.Q != "ada" || in.Mode != substrate.SearchLexical || in.K != 5 ||
		len(in.Kinds) != 1 || in.Kinds[0] != personKind {
		t.Fatalf("search input = %+v", in)
	}
	page := decodeJSON[substrate.RankedPage](t, rec)
	if len(page.Records) != 1 || page.Records[0].ID != "p1" {
		t.Fatalf("ranked records = %+v", page.Records)
	}
	if s := page.Scores[personKind+"/p1"]; s.Lexical != 0.5 || s.Semantic != 0 {
		t.Fatalf("scores = %+v", page.Scores)
	}
	// No keyset, no snapshot: the ranked page claims neither.
	for _, absent := range []string{`"cursor"`, `"head"`, `"generation"`} {
		if strings.Contains(rec.Body.String(), absent) {
			t.Fatalf("ranked page carries %s: %s", absent, rec.Body.String())
		}
	}
	if !strings.Contains(rec.Body.String(), `"pending":0`) {
		t.Fatalf("ranked page must always say how much of the index is pending: %s", rec.Body.String())
	}

	// An empty ranking is `[]`, never null.
	rec = env.do(t, http.MethodGet, recordsPath+"?q=nobody", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	if !strings.Contains(rec.Body.String(), `"records":[]`) {
		t.Fatalf("empty ranking = %s", rec.Body.String())
	}
}

// The ranked read narrows by filter.kinds alone: both arms cap candidates
// before hydration, so a predicate applied afterwards would not produce the
// filtered top-k. Every other arm, and every list parameter, is refused by
// name.
func TestRecordsRankedReadRefusesWhatItCannotHonor(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	for _, tc := range []struct{ path, want string }{
		{recordsPath + "?q=x&orderBy=at", `"orderBy"`},
		{recordsPath + "?q=x&after=c", `"after"`},
		{recordsPath + "?q=x&expand=manager", `"expand"`},
		{recordsPath + "?q=x&withAnnotations=1", `"withAnnotations"`},
		{filterPath(t, substrate.Filter{Implements: "x"}, "q=x"), "filter.implements"},
		{filterPath(t, substrate.Filter{IDs: []string{"a"}}, "q=x"), "filter.ids"},
		{filterPath(t, substrate.Filter{Properties: map[string]substrate.Cond{"a": {Eq: 1}}}, "q=x"), "filter.properties"},
		{filterPath(t, substrate.Filter{Labels: map[string]substrate.Cond{"a": {Eq: 1}}}, "q=x"), "filter.labels"},
		{filterPath(t, substrate.Filter{Deleted: new(bool)}, "q=x"), "filter.deleted"},
		{filterPath(t, substrate.Filter{Referencing: &substrate.Referencing{Ref: "k/a/b/c"}}, "q=x"), "filter.referencing"},
	} {
		rec := env.do(t, http.MethodGet, tc.path, tok, nil)
		wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
		if msg := decodeJSON[substrate.ErrorEnvelope](t, rec).Error.Message; !strings.Contains(msg, tc.want) {
			t.Errorf("%s: message = %q, want %s named", tc.path, msg, tc.want)
		}
	}
}

// --- the tail ------------------------------------------------------------

// `watch=1` is the tail, narrowed by filter.kinds alone: the change filter
// has no property arms, so any other arm is refused by name, as is every
// list parameter.
func TestRecordsWatchRefusesWhatItCannotHonor(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	for _, tc := range []struct{ path, want string }{
		{recordsPath + "?watch=1&orderBy=at", `"orderBy"`},
		{recordsPath + "?watch=1&first=5", `"first"`},
		{recordsPath + "?watch=1&after=c", `"after"`},
		{recordsPath + "?watch=1&expand=manager", `"expand"`},
		{recordsPath + "?watch=1&bogus=1", `"bogus"`},
		// `watch=1` wins the mode, so `q` is the parameter out of place.
		{recordsPath + "?watch=1&q=x", `"q"`},
		{filterPath(t, substrate.Filter{Properties: map[string]substrate.Cond{"a": {Eq: 1}}}, "watch=1"), "filter.properties"},
		{filterPath(t, substrate.Filter{Implements: "x"}, "watch=1"), "filter.implements"},
	} {
		rec := env.do(t, http.MethodGet, tc.path, tok, nil)
		wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
		if msg := decodeJSON[substrate.ErrorEnvelope](t, rec).Error.Message; !strings.Contains(msg, tc.want) {
			t.Errorf("%s: message = %q, want %s named", tc.path, msg, tc.want)
		}
	}
	// The tail's own parameters are honored.
	wantNotRefused(t, env, recordsOf(t, personKind, "watch=1", "from=0"), tok)
	generation := env.svc.datasets[fakeRepository].generation
	wantNotRefused(t, env, recordsPath+"?watch=1&from=1&generation="+generation, tok)
}

// --- the create ----------------------------------------------------------

// POST creates under a server-assigned id: the body names its kind, an
// unknown one is 404, and the status says what the write did.
func TestRecordsPostCreatesUnderTheBodysKind(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]

	created := createRecord(t, env, tok, personKind, map[string]any{"name": "Ada"})
	if created.Kind != personKind || created.ID == "" || created.Version != 1 {
		t.Fatalf("created = %+v", created)
	}
	if ds.lastPut.Kind != personKind || ds.lastPut.ID != "" {
		t.Fatalf("put input = %+v, want the body's kind and no id", ds.lastPut)
	}

	rec := env.do(t, http.MethodPost, recordsPath, tok, map[string]any{
		"kind": "samples.substrate.reamde.dev/people/widget", "properties": map[string]any{},
	})
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
	wantMessage(t, rec, "unknown kind samples.substrate.reamde.dev/people/widget")

	// A key beside the envelope's is refused by the strict decode, named.
	rec = env.do(t, http.MethodPost, recordsPath, tok, map[string]any{"kind": personKind, "propertys": map[string]any{}})
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
	wantMessage(t, rec, `"propertys"`)
}

// `kind` is required, and `id` is refused: a chosen id is a PUT at the record
// path, so the refusal names that PUT and nothing is written.
func TestRecordsPostRequiresKindAndRefusesID(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]

	rec := env.do(t, http.MethodPost, recordsPath, tok, map[string]any{"properties": map[string]any{"name": "Ada"}})
	wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)
	wantMessage(t, rec, "kind is required")

	rec = env.do(t, http.MethodPost, recordsPath, tok, map[string]any{
		"kind": personKind, "id": "chosen", "properties": map[string]any{"name": "Ada"},
	})
	wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)
	wantMessage(t, rec, "PUT "+peoplePath+"/chosen")
	if ds.lastPut.Kind != "" || len(ds.records) != 0 {
		t.Fatalf("a refused POST wrote %+v", ds.lastPut)
	}
}

// A repeat of a POST under one Idempotency-Key answers the first record with
// its 201; the same key with another body is the 409 naming the key.
func TestRecordsPostHonorsIdempotencyKey(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	body := map[string]any{"kind": personKind, "properties": map[string]any{"name": "Ada"}}

	first := env.do(t, http.MethodPost, recordsPath, tok, body, idempotencyHeader, "reused")
	wantStatus(t, first, http.StatusCreated)
	if ds.lastIdempotencyKey != "reused" {
		t.Fatalf("the dataset saw Idempotency-Key %q", ds.lastIdempotencyKey)
	}
	repeat := env.do(t, http.MethodPost, recordsPath, tok, body, idempotencyHeader, "reused")
	wantStatus(t, repeat, http.StatusCreated)
	if a, b := decodeJSON[substrate.Record](t, first), decodeJSON[substrate.Record](t, repeat); a.ID != b.ID {
		t.Fatalf("the repeat created %s, want the first attempt's %s", b.ID, a.ID)
	}
	if len(ds.records) != 1 {
		t.Fatalf("records = %d, want the one the first attempt created", len(ds.records))
	}

	rec := env.do(t, http.MethodPost, recordsPath, tok,
		map[string]any{"kind": personKind, "properties": map[string]any{"name": "Grace"}}, idempotencyHeader, "reused")
	wantErrorCode(t, rec, http.StatusConflict, codeConflict)
	wantMessage(t, rec, `Idempotency-Key "reused"`)

	// Without the header the write carries no key.
	rec = env.do(t, http.MethodPost, recordsPath, tok, body)
	wantStatus(t, rec, http.StatusCreated)
	if ds.lastIdempotencyKey != "" {
		t.Fatalf("a request without the header carried key %q", ds.lastIdempotencyKey)
	}
}

// PUT at the record path is the other create, and a second PUT there is the
// update: the two doors never disagree about what a repeat does.
func TestRecordsPostAndPutReportCreateAndUpdate(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)

	created := createRecord(t, env, tok, personKind, map[string]any{"name": "Ada"})
	rec := env.do(t, http.MethodPut, peoplePath+"/"+created.ID, tok, map[string]any{"properties": map[string]any{"name": "Ada L"}})
	wantStatus(t, rec, http.StatusOK)
	if e := decodeJSON[substrate.Record](t, rec); e.Version != 2 {
		t.Fatalf("the re-put landed at version %d", e.Version)
	}
	rec = env.do(t, http.MethodPut, peoplePath+"/fresh", tok, map[string]any{"properties": map[string]any{"name": "Grace"}})
	wantStatus(t, rec, http.StatusCreated)
}

// There is no collection path: a three-segment path under the version prefix
// names nothing, in every method, and answers the router's JSON 404.
func TestRecordsHasNoCollectionPath(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		rec := env.do(t, method, peoplePath, tok, map[string]any{"properties": map[string]any{"name": "x"}})
		wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
		wantMessage(t, rec, "no such API path")
	}
	if ds.lastPut.Kind != "" || len(ds.records) != 0 {
		t.Fatalf("a collection path wrote %+v", ds.lastPut)
	}
}
