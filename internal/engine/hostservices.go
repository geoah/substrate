package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"slices"
	"sort"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// hostEmbeddingsLabel is the embeddings service as the person reads it.
const hostEmbeddingsLabel = "Search by meaning"

// HostServices answers GET /api/v1/host/services (docs/plans/host-defaults.md):
// each llm/provider row and the embeddings provider, with where its
// credential comes from and whether it works, resolved by the same reads
// dispatch and the embed drain use (hostEntryFor, resolveProvider,
// resolveEmbedProvider), and the host's budget for the UTC day. Host.Name is
// the API's to fill from site.yaml; the engine does not read the site.
func (ds *dataset) HostServices(ctx context.Context) (substrate.HostServices, error) {
	out := substrate.HostServices{Items: []substrate.HostService{}}
	host := ds.svc.host
	if host != nil {
		used, byService, resets, err := ds.hostSpendToday(ctx)
		if err != nil {
			return out, err
		}
		budget := &substrate.HostBudget{
			CentsPerDay: host.Budget.CentsPerDay, UsedCents: wholeCents(used), ResetsAt: resets.UTC(),
		}
		for _, svc := range slices.Sorted(maps.Keys(byService)) {
			if c := wholeCents(byService[svc]); c > 0 {
				budget.ByService = append(budget.ByService, substrate.HostSpend{Service: svc, Cents: c})
			}
		}
		out.Host = &substrate.HostInfo{Budget: budget}
	}

	rows, err := ds.providerRows(ctx)
	if err != nil {
		return out, err
	}
	failing, err := ds.failingProviders(ctx)
	if err != nil {
		return out, err
	}
	live := map[string]bool{}
	for _, row := range rows {
		live[row.ID] = true
		out.Items = append(out.Items, ds.llmService(ctx, row, failing[row.ID]))
	}
	if host != nil {
		for id := range host.LLM {
			if live[id] {
				continue
			}
			// A host-listed row the person deleted stays deleted (the seed is
			// create-only); the service says how to have it back.
			out.Items = append(out.Items, substrate.HostService{
				ID: "llm/" + id, Service: substrate.HostServiceLLM, Label: id,
				Target: vocabulary.RecordPath(typeProvider, id),
				Source: substrate.HostSourceHost, Readiness: substrate.HostReadyNotSetUp,
				Message: fmt.Sprintf("there is no llm/provider row %s: create it without an apiKey to use the host's", id),
			})
		}
	}
	sort.SliceStable(out.Items, func(i, j int) bool { return out.Items[i].ID < out.Items[j].ID })
	item, err := ds.embeddingsService(ctx)
	if err != nil {
		return out, err
	}
	out.Items = append(out.Items, item)
	return out, nil
}

// wholeCents rounds a fractional spend up, so a budget never reads as less
// used than it is.
func wholeCents(c float64) int64 {
	return int64(math.Ceil(c - hostBudgetEpsilon))
}

// providerRows is every live llm/provider row, by id.
func (ds *dataset) providerRows(ctx context.Context) ([]*erow, error) {
	rows, err := ds.db.QueryContext(ctx,
		`SELECT id FROM records WHERE kind = $1 AND deleted_at IS NULL ORDER BY id`, typeProvider)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: list provider rows: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	out := make([]*erow, 0, len(ids))
	for _, id := range ids {
		row, err := ds.loadRowDB(ctx, eref{Kind: typeProvider, ID: id})
		if err != nil {
			return nil, err
		}
		if row != nil && row.DeletedAt == nil {
			out = append(out, row)
		}
	}
	return out, nil
}

// llmService is one provider row as a service. failing is the newest open
// alert's error of an agent running on the row, empty for none.
func (ds *dataset) llmService(ctx context.Context, row *erow, failing string) substrate.HostService {
	item := substrate.HostService{
		ID: "llm/" + row.ID, Service: substrate.HostServiceLLM, Label: propString(row, "label"),
		Target: vocabulary.RecordPath(typeProvider, row.ID),
	}
	if item.Label == "" {
		item.Label = row.ID
	}
	storedKey, _ := row.Props["apiKey"].(string)
	switch entry := ds.hostEntryFor(row.ID, storedKey); {
	case entry != nil:
		item.Source, item.Readiness, item.Models = substrate.HostSourceHost, substrate.HostReadyReady, entry.ModelIDs()
	case storedKey != "":
		item.Source, item.Readiness = substrate.HostSourceOwn, substrate.HostReadyReady
		if _, err := ds.resolveProvider(ctx, row.ID); err != nil {
			item.Readiness, item.Message = substrate.HostReadyNotSetUp, refusalText(err)
		}
	default:
		item.Source, item.Readiness = substrate.HostSourceNone, substrate.HostReadyNotSetUp
		item.Message = "no apiKey: write your key on the row to use it"
	}
	if item.Readiness == substrate.HostReadyReady && failing != "" {
		item.Readiness, item.Message = substrate.HostReadyFailing, failing
	}
	return item
}

