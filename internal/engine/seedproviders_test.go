package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/samples"
)

// The rows a new repository is seeded with are the rows the llm sample ships,
// so a price edit in one place cannot leave the other behind.
func TestSeededProviderRowsMatchTheShippedSample(t *testing.T) {
	raw, err := fs.ReadFile(samples.Samples(), "llm/providers.yaml")
	if err != nil {
		t.Fatal(err)
	}
	shipped := map[string]any{}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	for {
		var doc struct {
			Metadata struct {
				ID string `yaml:"id"`
			} `yaml:"metadata"`
			Data struct {
				Properties map[string]any `yaml:"properties"`
			} `yaml:"data"`
		}
		if err := dec.Decode(&doc); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		shipped[doc.Metadata.ID] = normalizeJSON(t, doc.Data.Properties)
	}
	seeded := map[string]any{}
	for _, p := range defaultLLMProviders {
		seeded[p.id] = normalizeJSON(t, p.props)
	}
	if !reflect.DeepEqual(seeded, shipped) {
		a, _ := json.MarshalIndent(seeded, "", "  ")
		b, _ := json.MarshalIndent(shipped, "", "  ")
		t.Fatalf("seeded rows differ from samples/llm/providers.yaml\nseeded:  %s\nshipped: %s", a, b)
	}
}

// normalizeJSON round-trips v through JSON, so an int and a float64 of the
// same value compare equal.
func normalizeJSON(t *testing.T, v any) any {
	t.Helper()
	buf, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(buf, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
