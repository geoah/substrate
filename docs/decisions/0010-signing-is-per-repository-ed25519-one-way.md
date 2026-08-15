---
status: accepted
date: 2026-08-15
decision-makers: George Antoniadis
---

# 0010. Changelog signing is a per-repository Ed25519 key with one-way activation

## Context and Problem Statement

The hash chain (0009) needs no secret, so an attacker with database write
access can rewrite an entry and re-chain everything after it undetectably.
Signing raises that bar — but only if the key cannot be read out of the same
database, only if "unsigned" cannot be forged by deleting signatures, and
only if enabling it does not put key management on the user (the model keeps
"no user-managed keys").

## Considered Options

- Per-repository Ed25519 key, sealed under the host credential key, signing
  every entry's chain hash; activation durable and one-way.
- HMAC-SHA256 keyed from the repository DEK.
- One host-wide signing key.
- Signed checkpoints only (sign every Nth head).
- Client-held keys signing writes.

## Decision Outcome

Chosen: per-repository Ed25519 with one-way activation
(`internal/engine/signing.go`). HMAC verification requires the secret, so a
database dump plus a public key could never be audited. A host-wide key breaks
the per-repository plane separation for no gain. Checkpoints save a cost that
is already negligible (~30µs per entry) and add a second artifact to store and
reason about. Client-held keys reverse the model's "no user-managed keys"
stance and were out of scope by design.

The rules that make it hold, each the answer to a specific attack:

- **Activation is durable and one-way** (`repositories.signed_from_seq`).
  From that seq on, a missing or invalid signature is a verification failure
  and the engine refuses to append unsigned — otherwise
  `UPDATE changelog SET sig = NULL` is an undetectable downgrade, and so is
  flipping the environment toggle. The toggle only ever activates.
- **The seed refuses plain framing, both ways.** The DEK wrap falls back to
  plaintext on a keyless host; the signing seed must not (the signature
  exists precisely to resist a database-only attacker), so activation
  requires the credential key and the loader rejects plain framing and wrong
  lengths rather than falling back.
- **The trust anchor is pinned outside.** Everything in the database — key
  rows, signatures, epochs, `signed_from_seq` itself — is rewritable by
  whoever holds the database. Activation therefore logs the
  `(public key, signed_from_seq)` pair for out-of-band pinning, and the
  activation epoch is signed. A dump alone proves internal consistency only,
  and the docs say so.

### Consequences

- Good, because the attacker model is now stated exactly: rewriting signed
  history requires the database AND the credential key, and whoever holds
  both is the host operator.
- Bad, because a lost credential key STOPS WRITES on an activated repository
  (by design: refusing beats quietly shedding the guarantee), and recovery is
  an operator act that shows up as an epoch.
- Bad, because signing is only as old as its activation: entries before
  `signed_from_seq` are chain-covered but unsigned, and verify reports
  coverage rather than pretending.

### Confirmation

`TestChangelogSigningSignsAndDetectsRemoval` (removal is named),
`TestSigningActivationIsOneWay` (the toggle cannot deactivate; a keyless host
refuses to append and to reseal), and the repositories CHECK constraints
(migration 0005) that keep key, public key and activation mark whole.

## More Information

Replaces the plan `docs/plans/changelog-integrity.md`. Reader-facing threat
model: [docs/changelog.md](../changelog.md#the-chain). Revisit if a second
consumer of the public key appears (external anchoring, a checkpoint
endpoint), which layers ON TOP of per-entry signatures rather than replacing
them.
