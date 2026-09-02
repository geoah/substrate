---
status: accepted
date: 2026-09-02
decision-makers: George Antoniadis
---

# 0046. A repository owns one authority, chosen at registration and defaulting to the server's host

## Context and Problem Statement

Every kind carries an authority ([0042](0042-every-kind-carries-an-authority.md)),
and the shipped vocabulary publishes under `substrate.reamde.dev`. A user's own
kinds have no home: any token may declare under any non-shipped authority
string, nothing records which one is the repository's, and the coming sample
import ([the plan](../plans/providers-and-samples.md)) needs one authority to
rewrite a copied closure onto. [#285](https://github.com/geoah/substrate/issues/285)
proposed deriving it from the signing key, which needs the authority grammar
widened ([0014](0014-authorities-widen-only-outside-the-id-alphabet.md)) and
is not v1 work.

## Considered Options

- The user names the authority at registration, defaulting to their username
  under the host the request reached; a DNS-style name, permanent
- Derive the authority from the signing public key (#285)
- Derive it from the username and a configured public URL of the server
- Let the user set it later, on the repository record

## Decision Outcome

Chosen: the authority is a registration input. `POST /register` takes an
optional `authority`; absent, the HTTP layer fills `<username>.<request host>`
(port stripped, lowercased), the way a handle sits under its server. The
engine requires a concrete value, holds it to the kind grammar's authority
(`vocabulary.ValidRepositoryAuthority`: lowercase DNS labels, at least one
dot, DNS length limits), refuses anything under `substrate.reamde.dev`, and
refuses one another repository already owns. It is stored on the control-plane
row (`repositories.authority`, unique), echoed in the register response and in
`RepositoryInfo.Authority`, and written onto the repository's self-description
record. It never changes.

The username stays the login identifier and the engine's repository key
(`Dataset(ctx, username)`); the authority is the repository's public name. The
two are distinct columns because renaming the key every envelope, cache and
idempotency string uses would buy nothing the authority column does not.

Key derivation is the right long-term proof of ownership and it is not
blocked by this: the same column takes a verified domain later, the way
atproto binds a handle to a key through a DNS record, and a repository that
never verifies keeps the default. A configured public URL would put the
deployment in configuration the request already carries. Setting it later
leaves a window in which kinds are declared with no home.

### Consequences

- Good, because sample import has an authority to rewrite onto from the
  first registration, and a repository can say where its kinds live.
- Good, because the default needs no configuration and is unique per user on
  a host.
- Bad, because a substrate reached under two hosts (a LAN name and a
  Tailscale name) defaults different registrations to different suffixes;
  the user names the authority to avoid it.
- Bad, because nothing yet proves the user controls the name they typed:
  `ada.example.com` is accepted on the user's word. Verification is future
  work and this record does not promise its shape.
- Bad, because an IPv6 literal host yields no legal default and such a
  registration must name an authority.
- Neutral: rows from before the column are backfilled `<username>.localhost`
  by migration 0013. Nothing rehomes their kinds; the tree assumes fresh
  repositories before v1.

### Confirmation

`TestRegistrationRefusals` and `TestRegistrationRefusesATakenAuthority`
(internal/engine) hold the grammar, the publisher-namespace refusal and
uniqueness; `TestRegistrationCreatesTheUserAndNothingBefore` asserts the
authority reaches the control-plane row and the repository record;
`TestRegisterThenLogin` and `TestRegistrationKeepsTheAuthorityItIsGiven`
(internal/api) pin the default and the pass-through;
`TestRepositoryAuthorityGrammar` (internal/vocabulary) pins the helpers.

## More Information

Reopen trigger: domain verification, or #285 landing a key-derived
authority. Either extends this record rather than replacing the column.
