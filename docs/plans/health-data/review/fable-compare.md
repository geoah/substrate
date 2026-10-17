# Health data proposal: comparison against other systems and best practice

Reviewer: Fable 5.1 research sub-agent, 2026-10-08. Subject:
`docs/plans/health-data.md` on branch `t3/d9d5090b` and the six reports under
`docs/plans/health-data/`. Read-only; nothing in the tree was changed.

Evidence legend, used on every claim below:

- [primary] fetched from the official document, source file or paper named.
- [excerpt] only a search-engine excerpt of the source was available; the
  URL is given but the page itself was not fetched (paywall, 403, or cert
  failure).
- [secondary] a third-party summary of a primary source.
- [unverified] stated from memory or inference; not confirmed today.

The proposal's own research is not repeated here. Where this review agrees
with it, it says so in one line and moves on.

## 1. How others physically store dense observations

### What others do

**Apple HealthKit (on-device SQLite)** [primary: christophhagen/HealthDB
README, https://github.com/christophhagen/HealthDB]. `samples` is the entry
point (date interval, data type). A scalar reading is one row in
`quantity_samples`. A series (a workout's heart rate) is one `samples` row,
one `quantity_sample_series` row holding the series specification and count,
and the values packed in `quantity_series_data` joined on `hfd_key ==
series_identifier`, scaled in the type's default unit. Every series also has a
`quantity_sample_statistics` row keyed by `owner_id == samples.data_id`. The
public API matches: `HKQuantitySeriesSampleBuilder` builds one sample holding
many quantities and `HKQuantitySeriesSampleQuery` reads them back [primary:
developer.apple.com HKQuantitySeriesSampleBuilder]. Samples are immutable;
corrections are delete and re-add under `HKMetadataKeySyncIdentifier` plus
`HKMetadataKeySyncVersion` (already in report 04).

**Google Health Connect (on-device SQLite)** [secondary: verybadsoldier/
android-health-connect-data-explorer; primary for the guidance:
developer.android.com write-data]. A `HeartRateRecord` is one row in
`heart_rate_record_table` (the interval) and one row per sample in
`heart_rate_record_series_table` (`epoch_millis`, `beats_per_minute`). So the
on-device store is one row per sample, chunked by record. The official sizing
guidance: "Instead of creating a single, day-long record that is constantly
updated, you should create multiple smaller records, each representing a
specific time interval. For example, for heart rate data, you should create a
new HeartRateRecord for each minute", and at each sync (for example every 15
minutes) "write all the one-minute records that have been created since the
previous sync". Upserts are keyed by `clientRecordId` and only land when
`clientRecordVersion` is higher than the stored one. Writers are told not to
default `zoneOffset` to UTC.

**Google Health API v4** [primary: developers.google.com/health/data-types
and the RPC reference]. "Every data point is stored with both a physical UTC
timestamp and the UTC offset active at the time of the event." The `DataPoint`
message carries `observation_sample_time`, `observation_time_interval` (with
offset) and `civil_time_interval`; `CivilDateTime` "ensures that neither the
timezone nor the UTC offset can be set". Three verbs: `rollUp` over physical
windows (`windowSize`, minimum 1 s, page 1,440 default and 10,000 max),
`dailyRollUp` over civil days (`windowSizeDays`, 90-day range, 14 days for
heart rate), and `reconcile`, which merges sources into one stream and
"will exclude data points that are identified as recorded by wearables in
intervals when they were not actually worn"; `dataSourceFamily` selects
`all-sources`, `google-wearables`, `google-sources`, `self-sources`.

**Strava** [excerpt: Strava Engineering, "From data streams to a data lake",
https://medium.com/strava-engineering/from-data-streams-to-a-data-lake-b6ca17c00a23;
the page itself returns 403]. Each stream (time, latlng, heartrate, power,
altitude) of each activity is one gzipped JSON array file on S3, over 150 TB
across a billion activities. The chunk is the activity. Cassandra holds
derived data (tiles, leaderboards), not the streams.

**WHOOP** [secondary: The DataStory, "Case study: WHOOP", with no citation
given for the figures]. Five sensors at 100 readings a second, about 100 MB
per user per day of raw data on WHOOP's side; none of it is exposed by the
API. Cycle boundaries are chosen by a model from sleep onset and "past cycle
times may change as WHOOP learns more" [primary: engineering.prod.whoop.com
dev-platform-2]. Oura, Garmin, Terra and Junction publish nothing about
their physical stores; Open Wearables is one narrow Postgres table (report
04). Epic Chronicles: no public source found [unverified].

**General time-series stores** [primary: Ganesh Vernekar, Prometheus TSDB
head block; excerpt: TsFile VLDB 2024 paper and the IoTDB cwiki]. Prometheus
appends samples to a WAL and an in-memory chunk; "once the chunk fills till
120 samples or spans up to chunk range (2h by default), a new chunk is cut";
2-hour blocks are compacted into 6h, 18h, 54h blocks; the 120 figure comes
from the Gorilla paper and yields about 1.3 bytes a sample. Since 2.39 an
`out_of_order_time_window` accepts late samples into separate out-of-order
chunks [excerpt: PromLabs]. Apache IoTDB's TsFile keeps statistics (count,
min, max, first, last, sum) at page, chunk and file level, so aggregate
queries read statistics without decoding values.

**Clinical waveforms** [excerpt: Goodwin et al., Physiol. Meas. 41(3) 2020,
doi 10.1088/1361-6579/ab7cb5]. "Traditional database approaches are not
appropriate for large scale storage of physiological waveforms"; with
domain-specific compression one year of full-resolution waveforms from a
42-bed unit fits in under 300 GB. ICM+ archives in HDF5 with chunked,
scale-offset compressed datasets [excerpt]. FHIR carries a waveform as
`SampledData`; R5 replaced `period` with `interval` plus `intervalUnit`
(UCUM) and added `offsets` and `codeMap`, with `E`, `L`, `U` markers in the
data string [primary: hl7.org R5 datatypes diff].

