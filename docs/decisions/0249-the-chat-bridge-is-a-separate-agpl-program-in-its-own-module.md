---
status: proposed
date: 2026-11-09
decision-makers: George Antoniadis
---

# 0249. The chat bridge is a separate AGPL program in its own module

## Context and Problem Statement

Connecting chat networks as the owner means running the mautrix bridgev2
network connectors ([the chat bridge plan](../plans/chat-bridge.md),
sections 2 and 5). Every one of them is AGPL-3.0, whatsmeow links the
GPL-3.0 `go.mau.fi/libsignal`, and Signal's connector links Signal's AGPL
`libsignal` through cgo. The server's own license is undecided (plan,
section 14), so it must not link any of that. Without a rule, the first
convenient import of a server package into the bridge, or of a connector into
the server, would decide the server's license by accident.

## Considered Options

- A separate program in this repository: `cmd/chatbridge` with its own
  `go.mod`, `LICENSE` and `NOTICE`, importing no package of the server's
  module and talking to it over the public HTTP API.
- A separate program in a repository of its own.
- The connectors linked into the server.

## Decision Outcome

Chosen: a separate AGPL-3.0 program in this repository, in its own Go
module, because it keeps the server's license open while the bridge's
contract (plan, section 6) is built beside the package that reads it and
changes with it in one pull request. The bridge imports nothing of
`github.com/geoah/substrate`, and the server's module never imports the
bridge; the bridge reaches the server only as an app does, through the
authorization flow, the webhook door, the change feed and record reads, so
the two are separate programs under the FSF's aggregate reading. A
repository of its own was the cleaner separation and would split one
contract across two histories. Linking the connectors into the server would
make the server AGPL-3.0, which is the owner's choice to make and not a
side effect of a provider.

### Consequences

- Good, because the server's license stays undecided, and the bridge is
  built, tested and released from the same commits as the package it
  delivers to.
- Good, because the boundary is the public API, so a bridge in another
  language (the on-device Mac bridge of plan section 10) speaks the same
  contract.
- Good, because each network joins the same module and binary, sharing one
  bridgev2 version.
- Bad, because the root's `./...` never reaches a nested module: `test`,
  `lint:go` and `audit:go` miss it, so it carries its own tasks, lint
  configuration and CI job, and they can drift from the root's.
- Bad, because Go's `internal` rule would let the nested module import the
  server's packages by path; only a lint rule and a dependency check stop it.
- Bad, because the program links C (SQLite and libwebp now, `libsignal`
  with Signal), so its build cannot cross-compile like the server's.
- Bad, because a published image of it is an AGPL distribution: its
  `LICENSE`, its `NOTICE` and the source of the exact build must ship with
  it.

### Confirmation

`mise run chatbridge:lint` fails on any package of
`github.com/geoah/substrate` outside `cmd/chatbridge` in the binary's
closure, on a depguard rule in `cmd/chatbridge/.golangci.yml`, and on a
`NOTICE` that no longer matches what the linux build links; the `chatbridge`
CI job runs it. The server's module cannot import the bridge without
requiring a module it does not list. The Dockerfile's `chatbridge` stage
copies `LICENSE` and `NOTICE` into the image and labels the commit.

## More Information

The plan's D1 and section 12. Reopen when the server's own license is
chosen: an AGPL server could take the connectors in, and a permissive one
makes this boundary permanent.
