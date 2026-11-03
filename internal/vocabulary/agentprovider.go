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
	if provider == input || (provider && a.Provider == "") || (input && a.ProviderInput == "") {
		l.errf("%s: data.provider is required unless providerInput is named; name exactly one of provider and providerInput", where)
		return false
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
