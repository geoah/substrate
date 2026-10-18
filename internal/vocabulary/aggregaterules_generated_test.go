package vocabulary_test

// THE CONSOLE'S COPY OF THE AGGREGATE READ'S RULES, written from them.
//
// The console offers the reducers each form admits, says which summary
// statistics add up and sizes its reads to the budgets
// (web/console/src/lib/aggregate.rules.json), and names the rollup tiers a
// metric may keep. This test writes that file from the tables the server
// enforces, so the two cannot drift: a change here fails it until
//
//	go test ./internal/vocabulary/ -run TestConsoleAggregateRulesMatchTheServer -update
//
// rewrites it. Do not edit it by hand.

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const consoleAggregateRules = "../../web/console/src/lib/aggregate.rules.json"

type consoleBudget struct {
	Default int64 `json:"default"`
	Cap     int64 `json:"cap"`
}

type consoleAggregateRulesFile struct {
	// Reducers is FormReducers: the reducers each form admits.
	Reducers map[string][]string `json:"reducers"`
	// AdditiveStatistics are the summary statistics sum adds.
	AdditiveStatistics []string `json:"additiveStatistics"`
	// Budget is each budget's default and the most a request may raise it to.
	Budget map[string]consoleBudget `json:"budget"`
	// MaxMetrics is the most metrics one read names.
	MaxMetrics int `json:"maxMetrics"`
	// RollupTiers are the tier widths a metric's `rollups` may list, finest
	// first, and DefaultRollups the ones it keeps when it lists none.
	RollupTiers    []string `json:"rollupTiers"`
	DefaultRollups []string `json:"defaultRollups"`
}

func renderConsoleAggregateRules(t *testing.T) []byte {
	t.Helper()
	rules := consoleAggregateRulesFile{
		Reducers:           substrate.FormReducers,
		AdditiveStatistics: substrate.AggregateAdditiveStatistics,
		Budget: map[string]consoleBudget{
			"maxBuckets": {substrate.AggregateMaxBuckets, substrate.AggregateMaxBucketsCap},
			"maxSamples": {substrate.AggregateMaxSamples, substrate.AggregateMaxSamplesCap},
			"maxGroups":  {substrate.AggregateMaxGroups, substrate.AggregateMaxGroupsCap},
		},
		MaxMetrics:     substrate.AggregateMaxMetrics,
		DefaultRollups: vocabulary.DefaultRollups,
	}
	for _, t := range vocabulary.RollupTiers {
		rules.RollupTiers = append(rules.RollupTiers, t.Name)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rules); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestConsoleAggregateRulesMatchTheServer(t *testing.T) {
	want := renderConsoleAggregateRules(t)
	if *updateGenerated {
		if err := os.WriteFile(consoleAggregateRules, want, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(consoleAggregateRules)
	if err != nil {
		t.Fatalf("read the console's copy: %v (rewrite it with -update)", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s differs from the server's aggregate rules: rewrite it with -update, never by hand", consoleAggregateRules)
	}
}
