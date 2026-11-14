package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/authorize"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// appActor is the actor the Notes app's token writes under, derived from its
// client id's host.
const appActor = "app:notes.example.org"

// appScopedToken mints an app's token on the engine: the Notes app's client
// id, so its writes are `app:notes.example.org`, holding scopes. No scopes
// is a full-access approval, a token without scopes.
func appScopedToken(t *testing.T, env *dbEnv, scopes ...string) string {
	t.Helper()
	return env.mint(t, substrate.MintTokenInput{Label: "Notes", Scopes: scopes, ClientID: appClientID, ClientName: "Notes"})
}

// appScopedDo sends a raw body to the handler as an app does, to the host
// the app's documents name.
func appScopedDo(t *testing.T, env *dbEnv, method, target, bearer string, body []byte, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	req.Host = "substrate.example.com"
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	env.h.ServeHTTP(rec, req)
	return rec
}

// appScopedRefPath is the record path a stored reference value names.
func appScopedRefPath(v any) string {
	if m, ok := v.(map[string]any); ok {
		s, _ := m[vocabulary.ReferenceValueKey].(string)
		return s
	}
	s, _ := v.(string)
	return s
}

// withAppFlow serves the Notes app's client document from memory, since the
// real fetcher never dials a loopback address, and paces no auth request, so
// a test's many tokens and refusals never wait on the limiter.
func withAppFlow() dbOption {
	return withConfig(func(c *Config) {
		c.ClientDocuments = &memDocs{docs: map[string]authorize.Document{
			appClientID: {ClientID: appClientID, RedirectURIs: []string{appRedirect}, Name: "Notes", URI: "https://notes.example.org"},
		}}
		c.AuthInterval = time.Nanosecond
	})
}

// appScopedPark sends a request to the authorization endpoint and answers
// the parked request's id.
func appScopedPark(t *testing.T, env *dbEnv, params map[string]string) string {
	t.Helper()
	rec := appScopedDo(t, env, http.MethodGet, authorizeURL(params), "", nil, "")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("authorize = %d %s, want 303", rec.Code, rec.Body.String())
	}
	id, ok := strings.CutPrefix(rec.Header().Get("Location"), "/authorize/")
	if !ok || id == "" {
		t.Fatalf("authorize redirected to %q, want /authorize/{request}", rec.Header().Get("Location"))
	}
	return id
}

// appScopedShow loads a parked request as the approval page does, which an
// approval is later held to.
func appScopedShow(t *testing.T, env *dbEnv, id string) *httptest.ResponseRecorder {
	t.Helper()
	return appScopedDo(t, env, http.MethodGet, "/api/v1/authorizations/"+id, env.token, nil, "")
}

// appScopedApprove answers a request the page has shown, as the owner.
func appScopedApprove(t *testing.T, env *dbEnv, id string, scopes []string, days int) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(substrate.AuthorizationApproval{Scopes: scopes, LifetimeDays: &days})
	return appScopedDo(t, env, http.MethodPost, "/api/v1/authorizations/"+id, env.token, body, "application/json")
}

// appScopedExchange trades an authorization code at the token endpoint.
func appScopedExchange(t *testing.T, env *dbEnv, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	return appScopedDo(t, env, http.MethodPost, "/oauth/token", "", []byte(form.Encode()), "application/x-www-form-urlencoded")
}

// appScopedActors lists the actors of every changelog entry that addressed
// one record, oldest first.
func appScopedActors(t *testing.T, env *dbEnv, kind, id string) []substrate.Actor {
	t.Helper()
	changes, err := env.ds.Changes(context.Background(), 0, substrate.ChangeFilter{Kinds: []string{kind}, RecordID: id}, 10000)
	if err != nil {
		t.Fatalf("read the changelog: %v", err)
	}
	out := make([]substrate.Actor, 0, len(changes))
	for _, c := range changes {
		// The hand the linked actor record is: the server derived it in this
		// process (decision 0270).
		key, _ := vocabulary.ActorKeyOf(c.Actor)
		out = append(out, key)
	}
	return out
}

// appScopedLastActor is the actor of the newest entry that addressed one
// record.
func appScopedLastActor(t *testing.T, env *dbEnv, kind, id string) substrate.Actor {
	t.Helper()
	actors := appScopedActors(t, env, kind, id)
	if len(actors) == 0 {
		t.Fatalf("no entry addressed %s/%s", kind, id)
	}
	return actors[len(actors)-1]
}

// appScopedCount is how many live records of kind the repository holds.
func appScopedCount(t *testing.T, env *dbEnv, kind string) int {
	t.Helper()
	page, err := env.ds.List(context.Background(), substrate.Query{Filter: substrate.Filter{Kinds: []string{kind}}, First: 1000})
	if err != nil {
		t.Fatalf("list %s: %v", kind, err)
	}
	return len(page.Records)
}

// The wider scoped surface (decision 0219): the window and aggregate reads,
// the semantic search, a blob's bytes, the function call and the feed's
// history, each held to what the token reads.

