---
status: accepted
date: 2026-11-13
decision-makers: George Antoniadis
---

# 0220. Code from an outside authority reaches only the hosts it declares, waits for review when it changes, and needs a sandbox that confines it

## Context and Problem Statement

[#1144](https://github.com/geoah/substrate/issues/1144) identifies that a
publisher's authenticated document proves provenance, not safe execution.
The destination gate of [0035](0035-a-network-body-connect-is-filtered-by-destination.md)
and declaration grammar of [0038](0038-a-network-entry-is-a-bare-host-or-cidr-destination.md)
do not bind network access to declared names. This decision extends those
rules for document-published code; binary-shipped code keeps its rules.

## Considered Options

- Trust every authenticated publisher with all public destinations.
- Resolve declared names into an IP allowlist.
- Confine each outside body to a hostname-checking proxy and require review
  when installed code changes.

## Decision Outcome

Choose the proxy. Outside means a package row carries `publishedFrom`.
Functions, agents, record mappings, triggers and OAuth metadata are code.
`SUBSTRATE_OUTSIDE_CODE` defaults to `allow`; `refuse` blocks admission and
execution of all of it. An outside function body, the one form of code the
sandbox runs, also requires a working filesystem sandbox, syscall filter and
connect gate, even on Linux in best-effort mode. An agent, a trigger, a
mapping and an OAuth block run in the server's own process under the
engine's rules (an agent's reach and daily cap, decisions 0215 and 0217),
so a host whose kernel lacks a layer, such as one without Landlock, still
takes them. An explicit `SUBSTRATE_INSECURE_UNCONFINED_OUTSIDE_CODE=true`
waives the bodies' confinement and warns at boot; it never waives `refuse`.

For outside functions, the kernel connect gate admits only the body's own
proxy listener. The proxy checks exact DNS names or wildcard suffixes
before resolving and dialing them. Address literals are refused. Its dials
retain deployment-network denial and the operator's
`SUBSTRATE_SANDBOX_EGRESS_ALLOW` exception. That exception does not widen
host permissions. Outside bodies cannot create datagram sockets, so UDP
cannot bypass connect. An empty network grant creates no internet sockets.

A proxy is necessary because an address allowlist would admit a raw IP and
all other names sharing that address. Clients honor the supplied HTTP proxy
settings or speak CONNECT. CONNECT authorizes a TCP destination; it does
not inspect application data sent to an allowed server or prevent that
server forwarding it elsewhere.

The upgrade offer carries readable before-and-after diffs for code,
including function permissions, shared modules and OAuth metadata. The
console requires review of that evidence. Publisher refresh must treat
code changes as requiring owner review, as the catalog phase specifies.

Every outside OAuth endpoint uses HTTPS. Endpoint hosts appear on approval
and package pages. They need not belong to the publisher: an add-on can
connect to another provider, whose authorization and token endpoints are
necessarily on that provider's domain.

## Consequences

- A publisher cannot silently widen an installed body's network reach.
- Outside clients that ignore proxy settings fail closed. Non-HTTP TCP
  clients need CONNECT support; UDP is unavailable. Outside Python dependencies
  must have wheels: `uv --no-build` prevents downloaded build backends from
  executing with the provisioning process's broader network access.
- A non-Linux developer explicitly opts into unconfined execution.
- Shared modules participate in review because they can change a function
  without changing its own source.
- The managed provenance properties are owned by the catalog work package;
  this implementation carries their install options and transactional stamp.

## Confirmation

`TestOutsideHostPatterns`, `TestOutsideProxyRefusesUndeclaredHostsAndIPs`
and `TestOutsideConnectGateAdmitsOnlyItsProxy` cover hostname and destination
boundaries. `TestOutsideInstalledFunctionCannotReachUndeclaredHost` installs
an outside package and calls its real Python function; it requires Linux.
`TestOutsideRefuseOverridesInsecureException`,
`TestOutsideCodeRejectsUnknownPolicy` and
`TestOutsideCodeDiffIncludesPermissionsAndSource` cover policy and review.
