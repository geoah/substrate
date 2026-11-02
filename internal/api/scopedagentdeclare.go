package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// What a `declare:` scope admits of an agent (decision 0207): the one agent
// an `agent` detail approved, in the package the scope declares, held to the
// approval. The engine holds it again at every run (a live approval, a priced
// model, a root run); these are the apply's ceilings.

// providerKind is the kind of the row an agent's `provider` names.
var providerKind = vocabulary.PackageLLM + "/provider"

// declarableAgentKeys are the agent keys an app's agent may carry.
// `toolsFrom` and `subagents` reach other code and other grants,
// `hiddenFromChat` hides an agent the owner approved to chat with, and
// `resume` is the owner's call on how a stopped thread goes on.
var declarableAgentKeys = map[string]bool{
	"authority": true, "package": true, "description": true, "purpose": true,
	"prompt": true, "provider": true, "model": true, "params": true, "tools": true,
	"context": true, "compaction": true, "budgets": true, "permissions": true,
}

// agentTools are the tools an app's agent may name: reading, writing and
// asking the owner, each a host function bound by the agent's own grant.
var agentTools = map[string]bool{
	vocabulary.PackageCore + "/query": true,
	vocabulary.PackageCore + "/write": true,
	vocabulary.PackageCore + "/ask":   true,
}

// declarableAgent holds one agent document to its approval: everything the
// document alone says. The provider's pricing is read by agentsPriced.
func (s *scopedDataset) declarableAgent(id string, data map[string]any) error {
	refuse := func(format string, args ...any) error {
		return fmt.Errorf("%w: an app's agent %s: %s", substrate.ErrForbidden, id, fmt.Sprintf(format, args...))
	}
	approval, ok := s.grant.Agent(id)
	if !ok {
		return refuse("the owner approved no such agent for this app: ask for it with an `agent` authorization detail")
	}
	for _, key := range declaredKeys(data) {
		if !declarableAgentKeys[key] {
			return refuse("data.%s is the owner's to declare", key)
		}
	}
	if p, ok := data["purpose"]; ok && p != "primary" {
		return refuse("data.purpose may only be primary, so the owner sees the agent they approved")
	}
	ask := false
	tools, _ := data["tools"].([]any)
	for i, t := range tools {
		entry, _ := t.(map[string]any)
		fn, _ := entry["function"].(string)
		if len(entry) != 1 || !agentTools[fn] {
			return refuse("data.tools[%d] must be one of %s/query, write or ask, with nothing beside it", i, vocabulary.PackageCore)
		}
		ask = ask || fn == vocabulary.PackageCore+"/ask"
	}
	params, _ := data["params"].(map[string]any)
	if on, set := params["engineGuidance"].(bool); set && !on {
		return refuse("data.params.engineGuidance stays on: its trust boundary is the agent's defense against what it reads")
	}
	perms, _ := data["permissions"].(map[string]any)
	readsBlock, _ := perms["reads"].(map[string]any)
	reads := kindList(readsBlock["kinds"])
	writes := kindList(perms["writes"])
	if len(reads) == 0 {
		return refuse("data.permissions.reads.kinds names nothing")
	}
	budgets, _ := data["budgets"].(map[string]any)
	capCents, err := wholeNumber(budgets["spendCentsPerDay"])
	if err != nil {
		return refuse("data.budgets.spendCentsPerDay %v", err)
	}
	if p := scope.AgentReachProblem(approval, reads, writes, ask, capCents); p != "" {
		return refuse("%s", p)
	}
	return nil
}

// agentsPriced refuses an agent whose provider row does not price its
// model: spend is what threads record, and an unpriced model records 0, so
// its cap would never hold.
func (s *scopedDataset) agentsPriced(ctx context.Context, docs []map[string]any) error {
	for _, doc := range docs {
		if doc["kind"] != vocabulary.CoreKind(vocabulary.DocAgent) {
			continue
		}
		meta, _ := doc["metadata"].(map[string]any)
		id, _ := meta["id"].(string)
		data, _ := doc["data"].(map[string]any)
		provider, _ := data["provider"].(string)
		model, _ := data["model"].(string)
		if provider == "" || model == "" {
			return fmt.Errorf("%w: an app's agent %s names its provider and its model", substrate.ErrForbidden, id)
		}
		rec, err := s.inner.Get(ctx, providerKind, provider)
		switch {
		case errors.Is(err, substrate.ErrNotFound) || (err == nil && rec.DeletedAt != nil):
			return fmt.Errorf("%w: an app's agent %s names the AI provider %s, which this repository does not have", substrate.ErrForbidden, id, provider)
		case err != nil:
			return err
		}
		if !PricesModel(rec.Properties, model) {
			return fmt.Errorf("%w: an app's agent %s runs %s, which the AI provider %s has no price for: its spend would count as nothing and its daily cap would never hold; add the model's price to the provider, or choose a priced model",
				substrate.ErrForbidden, id, model, provider)
		}
	}
	return nil
}

