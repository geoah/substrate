package runner

// THE TWO SDKs SPELL ONE GRAMMAR. host.py's `_RE_KIND` and substratefn's
// `reKind` are the same regular expression written twice, so a body learns its
// mistake in the runtime it is written in rather than at admission, and the two
// runtimes never disagree about what a kind reference is. This runs the Python
// side against the same table the Go side asserts (substratefn/kindref_test.go).

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestHostPythonKindGrammarMatchesTheEngine(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not on PATH; the Python SDK's grammar is checked where it is")
	}
	// _need_kind is the door every staged effect passes: it answers with the
	// value or raises, so the probe reports one bool per case.
	const probe = `
import json, sys, importlib.util
spec = importlib.util.spec_from_file_location("host", "host.py")
host = importlib.util.module_from_spec(spec)
spec.loader.exec_module(host)
out = {}
for case in json.load(sys.stdin):
    try:
        host._need_kind("put", case)
        out[case] = True
    except ValueError:
        out[case] = False
print(json.dumps(out))
`
	admitted := []string{
		"widget",
		"samples.substrate.reamde.dev/tasks/task",
		"acme.example.com/tools/widget2",
		"a.b/c/d",
	}
	refused := []string{
		"",
		// The retired two-segment form: an authority and a name, no package.
		"samples.substrate.reamde.dev/task",
		"acme.example.com/widget",
		// Four segments is a record path, not a kind.
		"acme.example.com/tools/widget/w1",
		"acme.example.com/Tools/widget",
		"acme.example.com/tools/Widget",
		"acme/tools/widget",
	}
	cases, err := json.Marshal(append(append([]string{}, admitted...), refused...))
	if err != nil {
		t.Fatal(err)
	}
	// -B: no bytecode. The probe imports host.py from the package directory,
	// and a __pycache__ beside it is a build artifact in a tracked tree.
	cmd := exec.Command(python, "-B", "-c", probe)
	cmd.Dir = "."
	cmd.Stdin = strings.NewReader(string(cases))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("run the probe against host.py: %v", err)
	}
	var got map[string]bool
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decode the probe's answer %q: %v", out, err)
	}
	for _, ok := range admitted {
		if !got[ok] {
			t.Errorf("host.py refuses %q, which the engine admits", ok)
		}
	}
	for _, bad := range refused {
		if got[bad] {
			t.Errorf("host.py admits %q, which the engine refuses", bad)
		}
	}
}