// appScopedWindowPath is a window read over every temporal kind, from to to.
func appScopedWindowPath(from, to string) string {
	filter := map[string]any{
		"implements": "substrate.reamde.dev/core/temporal",
		"properties": map[string]any{"at": map[string]any{"gte": from, "lt": to}},
	}
	raw, _ := json.Marshal(filter)
	return "/api/v1/records?filter=" + url.QueryEscape(string(raw))
}

// appScopedWindow declares, under the repository's own authority, a dose kind
// that is temporal, recurring and holds its own overrides, and a meeting kind
// that is temporal alone, and writes a daily dose from July 1st with the 3rd
// exdated and the 4th moved to the evening, and a meeting on the 2nd. It
// answers the two kinds.
func appScopedWindow(t *testing.T, env *dbEnv) (dose, meeting string) {
	t.Helper()
	pkg := env.repository + "/timeline"
	dose, meeting = pkg+"/dose", pkg+"/meeting"
	appAgentsApply(t, env,
		vocabulary.PackageManifest(pkg, 1),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "dose"}, map[string]any{
			"traits": []any{"substrate.reamde.dev/core/temporal(point)", "substrate.reamde.dev/core/recurring", "substrate.reamde.dev/core/override"},
			"properties": map[string]any{
				"name":         map[string]any{"type": "string"},
				"recurrence":   map[string]any{"type": "recurrence"},
				"rdates":       map[string]any{"type": "datetime", "repeated": true},
				"exdates":      map[string]any{"type": "datetime", "repeated": true},
				"timezone":     map[string]any{"type": "timezone"},
				"recurrenceOf": map[string]any{"type": "reference", "kind": dose},
				"originalAt":   map[string]any{"type": "datetime"},
			},
		}),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "meeting"}, map[string]any{
			"traits":     []any{"substrate.reamde.dev/core/temporal(range)"},
			"properties": map[string]any{"name": map[string]any{"type": "string"}},
		}),
	)
	env.put(t, dose, "levo", map[string]any{
		"name": "Levothyroxine", "recurrence": "RRULE:FREQ=DAILY", "timezone": "Europe/Athens",
		"at": "2026-07-01T06:00:00Z", "exdates": []any{"2026-07-03T06:00:00Z"},
	})
	env.put(t, dose, "levo_20260704T060000Z", map[string]any{
		"name": "Levothyroxine", "at": "2026-07-04T18:00:00Z",
		"recurrenceOf": dose + "/levo", "originalAt": "2026-07-04T06:00:00Z",
	})
	env.put(t, meeting, "dentist", map[string]any{"name": "Dentist", "at": "2026-07-02T12:00:00Z", "endsAt": "2026-07-02T13:00:00Z"})
	return dose, meeting
}

// appScopedWindowPage is a window read's page as the test reads it.
type appScopedWindowPage struct {
	Records []struct {
		ID       string `json:"id"`
		Kind     string `json:"kind"`
		Computed bool   `json:"computed"`
	} `json:"records"`
	Head int64 `json:"head"`
}

func TestAnAppsWindowReadIsHeldToTheKindsItReads(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	dose, meeting := appScopedWindow(t, env)
	from, to := "2026-07-01T00:00:00Z", "2026-07-06T00:00:00Z"

	meetings := appScopedToken(t, env, "read:"+meeting)
	rec := appScopedDo(t, env, http.MethodGet, appScopedWindowPath(from, to), meetings, nil, "")
	wantStatus(t, rec, http.StatusOK)
	page := decodeJSON[appScopedWindowPage](t, rec)
	if len(page.Records) != 1 || page.Records[0].ID != "dentist" {
		t.Fatalf("a window over every readable kind answered %+v, want the meeting alone", page.Records)
	}
	if page.Head != 0 {
		t.Fatalf("an app's window page carries head %d", page.Head)
	}
	// A kind the token cannot read is unknown, named alone or beside one it
	// reads.
	for _, kinds := range [][]string{{dose}, {dose, meeting}} {
		filter := map[string]any{"kinds": kinds, "properties": map[string]any{"at": map[string]any{"gte": from, "lt": to}}}
		raw, _ := json.Marshal(filter)
		wantStatus(t, appScopedDo(t, env, http.MethodGet, "/api/v1/records?filter="+url.QueryEscape(string(raw)), meetings, nil, ""), http.StatusNotFound)
	}

	// Reading both kinds, the series expands and its override claims its slot.
	both := appScopedToken(t, env, "read:"+env.repository+"/timeline/*")
	rec = appScopedDo(t, env, http.MethodGet, appScopedWindowPath(from, to), both, nil, "")
	wantStatus(t, rec, http.StatusOK)
	page = decodeJSON[appScopedWindowPage](t, rec)
	computed, override, meetingSeen := 0, false, false
	for _, r := range page.Records {
		switch {
		case r.Computed:
			computed++
		case r.ID == "levo_20260704T060000Z":
			override = true
		case r.ID == "dentist":
			meetingSeen = true
		}
	}
	if computed == 0 || !override || !meetingSeen {
		t.Fatalf("a window over both kinds answered %+v, want computed doses, the override and the meeting", page.Records)
	}
}

