package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/geoah/substrate/internal/llm"
	"github.com/geoah/substrate/internal/operatordefaults"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// THE OPERATOR TRANSPORT (decision 0232). A live llm/provider row with an
// empty apiKey whose id the operator's defaults file lists is served by the
// operator's entry WHOLE: wire, base URL, key, headers, defaults, cache, title
// model, models and prices all come from the entry, and nothing the row holds
// besides its id reaches the request. A row with a key of its own is the
// person's, exactly as without a defaults file. The rule is read in one
// place, operatorEntryFor, so dispatch, titles, compaction, the allowance hold
// and the operator defaults read cannot disagree about which rows the
// operator pays for.

// operatorEmbedProviderID is the provider id the operator's embeddings entry
// stamps its vectors with. It names no row: the semantic query and the
// reconcile compare it as they compare a row id. Its parentheses are outside
// the record id alphabet (vocabulary.ValidID), so no llm/provider row can
// carry it, and moving between the operator's entry and a row always changes
// the pair, whatever the row is called and whichever model it names.
const operatorEmbedProviderID = "(operator)"

// operatorEntryFor is the operator entry serving the provider row id, given
// the row's stored apiKey value, or nil when the row is the person's: a key
// of its own, or an id the operator does not list. The caller has already
// found the row live.
func (ds *dataset) operatorEntryFor(id, storedKey string) *operatordefaults.LLM {
	op := ds.svc.operatorDefaults
	if op == nil || storedKey != "" {
		return nil
	}
	return op.LLM[id]
}

// operatorOffersLLM reports whether the operator's file lists the provider
// row id, whatever the row holds: removing the person's own key would fall
// back to the operator's.
func (ds *dataset) operatorOffersLLM(id string) bool {
	op := ds.svc.operatorDefaults
	return op != nil && op.LLM[id] != nil
}

// siteName is the operator as a refusal names it: the site's name, or "this
// server" where the deployment has none.
func (ds *dataset) siteName() string {
	if ds.svc.operatorName != "" {
		return ds.svc.operatorName
	}
	return "this server"
}

// operatorProviderConfig is the providerConfig an operator-served row
// resolves to: the entry's transport, never the row's. Its client retries no
// attempt that may have been billed, because each call reserves one
// attempt's ceiling.
func operatorProviderConfig(e *operatordefaults.LLM) *providerConfig {
	pc := &providerConfig{
		id:         e.ID,
		wire:       e.Wire,
		operator:   e,
		defaults:   e.Defaults,
		titleModel: e.TitleModel,
		pricing:    map[string]modelPrice{},
		cfg:        llm.Config{BaseURL: e.BaseURL, APIKey: e.APIKey.Value(), RetryOnlyUnbilled: true},
	}
	pc.cfg.Headers = operatorHeaders(e)
	if e.Cache != nil {
		pc.cacheTTL = providerCacheTTLs[e.Cache.TTL]
		pc.cfg.CacheKey, pc.cfg.CacheBreakpoints = e.Cache.Key, e.Cache.Breakpoints
	}
	for _, m := range e.Models {
		pc.pricing[m.Model] = operatorModelPrice(m)
	}
	return pc
}

// operatorModelPrice reads one operator model's prices the way a provider
// row's pricing row is read, so an absent cache rate takes the same default
// ratio.
func operatorModelPrice(m operatordefaults.Model) modelPrice {
	entry := map[string]any{
		"inputPer1M": m.InputPer1M, "outputPer1M": m.OutputPer1M,
		"contextWindow": float64(m.ContextWindow),
	}
	if m.CacheReadPer1M != "" {
		entry["cacheReadPer1M"] = m.CacheReadPer1M
	}
	if m.CacheWritePer1M != "" {
		entry["cacheWritePer1M"] = m.CacheWritePer1M
	}
	return pricingRow(entry)
}

// operatorHeaders is an entry's headers as the clients take them; nil for
// none.
func operatorHeaders(e *operatordefaults.LLM) map[string]string {
	if len(e.Headers) == 0 {
		return nil
	}
	out := make(map[string]string, len(e.Headers))
	for _, h := range e.Headers {
		out[h.Name] = h.Value
	}
	return out
}

// checkOperatorModel refuses a model the operator's entry does not serve,
// naming the ones it does and the way out: a key of the person's own on the
// row.
func (ds *dataset) checkOperatorModel(pc *providerConfig, who, model string) error {
	if pc.operator == nil {
		return nil
	}
	if _, ok := pc.operator.Model(model); ok {
		return nil
	}
	return fmt.Errorf("%w: llm/provider row %q runs on %s's key, which serves only %s, and %s names %q: name one of those, or add your own key to the row to use any model",
		substrate.ErrValidation, pc.id, ds.siteName(), strings.Join(pc.operator.ModelIDs(), ", "), who, model)
}

// operatorProviderRow reads the provider row id and answers the operator
// entry that serves it, or nil: no defaults file, no live row, a keyed row,
// or an id the operator does not list. It opens no secret, so a dispatcher
// pass can ask it of every trigger.
func (ds *dataset) operatorProviderRow(ctx context.Context, id string) (*operatordefaults.LLM, error) {
	if ds.svc.operatorDefaults == nil || id == "" {
		return nil, nil
	}
	row, err := ds.loadRowDB(ctx, eref{Kind: typeProvider, ID: id})
	if err != nil {
		return nil, err
	}
	if row == nil || row.DeletedAt != nil || row.Kind != typeProvider {
		return nil, nil
	}
	key, _ := row.Props["apiKey"].(string)
	return ds.operatorEntryFor(id, key), nil
}

// agentProviderID is the provider row an agent's run resolves: its own
// `provider`, or the row its bundle input binds.
func (ds *dataset) agentProviderID(ctx context.Context, ag *vocabulary.Agent) (string, error) {
	if ag.ProviderInput == "" {
		return ag.Provider, nil
	}
	b, ok := ds.registry().BundleOf(ag.Package)
	if !ok {
		return "", nil
	}
	input, err := ds.resolveBundleInput(ctx, b, ag.ProviderInput)
	if err != nil || input.Row == nil {
		// Dispatch refuses it with its own words; nothing is operator-paid.
		return "", nil
	}
	return input.Row.ID, nil
}
