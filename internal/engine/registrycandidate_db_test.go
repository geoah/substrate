package engine_test

// A BATCH READS ITS CANDIDATE (#150). Inside a vocabulary apply the live
// registry still holds the declarations being replaced, because the pointer
// swaps only after the commit. Every consumer inside the batch reads the
// candidate instead (engine.txn.declarations), so a closure's own data
// documents are resolved, referenced, mapped and admitted against the closure
// being installed. Before that, `extra` puts read the live pointer: a record
// of a kind the same batch declared was refused as an unknown kind.

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const candPackage = "candidate.example.substrate.reamde.dev/candidate"

// One closure declares four kinds, a mapping and a function, and ships data
// documents that need every one of them: a widget, a holder whose reference
// must resolve to that widget, a mirror whose mapping mints its subject, and a
// trigger on widgets naming the function. None of it exists in the registry
// the repository holds when the batch opens. Not exercised here: checkBundleWrite
// and checkBundleOwnerGate (the install's transaction is internal, which both
// skip) and checkCreateID (no mapping targets widget, so its explicit id is
// admitted).
func TestABatchWritesAgainstItsOwnCandidate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newCoreDataset(t)
	inst, ok := ds.(substrate.BundleInstaller)
	if !ok {
		t.Fatal("dataset does not implement the closure-install seam")
	}
	const (
		widget  = candPackage + "/widget"
		holder  = candPackage + "/holder"
		mirror  = candPackage + "/mirror"
		subject = candPackage + "/subject"
		trigger = "substrate.reamde.dev/core/trigger"
	)
	closure := []map[string]any{
		vocabulary.PackageManifest(candPackage, 0),
		vocabulary.ActorManifest(candPackage, vocabulary.PackageActor(candPackage)),
		vocabulary.BundleManifest(candPackage, map[string]any{
			"description": "a closure whose data documents are held to its own candidate",
			"installs": []any{
				widget, holder, mirror, subject,
				candPackage + "/mirrorsubject", candPackage + "/onwidget",
			},
		}),
		vocabulary.KindManifest(candPackage,
			map[string]any{"singular": "widget"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
		vocabulary.KindManifest(candPackage,
			map[string]any{"singular": "holder"},
			map[string]any{"properties": map[string]any{
				"points": map[string]any{"type": "reference", "kind": widget, "mustExist": true},
			}}),
		vocabulary.KindManifest(candPackage,
			map[string]any{"singular": "subject"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
		vocabulary.KindManifest(candPackage,
			map[string]any{"singular": "mirror"},
			map[string]any{"properties": map[string]any{
				"name": map[string]any{"type": "string"},
				"subject": map[string]any{
					"type": "reference", "kind": subject,
					"required": true, "mustExist": true, "subject": true,
				},
			}}),
		vocabulary.MappingManifest(candPackage, "mirrorsubject", map[string]any{
			"from": mirror, "to": subject, "property": "subject",
			"map": map[string]any{"name": map[string]any{"path": "name"}},
		}),
		vocabulary.FunctionManifest(candPackage, "onwidget", map[string]any{
			"description": "does nothing on a widget",
			"runtime":     vocabulary.RuntimePython,
			"permissions": map[string]any{"writes": []any{widget}},
			"source": `
def main(input, host):
    return {"effects": []}
`,
		}),
	}
	data := []substrate.PutInput{
		{Kind: widget, ID: "w1", Properties: map[string]any{"name": "first"}},
		{Kind: holder, ID: "h1", Properties: map[string]any{"points": "w1"}},
		{Kind: mirror, ID: "m1", Properties: map[string]any{"name": "mirrored"}},
		{Kind: trigger, ID: "on-widget", Properties: map[string]any{
			"enabled":  true,
			"source":   map[string]any{"record": map[string]any{"kinds": []any{widget}}},
			"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", candPackage+"/onwidget"),
		}},
	}
	if _, err := inst.InstallBundleClosure(ctx, owner, closure, data, substrate.BundleInstall{}); err != nil {
		t.Fatalf("a closure must be able to ship records of the kinds it declares: %v", err)
	}

	mustGet(t, ds, widget, "w1")
	if got := refPathValue(mustGet(t, ds, holder, "h1"), "points"); got != vocabulary.RecordPath(widget, "w1") {
		t.Fatalf("the holder's reference resolved to %q, want the batch's own widget", got)
	}
	// The mapping declared in the batch minted the mirror's subject and mapped
	// the name onto it.
	subjKind, subjID, ok := vocabulary.SplitRecordPath(refPathValue(mustGet(t, ds, mirror, "m1"), "subject"))
	if !ok || subjKind != subject {
		t.Fatalf("the mirror's subject is %q/%q, want a minted %s", subjKind, subjID, subject)
	}
	if got := mustGet(t, ds, subject, subjID).Properties["name"]; got != "mirrored" {
		t.Fatalf("the mapping did not carry the name onto the minted subject: %v", got)
	}
	mustGet(t, ds, trigger, "on-widget")
}

// The tier of an actor the closure declares holds on the FIRST install. inTx
// resolves the transaction's tier against the live registry before the batch
// sets its candidate, so a closure installed under its own declared actor
// wrote manager rows at the reserved-actor default (machine) until a
// re-install; the apply now re-resolves the tier against the candidate.
func TestAClosureActorsDeclaredTierHoldsOnFirstInstall(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newCoreDataset(t)
	inst, ok := ds.(substrate.BundleInstaller)
	if !ok {
		t.Fatal("dataset does not implement the closure-install seam")
	}
	const (
		pkg    = "keeper.example.substrate.reamde.dev/keeper"
		widget = pkg + "/widget"
		keeper = substrate.Actor("connector:keeper")
	)
	actorDoc := vocabulary.ActorManifest(pkg, string(keeper))
	actorDoc["data"].(map[string]any)["tier"] = string(substrate.TierOwner)
	closure := []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		actorDoc,
		vocabulary.KindManifest(pkg,
			map[string]any{"singular": "widget"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
	}
	if _, err := inst.InstallBundleClosure(ctx, keeper, closure, []substrate.PutInput{
		{Kind: widget, ID: "w1", Properties: map[string]any{"name": "kept"}},
	}, substrate.BundleInstall{}); err != nil {
		t.Fatalf("install under the declared actor: %v", err)
	}
	m, ok := mustGet(t, ds, widget, "w1").PropertyMeta["name"]
	if !ok {
		t.Fatal("the installed record carries no propertyMeta for name")
	}
	if m.Manager != string(keeper) || m.Tier != substrate.TierOwner {
		t.Fatalf("name manager = %q at %q, want %q at %q on the first install", m.Manager, m.Tier, keeper, substrate.TierOwner)
	}
}
