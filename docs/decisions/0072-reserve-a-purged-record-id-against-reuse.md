---
status: rejected
date: 2026-09-09
decision-makers: George Antoniadis
---

# 0072. Reserve a purged record id so a later write cannot take it

## Context and Problem Statement

Once the sweep purges a deleted record, a `put` at the same kind and id
creates a new record, and a reference another record still holds resolves to
the newcomer. [Issue #372](https://github.com/geoah/substrate/issues/372)
read that as a defect, and PR
[#394](https://github.com/geoah/substrate/pull/394) implemented the fix: keep
a purged id in `former_ids` as a reservation, refuse a `put` there with `409`,
and take the record's advisory lock in the sweep so a racing write meets the
reservation. Three docs pages promised at the time that ids are never reused.
[0055](0055-a-retired-name-is-declared-and-never-inferred-from-a-prune.md)
named #372 as the open counterpart of retired declaration names, so without a
record the reservation would be proposed again.

## Considered Options

- Reserve a purged id: `applyPurge` keeps the id in `former_ids`, `checkID`
  refuses a write there with `409 conflict` (PR #394).
- Do not reserve: an id is stable while its record exists and free after the
  purge; the docs say so.

## Decision Outcome

Rejected: the reservation. The owner, 2026-09-09: "Don't promise ids never
get reused. That's a lie. This is a database, the user should be able to do
what they want."

The rule, as the engine already had it and as the docs now state: an id is
stable while its record exists, not unique across time. A `put` onto a
tombstone restores it until the sweep purges it. After the purge the id is
free, a `put` there creates a fresh record, and a reference that survived the
purge resolves to whatever holds the id. A former id (a merge loser's) still
resolves to its winner for as long as the winner exists, which
[0044](0044-a-reference-is-the-only-link-between-records.md) already states.

The reservation was refused because it spends ids nobody meant to spend. A
provider composes stable ids out of its own key
([data-model](../data-model.md)) and may delete and recreate a record at
will, so a mirror deleted upstream and restored later re-puts the same id, and
a `409` per item on that re-sync would have broken it. A bundle input record
named `default` is a well-known id, and deleting it would have reserved it
forever. Neither cost buys anything the user asked for.

### Consequences

- Good, because a provider's re-put of a stable id works after trash, restore
  and purge alike, and a deleted `default` input can be written again.
- Good, because no `former_ids` reservation, no fold-time release and no
  backfill exist; the sweep leaves nothing behind.
- Bad, because a `<kind>/<id>` pair held outside the repository (a note, an
  agent's memory) may resolve to a different record than it was written for,
  once the original is purged and the id taken again. The docs say so
  ([data-model](../data-model.md), [projection](../projection.md),
  [api](../api.md)), corrected in
  [#455](https://github.com/geoah/substrate/pull/455).
- Neutral: the one race #394 found is fixed on its own in
  [#456](https://github.com/geoah/substrate/pull/456): `gcPass` takes the
  record's advisory lock before its row lock, so a `put` racing the sweep
  either restores the tombstone or creates a fresh record, and neither side
  errors. The former-id message no longer says "never reused"
  ([#457](https://github.com/geoah/substrate/pull/457),
  [#460](https://github.com/geoah/substrate/pull/460)).

### Confirmation

`TestGCSweepSerializesWithPutAtSameID`
(internal/engine/gc_record_lock_db_test.go) holds both orders of the race:
`put, delete, gc, put` yields a fresh record at version 1, `put, delete, put`
a restored one at version 3.
`TestPutResurrectsATombstone` holds the restore before the sweep, and
`TestFormerIDNamesItsWinner` holds that a merge loser's id still points at its
winner. Nothing tests that a reservation is absent, because there is nothing to
test.

## More Information

This is the record 0055's More Information pointed at #372 for: retired
declaration names are reserved by an explicit `retired:` block; purged record
ids are not reserved at all, and no marker is planned. PR #394 is closed
unmerged. Reopen trigger: the owner asking for a reservation on a named kind,
which would need the exemption question #394 raised (blobs, provider ids,
`default`) answered first.