func TestAnAppsAggregateReadIsCutToTheKindsItReads(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"), withEngine(engine.WithUnitSeed(true)))
	observation := vocabulary.PackageObservations + "/observation"
	trace := vocabulary.PackageObservations + "/trace"
	source := vocabulary.PackageObservations + "/source"
	const metricID = "vitals/heart_rate"
	metric := kindMetric + "/" + metricID
	env.put(t, kindMetric, metricID, map[string]any{
		"name": "Heart rate", "unit": map[string]any{"ref": "substrate.reamde.dev/core/unit/1/min"}, "form": "gauge", "reducers": []any{"avg"},
	})
	env.put(t, source, "s1", map[string]any{"name": "Watch", "class": "watch"})
	ref := func(kind, id string) map[string]any {
		return map[string]any{vocabulary.ReferenceValueKey: kind + "/" + id}
	}
	// One reading the app's kinds hold, and a trace of the same metric at
	// another value, in an observed kind the app does not read.
	env.put(t, observation, "o1", map[string]any{
		"metric": ref(kindMetric, metricID), "source": ref(source, "s1"), "form": "gauge",
		"value": map[string]any{"value": "60", "unit": "/min"}, "at": "2026-04-07T08:00:00Z", "hold": "PT1M", "utcOffset": 0,
	})
	env.put(t, trace, "tr1", map[string]any{
		"metric": ref(kindMetric, metricID), "source": ref(source, "s1"), "form": "gauge", "unit": "/min",
		"at": "2026-04-08T08:00:00Z", "endsAt": "2026-04-08T09:00:00Z", "offsets": []any{0}, "values": []any{"200"},
		"hold": "PT1M", "utcOffset": 0, "revision": 1,
	})
	body := `{"metric":{"ref":"` + metric + `"},"range":{"gte":"2026-04-06T00:00:00Z","lt":"2026-10-05T00:00:00Z"},"bucket":{"calendar":"week"},"groupBy":["source"],"reducers":["avg"]}`
	aggregate := func(filter string) string {
		path := recordsPath + "?aggregate=" + url.QueryEscape(body)
		if filter != "" {
			path += "&filter=" + url.QueryEscape(filter)
		}
		return path
	}
	avg := func(res substrate.AggregateResult) []string {
		var out []string
		for _, row := range res.Rows {
			for _, v := range row.Values {
				if v.Reducer.Name == substrate.ReducerAvg {
					raw, _ := json.Marshal(v.Value)
					out = append(out, strings.Trim(string(raw), `"`))
				}
			}
		}
		return out
	}

	// A request naming no kind is cut to the readable kinds the observed
	// index holds, never widened to the person kind or the metric kind: the
	// engine refuses a metric read over a kind the index does not hold, and
	// the trace the app does not read stays out of the answer.
	app := appScopedToken(t, env, "read:"+observation, "read:"+kindMetric, "read:"+personKind)
	rec := appScopedDo(t, env, http.MethodGet, aggregate(""), app, nil, "")
	wantStatus(t, rec, http.StatusOK)
	res := decodeJSON[substrate.AggregateResult](t, rec)
	if got := avg(res); strings.Join(got, ",") != "60" {
		t.Fatalf("the aggregate averaged %v, want the observation's 60 alone", got)
	}
	if res.Head.Changelog != 0 || res.Head.Generation != "" {
		t.Fatalf("an app's aggregate carries head %+v", res.Head)
	}
	// The sources an answer names are cut to the kinds the token reads, on
	// the answer and on each row, and a group's source with them.
	if len(res.Sources) != 0 || len(res.Rows[0].Sources) != 0 {
		t.Fatalf("sources = %v and %v, want none the token reads", res.Sources, res.Rows[0].Sources)
	}
	if res.Rows[0].Group == nil || res.Rows[0].Group.Source != "" {
		t.Fatalf("the row's group = %+v, want its source left out", res.Rows[0].Group)
	}
	// The owner's read of the same answer names the source the cut took.
	owner := decodeJSON[substrate.AggregateResult](t, appScopedDo(t, env, http.MethodGet, aggregate(`{"kinds":["`+observation+`"]}`), env.token, nil, ""))
	if strings.Join(owner.Sources, ",") != source+"/s1" || owner.Rows[0].Group == nil || owner.Rows[0].Group.Source != source+"/s1" {
		t.Fatalf("the owner's answer = %+v, want the source named", owner)
	}
	// A token that reads the source kind keeps it.
	sources := appScopedToken(t, env, "read:"+observation, "read:"+kindMetric, "read:"+source)
	if res := decodeJSON[substrate.AggregateResult](t, appScopedDo(t, env, http.MethodGet, aggregate(""), sources, nil, "")); strings.Join(res.Sources, ",") != source+"/s1" ||
		strings.Join(res.Rows[0].Sources, ",") != source+"/s1" || res.Rows[0].Group.Source != source+"/s1" {
		t.Fatalf("a token reading the sources got %+v", res)
	}
	// A kind the token cannot read is unknown, and so is a metric whose kind
	// it cannot read.
	wantStatus(t, appScopedDo(t, env, http.MethodGet, aggregate(`{"kinds":["`+trace+`"]}`), app, nil, ""), http.StatusNotFound)
	noMetric := appScopedToken(t, env, "read:"+observation)
	wantStatus(t, appScopedDo(t, env, http.MethodGet, aggregate(""), noMetric, nil, ""), http.StatusNotFound)
	// A token reading no kind the index holds is answered an empty result.
	people := appScopedToken(t, env, "read:"+personKind, "read:"+kindMetric)
	rec = appScopedDo(t, env, http.MethodGet, aggregate(""), people, nil, "")
	wantStatus(t, rec, http.StatusOK)
	if res := decodeJSON[substrate.AggregateResult](t, rec); len(res.Rows) != 0 {
		t.Fatalf("an aggregate over nothing readable answered %+v", res)
	}
	// A property read over named kinds the token reads passes as a list does.
	pkg := env.repository + "/vitals"
	score := pkg + "/score"
	appAgentsApply(t, env,
		vocabulary.PackageManifest(pkg, 1),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "score"}, map[string]any{
			"traits":     []any{"substrate.reamde.dev/core/temporal(point)"},
			"properties": map[string]any{"points": map[string]any{"type": "int"}},
		}),
	)
	env.put(t, score, "sc1", map[string]any{"points": 7, "at": "2026-01-07T08:00:00Z"})
	scores := appScopedToken(t, env, "read:"+score, "read:"+kindMetric)
	property := recordsPath + "?aggregate=" + url.QueryEscape(`{"property":{"name":"points"},"range":{"gte":"2026-01-01T00:00:00Z","lt":"2026-02-01T00:00:00Z"},"bucket":{"calendar":"week","timezone":"UTC"},"reducers":["avg"]}`) +
		"&filter=" + url.QueryEscape(`{"kinds":["`+score+`"]}`)
	rec = appScopedDo(t, env, http.MethodGet, property, scores, nil, "")
	wantStatus(t, rec, http.StatusOK)
	if got := avg(decodeJSON[substrate.AggregateResult](t, rec)); strings.Join(got, ",") != "7" {
		t.Fatalf("the property read averaged %v, want the score's 7", got)
	}
}

