# Dynamic units: implementation map

This is the work plan for
[decision 0186](../decisions/0186-units-and-conversions-are-core-records-and-a-value-converts-through-the-repositorys-graph.md),
which makes units and conversions core records and replaces the engine's
closed unit table of
[0153](../decisions/0153-a-quantity-is-exact-digits-and-a-ucum-code-converted-only-within-its-measure.md).
Steps 1 and 2 are implemented; the rest is not. Line numbers are against `main` at
`7d0a0cdfc`.
This revision takes in the owner's answers of 2026-10-21 and a design review
of the first draft.

The work lands in five steps. Each one builds and passes `mise run test`,
`mise run lint`, `mise run kinds:check` and, where it touches the console,
`mise run ci:console`. They are not separately releasable: steps 2 and 4
change behavior, and release pull request #878 (v0.114.0) stays held until
step 4 merges. Steps 1, 3 and 5 change no behavior a user can see, except
the two noted in step 3 and the old API fields step 5 removes.

## Settled

- Units and conversions are the core kinds `substrate.reamde.dev/core/unit`
  and `substrate.reamde.dev/core/conversion`, seeded at registration and by
  every open. There is no `units` sample.
- A metric's `unit` and `units` and a conversion's `from` and `to` are
  references to unit records; a stored quantity keeps the unit id. The owner
  asked why and accepted the recommendation, so this is the working decision.
- Absent `units` admits every unit that converts exactly to `unit`; a listed
  `units` narrows that, and `unit` is always admitted.
- CEL conversions stay, for converting one value only.
- Deleting a unit or conversion that stored values use is refused.
- The id map below; every old code stays as the unit's `code` or an alias.

## Still open

1. **Deleting a metric is refused while any stored value points at it,
   tombstones included**, so deleting a metric means purging its values
   first. Counting tombstones follows the unit guard; a restore is checked
   against its metric anyway, so live values alone would also be coherent.
2. **"The owner" in the seed rule is the human doors** (`api`, `console`,
   `substratectl`; internal/substrate/actor.go:116), as `ownerWrote` reads it
   (internal/engine/vocabularywrite.go:355-368). An edit a function or a
   bundle makes to a shipped unit is refreshed by the next open. The
   alternative leaves alone any record a writer other than the seed touched.
3. **Ordering an observed kind by `value` is refused**, because its `value`
   declares no `unit` and the metric decides the unit per row. A later rule
   could anchor the order on the one metric a filter names.
4. **`host.units.*` needs no read grant and charges one call each** against
   the invocation's call budget, instead of nothing as today.
5. **The limits in step 1** are proposals.

## The unit ids of the shipped table

A code that fits the record id alphabet,
`^[A-Za-z0-9][A-Za-z0-9._~:@/-]*$` (internal/vocabulary/naming.go:79), is
its own id (`kg`, `kW.h`, `mg/dL`, `1`). The rest take the id below, keep
the UCUM spelling as `code`, and every alias today's table lists for them,
so every spelling a writer sends today still resolves.
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
generated seed holds 216 units and 149 conversions: one per code with
a factor, to its measure's first listed code with a factor (its map divided
through the old base, never copied), plus two for the assay prefix rule (`mIU/L` and `IU/mL` to `IU/L`).

## Step 1: contracts, the pure graph and the seed data

No behavior change. Nothing outside the new files and their tests reads them.

- `internal/vocabulary/rational.go`: `ParseRational` (digits or `n/d`, no
  exponent, each part at most 64 digits).
- `internal/vocabulary/unitgraph.go`, pure Go with no database:
  - `UnitRecord` and `ConversionRecord`, read from properties, with
    `CheckUnitRecord` (`scale` is `level` or `powerlevel`, `turn` above
    zero, `aliases` strings) and `CheckConversionRecord` (exactly one form,
    `factor` above zero, `expr` and `inverse` together, each compiling in a
    CEL environment with one double `value` and typed to return a double).
  - `BuildUnitGraph(units, conversions) (*UnitGraph, []Problem)`: a
    traversal of the linear edges that gives each unit an exact affine map to
    its component's root (the smallest id in byte order) and checks every
    cycle on the way; spelling collisions over ids, codes, aliases and their
    normalized forms (`normalizeUnitCharacters`, moved from
    internal/vocabulary/units.go:322); the forest rule for expression
    edges; the circular rule (offset-free conversions and agreeing turns in
    any component holding a `turn`); and the limits below.
  - Methods: `Resolve(spelling)`, `Linear(from, to, delta)` (exact factor and
    offset, offsets dropped for a difference), `Convert(ctx, value, from, to,
    delta)` (linear inside a component; else the one expression route,
    evaluated in float64 under the function CEL cost limit at
    internal/vocabulary/function.go:419, each result finite, `ctx` checked
    between steps, refused for a difference), `Root`, `Component`, `Symbol`,
    `Scale`, `Turn`, and a memo of the SQL arms per component, so arm
    caching lives and dies with one immutable graph.
