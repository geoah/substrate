package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func TestAgentProviderInputResolution(t *testing.T) {
	ds := openInternalDataset(t)
	ctx := context.Background()
	const pkg = "coach.example.com/coach"
	agent := vocabulary.AgentManifest(pkg, "run", map[string]any{"description": "Coach", "prompt": "Coach.", "providerInput": "model", "models": []any{"preferred", "fallback"}, "budgets": map[string]any{"spendCentsPerDay": 100}})
	docs := []map[string]any{
		vocabulary.PackageManifest(pkg, 1),
		vocabulary.BundleManifest(pkg, map[string]any{"description": "Coach", "inputs": map[string]any{"model": map[string]any{"kind": typeProvider}}, "installs": []any{pkg + "/run"}}),
		agent,
	}
	install := func() {
		t.Helper()
		if _, err := ds.InstallBundleClosure(ctx, substrate.BundleActor(vocabulary.SplitPackageRef(pkg)), docs, nil, substrate.BundleInstall{Published: true}); err != nil {
			t.Fatal(err)
		}
	}
	install()
	ag, err := ds.registry().ResolveAgent(pkg + "/run")
	if err != nil {
		t.Fatal(err)
	}
	put := func(id, model string) {
		t.Helper()
		_, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: typeProvider, ID: id, Properties: map[string]any{"wire": "openai", "baseURL": "https://ai.example.com", "apiKey": "test-key", "pricing": []any{map[string]any{"model": model, "inputPer1M": "1", "outputPer1M": "1"}}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	check := func(id, model string) {
		t.Helper()
		p, m, err := ds.resolveAgentProvider(ctx, ag, "")
		if err != nil {
			t.Fatal(err)
		}
		if p.id != id || m != model {
			t.Fatalf("%s %s, want %s %s", p.id, m, id, model)
		}
	}
	put("default", "gateway/preferred")
	check("default", "gateway/preferred")
	put("chosen", "fallback")
	if err := ds.BindBundleInput(ctx, pkg, "model", "chosen"); err != nil {
		t.Fatal(err)
	}
	check("chosen", "fallback")
	install()
	check("chosen", "fallback")
	if err := ds.BindBundleInput(ctx, pkg, "model", ""); err != nil {
		t.Fatal(err)
	}
	check("default", "gateway/preferred")
	for _, id := range []string{"default", "openai", "anthropic", "gemini"} {
		if _, err := ds.Delete(ctx, substrate.ActorPerson, typeProvider, id, substrate.DeleteInput{}); err != nil && !errors.Is(err, substrate.ErrNotFound) {
			t.Fatal(err)
		}
	}
	check("chosen", "fallback")
	if err := ds.BindBundleInput(ctx, pkg, "model", "chosen"); err != nil {
		t.Fatal(err)
	}
	put("chosen", "wrong")
	if _, _, err := ds.resolveAgentProvider(ctx, ag, ""); err == nil || !strings.Contains(err.Error(), "chosen") || !strings.Contains(err.Error(), "preferred") {
		t.Fatalf("refusal: %v", err)
	}
}

func TestPublishedAgentRequiresDailyCap(t *testing.T) {
	ds := openInternalDataset(t)
	const pkg = "cap.example.com/coach"
	for _, cap := range []any{nil, 1001, 100} {
		data := map[string]any{"description": "Coach", "prompt": "Coach.", "providerInput": "model", "models": []any{"m"}}
		if cap != nil {
			data["budgets"] = map[string]any{"spendCentsPerDay": cap}
		}
		docs := []map[string]any{
			vocabulary.PackageManifest(pkg, 1),
			vocabulary.BundleManifest(pkg, map[string]any{"inputs": map[string]any{"model": map[string]any{"kind": typeProvider}}, "installs": []any{pkg + "/run"}}),
			vocabulary.AgentManifest(pkg, "run", data),
		}
		_, err := ds.InstallBundleClosure(context.Background(), substrate.ActorPerson, docs, nil, substrate.BundleInstall{Published: true})
		if (err == nil) != (cap == 100) {
			t.Fatalf("cap %v: %v", cap, err)
		}
	}
}

func TestAgentProviderInputAdmissionDoors(t *testing.T) {
	ds := openInternalDataset(t)
	ctx := context.Background()
	const pkg = "admission.example.com/coach"
	data := map[string]any{"description": "Coach", "prompt": "Coach.", "providerInput": "model", "models": []any{"m"}}
	agent := vocabulary.AgentManifest(pkg, "run", data)
	docs := []map[string]any{
		vocabulary.PackageManifest(pkg, 1),
		vocabulary.BundleManifest(pkg, map[string]any{"description": "Coach", "inputs": map[string]any{"model": map[string]any{"kind": typeProvider}}, "installs": []any{pkg + "/run"}}),
		agent,
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, docs); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: kindAgent, ID: pkg + "/run", Properties: agent["data"].(map[string]any)}); err != nil {
		t.Fatal(err)
	}
	agent["data"].(map[string]any)["providerInput"] = "missing"
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: kindAgent, ID: pkg + "/run", Properties: agent["data"].(map[string]any)}); err == nil {
		t.Fatal("put admitted missing input")
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, docs); err == nil {
		t.Fatal("apply admitted missing input")
	}
	if _, err := ds.InstallBundleClosure(ctx, substrate.ActorPerson, docs, nil, substrate.BundleInstall{}); err == nil {
		t.Fatal("install admitted missing input")
	}
}

func TestPublishedAgentRequiresProviderInput(t *testing.T) {
	ds := openInternalDataset(t)
	ctx := context.Background()
	const pkg = "provider.example.com/coach"
	docs := []map[string]any{
		vocabulary.PackageManifest(pkg, 1),
		vocabulary.AgentManifest(pkg, "run", map[string]any{
			"description": "Coach", "prompt": "Coach.", "provider": "openai", "model": "unpriced",
			"budgets": map[string]any{"spendCentsPerDay": 100},
		}),
	}
	if _, err := ds.InstallBundleClosure(ctx, substrate.ActorPerson, docs, nil, substrate.BundleInstall{Published: true}); !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "providerInput") {
		t.Fatalf("published direct provider admitted: %v", err)
	}
	if _, err := ds.InstallBundleClosure(ctx, substrate.ActorPerson, docs, nil, substrate.BundleInstall{}); err != nil {
		t.Fatalf("owner's direct provider refused: %v", err)
	}
}