// appScopedCallEnv is the engine whose repository declares four functions in
// its own tasks package and one agent: summarize reads and writes tasks (and
// reaches the network, so a call of it is audited with its caller), report
// reads tasks and people, chain calls summarize, and runner runs the
// assistant, which reads people. Core's query built-in is there as shipped.
type appScopedCallEnv struct {
	*dbEnv
	summarize, report, chain, runner, talked string
	// calls counts the calls the engine was handed, and scoped is whether the
	// last one still carried the app's scope mark.
	mu     sync.Mutex
	calls  int
	scoped bool
}

func newAppScopedCallEnv(t *testing.T, opts ...dbOption) *appScopedCallEnv {
	t.Helper()
	env := newDBEnv(t, append([]dbOption{withSamples("tasks"), withAppFlow()}, opts...)...)
	// Declared under the repository's full name, answered under `~`
	// (decision record 0259).
	pkg := env.repository + "/tasks"
	e := &appScopedCallEnv{
		dbEnv: env, summarize: "~/tasks/summarize", report: "~/tasks/report", chain: "~/tasks/chain", runner: "~/tasks/runner",
		talked: "~/chat/assistant",
	}
	fn := func(name, description string, perms map[string]any, source string) map[string]any {
		return vocabulary.FunctionManifest(pkg, name, map[string]any{
			"description": description, "runtime": vocabulary.RuntimePython, "permissions": perms, "source": source,
		})
	}
	const noop = `
def main(input, host):
    return {"output": {}}
`
	appAgentsApply(t, env,
		vocabulary.PackageManifest(pkg, 1),
		fn("summarize", "Summarizes the open tasks", map[string]any{
			"reads": map[string]any{"kinds": []any{taskKind}}, "writes": []any{taskKind}, "network": []any{"example.com"},
		}, `
def main(input, host):
    return {"effects": [{"action": "put", "kind": "`+taskKind+`", "id": host.ids.minted("summary", "1"), "properties": {"name": "summary"}}],
            "output": {"ran": "summarize"}}
`),
		fn("report", "Reports on tasks by person", map[string]any{
			"reads": map[string]any{"kinds": []any{taskKind, personKind}},
		}, noop),
		fn("chain", "Runs the summary", map[string]any{"call": []any{e.summarize}}, noop),
		fn("runner", "Asks the assistant", map[string]any{"agents": []any{e.talked}}, noop),
		vocabulary.PackageManifest(env.repository+"/chat", 1),
		vocabulary.AgentManifest(env.repository+"/chat", "assistant", map[string]any{
			"description": "assistant under test", "prompt": "You are assistant.",
			"provider": appAgentsLLMProvider, "model": "assistant",
			"permissions": map[string]any{"reads": map[string]any{"kinds": []any{personKind}}},
		}),
	)
	env.wrap = func(ds substrate.Dataset) substrate.Dataset { return appScopedCalls{Dataset: ds, e: e} }
	return e
}

