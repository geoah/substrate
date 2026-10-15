Reviewer: Codex (gpt-6-astra), 2026-10-11, over the range `55249fbf..8f7eabd6`; the text below is its report, verbatim.
Each link to an absolute local path broke `lint:docs`, so each is now its plain link text.

# Observations review, round 4

Do not approve `8f7eabd6`: ten round-2 findings are fixed, while six retain budget, cancellation, upgrade, or numeric-correctness gaps. The aggregate regressions pass, but they do not exercise legacy rollups after an ordinary upgrade, and the new exact-extrema comparator can choose the wrong value after conversion. Migration `0013` preserves table security and rebuild coverage, but its passing refold test reruns an earlier repository migration that an existing installation will skip.

## Scope and checks

Reviewed `/tmp/observations-review/round-2.md`, the requested second-parent log and diff from `55249fbf`, and the resulting implementation at `8f7eabd62bb641adeb5eacaa23ed01f59402ff3d`. `HEAD` was that commit and the working tree was clean. Locations below refer to that commit.

Executed successfully:

- `go test ./internal/observed/ ./internal/substrate/ ./internal/api/`.
- `go test -count=1 -run 'TestAggregate|TestObservedReadsFailWhenAnIterationFails' ./internal/engine/` (14.102 seconds).
- `go test -count=1 -run 'TestEveryMigrationLeavesAFoldTheRebuildReproduces/0013|TestObservedRollupRecomputeReadsBoundedPages' ./internal/engine/` (13.110 seconds).
- `go test -count=1 -run '^TestObservedRollupEqualsTheRawAggregate$|^TestRebuildReproducesTheObservedTables$' ./internal/engine/` (12.348 seconds).
- `go vet ./internal/observed/ ./internal/substrate/ ./internal/api/ ./internal/engine/`.
- `go test -run '^$' -bench '^BenchmarkPrioritizeFragmented$' -benchtime=1x -benchmem ./internal/observed/`: 389 ms, 419,478,464 allocated bytes, 81 allocations for two 300,000-span sources.
- `git diff --check 55249fbf 8f7eabd6`.

Database test commands ran sequentially. Reversion assessments below come from reading the implementation and assertions, without reverting or editing files. “No regression found” is limited to this inspection and these checks.

## The 16 findings