**FHIR servers** [primary: hapifhir.io schema and NormalizedQuantitySearchLevel;
medplum.com search-architecture]. HAPI stores the resource JSON in
`HFJ_RES_VER` and one index row per search parameter (`HFJ_SPIDX_QUANTITY`
for `value-quantity`); with normalized quantity search enabled it also writes
a row converted to the canonical UCUM unit and searches that. `$lastn` needs
Elasticsearch. Medplum keeps one table per resource type with one column per
search parameter (quantity as `double precision`) beside the JSON, plus
lookup tables. Metriport runs a HAPI fork on Aurora Postgres with S3 for
documents and OpenSearch for retrieval [excerpt: deepwiki metriport].
Google Cloud Healthcare answers analytics by streaming every resource change
to one BigQuery table per resource type [primary: cloud.google.com FHIR to
BigQuery streaming tutorial]. Microsoft's server uses a resource table and
typed search-parameter tables in SQL Server [excerpt; not verified in
detail]. EHRbase 2.0 (April 2024) rewrote its AQL engine; an operator still
reports 20-minute AQL queries over 150 GB [excerpt: openEHR discourse
11953]. None of these stores dense samples as one resource per sample at
wearable rates; they store resources and index columns.

### Where the proposal agrees

The trace (one metric, one source, one closed period, arrays on the record,
precomputed `stats`) is the HealthKit series sample, the Health Connect
record, the Strava stream file, the Prometheus chunk and the IoTDB chunk with
its statistics. The derived narrow index beside the truth is the HAPI and
Medplum search-column pattern. Replace-by-key corrections match HealthKit
sync versions, Health Connect `clientRecordVersion`, Garmin re-sent
`summaryId` and Google `patch`. The regular form (`at`, `interval`,
`values` with nulls) is FHIR R5 `SampledData` exactly, and the irregular
form is its `offsets`.

### Where it diverges

1. **`stats` and the rollup rows lack `first` and `last` (with their
   timestamps) and any duration-weighted sum.** IoTDB keeps first and last at
   every level; HealthKit offers `mostRecent`; the proposal lists `first`,
   `last` and a `timeWeighted` style in P1 and P5 but the rollup row
   `(n, sum, min, max, sum_squares)` cannot produce any of them, and M4
   needs first and last per bucket too. Time-weighted averages over irregular
   samples need a duration-weighted sum (Timescale `time_weight` with LOCF
   or linear) [primary: docs.tigerdata.com time-weighted averages].
2. **One `utcOffset` per trace is lossy.** Health Connect carries
   `startZoneOffset` and `endZoneOffset` on every interval record; Google
   carries an offset per point. A one-hour chunk spanning a flight or a DST
   change assigns the wrong civil date to part of its samples.
3. **"The changelog holds each period about once" assumes a source delivers
   an hour complete.** Health Connect's guidance presumes 15-minute syncs of
   closed one-minute records; Apple Watch and Garmin deliver hours later,
   WHOOP rewrites cycle boundaries retroactively, and Junction documents that
   "a correction at the exact same timestamp replaces the earlier value"
   [primary: docs.junction.com time-and-data-semantics]. Rewrites of closed
   chunks will be routine for some sources.
4. **Rollups cannot serve decision 4.** The rollup key has no offset or
   local date, so "the civil date at the sample's own offset" is not a sum of
   rollup rows; only a home-zone day is. See section 7.
5. **"Nobody stores one row per dense sample in a general table that lasts"
   is overstated.** Health Connect's on-device store does exactly that, and
   HAPI and Medplum do where an integration writes one Observation per
   reading. The point holds for a changelog line per sample, which is the
   proposal's real argument.

### Recommended changes

- Add `first`, `last` (value and timestamp) and `durationSum` (or a
  `weightedSum` under a declared interpolation) to `stats` and to
  `sample_rollups`; declare which functions each row can serve.
- Carry `utcOffsetStart` and `utcOffsetEnd` on a trace, or require the trace
  builder to split a chunk at an offset change; add `utc_offset_min` to the
  rollup key (section 7).
- State the rewrite model plainly: a trace is keyed by (metric, source,
  period), rewrites are expected, and a monotonic `revision` on the trace
  refuses a stale rewrite the way Health Connect refuses a lower
  `clientRecordVersion`.
- Rephrase the "nobody" sentence to "no one keeps one changelog line per
  sample".

## 2. Append-only stores carrying dense data

### What others do

- **Kafka** keeps the latest record per key under `cleanup.policy=compact`;
  a null-valued tombstone deletes the key and is itself removed after
  `delete.retention.ms` (one day by default) [primary: docs.confluent.io
  log compaction]. This is the pattern people cite for erasure in event
  pipelines.
- **EventStoreDB / KurrentDB** deletes nothing on write; `$maxAge`,
  `$maxCount` and `$tb` (truncate before) mark events eligible and a
  scavenge reclaims them [primary: docs.kurrent.io streams].
- **Datomic** has excision: "the complete removal of a set of datoms
  matching a predicate", for "removing data for privacy reasons" and
  retention, never to correct data; "every excision creates a permanent
  record"; large excisions trigger indexing "proportional to the size of the
  entire database" [primary: docs.datomic.com excision].
- **XTDB v2** has `ERASE`: "Irrevocably erases documents from a table, for
  all valid-time, for all system-time", added because "in some cases it is
  legally necessary to irretrievably delete data (e.g. for a GDPR request)"
  [primary: docs.xtdb.com txs]. XTDB v1 kept only hashes in the immutable
  transaction log and put documents in an evictable store [excerpt: JUXT
  blog, xtdb docs].
