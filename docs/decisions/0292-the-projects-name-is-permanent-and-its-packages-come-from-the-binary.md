---
status: accepted
date: 2026-11-14
decision-makers: George Antoniadis
---

# 0292. The project's name is permanent, and its packages come from the binary

## Context and Problem Statement

`kinds/substrate.reamde.dev/authority.yaml` called the project's name a
placeholder. Every repository holds records of kinds under
`substrate.reamde.dev`, so it can never change. A package under it that the
binary does not ship was read from wherever the name's DNS pointed
(`internal/catalog/published.go`). The 1.0 review (F2 point 7, row 19) asks
that the name be permanent, that who holds the domain be written down, and
that the server know its own packages without DNS.

## Considered Options

- Leave the name a placeholder until a name is chosen.
- Declare the name permanent, and take every package under it from the
  binary, refusing one the binary does not ship.
- Declare it permanent, take from the binary every package under it that the
  binary ships, whatever DNS says, and leave a package it does not ship to
  the project's document as today until that is decided.

## Decision Outcome

Chosen: the third. `substrate.reamde.dev` is permanent. Its domain is held by
the project's maintainer, who keeps its DNS and the authority document the
project's apps are verified by. A package under it, or under any name beneath
it, that the binary ships comes from the binary. The catalog answers it
before it reads any document, so the name's DNS pointing elsewhere changes
nothing. No repository publishes under the name or a name beneath it
([0289](0289-a-repository-publishes-a-package-by-moving-it-out-of-the-local-namespace-to-an-authority-it-proves.md)),
as none may be named there.

A package under the project's name that the binary does not ship is still
read from the project's authority document, as the Workouts app's
packages are today. Whether the project's name should publish only what the
binary ships, which would refuse those, is not decided here. The answer goes
in one function (`projectPackageFromDocument`), and one test states today's
behavior.

### Consequences

- Good, because a server's own packages do not depend on DNS, and a domain
  lost or hijacked cannot replace them.
- Bad, because a package the project publishes outside the binary still
  depends on the project's DNS until that is decided.

### Confirmation

`TestTheProjectsOwnPackagesComeFromTheBinaryWhateverDNSSays`, with a stand-in
for DNS pointing the name at another server's document, and
`TestAProjectPackageTheBinaryDoesNotShipIsReadFromTheProjectDocument`.
