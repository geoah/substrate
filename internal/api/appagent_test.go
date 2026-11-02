package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// An app's agent (decision 0206): an `agent` detail approves one agent, its
// exact reach and its cap; the approval rides the token, never its scopes;
// the app's declare apply admits that agent held to the approval, and reads
// its row.

var (
	aaTraining = fakeRepository + "/training"
	aaCoachPkg = fakeRepository + "/trainingcoach"
	aaAgent    = aaCoachPkg + "/workoutCoach"
	aaMemory   = aaTraining + "/memory"
	aaRoutine  = aaTraining + "/routine"
	aaSleep    = "substrate.reamde.dev/health/sleep"
)

const aaScopes = "declare:~/trainingcoach write:~/training/*"

func aaDetail(capCents int) string {
	raw, _ := json.Marshal([]map[string]any{{
		"type": "agent", "agent": "~/trainingcoach/workoutCoach",
		"reads":  []string{"~/training/routine", "~/training/memory", aaSleep},
		"writes": []string{"~/training/routine", "~/training/memory"}, "spendCentsPerDay": capCents,
	}})
	return string(raw)
}

func aaRequest(scopes, details string) map[string]string {
	r := appRequest(scopes)
	r["authorization_details"] = details
	return r
}

// coachToken is the app's token, approved with the coach at capCents, and
// the token response it came in.
func (e *appEnv) coachToken(capCents int) (string, tokenResponse) {
	e.t.Helper()
	id := e.park(aaRequest(aaScopes, aaDetail(capCents)))
	rec := e.approve(id, []string{"declare:" + aaCoachPkg, "write:" + aaTraining + "/*"}, 30)
	wantStatus(e.t, rec, http.StatusOK)
	tok := e.exchange(tokenForm(codeFrom(e.t, decodeJSON[substrate.AuthorizationAnswer](e.t, rec).Redirect)))
	wantStatus(e.t, tok, http.StatusOK)
	res := decodeJSON[tokenResponse](e.t, tok)
	return res.AccessToken, res
}

func TestAnAgentDetailComesWithItsDeclareScope(t *testing.T) {
	e := newSetupEnv(t).appEnv
	rec := e.do(http.MethodGet, authorizeURL(aaRequest("write:~/training/*", aaDetail(100))), "", nil, "")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "declare:~/trainingcoach") {
		t.Fatalf("a request without the agent's declare scope: %d %s", rec.Code, rec.Body.String())
	}
	// Unchecking it approves the rest and leaves the agent off the token:
	// the app could never declare it (decisions 0205 and 0206).
	id := e.park(aaRequest(aaScopes, aaDetail(100)))
	rec = e.approve(id, []string{"write:" + aaTraining + "/*"}, 30)
	wantStatus(t, rec, http.StatusOK)
	tok := e.exchange(tokenForm(codeFrom(t, decodeJSON[substrate.AuthorizationAnswer](t, rec).Redirect)))
	wantStatus(t, tok, http.StatusOK)
	res := decodeJSON[tokenResponse](t, tok)
	e.svc.mu.Lock()
	info := e.svc.tokens[res.AccessToken].info
	e.svc.mu.Unlock()
	if len(info.Agents) != 0 || len(res.AuthorizationDetails) != 0 {
		t.Fatalf("an approval without the declare scope approved the agent: %+v, %+v", info.Agents, res.AuthorizationDetails)
	}
}

// A coach package the owner took over refuses the app's declare scope at
// approval (decision 0205); the agent row says approving leaves it out, and
// the rest of the request is approved.
func TestATakenOverCoachPackageLeavesTheAgentOutOfTheApproval(t *testing.T) {
	e := newSetupEnv(t).appEnv
	ds := e.svc.datasets[fakeRepository]
	ds.mu.Lock()
	ds.records[aaCoachPkg] = &substrate.Record{Kind: corePackage + "/package", ID: aaCoachPkg, Properties: map[string]any{
		substrate.PackageDeclaredBy: "console", substrate.PackageTakenOverFrom: appClientID,
	}}
	ds.mu.Unlock()
	id := e.park(aaRequest(aaScopes, aaDetail(100)))
	view := decodeJSON[substrate.AuthorizationRequest](t, e.show(id))
	if plan := view.Setup[0].Agent; plan == nil || !strings.Contains(plan.Withheld, aaCoachPkg+" is your package") {
		t.Fatalf("the agent row of a taken-over package = %+v", view.Setup[0].Agent)
	}
	rec := e.approveShown(id, []string{"write:" + aaTraining + "/*"}, 30)
	wantStatus(t, rec, http.StatusOK)
}

