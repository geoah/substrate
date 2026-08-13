package testenv_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/testenv"
)

// The function sandbox, end to end: a real substrate, a real repository, a
// bundle installed through the real admission path, and its functions invoked
// over HTTP with a real token. Every assertion here is about a property no
// single package can establish on its own: the runner confines what it is
// handed, the engine decides what to hand it, and the API decides who may ask.

const probeAuthority = "probe.bundles.substrate.reamde.dev"

// probeBundle assembles a one-kind, N-function bundle around the given bodies.
// It is written the way a real bundle is written: an authority header, a
// bundle document whose `installs` names every member, a kind, then the
// functions: because the point of these tests is that a function arriving the
// ORDINARY way is confined.
func probeBundle(fns ...probeFn) []string {
	installs := []string{probeAuthority + "/config", probeAuthority + "/note"}
	docs := []string{
		"kind: core.substrate.reamde.dev/authority\nmetadata: {id: " + probeAuthority + "}\ndata:\n  version: v1alpha1\n",
	}
	// The config kind exists because every bundle must name one: it is the
	// bundleconfig-trait record a user fills in, not because these probes
	// read it.
	body := []string{fmt.Sprintf(`kind: core.substrate.reamde.dev/kind
metadata: {id: %s/config}
data:
  authority: %s
  names: {singular: config, plural: configs}
  traits: [bundleconfig]
  properties:
    unused: {type: string, description: the bundle needs a config kind}
---
kind: core.substrate.reamde.dev/kind
metadata: {id: %s/note}
data:
  authority: %s
  names: {singular: note, plural: notes}
  properties:
    note: {type: string, description: what a probe would emit if it emitted}
`, probeAuthority, probeAuthority, probeAuthority, probeAuthority)}

	for _, fn := range fns {
		installs = append(installs, probeAuthority+"/"+fn.name)
		body = append(body, fn.document())
	}
	bundle := fmt.Sprintf(`kind: core.substrate.reamde.dev/bundle
metadata: {id: %s/probe}
data:
  authority: %s
  description: sandbox probes, installed the way a bundle installs
  configType: %s/config
  installs:
%s
`, probeAuthority, probeAuthority, probeAuthority, indent(installs, "    - "))
	return append(append(docs, bundle), body...)
}

// probeFn is one function body plus the capability envelope it declares.
type probeFn struct {
	name    string
	source  string
	network []string
}

func (f probeFn) document() string {
	net := ""
	if len(f.network) > 0 {
		net = "\n    network:\n" + indent(f.network, "      - ")
	}
	return fmt.Sprintf(`kind: core.substrate.reamde.dev/function
metadata: {id: %s/%s}
data:
  authority: %s
  description: a sandbox probe
  runtime: python
  timeoutMs: 20000
  capabilities:
    emit: [%s/note]%s
  source: |
%s
`, probeAuthority, f.name, probeAuthority, probeAuthority, net, blockScalar(f.source))
}

// blockScalar indents a python body into a YAML literal block.
func blockScalar(source string) string {
	lines := strings.Split(strings.Trim(source, "\n"), "\n")
	for i, line := range lines {
		if line == "" {
			continue // a blank line needs no indent, and trailing spaces are noise
		}
		lines[i] = "    " + line
	}
	return strings.Join(lines, "\n")
}

