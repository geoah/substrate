---
status: accepted
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

The check (`POST /api/v1/repository/domain/verify`) reads that record and
holds `proof=` to the pending proof. That proof is the evidence: only the
name's controller can publish it. The check then confirms that `server=`
reaches this server. With a configured public URL (`SUBSTRATE_PUBLIC_URL`),
`server=` must equal that origin exactly. With or without one, the check
fetches `server=`'s `/.well-known/substrate/server.json` through the confined
client and compares its `server.instance` with this server's, an id the
server mints once into a file under the data root. Both comparisons are a
consistency check that the record points here, not authentication of the
server: a proxy that forwards to this server relays its document. A passing
check stamps the managed `domainVerifiedAt` and spends the proof.

A name of exactly one label under the host of the configured public URL is
verified without a record, because the operator controls that DNS. The
request's `Host` never decides anything, since a caller names any `Host` it
likes; with no public URL configured there is no shortcut, and every name
needs the record. The console marks the name unverified until the stamp
lands. Both routes refuse a scoped app token; a full-access token is a
sign-in and acts as the owner
([0204](0204-an-app-may-ask-for-full-access-and-its-owner-approves-it-whole.md)).

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
- Bad, because a server reached on a loopback or private address verifies
  only names under its configured public URL's host: the confined client
  never dials such an address.
- Bad, because without a public URL an origin that relays this server's
  document passes the server comparison; the proof in DNS still has to be
  published by the name's controller.
- Bad, because a stamp is never checked again: a name whose record later
  names another server, or whose domain lapses, reads verified until the
  owner runs the check again. Its audience today is the owner alone; a
  periodic re-check belongs with the step up below.
- Neutral: a server whose data root is lost mints a new instance id. The
  stamps it wrote stay, and a later check compares against the new id and
  still passes while `server=` reaches this server.
- Neutral: with a configured public URL, `server=` naming it exactly
  decides the server check and the origin's `server.json` is not read
  back; a proof binds one check, so two checks of one proof stamp once.

### Confirmation

`internal/api/domain_test.go` holds the lifecycle, each refusal reason, the
public-URL shortcut, a forged `Host` refused, the exact `server=` origin
under a public URL, and a scoped app token refused while a full-access one is
admitted; `internal/engine/instance_test.go` holds one persisted instance id
across racing first boots; `internal/engine/domain_db_test.go`
holds the proof on the repository record and refuses a generic write of
either property.

## More Information

The step up is the third option: when a server opens registration to people
its operator does not know, the register page asks for the same record before
it creates the repository.
