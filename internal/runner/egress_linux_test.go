//go:build linux

package runner

import (
	"context"
	"net"
	"os"
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

// A network body's unix sockets are connected by the supervisor too, on the
// body's own socket, so a pathname listener in the body's directory stays
// reachable, by an absolute path and by one relative to its working
// directory, as a stream and as a datagram socket. An abstract address is
// refused: Landlock confines a body's abstract connects to its own domain, and
// a connect the supervisor makes would be outside it.
func TestNetworkBodyKeepsItsUnixSockets(t *testing.T) {
	r := New()
	requireSeccomp(t, r)
	requireConnectGate(t, r)

	const probe = `
import os, shutil, socket, tempfile
def roundtrip(srv, client):
    a, _ = srv.accept()
    with a:
        client.sendall(b"ping")
        return a.recv(4).decode()
def main(input, host):
    # The work dir outlives the process, so each run binds in a fresh one.
    tmp = tempfile.mkdtemp(dir=os.environ["TMPDIR"])
    try:
        return {"output": probe(tmp)}
    finally:
        os.chdir(os.environ["TMPDIR"])
        shutil.rmtree(tmp, ignore_errors=True)
def probe(tmp):
    out = {}
    path = os.path.join(tmp, "s1")
    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as srv:
        srv.bind(path)
        srv.listen(4)
        try:
            with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as c:
                c.connect(path)
                out["absolute"] = roundtrip(srv, c)
        except OSError as e:
            out["absolute"] = "err:%d" % e.errno
        try:
            with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as c:
                c.setblocking(False)
                c.connect(path)
                c.setblocking(True)
                out["nonblocking"] = roundtrip(srv, c)
        except OSError as e:
            out["nonblocking"] = "err:%d" % e.errno
        os.chdir(tmp)
        try:
            with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as c:
                c.connect("s1")
                out["relative"] = roundtrip(srv, c)
        except OSError as e:
            out["relative"] = "err:%d" % e.errno
    dpath = os.path.join(tmp, "d1")
    with socket.socket(socket.AF_UNIX, socket.SOCK_DGRAM) as srv:
        srv.bind(dpath)
        try:
            with socket.socket(socket.AF_UNIX, socket.SOCK_DGRAM) as c:
                c.connect(dpath)
                c.send(b"ping")
                out["datagram"] = srv.recv(4).decode()
        except OSError as e:
            out["datagram"] = "err:%d" % e.errno
    try:
        with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as c:
            c.connect(os.path.join(tmp, "missing"))
        out["missing"] = "connected"
    except OSError as e:
        out["missing"] = "err:%d" % e.errno
    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as srv:
        name = "\0substrate-gate-test-%d" % os.getpid()
        srv.bind(name)
        srv.listen(1)
        try:
            with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as c:
                c.connect(name)
            out["abstract"] = "connected"
        except OSError as e:
            out["abstract"] = "err:%d" % e.errno
    return out
`
	spec := Spec{
		Repository: "t1", Function: "unixsockets.g.test", Runtime: "python",
		Source: probe, TimeoutMs: 20000, Network: []string{"api.example.com"},
	}
	res, err := r.Invoke(context.Background(), spec, testInput(), nil)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	out, ok := res.Output.(map[string]any)
	if !ok {
		t.Fatalf("unexpected output: %#v", res.Output)
	}
	for _, conn := range []string{"absolute", "nonblocking", "relative", "datagram"} {
		if out[conn] != "ping" {
			t.Errorf("a network body's %s unix connect = %v, want the echo", conn, out[conn])
		}
	}
	// The kernel's own answer for a path with nothing behind it.
	if out["missing"] != "err:2" {
		t.Errorf("a unix connect to a missing path = %v, want err:2 (ENOENT)", out["missing"])
	}
	if out["abstract"] != "err:13" {
		t.Errorf("a network body's abstract unix connect = %v, want err:13 (EACCES)", out["abstract"])
	}
}

// A blocking unix connect waits while the listener's queue is full, and the
// runner makes that connect on the body's behalf. It must still end where the
// kernel's would: at the body's own send timeout, with EAGAIN. And a body that
// waits with no timeout must not hold the gate: a timeout's kill closes the
// gate before it signals the body, so a runner stuck in the connect would
// never tear the body down.
func TestABlockedUnixConnectDoesNotHoldTheConnectGate(t *testing.T) {
	r := New()
	requireSeccomp(t, r)
	requireConnectGate(t, r)

	const probe = `
import os, shutil, socket, struct, tempfile, time
def main(input, host):
    tmp = tempfile.mkdtemp(dir=os.environ["TMPDIR"])
    path = os.path.join(tmp, "q")
    try:
        with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as srv:
            srv.bind(path)
            srv.listen(0)
            fillers = []
            while True:
                c = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
                c.setblocking(False)
                try:
                    c.connect(path)
                except BlockingIOError:
                    c.close()
                    break
                fillers.append(c)
            with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as c:
                if not input["args"]["forever"]:
                    c.setsockopt(socket.SOL_SOCKET, socket.SO_SNDTIMEO, struct.pack("ll", 1, 0))
                start = time.monotonic()
                try:
                    c.connect(path)
                    result = "connected"
                except OSError as e:
                    result = "err:%d" % e.errno
                return {"output": {"result": result, "queued": len(fillers), "waited": time.monotonic() - start}}
    finally:
        shutil.rmtree(tmp, ignore_errors=True)
`
	spec := Spec{
		Repository: "t1", Function: "unixqueue.g.test", Runtime: "python",
		Source: probe, TimeoutMs: 3000, Network: []string{"api.example.com"},
	}
	in := testInput()
	in.Args = map[string]any{"forever": false}
	res, err := r.Invoke(context.Background(), spec, in, nil)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	out, _ := res.Output.(map[string]any)
	if out["queued"] == float64(0) {
		t.Fatalf("the listener's queue never filled: %v", out)
	}
	if out["result"] != "err:11" {
		t.Errorf("a blocking unix connect to a full queue with a 1s send timeout = %v, want err:11 (EAGAIN)", out["result"])
	}
	if waited, _ := out["waited"].(float64); waited < 0.9 || waited > 2.5 {
		t.Errorf("the connect gave up after %.2fs, want about the body's own 1s", waited)
	}

	in.Args = map[string]any{"forever": true}
	invoked := make(chan error, 1)
	go func() {
		_, err := r.Invoke(context.Background(), spec, in, nil)
		invoked <- err
	}()
	select {
	case err := <-invoked:
		if err == nil || !strings.Contains(err.Error(), "exceeded") {
			t.Fatalf("invocation = %v, want its own timeout", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the invocation did not end within 15s: a blocked unix connect held the connect gate")
	}
}