1. **Fixed:** `aggregatemetric.go:254` discovers forms from whole rollup slots, both raw cut ends, full-window summaries, and a surviving gauge carry; `TestAggregateFormsComeFromTheRange` fails under the old latest-form selection, but does not pin cut-only forms, summary-only discovery, or empty-range units. The intervals cover both cut ends even when no whole slot exists; units are now resolved before discovery (`:132`), so an empty range retains its metric/selected-stream unit. Discovery's budget exemption is discussed below.
2. **Fixed:** `aggregateread.go:246,469,1078` derives raw ranges, rollup ranges, and gauge carries from merged record windows; `TestAggregateRecordWindowsReachPastTheRange` pins amounts, held gauges, and widening at both ends and would fail if reverted. No regression found.
3. **Partly:** `aggregateread.go:1175,1191,1220` joins disjoint extents and otherwise computes the raw union across all streams in the output group, including code/local filters; `TestAggregateAmountCoverageIsAUnion` pins overlapping rows, offset groups, and the summary choice and would fail if reverted. Missing `span_end` conservatively forces overlap fallback, but the fallback has no sample/work budget (new finding 1); the test does not cover multiple sources, legacy rows, or that budget.
4. **Fixed:** `aggregatemetric.go:485` counts code series before reduction, and `aggregatefill.go:38,69` checks fill cardinality and final output; `TestAggregateBucketBudgetCountsCodes` pins the eight-versus-four-row case and would fail if reverted. Its zero trace counters exclude discovery reads, so they do not prove that SQL read no points. No output-cap regression found.
5. **Partly:** `observed/spans.go:57,87,125` replaces the quadratic whole-cover walk with binary search and sorted merging; positive overlapping, adjacent, and nested intervals retain the correct uncovered pieces. The fragmented test checks the same output the old algorithm eventually produced, so its protection against reversion is runtime/timeout, not an explicit complexity assertion; the benchmark confirms the new speed. Cancellation remains incomplete (new finding 7).
6. **Fixed:** every `rows.Next()` site in the aggregate files and `observedsettle.go` now ends in `rows.Err()` or `finishRows`; the fake-driver test would fail if the carry/page fixes were reverted. It injects the first carry failure and `readPointsAfter` failure, but does not separately reach the counter predecessor or every paged-recompute iteration. No unchecked iteration remains in the requested paths.
7. **Fixed:** `aggregateread.go:1661` converts separate counter extrema while `Step` retains stored digits; `TestAggregateCounterInAnotherUnit` pins lone and mixed-unit readings on both paths and would fail if reverted. The existing counter suite also passes. The broader converted-extrema defect in new finding 2 affects both paths, beyond this unit-label correction.
8. **Partly:** new `BucketBuilder` rows leave `Increase` nil until a step exists (`observed/rollup.go:228`); the lone-counter assertions in the aggregate and observed tests would fail if reverted. Legacy rows still contain the previous zero accumulator, which the reader accepts as a step (new finding 4).
9. **Partly:** `aggregatebuckets.go:87,120` marks actual point extrema and uses the reported duration; `TestAggregateRecordedLocalDayFromRealPoints` pins the reported 31-hour day and coverage-floor correction and would fail if reverted. Legacy tie keys and offsets that are not multiples of 15 minutes remain incorrect (new findings 3 and 6).
10. **Partly:** new rows retain `(timestamp, record_id, ord)`, `mergeStats` adds a stable stream tie key, and fragment groups merge in sorted order (`aggregateread.go:1160,1309`); `TestAggregateFirstAndLastBreakTiesByRecord` would fail if reverted. Null legacy keys become invented empty keys instead of triggering an exact boundary lookup (new finding 3); ordinal ties and legacy rows lack dedicated regressions.
11. **Fixed:** `aggregate.go:542` intersects `eq` and `in`, including an empty intersection; all aggregate stream-key/code consumers use it. `TestAggregateFilterConditionsIntersect` checks disjoint and overlapping source sets against the list, plus disjoint codes, and would fail if reverted. No filter-broadening regression found.
12. **Fixed:** `aggregatefill.go:99` copies the previous values whole, preserving each reducer/state identity; `TestAggregateLocfCarriesThePreviousCode` checks coded `last` and changing `timeInState` membership and would fail if reverted. Rolling already matches reducer/code keys, so the copied identities remain usable. No regression found.
13. **Fixed:** `aggregatelatest.go:239` counts applicable kind/currency groups and checks both budgets before the value scan, with a defensive accumulation check. `TestAggregatePropertyBudgetsBeforeTheScan` would fail if the group check were reverted, but its bucket assertion would still pass with the old late refusal: the test does not pin pre-scan timing. No implementation regression found.
14. **Partly:** new rollups preserve exact extrema, and `TestExtremesKeepTheirDigits` plus `TestAggregateExtremesKeepTheirDigits` pin large integers, close decimals, merged rows, and M4 and would fail if reverted. A standalone legacy winner becomes inexact, but converted comparisons and mixed legacy/new comparisons can still return a wrong answer with `exact: true` (new findings 2 and 5).
15. **Fixed:** `aggregate.go:445` derives column width from the aligned origin's extent; `TestAggregateDownsampleKeepsItsColumnCount` covers one, two, and three requested columns from a non-aligned start and would fail if reverted. No column-count regression found.
16. **Fixed:** `aggregatebuckets.go:467` increments matched-record count before skipping empty windows; `TestAggregateMaxRecordsCountsEveryMatch` reproduces two instants before a usable window and would fail if reverted. No silent-truncation regression found.

## New and remaining findings

### 1. P1: Coverage fallback bypasses `maxSamples` for raw reductions

Location: aggregateread.go:1251, aggregateread.go:1191.

