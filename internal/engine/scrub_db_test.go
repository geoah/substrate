package engine_test

// The invocation scrubber's acceptance gate: a bundle function
// receives its config secrets RAW, but nothing that leaves the runner
// boundary carries them — outputs (and so API responses), error text (and so
// run rows and parked-failure rows). Exact-value scrubbing is containment,
// not a defense against a body that TRANSFORMS a secret before exfiltrating
// it — that follow-up needs opaque credential handles.

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	vAuthority  = "vault.bundles.substrate.reamde.dev"
	vConfigType = vAuthority + "/vaultconfig"
	vSecret     = "sk-live-supersecret-77"
)

// installVaultBundle stands up a bundle whose config carries one secret and
// one plain property, plus a function that spills its config and one that
// crashes with the secret in the exception text.
const vNoteType = vAuthority + "/vaultnote"

func installVaultBundle(t *testing.T) substrate.Dataset {
	t.Helper()
	_, ds := newDataset(t)
	fn := func(name, source string) map[string]any {
		return vocabulary.FunctionManifest(vAuthority, name, map[string]any{
			"description": "test function " + name,
			"runtime":     vocabulary.RuntimePython,
			"source":      source,
			"permissions": map[string]any{"writes": []any{vConfigType}},
		})
	}
	// noteFn emits the plain vaultnote type (and may host-call a target): the
	// leak/escape fixtures write ordinary rows carrying a secret verbatim.
	noteFn := func(name string, call []any, source string) map[string]any {
		data := map[string]any{
			"description": "test function " + name, "runtime": vocabulary.RuntimePython,
			"source": source, "permissions": map[string]any{"writes": []any{vNoteType}},
		}
		if call != nil {
			data["permissions"].(map[string]any)["call"] = call
		}
		return vocabulary.FunctionManifest(vAuthority, name, data)
	}
	docs := []map[string]any{
		vocabulary.AuthorityManifest(vAuthority, ""),
		vocabulary.BundleManifest(vAuthority, map[string]any{
			"description": "the vault bundle",
			"inputs": map[string]any{
				"connector": map[string]any{"kind": vConfigType, "inject": "functions"},
			},
			"installs": []any{
				vConfigType, vNoteType,
				vAuthority + "/spill", vAuthority + "/crash",
				vAuthority + "/leakval", vAuthority + "/leakid", vAuthority + "/callerleak",
			},
		}),
		vocabulary.KindManifest(vAuthority,
			map[string]any{"singular": "vaultconfig", "plural": "vaultconfigs"},
			map[string]any{
				"properties": map[string]any{
					"apiToken": map[string]any{"type": "secret"},
					"note":     map[string]any{"type": "string"},
				},
			}),
		vocabulary.KindManifest(vAuthority,
			map[string]any{"singular": "vaultnote", "plural": "vaultnotes"},
			map[string]any{"properties": map[string]any{"text": map[string]any{"type": "string"}}}),
		fn("spill", `
def main(input, host):
    props = input["config"]["inputs"]["connector"]["properties"]
    return {"effects": [], "output": {"stolen": props["apiToken"], "note": props["note"]}}
`),
		fn("crash", `
def main(input, host):
    props = input["config"]["inputs"]["connector"]["properties"]
    raise Exception("boom " + props["apiToken"])
`),
		// leakval copies the injected secret into a NON-secret property VALUE.
		noteFn("leakval", nil, `
def main(input, host):
    props = input["config"]["inputs"]["connector"]["properties"]
    return {"effects": [{"action": "put", "kind": "vault.bundles.substrate.reamde.dev/vaultnote",
                         "id": "leak-note", "properties": {"text": props["apiToken"]}}],
            "output": {}}
`),
		// leakid smuggles the injected secret into an ADDRESSED field (the id).
		noteFn("leakid", nil, `
def main(input, host):
    props = input["config"]["inputs"]["connector"]["properties"]
    return {"effects": [{"action": "put", "kind": "vault.bundles.substrate.reamde.dev/vaultnote",
                         "id": "leak-" + props["apiToken"], "properties": {"text": "x"}}],
            "output": {}}
`),
		// callerleak host-calls leakval: the callee's secret-bearing effect
		// must be rejected at the host-Call boundary too.
		noteFn("callerleak", []any{vAuthority + "/leakval"}, `
def main(input, host):
    host.call("vault.bundles.substrate.reamde.dev/leakval", {})
    return {"effects": [], "output": {}}
`),
	}
	if _, err := applier(t, ds).ApplyVocabularyDocuments(context.Background(), owner, docs); err != nil {
		t.Fatalf("install vault bundle: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: vConfigType, Properties: map[string]any{"apiToken": vSecret, "note": "plain"},
	})
	return ds
}

func TestScrubberHoldsFunctionOutput(t *testing.T) {
	t.Parallel()
	// The body sees the raw secret; the output crossing back out does not.
	ctx := context.Background()
	ds := installVaultBundle(t)
	out, _, err := ds.(fnOps).CallFunction(ctx, vAuthority+"/spill", map[string]any{})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	m, _ := out.(map[string]any)
	if m["stolen"] != engine.Redacted {
		t.Fatalf("the secret escaped through the output: %v", m["stolen"])
	}
	// Non-secret config values survive untouched.
	if m["note"] != "plain" {
		t.Fatalf("the scrubber shredded a plain value: %v", m["note"])
	}
}

