package vocabulary_test

// The shipped metric catalogues read through the shipped unit graph
// (decision 0186): every metric's `unit` is a reference to a shipped unit, so
// a repository that imports the catalogue holds every unit it names, and the
// metrics that must never pool are in apart components.

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/vocabulary"
	"github.com/geoah/substrate/samples"
)

var metricCatalogues = []string{"observations/metrics.yaml", "health/metrics.yaml"}

type catalogueMetric struct {
	id, unit string
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
						Unit struct {
							Ref string `yaml:"ref"`
						} `yaml:"unit"`
					} `yaml:"properties"`
				} `yaml:"data"`
			}
			if err := dec.Decode(&doc); errors.Is(err, io.EOF) {
				break
			} else if err != nil {
				t.Fatalf("decode %s: %v", name, err)
			}
			if !strings.HasSuffix(doc.Kind, "/observations/metric") || doc.Data.Properties.Unit.Ref == "" {
				continue
			}
			kind, id, ok := vocabulary.SplitRecordPath(doc.Data.Properties.Unit.Ref)
			if !ok || kind != vocabulary.KindUnit {
				t.Fatalf("metric %s: unit %q is no reference to a %s record", doc.Metadata.ID, doc.Data.Properties.Unit.Ref, vocabulary.KindUnit)
			}
			out = append(out, catalogueMetric{doc.Metadata.ID, id})
		}
	}
	if len(out) == 0 {
		t.Fatal("no metric with a unit in the catalogues")
	}
	return out
}

func TestEveryMetricUnitIsAShippedUnit(t *testing.T) {
	g := shippedGraph(t)
	for _, m := range readMetricCatalogues(t) {
		if _, ok := g.Unit(m.unit); !ok {
			t.Errorf("metric %s: unit %s is no shipped unit", m.id, m.unit)
		}
	}
}

// A metric read in a unit no linear conversion reaches is refused, never
// converted: HbA1c's NGSP percent never reads in IFCC mmol/mol, and a vendor
// score never reads as a percent.
func TestAMetricRefusesAUnitOfAnotherComponent(t *testing.T) {
	g := shippedGraph(t)
	byID := map[string]catalogueMetric{}
	for _, m := range readMetricCatalogues(t) {
		byID[m.id] = m
	}
	for id, to := range map[string]string{
		"health/hba1c":        "mmol/mol",
		"health/hba1c_ifcc":   "percent",
		"health/whoop_strain": "percent",
	} {
		m, ok := byID[id]
		if !ok {
			t.Errorf("metric %s is not in the catalogue", id)
			continue
		}
		if _, _, err := g.Linear(m.unit, to, false); err == nil {
			t.Errorf("metric %s converts %s to %s", id, m.unit, to)
		}
	}
}