- The limits, each refused by name: 2,000 units and 4,000 conversions per
  repository; 32 aliases per unit; 128 bytes per id, code or alias (ids are
  already held to 128 by internal/vocabulary/naming.go:84); 1,024 bytes per
  expression; 4 expression edges per route; 4,096 bits for the numerator or
  denominator of any composed map, checked when the graph is built; output
  digits at `MaxDecimalDigits`, through `fitDecimal`
  (internal/vocabulary/units.go:881) as today.
- `internal/engine/unitseed/units.yaml` and `conversions.yaml`: today's
  listed table as records, generated by
  `TestUnitSeedMatchesTheTable` in internal/vocabulary under `-update` from
  internal/vocabulary/unitstable.go, the way `TestUnitsPage`
  (internal/vocabulary/units_generated_test.go:266) writes docs/units.md.
  The id map above lives in that test.

Tests: `TestUnitSeedMatchesTheTable`, `TestShippedGraphMatchesTheOldTable`
(every pair of codes inside every old measure converts through
`ConvertIn` and through the graph to the same exact answer, a difference
included), `TestShippedUnitIDsAreRecordIDs` (alphabet and length, conversion
ids too), `TestShippedSpellingsAreUnique` (normalized forms included),
`TestShippedUnitsKeepMeasuresApart` (no path between `J` and `N.m`, `Gy` and
`Sv`, `Hz` and `Bq`, `count` and `percent`, `mm/h` and `km/h`), and for the
graph `TestLinearConversionsComposeExactly`,
`TestAnInconsistentCycleIsAProblem`, `TestExpressionEdgesMustFormAForest`,
`TestTheReverseRouteUsesTheInverses`, `TestANonFiniteExpressionIsRefused`,
`TestADifferenceNeverCrossesAnExpression`,
`TestCircularComponentNeedsConsistentTurns`,
`TestARepeatingAnswerIsInexact`, and one test per limit.

Size: about 1,300 lines of Go, 900 of tests, 3,000 of generated YAML.

### Details step 1 decided

The map left these open; the step 1 code settles them as follows.

- `BuildUnitGraph` answers `[]UnitProblem` (`Kind`, `ID`, `Message`; an
  empty `ID` is a problem of the whole repository) and a graph built from
  what passes, so a caller decides whether a problem refuses its write. A
  conversion that closes an inconsistent cycle, joins two scales or composes
  past the bit bound is left out of the walk; the problem lands on the
  conversion the walk reaches last, and its message names both routes.
- Every unit of one linear component has one scale, so `Scale` is a
  property of the component and a level never joins a ratio unit. Two levels
  may join (`dBW` to `dBm`, offset 30); the seed ships no such edge.
- A conversion whose `from` or `to` names no unit is a problem, so a
  transaction that deletes a unit deletes its conversions too.
  `CheckConversionRecord` refuses a conversion from a unit to itself.
- `factor`, `offset` and `turn` are strings read by `ParseRational`; a number
  is refused, because a float has already lost digits. Each part of a
  fraction may carry decimals (`1/0.3048`).
- An expression is standard CEL with no extensions over one double `value`,
  its output type exactly `double`, run under the cost limit and interrupt
  frequency a trigger's guard uses. The graph compiles an expression at the
  first conversion that crosses it, never at build. Its answer is the
  shortest decimal that round-trips the float64, and never exact.
- The route bound (`MaxExpressionRoute`) is checked by `Convert`, not by the
  build, which would need every pair of components.
