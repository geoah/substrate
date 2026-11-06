package providertest

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/sandboxtest"
	"github.com/geoah/substrate/internal/vocabulary"
)

// TestBodyHelpersGoogle runs internal/providertest/py/test_google_trait.py
// over the shipped google sync bodies: the account-patch rules a body alone
// decides (the sync trait's rollup, the request's answer) in seconds, with no
// database and no uv, so the short suite holds them. The bodies are written
// from the parsed bundle.yaml, exactly the source the runner registers.
func TestBodyHelpersGoogle(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	written := writeBodies(t, providersDir+"/google/bundle.yaml", dir, func(name string) bool {
		return strings.HasPrefix(name, "sync")
	})
	if written != 4 {
		t.Fatalf("wrote %d google sync bodies, want the four streams' (contacts, gmail, calendar, drive)", written)
	}
	runBodyCases(t, bodyPython(t, dir), "py/test_google_trait.py", dir)
}

// TestBodyHelpersBeeper runs internal/providertest/py/test_beeper_offline.py
// over the shipped beeper messagessync body: which failures mean Beeper
// Desktop is not reachable, and the throttled stamp that pauses the sync
// instead of failing it (decision 0183).
func TestBodyHelpersBeeper(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	written := writeBodies(t, providersDir+"/beeper/bundle.yaml", dir, func(name string) bool {
		return name == "messagessync"
	})
	if written != 1 {
		t.Fatalf("wrote %d beeper bodies, want messagessync alone", written)
	}
	runBodyCases(t, bodyPython(t, dir), "py/test_beeper_offline.py", dir)
}

// TestBodyHelpersFirecrawl runs internal/providertest/py/test_firecrawl_api.py
// over the shipped firecrawl_api module: the origin the key may go to, that
// a call with no baseUrl (what the operator's key arrives with) goes to the
// pinned origin, and that the opener refuses every redirect.
func TestBodyHelpersFirecrawl(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if written := writeModules(t, firecrawlDir+"/bundle.yaml", dir); written != 1 {
		t.Fatalf("wrote %d firecrawl modules, want firecrawl_api.py alone", written)
	}
	runBodyCases(t, bodyPython(t, dir), "py/test_firecrawl_api.py", dir)
}

// writeModules writes every shared module the bundle document ships into dir
// under its own file name, and returns how many.
func writeModules(t *testing.T, bundle, dir string) int {
	t.Helper()
	written := 0
	for _, d := range loadDocs(t, bundle) {
		if d["kind"] != vocabulary.CoreKind(vocabulary.DocBundle) {
			continue
		}
		data, _ := d["data"].(map[string]any)
		modules, _ := data["modules"].(map[string]any)
		for name, src := range modules {
			text, _ := src.(string)
			if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
				t.Fatal(err)
			}
			written++
		}
	}
	return written
}

// bodyPython is an interpreter every body written into dir runs on. A body
// with no PEP 723 block runs on the deployment's own python3, which may be as
// old as the host's floor (.ruff.toml), so python3 is the one to test it on. A
// block's requires-python may ask for more than the python3 on PATH has:
// beeper's asks for >=3.11, whose datetime.fromisoformat reads the one-digit
// fraction of a second 3.9's refuses. uv picks an installed interpreter every
// floor admits, and never downloads one. CI's short suite sets the sandbox
// gate, and there a missing interpreter is a failure rather than a skip.
func bodyPython(t *testing.T, dir string) string {
	t.Helper()
	bodies, err := filepath.Glob(filepath.Join(dir, "*.py"))
	if err != nil {
		t.Fatal(err)
	}
	var floors []string
	for _, body := range bodies {
		src, err := os.ReadFile(body)
		if err != nil {
			t.Fatal(err)
		}
		if floor := requiresPython(t, body, string(src)); floor != "" {
			floors = append(floors, floor)
		}
	}
	if len(floors) == 0 {
		python, err := exec.LookPath("python3")
		if err != nil {
			sandboxtest.Unavailablef(t, "python3 not on PATH: the body helpers run under it")
		}
		return python
	}
	slices.Sort(floors)
	spec := strings.Join(slices.Compact(floors), ",")
	uv, err := exec.LookPath("uv")
	if err != nil {
		sandboxtest.Unavailablef(t, "uv not on PATH: it finds the Python %s the bodies require", spec)
	}
	var stderr bytes.Buffer
	find := exec.Command(uv, "python", "find", spec)
	find.Stderr = &stderr
	out, err := find.Output()
	if err != nil {
		sandboxtest.Unavailablef(t, "no installed Python satisfies %s, which the bodies require (uv python install '%s' adds one): uv python find: %v\n%s",
			spec, spec, err, stderr.String())
	}
	return strings.TrimSpace(string(out))
}

// scriptBlockRe is the pattern PEP 723 gives for a metadata block, as
// internal/runner/pep723.go reads it.
var scriptBlockRe = regexp.MustCompile(`(?m)^# /// (?P<type>[a-zA-Z0-9-]+)$\s(?P<content>(^#(| .*)$\s)+)^# ///$`)

// requiresPythonRe is a script block's requires-python in the spelling every
// shipped body uses: a TOML basic string on a line of its own.
var requiresPythonRe = regexp.MustCompile(`(?m)^# requires-python = "([^"]*)"$`)

// requiresPython is the requires-python of the body's PEP 723 script block,
// or "" where it declares none. PEP 723 allows one script block.
func requiresPython(t *testing.T, body, src string) string {
	t.Helper()
	for _, m := range scriptBlockRe.FindAllStringSubmatch(src, -1) {
		if m[1] != "script" {
			continue
		}
		if floor := requiresPythonRe.FindStringSubmatch(m[2]); floor != nil {
			return floor[1]
		}
		// A spelling the pattern misses would run the body under python3,
		// whatever it asks for.
		if strings.Contains(m[2], "requires-python") {
			t.Fatalf("%s: its script block names a requires-python this test cannot read:\n%s", body, m[0])
		}
		return ""
	}
	return ""
}

// writeBodies writes the source of every function in the bundle whose last
// id segment `want` accepts into dir as <name>.py, and returns how many.
func writeBodies(t *testing.T, bundle, dir string, want func(name string) bool) int {
	t.Helper()
	written := 0
	for _, d := range loadDocs(t, bundle) {
		if d["kind"] != vocabulary.CoreKind(vocabulary.DocFunction) {
			continue
		}
		meta, _ := d["metadata"].(map[string]any)
		id, _ := meta["id"].(string)
		data, _ := d["data"].(map[string]any)
		src, _ := data["source"].(string)
		name := id[strings.LastIndex(id, "/")+1:]
		if !want(name) || src == "" {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name+".py"), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		written++
	}
	return written
}

func runBodyCases(t *testing.T, python, rel, dir string) {
	t.Helper()
	script, err := filepath.Abs(rel)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(python, "-I", script)
	cmd.Env = append(os.Environ(), "SUBSTRATE_BODIES_DIR="+dir)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s -I %s: %v\n%s", python, script, err, out.String())
	}
	// unittest exits 0 on a run of no tests too, which would hold nothing.
	if !strings.Contains(out.String(), "\nOK") || strings.Contains(out.String(), "Ran 0 tests") {
		t.Fatalf("the python cases did not run:\n%s", out.String())
	}
	t.Logf("%s -I %s:\n%s", python, rel, out.String())
}
