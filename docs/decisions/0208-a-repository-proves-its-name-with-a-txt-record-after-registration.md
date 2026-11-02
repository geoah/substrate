---
status: proposed
date: 2026-11-02
decision-makers: George Antoniadis
---

# 0208. A repository proves its name with a TXT record after registration

## Context and Problem Statement

Registration takes a repository name on its owner's word
([0046](0046-a-repository-owns-one-authority-chosen-at-registration.md),
[0074](0074-the-repository-name-is-the-login-name.md)), so on a shared server
anyone can register `example.org` before the domain's owner does, and nothing
tells the two apart. An app that starts from a name also has no way to find
the server holding it. The design is decision 2 of
[the domains plan](https://github.com/geoah/substrate/pull/1145).

## Considered Options

- No proof (the state before this record)
- The name's DNS must point at this server at registration
- The same, plus a one-time code from the register page in the record
- Register freely, prove the name afterwards, and mark it until then

## Decision Outcome

Chosen: register freely and prove the name afterwards. Registration is
unchanged. **Verify domain** (`POST /api/v1/repository/domain`) mints a
one-time proof, keeps it on the repository record as the managed
`domainProof`, and answers the record to publish:

```text
_substrate.example.org.  TXT  "v=substrate1 server=https://substrate.example.net proof=k3j9x2abcd"
```

The check (`POST /api/v1/repository/domain/verify`) reads that record, holds
`proof=` to the pending proof, and fetches `server=`'s
`/.well-known/substrate/server.json` through the confined client to compare
its `server.instance` with this server's. A server mints its instance id once,
into a file under the data root, so it recognizes itself without a configured
public URL. A passing check stamps the managed `domainVerifiedAt` and spends
the proof. A name of one label under the host the request reached is
verified without a record, because the server's operator controls that DNS.
The console marks the name unverified until the stamp lands. Both routes are
the owner's.

The authority stays permanent and the repository id
([0052](0052-the-authority-is-the-repository-id.md)): verification renames
nothing. This amends 0046 and 0074, which accepted a name without proof.

### Consequences

- Good, because the register door keeps its one step, and the `_substrate`
  record a verified repository publishes is the same one an app reads to
  find its server.
- Good, because a squatted name is never found by discovery: an app that
  starts from the name reaches the server the name's own DNS points at.
- Bad, because squatting stays possible on a shared server; the mark is a
  warning, not a refusal.
- Bad, because a server reached on a loopback or private address verifies no
  name but its own host's: the confined client never dials such an address.
- Bad, because a server whose data root is lost mints a new instance id, and
  every name it verified must publish its record and check again.

### Confirmation

`internal/api/domain_test.go` holds the lifecycle, each refusal reason, the
own-host case and the owner-only rule; `internal/engine/domain_db_test.go`
holds the proof on the repository record and refuses a generic write of
either property.

## More Information

The step up is the third option: when a server opens registration to people
its operator does not know, the register page asks for the same record before
it creates the repository.
