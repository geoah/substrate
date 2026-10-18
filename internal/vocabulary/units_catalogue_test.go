package vocabulary_test

// The shipped metric catalogues read through the unit table: every metric's
// `unit` belongs to its declared `measures:`, so an aggregate read in the
// metric's unit converts every stream a writer stores in it.

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"math/big"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/vocabulary"
	"github.com/geoah/substrate/samples"
)

var metricCatalogues = []string{"observations/metrics.yaml", "health/metrics.yaml"}

type catalogueMetric struct {
	id, measure, unit string
}

func readMetricCatalogues(t *testing.T) []catalogueMetric {
	t.Helper()
	var out []catalogueMetric
	for _, name := range metricCatalogues {
		raw, err := fs.ReadFile(samples.Samples(), name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		dec := yaml.NewDecoder(bytes.NewReader(raw))
		for {
			var doc struct {
				Kind     string `yaml:"kind"`
				Metadata struct {
					ID string `yaml:"id"`
				} `yaml:"metadata"`
				Data struct {
					Properties struct {
						Measures string `yaml:"measures"`
						Unit     string `yaml:"unit"`
					} `yaml:"properties"`
				} `yaml:"data"`
			}
			if err := dec.Decode(&doc); errors.Is(err, io.EOF) {
				break
			} else if err != nil {
				t.Fatalf("decode %s: %v", name, err)
			}
			if !strings.HasSuffix(doc.Kind, "/observations/metric") || doc.Data.Properties.Unit == "" {
				continue
			}
			out = append(out, catalogueMetric{doc.Metadata.ID, doc.Data.Properties.Measures, doc.Data.Properties.Unit})
		}
	}
	if len(out) == 0 {
		t.Fatal("no metric with a unit in the catalogues")
	}
	return out
}

func TestEveryMetricUnitBelongsToItsMeasure(t *testing.T) {
	for _, m := range readMetricCatalogues(t) {
		if _, err := vocabulary.ResolveMeasure(m.unit, m.measure); err != nil {
			t.Errorf("metric %s: unit %s under measures %q: %v", m.id, m.unit, m.measure, err)
		}
	}
}

// A metric read in a unit of another measure is refused, never converted:
// HbA1c's NGSP % never reads in IFCC mmol/mol, and a vendor score never reads
// as a percent.
func TestAMetricRefusesAUnitOfAnotherMeasure(t *testing.T) {
	byID := map[string]catalogueMetric{}
	for _, m := range readMetricCatalogues(t) {
		byID[m.id] = m
	}
	for id, to := range map[string]string{
		"health/hba1c":               "mmol/mol",
		"health/hba1c_ifcc":          "%",
		"health/whoop_strain":        "%",
		"health/garmin_stress":       "%",
		"health/garmin_body_battery": "%",
	} {
		m, ok := byID[id]
		if !ok {
			t.Errorf("metric %s is not in the catalogue", id)
			continue
		}
		if _, err := vocabulary.ResolveMeasure(to, m.measure); err == nil {
			t.Errorf("metric %s (measures %q) admits a read in %s", id, m.measure, to)
		}
		if _, err := vocabulary.ConvertIn(m.measure, big.NewRat(1, 1), m.unit, to); err == nil {
			t.Errorf("metric %s converts %s to %s", id, m.unit, to)
		}
	}
}
