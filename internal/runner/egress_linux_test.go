//go:build linux

package runner

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// What a network body may open and how it may reach a peer, asserted against
// real bodies under the production policy. Every case listens on loopback and
// nothing here reaches the internet.

// listenLoopback starts a TCP listener on host and returns it with its port.
func listenLoopback(t *testing.T, host string) (*net.TCPListener, string) {
	t.Helper()
	ln, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	return ln.(*net.TCPListener), port
}

// drainAccepts counts the connections waiting on ln. A connection the body
// opened during its invocation is in the backlog by the time it returns, so
// accepting until a short deadline counts every one without a race.
func drainAccepts(t *testing.T, ln *net.TCPListener) int {
	t.Helper()
	if err := ln.SetDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	n := 0
	for {
		c, err := ln.Accept()
		if err != nil {
			return n
		}
		n++
		_ = c.Close()
	}
}

// TCP Fast Open opens a connection from a send call, with the destination in
// the send's own arguments, so the connect gate never sees it. A network body
// must not reach a refused address that way: every send carrying
// MSG_FASTOPEN is refused, and TCP_FASTOPEN_CONNECT, which defers the
// handshake to the first write after connect(2), still meets the gate at the
// connect.
func TestNetworkBodyCannotConnectWithTCPFastOpen(t *testing.T) {
	r := New()
	requireSeccomp(t, r)
	requireConnectGate(t, r)
	ln, port := listenLoopback(t, "127.0.0.1")

	const probe = `
import ctypes, os, socket, struct
MSG_FASTOPEN = 0x20000000
TCP_FASTOPEN_CONNECT = 30
libc = ctypes.CDLL(None, use_errno=True)

class iovec(ctypes.Structure):
    _fields_ = [("base", ctypes.c_void_p), ("len", ctypes.c_size_t)]

class msghdr(ctypes.Structure):
    _fields_ = [("name", ctypes.c_void_p), ("namelen", ctypes.c_uint32),
                ("iov", ctypes.POINTER(iovec)), ("iovlen", ctypes.c_size_t),
                ("control", ctypes.c_void_p), ("controllen", ctypes.c_size_t),
                ("flags", ctypes.c_int)]

class mmsghdr(ctypes.Structure):
    _fields_ = [("hdr", msghdr), ("len", ctypes.c_uint)]

def result(fn):
    try:
        fn()
        return "sent"
    except OSError as e:
        return "err:%d" % e.errno

def main(input, host):
    port = int(input["args"]["port"])
    dest = ("127.0.0.1", port)
    def sendto():
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
            s.sendto(b"x", MSG_FASTOPEN, dest)
    def sendmsg():
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
            s.sendmsg([b"x"], [], MSG_FASTOPEN, dest)
    def sendmmsg():
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
            sa = struct.pack("=H", socket.AF_INET) + struct.pack("!H", port) + socket.inet_aton("127.0.0.1") + b"\x00" * 8
            name = ctypes.create_string_buffer(sa, 16)
            data = ctypes.create_string_buffer(b"x", 1)
            iov = iovec(ctypes.cast(data, ctypes.c_void_p), 1)
            msg = mmsghdr(msghdr(ctypes.cast(name, ctypes.c_void_p), 16, ctypes.pointer(iov), 1, None, 0, 0), 0)
            if libc.sendmmsg(s.fileno(), ctypes.byref(msg), 1, MSG_FASTOPEN) < 0:
                e = ctypes.get_errno()
                raise OSError(e, os.strerror(e))
    def fastopen_connect():
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
            s.setsockopt(socket.IPPROTO_TCP, TCP_FASTOPEN_CONNECT, 1)
            s.connect(dest)
            s.send(b"x")
    return {"output": {
        "sendto": result(sendto),
        "sendmsg": result(sendmsg),
        "sendmmsg": result(sendmmsg),
        "fastopen_connect": result(fastopen_connect),
    }}
`
	spec := Spec{
		Repository: "t1", Function: "fastopen.g.test", Runtime: "python",
		Source: probe, TimeoutMs: 20000, Network: []string{"api.example.com"},
	}
	in := testInput()
	in.Args = map[string]any{"port": port}
	res, err := r.Invoke(context.Background(), spec, in, nil)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	out, ok := res.Output.(map[string]any)
	if !ok {
		t.Fatalf("unexpected output: %#v", res.Output)
	}
	// The listener is the ground truth: a refused address accepts nothing.
	if n := drainAccepts(t, ln); n != 0 {
		t.Errorf("a network body opened %d connection(s) to a refused loopback address: %v", n, out)
	}
	for _, call := range []string{"sendto", "sendmsg", "sendmmsg", "fastopen_connect"} {
		if got := out[call]; got != "err:13" {
			t.Errorf("%s to a refused address = %v, want err:13 (EACCES)", call, got)
		}
	}
}

