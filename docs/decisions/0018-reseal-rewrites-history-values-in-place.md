---
status: accepted
date: 2026-08-15
decision-makers: George Antoniadis
---

# 0018. Reseal rewrites history's values in place, so entry bytes are not identity

## Context and Problem Statement

This record is written retrospectively, at the pre-stabilization design
review, from `internal/engine/reseal.go` and the `reseal` entry in
[terms.md](../terms.md). Early repositories stored secret property values as
plaintext in records and in changelog payloads. Once the sealed store
existed, that history was a liability: an append-only history otherwise keeps
a leaked key or password forever, in every backup, past every rotation.
Something had to reconcile append-only with the right to erase, and the shape
of the answer constrains every future design that wants to hash, sign or
replicate the changelog.

The chain landed first
([0009](0009-the-chain-hashes-what-postgres-stored.md)): every entry now
carries a hash over the bytes Postgres stored, and signed repositories sign
it. A hash over stored bytes and a rewrite of those bytes cannot both be
unconditional, so this record states which one gives way.

## Considered Options

- A values-only, in-place rewrite: secret values in records and in historical
  changelog payloads are replaced by sealed refs; no entry is added, removed
  or renumbered.
- Append a correction and accept that the plaintext remains readable in
  history and in every backup.
- Rewrite history into a new changelog and renumber.

## Decision Outcome

Chosen: the values-only in-place rewrite, as `repository reseal`, the one
sanctioned rewrite of history's bytes. It erases the material while every
structural property of the changelog survives: seq is untouched, cursors keep
resuming, replay keeps folding, and the untouched values re-marshal
byte-faithfully
(`decodeNumberPreserving` exists exactly for this). Appending a correction
erased nothing. Renumbering broke every consumer of decision
[0017](0017-the-changelog-is-one-writers-total-order.md)'s ordering for a
problem that only concerned values.

The rewrite wins over the hash. Reseal verifies the chain before it touches
anything, so it cannot launder a tamper into fresh hashes; it re-chains from
the first rewritten seq, re-signs where signing is active, and records the
move as a chain epoch carrying the old and new head
([0011](0011-chain-transitions-are-repository-scoped-epochs.md)). What the
chain gives up is that an entry's hash is not stable for the entry's
lifetime, so a hash is tamper evidence, never an address or an identity.

### Consequences

- Good, because secrets are actually erasable: for every kind in the live
  registry, the changelog rows, the records, and every logical dump taken
  afterwards hold refs, and the material lives only in the sealed store where
  rotation deletes it.
- Good, because the rewrite does not change what a backup or a follower must
  copy: `changelog + blobs + sealed`, plus the chain metadata the chain added
  (`chain_epochs` and the repository's signing state), without which a reseal
  is indistinguishable from a tamper.
- Bad, because erasure is bounded by retention, not by the reseal. The
  rewrite is an in-place `UPDATE`, so the old tuple versions and the WAL that
  carries them keep the plaintext: a physical backup or WAL archive taken
  before the reseal, or a chain spanning it, is sanitized only when its
  retention expires.
- Bad, because erasure reaches only as far as the repository's live registry.
  A kind uninstalled before the reseal keeps whatever plaintext its old
  entries held, because no declaration survives to say which of its
  properties were secret; the change feed fails closed for exactly those
  kinds (`redactChangePayload`), so the bytes do not ride the wire, but they
  are still in the changelog.
- Bad, because "append-only" carries one asterisk that every future design
  must know. Content-addressed entries and replicate-by-digest stay
  foreclosed as long as an entry's bytes may change after commit: an entry is
  addressed by `(repository, seq)`, and a design that wants a durable digest
  must either exclude every value a reseal may rewrite or re-scope reseal
  first.
- Bad, because a follower that copied payloads before a reseal has no signal
  that they changed unless the sync protocol handles in-place payload
  updates;
  naive changelog shipping replicates the plaintext the reseal exists to
  erase. The reseal epoch makes the rewrite observable to anything that
  re-reads the source, which is what a protocol would key on.

### Confirmation

`TestResealMovesLegacyValuesIntoTheStore`
(`internal/engine/sealing_general_db_test.go`) holds the rewrite; the
values-only contract is stated at the top of `internal/engine/reseal.go`.
`TestResealRefusesTamperThenRechainsLegacy`
(`internal/engine/chain_db_test.go`) holds the reconciliation with the chain:
a hand rewrite makes the reseal refuse, while the sanctioned rewrite verifies
and records old and new head. Nothing mechanical stops a future design from
treating an entry hash as an identity; stating that collision before somebody
builds on it is this record's job.

## More Information

Reopen alongside any proposal to content-address or replicate changelog
entries by digest, and settle the two together: either the digest excludes
every value a reseal may rewrite, or reseal is retired first.
