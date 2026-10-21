# Dynamic units: implementation map

This is the work plan for
[decision 0186](../decisions/0186-units-and-conversions-are-repository-records-and-a-value-converts-through-their-graph.md),
which makes units and conversions repository records and replaces the
engine's closed unit table of
[0153](../decisions/0153-a-quantity-is-exact-digits-and-a-ucum-code-converted-only-within-its-measure.md).
Nothing here is implemented. Line numbers are against `main` at `7d0a0cdfc`.

The work lands in six ordered layers. Each one builds, passes `mise run
test`, `mise run lint` and `mise run kinds:check` on its own, and leaves the
tree releasable. Release pull request #878 (v0.114.0) stays held until the
last layer merges, because layers 3 and 4 change what a stored quantity
holds.

## Open questions for the owner

These are the choices where the record departs from the 2026-10-21 design
defaults, or where the defaults say nothing. Each layer below assumes the
answer the record gives.

1. **A metric's `unit` and `units` are unit ids, not references.** The
   proposal wrote them as references to unit records. The record keeps
   strings because a quantity, a trace's `unit`, an aggregate request's
   `unit` and a property declaration all spell a unit as its id, and core's
   `metric` trait already contracts `unit: string`
   (kinds/substrate.reamde.dev/core/core.yaml:420). References would make
   the metric the one place a unit is spelled as a record path.
2. **The unit and conversion kinds live in a new `units` sample**, which
   `observations` requires, not in `observations` itself. `quantity` is a
   core datatype, and a repository with a recipe kind should not need
   metrics and traces to store grams.
3. **Should registration import the `units` sample?** Without it a fresh
   repository refuses every quantity, where today any code converts.
   Registration already imports the LLM sample. The record leaves it out.
4. **A metric or property with `unit` and no `units` admits every unit in
   `unit`'s linear component.** The defaults admitted the default unit
   alone. Under that rule each of the 58 health metrics that declares
   `measures:` would need a hand-written `units` list, and a writer's unit
   that converts exactly would be refused for no loss of meaning.
5. **A property declares `unit:` as well as `units:`.** The defaults gave
   properties `units:` only. No property in the tree lists `units:` today;
   the eight quantity properties that constrain their unit declare `measures:`
   (samples/health/workout.yaml:97-122, sleep.yaml:88-95), which means "any
   unit of this family", and `unit:` is that meaning.
6. **Expression conversions convert one value only.** They are refused in
   filters, ordering, bounds, a metric's accepted units and the aggregate
   read. Is a CEL conversion still worth shipping on those terms?
7. **Deleting a unit that values use is allowed**, and those values stay
   stored but unconverted. The alternative refuses the delete, which needs a
   scan of every quantity in the repository.
8. **`host.units.*` stays ungated and uncharged**, though it now reads
   repository records (the graph).
9. **The id map below** for the 76 codes that do not fit the record id
   alphabet.

## The unit ids of the shipped table

A code that fits the record id alphabet,
`^[A-Za-z0-9][A-Za-z0-9._~:@/-]*$` (internal/vocabulary/naming.go:79), is
its own id (`kg`, `kW.h`, `mg/dL`, `1`). The rest take the id below, keep
the UCUM spelling as `code`, and every alias today's table lists for them.
Two units the shipped samples write without a table row are added:
`{steps}` as `steps` and `{floors}` as `floors`.

