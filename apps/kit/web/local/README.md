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
link.linked.subscribe(() => render(link.linked.getSnapshot()))
await link.outbox.write(RecordWrites.create(kind, uuidv7(), { name: "Milk" }))
```

`@substrate/react`'s `SubstrateApp` does this for an app.

## The outbox

Every write is kept before `write` resolves, and sent in order when the
server can be reached, as the Swift kit's `Outbox` and `RecordWrites`:

| Write                 | Sent as                                                                       | On a conflict                                                           |
| --------------------- | ----------------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| `RecordWrites.create` | `PUT` with `ifVersion: 0` at a device-minted id                               | its earlier try landed: sent again as a `PUT` that merges, the same row |
| `RecordWrites.edit`   | `PATCH` on the version the owner saw, moved along the app's own landed writes | parked as a conflict: Keep My Edit or Use Theirs                        |
| `RecordWrites.change` | `PATCH` on the version the device last saw                                    | read again, sent once more on top                                       |
| `RecordWrites.delete` | plain `DELETE`, replacing the record's waiting writes                         | none; one already gone is done                                          |

The network failing, a 5xx or a 429 stops the run and tries again with
backoff (a fifth of jitter, `Retry-After` honoured); the browser's online
event and the page coming to the front send at once. A 401 stops it as
`signedOut`, a missing scope as `needsApproval`, and nothing is lost. A
refusal of the write itself (a validation error, a record gone) parks it
with the server's words and holds back that record's later writes, never
another's, until the owner retries or discards it. One tab sends at a time
(Web Locks) and every tab hears the others' changes (BroadcastChannel).

## The cache and the linked records

`LinkedRecords` shows what the cache kept at once, then opens the watch,
lists after its bookmark (an app token's lists carry no head, decision
0195), reads again any record that changed during the list, and follows
the feed. The cache keeps the cursor with the records and the rows it moved
past in one transaction, so a reload resumes where it was and reads again
what it had not; a 410 or a `compacted` frame relists. The outbox's waiting
writes are laid over the records (`foldRecords`), a parked one not, so a
change made offline shows at once and stays shown until the outbox says it
landed; its snapshot names the records with a write `pending` and those
`parked`.
