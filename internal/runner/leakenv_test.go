package runner

import (
	"context"
	"testing"
)

// Ticket 017: a bundle body is arbitrary third-party code running as the same
// container user, so it must NOT be able to read the substrate's master
// secrets out of its own process environment. Every runner child is started
// with a minimal allowlisted env (childEnv), never os.Environ() — these tests
// set the secrets on the HOST (parent) process and prove a body reads them
// back EMPTY across every exec path.

// hostSecretNames are the master secrets that must never reach a child: the
// credential-seal key (unseals every repository's OAuth), DATABASE_URL (direct
// Postgres as the app role), the operator OTP, the OAuth state key, and a
// LiteLLM key.
var hostSecretNames = []string{
	"SUBSTRATE_CREDENTIAL_KEY",
	"DATABASE_URL",
	"SUBSTRATE_OPERATOR_OTP",
	"SUBSTRATE_OAUTH_STATE_KEY",
	"LITELLM_API_KEY",
}

// setHostSecrets plants a recognizable value for every master secret on the
// parent process; t.Setenv restores them when the test ends.
func setHostSecrets(t *testing.T) {
	t.Helper()
	for _, n := range hostSecretNames {
		t.Setenv(n, "LEAK-"+n)
	}
}

// pySecretProbe is a python body that returns every host-secret env var it can
// read; the allowlisted child env must make each one empty.
const pySecretProbe = `
import os
def main(input, host):
    names = ["SUBSTRATE_CREDENTIAL_KEY", "DATABASE_URL", "SUBSTRATE_OPERATOR_OTP",
             "SUBSTRATE_OAUTH_STATE_KEY", "LITELLM_API_KEY"]
    return {"output": {"leaked": {n: os.environ.get(n, "") for n in names},
                       "hasPath": bool(os.environ.get("PATH", ""))}}
`

func assertNoLeak(t *testing.T, out any) {
	t.Helper()
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("unexpected output shape: %#v", out)
	}
	leaked, ok := m["leaked"].(map[string]any)
	if !ok {
		t.Fatalf("no leaked map in output: %#v", out)
	}
	for _, n := range hostSecretNames {
		if v := leaked[n]; v != "" {
			t.Fatalf("host secret %s leaked into the child env: %q", n, v)
		}
	}
	// The allowlist is minimal but not empty: a legitimate PATH still reaches
	// the child, proving childEnv is a real base and not an accidental wipe.
	if m["hasPath"] != true {
		t.Fatalf("childEnv dropped PATH — the allowlist is broken, not just secure: %#v", out)
	}
}

func TestSharedPythonChildEnvOmitsHostSecrets(t *testing.T) {
	setHostSecrets(t)
	r := New()
	spec := Spec{
		Repository: "t1", Function: "leak.g.test",
		Runtime: "python", Source: pySecretProbe, TimeoutMs: 5000,
	}
	res, err := r.Invoke(context.Background(), spec, testInput(), nil)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	assertNoLeak(t, res.Output)
}

func TestIsolatedPythonChildEnvOmitsHostSecrets(t *testing.T) {
	setHostSecrets(t)
	r := New()
	// A shared `.py` module forces the ISOLATED path (startIsoPython) without
	// needing uv or the network — that path is the one that used to do
	// append(os.Environ(), …).
	spec := Spec{
		Repository: "t1", Function: "leakiso.g.test",
		Runtime: "python", Source: pySecretProbe, TimeoutMs: 5000,
		Modules: map[string]string{"unused.py": "x = 1\n"},
	}
	if !spec.pythonIsolated() {
		t.Fatal("test setup: expected the isolated path")
	}
	res, err := r.Invoke(context.Background(), spec, testInput(), nil)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	assertNoLeak(t, res.Output)
}

func TestGoChildEnvOmitsHostSecrets(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles with the host toolchain")
	}
	setHostSecrets(t)
	r := New()
	spec := Spec{
		Repository: "t1", Function: "goleak.g.test",
		Runtime: "go",
		Source: `
import (
	"os"

	"substratefn.local/substratefn"
)

func Main(in *substratefn.Input, host *substratefn.Host) (*substratefn.Result, error) {
	names := []string{"SUBSTRATE_CREDENTIAL_KEY", "DATABASE_URL", "SUBSTRATE_OPERATOR_OTP",
		"SUBSTRATE_OAUTH_STATE_KEY", "LITELLM_API_KEY"}
	leaked := map[string]any{}
	for _, n := range names {
		leaked[n] = os.Getenv(n)
	}
	return &substratefn.Result{Output: map[string]any{
		"leaked":  leaked,
		"hasPath": os.Getenv("PATH") != "",
	}}, nil
}
`,
		TimeoutMs: 30000,
	}
	res, err := r.Invoke(context.Background(), spec, testInput(), widgetBackend())
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	assertNoLeak(t, res.Output)
}
