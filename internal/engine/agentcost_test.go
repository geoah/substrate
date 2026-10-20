package engine

import (
	"math"
	"testing"

	"github.com/geoah/substrate/internal/llm"
)

// A pricing row's cache rates price the cached parts of the prompt (decision
// 0181): stated rates as stated, absent ones at the default ratios of the
// input rate, a stated "0" as free, and an unpriced model at nothing.
func TestModelPriceChargesCacheReadsAndWritesAtTheirRates(t *testing.T) {
	usage := &llm.Usage{PromptTokens: 100, CompletionTokens: 5, CacheReadTokens: 60, CacheWriteTokens: 20}
	for name, tc := range map[string]struct {
		row   map[string]any
		usage *llm.Usage
		want  float64
	}{
		"explicit rates": {
			row:   map[string]any{"inputPer1M": "1", "outputPer1M": "5", "cacheReadPer1M": "0.2", "cacheWritePer1M": "2"},
			usage: usage, want: (20*1 + 60*0.2 + 20*2 + 5*5) / 1e6,
		},
		"default ratios": {
			row:   map[string]any{"inputPer1M": "1", "outputPer1M": "5"},
			usage: usage, want: (20*1 + 60*0.1 + 20*1.25 + 5*5) / 1e6,
		},
		"an explicit zero is a free cache": {
			row:   map[string]any{"inputPer1M": "1", "outputPer1M": "5", "cacheReadPer1M": "0", "cacheWritePer1M": "0"},
			usage: usage, want: (20*1 + 5*5) / 1e6,
		},
		"an unreadable rate takes the default": {
			row:   map[string]any{"inputPer1M": "1", "outputPer1M": "5", "cacheReadPer1M": "cheap"},
			usage: usage, want: (20*1 + 60*0.1 + 20*1.25 + 5*5) / 1e6,
		},
		"an unpriced model": {
			row:   map[string]any{},
			usage: usage, want: 0,
		},
		"no cache counts is the old formula": {
			row:   map[string]any{"inputPer1M": "1", "outputPer1M": "5"},
			usage: &llm.Usage{PromptTokens: 10, CompletionTokens: 5}, want: (10*1 + 5*5) / 1e6,
		},
		"counts past the prompt clamp the uncached part at zero": {
			row:   map[string]any{"inputPer1M": "1", "outputPer1M": "5"},
			usage: &llm.Usage{PromptTokens: 50, CacheReadTokens: 60}, want: 60 * 0.1 / 1e6,
		},
		"no usage": {
			row:   map[string]any{"inputPer1M": "1", "outputPer1M": "5"},
			usage: nil, want: 0,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := pricingRow(tc.row).cost(tc.usage); math.Abs(got-tc.want) > 1e-12 {
				t.Fatalf("cost = %v, want %v", got, tc.want)
			}
		})
	}
}