func TestScrubberHoldsErrorsAndParkedFailures(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installVaultBundle(t)

	// A crash whose exception text embeds the secret: the call error is
	// scrubbed before anything (API response, run row) renders it.
	_, _, err := ds.(fnOps).CallFunction(ctx, vAuthority+"/crash", map[string]any{})
	if err == nil {
		t.Fatal("crash returned no error")
	}
	if strings.Contains(err.Error(), vSecret) {
		t.Fatalf("the secret escaped through the error text: %v", err)
	}
	if !strings.Contains(err.Error(), engine.Redacted) {
		t.Fatalf("the error text was not scrubbed in place: %v", err)
	}

	// The same crash through a trigger: the parked-failure row and the
	// parked run row hold the scrubbed text, never the raw secret.
	tr := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "core.substrate.reamde.dev/trigger",
		Properties: map[string]any{
			"enabled":  true,
			"source":   map[string]any{"record": map[string]any{"kinds": []any{vConfigType}}},
			"callable": vocabulary.RecordPath("core.substrate.reamde.dev/function", vAuthority+"/crash"),
		},
	})
	mustPatch(t, ds, owner, vConfigType, mustConfigID(t, ds), substrate.PatchInput{Properties: map[string]any{"note": "poke"}})
	if _, err := ds.(fnOps).ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	failures, err := ds.(fnOps).TriggerFailures(ctx, tr.ID)
	if err != nil {
		t.Fatalf("failures: %v", err)
	}
	if len(failures) == 0 {
		t.Fatal("the crash never parked")
	}
	for _, f := range failures {
		if strings.Contains(f.LastError, vSecret) {
			t.Fatalf("the secret escaped into a parked failure: %s", f.LastError)
		}
		if !strings.Contains(f.LastError, engine.Redacted) {
			t.Fatalf("the parked failure was not scrubbed in place: %s", f.LastError)
		}
	}
}

// noVaultNotes asserts nothing of the plain note type persisted — the secret
// never reached storage through a rejected effect.
func noVaultNotes(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	page, err := ds.List(context.Background(), substrate.Query{
		Filter: substrate.Filter{Kinds: []string{vNoteType}}, First: 5,
	})
	if err != nil {
		t.Fatalf("list notes: %v", err)
	}
	if len(page.Records) != 0 {
		t.Fatalf("a secret-bearing effect persisted: %d note(s)", len(page.Records))
	}
}

// Final review #3: an injected secret copied into a RETURNED effect — a plain
// property value or an addressed id — is rejected before decode, so nothing
// persists. The scrubber cannot redact addressed data in place, so refusal is
// the only safe move.
func TestScrubberRejectsSecretInEffectValue(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installVaultBundle(t)
	_, _, err := ds.(fnOps).CallFunction(ctx, vAuthority+"/leakval", map[string]any{})
	if err == nil {
		t.Fatal("a secret in an effect property value was applied")
	}
	if strings.Contains(err.Error(), vSecret) {
		t.Fatalf("the rejection error itself leaked the secret: %v", err)
	}
	noVaultNotes(t, ds)
}

func TestScrubberRejectsSecretInEffectID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installVaultBundle(t)
	_, _, err := ds.(fnOps).CallFunction(ctx, vAuthority+"/leakid", map[string]any{})
	if err == nil {
		t.Fatal("a secret in an effect id was applied")
	}
	if strings.Contains(err.Error(), vSecret) {
		t.Fatalf("the rejection error itself leaked the secret: %v", err)
	}
	noVaultNotes(t, ds)
}

// The host-Call boundary rejects a callee's secret-bearing effect too.
func TestScrubberRejectsSecretThroughHostCall(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installVaultBundle(t)
	_, _, err := ds.(fnOps).CallFunction(ctx, vAuthority+"/callerleak", map[string]any{})
	if err == nil {
		t.Fatal("a callee's secret effect escaped through a host Call")
	}
	if strings.Contains(err.Error(), vSecret) {
		t.Fatalf("the rejection error itself leaked the secret: %v", err)
	}
	noVaultNotes(t, ds)
}

// A parked trigger: the delivery whose callable returns a secret-bearing
// effect parks without persisting the effect, and neither the parked-failure
// row nor its run row carries the raw secret.
func TestScrubberRejectsSecretThroughParkedTrigger(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installVaultBundle(t)
	tr := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "core.substrate.reamde.dev/trigger",
		Properties: map[string]any{
			"enabled":  true,
			"source":   map[string]any{"record": map[string]any{"kinds": []any{vConfigType}}},
			"callable": vocabulary.RecordPath("core.substrate.reamde.dev/function", vAuthority+"/leakval"),
		},
	})
	mustPatch(t, ds, owner, vConfigType, mustConfigID(t, ds), substrate.PatchInput{Properties: map[string]any{"note": "poke"}})
	if _, err := ds.(fnOps).ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	failures, err := ds.(fnOps).TriggerFailures(ctx, tr.ID)
	if err != nil {
		t.Fatalf("failures: %v", err)
	}
	if len(failures) == 0 {
		t.Fatal("the secret-bearing delivery never parked")
	}
	for _, f := range failures {
		if strings.Contains(f.LastError, vSecret) {
			t.Fatalf("a parked failure leaked the secret: %s", f.LastError)
		}
	}
	noVaultNotes(t, ds)
}

// mustConfigID reads the single live vaultconfig row's id.
func mustConfigID(t *testing.T, ds substrate.Dataset) string {
	t.Helper()
	page, err := ds.List(context.Background(), substrate.Query{
		Filter: substrate.Filter{Kinds: []string{vConfigType}}, First: 2,
	})
	if err != nil || len(page.Records) != 1 {
		t.Fatalf("vaultconfig rows: %v %v", err, page)
	}
	return page.Records[0].ID
}
