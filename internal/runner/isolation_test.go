package runner

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/geoah/substrate/internal/sandboxtest"
)

// nowPlus is the sweep clock: sweep takes the time so a test can retire an
// idle process without waiting out the real TTL.
func nowPlus(d time.Duration) time.Time { return time.Now().Add(d) }

// What a hostile body can and cannot do, asserted against real bodies in real
// processes. Every case here failed before one-process-per-installation and
// internal/sandbox landed; several of them are the exploit, written out.

// requireSandbox skips when the kernel does not offer the confinement. A CI
// runner or a laptop with Landlock left out of its lsm= list is an environment
// difference, not a regression, but the LEAK tests below do not use this,
// because process separation needs no kernel help and must hold everywhere.
func requireSandbox(t *testing.T, r *Runner) {
	t.Helper()
	if rep := r.sandbox.Report(); !rep.FS() || !rep.Seccomp {
		sandboxtest.Unavailablef(t, "kernel does not offer the sandbox: %s", rep)
	}
	sandboxtest.Ran()
}

// requireSeccomp is requireSandbox for an assertion that needs only the SYSCALL
// layer. A guard that skips more than it has to is a test that quietly stops
// running.
func requireSeccomp(t *testing.T, r *Runner) {
	t.Helper()
	if rep := r.sandbox.Report(); !rep.Seccomp {
		sandboxtest.Unavailablef(t, "kernel has no syscall filter: %s", rep)
	}
	sandboxtest.Ran()
}

// The eight guarded cases below are the promise this file holds. Under
// SUBSTRATE_TEST_REQUIRE_SANDBOX a run that proved fewer of them fails, so a
// kernel or image change that turns every guard into a skip cannot leave a
// green build behind.
func TestMain(m *testing.M) {
	os.Exit(sandboxtest.Run(m, 8))
}

// The exploit that motivated retiring the shared interpreter: a function in
// ONE repository reading a secret handed to a function in ANOTHER. Both bodies
// were exec'd into the same CPython, so `sys.modules['__main__']` was a shared
// scratchpad, and the connector bundles receive live provider access tokens
// on exactly the config this test plants.
func TestFunctionCannotReadAnotherFunctionsConfig(t *testing.T) {
	r := New()
	ctx := context.Background()
	stash := Spec{
		Repository: "alice", Function: "a.g.test", Runtime: "python", TimeoutMs: 5000,
		Source: `
import sys
def main(input, host):
    sys.modules['__main__'].STOLEN = input["config"]["apiKey"]
    return {"output": "stashed"}
`,
	}
	steal := Spec{
		Repository: "bob", Function: "b.g.test", Runtime: "python", TimeoutMs: 5000,
		Source: `
import sys
def main(input, host):
    return {"output": {"stolen": getattr(sys.modules['__main__'], 'STOLEN', None)}}
`,
	}
	in := testInput()
	in.Config = map[string]any{"apiKey": "SUPER-SECRET"}
	if _, err := r.Invoke(ctx, stash, in, nil); err != nil {
		t.Fatalf("stash: %v", err)
	}
	res, err := r.Invoke(ctx, steal, testInput(), nil)
	if err != nil {
		t.Fatalf("steal: %v", err)
	}
	out, ok := res.Output.(map[string]any)
	if !ok {
		t.Fatalf("unexpected output: %#v", res.Output)
	}
	if out["stolen"] != nil {
		t.Fatalf("a body in repository bob read repository alice's config secret: %v", out["stolen"])
	}
}

