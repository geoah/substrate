---
status: accepted
date: 2026-09-26
decision-makers: George Antoniadis (via the #585 agent session)
---

# 0107. A delete may purge, so the next put starts the record over

## Context and Problem Statement

A `put` onto a tombstone restores the row with every property it held, and a
write may not re-point a mapping's subject slot; merge and split are what move
it ([api](../api.md#the-five-mutations)).
Together they mean a mirror cannot be resolved again after its mapping's
probes improve: deleting and re-putting it brings the old subject back, and
an account row deleted and re-put brings back `lastSyncedAt`, its cursors and
`syncStatus`, so the on-connect trigger never fires
([#585](https://github.com/geoah/substrate/issues/585)). The ways out were
waiting up to five minutes for the collector's sweep after the delete, or
deleting the subject too, which is wrong for a subject with history.

## Considered Options

- A `put` onto a tombstone starts from an empty row: only the document's
  properties land, and the subject slot resolves again.
- `delete` takes `purge`: the record is collected in the delete's own
  transaction, and the next `put` at the id is a create.
- A write may clear a subject slot to `null` when no other source points at
  the current subject.

## Decision Outcome

Chosen: `delete` takes `purge`, because it is additive and leaves the common
restore alone. A provider that trashes and restores a contact upstream
re-puts the same id, and that restore keeps pointing at the person the
contact always had. Under the first option it would resolve again, and a
person whose values came from that one contact, emptied by the tombstone's
recompute, would match nothing: the restore would mint a second person and
strand every record pointing at the first. The third option adds a rule to
the subject guard for one case and does nothing for the account rows.

A purge is the collector's pass run early: `onDelete: cascade` children are
tombstoned for the sweep, then the row and everything hanging off it are
hard-deleted, and the changelog records a `gc` entry with reason `purged` by
the caller. A record a finalizer holds refuses the purge with `409` and the
whole delete rolls back, because the hold is somebody else's to release: the
caller deletes without purge, waits for the finalizers to release, then
purges. A purge addressed through a merge loser's former id refuses with
`409` naming the canonical id, as a `put` there does, because the former id
resolves to the winner and a purge cannot be undone. A declaration record
refuses `purge` with `422 validation`: it leaves through admission.
Re-resolving one identity is then `delete ?purge=true` on the mirror and a
`put` of it; the old subject is not deleted or re-pointed, and keeps its
history.

### Consequences

- Good, because a re-seed and a re-resolution are two requests per record,
  and neither touches a subject another record may point at.
- Good, because a plain delete and put still restores, as
  [0072](0072-reserve-a-purged-record-id-against-reuse.md) relies on.
- Bad, because a purge is not reversible: the tombstone a plain delete leaves
  is the undo, and a purge skips it.
- Bad, because the old subject is not deleted or re-pointed; if nothing
  else points at it, it is an orphan for
  [0092](0092-an-orphaned-mapping-target-is-marked-and-collected-on-request.md)
  to collect.
- Bad, because a held record needs the holder's release first, so a purge of
  a connected account waits for its teardown.

### Confirmation

`TestPurgeThenPutStartsARecordOver`, `TestPurgeRefusesAHeldRecord`,
`TestPurgeRefusesADeclarationRecord`, `TestPurgeRefusesAFormerID`,
`TestPurgeTombstonesCascadeChildren`, `TestPurgeUnderIfVersion`
(internal/engine/core_db_test.go) and `TestPurgedSourceResolvesItsSubjectAgain`
(internal/engine/mapping_db_test.go) hold the engine; `TestRESTDeletePurge`
(internal/api/precondition_test.go) the wire, and `TestDeletePurge` the
`substratectl delete --purge` flag.

## More Information

Reopen trigger: a caller that needs a door that re-runs a mapping's
resolution over many records at once, which is the other ask in #585 and
would be a bulk verb, not a delete option.
