---
status: accepted
date: 2026-10-15
decision-makers: George Antoniadis
---

# 0167. The webhook door answers `202` after a sealed spool write, and a drain records the request

## Context and Problem Statement

Under [0068](0068-an-accepted-webhook-is-a-pending-entry-in-the-delivery-ledger.md)
the door answered `202` only after its changelog transaction committed. That
transaction takes the repository's changelog lock, which background writers
hold for 2 to 4 seconds at a time in production. The Slack Socket Mode
sidecar gives substrate a few seconds per post, and 14% of its posts timed
out before #952 (the door goes ahead of queued background writers) and #954
(one transaction per delivery). Both shorten the wait; neither removes the
lock from the answer's path.

## Considered Options

- A file spool under the repository directory: the door writes the request
  sealed and fsynced, answers, and a drain records it in the ledger.
- A Postgres-only inbox table written before the answer.
- Keep 0068's rule and shorten the lock holds of every background writer.

## Decision Outcome

Chosen: the file spool, because it takes the changelog lock out of the
answer's path while keeping 0068's conditions. The Postgres-only inbox was
rejected by the owner, as it was in 0068: it fails the restore into an empty
database. Shortening every background writer is open-ended work and still
leaves the door behind whichever transaction holds the lock.

The door (`receiveWebhook` in `internal/engine/webhooks.go`) runs 0068's
checks, then `spoolWebhook` (`webhookspool.go`) writes the request to
`repositories/<authority>/spool/<ordinal>-<fire id>.webhook` through a
temporary file, an fsync, a rename and a directory fsync, and answers `202`
with the fire id it minted. A drain task (`kickSpoolDrain`), kicked by the
door after every write and by the dispatcher at every pass, records each
file in arrival order through the admission transaction of #954
(`admitWebhook`), under the fire id and receipt time the sender was given.
It deletes the file only after that transaction commits, then hands the fire
to the background. From the drain on, 0068 holds unchanged: the request is a
pending entry, the fire is its retry, and the dispatcher's resume runs what
a stop left pending.

The drain is idempotent. The admission transaction first asks, under the
changelog lock, whether a `delivery` entry above the head the door read at
spool time names the fire id (`webhookRecordedSQL`, on the trigger's indexed
record id), and records nothing if one does. A file whose commit landed and
whose deletion did not (a stop between the two, or a copy taken between
them) is then only deleted. The check is in the changelog, which a restore
carries, so it holds across processes and copies, not only within one.

How each of 0068's conditions holds:

- **A stop right after the `202`.** The file and its directory entry are on
  disk before the answer. The first dispatcher pass of the next process
  drains it. A server that runs no dispatcher drains at its door's next
  request, which kicks the drain over the whole spool.
- **A restore into an empty database.** `spool/` is in the repository
  directory. `repository snapshot` copies it with the server stopped and
  reads it back. The export lists and reads it under the writer mutex that
  pins its point, so a file in the archive is either absent from the
  archived changelog or found there by the restored drain. The files are
  sealed under the repository's DEK, which the manifest carries wrapped and
  `repository rewrap` keeps, so a same-key restore and a recovery-key restore
  both open them. One window remains: a live file copy (`rsync`) reads
  `changelog/` before `spool/`, so a request drained between the two reads
  is in neither. A snapshot or an export has no such window.
- **No credential in plaintext.** The door narrows the request to the form
  the ledger records (the body's headers and the trigger's declared ones,
  no query; the HTTP layer already drops `authorization` and `cookie`), then
  seals the whole entry, body included, under the DEK with AES-256-GCM,
  bound to the repository and the fire id. The drain's log line names the
  file and a fixed outcome word, never the error text.
- **A file spool, not a table.** No migration and no table.
- **Bounded.** 10,000 requests or 256 MiB of sealed bytes per repository.
  At either bound the door answers `503` with `Retry-After` and writes
  nothing. A drain that cannot record a request stops at it, retries with a
  backoff from 1 to 30 seconds, and lets the spool fill rather than skip it.
  A file that does not open or decode is renamed `<name>.unreadable` and no
  longer counted.
- **Observable.** `substrate_webhook_spool_entries`,
  `substrate_webhook_spool_bytes` and
  `substrate_webhook_spool_oldest_age_seconds` per repository, and the
  counters `substrate_webhook_spool_drain_failures_total` and
  `substrate_webhook_spool_refusals_total`.

### Consequences

- Good, because the sender's answer waits on two fsyncs and a few indexed
  reads, never on the changelog lock.
- Good, because the spooled request is sealed, where the ledger's copy of a
  body is plaintext in the blob store (0031).
- Bad, because between the `202` and the drain the request is in no table:
  `…/parked` does not list it, the trigger's `pending` count does not count
  it, and a hand cannot retry or forget it. The spool metrics are the only
  view.
- Bad, because a request the drain cannot record holds back every later
  request of its repository until it is fixed or removed by hand, and the
  door answers `503` once the bounds are reached.
- Bad, because a live `rsync` copy taken while the drain runs can miss a
  request that both reads straddled; snapshots and exports cannot.

### Confirmation

`TestAWebhookIsAnsweredWhileTheChangelogLockIsHeld` and
`TestASpooledWebhookIsNarrowedAndSealedBeforeItIsWritten` (internal
package), `TestASpooledWebhookSurvivesAStopBeforeItsDrain`,
`TestADrainThatFindsARecordedRequestOnlyDeletesItsFile`,
`TestTheWebhookDoorRefusesWhileTheSpoolIsFull`,
`TestASpooledWebhookHoldsNoCredentialInTheClear` and
`TestASpooledWebhookRestoresFromASnapshotAndAnExport` (internal/engine,
external package), `TestWebhookSpoolFullIsUnavailableWithRetryAfter`
(internal/api) and `TestWebhookSpoolGauges` (internal/metrics). 0068's own
tests still hold the ledger half.

## More Information

This supersedes the rule of 0068 that the door answers after the ledger
commit; the rest of 0068 (the pending entry, the fire as its retry, the
resume, the agent exception) stands, now starting at the drain. Reopen if a
second process writes one repository, or if senders need a spooled request
listed and retried by hand before it is drained.
