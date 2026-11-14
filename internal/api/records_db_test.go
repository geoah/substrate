package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// recordsStaff declares, under the repository's full name, two kinds that
// point at a person (the people sample, which has no pointer from one person
// to another): a member's `manager`, which may name a person that is not
// there, and a memo's `author`, whose link carries `since`, beside its
// repeated `mentions`. It answers the two kind references as the repository
// holds them, in its own namespace, `~` (decision record 0259).
func recordsStaff(t *testing.T, env *dbEnv) (member, memo string) {
	t.Helper()
	env.declare(t, fmt.Sprintf(`
kind: substrate.reamde.dev/core/package
metadata:
  id: %[1]s/staff
data:
  authority: %[1]s
  package: staff
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: %[1]s/staff/member
data:
  authority: %[1]s
  package: staff
  names:
    singular: member
  displayTemplate: "{name}"
  properties:
    name:
      type: string
    manager:
      type: reference
      kind: %[2]s
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: %[1]s/staff/memo
data:
  authority: %[1]s
  package: staff
  names:
    singular: memo
  displayTemplate: "{text}"
  properties:
    text:
      type: string
    author:
      type: reference
      kind: %[2]s
      properties:
        since:
          type: int
    mentions:
      type: reference
      kind: %[2]s
      repeated: true
`, env.repository, personKind))
	return "~/staff/member", "~/staff/memo"
}

// recordsCount reads the `count` a list answered: absent is (0, false).
func recordsCount(t *testing.T, rec *httptest.ResponseRecorder) (int64, bool) {
	t.Helper()
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	raw, ok := body["count"]
	if !ok {
		return 0, false
	}
	var n int64
	if err := json.Unmarshal(raw, &n); err != nil {
		t.Fatalf("count = %s, want a number", raw)
	}
	return n, true
}

