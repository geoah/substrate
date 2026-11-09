package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The wider scoped surface (decision 0219): the window and aggregate reads,
// the semantic search, a blob's bytes, the function call and the feed's
// history, each held to what the token reads.

const (
	observationKind = fakeRepository + "/observations/observation"
	sourceKind      = fakeRepository + "/observations/source"
	albumKind       = "samples.substrate.reamde.dev/media/album"
	summarizeFn     = fakeRepository + "/tasks/summarize"
	reportFn        = fakeRepository + "/tasks/report"
	chainFn         = fakeRepository + "/tasks/chain"
	runnerFn        = fakeRepository + "/tasks/runner"
	coreQueryFn     = corePackage + "/query"
)

func TestAnAppsWindowReadIsHeldToTheKindsItReads(t *testing.T) {
	e := newAppEnv(t)
	seedWindow(e.svc.datasets[fakeRepository])
	from, to := "2026-07-01T00:00:00Z", "2026-07-06T00:00:00Z"

	meetings := e.grant("read:"+kindMeeting, []string{"read:" + kindMeeting})
	rec := e.do(http.MethodGet, windowPath(from, to), meetings, nil, "")
	wantStatus(t, rec, http.StatusOK)
	page := decodeJSON[windowPage](t, rec)
	if len(page.Records) != 1 || page.Records[0].ID != "dentist" {
		t.Fatalf("a window over every readable kind answered %+v, want the meeting alone", page.Records)
	}
	if body := decodeJSON[substrate.Page](t, e.do(http.MethodGet, windowPath(from, to), meetings, nil, "")); body.Head != 0 {
		t.Fatalf("an app's window page carries head %d", body.Head)
	}
	// A kind the token cannot read is unknown, named alone or beside one it
	// reads.
	for _, kinds := range [][]string{{kindDose}, {kindDose, kindMeeting}} {
		filter := map[string]any{"kinds": kinds, "properties": map[string]any{"at": map[string]any{"gte": from, "lt": to}}}
		raw, _ := json.Marshal(filter)
		wantStatus(t, e.do(http.MethodGet, "/api/v1/records?filter="+url.QueryEscape(string(raw)), meetings, nil, ""), http.StatusNotFound)
	}

	// Reading both kinds, the series expands and its override claims its slot.
	both := e.grant("read:samples.substrate.reamde.dev/*", []string{"read:samples.substrate.reamde.dev/*"})
	rec = e.do(http.MethodGet, windowPath(from, to), both, nil, "")
	wantStatus(t, rec, http.StatusOK)
	page = decodeJSON[windowPage](t, rec)
	computed, override, meeting := 0, false, false
	for _, r := range page.Records {
		switch {
		case r.Computed:
			computed++
		case r.ID == "levo_20260704T060000Z":
			override = true
		case r.ID == "dentist":
			meeting = true
		}
	}
	if computed == 0 || !override || !meeting {
		t.Fatalf("a window over both kinds answered %+v, want computed doses, the override and the meeting", page.Records)
	}
}

// seedObservations declares an observation kind the observed index holds, a
// source kind, and the metric kind an aggregate names.
func seedObservations(ds *fakeDataset) {
	ds.types = append(ds.types,
		substrate.KindInfo{Identity: observationKind, Name: "observation", Authority: fakeRepository, Package: "observations", Definition: map[string]any{"properties": map[string]any{}}},
		substrate.KindInfo{Identity: sourceKind, Name: "source", Authority: fakeRepository, Package: "observations", Definition: map[string]any{"properties": map[string]any{}}},
		substrate.KindInfo{Identity: kindMetric, Name: "metric", Authority: coreAuthorityName, Package: "observations", Definition: map[string]any{"properties": map[string]any{}}},
	)
	ds.traits[vocabulary.TraitObservedCore] = append(ds.traits[vocabulary.TraitObservedCore], observationKind)
}

