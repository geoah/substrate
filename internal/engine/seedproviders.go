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
// cannot invent one. Create-only, including tombstones: an owner who deleted
// a row does not see it come back on the next open, and an owner who edited
// one is not overwritten.
//
// Shipped sample agents name `openai`. The other two rows are the same shape
// for the other vendors, so keying one is a write to that row and nothing else.
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
			"pricing": []any{
				map[string]any{"model": "gpt-5", "inputPer1M": "1.25", "outputPer1M": "10"},
				map[string]any{"model": "gpt-5-mini", "inputPer1M": "0.25", "outputPer1M": "2"},
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
				map[string]any{"model": "claude-opus-5", "inputPer1M": "5", "outputPer1M": "25"},
				map[string]any{"model": "claude-sonnet-5", "inputPer1M": "3", "outputPer1M": "15"},
				map[string]any{"model": "claude-haiku-4-5", "inputPer1M": "1", "outputPer1M": "5"},
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
				map[string]any{"model": "gemini-2.5-pro", "inputPer1M": "1.25", "outputPer1M": "10"},
				map[string]any{"model": "gemini-2.5-flash", "inputPer1M": "0.15", "outputPer1M": "0.60"},
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
		if _, err := t.put(substrate.PutInput{Kind: typeProvider, ID: p.id, Properties: p.props}); err != nil {
			return fmt.Errorf("substrate/engine: seed llm/provider %s: %w", p.id, err)
		}
	}
	return nil
}

// ensureDefaultProviders writes the three vendor rows if this repository
// does not already hold them (live or tombstoned). Creation writes them in
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
		if row == nil {
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
