package runner

// host.py is the SDK, so what it accepts is part of the contract. These two
// tests hold it to the engine's own kind grammar (vocabulary/naming.go) and to
// the protocol version this package pins. Both are written twice, once in Go
// and once in Python, and nothing but a test notices the two drifting.

import (
	"encoding/json"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The protocol number is written twice: protocol.go's ProtocolVersion, which
// documents the frames, and the integer host.py answers a describe with. The
// parent no longer negotiates it at startup (there is one SDK, shipped in the
// same binary), so nothing but this test would notice the two disagreeing, and
// the frames a reader trusts would be the ones nobody serves.
func TestHostPythonPinsTheProtocolVersion(t *testing.T) {
	src, err := os.ReadFile("host.py")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`"protocol":\s*(\d+)`).FindAllSubmatch(src, -1)
	if len(m) == 0 {
		t.Fatal("host.py answers no `protocol` in its describe response")
	}
	for _, hit := range m {
		got, err := strconv.Atoi(string(hit[1]))
		if err != nil {
			t.Fatal(err)
		}
		if got != ProtocolVersion {
			t.Errorf("host.py speaks protocol %d, protocol.go pins %d", got, ProtocolVersion)
		}
	}
}

func TestHostPythonKindGrammarMatchesTheEngine(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not on PATH; host.py's grammar cannot be probed here")
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