// `count=1` asks for the size of the filtered set beside the page: the answer
// carries `count` (zero included), and a list that did not ask carries none.
// The spelling and the modes that refuse it are the door's
// (TestRecordsListCountIsOneSpelling).
func TestRecordsListCount(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	f := substrate.Filter{Kinds: []string{personKind}}

	// An empty set is counted, not omitted: zero is an answer.
	rec := env.do(t, http.MethodGet, filterPath(t, f, "first=1", "count=1"), env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	if n, ok := recordsCount(t, rec); !ok || n != 0 {
		t.Fatalf("count over an empty set = %d (present %v), want 0 present", n, ok)
	}

	for _, name := range []string{"Ada", "Grace", "Hedy"} {
		createRecord(t, env.testEnv, env.token, personKind, map[string]any{"name": name})
	}
	rec = env.do(t, http.MethodGet, filterPath(t, f, "first=1", "count=1"), env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	if n, ok := recordsCount(t, rec); !ok || n != 3 {
		t.Fatalf("count = %d (present %v), want 3", n, ok)
	}
	if page := decodeJSON[substrate.Page](t, rec); len(page.Records) != 1 {
		t.Fatalf("first=1 answered %d records; the count is the set's, the page is one", len(page.Records))
	}

	// A list that did not ask carries no count at all.
	rec = env.do(t, http.MethodGet, filterPath(t, f), env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	if n, ok := recordsCount(t, rec); ok {
		t.Fatalf("a list without count=1 answered count %d", n)
	}
}

// With no filter at all the list is every record: there is no collection to
// scope it, so the route does not invent one. The two newest records are of
// two kinds.
func TestRecordsListWithoutAFilterIsEveryKind(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("tasks"))
	env.put(t, personKind, "p1", map[string]any{"name": "Ada"})
	env.put(t, "samples.substrate.reamde.dev/tasks/task", "t1", map[string]any{"name": "Write"})

	rec := env.do(t, http.MethodGet, recordsPath+"?orderBy=createdAt:desc&first=2", env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	page := decodeJSON[substrate.Page](t, rec)
	var got []string
	for _, r := range page.Records {
		got = append(got, r.Kind+"/"+r.ID)
	}
	sort.Strings(got)
	want := []string{personKind + "/p1", "samples.substrate.reamde.dev/tasks/task/t1"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("records = %v, want both kinds %v", got, want)
	}
}

// filter.kinds resolves each reference against the repository's kinds, so an
// unknown one is the record path's 404, named, in every mode.
func TestRecordsUnknownKindInFilterIs404(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	const unknown = "samples.substrate.reamde.dev/people/widget"
	for _, extra := range [][]string{nil, {"q=ada"}, {"watch=1"}} {
		rec := env.do(t, http.MethodGet, recordsOf(t, unknown, extra...), env.token, nil)
		wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
		if msg := decodeJSON[substrate.ErrorEnvelope](t, rec).Error.Message; !strings.Contains(msg, "unknown kind "+unknown) {
			t.Fatalf("%v: message = %q, want the kind named", extra, msg)
		}
	}
}

// A bare word in filter.kinds, or as a create's kind, is not a missing kind
// but a malformed reference: 422, naming the identity the repository declares
// under it, on every read mode and on the create (decision record 0101).
func TestRecordsBareKindIs422NamingTheSpelling(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	for _, extra := range [][]string{nil, {"q=ada"}, {"watch=1"}} {
		rec := env.do(t, http.MethodGet, recordsOf(t, "person", extra...), env.token, nil)
		wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)
		wantMessage(t, rec, `"person" is a bare name`, "samples.substrate.reamde.dev/people/person")
	}
	rec := env.do(t, http.MethodPost, "/api/v1/records", env.token, map[string]any{
		"kind": "person", "properties": map[string]any{"name": "Ada"},
	})
	wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)
	wantMessage(t, rec, `"person" is a bare name`, "samples.substrate.reamde.dev/people/person")
}

// The reverse read is a filter arm: `filter.referencing` narrows to the
// records pointing at one target, and the page's `matches` says from which
// property each one points. `property` narrows to one site.
func TestRecordsReferencingFillsMatches(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	member, memo := recordsStaff(t, env)
	const target = personKind + "/p1"
	env.put(t, personKind, "p1", map[string]any{"name": "Sam"})
	env.put(t, member, "p2", map[string]any{"name": "Ada", "manager": map[string]any{"ref": target}})
	env.put(t, memo, "m1", map[string]any{"text": "hi", "author": map[string]any{"ref": target, "since": 3}})
	env.put(t, personKind, "p3", map[string]any{"name": "Nobody"})

	rec := env.do(t, http.MethodGet, filterPath(t, substrate.Filter{Referencing: &substrate.Referencing{Ref: target}}), env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	page := decodeJSON[substrate.Page](t, rec)
	if len(page.Records) != 2 {
		t.Fatalf("referencing page = %+v, want the two pointers", page.Records)
	}
	if got := page.Matches[member+"/p2"]; len(got) != 1 || got[0].Property != "manager" {
		t.Fatalf("matches for p2 = %+v", got)
	}
	if got := page.Matches[memo+"/m1"]; len(got) != 1 || got[0].Property != "author" {
		t.Fatalf("matches for m1 = %+v", got)
	}

	rec = env.do(t, http.MethodGet, filterPath(t, substrate.Filter{
		Kinds:       []string{member},
		Referencing: &substrate.Referencing{Ref: target, Property: "manager"},
	}), env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	page = decodeJSON[substrate.Page](t, rec)
	if len(page.Records) != 1 || page.Records[0].ID != "p2" {
		t.Fatalf("property-narrowed page = %+v", page.Records)
	}
}

// `referencing.refs` is several targets in one read, spelled on the wire as a
// list beside the single `ref`: a record pointing at two of them is on the
// page once, and the list past the cap, or set together with `ref`, is
// refused naming why.
func TestRecordsReferencingTakesAListOfTargets(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	member, memo := recordsStaff(t, env)
	const work, home = personKind + "/work", personKind + "/home"
	env.put(t, personKind, "work", map[string]any{"name": "Work"})
	env.put(t, personKind, "home", map[string]any{"name": "Home"})
	env.put(t, member, "a", map[string]any{"name": "A", "manager": map[string]any{"ref": work}})
	env.put(t, member, "b", map[string]any{"name": "B", "manager": map[string]any{"ref": home}})
	env.put(t, memo, "both", map[string]any{
		"text": "hi", "author": map[string]any{"ref": work}, "mentions": []any{map[string]any{"ref": home}},
	})
	env.put(t, member, "none", map[string]any{"name": "None"})

	raw := `{"referencing":{"refs":["` + work + `","` + home + `"]}}`
	rec := env.do(t, http.MethodGet, recordsPath+"?filter="+url.QueryEscape(raw), env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	page := decodeJSON[substrate.Page](t, rec)
	var ids []string
	for _, e := range page.Records {
		ids = append(ids, e.ID)
	}
	sort.Strings(ids)
	if strings.Join(ids, ",") != "a,b,both" {
		t.Fatalf("referencing refs page = %v, want a, b and both once each", ids)
	}
	if got := page.Matches[memo+"/both"]; len(got) != 2 {
		t.Fatalf("matches for the record pointing at both = %+v, want both sites", got)
	}

	// The list keeps the `property` narrowing the single form has.
	raw = `{"referencing":{"refs":["` + work + `","` + home + `"],"property":"manager"}}`
	rec = env.do(t, http.MethodGet, recordsPath+"?filter="+url.QueryEscape(raw), env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	if page := decodeJSON[substrate.Page](t, rec); len(page.Records) != 2 {
		t.Fatalf("property-narrowed refs page = %+v, want a and b", page.Records)
	}

	tooMany := make([]string, substrate.MaxReferencingTargets+1)
	for i := range tooMany {
		tooMany[i] = personKind + "/p" + strconv.Itoa(i)
	}
	tooManyJSON, err := json.Marshal(tooMany)
	if err != nil {
		t.Fatal(err)
	}
	// Spelled as the wire carries them, so an empty `refs` beside `ref` is a
	// case: a Go client's omitempty would drop it before it left.
	for name, tc := range map[string]struct{ referencing, want string }{
		"past the cap":       {`{"refs":` + string(tooManyJSON) + `}`, "the cap is " + strconv.Itoa(substrate.MaxReferencingTargets)},
		"ref and refs":       {`{"ref":"` + work + `","refs":["` + home + `"]}`, "set ref or refs, not both"},
		"ref and empty refs": {`{"ref":"` + work + `","refs":[]}`, "set ref or refs, not both"},
		"an empty refs":      {`{"refs":[]}`, "name a target"},
		"neither":            {`{"property":"manager"}`, "name a target"},
	} {
		raw := `{"referencing":` + tc.referencing + `}`
		rec := env.do(t, http.MethodGet, recordsPath+"?filter="+url.QueryEscape(raw), env.token, nil)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s: status = %d, want 422: %s", name, rec.Code, rec.Body.String())
		}
		wantMessage(t, rec, tc.want)
	}
}

// The reverse read sizes its fan-in only when asked: a referencing page
// without `count=1` answers no count, and one with it answers the number of
// pointers, beyond the one record the page holds (issue #334).
func TestRecordsReferencingCountsOnRequest(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	member, _ := recordsStaff(t, env)
	const target = personKind + "/p1"
	env.put(t, personKind, "p1", map[string]any{"name": "Sam"})
	for _, id := range []string{"p2", "p3", "p4"} {
		env.put(t, member, id, map[string]any{"name": id, "manager": map[string]any{"ref": target}})
	}
	f := substrate.Filter{Referencing: &substrate.Referencing{Ref: target}}

	rec := env.do(t, http.MethodGet, filterPath(t, f, "first=1"), env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	if n, ok := recordsCount(t, rec); ok {
		t.Fatalf("a referencing page without count=1 answered count %d", n)
	}

	rec = env.do(t, http.MethodGet, filterPath(t, f, "first=1", "count=1"), env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	page := decodeJSON[substrate.Page](t, rec)
	if page.Count == nil {
		t.Fatal("a referencing page with count=1 answered no count")
	}
	if *page.Count != 3 || len(page.Records) != 1 {
		t.Fatalf("count = %d over a page of %d, want the three pointers over one", *page.Count, len(page.Records))
	}
}

// `expand` hydrates the named reference properties one hop: the referents
// land in `included`, keyed by record path, each once; a dangling pointer has
// no entry.
func TestRecordsExpandFillsIncluded(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	member, _ := recordsStaff(t, env)
	env.put(t, personKind, "boss", map[string]any{"name": "Sam"})
	env.put(t, member, "p2", map[string]any{"name": "Ada", "manager": map[string]any{"ref": personKind + "/boss"}})
	env.put(t, member, "p3", map[string]any{"name": "Grace", "manager": map[string]any{"ref": personKind + "/boss"}})
	env.put(t, member, "p4", map[string]any{"name": "Orphan", "manager": map[string]any{"ref": personKind + "/gone"}})

	rec := env.do(t, http.MethodGet, recordsOf(t, member, "expand=manager"), env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	page := decodeJSON[substrate.Page](t, rec)
	if len(page.Records) != 3 {
		t.Fatalf("records = %d, want the three members", len(page.Records))
	}
	if len(page.Included) != 1 {
		t.Fatalf("included = %+v, want the one referent once", page.Included)
	}
	if boss := page.Included[personKind+"/boss"]; boss == nil || boss.Properties["name"] != "Sam" {
		t.Fatalf("included[boss] = %+v", boss)
	}

	// Without `expand`, the key is absent, not an empty object.
	rec = env.do(t, http.MethodGet, recordsOf(t, member), env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	if strings.Contains(rec.Body.String(), `"included"`) {
		t.Fatalf("an unexpanded page carries included: %s", rec.Body.String())
	}
}

// A list cursor from another history is the changefeed's signal, 410
// `compacted` with the head to start over from, never a 422 the client reads
// as its own mistake. The cursor is minted by a second repository, whose
// history is another one.
func TestRecordsStaleCursorIs410Compacted(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	ctx := context.Background()
	other := testdb.RepositoryLabel(t) + "2.example.com"
	if _, err := env.svc.CreateRepository(ctx, other); err != nil {
		t.Fatalf("create the other repository: %v", err)
	}
	ods, err := env.svc.Dataset(ctx, other)
	if err != nil {
		t.Fatalf("open the other repository: %v", err)
	}
	theirs, err := ods.List(ctx, substrate.Query{First: 1})
	if err != nil || theirs.Cursor == "" {
		t.Fatalf("the other repository's first page = %+v (%v), want a cursor", theirs, err)
	}
	head, err := env.ds.Head(ctx)
	if err != nil {
		t.Fatalf("head: %v", err)
	}

	rec := env.do(t, http.MethodGet, recordsPath+"?first=1&after="+url.QueryEscape(theirs.Cursor), env.token, nil)
	wantErrorCode(t, rec, http.StatusGone, codeCompacted)
	problem := decodeJSON[substrate.ErrorEnvelope](t, rec).Error
	if problem.Head == nil || *problem.Head != head.Seq || problem.Generation != head.Generation {
		t.Fatalf("compacted problem = %+v, want head %d and the live generation %q", problem, head.Seq, head.Generation)
	}
}

// The ranked read answers the ranked page: the hits with the per-arm scores
// keyed by record path, no keyset and no snapshot, how much of the index is
// pending, and an empty ranking as `[]`. filter.purposes narrows it.
func TestRecordsRankedReadAnswersTheRankedPage(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	env.put(t, personKind, "p1", map[string]any{"name": "Ada Lovelace"})
	env.put(t, personKind, "p2", map[string]any{"name": "Grace Hopper"})

	rec := env.do(t, http.MethodGet, recordsOf(t, personKind, "q=ada", "mode=Lexical", "first=5"), env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	page := decodeJSON[substrate.RankedPage](t, rec)
	if len(page.Records) != 1 || page.Records[0].ID != "p1" {
		t.Fatalf("ranked records = %+v", page.Records)
	}
	if s := page.Scores[personKind+"/p1"]; s.Lexical <= 0 || s.Semantic != 0 {
		t.Fatalf("scores = %+v, want a lexical score alone", page.Scores)
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

	// A person is a primary kind: narrowed to the supporting kinds the read
	// finds nobody, narrowed to the primary ones it finds Ada.
	for purpose, want := range map[string]int{"supporting": 0, "primary": 1} {
		rec = env.do(t, http.MethodGet, filterPath(t, substrate.Filter{Kinds: []string{personKind}, Purposes: []string{purpose}}, "q=ada"), env.token, nil)
		wantStatus(t, rec, http.StatusOK)
		if got := decodeJSON[substrate.RankedPage](t, rec).Records; len(got) != want {
			t.Fatalf("purposes [%s]: %d records, want %d", purpose, len(got), want)
		}
	}

	// An empty ranking is `[]`, never null.
	rec = env.do(t, http.MethodGet, recordsPath+"?q=zzqxjvwk", env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	if !strings.Contains(rec.Body.String(), `"records":[]`) {
		t.Fatalf("empty ranking = %s", rec.Body.String())
	}
}

// The tail honors its own parameters: the kind narrowing, `from`, and the
// generation the cursor belongs to.
func TestRecordsWatchHonorsItsOwnParameters(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	head, err := env.ds.Head(context.Background())
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	wantNotRefused(t, env.testEnv, recordsOf(t, personKind, "watch=1", "from=0"), env.token)
	wantNotRefused(t, env.testEnv, recordsPath+"?watch=1&from=1&generation="+head.Generation, env.token)
}

// POST creates under a server-assigned id: the body names its kind, an
// unknown one is 404, and the status says what the write did.
func TestRecordsPostCreatesUnderTheBodysKind(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))

	created := createRecord(t, env.testEnv, env.token, personKind, map[string]any{"name": "Ada"})
	if created.Kind != personKind || created.ID == "" || created.Version != 1 {
		t.Fatalf("created = %+v", created)
	}
	if got := env.get(t, personKind, created.ID); got == nil || got.Properties["name"] != "Ada" {
		t.Fatalf("stored under the body's kind and the assigned id: %+v", got)
	}

	rec := env.do(t, http.MethodPost, recordsPath, env.token, map[string]any{
		"kind": "samples.substrate.reamde.dev/people/widget", "properties": map[string]any{},
	})
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
	wantMessage(t, rec, "unknown kind samples.substrate.reamde.dev/people/widget")
}

// A repeat of a POST under one Idempotency-Key answers the first record with
// its 201; the same key with another body is the 409 naming the key, and a
// POST without the header is a write of its own.
func TestRecordsPostHonorsIdempotencyKey(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	body := map[string]any{"kind": personKind, "properties": map[string]any{"name": "Ada"}}
	people := func() int64 {
		t.Helper()
		rec := env.do(t, http.MethodGet, recordsOf(t, personKind, "count=1"), env.token, nil)
		wantStatus(t, rec, http.StatusOK)
		n, _ := recordsCount(t, rec)
		return n
	}

	first := env.do(t, http.MethodPost, recordsPath, env.token, body, idempotencyHeader, "reused")
	wantStatus(t, first, http.StatusCreated)
	repeat := env.do(t, http.MethodPost, recordsPath, env.token, body, idempotencyHeader, "reused")
	wantStatus(t, repeat, http.StatusCreated)
	a, b := decodeJSON[substrate.Record](t, first), decodeJSON[substrate.Record](t, repeat)
	if a.ID != b.ID {
		t.Fatalf("the repeat created %s, want the first attempt's %s", b.ID, a.ID)
	}
	if n := people(); n != 1 {
		t.Fatalf("people = %d, want the one the first attempt created", n)
	}

	rec := env.do(t, http.MethodPost, recordsPath, env.token,
		map[string]any{"kind": personKind, "properties": map[string]any{"name": "Grace"}}, idempotencyHeader, "reused")
	wantErrorCode(t, rec, http.StatusConflict, codeConflict)
	wantMessage(t, rec, `Idempotency-Key "reused"`)

	// Without the header the write carries no key, so the same body is a
	// second record.
	rec = env.do(t, http.MethodPost, recordsPath, env.token, body)
	wantStatus(t, rec, http.StatusCreated)
	if c := decodeJSON[substrate.Record](t, rec); c.ID == a.ID {
		t.Fatalf("a request without the header answered the keyed record %s", c.ID)
	}
	if n := people(); n != 2 {
		t.Fatalf("people = %d, want the keyed one and the unkeyed one", n)
	}
}

// PUT at the record path is the other create, and a second PUT there is the
// update: the two doors never disagree about what a repeat does.
func TestRecordsPostAndPutReportCreateAndUpdate(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))

	created := createRecord(t, env.testEnv, env.token, personKind, map[string]any{"name": "Ada"})
	rec := env.do(t, http.MethodPut, peoplePath+"/"+created.ID, env.token, map[string]any{"properties": map[string]any{"name": "Ada L"}})
	wantStatus(t, rec, http.StatusOK)
	if e := decodeJSON[substrate.Record](t, rec); e.Version != 2 {
		t.Fatalf("the re-put landed at version %d", e.Version)
	}
	rec = env.do(t, http.MethodPut, peoplePath+"/"+mintedID("fresh"), env.token, map[string]any{"properties": map[string]any{"name": "Grace"}})
	wantStatus(t, rec, http.StatusCreated)
}

// A PUT that changes nothing is not a create, even on a record no write has
// moved past version 1: a client counting its creates by status counts one.
func TestRecordsPutThatChangesNothingIsNotACreate(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	body := map[string]any{"properties": map[string]any{"name": "Grace"}}
	path := peoplePath + "/" + mintedID("fresh")

	wantStatus(t, env.do(t, http.MethodPut, path, env.token, body), http.StatusCreated)
	rec := env.do(t, http.MethodPut, path, env.token, body)
	wantStatus(t, rec, http.StatusOK)
	if e := decodeJSON[substrate.Record](t, rec); e.Version != 1 {
		t.Fatalf("the unchanged re-put moved the record to version %d", e.Version)
	}
}
