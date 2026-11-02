package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// An app's agent (decision 0206) runs only while its app holds a live
// approval that covers it, on a priced model, as a root run, and stops at
// its next model call when that ends.

var (
	aaApp      = substrate.AppActor("workouts.example.org")
	aaClientID = "https://workouts.example.org/client.json"
)

// aaFixture is an app's training package with one kind, its coach package
// with the agent, a priced provider and a fake model.
type aaFixture struct {
	ds       *dataset
	fake     *fakeLLM
	training string
	memory   string
	coachPkg string
	agent    string
}

func (f aaFixture) approval() substrate.AgentApproval {
	return substrate.AgentApproval{Agent: f.agent, Reads: []string{f.memory}, Writes: []string{f.memory}, SpendCentsPerDay: 100}
}

// coachDocs is the coach package as the app declares it; prompt varies the
// agent so a re-apply is a change.
func (f aaFixture) coachDocs(prompt string, capCents int) []map[string]any {
	return []map[string]any{
		vocabulary.PackageManifest(f.coachPkg, 0),
		vocabulary.BundleManifest(f.coachPkg, map[string]any{"installs": []any{f.agent}}),
		vocabulary.AgentManifest(f.coachPkg, "workoutCoach", map[string]any{
			"description": "the coach under test", "prompt": prompt,
			"provider": "coachllm", "model": "coach",
			"tools": []any{
				map[string]any{"function": "substrate.reamde.dev/core/query"},
				map[string]any{"function": "substrate.reamde.dev/core/write"},
				map[string]any{"function": "substrate.reamde.dev/core/ask"},
			},
			"permissions": map[string]any{
				"reads":  map[string]any{"kinds": []any{f.memory}},
				"writes": []any{f.memory, "substrate.reamde.dev/llm/interaction"},
			},
			"budgets": map[string]any{"spendCentsPerDay": capCents, "maxTurns": 6},
		}),
	}
}

func openAppAgent(t *testing.T) aaFixture {
	t.Helper()
	ctx := context.Background()
	ds := openInternalDataset(t)
	fake := newFakeLLM(t)
	ds.mu.Lock()
	ds.skipThreadTitles = true
	ds.mu.Unlock()
	home := ds.Repository().Authority
	f := aaFixture{
		ds: ds, fake: fake, training: home + "/training", memory: home + "/training/memory",
		coachPkg: home + "/trainingcoach", agent: home + "/trainingcoach/workoutCoach",
	}
	putAAProvider(t, ds, fake, "1", "5")
	if _, err := ds.ApplyVocabularyDocuments(ctx, aaApp, []map[string]any{
		vocabulary.PackageManifest(f.training, 0),
		vocabulary.BundleManifest(f.training, map[string]any{"installs": []any{f.memory}}),
		vocabulary.KindManifest(f.training, map[string]any{"singular": "memory"}, map[string]any{"properties": map[string]any{
			"text": map[string]any{"type": "text"},
		}}),
	}); err != nil {
		t.Fatalf("the app declares training: %v", err)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, aaApp, f.coachDocs("You coach.", 100)); err != nil {
		t.Fatalf("the app declares its coach: %v", err)
	}
	return f
}

func putAAProvider(t *testing.T, ds *dataset, fake *fakeLLM, in, out string) {
	t.Helper()
	pricing := []any{}
	if in != "" {
		pricing = append(pricing, map[string]any{"model": "coach", "inputPer1M": in, "outputPer1M": out})
	}
	if _, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{
		Kind: typeProvider, ID: "coachllm",
		Properties: map[string]any{"wire": "openai", "baseURL": fake.srv.URL, "apiKey": "row-key-coach", "pricing": pricing},
	}); err != nil {
		t.Fatalf("put the coach's provider: %v", err)
	}
}

