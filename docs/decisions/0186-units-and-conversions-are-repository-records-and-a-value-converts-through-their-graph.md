---
status: proposed
date: 2026-10-21
decision-makers: George Antoniadis
---

# 0186. Units and conversions are repository records, and a value converts through their graph

## Context and Problem Statement

[0153](0153-a-quantity-is-exact-digits-and-a-ucum-code-converted-only-within-its-measure.md)
put the unit table inside the engine: a closed list of named measures that
grows only by release. A repository cannot add a unit (a cup of rice, a
vendor's score, a lab's assay unit) or a conversion, so a reading in an
unlisted unit is refused and survives only as `sourceUnit` text. The table is
also copied into the console (`units.table.json`) and served to function code
(`host.units.*`), and the three must agree. On 2026-10-21 the owner asked for
units and conversions to be data. No release stores a quantity yet (release
pull request #878, v0.114.0, is open), so the stored shape can still change.
[The implementation map](../plans/dynamic-units.md) lists every file this
touches.

## Considered Options

- Keep 0153: the engine's closed table, grown by release.
- The engine's table plus user-declared units beside it, which 0153 rejected
  because the table would become per-repository and a removed unit would
  rescale history.
- Unit records that each name a base unit and an exact factor to it: today's
  table stored as data, one star per measure.
- Unit records and conversion records: each conversion is one edge between
  two units, and the server chains the edges into a per-repository graph.

## Decision Outcome

Chosen: unit records and conversion records, converted through a
per-repository graph. A repository adds the unit or conversion it needs
without a release, and the shipped table becomes records it may edit. Edges
beat stars because a private conversion (an assay's rescale, a cup of rice in
grams) links two units without naming a base, and the shipped stars are one
shape the graph holds. Keeping 0153 refuses every unlisted unit until a
release. A user table beside the engine's gives a value two places to look.

### Units

Core gains the trait `substrate.reamde.dev/core/unit`: `name`, `symbol` (what
a title prints), `code` (the UCUM spelling, for interchange), `aliases`,
`scale` and `turn`. A unit's record id is what a quantity stores (`kg`, `lb`,
`Cel`). A write reads a written unit as an id, then as a `code`, then as an
alias, each also after 0153's character normalization (both micro signs as
`u`, `²` and `³` as digits), and stores the id. Ids, codes and aliases are
unique across every live record whose kind binds the trait; a transaction
that leaves two units answering one spelling is refused. `scale: level` marks
a logarithmic unit, whose values do not add, and `scale: powerlevel` the
level of a power, whose values also do not average: the two rules 0153 held
per measure. `turn` is one full circle in the unit (`360` for `deg`), which a
`circular` value needs.

### Conversions

Core gains `substrate.reamde.dev/core/conversion`: `from` and `to`,
references pinned to the `unit` trait, and exactly one of two forms.

- Linear: `factor` and an optional `offset`, each an exact rational written
  as digits or a fraction (`"0.45359237"`, `"5/9"`), meaning
  `to = from * factor + offset`. The factor is above zero, so every linear
  conversion is increasing, which the filter's sign test needs
  (internal/engine/quantity.go:7-12). It inverts exactly.
- Expression: `expr` and `inverse`, CEL over the double `value`, compiled at
  write under the cost limit function CEL already uses
  (internal/vocabulary/function.go:419). Its answers are marked inexact.

### The graph

The linear conversions divide the units into components. Inside one, two
units convert by composing edges with exact rational arithmetic, and each
unit has an exact map to its component's root, the smallest id in byte
order. A transaction that writes a unit or a conversion is refused when a
cycle of linear conversions composes to anything but the identity (`st` to
`lb` to `kg` beside a direct `st` to `kg` of 6.35), so every path between two
units gives one answer. The check runs at the end of the transaction, because
an import may write a conversion before the units it names. A conversion
whose `from` or `to` names no live unit adds no edge.