func TestAnAppsAggregateReadIsCutToTheKindsItReads(t *testing.T) {
	e := newAppEnv(t)
	ds := e.svc.datasets[fakeRepository]
	seedObservations(ds)
	metric := kindMetric + "/health/heart_rate"
	body := `{"metric":{"ref":"` + metric + `"},"range":{"gte":"2026-04-06T00:00:00Z","lt":"2026-10-05T00:00:00Z"},"bucket":{"calendar":"week"},"reducers":["avg"]}`
	aggregate := func(filter string) string {
		path := recordsPath + "?aggregate=" + url.QueryEscape(body)
		if filter != "" {
			path += "&filter=" + url.QueryEscape(filter)
		}
		return path
	}
	ds.aggregateResult = &substrate.AggregateResult{
		Metric: metric, Unit: "/min", Exact: true,
		Rows: []substrate.AggregateRow{{
			Values:  []substrate.AggregateValue{{Reducer: substrate.AggregateReducer{Name: substrate.ReducerAvg}}},
			Sources: []string{sourceKind + "/s1", personKind + "/p1"},
			Group:   &substrate.AggregateGroup{Source: sourceKind + "/s1"},
		}},
		Sources: []string{sourceKind + "/s1", personKind + "/p1"},
		Head:    substrate.AggregateHead{Changelog: 9, Generation: "g"},
	}

	// A request naming no kind is cut to the readable kinds the observed
	// index holds, never widened to the person kind or the metric kind.
	app := e.grant("read:~/observations/observation read:"+kindMetric+" read:"+personKind,
		[]string{"read:" + observationKind, "read:" + kindMetric, "read:" + personKind})
	rec := e.do(http.MethodGet, aggregate(""), app, nil, "")
	wantStatus(t, rec, http.StatusOK)
	if q := ds.lastAggregate; q == nil || strings.Join(q.Filter.Kinds, ",") != observationKind {
		t.Fatalf("the aggregate ran over %+v, want the observation kind alone", q)
	}
	res := decodeJSON[substrate.AggregateResult](t, rec)
	if res.Head.Changelog != 0 || res.Head.Generation != "" {
		t.Fatalf("an app's aggregate carries head %+v", res.Head)
	}
	// The sources an answer names are cut to the kinds the token reads, on
	// the answer and on each row, and a group's source with them.
	if strings.Join(res.Sources, ",") != personKind+"/p1" || strings.Join(res.Rows[0].Sources, ",") != personKind+"/p1" {
		t.Fatalf("sources = %v and %v, want the readable record alone", res.Sources, res.Rows[0].Sources)
	}
	if res.Rows[0].Group == nil || res.Rows[0].Group.Source != "" {
		t.Fatalf("the row's group = %+v, want its source left out", res.Rows[0].Group)
	}
	// A kind the token cannot read is unknown, and so is a metric whose kind
	// it cannot read.
	wantStatus(t, e.do(http.MethodGet, aggregate(`{"kinds":["`+taskKind+`"]}`), app, nil, ""), http.StatusNotFound)
	noMetric := e.grant("read:~/observations/*", []string{"read:" + fakeRepository + "/observations/*"})
	wantStatus(t, e.do(http.MethodGet, aggregate(""), noMetric, nil, ""), http.StatusNotFound)
	// A token reading no kind the index holds is answered an empty result.
	people := e.grant("read:"+personKind+" read:"+kindMetric, []string{"read:" + personKind, "read:" + kindMetric})
	ds.lastAggregate = nil
	rec = e.do(http.MethodGet, aggregate(""), people, nil, "")
	wantStatus(t, rec, http.StatusOK)
	if res := decodeJSON[substrate.AggregateResult](t, rec); len(res.Rows) != 0 || ds.lastAggregate != nil {
		t.Fatalf("an aggregate over nothing readable answered %+v and reached the dataset", res)
	}
	// A property read over named kinds the token reads passes as a list does.
	property := recordsPath + "?aggregate=" + url.QueryEscape(`{"property":{"name":"age"},"range":{"gte":"2026-01-01T00:00:00Z","lt":"2026-02-01T00:00:00Z"},"bucket":{"calendar":"week"},"reducers":["avg"]}`) +
		"&filter=" + url.QueryEscape(`{"kinds":["`+personKind+`"]}`)
	wantStatus(t, e.do(http.MethodGet, property, people, nil, ""), http.StatusOK)
	if q := ds.lastAggregate; q == nil || strings.Join(q.Filter.Kinds, ",") != personKind {
		t.Fatalf("the property read ran over %+v", q)
	}
}

