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
	"syscall"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/geoah/substrate/internal/sandboxtest"
)

// These tests exercise the REAL kernel facilities against real child
// processes: a confinement that is only unit-tested against its own BPF
// assembler proves nothing. Where the kernel does not offer a layer the test
// skips rather than fails: a CI runner or a developer laptop with Landlock
// left out of its lsm= list is an environment difference, not a regression.
// Under SUBSTRATE_TEST_REQUIRE_SANDBOX that skip is a failure instead, so a
// kernel or image that lost the confinement cannot pass CI quietly.

func newTestConfiner(t *testing.T) *Confiner {
	t.Helper()
	c := New(ModeBestEffort)
	if !c.Report().FS() {
		sandboxtest.Unavailablef(t, "landlock unavailable: %s", c.Report())
	}
	if !c.Report().Seccomp {
		sandboxtest.Unavailablef(t, "seccomp unavailable: %s", c.Report())
	}
	sandboxtest.Case(t)
	return c
}

// Five cases below run a real confined child, and under
// SUBSTRATE_TEST_REQUIRE_SANDBOX exactly five must reach the guard.
// TestFilterAssembles needs no kernel and is not one of them. There is no
// TestMain off Linux: sandbox_other.go confines nothing by construction, so
// there is no case there to count.
func TestMain(m *testing.M) {
	os.Exit(sandboxtest.Run(m, 5))
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

// runConfinedCmd runs cmd under a policy and returns its combined output.
// With no shell in between, the program's parent is this test binary.
func runConfinedCmd(t *testing.T, c *Confiner, p Policy, cmd *exec.Cmd) (string, error) {
	t.Helper()
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

// permissions.network stops being decorative: no declaration, no sockets.
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

	// The granted half needs the connect gate's supervisor, because Wrap
	// refuses a network policy where the two syscalls it answers notifications
	// with are denied. That is a container profile, not a regression — and
	// under the require gate, a failure.
	if !c.Report().ConnectGate {
		sandboxtest.Unavailablef(t, "the connect gate cannot be serviced here: %s", c.Report())
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

// A confined body must not signal the substrate that started it, its process
// group, or every process at once: SIGTERM is the substrate's graceful stop,
// an unhandled SIGHUP ends it, and kill(-1) reaches every process of this uid,
// every other body included. The probe is started straight under the stub, so
// its parent is this test binary, standing in for the substrate. It runs
// twice: in a process group of its own, as the runner starts a body, and in
// this binary's group, as the runner starts a provision step.
func TestConfinedChildCannotSignalTheSubstrate(t *testing.T) {
	c := newTestConfiner(t)
	probe, err := buildProbe(t, signalProbeSrc)
	if err != nil {
		t.Skipf("cannot build the signal probe: %v", err)
	}
	// An unconfined process of this uid, standing in for another body.
	sibling := exec.Command("/bin/sleep", "30")
	if err := sibling.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = sibling.Process.Kill()
		_ = sibling.Wait()
	})
	siblingPid := strconv.Itoa(sibling.Process.Pid)
	// One of this binary's threads other than its leader: kill(2) on it
	// signals the whole process, and its id is a value the syscall filter
	// cannot tell from a body's child's.
	thread := otherThread(t)
	// Another body's pid, and a thread id of the substrate, are values the
	// syscall filter cannot tell from a child's. Landlock's signal scope
	// (ABI 6) refuses them; below it they are the residual docs/functions.md
	// names, and the run holds the docs to it.
	residual := []string{"kill(sibling)", "kill(parent thread)", "tkill(parent thread)"}
	p := shellPolicy()
	p.ReadExec = append(p.ReadExec, filepath.Dir(probe))

	for _, tc := range []struct {
		name     string
		ownGroup bool
		refused  []string
		kept     []string
	}{{
		name:     "own group",
		ownGroup: true,
		refused: []string{
			"kill(parent)", "kill(-group)", "kill(-1)", "tkill(parent)", "tgkill(parent)",
			"rt_sigqueueinfo(parent)", "rt_tgsigqueueinfo(parent)", "setpgid(group)", "fcntl(F_SETSIG)",
		},
		// What a body legitimately signals: its own process group, itself,
		// its own threads (musl's raise(3) is tkill on its own tid) and its
		// children.
		kept: []string{"kill(0)", "kill(self)", "tkill(self)", "tgkill(self)", "kill(child)", "fcntl(F_SETOWN)"},
	}, {
		// Here kill(0) is the substrate's group, so it goes too.
		name: "the substrate's group",
		refused: []string{
			"kill(parent)", "kill(-group)", "kill(0)", "kill(-1)", "tkill(parent)", "tgkill(parent)",
			"rt_sigqueueinfo(parent)", "rt_tgsigqueueinfo(parent)", "setpgid(group)", "fcntl(F_SETSIG)",
		},
		kept: []string{"kill(self)", "tkill(self)", "tgkill(self)", "kill(child)", "fcntl(F_SETOWN)"},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			start := func() *exec.Cmd {
				cmd := exec.Command(probe, siblingPid, thread)
				cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: tc.ownGroup}
				return cmd
			}
			// Establish the hole first: unconfined, every call the confined
			// run must lose succeeds, so a refusal below is the sandbox's and
			// nothing else's.
			free := start()
			free.Env = []string{"PATH=/usr/bin:/bin"}
			unconfined, err := free.CombinedOutput()
			if err != nil {
				t.Fatalf("the probe failed unconfined: %v (%s)", err, unconfined)
			}
			before := probeResults(string(unconfined))
			for _, call := range append(append(append([]string{}, tc.refused...), tc.kept...), residual...) {
				if before[call] != "ok" {
					t.Skipf("%s fails unconfined (%q), so a refusal under the sandbox would prove nothing: %s",
						call, before[call], unconfined)
				}
			}

			out, err := runConfinedCmd(t, c, p, start())
			if err != nil {
				t.Fatalf("the probe failed confined: %v (%s)", err, out)
			}
			got := probeResults(out)
			for _, call := range tc.refused {
				if got[call] != "operation not permitted" {
					t.Errorf("a confined body made %s: %q, want operation not permitted", call, got[call])
				}
			}
			for _, call := range tc.kept {
				if got[call] != "ok" {
					t.Errorf("a confined body was refused %s: %q", call, got[call])
				}
			}
			for _, call := range residual {
				switch {
				case c.Report().SignalScope():
					if got[call] != "operation not permitted" {
						t.Errorf("a body confined at Landlock ABI v%d made %s outside its domain: %q",
							c.Report().LandlockABI, call, got[call])
					}
				case got[call] != "ok":
					t.Errorf("landlock ABI v%d has no signal scope, and docs/functions.md says a body can make %s "+
						"there, but it got %q: correct the docs if something now refuses it",
						c.Report().LandlockABI, call, got[call])
				default:
					t.Logf("landlock ABI v%d has no signal scope: %s=ok is the documented residual",
						c.Report().LandlockABI, call)
				}
			}
		})
	}
}

