# Health data proposal review

The proposed aggregate API can return incorrect totals because its storage model drops interval boundaries, summary classification, and filter distinctions before aggregation. Build with changes. Keep canonical records, provider-owned mirrors, a user-owned health package, and derived indexes. Settle the measurement and correction contracts before committing to the proposed tables or API.

Reviewed on 2026-10-08 against the supplied worktree. I read `docs/plans/health-data.md` in full first, then `CLAUDE.md`, relevant decisions, sections of all six research reports, benchmark SQL and output, and the implementation cited below. Repository references are relative to the repository root. This was read-only: no repository edits, database suites, database benchmarks, or dev server. Performance objections below come from inspection of the supplied SQL, not a replacement benchmark. A small local Python calculation checked storage arithmetic and a historical timezone counterexample.

The changes required before implementation are:

- Define points, interval amounts, cumulative counters, and provider summaries separately, including gaps, coverage, and correction identity.
- Specify exact quantity comparison, approximate analytics, and the ownership of metric semantics.
- Make derived reads preserve record filters, caller permissions, deletion, and snapshot consistency.
- Bound query work and write amplification, then benchmark the actual fold and read paths.
- Reduce phase 0 to one working provider and one consumer. Defer the broad catalogue and advanced aggregate modes.

Findings follow in severity order. P1 means resolve before building the affected feature. P2 means resolve before shipping it.

## P1.1 Interval data cannot survive the proposed samples projection

**Sections:** 2.2, 2.8, 5.2, 5.3, 5.4, 6.

**Problem and cost:** The sources have interval amounts, but `samples` retains only `ts` and `value`. A regular sampling interval describes timestamp spacing, not necessarily the interval over which each amount was measured. Irregular `offsets` cannot carry independently varying end times. There is also no distinction between a per-interval increment and a cumulative counter since session start. Research report 04, shape 2, explicitly distinguishes these forms; FIT distance and some step streams are counters.

For 412 steps over `[07:00, 07:15)`, a query ending at 07:05 needs a stated rule: refuse partial allocation, return only complete intervals, or estimate by duration. Assigning all 412 to one timestamp invents an answer. `durationIn` and `timeWeighted` also need a hold/interpolation rule, a final endpoint, and a maximum gap. Counting observations in range is not duration in range.

**Change:** Define a measurement form independent of metric aggregation style: instant, interval increment, cumulative counter, and summary. Preserve interval endpoints or durations, counter reset semantics, and coverage. Start dense indexing with instant discrete measurements. Add interval aggregation only with boundary, overlap, reset, and missing-data tests. Do not promise time-in-range from the current table.

## P1.2 Rollups cannot honor the existing record filter grammar

**Sections:** 4.2, 5.3, 5.4, 6.

**Problem and cost:** Rollups group by repository, metric, source, and UTC bucket. They discard trace identity, kind, labels, `origin`, recording method, summary status, and arbitrary properties. A later `filter` on any of those fields cannot recover the admitted subset. Reusing `buildFilter` does not make already-combined rows filterable.

The proposal also projects individual observations into this table. Its rows contain no `statistic`, so an average or daily total becomes indistinguishable from one raw sample unless the read joins back to records before aggregation. That join cannot undo a rollup mixing both. An hourly average of 60 over one hour and 90 over three hours has weighted mean 82.5, not the unweighted 75. A `statistic: avg` without its denominator cannot be combined correctly.

**Evidence:** `internal/substrate/query.go` defines filters over kinds, traits, properties, labels, references, and lifecycle state. `internal/engine/query.go:630` explicitly makes count use the same admitted set as list, as decision 0134 requires.

**Change:** Define record eligibility before sample expansion. Use rollups only when every predicate is constant over their grouping keys; otherwise read raw projected samples joined to eligible records, or refuse the unsupported combination. Exclude provider summaries by an explicit form field. Preserve count or observed duration when a summary is intended to be composable. Compare every optimized result against the raw path under the same filter.

## P1.3 Mutable metric records can silently change stored values' meaning

**Sections:** 4.1, 5.1, 5.3, 5.4, 6, 7.

**Problem and cost:** The index drops `unit`, while a user-editable metric record supplies canonical unit, dimension, and legal aggregation functions. Changing `metric.unit` from kg to g would relabel existing indexed numbers unless all dependents are converted or reprojected. A vocabulary-change hook will not notice an ordinary metric record edit. Live projection and rebuild can then disagree.

The generic `observation.value` also cannot declare one fixed dimension for both weight and glucose. The engine has no declared rule connecting that property's admitted dimension to another record's `metric.dimension`. Reference validation checks the pointer's contract, not this relationship. A writer could associate a mass quantity with a glucose metric and still satisfy the two independent properties.

