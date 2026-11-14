---
status: accepted
date: 2026-11-14
decision-makers: the 1.0 plan's orchestrator session, for George Antoniadis
---

# 0319. The hands that wrote a shipped record outlive its history

## Context and Problem Statement

An open, an install or an upgrade writes a record a package ships again
where it is missing or differs from what ships, unless a hand other than
the shipping ones ever wrote or deleted it
([0191](0191-a-shipped-record-is-held-by-any-hand-but-the-one-that-ships-it.md)).
The rule reads who wrote the record from its history, and the compaction
pass shortens history and forgets old deletions. The interim of
[0310](0310-a-record-a-package-ships-is-left-out-of-compaction-for-now.md)
left every shipped record out of compaction, so about 390 seeded records
and each installed package's data records were never compacted and their
deletions never forgotten.

A compaction already keeps the hands: a state entry holds the actor, whom
it acted for and the operation of every line it replaces (`writers`), and
every rule that asks who wrote a record reads the lines that remain and
those, through one reader (`recordHands`). The one history that leaves no
line, and so no hand, is a forgotten deletion.

## Considered Options

- The hands outlive the history: a shipped record is compacted like any
  other, and where the pass would forget its deletion it keeps the
  deletion's line with the hands and no value.
- A separate list of the shipped records an owner removed or changed, kept
  for good and read by the seeds and the installs instead of the history.
- Accept that a deleted shipped record comes back once the pass forgets its
  deletion.
- Keep the interim for good.

## Decision Outcome

Chosen: the hands outlive the history. Nothing new is stored: no table, no
list, no effect and no entry shape. `recordHands` stays the one reader.

- A record a package ships is compacted by its kind's retention, by
  `repository compact` and by the pass, like any other. The rule answers
  after a compaction what it answered before, from `writers`.
- Where the pass would forget the deletion of a record a package ships
  (`shippedAmong`), it takes what forgetting the deletion takes, the
  record's own entries, and keeps the deletion's line, with the hands and no
  value. That line is never forgotten and moves no horizon. A deleted record
  no package ships is forgotten whole, as before.
- Keeping a deletion is not a permanent delete
  ([0304](0304-a-permanent-delete-erases-the-records-history-and-the-intent-is-in-the-history.md),
  [0305](0305-a-permanent-delete-reaches-what-holds-the-records-values-and-not-what-it-owns.md)):
  the owner asked for a deletion, not an erasure. It takes none of what an
  erasure's scope also reaches (a change request on the record, a merge's
  or a split's own record, a copy a mapping made, a trigger's saved
  position), and what holds a forgetting back (a merge's or a split's
  resync, a line a parked failure names) holds it back the same way.
- A bundle record's deletion is kept the same way, since the operator's
  install list asks whether the changelog ever held a package's bundle
  record, and a forgotten one would install the package again.
- "Forget history" is no longer refused for a shipped record. Forgetting a
  record's history is not a write of it: the forget's own line carries its
  intent and no value of the record, and `recordHands` does not count it,
  so the rule answers after a forget what it answered before.
- [0311](0311-a-purge-the-engine-writes-for-a-hand-is-that-hands-deletion-of-a-shipped-record.md)
  stands as it is: a purge the engine writes acting for a hand is that
  hand's deletion.
- A repository whose pass already ran under the interim has its shipped
  deletions below the forgetting's mark. A repository migration lowers that
  mark once, so the next pass looks at them again and keeps them as above.

A separate list was ruled out because it is a second memory of the same
fact, which the hands already hold, and it must be kept in step with
history on every door that writes or erases. Accepting a deleted record's
return was ruled out because an owner's deletion of a vendor row or a view
would last only as long as the deletion period. Keeping the interim was
ruled out because those histories then grow for good, and an earlier value
of a shipped record stays until its owner erases it.

**How this differs from the plan's page** (#1313). The page has the rule
read the marks. A mark sits on a property, so no mark holds a record that
was deleted or a property that was removed. The rule reads the hands the
changelog keeps instead.

### Consequences

- Good, because a shipped record's history is compacted and its earlier
  values go with it, as any other record's do.
- Good, because the owner's edit and deletion outlive every compaction and
  every forgetting, read from the one place every rule reads.
- Bad, because a shipped record's deletion line stays for good, after its
  package is removed too: one line per deleted shipped record, with the
  hands and no value.
- Bad, because a repository whose pass already ran reads every deletion
  again once, at its first pass under this release.

### Confirmation

The tests in `internal/engine/compactshipped_internal_db_test.go`: a seeded
record and an installed package's record, edited by the owner, compacted,
and each deleted, swept and kept past the deletion period, are left as the
owner left them by the next open and the next upgrade, after a second pass,
`repository compact`, an archive pass with a restore and a rebuild; one
nobody touched is compacted and still takes what ships next; a forget of
one whose newest line is the shipper's is allowed and answers as before; an
uninstalled and purged package is not installed again by the operator's
list; a repository whose pass ran under the interim ends the same; an
ordinary record, and one a package's function wrote, are forgotten whole.

## More Information

Supersedes 0310. Built in the pull request that closes #1420.