func TestAnAppReadsABlobARecordItReadsNamesAndNoOther(t *testing.T) {
	e, d := newDeliveryEnv(t)
	inner := e.svc.datasets[fakeRepository]
	inner.types = append(inner.types, substrate.KindInfo{
		Identity: albumKind, Name: "album", Authority: "samples.substrate.reamde.dev", Package: "media", Version: 1, Source: "installed",
		Definition: map[string]any{"properties": map[string]any{
			"images": map[string]any{"type": "blobref", "repeated": true},
		}},
	})
	digestOf := func(data []byte) string {
		sum := sha256.Sum256(data)
		return substrate.BlobDigestPrefix + hex.EncodeToString(sum[:])
	}
	photo, scan, nobody := []byte("the cat"), []byte("the owner's scan"), []byte("stored and never named")
	for _, b := range []struct {
		data  []byte
		query string
	}{{photo, "?name=cat.jpg"}, {scan, "?name=passport.pdf"}, {nobody, ""}} {
		wantStatus(t, putBlobAs(e, e.owner, "/api/v1/blobs"+b.query, b.data, "image/jpeg"), http.StatusCreated)
	}
	e.seed(substrate.Record{ID: "p1", Kind: photoKind, Properties: map[string]any{"caption": "cat", "image": digestOf(photo)}}, nil)
	e.seed(substrate.Record{ID: "s1", Kind: scanKind, Properties: map[string]any{"file": digestOf(scan)}}, nil)
	e.seed(substrate.Record{ID: "a1", Kind: albumKind, Properties: map[string]any{"images": []any{digestOf(scan)}}}, nil)
	get := func(bearer, digest string, headers map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/blobs/"+digest, nil)
		req.Host = "substrate.example.com"
		req.Header.Set("Authorization", "Bearer "+bearer)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, req)
		return rec
	}

	app := appToken(e.svc, fakeRepository, "read:"+photoKind)
	rec := get(app, digestOf(photo), nil)
	wantStatus(t, rec, http.StatusOK)
	if rec.Body.String() != string(photo) {
		t.Fatalf("the blob read answered %q", rec.Body.String())
	}
	// The manifest's name and media type are the uploader's words, which a
	// token that does not read the blob manifests never learns (0202).
	if rec.Header().Get("Content-Disposition") != "" || rec.Header().Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("the blob read carried the manifest: %v", rec.Header())
	}
	if rec.Header().Get("ETag") == "" {
		t.Fatal("the blob read carries no ETag")
	}
	wantStatus(t, get(app, digestOf(photo), map[string]string{"If-None-Match": rec.Header().Get("ETag")}), http.StatusNotModified)
	// A blob no record the token reads names is as absent as one nobody
	// stored: the two answers are one answer.
	hidden := get(app, digestOf(scan), nil)
	wantStatus(t, hidden, http.StatusNotFound)
	unknown := get(app, digestOf([]byte("never stored")), nil)
	wantStatus(t, unknown, http.StatusNotFound)
	normalize := func(rec *httptest.ResponseRecorder, digest string) string {
		return strings.ReplaceAll(rec.Body.String(), digest, "<digest>")
	}
	if normalize(hidden, digestOf(scan)) != normalize(unknown, digestOf([]byte("never stored"))) {
		t.Fatalf("a hidden blob %s and an unknown one %s are told apart", hidden.Body.String(), unknown.Body.String())
	}
	wantStatus(t, get(app, digestOf(nobody), nil), http.StatusNotFound)
	if d.puts != 3 {
		t.Fatalf("the store saw %d puts", d.puts)
	}
	// A token reading the blob manifests reads the name and type too.
	manifests := appToken(e.svc, fakeRepository, "read:"+photoKind, "read:"+kindBlob)
	rec = get(manifests, digestOf(photo), nil)
	wantStatus(t, rec, http.StatusOK)
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "cat.jpg") || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("a token reading the manifests got %v", rec.Header())
	}
	// A repeated blob-ref names its blobs too, and an exact scope reaches an
	// internal kind's.
	wantStatus(t, get(appToken(e.svc, fakeRepository, "read:"+albumKind), digestOf(scan), nil), http.StatusOK)
	wantStatus(t, get(appToken(e.svc, fakeRepository, "read:"+scanKind), digestOf(scan), nil), http.StatusOK)
	// A glob never reaches the internal kind, so its blob stays hidden under one.
	wantStatus(t, get(appToken(e.svc, fakeRepository, "read:samples.substrate.reamde.dev/vault/*"), digestOf(scan), nil), http.StatusForbidden)
	// A token reading no kind with a blob-ref reaches no blob at all.
	none := get(appToken(e.svc, fakeRepository, "read:"+taskKind), digestOf(photo), nil)
	wantStatus(t, none, http.StatusForbidden)
	if wa := none.Header().Get("WWW-Authenticate"); !strings.Contains(wa, `error="insufficient_scope"`) {
		t.Fatalf("WWW-Authenticate = %q", wa)
	}
}

