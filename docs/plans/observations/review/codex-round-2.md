Reviewer: Codex (gpt-6-astra), 2026-10-11, over the range `4b4d9e9b..561fc287`; the text below is its report, verbatim.
Each link to an absolute local path broke `lint:docs`, so each is now its plain link text.

# Observations review, round 2

Do not merge `4b4d9e9b..561fc287`: the aggregate read can omit stored observations, choose a different total when served from rollups, and exceed its output budget. Focused reproductions also found incorrect unit conversion, local-day boundaries, filter semantics, fill values, and exactness reporting. The round 1 regressions tested here pass, but findings 12 and 13 are only partly resolved because open-unit discovery still scans records and dense-bucket writes still recompute the entire bucket.

## Scope and evidence

Reviewed the committed range and the merge's second-parent fixes using both requested logs and `git diff --stat 4b4d9e9b...561fc287`. `HEAD` was `561fc287f48651deb5460c64ffda56b0c51f9800`, and the working tree was clean. All source locations below refer to that commit.

Read the aggregate implementation, its callers and wire types, the round 1 report, the implementation contract, reading proposal and worked aggregate examples, decisions 0160/0161, and the relevant observation/API documentation. Round 1 findings 4, 5, 8, 9, 10 and 14 were excluded as requested.

“Reproduced” means an executed check against the committed implementation. Additional tests used Go's `-overlay` with source and overlay JSON held in anonymous Linux memory descriptors; no repository file or index was edited. These tests reused the existing fixtures and `engine.AggregateRaw` comparison seam. This report is the only authored disk file.

## Findings

### 1. P1: The latest point's form hides historical observations

Location: aggregatemetric.go:183, aggregateread.go:455.

`observed_latest` has one row per `(metric, kind, source, about, channel)`, but selection treats that row's `form` and `code` as properties of the entire history. Every subsequent raw/rollup query restricts `form` to the latest value. Older forms therefore disappear before mixed-form validation can see them; a later summary can also remove a stream from a `from: raw` read.

Reproduced: write an amount of 10 steps at `2026-09-01T00:00:00Z`, ending at `00:10`, then a gauge of 20 steps at `00:30`, using the same kind, metric and source. A `count` read over `[00:00,00:15)` returns no rows, although the amount is in that range. Both writes were accepted.

Fix: use the latest table to enumerate stream identities, then discover the forms and numeric/coded variants present in the requested windows. Validate all participating forms before choosing a path. Do not let a write outside the requested range change which historical points exist to the reader.

### 2. P1: Rollups omit the part of a record window outside `range`

Location: aggregateread.go:454, aggregateread.go:1152.

Raw reads use the union of record windows, but `readRollups` and the gauge carry walk use `p.from`/`p.to`. The documented decision to leave record windows unclipped consequently works only on some execution paths. A rollup-served request can return an empty or partial result for a window it reports in full.

Reproduced: request range `[00:00,01:00)`, select a sleep record spanning `[00:00,02:00)`, and write a 10-step amount at `01:30` ending at `01:40`. The rollup read returns zero rows; the forced-raw read returns one row with sum `10`. The same problem applies to `around.before` and `around.after`.

Fix: derive rollup ranges and gauge carries from the merged record windows too. Clip contributions separately into each output window and compare widened windows on both paths.

### 3. P1: Added rollup coverage makes `prefer-raw` choose the wrong total

Location: aggregateread.go:985, aggregateread.go:599.

An amount's raw coverage is a union of spans. Rollup coverage adds each row's union length, losing overlaps across quarter-hour rows and groups. This changes the `minCoverage` decision, not merely a descriptive field.

Reproduced: two amount traces from one source, each worth 1 step, span `[00:00,00:35)` and `[00:15,00:50)`. Add a single-observation summary of 100 steps for `[00:00,01:00)`. With `from: prefer-raw`, `sum`, and the default 0.9 floor, rollups report `PT1H10M` coverage and return `2`; forced-raw evaluation sees 50 minutes of raw coverage and returns the summary `100`. A separate two-amount case produced 90 minutes of coverage from rollups versus 60 from raw.

