---
status: accepted
date: 2026-09-26
decision-makers: George Antoniadis (via the issue-637 agent session)
---

# 0106. A record trigger's changelog read names its kinds

## Context and Problem Statement

The dispatcher read every changelog entry past a record trigger's cursor,
200 a batch, and filtered by kind in Go (`changesPast`, then
`matchChanges`). A `trigger replay` over one small kind in a large
repository therefore walked the whole changelog: Mneme's
`extractlinks-on-calendarevent` read 2.1 M entries to find about 3 k, about
10 hours ([#637](https://github.com/geoah/substrate/issues/637)). The issue
asks that a replay cost in proportion to the trigger's kinds.

## Considered Options

- Replay over the kinds' current rows instead of the changelog
- Read the changelog by kind: name the source's kinds in the SQL read and
  move the cursor over the rest
- Name the kinds as a C-collation range per glob, with a C-collation index

## Decision Outcome

Chosen: read the changelog by kind. `changesPast` reads the head, then the
entries of the source's kinds in `(cursor, head]`, ordered by seq, through a
new `(repository, kind, seq)` index that replaces `(repository, kind)`. A
short batch covers through the head, so the cursor moves over every entry of
another kind in one step; a full batch covers through its last entry. This
holds because sequence order is commit-visibility order
([changelog.md](../changelog.md)). A package or authority glob is matched in
Go against the distinct kinds the changelog holds, read at each batch by a
skip scan over the new index.

A replay over current rows would change what a replay delivers (one delivery
per record, no deletes, no history) and needs a second cursor shape; the
changelog read keeps every delivery the same. A C-collation range would put
prefix matching in SQL, but a second index collation and its planner
estimates are harder to reason about than one distinct-kind probe per kind.

### Consequences

- Good, because a drain or a replay reads only its kinds' entries, and the
  cursor still reaches head.
- Good, because the public change feed's `kind = ANY(...)` read with a seq
  bound also starts at its seq through the new index.
- Bad, because a glob source pays one index probe per distinct kind in the
  changelog on every batch.
- Bad, because a `*` source gains nothing: it still reads every entry.
- Bad, because migration 0007 builds an index over the whole changelog, and
  the build holds writes to it for its duration.

### Confirmation

`internal/engine/triggerread_db_test.go`: a read over a backlog larger than
one batch returns only the source's kinds for an exact kind, a package glob
and an authority glob, and covers through the head; the plan of that read
walks `changelog_kind_seq_idx` and filters out no entry of another kind; a
replay from seq 0 delivers every matched record and leaves the cursor at
head.

## More Information

The op filter and the CEL guard still run in Go (`matchChanges`): an op is
derived from the entry's payload (`runner.OpOf`).
