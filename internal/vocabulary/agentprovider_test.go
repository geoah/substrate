package vocabulary

import (
	"testing"
)

func TestAgentProviderShapes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		data  map[string]any
		valid bool
	}{
		{"direct", map[string]any{"provider": "default", "model": "m"}, true},
		{"input", map[string]any{"providerInput": "model", "models": []any{"a", "b"}}, true},
		{"neither", map[string]any{}, false},
		{"both", map[string]any{"provider": "default", "providerInput": "model", "model": "m"}, false},
		{"missing model", map[string]any{"provider": "default"}, false},
		{"wrong model arm", map[string]any{"providerInput": "model", "model": "m"}, false},
		{"wrong models arm", map[string]any{"provider": "default", "model": "m", "models": []any{"m"}}, false},
		{"empty", map[string]any{"providerInput": "model", "models": []any{}}, false},
		{"nonstring", map[string]any{"providerInput": "model", "models": []any{3}}, false},
		{"blank", map[string]any{"providerInput": "model", "models": []any{" "}}, false},
		{"too many", map[string]any{"providerInput": "model", "models": []any{"a", "b", "c", "d", "e", "f", "g", "h", "i"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := &loader{}
			if got := l.parseAgentProvider("test", tc.data, &Agent{}); got != tc.valid {
				t.Fatalf("got %v want %v", got, tc.valid)
			}
		})
	}
}

func TestPreferredAgentModel(t *testing.T) {
	for _, tc := range []struct {
		priced []string
		want   string
	}{
		{[]string{"b", "gateway/a"}, "gateway/a"},
		{[]string{"gateway/a", "a"}, "a"},
		{[]string{"a-20261103", "b"}, "b"},
		{[]string{"unknown"}, ""},
	} {
		if got := PreferredAgentModel([]string{"a", "b"}, tc.priced); got != tc.want {
			t.Fatalf("got %q want %q", got, tc.want)
		}
	}
}

func TestPublishedAgentBudgets(t *testing.T) {
	for _, cap := range []any{nil, -1, 0, 1000, 1001, 1.5, "100"} {
		docs := []Document{{Kind: DocAgent, ID: "agents.example.com/coach/run", Data: map[string]any{"budgets": map[string]any{"spendCentsPerDay": cap}}}}
		err := PublishedAgentBudgets(docs)
		valid := cap == 0 || cap == 1000
		if (err == nil) != valid {
			t.Fatalf("cap %v: %v", cap, err)
		}
	}
}

func TestAgentProviderInputBelongsToOwnBundle(t *testing.T) {
	ag := &Agent{Name: "run", Package: "coach.example.com/coach", ProviderInput: "model"}
	for _, bundle := range []*Bundle{nil, {Inputs: map[string]BundleInput{}}, {Inputs: map[string]BundleInput{"model": {Kind: CoreKind("setting")}}}, {Inputs: map[string]BundleInput{"model": {Kind: LLMKind("provider")}}}} {
		g := &Package{Agents: map[string]*Agent{"run": ag}, AgentOrder: []string{"run"}, Bundle: bundle}
		problems := (&Registry{}).resolvePackageAgents(g)
		valid := bundle != nil && bundle.Inputs["model"].Kind == LLMKind("provider")
		if (len(problems) == 0) != valid {
			t.Fatalf("bundle %+v: %v", bundle, problems)
		}
	}
}
