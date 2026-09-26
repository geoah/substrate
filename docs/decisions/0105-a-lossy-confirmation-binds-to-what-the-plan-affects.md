---
status: accepted
date: 2026-09-26
decision-makers: George Antoniadis (via the issue-641 agent session)
---

# 0105. A lossy confirmation binds to what the plan affects

## Context and Problem Statement

[0067](0067-a-lossy-conversion-runs-only-with-a-confirmation-bound-to-its-preview.md)
bound the consent to a lossy conversion to the changelog head: `admitConversion`
refused a `confirm: {planHash, changelogSeq}` whenever any write landed after
the preview. On a repository that syncs, the head moves every few seconds, so
the confirmation never lands:
[issue #641](https://github.com/geoah/substrate/issues/641) records fifty
refusals in twenty minutes with every trigger disabled. 0067 named this as
its reopen trigger. This record amends the binding half of 0067 and leaves
the rest of it standing.

## Considered Options

- Keep the head binding and make the CLI retry the preview and confirm
- Bind to the affected kinds: the head of writes to any record of a
  converted kind
- Bind to what the conversion affects: fold into `planHash` a digest of the
  (id, version) pairs of every record a step rewrites and a digest of every
  converted kind's stored declaration, and check the hash alone
- Add the affected state as a new field on the confirmation

## Decision Outcome

Chosen: the hash of a lossy plan covers what the conversion affects, and the
door checks the hash alone. `conversionPlan.wire` counts every step first,
then digests each rewriting step's rows over the same predicate it counted
them with (`affectedRowsQuery`), and hashes the stored declaration row of
each kind a step converts by content, so a re-projection that writes the same
definition back refuses nothing. The digests are read only for a lossy plan
whose work is at or under the ceiling, and never on the boot path, which
takes no confirmation. The door recounts under its locks as before: same hash
means nothing that matters moved.

The wire shape stays. `changelogSeq` keeps its name and its value (the head
the preview counted at) and changes meaning: it dates the preview and binds
nothing, and the door refuses only a seq past the head (`conflict`), which no
preview here could have produced. A mismatched hash is refused with
`conflict` (409), the code a stale confirmation had under 0067, so a client
that re-previews on 409 keeps working; the message names what moved.

Retrying loses to a writer that never pauses. Binding to the kind still
refuses a conversion on a kind a sync writes constantly, even when the synced
rows are not the ones converted. A new field would be a wire change for no
gain: the hash already travels and is already recomputed.

### Consequences

- Good, because a lossy upgrade confirms on a live repository: a write to
  another kind, or to a record of the same kind that no step rewrites, leaves
  the confirmation standing.
- Good, because a write to a record a step rewrites, or a change to a
  converted kind's declaration, still refuses, even when the counts and steps
  read the same.
- Bad, because a lossy preview and its door read every affected row's id and
  version, which is more than a count. The ceiling bounds it where one is
  set; a deployment that removes the ceiling (`SUBSTRATE_CONVERSION_CEILING=0`)
  pays the aggregate over every affected row.
- Bad, because a mismatched hash cannot tell a stale preview from a
  confirmation for another batch, so both answer `conflict`, where 0067
  answered `lossy` for the second.
- Bad, because a write to a record the plan does not rewrite can still change
  what a lossy remap collapses into (a new record holding the target value)
  without refusing, since those records are not rewritten; the consent covers
  the records whose values move.
- Bad, because the shipped upgrade preview's hash, and a lossless plan's,
  cover the steps alone; neither takes a confirmation, so nothing compares
  them with a bound hash.

### Confirmation

`TestLossyConfirmationSurvivesWritesThePlanDoesNotTouch`,
`TestLossyConfirmationRefusesAfterWhatThePlanTouchesMoved`,
`TestLossyConfirmationRefusesAfterANewRecordHoldsTheSourceValue` and
`TestLossyNullConfirmationBindsTheRecordsItClears`
(`internal/engine/convert_db_test.go`) hold both halves;
`TestSampleReimportOverAnEditedCopyNeedsTheConfirmationItPreviewed`
(`internal/catalog`) holds the import door.

## More Information

Reopen trigger: a conversion over records a writer rewrites continuously,
where even the row binding never lands; that is chunked execution, which 0067
left unbuilt.
