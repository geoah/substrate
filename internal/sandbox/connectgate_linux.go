//go:build linux

package sandbox

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/geoah/substrate/internal/egress"
)

// The connect gate: what makes `permissions.network` mean "reach the internet"
// rather than "reach anything the substrate's network routes to". A body shares
// the substrate's network namespace and the container grants no capability to
// give it its own, so the destination is filtered per connection instead of by
// network topology (0035-a-network-body-connect-is-filtered-by-destination).
//
// The stub installs a SECOND seccomp filter, stacked under the main deny
// filter, that returns SECCOMP_RET_USER_NOTIF for connect(2). It hands the
// listener descriptor back to the runner (an ancestor, so ptrace-scope 1 lets
// it read the body's memory) over a unix socket. On each connect the runner
// reads the target sockaddr and refuses the deployment's own ranges, letting the
// public internet through.

// seccompIoctlNotifIDValid is the one notify ioctl x/sys does not name. RECV,
// SEND and the CONTINUE flag it does.
const seccompIoctlNotifIDValid = 0x40082102

// seccompData mirrors struct seccomp_data; seccompNotif and seccompNotifResp
// mirror the notification structs. Their sizes are encoded into the RECV/SEND
// ioctl numbers (0x50 and 0x18), so a layout mismatch is a kernel error, not a
// silent misread.
type seccompData struct {
	NR                 int32
	Arch               uint32
	InstructionPointer uint64
	Args               [6]uint64
}

type seccompNotif struct {
	ID    uint64
	Pid   uint32
	Flags uint32
	Data  seccompData
}

type seccompNotifResp struct {
	ID    uint64
	Val   int64
	Error int32
	Flags uint32
}

// pendingGate is the parent end of one connect-gate socket, held between Wrap
// and Serve.
type pendingGate struct {
	parent *os.File
	child  *os.File
}

// buildConnectNotifyFilter returns USER_NOTIF for connect(2) on this
// architecture and ALLOW for everything else. Stacked under the main deny
// filter: for connect the main filter returns ALLOW and this one USER_NOTIF, and
// the kernel takes the higher-precedence USER_NOTIF. A foreign architecture or
// any other syscall falls through to ALLOW, leaving the main filter's decision
// in force. x32 need not be handled: the main filter already denies the whole
// x32 table.
func buildConnectNotifyFilter() []sockFilter {
	return []sockFilter{
		{Code: bpfLdAbsW, K: sdArch},
		// Not our architecture: skip to ALLOW at index 4.
		{Code: bpfJeqK, JT: 0, JF: 2, K: auditArch},
		{Code: bpfLdAbsW, K: sdNR},
		// connect: jump to USER_NOTIF at index 5.
		{Code: bpfJeqK, JT: 1, JF: 0, K: uint32(unix.SYS_CONNECT)},
		{Code: bpfRetK, K: seccompRetAllow},
		{Code: bpfRetK, K: uint32(unix.SECCOMP_RET_USER_NOTIF)},
	}
}

// installConnectGate installs the connect notify filter and hands the listener
// descriptor to the parent over the socket at notifFD, then closes both. It runs
// in the stub, after the main filter and just before exec. A failure is fatal:
// a body that reached the network with no supervisor serving it would block
// forever on its first connect.
func installConnectGate(notifFD int) error {
	if auditArch == 0 {
		return fmt.Errorf("no syscall table for this architecture")
	}
	prog := buildConnectNotifyFilter()
	fprog := sockFprog{Len: uint16(len(prog)), Fil: &prog[0]}
	lfd, _, errno := unix.Syscall(unix.SYS_SECCOMP, seccompSetModeFilter,
		unix.SECCOMP_FILTER_FLAG_NEW_LISTENER, uintptr(unsafe.Pointer(&fprog)))
	if errno != 0 {
		return fmt.Errorf("install listener: %w", errno)
	}
	defer func() { _ = unix.Close(int(lfd)) }()
	rights := unix.UnixRights(int(lfd))
	if err := unix.Sendmsg(notifFD, []byte{0}, rights, nil, 0); err != nil {
		return fmt.Errorf("send listener: %w", err)
	}
	_ = unix.Close(notifFD)
	return nil
}

// serve is the linux half of Confiner.Serve: it receives the listener
// descriptor for cmd and starts the supervisor.
func (c *Confiner) serve(cmd *exec.Cmd) (io.Closer, error) {
	v, ok := c.pending.LoadAndDelete(cmd)
	if !ok {
		return noopCloser{}, nil
	}
	g := v.(*pendingGate)
	// The child holds its own dup of the socket; drop the parent's copy of the
	// child end so recvmsg reports EOF if the stub dies before sending.
	_ = g.child.Close()
	defer func() { _ = g.parent.Close() }()

	lfd, err := recvListener(g.parent)
	if err != nil {
		return nil, fmt.Errorf("sandbox: connect gate: %w", err)
	}
	return startConnectGate(lfd, c.resolvers)
}