Expression conversions join components. They serve the conversion of one
value only: `GET /api/v1/units/convert`, `host.units.convert` and the
console's display of one value. Filters, ordering, bounds, a metric's
accepted units and the aggregate read cross linear edges only: a sum, a mean
and a sign test commute with a linear map, not with any function, and an
expression may be decreasing.

`delta: true` on an observed value now marks a difference in any unit, whose
conversion drops every offset on the path. That replaces the
`temperature-difference` measure and also covers a level difference (`dBm`
against `dBW`).

The engine builds the graph from the live rows, caches it per repository and
drops the cache when the fold writes a row of a kind binding either trait. A
transaction that wrote such a row reads its own graph.

### Declarations, metrics and observed values

`measures:` leaves the property dialect, the core property type and the
`metric` trait. A quantity property and a metric declare the same two keys:
`unit`, a unit whose linear component every value's unit must belong to, and
`units`, a closed list of unit ids, `unit` always admitted. With neither, any
unit record is admitted. The loader checks only that each id is spelled as an
id, and the write checks it against the graph, so a declaration may name a
unit the repository does not hold yet. A `min` or `max` converts the value
into the bound's unit and refuses a value it cannot compare.

A metric's `unit` and `units` are unit ids as strings, not references: a
quantity, a trace's `unit`, an aggregate request's `unit` and a declaration
all spell a unit as its id, and the core `metric` trait already contracts
`unit` as a string. A write of an observed value whose `metric` names a live
record binding `metric` refuses a unit that record does not admit. This
reverses one sentence of
[0156](0156-the-forms-are-gauge-amount-counter-and-summary-and-live-on-the-value.md):
the write rules now read the metric, and the observed index still does not.
The managed `measure` leaves the `observed` trait and the sample kinds.

### What replaces each use of the built-in table

| Use today | With the graph |
| --- | --- |
| Write: alias to code, measure, bounds (`coerceQuantity`) | Shape checks stay pure; a write step resolves the spelling to an id and applies `unit`, `units`, the bounds and the metric through the graph |
| Filter: the operand into each stored unit of its measure | The operand into each unit of its linear component, compared per unit; a `delta` row by the factor alone |
| Order: the value at its measure's base | The value at its component's root, at 40 places as today; a unit with no record keys NULL |
| A declared index on a quantity | Refused: one index serves every repository that holds the kind, and the key would depend on one repository's graph. No shipped kind declares one |
| Observed index: a `base` per point, the stamped `measure` | Neither: rollup rows already keep their statistics in their own unit (migration 0013); a circular point's angle reads its unit's `turn` |
| Aggregate read: into the metric's measure | Into the request's `unit`, else the metric's, through the linear component at read; levels refused by `scale` |
| `durationUnit` and `duration` properties | A fixed engine list (`ms`, `s`, `min`, `h`, `d`, `wk`): those answers are computed from instants, not from a stored unit |
| Titles and search text | The unit's `symbol`, read when the record is written |
| `GET /api/v1/units` and `/units/convert` | The repository's units with their root and exact map, its conversions, and conversion through its graph |
| `host.units.convert` and `canonical` | Through the repository's graph; `host.units.measure` becomes `host.units.converts`, whether two units share a component |
| The console's `units.table.json` and `units.prints.json` | Deleted; the console reads `GET /api/v1/units` |

### When a unit or a conversion changes

A stored quantity keeps its digits and its unit id, and no unit or conversion
write rewrites one. A changed factor changes every later filter, order and
aggregate answer, because each converts at read; no derived table holds a
converted number, so nothing is re-derived. The exception is `turn`: a write
that changes it requests the background re-derivation of every observed
kind, as a changed `rollups` on a metric does today. A deleted unit leaves
its values stored and unconverted: a filter in another unit skips them, an
order keys them NULL, and an aggregate that must convert one refuses, naming
the unit. Restoring the record restores all three. A list cursor minted
before such a change is refused, because its order key moved. A title keeps
the symbol it was written with until its record is next written.

### The shipped table