// A network body's internet sockets are TCP streams and datagram sockets.
// SCTP opens an association from sendmsg(2) and from a socket option, MPTCP
// is a different protocol from the TCP the gate emulates, and a raw socket
// writes its own headers, so none of them is held to the destination the gate
// checks.
func TestNetworkBodyGetsOnlyTCPStreamsAndDatagramSockets(t *testing.T) {
	r := New()
	requireSeccomp(t, r)
	requireConnectGate(t, r)

	const probe = `
import socket
def main(input, host):
    out = {}
    for name, fam, typ, proto in [
            ("tcp", socket.AF_INET, socket.SOCK_STREAM, 0),
            ("tcp_explicit", socket.AF_INET, socket.SOCK_STREAM, socket.IPPROTO_TCP),
            ("tcp6", socket.AF_INET6, socket.SOCK_STREAM, 0),
            ("udp", socket.AF_INET, socket.SOCK_DGRAM, 0),
            ("udp6", socket.AF_INET6, socket.SOCK_DGRAM, socket.IPPROTO_UDP),
            ("sctp_stream", socket.AF_INET, socket.SOCK_STREAM, 132),
            ("sctp6_stream", socket.AF_INET6, socket.SOCK_STREAM, 132),
            ("sctp_seqpacket", socket.AF_INET, socket.SOCK_SEQPACKET, 0),
            ("mptcp", socket.AF_INET, socket.SOCK_STREAM, 262),
            ("inet_raw", socket.AF_INET, socket.SOCK_RAW, socket.IPPROTO_TCP)]:
        try:
            socket.socket(fam, typ, proto).close()
            out[name] = "opened"
        except OSError as e:
            out[name] = "err:%d" % e.errno
    return {"output": out}
`
	spec := Spec{
		Repository: "t1", Function: "protocols.g.test", Runtime: "python",
		Source: probe, TimeoutMs: 5000, Network: []string{"api.example.com"},
	}
	res, err := r.Invoke(context.Background(), spec, testInput(), nil)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	out, ok := res.Output.(map[string]any)
	if !ok {
		t.Fatalf("unexpected output: %#v", res.Output)
	}
	for _, name := range []string{"tcp", "tcp_explicit", "tcp6", "udp", "udp6"} {
		if out[name] != "opened" {
			t.Errorf("a network body was refused a %s socket: %v", name, out[name])
		}
	}
	for _, name := range []string{"sctp_stream", "sctp6_stream", "sctp_seqpacket", "mptcp", "inet_raw"} {
		if out[name] == "opened" {
			t.Errorf("a network body opened a %s socket", name)
		}
	}
	// Where this kernel offers SCTP, the refusal above is the filter's: log
	// it, so a run on a kernel without SCTP is not read as proof.
	if fd, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM, unix.IPPROTO_SCTP); err == nil {
		_ = unix.Close(fd)
		if out["sctp_stream"] != "err:13" {
			t.Errorf("this kernel offers SCTP, and the body's SCTP socket answered %v, want err:13 (EACCES)", out["sctp_stream"])
		}
	} else {
		t.Logf("this kernel offers no SCTP (%v): the SCTP refusals prove nothing here", err)
	}
}