// PricesModel reports whether a provider row's `pricing` names model with
// input and output rates above zero.
func PricesModel(provider map[string]any, model string) bool {
	rows, _ := provider["pricing"].([]any)
	for _, r := range rows {
		entry, _ := r.(map[string]any)
		if entry["model"] != model {
			continue
		}
		in, inOK := positiveRate(entry["inputPer1M"])
		out, outOK := positiveRate(entry["outputPer1M"])
		return inOK && outOK && in > 0 && out > 0
	}
	return false
}

func positiveRate(v any) (float64, bool) {
	if v == nil {
		return 0, false
	}
	f, err := strconv.ParseFloat(fmt.Sprint(v), 64)
	return f, err == nil
}

// kindList reads a grant list: each entry a kind reference, as a string or a
// stored `{ref: …}`.
func kindList(v any) []string {
	var out []string
	for _, e := range declaredList(v) {
		switch t := e.(type) {
		case string:
			out = append(out, t)
		case map[string]any:
			if ref, ok := t["ref"].(string); ok {
				out = append(out, ref)
			} else {
				out = append(out, fmt.Sprint(t))
			}
		default:
			out = append(out, fmt.Sprint(t))
		}
	}
	return out
}

// wholeNumber reads a declared integer: nil when absent.
func wholeNumber(v any) (*int, error) {
	var n float64
	switch t := v.(type) {
	case nil:
		return nil, nil
	case float64:
		n = t
	case int:
		n = float64(t)
	case int64:
		n = float64(t)
	case json.Number:
		f, err := t.Float64()
		if err != nil {
			return nil, errors.New("is not a number")
		}
		n = f
	default:
		return nil, errors.New("is not a number")
	}
	if n != float64(int(n)) {
		return nil, errors.New("is not a whole number of cents")
	}
	i := int(n)
	return &i, nil
}

// declaresAgent reports whether the grant declares the agent whose record id
// is id: the owner approved it and the grant keeps its package.
func (s *scopedDataset) declaresAgent(id string) bool {
	if _, ok := s.grant.Agent(id); !ok {
		return false
	}
	return s.grant.DeclaresIn(vocabulary.KindPackage(id), s.inner.Repository().Authority)
}

// talksTo is the grant's agents rule for one agent, with decision 0207's
// coverage rule on top: an agent an app keeps reads what the owner approved
// for that app, so another token reaches it (chats with it, reads its row,
// its threads, messages and interactions) only when its own read scopes
// cover every kind the agent reads. The agent is then no channel to data the
// caller could not read itself. Substrate Chat's `read:*` covers a coach that
// reads no internal kind; a token that does not cover sees no such agent.
func (s *scopedDataset) talksTo(ctx context.Context, agent string) (bool, error) {
	if !s.grant.TalksTo(agent) {
		return false, nil
	}
	s.mu.Lock()
	known, ok := s.agentCovered[agent]
	s.mu.Unlock()
	if ok {
		return known, nil
	}
	covered, err := s.coversAppAgent(ctx, agent)
	if err != nil {
		return false, err
	}
	s.mu.Lock()
	if s.agentCovered == nil {
		s.agentCovered = map[string]bool{}
	}
	s.agentCovered[agent] = covered
	s.mu.Unlock()
	return covered, nil
}

// coversAppAgent reports whether the grant reads every kind the agent reads,
// true for an agent no app keeps.
func (s *scopedDataset) coversAppAgent(ctx context.Context, agent string) (bool, error) {
	pkg, err := s.inner.Get(ctx, vocabulary.CoreKind(vocabulary.DocPackage), vocabulary.KindPackage(agent))
	switch {
	case errors.Is(err, substrate.ErrNotFound):
		return true, nil
	case err != nil:
		return false, err
	}
	if pkg.DeletedAt != nil || !substrate.PackageOwnerOf(pkg.Properties).App() {
		return true, nil
	}
	rec, err := s.inner.Get(ctx, vocabulary.CoreKind(vocabulary.DocAgent), agent)
	if errors.Is(err, substrate.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	reg, err := s.kinds(ctx)
	if err != nil {
		return false, err
	}
	perms, _ := rec.Properties["permissions"].(map[string]any)
	readsBlock, _ := perms["reads"].(map[string]any)
	for _, k := range kindList(readsBlock["kinds"]) {
		k = strings.TrimPrefix(k, vocabulary.CoreKind(vocabulary.DocKind)+"/")
		info, held := reg[k]
		// A kind the registry lacks reads as internal, which a glob never
		// reaches.
		if !s.grant.Reads(k, !held || internalKind(info)) {
			return false, nil
		}
	}
	return true, nil
}
