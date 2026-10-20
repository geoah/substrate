---
status: proposed
date: 2026-10-20
decision-makers: George Antoniadis
---

# 0184. A record view grows by adding a field, never by retyping one

## Context and Problem Statement

The review of the record views [0175](0175-a-record-view-draws-tiles-trends-and-tables-from-one-batched-read-per-block.md)
shipped found three things the grammar cannot say. A table lists the
records sharing one property (`sameAs: metric`), so a regional DXA result's
page mixes the trunk's and the arms' values in one line. A chart has no
`empty`, so a sleep a source scored without samples (WHOOP) says "No heart
rate was measured" three times. A lab result's reference range has no tone,
so a cut-off past which a value is abnormal draws as the normal band.

`sameAs` is a scalar string, and every repository that imported the health
sample holds a lab result view naming it. The engine refuses a property
that flips from a scalar to a list while live records hold the scalar
(`internal/engine/schemadiff.go`, a container flip), and the boot upgrade
runs that guard on the seeded console package, so retyping `sameAs` would
park the package at boot on those repositories. `renamedFrom` moves a value
to a new name of the same shape and cannot reshape it either.

## Considered Options

- Retype `sameAs` to a repeated string
- Keep `sameAs` a string and read a comma-joined list from it
- Add a boolean `sameChannel` beside `sameAs`
- Add a repeated `sharing` beside `sameAs`, and mark `sameAs` deprecated

## Decision Outcome

Chosen: add `sharing` and keep `sameAs` as its deprecated one-name spelling,
because it is the only option that upgrades every live repository and keeps
the grammar a list of names.

The console reads `sameAs: x` as `sharing: [x]` and refuses a table naming
both, as a notice. Each name in `sharing` narrows the list: one the record
holds to its value, one it does not to the records holding none, so a blood
test (no channel) lists blood tests and a trunk result lists the trunk's.

The same console package adds `empty` (`say` or `hide`) to a chart, as the
stats, trend and table blocks have it. The health sample adds `tone` (`good`,
`fair`, `poor` or `none`) to a lab result's reference range, absent read as
`good`. Each is a new optional field. The console package moves to 3 and the
health sample to 5, because a same-version change reaches no repository.

Retyping `sameAs` fails at boot on every repository holding a lab result
view. A comma-joined string is an expression in a field the grammar keeps to
names. `sameChannel` solves one case and the next shared property needs
another boolean.

### Consequences

- Good, because every change is additive: the boot upgrade lands console 3
  on a repository holding console 2 views, and each stored view reads back
  and writes as it was.
- Good, because the rule stands for the next change to the grammar: a field
  that must change shape gets a new name beside the old one.
- Bad, because one table rule has two spellings until the engine can reshape
  a value on upgrade; the shipped views use `sharing` alone, and a view an
  owner wrote keeps `sameAs` until they edit it.

### Confirmation

`TestBootUpgradeMovesAConsole2RepositoryWithLiveViewsToConsole3` and the
chart and table cases of `TestRecordViewRefusesAMalformedView` in
`internal/engine/recordview_db_test.go`; the `sharing`, `sameAs` and chart
`empty` cases in `web/console/src/lib/record-view.test.ts`; the chart hiding
cases in `web/console/src/components/record-view/record-views.test.tsx`; and
the range tone cases in
`web/console/src/components/observations/observed-section.test.tsx`.

## More Information

Revisit when a declaration upgrade can rewrite a stored scalar into a list
as ordinary record writes, as `renamedFrom` moves a value
([0063](0063-a-property-rename-is-ordinary-record-writes.md)): `sameAs` can
then be folded into `sharing` and dropped.
