---
status: proposed
date: 2026-10-14
decision-makers: George Antoniadis
---

# 0166. A record page's layout is a record in a seeded console package

## Context and Problem Statement

The console's record pages for the health sample's `sleep` and `workout`
kinds had a bespoke "During this" section: heart rate over the record's
window, time in five heart rate zones, a sleep-stage hypnogram. The code
named the sample to find them: the `health` package, the kind names
`sleep` and `workout`, the metric ids `health/heart_rate` and
`health/sleep_stage`, hard-coded zone bounds, and branches on a metric's
`category` of `sleep`. A sample is vocabulary a repository copies and edits,
so code that recognises it by name breaks when the owner renames or extends
it, and every new sample needs a console release. The owner's rule: the
console gives no kind outside the seeded packages special treatment.
Presentation for a sample's kinds comes from data, and stays out of the kind
declarations.

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
  `spans` (a coded metric drawn as rows of spans). The block shapes are
  declared, so the engine refuses a malformed view on write. The object
  grammar has no union, so a section declares one optional field per block
  type; "exactly one block per section" is checked by the console, which
  shows a section with none, with two, or with a type it does not know as a
  notice. A new block type is an upgrade of the console package.
- Record pages only. Saved table views and the streams grid's saved views
  stay where they are. Docs say "record view" for these and keep "saved
  view" for tables.
- Upgrades: an install or import writes a shipped record of a console kind
  only where the repository holds no live record at its id, so an owner's
  edit survives a re-import or an upgrade. A tombstone counts as absent: to
  take a newer shipped view, the owner deletes theirs and imports again.
  This is built to change, together with owner corrections to adapter
  outputs.
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
  has no diff to read and takes the new one only by deleting theirs.
- Bad, because a view is checked only as far as the grammar reaches: a
  chart naming a metric id no record holds, or a property the kind does not
  declare, is admitted and shows nothing.

### Confirmation

`internal/engine/recordview_db_test.go` holds the boot upgrade of a
repository created without the package and the refusal of malformed views.
`internal/catalog/recordview_db_test.go` holds the rehoming of the health
sample's views and the re-import that keeps an edited one.
`web/console/src/console-names-no-sample.test.ts` refuses console code that
names a sample, and `web/console/src/components/record-view/` tests each
block type and the page with no view.

## More Information

Record 0077 is the precedent for a seeded package beside core. The health
sample's views are `samples/health/views.yaml`.
