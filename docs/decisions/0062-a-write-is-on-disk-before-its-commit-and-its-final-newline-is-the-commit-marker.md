---
status: accepted
date: 2026-09-08
decision-makers: George Antoniadis
---

# 0062. A write is on disk before its commit, and its final newline is the commit marker

## Context and Problem Statement

[0050](0050-the-changelog-is-checksummed-segment-files-and-postgres-indexes-it.md)
made the Postgres transaction the commit point and had the repository's one
writer append the committed lines after it. A failure there was latched for
later writes while the request that met it returned success
(`commitAndMirror`,
[#364](https://github.com/geoah/substrate/issues/364)), and the three
sealed-store writers outside `inTx` did the same. `docs/operations.md`
promises that nothing in the database is needed to restore a repository, so an
acknowledged write could be absent from every backup. The owner's condition
on the fix: a crash between an append and its commit must not replay as a
committed write, and the frame of
[0057](0057-a-changelog-line-names-its-transaction-and-an-unfinished-one-is-cut-whole.md)
proves only that a transaction's bytes are complete, not that Postgres
committed them.

## Considered Options

- Keep commit-then-append and return the append's error.
- Append the whole transaction before the commit; the boot imports a file
  ahead of its table.
- Append before the commit, commit, then write a commit marker: a file
  naming the committed head, or a commit line.
- Prepare before the commit: every byte of the transaction but its last
  newline, then the Postgres commit, then the newline.
- For the sealed files: write each file in place before the commit and put
  the old one back on failure; or stage each under a pending name and rename
  it into place after the commit.
- Two-phase commit in Postgres (`PREPARE TRANSACTION`).

## Decision Outcome

Chosen: prepare before the commit, with the transaction's own final newline
as the commit marker for the lines and a rename as the marker for each sealed
file. Under `writerMu` a write stages its sealed files (each payload written
and fsynced under `sealed/<file>.json.pending`, the record's own file
untouched), then writes and fsyncs its changelog lines without the last
line's newline, then commits in Postgres, then renames the pending files
into place, then writes and fsyncs that newline, then removes the sealed
files whose rows it deleted (`commitAndMirror`, `changelogfile.Writer.Prepare`,
`Commit` and `Abort`, `changelogfile.StageSealed`, `CommitSealed` and
`DiscardSealed`). The reader of 0057 already cuts a final line with no newline
together with every complete line of its transaction, and `ReadSealed` lists
only the records, so neither a tail nor a pending file Postgres never
committed is read by anything: not by the boot, not by `repository verify`,
not by an import into an empty database. The three writers outside `inTx`
(`updateCredential`, `deleteCredentialsFor`, `consumeTOTPStep`) commit
through the same steps (`commitSealed`).

Recovery, step by step. A failure before the Postgres commit rolls back: the
pending files are discarded, the prepared bytes are cut, the transaction
rolls back, and the caller gets `ErrDirectoryWrite` with nothing durable
anywhere, except that a changelog writer which failed refuses every prepare
until a restart, so that failure latches `ErrChangelogFileBehind` instead. A crash between the stage or the prepare and the commit leaves a
tail with no newline and pending files; the next open cuts the tail whole and
drops the pending files once the records are written from the table, and an
import into an empty database loads each record's own file, which is the
payload the table held. The client had no acknowledgement and no store holds
the write. A crash between the commit and the renames or the newline leaves
the table ahead of the directory by that transaction: the boot rewrites the
records from the table, drops the pending files and appends the transaction
again, which is why the catch-up and the latch stay. A failure of a rename,
of the newline or of a sealed delete after the commit is latched
(`ErrChangelogFileBehind`) and returned: the tables hold a write the caller
was told failed, and the restart heals the directory from them. That window
is a rename and one byte's write, and this design cannot close it without a
second commit point.

Commit-then-append with the error returned was rejected because an error
after an irreversible commit tells the client "no" about a write that
happened. Appending the whole transaction before the commit was rejected
because a crash in the window makes the boot import a write nobody
acknowledged. A marker file costs a second fsync and a rename per write and
is a second thing the reader must reconcile with the segments; a commit line
was rejected in 0057, because a finished segment's last seq is its newline
count. Writing a sealed file in place before the commit was rejected because a
sealed file has no marker of its own: a crash before the commit would leave
a payload an import into an empty database loads, a token or a spent TOTP
step nobody acknowledged, and putting the old file back on a failure needs
the old file read first. Two-phase commit needs `max_prepared_transactions`
above Postgres's default of zero and keeps a prepared transaction's locks
across a crash.

### Consequences

- Good, because an acknowledgement means the directory holds the write: a
  copy of the directory taken after the response holds every acknowledged
  write.
- Good, because a write the sealed store refused is retryable: the caller
  sees `ErrDirectoryWrite`, an `ErrUnavailable`, and neither store took it. A
  write the changelog writer refused is not, because the writer stays failed
  until a restart, and it is latched as `ErrChangelogFileBehind` for that
  reason.
- Good, because no format changed: a tail without a newline is what 0057's
  reader already cuts, so a directory this release writes opens under the
  release that carries 0057.
- Bad, because every write pays two fsyncs of the segment instead of one and
  each sealed file a staged write plus a rename, and a failed commit has
  pending files to discard.
- Bad, because the window after the commit (the renames, the newline, the
  sealed deletes) still exists: a failure there is reported as an error for a
  write the tables hold, healed at the next restart.
- Bad, because a copy taken while a write is between its stage and its
  commit holds a tail that `verify` counts as truncated and a pending file it
  reports: the finding a torn line gave, now on every write in flight.
- Bad, because a failure after the commit returns before `inTx` runs the
  write's after-commit work: no watch signal, no change-sink entry, no
  `afterCommit` hook (the changelog dialect stamp's flag among them). The
  dataset is latched at that point, so nothing writes again before the
  restart that re-derives all of it.
- Bad, because a commit whose answer is lost after Postgres committed is
  treated as a failure: the lines are cut and the pending files discarded. A
  transaction with lines has the caller told "retry" about a write that
  happened; the table is then ahead, so the next write's prepare meets a seq
  gap and latches instead of refusing as retryable (`prepareLines`), and the
  caller learns the true outcome one write late. A sealed-only transaction
  has no seq to catch it, so its commit error latches at once and the
  record's file stays the old one until the boot rewrites it from the table.

### Confirmation

`TestAPreparedTransactionIsCutUntilCommitted`,
`TestAPreparedTransactionLeftByACrashIsCutAtTheNextOpen`,
`TestAbortLeavesTheSegmentAsItWas` and `TestCommitFinishesTheSegmentWhenDue`
(internal/changelogfile) hold the writer, and
`TestAStagedSealedFileIsNotARecordUntilCommitted` the sealed files. In
internal/engine, `TestAWriteIsRefusedWhenTheDirectoryCannotTakeIt` holds the
refusal of a put, a credential update and a TOTP consume with the changelog
writer and then the sealed store broken, and that an import of the directory
holds exactly the acknowledged set;
`TestACrashBetweenPrepareAndCommitFoldsNoUncommittedLine`,
`TestACrashBetweenTheSealedStageAndTheCommitRestoresTheOldPayload` and
`TestACrashBetweenCommitAndTheNewlineIsCaughtUpAtTheNextOpen` hold the crash
windows on both halves of the directory, the second against an import into
an empty database; `TestAStageThatFailsAfterItsRenameLeavesNoPendingFile`
holds the stage that fails after its rename, and
`TestACommitInDoubtLatchesUntilTheBootCatchesUp` the commit whose answer was
lost, `TestASealedOnlyCommitInDoubtLatchesUntilTheBootRewritesTheFile` the
same for a transaction with no lines, and
`TestAReadOnlyServiceRefusesToSpendATOTPStep` that a process with no writer
commits no sealed row either. `TestRESTErrorEnvelopeMapping` (internal/api) holds the rolled-back
refusal at 503 and the latched one at 500.

## More Information

Amends 0050's write path (the writer "appends the committed lines after
commit", and the consequence that a write reaching the table and not the file
is caught at the next open) and 0057's recovery paragraph, which had the file
at most one transaction behind the table: it can now also be one prepared
transaction ahead, and that transaction is cut. 0050 keeps its `accepted`
status for the reason 0057 gave: it is the successor of 0009, 0010, 0011 and
0018. Retry after a lost response is
[#378](https://github.com/geoah/substrate/issues/378). Reopen if a second
commit point ever exists (a marker file, a second writer): the newline
carries the commit only because there is exactly one writer and its Postgres
transaction is the one thing it waits on.
