package engine_test

// Issue 464: core's `trigger.source` and `recordpatchpolicy.action` are
// required. The dispatcher never ran a trigger without a source and the policy
// load never ran a rule without an action, and the write door refused both,
// so a row lacking one is what a binary older than those checks left behind.
// Neither property has a `default`, so adding `required` is a narrowing the
// boot upgrade refuses while such a row lives. The refusal names each kind and
// its count, the repository still opens and serves on the stored
// declarations, and the next boot after the rows are deleted or given the value
// takes the upgrade.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	triggerKind = corePackage + "/trigger"
	policyKind  = vocabulary.KindRecordPatchPolicy
)

// optionalSourceAndActionTree is the shipped tree as the binary before issue
// 464 shipped it: neither property required, and each kind at the version it
// pinned then, below the shipped one, so the boot upgrade rewrites both.
func optionalSourceAndActionTree(t *testing.T) string {
	t.Helper()
	tree := shippedTree(t)
	patchShipped(t, coreKind(tree, "trigger.yaml"), func(doc string) string {
		return pinVersion(t, dropRequired(t, doc, "    source:\n      type: object\n"), "19")
	})
	patchShipped(t, coreKind(tree, "recordpatchpolicy.yaml"), func(doc string) string {
		return pinVersion(t, dropRequired(t, doc, "    action:\n      type: enum\n"), "18")
	})
	return tree
}

// dropRequired removes the `required: true` line right after head.
func dropRequired(t *testing.T, doc, head string) string {
	t.Helper()
	from := head + "      required: true\n"
	if !strings.Contains(doc, from) {
		t.Fatalf("the shipped declaration no longer carries %q", from)
	}
	return strings.Replace(doc, from, head, 1)
}

// declaresRequired reads whether a repository's live declaration of one kind
// marks one property required, from the stored rows rather than the tree.
func declaresRequired(t *testing.T, ds substrate.Dataset, kind, prop string) bool {
	t.Helper()
	ty, err := ds.KindByRef(context.Background(), kind)
	if err != nil {
		t.Fatalf("the %s kind: %v", kind, err)
	}
	props, _ := ty.Definition["properties"].(map[string]any)
	p, _ := props[prop].(map[string]any)
	if p == nil {
		t.Fatalf("%s declares no %q: %v", kind, prop, props)
	}
	required, _ := p["required"].(bool)
	return required
}

// lacking lists the live ids of one kind holding no value for prop, with the
// filter the upgrade note gives an operator.
func lacking(t *testing.T, ds substrate.Dataset, kind, prop string) []string {
	t.Helper()
	absent := false
	page, err := ds.List(context.Background(), substrate.Query{
		Filter: substrate.Filter{
			Kinds:      []string{kind},
			Properties: map[string]substrate.Cond{prop: {Exists: &absent}},
		},
		First: 50,
	})
	if err != nil {
		t.Fatalf("list %s without %s: %v", kind, prop, err)
	}
	ids := make([]string, 0, len(page.Records))
	for _, r := range page.Records {
		ids = append(ids, r.ID)
	}
	return ids
}