A new sample, `samples.substrate.reamde.dev/units`, ships the kinds `unit`
and `conversion`, binding the two traits, and the table as their records;
`observations` requires it. The kinds get their own sample rather than living
in `observations` because `quantity` is a core datatype any kind may declare:
a repository with a recipe kind needs grams, not metrics and traces.

A test generates the records once from today's table
(internal/vocabulary/unitstable.go). Every code is a unit. Its id is the code
where the code fits the record id alphabet
(internal/vocabulary/naming.go:79), and otherwise a spelling from a written
map (`[lb_av]` is `lb`, `/min` is `1/min`, `%` is `percent`); the UCUM
spelling goes in `code` and today's aliases in `aliases`. Every code with a
factor gets one linear conversion to its measure's first listed code with a
factor (`g` to `kg`), because some bases are not codes the table lists
(`m3/(kg.s)`). A code two measures
list is linked through one of them only: `Cel` through `temperature`, which
`delta` reads as a difference, and `1` through `fraction`, so `count` never
reaches a percent. The prefix rule for arbitrary units becomes explicit
conversions (`mIU/L` to `IU/L`); levels and `rad` get none. The annotated
units the shipped samples write (`{steps}`, `{floors}`) become units too.
After that commit the YAML is the source, and the Go table and the UCUM
grammar are deleted.

### Consequences

- Good, because a repository adds a unit or a conversion as a record, and a
  lab result in a new unit is stored as a value.
- Good, because the engine, the console and function code read one graph, so
  no copy can drift.
- Good, because a corrected factor corrects every answer at once, with no
  re-derivation.
- Good, because 0153's special rules (energy apart from torque, `Gy` apart
  from `Sv`, an annotation apart from its bare unit) become absent edges, not
  code.
- Bad, because a repository without the `units` sample refuses every
  quantity, and a deleted unit refuses every new value in it.
- Bad, because a wrong factor is believed: only the cycle check stands
  between it and every answer.
- Bad, because the UCUM grammar goes: `kg/h` is refused until somebody writes
  a record for it.
- Bad, because a unit id in a declaration is checked only at write, never by
  `substratectl validate`.
- Bad, because the write path, filters and the aggregate read all read the
  graph, so a cache miss reads every unit and conversion row.
- Bad, because an upgrade of the `units` sample leaves a unit or conversion
  the owner edited or deleted as the owner left it
  (internal/engine/vocabularywrite.go:285-286), so a corrected shipped factor
  misses those repositories, and a new shipped conversion that closes an
  inconsistent cycle with an edited one refuses the whole upgrade.
- Bad, because an expression conversion is inexact and is refused wherever
  values are compared or aggregated.

### Confirmation

The implementation adds these tests (the map's layers name their files).
In internal/engine: `TestUnitGraphRefusesAnInconsistentCycle`,
`TestUnitSpellingsAreUniqueAcrossUnitKinds`, `TestQuantityStoresTheUnitID`,
`TestObservedValueOutsideItsMetricsUnitsIsRefused`,
`TestFilterConvertsThroughTheLinearComponent`,
`TestExpressionConversionIsRefusedInAFilterAndAnAggregate`,
`TestChangedConversionMovesNoStoredValue` and
`TestDeletedUnitLeavesItsValuesUnconverted`. In samples:
`TestShippedUnitsKeepMeasuresApart`, which holds pairs the graph must never
connect (`J` and `N.m`, `Gy` and `Sv`, `Hz` and `Bq`, `count` and
`percent`).

## More Information

This record supersedes 0153 once accepted. While it is `proposed`, 0153 stays
`accepted` with no `superseded-by:`, because the docs linter refuses a link
to a successor that is not accepted; the commit that accepts this record sets
0153 to `superseded` with `superseded-by: 0186`. It also changes
[0154](0154-an-observed-value-binds-the-core-observed-trait-which-never-gains-a-property.md),
whose trait loses `measure`, and 0156, whose write rules now read the
metric. Revisit if repositories need conversions that are neither linear nor
a single value's display, such as a mean through a nonlinear conversion.