// callRecord is what the fake's function call was asked, and what the
// context it was asked under carried.
type callRecord struct {
	mu     sync.Mutex
	calls  int
	actor  substrate.Actor
	name   string
	scoped bool
}

type callDataset struct {
	*mintingDataset
	rec *callRecord
}

func (d *callDataset) CallFunction(ctx context.Context, actor substrate.Actor, name string, _ any) (any, int, error) {
	d.rec.mu.Lock()
	defer d.rec.mu.Unlock()
	d.rec.calls++
	d.rec.actor, d.rec.name = actor, name
	_, d.rec.scoped = substrate.ScopesFrom(ctx)
	return map[string]any{"ran": name}, 1, nil
}

// newCallEnv is an app environment whose repository declares four functions
// of its own and one of core: one reading and writing tasks, one reading
// tasks and people, one calling the first, one running an agent that reads
// people.
func newCallEnv(t *testing.T) (*appEnv, *callRecord) {
	t.Helper()
	e := newAppEnv(t)
	rec := &callRecord{}
	e.svc.wrap = func(d *fakeDataset) substrate.Dataset {
		return &callDataset{mintingDataset: &mintingDataset{fakeDataset: d, svc: e.svc}, rec: rec}
	}
	ds := e.svc.datasets[fakeRepository]
	internal := func(identity, name string) substrate.KindInfo {
		return substrate.KindInfo{
			Identity: identity, Name: name, Authority: coreAuthorityName, Package: "core", Version: 1, Source: "builtin",
			Definition: map[string]any{"purpose": vocabulary.PurposeInternal, "properties": map[string]any{
				"description": map[string]any{"type": "string"}, "permissions": map[string]any{"type": "object"},
			}},
		}
	}
	ds.types = append(ds.types, internal(kindFunction, "function"), internal(kindAgent, "agent"))
	fn := func(id, description string, perms map[string]any) {
		e.seed(substrate.Record{ID: id, Kind: kindFunction, Version: 1, Properties: map[string]any{"description": description, "permissions": perms}}, nil)
	}
	fn(summarizeFn, "Summarizes the open tasks", map[string]any{
		"reads": map[string]any{"kinds": []any{taskKind}}, "writes": []any{taskKind},
	})
	fn(reportFn, "Reports on tasks by person", map[string]any{
		"reads": map[string]any{"kinds": []any{taskKind, map[string]any{"ref": vocabulary.CoreKind(vocabulary.DocKind) + "/" + personKind}}},
	})
	fn(chainFn, "Runs the summary", map[string]any{"call": []any{summarizeFn}})
	fn(runnerFn, "Asks the assistant", map[string]any{"agents": []any{talkedAgent}})
	fn(coreQueryFn, "The query built-in", map[string]any{"reads": map[string]any{"kinds": []any{"*"}}})
	e.seed(substrate.Record{ID: talkedAgent, Kind: kindAgent, Version: 1, Properties: map[string]any{
		"permissions": map[string]any{"reads": map[string]any{"kinds": []any{personKind}}},
	}}, nil)
	return e, rec
}

func callPath(name string) string {
	return "/api/v1/" + corePackage + "/function/" + url.PathEscape(name) + "/call"
}

