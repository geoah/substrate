---
status: accepted
date: 2026-09-09
decision-makers: George Antoniadis
---

# 0073. Idempotency keys live in a Postgres table, not the changelog

## Context and Problem Statement

[Issue #378](https://github.com/geoah/substrate/issues/378) asked for the
`Idempotency-Key` contract `docs/api.md` already promised: a retried create,
function call, agent call, merge or split answers the first attempt's outcome
instead of running again. The stored key, fingerprint and outcome had to live
somewhere.
[0068](0068-an-accepted-webhook-is-a-pending-entry-in-the-delivery-ledger.md)
had just rejected a Postgres-only table for accepted webhook requests because
such a table fails the fresh-database restore: a repository rebuilt from its
directory alone would not know the request existed. The same objection had to
be answered for keys before PR
[#458](https://github.com/geoah/substrate/pull/458) could pick a store.

## Considered Options

- A Postgres table, `idempotency_keys`, outside the changelog (migration
  `0023_idempotency_keys.up.sql`).
- Entries in the changelog, folded into a table like the delivery ledger of
  [0064](0064-trigger-bookkeeping-is-a-delivery-ledger-folded-from-the-changelog.md),
  with a dialect step.

## Decision Outcome

Chosen: the Postgres table. A key is request bookkeeping with a 24 hour life,
not repository state: it records that one HTTP attempt happened and what it
answered, and nothing a record, a declaration or a delivery carries depends on
it. The changelog is the repository's history and a rebuild reproduces the
repository from it; a key is not part of what a rebuild must reproduce. The
owner's ticket decision drew the same line, separating request deduplication
from recovering a request's saved outcomes and pointing the latter at
[#363](https://github.com/geoah/substrate/issues/363) and
[#370](https://github.com/geoah/substrate/issues/370)'s recovery format.

The cost 0068 refused is accepted here and stated where a client reads it:
`docs/api.md` says a repository restored from its directory alone forgets
every key, and the first retry after such a restore runs once more. Keys
survive a restart and a `repository rebuild`, which replay nothing the keys
answer for. The difference from a webhook request is what is lost: a
forgotten webhook is a delivery that never happens, a forgotten key is one
duplicate the client already had to expect from any server that expires keys.

The table is scoped by row level security like every repository table, the
key binds to the repository and the operation and never to the token, the
outcome is `bytea`, and each attempt holds its row under an `owner` token so a
stale attempt cannot release a successor's row. The migration's comment
carries the rest.

### Consequences

- Good, because the changelog dialect does not move: no new op, no new fold
  effect, no `repository.json` reader requirement.
- Good, because a settled key is read on the pool without the repository's
  write transaction, so a replay takes no lock.
- Good, because expiry is a row's `expires_at` and the GC sweep, not a
  changelog entry that would have to be written to forget something.
- Bad, because a directory-only restore forgets every key, and a client
  retrying across that restore repeats its operation once.
- Bad, because a fresh database and the directory now disagree for 24 hours
  about which attempts happened, which no verify command reports.

### Confirmation

`TestIdempotencyKeySurvivesReopen` and `TestIdempotencyKeyOutlivesTheToken`
(internal/engine) hold what the table keeps across a restart and a new token;
`TestIdempotencyKeyExpiredRowIsDeadBeforeTheSweep` and
`TestIdempotencyKeyExpiredIsSwept` hold the 24 hour life;
`TestRowLevelSecurityIsDeclared` holds the scope. `docs/api.md` states the
restore consequence and `lint:docs` holds the page. No test asserts the table
is absent from the changelog; `lint:migrations` and `frozen:check` hold the
migration.

## More Information

This does not amend 0068: a webhook request is repository state and stays in
the ledger; a key is not. If request bookkeeping ever moves into the
changelog, #363 and #370 own the format question and this record is the one
to supersede. Reopen trigger: a client that needs a retry to be safe across a
directory-only restore.