Fix: retain sufficient interval information to merge coverage unions, or fall back to raw coverage wherever overlap cannot be ruled out. Adding scalar coverage lengths cannot implement the promised union.

### 4. P1: Code groups and `fill` bypass `maxBuckets`

Location: aggregateread.go:551, aggregateread.go:580, aggregatefill.go:45.

The bucket budget multiplies temporal buckets by stream groups before splitting by code. Code groups are counted only after reduction, and filling them never rechecks the output budget.

Reproduced: two coded amounts (`a` and `b`), four 15-minute buckets, `groupBy: ["code"]`, `fill: "zero"`, and `budget: {maxBuckets: 4, maxGroups: 2}` return eight rows successfully. At the allowed caps, 2,000 codes and 10,000 buckets can produce 20 million filled rows despite the 10,000-row cap. That larger case was not executed.

Fix: preflight the actual series cardinality, including code, and apply the product budget before reducing or filling. Keep a final invariant check that output rows cannot exceed the approved bound.

### 5. P1: Priority resolution can run billions of comparisons after SQL ends

Location: spans.go:58, spans.go:86, aggregateread.go:692.

`Prioritize` calls `subtract` for every lower-priority span, and `subtract` walks the entire higher-priority coverage array, including intervals already behind or still ahead of that span. No context is checked in these loops. SQL's statement timeout does not bound this CPU work.

Reproduction by inspection: two sources each have 300,000 one-second holds separated by gaps; interleave the sources so the higher-priority coverage retains 300,000 disjoint intervals. The lower source triggers roughly 90 billion coverage comparisons. The 600,000 points fit the default sample budget and the six-month weekly `durationIn` example's output budget. I did not execute this workload.

Fix: sweep the two sorted interval sequences with advancing cursors, or binary-search the first possible overlap and stop after the last. Propagate cancellation through long reductions and benchmark the actual six-month priority request with fragmented coverage.

### 6. P2: A carry iteration failure becomes a successful aggregate

Location: aggregateread.go:409, aggregateread.go:418.

`readSeries` checks `rows.Close()` after iteration but never `rows.Err()`. `database/sql` automatically closes rows when `Next` encounters an error, so the later explicit `Close` returns nil. An error in the final carry query can therefore leave missing carry/previous-point data and return success, with no subsequent database statement to reveal the aborted read.

Reproduced with a minimal `database/sql` driver: the points query finishes normally, the gauge carry's `Next` returns `statement timeout`, and `readSeries` returns nil error. This verifies the error-swallowing path; a real Postgres mid-stream timeout was not injected.

Fix: check `Rows.Err()` after every iteration, before accepting the collected data. Audit the same pattern in the new paged recompute code, including `readPointsAfter`, although subsequent writes may currently expose its transaction error.

### 7. P2: Raw counter extrema are labelled with an unconverted unit

Location: aggregateread.go:338, aggregateread.go:1308.

Counter points deliberately retain stored numbers for differencing, but raw `min` and `max` reduce those numbers and attach the requested output unit. Rollup extrema are converted first.

Reproduced: one monotonic counter reading of `1 kW.h`, queried with `unit: "W.h"`, returns min/max `1000` from rollups and `1` from raw, both labelled `W.h`. Recorded-local minute buckets provide a public path that forces raw evaluation.

Fix: keep original counter digits for `Step`, and use separately converted values for extrema. Test both units and mixed-unit histories without changing the counter step contract.

### 8. P2: A counter with no previous reading has two different increases

Location: rollup.go:153, aggregateread.go:988, aggregateread.go:1402.

A counter rollup initializes `Increase` to zero even when no step exists. The aggregate treats non-nil `Increase` as evidence of a step. Raw evaluation only marks a step when a predecessor exists.

Reproduced with the single counter in finding 7: `increase` is `"0"` from rollups and null from raw. Zero asserts a measured lack of change; null says no change can be determined.