func TestAnAppCallsTheFunctionsItsScopeNamesWhereTheirReachIsCovered(t *testing.T) {
	e, rec := newCallEnv(t)
	body := []byte(`{"input":{"n":1}}`)
	call := func(bearer, name string) *httptest.ResponseRecorder {
		return e.do(http.MethodPost, callPath(name), bearer, body, "application/json")
	}

	app := e.grant("call:~/tasks/* write:"+taskKind, []string{"call:" + fakeRepository + "/tasks/*", "write:" + taskKind})
	got := call(app, summarizeFn)
	wantStatus(t, got, http.StatusOK)
	if out := decodeJSON[substrate.FunctionCalled](t, got); out.Effects != 1 {
		t.Fatalf("the call answered %+v", out)
	}
	rec.mu.Lock()
	if rec.calls != 1 || rec.name != summarizeFn || rec.actor != appActor || rec.scoped {
		t.Fatalf("the call reached the engine %d times as %q for %q, scoped %v; want once, as the app, unscoped", rec.calls, rec.actor, rec.name, rec.scoped)
	}
	rec.mu.Unlock()
	// A function that calls a covered function is covered through it.
	wantStatus(t, call(app, chainFn), http.StatusOK)
	// One whose reach the scopes do not cover is refused, naming what is
	// missing; so is one whose agent reaches further.
	for _, name := range []string{reportFn, runnerFn} {
		refused := call(app, name)
		wantStatus(t, refused, http.StatusForbidden)
		if !strings.Contains(refused.Body.String(), "read "+personKind) {
			t.Fatalf("%s refused with %s, want the uncovered read named", name, refused.Body.String())
		}
	}
	// A core function is never matched, and a function the pattern does not
	// name or the repository does not hold is unknown.
	for _, name := range []string{coreQueryFn, "other.example.com/tasks/summarize", fakeRepository + "/tasks/nothing"} {
		wantStatus(t, call(app, name), http.StatusNotFound)
	}
	// A function is named in full.
	wantStatus(t, call(app, "summarize"), http.StatusUnprocessableEntity)
	rec.mu.Lock()
	if rec.calls != 2 {
		t.Fatalf("refused calls reached the engine: %d calls", rec.calls)
	}
	rec.mu.Unlock()

	// A read scope covers the function's read and not its write.
	reader := e.grant("call:~/tasks/* read:"+taskKind, []string{"call:" + fakeRepository + "/tasks/*", "read:" + taskKind})
	refused := call(reader, summarizeFn)
	wantStatus(t, refused, http.StatusForbidden)
	if !strings.Contains(refused.Body.String(), "write "+taskKind) {
		t.Fatalf("refused with %s, want the uncovered write named", refused.Body.String())
	}
	// A token without a call scope reaches no function.
	none := call(e.grant("write:"+taskKind, []string{"write:" + taskKind}), summarizeFn)
	wantStatus(t, none, http.StatusForbidden)
	if wa := none.Header().Get("WWW-Authenticate"); !strings.Contains(wa, `error="insufficient_scope"`) {
		t.Fatalf("WWW-Authenticate = %q", wa)
	}
}

func TestTheApprovalPageShowsTheFunctionsACallScopeMatchesWithTheirReach(t *testing.T) {
	e, _ := newCallEnv(t)
	id := e.park(appRequest("call:~/tasks/* read:" + taskKind))
	view := e.do(http.MethodGet, "/api/v1/authorizations/"+id, e.owner, nil, "")
	wantStatus(t, view, http.StatusOK)
	got := decodeJSON[substrate.AuthorizationRequest](t, view)
	if len(got.Scopes) != 2 || got.Scopes[0].Verb != "call" || !got.Scopes[0].Glob || len(got.Scopes[0].Kinds) != 0 {
		t.Fatalf("scopes = %+v", got.Scopes)
	}
	fns := got.Scopes[0].Functions
	ids := make([]string, len(fns))
	for i, f := range fns {
		ids[i] = f.Identity
	}
	if strings.Join(ids, ",") != strings.Join([]string{chainFn, reportFn, runnerFn, summarizeFn}, ",") {
		t.Fatalf("the call scope matches %v, want the four functions of the package and never core's", ids)
	}
	byID := map[string]substrate.AuthorizationCallFunction{}
	for _, f := range fns {
		byID[f.Identity] = f
	}
	summarize := byID[summarizeFn]
	if summarize.Description != "Summarizes the open tasks" || strings.Join(summarize.Reads, ",") != taskKind || strings.Join(summarize.Writes, ",") != taskKind ||
		summarize.Covered || strings.Join(summarize.Uncovered, ",") != "write "+taskKind {
		t.Fatalf("summarize = %+v", summarize)
	}
	if report := byID[reportFn]; strings.Join(report.Reads, ",") != personKind+","+taskKind || report.Covered || strings.Join(report.Uncovered, ",") != "read "+personKind {
		t.Fatalf("report = %+v", report)
	}
	if chain := byID[chainFn]; strings.Join(chain.Writes, ",") != taskKind || chain.Covered {
		t.Fatalf("chain carries the reach of what it calls: %+v", chain)
	}
	if runner := byID[runnerFn]; strings.Join(runner.Reads, ",") != personKind || runner.Covered {
		t.Fatalf("runner carries the reach of the agent it runs: %+v", runner)
	}
	if len(got.Scopes[1].Functions) != 0 || len(got.Scopes[1].Kinds) != 1 {
		t.Fatalf("the read scope = %+v", got.Scopes[1])
	}
	// A request whose scopes cover the reach shows the function covered.
	id = e.park(appRequest("call:~/tasks/summarize write:" + taskKind))
	got = decodeJSON[substrate.AuthorizationRequest](t, e.do(http.MethodGet, "/api/v1/authorizations/"+id, e.owner, nil, ""))
	if fns := got.Scopes[0].Functions; len(fns) != 1 || !fns[0].Covered || fns[0].Uncovered != nil || got.Scopes[0].Glob {
		t.Fatalf("an exact call scope = %+v", got.Scopes[0])
	}
}

