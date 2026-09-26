---
status: proposed
date: 2026-09-26
decision-makers: George Antoniadis
---

# 0106. A direct call of a networked function writes a run row

## Context and Problem Statement

`POST …/core/function/{name}/call` applied a function's effects and wrote no
run row ([#645](https://github.com/geoah/substrate/issues/645)). A provider
function that sends a message or approves a pull request on the owner's
behalf does its real work outside the repository, so after the call nothing
recorded who called it, when, or what it returned. A trigger delivery of the
same function writes a `triggerrun` row in the transaction that commits its
effects.

## Considered Options

- Write a `triggerrun` row with mode `call` and no trigger, for a direct call
  of a function that declares `permissions.network`.
- The same row for every direct call, networked or not.
- A new kind for call runs beside `triggerrun`.

## Decision Outcome

Chosen: a `triggerrun` row with mode `call`, for networked functions only.
A function counts as networked when it declares `permissions.network` or its
`permissions.call` grant reaches, at any depth, a function that does: a
sub-call's effects commit with the root's, so the root's row covers what the
callee sent.
One kind means one read finds everything a callable did: a `referencing`
read on `callableRef` returns its deliveries and its calls together, and
the loop guard that keeps a trigger from ever matching `triggerrun` covers
call runs with no new code. A function with no network grant reaches
nothing outside the repository, and its effects already sit in the
changelog under the token's principal, so a row per call would add a
changelog entry to every pure read-shaped call and record nothing new.

The kind widens to fit (version 18): `trigger` is no longer required, `mode`
gains `call`, `status` gains `failed`, and four properties hold a call's
audit: `caller` (the request's actor), `principal` (the token id), `output`
(kept whole when its JSON is at most 4096 bytes) and `outputBytes`. The
output is never truncated, because a cut JSON value is not the value, and
past the cap only its size lands, so a large answer cannot fail the call's
own commit.

A call that settles writes the row in the transaction that applies its
effects and settles its `Idempotency-Key`. A call whose body ran and failed
writes a `failed` row in a transaction of its own, since the body may have
sent something before it failed. A call refused before the body runs, and a
replayed idempotent outcome, write nothing: nothing went out. Call runs are
not pruned; the per-trigger retention keys on a trigger they do not have.

### Consequences

- Good, because a networked call's audit commits atomically with its effects,
  the same guarantee a delivery has.
- Good, because one `referencing` read on a callable lists both its
  deliveries and its direct calls.
- Bad, because `triggerrun` now holds rows no trigger fired, and a reader
  that assumed `trigger` is always set must allow for its absence.
- Bad, because call runs grow without bound: a script calling a networked
  function in a loop writes one row per call until someone deletes them.
- Bad, because a call of a function without a network grant still writes no
  row, so "who called this" for it is answered only by the principal on its
  effects' changelog entries, and not at all when it applied none.
- Bad, because a trigger can never match a call run, so no automation can
  react to one.

### Confirmation

`TestNetworkedCallWritesRunRow` in `internal/engine/functions_call_db_test.go`
calls a networked function and reads its `ok`, oversized-output and `failed`
rows. `TestCallModeValidatesAndApplies` holds that a call of a function with
no network grant writes none.

## More Information

Revisit when call runs need a retention, or when a function without a
network grant gains an external effect through another door (a host function
that sends).
