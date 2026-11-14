# @substrate/local

What a Substrate web app keeps in the browser, per repository, with no
framework: the outbox its writes wait in, the record cache it opens from,
and the linked records its screens read. IndexedDB holds both, in one
database per app and repository (`substrate.<app>.<repository>`); where the
browser keeps nothing, an in-memory store stands in and says so.

```ts
import { RecordWrites, openRepository } from "@substrate/local"

const link = await openRepository({
  app: "template",
  repository,
  client,
  kinds,
  kept: "items",
})
link.start()
link.linked.subscribe(() => render(link.linked.kindSnapshot(kind)))
await link.outbox.write(RecordWrites.create(kind, uuidv7(), { name: "Milk" }))
await link.close() // or forget(), a sign-out
```

`@substrate/react`'s `SubstrateApp` does this for an app.

`close()` stops reading and sending, waits out the send in progress and
the cache's last save, and closes the database; every call answers the
same close. `forget()` closes, then deletes the database, and rejects with
`NotForgotten` when the browser would not: another tab of the app still
holds it (its `message` says to close the other tabs) or the deletion
failed. Until it resolves, what the browser kept is still there.

## The outbox

Every write is kept before `write` resolves, and sent in order when the
server can be reached, as the Swift kit's `Outbox` and `RecordWrites`:

| Write                 | Sent as                                                                       | On a conflict                                                    |
| --------------------- | ----------------------------------------------------------------------------- | ---------------------------------------------------------------- |
| `RecordWrites.create` | `PUT` with `ifVersion: 0` at a device-minted id                               | its earlier try landed: the row is read, and never written again |
| `RecordWrites.edit`   | `PATCH` on the version the owner saw, moved along the app's own landed writes | parked as a conflict: Keep My Edit or Use Theirs                 |
| `RecordWrites.change` | `PATCH` on the version the device last saw                                    | read again, sent once more on top                                |
| `RecordWrites.delete` | plain `DELETE`, replacing the record's waiting writes                         | none; one already gone is done                                   |

A create met by a record at its id counts as landed with version 1, the
version every record is created at: its properties are never sent again,
so a change another device made since stands, and an edit queued on the
create is sent at version 1 and parks as a conflict if the record moved.
`onLanded` hears each landed write with the record the server holds after
it, or null where it holds none: a delete, or a create whose row was found
gone since, which the linked records and the cache then drop.

The network failing, a 5xx or a 429 stops the run and tries again with
backoff (a fifth of jitter, `Retry-After` honoured); the browser's online
event and the page coming to the front send at once. A 401 stops it as
`signedOut`, a missing scope as `needsApproval`, and nothing is lost. A
refusal of the write itself (a validation error, a record gone) parks it
with the server's words and holds back that record's later writes, never
another's, until the owner retries or discards it. A 422 with the reason
`stale_revision` is dropped: a newer delivery of the record landed first.
From a server before that reason, a 422 at `props.revision` is dropped when
the stored record, read again, holds a later revision than the write
carries. One tab sends at a
time (Web Locks) and every tab hears the others' changes
(BroadcastChannel); a tab reads and writes the kept queue in the order it
asks, so a read another tab's change starts never puts back a write that
landed meanwhile.

## The cache and the linked records

`LinkedRecords` shows what the cache kept at once, then opens the watch,
lists after its bookmark (an app token's lists carry no head, decision
0195), reads again any record that changed during the list, and follows
the feed. The cache keeps the cursor with the records, the rows it moved
past and the kinds it listed under it in one transaction, so a reload
resumes where it was and reads again what it had not. A 410, a `compacted`
frame, or a kind the app shows that the cache never listed relists: the
kept cursor is dropped, everything is listed again, and the watch follows
from where that list began. That recovery happens at most once a minute:
another `compacted` within 60 seconds of the last recovery's start is a
failure, not another relist, whatever came between (a bookmark, the list
landing, a change). The status is `error` with words that say the records
are not being updated, the refused cursor is dropped, and it is tried
again, from a fresh list, when the page comes back or the network does.
After a minute with no refusal the next one is recovered again. The clock
is the `now` option, which a test steps. So is a list refused `compacted` even
after `listAll` started it over from the first page; a refusal part way
that the restart cures shows nothing. A save
that fails is kept and written with the next, or tried once more a few
seconds later while nothing changes, so the kept cursor never passes a
change the kept records lack. A record's read answered after its removal,
a list or one of the app's writes landed is dropped, and a read never
replaces a newer version.

`setKinds(kinds)` changes the kinds an open `LinkedRecords` reads without
closing it: a kind still read keeps its records shown, each the same
object; a kind left out goes from what is shown and kept; every kind is
listed again under a new watch, since a watch from the old cursor would
miss what changed in an added kind. The outbox is not touched, so a write
waiting for a kind that comes back is laid over its records again.
`SubstrateProvider` does this when its `kinds` change.

A list stops at `limit` records (10,000 unless the app says), and one that
stops with more left reads its kinds **in part**: `limitReached` says how
many it read, `hasListed` stays false, and the cache keeps the kinds as
read in part, so a reload does not take them for whole. A record not among
them is not known absent until `resolve(kind, id)` reads it by itself,
which `useRecord` does. What such a read found absent is forgotten when the
history changes (a new generation, a relist) or a list replaces the kinds,
and every listener hears it, so a screen still showing the record asks
again. A read that fails says why (`readError`) and is tried again by
itself a few times, after a growing wait (`readRetryDelays`), then when the
watch opens again or a screen asks (`retryRead`).

The outbox's waiting writes are laid over the records (`foldRecords`), a
parked one not, so a change made offline shows at once and stays shown
until the outbox says it landed. A screen reads what it shows, each
snapshot the same object until what it holds changes, so a change to one
record redraws only what reads it:

| Read                       | Answers                                                                                 |
| -------------------------- | --------------------------------------------------------------------------------------- |
| `kindSnapshot(kind)`       | the kind's records as the owner sees them, by id; a new map only when one of them moves |
| `recordSnapshot(kind, id)` | one record; a new object only when it moves                                             |
| `writeState(kind, id)`     | `"pending"`, `"parked"` (a refused write, holding back the rest) or undefined           |
| `hasListed(kind)`          | whether the kind's records are known whole, listed or kept, so one missing is absent    |
| `isAbsent(kind, id)`       | whether a record not shown is known absent: its kind whole, or it read and not found    |
| `readError(kind, id)`      | why a record's last read failed, until it is read                                       |
| `statusSnapshot()`         | the status, its words, `cached`, `limitReached`, and the pending and parked counts      |

`subscribe` hears every change once per turn. `getSnapshot()` is every
kind and the status at once, built when asked: a screen that shows one
kind reads that kind.