// otherThread returns the lowest id among this process's threads other than
// its leader: sysmon, the first thread the Go runtime starts, which runs for
// the life of the process, and usually the pid plus one.
func otherThread(t *testing.T) string {
	t.Helper()
	tasks, err := os.ReadDir("/proc/self/task")
	if err != nil {
		t.Fatal(err)
	}
	lowest := 0
	for _, task := range tasks {
		tid, err := strconv.Atoi(task.Name())
		if err != nil || tid == os.Getpid() {
			continue
		}
		if lowest == 0 || tid < lowest {
			lowest = tid
		}
	}
	if lowest == 0 {
		t.Fatal("this process has no thread but its leader")
	}
	return strconv.Itoa(lowest)
}

// probeResults reads the probe's `call=result` lines.
func probeResults(out string) map[string]string {
	results := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if call, result, ok := strings.Cut(line, "="); ok {
			results[call] = result
		}
	}
	return results
}

// buildSocketProbe compiles a tiny helper that reports whether it can create
// an AF_INET socket. A compiled probe rather than a shell one-liner because
// neither sh nor a guaranteed-present utility can open a raw socket portably.
func buildSocketProbe(t *testing.T) (string, error) {
	t.Helper()
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
	return buildProbe(t, probeSrc)
}

// buildProbe compiles one self-contained main package into a temporary
// directory and returns the binary's path.
func buildProbe(t *testing.T, src string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "main.go")
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		return "", err
	}
	bin := filepath.Join(dir, "probe")
	cmd := exec.Command("go", "build", "-o", bin, file)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOFLAGS=")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("%w: %s", err, out)
	}
	return bin, nil
}