Fix: preserve whether a rollup contains any valid step, and apply one absent-step rule on both paths. Do not infer step availability from a zero-valued accumulator.

### 9. P2: Recorded-local rollups reverse offsets within one quarter hour

Location: aggregateread.go:963, aggregateread.go:865, aggregatebuckets.go:78.

The rollup path chooses a local day's first/last offsets using `bucket15`, not the rows' actual first/last point times. Two offset groups in one row slot tie, so SQL group order chooses the day boundaries. The held-time path also calls this mutating helper while iterating a map, making boundary selection order-dependent.

Reproduced: same-source amounts at `2026-09-01T12:01Z` with offset +180 and `12:10Z` with offset -240. A recorded-local day starts at `00:00-04:00` and ends at next midnight `+03:00` from rollups; raw returns the correct `00:00+03:00` through next midnight `-04:00`. That is a reported 17-hour day versus 31 hours.

Fix: separate bucket lookup from offset-extrema updates and compare actual point timestamps with a stable tie key. Also correct `length()` for local coverage: it currently divides by 24 wall-clock hours even when the reported day spans 31 hours, which can make `prefer-raw` pass its floor too early.

### 10. P2: Rollup `first` and `last` use a different tie order from raw

Location: aggregateread.go:457, aggregateread.go:1062.

Raw points order equal timestamps by record id and ordinal. Rollup rows instead arrive by code/unit/delta/offset, and `mergeStats` compares timestamps alone. First/last identity is lost when a timestamp spans rollup groups.

Reproduced: at one timestamp, record `a-reversed` contains `2000 W.h` and `z-reversed` contains `1 kW.h`. Reading in `W.h` gives first/last `1000/2000` from rollups versus `2000/1000` from raw.

Fix: retain the first/last point's full ordering key in each rollup and use the same comparator everywhere, or use raw boundary lookups for ambiguous ties. Sort fragment-group merges too; their map iteration currently supplies no stable tie order.

### 11. P2: Aggregate filters union `eq` and `in` instead of intersecting

Location: aggregate.go:544, aggregatemetric.go:177.

`condStrings` appends both conditions to one list, then emits `= ANY(...)`. The ordinary filter applies both predicates. An aggregate can therefore include records excluded by the same list filter.

Reproduced with `filter.properties.source = {"eq": "<source A path>", "in": ["<source B path>"]}`: the list returns zero records; the aggregate returns two source rows. This is filter broadening within the repository, not a demonstrated cross-repository escape.

Fix: intersect the conditions or reject their simultaneous use by parameter name. Apply the same correction to code filtering and the other stream-key conditions.

### 12. P2: Coded `locf` repeats the first code instead of the previous code

Location: aggregatefill.go:53.

Synthetic values copy `Code` from the first row's template and only copy the numeric `Value` from the previous row. A coded `last` has a null numeric value, so its carried code never advances. Index-based copying also mismatches `timeInState` values when different rows contain different states.

Reproduced: hourly buckets contain `open` at 00:15 and `closed` at 01:15, both with a one-minute hold; the third hour is empty. With `reducers: ["last"]` and `fill: "locf"`, the third hour reports `open`, not `closed`.

Fix: carry the complete previous value by reducer/state identity. Build state-valued fills from matching keys rather than positional indexes, then apply rolling to those corrected values.

### 13. P2: Property aggregates ignore `maxGroups` and check cells too late

Location: aggregatelatest.go:249, aggregatelatest.go:328.

The property loop builds every group and rational accumulator before checking groups times buckets. It never checks `p.maxGroups` at all. A small result budget therefore does not prevent a full permitted record scan and reduction.

Reproduced: two money records, one EUR and one JPY, queried with `budget: {maxGroups: 1}`, return two groups successfully. Inspection also shows that an over-`maxBuckets` result is refused only after the rows have been reduced.

Fix: count the applicable kind/currency groups before the value scan, enforce both budgets, and stop incremental accumulation defensively if an invariant is exceeded. Retain the existing sample-count preflight.

### 14. P2: Rounded extrema are returned with `exact: true`