| Code | Id | Code | Id | Code | Id |
| --- | --- | --- | --- | --- | --- |
| `%` | `percent` | `[degF]` | `degF` | `[ppm]` | `ppm` |
| `'` | `arcmin` | `[foz_br]` | `floz_br` | `[ppth]` | `ppth` |
| `''` | `arcsec` | `[foz_us]` | `floz` | `[psi]` | `psi` |
| `/L` | `1/L` | `[ft_i]` | `ft` | `[pt_us]` | `pt` |
| `/d` | `1/d` | `[ft_i]/s` | `ft/s` | `[qt_us]` | `qt` |
| `/h` | `1/h` | `[g]` | `gn` | `[sft_i]` | `ft2` |
| `/min` | `1/min` | `[gal_br]` | `gal_br` | `[stone_av]` | `st` |
| `/s` | `1/s` | `[gal_us]` | `gal` | `[tbs_m]` | `tbsp_m` |
| `/uL` | `1/uL` | `[gal_us]/min` | `gal/min` | `[tbs_us]` | `tbsp` |
| `/wk` | `1/wk` | `[in_i'Hg]` | `inHg` | `[tsp_m]` | `tsp_m` |
| `10*12/L` | `1e12/L` | `[in_i]` | `in` | `[tsp_us]` | `tsp` |
| `10*3/uL` | `1e3/uL` | `[in_i]/h` | `in/h` | `[yd_i]` | `yd` |
| `10*6/uL` | `1e6/uL` | `[kn_i]` | `kn` | `cm[H2O]` | `cmH2O` |
| `10*9/L` | `1e9/L` | `[lb_av]` | `lb` | `dB[SPL]` | `dBSPL` |
| `L/(100.km)` | `L/100km` | `[lbf_av]` | `lbf` | `dB[W]` | `dBW` |
| `V.A{reactive}` | `var` | `[lbf_av].[ft_i]` | `lbf.ft` | `kW.h/(100.km)` | `kW.h/100km` |
| `[Btu_IT]` | `Btu` | `[mi_i]` | `mi` | `m[IU]/L` | `mIU/L` |
| `[Btu_IT]/h` | `Btu/h` | `[mi_i]/[gal_us]` | `mpg` | `min/[mi_i]` | `min/mi` |
| `[Cal]` | `Cal` | `[mi_i]/h` | `mph` | `mL/(kg.min)` | `mL/kg/min` |
| `[HP]` | `hp` | `[nmi_i]` | `nmi` | `mm[Hg]` | `mmHg` |
| `[IU]` | `IU` | `[oz_av]` | `oz` | `s/(100.[yd_i])` | `s/100yd` |
| `[IU]/L` | `IU/L` | `[pH]` | `pH` | `s/(100.m)` | `s/100m` |
| `[IU]/mL` | `IU/mL` | `[ppb]` | `ppb` | `s/(500.m)` | `s/500m` |
| `[MET]` | `MET` | `[cup_us]` | `cup` | `{count}` | `count` |
| `[arb'U]` | `arbU` | `[cup_m]` | `cup_m` | `{score}` | `score` |
| `[cft_i]` | `ft3` |  |  |  |  |

Every id here that is also one of today's aliases (`lb`, `in`, `mi`, `oz`,
`yd`, `degF`, `mmHg`, `inHg`, `IU`, `Cal`, `ppm`, `ppb`, `var`) names the
same unit the alias names today, so no spelling changes meaning. `count`,
which today's table refuses by name, becomes the id of `{count}`. A
conversion's id is `<from>-<to>` (`lb-kg`), which also fits the alphabet. The
generated sample holds 216 units and about 150 conversions: one per code with
a factor, to its measure's first listed code with a factor, plus two for the
assay prefix rule (`mIU/L` and `IU/mL` to `IU/L`).

## Layer 1: core traits, the `units` sample and its generated records

Additive. Nothing reads the new records yet.

- `kinds/substrate.reamde.dev/core/core.yaml`: add the traits
  `substrate.reamde.dev/core/unit` (`name`, `symbol`, `code`, `aliases`,
  `scale`, `turn`) and `substrate.reamde.dev/core/conversion` (`from`, `to`,
  `factor`, `offset`, `expr`, `inverse`) after `metric` (lines 392-430), and
  bump the package `version` (line 72) to 43.
- `internal/vocabulary/`: `TraitUnitCore` and `TraitConversionCore` beside
  `TraitMetricCore`; a new `unitrecord.go` with `ParseRational` (digits or
  `n/d`, no exponent), `CheckUnitRecord(props) []string` (`scale` is
  `level` or `powerlevel`, `turn` above zero, `aliases` strings) and
  `CheckConversionRecord(props) []string` (exactly one form, `factor` above
  zero, `expr` and `inverse` together, each compiling against a CEL
  environment with one double `value` under the cost limit at
  internal/vocabulary/function.go:419).