// appScopedCalls counts each function call the engine was handed and reads
// whether its context still carried the app's scope mark: the handler drops
// the mark before the call runs under the function's own permissions
// (decision 0219), and nothing the call writes shows whether it did, so the
// hand-off itself is read here, then passed on.
type appScopedCalls struct {
	substrate.Dataset
	e *appScopedCallEnv
}

func (d appScopedCalls) CallFunction(ctx context.Context, actor substrate.Actor, name string, args any) (any, int, error) {
	d.e.mu.Lock()
	d.e.calls++
	_, d.e.scoped = substrate.ScopesFrom(ctx)
	d.e.mu.Unlock()
	return d.Dataset.CallFunction(ctx, actor, name, args)
}

func (e *appScopedCallEnv) handed() (int, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls, e.scoped
}

func callPath(name string) string {
	return "/api/v1/" + corePackage + "/function/" + url.PathEscape(name) + "/call"
}

func TestAnAppCallsTheFunctionsItsScopeNamesWhereTheirReachIsCovered(t *testing.T) {
	t.Parallel()
	e := newAppScopedCallEnv(t)
	body := []byte(`{"input":{"n":1}}`)
	call := func(bearer, name string) *httptest.ResponseRecorder {
		return appScopedDo(t, e.dbEnv, http.MethodPost, callPath(name), bearer, body, "application/json")
	}

	app := appScopedToken(t, e.dbEnv, "call:"+e.repository+"/tasks/*", "write:"+taskKind)
	got := call(app, e.summarize)
	wantStatus(t, got, http.StatusOK)
	if out := decodeJSON[substrate.FunctionCalled](t, got); out.Effects != 1 {
		t.Fatalf("the call answered %+v", out)
	}
	// The call reached the engine as the app, unscoped: its audit names the
	// app as the caller.
	if n, scoped := e.handed(); n != 1 || scoped {
		t.Fatalf("the call reached the engine %d times, scoped %v; want once, unscoped", n, scoped)
	}
	runs, err := e.ds.List(context.Background(), substrate.Query{Filter: substrate.Filter{Kinds: []string{corePackage + "/triggerrun"}}})
	if err != nil || len(runs.Records) != 1 || !linksActorRecord(runs.Records[0].Properties["caller"], appActor) || runs.Records[0].Properties["callable"] != e.summarize {
		t.Fatalf("the call's audit = %+v, %v; want one run of %s by the app", runs, err, e.summarize)
	}
	// A function that calls a covered function is covered through it.
	wantStatus(t, call(app, e.chain), http.StatusOK)
	// One whose reach the scopes do not cover is refused, naming what is
	// missing; so is one whose agent reaches further.
	for _, name := range []string{e.report, e.runner} {
		refused := call(app, name)
		wantStatus(t, refused, http.StatusForbidden)
		if !strings.Contains(refused.Body.String(), "read "+personKind) {
			t.Fatalf("%s refused with %s, want the uncovered read named", name, refused.Body.String())
		}
	}
	// A core function is never matched, and a function the pattern does not
	// name or the repository does not hold is unknown.
	for _, name := range []string{corePackage + "/query", "other.example.com/tasks/summarize", e.repository + "/tasks/nothing"} {
		wantStatus(t, call(app, name), http.StatusNotFound)
	}
	// A function is named in full.
	wantStatus(t, call(app, "summarize"), http.StatusUnprocessableEntity)
	if n, _ := e.handed(); n != 2 {
		t.Fatalf("refused calls reached the engine: %d calls", n)
	}

	// A read scope covers the function's read and not its write.
	reader := appScopedToken(t, e.dbEnv, "call:"+e.repository+"/tasks/*", "read:"+taskKind)
	refused := call(reader, e.summarize)
	wantStatus(t, refused, http.StatusForbidden)
	if !strings.Contains(refused.Body.String(), "write "+taskKind) {
		t.Fatalf("refused with %s, want the uncovered write named", refused.Body.String())
	}
	// A token without a call scope reaches no function.
	none := call(appScopedToken(t, e.dbEnv, "write:"+taskKind), e.summarize)
	wantStatus(t, none, http.StatusForbidden)
	if wa := none.Header().Get("WWW-Authenticate"); !strings.Contains(wa, `error="insufficient_scope"`) {
		t.Fatalf("WWW-Authenticate = %q", wa)
	}
	if n, _ := e.handed(); n != 2 {
		t.Fatalf("refused calls reached the engine: %d calls", n)
	}
}