- `Turn` answers a unit's own turn, else the turn its circular component
  converts to, so every unit of a circular component has one.
- `Resolve` trims the spelling and tries ids, then codes, then aliases, as
  written and then normalized. `Convert` takes spellings and resolves them;
  `Linear`, `Root`, `ToRoot`, `Component`, `Symbol`, `Scale` and `Turn` take
  ids.
- An absent `symbol` prints the id; an empty one prints nothing (`1`).
- `Memo` keeps at most 4,096 entries per graph and never stores an error.
- The seed: every unit carries a `name` (a map in the generating test), its
  old print as `symbol` (`{count}` keeps `{count}`), its UCUM spelling as
  `code`, and today's aliases of that code except its own id. `deg`, `arcmin`,
  `arcsec` and `rad` carry turns; `dB` and `pH` are `level`, `dBSPL`, `dBW`
  and `dBm` `powerlevel`. Each code links to the first earlier code of its
  home measure (`MeasureOf`) that `ConvertIn` converts it to, which gives
  149 conversions, the two assay ones included. `internal/engine/unitseed`
  embeds both files for step 2; nothing reads it yet.

## Step 2: lifecycle integration

Behavior changes: every repository holds the two kinds and the seeded
records; writes of them are checked and guarded; one migration and one open
step are added; `GET /api/v1/units` gains fields. Every quantity is still
written and read through the old table.

Vocabulary:

- `kinds/substrate.reamde.dev/core/unit.yaml` declares both kinds (`from`
  and `to` pinned to `substrate.reamde.dev/core/unit`), and
  kinds/substrate.reamde.dev/core/core.yaml:72 bumps the core version.

Storage, migration `0016_unit_graph.up.sql`:

- `unit_graph (repository, revision)`: the counter a fold of a unit or
  conversion row moves, in the same transaction.
- `unit_uses (repository, kind, unit, live, tombstoned)`: how many rows of a
  kind name a unit, live and tombstoned counted apart; a purge removes a
  row's uses. The fold keeps it (internal/engine/fold.go:490, beside the
  `syncObserved` call at line 572) from each row's quantity leaves, nested
  fields, lists and link properties included, a trace's `unit`, and every
  reference to `substrate.reamde.dev/core/unit`. A replay, a boot import
  and `repository rebuild` derive both tables again.

Engine, a new internal/engine/unitgraph.go:

- `(ds *dataset) unitGraph(ctx, tx)` reads `unit_graph.revision` inside the
  caller's transaction and returns the immutable graph built for that
  revision, building it from the same snapshot on a miss. The cache holds
  at most two revisions per repository and at most 256 graphs per process,
  and a build for an older revision never replaces a newer one. List
  (internal/engine/query.go:494) and Aggregate
  (internal/engine/aggregate.go:260) already read in a repeatable-read
  snapshot, so each sees one revision throughout.
- `(t *txn) unitGraph()`: a transaction that writes a unit or conversion
  row marks its graph stale, and rebuilds it at its next read, so an import
  of 366 records builds it once at the end rather than once per record. The
  graph is published to the dataset cache only after commit; a rollback
  drops it.
- `(t *txn) checkUnitGraph()`, run in `inTx` before `settleObserved`
  (internal/engine/dataset.go:733-737): refuses the transaction when
  `BuildUnitGraph` names a problem, and runs the guard. The guard reads
  `unit_uses` and the registry's declarations (`unit`, `units`, bounds and
  defaults) for the used set, and refuses a deleted used unit, or two units
  of one linear component before the transaction that are apart after it
  while both are used or one is used and the other is an anchor (a
  declaration's or a metric's `unit`). A changed factor passes; a retarget,
  a deletion and a linear form replaced by an expression are each judged by
  the components they leave.
- Before the switch, stored values hold UCUM spellings, so the guard maps
  each stored spelling through `Resolve` before it compares.

Seeding, a new internal/engine/unitseed.go:

- `(t *txn) seedUnits()` writes the seed with apply semantics as the seed
  actor: an absent row is written, a row only the seed has written is
  refreshed, and a row `ownerWrote` reports
  (internal/engine/vocabularywrite.go:355-368) is left alone, tombstones
  included. A shipped record whose write would make `BuildUnitGraph` name a
  problem against the repository's graph is skipped, so the transaction's
  final check passes.
