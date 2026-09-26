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

A put that names the subject slot keeps that subject: the probes run only
when the put leaves the slot out. To resolve a Google contact mirror again
after the `googlecontactperson` mapping's probes improve, purge the mirror
and put it back without the `person` slot. Until the mirror is written
again, the person loses the names, emails and phones only this contact gave
it.

For a few contacts, put each one back yourself. Read the record first and
send its `properties` with `person` removed. `PUT` takes only
`{"properties": ...}`, so sending the whole document the `GET` returned
(with `version`, `createdAt` and `updatedAt`) answers `400 bad_request`:

```http
GET    /api/v1/providers.substrate.reamde.dev/google/contact/c1
DELETE /api/v1/providers.substrate.reamde.dev/google/contact/c1?purge=true
PUT    /api/v1/providers.substrate.reamde.dev/google/contact/c1
       {"properties": {"account": {"ref": "..."}, "resourceName": "people/c1", "emailAddresses": [...]}}
```

Waiting for the next scheduled sync does not bring the contact back: that
run reads only the People changes since the stored `contactsSyncToken`, so
it writes a purged contact again only once the contact changes upstream. To
make the connector write it, purge the mirror and then stamp
`syncRequestedAt` on the account the contact's `account` ref names:

```http
DELETE /api/v1/providers.substrate.reamde.dev/google/contact/c1?purge=true
PATCH  /api/v1/providers.substrate.reamde.dev/google/account/<account-id>
       {"properties": {"syncRequestedAt": "2026-09-26T12:00:00Z"}}
```

The request drives a full read on every enabled stream of that account
(contacts, Gmail, Calendar and Drive), not only contacts. The contacts read
writes each purged contact as a fresh record, and the probes resolve it.
Use it when many contacts were purged, and budget for the other streams'
full reads.

The CLI covers the delete only,
`substratectl delete --purge providers.substrate.reamde.dev/google/contact c1`.
A document from `substratectl get -o yaml` carries `data.properties.person`,
so remove that line before `substratectl apply -f`, or the contact points at
the old person again.