The proposed pure function of a row and its declaration therefore has an undeclared third input: mutable referenced data. It also makes a generic engine feature depend on the layout of a user-owned health package.

**Change:** Choose an explicit contract. Prefer immutable semantic metric revisions, full metric references, a unit retained on each stored measurement, and validation of metric/unit compatibility. Define how the engine recognizes a metric contract without naming one user's health kind. If semantic edits remain allowed, enumerate and invalidate dependents transactionally or migrate them through ordinary writes. Source priority changes must have a defined read-time effect too. Store derivation version and coefficients on converted records; rebuilding canonical records should replay them, not rerun today's converter.

## P1.4 The fold hook misses deletes and old correction buckets

**Sections:** 5.2, 5.3, 7.

**Problem and cost:** The proposed hook is in `foldRecordOp`, but a tombstone and purge use different fold effects. `foldOne` dispatches them separately in `internal/engine/fold.go:404`; `applyTombstone` in `rows.go:367` changes deletion state, and `applyPurge` in `rows.go:704` removes derived rows explicitly. Copying the create/update hook alone leaves deleted health samples in aggregates.

Recomputing only buckets the new trace touches also leaves stale values when a correction changes the period, metric, source, or timestamps. A trace moved from 06:00 to 07:00 must remove its contribution from 06:00. Removing the `samples` binding must clear existing projections even though future writes no longer qualify. Merges and restores must agree with ordinary records reads as well.

**Change:** Specify a projection lifecycle for create, no-op update, correction, tombstone, purge, restore, merge/split, declaration removal, and import/rebuild. Capture old projected keys before deletion and recompute the union of old and new buckets, including removing empty buckets. Test each lifecycle operation against an aggregate from the eligible live records. Define whether `filter.deleted` is supported or explicitly refused; do not silently return live-only data for it.

## P1.5 Aggregate access needs both repository isolation and caller grants

**Sections:** 5.3, 5.4.

**Problem and cost:** A `repository` column is not an isolation policy. The new tables need explicit RLS and grants. The repository currently relies on `ENABLE` plus `FORCE ROW LEVEL SECURITY`, policies with both `USING` and `WITH CHECK`, and repository-scoped connection state. See `internal/engine/migrations/0001_init.up.sql:553` and `internal/engine/repositories.go:479`.

Within one repository, functions and agents have kind read grants. A rollup keyed only by metric and source cannot distinguish samples contributed by an allowed kind from a denied kind. Checking that an agent may read the metric record does not authorize the measurements. A session query also reads session records, and source/metric labels expose other records.

