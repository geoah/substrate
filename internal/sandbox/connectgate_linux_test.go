//go:build linux

package sandbox

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/netip"
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
