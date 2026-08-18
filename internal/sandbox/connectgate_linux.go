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
	"strconv"
	"strings"
	"sync"
	"time"
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

// seccompNotifAddfd mirrors struct seccomp_notif_addfd. Its size (24 bytes) is
// baked into the ADDFD ioctl number (0x40182103).
type seccompNotifAddfd struct {
	ID         uint64
	Flags      uint32
	SrcFD      uint32
	NewFD      uint32
	NewFDFlags uint32
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

// handleOne services one connect notification. It returns false only on an
// error that makes the listener unusable.
func (g *connectGate) handleOne() bool {
	var n seccompNotif
	if err := notifIoctl(g.lfd, unix.SECCOMP_IOCTL_NOTIF_RECV, unsafe.Pointer(&n)); err != nil {
		// EINTR, or a notification canceled before we read it: recoverable.
		return errors.Is(err, unix.EINTR) || errors.Is(err, unix.ENOENT)
	}
	return g.service(&n)
}

// verdict is what to do with one connect.
type verdict int

const (
	verdictDeny    verdict = iota // refuse with EACCES
	verdictPass                   // let the kernel run it: a non-INET family
	verdictConnect                // an allowed INET destination: emulate it
)

// service answers one notification. An allowed INET destination is NOT answered
// with CONTINUE: the kernel would re-read the body's sockaddr when it resumed
// the syscall, and a second thread could flip the address in between (a TOCTOU
// race). Instead the runner connects to the address it verified and installs
// that connected socket into the body, so the kernel never re-reads the body's
// memory. Every other path refuses; nothing falls open.
func (g *connectGate) service(n *seccompNotif) bool {
	v, family, dest := g.classify(n)
	switch v {
	case verdictPass:
		// A non-INET family (AF_UNIX, AF_NETLINK). CONTINUE is safe here: these
		// reach nothing off the IP stack, and the kernel and Landlock still
		// mediate them when the syscall resumes. Only INET is raceable, and only
		// INET is emulated.
		return g.send(&seccompNotifResp{ID: n.ID, Flags: uint32(unix.SECCOMP_USER_NOTIF_FLAG_CONTINUE)})
	case verdictConnect:
		return g.emulateConnect(n, family, dest)
	default:
		return g.send(denyResp(n.ID, unix.EACCES))
	}
}

// classify reads the connect target from the body's memory and decides. The
// read is trustworthy only while the target is still blocked in this
// notification, which ID_VALID confirms; anything it cannot read, parse or
// allow is a deny.
//
// SAFETY: treating every non-INET family as verdictPass is safe only because
// the main seccomp socket-domain allowlist (seccomp_linux.go, buildFilter)
// bounds a body to AF_UNIX, AF_INET, AF_INET6 and AF_NETLINK, none of which
// reach an off-machine IP service without going through the AF_INET(6) arms
// below. If that allowlist ever gains AF_PACKET or AF_VSOCK, this default would
// pass them unfiltered and both sides must change together.
func (g *connectGate) classify(n *seccompNotif) (verdict, int, netip.AddrPort) {
	uaddr := uintptr(n.Data.Args[1])
	alen := int(int32(n.Data.Args[2]))
	if uaddr == 0 || alen < 2 || alen > 128 {
		return verdictDeny, 0, netip.AddrPort{}
	}
	buf := make([]byte, alen)
	if !readTargetMem(int(n.Pid), uaddr, buf) {
		return verdictDeny, 0, netip.AddrPort{}
	}
	if err := notifIoctl(g.lfd, seccompIoctlNotifIDValid, unsafe.Pointer(&n.ID)); err != nil {
		return verdictDeny, 0, netip.AddrPort{}
	}
	family := int(binary.LittleEndian.Uint16(buf[0:2]))
	switch family {
	case unix.AF_UNIX:
		return verdictPass, family, netip.AddrPort{}
	case unix.AF_INET:
		if alen < 8 {
			return verdictDeny, family, netip.AddrPort{}
		}
		port := binary.BigEndian.Uint16(buf[2:4])
		var a [4]byte
		copy(a[:], buf[4:8])
		addr := netip.AddrFrom4(a)
		if !g.allowDest(addr, port) {
			return verdictDeny, family, netip.AddrPort{}
		}
		return verdictConnect, family, netip.AddrPortFrom(addr, port)
	case unix.AF_INET6:
		if alen < 24 {
			return verdictDeny, family, netip.AddrPort{}
		}
		port := binary.BigEndian.Uint16(buf[2:4])
		var a [16]byte
		copy(a[:], buf[8:24])
		addr := netip.AddrFrom16(a)
		if !g.allowDest(addr, port) {
			return verdictDeny, family, netip.AddrPort{}
		}
		return verdictConnect, family, netip.AddrPortFrom(addr, port)
	default:
		return verdictPass, family, netip.AddrPort{}
	}
}

// emulateConnect connects to the verified destination in the runner and
// installs the connected socket into the body over the descriptor it called
// connect(2) on, then completes the syscall. Every failure path answers the
// body with an error and never allows: a socket the runner cannot faithfully
// reproduce is refused (EACCES), and a genuine connection failure is handed back
// as its real errno so the body sees an ordinary failed connect.
//
// A body's own connect timeout is honored by NOT waiting on a non-blocking
// socket: the runner starts the connect to the verified address, hands the body
// the still-connecting descriptor and returns EINPROGRESS, so the body's own
// poll loop drives it to completion on its own clock. The connection is already
// aimed at the address the runner checked, so the body cannot redirect it. A
// blocking socket has no body-side timeout, so the runner waits for it, bounded
// by the body's own lifetime (waitConnect abandons when the notification goes
// invalid).
func (g *connectGate) emulateConnect(n *seccompNotif, family int, dest netip.AddrPort) bool {
	bodyFD := int(int32(n.Data.Args[0]))
	// seccomp reports the connecting THREAD's tid, but pidfd_open needs the
	// thread-group leader; a body that connects from a worker thread (uv, tokio)
	// has tid != tgid. Threads share the fd table, so the leader's pidfd reaches
	// the same descriptor.
	tgid := tgidOf(int(n.Pid))
	info, err := inspectSocket(tgid, bodyFD)
	if err != nil {
		return g.send(denyResp(n.ID, unix.EACCES))
	}
	defer info.close()
	info.cloexec = fdCloexec(tgid, bodyFD)
	// A bound source address the fresh socket cannot carry, or a type the runner
	// does not emulate (only stream and datagram): refuse rather than connect
	// with different semantics.
	if info.bound || (info.typ != unix.SOCK_STREAM && info.typ != unix.SOCK_DGRAM) {
		return g.send(denyResp(n.ID, unix.EACCES))
	}
	// Confirm the notification is still live before spending a connect on it.
	if err := notifIoctl(g.lfd, seccompIoctlNotifIDValid, unsafe.Pointer(&n.ID)); err != nil {
		return g.send(denyResp(n.ID, unix.EACCES))
	}

	fd, err := unix.Socket(info.domain, info.typ|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return g.send(denyResp(n.ID, toErrno(err, unix.EACCES)))
	}
	defer func() { _ = unix.Close(fd) }() // addfd dups it in; the runner's copy always closes
	sa := sockaddrFor(family, dest)
	if sa == nil {
		return g.send(denyResp(n.ID, unix.EAFNOSUPPORT))
	}

	// The response errno to hand the body: 0 means the connect returns success.
	var respErr unix.Errno
	switch err := unix.Connect(fd, sa); {
	case err == nil:
		// A datagram socket, or a stream connection that completed at once.
	case errors.Is(err, unix.EINPROGRESS):
		if info.nonblock {
			// Hand back the connecting socket and let the body poll it on its own
			// timeout.
			respErr = unix.EINPROGRESS
		} else if e := g.waitConnect(fd, n.ID); e != 0 {
			return g.send(denyResp(n.ID, e))
		}
	default:
		return g.send(denyResp(n.ID, toErrno(err, unix.ECONNREFUSED)))
	}

	// Give the body the blocking mode its own socket had, then install the socket
	// at the body's descriptor number.
	if err := setNonblock(fd, info.nonblock); err != nil {
		return g.send(denyResp(n.ID, unix.EACCES))
	}
	if err := g.addfd(n.ID, fd, bodyFD, info.cloexec); err != nil {
		return g.send(denyResp(n.ID, unix.EACCES))
	}
	if respErr != 0 {
		return g.send(denyResp(n.ID, respErr))
	}
	return g.send(&seccompNotifResp{ID: n.ID})
}

const (
	connectTimeout = 30 * time.Second
	connectPollMs  = 250
)

// waitConnect waits for a non-blocking stream connect to finish, in slices, so
// it can drop the attempt the moment the body's notification is no longer valid
// (the body exited or was interrupted) or the gate is being torn down. It
// returns 0 on success or the errno the connection failed with. The stop
// eventfd is in the pollset so Close does not have to wait out connectTimeout on
// a blocking connect to a slow address.
func (g *connectGate) waitConnect(fd int, id uint64) unix.Errno {
	deadline := time.Now().Add(connectTimeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return unix.ETIMEDOUT
		}
		ms := int(remaining / time.Millisecond)
		if ms > connectPollMs {
			ms = connectPollMs
		}
		pfd := []unix.PollFd{
			{Fd: int32(fd), Events: unix.POLLOUT},
			{Fd: int32(g.stop), Events: unix.POLLIN},
		}
		nr, err := unix.Poll(pfd, ms)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return unix.EACCES
		}
		if pfd[1].Revents != 0 {
			// Teardown: abandon the connect. The body is being killed anyway, so a
			// failed connect is the right answer.
			return unix.ECONNABORTED
		}
		if nr == 0 {
			// A poll slice elapsed; the body must still be waiting on this connect.
			if notifIoctl(g.lfd, seccompIoctlNotifIDValid, unsafe.Pointer(&id)) != nil {
				return unix.ECONNABORTED
			}
			continue
		}
		if pfd[0].Revents == 0 {
			continue
		}
		soErr, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_ERROR)
		if err != nil {
			return unix.EACCES
		}
		if soErr != 0 {
			return unix.Errno(soErr)
		}
		return 0
	}
}

