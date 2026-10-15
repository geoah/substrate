---
status: proposed
date: 2026-10-14
decision-makers: George Antoniadis
---

# 0166. A record page's layout is a record in a seeded console package

## Context and Problem Statement

The console drew a bespoke section on the health sample's `sleep` and
`workout` pages, found by the sample's package, kind names and metric ids,
with the heart rate zones hard-coded. A sample is vocabulary a repository
copies and edits, so code that recognises it by name breaks when the owner
changes it, and every new sample needs a console release. The owner's rule:
the console gives no kind outside the seeded packages special treatment, and
a sample's presentation comes from data kept out of the kind declarations.

## Considered Options

- Bespoke console code per sample, as the health section was.
- Presentation keys on the kind declarations (a `view:` block on a kind).
- A seeded console package holding records that say how a kind's record
  page is laid out.

## Decision Outcome

Chosen: a seeded package, `substrate.reamde.dev/console`, whose first kind
`recordview` says how the console lays out the record page of one kind.
Bespoke code is what the rule forbids. Keys on the declarations would make
every presentation change a vocabulary upgrade of the sample, mix what a
record is with how one console shows it, and need a new dialect key
([0020](0020-dialect-keys-are-reserved-not-tolerated.md)) on every binary.
Records are data the owner edits through the ordinary write path, and a
sample ships them beside its kinds.

- The package is seeded into every repository as `llm` is
  ([0077](0077-the-llm-kinds-live-in-their-own-seeded-package.md)), not put
  in core, and its kinds are `internal`. A repository created before it
  receives it at its next open, as every shipped upgrade does.
- A view names its kind by a reference to the kind's declaration record
  (`appliesTo`), so the sample import's rehoming
  ([0048](0048-providers-are-published-samples-are-copied.md))
  carries it onto the repository's own kind. A view holds an optional name
  and intro, the record properties that bind its window (start, end, who it
  is about, its source) by name, properties to fold away from the generic
  sheet, and an ordered list of sections.
- A section holds one block: `heading`, `text`, `properties`, `related`,
  `chart` (an aggregate read over the record's window: metric ids,
  reducers including `durationIn` bands, a bucket and a source policy) or
  `spans` (a coded amount metric drawn as rows of spans; any other form is
  a notice). A related block names its kind by a reference to the kind's
  declaration, as `appliesTo` does. The block shapes are declared, so the
  engine refuses a malformed view on write. A new block type is an upgrade
  of the console package.
- Four rules the object grammar cannot state are checked by the console,
  which shows a section breaking one as a notice, as it does a block type it
  does not know: a section holds exactly one block (the grammar has no
  union, so a section declares one optional field per block type); a
  related block names an outgoing `property`, or a `kind` and the `via`
  property on it, not both; a `durationIn` reducer names `low` and `high`,
  low under high; a `percentile` reducer names `rank`.
- A view reads the observations of who the record is about, through the
  property the view binds, and the owner's where the view binds none or the
  record names nobody. A record that does not hold the window the view
  binds shows the view without its chart and spans blocks, and no view
  where nothing else remains.
- Record pages only. Saved table views and the streams grid's saved views
  stay where they are. Docs say "record view" for these and keep "saved
  view" for tables.
- Upgrades: an install or import replaces a shipped record of a console
  kind while every property of the stored one was last written by the
  installing bundle (its manager in the property ledger), and leaves it
  alone once any other actor wrote one, so an owner's edit survives a
  re-import or an upgrade. A deleted view counts as absent. To take a newer
  shipped view over an edited one, the owner deletes theirs and imports
  again. `substratectl apply -f` of a sample's files writes views directly
  and skips the rule. This is built to change, together with owner
  corrections to adapter outputs.
- `consolepreference` stays in core for now. Moving it is a kind move
  ([0078](0078-a-kind-move-is-ordinary-record-writes.md)) that changes the
  reference every console build and script spells, and it shares nothing
  with record views.
- The metric kind of the observations sample gains `bucketPlace`, read by
  clients alone, which replaces the console's `category === "sleep"`
  branches: `end` lands a night on the day it ends.

### Consequences

- Good, because the console names no sample, and a test fails the build
  when its code does again.
- Good, because a sample adds or changes a record page by shipping a
  record, and an owner tailors one (zone bounds from a measured maximum,
  say) by editing it.
- Bad, because a third seeded package widens what every repository holds,
  and the console package joins core and llm as one admission unit.
- Bad, because a newer shipped view never replaces an edited one: the owner
  is not told one shipped, has no diff to read, and takes it only by
  deleting theirs.
- Bad, because a property the owner only deleted leaves no manager row, so
  a view whose one edit was a removal reads as unedited and is replaced.
- Bad, because a view is checked only as far as the grammar reaches: a
  chart naming a metric id no record holds, or a property the kind does not
  declare, is admitted and shows nothing.

### Confirmation

`internal/engine/recordview_db_test.go` holds the boot upgrade of a
repository created without the package (its authority row moving from 2 to
3), the refusal of malformed views, and a re-install that replaces an
unedited view and keeps an edited one.
`internal/catalog/recordview_db_test.go` holds the rehoming of the health
sample's views and the re-import that keeps an edited one.
`web/console/src/console-names-no-sample.test.ts` refuses console code that
names a sample, `web/console/src/components/record-view/` tests each block
type and the rules above, and `web/console/src/pages/record.test.tsx` the
page with and without a view.

## More Information

Record 0077 is the precedent for a seeded package beside core. The health
sample's views are `samples/health/views.yaml`.