func TestTheApprovalPageShowsTheFunctionsACallScopeMatchesWithTheirReach(t *testing.T) {
	t.Parallel()
	e := newAppScopedCallEnv(t)
	id := appScopedPark(t, e.dbEnv, appRequest("call:~/tasks/* read:"+taskKind))
	view := appScopedShow(t, e.dbEnv, id)
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
	if strings.Join(ids, ",") != strings.Join([]string{e.chain, e.report, e.runner, e.summarize}, ",") {
		t.Fatalf("the call scope matches %v, want the four functions of the package and never core's", ids)
	}
	byID := map[string]substrate.AuthorizationCallFunction{}
	for _, f := range fns {
		byID[f.Identity] = f
	}
	summarize := byID[e.summarize]
	if summarize.Description != "Summarizes the open tasks" || strings.Join(summarize.Reads, ",") != taskKind || strings.Join(summarize.Writes, ",") != taskKind ||
		summarize.Covered || strings.Join(summarize.Uncovered, ",") != "write "+taskKind {
		t.Fatalf("summarize = %+v", summarize)
	}
	if report := byID[e.report]; strings.Join(report.Reads, ",") != personKind+","+taskKind || report.Covered || strings.Join(report.Uncovered, ",") != "read "+personKind {
		t.Fatalf("report = %+v", report)
	}
	if chain := byID[e.chain]; strings.Join(chain.Writes, ",") != taskKind || chain.Covered {
		t.Fatalf("chain carries the reach of what it calls: %+v", chain)
	}
	if runner := byID[e.runner]; strings.Join(runner.Reads, ",") != personKind || runner.Covered {
		t.Fatalf("runner carries the reach of the agent it runs: %+v", runner)
	}
	if len(got.Scopes[1].Functions) != 0 || len(got.Scopes[1].Kinds) != 1 {
		t.Fatalf("the read scope = %+v", got.Scopes[1])
	}
	// A request whose scopes cover the reach shows the function covered.
	id = appScopedPark(t, e.dbEnv, appRequest("call:~/tasks/summarize write:"+taskKind))
	got = decodeJSON[substrate.AuthorizationRequest](t, appScopedShow(t, e.dbEnv, id))
	if fns := got.Scopes[0].Functions; len(fns) != 1 || !fns[0].Covered || fns[0].Uncovered != nil || got.Scopes[0].Glob {
		t.Fatalf("an exact call scope = %+v", got.Scopes[0])
	}
}