Failing input by inspection: use the two amounts from round-2 finding 3, spanning `[00:00,00:35)` and `[00:15,00:50)`, request an aligned hour's `sum` from raw, and set `maxSamples: 1`. Planning counts zero fragment points, then the coverage statement reads and sorts both raw spans without checking the budget. Increasing the stored points does not change that planning count. Overlapping record windows can repeat the scan per output cell.

The union logic correctly combines nested/overlapping spans across the selected streams and groups. Returning one scalar per cell bounds response size, not input work; statement timeout is the only bound on this SQL reduction. The docs explicitly exempt it, so this is a documented budget hole introduced by the coverage fix.

Count fallback inputs before reducing them and charge them to a budget, or introduce an explicit capped SQL-work budget. Bound form discovery too: cut ends can contain arbitrarily many points, and its summary arm scans the full window before the ordinary raw-count refusal.

### 2. P2: Converted extrema can select the wrong exact digits

Location: observed.go:215, aggregateread.go:350.

Failing input: two gauge readings on a temperature metric, `-273.14999999999999 Cel` and `0.000000000000001 K`, queried with `unit: K` and `min`/`max`. The exact converted Celsius value is `0.00000000000001 K`, but its separately converted float is zero. `NumberLess` takes the float fast path because zero and `1e-15` are relatively far apart, selecting the larger exact value as the minimum and the smaller as the maximum. Both conversions terminate exactly, so the response remains `exact: true`; raw, merged rollups, and M4 share the defect.

A Python binary64/rational calculation confirmed the numeric counterexample; the complete API input was not executed. Compare exact digits whenever available, or rebuild the comparison float from the converted digits and use a proven error bound. Add this affine-conversion case to both paths and M4.

### 3. P2: Legacy rollups lose first/last and local-boundary tie order

Location: aggregateread.go:464, aggregateread.go:1160.

Failing input by inspection: before upgrading, store the finding-10 pair at one timestamp in one stream: record `a-reversed` has `2000 W.h`, record `z-reversed` has `1 kW.h`. Apply `0013` without rebuilding. Both rollup keys become `("", 0)` through `coalesce`; both first and last retain the first SQL group encountered, instead of the required `2000/1000 W.h` pair.

The same loss affects recorded-local boundaries when equal-time records carry different offsets. Inventing a key that sorts first is deterministic in some cases, but does not conservatively preserve the required identity.

Keep missing-key metadata explicit and use raw boundary lookups/fallback for ambiguous legacy rows, or rederive them through a new repository migration before relying on the keys. Test an upgrade with the existing repository migration ledger preserved.

### 4. P2: Legacy counter rollups still report a fabricated zero increase

Location: aggregateread.go:533, aggregateread.go:1066.

Failing input by inspection: write one counter reading with no predecessor under the old implementation, then apply `0013` and query its aligned bucket's `increase`. The stored `increase = 0` survives migration; `st.Increase != nil` sets `steps.any`, returning `"0"`. Forced raw still returns null.

Recompute legacy counter rows, or establish step availability from raw predecessor information when the new metadata is absent. The new nil accumulator only corrects rows recomputed by the new binary. Add a pre-upgrade singleton-counter fixture.

### 5. P2: Merging legacy extrema can restore `exact: true` incorrectly

Location: rollup.go:340, aggregateread.go:1553.

Failing input by inspection: an old amount row at `00:15` stores `9007199254740992`; after upgrading, insert `9007199254740993` at `00:00` in a separate quarter-hour row. Query the hour's `min`. Both floats equal `9007199254740992`; the first row has exact digits, the legacy row does not, and `NumberLess` falls back to a false float comparison. The merger retains `9007199254740993`, and the response sees nonempty winning digits and reports `exact: true`.

This also affects M4. Propagate uncertainty from every participating candidate, not just the winner, and use a raw lookup when missing digits prevent deciding the winner. A new repository migration can remove the legacy case; the mixed-row test must still pin the conservative reader until recomputation completes.

### 6. P2: Recorded-local rollups can assign points to the wrong day

Location: aggregateread.go:1032, aggregateread.go:219.

