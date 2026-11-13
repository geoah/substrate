---
status: accepted
date: 2026-11-13
decision-makers: George Antoniadis
---

# 0277. The archive is pieces, blobs and sealed values, and its manifest is the mark

## Context and Problem Statement

[0262](0262-postgres-alone-confirms-a-write-and-an-archive-is-copied-after-the-commit.md)
copies each repository to a store after its writes commit. A store has four
operations, put, get, list and delete: nothing appends, renames or locks, so
the segment files of
[0050](0050-the-changelog-is-checksummed-segment-files-and-postgres-indexes-it.md),
whose last one grows, cannot be held there. The plan asks for a durable mark
of what is copied, the files and secrets before the history that names them,
and a layout in which a later base replaces older history.

## Considered Options

- Segments as today, each rewritten whole as it grows.
- Pieces: runs of whole transactions, each written once, named by the first
  and last seq they hold; the mark in a separate object.
- Pieces, with the mark in the manifest; or the mark in Postgres alone.

## Decision Outcome

Chosen: pieces, with the mark in the manifest. A repository's copy is one
archive at a time, a life under an id of its own
([0279](0279-the-archive-holds-against-a-written-contract.md)). Under
`repositories/<id>/<archive>/` it holds `changelog/<dialect>/` pieces (whole
transactions, consecutive seqs, lines in the canonical form of
[0260](0260-the-stored-format-is-a-contract-each-release-keeps-a-directory-and-history-converts-forward-once.md)),
`blobs/<digest>.<pass>`, `sealed/<ref>.<sum>.<pass>.json` (one object per
version of a sealed value, still under the repository's data key) and
`spool/`; its manifests sit apart, under
`repositories/<id>/manifests/<archive>/<epoch>-<hash>.json`, format 3:
format 2's keys and the mark, `head` and the checksum of the line at it,
`pieces`, the chain from seq 1 to the head with each piece's checksum,
`sealed` and `spool`, the objects the archive at the head selects,
`missingBlobs` and `pendingBlobs`, the blobs the records hold that it lacks,
and `epoch`. A reader takes the newest archive's manifest of the highest
epoch. **No name ever holds two contents**: every object's name carries the
pass that put it, and a manifest's its epoch and its bytes' hash, so a write
a store applies late lands under a name nothing else has. Overwriting one
`repository.json` was rejected for that: with four operations and no
conditional write, a late put of an older manifest would replace a newer
one. A pass writes the blobs, spool and sealed values the records hold, then
the pieces to the head, then the manifest, and deletes what that manifest no
longer selects only after its publication is recorded. So the manifest is
the pass's one commit point, and what it names is there. No object a
manifest selects is written over: a changed sealed value is a new object
beside the version the manifest before selects, so an archive a pass
stopped short of its manifest still opens every value bound to the record
its history names. Writing sealed values over in place was rejected for
that: a conversion that binds a value to another record would leave the old
manifest's history paired with the new binding.

The manifest lists its pieces. A small last piece is written again with the
next pass's lines under a name of that pass, and the manifest that selects
the longer one drops it; a later compaction can replace older pieces with a
base the same way. Reading the chain off a listing (at each seq, the piece
that reaches furthest) was tried first and rejected: with pieces named by
their seqs alone, two histories that share a range share a name, and a
listing is not the record of what a manifest selects. Rewriting segments
whole was rejected for its cost: every pass would copy the growing segment
again. The mark in Postgres alone was rejected because a restore reads the
archive with no database; Postgres repeats it on the row (`archived_seq`)
for an operator and the delay, and keeps the record of what it published.

### Consequences

- Good, because every object is written once, which any object store
  holds, and a crash leaves the last manifest's archive intact.
- Good, because a manifest is read whole with no listing of its pieces,
  each held to the checksum the manifest names.
- Bad, because a busy repository leaves a piece per pass until compaction
  merges them, and the manifest grows by a line per piece.
- Bad, because a sealed value in the archive may be newer than its head: a
  pass writes the values the table holds, and a value changed after the head
  it names is still the newer one.
- Bad, because a blob's bytes are named per pass, so a blob dropped and
  needed again is put again rather than kept.

### Confirmation

`internal/changelogfile/archive_test.go` (names, the chain, the manifest)
and `piece_test.go` (a damaged piece), `internal/engine/conversion_internal_db_test.go`
(a pass stopped after the sealed values of a conversion that rebinds one)
and the archive tests of
[0262](0262-postgres-alone-confirms-a-write-and-an-archive-is-copied-after-the-commit.md)
and [0279](0279-the-archive-holds-against-a-written-contract.md);
`docs/stored-format.md` writes the layout down.

## More Information

Replaces what [0051](0051-a-repository-directory-is-the-backup-unit.md) left
standing: the backup unit is the archive, not the server's directory.
