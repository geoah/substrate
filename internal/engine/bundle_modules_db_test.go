package engine_test

// The shared-module wiring end to end: a bundle ships a `modules:`
// library and one of its functions imports it. The chain under test is
// runnerSpec attaching BundleOf(fn.Package).Modules → the runner writing the
// module onto a bundle-scoped PYTHONPATH → the python body importing it.

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

const mbImpFn = mbPackage + "/imp"

// mbModuleDocs is the standard closure plus a function that imports the shared
// module, and a `modules:` section carrying that module's source. Modules are
// inline sources on the bundle document, NOT closure members, so installs is
// unchanged.
func mbModuleDocs(modules map[string]any) []map[string]any {
	members := []map[string]any{
		mbConfigTypeDoc(), mbAccountTypeDoc(), mbItemTypeDoc(), mbMessageTypeDoc(),
		mbFnDoc("mark", mbMarkSource), mbFnDoc("echo", mbEchoSource),
		mbFnDoc("imp", `
import connkit
def main(input, host):
    return {"effects": [], "output": {"greeting": connkit.greet("mail")}}
`),
	}
	var installs []any
	for _, m := range members {
		meta := m["metadata"].(map[string]any)
		installs = append(installs, meta["id"].(string))
	}
	docs := []map[string]any{
		vocabulary.PackageManifest(mbPackage, 0),
		vocabulary.ActorManifest(mbPackage, vocabulary.PackageActor(mbPackage)),
		vocabulary.BundleManifest(mbPackage, map[string]any{
			"description": "the mail bundle",
			"inputs": map[string]any{
				"client": map[string]any{"kind": mbConfigType},
			},
			"installs": installs,
			"modules":  modules,
			"oauth2": map[string]any{
				"clientInput":           "client",
				"authorizationEndpoint": "https://provider.example/authorize",
				"tokenEndpoint":         "https://provider.example/token",
				"revocationEndpoint":    "https://provider.example/revoke",
				"featureScopes": map[string]any{
					"enabledMail": map[string]any{"scopes": []any{"mail.read"}},
				},
			},
		}),
	}
	return append(docs, members...)
}

func goodModules() map[string]any {
	return map[string]any{"connkit.py": "def greet(who):\n    return 'hi ' + who\n"}
}

func TestBundleSharedModuleImportable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	sa := applier(t, ds)

	// A bad module is a load error, refused at admission before the bundle
	// installs — a non-.py/.go extension and an empty source both.
	if _, err := sa.ApplyVocabularyDocuments(ctx, owner,
		mbModuleDocs(map[string]any{"connkit.txt": "x = 1\n"})); err == nil ||
		!strings.Contains(err.Error(), ".py or .go") {
		t.Fatalf("a non-.py/.go module was admitted: %v", err)
	}
	if _, err := sa.ApplyVocabularyDocuments(ctx, owner,
		mbModuleDocs(map[string]any{"connkit.py": "  \n"})); err == nil ||
		!strings.Contains(err.Error(), "source is required") {
		t.Fatalf("an empty module was admitted: %v", err)
	}

	// Finding #11: a `.py` module that shadows the runner host, auto-runs at
	// interpreter startup, or shadows the stdlib serializer is refused at
	// admission — the protocol host can never be corrupted before it is
	// established.
	for _, name := range []string{"sitecustomize.py", "host.py"} {
		if _, err := sa.ApplyVocabularyDocuments(ctx, owner,
			mbModuleDocs(map[string]any{name: "x = 1\n"})); err == nil ||
			!strings.Contains(err.Error(), "reserved module name") {
			t.Fatalf("a reserved module %q was admitted: %v", name, err)
		}
	}
	if _, err := sa.ApplyVocabularyDocuments(ctx, owner,
		mbModuleDocs(map[string]any{"json.py": "x = 1\n"})); err == nil ||
		!strings.Contains(err.Error(), "standard-library module") {
		t.Fatalf("a stdlib-shadowing module json.py was admitted: %v", err)
	}

	if _, err := sa.ApplyVocabularyDocuments(ctx, owner, mbModuleDocs(goodModules())); err != nil {
		t.Fatalf("install bundle with modules: %v", err)
	}

	fops := ds.(fnOps)
	out, _, err := fops.CallFunction(ctx, mbImpFn, map[string]any{})
	if err != nil {
		t.Fatalf("call function importing the shared module: %v", err)
	}
	m, ok := out.(map[string]any)
	if !ok || m["greeting"] != "hi mail" {
		t.Fatalf("shared module not imported by the function: %v", out)
	}

	// Sanity: modules are not smuggled into the install closure.
	if _, err := ds.Get(ctx, "substrate.reamde.dev/core/function", "connkit.py"); err == nil {
		t.Fatal("a shared module became a record")
	}
}

// A MODULE-ONLY change re-prepares the bodies that import it.
//
// "Bodies prepare at registration" is the contract: every function the batch
// adds or changes must compile or register NOW, and the first failure fails
// the whole batch as an admission error. The skip that implements "unchanged,
// already prepared" used to compare runtime and source alone — a narrower
// identity than the one being warmed, because runner.Spec.Key() hashes the
// body, its bundle's shared modules AND the enforced capability envelope.
//
// So a bundle that edited only its library changed no function's source, every
// body skipped preparation, and a module that cannot load reached activation:
// the installer saw a clean install and the FIRST DELIVERY parked instead.
// That is exactly the failure the prepare phase exists to move forward.
func TestModuleOnlyChangeStillPreparesTheBodiesThatImportIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	sa := applier(t, ds)

	if _, err := sa.ApplyVocabularyDocuments(ctx, owner, mbModuleDocs(goodModules())); err != nil {
		t.Fatalf("install bundle with modules: %v", err)
	}

	// Every function source is byte-identical to the installed one; only the
	// module moved, and it no longer parses. The importing body cannot
	// register against it, so admission must refuse the batch.
	broken := map[string]any{"connkit.py": "def greet(who:\n    return 'hi ' + who\n"}
	_, err := sa.ApplyVocabularyDocuments(ctx, owner, mbModuleDocs(broken))
	if err == nil {
		t.Fatal("a module-only change that breaks an importing body was admitted — " +
			"the bodies skipped preparation and the first delivery will park")
	}
	if !strings.Contains(err.Error(), "failed to prepare") {
		t.Fatalf("refused, but not by the prepare phase: %v", err)
	}

	// The refusal left the install alone: the old module is still what the
	// function imports, because preparation happens BEFORE the transaction.
	fops := ds.(fnOps)
	out, _, err := fops.CallFunction(ctx, mbImpFn, map[string]any{})
	if err != nil {
		t.Fatalf("call after the refused batch: %v", err)
	}
	if m, ok := out.(map[string]any); !ok || m["greeting"] != "hi mail" {
		t.Fatalf("the refused batch disturbed the installed module: %v", out)
	}

	// And a module-only change that DOES load lands, body untouched.
	fixed := map[string]any{"connkit.py": "def greet(who):\n    return 'hey ' + who\n"}
	if _, err := sa.ApplyVocabularyDocuments(ctx, owner, mbModuleDocs(fixed)); err != nil {
		t.Fatalf("a valid module-only change was refused: %v", err)
	}
	out, _, err = fops.CallFunction(ctx, mbImpFn, map[string]any{})
	if err != nil {
		t.Fatalf("call after the module-only change: %v", err)
	}
	if m, ok := out.(map[string]any); !ok || m["greeting"] != "hey mail" {
		t.Fatalf("the module-only change did not reach the body: %v", out)
	}
}