func TestBootUpgradeRefusesRequiredSourceAndActionWhileARowLacksThem(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := engine.MigratedDSN(t)

	// --- binary N: neither property required -----------------------------
	old := openTree(t, dsn, optionalSourceAndActionTree(t))
	if _, err := old.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := old.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	if declaresRequired(t, ds, triggerKind, "source") || declaresRequired(t, ds, policyKind, "action") {
		t.Fatal("the old tree already requires source or action; it is not the old tree")
	}
	const pkg = "keeper.test.dev/keeper"
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.AgentManifest(pkg, "keeper", map[string]any{
			"description": "the callable the triggers name",
			"prompt":      "You keep.", "provider": "openai", "model": "gpt-5",
		}),
	}); err != nil {
		t.Fatalf("declare the callable: %v", err)
	}
	callable := vocabulary.RecordPath("substrate.reamde.dev/core/agent", pkg+"/keeper")
	selector := map[string]any{"kinds": []any{"*"}}
	webhook := map[string]any{"webhook": map[string]any{}}
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: triggerKind, ID: "sourceless",
		Properties: map[string]any{"source": webhook, "callable": callable},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: triggerKind, ID: "healthy",
		Properties: map[string]any{"source": webhook, "callable": callable},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: policyKind, ID: "actionless",
		Properties: map[string]any{"selector": selector, "action": "gate"},
	})
	// Binary N's write door refused both shapes too, so the rows are planted
	// through the fold, as a binary older than that door left them.
	p := planter(t, ds)
	if err := p.PlantDeclarationRow(ctx, triggerKind, "sourceless", map[string]any{"callable": callable}); err != nil {
		t.Fatalf("plant the trigger without a source: %v", err)
	}
	if err := p.PlantDeclarationRow(ctx, policyKind, "actionless", map[string]any{"selector": selector}); err != nil {
		t.Fatalf("plant the policy without an action: %v", err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}

	// --- binary N+1: the upgrade is refused, the repository opens ----------
	triggerLine := `kind substrate.reamde.dev/core/trigger: property "source" becomes required while 1 live records lack it`
	policyLine := `kind substrate.reamde.dev/core/recordpatchpolicy: property "action" becomes required while 1 live records lack it`
	wantRefusedUpgrade(t, openRefused(t, dsn, shippedTree(t)), triggerLine, policyLine)

	next := openTree(t, dsn, shippedTree(t))
	ds, err = next.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("a refused upgrade must still open the repository: %v", err)
	}
	if v := kindVersion(t, ds, triggerKind); v != 19 {
		t.Fatalf("trigger is at version %d while a row lacks source, want the stored 19", v)
	}
	if v := kindVersion(t, ds, policyKind); v != 18 {
		t.Fatalf("recordpatchpolicy is at version %d while a row lacks action, want the stored 18", v)
	}
	blockers := strings.Join(planCore(t, ds).Upgrade.Blockers, "\n")
	for _, line := range []string{triggerLine, policyLine} {
		if !strings.Contains(blockers, line) {
			t.Fatalf("GET /vocabulary/upgrade does not name %q: %s", line, blockers)
		}
	}
	// The upgrade note's search finds exactly the planted rows.
	if got := lacking(t, ds, triggerKind, "source"); len(got) != 1 || got[0] != "sourceless" {
		t.Fatalf("triggers without a source = %v, want [sourceless]", got)
	}
	if got := lacking(t, ds, policyKind, "action"); len(got) != 1 || got[0] != "actionless" {
		t.Fatalf("policies without an action = %v, want [actionless]", got)
	}
	// The repository serves: a write under the stored declarations lands, and
	// the two ways out, a delete and a write of the missing value, both work.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: triggerKind, ID: "another",
		Properties: map[string]any{"source": webhook, "callable": callable},
	})
	if _, err := ds.Delete(ctx, owner, triggerKind, "sourceless", substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete the trigger without a source: %v", err)
	}
	if _, err := ds.Patch(ctx, owner, policyKind, "actionless", substrate.PatchInput{
		Properties: map[string]any{"action": "refuse"},
	}); err != nil {
		t.Fatalf("give the policy an action: %v", err)
	}
	if err := next.Close(); err != nil {
		t.Fatal(err)
	}

	// --- binary N+1, next boot: the upgrade lands -------------------------
	if refused := openRefused(t, dsn, shippedTree(t)); refused != "" {
		t.Fatalf("the boot still refused the upgrade after the rows were fixed: %s", refused)
	}
	final := openTree(t, dsn, shippedTree(t))
	t.Cleanup(func() { _ = final.Close() })
	ds, err = final.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	if !declaresRequired(t, ds, triggerKind, "source") || !declaresRequired(t, ds, policyKind, "action") {
		t.Fatal("the upgrade landed without `required` on source and action")
	}
	if v := kindVersion(t, ds, triggerKind); v <= 19 {
		t.Fatalf("trigger is at version %d after the upgrade, want above 19", v)
	}
	if v := kindVersion(t, ds, policyKind); v <= 18 {
		t.Fatalf("recordpatchpolicy is at version %d after the upgrade, want above 18", v)
	}
	// A write that leaves either value out is refused, naming it.
	_, err = ds.Put(ctx, owner, substrate.PutInput{
		Kind: triggerKind, ID: "late",
		Properties: map[string]any{"callable": callable},
	})
	if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "props.source") {
		t.Fatalf("a trigger without a source = %v, want a validation error naming props.source", err)
	}
	_, err = ds.Put(ctx, owner, substrate.PutInput{
		Kind: policyKind, ID: "late",
		Properties: map[string]any{"selector": selector},
	})
	if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "props.action") {
		t.Fatalf("a policy without an action = %v, want a validation error naming props.action", err)
	}
	// A patch that never mentions the required property is not refused for it.
	if _, err := ds.Patch(ctx, owner, triggerKind, "healthy", substrate.PatchInput{
		Properties: map[string]any{"enabled": false},
	}); err != nil {
		t.Fatalf("a patch of `enabled` alone: %v", err)
	}
	if _, err := ds.Patch(ctx, owner, policyKind, "actionless", substrate.PatchInput{
		Properties: map[string]any{"criteria": "only what the owner wrote"},
	}); err != nil {
		t.Fatalf("a patch of `criteria` alone: %v", err)
	}
}