// An app reads the row of a package its scopes reach into, which says the
// version the repository holds, and no other package's row.
func TestAnAppReadsTheRowOfAPackageItsScopesName(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	training, clinical := env.repository+"/training", env.repository+"/clinical"
	appAgentsApply(t, env,
		vocabulary.PackageManifest(training, 5),
		vocabulary.KindManifest(training, map[string]any{"singular": "plan"}, map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
		vocabulary.PackageManifest(clinical, 5),
		vocabulary.KindManifest(clinical, map[string]any{"singular": "visit"}, map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
	)
	tok := appScopedToken(t, env, "write:"+training+"/*")
	rec := appScopedDo(t, env, http.MethodGet, "/api/v1/"+kindPackageRow+"/"+url.PathEscape(training), tok, nil, "")
	wantStatus(t, rec, http.StatusOK)
	if got := decodeJSON[substrate.Record](t, rec); got.ID != "~/training" || got.Properties["version"] != float64(5) {
		t.Fatalf("the package row answered %s %+v", got.ID, got.Properties)
	}
	wantStatus(t, appScopedDo(t, env, http.MethodGet, "/api/v1/"+kindPackageRow+"/"+url.PathEscape(clinical), tok, nil, ""), http.StatusNotFound)
	wantStatus(t, appScopedDo(t, env, http.MethodGet, "/api/v1/"+kindDeclarationRow+"/"+url.PathEscape(training+"/set"), tok, nil, ""), http.StatusNotFound)
}

// An app reads the declaration row of a kind its grant reads, and no other
// kind's.
func TestAnAppReadsTheDeclarationOfAKindItReads(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("tasks"))
	tok := appScopedToken(t, env, "read:"+taskKind)
	rec := appScopedDo(t, env, http.MethodGet, "/api/v1/"+kindDeclarationRow+"/"+url.PathEscape(taskKind), tok, nil, "")
	wantStatus(t, rec, http.StatusOK)
	if got := decodeJSON[substrate.Record](t, rec); got.ID != taskKind {
		t.Fatalf("the declaration answered %+v", got)
	}
	wantStatus(t, appScopedDo(t, env, http.MethodGet, "/api/v1/"+kindDeclarationRow+"/"+url.PathEscape(personKind), tok, nil, ""), http.StatusNotFound)
}

func TestAnAppReadsTheFeedsHistoryCutToWhatItReads(t *testing.T) {
	t.Parallel()
	e := newAppAgentsEnv(t, false)
	// A tag is a kind the token does not read, and an enabled trigger
	// watches messages, so the owner's rows carry its stance.
	feed := e.repository + "/feed"
	tag := feed + "/tag"
	appAgentsApply(t, e.dbEnv,
		vocabulary.PackageManifest(feed, 1),
		vocabulary.KindManifest(feed, map[string]any{"singular": "tag"}, map[string]any{"properties": map[string]any{
			"label": map[string]any{"type": "string"},
		}}),
		vocabulary.FunctionManifest(feed, "noop", map[string]any{
			"description": "does nothing", "runtime": vocabulary.RuntimePython,
			"source": "\ndef main(input, host):\n    return {\"output\": {}}\n",
		}),
	)
	e.put(t, kindTrigger, "messages", map[string]any{
		"label": "messages", "enabled": true,
		"source":   map[string]any{"record": map[string]any{"kinds": []any{kindMessage}}},
		"callable": map[string]any{vocabulary.ReferenceValueKey: vocabulary.RecordPath(kindFunction, feed+"/noop")},
	})
	e.wrap = func(ds substrate.Dataset) substrate.Dataset {
		return appAgentsWideFeed{Dataset: ds, kind: personKind, id: "p1", also: substrate.AffectedRecord{Kind: tag, ID: "t1", Version: 3}}
	}
	app := appScopedToken(t, e.dbEnv, "read:"+personKind, "agents:"+e.repository+"/chat/*")
	patch := func(kind, id string, props map[string]any) {
		if _, err := e.ds.Patch(context.Background(), substrate.ActorSystem, kind, id, substrate.PatchInput{Properties: props}); err != nil {
			t.Fatalf("patch %s/%s: %v", kind, id, err)
		}
	}
	e.put(t, tag, "t1", map[string]any{"label": "secret"}) // hidden
	e.put(t, personKind, "p1", map[string]any{"name": "secret"})
	patch(kindMessage, "mB", map[string]any{"content": "secret"}) // another agent's
	patch(kindMessage, "mA", map[string]any{"content": "secret"})
	e.put(t, tag, "t2", map[string]any{"label": "secret"}) // hidden
	var p1Seq int64
	if changes, err := e.ds.Changes(context.Background(), 0, substrate.ChangeFilter{Kinds: []string{personKind}, RecordID: "p1"}, 10); err != nil || len(changes) != 1 {
		t.Fatalf("p1's entries = %v, %v", changes, err)
	} else {
		p1Seq = changes[0].Seq
	}

	// The history page, newest first: hidden rows are skipped and the page
	// stays full while more lies below, so the cursor is right.
	rec := e.read(t, "/api/v1/changes?before=0&first=2", app)
	wantStatus(t, rec, http.StatusOK)
	page := decodeJSON[substrate.ChangePage](t, rec)
	if len(page.Changes) != 2 || page.Changes[0].RecordID != "mA" || page.Changes[1].RecordID != "p1" || page.Cursor != p1Seq {
		t.Fatalf("page = %+v", page)
	}
	for _, row := range page.Changes {
		if row.Payload != nil || row.Hash != "" || len(row.Triggers) != 0 {
			t.Fatalf("a row carries more than names: %+v", row)
		}
		if len(row.Affected) != 1 || row.Affected[0].Kind == tag {
			t.Fatalf("affected = %+v, want the readable record alone", row.Affected)
		}
	}
	if !strings.Contains(rec.Body.String(), `"generation"`) || strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("body = %s", rec.Body.String())
	}
	// The owner's same rows carry the payload and the trigger's stance the
	// app's leave out.
	owned := decodeJSON[substrate.ChangePage](t, e.read(t, "/api/v1/changes?before=0&first=2", e.token))
	if len(owned.Changes) == 0 || owned.Changes[0].RecordID != "t2" {
		t.Fatalf("the owner's page = %+v", owned)
	}
	ownerMA := decodeJSON[substrate.ChangePage](t, e.read(t, "/api/v1/changes?before=0&first=1&kinds="+kindMessage, e.token))
	if len(ownerMA.Changes) != 1 || ownerMA.Changes[0].RecordID != "mA" || len(ownerMA.Changes[0].Triggers) == 0 || ownerMA.Changes[0].Payload == nil {
		t.Fatalf("the owner's message row = %+v, want its payload and the trigger's stance", ownerMA.Changes)
	}
	// Every page below holds rows the token reads, full while more lies
	// below, newest first, down to the bottom.
	gen := page.Generation
	seen := map[int64]bool{}
	last := p1Seq
	for cursor := page.Cursor; cursor != 0; {
		next := e.read(t, fmt.Sprintf("/api/v1/changes?before=%d&first=2&generation=%s", cursor, gen), app)
		wantStatus(t, next, http.StatusOK)
		p := decodeJSON[substrate.ChangePage](t, next)
		if p.Cursor != 0 && len(p.Changes) != 2 {
			t.Fatalf("a page above the bottom = %+v, want it full", p)
		}
		for _, row := range p.Changes {
			if row.Seq >= last || seen[row.Seq] || row.Kind == tag || row.RecordID == "mB" || row.RecordID == "tB" || row.RecordID == otherAgent {
				t.Fatalf("a lower page carried %+v below seq %d", row, last)
			}
			seen[row.Seq], last = true, row.Seq
		}
		cursor = p.Cursor
	}
	// Run summaries read the same held rows.
	runs := e.read(t, "/api/v1/changes?runs=1", app)
	wantStatus(t, runs, http.StatusOK)
	if got := decodeJSON[substrate.ChangeRunPage](t, runs); len(got.Runs) < 2 || got.Runs[0].Kind != kindMessage || got.Runs[1].Kind != personKind || got.Runs[1].Count != 1 {
		t.Fatalf("runs = %+v", got.Runs)
	}
	// The forward read from a cursor, held the same way.
	forward := e.read(t, "/api/v1/changes?from=0", app)
	wantStatus(t, forward, http.StatusOK)
	lines := strings.Split(strings.TrimSpace(forward.Body.String()), "\n")
	if len(lines) < 3 || !strings.Contains(lines[0], `"bookmark"`) || !strings.Contains(lines[len(lines)-2], `"p1"`) || !strings.Contains(lines[len(lines)-1], `"mA"`) {
		t.Fatalf("forward read = %q", lines)
	}
	if strings.Contains(forward.Body.String(), tag) || strings.Contains(forward.Body.String(), `"mB"`) || strings.Contains(forward.Body.String(), "secret") {
		t.Fatalf("the forward read carried a row the token does not read: %s", forward.Body.String())
	}
	// A kind it cannot read is unknown on the history too.
	wantStatus(t, e.read(t, "/api/v1/changes?before=0&kinds="+tag, app), http.StatusNotFound)
	// A token reading nothing else the feed holds is answered that alone.
	other := appScopedToken(t, e.dbEnv, "read:"+tag)
	rec = e.read(t, "/api/v1/changes?before=0&first=5&kinds="+tag, other)
	wantStatus(t, rec, http.StatusOK)
	if page := decodeJSON[substrate.ChangePage](t, rec); len(page.Changes) != 2 || page.Cursor != 0 {
		t.Fatalf("the tag reader's page = %+v", page)
	}
}

