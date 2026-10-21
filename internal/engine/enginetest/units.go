package enginetest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/engine/unitseed"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// --- the shipped units, a test's share of them ----------------------------------
//
// A test repository skips the unit seed (engine.WithUnitSeed, off in
// OpenForTest), because the seed is several hundred records. A repository
// holds the seed in production, and every quantity converts through it
// (decision 0186), so a test that imports a sample naming units first puts
// the shipped records those units need: PutShippedUnits, which
// ImportVocabulary calls for each sample it imports.

// PutShippedUnits writes, as the owner, the shipped unit records of every
// linear component that holds one of ids, and the shipped conversions inside
// those components, each one the repository does not hold yet. A component
// is what a filter, a bound or a metric converts across, so a test reading
// `kg` may write `lb` too.
func PutShippedUnits(ctx context.Context, ds substrate.Dataset, ids ...string) error {
	if len(ids) == 0 {
		return nil
	}
	g, problems, err := unitseed.Graph()
	if err != nil {
		return err
	}
	if len(problems) > 0 {
		return fmt.Errorf("enginetest: the shipped units: %v", problems)
	}
	want := map[string]bool{}
	for _, id := range ids {
		u, ok := g.Resolve(id)
		if !ok {
			return fmt.Errorf("enginetest: %q is no shipped unit", id)
		}
		for _, member := range g.Component(u) {
			want[member] = true
		}
	}
	units, conversions, err := shippedUnitDocuments()
	if err != nil {
		return err
	}
	put := func(kind, id string, props map[string]any) error {
		if _, err := ds.Get(ctx, kind, id); err == nil {
			return nil
		} else if !errors.Is(err, substrate.ErrNotFound) {
			return err
		}
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: kind, ID: id, Properties: props}); err != nil {
			return fmt.Errorf("enginetest: put %s %s: %w", kind, id, err)
		}
		return nil
	}
	for _, id := range sortedKeys(want) {
		if err := put(vocabulary.KindUnit, id, units[id]); err != nil {
			return err
		}
	}
	for _, id := range sortedKeys(conversions) {
		props := conversions[id]
		if want[unitRefID(props["from"])] && want[unitRefID(props["to"])] {
			if err := put(vocabulary.KindConversion, id, props); err != nil {
				return err
			}
		}
	}
	return nil
}

// sampleUnitPattern finds the units a sample's documents name: a reference
// to a unit record, and a quantity declaration's `unit:`.
var sampleUnitPattern = regexp.MustCompile(`(?m)(?:` + regexp.QuoteMeta(vocabulary.KindUnit) + `/(\S+)$|^\s+unit: (\S+)$)`)

// SampleUnits is every unit a shipped sample's documents name, by id.
func SampleUnits(name string) ([]string, error) {
	raw, err := readSampleText(name)
	if err != nil {
		return nil, err
	}
	g, _, err := unitseed.Graph()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, m := range sampleUnitPattern.FindAllStringSubmatch(raw, -1) {
		spelling := m[1]
		if spelling == "" {
			spelling = m[2]
		}
		if id, ok := g.Resolve(spelling); ok && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out, nil
}

// PutSampleUnits is PutShippedUnits for every unit the named samples name.
func PutSampleUnits(ctx context.Context, ds substrate.Dataset, names ...string) error {
	var ids []string
	for _, name := range names {
		got, err := SampleUnits(name)
		if err != nil {
			return err
		}
		ids = append(ids, got...)
	}
	return PutShippedUnits(ctx, ds, ids...)
}

func shippedUnitDocuments() (units, conversions map[string]map[string]any, err error) {
	units, conversions = map[string]map[string]any{}, map[string]map[string]any{}
	for _, file := range [][]byte{unitseed.Units(), unitseed.Conversions()} {
		dec := yaml.NewDecoder(bytes.NewReader(file))
		for {
			var doc struct {
				Kind     string `yaml:"kind"`
				Metadata struct {
					ID string `yaml:"id"`
				} `yaml:"metadata"`
				Data struct {
					Properties map[string]any `yaml:"properties"`
				} `yaml:"data"`
			}
			err := dec.Decode(&doc)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, nil, fmt.Errorf("enginetest: the unit seed: %w", err)
			}
			if doc.Kind == vocabulary.KindUnit {
				units[doc.Metadata.ID] = doc.Data.Properties
			} else {
				conversions[doc.Metadata.ID] = doc.Data.Properties
			}
		}
	}
	return units, conversions, nil
}

func unitRefID(v any) string {
	m, _ := v.(map[string]any)
	path, _ := m[vocabulary.ReferenceValueKey].(string)
	_, id, _ := vocabulary.SplitRecordPath(path)
	return id
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// readSampleText is every YAML document of one shipped sample, as text.
func readSampleText(name string) (string, error) {
	entries, err := os.ReadDir(filepath.Join(SamplesDir, name))
	if err != nil {
		return "", fmt.Errorf("enginetest: %w", err)
	}
	var b strings.Builder
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(SamplesDir, name, e.Name()))
		if err != nil {
			return "", fmt.Errorf("enginetest: %w", err)
		}
		b.Write(raw)
		b.WriteByte('\n')
	}
	return b.String(), nil
}
