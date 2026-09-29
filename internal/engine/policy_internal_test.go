package engine

// The policy selector's grammar and the judge's threshold routing, both pure
// enough to test without a database.

import (
	"errors"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

// TestPolicySelectorKindsTakeTheTriggerGlob: `selector.kinds` is the trigger
// source's grammar and nothing else — a reference, `<authority>/*` or `*` —
// so an owner who wants "everything this authority publishes" writes one
// pattern instead of an enumerated snapshot that misses the next installed
// kind.
func TestPolicySelectorKindsTakeTheTriggerGlob(t *testing.T) {
	t.Parallel()
	cases := []struct {
		pats []string
		kind string
		want bool
	}{
		{nil, "samples.substrate.reamde.dev/tasks/task", true},
		{[]string{"*"}, "samples.substrate.reamde.dev/tasks/task", true},
		{[]string{"samples.substrate.reamde.dev/tasks/*"}, "samples.substrate.reamde.dev/tasks/task", true},
		{[]string{"samples.substrate.reamde.dev/tasks/*"}, "samples.substrate.reamde.dev/tasks/project", true},
		{[]string{"samples.substrate.reamde.dev/tasks/*"}, "samples.substrate.reamde.dev/people/person", false},
		{[]string{"samples.substrate.reamde.dev/tasks/task"}, "samples.substrate.reamde.dev/tasks/task", true},
		{[]string{"samples.substrate.reamde.dev/tasks/task"}, "samples.substrate.reamde.dev/tasks/project", false},
		{[]string{"samples.substrate.reamde.dev/people/person", "samples.substrate.reamde.dev/tasks/*"}, "samples.substrate.reamde.dev/tasks/task", true},
		// The glob cuts on the authority boundary, never on a prefix of it:
		// `samples.substrate.reamde.dev/tasks/*` does not reach
		// `samples.substrate.reamde.dev/tasks.evil`.
		{[]string{"samples.substrate.reamde.dev/tasks/*"}, "samples.substrate.reamde.dev/tasks.evil/task", false},
	}
	for _, c := range cases {
		rule := policyRule{kinds: c.pats, action: policyGate}
		if got := rule.matches(c.kind, policyOpPut, "a"); got != c.want {
			t.Fatalf("kinds %v against %s: %v, want %v", c.pats, c.kind, got, c.want)
		}
	}
}

// TestPolicySelectorOpsAndAgentsStayExact: only the kinds dimension globs. An
// agent identity has no authority half to cut on and the ops are a closed
// enum, so a `*` in either is a literal that matches nothing.
func TestPolicySelectorOpsAndAgentsStayExact(t *testing.T) {
	t.Parallel()
	rule := policyRule{ops: []string{"*"}, agents: []string{"*"}, action: policyGate}
	if rule.matches("k", policyOpPut, "crew.test.dev/crew/editor") {
		t.Fatal("a literal * matched an op and an agent")
	}
}

// TestPolicySelectorFunctionsArm: `functions` matches the function whose body
// returned the effect or the root of its call chain, exactly, wherever the
// function runs. A rule naming no function speaks for agent writes alone, so
// a write no agent wants (a trigger's, a call's) meets only rules that name
// its function.
func TestPolicySelectorFunctionsArm(t *testing.T) {
	t.Parallel()
	const (
		fn    = "crew.test.dev/crew/annotate"
		other = "crew.test.dev/crew/measure"
		agent = "crew.test.dev/crew/editor"
	)
	named := policyRule{functions: []string{fn}, action: policyGate}
	empty := policyRule{action: policyGate}
	narrowed := policyRule{functions: []string{fn}, agents: []string{agent}, action: policyGate}
	cases := []struct {
		name      string
		rule      policyRule
		agent     string
		functions []string
		want      bool
	}{
		{"named, a trigger run", named, "", []string{fn}, true},
		{"named, a callee under the root", named, "", []string{other, fn}, true},
		{"named, another function", named, "", []string{other}, false},
		{"named, an agent's tool call", named, agent, []string{fn}, true},
		{"named, an agent's own write", named, agent, nil, false},
		{"empty, a trigger run", empty, "", []string{fn}, false},
		{"empty, an agent's tool call", empty, agent, []string{fn}, true},
		{"empty, an agent's own write", empty, agent, nil, true},
		{"narrowed, a trigger run", narrowed, "", []string{fn}, false},
		{"narrowed, that agent's tool call", narrowed, agent, []string{fn}, true},
		{"a literal *", policyRule{functions: []string{"*"}, action: policyGate}, "", []string{fn}, false},
	}
	for _, c := range cases {
		if got := c.rule.matches("k", policyOpPut, c.agent, c.functions...); got != c.want {
			t.Fatalf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// TestEffectVerdictReadsTheConfirmationFloor: `confirmation: always` on the
// function that returned an effect, or on the root of its call chain, gates
// the effect with no policy. A proposal of its own (the request kind) is
// exempt, and a merge under the floor is refused because no request can
// carry it.
func TestEffectVerdictReadsTheConfirmationFloor(t *testing.T) {
	t.Parallel()
	floored := &vocabulary.Function{Package: "crew.test.dev/crew", Name: "burn", Confirmation: vocabulary.FunctionConfirmAlways}
	plain := &vocabulary.Function{Package: "crew.test.dev/crew", Name: "annotate"}
	put := func(by *vocabulary.Function, kind string) effect {
		return effect{Action: effectPut, Type: kind, ID: "x", by: by}
	}
	cases := []struct {
		name string
		root *vocabulary.Function
		ef   effect
		want string
	}{
		{"no floor", plain, put(plain, "k"), policyAllow},
		{"the root's floor", floored, put(floored, "k"), policyGate},
		{"the root's floor over a callee", floored, put(plain, "k"), policyGate},
		{"a callee's floor under a plain root", plain, put(floored, "k"), policyGate},
		{"a proposal of its own", floored, put(floored, vocabulary.KindRecordPatchRequest), policyAllow},
	}
	for _, c := range cases {
		got, rule, err := effectVerdict(nil, c.root, c.ef, "")
		if err != nil || got != c.want || rule != nil {
			t.Fatalf("%s: %q %v %v, want %q", c.name, got, rule, err, c.want)
		}
	}
	merge := effect{Action: effectMerge, Type: "k", ID: "x", Loser: "y", by: floored}
	if _, _, err := effectVerdict(nil, floored, merge, ""); err == nil {
		t.Fatal("a merge under the floor was admitted")
	}
	merge.by = plain
	if got, _, err := effectVerdict(nil, plain, merge, ""); err != nil || got != policyAllow {
		t.Fatalf("a merge with no floor: %q %v", got, err)
	}
}

// TestValidatePolicyRowRefusesARuleTheDoorCannotAct: an actionless rule speaks
// for nothing, and a pattern outside the grammar (`tasks.*`) matches no write
// at all — on this kind that reads as a gate the owner believes is closed.
func TestValidatePolicyRowRefusesARuleTheDoorCannotAct(t *testing.T) {
	t.Parallel()
	sel := func(kinds ...any) map[string]any {
		return map[string]any{"kinds": kinds}
	}
	bad := []map[string]any{
		{"selector": sel("samples.substrate.reamde.dev/tasks/task")},
		{"action": "gate", "selector": sel("tasks.*")},
		{"action": "gate", "selector": sel("samples.substrate.reamde.dev/tasks/*", "*task*")},
		{"action": "gate", "selector": sel("**")},
	}
	for _, props := range bad {
		if err := validatePolicyRow(nil, props); err == nil {
			t.Fatalf("admitted %v", props)
		}
	}
	good := []map[string]any{
		{"action": "gate"},
		{"action": "refuse", "selector": sel("*")},
		{"action": "allow", "selector": sel("samples.substrate.reamde.dev/tasks/*", "task")},
	}
	for _, props := range good {
		if err := validatePolicyRow(nil, props); err != nil {
			t.Fatalf("refused %v: %v", props, err)
		}
	}
}

// TestGovernsPrefersTheRuleThatCannotAutoAccept: among matches of equal
// severity the governing rule carries the judge, so an id tie-break alone
// would let the laxer of two rules decide by alphabet — and would let a
// selector that starts matching (a bare name now resolved, a stored `*` now
// honored) move a write from "the owner decides" to "a model may decide".
func TestGovernsPrefersTheRuleThatCannotAutoAccept(t *testing.T) {
	t.Parallel()
	f := func(v float64) *float64 { return &v }
	judged := func(id string) *policyRule {
		return &policyRule{id: id, action: policyGate, judge: "j", mode: "enforce", autoAccept: f(0.6)}
	}
	plain := func(id string) *policyRule { return &policyRule{id: id, action: policyGate} }

	// The judged rule sorts first by id and still does not govern.
	if !governs(plain("z-plain"), judged("a-judged")) {
		t.Fatal("a judged gate outranked an unjudged one")
	}
	if governs(judged("a-judged"), plain("z-plain")) {
		t.Fatal("the comparison is not antisymmetric")
	}
	// Severity still comes first: a refuse outranks any gate.
	if !governs(&policyRule{id: "z", action: policyRefuse}, plain("a")) {
		t.Fatal("a gate outranked a refuse")
	}
	// A judge that cannot auto-accept is not laxer, so the id decides.
	advisory := &policyRule{id: "a-advisory", action: policyGate, judge: "j", mode: "advise", autoAccept: f(0.6)}
	noFloor := &policyRule{id: "a-nofloor", action: policyGate, judge: "j", mode: "enforce"}
	for _, r := range []*policyRule{advisory, noFloor} {
		if r.canAutoAccept() {
			t.Fatalf("%s counts as able to auto-accept", r.id)
		}
		if !governs(r, plain("z-plain")) {
			t.Fatalf("%s lost the id tie-break it should win", r.id)
		}
	}
}

// TestJudgeThresholdsAreTwoFloorsNotABand: the verdict picks the threshold, so
// `autoAccept` and `autoRefuse` never decide the same reply between them, and
// where one confidence clears both floors the refusal wins.
func TestJudgeThresholdsAreTwoFloorsNotABand(t *testing.T) {
	t.Parallel()
	f := func(v float64) *float64 { return &v }
	enforce := func(accept, refuse *float64) *policyRule {
		return &policyRule{mode: "enforce", autoAccept: accept, autoRefuse: refuse}
	}
	cases := []struct {
		name    string
		rule    *policyRule
		verdict judgeVerdict
		jerr    error
		want    string
	}{
		{
			"an accept reads autoAccept alone",
			enforce(f(0.5), f(0.5)),
			judgeVerdict{Verdict: judgeVerdictAccept, Confidence: 0.9},
			nil, judgedAccepted,
		},
		{
			"a reject reads autoRefuse alone",
			enforce(f(0.5), f(0.5)),
			judgeVerdict{Verdict: judgeVerdictReject, Confidence: 0.9},
			nil, judgedRejected,
		},
		{
			"a reject clearing both floors refuses",
			enforce(f(0.1), f(0.1)),
			judgeVerdict{Verdict: judgeVerdictReject, Confidence: 1},
			nil, judgedRejected,
		},
		{
			"an accept under its own floor escalates, whatever autoRefuse says",
			enforce(f(0.9), f(0.1)),
			judgeVerdict{Verdict: judgeVerdictAccept, Confidence: 0.5},
			nil, judgedEscalated,
		},
		{
			"a reject with no autoRefuse escalates",
			enforce(f(0.1), nil),
			judgeVerdict{Verdict: judgeVerdictReject, Confidence: 1},
			nil, judgedEscalated,
		},
		{
			"an escalate verdict escalates past both floors",
			enforce(f(0), f(0)),
			judgeVerdict{Verdict: judgeVerdictEscalate, Confidence: 1},
			nil, judgedEscalated,
		},
		{
			"advise mode decides nothing",
			&policyRule{mode: "advise", autoAccept: f(0), autoRefuse: f(0)},
			judgeVerdict{Verdict: judgeVerdictAccept, Confidence: 1},
			nil, judgedAdvised,
		},
		{
			"a judge that failed decides nothing",
			enforce(f(0), f(0)),
			judgeVerdict{Verdict: judgeVerdictAccept, Confidence: 1},
			errors.New("transport"), judgedError,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, note := routeVerdict(c.rule, c.verdict, c.jerr)
			if got != c.want {
				t.Fatalf("outcome %q, want %q", got, c.want)
			}
			if (note != "") != (c.jerr != nil) {
				t.Fatalf("note %q against error %v", note, c.jerr)
			}
		})
	}
}
