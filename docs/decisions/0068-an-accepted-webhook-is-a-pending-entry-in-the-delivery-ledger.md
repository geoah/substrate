---
status: accepted
date: 2026-09-08
decision-makers: George Antoniadis
---

# 0068. An accepted webhook request is a pending entry in the delivery ledger before its `202`

## Context and Problem Statement

The webhook door answered `202 {"fire": id}` once the fire was handed to the
background supervisor, and nothing about the request was durable until the
fire's effects committed: a process that stopped after the answer, or while
the fire ran (shutdown cancels after a ten second drain), dropped the
delivery, and only the sender's redelivery could bring it back
([#367](https://github.com/geoah/substrate/issues/367)).
[0045](0045-a-webhook-trigger-is-a-public-endpoint-with-an-optional-key.md)
named the gap and deferred it ("a durable inbox is a later step").
[0064](0064-trigger-bookkeeping-is-a-delivery-ledger-folded-from-the-changelog.md)
then gave a parked webhook a durable form: a failure row folded from a
`delivery` entry, its body spooled to the blob store by digest, only an exact
allowlist of headers kept and the query dropped. The owner's conditions: the
request survives both a stop right after the `202` and a stop during the
fire, including a restore into an empty database; only the headers the
callable needs are kept and no credential enters plaintext history; the body
is referenced bytes; a delivery that parks is still retried by hand.

## Considered Options

- Record the admitted request as a parked failure carrying a `pending`
  error, in the ledger, before the `202`; the fire is that row's retry.
- A new fold effect and table (an inbox), with a changelog dialect step.
- A Postgres-only inbox table outside the ledger.
- Keep accept-and-dispatch and rely on the sender's redelivery.

## Decision Outcome

Chosen: the parked failure with a pending error. After the key check and the
part spool, `admitWebhook` runs the built envelope through the parking policy
of 0064 (`parkedEnvelope`) and writes it as a failure row whose `last_error`
is `pendingWebhookError`, with the fire id and the receipt time, on a
`delivery` entry of its own whose seq is the row's id, through `inTx`. The
door answers once that transaction has committed, which
[0062](0062-a-write-is-on-disk-before-its-commit-and-its-final-newline-is-the-commit-marker.md)
makes durable. The fire then runs as the row's retry: its settlement carries
`retire`, so the transaction that commits the effects retires the row
(`unpark`) and writes the run record, and a fire that fails parks by
rewriting the same row with the error and the attempt count. Nothing new is
stored: the same row holds an accepted delivery, a running one and a parked
one, the same `parkedBlobsSQL` hold keeps its blobs past `BlobUploadGrace`,
and a rebuild or an import reproduces all three states from the entries.

Recovery runs on the trigger dispatcher's pass (`ProcessTriggers`):
`resumeWebhooks` reads the rows still carrying the pending error, oldest
first, and runs each fire under its original id in one detached task,
serially, one walk at a time. The pass is the moment, and not the repository
open, because only `substrated` dispatches: an operator's process (`repository
rebuild`, `user reset`) opens the repository and must fire nothing, while the
server's first pass after a restart or an import runs what is pending. Each
row is read again right before its fire, so one a hand retired since the
walk began is left alone; the fire holds the row in `runningClaims` before
anything runs, by compare-and-swap (`settlement.acquire`), so a walk racing
the door's own spawn or a hand's retry loses the swap and starts nothing, a
retry by hand of a row this process is running answers `409`, and a fire
whose row is gone at its first settlement stops (`errFailureRetired`) rather
than running the callable again. A row whose trigger is disabled or does not
resolve stays pending, listed under `…/parked`, until a pass finds the
trigger running again or a hand retries it.

`GET …/triggers/status` counts these rows as `pending`, not `parked`: an
accepted request that has not settled is the door working, and the parked
count keeps meaning what it meant. The `…/parked` list still shows them,
with `lastError` saying they are pending, because that list is where the
retry-by-hand verb reads its ids.

An agent callable's fire is the named exception. Its claim
(`settlement.claim`) rewrites the pending row to `inFlightError` before the
loop, so a process that dies mid-loop leaves a claim and not a pending entry:
the resume does not run it, and a hand retries it, which is 0064's rule for
every interrupted agent delivery. A function fire has no such window: its
effects and the row's retirement are one transaction, so a stop before the
commit leaves the row pending and the resume runs the fire again from
nothing.

Retention: a settled entry is deleted with the `unpark` effect, so nothing
accumulates in the table; the changelog keeps the entry that recorded the
request and the one that retired it, as it keeps every delivery entry. The
body's blob is an orphan once the row is gone and the sweep collects it after
the grace unless a record references the digest.

The new effect and table were rejected because the ledger already had the
row, the hold, the hiding from every public read and the rebuild, and a
second store for the same request would need all four again plus a dialect
step. The Postgres-only table fails the fresh-database restore. Redelivery by
the sender was the standing position and is what this record retires.

### Consequences

- Good, because a `202` names a request the repository holds, and a stop
  after it, before the fire or during it, loses nothing.
- Good, because the request lives in the directory: a restore into an empty
  database resumes it, body and file parts included.
- Good, because the changelog dialect does not move: no new op, no new
  effect.
- Bad, because an accepted request is listed under the trigger's parked
  failures while it runs, with `lastError` saying it is pending, and a reader
  of that list has to tell the two apart by it.
- Bad, because the door does one more transaction per request before it
  answers: a blob manifest for the body and a `delivery` entry.
- Bad, because a pending row whose trigger is disabled or whose callable does
  not resolve waits under `…/parked`, and nothing says so but the log and the
  `pending` count.
- Bad, because a request accepted by a process that dispatches nothing (a
  server started without the trigger loop) is never fired by that process.
- Bad, because an agent webhook interrupted mid-loop is not resumed by itself,
  where a function one is.

### Confirmation

`TestWebhookAcceptedRequestSurvivesAStopBeforeTheFire`,
`TestWebhookFireCancelledMidRunResumesAtTheNextOpen`,
`TestWebhookAcceptedRequestRestoresIntoAFreshDatabase`,
`TestWebhookPendingEntryRefusesAHandRetryWhileItRuns`,
`TestWebhookPendingEntryRetiredByAHandIsNotResumed` (internal/engine,
external package) and `TestAnAgentWebhookInterruptedMidLoopWaitsForAHand`
(internal package) hold each sentence above.
`TestWebhookParkedRetryReplaysRequest` and
`TestARestoredRepositoryResumesItsDeliveries` hold the park of a fire that
fails and its retry by hand.

## More Information

This replaces the last consequence of 0045, "the window between the `202`
and the fire starting is lost on a crash; a durable inbox is a later step";
the rest of 0045 stands. Reopen if a second writer process shares one
repository, when the open-time resume and `runningClaims` stop being a
sufficient serializer, or if the pending row's presence under `…/parked`
needs its own listing.
