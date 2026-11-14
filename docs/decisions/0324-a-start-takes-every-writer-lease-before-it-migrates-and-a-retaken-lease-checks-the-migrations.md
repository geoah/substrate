---
status: accepted
date: 2026-11-14
decision-makers: George Antoniadis
---

# 0324. A start takes every writer lease before it migrates, and a retaken lease checks the migrations

## Context and Problem Statement

A server applied its SQL migrations before it took any writer lease
([0083](0083-a-repository-has-one-writer-and-a-second-is-refused-at-open.md)),
so a new binary started beside a running server migrated that server's
database first and was refused its repositories after
([#1352](https://github.com/geoah/substrate/issues/1352)). The running server
went on under a schema it was not built for. A search reindex or a
reprojection part-way through resumes from a position it holds in memory, and
a migration that changes an order or drops a column makes that resume skip
rows and record its pass as done. Migration 0021 refused for itself while
another server held a repository. Every other migration had the same
exposure, and a lease proves a session, not a process: a server whose lease
connection dropped holds nothing until its heartbeat retakes it.

## Considered Options

- Take every repository's lease before the first migration, on the session
  that runs the migrations, and keep it. A retake checks that the database's
  migrations are the ones the process started under, and restarts the passes
  that hold a position in memory.
- Keep the order, and give each migration that needs it a guard of its own,
  as 0021 has.
- Refuse a start while any other server process is connected, whatever it
  holds.

## Decision Outcome

Chosen: **every lease before the first migration, on the session that runs
them, kept for the life of the process; a retake checks the migrations.** It
is one mechanism for every migration, and it closes the case a per-migration
guard cannot: the old server that retakes its lease after the migration.

- **The order of a start** that writes: the roles, then the writer lease's
  session (a connection of the DDL pool, pinned for the process). That session
  takes every repository's lease under a share lock on `repositories`, which
  stops a row from appearing while it reads them, since a registration takes
  its lease before its row exists. It also checks the name-keyed lease of a
  release before ids. A lease another session holds refuses the start and
  names the holder, and the start has applied no migration and written no
  row. What a start does before the lease stays where it was: the data
  root's `repositories/` directory, made where absent, which proves the data
  root holds files before the database is touched, and the two isolation
  roles, made where absent, which the migrations grant to. Beside a running
  server both exist. An instance id is minted only past the lease. Then the
  migrations run on that same session, so 0021's own guard reads the leases
  as this process's.
  Before each migration, the runner checks, under the same table lock, that
  no repository it lacks has appeared under another holder. The rest of the
  start follows: the orphan sweep, the indexes, the boot check.
- **A read-only process** applies none and takes no lease, as before.
- **A retake** after the lease's connection dropped takes every lease back,
  reads `schema_migrations` and compares it with what the process started
  under. A database another server migrated meanwhile keeps the process
  refused for the rest of its life, and it says to restart. Its leases are
  let go, so the server that migrated can start again, and its passes are
  stopped. Under the same migrations every write stays refused while each
  pass that holds a position in memory (`retakePasses`) is stopped and reset;
  then the refusal clears, and then the passes start again. A pass that did
  not stop keeps the refusal for the next beat's retake, and a pass one
  reset stopped is started again by whichever reset is the one that
  finishes.
- **What holds the list.** `TestEveryBackgroundPassIsClassedForARetake`
  reads the engine package's source, every file but the tests. It
  enumerates every task started through `spawn` or a pass's `start` under a
  string literal or a constant, which `retakePasses` classes, and every
  goroutine started any other way (a `go` statement, a `time.AfterFunc` or
  a `context.AfterFunc`, a `.Go` call of an `errgroup.Group` or a
  `sync.WaitGroup`), counted by the function it is in, each function with
  how many it starts and why none holds a position a retake could leave
  wrong. Work started outside the package
  is not seen. It writes through the engine's methods, which refuse while
  the lease is lost and, once a retake found the database migrated, for
  good, so no position it holds is written from again.

### Consequences

- Good, because a start beside a running server is refused before it
  applies a migration or writes a row, whatever is pending, and no
  migration lands under a running server.
- Good, because an old server whose lease connection dropped finds the lease
  taken while the new one runs, and finds the migrations moved once it stops,
  and so never goes on under a schema it was not built for.
- Bad, because the lease's session is now a connection of the DDL pool, which
  grows by one, and a superuser DSN makes the lease's session a superuser's.
- Bad, because a deployment that starts the new process before it stops the
  old one now waits for ever. It replaces its one server process and never
  rolls two (docs/operations.md).
- Bad, because the check is this release's own. A server of v0.123.1 or
  before retakes its lease without reading the migrations: one whose lease
  session drops while the first start of this release migrates, and which
  then stops, retakes and goes on under a schema it was not built for. The
  guarantee across that upgrade is the documented order: stop the old
  server, and see it gone, before the first start of this release.
- Bad, because a retake sees a migration, not a server. One of the same
  release that started, served and stopped while the lease was down goes
  unseen, and the process goes on with the datasets, registries and caches
  it held. Comparing each open repository's changelog head and history
  generation at the retake is left to a follow-up.
- Bad, because the guard sees leases, not processes. A server holding no
  repository at all, with a registration it has not yet started, is not seen,
  and a registration such a server completes after the last migration lands
  under the new schema.

### Confirmation

`TestAMigrationWaitsForEveryLease` (a lease held by another session refuses
the start with `schema_migrations` unchanged, by id and by a release before
ids' name), `TestAServerThatLostItsLeaseToAMigrationStaysRefused` (the
scenario of #1352: the old server's lease connection drops, a new server
applies a migration and stops, and the old server's retake leaves it refused
with its passes stopped), `TestARetakeRefusesWritesUntilThePassesAreReset`
(the leases taken back and the reset held: a write is refused, and the same
write lands once the reset is done),
`TestARetakeStartsAgainAReindexWhoseStopTimedOut`,
`TestAFreshDatabaseStarts`, and `TestEveryBackgroundPassIsClassedForARetake`.

## More Information

This extends [0083](0083-a-repository-has-one-writer-and-a-second-is-refused-at-open.md):
the lease is taken earlier and on another pool, and its retake reads one more
thing. Migration 0021's guard stays, because a landed migration is never
edited, and it now finds its leases held by its own session.
