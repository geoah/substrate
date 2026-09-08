---
status: accepted
date: 2026-09-08
decision-makers: George Antoniadis (via the issue-152 agent session)
---

# 0067. A lossy conversion runs only with a confirmation bound to its preview

## Context and Problem Statement

[0063](0063-a-property-rename-is-ordinary-record-writes.md) and
[0066](0066-a-backfill-and-an-enum-remap-are-ordinary-record-writes.md) made a
rename, a backfill and an injective remap ordinary record writes at admission,
and 0066 refused the one lossy shape it met (a value renamed onto a value the
declaration keeps) by declaration, leaving the consent that would admit it to
[issue #152](https://github.com/geoah/substrate/issues/152). Two conversions
remove values from the fold: that remap, and dropping a property live records
carry, which was refused with a count and no route through. The upgrade
preview listed renames alone, nothing bounded the work one transaction could
take on, and "lossy" was defined nowhere. Three things had to be chosen: how a
caller consents, what the consent is bound to, and what bounds the plan.

## Considered Options

- A confirmation carrying the previewed plan's hash and the changelog head it
  was counted at, in the existing install and apply request bodies
- A bare flag (`force: true`, `--allow-data-loss` alone) on the same bodies
- A new `…/upgrade` verb that previews and confirms in one exchange
- Keep refusing every lossy change and leave the hand rewrite as the route

For what the consent binds to:

- The changelog head (`changelogSeq`): any write since the preview refuses
- A hash of the affected rows: only a touched record refuses

For the plan's bound:

- A ceiling in records rewritten, with a deployment-wide default
- No ceiling (0063's "nothing caps it")

## Decision Outcome

Chosen: a confirmation carrying the plan's hash and the changelog head, as a
body field (`confirm: {planHash, changelogSeq}`) on `POST
/api/v1/catalog/{id}/install` and `POST /api/v1/vocabulary/apply`, with the
plan itself on every preview (`upgrade` on the catalog read, `GET
/api/v1/vocabulary/upgrade`, and the new `POST /api/v1/vocabulary/plan` for a
hand apply). The engine counts the plan in one place (`conversionPlan.wire`,
`internal/engine/convert.go`) for both previews and both doors, so the hash a
preview hands out is the hash the door recomputes under its locks. A bare flag
would authorize whatever the data had since become; a new verb would split the
install into two doors with two admissions where one body field keeps one. A
lossless plan runs unconfirmed and ignores a confirmation, because there is
nothing to consent to.

**Lossy is judged over the whole plan, against the live records.** A plan is
lossy when a step removes values from the fold: a `null` step (a dropped
property some live record carries), or a remap onto a value another stored
value already maps to (a value the stored declaration keeps, or another
remap's target), which the loader cannot see because it reads one document.
A step touching no live record is not a step, so a remap onto a retained value
nobody holds lands unconfirmed; this narrows 0066's refusal by declaration to
the case where a stored distinction exists to lose. The old values stay in the
changelog either way: a lossy step removes them from the fold, and no surface
claims erasure.

**The consent binds to the changelog head, not to the touched rows.** Any
write moves the head, so a confirmation is refused after a write that changed
nothing the plan touches. That is the conservative side: the counts may have
moved, the preview is a read over the bare pool, and re-previewing costs one
request. A hash over the affected rows would need the plan to name its rows,
and the wire carries counts only, never record ids, so a preview of a
provider's kind with thousands of records stays one small object.

**The ceiling is in records rewritten, 10000 by default.** `work` is the sum
of the steps' counts, an upper bound on the changelog entries the transaction
appends. `SUBSTRATE_CONVERSION_CEILING` sets it, `0` removes it, and a plan
above it is refused on both doors and listed among the previews' `blockers`
with the expand-and-contract alternative. The unit is the one the conversion
pays in (one entry and one row rewrite per record) and the one a person can
read off the preview.

**The boot upgrade never runs a lossy step.** It runs unattended, with nobody
to confirm, so a lossy step refuses the shipped set whole and
`PlanShippedUpgrade` names it among the blockers, cleared by rewriting the
records it counts. The ceiling binds there too.

### Consequences

- Good, because the two conversions that lose values have a route through, and
  the consent covers exactly the plan the owner read, never a later one.
- Good, because one counted plan serves the console dialog, `substratectl
  install --allow-data-loss`, `substratectl apply --allow-data-loss` and the
  boot's refusal, so no surface can disagree about what moves.
- Good, because a transaction's cost is bounded by a number an operator sets,
  rather than by whatever a provider's next closure happens to declare.
- Bad, because a confirmation dies on any intervening write, so an active
  repository (a sync landing records every minute) may need the preview and
  the confirmation in quick succession; the CLI does both in one command for
  that reason.
- Bad, because `BundleUpgrade.renames`, shipped days earlier under a stable
  feature, is replaced by `steps` rather than kept beside it: a rename is a
  step, and two lists of one thing would be the wire forever.
- Bad, because the ceiling refuses a legitimate large conversion outright, and
  the route through (expand, migrate through ordinary writes, contract) is
  slower than one click. Chunked execution stays unbuilt until a repository
  hurts.
- Bad, because the import door takes the same body but a sample has no
  preview to read the hash from, so a lossy re-import is refused with no
  confirmation to give until a sample preview exists.

### Confirmation

`TestLossyPlanRunsOnlyWithAConfirmationBoundToItsPreview`,
`TestNullStepRemovesADroppedPropertyOnConfirmation`,
`TestLosslessPlanInstallsUnconfirmed` and
`TestConversionAboveTheCeilingIsRefused`
(`internal/engine/convert_db_test.go`) hold the refusal without consent, the
refusal after an intervening write and for another plan, the null step's
effects and its replay, the unconfirmed lossless install and the ceiling;
`TestBootUpgradeRefusesAShippedLossyRemap`
(`internal/engine/upgrade_guard_db_test.go`) holds the boot door;
`TestSchemaPlanEndpoint`, `TestSchemaApplyCarriesTheConfirmation` and
`TestCatalogInstallCarriesTheConfirmation` (`internal/api`) hold both bodies
and the `lossy` code; `TestInstallConfirmsALossyUpgradeItPreviewed` and
`TestApplyConfirmsALossyPlanItPreviewed` (`cmd/substratectl/commands`) hold
the CLI's binding to the previewed hash; `TestWireGolden` and the console's
`wire.golden.test.ts` hold the shape.

## More Information

Reopen triggers: a repository where the ceiling binds on a conversion nobody
can stage through expand-and-contract (chunked execution); a preview for a
sample re-import, which would let the import door's confirmation be given;
and a consent bound to the touched rows rather than the head, if intervening
writes turn out to refuse confirmations faster than an owner can re-preview.