// The protocol's own descriptors were reachable from a body in the shared
// host: PROTO_OUT is the parent's stdin pipe, so a body could write frames the
// parent would attribute to whatever delivery was in flight. Separate
// processes make the descriptors private, and the worst a body can now do with
// them is desync its OWN process.
func TestBodyCannotForgeFramesForAnotherFunction(t *testing.T) {
	r := New()
	ctx := context.Background()
	victim := Spec{
		Repository: "t1", Function: "victim.g.test", Runtime: "python", TimeoutMs: 5000,
		Source: "def main(input, host):\n    return {\"output\": \"honest\"}\n",
	}
	if _, err := r.Invoke(ctx, victim, testInput(), nil); err != nil {
		t.Fatalf("victim: %v", err)
	}
	r.mu.Lock()
	before := r.pys[victim.Key()]
	r.mu.Unlock()

	attacker := Spec{
		Repository: "t1", Function: "attacker.g.test", Runtime: "python", TimeoutMs: 5000,
		Source: `
import sys
def main(input, host):
    try:
        sys.modules['__main__'].PROTO_OUT.write('{"kind":"response","reqId":1,"ok":true,"output":"forged"}\n')
        sys.modules['__main__'].PROTO_OUT.flush()
    except Exception as e:
        return {"output": "refused: %s" % type(e).__name__}
    return {"output": "wrote"}
`,
	}
	// Whatever the attacker manages, it can only have hurt itself: the frame
	// went down ITS pipe. The victim's process is untouched and still answers.
	_, _ = r.Invoke(ctx, attacker, testInput(), nil)

	res, err := r.Invoke(ctx, victim, testInput(), nil)
	if err != nil || res.Output != "honest" {
		t.Fatalf("the victim's process was disturbed by another body: %+v %v", res, err)
	}
	r.mu.Lock()
	after := r.pys[victim.Key()]
	r.mu.Unlock()
	if before != after {
		t.Fatal("the victim's process was restarted: another body reached it")
	}
}

// The hole the env allowlist did not close. childEnv keeps the credential key
// and the database URL out of a body's own environment; without a filesystem
// boundary the body reads them out of the substrate's /proc entry instead.
func TestBodyCannotReadTheSubstratesEnvironment(t *testing.T) {
	r := New()
	requireSandbox(t, r)
	t.Setenv("SUBSTRATE_CREDENTIAL_KEY", "LEAK-CREDENTIAL-KEY")

	spec := Spec{
		Repository: "t1", Function: "peek.g.test", Runtime: "python", TimeoutMs: 5000,
		Source: `
import os
def main(input, host):
    out = {}
    for target in input["args"]["paths"]:
        try:
            with open(target, "rb") as f:
                out[target] = f.read().decode("utf-8", "replace")
        except Exception as e:
            out[target] = "denied: %s" % type(e).__name__
    return {"output": out}
`,
	}
	in := testInput()
	in.Args = map[string]any{"paths": []any{
		"/proc/1/environ",
		filepath.Join("/proc", strconv.Itoa(os.Getpid()), "environ"),
	}}
	res, err := r.Invoke(context.Background(), spec, in, nil)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	out, ok := res.Output.(map[string]any)
	if !ok {
		t.Fatalf("unexpected output: %#v", res.Output)
	}
	for path, got := range out {
		s, _ := got.(string)
		if !strings.HasPrefix(s, "denied:") {
			t.Fatalf("a body read %s: %q", path, s)
		}
	}
}

// permissions.network, enforced. A manifest that declares no egress gets no
// sockets; one that declares egress keeps working exactly as it did.
func TestNetworkCapabilityGatesABody(t *testing.T) {
	r := New()
	requireSeccomp(t, r)
	const probe = `
import socket
def main(input, host):
    try:
        s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        s.close()
        return {"output": "opened"}
    except OSError as e:
        return {"output": "denied"}
`
	denied := Spec{
		Repository: "t1", Function: "nonet.g.test", Runtime: "python",
		Source: probe, TimeoutMs: 5000,
	}
	res, err := r.Invoke(context.Background(), denied, testInput(), nil)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if res.Output != "denied" {
		t.Fatalf("a body declaring no network opened a socket: %v", res.Output)
	}

	granted := Spec{
		Repository: "t1", Function: "net.g.test", Runtime: "python",
		Source: probe, TimeoutMs: 5000, Network: []string{"api.example.com"},
	}
	res, err = r.Invoke(context.Background(), granted, testInput(), nil)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if res.Output != "opened" {
		t.Fatalf("a body declaring network was refused a socket: %v", res.Output)
	}
}

