---
status: superseded
superseded-by: 0050
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

Signing is MANDATORY, not opt-in: every repository activates at its first
open under a keyed host — a brand-new repository (empty changelog) before its
seed appends, so it is signed from seq 1; an upgraded store after the
backfill, on a settled head. A keyless host refuses to boot; the one
exception is `SUBSTRATE_INSECURE_ALLOW_INVALID_SIGNATURES`, under which a
keyless host runs without activating, stamps the all-zero placeholder
signature (`sig` is NOT NULL), and `repository verify` names the state as a
finding. The placeholder and the switch are pre-v1 scaffolding
([#175](https://github.com/geoah/substrate/issues/175)).

The rules that make it hold, each the answer to a specific attack:

- **Activation is durable and one-way** (`repositories.signed_from_seq`).
  From that seq on, a placeholder or invalid signature is a verification
  failure and the engine refuses to append unsigned — otherwise writing the
  all-zero placeholder over a signature is an undetectable downgrade, and so
  is running the host keyless. The insecure switch never weakens an
  activated repository. A commit whose dataset still believes signing is off
  RE-READS the durable mark under the changelog lock (settleChain), so a
  second process activating concurrently cannot slip an unsigned entry past
  a stale one.
- **The seed refuses plain framing, both ways.** The DEK wrap falls back to
  plaintext on a keyless host; the signing seed must not (the signature
  exists precisely to resist a database-only attacker), so activation
  requires the credential key and the loader rejects plain framing and wrong
  lengths rather than falling back.
- **The trust anchor is pinned outside, and the pin is ENFORCEABLE.**
  Everything in the database — key rows, signatures, epochs,
  `signed_from_seq` itself — is rewritable by whoever holds the database.
  Activation therefore logs the `(public key, signed_from_seq)` pair for
  out-of-band pinning, the activation epoch is signed, and
  `repository verify --expect-public-key/--expect-signed-from/--expect-head`
  turns the pinned values into findings rather than an eyeball comparison.
  A dump alone, unpinned, proves internal consistency only, and the docs
  say so.

### Consequences

- Good, because the attacker model is now stated exactly: rewriting signed
  history requires the database AND the credential key, and whoever holds
  both is the host operator.
- Bad, because a lost credential key STOPS WRITES on an activated repository
  (by design: refusing beats quietly shedding the guarantee), and the only
  recovery is restoring the key: no rotation or deactivation path exists
  yet, which is a named revisit trigger below.
- Bad, because signing is only as old as its activation: entries before
  `signed_from_seq` are chain-covered but unsigned, and verify reports
  coverage rather than pretending.

### Confirmation

`TestChangelogSigningSignsAndDetectsRemoval` (a fresh repository is signed
from seq 1; stripping a signature to the placeholder is named),
`TestSigningActivationIsOneWay` (a keyless host cannot deactivate an
activated repository: appends and reseals refuse), and the repositories
CHECK constraints (migration 0005) that keep key, public key and activation
mark whole.

## More Information

Replaces the plan `docs/plans/changelog-integrity.md`. Reader-facing threat
model: [docs/changelog.md](../changelog.md#the-chain). Revisit when key
rotation or a sanctioned deactivation is needed (both require an epoch kind
this design does not yet define), or if a second consumer of the public key
appears (external anchoring, a checkpoint endpoint), which layers ON TOP of
per-entry signatures rather than replacing them.