- `samples/units/`: `bundle.yaml` (package and bundle, version 1),
  `unit.yaml` and `conversion.yaml` (the two kinds; `from` and `to` pin
  `trait: substrate.reamde.dev/core/unit`), and the generated `units.yaml`
  and `conversions.yaml`. Embed the directory in samples/samples.go:32-34.
- `samples/observations/bundle.yaml`: require
  `samples.substrate.reamde.dev/units` at least 1; bump the package version.
- `internal/vocabulary/units_sample_test.go`: `TestUnitsSampleMatchesTheTable`
  writes the two YAML files from internal/vocabulary/unitstable.go under
  `-update` and fails on a difference otherwise, the way `TestUnitsPage`
  (internal/vocabulary/units_generated_test.go:266) writes docs/units.md.
  The id map above lives in that test.

Tests to add: `TestUnitsSampleMatchesTheTable`,
`TestShippedUnitIDsAreRecordIDs`, `TestShippedUnitSpellingsAreUnique` (ids,
codes and aliases), `TestShippedUnitsKeepMeasuresApart` (no path between `J`
and `N.m`, `Gy` and `Sv`, `Hz` and `Bq`, `count` and `percent`, `mm/h` and
`km/h`), and in kinds/observations_test.go `TestObservationsRequiresUnits`
beside `TestHealthRequiresObservations` (line 67).

Size: about 300 lines of Go and YAML by hand, about 3,000 lines of generated
YAML.

## Layer 2: the unit graph, in vocabulary and in the engine

Additive for quantities: writes of unit and conversion records are now
checked, and nothing else reads the graph.

- `internal/vocabulary/unitgraph.go` (pure, no database): `UnitGraph`,
  built by `BuildUnitGraph(units, conversions) (*UnitGraph, []string)`, the
  second value naming every collision and inconsistent cycle. Union-find
  over linear edges with an exact `(factor, offset)` map to each
  component's root (the smallest id in byte order). Methods: `Resolve(spelling)`
  (id, then code, then alias, each also after `normalizeUnitCharacters`,
  moved here from internal/vocabulary/units.go:322), `Linear(from, to,
  delta) (f, o *big.Rat, err)`, `Convert(value, from, to, delta)
  (Conversion, error)` (linear inside a component, else the path with the
  fewest expression edges, ties broken by conversion id, evaluated in
  float64 and marked inexact), `Root(id)`, `Component(id)`, `Symbol(id)`,
  `Scale(id)`, `Turn(id)`, and `Version()`, a hash of what the methods
  answer, for cache keys and cursor signatures.
- `internal/engine/unitgraph.go`: `(ds *dataset) unitGraph(ctx, q)` reads
  the live rows of every kind that implements either trait and caches the
  result on the dataset; `(t *txn) unitGraph()` rebuilds from the
  transaction's own view once the transaction has written such a row. The
  fold marks the transaction (internal/engine/fold.go:490, beside the
  `syncObserved` call at line 572), and the commit drops the dataset's
  cache. A read-only process cannot see the writer's commits, so its cache
  is checked against the highest changelog sequence of the two traits'
  kinds before use.
- `internal/engine/write.go`: after `checkObserved` (line 1119), a unit or
  conversion row is held to `CheckUnitRecord` or `CheckConversionRecord`.
  At the end of the transaction, where `settleObserved` runs
  (`inTx`, internal/engine/dataset.go:733-737), the transaction is refused when
  `BuildUnitGraph` names a problem, so an import that writes a conversion
  before its units is judged whole.

Tests to add. In internal/vocabulary: `TestLinearConversionsComposeExactly`,
`TestLinearConversionInvertsExactly`, `TestDeltaDropsEveryOffset`,
`TestExpressionConversionIsInexact`, `TestNoPathIsRefused`,
`TestShippedConversionsAreConsistent` (builds the graph from the layer 1
YAML). In internal/engine: `TestUnitGraphRefusesAnInconsistentCycle`,
`TestUnitSpellingsAreUniqueAcrossUnitKinds`,
`TestConversionWrittenBeforeItsUnitsIsJudgedAtCommit`,
`TestUnitGraphCacheDropsOnAUnitWrite`,
`TestConversionToADeletedUnitAddsNoEdge`.

Size: about 900 lines of Go, 700 of tests.

## Layer 3: every reader converts through the graph

