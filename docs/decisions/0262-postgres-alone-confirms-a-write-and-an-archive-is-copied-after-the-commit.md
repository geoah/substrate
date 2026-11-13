---
status: accepted
date: 2026-11-13
decision-makers: George Antoniadis
---

# 0262. Postgres alone confirms a write, and an archive is copied after the commit

## Context and Problem Statement

A write waited for two copies before its answer: the Postgres commit, and its
lines in the repository's segment files, prepared before the commit and ended
by a newline after it
([0050](0050-the-changelog-is-checksummed-segment-files-and-postgres-indexes-it.md),
[0062](0062-a-write-is-on-disk-before-its-commit-and-its-final-newline-is-the-commit-marker.md)).
That cost two flushes on every write, a state in which the files fell behind
and every write was refused until a restart, a boot check that stopped the
server over one directory, a background digest that had to be paced, and a
server tied to a local disk. It protected complete history, which the 1.0
plan stops treating as the truth (F6, F9 of the plan on
`geoah/review-v1-stability-contracts`, `docs/plans/v1-review.md`).

## Considered Options

- Keep the synchronous copy.
- Postgres alone confirms a write; a background pass copies each repository
  to a store after the commit.
- Postgres alone, and no copy: backups are Postgres's own.

## Decision Outcome

Chosen: Postgres alone confirms a write, and a background pass copies each
repository to an ARCHIVE after the commit. The copy is what an object store
can hold, so the archive may be a directory or an S3-compatible bucket, and
it is on by default; an operator who backs Postgres up may turn it off. The
pass needs a durable mark of what it has copied, writes the blobs and sealed
values before the history that names them, and reports how far it is behind
([0277](0277-the-archive-is-pieces-blobs-and-sealed-values-and-its-manifest-is-the-mark.md)).
When the store cannot be reached the server goes on serving and the delay
grows; how far behind is too far is
[#1314](https://github.com/geoah/substrate/issues/1314), after 1.0. No copy
at all was rejected because a restore would then need a Postgres backup, and
the import of a directory, the export and the move between hosts all rest on
one.

The order and the one writer of
[0017](0017-the-changelog-is-one-writers-total-order.md) stand: the archive
copies the order Postgres committed. A commit whose answer is lost is a
Postgres question alone, so
[0165](0165-a-commit-in-doubt-is-caught-up-by-the-next-write-and-no-write-grant-names-the-owners-auth-kinds.md)'s
catch-up of the file has nothing left to catch up.

### Consequences

- Good, because a write costs one commit, and the file-behind state, its
  refusals and its restart are gone.
- Good, because the server no longer needs a local disk for history, and the
  archive can be in another place than the server.
- Good, because a damaged archive stops the archive and nothing else.
- Bad, because a write confirmed and not yet copied is lost if Postgres's
  storage is lost; a crash or a restart of Postgres loses nothing. Zero loss
  is Postgres's own synchronous replica.
- Bad, because an S3 store brings a library and its modules into the binary.

### Confirmation

`internal/engine/archive_db_test.go`: a pass stopped at each durable step
leaves an archive that agrees with the table, and the next pass catches it
up; an archive ahead of the table is not written over; the delay grows while
the store is unreachable and writes go on. What the archive holds against is
[0279](0279-the-archive-holds-against-a-written-contract.md)'s contract. `internal/archive` holds each
store to one conformance suite, the S3 one against an in-memory service.

## More Information

Replaces [0050](0050-the-changelog-is-checksummed-segment-files-and-postgres-indexes-it.md)
and [0062](0062-a-write-is-on-disk-before-its-commit-and-its-final-newline-is-the-commit-marker.md).
Revisit when [#1314](https://github.com/geoah/substrate/issues/1314) decides
whether a server should ever stop for its archive.