// signalProbeSrc makes every signal call a body could aim at another process,
// each with signal 0, tries to choose the signal a file's owner gets and to
// join its parent's process group, and prints one `call=result` line per
// call. Signal 0 asks the kernel for permission and
// delivers nothing, so a regression fails the test that runs this instead of
// stopping the test binary, or every process of this uid through kill(-1). The
// one real signal is to the probe's own child. A probe rather than a shell
// because sh can reach kill(2) alone.
const signalProbeSrc = `package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"unsafe"
)

func report(call string, err error) {
	if err == nil {
		fmt.Printf("%s=ok\n", call)
		return
	}
	fmt.Printf("%s=%v\n", call, err)
}

func raw(nr, a0, a1, a2, a3 uintptr) error {
	if _, _, errno := syscall.RawSyscall6(nr, a0, a1, a2, a3, 0, 0); errno != 0 {
		return errno
	}
	return nil
}

func main() {
	parent, self, tid := os.Getppid(), os.Getpid(), syscall.Gettid()
	group, err := syscall.Getpgid(parent)
	if err != nil {
		fmt.Printf("getpgid(parent)=%v\n", err)
		return
	}
	// A siginfo whose si_code is SI_QUEUE (-1): the kernel refuses a queued
	// signal to another process that claims to come from the kernel.
	var info [128]byte
	*(*int32)(unsafe.Pointer(&info[8])) = -1
	queued := uintptr(unsafe.Pointer(&info[0]))

	report("kill(parent)", syscall.Kill(parent, 0))
	report("kill(-group)", syscall.Kill(-group, 0))
	report("kill(-1)", syscall.Kill(-1, 0))
	report("tkill(parent)", raw(syscall.SYS_TKILL, uintptr(parent), 0, 0, 0))
	report("tgkill(parent)", raw(syscall.SYS_TGKILL, uintptr(parent), uintptr(parent), 0, 0))
	report("rt_sigqueueinfo(parent)", raw(syscall.SYS_RT_SIGQUEUEINFO, uintptr(parent), 0, queued, 0))
	report("rt_tgsigqueueinfo(parent)", raw(syscall.SYS_RT_TGSIGQUEUEINFO, uintptr(parent), uintptr(parent), 0, queued))

	// The owner of a file is signaled on its I/O with no signal syscall, so
	// the signal it gets must not be the body's to choose. Signal 0 here is
	// the default, SIGIO.
	var pipe [2]int
	if err := syscall.Pipe(pipe[:]); err != nil {
		fmt.Printf("pipe=%v\n", err)
		return
	}
	report("fcntl(F_SETSIG)", raw(syscall.SYS_FCNTL, uintptr(pipe[1]), syscall.F_SETSIG, 0, 0))
	report("fcntl(F_SETOWN)", raw(syscall.SYS_FCNTL, uintptr(pipe[1]), syscall.F_SETOWN, uintptr(self), 0))

	report("kill(0)", syscall.Kill(0, 0))
	report("kill(self)", syscall.Kill(self, 0))
	report("tkill(self)", raw(syscall.SYS_TKILL, uintptr(tid), 0, 0, 0))
	report("tgkill(self)", raw(syscall.SYS_TGKILL, uintptr(self), uintptr(tid), 0, 0))
	// The probe's own descriptors, because the policy grants no /dev/null
	// for os/exec to open.
	child := exec.Command("/bin/sleep", "30")
	child.Stdin, child.Stdout, child.Stderr = os.Stdin, os.Stderr, os.Stderr
	if err := child.Start(); err != nil {
		report("kill(child)", err)
	} else {
		report("kill(child)", syscall.Kill(child.Process.Pid, syscall.SIGTERM))
		_ = child.Process.Kill()
		_ = child.Wait()
	}

	if len(os.Args) > 2 {
		sibling, _ := strconv.Atoi(os.Args[1])
		report("kill(sibling)", syscall.Kill(sibling, 0))
		thread, _ := strconv.Atoi(os.Args[2])
		report("kill(parent thread)", syscall.Kill(thread, 0))
		report("tkill(parent thread)", raw(syscall.SYS_TKILL, uintptr(thread), 0, 0, 0))
	}
	// Last, because unconfined it moves the probe into the parent's group.
	report("setpgid(group)", syscall.Setpgid(0, group))
}
`