- **Prometheus, Gorilla, IoTDB** append samples and let the store cut
  immutable chunks per time window and compact them (section 1). The writer
  never decides the chunk.
- **Apache Iceberg** handles late data and upserts with position and
  equality delete files and a `rewrite_data_files` compaction that folds them
  in; CDC workloads leave dangling delete files behind until maintenance
  [primary: iceberg.apache.org spark-procedures; excerpt: iceberg issue
  12838]. Delta Lake, Materialize and Feldera were not examined [unverified].

### Where the proposal agrees

Chunk-per-closed-period with statistics is what every time-series store ends
up with on disk. Keying a trace by (metric, source, period) is the choice
that makes a Kafka-style key compaction possible later: the latest version
per key is the whole state, which an "append these samples" event design
would not give without a fold.

### Where it diverges

"State chunks replaced per period" is not how any store listed takes its
input; they all take appends and compact. The proposal moves chunk-cutting
to the provider because the changelog has no compaction. That is a defensible
relocation, with two costs the plan does not state:

1. Each correction of a closed hour is a full re-write of tens of kB and
   the superseded line stays in the segment files forever
   (`docs/operations.md`: "No erasure, compaction, or retention policy: the
   changelog keeps everything"). With late-arriving sources (section 1,
   divergence 3) that is not a rare path.
2. Every long-lived append-only store listed grew a scavenge, excision or
   erase path once it held regulated data. Substrate had one (decision 0018,
   `repository reseal`) and lost it when 0050 moved the changelog to segment
   files; nothing rewrites a segment today.

### Recommended change

Record both costs in the decision record for the trace, and decide now
whether a bounded, operator-run segment rewrite (drop the lines of purged
records, leave a `gc` marker, re-checksum) is in scope before any hosted
deployment. The justification for state chunks over append events stands.

## 3. Unit and quantity value types

### What others do