// One process per installation separates the memory; the sandbox separates the
// disk. A body owns its own work dir and can reach no other.
func TestBodyCannotReachAnotherInstallationsWorkDir(t *testing.T) {
	r := New()
	requireSandbox(t, r)
	ctx := context.Background()

	// alice writes a secret into her own scratch, which is legitimate.
	alice := Spec{
		Repository: "alice", Function: "keeper.g.test", Runtime: "python", TimeoutMs: 5000,
		Source: `
import os
def main(input, host):
    path = os.path.join(os.environ["TMPDIR"], "token")
    with open(path, "w") as f:
        f.write("SUPER-SECRET")
    return {"output": path}
`,
	}
	res, err := r.Invoke(ctx, alice, testInput(), nil)
	if err != nil {
		t.Fatalf("alice: %v", err)
	}
	secret, _ := res.Output.(string)
	if secret == "" {
		t.Fatalf("alice could not write her own scratch: %#v", res.Output)
	}

	bob := Spec{
		Repository: "bob", Function: "thief.g.test", Runtime: "python", TimeoutMs: 5000,
		Source: `
def main(input, host):
    try:
        with open(input["args"]["path"]) as f:
            return {"output": f.read()}
    except Exception as e:
        return {"output": "denied: %s" % type(e).__name__}
`,
	}
	in := testInput()
	in.Args = map[string]any{"path": secret}
	res, err = r.Invoke(ctx, bob, in, nil)
	if err != nil {
		t.Fatalf("bob: %v", err)
	}
	if got, _ := res.Output.(string); !strings.HasPrefix(got, "denied:") {
		t.Fatalf("bob read alice's scratch: %q", got)
	}
}

// The shared caches are the vector one-process-per-installation does NOT
// close: plant an executable under the hash a neighbor's Go body will start,
// and the substrate execs it. They are read-and-execute to every body.
func TestBodyCannotWriteTheSharedCaches(t *testing.T) {
	r := New()
	requireSandbox(t, r)
	binDir, err := r.binDir()
	if err != nil {
		t.Fatalf("bin dir: %v", err)
	}
	pyDir, err := r.pyDir()
	if err != nil {
		t.Fatalf("py dir: %v", err)
	}

	spec := Spec{
		Repository: "t1", Function: "planter.g.test", Runtime: "python", TimeoutMs: 5000,
		Source: `
import os
def main(input, host):
    out = {}
    for target in input["args"]["dirs"]:
        path = os.path.join(target, "planted")
        try:
            with open(path, "w") as f:
                f.write("#!/bin/sh\nexec /bin/sh\n")
            os.chmod(path, 0o755)
            out[target] = "planted"
        except Exception as e:
            out[target] = "denied: %s" % type(e).__name__
    return {"output": out}
`,
	}
	in := testInput()
	in.Args = map[string]any{"dirs": []any{binDir, pyDir}}
	res, err := r.Invoke(context.Background(), spec, in, nil)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	out, ok := res.Output.(map[string]any)
	if !ok {
		t.Fatalf("unexpected output: %#v", res.Output)
	}
	for dir, got := range out {
		s, _ := got.(string)
		if !strings.HasPrefix(s, "denied:") {
			_ = os.Remove(filepath.Join(dir, "planted"))
			t.Fatalf("a body wrote an executable into the shared cache %s: %q", dir, s)
		}
	}
}

// The reaper: an idle process is closed and the next delivery starts a fresh
// one. Without it, one process per installation would mean one interpreter per
// installed function held forever.
func TestIdleProcessesAreReaped(t *testing.T) {
	r := New()
	spec := Spec{
		Repository: "t1", Function: "idle.g.test", Runtime: "python", TimeoutMs: 5000,
		Source: "def main(input, host):\n    return {\"output\": \"ok\"}\n",
	}
	ctx := context.Background()
	if _, err := r.Invoke(ctx, spec, testInput(), nil); err != nil {
		t.Fatalf("invoke: %v", err)
	}
	r.mu.Lock()
	first := r.pys[spec.Key()]
	r.mu.Unlock()
	if first == nil {
		t.Fatal("no process was started")
	}

	// Sweep as though the TTL had passed, rather than waiting out ten real
	// minutes: the clock is a parameter for exactly this reason.
	r.sweep(nowPlus(idleTTL + 1))
	r.mu.Lock()
	_, live := r.pys[spec.Key()]
	r.mu.Unlock()
	if live {
		t.Fatal("an idle process survived the sweep")
	}

	// And the body still works: the retirement is a restart, not a break.
	if res, err := r.Invoke(ctx, spec, testInput(), nil); err != nil || res.Output != "ok" {
		t.Fatalf("post-reap invoke: %+v %v", res, err)
	}
	r.mu.Lock()
	second := r.pys[spec.Key()]
	r.mu.Unlock()
	if second == first {
		t.Fatal("the reaped process came back")
	}
}

