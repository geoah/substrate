//go:build linux

package sandbox

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// These tests exercise the REAL kernel facilities against real child
// processes: a confinement that is only unit-tested against its own BPF
// assembler proves nothing. Where the kernel does not offer a layer the test
// skips rather than fails: a CI runner or a developer laptop with Landlock
// left out of its lsm= list is an environment difference, not a regression.

func newTestConfiner(t *testing.T) *Confiner {
	t.Helper()
	c := New(ModeBestEffort)
	if !c.Report().FS() {
		t.Skipf("landlock unavailable: %s", c.Report())
	}
	if !c.Report().Seccomp {
		t.Skipf("seccomp unavailable: %s", c.Report())
	}
	return c
}

// runConfined runs /bin/sh -c under a policy and returns its combined output.
func runConfined(t *testing.T, c *Confiner, p Policy, script string) (string, error) {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}
	if err := c.Wrap(cmd, p); err != nil {
		t.Fatalf("wrap: %v", err)
	}
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	return buf.String(), err
}

// shellPolicy grants what /bin/sh needs and nothing else.
func shellPolicy(rw ...string) Policy {
	return Policy{
		ReadExec:  []string{"/usr", "/bin", "/sbin", "/lib", "/lib64", "/etc"},
		ReadWrite: rw,
		NoFile:    1024,
		FileSize:  64 << 20,
	}
}

// The hole this package exists for: the runner's env allowlist keeps the
// master secrets out of a child's own environment, and a child reads them out
// of the substrate's /proc entry instead. Landlock is what makes that stop.
func TestConfinedChildCannotReadParentEnviron(t *testing.T) {
	c := newTestConfiner(t)
	self := filepath.Join("/proc", strconv.Itoa(os.Getpid()), "environ")

	// Establish the hole first: unconfined, the same read succeeds. Without
	// this the test could pass on a box where the read fails for an unrelated
	// reason and prove nothing.
	if _, err := os.ReadFile(self); err != nil {
		t.Skipf("this process's own environ is already unreadable (%v): nothing to prove", err)
	}

	out, err := runConfined(t, c, shellPolicy(), "cat "+self)
	if err == nil {
		t.Fatalf("a confined child read the substrate's environment: %q", out)
	}
	if !strings.Contains(out, "denied") && !strings.Contains(out, "such file") {
		t.Fatalf("expected a permission denial, got: %q (%v)", out, err)
	}
}

func TestConfinedChildCannotWriteOutsideItsWorkDir(t *testing.T) {
	c := newTestConfiner(t)
	work := t.TempDir()

	out, err := runConfined(t, c, shellPolicy(work), "echo hello > "+filepath.Join(work, "ok"))
	if err != nil {
		t.Fatalf("a write INSIDE the work dir must succeed: %v (%s)", err, out)
	}
	if b, err := os.ReadFile(filepath.Join(work, "ok")); err != nil || strings.TrimSpace(string(b)) != "hello" {
		t.Fatalf("work-dir write did not land: %q %v", b, err)
	}

	outside := filepath.Join(t.TempDir(), "escape")
	if out, err := runConfined(t, c, shellPolicy(work), "echo pwned > "+outside); err == nil {
		t.Fatalf("a confined child wrote outside its work dir: %q", out)
	}
	if _, err := os.Stat(outside); err == nil {
		t.Fatal("the escaping write actually created the file")
	}
}