func TestApprovingAnAgentDetailPutsTheApprovalOnTheTokenNotItsScopes(t *testing.T) {
	e := newSetupEnv(t).appEnv
	id := e.park(aaRequest(aaScopes, aaDetail(100)))
	view := decodeJSON[substrate.AuthorizationRequest](t, e.show(id))
	if len(view.Setup) != 1 || view.Setup[0].Type != substrate.DetailAgent || view.Setup[0].Action != substrate.SetupNone {
		t.Fatalf("setup = %+v, want one agent row that sets nothing up", view.Setup)
	}
	plan := view.Setup[0].Agent
	if plan == nil || plan.Agent != aaAgent || plan.SpendCentsPerDay != 100 || len(plan.Reads) != 3 || plan.Reads[2].Identity != aaSleep ||
		plan.Reads[0].Identity != aaRoutine || len(plan.Writes) != 2 {
		t.Fatalf("the agent row = %+v", plan)
	}
	tok, res := e.coachToken(100)
	var info substrate.TokenInfo
	e.svc.mu.Lock()
	info = e.svc.tokens[tok].info
	e.svc.mu.Unlock()
	want := substrate.AgentApproval{Agent: aaAgent, Reads: []string{aaRoutine, aaMemory, aaSleep}, Writes: []string{aaRoutine, aaMemory}, SpendCentsPerDay: 100}
	if len(info.Agents) != 1 || info.Agents[0].Agent != want.Agent || !slices.Equal(info.Agents[0].Reads, want.Reads) ||
		!slices.Equal(info.Agents[0].Writes, want.Writes) || info.Agents[0].SpendCentsPerDay != 100 {
		t.Fatalf("the token's approvals = %+v, want %+v", info.Agents, want)
	}
	// The health read is the agent's: the app's own scopes never gain it.
	if slices.ContainsFunc(info.Scopes, func(s string) bool { return strings.Contains(s, "health") }) {
		t.Fatalf("the app's scopes gained the agent's reach: %v", info.Scopes)
	}
	if len(res.AuthorizationDetails) != 1 || res.AuthorizationDetails[0].Agent != aaAgent || res.AuthorizationDetails[0].SpendCentsPerDay != 100 ||
		!slices.Equal(res.AuthorizationDetails[0].Reads, want.Reads) {
		t.Fatalf("the token response's details = %+v", res.AuthorizationDetails)
	}
	// Approved once, the same app's next request shows it already allowed.
	again := decodeJSON[substrate.AuthorizationRequest](t, e.show(e.park(aaRequest(aaScopes, aaDetail(100)))))
	if again.Previous == nil || !again.Setup[0].Agent.Allowed {
		t.Fatalf("the next request's agent row = %+v", again.Setup[0].Agent)
	}
	if higher := decodeJSON[substrate.AuthorizationRequest](t, e.show(e.park(aaRequest(aaScopes, aaDetail(150))))); higher.Setup[0].Agent.Allowed {
		t.Fatalf("a higher cap read as already allowed")
	}
}

