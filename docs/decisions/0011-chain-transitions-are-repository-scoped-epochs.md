---
status: accepted
date: 2026-08-15
decision-makers: George Antoniadis
---

# 0011. Sanctioned chain transitions are recorded as repository-scoped chain epochs

## Context and Problem Statement

Three legitimate events move or begin a repository's hash chain: the backfill
that stamps pre-chain history, a reseal's sanctioned rewrite (the payload
bytes change, so every downstream hash moves), and signing activation. After
any of them, a head an operator wrote down earlier stops matching — and a
re-chained history is byte-indistinguishable from a tampered one. Without a
durable record of the transition, the chain's own receipts turn into false
alarms, or worse, real alarms get explained away.

## Considered Options

- A `chain_epochs` table, repository-scoped (RLS, erased with the
  repository), one row per transition, signed when signing is on.
- A control-plane table beside `repositories`.
- Ephemeral report fields (`RechainedFrom` on the reseal report).
- No record: document that reseal invalidates receipts.

## Decision Outcome

Chosen: the repository-scoped `chain_epochs` table (migration 0005), rows
written INSIDE the transaction that performs the transition — the backfill
is ONE transaction for exactly this reason, so its epoch is as durable as
its hashes — each carrying `(at, reason, from_seq, old_head, new_head)`
plus the signing state, and signed when a key exists. The verifier CHECKS
them, not merely lists them: an invalid epoch signature, an unsigned epoch
claiming a transition inside signed history, and an activation epoch that
is unsigned or disagrees with the durable mark are all findings. A pinned
head (`repository verify --expect-head`) that matches a reseal epoch's
`old_head` is explained-and-reported for re-pinning; one that matches
nothing is a plain finding.

The control-plane placement fails mechanically: reseal runs as one
transaction on the RLS-bound application pool, which cannot reach a
control-plane table, so the epoch could not land atomically with the rewrite
it describes. Ephemeral report fields fail the actual requirement — the
verifier that needs the explanation runs LATER, against the database, not
against a CLI's stdout. Documenting the invalidation without recording it
makes every reseal look like an attack to the exact person the chain exists
to inform.

The backfill epoch is mandatory, not optional: a backfilled hash is
byte-identical to a contemporaneous one, so without the epoch the chain would
silently claim to have witnessed history it only notarized.

### Consequences

- Good, because "explained" and "unexplained" head movement are now different
  observable states, which is the whole point of tamper EVIDENCE.
- Good, because the epoch lives in the user plane with the history it
  describes: erased with the repository, covered by RLS, recoverable with the
  same backup.
- Bad, because epoch DELETION is undetectable from the database alone (the
  rows are not themselves chained); signing helps only from activation on,
  and an unsigned pre-activation epoch is a statement, not a proof. The
  pinned head and key pair (0010) remain the anchor.
- Bad, because activation's control-plane mark and its epoch cannot share a
  transaction (two pools), so a crash between them leaves an activated
  repository with no activation epoch. Chosen order makes the GUARANTEE land
  first; the next open records the late epoch, signed
  (`ensureActivationEpoch`), so the state repairs instead of failing
  verification forever.

### Confirmation

`TestChainBackfillStampsLegacyHistory` (the backfill epoch is present),
`TestResealRefusesTamperThenRechainsLegacy` (a hand rewrite makes the reseal
REFUSE — it verifies first, so it cannot launder tampering into fresh
signatures — while the sanctioned rewrite verifies and records old and new
head), and `TestChangelogSigningSignsAndDetectsRemoval` (the activation
epoch is signed and verifies).

## More Information

Replaces the plan `docs/plans/changelog-integrity.md`. The verifier's use of
epochs: [docs/changelog.md](../changelog.md#the-chain).
