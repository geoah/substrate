---
status: accepted
date: 2026-09-08
decision-makers: George Antoniadis
---

# 0057. A changelog line names its transaction, and an unfinished transaction is cut whole

## Context and Problem Statement

One Postgres transaction appends several changelog lines (a merge writes the
loser's tombstone and the winner's entry in one commit; registration seeds the
whole `core` package in one), and the writer lands them as one `write()` and
one `fsync()`.
[0050](0050-the-changelog-is-checksummed-segment-files-and-postgres-indexes-it.md)
named the only damage a reader accepts as "a torn final line", so a power loss
between the write and the fsync left every complete line of the unfinished
transaction as history: a three-line transaction cut inside its third line
opened at head 2, and a new writer continued from 3
([#363](https://github.com/geoah/substrate/issues/363)). Neither the line nor
the `changelog` table recorded where a transaction ended, and the boot's
table-to-file catch-up (`appendFromTable`) paged the table 500 rows at a time
with no regard for transactions, so it could put one across two segments.

## Considered Options

- A `txn` key on every line, naming the seq of the transaction's last entry.
- An end flag on a transaction's last line only.
- A separate commit line after each transaction.
- The shared `ts`: every line of one transaction carries the same timestamp.

## Decision Outcome

Chosen: `txn` on every line, the seq of the last entry of the transaction that
appended it (line format 2, `changelogfile.LineFormat`), and the same value in
a nullable `changelog.txn` column (migration 0019), stamped at commit beside
`hash` and covered by the checksum. A line ends its transaction when `seq`
equals `txn`; a line with `seq` below `txn` must be followed by lines carrying
the same `txn`. The active-segment scan cuts back to the last line that ends a
transaction, so an unfinished transaction goes whole, its complete lines with
the torn one; `Open` reports the bytes and lines it cut, and `OpenReadOnly`
and `Verify` count them and leave them. One append is whole transactions: the
writer refuses a batch that leaves a transaction open (`ErrTxnIncomplete`) or
changes transaction inside one (`ErrTxnFraming`), and it rotates the segment
only after an append, so a transaction never crosses a segment, however large.
`appendFromTable` extends a page to the end of the transaction its last row
belongs to for the same reason, and `Walk`, behind `repository verify`,
refuses a transaction that crosses a segment.

The end flag alone was rejected because an unflagged line would be either a
format 1 line or a non-final line of a format 2 transaction, and nothing in
the file could tell the two apart. A commit line was rejected because a
finished segment's last seq is derived from its newline count, and a line
that is not an entry would break that. `ts` was rejected because two
transactions can share a microsecond: a heuristic where a recorded boundary is
needed.

Recovery runs in both directions. With the table (a crash on a live host) the
table holds the whole transaction, is ahead of the cut file by exactly it, and
the boot appends it again. Without the table (a restore from a copy of the
directory) the transaction that never finished is lost whole, never in part.
Under `repository verify` the unfinished tail is a finding, not a refusal:
beside a live server it can be a transaction the writer is still writing.

Segments and rows written by v0.46.0 and v0.47.0, the two releases between
0050 and this record, carry no `txn`. A line without one is read as a
transaction of its own, which is how those binaries read it: nothing
reconstructs a boundary that was never recorded, and no wipe is required. The
rows those binaries stamped keep `txn` NULL and are written out unframed when
a boot catches a file up from the table. From the first boot of the release
that carries this record every new line and row is framed, and the rollback
is closed on both halves of the store. The directory closes itself: a line
with `txn` does not decode under v0.46.0 or v0.47.0, whose reader refuses an
unknown key. The table needs a gate, and the changelog dialect is it: the
first framed append stamps `changelog_dialect` 3 (the stamp rides
`settleChecksums`, so it commits with the first framed row), and those two
binaries' `gateChangelogDialect` refuses a repository stamped above 2 with
`ErrChangelogDialectNewer` before touching anything. Without the stamp,
v0.47.0's `checkRecorded` ignores migration 0019, which it does not carry,
its `appendFromTable` over an empty file recomputes every checksum without
`txn` and rewrites every `changelog.hash` to that encoding (its `after == 0`
re-stamp branch), and the next upgrade refuses every row as
`ErrChangelogDiverged` with no repair. Going back means restoring both halves
from before the upgrade. Nothing is promised for a directory from before
v0.46.0: the format began there.

### Consequences

- Good, because interrupting a multi-entry write at any byte leaves the whole
  transaction or none of it, in the file as in the table.
- Good, because the boundary is in the line and in the row, so the catch-up,
  the import (#365) and any later reader page on it without a heuristic.
- Bad, because the format breaks in both places: a directory or a table this
  release writes is refused by v0.46.0 and v0.47.0, and every line grows by a
  `txn` key.
- Bad, because the changelog dialect now has a rung that changes no op and no
  effect: dialect 3 is the entry's frame, so the ladder gates two different
  things with one number.
- Bad, because a format 1 history is exactly as recoverable as it was: a copy
  taken mid-transaction under those releases can hold a prefix of one, and
  nothing here can tell.
- Bad, because `txn` names the last seq, unknown until commit, so the frame is
  stamped by the same UPDATE as the checksum and not by the INSERT.

### Confirmation

`TestOpenCutsBackToTheLastCompleteTransaction` (internal/changelogfile) cuts a
three-line transaction at every byte offset and asserts the head is the seq
before it or its last seq. `TestATransactionLargerThanSegmentBytesLandsInOneSegment`
and `TestWriterRefusesABatchThatIsNotWholeTransactions` hold the writer,
`TestVerifyRefusesATransactionCrossingASegment` the walk, and
`TestALineWithoutTxnIsATransactionOfItsOwn` the format 1 reading. In
internal/engine, `TestBootReappendsATransactionCutInTheFile` holds the
table-ahead re-append, `TestCatchUpAppendsWholeTransactions` the paging, and
`TestChangelogDialectThreeIsRefusedByADialectTwoBinary` the gate.

## More Information

Supersedes the torn-tail rule of 0050: the reader's "one kind of damage" and
the boot check that "heals the one-transaction gap". The rest of 0050 stands:
the segments, the checksum, the sidecars, the table as the index and the
Postgres commit as the commit point. 0050 keeps its `accepted` status, because
it is itself the successor of 0009, 0010, 0011 and 0018 and a record's
successor must be accepted; marking it superseded would orphan those four, so
this record names what it replaces here instead. Appending before the commit is
[#364](https://github.com/geoah/substrate/issues/364), the manifest naming the
format a directory needs is
[#370](https://github.com/geoah/substrate/issues/370), and aligning the
import's batches to transactions is
[#365](https://github.com/geoah/substrate/issues/365). Reopen if a
transaction's entries can ever stop being contiguous (a second writer, a
distributed commit), which this frame cannot describe.
