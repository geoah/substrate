//go:build linux

package sandbox

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// The connect-destination decision, tested without a kernel: a resolver is
// allowed on port 53 (so DNS through a loopback stub works), the deployment's
// own ranges are refused, and the public internet is allowed.
func TestConnectGateAllowDest(t *testing.T) {
	g := &connectGate{resolvers: []netip.Addr{
		netip.MustParseAddr("127.0.0.53"),
		netip.MustParseAddr("127.0.0.11"),
	}}
	cases := []struct {
		addr  string
		port  uint16
		allow bool
	}{
		{"127.0.0.53", 53, true},       // the resolver, DNS port
		{"127.0.0.11", 53, true},       // Docker's resolver, DNS port
		{"127.0.0.53", 5432, false},    // the resolver IP, but not port 53
		{"127.0.0.1", 5432, false},     // loopback: the substrate's own ports
		{"10.0.0.5", 5432, false},      // RFC1918: the compose Postgres
		{"172.16.0.2", 5432, false},    // RFC1918
		{"169.254.169.254", 80, false}, // cloud metadata
		{"8.8.8.8", 53, true},          // a PUBLIC resolver is allowed as public
		{"1.1.1.1", 443, true},         // the public internet
	}
	for _, c := range cases {
		got := g.allowDest(netip.MustParseAddr(c.addr), c.port)
		if got != c.allow {
			t.Errorf("allowDest(%s, %d) = %v, want %v", c.addr, c.port, got, c.allow)
		}
	}
}

// A confined egress (Policy.EgressOnly) is the whole of what a body reaches:
// inside its prefixes is allowed, the public internet and a private range are
// not, and neither is a resolver on port 53, though its address sits inside,
// because it forwards the name off the host. SUBSTRATE_SANDBOX_EGRESS_ALLOW,
// read once per process, cannot widen it either: this binary's TestMain sets
// nothing, so the allowlist case is the gate's own field.
func TestConnectGateConfinedEgressAdmitsOnlyItsPrefixes(t *testing.T) {
	g := &connectGate{
		only:      []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")},
		resolvers: []netip.Addr{netip.MustParseAddr("127.0.0.53"), netip.MustParseAddr("10.0.0.2")},
	}
	if !g.confined() {
		t.Fatal("a gate with EgressOnly does not report itself confined")
	}
	for _, c := range []struct {
		addr  string
		port  uint16
		allow bool
	}{
		{"127.0.0.1", 8080, true},            // a loopback fixture
		{"::1", 8080, true},                  // the same over IPv6
		{"::ffff:127.0.0.1", 8080, true},     // mapped loopback is loopback
		{"127.0.0.53", 53, false},            // the resolver, though inside the prefixes
		{"127.0.0.53", 8080, true},           // the resolver's address on another port
		{"10.0.0.2", 53, false},              // a resolver outside the prefixes
		{"8.8.8.8", 53, false},               // a public resolver
		{"1.1.1.1", 443, false},              // the public internet
		{"10.0.0.5", 5432, false},            // RFC1918
		{"::ffff:1.1.1.1", 443, false},       // a mapped public address
		{"169.254.169.254", 80, false},       // cloud metadata
		{"2606:4700:4700::1111", 443, false}, // public IPv6
	} {
		if got := g.allowDest(netip.MustParseAddr(c.addr), c.port); got != c.allow {
			t.Errorf("allowDest(%s, %d) = %v, want %v", c.addr, c.port, got, c.allow)
		}
	}

	// The refusal says the egress is confined, and names no variable: no
	// allowlist lifts it.
	g.refuse(42, netip.MustParseAddrPort("1.1.1.1:443"))
	got := g.RefusedSince(0)
	const want = "the sandbox's connect gate refused 1.1.1.1:443: this process confines a body's egress (sandbox.Policy.EgressOnly)"
	if len(got) != 1 || got[0].String() != want || strings.Contains(got[0].String(), EgressAllowVar) {
		t.Fatalf("RefusedSince = %v, want one refusal reading %q", got, want)
	}
}

