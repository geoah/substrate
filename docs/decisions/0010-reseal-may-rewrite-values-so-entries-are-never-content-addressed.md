---
status: accepted
date: 2026-08-15
decision-makers: George Antoniadis
---

# 0010. Reseal may rewrite history's values, so entries are never content-addressed

## Context and Problem Statement

This record is written retrospectively, at the pre-stabilization design
review, from `internal/engine/reseal.go` and the `reseal` entry in
[terms.md](../terms.md). Early repositories stored secret property values as
plaintext in records and in changelog payloads. Once the sealed store
existed, that history was a liability: an append-only log otherwise keeps a
leaked key or password forever, in every backup, past every rotation. Something
had to reconcile append-only with the right to erase, and the shape of the
answer constrains every future design that wants to hash, sign or replicate
the log.

## Considered Options

- A values-only, in-place rewrite: secret values in records and in historical
  changelog payloads are replaced by sealed refs; no entry is added, removed
  or renumbered.
- Append a correction and accept that the plaintext remains readable in
  history and in every backup.
- Rewrite history into a new log and renumber.

## Decision Outcome

Chosen: the values-only in-place rewrite, as `repository reseal`, the one
sanctioned mutation of history. It erases the material while every structural
property of the log survives: seq is untouched, cursors keep resuming, replay
keeps folding, and the untouched values re-marshal byte-faithfully
(`decodeNumberPreserving` exists exactly for this). Appending a correction
erased nothing. Renumbering broke every consumer of decision 0009's ordering
for a problem that only concerned values.

### Consequences

- Good, because secrets are actually erasable: after a reseal, the changelog,
  its backups going forward, and the records hold refs, and the material
  lives only in the sealed store where rotation deletes it.
- Good, because the backup and replication unit stays `changelog + blobs +
  sealed`, unchanged by the rewrite.
- Bad, because "append-only" now carries one asterisk that every future
  design must know. Content-addressed entries, signed entries and
  replicate-by-digest are foreclosed as long as an entry's bytes may change
  after commit; any future entry hash must exclude secret values, or reseal
  must be re-scoped first.
- Bad, because a follower that copied payloads before a reseal never learns
  of the rewrite unless the sync protocol handles in-place payload updates;
  naive log shipping replicates the plaintext the reseal existed to erase.

### Confirmation

`TestResealMovesLegacyValuesIntoTheStore`
(`internal/engine/sealing_general_db_test.go`) holds the rewrite; the
values-only contract is stated at the top of `internal/engine/reseal.go`.
Nothing mechanical prevents a future entry-hashing design from colliding with
this; that collision is what this record exists to make loud.

## More Information

Reopen alongside any proposal to hash, sign or content-address changelog
entries, and settle the two together: either the hash excludes every value a
reseal may rewrite, or reseal is retired first.
