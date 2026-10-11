# Health data proposal: correct aggregation before scaling ingestion

The proposal's chunked records and derived Postgres tables are a reasonable fit for substrate. Its proposed aggregation rules leave gaps that can produce wrong totals, dates, and converted lab values. Resolve those contracts before importing personal health data at scale.

Reviewed on 2026-10-08. I read `docs/plans/health-data.md`, including sections 1, 4, 5, 6, 7, and 11, then root `CLAUDE.md`, relevant sections of the six supporting reports, and the benchmark SQL. This review concentrates on gaps in those reports. The repository remained read-only; I ran no database suites, benchmark queries, or servers.

Public APIs establish exchange formats, not physical storage layouts. Below, implementation evidence is identified separately from API behavior. Where a vendor does not publish its storage design, the answer is unknown. Recommendations and counterexamples are my analysis, rather than claims that another system has implemented substrate's proposed design.

## 1. Dense observations, physical storage, and corrections

### What other systems actually expose

| System | Public evidence | Implication for substrate |
| --- | --- | --- |
| Apple HealthKit | Apple's quantity series builder accepts timed quantities and finishes a series into samples. Apple does not publish a supported SQLite schema. Backup database readers provide reverse engineering evidence of SQLite, not a contract for all current high-frequency storage. Revisions can replace an object using a stable sync identifier and a higher sync version. [Series builder](https://developer.apple.com/documentation/healthkit/hkquantityseriessamplebuilder), [sync version](https://developer.apple.com/documentation/healthkit/hkmetadatakeysyncversion), [backup reader source](https://github.com/christophhagen/HealthDB). | Series are useful API objects. They do not prove that Apple stores every series as one array-valued database row, or that completed periods never change. |
| Google Health Connect | AOSP's `HeartRateRecordHelper` defines a parent record table and a separate series table containing `beats_per_minute` and `epoch_millis`. `SeriesRecordHelper` links child rows to the parent. The public write guidance recommends minute-sized heart-rate records and provides client identifiers and versions for upserts. [Heart-rate storage source](https://android.googlesource.com/platform/packages/modules/HealthFitness/+/refs/heads/main/service/java/com/android/server/healthconnect/storage/datatypehelpers/HeartRateRecordHelper.java), [series storage source](https://android.googlesource.com/platform/packages/modules/HealthFitness/+/refs/heads/main/service/java/com/android/server/healthconnect/storage/datatypehelpers/SeriesRecordHelper.java), [write guidance](https://developer.android.com/health-and-fitness/health-connect/write-data). | This is a direct example of array-shaped API records backed by individual sample rows in SQLite. Parent chunks and narrow sample rows are compatible designs. |
| Garmin Connect | The Health API documents JSON summaries, delivery mechanisms, and historical backfill. FIT describes an interchange stream. Neither establishes Connect's internal tables, partitions, compression, correction algorithm, or rollup widths. [Health API](https://developer.garmin.com/gc-developer-program/health-api/), [FIT SDK](https://developer.garmin.com/fit/overview/). | Do not infer storage from daily JSON payloads or FIT messages. Preserve provider revisions and original files. |
| Oura | Public endpoints distinguish daily records, sessions, and sampled measurements. They do not disclose the backend's physical chunk widths, indexes, or late-data implementation. [API documentation](https://cloud.ouraring.com/v2/docs). | Oura supports the need for regular series and nullable samples at the interchange layer. Its API does not establish a universal storage recommendation. |
| Strava | Activity streams have activity identity, stream type, resolution, and original size. Strava's 2019 post about richer activity storage explicitly excludes streams from that discussion. [API reference](https://developers.strava.com/docs/reference/), [engineering post](https://medium.com/strava-engineering/a-richer-activity-part-1-58b6ce4bde6a). | Session-scoped streams are established. That post cannot be cited as the physical design of Strava's dense streams. |
| WHOOP | A September 2026 engineering post describes Postgres change capture through Debezium and Flink, append-only bronze data, and upserted Iceberg silver tables. Another storage account describes S3 and a separate hot layer. These describe analytics infrastructure, not a complete sensor ingestion schema. [CDC architecture](https://engineering.prod.whoop.com/cdc-at-whoop/), [WHOOP data lake engineering articles](https://engineering.prod.whoop.com/de/tags/data-lake/). | An immutable raw layer plus derived current state is a close architectural precedent. Exact sensor chunk duration and indexing remain undisclosed. |
| Terra | Its public model nests measurements inside activity, sleep, body, and daily payloads. Physical storage and rollup implementation are not documented there. [Data models](https://docs.tryterra.co/reference/health-and-fitness-api/data-models). | Useful normalization evidence, not evidence that a durable row-per-point index fails. |
| Junction | Sense documents replacement of summaries, independent summary and time-series products, and provider/source-type selection at output-group boundaries. It does not publish a complete storage schema. [Time and data semantics](https://docs.junction.com/sense/query-dsl/time-and-data-semantics), [prioritization](https://docs.junction.com/sense/data-prioritization). | Copy its explicit semantic distinctions. Do not silently call bucket-level source selection sample deduplication. |
| Open Wearables | Current source has one `DataPointSeries` row per source, metric, and timestamp, with numeric value, offset, and `is_daily_total`. Its archive model adds bucket start, aggregation type, value, and sample count. [Point model](https://github.com/the-momentum/open-wearables/blob/main/backend/app/models/data_point_series.py), [archive model](https://github.com/the-momentum/open-wearables/blob/main/backend/app/models/data_point_series_archive.py). | Narrow sample rows and separate aggregates are a maintained implementation, not merely an abandoned row-per-record experiment. The daily-total discriminator is relevant to the proposed index. |
| Metriport | Current source writes consolidated patient FHIR bundles as JSON snapshots in S3, with request and resource-type organization. That is clinical consolidation, not evidence of a wearable sample TSDB. [Snapshot implementation](https://github.com/metriport/metriport/blob/develop/packages/core/src/command/consolidated/snapshot-on-s3.ts). | Keep clinical document storage distinct from dense sensor storage. Public source does not answer every question about its historical wearable pipeline. |
| Epic | Public integration documentation separates discrete clinical observations and flowsheets from waveform streaming through Epic Monitor. It does not disclose a physical waveform schema or a universal rollup policy. [Clinical interfaces](https://open.epic.com/clinical/HL7v2). | A clinical observation model does not, by itself, specify storage for continuous waveforms. |
| EHRbase | PostgreSQL migrations separate version metadata, historical versions, and composition data, with structural indexes. Current and historical clinical content are explicitly versioned. [Version tables](https://github.com/ehrbase/ehrbase/blob/develop/jooq-pg/src/main/resources/db/migration/ehr/V6_1__version_tables.sql), [migration source](https://github.com/ehrbase/ehrbase/tree/develop/jooq-pg/src/main/resources/db/migration/ehr). | Versioned clinical objects and query projections fit substrate's model. These schemas do not establish an optimized dense-waveform store. |
| HAPI FHIR | The JPA schema separates logical resources, resource versions containing serialized content, and typed search parameter tables. Date indexes have low/high bounds. Quantity indexes can include normalized values. [Schema documentation](https://hapifhir.io/hapi-fhir/docs/server_jpa/schema.html). | Retaining complete source objects while indexing selected typed fields is normal. Storing `SampledData` does not automatically index every sample for time-series queries. |
| Medplum | Its documented search architecture uses a table per resource type, JSON content, typed columns, and lookup tables. Search indexes update with resource writes. Separate analytics pipelines export history into analytical storage. [Search architecture source](https://github.com/medplum/medplum/blob/main/packages/docs/docs/contributing/search-architecture.md), [analytics architecture](https://www.medplum.com/docs/analytics/redshift). | Transactional clinical reads and analytical projections can coexist without replacing the clinical resource model. |
| Google Cloud Healthcare API | FHIR changes can stream to BigQuery. Public documentation specifies the service and export behavior, not its private primary-storage layout. [BigQuery streaming](https://docs.cloud.google.com/healthcare-api/docs/how-tos/fhir-bigquery-streaming). | A managed FHIR service plus an analytics projection is a precedent. Its internal chunking cannot be claimed from that interface. |
| Microsoft FHIR server | Public SQL schemas store serialized resource bytes with resource identity and version, plus separate date search indexes containing start/end bounds. [Resource table](https://github.com/microsoft/fhir-server/blob/main/src/Microsoft.Health.Fhir.SqlServer/Features/Schema/Sql/Tables/Resource.sql), [date search table](https://github.com/microsoft/fhir-server/blob/main/src/Microsoft.Health.Fhir.SqlServer/Features/Schema/Sql/Tables/DateTimeSearchParam.sql). | Versioned resource storage plus specialized indexes is established. It does not imply that a FHIR resource per ECG sample is sensible. |

MongoDB supplies a useful comparison the proposal underuses: its point-oriented time-series API packs measurements into internal buckets according to metadata and time, subject to bucket limits. API granularity and storage granularity are separate decisions. [Bucketing documentation](https://www.mongodb.com/docs/v8.0/core/timeseries/timeseries-bucketing/), [implementation notes](https://github.com/mongodb/mongo/blob/master/src/mongo/db/timeseries/README.md).

### Where the proposal agrees

Keep a trace as a bounded transport and correction unit, plus a derived narrow index. This avoids substrate's expensive full record envelope per sample while preserving the existing changelog and fold. It is consistent with the separation visible in Health Connect and clinical resource servers. Stock Postgres is a reasonable first implementation; the evidence does not require a second authoritative TSDB.

Keep source identity and separate provider summaries. Preserve exact source content independently of the floating-point analytics projection. Keep percentiles off simple additive rollups unless a declared approximation is available.

### Changes required

1. **Project measurement semantics, not only `ts` and `value`.** The proposed row loses interval end, raw-versus-summary status, summary function, quality, and the weight needed for temporal averages. `412 steps over [07:00,07:15)` is not a point counter or a rate. A daily resting-heart-rate statistic is not interchangeable with raw heart rate. Add these fields to the projection or join them through an indexed immutable measurement descriptor. Give each metric explicit point, interval-total, rate, state, and summary semantics.
2. **Define overlap and identity.** Two records at the same timestamp may be duplicates, different components, or different samples. Preserve upstream identity and sample ordinal where necessary. A uniqueness constraint on `(metric, source, ts)` alone is not universally valid. Observation and trace projections must retain enough identity to avoid counting a mirrored measurement twice.
3. **Make chunk identity independent of pagination.** A sync page can change as late records appear. Use stable source/metric/time boundaries, or a stable upstream session identifier with deterministic subdivisions. A partial page must not overwrite a complete period. Distinguish complete replacement from patch ingestion.
4. **Specify late corrections.** Record upstream revision or modification time, fetch completeness, and ingestion time. Reject stale replacement when upstream ordering is available. On a move, deletion, or changed interval, invalidate both old and new buckets. Coalesce invalidations for a transaction. Time-weighted calculations can also require invalidating adjacent boundary segments.
5. **Treat rollups as eligible query plans.** A 15-minute aggregate can answer a query only when its boundaries, filters, source policy, units, and weighting match. Read raw edges for partial buckets. Recorded-offset grouping cannot always use a rollup that discarded the offset. Arbitrary quality or value predicates cannot be applied after their distinctions have been aggregated away.
6. **Version semantic dependencies.** A trace whose interpretation changes when `metric.unit`, `aggregation`, a conversion formula, or a stage mapping changes is not reproducible from its values alone. Pin the interpretation version or record normalized values with transformation provenance. Metric edits need an explicit dependent-index rebuild rule.

### Waveforms need an additional contract

FHIR `SampledData` provides an interchange representation for sampled signals, including dimensions and scaling. WFDB goes further: signal headers identify channels, sampling frequency, gain, baseline, units, and segment structure. A generic scalar metric trace omits important acquisition metadata. [FHIR datatypes](https://hl7.org/fhir/R4/datatypes.html), [WFDB header specification](https://physionet.org/physiotools/wag/header-5.htm).

Add a shared acquisition/session reference, channel identity, calibration, quality flags, and exact sample-period representation. At 512 Hz, the interval is 1.953125 ms; integer-millisecond irregular offsets cannot preserve that clock. A null value also needs a reason where the source distinguishes missing, clipped, invalid, and non-wear.

Inline arrays remain reasonable for bounded strips. Long multichannel waveforms deserve a separate size study. Content-addressed immutable payloads referenced by changelog records can preserve a single logical truth, but only if repository export, replay, integrity checks, and garbage collection retain every historically required payload. The current live-reference blob lifecycle must not be assumed sufficient. This would require an explicit repository-format decision, not an importer bypass.

## 2. Append-only systems do not share one chunk strategy

### What others do

| System | Mechanism and limit |
| --- | --- |
| Kafka | Log compaction eventually keeps the latest value for each key. It is different from time retention and does not preserve every historical value. Unique sample keys remain unique retained values; compaction helps repeated state keys. [Design documentation](https://kafka.apache.org/21/design/design/). |
| Prometheus | Samples enter a write-ahead log and compressed time blocks. Compaction combines blocks; deletion uses tombstones and later cleanup. It does not require replacing an entire application-level hour on every new sample. [Storage documentation](https://prometheus.io/docs/prometheus/latest/storage/). |
| EventStoreDB / Kurrent | Retention settings and scavenging can remove eligible old events. Snapshots improve replay but do not by themselves remove retained events. This is an engine capability, not evidence that all sensor customers use one schema. [Scavenging documentation](https://docs.kurrent.io/server/v26.1/operations/scavenge). |
| Datomic | Transaction logs group many datoms into log segments. `noHistory` changes historical retention behavior; excision is a separate operation for removing data. These are explicit choices beyond an ordinary logical retraction. [Log](https://docs.datomic.com/reference/log.html), [schema reference](https://docs.datomic.com/schema/schema-reference.html), [excision](https://docs.datomic.com/operation/excision.html). |
| XTDB | Its bitemporal storage design organizes immutable data into columnar files and compacts them through an LSM structure. Physical reorganization and semantic history retention are distinct. [Storage design](https://xtdb.com/blog/building-a-bitemp-index-3-storage.html). |
| Dolt | Persistent trees share unchanged structure across versions; its JSON storage work extends sharing inside changed JSON documents. Keeping history need not mean copying every unchanged byte of a logical object. [Storage engine](https://www.dolthub.com/blog/2024-02-29-storage-engine/), [JSON trees](https://www.dolthub.com/blog/2024-07-15-json-prolly-trees/). |
| Materialize | Incremental computation represents changes, including retractions, and temporal filters bound maintained state. Source-log retention remains a separate concern. [Temporal filter patterns](https://materialize.com/docs/transform-data/patterns/temporal-filters/). |
| Feldera | Time-series processing uses declared lateness to advance waterlines and bound state. This makes the accepted lateness contract explicit. [Time-series tutorial](https://docs.feldera.com/tutorials/time-series/). |
| RisingWave | Watermarks determine event-time progress and when state can be retired. They are a declared processing rule, not proof that an elapsed hour will receive no more source corrections. [Watermark explanation](https://risingwave.com/blog/watermarks-stream-processing-explained/). |
| Pinot | Upserts select the current row using keys and comparison columns, including timestamps. This exposes the ordering rule for mutable source data. [Upsert documentation](https://docs.pinot.apache.org/build-with-pinot/ingestion/upsert-dedup/upsert). |

### Assessment and recommendation

The standard answer is not universally "replace closed state chunks." Systems combine batched append events, compression, shared storage, compaction, retention, current-state projections, and late-data rules. These mechanisms save different things. Compaction that drops history is incompatible with unlimited historical replay unless substrate explicitly changes that promise.

State chunks are nevertheless justified for substrate because its existing mutation unit carries substantial metadata. Keep that choice, but document it as a tradeoff: fewer record envelopes and simpler replay in exchange for correction amplification and delayed availability.

Define three independent concepts:

- **Publication boundary:** when a provider first emits a bounded chunk.
- **Revision policy:** how later inserts, deletions, and corrections replace that chunk.
- **Retention policy:** which historical payloads remain recoverable, and for how long.

Elapsed wall time closes a publication window, not the possibility of correction. Health Connect's versioned upserts and HealthKit's sync-version replacement both support this distinction, as cited above.

Measure bytes per retained sample and bytes rewritten per correction. Include mirrors and canonical records, envelope bytes, provenance, repeated statistics, compression, and actual revision rates. Fewer changelog lines mainly reduce per-record overhead; they do not prove a particular annual byte total.

Do not add a new event subsystem immediately. If measurements show excessive replacement costs, compare smaller stable chunks, segment compression, and append deltas with periodic snapshots. Any such change must preserve deterministic replay and specify which old history, if any, becomes unavailable.

## 3. Quantity types need semantic compatibility as well as dimensions

### What the comparison systems provide

| System | Relevant design choice |
| --- | --- |
| FHIR Quantity | Separates decimal value, comparator, display unit, and machine-readable unit system/code. Decimal precision is meaningful; the display string is not the conversion key. [Datatype definition](https://hl7.org/fhir/R4/datatypes.html). |
| openEHR `DV_QUANTITY` | Carries magnitude and units within a richer ordered-value model with precision, accuracy, magnitude status, and reference-range concepts. Its numeric representation is not a blanket guarantee of exact decimal arithmetic. [Data types specification](https://specifications.openehr.org/releases/RM/latest/data_types.html). |
| OMOP | Separates measurement concept, numeric or coded value, operator, standardized unit concept, and source fields. It is an analytical model, not a general unit arithmetic engine. [Measurement table](https://ohdsi.github.io/CommonDataModel/cdm54.html). |
| Health Connect | Exposes distinct typed units, including `Temperature` and `TemperatureDelta`, rather than treating every temperature number alike. [Unit classes](https://developer.android.com/reference/android/health/connect/datatypes/units/package-summary). |
| HealthKit | `HKUnit` provides compatible-unit arithmetic and supports molar units with specified molar mass. That is a context-bearing conversion, not a prohibition on all mass/molar conversion. [HKUnit](https://developer.apple.com/documentation/healthkit/hkunit), [molar unit constructor](https://developer.apple.com/documentation/healthkit/hkunit/moleunit%28with%3Amolarmass%3A%29). |
| OpenTelemetry | Metric unit metadata follows UCUM conventions. Metadata conventions do not themselves supply dimension-checked database predicates or exact arithmetic. [Metric conventions](https://opentelemetry.io/docs/specs/semconv/general/metrics/). |
| Pint | Distinguishes absolute affine quantities from differences; contextual conversions can require extra parameters. [Nonmultiplicative units](https://pint.readthedocs.io/en/stable/user/nonmult.html), [contexts](https://pint.readthedocs.io/en/stable/user/contexts.html). |
| Unitful.jl | Represents units and dimensions in types, supports affine units, and permits rational conversion factors. Numeric exactness also depends on the chosen number type. [Unit definitions](https://juliaphysics.github.io/Unitful.jl/stable/newunits/), [types](https://juliaphysics.github.io/Unitful.jl/stable/types/). |
| `postgresql-unit` | An existing PostgreSQL extension stores a floating-point magnitude and dimensional exponents, with operators and conversions. Its README states limits around nonlinear units. It is not an exact clinical decimal type. [Source and documentation](https://github.com/df7cb/postgresql-unit). |
| Wolfram | Distinguishes temperature units from temperature-difference units. [Temperature units](https://reference.wolfram.com/language/tutorial/TemperatureUnits.html). |
| Frink | Offers dimensional calculation with exact integers and rationals alongside other numeric representations; absolute temperature conversion needs special treatment. [Language documentation](https://frinklang.org/). |

### What to keep

Keep `{value, unit, comparator}` as a compact core quantity representation. A closed, engine-owned UCUM subset is defensible for predictable validation and identical server/client behavior. State that it is a supported subset, not a complete UCUM evaluator. Generate Python and console tables from the same source and test conversion parity.

Keeping decimal digits in authoritative records and `float8` in a declared sensor analytics projection is also defensible. Do not extend the floating-point argument to money or exact lab predicates merely because they share the aggregate route.

### What it misses

**Dimension equality does not establish clinical equivalence.** NGSP HbA1c percent and IFCC mmol/mol are dimensionless ratios but use a method-specific relationship. The published master equation is `NGSP = 0.09148 * IFCC + 2.152`. A naive ratio conversion makes 6% equal 60 mmol/mol; the clinical conversion gives about 42 mmol/mol. [NGSP's IFCC guidance](https://ngsp.org/ifccngsp.asp).

Add a quantity or metric compatibility class above physical dimension: analyte, measurement property, method/standardization, specimen when relevant, and permitted conversion family. Apply it in filters and aggregation. Cross-dimension rejection alone misses this example.

**`kU/L` is misclassified.** UCUM `U` denotes enzyme activity, with a physical definition based on amount per time. It is different from bracketed international or arbitrary units. Arbitrary units also need assay identity; treating all instances of the same arbitrary-unit symbol as mutually comparable is unsafe. [UCUM specification](https://ucum.org/ucum).

**Affine differences need their own semantics.** A temperature deviation of 1 degree Celsius is a difference of 1.8 degrees Fahrenheit. Applying the absolute-temperature offset would produce the wrong answer. Declare absolute versus delta quantities. Standard deviation is a delta; variance has squared units. Count and duration results cannot inherit the input's unit.

**An exact rational conversion need not have finite decimal output.** Converting 1 degree Fahrenheit to Celsius gives `-155/9`. Define rational intermediates or bounded decimal arithmetic, output precision, and rounding. Preserve source scale where clinically meaningful. Return a conversion/rounding indication when a rendered decimal is approximate.

**Affine aggregation and numeric stability require rules.** A converted sum has an offset contribution for every value, unlike a converted mean. Many affine sums should be refused. Replace naive `sum_squares - sum^2/n` variance with a numerically stable mergeable state such as count, mean, and M2. Sensor error does not prevent cancellation in a variance calculation.

**Comparators describe sets of possible values.** `<5` guarantees `lt:5`; it only possibly satisfies `lt:3`. Specify whether predicates mean guaranteed truth, possible truth, or literal reported-limit comparison. Keep bounded values out of ordinary means, percentiles, and time-in-range calculations unless a censoring policy is explicitly chosen. Return exact-value count separately from bounded-result count.

**Unknown units need a preservation path.** A closed table should refuse unsupported arithmetic without discarding an otherwise valid clinical result. Preserve original unit text, code system, code, numeric lexeme, and reason normalization failed. A separate raw-result representation can remain queryable by identity and date.

**Molar conversion is a contextual conversion.** Keeping it out of generic dimension arithmetic is justified. A derived observation should identify the source measurement, formula version, substance basis, and conversion parameters. Molar mass alone does not describe every assay conversion. Mark equivalent representations so an aggregate cannot count both the original and derived observation as independent measurements.

**Three aggregation styles are insufficient for every planned metric.** HealthKit distinguishes arithmetic, temporally weighted, and equivalent-continuous-level discrete quantities, in addition to cumulative quantities. Sound level illustrates why an arithmetic mean of logarithmic readings can be wrong. Add declared reducer semantics for such metrics or exclude them from generic `avg`. [HealthKit aggregation styles](https://developer.apple.com/documentation/healthkit/hkquantityaggregationstyle).

## 4. A bounded JSON aggregation mode is sound, with a stricter contract

### Useful precedents

| API | Convention worth adopting |
| --- | --- |
| Google Health `rollUp` / `dailyRollUp` | Distinct physical and civil time concepts, bounded requests, half-open time ranges, explicit pagination, and documented source reconciliation. Current `rollUp` documentation specifies page limits and data-type-dependent range limits. [Aggregation guide](https://developers.google.com/health/aggregate-data), [rollUp reference](https://developers.google.com/health/reference/rest/v4/users.dataTypes.dataPoints/rollUp). |
| HealthKit statistics | Legal statistics depend on quantity aggregation style. Preserve that semantic check rather than exposing every reducer for every metric. [Aggregation styles](https://developer.apple.com/documentation/healthkit/hkquantityaggregationstyle). |
| Health Connect aggregate | Separates duration and calendar-period grouping. Activity and sleep deduplication depends on user source priority; other data types do not automatically get that policy. Missing buckets are not necessarily zero-valued buckets. [Aggregate guide](https://developer.android.com/health-and-fitness/health-connect/aggregate-data). |
| Junction Sense | Makes source selection and summary/time-series distinctions explicit. Its bucket-level priority is a named behavior, not a universal definition of deduplication. [Prioritization](https://docs.junction.com/sense/data-prioritization). |
| Cube | Separates measures, dimensions, filters, time dimensions, timezone, ordering, and pagination. Filter placement matters when distinguishing row filters from aggregate filters. [Query format](https://docs.cube.dev/reference/core-data-apis/rest-api/query-format). |
| Elasticsearch | Separates fixed intervals from calendar intervals, with explicit timezone, offset, empty-bucket, and bounds behavior. [Date histogram](https://www.elastic.co/docs/reference/aggregations/search-aggregations-bucket-datehistogram-aggregation). |
| MongoDB | Pipeline stages make the order of filtering, grouping, and later transformations explicit. [Group stage](https://www.mongodb.com/docs/manual/reference/operator/aggregation/group/). |
| Druid / Pinot | Druid's native JSON separates intervals, filters, granularity, aggregations, and post-aggregations. Pinot's query interface is primarily SQL; its ingestion upsert semantics are separate from query grouping. [Druid timeseries query](https://druid.apache.org/docs/latest/querying/timeseriesquery/), [Pinot query API options](https://docs.pinot.apache.org/release-1.2.0/for-users/user-guide-query/query-options). |
| Prometheus / Grafana | Prometheus range queries specify start, end, step, and timeout and can report warnings. Grafana's maximum data points is a visualization control. Neither makes chart resolution an analytical equivalence guarantee. [Prometheus API](https://prometheus.io/docs/prometheus/latest/querying/api/), [Grafana query options](https://grafana.com/docs/grafana/latest/visualizations/panels-visualizations/query-transform-data/). |
| FHIR `$stats` | Defines a bounded observation-statistics operation, including valid-result handling and unit concerns. Its trial-use definition is not evidence of universal FHIR-server support. [R4 operation](https://hl7.org/fhir/R4/observation-operation-stats.html). |
| SQL on FHIR | `ViewDefinition` maps FHIR content into tabular views. It is useful projection precedent, not a complete charting or streaming aggregation DSL. [Implementation guide](https://build.fhir.org/ig/FHIR/sql-on-fhir-v2/). |
| Timescale continuous aggregates | Materialization uses refresh and invalidation rules. Old corrections outside a refresh policy's lookback require explicit handling; raw-data retention must coordinate with refresh windows. [Refresh behavior](https://www.tigerdata.com/blog/continuous-aggregate-refresh-demystified), [retention and aggregates](https://www.tigerdata.com/docs/learn/data-lifecycle/data-retention/data-retention-with-continuous-aggregates). |

### Keep the route, clarify the selectors and stages

`GET /api/v1/records?filter=...&aggregate=...` fits the one-read-route constraint. A bounded typed expression is preferable here to arbitrary SQL or user code in the engine. Keep physical buckets, calendar buckets, explicit session windows, and aligned metric series.

Replace the ambiguous `"value":"heart_rate"` with a typed selector identifying either a full metric record reference or a declared numeric property. Separate parent-record filters, sample predicates, and aggregate-result predicates. A filter that selects traces with a certain property must not silently pretend to filter their individual values.

Specify this evaluation order: select records and measurements, apply validity rules, resolve overlap/source policy, clip to windows, aggregate, align/fill buckets, calculate rolling results, then downsample for display. Return the resolved policy with the result. If a different order is offered, name it explicitly.

### Source priority currently changes the answer with bucket size

Suppose a watch records 1,000 steps in the morning and a phone records 2,000 steps in the evening. The proposed "first source with data per bucket" policy returns only 1,000 for the day when the watch has priority. Hourly buckets can retain both sources' nonoverlapping contributions and total 3,000.

This is a property of the proposed rule, not an implementation bug. Offer two separate policies:

- Select one provider's authoritative summary for a reporting period.
- Reconcile overlapping raw intervals under a source priority independent of requested output resolution.

The second needs coverage and interval semantics. Do not claim it reproduces a vendor's private daily total. Return selected sources and covered duration so users can inspect the result.

### Simplify and harden the remaining surface

1. Use structured reducers such as `{"percentile":0.05,"method":"exact"}` instead of an expanding set of strings like `p05`. Declare approximation and error semantics when sketches arrive.
2. Define time-weighting, interpolation, clipping, and maximum gap. `durationIn` needs these rules before it can operate on irregular glucose samples. A last value must not imply hours of known coverage after a device disconnects.
3. Give each result its own unit. `avg`, `stddev`, `count`, and `durationIn` cannot all share one response unit.
4. Define rolling input and weights. A seven-day mean of daily means differs from a pooled mean of samples. State whether seven means calendar buckets or observed buckets, whether empty buckets consume positions, and whether lookback extends before the requested output range.
5. Keep missing distinct from zero. Fill values need an explicit synthetic flag or coverage metadata. Do not fill before calculating clinical statistics unless that is the requested policy.
6. Make session overlap explicit. One sample can legitimately fall in two requested windows. State whether output rows are independent and non-additive. Preserve the full session reference, effective boundaries, and clipping information.
7. Budget total output cells or rows across groups and metrics, scanned samples, execution time, and response bytes. `maxBuckets:400` alone does not bound a high-cardinality `groupBy` or an exact percentile over years.
8. Reject over-budget analytical requests by default, or paginate against a consistent snapshot. A bare `truncated` flag can hide incomplete totals. Pagination must preserve rolling lookback and group ordering. Never silently change analytical resolution to meet a chart budget.
9. Return a changelog sequence or other consistent read point, and identify projection freshness. Synchronous folds can make the answer current at that sequence; any later asynchronous materialization needs its own freshness contract.
10. Prefer an initial implementation of ordinary buckets, coverage, and typed reducers. Add rolling, session windows, and rendering modes after their boundary rules have executable examples. The existing Python function layer can initially compute rolling analyses from bounded aligned results.

### M4 belongs to rendering

M4 retains the first, last, minimum, and maximum samples for each horizontal display bin, including their original timestamps. That can mean up to four points per pixel column. Those points preserve visual extrema; they are not an evenly spaced analytical series. [M4 paper](https://www.vldb.org/pvldb/vol7/p797-jugel.pdf).

Rename the request budget to pixel columns or define whether `points` means bins or returned samples. Return extrema timestamps and stable ordering, deduplicating identical selected samples. Keep M4 output out of rolling means, correlations, and aligned analytical joins unless the caller explicitly chooses a lossy rendering product.

## 5. Generic observations need typed profiles, not hundreds of kinds

### What the standards imply

IEEE 1752.1 covers mobile health representations including activity and sleep; it is not a complete laboratory or waveform ontology. Open mHealth's schema work illustrates versioned measurement schemas and metadata. [IEEE standard scope](https://standards.ieee.org/ieee/1752.1/6982/), [Open mHealth schemas](https://github.com/openmhealth/schemas).

FHIR uses generic observations with profiles that constrain codes, components, units, and required context. Blood pressure is an observation with systolic and diastolic components. mCODE specializes cancer-related observations within that approach. [Vital-sign profiles](https://fhir.hl7.org/fhir/observation-vitalsigns.html), [mCODE tumor-marker profile](https://www.hl7.org/fhir/us/mcode/2023May/StructureDefinition-mcode-tumor-marker-test.html). The linked mCODE page is a published ballot-version example, not a claim about the latest release.

OMOP similarly standardizes measurement concepts without creating a database table for every analyte. openEHR uses archetypes and templates to constrain a shared information model. Health Connect instead exposes a finite catalog of typed records. These are different validation strategies; none requires substrate to create one kind per LOINC code. [OMOP model](https://ohdsi.github.io/CommonDataModel/cdm54.html), [openEHR data types](https://specifications.openehr.org/releases/RM/latest/data_types.html), [Health Connect record APIs](https://developer.android.com/reference/android/health/connect/datatypes/package-summary).

LOINC Groups are curated groupings for particular use cases, with explicit cautions about validation. A group is not automatically a replacement measurement code. LOINC properties also distinguish measurement forms such as mass and substance concentration. [LOINC Groups](https://loinc.org/kb/users-guide/additional-content-in-the-loinc-distribution/loinc-groups), [LOINC property axis](https://loinc.org/kb/users-guide/major-parts-of-a-loinc-term/property).

### Assessment and changes

Keep generic `metric`, `observation`, and `trace`, plus typed sessions and lab results. Add machine-checkable metric profiles: allowed value form, dimension and semantic compatibility, temporal meaning, component structure, reducers, required specimen/method context, and valid reference-range form. A reference to a metric must trigger validation; a descriptive metric row alone is insufficient.

An analyte above LOINC is useful for navigation and discovery. It must not erase specimen, property, timing, scale, or method distinctions. Return grouped measurements with their original coded identity. Only combine them into a trend after an explicit compatibility rule, including any required conversion. Preserve code system, version, code, and display text separately.

For `labresult`, enforce exactly one result form after merge, including on updates. Switching from a quantity to a coded result must clear the old value through an explicit operation. Reference intervals, specimen, method, status, and interpretation belong to the measurement context. Do not place them only on the analyte or assume one universal range per metric.

Support multi-component observations without splitting away their shared acquisition context. Blood pressure needs a common measurement identity even if systolic and diastolic receive independent aggregate selectors. Link derived measurements to exact source revisions and transformation versions, not only current record references.

### Scores, sleep, sports, and strength

Vendor scores need vendor and algorithm identity, scale bounds, direction, and version when available. A generic 0-to-100 value does not make Oura readiness comparable to WHOOP recovery. Keep them in distinct metric identities and preserve raw vendor fields when the score's method is unavailable.

The proposed sleep stage set mixes bed occupancy with sleep classification. HealthKit explicitly permits an in-bed interval to overlap a sequence of awake/core/deep/REM intervals. [Sleep-analysis documentation](https://developer.apple.com/documentation/healthkit/hkcategoryvaluesleepanalysis?changes=_7_1&language=objc).

Use separate layers for bed occupancy and sleep stages, or label interval roles explicitly. Enforce nonoverlap only within a classification layer from one source. Preserve unknown, unmeasured, and non-wear as distinct where available. Record classifier provenance. A broad `light` mapping can be useful for display without asserting equivalence between every vendor's classifier.

Sport and exercise mappings should be versioned code-system mappings with a preserved original value. FIT permits developer-defined data fields, which makes a closed universal sport or workout-detail enum a poor lossless import boundary. [Garmin developer-data guidance](https://developer.garmin.com/fit/articles/cookbook/developer_data.html). That documentation describes the extension mechanism; it does not establish a universal exercise taxonomy.

For strength sets, distinguish external load, assistance, bodyweight contribution, machine settings, and resistance-band descriptions. A negative mass shortcut for assistance makes unit arithmetic easy but hides the meaning of load. Add a load mode and preserve the reported value. Repetitions, duration, distance, laterality, unilateral counting, warm-up status, and set grouping should remain independently expressible. Avoid treating a vendor's exercise name as a stable cross-provider identifier.

## 6. Privacy must be designed before real health ingestion

### What the legal and operational sources establish

Purely personal or household processing can fall outside GDPR. A hosted service processing customer health data does not automatically inherit that exception. For processing within scope, health data requires an Article 9 condition as well as an Article 6 basis; access, correction, qualified erasure, and conditional portability rights matter. Security measures depend on risk. [European Commission scope guidance](https://commission.europa.eu/law/law-topic/data-protection/information-business-and-organisations/application-gdpr_en), [Commission legal-basis guidance](https://commission.europa.eu/law/law-topic/data-protection/information-business-and-organisations/legal-grounds-processing-data_en), [GDPR text](https://eur-lex.europa.eu/eli/reg/2016/679), [EDPS individual rights](https://www.edps.europa.eu/data-protection/our-work/subjects/rights-individual_en).

HIPAA applies through covered-entity and business-associate relationships, not because a database contains health measurements. Applicable deployments need the Security Rule's administrative, physical, and technical safeguards, including audit controls. A cloud operator handling protected information can be a business associate even when it cannot decrypt it. Consumer health services outside HIPAA can still face the FTC Health Breach Notification Rule. [HHS Security Rule](https://www.hhs.gov/hipaa/for-professionals/security/laws-regulations/index.html), [HHS cloud guidance](https://www.hhs.gov/hipaa/for-professionals/special-topics/health-information-technology/cloud-computing/index.html), [FTC rule guidance](https://www.ftc.gov/business-guidance/resources/complying-ftcs-health-breach-notification-rule-0).

Apple protects health data using device data protection and documents backup protections. Medplum documents encrypted database/object storage and distinguishes soft deletion from expunge. Datomic likewise distinguishes ordinary retraction from excision. These systems do not treat a logical deletion event as proof that historical payload bytes disappeared. [Apple health-data security](https://support.apple.com/en-euro/guide/security/sec88be9900f/web), [Medplum backup and recovery](https://www.medplum.com/docs/compliance/backup-and-recovery), [Datomic excision](https://docs.datomic.com/operation/excision.html).

### Required proposal changes

**Add a health-data threat model and deployment requirement before phase 0.** Protect the entire data root, Postgres data files and WAL, temporary files, backups, and exports. Encrypt transport and ensure observability does not record payloads, tokens, or full sensitive filter URLs. A health predicate in a GET query can itself reveal sensitive information.

Plaintext logical blob encoding is not automatically incompatible with encryption at rest: an encrypted volume can protect it on disk. However, the proposal currently supplies no complete protection story. The credential DEK does not protect ordinary health properties merely because both live in one repository. State exactly which threat each encryption layer addresses and how keys and recovery copies are managed.

**Choose an erasure contract.** A delete that appends a tombstone and removes a fold leaves prior values in segments and backups. Whole-repository deletion, selective record erasure, retention expiry, and correction with retained audit history are different operations. Document which are supported. For a service that must honor selective erasure, the current permanently readable payload history needs a design change.

Possible designs include erasable encrypted payloads with sufficiently granular keys, or an explicit expunge/rewrite process with a new verifiable repository history boundary. Either affects replay and disaster recovery. Key destruction is not sufficient if plaintext projections, cached keys, exports, or backups still recover the data. Retain minimal audit metadata separately from erased clinical content where appropriate.

**Make export useful outside substrate.** The repository tar is valuable for recovery. Also provide bounded interoperable exports with source units, timestamps, coding, provenance, and original files. FHIR is useful for supported clinical records; CSV/JSON plus documented metadata is useful for sensor data. Do not imply that one recovery tar necessarily satisfies every portability obligation.

**Separate read auditing from the mutation changelog.** Record sensitive reads, exports, administrative access, provider access, and key operations without duplicating clinical payloads into another permanent log. Keep access controls consistent with substrate's existing full-repository token model. If narrower provider permissions are desired, call that a separate authorization change rather than pretending the sandbox already supplies scopes.

These recommendations do not assert that every private installation is subject to HIPAA or GDPR, or that those laws universally require the same encryption mechanism. They identify architectural decisions needed before offering the store to others.

## 7. Recorded-offset civil dates are one useful mode

### What other systems mean by a day

| System | Public behavior |
| --- | --- |
| Google Health | Separates physical-time aggregation from civil-time aggregation using recorded offsets. This supports the proposal's recorded-local-date mode. [Aggregation guide](https://developers.google.com/health/aggregate-data). |
| Health Connect | Duration and period grouping are distinct, with instant/local-time choices. Interval records can have different start and end offsets. [Aggregation documentation](https://developer.android.com/health-and-fitness/health-connect/aggregate-data), [interval-record API](https://developer.android.com/reference/android/health/connect/datatypes/IntervalRecord). |
| Oura | Its documentation distinguishes Sleep Day, Calendar Day, and Activity Day. A provider period is therefore not reducible to one universally correct civil date. [Oura day definitions](https://partnersupport.ouraring.com/hc/en-us/articles/29040334963219-Understanding-the-Different-Types-of-Oura-Days-in-Oura-Teams). |
| WHOOP | WHOOP's engineering explanation defines a cycle from sleep onset to sleep onset. It is not the proposal's wake-to-wake cycle. [Cycle explanation](https://engineering.prod.whoop.com/de/dev-platform-2/), [cycle API](https://developer.prod.whoop.com/docs/developing/user-data/cycle/). |
| Junction | Documents physical timestamps, local-time behavior, offset availability, and source-specific caveats. Missing offsets and device-local clocks cannot all be interpreted as known UTC instants. [Timestamps and zones](https://docs.junction.com/wearables/providers/timestamps-and-time-zones), [Sense time semantics](https://docs.junction.com/sense/query-dsl/time-and-data-semantics). |
| Clinical exchange | FHIR distinguishes dates, datetimes, instants, and periods. A date-only clinical fact need not have an inventable UTC instant. [FHIR datatypes](https://hl7.org/fhir/R4/datatypes.html). |

### Assessment and recommended representation

Confirm recorded-offset civil date as a first-class mode. Reject it as the single meaning of local day. Support four explicitly named choices:

1. Elapsed-time windows on UTC instants.
2. Calendar windows in a named IANA timezone.
3. Recorded-local dates using the source's offset at each measurement.
4. Provider-defined periods or sessions, identified by their original period/session identity.

Store the original offset when known, optional zone identifier, original local date/time when that is all the source supplied, and time-quality/provenance. Unknown offset must remain unknown. Keep session start and end offsets separately. One offset on a whole trace is sufficient only when the trace is split at offset changes or carries an offset-change representation.

Travel can cause a recorded-local date to comprise multiple UTC intervals, or leave gaps. Do not require every recorded-local day to be described by one ordinary fixed-zone `[start,end)` interval. Return date labels and effective coverage separately from physical boundaries. A vendor's sleep-day assignment should survive later user travel or a changed display timezone.

**The 15-minute claim is false as stated.** IANA's `Africa/Monrovia` history includes an offset of minus 44 minutes 30 seconds until 1972. It is a real offset, is not divisible by 15 minutes, and cannot be represented by `utc_offset_min`. [IANA Africa source](https://raw.githubusercontent.com/eggert/tz/main/africa).

Modern common offsets such as +05:30 and +05:45 make 15-minute UTC rollups useful. They do not establish a universal guarantee. Arbitrary session boundaries also cut through those rollups even in ordinary modern zones. Use raw boundary samples when alignment fails, and define supported timestamp/offset precision explicitly. Include travel, DST transitions, missing offsets, fractional sample intervals, and historical offsets in acceptance examples.

## 8. The benchmark does not yet validate the advertised contract

The SQL provides useful exploratory evidence, but several headline claims exceed what it measures. This is a static review of the supplied files, not a rerun.

In `query.sql` and `query4.sql`, many timings execute the following shape:

```sql
SELECT count(*)
FROM (
  SELECT bucket, avg(value)
  FROM samples
  GROUP BY bucket
) q;
```

The outer query needs group count, not `avg(value)`. PostgreSQL can remove unused aggregate expressions; its planner explicitly removes unneeded subquery outputs. [Planner source](https://doxygen.postgresql.org/allpaths_8c.html). The same issue affects the `sum`, standard-deviation, rolling-window, and M4 examples. The chunk-statistics query only counts qualifying rows, so it does not establish the cost of extracting JSON statistics and can avoid reading the relevant large values.

Some later benchmark queries do consume aggregates and include execution plans. Do not discard those results. Identify precisely which measurements consumed which expressions before using the table to select an architecture. [Benchmark query file](../bench/query.sql), [rollup and M4 queries](../bench/query4.sql).

Additional limitations:

- The M4 query does not retain timestamps for minima and maxima, so it is not a full M4 result even if every expression is evaluated.
- The narrow benchmark schema omits several proposed identifiers and metadata fields. Its storage result is not the size of the proposed production schema.
- The bulk rebuild uses an `UNLOGGED` table and SQL expansion. It does not measure changelog replay through the actual fold, WAL, per-record validation, or concurrent reads.
- Warm single-user synthetic data cannot establish a fixed latency for any timezone, arbitrary filters, source reconciliation, late corrections, or concurrent users.
- SQL files alone do not reproduce the claimed observed results. The checked-in benchmark directory needs raw outputs, hardware/configuration, and plans for the exact reported query variants.

Recommended follow-up: consume every result in a materialized output or client sink, retain `EXPLAIN (ANALYZE, BUFFERS, VERBOSE)`, check result equivalence, and benchmark representative correction and replay workloads. Include the full projected schema, realistic missingness, mixed sources, large and small chunks, and cold as well as warm reads. This is proposed work; none of it was run for this review.

## 9. Ranked changes to make

1. **Specify measurement semantics before implementing rollups.** Preserve intervals, summary status, quality, coverage, weighting, and measurement identity. Otherwise fast aggregates can be wrong.
2. **Add privacy, encryption, erasure, and export decisions before real ingestion.** Explicitly reconcile retained changelog payloads with deletion promises and deployment threat models.
3. **Fix quantity compatibility.** Correct `kU/L`, distinguish temperature deltas, gate same-dimension clinical conversions, and define decimal rounding and bounded-result predicates.
4. **Define stable chunk identity and revision handling.** Remove sync pages as an unqualified identity boundary; handle incomplete fetches, stale updates, deletions, and all invalidated buckets.
5. **Separate source-summary selection from interval deduplication.** Make source resolution independent of requested output resolution when the goal is a reconciled raw series.
6. **Make time modes explicit and rollup use conditional.** Preserve offsets and uncertain local time; support raw boundary fallback and provider period identities.
7. **Replace benchmark headlines with verified measurements.** Preserve executed outputs and plans; measure correction amplification, full replay, and annual bytes including duplicate representations.
8. **Narrow the first aggregate API.** Start with typed selectors, legal reducers, coverage, explicit budgets, and consistent snapshots. Add rolling, sessions, and M4 under separate tested semantics.
9. **Add metric profiles and versioned provenance.** Keep the generic kind layout, but enforce clinical context, component structure, sleep layers, and mapping versions.
10. **Define a waveform profile and payload limits.** Preserve exact clocks and acquisition metadata; evaluate external immutable payloads only with a corresponding replay and retention design.
11. **Resolve rollout prerequisites.** Phase 0 cannot use an unimplemented `quantity` type without a documented temporary representation. Google Health is currently not onboarding new projects, so access is a provider-delivery dependency, not merely a scope-selection task. [Current Google Health notice](https://developers.google.com/health).

## 10. Proposal claims that are wrong or unsupported

| Proposal claim | Verdict and evidence | Replacement |
| --- | --- | --- |
| Section 2.2: nobody durably stores one row per dense sample in a general table; Open Wearables calls its approach a stopgap. | The universal claim is false. Health Connect's SQLite implementation has child sample rows; current Open Wearables has a narrow per-point relational model and a separate archive model. An API array does not establish physical array storage. [Health Connect source](https://android.googlesource.com/platform/packages/modules/HealthFitness/+/refs/heads/main/service/java/com/android/server/healthconnect/storage/datatypehelpers/HeartRateRecordHelper.java), [Open Wearables source](https://github.com/the-momentum/open-wearables/blob/main/backend/app/models/data_point_series.py). | Full substrate records per sample are expensive. Bounded records plus a narrow per-sample index are a justified local design. |
| Sections 3 and 5.3: 15 minutes divides every real UTC offset, so any civil day uses whole rollups. | False for historical offsets, and insufficient for recorded-offset travel days or arbitrary session boundaries. [IANA Monrovia history](https://raw.githubusercontent.com/eggert/tz/main/africa). | Fifteen-minute rollups cover many modern fixed-zone calendar queries; verify alignment and fall back to raw edges. |
| Section 5.1: `kU/L` is an arbitrary unit that converts to nothing. | False under UCUM. `U` is enzyme activity, distinct from `[IU]` and `[arb'U]`. [UCUM specification](https://ucum.org/ucum). | Model catalytic activity concentration correctly; constrain clinical compatibility separately. |
| Section 2.4: WHOOP's day is wake-to-wake. | Contradicted by WHOOP's sleep-onset-to-sleep-onset definition. [WHOOP engineering](https://engineering.prod.whoop.com/de/dev-platform-2/). | Preserve WHOOP cycle identity and boundaries. |
| Section 5.2: closing periods means the changelog holds each period about once. | Not established. Closure does not prevent late sync or revision; source platforms expose explicit replacement/version mechanisms. [Health Connect writes](https://developer.android.com/health-and-fitness/health-connect/write-data), [HealthKit sync version](https://developer.apple.com/documentation/healthkit/hkmetadatakeysyncversion). | State an observed revision-rate assumption and measure its storage effect. |
| Section 5.2: the heavy user's changelog falls below one segment per year. | Unsupported by the supplied workload accounting. Payload bytes, envelopes, both mirror and canonical storage, and revisions all count. The SQL estimates do not execute that end-to-end write workload. | Publish an explicit byte model and measured ingestion result; retain the estimate as conditional. |
| Sections 3 and 5.3: the benchmark establishes average/min/max/SD and arbitrary calendar aggregation at about 25 ms. | Overstated. The rollup timing query counts groups without consuming those values, and no one benchmark can establish every calendar/filter case. [Benchmark SQL](../bench/query4.sql). | Report only verified query plans and measured conditions. |
| Section 7: a local day is always the civil date at the sample's own offset. | Too broad as a universal data-model rule. It matches one Google mode, but provider cycles, source-defined days, uncertain local timestamps, and fixed-zone reports are different products. [Google aggregation](https://developers.google.com/health/aggregate-data), [Oura day definitions](https://partnersupport.ouraring.com/hc/en-us/articles/29040334963219-Understanding-the-Different-Types-of-Oura-Days-in-Oura-Teams). | Name the requested day mode and preserve provider period identity. |
| Implied by section 5.1: matching physical dimensions is sufficient for converting and comparing quantities. | False for clinically standardized ratios such as HbA1c. NGSP percent and IFCC mmol/mol need a method-specific formula. [NGSP equation](https://ngsp.org/ifccngsp.asp). | Require metric compatibility and a declared conversion family in addition to dimension checks. |

Source priority per bucket, inline chunks, a closed unit table, and the generic kind layout are choices, not factual errors. Their limitations should be explicit. With the changes above, each can remain part of the design.
