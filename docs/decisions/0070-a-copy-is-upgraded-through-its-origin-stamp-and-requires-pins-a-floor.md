---
status: accepted
date: 2026-09-08
decision-makers: George Antoniadis (via the issue-386 agent session)
---

# 0070. A sample copy is upgraded through its origin stamp, and `requiresAtLeast` pins a floor under a requirement

## Context and Problem Statement

[0048](0048-providers-are-published-samples-are-copied.md) made a sample a
copy the repository owns and left it two gaps by construction: no upgrade
path ("a fix to `samples/tasks` never reaches a repository that imported
it"), and a `requires:` that names packages with no version, so a closure
cannot say which dependency version it declares against. Since then the
import stamps the copy's package row with `origin`, `originVersion` and
`originDigest` ([#381](https://github.com/geoah/substrate/issues/381)), and
[0067](0067-a-lossy-conversion-runs-only-with-a-confirmation-bound-to-its-preview.md)
gave the install and import doors a confirmation bound to a preview, noting
that the import door had no preview to confirm against.
[Issue #386](https://github.com/geoah/substrate/issues/386) asks for the
dependency grammar, the sample preview, what a re-import of an edited copy
does, and whether a hand-rehomed apply (`substratectl apply --as`) records an
origin.

## Considered Options

For the dependency constraint:

- A sibling key on the bundle document, `requiresAtLeast: {<package>: N}`, a
  minimum and nothing else
- Retyping `requires:` entries to `{package, minVersion}` objects
- A string grammar inside each entry (`people@>=4`, semver ranges)
- An exact pin

For the sample upgrade:

- Preview the re-import through the existing `PlanBundleUpgrade` over the
  rehomed closure, offered only to a copy whose stamp names the entry, with
  an edited copy reported as discarding its edits and confirmed through 0067's
  contract
- A three-way merge of upstream changes into the edited copy
- Keep "no upgrade path"

For the hand-rehomed apply:

- The request names its `origin` and the server records the claim
- The client stamps nothing
- The server infers the origin by matching digests against the catalog

## Decision Outcome

Chosen: `requiresAtLeast` as a sibling key, the preview through the stamp
with 0067's confirmation, and a client-named `origin` the server records.

**`requiresAtLeast` is a map from a package `requires:` lists to the least
package version that satisfies it.** The loader refuses a key `requires`
does not list, a value that is not an integer of at least 1, and any other
constraint key; `resolveBundle` compares the floor against the stored package
version through `CompareVersions` on every door and at open, naming both
versions. A minimum is the whole grammar because a copy's version only rises:
an exact pin or a ceiling would refuse the next fix to the very package the
closure depends on. Retyping `requires:` was refused because the core
`bundle` kind declares it as a string list, and a string-to-object retype is
a narrowing the boot upgrade refuses on every repository holding a bundle
row; a string grammar spends characters
[0014](0014-authorities-widen-only-outside-the-id-alphabet.md) reserved and
spells one fact two ways. The key is reserved by name under
[0020](0020-dialect-keys-are-reserved-not-tolerated.md), so a binary from
before it refuses a closure that carries one. The shipped samples pin the
versions they were verified against.

**A sample is previewed through its stamp, as the import door would land
it.** `Catalog.Upgrade` rehomes the shipped closure and hands it to
`PlanBundleUpgrade`, so the version diff, the guards and the conversion plan
are the door's own, only when the held copy's `origin` is the entry's id: a
package with no stamp is one the user declared or a copy taken before the
stamp existed, and the shipped sample claims nothing over it. The plan reads
the stamp: `available` when the shipped version is past `originVersion`
(a copy whose own versions ran ahead diffs as current while its content is
not), and `discardsEdits` when the stored closure no longer hashes to
`originDigest`, because a re-import replaces the package whole (0048) and
every edit goes with it. The API keeps that preview on the entry even when
nothing shipped moved, so the re-import has a hash to confirm. The door
refuses an unconfirmed batch that names an origin over an edited copy under
0067's `lossy` code and body, and the plan hash is bound to the current
digest, so one more edit refuses the confirmation as one more write does.
`modified` and `discardsEdits` mean "differs from what the import landed",
not "differs from the shipped closure at `originVersion`": the catalog holds
one version of each sample, so there is no base for a three-way merge, which
is why merging was refused.

**A hand-rehomed apply names its origin and the server records the claim.**
`POST /api/v1/vocabulary/apply` and `/plan` take `origin`; the engine holds
it to a package identity whose package word the batch carries a package or
bundle document for, stamps the row with the batch's package version and a
digest of what landed, and treats a later batch naming an origin over an
edited copy as the import door does. `substratectl apply --as` sends it when
the input carries one package document. Inferring the origin by digest was
refused because a rehomed closure hashes differently from the shipped one and
the catalog holds only the current version.

### Consequences

- Good, because a fix to a shipped sample now reaches every repository that
  imported it, as an offer the owner reads before taking.
- Good, because a re-import over an edited copy is refused until the owner
  confirms the preview that said what goes, with the contract 0067 already
  gave the doors, and the console's Import again sends the same confirmation.
- Good, because a closure can say how new a dependency must be, and the
  console refuses the import first, naming both versions.
- Bad, because `discardsEdits` reports that the copy was edited, not what
  the edits were: the preview's `changes` diff versions, and an edit that
  moved a kind's version past the shipped one reads as no change. The
  changelog is the record of what the edits were.
- Bad, because a floor is honest only as a minimum: a shipped sample that
  breaks against a NEWER dependency has no way to say so, and the guards
  catch it at admission instead.
- Bad, because a copy imported before the stamp existed, and a package
  applied by hand without an `origin`, never receive an offer; a re-import or
  a `--as` apply naming the origin is what stamps them.
- Bad, because a hand `origin` is a claim the server cannot verify beyond
  its shape, and `modified` on such a copy measures drift from what that
  apply landed, not from the shipped sample.

### Confirmation

`TestRequiresAtLeastIsSatisfiedAtOrAboveTheFloor`,
`TestRequiresAtLeastRefusesAPackageBelowTheFloor` and
`TestRequiresAtLeastRefusesWhatItCannotHonor`
(`internal/vocabulary/requires_test.go`) hold the grammar and the check;
`TestSampleUpgradePreviewReadsTheOriginStamp`,
`TestSampleReimportOverAnEditedCopyNeedsTheConfirmationItPreviewed`,
`TestSampleUpgradeIsBlockedByARequiresFloorTheRepositoryDoesNotMeet` and
`TestAHandRehomedApplyRecordsItsOrigin`
(`internal/catalog/sampleupgrade_db_test.go`) hold the preview, the refusal
and the confirmation on both doors against a real engine;
`TestCatalogKeepsThePreviewOfAnEditedCopy`,
`TestCatalogCarriesTheRequiresFloors` and
`TestSchemaApplyAndPlanCarryTheOrigin` (`internal/api`) hold the wire;
`TestImportConfirmsAReimportThatReplacesEdits` and `TestApplyAsSendsTheOrigin`
(`cmd/substratectl/commands`) hold the CLI; the console's
`registry.test.tsx` and `registry.suggested.test.tsx` hold the import door's
confirmation, and `TestWireGolden` with `wire.golden.test.ts` hold the
additive fields (`requiresAtLeast`, `version`, `discardsEdits`).

## More Information

Amends [0048](0048-providers-are-published-samples-are-copied.md): its
"never offered an upgrade" and "no upgrade path at all, by construction"
consequences no longer hold, and its reopen trigger for a copy that tracks
its origin is discharged; the rest of 0048 stands. Discharges 0067's reopen
trigger for the import door. Rests on
[#381](https://github.com/geoah/substrate/issues/381)'s stamp.

Reopen when a sample needs to say it breaks against a newer dependency (a
ceiling, which this record refuses), or when the catalog can serve more than
one version of a sample, which would give an edited copy a base to merge
against instead of a replacement to confirm.
