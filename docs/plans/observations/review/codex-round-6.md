Reviewer: Codex (gpt-6-astra), 2026-10-11, over the range `8f7eabd6..9b4a6bb5`; the text below is its report, verbatim.
Each link to an absolute local path broke `lint:docs`, so each is now its plain link text.

# Observations review, round 6

Do not approve `9b4a6bb5` yet: five of round 4's seven new findings are fixed, but recorded-local gauge carries can still assign held time to the wrong day. Cancellation now interrupts the main evaluation loops, while a large individual reducer or priority helper still runs without interruption. All checks listed below passed, and I found no regression in the migration consolidation or exact-digit comparison.

## Scope and checks

Reviewed `/tmp/observations-review/round-4.md`, `git log --oneline 8f7eabd6..9b4a6bb5^2`, the corresponding three-dot diff, and the resulting code at `9b4a6bb57daeeb55dcc9f4bcd8d10266c49b8df5`. `HEAD` matched that merge, and the working tree and index were clean throughout. File locations refer to that commit; the two remaining counterexamples below were derived from code, without adding fixtures or editing tests.

Passed:

- `go test ./internal/observed/ ./internal/substrate/ ./internal/api/`.
- `go test -count=1 -run 'TestAggregate' ./internal/engine/` (20.590 seconds).
- `go test -count=1 -run 'TestObservedRollupRowsAlwaysCarryTheirDetail|TestEveryMigrationLeavesAFold' ./internal/engine/` (36.817 seconds).
- `go test -count=1 -run '^TestObservedRollupRecomputeReadsBoundedPages$|^TestObservedRollupEqualsTheRawAggregate$|^TestObservedIndexDerivesThePointsOfEveryShape$' ./internal/engine/` (11.577 seconds).
- `go vet ./internal/observed/ ./internal/substrate/ ./internal/api/ ./internal/engine/`.
- `git diff --check 8f7eabd6 9b4a6bb5^2`.

Database test commands ran sequentially. No whole engine suite ran.

## Round 4 findings

1. Fixed: `aggregatemetric.go:363`, `aggregateread.go:178,215,1326` charge discovery, reduction and coverage to one total; the discovery and coverage tests assert refusal before their value statements, although neither explicitly pins accumulation across phases.
2. Fixed: `observed/observed.go:219` compares both candidates' digits before floats; `TestAggregateConvertedExtremesCompareTheirDigits` pins the affine-conversion counterexample on raw reads, rollups and M4, with no comparison regression found.
3. Fixed: `migrations/0012_observed_index.up.sql:135` requires tie metadata beside timestamps from the table's creation; the row-detail test and existing first/last test cover storage and answers, with the constraint-test limitation described below.
4. Fixed: `observed/rollup.go:228` creates `Increase` only for an actual step, and the consolidated migration eliminates the legacy zero rows; the row-detail test pins both the singleton's null and the following counter's `1.75` increase.
5. Fixed: `migrations/0012_observed_index.up.sql:134` pairs float extrema with numeric digits from table creation; exact-extrema and row-detail tests cover valid writes and malformed-row refusal, so the legacy/new merge no longer arises on the supported upgrade path.
6. Partly: `aggregateread.go:143` rejects rollup execution for every selected row with an offset off the quarter hour, and the odd-offset test pins points inside such rows; carries loaded later at `:200` escape this guard (remaining finding 1).
7. Partly: `aggregateread.go:708,1082,1603` and `observed/spans.go:64` add effective cancellation boundaries, and the after-plan test is deterministic; individual reducers and several potentially large helpers still lack checks (remaining finding 2).

## New and remaining findings

### 1. P2: Recorded-local gauge carries bypass the offset guard

Locations: aggregateread.go:143, aggregateread.go:200, aggregateread.go:1487.

