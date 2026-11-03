package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// Trigger bodies are data records, so the vocabulary preview cannot see them.
func outsideTriggerChanges(ctx context.Context, ds substrate.Dataset, b *Bundle) ([]substrate.BundleCodeChange, error) {
	pkg, err := ds.Get(ctx, vocabulary.CoreKind(vocabulary.DocPackage), b.ID)
	if errors.Is(err, substrate.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if from, _ := pkg.Properties["publishedFrom"].(string); from == "" {
		return nil, nil
	}
	var changes []substrate.BundleCodeChange
	for _, doc := range b.dataDocs {
		in, err := dataPutInput(doc)
		if err != nil {
			return nil, err
		}
		if in.Kind != vocabulary.CoreKind("trigger") {
			continue
		}
		var before map[string]any
		held, err := ds.Get(ctx, in.Kind, in.ID)
		if err != nil && !errors.Is(err, substrate.ErrNotFound) {
			return nil, err
		}
		if held != nil {
			before = held.Properties
		}
		keepEnabled := vocabulary.TriggerKey(before) != ""
		before, err = triggerCode(before)
		if err != nil {
			return nil, err
		}
		candidate, err := triggerCode(in.Properties)
		if err != nil {
			return nil, err
		}
		if keepEnabled {
			delete(candidate, "enabled")
		}
		after := maps.Clone(before)
		if after == nil {
			after = map[string]any{}
		}
		maps.Copy(after, candidate)
		a, err := json.Marshal(before)
		if err != nil {
			return nil, err
		}
		z, err := json.Marshal(after)
		if err != nil {
			return nil, err
		}
		if string(a) == string(z) {
			continue
		}
		old, err := yaml.Marshal(before)
		if err != nil {
			return nil, err
		}
		next, err := yaml.Marshal(after)
		if err != nil {
			return nil, err
		}
		diff := "--- installed\n+++ candidate\n- " + strings.ReplaceAll(strings.TrimSpace(string(old)), "\n", "\n- ") + "\n+ " + strings.ReplaceAll(strings.TrimSpace(string(next)), "\n", "\n+ ")
		changes = append(changes, substrate.BundleCodeChange{Kind: "trigger", ID: in.ID, Diff: diff})
	}
	return changes, nil
}

// Webhook keys and the owner's enabled switch are operational settings, not
// publisher code. They must never appear in a code diff.
func triggerCode(props map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(props)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err = json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if out == nil {
		return nil, nil
	}
	if callable, ok := out["callable"].(string); ok {
		out["callable"] = map[string]any{"ref": callable}
	}
	if source, ok := out["source"].(map[string]any); ok {
		if webhook, ok := source["webhook"].(map[string]any); ok {
			if key, _ := webhook["key"].(string); key != "" {
				delete(out, "enabled")
			}
			delete(webhook, "key")
		}
	}
	return out, nil
}