// The gate decides on the address family it reads from the body's memory, and
// a second thread can change that memory while the gate decides. A body that
// flips the family between AF_UNIX and AF_INET under a connect to a refused
// loopback listener must never reach it: every connect the gate lets complete
// is made by the supervisor, to the address it read.
func TestNetworkBodyCannotRaceTheAddressFamily(t *testing.T) {
	r := New()
	requireSeccomp(t, r)
	requireConnectGate(t, r)

	var accepts atomic.Int64
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			accepts.Add(1)
			_ = c.Close()
		}
	}()
	_, blockedPort, _ := net.SplitHostPort(ln.Addr().String())

	const probe = `
import ctypes, socket, struct, threading
libc = ctypes.CDLL(None, use_errno=True)
def main(input, host):
    bp = int(input["args"]["blockedPort"])
    inet = struct.pack("=H", socket.AF_INET) + struct.pack("!H", bp) + socket.inet_aton("127.0.0.1") + b"\x00" * 8
    unix_family = struct.pack("=H", socket.AF_UNIX)
    inet_family = struct.pack("=H", socket.AF_INET)
    buf = ctypes.create_string_buffer(inet, 16)
    stop = threading.Event()
    def flip():
        while not stop.is_set():
            ctypes.memmove(buf, unix_family, 2)
            ctypes.memmove(buf, inet_family, 2)
    t = threading.Thread(target=flip, daemon=True); t.start()
    hits, iters = 0, 4000
    for _ in range(iters):
        s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        s.setblocking(False)
        try:
            libc.connect(s.fileno(), buf, 16)
            try:
                peer = s.getpeername()
                if peer[0] == "127.0.0.1" and peer[1] == bp:
                    hits += 1
            except OSError:
                pass
        finally:
            s.close()
    stop.set(); t.join(timeout=1)
    return {"output": {"hits": hits, "iters": iters}}
`
	spec := Spec{
		Repository: "t1", Function: "familyrace.g.test", Runtime: "python",
		Source: probe, TimeoutMs: 60000, Network: []string{"api.example.com"},
	}
	in := testInput()
	in.Args = map[string]any{"blockedPort": blockedPort}
	res, err := r.Invoke(context.Background(), spec, in, nil)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	out, _ := res.Output.(map[string]any)
	// Late SYNs from the last iterations land after the body returns.
	time.Sleep(200 * time.Millisecond)
	if got := accepts.Load(); got != 0 {
		t.Fatalf("a connect whose family was raced reached the refused loopback listener %d time(s) in %v", got, out["iters"])
	}
	if hits, _ := out["hits"].(float64); hits != 0 {
		t.Fatalf("a body socket reached the refused address %v times", hits)
	}
}

