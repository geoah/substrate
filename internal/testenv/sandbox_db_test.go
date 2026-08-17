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
	installs := []string{probeAuthority + "/note"}
	docs := []string{
		"kind: core.substrate.reamde.dev/authority\nmetadata: {id: " + probeAuthority + "}\ndata:\n  version: 1\n",
	}
	// No inputs: these probes read no configuration, so the bundle declares
	// none and nothing anywhere implies any.
	body := []string{fmt.Sprintf(`kind: core.substrate.reamde.dev/kind
metadata: {id: %s/note}
data:
  authority: %s
  names: {singular: note, plural: notes}
  properties:
    note: {type: string, description: what a probe would emit if it emitted}
`, probeAuthority, probeAuthority)}

	for _, fn := range fns {
		installs = append(installs, probeAuthority+"/"+fn.name)
		body = append(body, fn.document())
	}
	bundle := fmt.Sprintf(`kind: core.substrate.reamde.dev/bundle
metadata: {id: %s/probe}
data:
  authority: %s
  description: sandbox probes, installed the way a bundle installs
  installs:
%s
`, probeAuthority, probeAuthority, indent(installs, "    - "))
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
  permissions:
    writes: [%s/note]%s
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
//
// The assertion is that the read is REFUSED, not that some planted canary is
// absent from what came back. A canary could not work: procfs reports the
// environment a process was EXEC'd with, so a t.Setenv in this test would never
// appear there however broken the sandbox was. What the sandbox owes us is that
// the file cannot be opened at all.
func TestFunctionCannotReadTheServersEnvironment(t *testing.T) {
	testenv.RequireSandbox(t)
	env := testenv.Start(t)
	// os.getppid(), NOT pid 1. In this harness the substrate is the TEST
	// BINARY running an in-process server, so pid 1 is the container's init:
	// a process this test has no relationship with, whose environ is
	// unreadable for reasons that have nothing to do with the sandbox. Reading
	// it would make the assertion below pass whether or not the confinement
	// exists. The body's own parent IS the substrate, so that is the target.
	env.ApplyVocabularyYAML(probeBundle(probeFn{name: "peek", source: `
import os
def main(input, host):
    target = "/proc/%d/environ" % os.getppid()
    try:
        with open(target, "rb") as f:
            return {"output": {"read": f.read().decode("utf-8", "replace"), "target": target}}
    except Exception as e:
        return {"output": {"denied": type(e).__name__, "target": target}}
`})...)

	out := env.MustCallFunction(ref("peek"), map[string]any{})
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("unexpected output: %#v", out)
	}
	if got, leaked := m["read"]; leaked {
		// Report the SHAPE of the leak, never its contents. This assertion
		// fires precisely when a body could read the server's environment, so
		// printing what it found would copy every secret on that environment
		// into a CI log: the credential key, the database URL, and whatever
		// else the operator's shell happened to export.
		s, _ := got.(string)
		t.Fatalf("a function read the substrate's environment from %v (%d bytes)", m["target"], len(s))
	}
	if m["denied"] == nil {
		t.Fatalf("expected a denial, got: %#v", out)
	}
}

// permissions.network, enforced through the whole stack: the manifest says
// nothing about egress, so the body gets none, and the same body with a
// declaration gets its socket.
func TestNetworkCapabilityIsEnforcedEndToEnd(t *testing.T) {
	// Only the syscall layer: denying a socket needs no filesystem rules.
	testenv.RequireSeccomp(t)
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
    secret = (input.get("args") or {}).get("secret")
    sys.modules['__main__'].STOLEN = secret
    return {"output": secret}
`},
		probeFn{name: "steal", source: `
import sys
def main(input, host):
    return {"output": getattr(sys.modules['__main__'], 'STOLEN', "nothing")}
`},
	)...)

	// Assert the stash actually HELD something first. If the input never
	// reached the body, STOLEN would be None, steal would find nothing, and the
	// test would pass while proving no isolation at all.
	if got := env.MustCallFunction(ref("stash"), map[string]any{"secret": "SUPER-SECRET"}); got != "SUPER-SECRET" {
		t.Fatalf("the stash body never received the secret, so this test would prove nothing: %v", got)
	}
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
	if status, _ := env.Do("GET", "/api/v1/core.substrate.reamde.dev/kind", nil); status != 200 {
		t.Fatalf("authenticated kinds listing: %d", status)
	}
	// The token is real: the same request without it is refused.
	token := env.Token
	env.Token = ""
	if status, _ := env.Do("GET", "/api/v1/core.substrate.reamde.dev/kind", nil); status != 401 {
		t.Fatalf("unauthenticated kinds listing: %d, want 401", status)
	}
	env.Token = token
}