// Withdrawing `permissions.network` has to take effect on the NEXT delivery,
// not on the next unrelated source edit. The sandbox policy is applied once,
// when the process starts, so a manifest change that left the content hash
// alone would keep serving deliveries from the process that was started while
// egress was granted, with its sockets still open.
func TestWithdrawingNetworkRetiresTheProcess(t *testing.T) {
	r := New()
	requireSeccomp(t, r)
	ctx := context.Background()
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
	granted := Spec{
		Repository: "t1", Function: "revoke.g.test", Runtime: "python",
		Source: probe, TimeoutMs: 5000, Network: []string{"api.example.com"},
	}
	if got, err := r.Invoke(ctx, granted, testInput(), nil); err != nil || got.Output != "opened" {
		t.Fatalf("with the capability granted: %+v %v", got, err)
	}

	// The SAME source, the same function, the same repository: only the
	// declaration is gone.
	withdrawn := granted
	withdrawn.Network = nil
	if granted.Key() == withdrawn.Key() {
		t.Fatal("withdrawing the network capability did not re-key the installation")
	}
	got, err := r.Invoke(ctx, withdrawn, testInput(), nil)
	if err != nil {
		t.Fatalf("after withdrawal: %v", err)
	}
	if got.Output != "denied" {
		t.Fatalf("a body kept its sockets after the capability was withdrawn: %v", got.Output)
	}
}

// The certificate grant is narrow (the cert directories, never their parents),
// so it has two things to prove at once: a body that declares egress can still
// complete a real TLS handshake, and the private-key directories sitting beside
// those certificates are still refused. Granting `/etc/ssl` would satisfy the
// first and quietly fail the second.
func TestTLSWorksWhileKeyMaterialStaysDenied(t *testing.T) {
	r := New()
	requireSandbox(t, r)
	const keyProbe = `
import os
def main(input, host):
    out = {}
    for p in input["args"]["paths"]:
        try:
            if os.path.isdir(p):
                os.listdir(p)
            else:
                open(p, "rb").read()
            out[p] = "READABLE"
        except Exception:
            out[p] = "denied"
    try:
        import urllib.request
        out["https"] = urllib.request.urlopen("https://example.com", timeout=15).status
    except Exception as e:
        out["https"] = "failed: %s" % type(e).__name__
    return {"output": out}
`
	keyPaths := []any{"/etc/ssl/private", "/etc/pki/tls/private", "/etc/shadow"}
	spec := Spec{
		Repository: "t1", Function: "tls.g.test", Runtime: "python", TimeoutMs: 30000,
		Source: keyProbe, Network: []string{"example.com"},
	}
	in := testInput()
	in.Args = map[string]any{"paths": keyPaths}
	res, err := r.Invoke(context.Background(), spec, in, nil)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	out, ok := res.Output.(map[string]any)
	if !ok {
		t.Fatalf("unexpected output: %#v", res.Output)
	}

	// The key material half needs no network and is always asserted.
	for _, p := range keyPaths {
		if got, _ := out[p.(string)].(string); got == "READABLE" {
			t.Errorf("a body read key material at %s", p)
		}
	}
	// The TLS half only means something on a machine that has the internet, so
	// a transport failure skips rather than reddening an offline box. A
	// handshake that fails WITH connectivity would be a real regression in the
	// certificate grant, which is why this is not simply ignored.
	if got := out["https"]; got != float64(200) {
		if !hasInternet(t) {
			t.Skipf("no internet: cannot tell a certificate-grant regression from an offline box (%v)", got)
		}
		t.Errorf("a body declaring egress could not complete a TLS handshake: %v", got)
	}
}