// addfd installs srcfd into the body at the exact descriptor number newfd it
// called connect on, replacing the socket that was there.
func (g *connectGate) addfd(id uint64, srcfd, newfd int, cloexec bool) error {
	a := seccompNotifAddfd{
		ID:    id,
		Flags: uint32(unix.SECCOMP_ADDFD_FLAG_SETFD),
		SrcFD: uint32(srcfd),
		NewFD: uint32(newfd),
	}
	if cloexec {
		a.NewFDFlags = uint32(unix.O_CLOEXEC)
	}
	return notifIoctl(g.lfd, unix.SECCOMP_IOCTL_NOTIF_ADDFD, unsafe.Pointer(&a))
}

// send writes one response. It returns false only when the listener itself is
// unusable; a notification that vanished under us (ENOENT) or a signal (EINTR)
// is per-request and keeps the supervisor serving.
func (g *connectGate) send(resp *seccompNotifResp) bool {
	if err := notifIoctl(g.lfd, unix.SECCOMP_IOCTL_NOTIF_SEND, unsafe.Pointer(resp)); err != nil {
		return errors.Is(err, unix.ENOENT) || errors.Is(err, unix.EINTR)
	}
	return true
}

func denyResp(id uint64, errno unix.Errno) *seccompNotifResp {
	return &seccompNotifResp{ID: id, Error: -int32(errno)}
}

