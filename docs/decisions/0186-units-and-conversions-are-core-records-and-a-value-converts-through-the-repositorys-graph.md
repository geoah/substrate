---
status: accepted
date: 2026-10-21
decision-makers: George Antoniadis
---

# 0186. Units and conversions are core records, and a value converts through the repository's graph

## Context and Problem Statement

[0153](0153-a-quantity-is-exact-digits-and-a-ucum-code-converted-only-within-its-measure.md)
put the unit table inside the engine: a closed list of named measures that
grows only by release. A repository cannot add a unit (a cup of rice, a
vendor's score, a lab's assay unit) or a conversion, so a reading in an
unlisted unit is refused and survives only as `sourceUnit` text. The table is
also copied into the console (`units.table.json`) and served to function code
(`host.units.*`), and the three must agree. On 2026-10-21 the owner asked for
units and conversions to be data, and answered the first draft's questions
the same day. No release stores a quantity yet (release pull request #878,
v0.114.0, is open), so the stored shape can still change.
[The implementation map](../plans/dynamic-units.md) lists every file this
touches and the order the work lands in.

## Considered Options

- Keep 0153: the engine's closed table, grown by release.
- The engine's table plus user-declared units beside it, which 0153 rejected
  because the table would become per-repository and a removed unit would
  rescale history.
- Unit records that each name a base unit and an exact factor to it: today's
  table stored as data, one star per measure.
- Unit records and conversion records in a sample package a repository
  imports, the first draft of this record.
- Unit records and conversion records as core kinds, seeded in every
  repository: each conversion is one edge, and the server chains the edges
  into a per-repository graph.

## Decision Outcome

Chosen: core unit and conversion records, seeded in every repository and
converted through the repository's graph. A repository adds the unit or
conversion it needs without a release, and the shipped table becomes records
it may edit. Edges beat stars because a private conversion (an assay's
rescale, a cup of rice in grams) links two units without naming a base. Core
beats a sample because `quantity` is a core datatype: with units in a sample,
a repository that had not imported it refused every quantity. Keeping 0153
refuses every unlisted unit until a release, and a user table beside the
engine's gives a value two places to look.

### Units

`substrate.reamde.dev/core/unit` declares `name`, `symbol` (what a title
prints), `code` (the UCUM spelling, for interchange), `aliases`, `scale` and
`turn`. A unit's record id is what a quantity stores (`kg`, `lb`, `Cel`). A
write reads a written unit as an id, then as a `code`, then as an alias, each
also after 0153's character normalization (both micro signs as `u`, `²` and
`³` as digits), and stores the id. Ids, codes, aliases and their normalized
forms are unique across the repository's live units; a transaction that
leaves two units answering one spelling is refused. `scale: level` marks a
logarithmic unit, whose values do not add, and `scale: powerlevel` the level
of a power, whose values also do not average: the two rules 0153 held per
measure. `turn` is one full circle in the unit (`360` for `deg`); a
`circular` value needs one.

### Conversions

`substrate.reamde.dev/core/conversion` declares `from` and `to`, references to
unit records, and exactly one of two forms.

- Linear: `factor` and an optional `offset`, each an exact rational written
  as digits or a fraction (`"0.45359237"`, `"5/9"`), meaning
  `to = from * factor + offset`. The factor is above zero, so every linear
  conversion is increasing, which the filter's sign test needs
  (internal/engine/quantity.go:7-12). It inverts exactly.
- Expression: `expr` and `inverse`, CEL over the double `value`, each typed
  to return a double. The inverse is the author's contract; nothing proves
  it.

### The graph

The linear conversions divide the units into components. Each unit has an
exact affine map to its component's root, the smallest id in byte order, so
two units of one component convert by one composed map, with no all-pairs
matrix. A transaction is refused when a cycle of linear conversions composes
to anything but the identity (`st` to `lb` to `kg` beside a direct `st` to
`kg` of 6.35), so every path gives one answer. "Converts exactly" means a
rational affine path exists; a particular answer may still repeat (`1 cup`
in `L`), and is then rounded and marked inexact, as today.

Expression conversions must form a forest over the components: at most one
expression route joins two components. The reverse walks the same route
backwards through the declared inverses. They convert one value only, in
`GET /api/v1/units/convert`, `host.units.convert` and the console, which asks
the server. Filters, ordering, bounds, a metric's accepted units and the
aggregate read cross linear edges only: an affine map moves a sum by the
count (`a * sum + n * b`), a mean, the extremes and a variance by known rules
(internal/engine/aggregaterollup.go:121-125), and a sign test needs an
increasing map, none of which holds for an arbitrary function. Each
evaluation runs under function CEL's cost limit
(internal/vocabulary/function.go:419) and must return a finite double; a
failure refuses the request, naming the conversion. A difference never
crosses an expression edge.

