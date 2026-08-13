package runner

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// The connector-ergonomics half of the runner: PEP 723 detection and uv
// dispatch, the isolated-vs-shared path decision, and shared bundle modules on
// the import path. The uv-run tests skip (never fail) when uv is absent from
// the sandbox, and the pure-Go detection/dispatch checks run everywhere.

// hasUV reports whether `uv` is on PATH; the integration tests skip without it.
func hasUV() bool {
	_, err := exec.LookPath("uv")
	return err == nil
}

func TestPEP723Detection(t *testing.T) {
	// A body with a `# /// script` block is detected; a dependency-free body
	// and a non-script block are not. Pure Go — runs everywhere.
	withDeps := `# /// script
# requires-python = ">=3.11"
# dependencies = ["six"]
# ///
def main(input, host):
    return {}
`
	block, ok := pep723Block(withDeps)
	if !ok || !strings.Contains(block, "dependencies") {
		t.Fatalf("declared deps not detected: ok=%v block=%q", ok, block)
	}
	plain := "def main(input, host):\n    return {}\n"
	if _, ok := pep723Block(plain); ok {
		t.Fatal("a dependency-free body was flagged as PEP 723")
	}
	// A `# /// pyproject` block is not a script block.
	other := "# /// pyproject\n# [tool]\n# ///\ndef main(input, host):\n    return {}\n"
	if _, ok := pep723Block(other); ok {
		t.Fatal("a non-script block was taken as PEP 723")
	}
}

func TestPathClassification(t *testing.T) {
	// The dispatch decision, no process started. Every python body now gets
	// its own process, so the only question left is whether uv has to
	// provision one first — which is exactly the PEP 723 declaration and
	// nothing else.
	plain := Spec{Runtime: "python", Source: "def main(input, host):\n    return {}\n"}
	if _, uv := plain.pep723(); uv {
		t.Fatal("a plain body needs no uv")
	}

	deps := Spec{Runtime: "python", Source: "# /// script\n# dependencies = [\"six\"]\n# ///\ndef main(input, host):\n    return {}\n"}
	if _, uv := deps.pep723(); !uv {
		t.Fatal("a PEP 723 body must be provisioned by uv")
	}

	mods := Spec{
		Runtime: "python", Source: "def main(input, host):\n    return {}\n",
		Modules: map[string]string{"helper.py": "x = 1\n"},
	}
	if _, uv := mods.pep723(); uv {
		t.Fatal("a shared-module body with no deps needs no uv")
	}
	if len(mods.pythonModules()) != 1 {
		t.Fatal("a `.py` module must land on the import path")
	}
}

func TestWorkDirIsolatedPerInstallation(t *testing.T) {
	// Finding #12: two byte-identical modules in DIFFERENT installations must
	// materialize to separate on-disk work dirs, so one can never mutate a file
	// the other reloads. The content hash is deliberately equal; the work dir
	// (keyed on the full installation Key) must differ.
	base := Spec{
		Runtime: "python", Source: "def main(input, host):\n    return {}\n",
		Modules: map[string]string{"helper.py": "x = 1\n"},
	}
	a := base
	a.Repository, a.Function = "repoA", "fn.g.test"
	b := base
	b.Repository, b.Function = "repoB", "fn.g.test"

	if a.contentHash() != b.contentHash() {
		t.Fatalf("byte-identical bodies should share a content hash: %s vs %s", a.contentHash(), b.contentHash())
	}
	if a.workID() == b.workID() {
		t.Fatalf("two installations shared a work dir %q — a module one mutates the other reloads", a.workID())
	}
	// A different function in the same repository is also its own installation.
	c := base
	c.Repository, c.Function = "repoA", "other.g.test"
	if a.workID() == c.workID() {
		t.Fatal("two functions in one repository shared a work dir")
	}
	// The same installation is stable across calls (a restart reuses its dir).
	if a.workID() != (Spec{
		Runtime: "python", Source: base.Source, Modules: base.Modules,
		Repository: "repoA", Function: "fn.g.test",
	}).workID() {
		t.Fatal("the work dir is not stable for one installation")
	}
}

func TestEveryBodyGetsItsOwnProcess(t *testing.T) {
	// The placement rule, asserted directly: two bodies, two processes, keyed
	// by installation. There is no shared host to fall back to and no fast
	// path that skips the boundary.
	r := New()
	src := "def main(input, host):\n    return {\"output\": \"ok\"}\n"
	a := Spec{Repository: "t1", Function: "a.g.test", Runtime: "python", Source: src, TimeoutMs: 5000}
	b := Spec{Repository: "t1", Function: "b.g.test", Runtime: "python", Source: src, TimeoutMs: 5000}
	for _, spec := range []Spec{a, b} {
		if res, err := r.Invoke(context.Background(), spec, testInput(), nil); err != nil || res.Output != "ok" {
			t.Fatalf("invoke %s: %+v %v", spec.Function, res, err)
		}
	}
	r.mu.Lock()
	pa, pb, n := r.pys[a.Key()], r.pys[b.Key()], len(r.pys)
	r.mu.Unlock()
	if n != 2 || pa == nil || pb == nil {
		t.Fatalf("expected one process per installation, got %d", n)
	}
	if pa == pb {
		t.Fatal("two functions shared an interpreter")
	}
}