// sockInfo is what the runner learns about the body's socket before it emulates
// the connect, read through a dup of the body's descriptor that never changes
// the body's own socket.
type sockInfo struct {
	dupfd    int
	domain   int
	typ      int
	nonblock bool
	cloexec  bool
	bound    bool
}

func (s *sockInfo) close() {
	if s != nil && s.dupfd >= 0 {
		_ = unix.Close(s.dupfd)
	}
}

// inspectSocket dups the body's connect socket into the runner and reads its
// domain, type, blocking flag and whether it was bound. The dup shares the
// body's open file description, so all of those read through it without changing
// the body's socket; it is closed by sockInfo.close. pidfd_getfd is permitted
// because the runner is the body's ancestor under yama ptrace scope 1.
//
// The close-on-exec flag is NOT read here: FD_CLOEXEC is per-descriptor, not a
// property of the shared open file description, so the dup would report the
// runner's own copy, not the body's. It is read from the body's descriptor in
// fdCloexec.
func inspectSocket(tgid, fd int) (*sockInfo, error) {
	pidfd, err := unix.PidfdOpen(tgid, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = unix.Close(pidfd) }()
	dup, err := unix.PidfdGetfd(pidfd, fd, 0)
	if err != nil {
		return nil, err
	}
	info := &sockInfo{dupfd: dup}
	domain, err := unix.GetsockoptInt(dup, unix.SOL_SOCKET, unix.SO_DOMAIN)
	if err != nil {
		info.close()
		return nil, err
	}
	typ, err := unix.GetsockoptInt(dup, unix.SOL_SOCKET, unix.SO_TYPE)
	if err != nil {
		info.close()
		return nil, err
	}
	fl, err := unix.FcntlInt(uintptr(dup), unix.F_GETFL, 0)
	if err != nil {
		info.close()
		return nil, err
	}
	info.domain = domain
	info.typ = typ
	info.nonblock = fl&unix.O_NONBLOCK != 0
	if sa, err := unix.Getsockname(dup); err == nil {
		info.bound = sockaddrBound(sa)
	}
	return info, nil
}