- Registration calls it in the creation transaction, after
  `seedShippedSchema` (internal/engine/engine.go:1432).
- `(ds *dataset) ensureShippedUnits(ctx)` joins the open ladder
  (internal/engine/engine.go:1238-1248) right after
  `upgradeShippedVocabulary`, before `checkSearchIndex`,
  `checkObservedIndex` and `checkIndexReprojections`. It logs each skipped
  record and never fails the open for one.
- Test repositories skip the seed unless a test asks for it, as they skip
  the LLM sample (internal/engine/seedproviders.go:129-138), so the engine
  suite does not write 366 records per repository.

API:

- internal/substrate/units.go: `UnitTable` gains `units` (each with id,
  name, symbol, code, aliases, scale, turn, root, factor and offset),
  `conversions`, `revision` and `unseeded` (the shipped records skipped,
  and why) beside its current fields, which stay until step 5.
- internal/substrate/dataset.go:25: `Dataset` gains `Units(ctx)` and
  `ConvertUnit(ctx, UnitConvert)`; the engine implements both, and
  internal/api/fake_test.go implements both by hand.
- internal/api/units.go `getUnits` (27-51) fills the new fields from the
  dataset; regenerate the wire golden and add the fields to
  web/console/src/lib/api/types.ts:1442-1488.

Tests: `TestUnitGraphRefusesAnInconsistentCycle`,
`TestUnitSpellingsAreUniqueInARepository`,
`TestConversionWrittenBeforeItsUnitsIsJudgedAtCommit`,
`TestReadKeepsItsSnapshotsGraph` (a read that began before a conversion
commit converts with the old one), `TestRolledBackGraphIsNeverPublished`,
`TestUnitGraphsOfTwoRepositoriesStayApart`,
`TestDeletingAUsedUnitIsRefused` (a quantity in a nested field, in a list, in
a link property, a trace's unit, a metric's reference and a tombstone each
count), `TestDisconnectingUsedUnitsIsRefused` (a deleted conversion, a
deleted unused intermediate unit, a retarget, a linear form replaced by an
expression, inside one multi-record transaction too),
`TestChangingAFactorIsAdmitted`, `TestUnitUsesSurviveARebuild`,
`TestRegistrationSeedsTheUnits`, `TestEveryOpenFillsAbsentUnits`,
`TestSeedLeavesAnOwnersEditAndDeletion`,
`TestSeedSkipsAConflictingShippedEdge`, `TestRepeatedOpensSeedNothing`,
`TestExportAndImportKeepTheSeededUnits`, and `TestUnitsRouteListsTheGraph`
in internal/api.

Size: about 2,000 lines of Go, 1,800 of tests and 40 of SQL.

### Details step 2 decided

The step 2 code settles these as follows.

- `unit_uses` holds one row per record and stored spelling (`kind`,
  `record_id`, `unit`), not counts per kind. A tombstone keeps its rows and a
  purge deletes them, so the guard needs no live and tombstoned split. The
  fold derives the rows from the values alone: every object with a string
  `value` and a non-empty string `unit`, at any depth, plus the top-level
  `unit` of a kind that binds `metric` or packed `observed`. Each spelling is
  kept as `vocabulary.UnitSpellingKey` (trimmed, both micro signs as `u`, the
  superscript digits as ASCII), and the guard probes a unit's id, code and
  aliases by the same key: a metric keeps the `µg` its writer sent, and that
  is a use of the unit whose code is `ug`. The stored value is not
  rewritten. Rule version 2 (`unitUsesVersion`) is this keying.
- The top-level `unit` case is the one place the derivation reads the
  declaration (`readsTopLevelUnit`). A vocabulary apply or a boot upgrade
  that adds or removes a kind's `metric` or packed `observed` binding
  requests `index_reprojections.units` for every row of the kind,
  tombstones included, and the pass behind the commit re-derives them. The
  guard refuses a unit change while that request stands, so the live fold
  ends where a rebuild does.
- A reference to a unit record is read from `refs` (any source kind but
  `conversion`), not copied into `unit_uses`. A declaration's listed `units`,
  `min`, `max` and quantity `default` are read from the transaction's
  registry when the guard runs.