Stored values still hold UCUM codes until layer 4. Every reader resolves a
stored spelling through `Resolve`, which reads a code as well as an id, so
rows written before and after layer 4 read alike. The table stays in the
tree for the write path.

Filters and ordering, internal/engine/quantity.go:

- `condQuantity` (line 166), `quantityDecls` (258), `groupQuantityDecls`
  (142) and `quantityClause` (411): the operand resolves through the graph,
  and its arms are the units of its linear component, each compared with
  the `value * d - n` sign test at lines 7-12. Groups split on `delta` only,
  since a unit no longer reads in two measures. `storedQuantityUnits` (281),
  `readStoredUnits` (357) and `quantityUnitsClosed` (338) stop serving
  filters; the arm count is capped at `maxQuantityUnits` (333) and refused
  past it.
- `quantityOrderSQL` (523), `quantityBaseSQL` (557), `quantityCaseSQL`
  (579), `declaredArmsFor` (615) and `baseArm` (648): an arm keys a unit by
  its exact map to its component's root through `linearSQL` (736); a unit
  the graph does not hold keys NULL. The `declaredArms` cache (line 575) is
  keyed by the graph's `Version()`. `stripAnnotations` (711) and
  `annotationsSQL` go: an annotation is part of an id now.
- internal/engine/query.go:530-540: `orderSignature` covers the graph's
  `Version()` when the order holds a quantity, so an older cursor fails
  with the existing "cursor does not match" error.
- internal/engine/indices.go:480-491: `indexExpr` refuses an index on a
  quantity property, naming why.

Observed index:

- internal/engine/observedindex.go: `deriveObserved` (293) stops computing
  `base` (line 331), and so does `tracePoints` (399, lines 415 and 440).
  `storedMeasure` (453), `measureFor` (462), `baseValue` (468), `toBase`
  (479) and `declaredMeasure` (242) go. Bump `observedIndexVersion` (line
  56) to 2 so every repository re-derives.
- internal/engine/observedstore.go: the fingerprint drops `base` (lines
  189-191); a circular point's angle is `value * 2pi / turn` of its unit
  (lines 236-240), which `observedRows` carries from the graph, and a unit
  without a `turn` makes the point not circular.
- A write that changes a unit's `turn` queues every observed kind for
  re-derivation (`queueObservedRederivation`,
  internal/engine/observedindex.go:145), the way a metric's `rollups`
  change does (internal/engine/observedsettle.go:1112-1142).

Aggregate read:

- internal/engine/aggregate.go:571-577 and 750-756: `unit` is kept as
  written and resolved in `aggregateUnits`, which has the transaction.
  `durationUnit` (805-814) reads a fixed engine list, `ms`, `s`, `min`, `h`,
  `d` and `wk`, in a new `timeunits.go` that also serves `durationIn`
  (internal/engine/aggregatevalues.go:521-528) and the `duration` branch of
  internal/engine/aggregateproperty.go:278-283.
- internal/engine/aggregatemetric.go: `aggUnits.measure` (1302-1308)
  becomes the component of the answer unit; `factor` (1324-1352) asks
  `Linear(unit, target, delta)`; `aggregateUnits` (1374-1440) takes the
  request's unit, else the metric's `unit`, else the first stream's, and
  refuses a request unit outside that component, naming the units that
  are in it; `resolves` (1442) goes. `levelRefusal` and `checkLevels`
  (1663-1710) read `Scale()` of each value unit. `circularMean` is refused
  where the answer unit has no `turn` (line 1651) and answers in that unit
  rather than `deg` (internal/engine/aggregatevalues.go:248).
- internal/engine/aggregateproperty.go:252-283: the same conversion through
  `Linear`.
- The `rate` reducer's answer unit stays the text `<unit>/s`
  (internal/engine/aggregatevalues.go:146), a label no record needs to back.

API, runner and function code:

- internal/substrate/units.go: `UnitTable`, `UnitMeasure` and `UnitCode`
  become `UnitGraph` (units and conversions), `UnitInfo` (id, kind, name,
  symbol, code, aliases, scale, turn, root, factor, offset) and
  `ConversionInfo`; `UnitConversion` loses `Measure`.