// mintApproval mints the app's token with approvals, ending at expires
// (nil: until revoked).
func (f aaFixture) mintApproval(t *testing.T, expires *time.Time, approvals ...substrate.AgentApproval) substrate.TokenInfo {
	t.Helper()
	info, _, err := f.ds.MintToken(context.Background(), substrate.MintTokenInput{
		Label: "workouts.example.org", ExpiresAt: expires, Scopes: []string{"declare:" + f.coachPkg},
		ClientID: aaClientID, ClientName: "Substrate Workouts", Agents: approvals,
	})
	if err != nil {
		t.Fatalf("mint the app's token: %v", err)
	}
	return info
}

func (f aaFixture) revoke(t *testing.T, tok substrate.TokenInfo) {
	t.Helper()
	if _, err := f.ds.Delete(context.Background(), substrate.ActorConsole, kindToken, tok.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("revoke the app's token: %v", err)
	}
}

func wantStopped(t *testing.T, what string, err error, words string) {
	t.Helper()
	if !errors.Is(err, substrate.ErrGuard) || !strings.Contains(err.Error(), words) {
		t.Fatalf("%s: %v, want a guard saying %q", what, err, words)
	}
}

func TestAnAppsAgentRunsOnlyWhileItsAppHoldsALiveApproval(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := openAppAgent(t)

	_, err := f.ds.CallAgent(ctx, f.agent, "hello")
	wantStopped(t, "with no token at all", err, "holds no live approval")

	tok := f.mintApproval(t, nil, f.approval())
	f.fake.script("coach", fakeTurn{content: "hi"})
	res, err := f.ds.CallAgent(ctx, f.agent, "hello")
	if err != nil || res.Status != threadOK {
		t.Fatalf("with a live approval: %+v, %v", res, err)
	}

	f.revoke(t, tok)
	_, err = f.ds.CallAgent(ctx, f.agent, "hello")
	wantStopped(t, "after revocation", err, "Substrate Workouts, the app that keeps it, holds no live approval")

	// Signing in again restores it, with nothing to flip back.
	f.mintApproval(t, nil, f.approval())
	f.fake.script("coach", fakeTurn{content: "back"})
	if res, err := f.ds.CallAgent(ctx, f.agent, "hello"); err != nil || res.Status != threadOK {
		t.Fatalf("after signing in again: %+v, %v", res, err)
	}
}

func TestAnAppsAgentStopsWhenItsApprovalEndsOrNarrows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := openAppAgent(t)
	past := time.Now().Add(-time.Hour)
	narrow := f.approval()
	narrow.Writes = nil
	cheap := f.approval()
	cheap.SpendCentsPerDay = 50
	other := f.approval()
	other.Agent = f.coachPkg + "/otherCoach"
	for _, tc := range []struct {
		name    string
		expires *time.Time
		a       substrate.AgentApproval
	}{
		{"an expired token", &past, f.approval()},
		{"an approval without its writes", nil, narrow},
		{"an approval below its cap", nil, cheap},
		{"an approval of another agent", nil, other},
	} {
		tok := f.mintApproval(t, tc.expires, tc.a)
		_, err := f.ds.CallAgent(ctx, f.agent, "hello")
		wantStopped(t, tc.name, err, "holds no live approval")
		f.revoke(t, tok)
	}
	// Another app's token approving the same agent id covers nothing.
	if _, _, err := f.ds.MintToken(ctx, substrate.MintTokenInput{
		Label: "notes", Scopes: []string{"declare:" + f.coachPkg},
		ClientID: "https://notes.example.org/client.json", Agents: []substrate.AgentApproval{f.approval()},
	}); err != nil {
		t.Fatal(err)
	}
	_, err := f.ds.CallAgent(ctx, f.agent, "hello")
	wantStopped(t, "another app's approval", err, "holds no live approval")
}

func TestAnAppsAgentRefusesAnUnpricedModel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := openAppAgent(t)
	f.mintApproval(t, nil, f.approval())
	putAAProvider(t, f.ds, f.fake, "", "")
	_, err := f.ds.CallAgent(ctx, f.agent, "hello")
	wantStopped(t, "an unpriced model", err, "has no price")
	putAAProvider(t, f.ds, f.fake, "0", "0")
	_, err = f.ds.CallAgent(ctx, f.agent, "hello")
	wantStopped(t, "a model priced at nothing", err, "has no price")
}