func TestSharedModuleImportable(t *testing.T) {
	// A bundle's shared `.py` module is importable from a function, off the
	// installation's own module dir, no uv needed.
	r := New()
	spec := Spec{
		Repository: "t1", Function: "user.g.test", Runtime: "python",
		Source: `
import connkit
def main(input, host):
    return {"output": connkit.greet("world")}
`,
		TimeoutMs: 5000,
		Modules:   map[string]string{"connkit.py": "def greet(who):\n    return 'hi ' + who\n"},
	}
	res, err := r.Invoke(context.Background(), spec, testInput(), nil)
	if err != nil {
		t.Fatalf("shared-module invoke: %v", err)
	}
	if res.Output != "hi world" {
		t.Fatalf("shared module not imported: %v", res.Output)
	}
	r.mu.Lock()
	live := len(r.pys)
	r.mu.Unlock()
	if live != 1 {
		t.Fatalf("expected one process for the one installation, got %d", live)
	}
}

func TestGoSharedModuleImportable(t *testing.T) {
	// A bundle's shared `.go` module is vendored into the build as the
	// `substratefn.local/lib` package and importable from the body. Compiles with
	// the host toolchain, so it skips in short mode like the Go build test.
	if testing.Short() {
		t.Skip("compiles with the host toolchain")
	}
	r := New()
	spec := Spec{
		Repository: "t1", Function: "gouser.g.test", Runtime: "go",
		Source: `
import (
	"substratefn.local/lib"
	"substratefn.local/substratefn"
)

func Main(in *substratefn.Input, host *substratefn.Host) (*substratefn.Result, error) {
	return &substratefn.Result{Output: lib.Greet("mail")}, nil
}
`,
		TimeoutMs: 30000,
		Modules:   map[string]string{"greet.go": "package lib\n\nfunc Greet(who string) string { return \"hi \" + who }\n"},
	}
	res, err := r.Invoke(context.Background(), spec, testInput(), widgetBackend())
	if err != nil {
		t.Fatalf("go shared-module invoke: %v", err)
	}
	if res.Output != "hi mail" {
		t.Fatalf("go shared module not imported: %v", res.Output)
	}
}

func TestUVDispatchRunsBody(t *testing.T) {
	// A PEP 723 body runs through `uv run`, provisioning its own env. No
	// external dependency here (requires-python only), so it needs no network —
	// it proves dispatch and the intact protocol (host calls, effects).
	if !hasUV() {
		t.Skip("uv not on PATH — install uv to exercise the PEP 723 path")
	}
	r := New()
	spec := Spec{
		Repository: "t1", Function: "uvfn.g.test", Runtime: "python",
		Source: `# /// script
# requires-python = ">=3.9"
# ///
import sys
def main(input, host):
    got = host.get("widget.g.test", "w1")
    return {"effects": [{"action": "put", "kind": "widget.g.test", "id": "out"}],
            "output": {"py": sys.version_info[0], "from": got["id"]}}
`,
		TimeoutMs: 60000,
		ReadTypes: []string{"widget.g.test"},
	}
	res, err := r.Invoke(context.Background(), spec, testInput(), widgetBackend())
	if err != nil {
		t.Fatalf("uv invoke: %v", err)
	}
	out, ok := res.Output.(map[string]any)
	if !ok || out["from"] != "w1" || out["py"] != float64(3) {
		t.Fatalf("uv body output: %v", res.Output)
	}
	if len(res.Effects) != 1 {
		t.Fatalf("effects across the uv path: %v", res.Effects)
	}
	r.mu.Lock()
	live := len(r.pys)
	r.mu.Unlock()
	if live != 1 {
		t.Fatalf("expected one process for the uv body, got %d", live)
	}
}

func TestUVDeclaredDependencyImport(t *testing.T) {
	// A declared PyPI dependency is importable through the uv path. Skips
	// without uv; skips (not fails) when resolution cannot reach the network,
	// so the suite stays green offline.
	if !hasUV() {
		t.Skip("uv not on PATH — install uv to exercise dependency provisioning")
	}
	r := New()
	spec := Spec{
		Repository: "t1", Function: "dep.g.test", Runtime: "python",
		Source: `# /// script
# requires-python = ">=3.9"
# dependencies = ["six"]
# ///
import six
def main(input, host):
    return {"output": six.__name__}
`,
		TimeoutMs: 120000,
	}
	res, err := r.Invoke(context.Background(), spec, testInput(), nil)
	if err != nil {
		if strings.Contains(err.Error(), "register") {
			t.Skipf("uv could not provision the dependency (offline sandbox?): %v", err)
		}
		t.Fatalf("uv dependency invoke: %v", err)
	}
	if res.Output != "six" {
		t.Fatalf("declared dependency not imported: %v", res.Output)
	}
}