- Before the switch a metric's `unit` is text, so it counts through
  `unit_uses`. That is step 2's share of the metric rules: refusing to delete
  a metric that values point at, and checking values against their metric,
  stay in step 3's `checkMetricValues`.
- A repository whose rows predate migration 0016 has its uses derived behind
  the open. Migration 0016 also adds `unit_uses_index` (the rule version
  `unitUsesVersion`) and `index_reprojections.units`. `checkUnitUses` in the
  open ladder queues every kind for the pass, as `checkObservedIndex` does.
  While a `units` request is pending, the guard refuses with a conflict any
  transaction that affects a unit. A creation and a rebuild write the
  version, because they derive every row as they fold.
- Every changed fold of a unit or conversion row moves `unit_graph.revision`
  by one: create, update, restore, tombstone and purge. A replay therefore
  ends at the same revision. The service cache forgets the repository at a
  rebuild and at a creation all the same. The cache lives on the service: two
  revisions per repository, 256 graphs per process, least recently used out
  first.
- The end of the transaction (`checkUnitGraph`, from `inTx` before
  `settleObserved`) refuses only problems that the graph the transaction
  found did not name. A binary whose rules tighten therefore never locks a
  repository out of its units. Graph problems are validation errors (422).
  The use guard answers a conflict (409).
- The guard first finds the affected units of the found graph: deleted, a
  spelling that now names another unit or none, or a member of a linear
  component that splits. It then probes their ids, codes and aliases in
  `unit_uses` (one index probe each), `refs` and the declarations. It refuses
  a used unit that is deleted or re-spelled, and two used units that end in
  different components. A transaction that touches no component and deletes
  nothing runs no query.
- The seed plans outside any transaction first, so an open with nothing to
  seed takes no changelog lock. The owner rule is one changelog query for
  the human doors over both kinds, purges included. A row is current when
  every shipped property is stored with an equal value. A put merges, so a
  refresh never removes a property a later seed drops. A non-owner
  tombstone is written again, which resurrects it. Conflicts are found with
  one build of all candidates, and only if that names a new problem, one
  build per candidate in seed order.
- `unseeded` lists what the last open's seed skipped, kept in memory on the
  dataset and filtered to records still absent. A record the owner holds is
  never listed: that is the owner's choice, not a conflict.
- `WithUnitSeed` controls the seed. It is on by default and off in
  `OpenForTest`. The testenv, providertest and catalog harnesses open with the
  production default, so they seed. The seed adds about 1.4 s to a
  registration on this box (365 puts through the generic put path: manager
  rows, hydration, row loads); the unit hooks are not a measurable part of it.
- `Dataset.Units` answers the graph with `Measures` and `Aliases` empty;
  `getUnits` fills those from the old table. `Dataset.ConvertUnit` converts
  through the graph and leaves `UnitConversion.Measure` empty; nothing routes
  to it until step 3.
- Both kinds are `purpose: internal`; a unit titles itself
  `{name|localName}`, a conversion `{localName}`.

## Step 3: readers, writers and clients prepared behind the switch

No behavior change while the switch is off, which it is by default, with two
exceptions: the loader admits the new declaration keys `unit` and `delta` on
a quantity property, inert and validated as new keys land
([0020](../decisions/0020-dialect-keys-are-reserved-not-tolerated.md)), and
the `metric` trait and the sample `metric` kind gain `units`, a repeated
reference.

The switch is an engine option, `WithUnitGraph`, and a field the server
reports at `GET /.well-known/substrate/server.json`, which the console
reads before it chooses its unit code. Each item below is a second code path
the switch selects; the tests for it run with the switch on.

Writes:

- A new internal/engine/quantitywrite.go: `(t *txn) resolveQuantities(sp)`,
  called after `prepareObserved` (internal/engine/write.go:855), walks every
  written quantity leaf (nested fields and lists, as `quantityLeaves` at
  internal/engine/quantity.go:839 walks stored ones), stores the id, refuses
  an unknown unit, and applies `unit`, `units`, `delta` and the bounds.
  `coerceQuantity` (internal/engine/quantity.go:34-91) keeps only its shape
  and digit checks on this path. A value already stored is never checked
  again by a write that does not carry it.