func TestAnAppsAgentNeverRunsAsASubAgent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := openAppAgent(t)
	f.mintApproval(t, nil, f.approval())
	boss := f.ds.Repository().Authority + "/boss"
	if _, err := f.ds.ApplyVocabularyDocuments(ctx, substrate.ActorConsole, []map[string]any{
		vocabulary.PackageManifest(boss, 0),
		vocabulary.AgentManifest(boss, "lead", map[string]any{
			"description": "the owner's agent", "prompt": "You lead.", "provider": "coachllm", "model": "lead",
			"subagents": []any{f.agent},
		}),
	}); err != nil {
		t.Fatalf("the owner declares an agent that calls the coach: %v", err)
	}
	if _, err := f.ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeProvider, ID: "coachllm",
		Properties: map[string]any{"wire": "openai", "baseURL": f.fake.srv.URL, "apiKey": "k", "pricing": []any{
			map[string]any{"model": "coach", "inputPer1M": "1", "outputPer1M": "5"},
			map[string]any{"model": "lead", "inputPer1M": "1", "outputPer1M": "5"},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	f.fake.script("lead", fakeTurn{calls: []fakeCall{{"workoutCoach", `{"input":"plan"}`}}}, fakeTurn{content: "done"})
	if _, err := f.ds.CallAgent(ctx, boss+"/lead", "go"); err != nil {
		t.Fatalf("the owner's agent runs: %v", err)
	}
	if got := len(f.fake.requestsOf("coach")); got != 0 {
		t.Fatalf("the coach ran %d model calls as a sub-agent, want none", got)
	}
	reqs := f.fake.requestsOf("lead")
	if len(reqs) != 2 || !strings.Contains(fmtJSON(reqs[1]), "never as another agent's sub-agent") {
		t.Fatalf("the lead's second turn does not carry the refusal: %v", reqs)
	}
}

func TestARunningAppAgentStopsAtItsNextModelCallWhenRevoked(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := openAppAgent(t)
	tok := f.mintApproval(t, nil, f.approval())
	arrived, release := make(chan struct{}), make(chan struct{})
	f.fake.script("coach",
		fakeTurn{calls: []fakeCall{{"query", `{"filter":{"kinds":["` + f.memory + `"]}}`}}, arrived: arrived, release: release},
		fakeTurn{content: "never sent"},
	)
	type answer struct {
		res *substrate.AgentResult
		err error
	}
	done := make(chan answer, 1)
	go func() {
		res, err := f.ds.CallAgent(ctx, f.agent, "plan my week")
		done <- answer{res, err}
	}()
	<-arrived
	f.revoke(t, tok)
	close(release)
	got := <-done
	if got.err != nil || got.res.Status != threadStopped || !strings.Contains(got.res.Reason, "holds no live approval") {
		t.Fatalf("the run after revocation: %+v, %v", got.res, got.err)
	}
	if n := len(f.fake.requestsOf("coach")); n != 1 {
		t.Fatalf("the coach made %d model calls, want 1", n)
	}
}

func TestARunningAppAgentStopsAtItsCap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := openAppAgent(t)
	f.mintApproval(t, nil, f.approval())
	// 10 prompt and 5 completion tokens at these rates spend $10, past the
	// coach's dollar a day after its first call.
	putAAProvider(t, f.ds, f.fake, "1000000", "0.000001")
	f.fake.script("coach",
		fakeTurn{calls: []fakeCall{{"query", `{"filter":{"kinds":["` + f.memory + `"]}}`}}},
		fakeTurn{content: "never sent"},
	)
	res, err := f.ds.CallAgent(ctx, f.agent, "plan my week")
	if err != nil || res.Status != threadStopped || !strings.Contains(res.Reason, "spend cap reached") {
		t.Fatalf("the run past its cap: %+v, %v", res, err)
	}
	if n := len(f.fake.requestsOf("coach")); n != 1 {
		t.Fatalf("the coach made %d model calls, want 1", n)
	}
	_, err = f.ds.CallAgent(ctx, f.agent, "again")
	wantStopped(t, "the next run", err, "spend cap reached")
}