// fdCloexec reads the body descriptor's true close-on-exec flag from
// /proc/<tgid>/fdinfo/<fd>, because FD_CLOEXEC is per-descriptor and cannot be
// read from the pidfd_getfd dup. The fdinfo "flags:" line carries the octal open
// flags, with O_CLOEXEC (0o2000000) set when the descriptor is close-on-exec. A
// read failure defaults to close-on-exec, the safer choice for a descriptor the
// body did not ask to leak across an exec.
func fdCloexec(tgid, fd int) bool {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/fdinfo/%d", tgid, fd))
	if err != nil {
		return true
	}
	for _, line := range strings.Split(string(data), "\n") {
		rest, ok := strings.CutPrefix(line, "flags:")
		if !ok {
			continue
		}
		v, err := strconv.ParseInt(strings.TrimSpace(rest), 8, 64)
		if err != nil {
			return true
		}
		return v&0o2000000 != 0
	}
	return true
}

// tgidOf resolves the thread-group leader for a thread id, reading it from
// /proc. seccomp reports the connecting thread's tid; pidfd_open wants the
// leader. A read failure falls back to the id itself (the leader's own connect
// already carries tid == tgid).
func tgidOf(pid int) int {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return pid
	}
	for _, line := range strings.Split(string(data), "\n") {
		rest, ok := strings.CutPrefix(line, "Tgid:")
		if !ok {
			continue
		}
		if v, err := strconv.Atoi(strings.TrimSpace(rest)); err == nil {
			return v
		}
		break
	}
	return pid
}

// sockaddrBound reports whether a socket carries a source address it chose, so
// the emulation can refuse rather than connect from a different local address.
func sockaddrBound(sa unix.Sockaddr) bool {
	switch s := sa.(type) {
	case *unix.SockaddrInet4:
		return s.Port != 0 || s.Addr != [4]byte{}
	case *unix.SockaddrInet6:
		return s.Port != 0 || s.Addr != [16]byte{}
	}
	return false
}

// sockaddrFor builds the connect target for the family the body named.
func sockaddrFor(family int, dest netip.AddrPort) unix.Sockaddr {
	switch family {
	case unix.AF_INET:
		a := dest.Addr().Unmap()
		if !a.Is4() {
			return nil
		}
		return &unix.SockaddrInet4{Port: int(dest.Port()), Addr: a.As4()}
	case unix.AF_INET6:
		return &unix.SockaddrInet6{Port: int(dest.Port()), Addr: dest.Addr().As16()}
	}
	return nil
}

func setNonblock(fd int, nonblock bool) error {
	fl, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil {
		return err
	}
	if nonblock {
		fl |= unix.O_NONBLOCK
	} else {
		fl &^= unix.O_NONBLOCK
	}
	_, err = unix.FcntlInt(uintptr(fd), unix.F_SETFL, fl)
	return err
}

func toErrno(err error, fallback unix.Errno) unix.Errno {
	var e unix.Errno
	if errors.As(err, &e) {
		return e
	}
	return fallback
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
