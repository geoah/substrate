---
status: accepted
date: 2026-11-13
decision-makers: George Antoniadis
---

# 0281. A repository the start finds damaged is set aside in memory, and the rest serve

## Context and Problem Statement

The start-up check compares every repository's directory against its rows,
imports every directory with no row, and before it the credential check and
the keying (decision 0261) read every repository's data key. Each returned
at the first repository that failed, and the server did not start: one
damaged directory took every other person's repository offline with it. The
1.0 plan (pull request 1313, F13, row 48, step 9) sets the damaged one aside
and serves the rest, because several people will share a server. It leaves
open what "set aside" is, which failures it covers, what the repository
answers, and how an operator sees it and brings it back.

## Considered Options

- Store the mark: a column on `repositories`, or a table, written by the
  start and read by `substratectl repository list`.
- Decide it again at every start and hold it in the process's memory; the
  operator's list reads it from the writer leases the server already holds
  in Postgres (decision 0083).
- Set aside a repository whose open fails after the start too (a repository
  migration, the shipped vocabulary upgrade).

## Decision Outcome

Chosen: decided at every start and held in memory. What the start does
to one repository runs through one gate: the opening of a wrap that names
this host's key, the writer lease, the keying of a row and of a directory
(under the id the directory is proven to take as well as its name), the
recorded deletion and take, the read of its directory, the cleanup of an
adopted 0.x history, the import of a 0.x directory with no row, the restore
of an archive with no row and of the copy staged for a take, the check of
its rows and the removal of its stages no recorded choice names. The gate
passes over a repository already set aside, so no later pass reads its
directory, changes anything of it, or carries out a deletion or a take
recorded for it: those wait for a start that finds it whole. A failure that
is the repository's own sets it aside with the pass as its stage, and only
a failure that is nobody's (below) stops the start. What is not one
repository's stays outside: whether a wrap that names another key opens
(the host's key, below), the one read of which rows hold a recorded
deletion or take, the listing of the data root and of the archive. Two
outcomes are no failure of the repository and set nothing aside: a
recorded deletion that cannot finish (the repository is the owner's to lose
and served by no one either way, it keeps its mark, and a later start tries
again) and a take whose staged copy the restore would refuse (the
repository stays whole and served, its choice recorded). Nor is the
archive's store not answering anyone's damage: a pass that has written
nothing of the repository yet leaves it for a later start, as the start
leaves an archive whose store does not answer (decision 0262), and one that has written part of it
sets it aside at the stage `archive store`, which names the store, so what
is half written serves nobody until a start finds the store answering. A repository set aside is never
opened by the server (open answers `substrate.ErrSetAside`), listed to no
loop, its name taken by no registration and its writer lease held by no
one, and from then on the server writes nothing in it. What the start committed before the failure (a keying's new id, a
history conversion, part of an import) stays, and the next start carries it
on: each of those steps is resumable already. A directory
no row was proven to be (a refused import, or a directory from before ids
whose data key did not prove it a row's) only claims its name: it keeps a
registration off the name and takes no repository of that name down with
it, because a row holding the name is another repository until a data key
says otherwise. A directory is tied to a row by one of two things only. A
proof: its wrap opens to the row's data key (and a manifest that names an
id is held to the row of that id the same way, the same name and the same
key, before anything is leased, resealed, renamed or set aside under it).
Or the keying's own record: before the keying gives a row from before ids
its id, it records beside the directories, at `repositories/.keying/<name>`
under the data root, that the directory under that name is the repository
of that id. Every later start reads the records before it reads any
directory, so the tie holds whatever the directory then holds (no manifest,
one that does not decode, a directory that cannot be entered), and serving
the row would leave its blobs and its spool behind under the name, so it is
set aside with the directory. The record goes once the directory is under
its id; one naming an id no row of that name holds is a keying that never
committed, and ties nothing. Where neither holds, the directory claims its
name and nothing else, whatever its manifest says in the clear: no row is
set aside for it on a creation time or on an id it does not prove. Nothing about it is stored: the next start checks it again, so a repository repaired while the
others served is served after a restart. A stored mark would be a second
answer to the question the check already answers at every start, and one
that could disagree with it after a repair.

What the server cannot be cured of by leaving one repository out still
stops it: the start canceled, a database that no longer answers a ping
after the failure, another process holding a repository's writer lease or
this one losing its own, a credential key that names another key (the
wrong key, not a damaged row), and a start that set aside every repository
it found. The last keeps a deployment of one repository exactly as it was,
and a host fault (a full disk, a data root mounted wrong) failing every
repository stops the server rather than starting one that serves nothing.

A failure at a repository's first open, after the start, stays what it
was: that request answers it and the next open tries again. The open is
retried on purpose (claimOpen), and a failure there may be the database's
for a moment; setting the repository aside for the process's life would
turn it into an outage only a restart lifts.

Every request for a set-aside repository answers `503 unavailable` with
the reason `set_aside`, a new member of the closed reason set, and a
five-minute `Retry-After`: a token of its own, an app's check, a webhook
addressed by the repository's id (by its name the door's one refusal, so a
prober learns nothing), and a sign-in, a deletion or a second factor's
enrollment once its factors are right. The
`errors` feature moves to revision 3. The operator sees one warning per
repository at the start, the metric `substrate_repositories_set_aside`, and
`set aside` in `substratectl repository list`, read from `pg_locks`: a
running server holds the lease of every repository it serves and none of
one it set aside.

