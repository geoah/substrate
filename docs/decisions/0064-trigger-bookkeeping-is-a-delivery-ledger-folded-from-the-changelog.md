---
status: accepted
date: 2026-09-08
decision-makers: George Antoniadis
---

# 0064. Trigger bookkeeping is a delivery ledger folded from the changelog

## Context and Problem Statement

A trigger's bookkeeping lived in four Postgres tables the changelog never
described: `trigger_cursors`, `trigger_schedule`, `trigger_failures` and
`paged_cursors`. [0051](0051-a-repository-directory-is-the-backup-unit.md)
made the repository directory the backup unit and accepted that this state
"does not come back": a directory imported into an empty database started
every record trigger at the head, reset every schedule to the restore time,
lost every parked failure the retry API could name, and restarted every paged
drain at page one ([#366](https://github.com/geoah/substrate/issues/366)).
Separately, `recordRun` wrote a delivery's run record in a transaction after
the one that committed its effects and cursor, so a crash between the two
left effects with no completion recorded. The owner's conditions: one
recoverable ledger in the same durable transaction groups as the effects it
acknowledges, no side file, the window closed, `trigger_failures.id`
surviving a restore, and a parked payload that keeps no credential and holds
its body as referenced bytes.

## Considered Options

- Fold effects on an internal changelog entry: every motion of the four
  tables is a fold effect, recorded on a `delivery` entry appended in the
  transaction that commits the effects, hidden from every public read, and
  replayed by the same fold a rebuild drives.
- A side file mirrored after commit, like `sealed/`.
- Record the acknowledged position only, and also record the scan position
  past unmatched rows.
- Keep the tables runtime-only and document the loss (the 0051 position).

## Decision Outcome

Chosen: the fold effects on a `delivery` entry. Seven effects (`cursor`,
`schedule`, `park`, `unpark`, `page`, `unpage`, `forget`) are applied by
`foldOne` like every other effect, so `fold.go` stays the one path from the
changelog to the tables, and `RebuildRepository` and the boot import clear the
four tables with the rest of the fold and replay them. The entry rides
`settleDelivery` in the transaction whose effects it acknowledges, so
[0057](0057-a-changelog-line-names-its-transaction-and-an-unfinished-one-is-cut-whole.md)'s
frame keeps them together in the file, and the durable-write protocol of
[0062](0062-a-write-is-on-disk-before-its-commit-and-its-final-newline-is-the-commit-marker.md)
covers them without new code. The run record
moves into that same transaction (`putSystemRun`), which closes the window: a
delivery's effects, cursor motion, delivery entry and run record share one
`txn`. A parked failure's id is the seq of the delivery entry that parks it
(`reserveSeq`), unique per repository, monotonic and reproduced by replay; the
table's key becomes `(repository, id)` (migration 0022) and legacy serial ids
are negated so no future seq can collide with them. The changelog dialect
steps to 6.

The side file was rejected because it is a second format with `mirrorAfterCommit`'s
lag and its own consistency problem, and because a fold effect inherits the
transaction frame, the checksum, the import and the rebuild without new code.
Recording the scan position was rejected because a trigger reading a batch
that matched nothing would append an entry per pass, which every other
trigger would scan past and record in turn: unbounded growth from no user
write. The scan position (`advanceCursor`) stays Postgres-only between edits, at or above the acknowledged position and
never past an undelivered match, so a restore resumes from the last
acknowledged delivery and re-reads rows that deliver nothing under the source
that scanned them. The one case where re-reading is not nothing is a trigger
whose definition changed after the scan: widened to a kind it excluded, a
replay from the acknowledged position would deliver a change the live trigger
scanned past, an effect that never occurred. So every write that changes a
trigger record (its source, guard or `enabled` flag, by hand or by the
install or upgrade of the bundle that declares it) pins the current scan
position as a `cursor` effect in that same transaction
(`pinTriggerCursor`), and a replay applies the new definition only from the
edit on. The scan advance itself is fenced on the trigger record's version
the pass loaded, so a pass that scanned under the old source cannot land its
advance after the pin.

An agent delivery cannot settle in one transaction: the loop's writes are
many transactions across model turns. It is CLAIMED before the loop, in one
transaction that moves the cursor or fire state and records the delivery as
in flight under a parked failure (whose id is the claim's own entry), and
COMPLETED inside the transaction that settles the thread, which retires the
claim with the run record beside the thread's terminal status
(`settlement.claim`, `settlement.complete`, `agentInvocation.complete`). A
failed attempt of the same dispatch runs again under its claim; the final
park rewrites it with the error. A claim is the failure row carrying the
in-flight error and nothing else: an older park of the same change is not
one, so a replay that reaches a parked change parks it again with the cursor
motion and moves on, and a claim another dispatch holds (a replayed pass
under a running loop) is skipped with the cursor moved past it and a skipped
run naming it. The failure id is also held in this process, by a
compare-and-swap insert taken inside the transaction that writes a claim and
before a retry runs anything, and released when the completion, the park or
the retry ends: two hands on one failure cannot both start a loop, and the
second answers conflict. A crash mid-loop leaves the claim listed under the trigger's
parked failures as in flight, retried by hand: the loop's effects never
commit without a recorded delivery state, and nothing redelivers by itself.
The named exception to the one-transaction rule is the loop's own tool
writes, each its own transaction across model turns: a retry after a crash
repeats them, and they stay at-least-once, made safe by the function body's
own id composition (the idempotency key the tool is handed derives from the
delivery identity, not the attempt, so a body that keys on it composes the
same ids). The change reads as `parked` in the
feed's trigger chips while an agent runs. A retry holds its failure row
`FOR UPDATE` when it retires or rewrites it, and a row another retry retired
answers not found, so two hands retrying one failure repeat its effects on
the tables once.

Two behaviors change with the ledger. A restored schedule keeps the occurrence
it last fired and drains the ones it missed, oldest first and at most
`scheduleDrainPerPass` (10) per pass, where missed occurrences used to
coalesce to the newest one: the owner asked that outstanding occurrences not
be discarded and that the drain be bounded. A parked webhook's payload keeps
only the headers a replay needs, by exact name and never by pattern
(`parkedHeaderNames`): the ones that describe the body (`content-type`,
`content-length`, `content-encoding`, `user-agent`, `date`) and the ones the
providers whose webhooks the shipped kinds receive identify or sign a
delivery with (GitHub, Stripe, Slack, Linear, Standard Webhooks,
`idempotency-key`, `x-request-id`, the Pebble sample's `x-pebble-mode`).
Every other header is dropped, and so is the whole query string, which no
shipped body reads and where a sender that cannot set headers puts its
credential; a provider whose header is missing is added by name. The webhook
arm declares no header of its own, its one field being the substrate's `key`,
which the door checks and never forwards. The payload stores the body, and
every inline value of a multipart request (a form sender puts a token there as
readily as a transcript; the file parts were already digests), in the blob
store under the webhook actor,
referenced by digest: the changelog is append-only history nothing can
scrub, and blob bytes are plaintext in the directory
([0031](0031-blob-bytes-outside-postgres-are-stored-plaintext.md)), held by
the parked failure against the orphan sweep and collected once it retires.
The retry reads the body back and delivers the request without the dropped
headers.

### Consequences

- Good, because a directory copied into an empty database brings every
  trigger back where it stood, with the failure ids the retry API handed out.
- Good, because a function delivery's effects, completion and
  acknowledgement are one transaction (`settlement.settle`), and an agent
  delivery's acknowledgement precedes every effect it commits.
- Bad, because an agent delivery interrupted mid-loop is not redelivered by
  itself; it waits under `…/parked` for a hand.
- Bad, because the changelog dialect breaks again: a repository this release
  writes is refused by every earlier binary at open.
- Bad, because a directory written before dialect 6 and never opened under
  this release carries no ledger: restored into an empty database, it starts
  its triggers at the head. A repository that is opened under this release
  records its legacy rows as ledger entries at that first open
  (`adoptLegacyLedger`), so a rebuild keeps them.
- Bad, because a trigger with legacy bookkeeping meets `(repository, id)` on
  its failures with negated ids, an odd shape in `…/parked` until they retire.
- Bad, because a schedule that was down for a long time now fires every
  missed occurrence, over passes, where it used to fire one.
- Bad, because the scan position is not reproduced between edits: after a
  rebuild or a restore a trigger re-reads the unmatched rows since its last
  delivery or edit once.

### Confirmation

`TestARestoredRepositoryResumesItsDeliveries` (internal/engine) delivers a
record change, fires one overdue occurrence, parks a paged drain and a webhook
with a body and a credential-shaped header, imports the directory into an
empty database and asserts the cursors, the fire state, the failure ids, the
resume row, the retries, the hidden feed and the rebuild.
`TestADeliveryCommitsEffectsCursorAndRunTogether` fails the settlement and
shows neither effects nor run record committed, then shows one transaction
group for a retry and for a dispatched delivery.
`TestAnAgentDeliveryIsClaimedBeforeItsLoopAndCompletedAfter` fails the
completion of an agent delivery and shows the claim below the loop's first
write, no redelivery, and a retry completing it.
`TestConcurrentRetriesOfOneFailureRetireItOnce` holds the retry's row lock.
`TestARestoredTriggerAppliesAnEditedSourceFromTheEdit` widens a trigger past
a change it scanned past and shows the restore does not deliver it.
`TestAReplayOverAParkedChangeParksItAgainAndMovesOn` and
`TestAClaimAnotherDispatchHoldsIsSkipped` hold the claim rules, and
`TestAFirstOpenAdoptsLegacyTriggerBookkeeping` adopts a repository's legacy
rows in one transaction and plants a legacy payload with a credential in every
position, showing none reaches the ledger.
`TestScheduleFiresMissedOccurrencesInOrder` holds the bounded drain,
`TestALedgerMotionTakesTheChangelogFirst` the lock order,
`TestAScanAdvanceLosesToATriggerEdit` the version fence on the scan advance,
`TestARetryOfARunningClaimAnswersConflict` and
`TestConcurrentRetriesOfOneAgentFailureRunOneLoop` the in-process claim, and
`TestChangelogDialectCoversTheChangelogVocabulary` pins the seven effects and
the op to rung 6.

## More Information

This replaces the runtime-state consequence of 0051, whose record is already
superseded by [0052](0052-the-authority-is-the-repository-id.md) on the
directory's name; 0051's other choices stand. `oauth_flows` stays runtime
state: a consent flow interrupted by a restore is started again. A
repository's legacy bookkeeping is adopted into the ledger at its first open
under this release, in one transaction with the stamp that retires the step
and with every parked payload run through the parking policy first (a legacy
park held every header, the query and the body; the body's spool into the
blob store appends before that transaction, and the dialect stamp is held
until the adoption commits so a failure repeats the whole step),
so the "before dialect 6" gap is only a directory that was never opened
under it. Rows keyed by a callable identity rather than a trigger id, the
pre-wave-1 cursors a default trigger adopts at its creation
(`adoptableLegacyCursor`), belong to no live trigger and are not adopted: a
rebuild drops them, and a default trigger created after that starts at its
own creation. Reopen if the scan position ever needs to be reproduced (a
trigger whose unmatched tail is large enough that re-reading it once matters)
or if a second writer makes the changelog lock an insufficient serializer for
`reserveSeq`.
