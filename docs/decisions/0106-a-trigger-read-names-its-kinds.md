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
new `(repository, kind, seq)` index that replaces `(repository, kind)`. The
read names each kind in its own branch, `kind = $n AND seq > $cursor ORDER
BY seq LIMIT 200`, and joins the branches with `UNION ALL` under one
`ORDER BY seq LIMIT 200`, which Postgres runs as a Merge Append of index
scans. A single `kind = ANY($kinds)` does not bound the read: an array on the
index's second column cannot return rows in seq order, so on Postgres 16 the
planner either sorts every remaining entry of the kinds or walks the primary
key and filters out the entries of other kinds, and a kind above about 1% of
the changelog gets the second plan. A kind named twice is named once, so no
entry is read twice. A short batch covers through the head, so the cursor moves over every entry of
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

- Good, because a batch reads at most 200 entries of each named kind and
  sorts nothing, under both the custom and the generic plan, so a drain or a
  replay reads only its kinds' entries, and the cursor still reaches head.
- Bad, because a source over many kinds (a wide glob) runs one index scan
  per kind on every batch.
- Bad, because a glob source pays one index probe per distinct kind in the
  changelog on every batch.
- Bad, because a `*` source gains nothing: it still reads every entry.
- Bad, because migration 0007 builds an index over the whole changelog, and
  the build holds writes to it for its duration.

### Confirmation

`internal/engine/triggerread_db_test.go`: over 1,500 entries of another
kind, a drain of 250 entries of the read kind (more than one batch) returns
only the source's kinds, in seq order and each once, for an exact kind, a
package glob, an authority glob, a kind named twice and a two-kind source,
and every batch covers through its last entry or the head. The plan of the
read `changesPast` runs, for one kind and for two, under the custom and the
generic plan, walks `changelog_kind_seq_idx`, has no Sort node, filters out
no entry, and reads at most 200 entries in each index scan; the old
`kind = ANY(...)` read fails all four. A replay from seq 0 delivers every
matched record and leaves the cursor at head.

## More Information

The op filter and the CEL guard still run in Go (`matchChanges`): an op is
derived from the entry's payload (`runner.OpOf`).