func TestTheCheckSaysWhenTheAgentNeedsTheOwnerAgain(t *testing.T) {
	e := newAppEnv(t)
	tok, _ := e.coachToken(100)
	check := func(details string) substrate.AuthorizationCheckResult {
		raw, _ := json.Marshal(map[string]any{"scope": aaScopes, "details": json.RawMessage(details)})
		rec := e.do(http.MethodPost, "/api/v1/authorizations/check", tok, raw, "application/json")
		wantStatus(t, rec, http.StatusOK)
		return decodeJSON[substrate.AuthorizationCheckResult](t, rec)
	}
	if got := check(aaDetail(100)); got.Setup[0].Action != substrate.SetupNone || got.Setup[0].Agent.Agent != aaAgent {
		t.Fatalf("the approved agent checks %+v, want none", got.Setup[0])
	}
	if got := check(aaDetail(150)); got.Setup[0].Action != substrate.SetupEnable {
		t.Fatalf("a higher cap checks %+v, want enable", got.Setup[0])
	}
}

// aaAgentDoc is the coach as the app declares it; edit changes its data.
func aaAgentDoc(edit func(map[string]any)) map[string]any {
	data := map[string]any{
		"authority": fakeRepository, "package": "trainingcoach", "purpose": "primary",
		"description": "the coach", "prompt": "You coach.", "provider": "anthropic", "model": "claude-sonnet-5-5",
		"tools": []any{
			map[string]any{"function": "substrate.reamde.dev/core/query"},
			map[string]any{"function": "substrate.reamde.dev/core/write"},
			map[string]any{"function": "substrate.reamde.dev/core/ask"},
		},
		"permissions": map[string]any{
			"reads":  map[string]any{"kinds": []any{aaRoutine, aaMemory, aaSleep}},
			"writes": []any{aaRoutine, aaMemory, "substrate.reamde.dev/llm/interaction"},
		},
		"budgets": map[string]any{"spendCentsPerDay": 100},
	}
	if edit != nil {
		edit(data)
	}
	return map[string]any{"kind": corePackage + "/agent", "metadata": map[string]any{"id": aaAgent}, "data": data}
}

func aaSeedProvider(e *appEnv, pricing ...any) {
	ds := e.svc.datasets[fakeRepository]
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.records["anthropic"] = &substrate.Record{Kind: "substrate.reamde.dev/llm/provider", ID: "anthropic", Properties: map[string]any{
		"wire": "anthropic", "pricing": pricing,
	}}
}

func TestAnAppDeclaresOnlyItsApprovedAgentHeldToTheApproval(t *testing.T) {
	e := newAppEnv(t)
	tok, _ := e.coachToken(100)
	aaSeedProvider(e, map[string]any{"model": "claude-sonnet-5-5", "inputPer1M": "2", "outputPer1M": "10"},
		map[string]any{"model": "free", "inputPer1M": "0", "outputPer1M": "0"})
	apply := func(token string, docs ...map[string]any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(map[string]any{"documents": docs})
		return e.do(http.MethodPost, "/api/v1/vocabulary/apply", token, raw, "application/json")
	}
	wantStatus(t, apply(tok, aaAgentDoc(nil)), http.StatusOK)

	set := func(path string, value any) func(map[string]any) {
		return func(d map[string]any) {
			keys := strings.Split(path, ".")
			m := d
			for _, k := range keys[:len(keys)-1] {
				m = m[k].(map[string]any)
			}
			if value == nil {
				delete(m, keys[len(keys)-1])
			} else {
				m[keys[len(keys)-1]] = value
			}
		}
	}
	other := aaAgentDoc(nil)
	other["metadata"] = map[string]any{"id": aaCoachPkg + "/otherCoach"}
	for name, doc := range map[string]map[string]any{
		"another agent":           other,
		"a function tool":         aaAgentDoc(set("tools", []any{map[string]any{"function": aaCoachPkg + "/rollup"}})),
		"propose":                 aaAgentDoc(set("tools", []any{map[string]any{"function": "substrate.reamde.dev/core/propose"}})),
		"a background tool":       aaAgentDoc(set("tools", []any{map[string]any{"function": "substrate.reamde.dev/core/query", "background": true}})),
		"toolsFrom":               aaAgentDoc(set("toolsFrom", []any{"x"})),
		"subagents":               aaAgentDoc(set("subagents", []any{fakeRepository + "/boss/lead"})),
		"hiddenFromChat":          aaAgentDoc(set("hiddenFromChat", true)),
		"an internal purpose":     aaAgentDoc(set("purpose", "internal")),
		"a read past approval":    aaAgentDoc(set("permissions.reads.kinds", []any{aaRoutine, "substrate.reamde.dev/clinical/condition"})),
		"a read glob":             aaAgentDoc(set("permissions.reads.kinds", []any{aaTraining + "/*"})),
		"a write past approval":   aaAgentDoc(set("permissions.writes", []any{aaTraining + "/set"})),
		"llm memory":              aaAgentDoc(set("permissions.writes", []any{"substrate.reamde.dev/llm/memory"})),
		"interaction without ask": aaAgentDoc(set("tools", []any{map[string]any{"function": "substrate.reamde.dev/core/query"}})),
		"no cap":                  aaAgentDoc(set("budgets.spendCentsPerDay", nil)),
		"a cap of nothing":        aaAgentDoc(set("budgets.spendCentsPerDay", 0)),
		"a cap past approval":     aaAgentDoc(set("budgets.spendCentsPerDay", 150)),
		"no engine guidance":      aaAgentDoc(set("params", map[string]any{"engineGuidance": false})),
		"an unpriced model":       aaAgentDoc(set("model", "claude-unknown")),
		"a model priced at zero":  aaAgentDoc(set("model", "free")),
		"a missing provider":      aaAgentDoc(set("provider", "gateway")),
	} {
		if rec := apply(tok, doc); rec.Code != http.StatusForbidden {
			t.Errorf("%s: %d %s, want 403", name, rec.Code, rec.Body.String())
		}
	}
	// A broad token approved for no agent declares none, however wide its
	// scopes read: pattern coverage is not an approval.
	broad := e.grant("declare:~/trainingcoach write:*", []string{"declare:" + aaCoachPkg, "write:*"})
	if rec := apply(broad, aaAgentDoc(nil)); rec.Code != http.StatusForbidden {
		t.Errorf("a write:* token declared an agent: %d %s", rec.Code, rec.Body.String())
	}
}

