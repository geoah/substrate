package engine_test

// The boot upgrade's read-only preview (shippedupgrade.go). A refused core
// upgrade used to be one log line: the repository opened on its old
// declarations and no read said so. PlanShippedUpgrade is the same diff and the
// same guard lines, served to a token, so what it reports blocked must be
// exactly what the boot logged as refused.
//
// Same harness as upgrade_guard_db_test.go: two Opens over one database, the
// second carrying a shipped tree that moved. llmprovider pins a version of its
// own, so the narrowed declaration is pinned forward too; left at its stored
// pin it would keep its stored shape, the upgrade under test would never run,
// and this case would prove nothing (docs/testing.md). The assertion that the
// boot logged a refusal is what holds it to that.

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// planCore is the preview's entry for the core package, which every
// repository holds.
func planCore(t *testing.T, ds substrate.Dataset) substrate.ShippedUpgrade {
	t.Helper()
	planner, ok := ds.(substrate.ShippedUpgradePlanner)
	if !ok {
		t.Fatal("the dataset does not preview the shipped upgrade")
	}
	plans, err := planner.PlanShippedUpgrade(context.Background())
	if err != nil {
		t.Fatalf("plan the shipped upgrade: %v", err)
	}
	for _, p := range plans {
		if p.Package == corePackage {
			return p
		}
	}
	t.Fatalf("the preview lists no entry for %s: %+v", corePackage, plans)
	return substrate.ShippedUpgrade{}
}

func TestShippedUpgradePreviewReportsTheRefusedBootUpgrade(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := seededRepository(t)

	// Under the binary that seeded it, the repository holds every shipped
	// declaration at the shipped version: nothing moves, nothing blocks, and
	// the stored version is the tree's own.
	shipped, err := strconv.ParseInt(declaredVersion(t, shippedTree(t), corePackage), 10, 64)
	if err != nil {
		t.Fatalf("core's declared version: %v", err)
	}
	svc := openTree(t, dsn, shippedTree(t))
	ds, err := svc.Dataset(ctx, testdb.Username(t))
	if err != nil {
		t.Fatalf("dataset: %v", err)
	}
	current := planCore(t, ds)
	if current.Upgrade.Available || len(current.Upgrade.Blockers) != 0 || len(current.Upgrade.Changes) != 0 {
		t.Fatalf("an up-to-date repository previews a move: %+v", current.Upgrade)
	}
	if current.Upgrade.From != shipped || current.Upgrade.To != shipped {
		t.Fatalf("versions = %d -> %d, want %d -> %d", current.Upgrade.From, current.Upgrade.To, shipped, shipped)
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}

	// Binary N+1 narrows llmprovider while a live row holds the old shape. The
	// boot refuses the upgrade, logs the guard lines, and opens the repository
	// on the stored declarations.
	tree := shippedTree(t)
	patchShipped(t, coreKind(tree, "llmprovider.yaml"), func(doc string) string {
		return pinVersion(t, narrowLabel(t, doc), "99")
	})
	bumpPackageVersion(t, tree, corePackage, "99")
	var logs bytes.Buffer
	svc, err = engine.Open(ctx, dsn,
		engine.WithDataRoot(t.TempDir()), engine.WithCredentialKey(engine.TestCredentialKey),
		engine.WithKindsDir(tree), engine.WithLogger(slog.New(slog.NewJSONHandler(&logs, nil))))
	if err != nil {
		t.Fatalf("open the moved tree: %v", err)
	}
	defer func() { _ = svc.Close() }()
	ds, err = svc.Dataset(ctx, testdb.Username(t))
	if err != nil {
		t.Fatalf("a refused upgrade must not fail the open: %v", err)
	}
	refused := loggedRefusal(t, logs.String())
	if refused == "" {
		t.Fatal("the boot did not log a refusal; the narrowing landed or never ran")
	}

	// The preview on the SAME open reports the same package, the versions the
	// boot compared, and the guard lines the boot logged, verbatim.
	plan := planCore(t, ds)
	if !plan.Upgrade.Available {
		t.Fatalf("the withheld upgrade previews as not available: %+v", plan.Upgrade)
	}
	if plan.Upgrade.From != shipped || plan.Upgrade.To != 99 {
		t.Fatalf("versions = %d -> %d, want %d -> 99", plan.Upgrade.From, plan.Upgrade.To, shipped)
	}
	if got := strings.Join(plan.Upgrade.Blockers, "; "); got != refused {
		t.Fatalf("preview blockers:\n  %s\nlogged refused:\n  %s", got, refused)
	}
	if !strings.Contains(refused, "llmprovider") || !strings.Contains(refused, `"label"`) {
		t.Fatalf("the guard line names neither the kind nor the property: %s", refused)
	}
	// The preview wrote nothing: the repository still speaks the stored shape.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/llmprovider", ID: "after",
		Properties: map[string]any{"label": "still a string", "wire": "openai"},
	})
}

