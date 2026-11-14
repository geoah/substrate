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

	"github.com/geoah/substrate/internal/operatordefaults"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// embeddingsLabel is the embeddings slot as the person reads it.
const embeddingsLabel = "Search by meaning"

// OperatorDefaults answers GET /api/v1/operator/defaults
// (docs/plans/host-defaults.md): each llm/provider row, the embeddings
// provider, each provider the file lists under oauthClients and each package
// it lists under functionSecrets, with where its key comes from, whether the
// operator's file offers it and whether it works, resolved by the same reads
// dispatch, the embed drain, client resolution and function injection use
// (operatorEntryFor, resolveProvider, resolveEmbedProvider, oauthClientFrom,
// operatorFunctionEntry), and today's allowance. The operator's name is the site's, as the service
// was given it.
func (ds *dataset) OperatorDefaults(ctx context.Context) (substrate.OperatorDefaults, error) {
	out := substrate.OperatorDefaults{Items: []substrate.OperatorDefault{}}
	op := ds.svc.operatorDefaults
	if op != nil {
		used, bySlot, resets, err := ds.operatorSpendToday(ctx)
		if err != nil {
			return out, err
		}
		allowance := &substrate.OperatorAllowance{
			CentsPerDay: op.Budget.CentsPerDay, UsedCents: wholeCents(used), ResetsAt: resets.UTC(),
		}
		for _, slot := range slices.Sorted(maps.Keys(bySlot)) {
			if c := wholeCents(bySlot[slot]); c > 0 {
				allowance.BySlot = append(allowance.BySlot, substrate.OperatorSpend{Slot: slot, Cents: c})
			}
		}
		out.Operator = &substrate.OperatorInfo{Name: ds.svc.operatorName, Allowance: allowance}
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
		out.Items = append(out.Items, ds.llmDefault(ctx, row, failing[row.ID]))
	}
	if op != nil {
		for id := range op.LLM {
			if live[id] {
				continue
			}
			// An operator-listed row the person deleted stays deleted (the
			// seed is create-only): no key resolves, and the item says how to
			// have it back.
			out.Items = append(out.Items, substrate.OperatorDefault{
				ID: "llm/" + id, Slot: substrate.SlotLLM, Label: id,
				Target: vocabulary.RecordPath(typeProvider, id),
				Source: substrate.SourceNone, OperatorOffers: true, Readiness: substrate.ReadinessNotSetUp,
				Message: fmt.Sprintf("there is no llm/provider row %s: create it without an apiKey to use %s's key", id, ds.siteName()),
			})
		}
	}
	sort.SliceStable(out.Items, func(i, j int) bool { return out.Items[i].ID < out.Items[j].ID })
	item, err := ds.embeddingsDefault(ctx, live)
	if err != nil {
		return out, err
	}
	out.Items = append(out.Items, item)
	oauth, err := ds.oauthDefaults(ctx)
	if err != nil {
		return out, err
	}
	out.Items = append(out.Items, oauth...)
	functions, err := ds.functionDefaults(ctx)
	if err != nil {
		return out, err
	}
	out.Items = append(out.Items, functions...)
	return out, nil
}