// The gate makes every connect it allows itself, so what a body legitimately
// reaches has to keep working: a TCP service on an address the operator
// allowed, blocking and not, a resolver named in /etc/resolv.conf on port 53,
// and the AF_UNSPEC connect that dissolves a datagram socket's peer, which
// glibc's getaddrinfo makes while it orders the addresses it returns.
func TestNetworkBodyStillReachesAllowedAddressesAndItsResolver(t *testing.T) {
	r := New()
	requireSeccomp(t, r)
	requireConnectGate(t, r)
	ln, port := listenLoopback(t, allowedLoopback)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				buf := make([]byte, 4)
				if _, err := c.Read(buf); err == nil {
					_, _ = c.Write(buf)
				}
			}()
		}
	}()
	var resolvers []any
	if raw, err := os.ReadFile("/etc/resolv.conf"); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			if f := strings.Fields(line); len(f) >= 2 && f[0] == "nameserver" && net.ParseIP(f[1]) != nil {
				resolvers = append(resolvers, f[1])
			}
		}
	}

	const probe = `
import ctypes, select, socket
libc = ctypes.CDLL(None, use_errno=True)
def echo(s):
    s.sendall(b"ping")
    got = b""
    while len(got) < 4:
        chunk = s.recv(4 - len(got))
        if not chunk:
            break
        got += chunk
    return got.decode()
def main(input, host):
    a = input["args"]
    dest = (a["host"], int(a["port"]))
    out = {}
    try:
        with socket.create_connection(dest, timeout=5) as s:
            out["blocking"] = echo(s)
    except OSError as e:
        out["blocking"] = "err:%d" % e.errno
    try:
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
            s.setblocking(False)
            try:
                s.connect(dest)
            except BlockingIOError:
                select.select([], [s], [], 5)
            err = s.getsockopt(socket.SOL_SOCKET, socket.SO_ERROR)
            if err:
                raise OSError(err, "connect")
            s.setblocking(True)
            out["nonblocking"] = echo(s)
    except OSError as e:
        out["nonblocking"] = "err:%d" % e.errno
    resolvers = {}
    for ns in a["resolvers"]:
        fam = socket.AF_INET6 if ":" in ns else socket.AF_INET
        try:
            with socket.socket(fam, socket.SOCK_DGRAM) as s:
                s.connect((ns, 53))
            resolvers[ns] = "ok"
        except OSError as e:
            resolvers[ns] = "err:%d" % e.errno
    out["resolvers"] = resolvers
    try:
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as s:
            s.connect(dest)
            unspec = ctypes.create_string_buffer(16)
            if libc.connect(s.fileno(), unspec, 16) != 0:
                raise OSError(ctypes.get_errno(), "disconnect")
            try:
                s.getpeername()
                out["disconnect"] = "still connected"
            except OSError as e:
                out["disconnect"] = "err:%d" % e.errno
    except OSError as e:
        out["disconnect"] = "failed:%d" % e.errno
    return {"output": out}
`
	spec := Spec{
		Repository: "t1", Function: "allowed.g.test", Runtime: "python",
		Source: probe, TimeoutMs: 20000, Network: []string{"api.example.com"},
	}
	in := testInput()
	in.Args = map[string]any{"host": allowedLoopback, "port": port, "resolvers": resolvers}
	res, err := r.Invoke(context.Background(), spec, in, nil)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	out, ok := res.Output.(map[string]any)
	if !ok {
		t.Fatalf("unexpected output: %#v", res.Output)
	}
	for _, mode := range []string{"blocking", "nonblocking"} {
		if out[mode] != "ping" {
			t.Errorf("a %s connect to an allowed loopback address = %v, want the echo", mode, out[mode])
		}
	}
	// ENOTCONN: the peer is gone.
	if out["disconnect"] != "err:107" {
		t.Errorf("an AF_UNSPEC connect on a connected datagram socket = %v, want err:107 (ENOTCONN) after it", out["disconnect"])
	}
	got, _ := out["resolvers"].(map[string]any)
	for _, ns := range resolvers {
		if got[ns.(string)] != "ok" {
			t.Errorf("a UDP connect to the resolver %s on port 53 = %v, want ok", ns, got[ns.(string)])
		}
	}
	if len(resolvers) == 0 {
		t.Log("/etc/resolv.conf names no nameserver: the resolver half checked nothing")
	}
}

