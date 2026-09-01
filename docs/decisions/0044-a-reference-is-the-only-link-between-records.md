---
status: accepted
date: 2026-08-31
decision-makers: George Antoniadis
---

# 0044. A reference is the only link between records; `data.edges` is removed

## Context and Problem Statement

A record pointed at another record two ways. An edge was a row in the `edges`
table, declared under `data.edges`, named by a `rel` and written by the `link`
and `unlink` ops. A reference is a property of `type: reference`, declared
beside every other property and carrying a `<kind>/<id>` path in
`data.properties`. Both are a named, directed pointer at one record, so record
[0032](0032-an-owner-pointer-may-be-a-reference-and-a-mirror-account-is-one.md)
had to write a rule for choosing between them. The rule ran three paragraphs
and still left `account` spelled both ways across `kinds/`.

The duplication reached past the declaration. Filtering had two grammars
(`EdgeFilter` and `condReference`), `incoming` answered over two arms
(`ViaEdge` and `ViaReference`), and `patch` was asymmetric: it wrote properties
and could never touch a link, so a caller changing both wrote twice and the
record delta carried half of what the record pointed at.

## Considered Options

- Keep both mechanisms, and keep 0032's rule for choosing between them
- Fold references into edges, so every pointer is an `edges` row
- Fold edges into references, so `type: reference` absorbs link data,
  multiplicity, existence checking, cascade and the mapping subject

## Decision Outcome

Chosen: fold edges into references. `type: reference` takes `properties:` for
data on the link, `repeated: true` for multiple targets, `mustExist: true` for
the write-time existence check edges enforced, `onDelete: cascade` in place of
`ownerRef: true`, and `subject: true` for a `recordmapping`'s subject.
`data.edges` is refused in a declaration, naming `properties` and
`type: reference` as its replacement.

Keeping both lost on cost that fell on readers rather than on the engine. Two
declaration surfaces mean two places to look for what a kind points at; two
filter grammars mean a query that cannot be written once; two reverse arms mean
`incoming` returning rows a caller has to tell apart. SurrealDB is the
cautionary precedent: it offers record links, `RELATE` graph edges and embedded
records, and a reader's first question is which of the three to use rather than
what to write. EdgeDB is the working one: one `link`, with link properties and
an `on target delete` policy on the declaration, which is the shape taken here.

Folding the other direction was worse. A reference filters as a scalar, hops in
a `displayTemplate`, travels in the record delta and round-trips through
`get -o yaml | apply -f`; an edge did none of those, so making every pointer an
edge would have moved data out of the record to buy nothing.

Three things follow, and they are the reason this is a record and not a commit.

**The changelog op set shrinks to `put`, `patch`, `delete`, `merge`, `split`,
`gc`.** `link` and `unlink` are gone, and a pointer changes only when the
record holding it is written.

**The refs index is a derived projection, not a truth.** The `refs` table holds
one row per reference site, computed from a record's folded properties and its
kind declaration, on every live write and again for every live record after
`RebuildRepository` refolds. It has no fold effects of its own. It serves
`incoming` and the cascade sweep, which is what lets an unpinned reference be
read in reverse and lets `onDelete: cascade` drop its pin requirement.

**Merge stops repointing.** A merge moved a loser's edge rows to the winner and
parked what collided in the merge record so a split could put them back.
Nothing repoints now: a pointer at a merged-away id resolves at read through
the former-id trail, which is what references already did.

There is no migration. A changelog carrying `link`, `unlink` or edge fold
effects is not replayable, so opening a repository stamped below the current
changelog dialect asks the changelog for a `link` or `unlink` op and refuses
there, rather than serving a store whose links migration `0010` already dropped
and leaving the refusal to the day somebody rebuilds.

### Consequences

- Good, because a kind's pointers are declared in one place, filtered with one
  grammar, and read in reverse from one index.
- Good, because a pointer travels with the record: `patch` can change a link,
  and `get -o yaml | apply -f` round-trips everything a record points at.
- Good, because `incoming` and the cascade cover unpinned references, which
  neither could do while both needed a pin to know which kinds to read.
- Good, because a merge no longer parks rows outside the table a narrowing
  count reads, which closes the window 0027 recorded as a cost.
- Bad, because there is no path off an existing changelog: a repository with
  `link` entries stays on the old binary or is thrown away. Pre-v1 the answer
  is `mise run dev:wipe`.
- Good, because a reference value has ONE served shape. Stored and served, a
  reference is always the object `{ref: "<kind>/<id>"}`, with each declared link
  property beside `ref`; a bare path string is accepted at the write as
  shorthand and normalized to it. The shape was chosen so a reference can gain
  an attribute without a response changing shape: adding `properties:` to a live
  declaration adds a key to the value and a field to its GraphQL object, where a
  conditional shape would have turned every served string into an object and
  every scalar field into an object field. Readers still parse both shapes,
  because a reader never picks its parse from the declaration.
- Bad, because clearing a link means writing the record. A bundle uninstall
  puts its bound records instead of unlinking them, so the changelog shows a
  record delta where it showed a link delta.
- Bad, because the refs index is derived and unsigned. Nothing in the hash
  chain proves a row, so a wrong row is repaired by a rebuild rather than
  caught by `repository verify`.
- Bad, because `required:` on a converted link is checked on every write and
  not only at creation. An edge could be unlinked and left absent; the same
  pointer as a required property can no longer be cleared, and a kind that
  wants it clearable drops `required`.
- Bad, because a repeated reference refuses the same target twice. An edge set
  could hold two links to one record carrying different link data; that shape
  is gone, and a second pointer at one record is now a record of its own.
- Neutral, because a reference FILTER follows the former-id trail: `eq` on
  either the winner's path or a loser's matches every row naming that record,
  since the stored value keeps the path its author wrote and only reads
  resolve.

### Confirmation

The vocabulary loader refuses `data.edges`, naming `properties` and
`type: reference`, and refuses `ownerRef`, naming `onDelete`; both refusals are
held by the loader's tests. `mise run kinds:check` holds the converted
declarations in `kinds/`. `wire.golden.json` holds the envelope's field set
across Go and the console, so the removal of `data.edges` from the wire cannot
land in one and not the other. Migration `0010_refs_index.up.sql` drops the
`edges` table, and the engine suite holds that a live write and a
`RebuildRepository` derive the same `refs` rows.

Nothing holds the old changelog beyond the refusal to replay it, which is the
point: an unreplayable changelog is refused at open, not partially applied.

## More Information

This supersedes
[0027](0027-an-edge-outlives-a-tombstone-and-dies-with-a-purge.md), whose
subject no longer exists, and
[0032](0032-an-owner-pointer-may-be-a-reference-and-a-mirror-account-is-one.md),
whose rule for choosing between the two mechanisms has nothing left to choose.
0032's outcome survives in substance: a provider mirror's `account` is a
reference, and deleting the account collects the records it synced, now spelled
`onDelete: cascade`.
[0034](0034-a-reference-may-pin-a-trait-not-only-a-kind.md) stands and now
covers every pointer in the system.

Reopen this if link data outgrows a flat block of scalars. A link that needs
its own identity, history or lifecycle is a record with two references, not a
richer property, and choosing that would be a new decision.