Location: aggregateread.go:1324, aggregateread.go:1506.

Min/max use float64 and the 15-significant-digit formatter, but mark the answer inexact only when a unit conversion occurred. They can alter stored digits without updating `exact`, contradicting both the response comment and the documented departure from the plan.

Reproduced: amount `9007199254740993 {steps}`, reducer `min`, no conversion, returns `9007199254740990` with `exact: true`.

Fix: mark all float-derived extrema inexact, or preserve the exact winning digits and compare them exactly. Test large integers and long decimal fractions for min/max and M4.

### 15. P2: Aligning downsample columns can exceed `points`

Location: aggregate.go:441.

The width is calculated from `to - from`, then the origin moves backward to a quarter-hour boundary without recalculating the number of columns. The widened aligned extent can need another column.

Reproduced: range `[2026-09-01T00:01Z,01:01Z)`, amounts at 00:01 and 01:00, and `downsample: {points: 1}` return two columns. The documented bound is at most `points` columns.

Fix: derive width from the aligned origin's extent or widen until enumeration fits the requested column count. Include non-aligned starts in the downsample tests.

### 16. P2: Empty record windows let the records limit truncate silently

Location: aggregatebuckets.go:405, aggregatebuckets.go:420.

SQL limits matched records to `limit + 1`, but the overflow check counts only emitted non-empty windows. Skipping zero-length windows can consume the limited SQL result without reporting that more matching records exist.

Reproduction by inspection: set `maxRecords: 1`; the records filter matches, in time order, two instant records with no `endsAt` and then a record with a valid interval. With no widening, SQL returns the first two, both are skipped, and the request returns no buckets instead of refusing or reaching the valid interval.

Fix: count matched records independently if the limit is on matches, as documented. If the intended limit is on usable windows, filter out empty windows in SQL before applying the limit and make that contract explicit.

## Round 1 fixes

- **1, zero divisors: fixed.** Zero numeric factors are rejected, evaluation returns errors, and the vocabulary suite covers direct and nested forms.
- **2, dimension overflow: fixed.** Checked integer arithmetic precedes narrowing; dimensions outside ±8 are refused.
- **3, purge after unbinding: fixed.** `dropObservedOf` probes and removes existing points even after the binding goes. The regression deliberately pauses reprojection, purges/tombstones, resumes, and compares with rebuild; it passed.
- **6, parser work: fixed for the reported attacks.** Exponent, numeric-factor length, nesting and component bounds precede evaluation. Exponentiation uses bounded integer powers. The hostile-input vocabulary tests passed.
- **7, empty-code availability: fixed.** Validation rejects empty codes; old empty codes use the shared availability definition. The live/rebuild regression passed.
- **11, ordering admitted units: fixed for the reported case.** Stored open-unit codes now gain conversion arms. The cursor signature remains based on the declared expression, while continuation keys use the expanded expression; the test inserting a new unit between pages passed. No cursor-signature regression was found.
- **12, unrestricted unit discovery: partly fixed.** Closed declarations avoid discovery, tombstones follow the selected side, and the 256-unit cap bounds SQL arms. Open declarations still issue `SELECT DISTINCT` across all live records of the kind on every page, ignoring the request's other restrictions. A limit on distinct spellings does not bound rows scanned; the proposed indexed unit catalogue/normalized key is still absent.
- **13, bucket recomputation: partly fixed.** Points now arrive in bounded pages under `(ts, record_id, ord)` and share `BucketBuilder` state. The bounded-page/rebuild regression passed, and inspection found a consistent keyset order. CPU work still covers every point in each touched bucket while holding the repository lock; the dense-bucket benchmark explicitly acknowledges quadratic cumulative writes. Its fixture offsets the traces' timestamps, so it does not separately prove a page boundary cutting a large equal-timestamp run. Finding 6 also identifies missing iteration-error checks in this code pattern.

## What is good and should not change

