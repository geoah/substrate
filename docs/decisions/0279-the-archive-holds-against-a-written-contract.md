---
status: accepted
date: 2026-11-14
decision-makers: George Antoniadis
---

# 0279. The archive holds against a written contract, and the database's record decides whether an archive is its own

## Context and Problem Statement

[0262](0262-postgres-alone-confirms-a-write-and-an-archive-is-copied-after-the-commit.md)
copies each repository to a store after its writes commit, in the layout of
[0277](0277-the-archive-is-pieces-blobs-and-sealed-values-and-its-manifest-is-the-mark.md).
Five reviews of that code each found faults of a kind the one before had not
looked at (7, 3, 5, 5 and 8 findings): a listing that is stale, a put applied
late across a database restore, a delete applied late after a re-upload, two
histories that share a last line. Each fix added machinery the next found
holes in, because nothing said what the archive holds against. This records
the contract the code is now built to, and the options it was chosen over.

## Considered Options

- Keep hardening the archive case by case, each review's scenarios added as
  they are found.
- A conditional put (write only if absent, or only if unchanged), so a
  manifest never replaces another and a stale writer fails.
- Decide whether an archive is this database's by comparing its lines with
  the table's, across conversions by a per-line record of each conversion
  (`changelog_converted`, the checksum each line had in each dialect).
- Continue an archive after the database was restored, imported or chose
  between the two, trusting the objects it holds.
- A written contract: what the store must do, eight rules, and what the
  archive does not hold against; the database keeps a record of what it put
  and published, and that record, not the archive's lines, decides.

## Decision Outcome

Chosen: the written contract, set out in full in
[the archive's contract](../operations.md#the-archives-contract). In short:

- The store does four things, whole, and a request that failed may be
  applied later, at any time. Nothing else is assumed.
- Every object's name carries the pass that put it, and a manifest's its
  epoch and its bytes' hash, so no name holds two contents; a manifest
  selects only what its pass put or the manifest before selected, and a
  name it drops is never selected again.
- The database keeps a **record**: the archive it writes, the manifest it
  published (name and bytes), the exact bytes of the one it is about to
  publish, and a row for every object it puts or deletes, written before the
  request. A pending publication is finished with the same bytes.
- A pass, an idle one included, goes on only in an archive as the database
  left it. Anything else stops the passes, which write and delete nothing,
  and the operator chooses whole between the database and the archive
  (`substratectl archive report`, `archive resolve`). Nothing is merged, and
  taking the archive removes nothing of the database until the whole
  snapshot it installs is read, checked and on local disk.
- A pass deletes only what the record accounts for, after the publication
  that drops it.
- Lines are compared in one place: the first boot over a directory a release
  before the archive wrote, every line, before the history is converted.
- An archive is one life: a creation, an import, a restore or an operator's
  choice starts a new one, and only its own database life continues it.
- A deletion begins on the database's row, then writes a mark past every
  archive, which covers them all; a covered archive comes back only by an
  explicit import.

Case-by-case hardening was rejected because the count of findings did not go
down: with no stated contract there is always one more interleaving. A
conditional put was rejected because the store is a plain object store or a
directory, and not every S3-class service offers one; the record does its
job from the database, which already decides what happened. Comparing lines
was rejected because two histories can share any number of lines, a
compacted history cannot prove one extends another, and across a
conversion it needs a record of every line's checksum in every dialect,
which an archive restored in a later dialect cannot carry; so
`changelog_converted` goes, and a conversion's step is the one place a line
is rewritten (it may read the repository's declarations, conversion.go's
`Declarations`, because no copy of a line is ever converted alone to be
compared, and each step walks the table on its own, so its `Line` sees
every line once). Continuing an archive after a restore was rejected because a
database restored from a backup has forgotten which of the archive's
objects it had begun to delete, and a forgotten delete may land on an object
a new manifest selects.

A database restored from a backup needs no declaration: a publication of its
forgotten future that landed is a manifest the record does not hold, and
the pass stops; one still in flight lands at or above the current epoch (the
pass stops) or below it (ignored and reported); a forgotten delete targets
only what a landed publication dropped, which the pass saw, or an object no
manifest selected, which is never selected after a restart.

### Consequences

- Good, because a review holds the code to a written rule, and a scenario
  the contract excludes by name is not a defect.
- Good, because no automatic path takes an archive's word over the
  database's, and no archive is written over by a database that did not
  publish what it holds.
- Good, because deleting is accounted: no object leaves the store unless the
  database recorded putting it and a publication dropping it.
- Bad, because an object a pass put and no manifest selected is only
  deleted after a restart, so a process stopped in the middle of the first
  copy of a long history copies it again; blobs are bounded at 1 GiB a step.
- Bad, because every import, restore and resolution starts a new archive,
  which copies every blob again, and the old archive's bytes stay until the
  operator removes them (`substratectl archive remove`).
- Bad, because two databases on one archive are not safe, and a store that
  lists stale or loses objects after answering is not supported: both are
  stated limits, not handled cases.
- Bad, because a database restored from a backup beside a newer archive
  serves its own history and archives nothing until the operator chooses,
  and the two diverge with every write meanwhile.
- Bad, because the record is two more repository-scoped tables, a row per
  object.

### Confirmation

`internal/engine/archivecontract_db_test.go` holds each rule and each case
the fifth review found with a test that fails without it: requests applied
late after a restart and after a restore from a backup taken at each step
of a pass, every listing that is not the record's, a pass stopped between
any two steps, the pending manifest put again, the operator's choices, and
the deletion marks. `internal/engine/crashpoints_db_test.go` stops each step
of a restore, a 0.x import and a deletion, and `repodir_db_test.go` holds a
0.x directory to the table line for line.

## More Information

Amends [0260](0260-the-stored-format-is-a-contract-each-release-keeps-a-directory-and-history-converts-forward-once.md):
a conversion step is handed one line and, when it asks, the repository's
declarations, because no copy of a line is ever converted outside the walk
over the table. The plan is [pull request 1313](https://github.com/geoah/substrate/pull/1313),
step 7.
