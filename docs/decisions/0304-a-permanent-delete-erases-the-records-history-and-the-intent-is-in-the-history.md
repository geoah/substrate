---
status: accepted
date: 2026-11-14
decision-makers: George Antoniadis
---

# 0304. A permanent delete erases the record's history, and what remains to erase is in the history

## Context and Problem Statement

A permanent delete removed a record's rows and wrote a `gc` line, and every
earlier line of the record still held its values: in the table, in the state
entries [0296](0296-a-records-history-is-compacted-into-a-state-entry-in-the-line-of-its-newest-write.md)
writes and in the archive. Step 24 of the 1.0 plan (#1313) makes the delete
erase them. The erasure is a compaction, and some of it can only run after
the delete commits: the line that releases a mapped target's copy of a value
is the delete's own, and only a later compaction can put the target's state
past it. Whatever happens between the commit and the erasure (a crash, a
rebuild, a restore of an archive published between them) must not leave a
value behind with nothing that knows it is there.

## Considered Options

- Erase in the delete's own transaction.
- Two transactions, with what remains to erase kept in a table beside the
  history.
- Two transactions, with what remains to erase written into the history, as
  an effect the fold notes in a fold table.

## Decision Outcome

Chosen: two transactions, with the intent in the history. The delete's
transaction writes an `erase` effect for each record whose history is to be
erased (the record, its merge losers and the merge's and split's own
records, the change requests that target it, each mapped target whose copy
the delete released), naming
the record and the line it is erased up to, and no value. The fold notes it
in `compaction_erasures`, so a rebuild, an import and a restore of any
archive that holds the line know what remains. The second transaction, in
the same call, erases each record's history up to its line, takes the
`erase` effect off its line and the row out, together: in any one cut of the
history, a value an erasure names is never there without its intent, and an
intent is never left once nothing it names is left. The compaction pass
finishes what a call left. The changelog dialect goes up for the new effect.

It beat the first option because the target's state must follow the release
the delete writes, and beat the second because a table beside the history
is lost by exactly the restores and rebuilds the erasure must survive.

What holds an erasure, besides the rule that nothing is erased at or above a
line known not to verify
([0300](0300-a-paced-pass-compacts-what-is-past-each-kinds-retention-and-forgets-old-deletions.md),
an erasure is whole or held on it, intent kept, and the reply names the
seq): an enabled record trigger that watches the kind's deletions, has not
been delivered this one, and is delivering. One with a parked failure, or
whose cursor has not moved for five minutes, is passed, and meets the line
with no value when it resumes. Whoever may erase may finish an erasure now,
past every trigger (0306). A permanent delete settles the parked failures
and drains on the record's own lines first: a retry of a change to a record
that no longer exists has nothing to deliver.

### Consequences

- Good, because a crash, a rebuild or a restore between the two
  transactions leaves the intent in the history, and the next pass finishes
  the erasure.
- Good, because the delete's reply and the record's status read the fold
  table, so they say the same after a restart.
- Bad, because a server from before this release refuses a repository whose
  history holds an `erase` effect, by its dialect.
- Bad, because an erasure can wait, on a trigger for minutes or on a damaged
  line until its repair, and the person is told so rather than the values
  being gone.
- Bad, because copies taken before the delete (an export, a backup, an
  archive version a bucket's policy keeps) still hold the values.

### Confirmation

`TestAPermanentDeleteLeavesNoValueOfTheRecordInTheLog` scans every table and
the archive for the values; `TestAnErasureCutShortBetweenItsTransactionsIsFinishedByThePass`
stops the call between its transactions, rebuilds between them, and restores
an archive published between them; `TestAnErasureWaitsBelowALineKnownNotToVerify`
holds an erasure on a damaged line through both doors.

## More Information

What a permanent delete reaches, and what it cannot, is
[0305](0305-a-permanent-delete-reaches-what-holds-the-records-values-and-not-what-it-owns.md);
the forget door is [0306](0306-a-record-has-a-forget-door-that-erases-its-history-before-its-current-state.md).