func TestAnAppReadsTheFeedsHistoryCutToWhatItReads(t *testing.T) {
	e, _ := newAgentEnv(t)
	app := e.grant("read:"+personKind+" agents:~/chat/*", []string{"read:" + personKind, "agents:" + fakeRepository + "/chat/*"})
	ds := e.svc.datasets[fakeRepository]
	put := func(kind, id string) {
		ds.commit(substrate.Change{
			TS: time.Unix(1, 0).UTC(), Actor: substrate.ActorAPI, Op: substrate.OpPut, Kind: kind, RecordID: id,
			Payload: map[string]any{"properties": map[string]any{"name": "secret"}}, Hash: "abc",
			Affected: []substrate.AffectedRecord{{Kind: kind, ID: id, Version: 2}, {Kind: taskKind, ID: "t1", Version: 3}},
		})
	}
	put(taskKind, "t1")    // 1, hidden
	put(personKind, "p1")  // 2
	put(kindMessage, "mB") // 3, another agent's
	put(kindMessage, "mA") // 4
	put(taskKind, "t1")    // 5, hidden
	ds.trStates[4] = []substrate.ChangeTrigger{{Trigger: "x"}}

	// The history page, newest first: hidden rows are skipped and the page
	// stays full while more lies below, so the cursor is right.
	rec := e.do(http.MethodGet, "/api/v1/changes?before=0&first=2", app, nil, "")
	wantStatus(t, rec, http.StatusOK)
	page := decodeJSON[substrate.ChangePage](t, rec)
	if len(page.Changes) != 2 || page.Changes[0].RecordID != "mA" || page.Changes[1].RecordID != "p1" || page.Cursor != 2 {
		t.Fatalf("page = %+v", page)
	}
	for _, row := range page.Changes {
		if row.Payload != nil || row.Hash != "" || len(row.Triggers) != 0 {
			t.Fatalf("a row carries more than names: %+v", row)
		}
		if len(row.Affected) != 1 || row.Affected[0].Kind == taskKind {
			t.Fatalf("affected = %+v, want the readable record alone", row.Affected)
		}
	}
	if !strings.Contains(rec.Body.String(), `"generation"`) || strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("body = %s", rec.Body.String())
	}
	next := e.do(http.MethodGet, "/api/v1/changes?before=2&first=2&generation="+ds.generation, app, nil, "")
	wantStatus(t, next, http.StatusOK)
	if page := decodeJSON[substrate.ChangePage](t, next); len(page.Changes) != 0 || page.Cursor != 0 {
		t.Fatalf("the second page = %+v, want the bottom", page)
	}
	// Run summaries read the same held rows.
	runs := e.do(http.MethodGet, "/api/v1/changes?runs=1", app, nil, "")
	wantStatus(t, runs, http.StatusOK)
	if got := decodeJSON[substrate.ChangeRunPage](t, runs); len(got.Runs) != 2 || got.Runs[0].Kind != kindMessage || got.Runs[1].Kind != personKind || got.Runs[1].Count != 1 {
		t.Fatalf("runs = %+v", got.Runs)
	}
	// The forward read from a cursor, held the same way.
	forward := e.do(http.MethodGet, "/api/v1/changes?from=0", app, nil, "")
	wantStatus(t, forward, http.StatusOK)
	lines := strings.Split(strings.TrimSpace(forward.Body.String()), "\n")
	if len(lines) != 3 || !strings.Contains(lines[0], `"bookmark"`) || !strings.Contains(lines[1], `"p1"`) || !strings.Contains(lines[2], `"mA"`) {
		t.Fatalf("forward read = %q", lines)
	}
	// A kind it cannot read is unknown on the history too.
	wantStatus(t, e.do(http.MethodGet, "/api/v1/changes?before=0&kinds="+taskKind, app, nil, ""), http.StatusNotFound)
	// A token reading nothing the feed holds is answered an empty history.
	other := e.grant("read:"+taskKind, []string{"read:" + taskKind})
	rec = e.do(http.MethodGet, "/api/v1/changes?before=0&first=5&kinds="+taskKind, other, nil, "")
	wantStatus(t, rec, http.StatusOK)
	if page := decodeJSON[substrate.ChangePage](t, rec); len(page.Changes) != 2 || page.Cursor != 0 {
		t.Fatalf("the task reader's page = %+v", page)
	}
}