// loggedRefusal is the `refused` attribute of the boot's REFUSED line, or ""
// when no such line was logged.
func loggedRefusal(t *testing.T, logs string) string {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(logs), "\n") {
		if line == "" {
			continue
		}
		var entry struct {
			Msg     string `json:"msg"`
			Refused string `json:"refused"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log line is not JSON: %s: %v", line, err)
		}
		if strings.Contains(entry.Msg, "REFUSED to upgrade") {
			return entry.Refused
		}
	}
	return ""
}

// The retired-name guards (decision 0055) reach the preview through the same
// stage the boot runs: a binary that ships a kind by a name this repository's
// stored core header retired is refused at boot, and the preview names the
// same line. Decided without a count, so it is in the stage's refused list and
// guards returns it beside the counted narrowings.
func TestShippedUpgradePreviewReportsARetiredName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := seededRepository(t)

	// Binary N+1 retires `gadget` in core: additive, lands.
	retiring := shippedTree(t)
	patchShipped(t, packageHeader(retiring, corePackage), func(doc string) string {
		return retireShippedKind(t, doc, "gadget")
	})
	if err := openMoved(t, dsn, retiring); err != nil {
		t.Fatalf("retiring a name is additive and must land: %v", err)
	}

	// Binary N+2 ships a `gadget` kind under core from a tree without the
	// retirement: the boot refuses, and the preview on the same open says why.
	reusing := shippedTree(t)
	addShippedKind(t, reusing, corePackage, "gadget", "gadgets")
	bumpPackageVersion(t, reusing, corePackage, "100")
	var logs bytes.Buffer
	svc, err := engine.Open(ctx, dsn,
		engine.WithDataRoot(t.TempDir()), engine.WithCredentialKey(engine.TestCredentialKey),
		engine.WithKindsDir(reusing), engine.WithLogger(slog.New(slog.NewJSONHandler(&logs, nil))))
	if err != nil {
		t.Fatalf("open the reusing tree: %v", err)
	}
	defer func() { _ = svc.Close() }()
	ds, err := svc.Dataset(ctx, testdb.Username(t))
	if err != nil {
		t.Fatalf("a refused upgrade must not fail the open: %v", err)
	}
	refused := loggedRefusal(t, logs.String())
	if refused == "" {
		t.Fatal("the boot did not refuse the reused name")
	}
	plan := planCore(t, ds)
	if !plan.Upgrade.Available || plan.Upgrade.From != 99 || plan.Upgrade.To != 100 {
		t.Fatalf("plan = %+v, want available 99 -> 100", plan.Upgrade)
	}
	const declaredAgain = "kind substrate.reamde.dev/core/gadget: the name is retired in package substrate.reamde.dev/core; a retired name is never declared again"
	if got := strings.Join(plan.Upgrade.Blockers, "; "); got != refused || !strings.Contains(got, declaredAgain) {
		t.Fatalf("preview blockers:\n  %s\nlogged refused:\n  %s", got, refused)
	}
}

// A refusal is one list on both doors. A tree carrying both an unstorable
// default (decided without a count) and a narrowing over live rows (counted)
// is refused with both lines in the log, and the preview's blockers are that
// list exactly. A boot that stopped at the first kind of guard would log one
// line while the read reports two, and "these lines caused the refusal" would
// be false on one of them.
func TestShippedUpgradePreviewMatchesTheLogWithEveryGuardKind(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := seededRepository(t)

	tree := shippedTree(t)
	patchShipped(t, coreKind(tree, "llmprovider.yaml"), func(doc string) string {
		doc = pinVersion(t, narrowLabel(t, doc), "99")
		return strings.Replace(doc, "  properties:\n",
			"  properties:\n    region:\n      type: string\n      pattern: \"^eu-\"\n"+
				"      default: us-east\n      description: a default the pattern refuses\n", 1)
	})
	bumpPackageVersion(t, tree, corePackage, "99")
	var logs bytes.Buffer
	svc, err := engine.Open(ctx, dsn,
		engine.WithDataRoot(t.TempDir()), engine.WithCredentialKey(engine.TestCredentialKey),
		engine.WithKindsDir(tree), engine.WithLogger(slog.New(slog.NewJSONHandler(&logs, nil))))
	if err != nil {
		t.Fatalf("open the moved tree: %v", err)
	}
	defer func() { _ = svc.Close() }()
	ds, err := svc.Dataset(ctx, testdb.Username(t))
	if err != nil {
		t.Fatalf("a refused upgrade must not fail the open: %v", err)
	}
	refused := loggedRefusal(t, logs.String())
	for _, want := range []string{`property "region": default us-east`, `property "label"`} {
		if !strings.Contains(refused, want) {
			t.Fatalf("the log names one guard kind and not the other; missing %q in: %s", want, refused)
		}
	}
	plan := planCore(t, ds)
	if got := strings.Join(plan.Upgrade.Blockers, "; "); got != refused {
		t.Fatalf("preview blockers:\n  %s\nlogged refused:\n  %s", got, refused)
	}
}