Failing input by inspection: an amount starts at `2026-09-01T23:59:00Z` with `utcOffset: 1`; request recorded-local daily buckets over an aligned UTC range covering September 1 and 2. The declaration accepts integer offsets between -1080 and 1080. Raw places the point on September 2, but its rollup starts at `23:45Z`, which becomes September 1 at offset +1. Updating the bucket's extrema cannot correct the already-wrong bucket identity.

This remains from the old local-bucketing path. Require each participating row's offset to align its quarter hours with the requested calendar, otherwise split/read raw. Add points on both sides of local midnight inside one quarter-hour slot; selecting only a row's first timestamp would still fail when the row crosses the boundary.

### 7. P2: Long reductions still run without cancellation checks

Location: aggregateread.go:1023, aggregateread.go:1271, aggregateread.go:1487.

Failing execution by inspection: cancel a large rollup-served gauge request after planning has materialized its rows and while `evalRollups` is merging them. It continues through the row merge and `walkHeld` before `run` reaches its next bucket-level check. Raw preparation, `Prioritize` itself, final evidence sorting/coverage, fragment reduction, individual reducers, and fill/rolling also lack internal cancellation checks. The SQL timeout cannot interrupt this work.

The checks at raw point/span distribution and output-bucket boundaries are useful but do not fulfill the requested cancellation propagation. Add periodic checks to the remaining loops and context-aware reduction boundaries, plus a deterministic cancellation test after SQL completes. The fragmented benchmark measures the primitive only, not the full six-month priority request.

## Migration and documentation

`0013_observed_rollup_detail.up.sql` is the only migration added in the reviewed range; no previous SQL migration changed. All seven additions use `ADD COLUMN IF NOT EXISTS`, making their repeated application structurally idempotent. The migration changes no policy, grant, repository default, or RLS flag; `0012`'s forced RLS remains intact.

`observed_rollups` remains in both `repositoryScopedTables` lists (`repositories.go:478`, `isolation_db_test.go:28`), `foldTables` (`rebuild.go:112`), and the whole-row `foldSnapshot` (`rebuild.go:612`). The observed specification comparison now reads/checks all new columns. The named observed, bounded-page, and rebuild tests passed, providing evidence that new writes and rebuilds populate the detail consistently.

`TestEveryMigrationLeavesAFoldTheRebuildReproduces/0013` compares these columns, but does not prove the ordinary upgrade. `copyRefoldRows` explicitly omits `repository_migrations` (`migrationrefold_db_test.go:298`), so opening the staged repository reruns migration 3 and requests observed reprojection; the test drains that work before its first snapshot. An existing repository that already ran migration 3 skips it, leaving the new columns null and old counter accumulators unchanged. Add a stage that preserves completed repository migrations, then compare before/after rebuild. Do not exempt the new columns from comparison.

The new departure in `docs/api.md:1382` correctly acknowledges that form discovery precedes form/code-cardinality refusal. Its description is incomplete: discovery also reads full-window summary points and probes a gauge predecessor, and selection, unit lookup, and record-window reads can precede it. More seriously, the “every budget is checked before a point is reduced” claim at `:1334` is not universal: recorded-local bucket cardinality is checked after evaluation (`aggregateread.go:591`), while raw coverage is an explicitly unbudgeted SQL reduction. Describe which work is preflighted and capped, and resolve new finding 1 instead of presenting one SQL statement as a work bound.

## What I could not verify

The new failing API inputs above were derived from code; I did not inject fixtures, modify tests, or run legacy binaries. The affine-conversion arithmetic alone was executed independently. No real Postgres mid-stream timeout, cancellation-after-planning test, large fallback-coverage benchmark, full six-month aggregate benchmark, repeated application of the SQL migration, full engine suite, or console suite ran.

No test explicitly pins empty-range units after `ff2950b8`, cut-only form discovery, or property-budget refusal before the value query. The review found no remaining `rows.Err()` omission in the requested aggregate/paged-recompute paths, but the fake driver does not exercise every site.

The working tree and index remained unchanged. This review is the only authored file.