func TestAnAppReadsABlobARecordItReadsNamesAndNoOther(t *testing.T) {
	t.Parallel()
	e := newAppDeliveryEnv(t, false)
	photo, scan, nobody := []byte("the cat"), []byte("the owner's scan"), []byte("stored and never named")
	for _, b := range []struct {
		data  []byte
		query string
	}{{photo, "?name=cat.jpg"}, {scan, "?name=passport.pdf"}, {nobody, ""}} {
		wantStatus(t, appScopedDo(t, e.dbEnv, http.MethodPut, "/api/v1/blobs"+b.query, e.token, b.data, "image/jpeg"), http.StatusCreated)
	}
	e.put(t, e.photo, "p1", map[string]any{"caption": "cat", "image": appDeliveryDigest(photo)})
	e.put(t, e.scan, "s1", map[string]any{"file": appDeliveryDigest(scan)})
	e.put(t, e.album, "a1", map[string]any{"images": []any{appDeliveryDigest(scan)}})
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

	app := appScopedToken(t, e.dbEnv, "read:"+e.photo)
	rec := get(app, appDeliveryDigest(photo), nil)
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
	wantStatus(t, get(app, appDeliveryDigest(photo), map[string]string{"If-None-Match": rec.Header().Get("ETag")}), http.StatusNotModified)
	// A blob no record the token reads names is as absent as one nobody
	// stored: the two answers are one answer.
	hidden := get(app, appDeliveryDigest(scan), nil)
	wantStatus(t, hidden, http.StatusNotFound)
	never := []byte("never stored")
	unknown := get(app, appDeliveryDigest(never), nil)
	wantStatus(t, unknown, http.StatusNotFound)
	normalize := func(rec *httptest.ResponseRecorder, digest string) string {
		return strings.ReplaceAll(rec.Body.String(), digest, "<digest>")
	}
	if normalize(hidden, appDeliveryDigest(scan)) != normalize(unknown, appDeliveryDigest(never)) {
		t.Fatalf("a hidden blob %s and an unknown one %s are told apart", hidden.Body.String(), unknown.Body.String())
	}
	wantStatus(t, get(app, appDeliveryDigest(nobody), nil), http.StatusNotFound)
	for _, data := range [][]byte{photo, scan, nobody} {
		if _, err := e.ds.StatBlob(context.Background(), appDeliveryDigest(data)); err != nil {
			t.Fatalf("the store lost an upload: %v", err)
		}
	}
	// A token reading the blob manifests reads the name and type too.
	manifests := appScopedToken(t, e.dbEnv, "read:"+e.photo, "read:"+kindBlob)
	rec = get(manifests, appDeliveryDigest(photo), nil)
	wantStatus(t, rec, http.StatusOK)
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "cat.jpg") || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("a token reading the manifests got %v", rec.Header())
	}
	// A repeated blob-ref names its blobs too, and an exact scope reaches an
	// internal kind's.
	wantStatus(t, get(appScopedToken(t, e.dbEnv, "read:"+e.album), appDeliveryDigest(scan), nil), http.StatusOK)
	wantStatus(t, get(appScopedToken(t, e.dbEnv, "read:"+e.scan), appDeliveryDigest(scan), nil), http.StatusOK)
	// A glob never reaches the internal kind, so its blob stays hidden under one.
	wantStatus(t, get(appScopedToken(t, e.dbEnv, "read:"+e.repository+"/vault/*"), appDeliveryDigest(scan), nil), http.StatusForbidden)
	// A token reading no kind with a blob-ref reaches no blob at all.
	none := get(appScopedToken(t, e.dbEnv, "read:"+taskKind), appDeliveryDigest(photo), nil)
	wantStatus(t, none, http.StatusForbidden)
	if wa := none.Header().Get("WWW-Authenticate"); !strings.Contains(wa, `error="insufficient_scope"`) {
		t.Fatalf("WWW-Authenticate = %q", wa)
	}
}
