package authorize

import (
	"fmt"
	"strings"
	"testing"
)

// agentDetail is an agent detail with reads, writes and a cap spliced in as
// raw JSON members.
func agentDetail(agent, reads, writes, rest string) string {
	return fmt.Sprintf(`[{"type":"agent","agent":%q,"reads":%s,"writes":%s%s}]`, agent, reads, writes, rest)
}

const coachID = "~/trainingcoach/workoutCoach"

func TestAnAgentDetailIsReadAsTheAppWroteIt(t *testing.T) {
	got, err := ParseDetails(agentDetail(coachID,
		`["~/training/routine","~/training/memory","substrate.reamde.dev/observations/observation","substrate.reamde.dev/clinical/condition"]`,
		`["~/training/routine","~/training/memory"]`, `,"spendCentsPerDay":100`))
	if err != nil {
		t.Fatal(err)
	}
	d := got[0]
	if d.Agent != coachID || len(d.Reads) != 4 || len(d.Writes) != 2 || d.SpendCentsPerDay != 100 {
		t.Fatalf("agent detail = %+v", d)
	}
	if AgentDeclarePackage(d.Agent) != "~/trainingcoach" {
		t.Fatalf("its declare package = %s", AgentDeclarePackage(d.Agent))
	}
	// A coach that reads and writes nothing more is a read-only agent.
	if _, err := ParseDetails(agentDetail(coachID, `["~/training/routine"]`, `[]`, `,"spendCentsPerDay":1`)); err != nil {
		t.Errorf("a read-only agent at a cent a day: %v", err)
	}
}

func TestAnAgentDetailIsReadStrictly(t *testing.T) {
	reads := `["~/training/routine","~/training/memory"]`
	writes := `["~/training/memory"]`
	capped := `,"spendCentsPerDay":100`
	many := make([]string, maxAgentReads+1)
	for i := range many {
		many[i] = fmt.Sprintf("%q", fmt.Sprintf("example.com/k%c/kind", 'a'+i))
	}
	cases := map[string]string{
		"another authority's agent":  agentDetail("example.com/trainingcoach/workoutCoach", reads, writes, capped),
		"a bare agent":               agentDetail("workoutCoach", reads, writes, capped),
		"an agent glob":              agentDetail("~/trainingcoach/*", reads, writes, capped),
		"a kind word as name":        agentDetail("~/trainingcoach/Workout-coach", reads, writes, capped),
		"no cap":                     agentDetail(coachID, reads, writes, ""),
		"a cap of nothing":           agentDetail(coachID, reads, writes, `,"spendCentsPerDay":0`),
		"a cap past the ceiling":     agentDetail(coachID, reads, writes, `,"spendCentsPerDay":201`),
		"a fractional cap":           agentDetail(coachID, reads, writes, `,"spendCentsPerDay":1.5`),
		"reads nothing":              agentDetail(coachID, `[]`, `[]`, capped),
		"a read glob":                agentDetail(coachID, `["~/training/*"]`, `[]`, capped),
		"reads everything":           agentDetail(coachID, `["*"]`, `[]`, capped),
		"a bare read":                agentDetail(coachID, `["routine"]`, `[]`, capped),
		"reads a token":              agentDetail(coachID, `["substrate.reamde.dev/core/token"]`, `[]`, capped),
		"reads agents":               agentDetail(coachID, `["substrate.reamde.dev/core/agent"]`, `[]`, capped),
		"reads llm memory":           agentDetail(coachID, `["substrate.reamde.dev/llm/memory"]`, `[]`, capped),
		"reads providers":            agentDetail(coachID, `["substrate.reamde.dev/llm/provider"]`, `[]`, capped),
		"writes llm memory":          agentDetail(coachID, `["substrate.reamde.dev/llm/memory"]`, `["substrate.reamde.dev/llm/memory"]`, capped),
		"writes interactions":        agentDetail(coachID, reads, `["substrate.reamde.dev/llm/interaction"]`, capped),
		"writes change requests":     agentDetail(coachID, reads, `["substrate.reamde.dev/core/recordpatchrequest"]`, capped),
		"writes what it reads not":   agentDetail(coachID, `["~/training/routine"]`, `["~/training/memory"]`, capped),
		"a read twice":               agentDetail(coachID, `["~/training/routine","~/training/routine"]`, `[]`, capped),
		"too many reads":             agentDetail(coachID, "["+strings.Join(many, ",")+"]", `[]`, capped),
		"a package's member":         `[{"type":"agent","agent":"~/trainingcoach/workoutCoach","reads":["~/training/routine"],"spendCentsPerDay":1,"package":"example.com/x"}]`,
		"an unknown member":          `[{"type":"agent","agent":"~/trainingcoach/workoutCoach","reads":["~/training/routine"],"spendCentsPerDay":1,"tools":["query"]}]`,
		"a duplicate agent":          `[{"type":"agent","agent":"~/trainingcoach/workoutCoach","reads":["~/training/routine"],"spendCentsPerDay":1},{"type":"agent","agent":"~/trainingcoach/workoutCoach","reads":["~/training/set"],"spendCentsPerDay":2}]`,
		"agent members on a package": `[{"type":"package","package":"example.com/notes","version":1,"agent":"~/trainingcoach/workoutCoach"}]`,
	}
	for name, param := range cases {
		if d, err := ParseDetails(param); err == nil {
			t.Errorf("%s: ParseDetails admitted %+v, want refused", name, d)
		}
	}
}