- Metric rules at the end of the transaction, in `checkUnitGraph`'s
  neighbour `checkMetricValues`: every observed row the transaction wrote is
  checked against its metric's final state (a live record binding `metric`,
  admitting the unit); every metric the transaction created, edited,
  restored or deleted is checked against the distinct units of the rows
  pointing at it, live and tombstoned, read through the reference index the
  metric property already has (internal/engine/indices.go referenceIndexStatements).
- internal/engine/observed.go: `checkObserved` (57-104) stops stamping
  `measure` (line 97); `delta` is admitted on any unit, `circular` only on a
  unit with a `turn`.
- Titles print the unit's symbol from the transaction's graph
  (internal/engine/validate.go:895-897, 1107-1109); search text indexes the
  id and the code (validate.go:1224-1226), never the symbol.

Filters and ordering, internal/engine/quantity.go:

- `condQuantity` (166) and `quantityClause` (411): the operand resolves
  through the graph; its arms are the units `unit_uses` lists for the
  filtered kinds that share the operand's linear component, so units nobody
  uses never reach the SQL; each arm keeps the `value * d - n` sign test
  (lines 7-12); a `delta` row or property compares by the factor alone. The
  256-arm cap (`maxQuantityUnits`, line 333) counts used units only.
  `readStoredUnits` (357) gives way to `unit_uses`.
- `quantityOrderSQL` (523) through `baseArm` (648): an order is refused
  unless every candidate declaration names `unit` or `units` and all of them
  lie in one component; an arm keys a unit by its exact map to that
  component's root through `linearSQL` (736), at the 40 places of
  `orderPlaces` (727), which is approximate as it is today. The global
  `declaredArms` cache (575) gives way to the graph's own memo.
- internal/engine/query.go:530-540: `orderSignature` covers the graph
  revision whenever the filter or the order converts through the graph,
  references to filtered records included.
- internal/engine/indices.go:480-491: `indexExpr` refuses an index on a
  quantity property, naming why.

Observed index:

- internal/engine/observedindex.go: `deriveObserved` (293) and
  `tracePoints` (399) stop computing `base` (lines 331, 415 and 440); a
  circular point's angle is `value * 2pi / turn`, which the offset-free rule
  makes one direction in every unit of its component
  (internal/engine/observedstore.go:236-240), and the fingerprint drops
  `base` (189-191). `observedIndexVersion` (line 56) moves to 2 at the
  switch.
- A write that changes a turn, or a conversion inside a circular component,
  queues the observed kinds holding circular rows for re-derivation
  (`queueObservedRederivation`, internal/engine/observedindex.go:145), and
  `checkObservedReady` (internal/engine/aggregate.go:365-395) holds their
  reads until it finishes.

Aggregate read:

- internal/engine/aggregate.go:571-577 and 750-756: `unit` is resolved in
  `aggregateUnits`, which has the transaction; `durationUnit` (805-814),
  `durationIn` (internal/engine/aggregatevalues.go:521-528) and the
  `duration` branch of internal/engine/aggregateproperty.go:278-283 read a
  fixed list, `ms`, `s`, `min`, `h`, `d` and `wk`, in a new
  internal/engine/timeunits.go.
- internal/engine/aggregatemetric.go: `aggUnits` (1302-1308) holds the
  answer unit's component; `factor` (1324-1352) asks `Linear`; the affine
  rules of `convertStats` (internal/engine/aggregaterollup.go:121-130) stay
  as they are, now over composed maps; `aggregateUnits` (1374-1440) takes
  the request's unit, else the metric's, else the first stream's, and
  refuses a request unit outside that component, naming the units in it.
  `levelRefusal` and `checkLevels` (1663-1710) read `Scale`. `circularMean`
  needs a `turn` on the answer unit (line 1651) and answers in that unit
  rather than `deg` (internal/engine/aggregatevalues.go:248).
- A converted value or factor past float64's range is refused by name, not
  answered as null (internal/engine/aggregatevalues.go:466-474).
- The `rate` reducer keeps answering in the text `<unit>/s`
  (internal/engine/aggregatevalues.go:146), a label no record backs.

Function code:

- internal/runner/protocol.go:190: `Backend` gains `Units(ctx)`;
  internal/engine/runner.go's `callBackend` implements it, and `readState`
  (internal/runner/runner.go:1005) loads the graph at the first unit call
  and keeps it for the invocation. `serveUnits`
  (internal/runner/units.go:28-74) answers from it, charges one call each,
  and answers `units.converts` in place of `units.measure`.
