---
status: accepted
date: 2026-11-15
decision-makers: George Antoniadis
---

# 0325. A database has one writer process, and a retaken lease checks who else started

## Context and Problem Statement

A server holds each repository's writer lease on one database session, and
its heartbeat takes the leases back after that session drops
([0083](0083-a-repository-has-one-writer-and-a-second-is-refused-at-open.md)).
[0324](0324-a-start-takes-every-writer-lease-before-it-migrates-and-a-retaken-lease-checks-the-migrations.md)
has the retake compare `schema_migrations` with what the process started
under, which sees a process that migrated the database while the session was
down. It does not see one of the same release, a server a supervisor started
or an operator command, that took the free leases, wrote and stopped: the
retaking process then serves from a registry, caches and positions that
predate those writes
([#1449](https://github.com/geoah/substrate/issues/1449)).

A mark each start leaves is not enough on its own. The leases are per
repository, so nothing stopped two writers whose starts held different
repositories, and on a database with none, two starts held nothing at all.
One started first, the other registered a repository; while the second's
session was down the first opened it, wrote and stopped, and the second's
retake found its own mark. A process could become a repository's writer
without starting.

## Considered Options

- One writer process per database, held by a lock of its own that every
  start takes first, and a mark only a start leaves, which a retake compares.
- A counter each start raises, compared at a retake.
- Compare each open repository's changelog head and history generation at
  the retake.

## Decision Outcome

Chosen: **a database has one writer process, and a start is the only way to
become it; each start leaves a mark no other leaves, and a retake compares
it.** The lock makes a start the one door, so marking the starts sees every
writer. A counter can repeat: a database restored to an earlier value and
started on again reads as the retaking process's own. A head comparison needs
a head each dataset has confirmed, which it does not hold.

- **The database's writer lock.** A start that writes takes it on the
  writer lease's session before anything else: before the repository leases
  and before the first migration. It needs no table, so it holds on an empty
  database. Another process holding it refuses the start, naming the holder
  (`ErrDatabaseHasAnotherWriter`, which is `ErrRepositoryHasAnotherWriter`
  too). It is held for the life of the process, lost with the session like
  the leases, proven by the heartbeat even where the process holds no
  repository, and taken back first at a retake; a retake that cannot take it
  stays refused. Its key is the two-integer form of an advisory lock,
  `(hashtext(current_schema()), hashtext('substrate|writer'))`, a key space
  Postgres keeps apart from the one-bigint form every lease and every other
  lock of the engine uses.
- **The start mark.** Holding the lock and every lease, with its migrations
  applied, the start writes a random value of its own to `writer_starts`, a
  table of one row (migration 0032), and keeps it. Only the lease's session
  reads or writes it, as the DSN's own user; no role is granted it. A start
  refused at the lock or at a lease leaves no mark.
- **The retake** takes the lock, then the leases, then compares the
  migrations and the mark. Any of them moved ends the same way as 0324 says:
  `ErrWriterLeaseStale` for the rest of the process's life, the lock and the
  leases let go, the passes stopped, and an ERROR line that says which it
  found. Another mark says the stored mark is not this process's: another
  start, or a database restored from a copy taken before this process
  started. No mark, or no table, is a database emptied or replaced. An equal
  mark says only that the stored mark is this process's, not that the
  database holds the history this process wrote.
- **Who takes it.** Every process that opens as the writer goes through the
  same start: the server, and each `substratectl --dsn` command that writes
  (`repository rebuild`, `compact`, `reindex` and `rotate-generation`, the
  `archive` commands, and `user reset` where it writes an actor), all
  through `OpenOperator`. No operator command takes a lease any other way. A
  read-only open (`repository verify`, `history`, `reembed`, a `user reset`
  of a sign-in alone) takes nothing and compares nothing.
- **The first start of the release** takes the lock, applies 0032, which
  creates the table and writes no row, and then leaves its mark.

### Consequences

- Good, because a deployment runs one writer process per database, as
  docs/operations.md already said, and now nothing else can happen: a second
  start is refused whatever repositories either holds.
- Good, because a retake sees any process of this release that wrote
  meanwhile, a server or an operator command, and a database emptied under
  it or restored from a copy taken before it started.
- Good, because 0324's "the guard sees leases, not processes" no longer holds
  for a release with the lock: a server of this release holding no
  repository holds the database.
- Bad, because a process of a release without the lock or the mark (v0.123.1
  and before) takes neither, and is seen only through a lease it holds, the
  limit 0324 states; the migration check stays for that case.
- Bad, because a copy taken after the process started holds its mark. A
  database restored from one under a running process, with no other writer
  started on it, keeps the migrations and the mark the process started
  under, and the process goes on from memory newer than the database. The
  operator stops every writer before restoring a database and starts it
  again after, as docs/operations.md says. Closing it here needs a
  comparison of what each repository holds, which #1444 found is not small.
- Bad, because the mark sees starts, not writes. A start that held
  everything and died before it served still leaves a server whose session
  dropped meanwhile refused for good: a restart nobody expects, and never a
  missed writer.

### Confirmation

In `writerstarts_internal_db_test.go`:
- `TestAServerThatAnotherStartedBesideStaysRefused`: the example of #1449.
  A restart of the refused server serves what the other wrote.
- `TestAServerWhoseLeasesNobodyTookResumes`: a start refused beside A leaves
  no mark, and a gap nothing starts in is resumed from.
- `TestAServerThatAnOperatorCommandOpenedBesideStaysRefused`.
- `TestAReadOnlyOpenBesideALostLeaseIsNotSeen`.
- `TestTwoStartsOnAnEmptyDatabase`: the second is refused at the lock, with
  no migration applied and no mark left.
- `TestAWriterThatStartedFirstHoldsTheDatabase`: the interleaving above
  cannot happen.
- `TestARetakeWaitsForTheDatabasesWriterLock`.
- `TestTheStartMarkReplacedUnderAServerRefusesIt`: restored, then started
  on, and deleted.
- `TestTheFirstStartOfTheReleaseLeavesItsMark`.

## More Information

This amends [0324](0324-a-start-takes-every-writer-lease-before-it-migrates-and-a-retaken-lease-checks-the-migrations.md):
a start takes the database before its repositories, and a retake checks who
else started, not only what was migrated.