// An app reads the row of a package its scopes reach into, which says the
// version the repository holds, and no other package's row.
func TestAnAppReadsTheRowOfAPackageItsScopesName(t *testing.T) {
	e := newAppEnv(t)
	ds := e.svc.datasets[fakeRepository]
	ds.types = append(ds.types, substrate.KindInfo{Identity: kindPackageRow, Name: "package", Authority: "substrate.reamde.dev", Package: "core"})
	for _, id := range []string{"substrate.reamde.dev/training", "substrate.reamde.dev/clinical"} {
		ds.records[id] = &substrate.Record{ID: id, Kind: kindPackageRow, Properties: map[string]any{"shippedVersion": float64(5)}}
	}
	scope := "write:substrate.reamde.dev/training/*"
	tok := e.grant(scope, []string{scope})
	rec := e.do(http.MethodGet, "/api/v1/substrate.reamde.dev/core/package/substrate.reamde.dev%2Ftraining", tok, nil, "")
	wantStatus(t, rec, http.StatusOK)
	if got := decodeJSON[substrate.Record](t, rec); got.Properties["shippedVersion"] != float64(5) {
		t.Fatalf("the package row answered %+v", got.Properties)
	}
	wantStatus(t, e.do(http.MethodGet, "/api/v1/substrate.reamde.dev/core/package/substrate.reamde.dev%2Fclinical", tok, nil, ""), http.StatusNotFound)
	wantStatus(t, e.do(http.MethodGet, "/api/v1/substrate.reamde.dev/core/kind/substrate.reamde.dev%2Ftraining%2Fset", tok, nil, ""), http.StatusNotFound)
}

// An app reads the declaration row of a kind its grant reads, and no other
// kind's.
func TestAnAppReadsTheDeclarationOfAKindItReads(t *testing.T) {
	e := newAppEnv(t)
	ds := e.svc.datasets[fakeRepository]
	ds.types = append(ds.types, substrate.KindInfo{Identity: kindDeclarationRow, Name: "kind", Authority: "substrate.reamde.dev", Package: "core"})
	for _, id := range []string{taskKind, personKind} {
		ds.records[id] = &substrate.Record{ID: id, Kind: kindDeclarationRow, Properties: map[string]any{"label": map[string]any{"singular": id}}}
	}
	scope := "read:" + taskKind
	tok := e.grant(scope, []string{scope})
	rec := e.do(http.MethodGet, "/api/v1/substrate.reamde.dev/core/kind/"+url.PathEscape(taskKind), tok, nil, "")
	wantStatus(t, rec, http.StatusOK)
	if got := decodeJSON[substrate.Record](t, rec); got.ID != taskKind {
		t.Fatalf("the declaration answered %+v", got)
	}
	wantStatus(t, e.do(http.MethodGet, "/api/v1/substrate.reamde.dev/core/kind/"+url.PathEscape(personKind), tok, nil, ""), http.StatusNotFound)
}
