---
status: accepted
date: 2026-11-14
decision-makers: George Antoniadis
---

# 0326. A repository whose first open fails is set aside, like one the start sets aside

## Context and Problem Statement

Decision 0281 sets aside a repository the start finds damaged and kept one
case out: "A failure at a repository's first open, after the start, stays
what it was: that request answers it and the next open tries again." The
open ladder runs after the start, at a repository's first open in the
process: its repository migrations, the load of its stored vocabulary and
the upgrade of the shipped packages (`internal/engine/engine.go` openNew).
A repository whose migration fails there answers a plain `503 unavailable`,
shows nowhere an operator looks for a set-aside one, and every request and
every pass of the server's background loops runs the migration again, its
transaction rolling back each time (issue #1377). This record amends that
part of 0281; the rest of 0281 stands.

## Considered Options

- Keep 0281's choice: answer the open's failure and run the ladder again
  at the next open.
- Run every repository's first open at the start, inside the gate, so a
  failure there is set aside by the gate as every other is.
- Set a repository aside where its first open fails, after the start, at a
  stage of its own.

## Decision Outcome

Chosen: set it aside where its first open fails, at the stage
`first open`, by the gate's own rule. The server still listens as soon as
the start has checked every repository's directory against its rows: the
ladder stays at the first open, which a request or a loop drives after the
server listens, and a start that opened every repository first would make
every deployment wait for every repository's ladder before it serves
anyone.

Three steps of the ladder set their repository aside when they fail,
because each reads or rewrites the repository's own rows: a repository
migration, the load of its stored vocabulary and the upgrade of the shipped
packages. Only a failure that trying again cannot cure without a change to
the repository or to the binary sets it aside, read by the kind of its
error (`failureOf`), and one reading serves the start's gate and the first
open alike. **An error whose kind is unknown is not the repository's**, so
the repository's kind is named, never inferred:

- **The server's**, read first, which may pass with nothing changed: the
  start or the request canceled or past its deadline; another writer or a
  lost lease; a connection lost, refused, reset or timed out
  (`driver.ErrBadConn`, `sql.ErrConnDone`, `sql.ErrTxDone`,
  `pgconn.ErrConnClosed`, `*pgconn.ConnectError`, pgx's transport errors
  that carry a `SafeToRetry` method, `*net.OpError`); the operating
  system's network numbers and the host's limits (ECONNRESET, EPIPE,
  ETIMEDOUT, ECONNREFUSED, ECONNABORTED, ENETUNREACH, EHOSTUNREACH,
  ENETDOWN, ENOSPC, EDQUOT, EMFILE, ENFILE, ENOMEM), bare or inside a
  file's error, since a full disk or a network mount gone is never one
  repository's damage; a commit whose answer was lost
  (`ErrWriteInDoubt`); what says by name that it is not available yet or
  until a restart (`ErrUnavailable` and its family, `ErrRestartRequired`);
  and the SQLSTATE classes 08 (connection), 40 (serialization failure,
  deadlock), 53 (a resource limit), 57 (a statement canceled, the database
  shutting down), 58 (a system error) and `XX` (an internal error).
- **The repository's**, by name: a ledger this binary cannot continue
  (`ErrRepositoryMigrationsNewer`, and a ledger that diverges, recorded
  under another name or pending below its successors); a stored
  declaration in a shape or a dialect this binary cannot read
  (`ErrDeclarationUntranslated`, `ErrVocabularyDialectNewer`); a
  validation refusal, which is how a stored closure that does not load and
  a shipped upgrade a row's values refuse are told; stored data that is
  corrupt; and a migration's own refusal, marked where `runRepositoryMigrations`
  receives it, only where its error carries nothing of the database, the
  network, a file or the context, nor any of the server's names, nor a
  version conflict or no row.
- **Unknown**, the default: everything else. That includes a plain error,
  database/sql's own untyped errors ("sql: database is closed"), pgx's
  plain protocol errors, a version conflict (`ErrConflict`), no row
  (`sql.ErrNoRows`), any other database refusal, a file's error and the
  other numbers (EIO, ENOENT), and the classes 22 and 23. Those two are
  not proof: a 23505 or a 23P01 can come from a race with another session
  and a 22P02 from a parameter the code built. They would be the
  repository's only where the failing statement ran inside a wrapped
  step's own transaction and nothing outside the repository could make it
  fail. That was not shown. Migration 0001 grants the repository's role
  every table in the schema, so its privileges alone do not keep a step
  off a table shared by every repository, and showing that each step's
  every statement stays on the repository's own tables, all of whose
  unique constraints carry the repository column, is an audit of code,
  not a property anything holds. So they stay unknown.

A first open sets its repository aside for the repository's kind alone,
and answers any other as it did before this record, so the next open tries
again: an open that keeps failing for a reason nobody can tell goes on
answering, which is the lesser harm. **This is the side the rule errs
on**: a failure that is the repository's own and on no list is retried at
every open, as every failure was before this record, and a new refusal in
the ladder that should set its repository aside gets a name above or the
marker. The server's background loops reopen such a repository at most at
the trigger dispatcher's tick (5 seconds by default) and at each slower
loop's cadence, as before. The start's gate stops the start for the
server's kind, as it stops for a database that does not answer, and sets a
repository aside for the other two, since its passes read files whose
damage has no kind of its own.

A step that fails has rolled its transaction back, but for one case: a
commit whose answer was lost may have landed, and no rollback undoes it.
That is the server's kind, sets nothing aside, and the next open runs the
step again, which a repository migration bears: it is idempotent, and once
it commits it leaves its ledger row, so the next open finds it done.
The repository is then set aside in the process's memory as 0281 says: the
warning at warn (`substrate: repository set aside at its first open`, with
the stage and the error), `substrate_repositories_set_aside`, `set aside`
in `substratectl repository list` once the server gives up its writer
lease, and `503 unavailable` with the reason `set_aside` for every request.
No request and no loop opens it again, an open that was waiting for the one
that failed included, so the step runs again only at the next start, which
tries it once more. A read-only process opened beside the server sets
nothing aside: it runs no start, and the server decides.

0281 kept the open retried because a failure there "may be the database's
for a moment". The reading by kind answers that: such a failure is the
server's or unknown, and the open is retried as before. What is set aside is
a failure named as the repository's, which ran again on every request and
every loop with nothing to show for it.

### Consequences

- Good, because a repository whose first open fails is seen where every
  other set-aside one is, and its failing migration is no longer run again
  by every request and every loop.
- Good, because the server listens no later than before: nothing moves
  into the start.
- Good, because a connection lost, a serialization failure or a canceled
  request during a first open sets nothing aside, and the start's gate no
  longer sets a repository aside for one either.
- Bad, because at the start such a failure now stops the start, where
  before it set the repository aside when the database answered the ping
  right after; the next start tries again.
- Bad, because a refusal the reading cannot tell (a constraint or a value
  the data fails, a database refusal of no listed class, a file's error, a
  plain error of a step's code that no name or marker covers) still runs
  again at every open, every few seconds at the dispatcher's tick.
- Bad, because the other steps of the ladder still answer their open and
  run again at the next one: only the three that read or rewrite the
  repository's rows set it aside.
- Bad, because what a step refused is in the warning alone. The `503`
  names the step that failed and `repository list` reads `set aside`, and
  neither says which row or declaration; before, the client got a plain
  `503` and the log said why at every open.
- Bad, because a repair made in the database while the server runs takes a
  restart, where before the next open found it. A restart alone cures none
  of these refusals: a refused shipped upgrade needs a release whose
  declaration admits the rows, or the rows repaired, and the operations
  guide says so for each.

### Confirmation

`TestARepositoryWhoseFirstOpenFailsIsSetAsideAndTheOthersServe`
(internal/testenv) fails one repository's migration at its first open
beside two whole ones. The two serve. The failed one answers `set_aside`,
reads `set aside` in the operator's list, and is counted by the metric and
named in the warning at the stage `first open`. A second request, a direct
open and the loops' listing run nothing. Its ledger row is not written, and
the next start runs the migration again and serves it.
`TestAnOpenThatWaitedForAFailedFirstOpenRunsNothing` (internal/engine)
holds a second open waiting on the first and fails the first: the second is
refused without running the migration.
`TestAFirstOpenThatFailsForNoFaultOfItsOwnSetsNothingAside` fails a first
open's migration with a real lost connection (its backend terminated while
the maintenance pool answers), an injected serialization failure, a
canceled request, and a commit whose answer the seam loses after Postgres
committed. Each asserts its own cause, that nothing is set aside, and that
the next open serves. `TestAFailureIsReadByItsKind` holds the reading kind by kind,
`TestAMigrationsOwnRefusalIsMarkedOnlyWhereItIsOwn` the marker, and
`TestRealDriverErrorsAreReadByTheirKind` real errors of the driver (a
connection refused, a closed `database/sql` handle).