// A policy refusal names the sandbox's connect gate and the variable that
// would admit the destination, in the record the runner reads and in one WARN
// line per destination. A reader sees only the refusals after its own mark,
// which keeps a refusal recorded before an exchange out of that exchange's
// error.
func TestConnectGateRefusalNamesItsAllowlistVariable(t *testing.T) {
	logs := &syncBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	g := &connectGate{}
	mark := g.RefusalMark()
	g.refuse(42, netip.MustParseAddrPort("10.0.0.7:5432"))
	got := g.RefusedSince(mark)
	const want = "the sandbox's connect gate refused 10.0.0.7:5432; allow it with SUBSTRATE_SANDBOX_EGRESS_ALLOW=10.0.0.7"
	if len(got) != 1 || got[0].String() != want {
		t.Fatalf("RefusedSince = %v, want one refusal reading %q", got, want)
	}
	line := waitForLog(t, logs, "destination=10.0.0.7:5432")
	for _, part := range []string{
		"level=WARN",
		`msg="egress blocked by the sandbox's connect gate"`,
		"SUBSTRATE_SANDBOX_EGRESS_ALLOW=10.0.0.7",
	} {
		if !strings.Contains(line, part) {
			t.Fatalf("the refusal's log line lacks %q:\n%s", part, line)
		}
	}

	// The same destination again: recorded for the reader whose mark is
	// before it, not logged a second time.
	mark = g.RefusalMark()
	g.refuse(42, netip.MustParseAddrPort("10.0.0.7:5432"))
	if got := g.RefusedSince(mark); len(got) != 1 {
		t.Fatalf("RefusedSince after the repeat = %v, want the one repeat", got)
	}
	if got := g.RefusedSince(g.RefusalMark()); len(got) != 0 {
		t.Fatalf("RefusedSince(the newest mark) = %v, want none", got)
	}

	// An IPv4-mapped destination names its IPv4 address, the form the
	// allowlist matches. Its line is also the sentinel for the repeat above:
	// one goroutine writes the queue in order, so once this line is out, a
	// second line for the repeat would be out too.
	mark = g.RefusalMark()
	g.refuse(42, netip.MustParseAddrPort("[::ffff:10.0.0.8]:443"))
	if got := g.RefusedSince(mark); len(got) != 1 || !strings.HasSuffix(got[0].String(), "SUBSTRATE_SANDBOX_EGRESS_ALLOW=10.0.0.8") {
		t.Fatalf("RefusedSince for a mapped address = %v, want it to name 10.0.0.8", got)
	}
	all := waitForLog(t, logs, "destination=10.0.0.8:443")
	if n := strings.Count(all, "destination=10.0.0.7:5432"); n != 1 {
		t.Fatalf("a repeated destination logged %d lines, want 1:\n%s", n, all)
	}

	// A body looping on refused connects grows neither the record nor the set
	// of logged destinations.
	for i := range 200 {
		g.refuse(42, netip.MustParseAddrPort(fmt.Sprintf("10.0.%d.%d:80", i/250, i%250+1)))
	}
	if n := len(g.RefusedSince(0)); n > maxRefusals {
		t.Fatalf("the record holds %d refusals, want at most %d", n, maxRefusals)
	}
	g.refusalMu.Lock()
	warned := len(g.warned)
	g.refusalMu.Unlock()
	if warned > maxWarned {
		t.Fatalf("the gate warned about %d destinations, want at most %d", warned, maxWarned)
	}
}