**Change:** Include RLS, app-role grants, repository erase, rebuild, and cross-repository tests in the table migration plan. Preserve contributing kind identity or restrict rollup use to a complete authorized contributor set. Apply the caller's grants to measurements, sessions, metric/source resolution, and provenance. Charge the same host-call and response budgets on REST-backed function and agent paths. PostgreSQL's [row security documentation](https://www.postgresql.org/docs/16/ddl-rowsecurity.html) confirms that policies are per table and that owners require `FORCE` to be bound.

## P1.6 `maxBuckets` does not bound aggregate work

**Sections:** 2.8 P16, 5.4.

**Problem and cost:** A one-bucket percentile can scan and sort 166 million samples. One bucket combined with many sources and series can return many rows. Many overlapping session windows can repeatedly scan the same samples. `maxBuckets: 400` bounds none of those costs. `fill`, rolling windows, and chart methods introduce further expansion and lookback.

Decision 0134 explicitly accepts an uncapped exact count for the existing list. It does not justify adding unbounded percentile sorts or interval joins. Current function/agent budgets count returned records, so a new aggregate returning one row could evade the practical protection those budgets provide. See `internal/engine/agentloop.go:1109` and `internal/vocabulary/function.go:198`.

**Change:** Specify limits for time span, series, groups, sessions, functions, input work, response rows, response bytes, and concurrency per repository. Apply a database statement timeout and cancellation within the caller's remaining deadline. Reject over-budget requests before execution where possible. Distinguish output truncation from incomplete computation: never present an average over a silently truncated input as the average over the requested interval. Exact percentiles should have a separate smaller raw-sample budget; approximate percentiles need a named method and error contract.

## P1.7 Bucket-level source priority repeats a failure the research warns about

**Sections:** 2.9, 5.4, 6, 7.

**Problem and cost:** Section 2.9 says choosing one source per day undercounts additive metrics. Section 5.4 then chooses the first source with data per output bucket. One high-priority watch reading can suppress the rest of a day's phone readings. Changing chart resolution would change the total, because the winner changes with the bucket size.

`source.priority` is also a single rank across all metrics, although a user can prefer a chest strap for heart rate and a watch for steps. `groupBy: source` and priority merging have no defined interaction. Multiple ingestion paths can contain the same physical measurement, so different source records are not necessarily independent measurements.

**Change:** Keep per-source answers as the initial contract. Add merging only with metric-specific precedence, stable tie-breaking, underlying-origin identity, and overlap rules applied before output bucketing. For interval amounts, preserve uncovered lower-priority intervals and disclose any estimated allocation. Verify that regrouping the same admitted intervals from hours to days leaves additive totals unchanged.

## P1.8 Trace identity does not define replacement or retraction

**Sections:** 2.2, 5.2, 7.

**Problem and cost:** An hour, a sync page, and a session are offered as equivalent chunk boundaries. A sync page is not stable identity: changing page size or provider pagination can shift boundaries and create overlaps or leave old chunks. Two traces with the same metric/source/timestamp also need an explicit conflict rule. A primary key including trace identity admits duplicate physical samples; one excluding it makes overlapping writes conflict.

`host.ids.external` guarantees deterministic hashing of supplied inputs, not deterministic inputs or complete replacement. Section 7's provider/account/upstream-key recipe also omits the output metric or role needed when one mirror produces several observations of the same kind. See `internal/runner/ids.go:32`.

A scalar `origin` cannot represent an hourly trace built from several upstream rows or pages. A late reading changes an already closed period. A corrected timestamp can change the proposed ID, leaving its predecessor live. `put` merges properties; omitting an obsolete `interval`, `offsets`, or statistic does not clear it.

**Change:** Publish versioned ID recipes using stable upstream identity plus output role, or fixed UTC boundaries plus source, metric, and shard identity. Separate fetch pagination from stored chunk identity. Reconcile the complete desired output set per origin, deleting obsolete outputs and explicitly clearing mutually exclusive fields. Use repeated provenance references where a chunk has multiple origins. Define overlap ownership, late-data handling, deletion of the last sample, and tombstone resurrection policy. Prove that changing fetch page size produces identical live data.

## P1.9 Quantity exactness needs a conversion and rounding contract

**Sections:** 4.5, 5.1, 6, 7, 12.2.

**Problem and cost:** Exact decimal strings can hold the input digits, but exact rational conversion does not always produce a finite decimal. `1 [degF]` is `-155/9 Cel`. Dividing into a PostgreSQL `numeric` result or a Python decimal context can round it. A filter near that boundary can change truth if the plan simply converts the stored number into the operand unit. An average can also be a repeating rational.

Money is a useful shape precedent, but its decimal shift is exact and it does not convert between currencies. The quantity proposal adds substantially different arithmetic. See decisions 0012 and 0140, `internal/engine/validate.go:650`, and `internal/engine/query.go:888`.

**Change:** Keep all quantity inputs as decimal strings, including integer-valued readings. Preserve original digits and units. Compare rational affine conversions with exact arithmetic, such as cross multiplication, rather than rounded converted values. Define ordering for mixed dimensions, min/max declaration units, magnitude and digit limits, and conversion overflow. Specify requested output precision and rounding separately, labeling rounded results. Derived mass-to-molar and HbA1c records need coefficients, offset where applicable, source reference/version, and algorithm version; a factor alone does not describe an affine conversion.

## P1.10 Comparator filters are underspecified and can misstate lab results

**Sections:** 5.1, 5.4, 6.

**Problem and cost:** The example `lt: 5` carries no unit, unlike the structured operand promised elsewhere. The one example involving `<5` leaves most bounded-value logic undefined. Does `<10` match `lt: 5` because it possibly meets the bound, or fail because it does not certainly meet it? Does `<5` match `gt: 0`? Treating the reported threshold as a measured value gives incorrect filters and averages. `eq` never matching does not settle `in`, inclusive boundaries, sorting, or bounded operands.

**Change:** Require structured operands with value and unit. Define comparisons as certain interval containment, possible overlap, or an explicit separate operator. The first version should preferably exclude bounded values from ordinary numeric aggregation and refuse unsupported comparator queries. Provide a truth table for all four stored comparators against `eq`, `in`, `lt`, `lte`, `gt`, and `gte`, including equal thresholds and converted units. Define how excluded bounded values are counted and reported. Bound-aware results must not appear as ordinary exact numeric observations.

## P1.11 Canonicalisation needs a convergence protocol, not just triggers

**Sections:** 3, 7, 8, 11.

**Problem and cost:** Mirror writes commit before canonical trigger effects. `deliveryEnvelope` reads the current live record outside the write transaction, not the historical state of the triggering change (`internal/engine/runner.go:32`). A deleted record has no live record in that envelope. Canonicalisation must therefore reconcile current state and identify prior outputs without relying on the deleted record's properties.

WHOOP currently writes mirror puts/patches and does not implement the proposed upstream deletion flow. Its `_Sink` in `bundle.yaml:1880` explicitly clears authoritative omissions. `fromwhoop` cannot retract an upstream deletion that never reaches the mirror. A source correction can also make one of several derived outputs disappear without deleting the source itself.

The drain deadline persists from the first page and across retries (`internal/engine/functions.go:1759`). Waiting out another retry does not give a long backfill a fresh two-minute budget. The final page has a special completion exception; this is not a general long-import mechanism. Canonical triggers add deliveries and writes to a serial dispatcher and one repository write lane.

**Change:** Define per-origin reconciliation, missing-record handling, output-set cleanup, source-version stamps, and protection against stale reconciliation overwriting a newer result. Add an initial canonical backfill for already-existing mirrors and a converter-upgrade replay path. Process bounded historical windows as separate completed jobs with durable progress, allowing incremental sync to run between them. Expose mirror freshness, canonical freshness, backlog, and parked failures separately. Include rescored WHOOP values, deleted mirrors, missing scores, retry after crash, and slow multi-year backfill in acceptance tests.

## P1.12 Phase 0 cannot instantiate the proposed package without engine work

**Sections:** 5.1, 6, 9, 11.

**Problem and cost:** Phase 0 includes kinds whose values are `quantity`, but phase 1 introduces `quantity`. Today's loader has no such datatype and its closed property keys do not include `dimension`, `units`, or `comparator` (`internal/vocabulary/load.go:1789`). An existing `object` plus decimal fields could represent values, but would not supply quantity filters, conversion, or cross-field invariants. Those are different delivered capabilities.

The plan also states that a kind declares `dayAnchor`, without defining whether this is ordinary data or a new dialect key. Calling this phase free of decisions hides the choice of persistent canonical identity, period semantics, and units.

**Change:** Either move quantity admission ahead of the package, or specify a deliberately smaller engine-free package using exact decimal properties and explicit documented units. Start with WHOOP sleep plus a few observations and source/metric records. Remove labs, plans, exercises, and the full catalogue from this phase. Provide an upgrade path for any temporary value shape; converting a populated object property to quantity later is a retype, not a transparent rename. Record the durable identity and time decisions before ingesting personal history.

## P1.13 Health deletion and plaintext retention need an explicit contract

**Sections:** 1, 5.3, 6, 8, 11; missing operator contract.

**Problem and cost:** Index retention is not health-data retention. Deleting projected samples or tombstoning canonical records leaves earlier values in changelog files and their Postgres index. Raw mirrors, canonical copies, function delivery payloads, agent messages, PDF/FIT blobs, and backups can retain the same information.

Blob bytes are explicitly plaintext, protected by filesystem modes (`internal/blobbytes/fs.go:12`). Ordinary health properties are also not sealed merely because the deployment has a credential key. Decision 0050 states that cleartext history cannot be scrubbed by the old reseal mechanism. Account cascades operate on live records and do not erase history. Existing tokens have full repository access, which must remain distinct from function and agent grants.

**Change:** State the threat model and exactly what disconnect, delete, purge, repository erase, and backup expiry remove. Identify which surfaces retain raw payloads and avoid unnecessary copies in logs, FTS, and agent context. Document encryption at rest and export handling as operator requirements if required by the deployment. Do not promise selective historical erasure unless a separate design changes the changelog contract. Audit provider deletion/retention requirements before enabling each connector. Make the privacy boundary visible before import, not after a user has loaded genetic files or lab PDFs.

## P2.14 The headline benchmark does not measure all advertised work

**Sections:** 1, 3.1, 5.2, 5.3, 11; benchmark and report 05.

**Problem and cost:** `bench/query.sql`, `query2.sql`, and `query4.sql` repeatedly use `SELECT count(*) FROM (SELECT bucket, avg(...), ... GROUP BY ...)`. Unused select expressions can be pruned, so these timings do not establish the cost of computing and returning the statistics. The 26 ms rollup, 25 ms rolling-average, and 176 ms M4 statements all use this pattern. The M4 outer count does not need its ordered arrays. The chunk-statistics query can count rows without reading its JSON statistics. `query3.sql` does contain real aggregate `EXPLAIN ANALYZE` queries, so the whole benchmark should not be discarded.

The yearly Athens rollup output has 366 buckets because the input is a UTC year with partial local days at both ends. It establishes neither a complete civil-year result nor boundary correctness. Rebuild uses an unlogged bulk target and does not run the engine fold, RLS, changelog, provenance indexes, or repeated corrections.

Storage arithmetic also changes units mid-argument: 45,500 samples/day at 1,000 bytes each is 16.6 GB of segment payload/year, about 62 segments of 256 MiB. The cited 47 segments uses 750 bytes/sample. The 35 to 45 GB headline must distinguish files, Postgres changelog, records, and indexes. Packed sample payload at 10.9 bytes/sample is about 181 MB/year before hourly envelopes, manager effects, mirror copies, and delivery records. “Under one segment” is not established for that full system.

**Change:** Consume every aggregate output or use `EXPLAIN (ANALYZE, BUFFERS, WAL, VERBOSE)` on the actual query, verify numeric answers against a reference, and keep results for the same complete date range. Benchmark the proposed full table shape and engine transactions before promising milliseconds or rebuild duration. Publish separate per-user figures for segment files, Postgres tables/indexes, WAL, blobs, mirrors, and retained correction history.

## P2.15 UTC rollups cannot serve every proposed civil-time query

**Sections:** 2.4, 3.1, 5.2, 5.3, 5.4, 9.4.

**Problem and cost:** A named-zone query, a sample's observed offset, and a vendor's day are three different calendars. The proposal uses all three without selecting the precedence. WHOOP cycles are not civil days; Oura's activity boundary is not midnight. An offset does not identify a timezone, and one trace-wide offset cannot describe a DST or travel change inside the trace. The rollup drops the offset altogether.

Fifteen-minute boundaries work for many current civil-day boundaries, but not arbitrary query endpoints, sleep windows such as 23:07, or historical offsets. For example, `Africa/Monrovia` used UTC minus 44 minutes 30 seconds in 1970; a minute-valued offset already loses information. This is recorded in the [IANA timezone data](https://data.iana.org/time-zones/tzdb/africa) and reproduced by local `zoneinfo`.

**Change:** Offer explicit named-zone, observed-local-time, and provider-period semantics, initially implementing only one. Store provider date/period identity independently of UTC endpoints, preserve start/end offsets or split traces at changes, and retain unknown timezone status. Use rollups for wholly covered aligned interior buckets and raw data for the boundary fragments. If arbitrary historical dates or offset seconds are excluded, state the supported range. Test DST, half-hour and quarter-hour zones, travel, partial endpoints, and two WHOOP cycles sharing one date.

## P2.16 The physical index and rebuild plan omit the main write costs

**Sections:** 3.1, 5.3, 11.

**Problem and cost:** The benchmark's narrow key is repository plus an integer series and timestamp. The proposed rows repeat metric, source, trace kind, and trace ID, with an additional access path needed to delete one trace's rows. The measured 710 MB does not price that layout. One hour at 1 Hz means 3,600 derived inserts per metric. Corrections repeat deletion, index maintenance, WAL, and rollup recomputation while the repository changelog lock is held (`internal/engine/rows.go:257`). Multiple traces in one effect transaction can recompute the same bucket repeatedly.

`RebuildRepository` clears tables and replays historical effects inside a write-blocking transaction (`internal/engine/rebuild.go:147`, `:211`). Fewer changelog lines do not remove the need to materialize all samples, and naive replay repeats work for every historical correction.

**Change:** Specify primary keys, conflict policy, a trace-owner index, the metric/source/time read index, and rollup keys. Evaluate a rebuildable dictionary for compact internal series IDs while retaining full record references in canonical data. Bulk insert each trace and recompute each dirty bucket once per transaction. Benchmark initial ingest, repeated resync, correction, deletion, and full rebuild with WAL and lock-hold duration. Consider rebuilding samples once from the final folded live records using the same projection function, followed by bulk rollups; prove equivalence with live projection rather than rerunning every obsolete materialization.

## P2.17 A fixed-property trait does not enforce the promised sample contract

**Sections:** 5.2, 5.3, 12.1.

**Problem and cost:** Traits currently check property presence in the declaration and datatype equality. They do not check repetition, runtime presence, array lengths, dimension constraints, or relationships between fields (`internal/vocabulary/load.go:1564`, `:2780`). A `float` property and a repeated float property have the same datatype for this check. Repeated scalar coercion also validates each element, so a repeated numeric field does not automatically accept `null` gaps (`internal/engine/validate.go:181`).

A point observation has `value: quantity`; a trace has `values`, separate `unit`, and one of two timing forms. The one trait described in the proposal fits neither both at once nor a provider mirror whose fields are nested. Recommending that mirrors copy canonical field names beside vendor fields also conflicts with keeping canonicalisation in the health package and doubles stored content.

**Change:** Prefer an engine-recognized trait with explicit point, interval, and packed variants if all consumers intentionally adopt fixed canonical shapes. Add host validation for cardinality, numeric/gap encoding, required roles, timing constraints, and semantic compatibility. Use a declaration key only if field-role remapping has a demonstrated consumer; it needs its own closed grammar under decision 0020. Do not implement both mechanisms initially. Keep mirror conversion in functions and project canonical traces.

## P2.18 Chunk and statistics validation need common hard limits

**Sections:** 5.1, 5.2, 5.3, 10.

**Problem and cost:** “An hour across six channels” is not one trace under the proposed one-metric rule. The 235 KB estimate also depends on numeric precision and timestamp encoding. ECG at 512 Hz, long decimal strings, or irregular offsets can exceed the cap well before an hour. A function can create a value larger than a normal REST request can later reapply, because its complete response frame is 8 MiB rather than 1 MiB. Many individually valid traces can exceed that frame together (`internal/api/core.go:76`, `internal/runner/host.py:35`).

The stats check names count/min/max/sum but omits `sumSquares`, although rollups use it for SD. It does not specify all-null traces, non-finite/overflowing projected values, unequal array lengths, duplicate offsets, samples outside `[at, endsAt)`, or both timing forms being set. Integer millisecond offsets also cannot preserve every high-frequency irregular timestamp. A period being closed does not mean its provider data is final.

**Change:** Bound encoded record bytes, samples per trace, precision, and span on every write path. Chunk by a deterministic byte/sample policy independent of page size, and reserve frame space for all effects and protocol overhead. Define open-period buffering and its maximum freshness delay, plus late corrections to closed periods. Compute stats in one deterministic engine routine or validate every supplied field with a stated arithmetic policy. Validate timing, ordering, gaps, and finite projection. Include empty, all-null, ECG, maximum-size, and cross-boundary cases.

## P2.19 Float rollups are a separate numerical contract

**Sections:** 4.5, 5.1, 5.3, 5.4.

**Problem and cost:** Sensor uncertainty can justify approximate sensor analytics, but it does not justify converting every exact decimal observation, lab value, or future financial quantity to float8. Distinct exact values can become equal, affecting threshold counts and percentiles as well as displayed digits. Very large valid decimals can overflow the derived type. The trace example uses JSON numbers rather than exact decimal strings; fractional readings can therefore lose source exactness before the index is involved.

`sumSquares - sum*sum/n` can lose accuracy through cancellation and produce a negative variance from rounded operands. The stored rollup also cannot answer `first`/`last` because it retains no ordered endpoint values. It cannot answer time-weighted functions without observed durations and boundary values. Calling every discrete metric averageable is too broad for categorical, logarithmic, circular, or vendor-score quantities. PostgreSQL documents the difference between exact `numeric` and inexact floating point in its [numeric type reference](https://www.postgresql.org/docs/16/datatype-numeric.html).

**Change:** Separate approximate discrete-sensor projections from exact observations and money. Define whether trace payloads preserve decimal strings or scaled safe integers, or explicitly admit floating-point source values; do not promise exact digits for all three. Retain an exact `numeric` path where promised and expose approximation/rounding in results. Use mergeable stable variance state such as count/mean/M2, with a defined deterministic reduction policy. Specify sample versus population SD and `n < 2`. Add first/last timestamp-value pairs or explicitly fall back to raw data. Declare each metric's permitted operations instead of deriving every permission from three aggregation styles.

## P2.20 The aggregate request and response are not yet an implementable API

**Sections:** 4.3, 5.4; decisions 0079 and 0134.

**Problem and cost:** `value: heart_rate` could mean a property, metric ID, or code. A metric needs a full record reference; arbitrary kinds need a property selector. The session variant is not placed unambiguously within the JSON structure and offers no complete session filter. A bare kind list does not select one workout or exclude naps. The records path says a metric controls legal functions, but expenses and tasks need no metric record. “Tasks closed per week” also needs a declared closure-time anchor, not merely any temporal binding.

Current `readList` turns a doubly bounded `filter.properties.at` into a window read (`internal/engine/runner.go:296`). The proposal must define precedence and coexistence for `aggregate`, that filter, `q`, tail, `count=1`, `first`, `after`, `offset`, `orderBy`, and `expand`. The aggregate's range should clip samples, while filtering trace starts would incorrectly discard a trace that overlaps the range.

The result sketch omits coverage, source selection, excluded bounded values, exact/approximate status, and a snapshot identifier. A multi-function row needs a unit per result: count is unitless, duration-in-range is time, and SD of temperature is a temperature difference. `truncated` alone cannot tell a caller how to resume or what was omitted.

**Change:** Define a strict discriminated request with either metric-reference selection or kind/property selection and an explicit time anchor. Give session windows a full independent `Filter`, half-open interval semantics, overlap rules, maximum session count, and record version in results. Initially refuse recurrence expansion and conflicting modes. Define `percentile` probability/interpolation, function parameter units, calendar/fixed-window exclusivity, rolling denominators, fill-before/after-rolling, and stable ordering.

Name request/response structs in `internal/substrate`, expose the same operation through the runner and core query tool, and pin them in the wire golden. Return resolved series identities, function-specific units, zone/boundaries, count and coverage, source policy, snapshot head/generation, and explicit completeness. Page complete results or reject excessive output; do not invent continuation from a boolean. Unknown keys and unsupported combinations must fail by name.

## P2.21 Several health-kind shapes lose identity or cannot be validated

**Sections:** 2.6, 2.7, 6, 7, 10.

**Problem and cost:** Generic observations and typed sessions are a reasonable split, but the proposed fields do not yet carry the source model:

- `metric.code` and `codes` need coding system, code, and optional version/display, not ambiguous strings. `group` must be defined as a classification value or a reference if it names another record. Unit/dimension equivalence cannot make HRV methods or specimen types interchangeable.
- Daily observations need provider date, period identity, completeness, and raw-versus-summary form. `statistic` alone also describes an interval sum or nightly average. A single date cannot identify two WHOOP cycles.
- `metric.valueKind` admits coded and ratio results, but `observation` only declares quantity. Components need a declared shape with metric/code, value, and unit so a blood pressure reading remains one measurement with two identifiable components.
- A lab report can contain results from different specimens and collection times. A report-only specimen loses that distinction. Results need their own stable upstream identifiers, provenance, specimen/method context, coded system identifiers, and structured ratio numerator/denominator. The claimed exactly-one-of-value rule has no declared cross-property validator today.
- `labresult`, `lap`, `set`, `tag`, and plan/step do not all declare the `origin` and `source` that section 7 promises for every output. `trace` lacks an explicit session association where timing alone cannot distinguish overlapping workouts.
- `workout.gear` has no proposed target kind. `lap.planStep` needs the step kind omitted from the opening inventory and phase 0. `plan.completedBy` must be repeated if a reusable plan can be executed more than once, or omitted in favor of reverse reads of `workout.plan`.
- Sleep stages mix an in-bed envelope with exclusive sleep stages. Some inputs represent these as overlapping intervals; flattening them into one partition can double-count duration or erase context.

**Change:** Turn the chosen small subset into loadable declarations and real sample records before expanding the catalogue. State required fields, cardinalities, state transitions, identifiers, and validation ownership. Use typed reference properties for all record links under decision 0044. Make delete ownership explicit: parent ownership may cascade, while deleting a metric definition or one provenance input must not accidentally delete every independent derived result. Cascades currently run through GC (`internal/engine/gc.go:13`), so define immediate aggregate visibility separately. Preserve unverified lab mappings for review rather than presenting all catalogue codes as validated equivalences.

## P2.22 A closed unit table is viable only as a documented subset

**Sections:** 5.1, 10, 12.7.

**Problem and cost:** The table can cover a narrow initial feature, but the catalogue includes temperature deltas, pH, dB, counts, annotated lab units, and arbitrary assay units. A rational scale plus optional offset cannot express every such conversion. Absolute and delta temperatures cannot share the same affine conversion behavior: adding the Celsius offset to a temperature difference is wrong. Likewise, an SD conversion must scale without adding an offset. One pseudo-dimension per `[IU]` code does not make results from different analytes or assays comparable.

UCUM explicitly permits fixed-table limited conformance, while full conformance requires comparing expressions by meaning. Its expression language admits infinitely many codes, so 100 literals cannot mean general UCUM acceptance. See [UCUM sections 1 to 3](https://ucum.org/ucum).

**Change:** Publish the supported subset, immutable meanings, accepted aliases, and unsupported-unit behavior. Preserve the vendor unit string even when canonical conversion is refused. Distinguish absolute quantities from differences; either implement nonlinear conversions explicitly or keep those metrics non-convertible. Require metric/assay identity for arbitrary units. Keep one authoritative table and generate or conformance-test Python, Go/SQL, and console behavior. Use a bounded parser only when real imports require expressions beyond the subset; a parser does not supply analyte semantics. Test against authoritative UCUM cases, not solely agreement between two young libraries.

## P2.23 File imports, retention, and upgrades need separate delivery plans

**Sections:** 3, 5.3, 7, 8, 11, 12.6.

**Problem and cost:** Adding `host.blobs.get` does not make a multi-GB Apple export importable. REST blob upload caps each object at 64 MiB (`internal/api/blobs.go:21`), functions have a 60-second limit, and frames and drains are bounded. The missing work includes upload chunking, compressed archive streaming, restart position, progress, cleanup, and bounds on decompressed bytes. Routing ordinary personal imports through a DSN operator command would give the importer substantially different privileges from the token-based user path.

Dropping old raw index rows while retaining rollups also needs a completeness contract. Percentiles and partial buckets require raw data; a reader cannot distinguish “no samples” from “not materialized.” Rebuilding old data synchronously on demand can turn a small query into an unbounded write job. Multi-year correction after eviction needs the same old/new invalidation rules as live data.

A quantity value alone does not change the current wire golden because record properties are open values. That narrow claim is correct. It does not cover the new aggregate structs or the console. Unknown datatypes fall back to string handling in `web/console/src/lib/record-schema.ts`; editors, filters, titles, nested/repeated values, and unit/comparator controls all need implementation. New keys and datatypes require reader upgrades under decision 0020, and populated catalogue semantic corrections require more than adding rows.

**Change:** Start file import as a resumable token-authenticated client that streams locally and submits bounded records/blobs, or design a durable bounded import job explicitly. Defer whole Apple exports until that path exists. Define materialization coverage, retention policy, background restoration, unavailable-range responses, disk budgets, and rebuild objectives. Keep the recovery export's current point-in-time directory contract (`internal/engine/export.go`); document that derived tables rebuild and that blob availability follows existing GC. Add a separate application export format only when required.

Specify minimum server/console versions, capability discovery or explicit unknown-feature fallback, unit-table versions, and sample-package upgrades. Aggregate replies must be named structs mirrored in `types.ts` and generated into the golden by its Go test. Do not hand-edit the golden. Rollbacks must refuse unsupported closures clearly rather than silently reinterpret data.

## P2.24 Shipping every adapter inside one sample creates undeclared dependencies

**Sections:** 6, 7, 8, 11.

**Problem and cost:** The package would ship functions that grant reads on all provider kinds, even when those providers are not installed. `resolveFunction` resolves explicit read/write kind grants against the current registry and rejects missing kinds (`internal/vocabulary/function.go:1075`). Globs are a deliberate exception, but adopting them changes the permission contract and still does not define trigger installation or removal. Typed references to absent provider kinds have similar closure concerns.

The core package, optional providers, and a user-edited imported sample also have different ownership and upgrade rules. “One sample with purpose doing the listing work” answers navigation, not dependency resolution, converter updates, or preservation of user edits.

**Change:** Keep the base health declarations independent of every optional mirror. Install a provider-specific adapter closure only when its provider and health package are available, with explicit grants and full reference rewriting onto the repository authority. Test import with no provider, WHOOP only, and provider removal. Document how converter upgrades reconcile existing outputs and how owners retain their custom declarations. A shared Python module is already supported by the runner; do not budget it as missing runtime infrastructure.

## Recommended first delivery and acceptance checks

Build one WHOOP-backed consumer: a monthly sleep view or agent tool showing nightly sleep duration and a few labeled recovery measurements. Use `source`, a small metric catalogue, `sleep`, and only the observations the consumer needs. Preserve WHOOP identity, local offset, and source references. Either implement quantity first or explicitly use an engine-free exact-decimal shape with a planned upgrade. Use existing bounded record reads for this first consumer.

The first delivery should prove:

1. Import and sync work with WHOOP as the only installed provider; existing mirror history can be canonicalised.
2. Repeated identical sync changes no canonical values or versions. Missing scores, corrections, deleted mirrors, and re-enabled sources converge without duplicates or obsolete observations.
3. Provider summaries remain distinguishable from raw readings. Two cycles on one date remain distinct, and the monthly consumer states its day-assignment rule.
4. Rebuild and recovery import reproduce the canonical data. Export includes the expected source records and any retained blobs.
5. A new app or agent can consume the result without knowing WHOOP field names or inventing unit conversions.

Then prototype one dense discrete metric with fixed chunk identity, a single source, and only `count`, `avg`, `min`, and `max`. Compare a bounded raw aggregate with its rollup, including a partial boundary, one correction, deletion, and rebuild. Measure full write cost, query work, and storage before selecting the production index layout.

Cut from the first implementation: the 240-marker catalogue, full exercise catalogue, source merging, session joins, rolling functions, gap carry-forward, percentiles/sketches, M4, TimescaleDB, PDF OCR, and multi-GB Apple imports. Preserve these as separate follow-up proposals with a consumer and acceptance criteria. Lab comparator semantics should be settled before lab import, not inferred from the sensor prototype.

The Strava exclusion is supported by the current [API policy](https://www.strava.com/legal/api_policy), including its storage and AI restrictions. Keep that restriction separate from the technical architecture. Garmin availability is a dated provider-planning assumption based on the research's secondary reports; the [official program overview](https://developer.garmin.com/gc-developer-program/overview/) is insufficient to establish permanent closure. Recheck access when scheduling that connector, and retain file import as an independent capability.