func TestAnAppReadsItsAgentsRowAndTheKindsItReadsAndNothingElse(t *testing.T) {
	e := newAppEnv(t)
	tok, _ := e.coachToken(100)
	ds := e.svc.datasets[fakeRepository]
	ds.mu.Lock()
	ds.records[aaAgent] = &substrate.Record{Kind: corePackage + "/agent", ID: aaAgent, Properties: map[string]any{"prompt": "You coach."}}
	ds.records[fakeRepository+"/boss/lead"] = &substrate.Record{Kind: corePackage + "/agent", ID: fakeRepository + "/boss/lead"}
	ds.records[aaSleep] = &substrate.Record{Kind: corePackage + "/kind", ID: aaSleep}
	for _, k := range []string{"agent", "kind"} {
		ds.types = append(ds.types, substrate.KindInfo{Identity: corePackage + "/" + k, Authority: coreAuthorityName, Package: "core"})
	}
	ds.records["substrate.reamde.dev/clinical/allergy"] = &substrate.Record{Kind: corePackage + "/kind", ID: "substrate.reamde.dev/clinical/allergy"}
	ds.mu.Unlock()
	get := func(kind, id string) int {
		return e.do(http.MethodGet, "/api/v1/"+kind+"/"+url.PathEscape(id), tok, nil, "").Code
	}
	if code := get(corePackage+"/agent", aaAgent); code != http.StatusOK {
		t.Errorf("the approved agent's row: %d", code)
	}
	if code := get(corePackage+"/agent", fakeRepository+"/boss/lead"); code == http.StatusOK {
		t.Errorf("another agent's row: %d, want refused", code)
	}
	if code := get(corePackage+"/kind", aaSleep); code != http.StatusOK {
		t.Errorf("an approved read's definition: %d", code)
	}
	if code := get(corePackage+"/kind", "substrate.reamde.dev/clinical/allergy"); code == http.StatusOK {
		t.Errorf("an unapproved kind's definition: %d, want refused", code)
	}
	if code := get(aaSleep, "last-night"); code == http.StatusOK {
		t.Errorf("a record of the agent's read: %d, want refused to the app", code)
	}
}