// A network body connects no unix socket. The supervisor would have to make
// the connect for it, and a connect it made would carry its own credentials to
// the listener and share the body's socket flags and send timeout with every
// thread of the body. So every unix connect a network body makes is refused
// with EACCES: by path, absolute or relative, stream or datagram, blocking or
// not, and by abstract name. The listeners outside the body are the ground
// truth, and one that accepts names the peer's pid, which would be this test
// process's: the supervisor runs in it. A socketpair, which asyncio's event
// loop opens, still works, and glibc's getaddrinfo, which tries nscd's socket
// first, still resolves.
func TestNetworkBodyCannotConnectAUnixSocket(t *testing.T) {
	r := New()
	requireSeccomp(t, r)
	requireConnectGate(t, r)

	// sockaddr_un holds 108 bytes, so the directory is a short one.
	dir, err := os.MkdirTemp("", "ug")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	streamPath := filepath.Join(dir, "stream")
	stream, err := net.ListenUnix("unix", &net.UnixAddr{Name: streamPath, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	abstractName := fmt.Sprintf("substrate-gate-test-%d", os.Getpid())
	abstract, err := net.ListenUnix("unix", &net.UnixAddr{Name: "@" + abstractName, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer abstract.Close()
	datagramPath := filepath.Join(dir, "datagram")
	datagram, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: datagramPath, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer datagram.Close()

	const probe = `
import os, shutil, socket, tempfile
def connect(typ, address, blocking=True):
    try:
        with socket.socket(socket.AF_UNIX, typ) as c:
            c.setblocking(blocking)
            c.connect(address)
        return "connected"
    except OSError as e:
        return "err:%d" % e.errno
def main(input, host):
    a = input["args"]
    out = {
        "absolute": connect(socket.SOCK_STREAM, a["stream"]),
        "nonblocking": connect(socket.SOCK_STREAM, a["stream"], blocking=False),
        "datagram": connect(socket.SOCK_DGRAM, a["datagram"]),
        "abstract": connect(socket.SOCK_STREAM, "\0" + a["abstract"]),
    }
    tmp = tempfile.mkdtemp(dir=os.environ["TMPDIR"])
    try:
        with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as srv:
            srv.bind(os.path.join(tmp, "own"))
            srv.listen(4)
            out["own"] = connect(socket.SOCK_STREAM, os.path.join(tmp, "own"))
            os.chdir(tmp)
            out["relative"] = connect(socket.SOCK_STREAM, "own")
    finally:
        os.chdir(os.environ["TMPDIR"])
        shutil.rmtree(tmp, ignore_errors=True)
    left, right = socket.socketpair()
    with left, right:
        left.sendall(b"ping")
        out["socketpair"] = right.recv(4).decode()
    try:
        out["getaddrinfo"] = socket.getaddrinfo("localhost", 443, socket.AF_INET, socket.SOCK_STREAM)[0][4][0]
    except OSError as e:
        out["getaddrinfo"] = "err:%s" % e
    return {"output": out}
`
	spec := Spec{
		Repository: "t1", Function: "unixsockets.g.test", Runtime: "python",
		Source: probe, TimeoutMs: 20000, Network: []string{"api.example.com"},
	}
	in := testInput()
	in.Args = map[string]any{"stream": streamPath, "datagram": datagramPath, "abstract": abstractName}
	res, err := r.Invoke(context.Background(), spec, in, nil)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	out, ok := res.Output.(map[string]any)
	if !ok {
		t.Fatalf("unexpected output: %#v", res.Output)
	}
	for name, ln := range map[string]*net.UnixListener{"pathname": stream, "abstract": abstract} {
		if err := ln.SetDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
			t.Fatal(err)
		}
		for {
			c, err := ln.AcceptUnix()
			if err != nil {
				break
			}
			pid := int32(-1)
			if raw, err := c.SyscallConn(); err == nil {
				_ = raw.Control(func(fd uintptr) {
					if cred, err := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED); err == nil {
						pid = cred.Pid
					}
				})
			}
			_ = c.Close()
			t.Errorf("the %s listener accepted a connection from pid %d (the supervisor's is %d)", name, pid, os.Getpid())
		}
	}
	for _, conn := range []string{"absolute", "nonblocking", "datagram", "abstract", "own", "relative"} {
		if out[conn] != "err:13" {
			t.Errorf("a network body's %s unix connect = %v, want err:13 (EACCES)", conn, out[conn])
		}
	}
	if out["socketpair"] != "ping" {
		t.Errorf("a network body's socketpair = %v, want the echo", out["socketpair"])
	}
	if out["getaddrinfo"] != "127.0.0.1" {
		t.Errorf("getaddrinfo(localhost) in a network body = %v, want 127.0.0.1", out["getaddrinfo"])
	}
}

// A body's socket and the supervisor's duplicate of it share one set of flags
// and options, so a supervisor that connected the body's own unix socket could
// be made to wait: a second thread clears O_NONBLOCK or the send timeout
// between the supervisor's check and its connect, and the connect then waits
// on a full listener queue with no deadline, holding the gate and every
// teardown behind it. A body that does exactly that, while it connects the
// socket by path and dissolves it with AF_UNSPEC in a loop, gets an answer to
// every call at once, and its process tears down.
func TestAUnixConnectRacingItsSocketFlagsCannotHoldTheGate(t *testing.T) {
	r := New()
	requireSeccomp(t, r)
	requireConnectGate(t, r)

	const probe = `
import ctypes, fcntl, os, shutil, socket, struct, tempfile, threading, time
libc = ctypes.CDLL(None, use_errno=True)
def main(input, host):
    tmp = tempfile.mkdtemp(dir=os.environ["TMPDIR"])
    path = os.path.join(tmp, "q")
    try:
        with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as srv:
            srv.bind(path)
            srv.listen(0)
            # Where unix connects go through, fill the listener's queue, so a
            # blocking connect to it waits.
            fillers = []
            while True:
                c = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
                c.setblocking(False)
                try:
                    c.connect(path)
                except OSError:
                    c.close()
                    break
                fillers.append(c)
            c = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            fd = c.fileno()
            flags = fcntl.fcntl(fd, fcntl.F_GETFL)
            no_timeout = struct.pack("ll", 0, 0)
            stop = threading.Event()
            def mutate():
                while not stop.is_set():
                    fcntl.fcntl(fd, fcntl.F_SETFL, flags | os.O_NONBLOCK)
                    c.setsockopt(socket.SOL_SOCKET, socket.SO_SNDTIMEO, no_timeout)
                    fcntl.fcntl(fd, fcntl.F_SETFL, flags & ~os.O_NONBLOCK)
                    c.setsockopt(socket.SOL_SOCKET, socket.SO_SNDTIMEO, no_timeout)
            mutator = threading.Thread(target=mutate, daemon=True)
            mutator.start()
            sun = struct.pack("=H", socket.AF_UNIX) + path.encode() + b"\0"
            targets = [(ctypes.create_string_buffer(sun, len(sun)), len(sun)),
                       (ctypes.create_string_buffer(16), 16)]
            answers, slowest, end = {}, 0.0, time.monotonic() + 1.0
            while time.monotonic() < end:
                for target, size in targets:
                    start = time.monotonic()
                    rc = libc.connect(fd, target, size)
                    slowest = max(slowest, time.monotonic() - start)
                    key = "ok" if rc == 0 else "err:%d" % ctypes.get_errno()
                    answers[key] = answers.get(key, 0) + 1
            stop.set()
            mutator.join(timeout=1)
            c.close()
            return {"output": {"answers": answers, "queued": len(fillers), "slowest": slowest}}
    finally:
        shutil.rmtree(tmp, ignore_errors=True)
`
	spec := Spec{
		Repository: "t1", Function: "unixrace.g.test", Runtime: "python",
		Source: probe, TimeoutMs: 5000, Network: []string{"api.example.com"},
	}
	type result struct {
		res *Result
		err error
	}
	invoked := make(chan result, 1)
	go func() {
		res, err := r.Invoke(context.Background(), spec, testInput(), nil)
		invoked <- result{res, err}
	}()
	var got result
	select {
	case got = <-invoked:
	case <-time.After(20 * time.Second):
		t.Fatal("the invocation did not end within 20s: a unix connect held the connect gate")
	}
	if got.err != nil {
		t.Fatalf("invoke: %v", got.err)
	}
	out, ok := got.res.Output.(map[string]any)
	if !ok {
		t.Fatalf("unexpected output: %#v", got.res.Output)
	}
	// EACCES is the refused unix connect, EINVAL the kernel's answer to
	// AF_UNSPEC on a unix stream socket.
	answers, _ := out["answers"].(map[string]any)
	for answer := range answers {
		if answer != "err:13" && answer != "err:22" {
			t.Errorf("a racing connect answered %s: %v", answer, answers)
		}
	}
	if answers["err:13"] == nil {
		t.Errorf("no unix connect was refused: %v", answers)
	}
	if slowest, _ := out["slowest"].(float64); slowest > 1 {
		t.Errorf("the slowest connect took %.2fs, want an answer at once", slowest)
	}

	torn := make(chan struct{})
	go func() {
		r.Reconcile(context.Background(), spec.Repository, nil)
		close(torn)
	}()
	select {
	case <-torn:
	case <-time.After(10 * time.Second):
		t.Fatal("the body's process did not tear down within 10s")
	}
}
