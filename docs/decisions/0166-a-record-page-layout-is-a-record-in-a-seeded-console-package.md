---
status: accepted
date: 2026-10-18
decision-makers: George Antoniadis
amended-by: 0191
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
  `chart` (an aggregate read over the record's window: metrics, reducers
  including `durationIn` bands, a bucket and a source policy) or `spans` (a
  coded amount metric drawn as rows of spans; any other form is a notice).
  A related block names its kind by a reference to the kind's declaration,
  as `appliesTo` does. The block shapes are declared, so the engine refuses
  a malformed view on write. A new block type is an upgrade of the console
  package.
- A chart or spans block names each metric by a reference to the metric's
  record, pinned to core's `metric` trait, never by the metric's id alone.
  A record is its kind and its id, so two metric kinds (a verbatim install
  and an import of the observations sample, say) may each hold
  `health/heart_rate`, and only the reference tells them apart. The import
  rehomes the reference with the metric kind it names. A sample's view ids
  carry the sample's authority (`samples.substrate.reamde.dev/health/sleep`)
  for the same reason: `recordview` is a seeded kind no import moves, so
  the rehomed id is what keeps an installed copy's views and an imported
  copy's views apart.
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
- Upgrades: an install or import writes a shipped data record (a view, a
  metric record, any record a closure ships beside its kinds) only where the
  repository's owner never wrote or deleted the one at its id, which the
  record's changelog answers: no entry under a human door (`api`, `console`,
  `substratectl`). The rule is per record, not per property, so an edit, a
  removed property and a deletion (the tombstone) are all the owner's and all
  survive a re-import or an upgrade; a record only the closure, another
  bundle or the engine wrote takes the newer shipped one. Triggers are
  exempt: they name the callables the same closure ships, so the door always
  writes them. `substratectl apply -f` of a sample's files is the owner's own
  write and is how an owner takes a newer shipped view over theirs; for an
  imported sample the files are applied with the sample's authority
  rewritten to the repository's own, as the import rewrote them
  ([bundles](../bundles.md#the-two-doors)). Issue
  #950 holds what is left: telling the owner a newer record shipped and
  showing the difference.
- `consolepreference` stays in core for now. Moving it is a kind move
  ([0078](0078-a-kind-move-is-ordinary-record-writes.md)) that changes the
  reference every console build and script spells, and it shares nothing
  with record views.
- The metric kind of the observations sample gains `bucketPlace`, which
  replaces the console's `category === "sleep"` branches: `end` lands a night
  on the day it ends. The server reads it as the default `bucket.place` of an
  aggregate read, so every client gets the same days.

### Consequences

- Good, because the console needs no code that names a sample. The console
  change that draws the views adds a test that fails the build when console
  code names one again; until it lands, nothing holds that.
- Good, because a sample adds or changes a record page by shipping a
  record, and an owner tailors one (zone bounds from a measured maximum,
  say) by editing it.
- Bad, because a third seeded package widens what every repository holds,
  and the console package joins core and llm as one admission unit.
- Bad, because a newer shipped view never replaces one the owner edited or
  deleted: the owner is not told one shipped, has no diff to read, and takes
  it only by applying the shipped document by hand, rehomed where the sample
  was imported.
- Bad, because a view is checked only as far as the grammar reaches: a
  chart naming a metric record that does not exist, or a property the kind
  does not declare, is admitted and shows nothing.

### Confirmation

`internal/engine/recordview_db_test.go` holds the boot upgrade of a
repository created without the package (its authority row moving from 2 to
3), the refusal of malformed views (a metric named by its bare id or by a
record that is no metric among them), and a re-install that replaces a view
only the closure or the engine wrote and keeps one the owner edited, trimmed
or deleted.
`internal/catalog/recordview_db_test.go` holds the rehoming of the health
sample's views and their metric references, a verbatim install and an
import of the sample keeping four views apart, and the re-import that keeps
an edited and a deleted one. The console change that draws the views
brings the console's own tests:
`web/console/src/console-names-no-sample.test.ts` refuses console code that
names a sample, `web/console/src/components/record-view/` tests each block
type and the rules above, `web/console/src/lib/record-view.shipped.test.ts`
holds every shipped view to them and to metric records a sample ships, and
`web/console/src/pages/record.test.tsx` the page with and without a view.

## More Information

Record 0077 is the precedent for a seeded package beside core. The health
sample's views are `samples/health/views.yaml`.
