package engine

import (
	"context"
	"fmt"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/samples"
)

// defaultLLMProviders are the three vendor rows a repository is born with,
// keyless. Dispatch refuses a row until its owner writes apiKey; the seed
// cannot invent one. Create-only, and an absent row is written only where no
// hand but the seed's and the engine's ever wrote it (anotherHandWrote): an
// owner who deleted a row does not see it come back on the next open, after
// the sweep collected the tombstone too, and an owner who edited one is not
// overwritten.
//
// Shipped sample agents name `openai`. The other two rows are the same shape
// for the other vendors, so keying one is a write to that row and nothing else.
//
// Each pricing entry carries the model's contextWindow, which is what lets a
// thread on it compact. Being create-only, the seed does not add it to a row
// an older binary wrote; the owner patches that row.
//
// The rows are samples/llm/providers.yaml's, and
// TestSeededProviderRowsMatchTheShippedSample holds the two equal.
var defaultLLMProviders = []struct {
	id    string
	props map[string]any
}{
	{
		id: "openai",
		props: map[string]any{
			"label":   "openai",
			"wire":    "openai",
			"baseURL": "https://api.openai.com/v1",
			// No reasoningEffort default: the accepted set is the MODEL's, not
			// the row's, and one row serves several. gpt-5 takes minimal..high
			// and refuses "none"; the gpt-5.6 family takes "none" and needs it
			// to put function tools on a chat completion. An agent names the
			// value its own model accepts.
			//
			// OpenAI's own endpoint takes prompt_cache_key; the gemini row's
			// endpoint is not held to, so it is left out there.
			"cache": map[string]any{"key": true},
			"pricing": []any{
				map[string]any{"model": "gpt-5", "inputPer1M": "1.25", "outputPer1M": "10", "contextWindow": 400000},
				map[string]any{"model": "gpt-5-mini", "inputPer1M": "0.25", "outputPer1M": "2", "contextWindow": 400000},
			},
		},
	},
	{
		id: "anthropic",
		props: map[string]any{
			"label": "anthropic",
			"wire":  "anthropic",
			// The SDK resolves "v1/messages" against this, so the trailing
			// slash is load-bearing.
			"baseURL": "https://api.anthropic.com/",
			"pricing": []any{
				map[string]any{"model": "claude-opus-5-5", "inputPer1M": "4", "outputPer1M": "20", "cacheReadPer1M": "0.20", "contextWindow": 200000},
				map[string]any{"model": "claude-opus-5", "inputPer1M": "5", "outputPer1M": "25", "contextWindow": 200000},
				map[string]any{"model": "claude-sonnet-5-5", "inputPer1M": "2", "outputPer1M": "10", "cacheReadPer1M": "0.20", "contextWindow": 200000},
				map[string]any{"model": "claude-sonnet-5", "inputPer1M": "2", "outputPer1M": "10", "cacheReadPer1M": "0.20", "contextWindow": 200000},
				map[string]any{"model": "claude-haiku-5-5", "inputPer1M": "0.10", "outputPer1M": "0.50", "cacheReadPer1M": "0.01", "contextWindow": 100000},
				map[string]any{"model": "claude-haiku-4-5", "inputPer1M": "1", "outputPer1M": "5", "cacheReadPer1M": "0.10", "contextWindow": 200000},
			},
		},
	},
	{
		id: "gemini",
		props: map[string]any{
			"label":   "gemini",
			"wire":    "openai",
			"baseURL": "https://generativelanguage.googleapis.com/v1beta/openai",
			"pricing": []any{
				map[string]any{"model": "gemini-2.5-pro", "inputPer1M": "1.25", "outputPer1M": "10", "contextWindow": 1048576},
				map[string]any{"model": "gemini-2.5-flash", "inputPer1M": "0.15", "outputPer1M": "0.60", "contextWindow": 1048576},
			},
		},
	},
}

func (t *txn) seedDefaultProviders() error {
	if !t.ds.svc.seedLLMProviders {
		return nil
	}
	if _, err := t.resolveType(typeProvider); err != nil {
		// A binary whose tree does not declare the kind has nothing to seed.
		return nil
	}
	for _, p := range defaultLLMProviders {
		existing, err := t.loadRow(eref{Kind: typeProvider, ID: p.id}, true)
		if err != nil {
			return err
		}
		if existing != nil {
			continue
		}
		held, err := t.anotherHandWrote(eref{Kind: typeProvider, ID: p.id}, substrate.ActorSystem)
		if err != nil {
			return err
		}
		if held {
			continue
		}
		if _, err := t.put(substrate.PutInput{Kind: typeProvider, ID: p.id, Properties: p.props}); err != nil {
			return fmt.Errorf("substrate/engine: seed llm/provider %s: %w", p.id, err)
		}
	}
	return nil
}

// ensureDefaultProviders writes the three vendor rows if this repository
// does not already hold them (live or tombstoned) and no other hand wrote
// them (seedDefaultProviders decides, under the lock). Creation writes them in
// the seed transaction; this is the catch-up for a repository born before
// the seed existed.
func (ds *dataset) ensureDefaultProviders(ctx context.Context) error {
	if ds.svc.readOnly || !ds.svc.seedLLMProviders {
		return nil
	}
	for _, p := range defaultLLMProviders {
		row, err := ds.loadRowDB(ctx, eref{Kind: typeProvider, ID: p.id})
		if err != nil {
			return err
		}
		if row != nil {
			continue
		}
		// Read before the transaction, so an open after the sweep collected
		// a deleted row takes no changelog lock to find nothing to write.
		var held bool
		if err := ds.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM changelog WHERE record_id = $1 AND kind = $2 AND actor <> ALL($3))`,
			p.id, typeProvider, shippedHands(substrate.ActorSystem)).Scan(&held); err != nil {
			return err
		}
		if !held {
			return ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
				return t.seedDefaultProviders()
			})
		}
	}
	return nil
}

// importLLMSample imports the shipped LLM example onto the repository's own
// authority, the same admission a later Registry import would take. Tests
// skip it (OpenForTest) so the suite does not pay a vocabulary apply on
// every repository; production Open leaves it on.
func (s *service) importLLMSample(ctx context.Context, ds *dataset) error {
	if !s.seedLLMSample || s.readOnly {
		return nil
	}
	cat, err := s.sampleCatalog()
	if err != nil {
		return err
	}
	return cat.SeedImport(ctx, ds, samples.LLM)
}

func (s *service) sampleCatalog() (*catalog.Catalog, error) {
	s.sampleCatOnce.Do(func() {
		s.sampleCat, s.sampleCatErr = catalog.Load(catalog.SampleRoot(samples.Samples()))
	})
	return s.sampleCat, s.sampleCatErr
}