- **FHIR `Quantity`** [primary: hl7.org R5 codesystem quantity-comparator]:
  `value` (decimal), `comparator`, `unit` (display text), `system`, `code`.
  R4 comparators are `<`, `<=`, `>=`, `>`; R5 adds `ad` ("sufficient to
  achieve this total quantity"). Servers search quantities by converting to a
  canonical UCUM unit: HAPI's normalized quantity index, Medplum's
  `double precision` column with a `Normalize` to canonical UCUM form
  [primary: hapifhir.io; medplum.com core.converttosearchablequantities].
- **openEHR `DV_QUANTITY`** [primary: specifications.openehr.org RM data
  types]: `magnitude`, `precision` (decimal places), `units` (UCUM by
  default, else `units_system`), `magnitude_status` (`=`, `<`, `>`, `<=`,
  `>=`, `~`), `accuracy` with `accuracy_is_percent`, `normal_range`,
  `other_reference_ranges`.
- **OMOP `MEASUREMENT`**: `value_as_number` (float), `operator_concept_id`,
  `unit_concept_id`, source strings kept beside; unit harmonization is an
  ETL problem, with a 2025 paper converting 83.4% of records to reference
  units via LOINC-anchored tables [excerpt: OHDSI forums; sciencedirect
  S138650562500348X].
- **Health Connect**: typed classes (`Mass`, `Energy`, `BloodGlucose`,
  `TemperatureDelta`) storing value plus original unit, equality in a base
  unit, glucose by a fixed 1/18 factor (report 04).
- **HealthKit**: `HKUnit` grammar with a molar-mass annotation;
  `moleUnit(withMolarMass:)` "returns a HealthKit unit for measuring mass in
  moles for a given molar mass" with the `HKUnitMolarMassBloodGlucose`
  constant [primary: developer.apple.com]. `preferredUnits(for:)` returns
  the user's display units and `HKUserPreferencesDidChange` fires when they
  change [primary: developer.apple.com].
- **OpenTelemetry**: UCUM codes, `1` for dimensionless, `{request}`
  annotations, non-prefixed units preferred [primary: opentelemetry.io
  general metrics semconv].
- **Pint**: offset units raise on addition; `delta_degC` is a separate unit
  and `degC/meter` parses as `delta_degC/meter`; chemistry conversions
  (mass to moles) are contexts, not units [primary: pint.readthedocs nonmult].
- **Unitful.jl**: relative-scale temperatures share the dimension of
  absolute ones but are a distinct type; some operations throw
  `AffineError`; differences are expressed in K or Ra [primary: Unitful
  temperature.md].
- **postgresql-unit**: a 16-byte datum (double plus eight exponent bytes),
  dimension mismatch is an error, `unit_units.shift` implements affine
  units but is applied only in a "number name" context, `@` converts within
  a dimension; GPLv3 [primary: github.com/df7cb/postgresql-unit].
- **UCUM itself** [primary: ucum.org/ucum sections 21 to 25]: special units
  (`Cel`, `[degF]`) "may be scaled through a prefix or an arbitrary numeric
  factor" but cannot take part in products or exponentiation; arbitrary
  units "are not of any specific dimension and are not commensurable with
  any other unit", and "any term involving arbitrary units is itself an
  arbitrary unit and is not comparable with any other arbitrary unit or
  term".

### Where the proposal agrees

`{value, unit, comparator}` with exact digits, an engine-owned UCUM table,
conversion only within a dimension, comparator only where declared, and
molar mass on the metric are each the majority position (FHIR, openEHR,
HKUnit, ucum-lhc's `molecularWeight`). Keeping lab shapes as sibling
properties rather than widening the value type matches FHIR `value[x]`.
The R4 comparator set is the right one; `ad` and `~` can wait.

### Where it diverges

1. **Affine units are treated like linear ones.** The plan lets ordering
   "sort by the value converted to the dimension's base" and names a
   `skin_temperature_deviation` metric in `Cel`. UCUM forbids `Cel` in any
   product; Pint and Unitful make a delta a different unit; Health Connect
   has `TemperatureDelta`. Converting a 0.3 Cel deviation to `[degF]` by the
   affine formula gives 32.54, which is wrong.
2. **"Arbitrary units convert to nothing" may refuse a valid prefix
   scaling.** `m[IU]/L` and `u[IU]/mL` (TSH) are the same number; whether
   UCUM marks `[iU]` as metric (prefixable) decides it. The fetched table
   summary said no for every arbitrary unit, which conflicts with common lab
   usage; check `ucum-essence.xml` before freezing the rule [unverified].
3. **No canonical column in the index.** Converting the stored value to the
   operand's unit at filter time is a per-row expression and cannot use an
   index; HAPI and Medplum store a canonical base-unit number and search
   that.
4. **No display symbol or alias.** FHIR keeps `unit` (text) beside `code`.
   Vendors send `mmHg`, `bpm`, `mcg`; the engine will refuse them unless
   something maps them to `mm[Hg]`, `/min`, `ug`.
5. **No per-user display preference.** HealthKit's `preferredUnits` is the
   model; the console and agents need one place to read "this user wants
   mmol/L and kg".
6. **openEHR's `precision` and `accuracy` are absent.** Keeping the digits
   covers precision; accuracy is rarely supplied by consumer sources.
   Justified omission.

### Recommended changes

- Declare affine units as non-summable and refuse `sum` over them; make
  deviation metrics declare `K` (scale-only) or add a `delta: true` flag that
  suppresses the offset in conversion.
- Allow prefix scaling within one arbitrary base when UCUM marks it metric.
- Store `value_base float8` (converted to the dimension's base) in the
  derived index and build the typed expression index on it; keep the exact
  digits on the record.
- Admit a small alias table of display symbols on input, store the UCUM
  code only, render from the table.
- Add a `unitpreferences` record (or a `setting` row) read by the console
  and the `query` tool; not an engine conversion rule.

## 4. The aggregation request shape

### What others do

- **Google Health API**: physical `rollUp` (window, origin aligned, 14 or
  90 day caps, page 1,440 default and 10,000 max) and civil `dailyRollUp`
  as separate verbs; typed `{field}_{fn}` results; `reconcile` for sources;
  no percentiles [primary, section 1].
- **HealthKit**: aggregation style on the type (`cumulative`,
  `discreteArithmetic`, `discreteTemporallyWeighted`), anchor date plus
  interval components, `separateBySource`, `mostRecent`, `duration` (report
  05, primary there).
- **Health Connect**: several metrics in one request,
  `aggregateGroupByDuration` and `aggregateGroupByPeriod` (the latter
  requires `LocalDateTime` bounds), sparse buckets ("the list can be sparse,
  so a bucket is not included in the list if it doesn't contain any data"),
  priority dedup for activity and sleep only [primary: developer.android.com
  aggregate-data].
- **Junction Sense** [primary: docs.junction.com sense/*]: one query DSL for
  on-demand queries and continuous queries; `select` of `{func, arg}`
  expressions (`mean`, `newest`, `latest`), `group_by` with `date_trunc
  {value, unit}` on a resource's time index, `value_macro` columns
  (`sleep_score`, `chronotype`, `awakenings`); implicit source
  prioritization per group by source-type order (lab, automatic, watch,
  ring, chest strap, scale, cuff, fingerprick, manual scan, phone, app) and
  provider priority, "the selected provider and source-type pair contributes
  the complete aggregate output for that group. Sense does not combine
  individual fields from different source pairs"; query-level
  `config.provider_priority_overrides`; "Sense does not require a summary
  total to equal an aggregate calculated from timeseries samples"; no
  cross-table queries.
- **Cube** [primary: docs.cube.dev query-format and measures]: `measures`,
  `dimensions`, `timeDimensions {dimension, dateRange, granularity}`, a
  top-level `timezone`; `rolling_window {trailing, leading, offset}` or
  `to_date`.
- **Elasticsearch `date_histogram`** [primary: elastic.co reference]:
  `calendar_interval` (singular units only) versus `fixed_interval`;
  `time_zone` with the documented 23-hour DST day; `offset` to start a day
  at 06:00; `min_doc_count: 0` plus `extended_bounds` to emit empty buckets;
  `hard_bounds`; `missing`; `moving_fn` with `window` and `shift` and
  built-in `stdDev` [primary: elastic.co movfn]; `search.max_buckets`
  65,536 since 7.x with `too_many_buckets_exception` [excerpt].
- **MongoDB `$dateTrunc`**: `unit`, `binSize`, `timezone` (Olson or offset),
  `startOfWeek`; "accounts for Daylight Savings Time" [primary:
  mongodb.com].
- **Druid**: period granularity `{period, timeZone, origin}` [primary:
  druid.apache.org granularities].
- **Prometheus**: `query_range` with `step`; a hard cap of 11,000 points per
  series; `*_over_time` lookback separate from `step`; calendar functions
  UTC only [excerpt: prometheus issue 2253; report 05].
- **Timescale**: `time_bucket(width, ts, timezone, origin, offset)`,
  `time_bucket_gapfill` with `locf` and `interpolate`, `time_weight` (LOCF or
  linear), `lttb`, percentile sketches; continuous aggregates fix one
  timezone at definition [primary: docs.tigerdata.com; timescale issues
  1248, 6902].
- **FHIR `$stats`**: one period per call, statistic codes including median,
  percentiles, quartiles, regression; UCUM only; excludes entered-in-error
  [primary: hl7.org R4 operation stats]. SQL on FHIR flattens to views and
  leaves aggregation to SQL (report 05).
- **M4** (Jugel et al., VLDB 2014): first, last, min, max per pixel column;
  LTTB shipped by Timescale and Uber M3 [excerpt].

### Where the proposal agrees

Style declared on the metric (HealthKit), physical versus civil buckets as
different requests (Google, Health Connect), caller-chosen result unit
(HealthKit), explicit source handling (Junction, HealthKit, Health Connect),
a row budget (Google, Prometheus, Elasticsearch), `fill` (Elasticsearch,
Timescale), rolling over buckets (Elasticsearch `moving_fn`, Cube), aligned
multi-metric series (Health Connect), and refusing `sum` over a discrete
metric are all prior art. Session-scoped windows have no direct precedent
beyond Junction's sleep index and `$stats` with a period; they are a sound
extension. The response carrying per-bucket `start` and `end` with offsets
matches Google's `civil_start_time` and `civil_end_time`.

### Where it diverges

1. **The civil bucket example requires a named zone**, which is home-zone
   bucketing; decision 4 says the local day is the sample's own offset.
   Google, Health Connect and Junction all bucket by the record's own
   offset and have no named zone in the request at all.
2. **No `offset` or `origin` on calendar buckets.** Oura's activity day
   starts at 04:00 and its sleep day at 18:00 [primary: Oura partner
   support]; Elasticsearch, Druid and Timescale all take an offset or
   origin. Weeks have `weekStart`; days have nothing.
3. **Source priority per output bucket undercounts cumulative metrics.**
   The proposal's own lesson (2.9) says cumulative quantities dedupe per
   time interval. Picking "the first source with data per bucket" over a
   day bucket drops the phone's afternoon steps when the watch has the
   morning. Junction resolves per group but never mixes fields; Health
   Connect dedups by interval.
4. **`timeWeighted` and `first`/`last` are promised but unservable from
   rollups** (section 1).
5. **`fill` without bounds.** Elasticsearch needs `extended_bounds` with
   `min_doc_count: 0` to emit buckets over the whole range; the proposal
   should say that `fill` implies dense buckets over `range`.
6. **`maxBuckets: 400` is low.** A per-minute day is 1,440 buckets (Google's
   default page). Prometheus allows 11,000, Google 10,000.
7. **`m4` as a `bucket.method`** mixes point selection into an aggregation
   request. Timescale ships `lttb` as a separate function; Grafana asks for
   `maxDataPoints` and lets the source choose. Keeping it is fine, but it is
   a different result shape (points, not statistics) and should say so.
8. **Continuous queries.** Junction persists a query as a continuously
   re-evaluated result table. The proposal has no stored aggregate beyond
   rollups; a function on a schedule covers it. Justified omission.

### Recommended changes

- Make `bucket.timezone` optional; absent means each sample's own offset,
  present means the named zone; the response states which. Add
  `bucket.offset` (ISO duration) for 04:00 and 18:00 day anchors.
- Resolve `sources.priority` at the finest stored interval (rollup row or
  sample interval) before summing, and document that a bucket's fields come
  from one winner per interval, as Junction does per group.
- Define `fill` as dense buckets over `range`; raise the default
  `maxBuckets` to 1,440 and cap at 10,000.
- Split `m4` into a `downsample` mode returning points.
- Keep session-scoped windows, with a budget on matched records.

## 5. Canonical health data models

### What others do

- **FHIR** is generic: one `Observation` keyed by `code` (LOINC), with
  profiles fixing code and unit per vital sign, `component` for blood
  pressure, `hasMember` and `derivedFrom`, `SampledData` for waveforms, and
  `DiagnosticReport` for the panel (report 04, primary there).
- **OMOP** is generic: one `MEASUREMENT` row keyed by a standard concept,
  source values kept beside (report 03).
- **openEHR** is typed by archetype but stores a lab analyte as a generic
  cluster keyed by a coded name (report 03).
- **IEEE 1752.1** [primary: sagroups.ieee.org elements of schema writing;
  excerpt: omh-shim issue 26] is typed per measure (`heart-rate`,
  `step-count`, `sleep-episode`, `total-sleep-time`, `sleep-stage-summary`,
  `physical-activity`), each built from a generic `unit-value`, an
  `effective_time_frame` (instant or interval), an optional
  `descriptive_statistic` and, for duration summaries, a required
  `descriptive_statistic_denominator`; the header carries acquisition
  provenance with modality `sensed` or `self-reported`. P1752.2
  (cardiovascular, respiratory, metabolic) is in progress [excerpt; status
  unverified].
- **Health Connect** (about 45 typed records) and **HealthKit** (122 quantity
  identifiers) are closed typed sets; neither carries a vendor score type
  (HealthKit has an `appleEffortScore` unit only) (report 04).
- **LOINC Groups** [primary: danielvreeman.com grouping-data-loinc; loinc.org
  returned 403]: 2,178 groups over 6,438 terms in the 2017 release, defined
  as equivalence classes by component, property, system and method, meant
  for flowsheets; the explicit warning is "don't ever throw away information
  when mapping" and that HbA1c protocols (IFCC versus NGSP) differ by about
  30%, so a group is not a licence to merge values.
- **Sleep stages**: AASM clinical scoring is W, N1, N2, N3, R [excerpt];
  Health Connect has eight values including `AWAKE_IN_BED` and
  `OUT_OF_BED`; HealthKit six (report 04). No standard reconciles vendor
  sets.
- **Sport and strength taxonomies**: none shared; Health Connect's
  `ExerciseSessionType`, FIT's sport and sub-sport, HealthKit's activity
  types and FIT's 1,846 exercise names each stand alone (report 02).
- **Vendor scores**: no standard carries them. Junction computes its own
  `sleep_score` macro rather than passing a vendor's through [primary:
  docs.junction.com sleep-analysis].

### Where the proposal agrees

Generic scalars keyed by a `metric` record plus typed session kinds is the
FHIR and OMOP shape for values and the Health Connect and IEEE 1752.1 shape
for sessions. `observation.statistic` with `at`/`endsAt` is IEEE's
`descriptive_statistic` over a `time_interval`. `recordingMethod` is Health
Connect's; `source.kind` is Health Connect's device type and Junction's
source-type order. Vendor-named scores, never mapped, is what every standard
does by carrying none. Keeping `labresult.code` as received beside the
`metric` reference is OMOP's source-value rule.

### Where it diverges

1. **`metric.group` is invented where LOINC Groups exist.** Seed `group`
   from LOINC Group ids where a marker has one and carry the LOINC parts
   (component, property, system, scale, method) on the metric, so a trend
   view can refuse to merge two codes whose property or method differ
   (glucose mass versus molar, HbA1c NGSP versus IFCC) instead of trusting
   the group.
2. **Duration summaries lack a denominator.** IEEE 1752.1 requires one for a
   duration statistic ("7 h per night"). `observation.statistic` plus the
   range covers most cases; a daily `time_asleep` written as `sum` over a
   sleep session versus over a calendar day is ambiguous without it.
3. **No AASM mapping for clinical sleep studies.** Minor; a note in the
   stage mapping table is enough.
4. **Modality**: IEEE separates `sensed` from `self-reported`; the
   proposal's `recordingMethod` (`automatic`, `active`, `manual`) carries
   the same distinction. Justified.

### Recommended change

Adopt LOINC Group ids and LOINC parts on `metric`; add an optional
`denominator` to `observation` for duration statistics; otherwise the layout
is justified.

## 6. Privacy and regulation for a personal health store

### What others do and what the law says

- **GDPR Article 9(1)** prohibits processing "data concerning health" unless
  an Article 9(2) ground applies, most relevantly explicit consent (a) or
  care under a health professional (h) [primary: gdpr-info.eu art-9].
  **Article 17** gives erasure "without undue delay" when consent is
  withdrawn or the data are no longer necessary [primary: gdpr-info.eu
  art-17]. **Article 2(2)(c)** excludes "a natural person in the course of a
  purely personal or household activity", and Recital 18 keeps "controllers
  or processors which provide the means for processing" inside the
  Regulation [excerpt: dataprotection.ie; gdprhub]. A self-hosted
  single-user substrate is the exempt case; a hosted multi-user one is not.
- **EDPB Guidelines 02/2025 on blockchains**, final version adopted
  7 July 2026 [secondary: Bird & Bird; the EDPB page and PDF failed the
  fetcher's certificate check]: do not store personal data, even encrypted
  or hashed, on an immutable ledger; build erasure in at design time; key
  destruction is discussed as a mitigation but CNIL doubts it is full
  erasure [excerpt]. Crypto-shredding is the event-sourcing pattern
  (Verraes 2019), with the caveat that encrypted data remains personal data
  under EU law [excerpt: verraes.net; event-driven.io].
- **ICO** accepts backups being "put beyond use" while a scheduled overwrite
  runs, provided the person is told [excerpt: Osborne Clarke summary of ICO
  deletion guidance].
- **HIPAA** binds covered entities and business associates, not a person's
  own store. Section 164.312(b) audit controls are a required standard and
  are read as logging every access to ePHI including reads, retained six
  years [excerpt: several compliance summaries; primary text not fetched].
  The January 6, 2025 NPRM (Federal Register 2024-30983) proposes mandatory
  encryption at rest and in transit, MFA and removal of the addressable
  category; it is not final [secondary: multiple summaries; hhs.gov and
  federalregister.gov blocked the fetcher].
- **EHDS, Regulation (EU) 2025/327**, in force 26 March 2025: a wellness
  application that claims interoperability with an EHR system must carry a
  label, and data goes into an EHR only on the person's decision (Article
  48(2) per Covington) [secondary: insideprivacy.com EHDS series 4; primary
  EUR-Lex text not read].
- **Datomic, XTDB, EventStoreDB, Kafka** each have a real deletion path
  (section 2).

### What substrate has

Sealed material is AES-GCM under a per-repository DEK; a purge erases the
record's sealed rows and files "in the same transaction", but "the
ciphertext stays in Postgres's dead tuples until VACUUM, in the WAL until
its segment is recycled, on any replica, and in every backup"
[`docs/operations.md`]. Everything else, the changelog and the folded
records, is plaintext, and "the changelog keeps everything". Blob bytes are
plaintext by decision 0031, with disk encryption under the store as the
answer. The owner export is the repository tar (0069), which satisfies
portability. Nothing logs reads.

### Where the proposal agrees

Keeping samples in the changelog rather than in blobs keeps them inside the
one unit the export, backup and verify tools already cover. Not sealing
traces matches 0031's reasoning that the database already holds the
interesting half.

### Where it diverges

1. **The plan adds the most sensitive category of data to a store with no
   erasure path and does not say so.** Section 9 lists seven decisions and
   none is about deletion. A purged `observation` or `trace` keeps its
   plaintext in the segment files forever; a FIT file or lab PDF (name, date
   of birth, address) stays readable under the data root.
2. **A hosted deployment is Article 9 processing by the operator.** Explicit
   consent at registration, erasure on request, and encryption of the data
   root become the operator's obligations; the plan's "retention is a
   per-repository setting for later" covers only the derived index.
3. **Read audit** is absent, which matters only if a hosted deployment ever
   serves clinicians (HIPAA) or claims EHR interoperability (EHDS).

### Recommended changes

- Add an eighth decision: what deletion of health data means. The minimum
  is to state that a purge does not reach the segment files and that the
  hosted profile requires disk encryption and a documented backup retention
  after which purged data is "beyond use" (ICO). The better answer is a
  bounded operator erase that rewrites segments for purged ids and records
  the excision, as Datomic and XTDB do.
- Keep 0031 for blobs, but the health plan should name FIT and PDF uploads
  as directly identifying and make disk encryption a requirement, not a
  suggestion, in the hosted profile.
- Note consent capture and a read-access log as hosted-profile features,
  out of scope for v1 of the package.

## 7. Local day and timezone handling

### What others do

- **Google Health API**: physical UTC plus the offset in force "at the time
  of the event" on every point; `dailyRollUp` "automatically attributes
  data to the calendar day on which it was recorded according to the user's
  local time"; a fall-back day holds 25 hours of data, a spring-forward day
  23, and travel "causes even more significant variations in the physical
  duration of a single civil day" [primary: developers.google.com
  data-types].
- **Health Connect**: `LocalDateTime` is "a user experienced time concept
  such as '2021-01-03 at 10 o'clock', without knowing which time zone the
  user was at that time"; a record "without specifying zoneOffset will
  assume the current system zone offset at query time" [primary:
  TimeRangeFilter.kt KDoc]. Period aggregates bucket each record by its own
  offset [secondary: a third-party PR description; consistent with the
  KDoc].
- **Junction Sense**: "Sense derives floating time from each sample's
  stored time zone offset. If the offset is missing or unusable, Sense uses
  an offset of zero"; "a local-day bucket does not represent a fixed 24-hour
  elapsed interval"; "daylight saving transitions are not normalized"; a
  sleep session belongs to "the local-day bucket containing its session
  end"; Abbott Libre timestamps are floating time whose `+00:00` must be
  ignored [primary: docs.junction.com].
- **Oura**: activity day 04:00 to 04:00 stamped with the first date; sleep
  day 18:00 to 18:00 scored on the longest sleep; calendar day midnight to
  midnight "according to the timezone set by the user's mobile device"
  [primary: Oura partner support].
- **WHOOP**: cycles from sleep onset to sleep onset with a required
  `timezone_offset` per cycle; boundaries may be revised retroactively
  [primary: developer.whoop.com cycle; engineering.prod.whoop.com]; two
  cycles can share one local date [excerpt: open-wearables issue 1554].
- **Garmin**: explicit `calendarDate` "to track when an updated summary
  record should replace an existing summary record without the ambiguity of
  timezones"; local time is `startTimeInSeconds + startTimeOffsetInSeconds`
  [excerpt: Garmin developer portal, mydatahelps]. Sleep is dated to the
  wake day [secondary: sahha.ai].
- **Fitbit**: sleep belongs to the date the log ended; local clock times
  without offsets [secondary: sahha.ai]. **Samsung** infers the previous day
  by clock arithmetic [secondary: sahha.ai].
- **Apple**: `HKMetadataKeyTimeZone` is present on about all watch sleep
  records and watch workouts, never on steps or heart rate; the Health app's
  day is the device's current zone at midnight; a home-zone query over a
  trip returns afternoon steps as morning ones [primary: open-wearables
  discussion 606; openmhealth.org three painful lessons].
- **Terra**: provider offsets preserved with a `timestamp_localization`
  flag; daily records keyed on the calendar date [excerpt: tryterra.co
  community].
- **Clinical**: FHIR `dateTime` requires a zone offset once hours and
  minutes are present; a `tz-code` extension carries the zone name
  [primary: hl7.org datatypes]. openEHR `DV_DATE_TIME` is ISO 8601
  [unverified detail].

### Where the proposal agrees

"The civil date at the sample's own offset" is exactly Google's civil time,
Health Connect's local time and Junction's floating time. Sessions declaring
which end assigns the day matches Junction (end), Fitbit (end), Garmin (wake
day) and Oura's sleep day. Accepting 20-hour and 28-hour travel days and
unnormalized DST days is what Google and Junction document. The 15-minute
rollup divides every offset in the current tz database (the odd ones are
+05:45, +08:45, +12:45 and +13:45); only pre-1972 local-mean-time offsets
break it, which no wearable produces. Confirmed.

### Where it diverges

1. **The API example contradicts the decision** (section 4, divergence 1):
   `bucket: {calendar: day, timezone: Europe/Athens}` is a home-zone day.
   Both are needed; the request must distinguish them and the default should
   be the sample's own offset.
2. **Rollups cannot deliver own-offset days** without an offset or local
   date in the key (section 1, divergence 4). Home-zone days can be summed
   from UTC rollups; own-offset days cannot, because the same UTC quarter
   hour belongs to different civil dates for samples with different offsets.
3. **The fallback for a missing offset is unspecified in the proposal**
   (report 05 says "the request's timezone"). Junction uses zero, which the
   proposal rightly should not copy; Health Connect uses the current zone at
   query time, which falsifies history. Apple steps and heart rate never
   carry a zone, so this is the common case for the largest source. The
   open-wearables thread proposes inferring the zone from the nearest sleep
   record, which does carry one.
4. **A trace with one offset is wrong across a mid-chunk change** (section
   1, divergence 2).
5. **Vendor day anchors (04:00, 18:00) need a bucket offset** (section 4,
   divergence 2).

### Recommended changes

- Add `utc_offset_min` to the rollup key (rows per quarter hour per offset;
  a source changes offset a few times a year, so the row count barely moves).
- Specify the missing-offset rule for canonicalisation functions: the
  source's own offset from the nearest record that has one (sleep, workout)
  within a bounded window, else the source's last-known offset, else the
  user's declared home zone; never zero and never the query-time zone.
- Carry start and end offsets on a trace or split at the change.
- Make a named zone optional and add a bucket offset.

## Ranked changes to the proposal

1. Put `utc_offset_min` in the `sample_rollups` key and add `first`,
   `last` (value and time) and a duration-weighted sum to `stats` and the
   rollups. Without this, decision 4, M4, `last` and `timeWeighted` all
   fall back to full scans, and the "25 ms for any zone" claim holds only
   for home-zone days.
2. Fix the aggregate request's civil bucket: `timezone` optional, absent
   meaning each sample's own offset; add `offset` for 04:00 and 18:00 day
   anchors.
3. Add a deletion decision to section 9: a purge leaves health plaintext in
   segment files and blobs; decide between a documented "beyond use"
   retention and a bounded operator erase, and require disk encryption in
   the hosted profile.
4. Handle affine units and deltas in `quantity`: refuse `sum` over `Cel` and
   `[degF]`, express deviations in `K` or flag them as deltas, and allow
   prefix scaling of metric arbitrary units after checking the UCUM table.
5. Store a base-unit `value_base float8` in the derived index so range
   filters and ordering across units hit an index; accept display-symbol
   aliases on input.
6. Resolve source priority per stored interval, not per output bucket, so
   cumulative metrics from overlapping sources are not undercounted.
7. State the trace rewrite model: rewrites are routine for late sources, a
   monotonic `revision` refuses stale rewrites, and the decision record
   accepts the segment cost.
8. Carry start and end offsets on a trace (or split at the change) and
   specify the missing-offset inference rule for sources without zones.
9. Seed `metric.group` from LOINC Groups and carry LOINC parts; add an
   optional denominator for duration statistics.
10. Raise `maxBuckets` to 1,440 by default with a 10,000 cap; define `fill`
    as dense over the range; split `m4` into a `downsample` mode.
11. Rename the Google fields in section 2.4 to the API's own names and soften
    "nobody stores one row per dense sample".

## Claims in the proposal believed wrong or overstated

| Claim | Where | Evidence |
| --- | --- | --- |
| "rollups at 15 minutes ... make any calendar bucket in any zone a 25 ms question" | 3.1 | True for a named home zone only. Decision 4 defines the day by each sample's own offset, and the rollup key `(repository, metric, source, bucket15)` has no offset, so an own-offset day cannot be a sum of rollup rows. [repo text plus arithmetic] |
| "the changelog holds each period about once" | 5.2 | Health Connect's one-minute-record guidance assumes 15-minute syncs of closed records; Apple Watch and Garmin deliver hours late; WHOOP revises cycle bounds retroactively; Junction documents corrections replacing earlier values. Rewrites are routine, not rare. [primary: developer.android.com write-data; engineering.prod.whoop.com; docs.junction.com] |
| "Arbitrary units ... are their own dimension each and convert to nothing" | 5.1 | If UCUM marks `[iU]` metric, `m[IU]/L` to `[IU]/L` is a prefix scaling labs rely on (TSH). The fetched summary of the UCUM table disagreed; check `ucum-essence.xml` before freezing. [primary: ucum.org sections 24 and 25; table row unverified] |
| "Ordering sorts by the value converted to the dimension's base" and `skin_temperature_deviation` in `Cel` | 5.1, 10 | UCUM forbids special units in products; Pint and Unitful treat a temperature difference as a different unit; Health Connect has `TemperatureDelta`. An affine conversion of a delta is wrong. [primary: ucum.org section 21 to 22; pint nonmult; Unitful temperature.md] |
| "nobody stores one row per dense sample in a general table that lasts" | 2.2 | Health Connect's on-device SQLite keeps one row per sample in `heart_rate_record_series_table`. [secondary: android-health-connect-data-explorer] |
| Google points carry "`physicalTime` and `civilTime`" | 2.4 | The fields are `observation_sample_time`, `observation_time_interval` (with offset) and `civil_time_interval`. Semantics as stated are right; names are not. [primary: Google RPC reference] |
| "`stats` ... so a reader that only needs per-period statistics never unpacks the arrays" | 5.2 | True for count, min, max, mean and SD; false for first, last, percentiles and time-weighted mean, which P1, P2 and P5 promise. [proposal text] |
| Report 05: Junction has "no server-side bucketing found" | 05 section 3.4 | Junction Sense has `group_by` with `date_trunc`, implicit source prioritization, macros and continuous queries. [primary: docs.junction.com sense] |
| "A local day is the civil date at the sample's own offset" alongside the example `bucket: {calendar: day, timezone: Europe/Athens}` | 9 and 5.4 | The two are different semantics (Google `dailyRollUp` versus Elasticsearch `time_zone`); the proposal needs both and must say which the example is. [primary: Google data-types; elastic.co date_histogram] |
| Section 2.4 lists Oura's day as starting at 04:00 only | 2.4 | Oura's sleep day runs 18:00 to 18:00 and its calendar day midnight to midnight by the phone's zone; three day types, not one. [primary: Oura partner support] |
| "Strava's 2026 API Policy caps retention at 7 days ..." | 8 | Not checked in this review; outside the seven topics. [unverified] |

## Things this review could not verify

- The EDPB final guideline text (certificate failure on edpb.europa.eu);
  positions are from Bird & Bird's summary.
- The HHS NPRM text (hhs.gov and federalregister.gov blocked the fetcher).
- The UCUM table's metric flag on `[iU]`.
- Health Connect's bucketing by each record's own offset in
  `aggregateGroupByPeriod` beyond the KDoc's "user experienced time".
- WHOOP's raw data figures (unsourced secondary).
- Strava's stream storage (search excerpt of a blocked page).
- Epic, Oura, Garmin, Terra internal storage: no public sources found.
