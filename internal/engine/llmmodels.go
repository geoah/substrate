package engine

// THE MODELS A PROVIDER ROW OFFERS (decision record 0242): llm/model records,
// one per row and model, read beside the row's own deprecated `pricing`
// entries, and the choice an agent on `runsOn` makes among them.
//
// A row's own entry wins for the model it names, exactly as it priced before
// records existed, so no price stored today changes; a record prices a model
// no entry names, and only with both base rates. The context window resolves
// on its own: the entry's where it states one, else the record's. A row the
// operator serves reads neither (decision 0235).

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const typeModel = "substrate.reamde.dev/llm/model"

// modelRecord is one live llm/model record as dispatch reads it.
type modelRecord struct {
	id, provider, model string
	// price is set only where the record's pricing states both base rates: a
	// record without them describes a model without pricing it.
	price  *modelPrice
	window int
}

func readModelRecord(row *erow) modelRecord {
	r := modelRecord{id: row.ID, provider: vocabulary.ReferentID(row.Props["provider"], typeProvider)}
	r.model, _ = row.Props["model"].(string)
	if w, ok := anyFloat(row.Props["contextWindow"]); ok && w > 0 {
		r.window = int(w)
	}
	if pricing, ok := row.Props["pricing"].(map[string]any); ok {
		_, in := priceFloat(pricing["inputPer1M"])
		_, out := priceFloat(pricing["outputPer1M"])
		if in && out {
			p := pricingRow(pricing)
			p.contextWindow = r.window
			r.price = &p
		}
	}
	return r
}

// modelRecordsOf answers the live llm/model records that reference row id,
// in id order.
func modelRecordsOf(ctx context.Context, q dbx, id string) ([]modelRecord, error) {
	return queryModelRecords(ctx, q, ` AND `+referencePathSQL("props", "provider")+` = $2`,
		typeModel, vocabulary.RecordPath(typeProvider, id))
}