// recvListener reads the seccomp listener descriptor the stub sent, bounded so a
// stub that dies before sending fails the launch rather than hanging it.
func recvListener(parent *os.File) (int, error) {
	fd := int(parent.Fd()) // Fd() puts the socket in blocking mode; we poll it ourselves
	pfd := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	for {
		n, err := unix.Poll(pfd, 5000)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return -1, fmt.Errorf("poll: %w", err)
		}
		if n == 0 {
			return -1, fmt.Errorf("timed out waiting for the listener descriptor")
		}
		break
	}
	buf := make([]byte, 1)
	oob := make([]byte, unix.CmsgSpace(4))
	oobn, err := recvOOB(fd, buf, oob)
	if err != nil {
		return -1, fmt.Errorf("recvmsg: %w", err)
	}
	scms, err := unix.ParseSocketControlMessage(oob[:oobn])
	if err != nil {
		return -1, fmt.Errorf("parse control message: %w", err)
	}
	if len(scms) == 0 {
		return -1, fmt.Errorf("no listener descriptor received")
	}
	fds, err := unix.ParseUnixRights(&scms[0])
	if err != nil {
		return -1, fmt.Errorf("parse rights: %w", err)
	}
	if len(fds) == 0 {
		return -1, fmt.Errorf("no listener descriptor received")
	}
	return fds[0], nil
}

// recvOOB reads one message and returns the length of its control data, hiding
// the recvmsg return shape that carries a byte count and two fields this caller
// has no use for.
func recvOOB(fd int, buf, oob []byte) (int, error) {
	n, oobn, recvflags, _, err := unix.Recvmsg(fd, buf, oob, 0)
	_, _ = n, recvflags
	return oobn, err
}

// connectGate supervises one body's connect notifications.
type connectGate struct {
	lfd       int
	stop      int // eventfd, to wake the poll loop on Close
	done      chan struct{}
	resolvers []netip.Addr
	once      sync.Once
}

func startConnectGate(lfd int, resolvers []netip.Addr) (io.Closer, error) {
	efd, err := unix.Eventfd(0, unix.EFD_CLOEXEC|unix.EFD_NONBLOCK)
	if err != nil {
		_ = unix.Close(lfd)
		return nil, fmt.Errorf("sandbox: connect gate eventfd: %w", err)
	}
	g := &connectGate{lfd: lfd, stop: efd, done: make(chan struct{}), resolvers: resolvers}
	go g.run()
	return g, nil
}

// Close stops the supervisor and releases its descriptors. Idempotent: the
// runner closes it on every teardown path.
func (g *connectGate) Close() error {
	g.once.Do(func() {
		var one [8]byte
		one[0] = 1
		_, _ = unix.Write(g.stop, one[:]) // wake run() if it is polling
		<-g.done
		_ = unix.Close(g.stop)
		_ = unix.Close(g.lfd)
	})
	return nil
}

func (g *connectGate) run() {
	defer close(g.done)
	pfd := []unix.PollFd{
		{Fd: int32(g.lfd), Events: unix.POLLIN},
		{Fd: int32(g.stop), Events: unix.POLLIN},
	}
	for {
		pfd[0].Revents, pfd[1].Revents = 0, 0
		if _, err := unix.Poll(pfd, -1); err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return
		}
		if pfd[1].Revents != 0 {
			return // Close asked us to stop
		}
		if pfd[0].Revents&unix.POLLIN != 0 {
			if !g.handleOne() {
				return
			}
			continue
		}
		if pfd[0].Revents&(unix.POLLHUP|unix.POLLERR|unix.POLLNVAL) != 0 {
			return // every target thread exited: the listener is dead
		}
	}
}

// handleOne services one connect notification, answering CONTINUE for an
// allowed destination and EACCES for a refused one. It returns false only on an
// error that makes the listener unusable.
func (g *connectGate) handleOne() bool {
	var n seccompNotif
	if err := notifIoctl(g.lfd, unix.SECCOMP_IOCTL_NOTIF_RECV, unsafe.Pointer(&n)); err != nil {
		// EINTR, or a notification canceled before we read it: recoverable.
		return errors.Is(err, unix.EINTR) || errors.Is(err, unix.ENOENT)
	}
	resp := seccompNotifResp{ID: n.ID}
	if g.decideAllow(&n) {
		resp.Flags = uint32(unix.SECCOMP_USER_NOTIF_FLAG_CONTINUE)
	} else {
		resp.Error = -int32(unix.EACCES)
	}
	if err := notifIoctl(g.lfd, unix.SECCOMP_IOCTL_NOTIF_SEND, unsafe.Pointer(&resp)); err != nil {
		// ENOENT: the target was interrupted or exited before we answered, so
		// this notification is void. Keep serving.
		return errors.Is(err, unix.ENOENT) || errors.Is(err, unix.EINTR)
	}
	return true
}