`delta: true` marks a difference, whose conversion keeps the factor and drops
every offset. An observed value carries it as today, and a quantity property
may now declare it, replacing the `temperature-difference` measure; bounds,
filters, ordering and restores read the property's values as differences.

Graph size, alias count, expression length, route length, rational bit length
and output digits are bounded, and each bound is refused by name (the map
lists them).

### Revisions and caches

Every write of a unit or conversion row moves the repository's unit graph
revision, a counter the fold keeps in the same transaction. A read takes the
revision inside its own snapshot and uses the immutable graph built for that
revision, so a read that began before a conversion committed never sees the
new graph halfway. A transaction that writes such a row uses a graph of its
own, rebuilt on its next read after each change and published only after
commit. A list cursor's signature includes the revision whenever its filter
or its order converts through the graph, so a page after a change is refused.

### Declarations, metrics and observed values

`measures:` leaves the property dialect, the core property type and the
`metric` trait. A quantity property and a metric declare the same two keys:
`unit`, whose linear component every value's unit must belong to, and
`units`, a non-empty list that narrows the component, `unit` always
admitted. With neither, a property admits any unit. A `min` or `max` converts
the value into the bound's unit and refuses a value it cannot compare.

A metric's `unit` and `units`, and a conversion's `from` and `to`, are
references to unit records; a stored quantity keeps the short id. This is
the working decision: the owner asked why references and accepted the
recommendation. With units in core a reference names exactly one record, the
guards below find a metric's units as references, and the console can link
them. A quantity keeps the id because adapters write it on every reading and
a reference there would repeat the kind on millions of rows.

The metric governs every value that points at it. An observed value whose
`metric` names no live metric record is refused, and so is one whose unit
the metric does not admit. A transaction that creates, edits or restores a
metric so that a stored value pointing at it is no longer admitted is
refused, and so is one that deletes a metric any stored value points at.
These checks run at the end of the transaction, against its final state.
This reverses one sentence of
[0156](0156-the-forms-are-gauge-amount-counter-and-summary-and-live-on-the-value.md):
the write rules now read the metric, while the observed index still does not.
The managed `measure` leaves the `observed` trait and the sample kinds.

### Changes, deletions and the guard

A stored quantity keeps its digits and its unit id, and no unit or conversion
write rewrites one. A changed factor changes every later answer, because each
read converts; no derived table holds a converted number.

