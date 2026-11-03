package engine

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func codeDeclaration(d vocabulary.Document) map[string]any {
	switch d.Kind {
	case vocabulary.DocFunction, vocabulary.DocAgent, vocabulary.DocRecordMapping:
		return minusVersionKey(d.Data)
	case vocabulary.DocBundle:
		out := map[string]any{}
		for _, key := range []string{"oauth2", "modules"} {
			if v, ok := d.Data[key]; ok {
				out[key] = v
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

func codeDiff(before, after map[string]any) (string, error) {
	a, err := yaml.Marshal(before)
	if err != nil {
		return "", err
	}
	b, err := yaml.Marshal(after)
	if err != nil {
		return "", err
	}
	if string(a) == string(b) {
		return "", nil
	}
	var lines []string
	lines = append(lines, "--- installed", "+++ candidate")
	for _, line := range strings.Split(strings.TrimSuffix(string(a), "\n"), "\n") {
		lines = append(lines, "- "+line)
	}
	for _, line := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
		lines = append(lines, "+ "+line)
	}
	return strings.Join(lines, "\n"), nil
}

func (ds *dataset) outsideCodeChanges(ctx context.Context, pkg string, docs []vocabulary.Document) ([]substrate.BundleCodeChange, error) {
	outside, err := ds.outsidePackage(ctx, pkg)
	if err != nil || !outside {
		return nil, err
	}
	held, err := ds.vocabularyDocumentRows(ctx, ds.db, map[string]bool{pkg: true})
	if err != nil {
		return nil, err
	}
	next := map[string]vocabulary.Document{}
	for _, d := range docs {
		if d.DeclaredPackage() == pkg {
			next[d.Kind+"\x00"+d.ID] = d
		}
	}
	keys := map[string]bool{}
	for k := range held {
		keys[k] = true
	}
	for k := range next {
		keys[k] = true
	}
	var out []substrate.BundleCodeChange
	for key := range keys {
		before, after := codeDeclaration(held[key]), codeDeclaration(next[key])
		if before == nil && after == nil {
			continue
		}
		diff, err := codeDiff(before, after)
		if err != nil {
			return nil, fmt.Errorf("outside code diff: %w", err)
		}
		if diff == "" {
			continue
		}
		kind, id, _ := strings.Cut(key, "\x00")
		out = append(out, substrate.BundleCodeChange{Kind: kind, ID: id, Diff: diff})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}