Failing input by inspection: one coded gauge stream contains `open` at `2026-09-01T23:44:00Z` with `utcOffset: 1`, then `closed` at `2026-09-01T23:59:30Z` with `utcOffset: 0`; neither point has a hold limit. Request `[2026-09-01T23:45:00Z, 2026-09-02T00:15:00Z)`, `bucket: {calendar: "day"}`, and `timeInState: ["open"]`, without a tighter `maxGap`.

The only rollup row read has offset zero, so planning permits rollups. The earlier `open` point arrives through `readCarries` after that decision. Because the first slot contains a row, `walkHeld` skips `chunkEnd`, obtains 870 seconds of carried `open` time, and assigns all of it using the slot's start and offset `+1`. Raw `spanParts` splits that hold at `23:59Z`: September 1 gets 840 seconds and September 2 gets 30 seconds. Thus the new test's two odd-offset points inside the range pass while this carry-only odd offset still returns incorrect daily durations.

Recommended fix: include each surviving gauge carry's offset in rollup eligibility before deciding and budgeting the raw reads, or split the carry contribution at its own local boundaries even when a slot contains rows. Add this mixed-offset carry case to the raw-versus-rollup comparison, including a negative odd offset. This is an unclosed case of round 4 finding 6, not evidence that the patch broke a previously correct read.

### 2. P2: Cancellation still waits for a whole large reducer or helper

Locations: aggregateread.go:1582, aggregateread.go:1671, observed.go:385, spans.go:74.

Failing execution by inspection: request one aligned bucket containing 900,000 unsorted numeric gauge readings and a percentile reducer, then cancel immediately after `values` checks the context. `pts` copies the whole bucket, `numbers` builds its numeric slice, and `Percentile` sorts it before another cancellation boundary. Raising `maxSamples` permits the same uninterrupted work over much larger buckets. The same gap exists in `spansOf` plus held reducers, and in `Prioritize` when one long lower-priority span traverses a large fragmented cover inside `subtract`; its cover construction, sort and merge also lack internal checks.

Recommended fix: propagate cancellation through the large copies, reducers and priority helper loops, with cancellable sorting or bounded sorting chunks where necessary. Add a deterministic hook or controlled context that cancels after reduction has begun, and assert that work stops before consuming the bucket. The existing after-plan hook cancels synchronously before evaluation, so its result is deterministic and would catch removal of the initial rollup check; it does not pin periodic checks during active reduction. `TestPrioritizeStopsWhenItsContextEnds` likewise tests only an already-canceled context. This remains part of round 4 finding 7; the API documentation's broad claim that reduction stops with its caller still needs qualification until the gap is closed.

## Migration and constraint assessment

`git log origin/main -- internal/engine/migrations/0012_observed_index.up.sql` returned no commits, as did the corresponding `0013` history. The migration diff against the merge base with `origin/main` shows only added `0012`. `.mise/frozencheck.sh:60` selects that merge base and permits additions, so consolidating these unmerged migrations does not violate the landed-migration rule. I did not fetch or change `origin/main`.

The new CHECK expressions use `IS NULL` comparisons, so SQL's nullable CHECK semantics do not let a missing paired field through. They match the fold's writes:

- Numeric rows set both float extrema and both digit columns; coded gauges leave all four null but retain first/last timestamps and tie keys.
- Summaries never produce rollup rows (`BucketBuilder.Add` and `Rows` return without producing them).
- A counter with no preceding available reading leaves `Increase` nil; a genuine zero step still stores zero. The CHECK only forbids an increase on a non-counter, so the builder and tests, rather than this CHECK, establish the no-step invariant.
- An all-unavailable bucket has `n = 0`, no first/last, no extrema and no amount span end, satisfying every new CHECK while retaining the gap metadata needed to stop a hold.
- `rollupRowsOfChunk` finishes all pages before returning a bucket's rows. `rederiveHold` flushes completed buckets while keeping the current builder across pages. Neither writes an unfinished bucket state mid-page.

