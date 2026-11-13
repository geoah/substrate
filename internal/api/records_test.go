package api

import (
	"context"
	"encoding/json"
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

// recordsRecorder is a repository that resolves the person kind and keeps
// what the records route handed its List and its Search, answering an empty
// page and an empty ranking: the decoding is the handler's, and only the
// handed input shows it whole.
type recordsRecorder struct {
	stubKinds
	query  substrate.Query
	search substrate.SearchInput
}

func newRecordsRecorder() *recordsRecorder {
	return &recordsRecorder{stubKinds: newStubKinds(personKind)}
}

func (d *recordsRecorder) List(_ context.Context, q substrate.Query) (*substrate.Page, error) {
	d.query = q
	return &substrate.Page{Records: []*substrate.Record{}, Generation: "recordsgeneration"}, nil
}

func (d *recordsRecorder) Search(_ context.Context, in substrate.SearchInput) (substrate.SearchResult, error) {
	d.search = in
	return substrate.SearchResult{Hits: []substrate.Hit{}}, nil
}

// --- the list ------------------------------------------------------------

// The list carries the whole grammar to the dataset unchanged: every filter
// arm, the sort, the page and the expansion, and it answers the dataset's
// page as it was given.
func TestRecordsListCarriesTheGrammar(t *testing.T) {
	ds := newRecordsRecorder()
	env := newStubEnv(t, ds)

	f := substrate.Filter{
		Kinds:      []string{personKind},
		Search:     `rack lay* -"weekly sync"`,
		Properties: map[string]substrate.Cond{"company": {Eq: "Analytical"}, "notes": {Match: "engine*"}},
	}
	path := filterPath(t, f, "orderBy="+url.QueryEscape("at:desc,createdAt"), "first=7", "after=cur1",
		"expand="+url.QueryEscape("manager, author"), "withAnnotations=1")
	rec := env.do(t, http.MethodGet, path, stubToken, nil)
	wantStatus(t, rec, http.StatusOK)

	q := ds.query
	if len(q.Filter.Kinds) != 1 || q.Filter.Kinds[0] != personKind {
		t.Fatalf("filter.kinds = %v", q.Filter.Kinds)
	}
	if q.Filter.Properties["company"].Eq != "Analytical" {
		t.Fatalf("filter.properties lost: %+v", q.Filter.Properties)
	}
	if q.Filter.Search != f.Search {
		t.Fatalf("filter.search = %q, want the query verbatim", q.Filter.Search)
	}
	if q.Filter.Properties["notes"].Match != "engine*" {
		t.Fatalf("filter.properties.notes.match lost: %+v", q.Filter.Properties)
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
	if page.Generation != "recordsgeneration" {
		t.Fatalf("page generation = %q, want the dataset's", page.Generation)
	}
}

// `offset` addresses a page by its number and reaches the dataset as it was
// written; it is refused as a companion to `after` and on the ranked read,
// which has no keyset (decision 0085).
func TestRecordsListOffset(t *testing.T) {
	ds := newRecordsRecorder()
	env := newStubEnv(t, ds)

	f := substrate.Filter{Kinds: []string{personKind}}
	rec := env.do(t, http.MethodGet, filterPath(t, f, "first=25", "offset=100"), stubToken, nil)
	wantStatus(t, rec, http.StatusOK)
	if q := ds.query; q.First != 25 || q.Offset != 100 || q.After != "" {
		t.Fatalf("paging = first %d offset %d after %q, want first 25 offset 100 and no cursor", q.First, q.Offset, q.After)
	}

	// Not a number, and negative, are both refused at the door rather than
	// reaching the engine as a zero that silently answers page one.
	for _, bad := range []string{"offset=soon", "offset=-1"} {
		rec := env.do(t, http.MethodGet, filterPath(t, f, bad), stubToken, nil)
		wantStatus(t, rec, http.StatusBadRequest)
		wantMessage(t, rec, "offset")
	}

	// The ranked read names its own grammar and refuses the rest by name.
	rec = env.do(t, http.MethodGet, filterPath(t, f, "q=ada", "offset=10"), stubToken, nil)
	wantStatus(t, rec, http.StatusBadRequest)
	wantMessage(t, rec, "offset")
}

// `count=1` is the one spelling of the count: a value read as "no" would
// answer a page without the number the caller is about to render. The ranked
// read and the tail, which have no filtered set to count, refuse it by name.
// Each refusal is the door's, so the dataset is never reached.
func TestRecordsListCountIsOneSpelling(t *testing.T) {
	env := newStubEnv(t, nil)
	f := substrate.Filter{Kinds: []string{personKind}}
	for _, bad := range []string{"count=true", "count=0", "count="} {
		rec := env.do(t, http.MethodGet, filterPath(t, f, bad), stubToken, nil)
		wantStatus(t, rec, http.StatusBadRequest)
		wantMessage(t, rec, "count")
	}
	for _, mode := range []string{"q=ada", "watch=1"} {
		rec := env.do(t, http.MethodGet, filterPath(t, f, mode, "count=1"), stubToken, nil)
		wantStatus(t, rec, http.StatusBadRequest)
		wantMessage(t, rec, "count")
	}
}

// A parameter the list does not honor is a bad_request naming it, and a
// misspelled filter key is refused rather than silently broadening the list.
func TestRecordsListRefusesUnknownParameters(t *testing.T) {
	env := newStubEnv(t, nil)
	for _, query := range []string{"?bogus=1", "?first=2&bogus=1", "?limit=5", "?from=3", "?generation=g"} {
		rec := env.do(t, http.MethodGet, recordsPath+query, stubToken, nil)
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
	rec := env.do(t, http.MethodGet, recordsPath+"?filter=%7Bnot-json", stubToken, nil)
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
	rec = env.do(t, http.MethodGet, recordsPath+`?filter={"kind":"x"}`, stubToken, nil)
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
	if msg := decodeJSON[substrate.ErrorEnvelope](t, rec).Error.Message; !strings.Contains(msg, `"kind"`) {
		t.Fatalf("a misspelled filter key must be named: %q", msg)
	}
}

// --- the ranked read -----------------------------------------------------

// `q` switches the route to the ranked read: the query, its mode, the kind
// narrowing, the purposes and the hit count reach Search as written, the
// mode folded to its lower-case spelling. The ranked page itself is the
// engine's (TestRecordsRankedReadAnswersTheRankedPage).
func TestRecordsRankedReadShapesTheSearch(t *testing.T) {
	ds := newRecordsRecorder()
	env := newStubEnv(t, ds)

	rec := env.do(t, http.MethodGet, recordsOf(t, personKind, "q=ada", "mode=Lexical", "first=5"), stubToken, nil)
	wantStatus(t, rec, http.StatusOK)
	in := ds.search
	if in.Q != "ada" || in.Mode != substrate.SearchLexical || in.K != 5 ||
		len(in.Kinds) != 1 || in.Kinds[0] != personKind {
		t.Fatalf("search input = %+v", in)
	}

	// filter.purposes narrows the ranked read and reaches Search beside the
	// kinds.
	rec = env.do(t, http.MethodGet, filterPath(t, substrate.Filter{Purposes: []string{"primary", "supporting"}}, "q=ada"), stubToken, nil)
	wantStatus(t, rec, http.StatusOK)
	if got := ds.search.Purposes; len(got) != 2 || got[0] != "primary" || got[1] != "supporting" {
		t.Fatalf("search purposes = %v", got)
	}
}

// The ranked read narrows by filter.kinds and filter.purposes alone: both
// arms cap candidates before hydration, so a predicate applied afterwards
// would not produce the filtered top-k. Every other arm, and every list
// parameter, is refused by name.
func TestRecordsRankedReadRefusesWhatItCannotHonor(t *testing.T) {
	env := newStubEnv(t, nil)
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
		// `q` IS the search; a second query in the filter would be one of
		// them silently ignored.
		{filterPath(t, substrate.Filter{Search: "y"}, "q=x"), "filter.search"},
	} {
		rec := env.do(t, http.MethodGet, tc.path, stubToken, nil)
		wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
		if msg := decodeJSON[substrate.ErrorEnvelope](t, rec).Error.Message; !strings.Contains(msg, tc.want) {
			t.Errorf("%s: message = %q, want %s named", tc.path, msg, tc.want)
		}
	}
}

// --- the tail ------------------------------------------------------------

// `watch=1` is the tail, narrowed by filter.kinds alone: the change filter
// has no property arms, so any other arm is refused by name, as is every
// list parameter. The tail's own parameters are honored
// (TestRecordsWatchHonorsItsOwnParameters).
func TestRecordsWatchRefusesWhatItCannotHonor(t *testing.T) {
	env := newStubEnv(t, nil)
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
		{filterPath(t, substrate.Filter{Search: "x"}, "watch=1"), "filter.search"},
		// The tail matches a change against its kinds as it streams, and a
		// purpose resolved once would go stale as kinds are declared.
		{filterPath(t, substrate.Filter{Purposes: []string{"primary"}}, "watch=1"), "filter.purposes"},
	} {
		rec := env.do(t, http.MethodGet, tc.path, stubToken, nil)
		wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
		if msg := decodeJSON[substrate.ErrorEnvelope](t, rec).Error.Message; !strings.Contains(msg, tc.want) {
			t.Errorf("%s: message = %q, want %s named", tc.path, msg, tc.want)
		}
	}
}

// --- the create ----------------------------------------------------------

// A key beside the create envelope's is refused by the strict decode, named,
// before the dataset is reached.
func TestRecordsPostRefusesAnUnknownKey(t *testing.T) {
	env := newStubEnv(t, nil)
	rec := env.do(t, http.MethodPost, recordsPath, stubToken, map[string]any{"kind": personKind, "propertys": map[string]any{}})
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
	wantMessage(t, rec, `"propertys"`)
}

// `kind` is required, and `id` is refused: a chosen id is a PUT at the record
// path, so the refusal names that PUT. Both are the door's, so nothing is
// written: the stub has no Put, and reaching it would be a 500.
func TestRecordsPostRequiresKindAndRefusesID(t *testing.T) {
	env := newStubEnv(t, nil)

	rec := env.do(t, http.MethodPost, recordsPath, stubToken, map[string]any{"properties": map[string]any{"name": "Ada"}})
	wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)
	wantMessage(t, rec, "kind is required")

	rec = env.do(t, http.MethodPost, recordsPath, stubToken, map[string]any{
		"kind": personKind, "id": "chosen", "properties": map[string]any{"name": "Ada"},
	})
	wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)
	wantMessage(t, rec, "PUT "+peoplePath+"/chosen")
}

// There is no collection path: a three-segment path under the version prefix
// names nothing, in every method, and answers the router's JSON 404. The
// stub has no Put, so a write that reached the dataset would be a 500.
func TestRecordsHasNoCollectionPath(t *testing.T) {
	env := newStubEnv(t, newStubKinds(personKind))
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		rec := env.do(t, method, peoplePath, stubToken, map[string]any{"properties": map[string]any{"name": "x"}})
		wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
		wantMessage(t, rec, "no such API path")
	}
}