### Consequences

- Good, because one damaged repository no longer takes every other
  repository offline.
- Good, because nothing about a set-aside is stored (the keying's record
  is the keying's own, and goes once a directory is under its id), an
  older server reads the same data, and a repair is checked by the same
  start that set it aside.
- Bad, because a deployment of several repositories that relied on the
  server refusing to start must watch the log line, the metric or the
  list instead.
- Bad, because the repair needs a restart: the check runs only at the
  start.
- Bad, because while a start is still checking, the list reads a
  repository it has not reached yet as set aside too.
- Bad, because a sign-in into a repository whose data key wrap is damaged
  cannot verify the password and fails as a wrong one does; its tokens and
  the operator's log say why.
- Bad, because a keying cut short under a release from before this one
  left no record, so its directory, once it can no longer prove itself by
  its key, claims its name and the row serves without the blobs and spool
  left in it.
- Bad, because a repository that never had a directory under its name
  (one born beside the archive) serves beside a stranger's directory put
  there, which claims the name and nothing of the repository.

### Confirmation

`TestADamagedRepositoryIsSetAsideAndTheOthersServe` (internal/testenv)
starts three repositories once for each stage that sets one aside, has the
two others sign in, read, write and run a trigger, the damaged one answer
`set_aside` and the list name it, and brings the repaired one back with a
restart; `TestADirectoryFromBeforeIDsTheKeyingRefusesIsSetAside` does the
same for the keying; `TestWhatStopsTheStartStillStopsIt` holds what stays
fatal; the tests of `internal/engine/startgate_internal_db_test.go` hold the
gate (a cleanup that fails, or a directory the boot cannot read, sets its
repository aside beside a neighbor that serves; a repository set aside is
neither cleaned up, taken nor deleted; a store that does not answer sets
nothing aside, or sets aside at `archive store`); the keying's evidence is
held by `TestAKeyedRowIsSetAsideWithAnUndecodableDirectory`,
`TestAKeyedRowIsSetAsideWithADirectoryWithNoCreationTime`,
`TestAKeyedRowStaysAsideWithItsDirectoryAtTheNextStart`,
`TestARowWhoseDirectoryLostItsManifestStaysAsideWithIt`,
`TestARowWhoseDirectoryCannotBeReadStaysAsideWithIt`,
`TestEqualCreationTimesDoNotOverrideTwoDifferentKeys`,
`TestAForgedDirectoryUnderAHealthyRowsNameClaimsTheNameAlone`,
`TestAManifestClaimingAnotherRepositorysIDTouchesNothingOfIt` and
`TestAProvenDirectoryOfARepositorySetAsideIsLeftAsItIs`; and `TestNoLoopOpensASetAsideRepository` (cmd/substrated) runs every
background loop the server runs and fails if one opens it.

## More Information

docs/operations.md, "A repository set aside at start-up", lists each stage
with its repair.

An operator command runs in a process of its own and cannot read the
server's memory. A command that writes as the writer takes the lease of
every repository before its start writes to any, and refuses at the first
one the server holds: it has held every repository or written to none.
Taking each only as the start reached it would let the start write a
set-aside repository, which the server holds no lease of, before it met one
the server does. The one that writes from a read-only
open beside the server, `repository reembed`, reads the leases first and
refuses a repository the server holds none of while it holds others'.