// Two installations, two policies: neither can reach the other's work dir.
// This is the property that makes one process per installation worth having.
func TestConfinedChildrenCannotReachEachOther(t *testing.T) {
	c := newTestConfiner(t)
	alice, bob := t.TempDir(), t.TempDir()
	secret := filepath.Join(alice, "token")
	if err := os.WriteFile(secret, []byte("SUPER-SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := runConfined(t, c, shellPolicy(bob), "cat "+secret)
	if err == nil || strings.Contains(out, "SUPER-SECRET") {
		t.Fatalf("bob read alice's work dir: %q (%v)", out, err)
	}
}

// capabilities.network stops being decorative: no declaration, no sockets.
func TestNetworkCapabilityGatesSockets(t *testing.T) {
	c := newTestConfiner(t)
	probe, err := buildSocketProbe(t)
	if err != nil {
		t.Skipf("cannot build the socket probe: %v", err)
	}
	p := shellPolicy()
	p.ReadExec = append(p.ReadExec, filepath.Dir(probe))

	out, err := runConfined(t, c, p, probe)
	if err != nil {
		t.Fatalf("probe failed to run: %v (%s)", err, out)
	}
	if !strings.Contains(out, "inet=denied") {
		t.Fatalf("a body with no network capability opened an AF_INET socket: %q", out)
	}

	p.Network = true
	out, err = runConfined(t, c, p, probe)
	if err != nil {
		t.Fatalf("probe failed to run with network granted: %v (%s)", err, out)
	}
	if !strings.Contains(out, "inet=ok") {
		t.Fatalf("a body WITH the network capability was still refused a socket: %q", out)
	}
}

// buildSocketProbe compiles a tiny helper that reports whether it can create
// an AF_INET socket. A compiled probe rather than a shell one-liner because
// neither sh nor a guaranteed-present utility can open a raw socket portably.
func buildSocketProbe(t *testing.T) (string, error) {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	const probeSrc = `package main

import (
	"fmt"
	"syscall"
)

func main() {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if err != nil {
		fmt.Printf("inet=denied (%v)\n", err)
		return
	}
	syscall.Close(fd)
	fmt.Println("inet=ok")
}
`
	if err := os.WriteFile(src, []byte(probeSrc), 0o644); err != nil {
		return "", err
	}
	bin := filepath.Join(dir, "probe")
	cmd := exec.Command("go", "build", "-o", bin, src)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOFLAGS=")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("%w: %s", err, out)
	}
	return bin, nil
}

// The filter is assembled by hand, so its shape is worth asserting directly:
// a bad forward jump is the failure mode that would silently permit a denied
// syscall rather than crash.
func TestFilterAssembles(t *testing.T) {
	for _, network := range []bool{false, true} {
		prog, err := buildFilter(Policy{Network: network})
		if err != nil {
			t.Fatalf("network=%v: %v", network, err)
		}
		if len(prog) < 10 {
			t.Fatalf("network=%v: suspiciously short program (%d)", network, len(prog))
		}
		// Every jump must land inside the program, and forward.
		for i, insn := range prog {
			if insn.Code&0x07 != 0x05 { // not BPF_JMP
				continue
			}
			for _, off := range []uint8{insn.JT, insn.JF} {
				if target := i + 1 + int(off); target >= len(prog) {
					t.Fatalf("network=%v: instruction %d jumps to %d, past the end (%d)",
						network, i, target, len(prog))
				}
			}
		}
		// The last three are the terminals, in the order buildFilter documents.
		// The socket block carries its own refusal rather than jumping to a
		// fourth, so an unlisted domain cannot fall through to ALLOW.
		terminals := prog[len(prog)-3:]
		for i, want := range []uint32{
			seccompRetAllow, retErrno(unix.EPERM), retErrno(unix.ENOSYS),
		} {
			if terminals[i].K != want {
				t.Fatalf("terminal %d = %#x, want %#x", i, terminals[i].K, want)
			}
		}
		// The socket domain check must be an ALLOWLIST: exactly one refusal
		// instruction, and no compare against a domain that is not permitted.
		// A deny-list here would leave AF_PACKET reachable.
		refusals := 0
		for _, insn := range prog {
			if insn.Code == bpfRetK && insn.K == retErrno(unix.EAFNOSUPPORT) {
				refusals++
			}
		}
		if refusals != 1 {
			t.Fatalf("network=%v: %d socket refusals, want exactly 1", network, refusals)
		}
	}
}
