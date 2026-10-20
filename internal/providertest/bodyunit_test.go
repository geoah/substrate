package providertest

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
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
	python := bodyPython(t)
	dir := t.TempDir()
	written := writeBodies(t, providersDir+"/google/bundle.yaml", dir, func(name string) bool {
		return strings.HasPrefix(name, "sync")
	})
	if written != 4 {
		t.Fatalf("wrote %d google sync bodies, want the four streams' (contacts, gmail, calendar, drive)", written)
	}
	runBodyCases(t, python, "py/test_google_trait.py", dir)
}

// TestBodyHelpersBeeper runs internal/providertest/py/test_beeper_offline.py
// over the shipped beeper messagessync body: which failures mean Beeper
// Desktop is not reachable, and the throttled stamp that pauses the sync
// instead of failing it (decision 0183).
func TestBodyHelpersBeeper(t *testing.T) {
	t.Parallel()
	python := bodyPython(t)
	dir := t.TempDir()
	written := writeBodies(t, providersDir+"/beeper/bundle.yaml", dir, func(name string) bool {
		return name == "messagessync"
	})
	if written != 1 {
		t.Fatalf("wrote %d beeper bodies, want messagessync alone", written)
	}
	runBodyCases(t, python, "py/test_beeper_offline.py", dir)
}

func bodyPython(t *testing.T) string {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		// CI's short suite sets the sandbox gate, and there a missing
		// interpreter is a failure rather than a skip.
		sandboxtest.Unavailablef(t, "python3 not on PATH: the body helpers run under it")
	}
	return python
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
		t.Fatalf("python3 -I %s: %v\n%s", script, err, out.String())
	}
	// unittest exits 0 on a run of no tests too, which would hold nothing.
	if !strings.Contains(out.String(), "\nOK") || strings.Contains(out.String(), "Ran 0 tests") {
		t.Fatalf("the python cases did not run:\n%s", out.String())
	}
	t.Logf("%s", out.String())
}
