package vocabulary

import (
	"fmt"
	"slices"
	"strings"
)

func (l *loader) parseAgentProvider(where string, data map[string]any, a *Agent) bool {
	a.Provider = ReferentID(data["provider"], LLMKind("provider"))
	a.ProviderInput = strings.TrimSpace(mstr(data, "providerInput"))
	_, provider := data["provider"]
	_, input := data["providerInput"]
	_, runsOn := data["runsOn"]
	named := 0
	for _, says := range []bool{provider, input, runsOn} {
		if says {
			named++
		}
	}
	if named != 1 || (provider && a.Provider == "") || (input && a.ProviderInput == "") {
		l.errf("%s: data.provider is required unless providerInput or runsOn is named; name exactly one of provider, providerInput and runsOn", where)
		return false
	}
	if runsOn {
		return l.parseAgentRunsOn(where, data, a)
	}
	if provider {
		if !ValidID(a.Provider) {
			l.errf("%s: data.provider: %q is not a record id", where, a.Provider)
			return false
		}
		a.Model = strings.TrimSpace(mstr(data, "model"))
		if a.Model == "" {
			l.errf("%s: data.model is required beside provider", where)
			return false
		}
		if _, ok := data["models"]; ok {
			l.errf("%s: data.models belongs beside providerInput", where)
			return false
		}
		return true
	}
	if _, ok := data["model"]; ok {
		l.errf("%s: data.model belongs beside provider", where)
		return false
	}
	models, ok := data["models"].([]any)
	if !ok || len(models) < 1 || len(models) > 8 {
		l.errf("%s: data.models needs one to eight model ids", where)
		return false
	}
	for _, value := range models {
		model, ok := value.(string)
		if !ok || strings.TrimSpace(model) == "" || strings.TrimSpace(model) != model {
			l.errf("%s: data.models entries must be nonempty model ids without surrounding whitespace", where)
			return false
		}
		a.Models = append(a.Models, model)
	}
	return true
}

// parseAgentRunsOn reads `runsOn`: one to eight llm/model records, best first.
// Each record names its own row and model id, so neither `model` nor
// `models` may stand beside it.
func (l *loader) parseAgentRunsOn(where string, data map[string]any, a *Agent) bool {
	for _, key := range []string{"model", "models"} {
		if _, ok := data[key]; ok {
			l.errf("%s: data.%s does not belong beside runsOn: each llm/model record names its own model", where, key)
			return false
		}
	}
	entries, ok := data["runsOn"].([]any)
	if !ok || len(entries) < 1 || len(entries) > 8 {
		l.errf("%s: data.runsOn needs one to eight llm/model records", where)
		return false
	}
	pin := LLMKind("model")
	seen := map[string]bool{}
	for _, value := range entries {
		// A stored or authored `{ref: …}` names its kind; a bare id takes the pin.
		if path := referentPath(value); path != "" {
			if _, isRef := value.(map[string]any); isRef && !strings.HasPrefix(path, pin+"/") {
				l.errf("%s: data.runsOn: %q is not a record of %s", where, path, pin)
				return false
			}
		}
		id := ReferentID(value, pin)
		if !ValidID(id) {
			l.errf("%s: data.runsOn: %v is not a record id of %s", where, value, pin)
			return false
		}
		if seen[id] {
			l.errf("%s: data.runsOn names %q twice", where, id)
			return false
		}
		seen[id] = true
		a.RunsOn = append(a.RunsOn, id)
	}
	return true
}

// PreferredAgentModel chooses by declared preference, then exact id, then lexical order.
// A gateway prefix is admitted only at a slash boundary; dated suffixes do not match.
func PreferredAgentModel(models, priced []string) string {
	priced = slices.Sorted(slices.Values(priced))
	for _, model := range models {
		if slices.Contains(priced, model) {
			return model
		}
		for _, candidate := range priced {
			if candidate == model || strings.HasSuffix(candidate, "/"+model) {
				return candidate
			}
		}
	}
	return ""
}

// PublishedAgentBudgets holds publisher code to an explicit bounded daily cap.
func PublishedAgentBudgets(docs []Document) error {
	for _, d := range docs {
		if d.Kind != DocAgent {
			continue
		}
		budgets, _ := d.Data["budgets"].(map[string]any)
		cap, ok := VersionValue(budgets["spendCentsPerDay"])
		if !ok || cap < 0 || cap > 1000 {
			return fmt.Errorf("agent %s: budgets.spendCentsPerDay is required for a published agent and must be between 0 and 1000 cents", d.ID)
		}
	}
	return nil
}
