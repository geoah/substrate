---
status: accepted
date: 2026-11-13
decision-makers: George Antoniadis
---

# 0271. An error that may follow a write's commit says so, and a write of a named record takes a retry key

## Context and Problem Statement

[0165](0165-a-commit-in-doubt-is-caught-up-by-the-next-write-and-no-write-grant-names-the-owners-auth-kinds.md)
made a lost commit answer recoverable for the repository, and recorded the
cost for the client: "the client of a doubted commit is still answered with
the commit's error although the write may have landed". That answer was a
`500 internal` like any other, so a client could not tell a write that rolled
back from one that may have landed, and a write of a record it named
(`PUT`, `PATCH`, `DELETE`) took no `Idempotency-Key`, so retrying a guarded
one met the version its own first attempt moved. The proposal is F12 of
[pull request 1313](https://github.com/geoah/substrate/pull/1313).

## Considered Options

- Keep the plain `500`, and document that any server error on a write may
  have succeeded.
- A new error code for a write in doubt.
- A reason, `in_doubt`, on the code the cause already answers with, and a
  retry key on the three writes of a named record.

## Decision Outcome

Chosen: the reason `in_doubt` on the existing code, with a retry key on
`PUT`, `PATCH` and `DELETE`, because a client that knows the reason can act on
it and one that does not still acts on the code it always got, and because the
retry key is what makes acting on it safe.

Every error a write answers once its `COMMIT` was sent wraps
`substrate.ErrWriteInDoubt`: the commit's lost answer, a step after a commit
that landed (the directory's lines, the sealed files, a latch), and a read
after a committed declaration write. The API answers it with the reason
`in_doubt`, on `500 internal` with a message saying what the client may
conclude, or on `503 unavailable` where the repository latched. An error on a
write of one record without the reason rolled the write back. A put, a patch
and a delete each take `Idempotency-Key` as an operation of their own in the
key store, keyed by the record they address and their body, and a repeat
answers the first attempt's record and status.

A new code was rejected because every client would have to learn it before it
could treat the error as a failure at all, and the closed set of codes says
what kind of failure, not what may have happened. Documenting the plain `500`
was rejected because it leaves a client unable to tell the common case, a
rollback, from the rare one.

### Consequences

- Good, because a client tells a write that may have landed from one that did
  not, and retries the first under its key without writing twice.
- Good, because the reason rides the `errors` feature's revision 2 and the
  key the `records` feature's, so a client can check for both.
- Bad, because every write path must keep the guarantee: a new step after a
  commit that can fail, left unwrapped, answers a plain error for a write
  that landed.
- Bad, because a commit error Postgres answered with an error state certainly
  rolled back and is still marked, since the engine treats every commit error
  as in doubt (0165).
- Bad, because the key store holds a row per keyed write for 24 hours, and a
  repository restored from its directory alone forgets them.

### Confirmation

`TestAWriteInDoubtIsMarkedAndItsKeyAnswersIt` and
`TestARetryKeyOnTheWritesOfANamedRecord` (`internal/testenv`) hold the reason
and the key over HTTP against the real engine;
`TestAWriteInDoubtCarriesItsReason` (`internal/api`) holds the mapping to
`500` and `503`.

## More Information

Amends 0165's Bad consequence quoted above; the rest of 0165 stands. The
guide's "What a client may conclude after a failure" (`docs/api.md`) is the
table a client reads.
