---
status: accepted
date: 2026-09-15
decision-makers: George Antoniadis
---

# 0083. A repository has one writer, and a second is refused at open

## Context and Problem Statement

One writer process per repository is what keeps the changelog's seqs gapless
([0017](0017-the-changelog-is-one-writers-total-order.md),
[0062](0062-a-write-is-on-disk-before-its-commit-and-its-final-newline-is-the-commit-marker.md)),
and it was held by an exclusive flock on `<repository>/changelog/.lock`. That
lock is per DIRECTORY, so it cannot see a second server running under a DATA
ROOT OF ITS OWN — which is the ordinary shape of a development box, where
`.mise/dev.sh` gave every checkout and worktree a data root of its own and, until
[#554](https://github.com/geoah/substrate/issues/554), one shared database.
Two such servers each ran a boot upgrade, a garbage collection and a trigger
dispatcher over the same rows, and each discovered the other only at its next
append — the seq gap that latched a repository until a restart in
[#539](https://github.com/geoah/substrate/issues/539), and that
[#542](https://github.com/geoah/substrate/pull/542) turned into a repair plus
an error log. `docs/operations.md` said the arrangement was "not refused" and
that you should run one server; a warning nobody reads is what let #539 happen
in the first place.

## Considered Options

- Refuse at open: a Postgres session-level advisory lock per repository, taken
  on a pinned connection when the process opens the repository, before the
  boot import and before any write.
- Warn and continue: keep the catch-up and the error log, and add a louder one
  at boot.
- A heartbeat table: each writer writes `(repository, host, pid, seen_at)` and
  a second one refuses while a row is fresh.

## Decision Outcome

Chosen: **refuse at open, on a Postgres session-level advisory lock**, because
the exclusion has to live where both processes can see each other, and the one
thing two servers on one database certainly share is the database. A lock is
also unambiguous in the way a heartbeat is not: it is released by the session
ending, so a crashed writer's claim disappears with its connection and there
is no staleness window to guess at, no clock to compare and no `seen_at`
threshold to tune. The lease is one pinned connection per process holding one
session lock per repository — one connection because session locks stack on a
session, session-scoped because the lease outlives every transaction written
under it, and pinned because a session lock released from a different pooled
connection is a silent no-op.

The key is `hashtext(current_schema() || '|writer|' || <repository>)::bigint`,
composed the way every other advisory lock the engine takes is, so one
`pg_locks` query reads them all and two substrates sharing a database in
separate schemas do not hold each other's leases.

A refusal is a BOOT refusal: the process does not open the repository, the
error names it and says the rule, and `substrated` exits. A read-only service
(`WithDirectoryReadOnly`, which `repository verify` and `reembed` ride) takes
no lease at all, because running beside the writer is the whole reason it
exists. The commands that already needed the server stopped (`rebuild`,
`rotate-generation`, `snapshot`, `rewrap`, `user reset`) now meet the lease
before they meet the flock, which is earlier and says more.

If the pinned connection later drops, the process FAILS CLOSED: a heartbeat
every five seconds proves the connection alive — a session's advisory locks
can only be released by that session or by its end, so a live pinned session
*is* the lease and nothing has to be read back — and the moment it does not,
every write is refused with `ErrUnavailable` until every lease the process
held is taken again. Every round trip the lease makes is bounded well inside the
interval — an acquisition's as much as a beat's, because an acquisition runs
under the mutex the beat needs and an unbounded one would hold it past every
beat. A host that vanished leaves the socket open and a session on it never
answers, so one that cannot be proven alive is treated as holding nothing,
which is the same fail-closed reading. A connection that dies while the lease
holds nothing is nobody's loss and no beat's business, so the last claim to be
released takes both the connection and the refusal with it — a beat with
nothing to prove returns without trying, so a refusal left standing over an
empty claim set is one nothing would ever clear — and the next acquisition
starts clean on a connection of its own. Every beat retries, whether the
lease was lost on that beat or several beats earlier with no connection to be
had, so a Postgres that went away and came back is recovered from without a
restart. The refusal is an atomic read on the write path, not a round trip,
which is what makes it affordable to take three times per write: at the
write's door, once it holds the changelog lock, and immediately before it
commits. A write can wait a long time between the first and the last — for a
pool connection, for its own body, for another transaction's commit — and one
checked only at the door would land under a lease that went away while it
queued.

The lease is also taken EARLIER THAN THE REPOSITORY EXISTS, on both paths that
publish one. A creation writes the repository's own rows first and the
control-plane row last, and the directory after that; a lease taken at the
directory step would leave a window in which another process sees the row,
takes the lease and opens the repository, and the failing creation's erase
would delete what that process is serving. The boot import of a restored
directory has the mirror of it: the row it builds from the manifest is
published while the changelog table is still empty, so a process that claimed
it would write a directory out of nothing while the restored history sat
unimported. So both take the lease before they write anything, and release it
only if they fail. Those are the only two statements in the engine that insert
a `repositories` row.

### Consequences

- Good, because two servers on one database now fail at boot, at the first
  repository, instead of drifting into a latch or a mutual catch-up.
- Good, because the refusal is diagnosable: it names the repository, states
  the rule, and prints the SQL that finds the holder in `pg_locks`.
- Good, because a crashed writer needs no cleanup — its lock dies with its
  backend — and the next boot simply takes the lease.
- Bad, because it reverses documented behaviour: a deployment that ran two
  servers against one database, however unsupported, now fails to boot the
  second. That is the point, and it is why the release is a breaking one.
- Bad, because one maintenance connection is pinned for the life of the
  process (the pool's cap went from four to five to pay for it).
- Bad, because the fail-closed window is one heartbeat wide: between a
  dropped connection and the next beat, a process that is no longer the writer
  can still write. Closing it completely would cost a round trip per write.
  The cross-writer catch-up stays behind it as the repair, so the window is
  survivable rather than silent.

### Confirmation

`internal/engine/writerlease_db_test.go`: the second writer of a repository is
refused at open with the named error, closing the first releases the lease for
the second, a repository registered after the boot check is leased at its own
open, and a read-only process needs no lease.
`internal/engine/writerlease_internal_db_test.go`: a lost lease retries on
every beat while the database is unreachable and clears once it is back, a
heartbeat whose ping never answers refuses writes inside its own deadline
rather than waiting on it, an acquisition on a stalled session is bounded the
same way and leaves the mutex for the beat that repairs it, a connection that
died while the lease held nothing is replaced at the next acquisition, and a
write whose lease goes after it took the changelog lock is refused before it
commits, with nothing left in either store.
`internal/engine/writerleasecreation_internal_db_test.go`: a creation holds the
lease before its control-plane row exists, a second process cannot claim a
repository mid-creation, a creation that fails hands the lease back, and one
that met a dying lease leaves registration working for the next.
`internal/engine/writerleaseimport_db_test.go`: an import refused by the lease
publishes no row at all, the same directory imports whole once the lease is
free, and a failed import hands the key back.
`internal/engine/foreignwriter_db_test.go` keeps the catch-up honest by opting
out of the lease, which is the only way to reach the condition now.

## More Information

[docs/operations.md](../operations.md#one-writer-per-repository-and-a-second-is-refused)
says what an operator sees and how to read the lock.
The database being per tree is the other half of #554: isolation by default in
`.mise/dev.sh`, so the arrangement this record refuses does not arise on a
development box at all.
