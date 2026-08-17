package engine

// The policy selector's grammar and the judge's threshold routing, both pure
// enough to test without a database.

import (
	"errors"
	"testing"
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
		{nil, "tasks.substrate.reamde.dev/task", true},
		{[]string{"*"}, "tasks.substrate.reamde.dev/task", true},
		{[]string{"tasks.substrate.reamde.dev/*"}, "tasks.substrate.reamde.dev/task", true},
		{[]string{"tasks.substrate.reamde.dev/*"}, "tasks.substrate.reamde.dev/project", true},
		{[]string{"tasks.substrate.reamde.dev/*"}, "people.substrate.reamde.dev/person", false},
		{[]string{"tasks.substrate.reamde.dev/task"}, "tasks.substrate.reamde.dev/task", true},
		{[]string{"tasks.substrate.reamde.dev/task"}, "tasks.substrate.reamde.dev/project", false},
		{[]string{"people.substrate.reamde.dev/person", "tasks.substrate.reamde.dev/*"}, "tasks.substrate.reamde.dev/task", true},
		// The glob cuts on the authority boundary, never on a prefix of it:
		// `tasks.substrate.reamde.dev/*` does not reach
		// `tasks.substrate.reamde.dev.evil`.
		{[]string{"tasks.substrate.reamde.dev/*"}, "tasks.substrate.reamde.dev.evil/task", false},
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
	if rule.matches("k", policyOpPut, "crew.test.dev/editor") {
		t.Fatal("a literal * matched an op and an agent")
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
		{"selector": sel("tasks.substrate.reamde.dev/task")},
		{"action": "gate", "selector": sel("tasks.*")},
		{"action": "gate", "selector": sel("tasks.substrate.reamde.dev/*", "*task*")},
		{"action": "gate", "selector": sel("**")},
	}
	for _, props := range bad {
		if err := validatePolicyRow(props); err == nil {
			t.Fatalf("admitted %v", props)
		}
	}
	good := []map[string]any{
		{"action": "gate"},
		{"action": "refuse", "selector": sel("*")},
		{"action": "allow", "selector": sel("tasks.substrate.reamde.dev/*", "task")},
	}
	for _, props := range good {
		if err := validatePolicyRow(props); err != nil {
			t.Fatalf("refused %v: %v", props, err)
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
