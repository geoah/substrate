---
type: feature
---

# `DELETE ?purge=true` collects a record now, so the next `put` is fresh

A plain `DELETE` leaves a tombstone until the garbage collector's next pass,
and a `put` at the same id in that window restores the row with every
property it held: a mirror's old subject, an account's `lastSyncedAt`,
cursors and `syncStatus`. `purge=true` collects the record in the delete
itself, so the next `put` at the id is a new record at version 1, and a
mapping source resolves its subject again through the probes. The old
subject is not deleted or re-pointed. A record a finalizer holds answers
`409 conflict` and nothing changes: delete it without `purge`, wait for the
finalizers to release, then purge. A purge through a merge loser's former id
answers `409 conflict` naming the canonical id, and a declaration record
answers `422 validation`.

To resolve a Google contact mirror again after the `googlecontactperson`
mapping's probes improve, read the record first, purge it, then put the same
document back:

```http
GET    /api/v1/providers.substrate.reamde.dev/google/contact/c1
DELETE /api/v1/providers.substrate.reamde.dev/google/contact/c1?purge=true
PUT    /api/v1/providers.substrate.reamde.dev/google/contact/c1   (the document the GET returned)
```

A `PUT` with an empty body writes an empty mirror: the probes match nothing
and the mapping creates a new subject. The CLI covers the delete only,
`substratectl delete --purge providers.substrate.reamde.dev/google/contact c1`;
put the document back with `substratectl apply -f`, or let the next sync
write it.