- internal/substrate/dataset.go:25: `Dataset` gains `Units(ctx)` and
  `ConvertUnit(ctx, UnitConvert)`; the engine implements both;
  internal/api/fake_test.go implements both by hand.
- internal/api/units.go: `getUnits` (27-51) and `getUnitsConvert` (56-74)
  call the dataset; the `measure` parameter (line 23) becomes `delta`.
- Regenerate the wire golden (`go test ./internal/substrate/ -run
  TestWireGolden -update`) and change the interfaces in
  web/console/src/lib/api/types.ts:1442-1488 in the same layer, or the
  console's golden test fails.
- internal/runner/protocol.go:190: `Backend` gains `Units(ctx)`;
  internal/engine/runner.go's `callBackend` implements it; `readState`
  (internal/runner/runner.go:1005) reads the graph once per invocation.
  internal/runner/units.go `serveUnits` (28-74) answers from it, and
  `units.measure` becomes `units.converts`. The comment at
  internal/runner/runner.go:1043-1048 still holds: neither gated nor
  charged.
- internal/runner/host.py: `Units` (around lines 641-810) gains
  `converts(a, b)` and loses `measure`; `convert` takes `delta` instead of
  `measure`.
- samples/health/bundle.yaml:226-240: `convertlab` decides mass against
  amount concentration with `host.units.converts(unit, "g/L")` and
  `host.units.converts(unit, "mol/L")` instead of `host.units.measure`;
  bump the health package version.

Test harnesses: `openObserved` (internal/engine/observed_db_test.go:39-62)
and `openObservedIndex` (internal/engine/observed_index_db_test.go:42)
import `samples.substrate.reamde.dev/units` first. Twenty-two
internal/engine `*_db_test.go` files write unit spellings; the ones that
filter, order or aggregate need the units imported or a small fixture of a
few units and conversions (see the risks).