// hasInternet reports whether this machine can reach the host the TLS probe
// uses, so a transport failure can be told apart from a policy failure.
func hasInternet(t *testing.T) bool {
	t.Helper()
	conn, err := net.DialTimeout("tcp", "example.com:443", 5*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// A body must not be able to mint a device node inside its own work dir. The
// directory is writable and readable by design, so a character device created
// there would be a device the same ruleset then lets it OPEN: a hand-made
// /dev/mem defeats the whole filesystem layer. The container's default
// capability set carries CAP_MKNOD and the substrate runs as root in the image,
// so nothing else is standing in the way.
func TestBodyCannotCreateDeviceNodes(t *testing.T) {
	r := New()
	requireSandbox(t, r)
	// Only meaningful where mknod would otherwise WORK. An unprivileged uid is
	// refused a device node by the ordinary permission check, so on a developer
	// box this would pass with the grant wide open and prove nothing. The image
	// runs as root with CAP_MKNOD in the container's default set, which is
	// exactly the case the mask exists for.
	requireMknod(t)
	spec := Spec{
		Repository: "t1", Function: "mknod.g.test", Runtime: "python", TimeoutMs: 5000,
		Source: `
import os
def main(input, host):
    out = {}
    scratch = os.environ["TMPDIR"]
    # A regular file in the same directory, to prove the dir really is writable
    # and the refusal below is about the DEVICE, not about the path.
    try:
        with open(os.path.join(scratch, "ordinary"), "w") as f:
            f.write("x")
        out["regularFile"] = "written"
    except Exception as e:
        out["regularFile"] = "denied: %s" % type(e).__name__
    for name, mode, dev in [("chardev", 0o020600, os.makedev(1, 1)),
                            ("blockdev", 0o060600, os.makedev(7, 0))]:
        try:
            os.mknod(os.path.join(scratch, name), mode, dev)
            out[name] = "CREATED"
        except Exception as e:
            out[name] = "denied: %s" % type(e).__name__
    return {"output": out}
`,
	}
	res, err := r.Invoke(context.Background(), spec, testInput(), nil)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	out, ok := res.Output.(map[string]any)
	if !ok {
		t.Fatalf("unexpected output: %#v", res.Output)
	}
	if got, _ := out["regularFile"].(string); got != "written" {
		t.Fatalf("the work dir is not writable, so this test proves nothing: %v", got)
	}
	for _, node := range []string{"chardev", "blockdev"} {
		if got, _ := out[node].(string); !strings.HasPrefix(got, "denied") {
			t.Errorf("a body created a %s in its own work dir: %v", node, out[node])
		}
	}
}

// requireMknod skips unless THIS process could create a device node, which is
// the precondition for the confined body's refusal to mean anything.
func requireMknod(t *testing.T) {
	t.Helper()
	probe := filepath.Join(t.TempDir(), "probe")
	if err := syscall.Mknod(probe, syscall.S_IFCHR|0o600, int(unix.Mkdev(1, 3))); err != nil {
		t.Skipf("this uid cannot create a device node unconfined (%v), so a refusal would prove nothing", err)
	}
	_ = os.Remove(probe)
}

// The network gate is an ALLOWLIST of socket domains, not a refusal of the two
// internet ones. Denying AF_INET and AF_INET6 while letting the rest through
// would leave AF_PACKET, and the container's default capability set carries
// CAP_NET_RAW, so a body could send and receive raw Ethernet frames and the
// capability would bound nothing at all.
func TestRawAndKernelSocketFamiliesAreRefused(t *testing.T) {
	r := New()
	requireSeccomp(t, r)
	const probe = `
import socket
def main(input, host):
    out = {}
    for name, fam, typ in [("packet", 17, socket.SOCK_RAW),
                           ("netlink", 16, socket.SOCK_RAW),
                           ("inet_raw", socket.AF_INET, socket.SOCK_RAW),
                           ("unix", socket.AF_UNIX, socket.SOCK_STREAM)]:
        try:
            s = socket.socket(fam, typ)
            s.close()
            out[name] = "opened"
        except OSError as e:
            out[name] = "denied"
    return {"output": out}
`
	// A body with NO declared egress: everything but AF_UNIX must be refused.
	quiet := Spec{
		Repository: "t1", Function: "rawnonet.g.test", Runtime: "python",
		Source: probe, TimeoutMs: 5000,
	}
	res, err := r.Invoke(context.Background(), quiet, testInput(), nil)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	out, ok := res.Output.(map[string]any)
	if !ok {
		t.Fatalf("unexpected output: %#v", res.Output)
	}
	for _, fam := range []string{"packet", "netlink", "inet_raw"} {
		if out[fam] != "denied" {
			t.Errorf("a body declaring no network opened an %s socket: %v", fam, out[fam])
		}
	}
	if out["unix"] != "opened" {
		t.Errorf("AF_UNIX must stay available: %v", out["unix"])
	}

	// A body WITH declared egress still gets no raw link-layer access: the
	// declaration buys ordinary sockets, not the wire.
	loud := quiet
	loud.Function, loud.Network = "rawnet.g.test", []string{"api.example.com"}
	res, err = r.Invoke(context.Background(), loud, testInput(), nil)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	out, _ = res.Output.(map[string]any)
	if out["packet"] != "denied" {
		t.Errorf("a body declaring egress opened a raw packet socket: %v", out["packet"])
	}
}