// The filter is assembled by hand, so its shape is worth asserting directly:
// a bad forward jump is the failure mode that would silently permit a denied
// syscall rather than crash.
func TestFilterAssembles(t *testing.T) {
	for _, network := range []bool{false, true} {
		prog, err := buildFilter(Policy{Network: network}, substrateProc{pid: 4242, pgid: 4200, shared: true})
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

// The ruleset asks for the signal scope exactly where the kernel has it: the
// kernel refuses a ruleset struct longer than its own unless the extra bytes
// are zero, so a `scoped` field sent below ABI 6 would fail every body, and
// one left out at ABI 6 leaves a body able to signal any process of its uid.
// This host's own ABI is the live half, in every confinement case above.
func TestRulesetScopesSignalsFromABI6(t *testing.T) {
	for abi := 1; abi <= SignalScopeABI+1; abi++ {
		attr, size := rulesetFor(abi)
		if attr.Access_fs != handledFor(abi) || attr.Access_net != 0 {
			t.Fatalf("ABI %d: handled fs %#x net %#x, want fs %#x and no net", abi, attr.Access_fs, attr.Access_net, handledFor(abi))
		}
		wantScoped, wantSize := uint64(0), uintptr(8)
		if abi >= SignalScopeABI {
			wantScoped, wantSize = unix.LANDLOCK_SCOPE_SIGNAL|unix.LANDLOCK_SCOPE_ABSTRACT_UNIX_SOCKET, 24
		}
		if attr.Scoped != wantScoped || size != wantSize {
			t.Fatalf("ABI %d: scoped %#x in %d bytes, want %#x in %d", abi, attr.Scoped, size, wantScoped, wantSize)
		}
	}
}

// The filter's verdicts, read by running the assembled program the way the
// kernel would rather than by matching its instructions: a compare against the
// wrong value or a jump one instruction short is invisible to a shape check,
// and needs no kernel to catch. The live half is
// TestConfinedChildCannotSignalTheSubstrate, which cannot reach pid 1.
func TestFilterVerdicts(t *testing.T) {
	const parent, group = 4242, 4200
	var (
		allow  = uint32(seccompRetAllow)
		eperm  = retErrno(unix.EPERM)
		enosys = retErrno(unix.ENOSYS)
		noAF   = retErrno(unix.EAFNOSUPPORT)
	)
	// neg is a pid as the 64-bit register a caller passes: sign-extended, so
	// its high half is set and only the low half is the kernel's pid_t.
	neg := func(pid int64) uint64 { return uint64(pid) }
	for _, tc := range []struct {
		name    string
		network bool
		shared  bool
		arch    uint32
		nr      int
		arg0    uint64
		arg1    uint64
		want    uint32
	}{
		{name: "kill the substrate", nr: unix.SYS_KILL, arg0: parent, want: eperm},
		{name: "kill the substrate's process group", nr: unix.SYS_KILL, arg0: neg(-group), want: eperm},
		{name: "kill the group the substrate's pid would lead", nr: unix.SYS_KILL, arg0: neg(-parent), want: eperm},
		{name: "kill every process", nr: unix.SYS_KILL, arg0: neg(-1), want: eperm},
		{name: "kill every process, high half clear", nr: unix.SYS_KILL, arg0: 0xffffffff, want: eperm},
		{name: "kill pid 1", nr: unix.SYS_KILL, arg0: 1, want: eperm},
		{name: "kill the substrate, high half set", nr: unix.SYS_KILL, arg0: 1<<32 | parent, want: eperm},
		{name: "kill the body's own process group", nr: unix.SYS_KILL, arg0: 0, want: allow},
		{name: "kill its own group when that is the substrate's", shared: true, nr: unix.SYS_KILL, arg0: 0, want: eperm},
		{name: "kill a child", nr: unix.SYS_KILL, arg0: parent + 1, want: allow},
		{name: "kill a child's process group", nr: unix.SYS_KILL, arg0: neg(-(parent + 1)), want: allow},
		{name: "rt_sigqueueinfo the substrate", nr: unix.SYS_RT_SIGQUEUEINFO, arg0: parent, want: eperm},
		{name: "rt_sigqueueinfo every process", nr: unix.SYS_RT_SIGQUEUEINFO, arg0: neg(-1), want: eperm},
		{name: "rt_sigqueueinfo the substrate's process group", nr: unix.SYS_RT_SIGQUEUEINFO, arg0: neg(-group), want: eperm},
		{name: "rt_sigqueueinfo pid 1", nr: unix.SYS_RT_SIGQUEUEINFO, arg0: 1, want: eperm},
		{name: "rt_sigqueueinfo a child", nr: unix.SYS_RT_SIGQUEUEINFO, arg0: parent + 1, want: allow},
		{name: "tgkill the substrate", nr: unix.SYS_TGKILL, arg0: parent, want: eperm},
		{name: "tgkill pid 1", nr: unix.SYS_TGKILL, arg0: 1, want: eperm},
		{name: "tgkill the body itself", nr: unix.SYS_TGKILL, arg0: parent + 1, want: allow},
		{name: "rt_tgsigqueueinfo the substrate", nr: unix.SYS_RT_TGSIGQUEUEINFO, arg0: parent, want: eperm},
		{name: "rt_tgsigqueueinfo pid 1", nr: unix.SYS_RT_TGSIGQUEUEINFO, arg0: 1, want: eperm},
		{name: "rt_tgsigqueueinfo the body itself", nr: unix.SYS_RT_TGSIGQUEUEINFO, arg0: parent + 1, want: allow},
		{name: "tkill the substrate's main thread", nr: unix.SYS_TKILL, arg0: parent, want: eperm},
		{name: "tkill pid 1", nr: unix.SYS_TKILL, arg0: 1, want: eperm},
		{name: "tkill the body's own thread", nr: unix.SYS_TKILL, arg0: parent + 1, want: allow},
		{name: "join the substrate's group", nr: unix.SYS_SETPGID, arg0: 0, arg1: group, want: eperm},
		{name: "move a child into the substrate's group", nr: unix.SYS_SETPGID, arg0: parent + 1, arg1: group, want: eperm},
		{name: "lead a group of its own", nr: unix.SYS_SETPGID, want: allow},
		{name: "join a child's group", nr: unix.SYS_SETPGID, arg1: parent + 1, want: allow},
		{name: "choose the signal a file's owner gets", nr: unix.SYS_FCNTL, arg0: 3, arg1: unix.F_SETSIG, want: eperm},
		{name: "make the substrate a file's owner", nr: unix.SYS_FCNTL, arg0: 3, arg1: unix.F_SETOWN, want: allow},
		{name: "an ordinary fcntl", nr: unix.SYS_FCNTL, arg0: 3, arg1: unix.F_SETFD, want: allow},
		{name: "signal through a pidfd", nr: unix.SYS_PIDFD_SEND_SIGNAL, arg0: 3, want: eperm},
		// What the filter already decided, held while the layout moves.
		{name: "an unfiltered syscall", nr: unix.SYS_GETPID, want: allow},
		{name: "a denied syscall", nr: unix.SYS_PTRACE, want: eperm},
		{name: "clone3", nr: sysClone3, want: enosys},
		{name: "a foreign architecture", arch: auditArch + 1, nr: unix.SYS_GETPID, want: enosys},
		{name: "the x32 numbering", nr: x32Bit | unix.SYS_GETPID, want: enosys},
		{name: "a unix socket", nr: sysSocket, arg0: unix.AF_UNIX, want: allow},
		{name: "an internet socket without network", nr: sysSocket, arg0: unix.AF_INET, want: noAF},
		{name: "an internet socket with network", network: true, nr: sysSocket, arg0: unix.AF_INET, want: allow},
		{name: "a packet socket with network", network: true, nr: sysSocket, arg0: unix.AF_PACKET, want: noAF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := buildFilter(Policy{Network: tc.network}, substrateProc{pid: parent, pgid: group, shared: tc.shared})
			if err != nil {
				t.Fatal(err)
			}
			arch := tc.arch
			if arch == 0 {
				arch = auditArch
			}
			data := seccompData{NR: int32(tc.nr), Arch: arch, Args: [6]uint64{tc.arg0, tc.arg1}}
			if got := runFilter(t, prog, data); got != tc.want {
				t.Fatalf("verdict = %#x, want %#x", got, tc.want)
			}
		})
	}
	// A stub that could not read its parent (outside its pid namespace, or
	// already gone) must not then refuse the body its own process group.
	prog, err := buildFilter(Policy{}, substrateProc{})
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range []seccompData{
		{NR: unix.SYS_KILL, Arch: auditArch},
		{NR: unix.SYS_SETPGID, Arch: auditArch},
	} {
		if got := runFilter(t, prog, data); got != allow {
			t.Fatalf("unknown parent: syscall %d with zero arguments: verdict = %#x, want allow", data.NR, got)
		}
	}
}

// runFilter executes a classic-BPF program over one seccomp_data, for the
// instructions buildFilter emits, and returns the action it reaches. The
// argument's halves are laid out little-endian, as on both image targets.
func runFilter(t *testing.T, prog []sockFilter, d seccompData) uint32 {
	t.Helper()
	load := func(off uint32) uint32 {
		switch {
		case off == sdNR:
			return uint32(d.NR)
		case off == sdArch:
			return d.Arch
		case off >= sdArg0 && off < sdArg0+8*uint32(len(d.Args)) && off%4 == 0:
			arg := d.Args[(off-sdArg0)/8]
			if (off-sdArg0)%8 == 4 {
				return uint32(arg >> 32)
			}
			return uint32(arg)
		}
		t.Fatalf("load from offset %d, which this interpreter does not model", off)
		return 0
	}
	var a uint32
	for pc := 0; pc < len(prog); {
		insn := prog[pc]
		switch insn.Code {
		case bpfLdAbsW:
			a = load(insn.K)
			pc++
		case bpfJeqK, bpfJgeK:
			taken := a == insn.K
			if insn.Code == bpfJgeK {
				taken = a >= insn.K
			}
			if taken {
				pc += 1 + int(insn.JT)
			} else {
				pc += 1 + int(insn.JF)
			}
		case bpfRetK:
			return insn.K
		default:
			t.Fatalf("instruction %d has opcode %#x, which this interpreter does not model", pc, insn.Code)
		}
	}
	t.Fatal("the program ran off its end without a return")
	return 0
}