// wholeCents rounds a fractional spend up, so an allowance never reads as
// less used than it is.
func wholeCents(c float64) int64 {
	return int64(math.Ceil(c - allowanceEpsilon))
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

// llmDefault is one provider row's slot. A key the vendor refused reads
// `failing` with the vendor and the status (keyrefusals.go); otherwise failing
// is the newest open alert's error of an agent running on the row, empty for
// none.
func (ds *dataset) llmDefault(ctx context.Context, row *erow, failing string) substrate.OperatorDefault {
	item := substrate.OperatorDefault{
		ID: "llm/" + row.ID, Slot: substrate.SlotLLM, Label: propString(row, "label"),
		Target:         vocabulary.RecordPath(typeProvider, row.ID),
		OperatorOffers: ds.operatorOffersLLM(row.ID),
	}
	if item.Label == "" {
		item.Label = row.ID
	}
	storedKey, _ := row.Props["apiKey"].(string)
	switch entry := ds.operatorEntryFor(row.ID, storedKey); {
	case entry != nil:
		item.Source, item.Readiness, item.Models = substrate.SourceOperator, substrate.ReadinessReady, entry.ModelIDs()
		if status := ds.keyStatus(operatorKeyID(entry.ID)); status != 0 {
			failing = keyRefusedText(vendorName(propString(row, "label"), row.ID), status)
		}
	case storedKey != "":
		item.Source, item.Readiness = substrate.SourceOwn, substrate.ReadinessReady
		if _, err := ds.resolveProvider(ctx, row.ID); err != nil {
			item.Readiness, item.Message = substrate.ReadinessNotSetUp, refusalText(err)
		} else if status := ds.keyStatus(ownKeyID(ds.info.ID, row.ID, storedKey)); status != 0 {
			failing = keyRefusedText(vendorName(propString(row, "label"), row.ID), status)
		}
	default:
		item.Source, item.Readiness = substrate.SourceNone, substrate.ReadinessNotSetUp
		item.Message = "no apiKey: add your own key to the row to use it"
	}
	if item.Readiness == substrate.ReadinessReady && failing != "" {
		item.Readiness, item.Message = substrate.ReadinessFailing, failing
	}
	return item
}

// embeddingsDefault is the repository's embeddings provider as a slot. Its
// target is the row whose card shows the search line: the one row claiming
// embedModel, else the live row whose id the operator's embeddings entry is
// nested under, else none. live is every live provider row id. A key the
// vendor refused reads `failing` with the vendor and the status.
func (ds *dataset) embeddingsDefault(ctx context.Context, live map[string]bool) (substrate.OperatorDefault, error) {
	entry := ds.svc.operatorDefaults.EmbeddingsEntry()
	item := substrate.OperatorDefault{
		ID: substrate.SlotEmbeddings, Slot: substrate.SlotEmbeddings, Label: embeddingsLabel,
		OperatorOffers: entry != nil,
	}
	claims, err := embedClaims(ctx, ds.db)
	if err != nil {
		return item, err
	}
	switch {
	case len(claims) == 1:
		item.Target = vocabulary.RecordPath(typeProvider, claims[0].id)
		row, err := ds.loadRowDB(ctx, eref{Kind: typeProvider, ID: claims[0].id})
		if err != nil {
			return item, err
		}
		item.Source = substrate.SourceNone
		if row != nil {
			if key, _ := row.Props["apiKey"].(string); key != "" {
				item.Source = substrate.SourceOwn
			}
		}
	case len(claims) == 0 && entry != nil && live[entry.ID]:
		item.Target = vocabulary.RecordPath(typeProvider, entry.ID)
	}
	p, err := ds.resolveEmbedProvider(ctx)
	switch {
	case err != nil:
		if item.Source == "" {
			item.Source = substrate.SourceOwn
		}
		item.Readiness, item.Message = substrate.ReadinessNotSetUp, refusalText(err)
	case p == nil:
		item.Source, item.Readiness = substrate.SourceNone, substrate.ReadinessNotSetUp
		item.Message = "no llm/provider row declares " + propEmbedModel + ": name a model on a keyed openai-wire row to search by meaning"
	case p.gate != nil:
		item.Source, item.Readiness = substrate.SourceOperator, substrate.ReadinessReady
	default:
		item.Readiness = substrate.ReadinessReady
	}
	if p != nil && err == nil {
		if status := ds.keyStatus(p.key); status != 0 {
			vendor, verr := ds.rowVendorName(ctx, entryOrClaim(entry, claims))
			if verr != nil {
				return item, verr
			}
			item.Readiness, item.Message = substrate.ReadinessFailing, keyRefusedText(vendor, status)
		}
	}
	return item, nil
}

// entryOrClaim is the provider row id whose key buys the repository's
// vectors: the one claiming row, else the operator entry's id.
func entryOrClaim(entry *operatordefaults.LLM, claims []embedClaim) string {
	if len(claims) == 1 {
		return claims[0].id
	}
	if entry != nil {
		return entry.ID
	}
	return ""
}

// rowVendorName is the vendor of the llm/provider row id as a person reads it
// (vendorName), from its label where the row is live.
func (ds *dataset) rowVendorName(ctx context.Context, id string) (string, error) {
	row, err := ds.loadRowDB(ctx, eref{Kind: typeProvider, ID: id})
	if err != nil {
		return "", err
	}
	label := ""
	if row != nil && row.DeletedAt == nil {
		label = propString(row, "label")
	}
	return vendorName(label, id), nil
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