A unit is **used** when a stored value names it: a quantity anywhere in a
record (nested fields, lists and a reference's link properties included), a
trace's `unit`, a metric's reference, or a declaration's `unit`, `units`,
bound or default. Tombstoned rows count, because a restore brings the value
back. A transaction is refused, at its end, when it deletes a used unit or
leaves two units that were in one linear component apart while both are used
(or one is used and the other is the `unit` a declaration or metric anchors
to). That one rule covers deleting a conversion, deleting an unused unit that
joined two used ones, retargeting a conversion and replacing a linear form
with an expression. The engine counts uses in a derived table the fold keeps,
the way it keeps `refs`.

A circular component holds only offset-free conversions whose turns agree
(`turn` of `to` is `factor` times `turn` of `from`), so one direction has one
reading in every unit. `rad` keeps no rational edge to `deg` and declares
its turn as 2π to 40 places, the precision an order key already reads pi at.
A write that changes a turn or a conversion inside a circular component asks
the background re-derivation of the observed kinds that hold circular rows,
and their aggregate reads answer `ErrObservedRederiving` until it finishes
(internal/engine/aggregate.go:365-395).

A title keeps the symbol it was written with, because the changelog stores
the title (internal/engine/fold.go:131) and a rebuild replays it. Search text
is derived again from the row, so it indexes the unit's id and code, never
its symbol, and a symbol edit or a rebuild never changes a search result.

### What replaces each use of the built-in table

| Use today | With the graph |
| --- | --- |
| Write: alias to code, measure, bounds (`coerceQuantity`) | Shape checks stay pure; a write step resolves the spelling to an id and applies `unit`, `units`, `delta`, the bounds and the metric through the graph |
| Filter: the operand into each stored unit of its measure | The operand into each unit the filtered kinds use that shares its linear component, compared per unit |
| Order: the value at its measure's base | The value at its component's root, at 40 places, which stays approximate as today; refused unless every declaration anchors the property with `unit` or `units` in one component |
| A declared index on a quantity | Refused: the index serves every repository that holds the kind, and the key would depend on one repository's graph. No shipped kind declares one |
| Observed index: a `base` per point, the stamped `measure` | Neither: rollup rows keep their statistics in their own unit (migration 0013); a circular point's angle reads its unit's `turn` |
| Aggregate read: into the metric's measure | Into the request's `unit`, else the metric's, through the linear component at read; levels refused by `scale`; a factor that overflows a float is refused by name |
| `durationUnit` and `duration` properties | A fixed engine list (`ms`, `s`, `min`, `h`, `d`, `wk`): those answers are computed from instants, not from a stored unit |
| `GET /api/v1/units` and `/units/convert` | The repository's units, each with its root and exact map, its conversions, and the graph revision; conversion through the graph |
| `host.units.convert` and `canonical` | Through the repository's graph, loaded at the first call; `host.units.measure` becomes `host.units.converts` |
| The console's `units.table.json` and `units.prints.json` | Deleted; the console caches `GET /api/v1/units` by server, repository and revision, and drops it on a unit or conversion change in the change stream |

### Seeding

Registration writes today's listed table as unit and conversion records in
its creation transaction, right after the shipped vocabulary. Every open
runs the same seed right after the shipped vocabulary upgrade, before any
step that re-derives or validates through the graph. The seed fills a record
that is absent, refreshes one only the seed has written, and never rewrites
one the owner wrote or deleted, the rule a bundle's shipped records already
follow (internal/engine/vocabularywrite.go:285-286). A shipped record that
would collide with the owner's spellings or close an inconsistent cycle is
skipped: the open logs it and `GET /api/v1/units` lists it, and the open
never fails for it.

Every listed code becomes a unit. Its id is the code where the code fits the
record id alphabet (internal/vocabulary/naming.go:79), and otherwise a
spelling from a written map (`[lb_av]` is `lb`, `/min` is `1/min`, `%` is
`percent`); the old code stays as `code`, and every old spelling resolves.
Every code with a factor gets one linear conversion to its measure's first
listed code with a factor, its map divided through the measure's base,
because some bases are not listed codes (`m3/(kg.s)`). A code two measures
list is linked through one of them only: `Cel` through `temperature`, which
`delta` reads as a difference, and `1` through `fraction`, so `count` never
reaches a percent. The prefix rule for arbitrary units becomes explicit
conversions (`mIU/L` to `IU/L`); levels and `rad` get none. The annotated
units the shipped samples write (`{steps}`, `{floors}`) become units too.
The UCUM grammar goes: `kg/h` converts once someone writes its record.

### Consequences

- Good, because a repository adds a unit or a conversion as a record, and a
  lab result in a new unit is stored as a value.
- Good, because the engine, the console and function code read one graph at
  one revision, so no copy can drift.
- Good, because a corrected factor corrects every answer at once, with no
  re-derivation outside circular components.
- Good, because 0153's special rules (energy apart from torque, `Gy` apart
  from `Sv`, an annotation apart from its bare unit) become absent edges, not
  code.
- Bad, because a wrong factor is believed: only the cycle check stands
  between it and every answer.
- Bad, because a compound (`kg/h`) needs a record before anyone may write it.
- Bad, because deleting a metric, a unit or a conversion that stored values
  depend on means purging those values first, tombstones included.
- Bad, because an observed value can no longer be written before its metric.
- Bad, because a unit id in a declaration is checked only against a
  repository, never by `substratectl validate`.
- Bad, because writes, filters and aggregates read the graph, and every open
  runs the seed, so the cold paths cost more than a compiled table.
- Bad, because the owner's edit to a shipped unit stops it receiving a
  corrected factor, and a shipped record the owner's graph contradicts is
  skipped until someone resolves it.
- Bad, because an order by a quantity property that declares no `unit` is
  refused, where 0153 ordered every measure by its base.
- Bad, because an expression conversion is inexact and is refused wherever
  values are compared or aggregated.

### Confirmation

The implementation adds, among the tests the map lists:
`TestUnitGraphRefusesAnInconsistentCycle`,
`TestDeletingAUsedUnitIsRefused`,
`TestDisconnectingUsedUnitsIsRefused` (a deleted conversion, a deleted
intermediate unit, a retarget and a linear form replaced by an expression),
`TestReadKeepsItsSnapshotsGraph`,
`TestCursorAfterAConversionChangeIsRefused` (a quantity filter ordered by
`at`), `TestObservedValueWithoutItsMetricIsRefused`,
`TestMetricEditThatStrandsValuesIsRefused`,
`TestCircularComponentNeedsConsistentTurns`,
`TestSeedSkipsAConflictingShippedEdge` and
`TestShippedGraphMatchesTheOldTable`, which converts every pair of every old
measure through both and must agree until the table is deleted.

## More Information

This record supersedes 0153, which is marked `superseded` with
`superseded-by: 0186` in the commit that accepted this one, when step 4 of
the map switched every repository to the graph. It also changes
[0154](0154-an-observed-value-binds-the-core-observed-trait-which-never-gains-a-property.md),
whose trait loses `measure`, and 0156, whose write rules now read the
metric. The first draft's sample package, string metric units and permitted
deletion were replaced by the owner's answers and by a design review of
2026-10-21. Revisit if repositories need a nonlinear conversion inside a
comparison or an aggregate.