func indent(items []string, prefix string) string {
	var b strings.Builder
	for _, item := range items {
		b.WriteString(prefix + item + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// ref is how the HTTP surface addresses a function: the route is
// `/functions/{name}/call`, one path segment, so a call names the function by
// its bare name and the registry resolves it.
func ref(name string) string { return name }

// The headline: a function installed and invoked through the real substrate
// cannot read the server's own environment, where the credential key that
// unseals every stored provider token lives.
func TestFunctionCannotReadTheServersEnvironment(t *testing.T) {
	testenv.RequireSandbox(t)
	t.Setenv("SUBSTRATE_CREDENTIAL_KEY", "LEAK-CREDENTIAL-KEY-END-TO-END")
	env := testenv.Start(t)
	env.ApplyVocabularyYAML(probeBundle(probeFn{name: "peek", source: `
def main(input, host):
    try:
        with open("/proc/1/environ", "rb") as f:
            return {"output": {"read": f.read().decode("utf-8", "replace")}}
    except Exception as e:
        return {"output": {"denied": type(e).__name__}}
`})...)

	out := env.MustCallFunction(ref("peek"), map[string]any{})
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("unexpected output: %#v", out)
	}
	if got, leaked := m["read"]; leaked {
		s, _ := got.(string)
		if strings.Contains(s, "LEAK-CREDENTIAL-KEY") {
			t.Fatal("a function read the substrate's credential key")
		}
		t.Fatalf("a function read the substrate's environment: %q", s)
	}
	if m["denied"] == nil {
		t.Fatalf("expected a denial, got: %#v", out)
	}
}

// capabilities.network, enforced through the whole stack: the manifest says
// nothing about egress, so the body gets none, and the same body with a
// declaration gets its socket.
func TestNetworkCapabilityIsEnforcedEndToEnd(t *testing.T) {
	testenv.RequireSandbox(t)
	env := testenv.Start(t)
	const probe = `
import socket
def main(input, host):
    try:
        s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        s.close()
        return {"output": "opened"}
    except OSError:
        return {"output": "denied"}
`
	env.ApplyVocabularyYAML(probeBundle(
		probeFn{name: "quiet", source: probe},
		probeFn{name: "chatty", source: probe, network: []string{"api.example.com"}},
	)...)

	if got := env.MustCallFunction(ref("quiet"), map[string]any{}); got != "denied" {
		t.Fatalf("a function declaring no network opened a socket: %v", got)
	}
	if got := env.MustCallFunction(ref("chatty"), map[string]any{}); got != "opened" {
		t.Fatalf("a function declaring network was refused a socket: %v", got)
	}
}

// Two functions in ONE repository and ONE bundle: the ordinary case: still
// cannot see each other. A shared interpreter made `sys.modules['__main__']` a
// scratchpad between every body in the substrate.
func TestTwoFunctionsCannotSeeEachOther(t *testing.T) {
	env := testenv.Start(t)
	env.ApplyVocabularyYAML(probeBundle(
		probeFn{name: "stash", source: `
import sys
def main(input, host):
    sys.modules['__main__'].STOLEN = (input.get("args") or {}).get("secret")
    return {"output": "stashed"}
`},
		probeFn{name: "steal", source: `
import sys
def main(input, host):
    return {"output": getattr(sys.modules['__main__'], 'STOLEN', "nothing")}
`},
	)...)

	env.MustCallFunction(ref("stash"), map[string]any{"secret": "SUPER-SECRET"})
	if got := env.MustCallFunction(ref("steal"), map[string]any{}); got != "nothing" {
		t.Fatalf("one function read another's memory: %v", got)
	}
}

// The harness itself: a substrate that starts, registers and answers is worth
// asserting on its own, so a failure above is never first diagnosed as a
// broken fixture.
func TestEnvironmentStartsAndAuthenticates(t *testing.T) {
	env := testenv.Start(t)
	if status, body := env.Do("GET", "/healthz", nil); status != 200 {
		t.Fatalf("healthz: %d %s", status, body)
	}
	if status, _ := env.Do("GET", "/api/v1/core.substrate.reamde.dev/kinds", nil); status != 200 {
		t.Fatalf("authenticated kinds listing: %d", status)
	}
	// The token is real: the same request without it is refused.
	token := env.Token
	env.Token = ""
	if status, _ := env.Do("GET", "/api/v1/core.substrate.reamde.dev/kinds", nil); status != 401 {
		t.Fatalf("unauthenticated kinds listing: %d, want 401", status)
	}
	env.Token = token
}
