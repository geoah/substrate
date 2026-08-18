---
status: accepted
date: 2026-08-18
decision-makers: George Antoniadis
---

# 0038. A `permissions.network` entry is a bare host or CIDR destination

## Context and Problem Statement

A function's `permissions.network` is the egress allowlist, but an entry had no
declared grammar and nothing validated it at load
([#50](https://github.com/geoah/substrate/issues/50)): the loader admitted any
non-empty string, so `https://*`, `beeper.com/*` and a typo all loaded and only
misled a reader or silently did nothing. The dialect key set freezes at v1, so a
narrowing on the entry is safe to add now and refused after. The grammar has to
describe the same kind of destination the egress confinement acts on
([0035](0035-a-network-body-connect-is-filtered-by-destination.md)): a host or
IP, not a URL.

## Considered Options

- A URL pattern with wildcards (`https://*.example.com/v1`), matched per host.
- A bare destination: a host, a `host:port` or a CIDR, no scheme, path or glob.
- No grammar: keep the entry a free string, documentation only.

## Decision Outcome

Chosen: a bare destination (a host, a `host:port` or a CIDR), because that is
what the confinement can act on. The confinement filters a body's `connect(2)`
by resolved DESTINATION address through `internal/egress`, which has no scheme,
path or per-host pattern: it allows any public address and blocks the private
ranges. A URL grammar would promise per-host and per-path matching the runtime
does not do, and a wildcard would promise a match nothing evaluates. So an entry
carries no scheme, no path, no query and no glob, and its host is a DNS
hostname, an IP literal or a CIDR. `networkEntryProblem` in
`internal/vocabulary/function.go` holds each entry to this and refuses a
malformed one, naming it.

The grammar validates SHAPE, not reachability. Whether a declared destination
can be reached is the runtime confinement's decision, and it depends on the
deployment: `SUBSTRATE_SANDBOX_EGRESS_ALLOW` lets an operator permit a private
destination (a loopback provider, [#241](https://github.com/geoah/substrate/issues/241)).
So a private or loopback address (`127.0.0.1:11434`, `10.0.0.0/8`) is a
well-formed entry accepted at load, and the confinement decides at connect
whether it answers. The loader cannot make that call, because it does not know
the deployment's escape list, and a hostname's address is not known until it
resolves.

Enforcement stays all-or-nothing today: a non-empty list grants the body sockets
and confines every connect by destination, while the per-entry host is
documentation until a per-host gate exists. The grammar is what a per-host gate
will read.

### Consequences

- Good, because a malformed entry fails at load with a message naming it,
  instead of misleading a reader or doing nothing at runtime.
- Good, because the grammar describes the destination shape the confinement
  acts on, so a per-host gate has a validated shape to read.
- Bad, because the loader accepts an entry the confinement then refuses at
  connect (a private address on a deployment with no escape configured): shape
  is checked at load, reachability only at connect, so the two are not one
  error.
- Bad, because the one shipped entry that used a glob, beeper's `beeper.com/*`,
  had to change to the bare host `beeper.com` (beeper bundle version 5 -> 6).
- Bad, because a future need for a path or a scheme in an entry is now a second
  dialect change, not a free string the runtime could start reading.

### Confirmation

`TestNetworkEntryGrammar` in `internal/vocabulary/function_test.go` covers the
admitted forms (host, `host:port`, rooted DNS name, IP, CIDR, bare and bracketed
IPv6, and a private address) and the refused ones (URL, path, glob,
out-of-range port, a raw IDN, and a non-host string). `mise run kinds:check`
holds the shipped bundles to the grammar, since a bundle that violated it would
fail to load.

## More Information

Builds on [0035](0035-a-network-body-connect-is-filtered-by-destination.md),
whose confinement acts on the same destination shape. Reopen when a per-host
egress gate lands: the grammar is already the shape it needs, but a scheme or
path would then have a runtime that reads it and could be admitted.