- internal/runner/host.py `Units` (around lines 641-810): `convert` takes
  `delta` instead of `measure`; `converts(a, b)` replaces `measure`.
- internal/api/units.go `getUnitsConvert` (56-74) converts through
  `ConvertUnit`, with `delta` in place of `measure` (line 23).

Console:

- web/console/src/lib/api/units.ts (new): `GET /api/v1/units`, cached by
  server, repository and graph revision, dropped on any change of a unit or
  conversion record in the change stream and on a repository switch.
- web/console/src/lib/units.ts and lib/unit-choices.ts gain graph-backed
  versions of `unitSymbol` (318), `canonicalUnit` (386), `convertUnit`
  (128, exact through roots; an expression route asks the server),
  `unitsFor` (351), `checkQuantity` (462) and `unitChoices`, chosen by the
  server's switch field; record-schema.ts:123-135 and 287-297 read `unit`
  and `delta`.

Tests, each with the switch on: `TestQuantityStoresTheUnitID`,
`TestOldCodesAndAliasesResolveToTheID`, `TestUnknownUnitIsRefused`,
`TestPropertyUnitAdmitsItsComponent`, `TestPropertyUnitsNarrows`,
`TestPropertyDeltaReadsDifferences` (bounds, filter, order and a restore),
`TestBoundRefusesAValueItCannotCompare`,
`TestObservedValueWithoutItsMetricIsRefused`,
`TestObservedValueOutsideItsMetricsUnitsIsRefused`,
`TestMetricEditThatStrandsValuesIsRefused` (create, edit, restore, delete,
and a metric written after its values in one transaction),
`TestFilterConvertsThroughTheLinearComponent`,
`TestUnusedUnitsNeverWidenAFilter`,
`TestExpressionConversionIsRefusedInAFilterAndAnAggregate`,
`TestOrderWithoutAnAnchorIsRefused`,
`TestCursorAfterAConversionChangeIsRefused` (a quantity filter ordered by
`at`), `TestQuantityIndexIsRefused`, `TestChangedConversionMovesNoStoredValue`,
`TestAffineSumsAcrossTwoEdges`, `TestOverflowingFactorIsRefused`,
`TestCircularRawAndRollupAgreeAfterATurnChange` (and after a restart),
`TestHostUnitsLoadsTheGraphOnce`, `TestTitlePrintsTheSymbolAndSearchTheID`,
and console cases for the graph client (spelling to id, exact conversion
through a root, `delta`, no conversion across components, cache dropped on a
change).

Size: the largest step. About 2,800 lines of Go, 900 of TypeScript and 2,800
of tests.

## Step 4: the switch

Behavior changes: all of it, at once.

- The switch turns on by default and the server reports it; the console
  takes the graph path.
- Vocabulary documents: kinds/substrate.reamde.dev/core/propertytype.yaml:94-97
  swaps `measures` for `unit` and `delta`; core.yaml drops `measures` from
  the `metric` trait (line 419), turns its `unit` (420) into a reference,
  keeps `units`, and drops `measure` from both `observed` variants (lines
  359 and 386), with the comments at 321-325 and 392-405; the loader drops
  `measures` from its key sets (internal/vocabulary/load.go:277, 438, 1204,
  1805 and 2149). Bump the core version.
- samples/observations: metric.yaml drops `measures` and makes `unit` a
  reference; metrics.yaml writes `unit` as a reference
  (`substrate.reamde.dev/core/unit/Cel`) and drops six `measures:` lines;
  observation.yaml:123-128 and trace.yaml:120-125 drop `measure` and list it
  under `retired`. Bump the package version.
- samples/health: metrics.yaml drops 58 `measures:` lines and writes each
  `unit` as a reference; `health/skin_temperature_deviation` keeps `unit: K`
  (metrics.yaml:267) as a reference, its values written with `delta: true`;
  workout.yaml:97-122 and sleep.yaml:88-95 swap `measures:` for `unit:`
  (`m`, `kJ`, `1/min`, `percent`). In bundle.yaml, `convertlab` reads the
  id out of the metric's `unit` reference in `target_unit` (lines 241-246)
  and decides mass against amount concentration with `host.units.converts`
  (226-240). Bump the package version.