// decideAllow reads the connect target from the body's memory and classifies it.
// Anything it cannot read or parse is refused: a destination that cannot be
// checked is not one to allow.
func (g *connectGate) decideAllow(n *seccompNotif) bool {
	uaddr := uintptr(n.Data.Args[1])
	alen := int(int32(n.Data.Args[2]))
	if uaddr == 0 || alen < 2 || alen > 128 {
		return false
	}
	buf := make([]byte, alen)
	if !readTargetMem(int(n.Pid), uaddr, buf) {
		return false
	}
	// The read is trustworthy only while the target is still blocked in this
	// notification; ID_VALID confirms it has not been interrupted since.
	if err := notifIoctl(g.lfd, seccompIoctlNotifIDValid, unsafe.Pointer(&n.ID)); err != nil {
		return false
	}
	// SAFETY: passing every non-INET family is safe only because the main
	// seccomp socket-domain allowlist (seccomp_linux.go, buildFilter) bounds a
	// body to AF_UNIX, AF_INET, AF_INET6 and AF_NETLINK, none of which reach an
	// off-machine IP service without going through the AF_INET(6) arms below. If
	// that allowlist ever gains AF_PACKET or AF_VSOCK, this default would pass
	// them unfiltered and both sides must change together.
	switch binary.LittleEndian.Uint16(buf[0:2]) {
	case unix.AF_UNIX:
		return true // addresses nothing off the machine
	case unix.AF_INET:
		if alen < 8 {
			return false
		}
		port := binary.BigEndian.Uint16(buf[2:4])
		var a [4]byte
		copy(a[:], buf[4:8])
		return g.allowDest(netip.AddrFrom4(a), port)
	case unix.AF_INET6:
		if alen < 24 {
			return false
		}
		port := binary.BigEndian.Uint16(buf[2:4])
		var a [16]byte
		copy(a[:], buf[8:24])
		return g.allowDest(netip.AddrFrom16(a), port)
	default:
		// AF_NETLINK and the like: the socket gate allowed them and they reach
		// nothing off the IP stack, so leave them to the main filter.
		return true
	}
}

// allowDest is the destination policy: an operator-permitted range, then a
// resolver on port 53 (so DNS through a loopback stub keeps working), then any
// address the egress classifier does not mark as the deployment's own.
func (g *connectGate) allowDest(addr netip.Addr, port uint16) bool {
	a := addr.Unmap()
	for _, p := range egressAllow() {
		if p.Contains(a) {
			return true
		}
	}
	if port == 53 && g.isResolver(addr) {
		return true
	}
	return !egress.Blocked(addr)
}

// egressAllow is the operator's escape from the private-range block: a
// comma-separated list of CIDRs (or bare addresses) in
// SUBSTRATE_SANDBOX_EGRESS_ALLOW that a network body may reach even though they
// fall in a private range. It is how a deployment permits a local provider (a
// loopback Ollama, issue #241's documented escape) without opening the whole
// private range. Parsed once, at first use, so a process that sets the variable
// before it starts a body (every real one) is read correctly.
var egressAllow = sync.OnceValue(func() []netip.Prefix {
	raw := strings.TrimSpace(os.Getenv("SUBSTRATE_SANDBOX_EGRESS_ALLOW"))
	if raw == "" {
		return nil
	}
	var out []netip.Prefix
	for _, tok := range strings.Split(raw, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		if p, err := netip.ParsePrefix(tok); err == nil {
			out = append(out, p.Masked())
			continue
		}
		if a, err := netip.ParseAddr(tok); err == nil {
			out = append(out, netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()))
		}
	}
	return out
})

func (g *connectGate) isResolver(addr netip.Addr) bool {
	a := addr.Unmap()
	for _, r := range g.resolvers {
		if r == a {
			return true
		}
	}
	return false
}

// readTargetMem copies len(buf) bytes from the body's address space. It is
// permitted because the runner is the body's ancestor under yama ptrace scope 1
// and shares its uid.
func readTargetMem(pid int, remote uintptr, buf []byte) bool {
	local := []unix.Iovec{{Base: &buf[0], Len: uint64(len(buf))}}
	remoteIov := []unix.RemoteIovec{{Base: remote, Len: len(buf)}}
	n, err := unix.ProcessVMReadv(pid, local, remoteIov, 0)
	return err == nil && n == len(buf)
}

func notifIoctl(fd int, req uint, arg unsafe.Pointer) error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(req), uintptr(arg))
	if errno != 0 {
		return errno
	}
	return nil
}

// loadResolvers reads the nameserver addresses from /etc/resolv.conf. They are
// allowed on port 53 so a body's name resolution through a loopback stub
// (Docker's 127.0.0.11, systemd-resolved's 127.0.0.53) survives the connect
// filter that otherwise refuses loopback. A parse failure yields no resolvers,
// which only tightens the gate.
func loadResolvers() []netip.Addr {
	f, err := os.Open("/etc/resolv.conf")
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	var out []netip.Addr
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 || fields[0] != "nameserver" {
			continue
		}
		if a, err := netip.ParseAddr(fields[1]); err == nil {
			out = append(out, a.Unmap())
		}
	}
	return out
}
