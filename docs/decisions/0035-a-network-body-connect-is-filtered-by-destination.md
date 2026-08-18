---
status: accepted
date: 2026-08-18
decision-makers: George Antoniadis
---

# 0035. A network-granted body's connect is filtered by destination

## Context and Problem Statement

A function body that declares `permissions.network` gets `AF_INET`/`AF_INET6`
sockets with no restriction on where they connect
([#239](https://github.com/geoah/substrate/issues/239)). The body shares the
substrate's network namespace, so `connect("postgres", 5432)` reaches the
deployment's own Postgres, which on the shipped compose defaults answers as the
`postgres` superuser past every repository's row-level security. `internal/sandbox`
confines a body to ITSELF (Landlock, seccomp, rlimits): the container gives it
no CAP_SYS_ADMIN, and Docker's default seccomp plus Ubuntu's AppArmor deny the
namespace and user-namespace calls, so a per-body network namespace with its own
route table is not available (the same measured constraint recorded in
`sandbox.go`). `permissions.network` has to mean "reach the internet", not
"reach anything the substrate's network happens to route to".

## Considered Options

- A per-body network namespace with a firewall dropping RFC1918 and the DB host.
- A transparent egress proxy the body's traffic is redirected through.
- A classic-BPF seccomp rule that denies `connect` to private addresses.
- A seccomp user-notification (`SECCOMP_RET_USER_NOTIF`) supervisor that reads
  the `connect` destination and refuses the private ones.

## Decision Outcome

Chosen: the seccomp user-notification supervisor, because it is the only option
that enforces a destination policy from an unprivileged process inside a stock
container. A network namespace and a redirecting proxy both need a capability
(CAP_SYS_ADMIN or CAP_NET_ADMIN) the deployment does not grant and this package
refuses to require. A classic-BPF rule cannot express it at all: the destination
sits behind the `sockaddr` pointer, and a BPF filter cannot dereference a
pointer.

The stub installs a second seccomp filter that returns `SECCOMP_RET_USER_NOTIF`
for `connect(2)` and passes the listener descriptor back to the runner (the
body's ancestor) over a unix socket. On each `connect`, the runner reads the
target `sockaddr` from the body's memory (`process_vm_readv`, permitted because
the runner is an ancestor under `yama` ptrace scope 1), and refuses loopback,
link-local (the cloud metadata address `169.254.169.254` among them), RFC1918
and unique-local ranges, carrier-grade NAT (`100.64.0.0/10`, where a tailnet
lives), and the `0.0.0.0/8` block. A resolver named in `/etc/resolv.conf` is
allowed on port 53 only, so name resolution through a loopback stub (Docker's
`127.0.0.11`, systemd-resolved's `127.0.0.53`) keeps working while the body
cannot otherwise reach loopback. The classifier lives in `internal/egress`, so
the server-side dial policy
([#241](https://github.com/geoah/substrate/issues/241)) can reuse it.

An ALLOWED destination is NOT answered with `SECCOMP_USER_NOTIF_FLAG_CONTINUE`.
`CONTINUE` makes the kernel re-read the `sockaddr` when it resumes the syscall,
so a second thread that rewrites the address in that window reaches a blocked
destination (a TOCTOU race, reproduced: 941 connections to a blocked listener in
4000 iterations against the `CONTINUE` version). Instead the runner connects a
fresh socket to the address it verified (a value it holds, not a pointer the body
can race), then installs that connected socket into the body over the descriptor
it called `connect` on with `SECCOMP_IOCTL_NOTIF_ADDFD` and
`SECCOMP_ADDFD_FLAG_SETFD`, and returns success. The kernel never re-reads the
body's memory, so there is no window. The body's own connect timeout is honored:
a non-blocking socket gets the still-connecting descriptor and `EINPROGRESS`, so
the body drives its own poll loop; a blocking socket is waited on, bounded by the
body's own lifetime.

### Consequences

- Good, because the proven path is closed: a body's `connect` to the private
  Postgres address is refused with `EACCES`, while a connect to a public address
  and a TLS handshake still complete.
- Good, because the destination set is stated by the code, not by whatever the
  substrate's network routes to, so a new private service on the network is
  denied by default.
- Good, because `internal/api` never imports the runner, and the classifier is a
  leaf package, so the server can adopt the same policy for #241 without new
  coupling.
- Good, because the TOCTOU race is closed, not narrowed: a multi-threaded body
  cannot flip the address past the check, because the runner connects to the
  value it read and the kernel never re-reads the body's memory.
- Bad, because a socket configuration the runner cannot faithfully reproduce (a
  bound source address, a type other than stream or datagram) is refused with
  `EACCES` rather than emulated. These are exotic for an outbound client; a
  refused connect is safe, a raced one is not.
- Bad, because a UDP body reaching a private service through `sendto`/`sendmsg`
  (no `connect`) is not filtered. Postgres is TCP and requires `connect`, so the
  proven path is unaffected; a UDP residual remains.
- Bad, because a network-granted body costs one supervisor goroutine, one
  unix-socket round trip at startup, and one supervised connect per outbound
  connection.

### Confirmation

`internal/runner/isolation_test.go` gains two cases: a network-granted body is
refused `connect` to a loopback and an RFC1918 address and still reaches an
allowed public destination, and a multi-threaded body flipping a shared
`sockaddr` between an allowed and a blocked address reaches the blocked listener
zero times (the case fails against the `CONTINUE` version). Both run under the
`SUBSTRATE_TEST_REQUIRE_SANDBOX` gate that #209 added, so a build where the
confinement stopped running fails rather than skips. `internal/egress` has unit
tests for the range classifier.

## More Information

Supersedes the residual named in `sandbox.go` that "a body GRANTED network
reaches loopback, so it can talk to the substrate's own HTTP port"; that
sentence is now narrowed to the UDP-`sendto` residual above. Reopen if the
deployment gains a capability that makes a per-body network namespace possible,
which would replace this with a route-table boundary and drop the per-connect
emulation.