// The off switch is the owner's: disabling the bundle stops the coach, the
// app's re-apply keeps it disabled, and enabling it is the owner's too.
func TestTheOwnerTurnsAnAppsAgentOffAndTheAppsApplyKeepsItOff(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := openAppAgent(t)
	f.mintApproval(t, nil, f.approval())
	if err := f.ds.DisableBundle(ctx, f.coachPkg); err != nil {
		t.Fatalf("the owner disables the coach's bundle: %v", err)
	}
	_, err := f.ds.CallAgent(ctx, f.agent, "hello")
	wantStopped(t, "a disabled coach", err, "is disabled")
	if _, err := f.ds.ApplyVocabularyDocuments(ctx, aaApp, f.coachDocs("You coach, revision 2.", 100)); err != nil {
		t.Fatalf("the app re-applies its coach: %v", err)
	}
	st, err := f.ds.bundleStateOf(ctx, f.coachPkg)
	if err != nil || !st.Disabled {
		t.Fatalf("after the app's re-apply the bundle is %+v, %v; want it still disabled", st, err)
	}
	_, err = f.ds.CallAgent(ctx, f.agent, "hello")
	wantStopped(t, "still disabled", err, "is disabled")
	if err := f.ds.EnableBundle(ctx, f.coachPkg); err != nil {
		t.Fatalf("the owner enables it again: %v", err)
	}
	f.fake.script("coach", fakeTurn{content: "on"})
	if res, err := f.ds.CallAgent(ctx, f.agent, "hello"); err != nil || res.Status != threadOK {
		t.Fatalf("enabled again: %+v, %v", res, err)
	}
}

// An agent row of an app's package is the app's declaration like its
// kinds: the owner changes it by taking the package over, after which it is
// the owner's own agent and no app approval holds it.
func TestAnAppsAgentRowIsTheAppsUntilTheOwnerTakesItOver(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := openAppAgent(t)
	_, err := f.ds.ApplyVocabularyDocuments(ctx, substrate.ActorConsole, f.coachDocs("Owner's prompt.", 100)[2:])
	if !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("the owner applies the app's agent: %v, want refused", err)
	}
	_, err = f.ds.Patch(ctx, substrate.ActorConsole, vocabulary.CoreKind(vocabulary.DocAgent), f.agent, substrate.PatchInput{
		Properties: map[string]any{"prompt": "Owner's prompt."},
	})
	if !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("the owner patches the app's agent row: %v, want refused", err)
	}
	if _, err := f.ds.TakeOverPackage(ctx, substrate.ActorConsole, f.coachPkg, f.coachPkg); err != nil {
		t.Fatalf("take over: %v", err)
	}
	f.fake.script("coach", fakeTurn{content: "mine now"})
	if res, err := f.ds.CallAgent(ctx, f.agent, "hello"); err != nil || res.Status != threadOK {
		t.Fatalf("the owner's agent after takeover, with no app token: %+v, %v", res, err)
	}
}

func TestAnAppRemovesOnlyAPackageItKeepsThatHoldsNoKinds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := openAppAgent(t)
	if err := f.ds.UninstallAppBundle(ctx, substrate.ActorConsole, f.coachPkg); !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("the owner through the app's door: %v, want refused", err)
	}
	if err := f.ds.UninstallAppBundle(ctx, substrate.AppActor("notes.example.org"), f.coachPkg); !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("another app: %v, want refused", err)
	}
	if err := f.ds.UninstallAppBundle(ctx, aaApp, f.training); !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("the app removes its training kinds: %v, want refused", err)
	}
	if err := f.ds.UninstallAppBundle(ctx, aaApp, f.coachPkg); err != nil {
		t.Fatalf("the app removes its coach: %v", err)
	}
	if _, err := f.ds.registry().ResolveAgent(f.agent); err == nil {
		t.Fatal("the coach still resolves after its package was removed")
	}
	if _, err := f.ds.KindByRef(ctx, f.memory); err != nil {
		t.Fatalf("the memory kind went with the coach: %v", err)
	}
}

func fmtJSON(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}