// syncBuffer is a log sink the warning goroutine writes while a test reads.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// waitForLog returns the log once it holds want. The warning is written by
// another goroutine, so it may land after refuse returns.
func waitForLog(t *testing.T, logs *syncBuffer, want string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := logs.String()
		if strings.Contains(got, want) {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("no log line with %q within 5s:\n%s", want, got)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// The notify filter is hand-assembled, so its shape is asserted directly: it
// must end in an ALLOW then a USER_NOTIF terminal, and every jump must land
// inside the program and forward.
func TestConnectNotifyFilterAssembles(t *testing.T) {
	prog := buildConnectNotifyFilter()
	if len(prog) < 4 {
		t.Fatalf("suspiciously short program (%d)", len(prog))
	}
	for i, insn := range prog {
		if insn.Code&0x07 != 0x05 { // not BPF_JMP
			continue
		}
		for _, off := range []uint8{insn.JT, insn.JF} {
			if target := i + 1 + int(off); target >= len(prog) {
				t.Fatalf("instruction %d jumps to %d, past the end (%d)", i, target, len(prog))
			}
		}
	}
	last := prog[len(prog)-2:]
	if last[0].Code != bpfRetK || last[0].K != seccompRetAllow {
		t.Fatalf("second-to-last terminal = %#x, want ALLOW %#x", last[0].K, seccompRetAllow)
	}
	if last[1].Code != bpfRetK || last[1].K != uint32(unix.SECCOMP_RET_USER_NOTIF) {
		t.Fatalf("last terminal = %#x, want USER_NOTIF %#x", last[1].K, uint32(unix.SECCOMP_RET_USER_NOTIF))
	}
}

// The unix address the runner connects to on a body's behalf: an absolute
// path as the kernel would read it, a relative one through a descriptor on
// the connecting thread's working directory, and an abstract one refused,
// because Landlock holds a body's abstract connects to its own domain and the
// runner's connect is outside it.
func TestUnixAddressIsTheBodysPath(t *testing.T) {
	sun := func(path string) []byte {
		return append([]byte{unix.AF_UNIX, 0}, path...)
	}
	tid := unix.Gettid()
	for _, tc := range []struct {
		name  string
		raw   []byte
		want  string
		errno unix.Errno
	}{
		{name: "an absolute path", raw: sun("/run/app.sock"), want: "/run/app.sock"},
		{name: "an absolute path the kernel ends at its NUL", raw: sun("/run/app.sock\x00junk"), want: "/run/app.sock"},
		{name: "an abstract address", raw: sun("\x00name"), errno: unix.EACCES},
		{name: "no path at all", raw: sun(""), errno: unix.EINVAL},
		{name: "longer than sockaddr_un", raw: sun("/" + strings.Repeat("a", 108)), errno: unix.EINVAL},
		{name: "a relative path past the room the prefix leaves", raw: sun(strings.Repeat("a", 100)), errno: unix.ENAMETOOLONG},
	} {
		addr, dirfd, errno := unixAddress(tid, tc.raw)
		if errno != tc.errno {
			t.Errorf("%s: errno %v, want %v", tc.name, errno, tc.errno)
			continue
		}
		if dirfd != -1 {
			t.Errorf("%s: opened descriptor %d, want none", tc.name, dirfd)
			_ = unix.Close(dirfd)
		}
		if errno == 0 && string(addr[2:]) != tc.want {
			t.Errorf("%s: path %q, want %q", tc.name, addr[2:], tc.want)
		}
	}

	addr, dirfd, errno := unixAddress(tid, sun("app.sock"))
	if errno != 0 || dirfd < 0 {
		t.Fatalf("a relative path: errno %v, descriptor %d", errno, dirfd)
	}
	defer func() { _ = unix.Close(dirfd) }()
	if want := fmt.Sprintf("/proc/self/fd/%d/app.sock", dirfd); string(addr[2:]) != want {
		t.Fatalf("a relative path: %q, want %q", addr[2:], want)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if dir, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", dirfd)); err != nil || dir != cwd {
		t.Fatalf("the descriptor names %q (%v), want the working directory %q", dir, err, cwd)
	}
}
