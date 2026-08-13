package engine_test

// The shared-module wiring end to end: a bundle ships a `modules:`
// library and one of its functions imports it. The chain under test is
// runnerSpec attaching BundleOf(fn.Authority).Modules → the runner writing the
// module onto a bundle-scoped PYTHONPATH → the python body importing it.

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

const mbImpFn = mbAuthority + "/imp"

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
		vocabulary.AuthorityManifest(mbAuthority, ""),
		vocabulary.ActorManifest(mbAuthority, vocabulary.AuthorityActor(mbAuthority)),
		vocabulary.BundleManifest(mbAuthority, map[string]any{
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
				"featureScopes":         map[string]any{"enabledMail": []any{"mail.read"}},
			},
		}),
	}
	return append(docs, members...)
}

func goodModules() map[string]any {
	return map[string]any{"connkit.py": "def greet(who):\n    return 'hi ' + who\n"}
}

func TestBundleSharedModuleImportable(t *testing.T) {
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
	if _, err := ds.Get(ctx, "core.substrate.reamde.dev/function", "connkit.py"); err == nil {
		t.Fatal("a shared module became a record")
	}
}