// embeddingsService is the repository's embeddings provider as a service.
func (ds *dataset) embeddingsService(ctx context.Context) (substrate.HostService, error) {
	item := substrate.HostService{ID: substrate.HostServiceEmbeddings, Service: substrate.HostServiceEmbeddings, Label: hostEmbeddingsLabel}
	claims, err := embedClaims(ctx, ds.db)
	if err != nil {
		return item, err
	}
	if len(claims) == 1 {
		item.Target = vocabulary.RecordPath(typeProvider, claims[0].id)
		row, err := ds.loadRowDB(ctx, eref{Kind: typeProvider, ID: claims[0].id})
		if err != nil {
			return item, err
		}
		item.Source = substrate.HostSourceNone
		if row != nil {
			if key, _ := row.Props["apiKey"].(string); key != "" {
				item.Source = substrate.HostSourceOwn
			}
		}
	}
	p, err := ds.resolveEmbedProvider(ctx)
	switch {
	case err != nil:
		if item.Source == "" {
			item.Source = substrate.HostSourceOwn
		}
		item.Readiness, item.Message = substrate.HostReadyNotSetUp, refusalText(err)
	case p == nil:
		item.Source, item.Readiness = substrate.HostSourceNone, substrate.HostReadyNotSetUp
		item.Message = "no llm/provider row declares " + propEmbedModel + ": name a model on a keyed openai-wire row to search by meaning"
	case p.gate != nil:
		item.Source, item.Readiness = substrate.HostSourceHost, substrate.HostReadyReady
	default:
		item.Readiness = substrate.HostReadyReady
	}
	return item, nil
}

// refusalText is an engine refusal without its sentinel's prefix, the words a
// person reads.
func refusalText(err error) string {
	msg := err.Error()
	for _, sentinel := range []error{substrate.ErrValidation, substrate.ErrNotFound, substrate.ErrGuard} {
		msg = strings.TrimPrefix(msg, sentinel.Error()+": ")
	}
	return msg
}

// failingProviders maps a provider row id to the error of the newest open
// alert about an agent that runs on it (decision 0148): an agent's trigger
// parks with the endpoint's refusal, and the alert names the agent beside
// the trigger.
func (ds *dataset) failingProviders(ctx context.Context) (map[string]string, error) {
	rows, err := ds.db.QueryContext(ctx, `
		SELECT props->'about', coalesce(props->>'detail', ''), coalesce(props->>'summary', '')
		  FROM records WHERE kind = $1 AND deleted_at IS NULL AND props->>'state' = $2
		 ORDER BY updated_at DESC, id`, kindAlert, alertStateOpen)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: read open alerts: %w", err)
	}
	type alert struct {
		about           []string
		detail, summary string
	}
	var alerts []alert
	for rows.Next() {
		var raw []byte
		var a alert
		if err := rows.Scan(&raw, &a.detail, &a.summary); err != nil {
			_ = rows.Close()
			return nil, err
		}
		a.about = referencePaths(raw)
		alerts = append(alerts, a)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	out := map[string]string{}
	reg := ds.registry()
	for _, a := range alerts {
		for _, path := range a.about {
			kind, id, ok := vocabulary.SplitRecordPath(path)
			if !ok || kind != kindAgent {
				continue
			}
			ag, err := reg.ResolveAgent(id)
			if err != nil {
				continue
			}
			pid, err := ds.agentProviderID(ctx, ag)
			if err != nil {
				return nil, err
			}
			if pid == "" || out[pid] != "" {
				continue
			}
			out[pid] = a.detail
			if out[pid] == "" {
				out[pid] = a.summary
			}
		}
	}
	return out, nil
}

// referencePaths reads a repeated reference's stored value: each element a
// `{ref: path}` object or, in older rows, the bare path.
func referencePaths(raw []byte) []string {
	var items []any
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		switch v := it.(type) {
		case string:
			out = append(out, v)
		case map[string]any:
			if p, ok := v[vocabulary.ReferenceValueKey].(string); ok {
				out = append(out, p)
			}
		}
	}
	return out
}