The aggregate remains behind `substrate.Dataset`; `internal/api` has no new engine import. Strict parameter decoding, named validation errors, and separate API fakes keep the route testable. The function host checks both the outer kinds and record-window kinds against its reads allowlist; the agent path also constrains both selections. Property reductions only admit numeric, quantity and money datatypes, which excludes the secret/digest datatypes, and use the ordinary filter builder for live/tombstone selection.

Keep the repeatable-read transaction, repository-scoped database connection and forced RLS on the observed tables. No cross-repository read or sensitive-material exposure was found in the reviewed paths. Keep gauge transitions before code filtering, rational counter steps, per-row affine/delta conversion, the tight-`maxGap` raw fallback, and the shared observed specification package. Those are the right boundaries for fixing the discrepancies above.

The Go golden check passes, and the inspected TypeScript mirror matches the aggregate field names and optionality, including required nullable `value`, required row booleans, and string `head.generation`. The API documentation explicitly discloses comparator-bounded values entering averages and sums; I did not count that acknowledged departure as a newly discovered numeric bug. However, `AggregateRow.Bounded` still says “never averaged” at `internal/substrate/aggregate.go:339`, so that comment must follow the documented behavior or the implementation must regain exclusion.

The named-calendar code advances civil dates rather than adding 24 hours. The existing Athens clock-change and recorded-local tests are useful, but the local test does not cover two offset groups within one rollup slot. Keep the correction/deletion/rebuild comparisons and add the failing cases above to the raw/rollup comparison matrix.

## Performance and remaining verification limits

Stream enumeration does use `observed_latest`, followed by per-stream range queries that constrain the leading keys of `observed_points_stream_idx` and the rollup primary key. That avoids the skip-scan problem described in the benchmark. It still produces queries proportional to stream count: a raw stream needs a count, a points query and a carry query, plus a second predecessor query for counters; a gauge served from rollups needs a rollup query and a carry query. Neither stream enumeration nor rollup materialization has its own row cap, and many channels can share one output group.

A 600,000-point `durationIn` request counts and then reads the raw points, retains their decoded series, copies points into reduction data, constructs held spans, and copies spans again for reducers. Five duration bands each traverse their spans. Peak memory therefore includes several full-size point/span arrays plus strings, rather than one bounded page. No six-month end-to-end latency or heap benchmark was run, so the SQL-only read timings in the committed benchmark should not be presented as this API's latency. The quadratic priority case is detailed in finding 5. Streaming reduction and a linear priority merge need measurements before claiming this workload is bounded in practice.

Inspection also found floating additions over unsorted maps in `walkHeld` and fragment-group merges. The tied-value discrepancy was reproduced; externally visible variation in the final 15-digit floating output was not measured. Sort reduction keys and test cancellation-heavy values if identical reads must return identical decimal strings.

Checks completed:

- `go test ./internal/vocabulary/ ./internal/observed/ ./internal/substrate/ ./internal/api/ ./cmd/...`: passed.
- `go vet` over the same package list: passed.
- Named engine tests `TestAggregateRollupEqualsRaw`, `TestAggregateSleepStagesByRecordWindows`, `TestAggregateRefusesOverBudgetBeforeRunning`, and `TestAggregateRecordedLocalDayAcrossAnOffsetChange`: passed together in 11.367 seconds.
- Named round 1 engine regressions for purge-after-unbinding, empty-code availability, bounded pages, admitted-unit ordering, unit-discovery narrowing and unit-count refusal: passed together in 10.799 seconds.
- In-memory regression overlays reproduced findings 1 through 4 and 6 through 15. The timeout reproduction used a fake SQL driver; the numeric and budget cases used Postgres testcontainers. An initial summary fixture was correctly refused because traces cannot be summaries; the corrected fixture used amount traces plus a single-observation summary and reproduced finding 3.
- `git diff --check 4b4d9e9b 561fc287`: passed. The working tree and index remained unchanged.

No full engine suite, database aggregate task, live provider/LLM call, full console suite, or new large-scale benchmark ran. The TypeScript mirror was inspected, but its vitest was not run. Findings 5 and 16 are code-inspection reproductions, and the resource-amplification case in finding 4 was verified only at the small eight-row scale.