- The observed index re-derives every row at the next open
  (`observedIndexVersion` 2).
- Tests that relied on compounds, measures or string metric units move to
  ids and references; the observed harnesses (`openObserved`,
  internal/engine/observed_db_test.go:39-62, and `openObservedIndex`,
  internal/engine/observed_index_db_test.go:42) ask for the seed or a small
  fixture.

The rollback boundary: a repository written after this step holds unit ids
(`percent`, `1/min`) and closures without `measures`, which an older binary
reads through the old table and loader. No release sits on either side, so
a development repository is reset with `mise run dev:wipe` rather than
migrated, and a rollback past this step restores a backup taken before it.

Size: about 700 lines of Go, 250 of YAML and 2,000 lines of tests changed.

## Step 5: removal

No behavior change, except that `GET /api/v1/units` loses its old fields.

- Delete internal/vocabulary/units.go, unitstable.go, unitsgrammar.go and
  their tests (units_test.go, units_pin_test.go, units_generated_test.go,
  unitsbounds_test.go, units_catalogue_test.go), the switch and every old
  code path it selected, and `TestUnitSeedMatchesTheTable`;
  internal/engine/unitseed/ becomes the source of the shipped units.
  `RatDecimal`, `ConvertedDigits` and `fitDecimal`
  (internal/vocabulary/units.go:881-1200) move to
  internal/vocabulary/quantity.go.
- internal/substrate/units.go drops `Measures` and the `Aliases` map;
  regenerate the wire golden; web/console/src/lib/api/types.ts follows.
- Delete web/console/src/lib/units.table.json and units.prints.json and the
  table code in lib/units.ts and lib/unit-choices.ts; the remaining
  importers of `@/lib/units` (components/record/quantity-input.tsx,
  components/identity/quantity-value.tsx,
  components/property-sheet/sheet-model.ts, the record-view and observations
  charts, lib/format.ts, lib/filters.ts, lib/record-view.ts,
  lib/observations.ts, lib/overlay.ts, lib/record-form.ts) read the graph.
- Docs: docs/units.md becomes how units and conversions work plus the
  shipped seed as a table, written by a `TestUnitsPage` that reads
  internal/engine/unitseed; docs/data-model.md:322, 410-470 and 512;
  docs/api.md:558-576, 652, 1568 and "The unit table" (2011 on);
  docs/functions.md:848-863; docs/observations.md:55, 91-95, 143-151,
  211-214, 360, 439 and 644; docs/traits.md:245-248; docs/vocabulary.md:403;
  docs/terms.md:65-68 (drop **measure**, add **unit** and **conversion**);
  CLAUDE.md's registration paragraph names the seeded units.
- When 0186 is accepted: set its status, and set 0153 to `superseded` with
  `superseded-by: 0186`, in the same commit.

Size: minus about 6,000 lines of Go and 2,800 of JSON; about 500 lines of
docs.

## Benchmarks before step 4 merges

Cold and warm graph reads; a registration (the seed in the creation
transaction); an open of a repository whose seed is current; a provider sync
writing a few thousand observations; an import of several hundred units and
conversions; and reads in many repositories at once against the graph
cache. Each is measured on `main` and on the branch with the switch on.

## Risks

- **Test time.** The full seed is 366 records. Test repositories skip it
  unless asked, and most tests use a fixture of the few units they need.
- **Write cost.** Every quantity write resolves through the graph, and every
  observed write and metric write runs an end-of-transaction check; the
  provider-sync benchmark decides whether the metric read needs a batch.
- **The guard's reach.** `unit_uses` is only as complete as the fold's walk
  of quantity leaves; a datatype that later nests quantities somewhere new
  must join the walk, or the guard misses its uses.
- **Seed conflicts.** A repository whose owner edited a shipped conversion
  may skip a later shipped edge; `unseeded` in the units route is the only
  place that says so.
- **Two code paths in step 3.** Until step 5 every reader has an old and a
  new path; a test that runs only one of them can let the other rot.
- **Development repositories.** After step 4 a repository written before it
  parks its `observations` and `health` copies; `mise run dev:wipe` is the
  answer, and the step's notes say so.