func queryModelRecords(ctx context.Context, q dbx, where string, args ...any) ([]modelRecord, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT `+recordCols+` FROM records WHERE kind = $1 AND deleted_at IS NULL`+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: read the llm/model records: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []modelRecord
	for rows.Next() {
		row, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, readModelRecord(row))
	}
	return out, rows.Err()
}

// priceFromRecords fills in what the row's own entries leave open: entries
// names the models an entry priced. A record prices a model no entry names
// and gives a window to an entry that states none. The write keeps one record
// per row and model; a duplicate that predates it loses to the lower id.
func (pc *providerConfig) priceFromRecords(records []modelRecord, entries map[string]bool) {
	seen := map[string]bool{}
	for _, r := range records {
		if r.model == "" || seen[r.model] {
			continue
		}
		seen[r.model] = true
		if entries[r.model] {
			if p := pc.pricing[r.model]; p.contextWindow <= 0 && r.window > 0 {
				p.contextWindow = r.window
				pc.pricing[r.model] = p
			}
			continue
		}
		if r.price != nil {
			pc.pricing[r.model] = *r.price
			continue
		}
		if r.window > 0 {
			if pc.windows == nil {
				pc.windows = map[string]int{}
			}
			pc.windows[r.model] = r.window
		}
	}
}

// contextWindow is the window a thread on model compacts against: its
// price's, else an unpriced record's. Zero never compacts.
func (pc *providerConfig) contextWindow(model string) int {
	if p, ok := pc.pricing[model]; ok && p.contextWindow > 0 {
		return p.contextWindow
	}
	return pc.windows[model]
}

// --- one record per row and model -------------------------------------------------

// modelPairHolderSQL finds the live llm/model record, other than $2, that
// already names row path $3 and model $4. The pair is compared as stored and
// never read out of an id: row ids and model ids may both hold a colon.
var modelPairHolderSQL = `SELECT id FROM records
	WHERE kind = $1 AND id <> $2 AND deleted_at IS NULL
	  AND ` + referencePathSQL("props", "provider") + ` = $3 AND props->>'model' = $4
	ORDER BY id LIMIT 1`

func modelPairHolder(ctx context.Context, q dbx, id string, props map[string]any) (string, error) {
	provider := storedReferencePath(props["provider"])
	model, _ := props["model"].(string)
	if provider == "" || model == "" {
		// The kind requires both; its own refusal names the missing one.
		return "", nil
	}
	var other string
	err := q.QueryRowContext(ctx, modelPairHolderSQL, typeModel, id, provider, model).Scan(&other)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("substrate/engine: check the llm/model pair: %w", err)
	}
	return other, nil
}

// admitModelRow holds an llm/model record to one record per row and model,
// at the write: a price is a fact about one model on one endpoint, and two
// records would answer it twice. The lock makes it a claim, as the
// embeddings claim is (admitProviderRow): two concurrent writes cannot both
// find no holder and both land.
func (t *txn) admitModelRow(id string, props map[string]any) error {
	if err := t.lockKey("llmmodel"); err != nil {
		return err
	}
	other, err := modelPairHolder(t.ctx, t.tx, id, props)
	if err != nil || other == "" {
		return err
	}
	model, _ := props["model"].(string)
	return fmt.Errorf("%w: llm/model %q already describes %q on llm/provider row %q — one record per row and model, so change that one",
		substrate.ErrValidation, other, model, vocabulary.ReferentID(props["provider"], typeProvider))
}

// --- what an agent on runsOn runs on ------------------------------------------------

// agentChoice is what one run resolves to before anything is sent: the
// provider row, the model id, and for an agent on runsOn the llm/model record
// it chose.
type agentChoice struct {
	provider, model, entry string
}

// chooseRunsOn answers what a run of an agent on runsOn runs on, opening no
// secret, so the allowance hold and the operator diagnostics can ask it of
// every agent and dispatch asks it once before opening the row it names.
//
// A new thread takes the first entry available now. A thread that already
// ran stays on the model its stamps name: its history holds that model's
// reasoning, which the anthropic wire sends back unchanged, and the thread
// row says which model it is on. The entries are preferences, not retries.
func (ds *dataset) chooseRunsOn(ctx context.Context, ag *vocabulary.Agent, threadID string) (agentChoice, error) {
	if threadID != "" {
		thread, err := ds.loadRowDB(ctx, eref{Kind: typeThread, ID: threadID})
		if err != nil {
			return agentChoice{}, err
		}
		// A thread that does not resolve is the claim's to refuse, in its
		// own words; until then it is chosen for as a new one.
		if thread != nil && thread.DeletedAt == nil {
			return ds.continuedRunsOn(ctx, ag, thread)
		}
	}
	reasons := make([]string, 0, len(ag.RunsOn))
	for _, id := range ag.RunsOn {
		choice, reason, err := ds.runsOnEntry(ctx, id)
		if err != nil {
			return agentChoice{}, err
		}
		if reason == "" {
			return choice, nil
		}
		reasons = append(reasons, reason)
	}
	return agentChoice{}, fmt.Errorf("%w: agent %s has no model it can run on now: %s",
		substrate.ErrValidation, ag.Identity(), strings.Join(reasons, "; "))
}

// runsOnEntry checks one entry as a new thread takes it. The refusals it
// answers as a reason (no record, no row, no key, a model the operator does
// not serve) are the only ones that move to the next entry; an error stops
// the walk.
func (ds *dataset) runsOnEntry(ctx context.Context, id string) (agentChoice, string, error) {
	row, err := ds.loadRowDB(ctx, eref{Kind: typeModel, ID: id})
	if err != nil {
		return agentChoice{}, "", err
	}
	if row == nil || row.DeletedAt != nil || row.Kind != typeModel {
		return agentChoice{}, fmt.Sprintf("llm/model %q does not resolve", id), nil
	}
	rec := readModelRecord(row)
	choice := agentChoice{provider: rec.provider, model: rec.model, entry: id}
	reason, err := ds.modelAvailable(ctx, rec.provider, rec.model)
	if reason != "" {
		reason = fmt.Sprintf("llm/model %q: %s", id, reason)
	}
	return choice, reason, err
}

// continuedRunsOn is the choice for a thread that already ran: the entry
// naming the row and model the thread is stamped with, if it is still listed
// and available, and a refusal naming it otherwise.
func (ds *dataset) continuedRunsOn(ctx context.Context, ag *vocabulary.Agent, thread *erow) (agentChoice, error) {
	provider := vocabulary.ReferentID(thread.Props["provider"], typeProvider)
	model, _ := thread.Props["model"].(string)
	for _, id := range ag.RunsOn {
		row, err := ds.loadRowDB(ctx, eref{Kind: typeModel, ID: id})
		if err != nil {
			return agentChoice{}, err
		}
		if row == nil || row.DeletedAt != nil {
			continue
		}
		if rec := readModelRecord(row); rec.provider != provider || rec.model != model {
			continue
		}
		reason, err := ds.modelAvailable(ctx, provider, model)
		if err != nil {
			return agentChoice{}, err
		}
		if reason != "" {
			return agentChoice{}, fmt.Errorf("%w: thread %s runs on %q at llm/provider row %q, which cannot run now (%s); start a new thread to run on another model",
				substrate.ErrValidation, thread.ID, model, provider, reason)
		}
		return agentChoice{provider: provider, model: model, entry: id}, nil
	}
	return agentChoice{}, fmt.Errorf("%w: thread %s runs on %q at llm/provider row %q, which agent %s no longer runs on; start a new thread to run on another model",
		substrate.ErrValidation, thread.ID, model, provider, ag.Identity())
}

// modelAvailable answers "" when a call for model on row id can be sent: the
// row is live and carries its own key, or the operator serves it and lists
// the model. Otherwise it says why. It opens no secret, and a key the vendor
// refused is no reason: that is the vendor's answer, which stops a run rather
// than moving it.
func (ds *dataset) modelAvailable(ctx context.Context, id, model string) (string, error) {
	row, err := ds.loadRowDB(ctx, eref{Kind: typeProvider, ID: id})
	if err != nil {
		return "", err
	}
	if row == nil || row.DeletedAt != nil || row.Kind != typeProvider {
		return fmt.Sprintf("llm/provider row %q does not resolve", id), nil
	}
	key, _ := row.Props["apiKey"].(string)
	if key != "" {
		return "", nil
	}
	entry := ds.operatorEntryFor(id, key)
	if entry == nil {
		return fmt.Sprintf("llm/provider row %q has no key", id), nil
	}
	if _, ok := entry.Model(model); !ok {
		return fmt.Sprintf("llm/provider row %q runs on %s's key, which serves only %s",
			id, ds.siteName(), strings.Join(entry.ModelIDs(), ", ")), nil
	}
	return "", nil
}

// --- what each row offers -----------------------------------------------------------

// ModelOffers answers, for each live llm/provider row, the model ids it can
// run priced: the operator's list where the operator serves the row, else
// the models its own entries name and its llm/model records price. App
// approval offers a provider input only the rows that price every one of
// its agents' models.
func (ds *dataset) ModelOffers(ctx context.Context) (map[string][]string, error) {
	rows, err := ds.db.QueryContext(ctx,
		`SELECT `+recordCols+` FROM records WHERE kind = $1 AND deleted_at IS NULL ORDER BY id`, typeProvider)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: read the llm/provider rows: %w", err)
	}
	var providers []*erow
	for rows.Next() {
		row, err := scanRecord(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		providers = append(providers, row)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	// Closed before the next read takes a connection of its own.
	if err := rows.Close(); err != nil {
		return nil, err
	}
	records, err := queryModelRecords(ctx, ds.db, "", typeModel)
	if err != nil {
		return nil, err
	}
	byRow := map[string][]modelRecord{}
	for _, r := range records {
		byRow[r.provider] = append(byRow[r.provider], r)
	}
	out := map[string][]string{}
	for _, row := range providers {
		key, _ := row.Props["apiKey"].(string)
		if entry := ds.operatorEntryFor(row.ID, key); entry != nil {
			out[row.ID] = slices.Sorted(slices.Values(entry.ModelIDs()))
			continue
		}
		pc := &providerConfig{pricing: map[string]modelPrice{}}
		entries := map[string]bool{}
		for _, entry := range objectRows(row.Props["pricing"]) {
			if model, _ := entry["model"].(string); model != "" {
				pc.pricing[model] = pricingRow(entry)
				entries[model] = true
			}
		}
		pc.priceFromRecords(byRow[row.ID], entries)
		models := make([]string, 0, len(pc.pricing))
		for model := range pc.pricing {
			models = append(models, model)
		}
		slices.Sort(models)
		out[row.ID] = models
	}
	return out, nil
}