`TestObservedRollupRowsAlwaysCarryTheirDetail` exercises numeric and coded gauges, amounts, and counters with and without a step, plus four malformed inserts. Its malformed first/last insert omits both keys together, and its extrema insert omits both digits together: these pin refusal of those examples, not each individual CHECK independently. Summary/null behavior has specification coverage, and the additional shape and paged tests passed; the new detail test itself does not enumerate every all-null form.

The deleted infinite-extent fallback and float-only extrema response are safe for rows produced by this migration and fold. `readRollups` still uses nullable scans for legitimately absent values, and `NumberLess` still falls back to floats for digitless library points. Those are not an upgrade repair mechanism: a present timestamp cannot lack its tie keys under the new table, and a stored numeric extreme cannot lack its digits. The constraints do not validate arbitrary direct SQL against the raw points, which is outside the supported write contract.

## Refold test assessment

Fixed for the supported migration sequence: `copyRefoldLedger` keeps the completed repository migrations whose SQL floor predates the staged upgrade. A later SQL migration after `0012` will therefore be tested with `derive_observed_index` already completed, preventing an accidental rerun from masking its missing backfill. The complete refold test passed.

`head_ledger_kept` at `migrationrefold_db_test.go:164` applies no upgrade and copies the current binary's already-populated columns. It detects disagreement between current fold writes and rebuild without repository migrations repairing the fold first; it does not, by itself, prove that a historical column-adding migration populated old rows. That responsibility belongs to the per-upgrade `ledger_kept` stages and representative old fixtures. The `0012` upgrade legitimately runs repository migration 3 because its prior stage has no observed tables. No ordinary legacy-detail upgrade remains after consolidating `0013`.

## Budget, numeric and test details

The six-month weekly example's endpoints are quarter-hour aligned in UTC, and discovery uses the whole range, not a separate raw discovery scan for every weekly output bucket. With `from: raw`, its roughly 600,000 gauge points therefore cost roughly 600,000 samples, not 1.2 million; the shown request also explicitly permits five million. A dense unaligned cut-end range containing 600,000 points, or a summary-only range containing that many points, can now cost 1.2 million and fail the default budget. That is the documented change from counting distinct reduction inputs to counting inputs across statements, not an accidental doubling of the aligned example. A regression test should additionally pin a request whose separate phases each fit but whose sum exceeds the budget.

`CompareDigits` correctly normalizes negative zero, leading integer zeros and trailing fractional zeros before comparing plain decimals. It falls back to rational comparison for exponent notation; `TestCompareDigits` covers these cases and malformed strings. `NaN` is not a rational and falls back through `NumberLess` to float comparison, but authored quantities and trace decimals reject it through the decimal grammar, so no accepted-input regression was identified. The aggregate affine-conversion test verifies `exact: true` on raw and rollup answers and checks the M4 extrema, though it does not separately assert M4's `Exact` flag.

Offset zero and negative multiples of 15 pass the modulo test; positive and negative nonmultiples fail it. One offending row disables rollups for the entire aggregate group, including mixed streams. The new odd-offset test covers `+1` for an amount and a gauge, not negative offsets or the pre-range carry described above.

The empty-range unit, cut-end form and summary-only tests pin their stated behaviors. The property-budget test now asserts both that refused reads send no value query and that a successful read sends exactly one, which makes the query-fragment assertion meaningful. `docs/api.md` correctly describes discovery, cumulative sample charging, the rollup/carry exemptions and recorded-local cardinality refusal after evaluation but before row assembly; that late cardinality check remains a stated limitation rather than a newly fixed preflight check.

## What I could not verify

I did not execute the new mixed-offset carry input or cancel a reducer already in progress; those findings follow the cited control flow. No six-month end-to-end latency or memory benchmark, live Postgres timeout injection, historical-binary upgrade, full engine suite or console suite ran. The refold assertions and constraint tests passed, but I did not revert fixes to demonstrate individual test failures or inject an observed-table corruption fixture with the ledger retained.

No repository file or index entry was changed. This review is the only authored file.
