---
status: accepted
date: 2026-08-15
decision-makers: George Antoniadis
---

# 0017. The changelog is one writer's total order per repository

## Context and Problem Statement

This record is written retrospectively, at the pre-stabilization design
review, from the code alone: the comment over the `changelog` table
(`internal/engine/migrations/0001_init.up.sql`), the append path
(`appendChange` in `internal/engine/rows.go`) and the replay path
(`internal/engine/fold.go`, `internal/engine/rebuild.go`). The review had to
answer whether the changelog's shape forecloses the futures the project
wants, multi-user repositories and offline replication among them, and the
answer deserved a record because nothing written said what the shape had
bought or what it had spent.

## Considered Options

- One strictly sequential, gapless `seq` per repository, allocated under a
  per-repository advisory lock held from first append until commit.
- A multi-writer changelog: entries carry their own identity and causal
  metadata, and replicas merge histories.
- The sequential changelog, plus a stable per-entry identifier beside `seq`
  as insurance for a later sync protocol.

## Decision Outcome

Chosen: the strictly sequential single-writer changelog. Holding the
repository's advisory lock from first append to commit makes seq order equal
commit visibility order, so a cursor is a resume token with no gap and no
skip, and every consumer (watch, trigger cursors, the console feed,
list-to-watch handoff via `head`) is built on that one guarantee. Deltas
carry values, so replay needs no declaration and the fold is deterministic
(`foldFTS`'s search bands are the fold's one registry read, an index over the
folded row rather than fold data); `RebuildRepository` is "delete the fold,
replay the changelog" in one transaction. A multi-writer changelog would have
traded all of that for a merge protocol nobody needs yet.

The choice runs deeper than the lock: an entry's only identity is
`(repository, seq)`, and seq escapes the changelog into data. `caused_by`
names a seq, an llmmessage's `changes` property stores seq addresses, and
trigger cursors key on it. Entries are addressed by position by design.
Interleaving a second history would mean renumbering, and renumbering breaks
all of it.

### Consequences

- Good, because resumable consumers get an airtight contract from one
  mechanism, and the failure it prevents (a cursor that silently skipped a
  committed entry) is unrecoverable any other way.
- Good, because follower replication stays cheap: ship `changelog + blobs +
  sealed` in seq order and replay; the entries are self-contained value
  deltas and the fold's clock is the entry's own `ts`. Cheap for appends
  only: a reseal rewrites historical payloads without advancing seq, so a
  follower converges only if it re-reads the rewritten range, and a follower
  that verifies needs more than the seq stream carries
  ([0018](0018-reseal-rewrites-history-values-in-place.md) names the copy
  set).
- Bad, because device-local offline writes are out. Merging two histories is
  a different system, not an upgrade to this one.
- Bad, because every writer to one repository serializes at commit. At one
  human per repository this is invisible; a busy shared repository would feel
  it.
- Bad, because entries have no identity independent of position, so any
  future sync protocol must either preserve seq exactly or be built before
  years of history make renumbering unthinkable. The chain's per-entry hash
  ([0009](0009-the-chain-hashes-what-postgres-stored.md)) is not that
  identity: it chains on the previous entry's hash and a reseal recomputes
  it ([0018](0018-reseal-rewrites-history-values-in-place.md)).

### Confirmation

The lock and its reason are stated where they act (`appendChange`,
`internal/engine/rows.go`); `settleFold` rolls back any transaction whose
fold effects have no entry to ride. `TestRebuildReproducesTheFold`,
`TestRebuildIsIdempotent` and `TestLogEntryCarriesValues`
(`internal/engine/rebuild_db_test.go`) hold replay determinism. Nothing
mechanical holds the foreclosure itself; that is this record's job.

## More Information

Revisit if device-local writes or multi-device sync become requirements. The
compatible insurance, should that day approach, is the third option above: a
stable per-entry identifier column that nothing depends on yet. Adding it
does not reverse this decision; it makes entries addressable without
renumbering them.