Tests to add: `TestFilterConvertsThroughTheLinearComponent`,
`TestExpressionConversionIsRefusedInAFilterAndAnAggregate`,
`TestChangedConversionMovesNoStoredValue` (a factor change moves a filter's
and an aggregate's answer and no stored digit),
`TestDeletedUnitLeavesItsValuesUnconverted`, `TestQuantityIndexIsRefused`,
`TestCursorAfterAConversionChangeIsRefused`,
`TestCircularMeanReadsTheUnitsTurn`, `TestTurnChangeRederivesObservedRows`,
`TestEditingAConversionChangesNoIndexedRow`, and in internal/api and
internal/runner `TestUnitsRouteAnswersTheRepositorysGraph` and
`TestHostUnitsConvertReadsTheRepositorysGraph`. Rewrite
internal/api/units_test.go and internal/runner/units_test.go.

Size: the largest layer. About 1,500 changed lines of Go and 2,500 of
tests.

## Layer 4: writes store ids, and declarations leave `measures`

Vocabulary:

- internal/vocabulary/quantity.go: `QuantitySpec` (31-46) swaps `Measure`
  for `Unit`; `CheckUnit` (68) and `CheckBounds` (86) take the graph;
  `parseQuantitySpec` (204) reads `unit` and `units` as ids
  (`ValidID`), never against a table; `parseQuantityBound` (304) needs no
  `measures:`; `FormatQuantity` (116) takes a symbol lookup, and its
  no-space rule for angles reads the symbol (`°`, `′`, `″`) instead of the
  codes at line 131; `measureNames` (369) goes.
- internal/vocabulary/load.go: `measures` becomes `unit` in the key sets at
  lines 277, 1204 and 1805, in the property-type projection at 438 and in
  the datatype check at 2149.
- internal/vocabulary/metric.go: `MetricMeasures` (19) becomes
  `MetricUnits`; `MetricContract` (73-80) carries `Units`; `CheckMetric`
  (119-151) takes the graph: `unit` resolves, each of `units` resolves and
  shares `unit`'s component.
- internal/vocabulary/observed.go: `ObservedMeasure` (89-92),
  `ObservedMeasureFor` (530) and the measure in `traceSize` (508-524) go;
  the packed `unit` check (454-461) and `measureFlag` (645) move to the
  engine, which has the graph: `delta` is admitted on any unit, `circular`
  only on a unit with a `turn`.

Vocabulary documents:

- kinds/substrate.reamde.dev/core/propertytype.yaml:94-97: `measures`
  becomes `unit`.
- kinds/substrate.reamde.dev/core/core.yaml: the `metric` trait swaps
  `measures` (line 419) for `units`; the `observed` trait drops `measure`
  from both variants (lines 359 and 386); its comment (321-325) and the
  metric trait's (392-405) follow. Bump the package version again.
- samples/observations/metric.yaml: `measures` becomes `units`, a repeated
  string; observation.yaml:123-128 and trace.yaml:120-125 drop `measure`
  and list it under `retired`; metrics.yaml drops six `measures:` lines.
  Bump the package version.
- samples/health: metrics.yaml drops 58 `measures:` lines; workout.yaml
  (97-122) and sleep.yaml (88-95) swap `measures:` for `unit:` (`m`, `kJ`,
  `1/min`, `percent`); the metric for skin temperature drops
  `temperature-difference` and keeps `unit: Cel`, its values written with
  `delta: true`. Bump the package version.

Engine:

- internal/engine/quantity.go `coerceQuantity` (34-91) keeps the shape and
  digit checks and drops the unit work at lines 65-71 and 84-89. A new
  `(t *txn) resolveQuantities(sp)` in internal/engine/quantitywrite.go,
  called after `prepareObserved` (internal/engine/write.go:855), walks every
  written quantity leaf (nested object fields and lists included, as
  `quantityLeaves` at quantity.go:839 walks stored ones), rewrites the
  spelling to the id, refuses an unknown unit, and applies `unit`, `units`
  and the bounds. A value already stored is never checked again.
- internal/engine/observed.go: `prepareObserved` (34-48) stops handling
  `measure` and the packed unit (the new step resolves it);
  `checkObserved` (57-104) stops stamping `measure` (line 97) and refuses a
  unit the value's metric does not admit, reading the metric through
  `metricContract` (internal/engine/metric.go:25) once per metric per
  transaction; `observedMeasure` (130-145) goes.
- internal/engine/schemadiff.go:1258-1344: the measure-change branches go;
  `quantityNarrows` (quantity.go:764) counts a changed `unit`, a shortened
  `units`, a comparator turned off and a moved bound; `quantityRereads`
  (801), `quantityRereadsDeleted` (821), `quantityMeasureChanges` (857) and
  `specMeasure` (862) go; `quantityRefuses` (789) passes the graph.
- internal/engine/validate.go:895-897, 1107-1109 and 1224-1226: titles and
  search text print the unit's `symbol` from the transaction's graph.

Tests to add: `TestQuantityStoresTheUnitID`,
`TestQuantityCodeAndAliasResolveToTheID`, `TestUnknownUnitIsRefused`,
`TestPropertyUnitAdmitsItsComponent`, `TestPropertyUnitsIsAClosedList`,
`TestBoundRefusesAValueItCannotCompare`,
`TestObservedValueOutsideItsMetricsUnitsIsRefused`,
`TestObservedValueWithAMissingMetricChecksOnlyItsUnit`,
`TestDeltaIsAdmittedOnAnyUnit`, `TestCircularNeedsATurn`,
`TestNarrowingAUnitsListCountsStrandedValues`,
`TestTitlePrintsTheUnitsSymbol`. Rewrite internal/vocabulary/quantity_test.go,
internal/engine/quantity_db_test.go, quantity_internal_test.go,
quantity_observed_db_test.go and metric_db_test.go. kinds/observations_test.go
`TestHealthLabMetricCodesMatchTheirMeasure` (line 167) becomes a check
that each analyte's two metrics have units in different components.

Size: about 1,200 changed lines of Go, 1,500 of tests, and about 80 lines
of YAML.

## Layer 5: the console reads the graph

- web/console/src/lib/api/units.ts (new): fetch `GET /api/v1/units` once
  per session, refetched after a write to a unit or conversion record.
- web/console/src/lib/units.ts: drop the table import (line 23), `MEASURES`
  (72), `lookupMeasure` (100), `isListedUnit` (105), `measureLabel` (152)
  and `measureOf` (160); `unitSymbol` (318), `canonicalUnit` (386),
  `convertUnit` (128), `unitsFor` (351) and `checkQuantity` (462) read the
  graph; `QuantityDeclaration.measures` (56-64) becomes `unit`.
- web/console/src/lib/unit-choices.ts: `converts` compares roots;
  `unitChoices` offers the metric's `units`, else the stored unit's
  component; the hand-written `EVERYDAY` lists give way to metrics'
  `units`.
- web/console/src/lib/record-schema.ts:123-135 and 287-297: `measures`
  becomes `unit`.
- The other importers of `@/lib/units`: components/record/quantity-input.tsx,
  components/identity/quantity-value.tsx, components/property-sheet/sheet-model.ts,
  components/record-view/ (points-chart, trend-block, observed-blocks,
  stat-tile, stats-block), components/observations/ (overlay-chart,
  rollup-table, marks), lib/format.ts, lib/filters.ts, lib/record-view.ts,
  lib/observations.ts, lib/overlay.ts and lib/record-form.ts take the graph
  where they convert or label a unit.
- Delete web/console/src/lib/units.table.json and units.prints.json.

Tests: rewrite web/console/src/lib/units.test.ts against a fixture graph
(spelling to id, exact conversion through a root, `delta`, no conversion
across components) and add unit-choices cases. Check the record and
observation pages with `mise run console:render`.

Size: about 900 changed lines of TypeScript; minus 2,800 lines of JSON.

## Layer 6: delete the table, and write the docs

- Delete internal/vocabulary/units.go, unitstable.go, unitsgrammar.go and
  their tests (units_test.go, units_pin_test.go, units_generated_test.go,
  unitsbounds_test.go, units_catalogue_test.go), and the layer 1 generator.
  `RatDecimal` and `ConvertedDigits` (units.go:1122-1200), which the engine
  still calls, move to internal/vocabulary/quantity.go.
- docs/units.md: how units and conversions work, and the shipped `units`
  sample as a table, written by a `TestUnitsPage` that reads samples/units
  instead of the Go table.
- docs/data-model.md:322 and 410-470 (the quantity type and its
  declaration keys), 512; docs/api.md:558-576, 652, 1568 and "The unit
  table" (2011 on); docs/functions.md:848-863; docs/observations.md:55,
  91-95, 143-151, 211-214, 360, 439 and 644; docs/traits.md:245-248;
  docs/vocabulary.md:403; docs/terms.md:65-68 (drop **measure**, add
  **unit** and **conversion**).
- When 0186 is accepted: set its status, and set 0153 to `superseded` with
  `superseded-by: 0186`, in the same commit.

Size: minus about 5,000 lines of Go, about 400 changed lines of docs.

## Risks

- **Test time.** Each observed test harness would import about 370 extra
  records. CLAUDE.md puts the engine suite at about 106 s; a small fixture
  of the units each test uses keeps that, and the full sample import stays
  in the few tests that read the shipped graph.
- **Dev repositories.** After layer 4 the loader refuses `measures:`, so a
  stored copy of `observations` or `health` in a dev repository parks until
  it is re-imported, and rows stamped with `measure` hold a dropped
  property. No release holds either; `mise run dev:wipe` is the answer.
- **The write path reads more.** Every quantity write resolves through the
  graph and every observed write reads its metric. Both are cached per
  transaction; a provider sync writing thousands of rows should be timed
  before and after layer 4.
- **Stale caches.** A read-only process holding a cached graph answers with
  old factors until it checks the changelog head of the two traits' kinds.
- **Upgrades of the `units` sample.** An install leaves a record the owner
  edited or deleted as the owner left it
  (internal/engine/vocabularywrite.go:285-286). A shipped conversion that
  closes a cycle with an edited one makes the end-of-transaction check
  refuse the whole upgrade.
- **Order SQL size.** An order over a property with many stored units
  carries one arm per unit, up to `maxQuantityUnits` (256).
- **CEL cost.** An expression conversion runs once per converted value,
  under the function CEL cost limit; a slow one slows only the routes that
  convert one value.
- **Layer 3 alone.** Between layers 3 and 4 the readers read UCUM codes
  through `Resolve` while the write path still stores them, so the shipped
  records' `code` values must stay unique; `TestShippedUnitSpellingsAreUnique`
  holds that.
