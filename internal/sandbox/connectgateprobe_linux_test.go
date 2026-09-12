//go:build linux

package sandbox

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

// The connect gate's own preconditions. The gate installs a filter in the
// CHILD and answers every notification in the PARENT, with pidfd_getfd(2) and
// process_vm_readv(2) against that child; Docker's and containerd's default
// seccomp profiles permit those two only for a container carrying
// CAP_SYS_PTRACE in its bounding set. Without them the gate refuses every
// connect, which is how a provider install whose PEP 723 block sends uv to
// fetch a dependency failed with "Permission denied (os error 13)" while the
// boot line reported every layer green.
//
// Both directions are reproducible from a macOS laptop, and the first is the
// stock-container failure this file exists for:
//
//	docker run --rm -v "$PWD":/src -w /src golang:1.26-alpine \
//	  go test ./internal/sandbox/...                      # gate MISSING
//	docker run --rm --cap-add SYS_PTRACE -v "$PWD":/src -w /src \
//	  golang:1.26-alpine go test ./internal/sandbox/...   # gate PRESENT

// The probe's three answers are exclusive, and its refusal has to name the
// call the boot line prints: a gate refused by nothing sends an operator
// looking for a kernel setting that is not the problem.
func TestConnectGateProbeAnswers(t *testing.T) {
	ok, refused, err := connectGateAvailable()
	// A run under `docker run` is the point of this case, and the two
	// directions are otherwise indistinguishable from a pass.
	t.Logf("connect gate available=%v refused=%q err=%v", ok, refused, err)

	switch {
	case ok:
		if refused != "" || err != nil {
			t.Fatalf("an available gate carries a refusal (%q) or an error (%v)", refused, err)
		}
	case refused != "":
		if err != nil {
			t.Fatalf("a kernel refusal (%q) must not also be a probe failure: %v", refused, err)
		}
		named := false
		for _, call := range []string{"pidfd_open", "pidfd_getfd", "process_vm_readv"} {
			named = named || strings.HasPrefix(refused, call+":")
		}
		if !named {
			t.Fatalf("the refusal names no gate syscall: %q", refused)
		}
	case err == nil:
		t.Fatal("the probe reported neither a working gate, a refusal, nor a failure")
	}

	if got := New(ModeBestEffort).Report().ConnectGate; got != ok {
		t.Fatalf("the report carries ConnectGate=%v while the probe answers %v", got, ok)
	}
}

// A profile that refuses the calls against this process refuses them against a
// child too, so the probe may not report a working gate there. The converse is
// not asserted: permitting self-access while denying a child is exactly the
// case the child probe exists for, and it is what this invariant leaves room
// for.
func TestConnectGateProbeIsNotWiderThanTheProfile(t *testing.T) {
	if selfSyscallsPermitted() {
		t.Skip("this host permits the gate syscalls against itself: nothing to bound here")
	}
	ok, _, _ := connectGateAvailable()
	if ok {
		t.Fatal("the probe reports a working gate on a host that refuses the syscalls against its own process")
	}
}

// selfSyscallsPermitted makes the two calls against this process, without
// going through the code under test.
func selfSyscallsPermitted() bool {
	pidfd, err := unix.PidfdOpen(unix.Getpid(), 0)
	if err != nil {
		return false
	}
	defer func() { _ = unix.Close(pidfd) }()
	dup, err := unix.PidfdGetfd(pidfd, pidfd, 0)
	if err != nil {
		return false
	}
	_ = unix.Close(dup)
	src := []byte("probe")
	dst := make([]byte, len(src))
	_, err = unix.ProcessVMReadv(unix.Getpid(), []unix.Iovec{{Base: &dst[0], Len: uint64(len(dst))}},
		[]unix.RemoteIovec{{Base: uintptr(unsafe.Pointer(&src[0])), Len: len(src)}}, 0)
	return err == nil
}

