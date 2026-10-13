---
status: accepted
date: 2026-10-13
decision-makers: George Antoniadis
---

# 0165. A commit in doubt is caught up by the next write, and no write grant names the owner's auth kinds

## Context and Problem Statement

Written after the fact, from PR #764 (commit 2485563a), PR #797 (commit
1ff1b5ed) and issue #827. Two accepted records describe behaviour those
changes replaced, and an accepted record is frozen, so neither can be
corrected in place.

[0062](0062-a-write-is-on-disk-before-its-commit-and-its-final-newline-is-the-commit-marker.md)
says a commit whose answer was lost latches the repository until a restart,
and that the latched refusal answers `500`. A client that disconnected while
its write committed did exactly that (#516): the driver ran the `COMMIT` under
the request's context, the cancel abandoned a commit Postgres had applied,
the directory cut the lines, and every later write was refused until a
restart.

[0080](0080-a-kind-grant-may-glob-and-a-glob-never-reaches-auth-material.md)
says the auth kinds are "reachable only by an entry that spells one out". A
function or agent granted `substrate.reamde.dev/core/token` in
`permissions.writes` could then delete the tokens the owner signs in with
(#135).

## Considered Options

For the commit in doubt:

- Keep 0062's latch until restart.
- Run the commit phase outside the request's context, and let the next write
  append from the table whatever a doubted commit left there.

For a write grant naming an auth kind:

- Keep 0080: a spelled-out entry grants any auth kind.
- Refuse the owner's auth kinds in `permissions.writes` at load, and refuse a
  token delete below the owner tier in the engine whatever the grant says.

## Decision Outcome

Chosen: the second option of each pair.

A write's transaction follows the request's context until its commit phase
starts, and from then on only a 30-second commit budget (`beginWrite`,
`commitBudget` in `internal/engine/dataset.go`), so a client that leaves no
longer cancels a `COMMIT`. A commit that still reports failure with its lines
prepared is marked in doubt, and the next write that appends changelog lines
runs `catchUpBeforePrepare` (`internal/engine/repodir.go`), which appends the
rows the table holds past the file, sealed files included, before its own
prepare; nothing latches. A sealed-only write in between does not close the
gap; the next write with lines does. Three cases
still latch at once, because no later write would repair them: a sealed-only
transaction (no seq tells a later write about it), a commit that would have
published a registry (the served registry may be behind the table's
declarations), and a catch-up that appended the rows and then failed to
mirror `sealed/`. Every latch is `ErrChangelogFileBehind`, which wraps
`substrate.ErrRestartRequired`; the API answers it `503 unavailable` with
`Retry-After: 30` and the message, where 0062 had `500`.

The loader refuses `substrate.reamde.dev/core/token`, `core/credential` and
`core/recoverykey` in a function's or an agent's `permissions.writes`, naming
the kind (`ownerOnlyKinds`, `internal/vocabulary/function.go`), and a stored
declaration of that shape quarantines its package at the next open. The
engine refuses a token delete below the owner tier whatever the grant says,
which covers a function's `delete` effect, an agent's `write` and a policy
judge's accept of a proposed delete. `core/secret` stays reachable by an
entry that spells it out, because it is a bundle's own configuration, and
read grants are unchanged: a read of a token shows its label and a digest,
never the secret. Globs never reached these kinds, as 0080 decided.

Latching until restart was rejected because a client disconnect is ordinary,
and one disconnect cost the repository every write until an operator acted.
Keeping 0080's spelled-out grants was rejected because no function or agent
has a reason to revoke the owner's sign-in or write a credential, and the
engine already refused every non-internal put and patch of these kinds, so
such a grant could only revoke tokens or promise a write nothing honours.

### Consequences

- Good, because a write whose client left during the commit lands, and the
  repository keeps taking writes after a lost commit answer.
- Good, because a refusal only a restart lifts says so: `503` with a
  `Retry-After` and a message naming the restart, not an opaque `500`.
- Good, because installed code cannot sign the owner out, whatever its
  declaration says.
- Bad, because a write may now wait up to the commit budget after its client
  has gone.
- Bad, because the client of a doubted commit is still answered with the
  commit's error although the write may have landed: the next write repairs
  the directory, not that answer.
- Bad, because the write after a doubted commit that did land dials a
  connection of its own, outside the pool, to read the table before its
  prepare.
- Bad, because a package that granted itself a token write stops admitting at
  the next open, and its owner has to edit and reapply it before it runs
  again.

### Confirmation

`TestAWriteContextDetachesFromTheRequestAtTheCommit`,
`TestAWriteWhoseClientLeavesDuringTheCommitLands`,
`TestACommitInDoubtIsCaughtUpByTheNextWrite`,
`TestACommitInDoubtLatchesWhenTheCatchUpFails`,
`TestAVocabularyCommitInDoubtLatchesAtOnce` and
`TestASealedOnlyCommitInDoubtLatchesUntilTheBootRewritesTheFile`
(internal/engine) hold the commit in doubt;
`TestRESTRestartRequiredIsUnavailableNamingTheRestart` (internal/api) holds
the `503`. `TestWritesRefuseTheOwnersAuthKinds` (internal/vocabulary),
`TestAStoredTokenGrantQuarantinesItsPackage`, `TestOnlyTheOwnerRevokesAToken`
and `TestAJudgedProposalCannotRevokeAToken` (internal/engine) hold the grant.

## More Information

Amends 0062's recovery paragraph, its fifth Bad consequence and the `500` in
its Confirmation, and 0080's second rule ("A glob never matches an auth
kind"), whose glob half stands. 0062 and 0080 point here with `amended-by`,
because the rest of each still holds (#827). Reopen the first half if a
second commit point ever exists, for the reason 0062 gives.
