# Codex review of the observations plan

Reviewer: Codex (gpt-6-astra), run through T3 on 2026-10-10 against
[observations.md](../../observations.md) as it stood that day. The brief was
to review, research alternatives online, and suggest changes without editing
the plan. The text below is the reviewer's final message, verbatim except that
absolute file paths were turned into relative links. Section 13 of the plan
records what was adopted.

---

The proposal’s overall direction fits Substrate, but I would resolve several correctness gaps before implementing the index or freezing the `observed` trait. The largest risks are incorrect aggregates, mutable measurement semantics, and retention that cannot reproduce earlier answers.

I reviewed [the observations proposal](../../observations.md), its inherited health design, previous reviews, the unit-table generator, and relevant loader and fold code. I also checked current primary documentation for UCUM, FHIR, LOINC, OpenTelemetry, SensorThings, Health Connect, Home Assistant, PostgreSQL, and TimescaleDB. **No design files were changed.** The findings below are suggestions for Fable to consider, not accepted design decisions.

1. **Make measurement semantics immutable before building the index.**

   Section 7.2 says the engine reads nothing from `metric`, so editing a metric cannot change an indexed value’s meaning. Other sections nevertheless rely on the metric for `wrapAt`, `circular`, temperature differences, and compatibility.

   For example, `{value: "0.3", unit: "Cel"}` does not distinguish an absolute temperature from a temperature difference. The trace shape is more ambiguous: it carries only `unit` and decimal values.

   Fable should choose one explicit mechanism:

   - Copy the interpretation fields needed by the engine onto each observation and trace.
   - Reference an immutable, versioned measurement definition whose historical version remains available.

   I lean toward copying the small set of engine-required fields, consistent with the existing fold model. Keep names, descriptions, and display preferences mutable.

   A dimension vector cannot supply the missing meaning. LOINC distinguishes component, property, timing, specimen, scale, and sometimes method precisely because superficially similar measurements can mean different things. [LOINC’s model](https://loinc.org/kb/faq/structure)

2. **Define exactly which queries each rollup can answer.**

   The proposed `twa_sum`, `held_ms`, and boundary fields cannot generally answer every request-specific `maxGap`, quality filter, threshold duration, or source-priority policy.

   A precomputed duration-weighted total has already discarded internal gaps. Applying a different gap cap later requires knowing those gaps. Similarly, `sum`, `min`, and `max` cannot reveal how long glucose exceeded an arbitrary threshold.

   Grouping keys being present is necessary, but does not establish that a rollup contains enough information for the reducer.

   My recommendation is an explicit eligibility rule: use a rollup only when its stored semantics and sufficient statistics match the request. Otherwise read raw points within a budget or return an explicit unsupported/unavailable result.

   Timescale’s two-step aggregates are useful precedent: retain an intermediate aggregate, merge compatible aggregates, then compute the requested result. Its boundary interpolation API explicitly accepts neighboring aggregates. [Time-weighted aggregates](https://www.tigerdata.com/docs/reference/toolkit/time_weight/time_weight), [boundary interpolation](https://www.tigerdata.com/docs/reference/toolkit/time_weight/interpolated_average)

3. **Compute state intervals before filtering by their values.**

   The inherited evaluation order filters values before reduction. That can produce incorrect `timeInState` and `durationIn` answers.

   Consider a door that is open at 10:00, closed at 10:05, and open at 10:10. During `[10:00, 10:15)`, it was open for ten minutes. Filtering out `closed` observations before constructing intervals can make it appear open for fifteen minutes.

   The same problem occurs when filtering out an invalid reading that should terminate a previously valid reading.

   Separate stream selection from result-value predicates. Select the relevant streams, construct their valid intervals using all necessary transitions, then select the intervals that satisfy the requested code or threshold.

   This deserves a small executable specification before SQL optimization.

4. **Do not turn boundary carry-in values into new observations.**

   Section 5.4 proposes copying the previous trace’s last value to offset zero of the next trace. Without additional metadata, that copy:

   - Increases `count` despite no new measurement.
   - Changes a sample-weighted `avg`.
   - Resets the apparent age of a stale reading.
   - Requires correcting subsequent traces when the original changes.

   Prefer retrieving the preceding point during the read. If a trace must be independently readable, store a boundary anchor with its original timestamp and an explicit indication that it is not another measurement.

   Also define whether a hold expires during an internal gap, even when a successor eventually arrives. A later heartbeat should not silently turn an offline period into continuously observed coverage.

5. **Separate counter resets, wraparound, and signed accumulation.**

   The counter section admits decreasing values for net meters, but defines `increase` as a sum of positive steps. That loses legitimate negative changes.

   Other unresolved cases include multiple resets within one trace, unknown initial baselines, multiple wraps between readings, and equal timestamps with conflicting values. A single trace-level `resetAt` cannot describe several reset epochs unless traces split at each reset.

   The proposed `decrease` rule also differs from Home Assistant: its documented increasing-total behavior tolerates decreases below 10%, whereas the proposal treats every decrease as a reset. A noisy `100 → 99 → 101` sequence can otherwise create a large false increase. [Home Assistant sensor semantics](https://github.com/home-assistant/developers.home-assistant/blob/master/docs/core/entity/sensor.md)

   I suggest distinct contracts for monotonic totals, signed cumulative totals, and wrapping registers. Preserve reset epochs, define wrap modulus precisely, and return uncertainty when missing readings prevent reconstruction. OpenTelemetry’s distinction between known and unknown start times is particularly useful here. [OpenTelemetry resets and gaps](https://opentelemetry.io/docs/specs/otel/metrics/data-model/)

6. **Treat missingness and validity as first-class information.**

   `quality` is useful, but the proposed generic observation contract still mostly expects a numeric or coded result. Real feeds also report “unavailable,” “not measured,” “invalid,” and “normalization failed.”

   These have different consequences. A thermometer becoming unavailable should usually stop contributing held temperature. A missing lab result should remain visible without becoming zero or a clinical result code.

   Keep three concepts distinct: the result, whether it is usable, and why a result is absent. For packed traces, decide whether this requires per-point flags, validity spans, or splitting traces whenever validity changes.

   FHIR explicitly models `dataAbsentReason`; Home Assistant distinguishes unknown values from unavailable devices. Those distinctions should survive ingestion. [FHIR Observation](https://hl7.org/fhir/R4/observation.html), [Home Assistant sensor states](https://www.home-assistant.io/integrations/sensor/)

7. **Reconsider stream identity, even if a `stream` record remains deferred.**

   `(metric, source, about)` is insufficient unless `source` has a very strict definition. One device can expose multiple channels, tariff registers, or measurement methods for the same metric and observed thing.

   The proposal has `channel`, `acquisition`, and qualifiers, but they do not appear in the stated stream key. This risks merging streams or allowing one reading to replace another in `observed_latest`.

   A persistent stream record is one reasonable alternative. SensorThings uses a Datastream to group observations of one observed property produced by one sensor. [SensorThings model](https://docs.ogc.org/is/15-078r6/15-078r6.html)

   I would not require another kind immediately. First define a bounded, stable stream identity and test two channels from the same device. A stream record becomes worthwhile if it also owns configuration, availability, or semantic versions.

8. **Separate source priority from duplicate identity.**

   Preserving sources and resolving priority before bucketing are good choices. But choosing between overlapping sources does not establish whether two records represent the same original measurement.

   A watch measurement may arrive directly and through a phone aggregator. Preserve original producer identity and upstream measurement identity separately from the provider that delivered it. Use exact identity where available; avoid silently deduplicating merely similar timestamps and values.

   Health Connect itself applies priority-based deduplication only to Activity and Sleep data, rather than treating it as one universal rule. [Health Connect aggregation](https://developer.android.com/health-and-fitness/health-connect/aggregate-data)

   Also reconsider `prefer-raw`: the inherited rule chooses raw whenever a bucket has *any* raw data. One minute of raw steps could displace a complete daily summary. Require a coverage policy, or return both results with their coverage and let the application choose.

9. **Distinguish index retention from permanent raw-data deletion.**

   Removing `observed_points` saves database space, but the original traces remain in records and the append-only changelog. Raw data still exists and can be reconstructed.

   Permanently removing authoritative raw data is a different operation. Once only a rollup remains, arbitrary historical filters, revised source priorities, partial-window calculations, and some corrections cannot be reproduced.

   The design should distinguish:

   - Expiring a reconstructible index.
   - Archiving raw records with slower retrieval.
   - Permanently discarding raw data and retaining durable summaries.

   For the first release, I recommend retaining authoritative raw data and treating point retention as a materialization policy. Defer permanent reduction until the supported historical queries and correction behavior are explicit.

   This is a real operational constraint in existing systems: Timescale warns that refreshing aggregates over deleted raw data can remove the retained aggregate results. [Refresh and retention behavior](https://www.tigerdata.com/docs/build/continuous-aggregates/refresh-policies)

10. **Keep the named quantity categories, but tighten the UCUM boundary.**

    The proposal correctly rejects dimension-only compatibility. However, it later says compound units acquire their quantity category through their vector, which reintroduces ambiguity between energy and torque, or frequency and radioactivity.

    There is also a concrete inconsistency in the generator: [`V.A` is placed in `power`, with base `W`](../coretable.py) (line 62), despite the accompanying note and proposal saying apparent power is distinct.

    Separate unit syntax, mathematical conversion, and measurement compatibility. Require semantic context when the unit cannot determine the category.

    A bounded UCUM subset is reasonable. UCUM explicitly allows limited conformance, but annotation-sensitive compatibility and arbitrary-unit conversion extensions should be documented as Substrate rules. [UCUM specification](https://ucum.org/ucum)

    I would also prefer `measures` over redefining `dimension` to mean a named semantic category. That naming choice is less urgent than specifying where the category is stored.

11. **Define a stronger numerical contract for counter differences and rollups.**

    Keeping exact decimal source values is the right foundation. A general `approximate` flag does not fully address errors introduced by converting cumulative counters to `float8`.

    For example, `9007199254740992` and `9007199254740993` collapse to the same binary64 value. Subtracting them after conversion loses an increment that was exact in storage. This is a boundary example, not a claim about typical household meters.

    Preserve exact counter endpoints through differencing, or impose documented magnitude and resolution bounds. Use approximate arithmetic where its error is acceptable and test that bound.

    Reducers also need semantic rules beyond the four forms. Mean pace differs from total duration divided by total distance; a circular mean can be undefined for opposing directions; a mean of logarithmic readings differs from a physical exposure average.

12. **Design trait evolution instead of assuming the first contract can be complete.**

    I confirmed that the [loader requires contracted properties to exist](../../../../internal/vocabulary/load.go) (line 2783). The compatibility concern is real. The conclusion that the trait must anticipate every future requirement is too strong.

    The current proposal already leaves out fields needed by some of its promised behavior. Reserving more fields will not establish completeness.

    Fable should compare a minimal stable trait with additional traits, versioned variants, or an explicit backward-compatible extension mechanism. Each has costs, but those costs can be evaluated directly.

    I would keep the base `observations` sample and domain-specific samples. I would also soften “sample kinds can never move”: ordinary sample upgrades cannot perform the move today, but a purpose-built migration is possible. It is expensive, rather than fundamentally impossible.

13. **Complete the correction protocol and latest-value lifecycle.**

    Stable IDs, revisions, and per-origin reconciliation are strong foundations. Two gaps remain.

    First, one trace can combine several origins. Independently rerunning each origin’s adapter must not let the last writer erase points contributed by another origin. Reconciliation needs ownership at the trace-period level and an atomic replacement strategy.

    Second, `observed_latest` describes newer inserts and deletions, but not all corrections. If the newest observation moves backward in time or changes `metric`, `source`, or `about`, both the old and new stream entries need recomputation. Equal timestamps also need a deterministic tie rule.

    Preserve the upstream revision separately from the adapter version. An adapter bug fix must be able to regenerate canonical output even when the upstream revision has not changed.

    Expired change cursors also need a recovery strategy. Health Connect documents full rereads and deduplication as one recovery path. [Health Connect synchronization](https://developer.android.com/health-and-fitness/health-connect/sync-data)

14. **Benchmark the complete write and rebuild path before choosing a storage backend.**

    The existing report appropriately acknowledges that some timings measured grouping without consuming the aggregate results. Those figures should remain exploratory.

    The next benchmark should measure authoritative traces, changelog revisions, point indexes, rollups, WAL, correction cost, and rebuild time together. Include late data, repeated rewrites, sparse states, several repositories, and concurrent reads.

    The current estimate of 5.3 million rollup rows per home per year makes rollup layout worth measuring early. Consider reducer-specific storage if the wide row proves costly.

    Plain PostgreSQL remains a reasonable first implementation. Time partitioning offers pruning and lifecycle operations, but partition layout must match repository isolation and query patterns. TimescaleDB should earn its place through the complete workload. [PostgreSQL partitioning](https://www.postgresql.org/docs/current/ddl-partitioning.html)

There are also three direct specification contradictions to resolve: coded `amount` values are refused in section 5.1 but allowed in section 5.2; the rollup table excludes counters while the reducer table reads counter rollups; and the “latest” projection lacks several fields needed to interpret its returned values without consulting the original record.

For Fable’s next pass, I would request these concrete deliverables:

- A measurement-semantics contract showing which interpretation fields are immutable and where they live.
- A reducer specification with counterexamples for gaps, transitions, resets, overlap, and partial coverage.
- A rollup eligibility matrix identifying raw-data fallbacks and behavior after retention.
- A correction specification covering multi-origin traces and every `observed_latest` mutation.
- A smaller first implementation: one health provider, exact quantities, separate sources, raw aggregates, and replay equivalence. Exercise home counters and categorical states with fixtures before freezing the trait or enabling optimized rollups.
