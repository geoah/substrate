package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/geoah/substrate/internal/hostdefaults"
	"github.com/geoah/substrate/internal/llm"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// THE HOST TRANSPORT (decision 0230). A live llm/provider row with an empty
// apiKey whose id the host file lists is served by the host's entry WHOLE:
// wire, base URL, key, headers, defaults, cache, title model, models and
// prices all come from the entry, and nothing the row holds besides its id
// reaches the request. A row with a key of its own is the person's, exactly
// as without a host file. The rule is read in one place, hostEntryFor, so
// dispatch, titles, compaction, embeddings, the bundle setup items and the
// host services read cannot disagree about which rows the host pays for.

// hostEmbedProviderID is the provider id the host's embeddings entry stamps
// its vectors with. It names no row: the semantic query and the reconcile
// compare it as they compare a row id. Its parentheses are outside the record
// id alphabet (vocabulary.ValidID), so no llm/provider row can carry it, and
// moving between the host's entry and a row always changes the pair, whatever
// the row is called and whichever model it names.
const hostEmbedProviderID = "(host)"

// hostEntryFor is the host entry serving the provider row id, given the row's
// stored apiKey value, or nil when the row is the person's: a key of its own,
// or an id the host does not list. The caller has already found the row live.
func (ds *dataset) hostEntryFor(id, storedKey string) *hostdefaults.LLM {
	host := ds.svc.host
	if host == nil || storedKey != "" {
		return nil
	}
	return host.LLM[id]
}

// hostProviderConfig is the providerConfig a host-backed row resolves to:
// the entry's transport, never the row's.
func hostProviderConfig(e *hostdefaults.LLM) *providerConfig {
	pc := &providerConfig{
		id:         e.ID,
		wire:       e.Wire,
		host:       e,
		defaults:   e.Defaults,
		titleModel: e.TitleModel,
		pricing:    map[string]modelPrice{},
		cfg:        llm.Config{BaseURL: e.BaseURL, APIKey: e.APIKey.Value()},
	}
	pc.cfg.Headers = hostHeaders(e)
	if e.Cache != nil {
		pc.cacheTTL = providerCacheTTLs[e.Cache.TTL]
		pc.cfg.CacheKey, pc.cfg.CacheBreakpoints = e.Cache.Key, e.Cache.Breakpoints
	}
	for _, m := range e.Models {
		pc.pricing[m.Model] = hostModelPrice(m)
	}
	return pc
}

// hostModelPrice reads one host model's prices the way a provider row's
// pricing row is read, so an absent cache rate takes the same default ratio.
func hostModelPrice(m hostdefaults.Model) modelPrice {
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

// hostHeaders is an entry's headers as the clients take them; nil for none.
func hostHeaders(e *hostdefaults.LLM) map[string]string {
	if len(e.Headers) == 0 {
		return nil
	}
	out := make(map[string]string, len(e.Headers))
	for _, h := range e.Headers {
		out[h.Name] = h.Value
	}
	return out
}

// checkHostModel refuses a model the host entry does not serve, naming the
// ones it does and the way out: a key of the person's own on the row.
func checkHostModel(pc *providerConfig, who, model string) error {
	if pc.host == nil {
		return nil
	}
	if _, ok := pc.host.Model(model); ok {
		return nil
	}
	return fmt.Errorf("%w: llm/provider row %q runs on the host's key, which serves only %s, and %s names %q: name one of those, or write your own apiKey on the row to use any model",
		substrate.ErrValidation, pc.id, strings.Join(pc.host.ModelIDs(), ", "), who, model)
}

// hostProviderRow reads the provider row id and answers the host entry that
// serves it, or nil: no host file, no live row, a keyed row, or an id the host
// does not list. It opens no secret, so a dispatcher pass can ask it of every
// trigger.
func (ds *dataset) hostProviderRow(ctx context.Context, id string) (*hostdefaults.LLM, error) {
	if ds.svc.host == nil || id == "" {
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
	return ds.hostEntryFor(id, key), nil
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
		// Dispatch refuses it with its own words; nothing is host-paid.
		return "", nil
	}
	return input.Row.ID, nil
}
