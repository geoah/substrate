package synth

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// catalogueCodes is every code each health metric lists, by metric id, from
// the health sample's catalogue.
func catalogueCodes(t *testing.T) map[string]map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "samples", "health", "metrics.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]map[string]bool{}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	for {
		var doc struct {
			Metadata struct{ ID string } `yaml:"metadata"`
			Data     struct {
				Properties struct {
					Codes []struct{ Code string } `yaml:"codes"`
				} `yaml:"properties"`
			} `yaml:"data"`
		}
		if err := dec.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				return out
			}
			t.Fatal(err)
		}
		out[doc.Metadata.ID] = map[string]bool{}
		for _, c := range doc.Data.Properties.Codes {
			out[doc.Metadata.ID][c.Code] = true
		}
	}
}

// manualDocs decodes the manual emitter's labs.yaml.
func manualDocs(t *testing.T) []map[string]any {
	t.Helper()
	p, err := NewPersona(Options{})
	if err != nil {
		t.Fatal(err)
	}
	out, err := Manual{}.Emit(p)
	if err != nil {
		t.Fatal(err)
	}
	var docs []map[string]any
	dec := yaml.NewDecoder(bytes.NewReader(out.Files[0].Body))
	for {
		var doc map[string]any
		if err := dec.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				return docs
			}
			t.Fatal(err)
		}
		docs = append(docs, doc)
	}
}

// The hand-entered records in the shapes the health sample declares: a lab
// result's code is one its metric lists, each reference range bound a
// quantity, a US panel in mg/dL on the mass twins for convertlab to bridge,
// and the hotel weigh-in held for the day like the catalogue's body gauges.
func TestManualWritesTheHealthSamplesShapes(t *testing.T) {
	codes := catalogueCodes(t)
	var results, mass, weighIns int
	for _, doc := range manualDocs(t) {
		kind, _ := doc["kind"].(string)
		props := doc["data"].(map[string]any)["properties"].(map[string]any)
		id := doc["metadata"].(map[string]any)["id"]
		switch {
		case strings.HasSuffix(kind, "/health/labresult"):
			results++
			metric := strings.TrimPrefix(props["metric"].(map[string]any)["ref"].(string), "ada.example.com/observations/metric/")
			code := props["coding"].(map[string]any)["code"].(string)
			if !codes[metric][code] {
				t.Errorf("%s codes %s, which %s does not list", id, code, metric)
			}
			for _, rr := range props["referenceRanges"].([]any) {
				rng := rr.(map[string]any)
				if _, ok := rng["unit"]; ok {
					t.Errorf("%s has a range with a unit beside its bounds: %v", id, rng)
				}
				for _, bound := range []string{"low", "high"} {
					if v, ok := rng[bound]; ok {
						q, _ := v.(map[string]any)
						if q["value"] == nil || q["unit"] == nil {
							t.Errorf("%s range %s is %v, want a quantity", id, bound, v)
						}
					}
				}
			}
			if v := props["value"].(map[string]any); strings.HasSuffix(metric, "_mass") && v["unit"] == "mg/dL" {
				mass++
			}
		case strings.HasSuffix(kind, "/observations/observation"):
			weighIns++
			if props["hold"] != "P1D" {
				t.Errorf("the hotel weigh-in %s holds %v, want P1D", id, props["hold"])
			}
		}
	}
	if results < 30 || mass < 5 || weighIns != 1 {
		t.Errorf("%d lab results (%d in mg/dL on a mass twin) and %d weigh-ins", results, mass, weighIns)
	}
}
