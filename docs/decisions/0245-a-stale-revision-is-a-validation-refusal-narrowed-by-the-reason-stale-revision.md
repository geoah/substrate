---
status: proposed
date: 2026-11-08
decision-makers: George Antoniadis
---

# 0245. A stale revision is a validation refusal narrowed by the reason `stale_revision`

## Context and Problem Statement

A write of a record whose kind binds `observed` or `revised` is refused when
its `revision` is below the stored one
([0162](0162-an-observed-row-names-its-origin-and-source-and-composes-its-id-from-upstream-data.md)):
`422 validation`, the problem at `props.revision`, and an English message.
The app kits' outboxes must tell this refusal from every other validation
error, because a newer write landed first and the queued one is dropped,
where any other refusal is parked for the owner. Both kits matched the
message (`apps/kit/Sources/SubstrateLocal/Outbox.swift`, and the web kit of
[#1275](https://github.com/geoah/substrate/pull/1275)), and a message is
prose, not a contract: rewording it would send every stale write to the
owner as a problem they cannot fix.

## Considered Options

- A `reason` of `stale_revision` on the `validation` code, at `error.reason`.
- A new error code, such as `stale`.
- A marker on the one problem, in `problemDetails[]`.
- Keep matching the message, or read the stored record after any refusal at
  `props.revision`.

## Decision Outcome

Chosen: the reason `stale_revision` on the `validation` code, at
`error.reason`, the place the three reasons before it
(`observed_rederiving`, `key_refused`, `quota`) already sit. The code, the
status, the message and the problems stay what they were, so a client that
matches the old words keeps working and one that knows no reason acts on
the code, as [the error docs](../api.md#errors) promise. A new code would
change what every older client reads for a refusal it already handles, and
widen a set that is closed at thirteen. A marker in `problemDetails` would
move the shape of every problem detail, and a client would walk a list to
answer a question about the whole refusal. Reading the record after the
refusal costs a round trip and races the next write.

The reason rides the whole refusal whenever one of its problems is a falling
revision, other problems included: fixing the rest still leaves a write a
newer one superseded, so dropping it is right. A write that removes a stored
revision is not stale (no newer write landed; the writer dropped the value)
and carries no reason, so it is parked as before. The engine decides it
(`vocabulary.RevisionProblems` reports the case and the write path sets
`substrate.ValidationError.Reason`), and the API copies the reason from any
validation error, so a later refusal that wants a reason of its own takes
the same path.

### Consequences

- Good, because a client tells a superseded write from a refusal its owner
  must see by one field, with no prose and no extra read.
- Good, because nothing an older client reads changes.
- Bad, because the reason set is now a contract both kits act on: renaming
  `stale_revision` or narrowing when it is set is a break for them, held only
  by the tests below.
- Bad, because the reason names the whole refusal, so a stale write that is
  also malformed is dropped rather than reported; the malformed half is
  never seen by the owner.
- Bad, because the reason is set only where the revision is compared: a
  write its declaration or a required property refuses first never reaches
  the comparison, so it carries no reason even when its revision is stale,
  and is parked.

### Confirmation

`TestRESTStaleRevisionIsValidationWithItsReason` holds the envelope (code,
`error.reason`, the unchanged message and problems) on a create, a put and a
patch; `TestRevisionProblemsReportOnlyAFallingRevisionStale` holds which
cases are stale; `TestObservedWriteRefusesAFallingRevision` and
`TestRevisedKindRefusesAFallingRevision` hold the engine setting it on both
traits and leaving it off a removed revision.

## More Information

The console mirrors the set as `ErrorReason` in
`web/console/src/lib/api/types.ts`. Revisit if a second validation refusal
needs a reason and the two can meet on one write: one reason per refusal
would then have to rank them.