// Destination filtering is what a network grant is issued under (decision
// 0035), so a gate nobody can service REFUSES the body in best-effort too: the
// mode degrades layers, it does not hand a body the deployment's own network.
// Enforce refuses for the same reason, and a body that declares no network
// runs under both.
func TestANetworkBodyIsRefusedWhenTheGateCannotRun(t *testing.T) {
	for _, mode := range []Mode{ModeBestEffort, ModeEnforce} {
		c := &Confiner{
			mode: mode,
			report: Report{
				OS: "linux", LandlockABI: 4, Seccomp: true,
				ConnectGateErr: "pidfd_getfd: operation not permitted",
			},
		}
		err := c.Wrap(exec.Command("/bin/true"), Policy{Network: true, NotifyConnect: true})
		if err == nil {
			t.Fatalf("%s ran a network body with no destination filter", mode)
		}
		if !strings.Contains(err.Error(), "CAP_SYS_PTRACE") || !strings.Contains(err.Error(), "pidfd_getfd") {
			t.Fatalf("%s: the refusal names neither the remedy nor the refused syscall: %v", mode, err)
		}
		if err := c.Wrap(exec.Command("/bin/true"), Policy{}); err != nil {
			t.Fatalf("%s refused a body that declared no network: %v", mode, err)
		}
	}
}

// A probe that could not complete is not a capability the operator is missing,
// and it is not a license to run unfiltered either: the body is refused with
// the probe's own failure.
func TestANetworkBodyIsRefusedWhenTheProbeFailed(t *testing.T) {
	errProbe := errors.New("connect-gate probe: pidfd_open: too many open files")
	c := &Confiner{
		mode:   ModeBestEffort,
		report: Report{OS: "linux", LandlockABI: 4, Seccomp: true, Err: errProbe},
	}
	err := c.Wrap(exec.Command("/bin/true"), Policy{Network: true, NotifyConnect: true})
	if err == nil || !strings.Contains(err.Error(), errProbe.Error()) {
		t.Fatalf("a failed probe admitted a network body or hid its cause: %v", err)
	}
	if strings.Contains(err.Error(), "CAP_SYS_PTRACE") {
		t.Fatalf("a failed probe reads as a missing capability: %v", err)
	}
}

// A serviceable gate still rides argv into the stub, so the policy the child
// receives is the one the grant was issued under.
func TestAServiceableGateReachesTheStub(t *testing.T) {
	c := &Confiner{mode: ModeBestEffort, report: Report{OS: "linux", LandlockABI: 4, Seccomp: true, ConnectGate: true}}
	cmd := exec.Command("/bin/true")
	if err := c.Wrap(cmd, Policy{Network: true, NotifyConnect: true}); err != nil {
		t.Fatalf("wrap: %v", err)
	}
	// Wrap parked the parent end of the gate socket for Serve, which this test
	// never calls: both ends close here or the descriptors outlive the case.
	defer func() {
		if v, ok := c.pending.LoadAndDelete(cmd); ok {
			g := v.(*pendingGate)
			_ = g.parent.Close()
			_ = g.child.Close()
		}
	}()
	if got := decodeStubPolicy(t, cmd); !got.NotifyConnect {
		t.Fatal("a serviceable gate was dropped from the policy")
	}
	if len(cmd.ExtraFiles) != 1 {
		t.Fatalf("the stub was handed %d extra descriptors, want the one gate socket", len(cmd.ExtraFiles))
	}
}

// decodeStubPolicy reads back the policy wrap encoded into argv.
func decodeStubPolicy(t *testing.T, cmd *exec.Cmd) Policy {
	t.Helper()
	if len(cmd.Args) < 3 || cmd.Args[1] != stubArgv {
		t.Fatalf("the command was not wrapped: %v", cmd.Args)
	}
	raw, err := base64.RawURLEncoding.DecodeString(cmd.Args[2])
	if err != nil {
		t.Fatalf("decoding the policy: %v", err)
	}
	var p Policy
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("unmarshaling the policy: %v", err)
	}
	return p
}
