# Health, fitness and biomarker data on a substrate

Version 1, health only. Status: research and proposal, 2026-10-08, revised
the same day after three independent reviews. A second version that widens
the same model to every observed quantity (home sensors, energy, weather,
vehicles) is [observations.md](observations.md); where the two disagree, the
wider one is the current proposal and this page stays as the health-specific
research and catalogue. Nothing here is built and no decision record is written
yet; section 9 lists the records this plan would need. The six research
reports it rests on are under [docs/plans/health-data/](health-data/01-wearables.md),
the three reviews under `health-data/review/`, and the storage benchmark's SQL
and output under `health-data/bench/`. Every number quoted below is either
measured there or marked as an estimate, and section 3.1 says which benchmark
numbers the reviews found unreliable.

The question: how should a substrate store very large volumes of personal
health data per user (wearables, workouts, lab results), make providers cheap
to write, convert everything to common units and shapes, and give developers
and agents an API that answers aggregate questions without reading every row.

## 1. The answer in one page

**Three shapes cover every source, and two of them already fit.** Every
vendor, standard and open source model studied converges on the same four
shapes: an instant sample, an interval amount (half-open, summable), a session
with children (sleep, workout, meal, lab report) and a daily summary keyed by
a local calendar date. Sessions, daily summaries and lab results are tens to
hundreds of records a day; they fit the record model substrate has today, one
record per thing, and the WHOOP provider already mirrors them that way. Dense
samples do not: a heavy user produces about 45,000 numeric samples a day
(heart rate every 5 s, a CGM every 5 min, 1 Hz ride streams), and one record
per sample costs about 1 KB of changelog per sample and 2.5 to 7 s to average
one metric over one year.

**The proposal, in eight pieces:**

1. Every stored value says what it is: a `form` of `instant`, `interval`,
   `counter` or `summary`, a unit on the value itself, and for a summary the
   statistic and the period it covers. This is the fact the reviews found
   missing, and it is what lets vendor rollups (WHOOP recovery, Oura
   readiness, Garmin dailies) live beside raw samples without ever being
   averaged into them (section 5.1).
2. A `quantity` value type, `{value, unit, comparator?}`, modeled on `money`:
   an exact decimal and a UCUM code from an engine-owned table that only
   grows; the engine converts within a dimension for filters and ordering and
   refuses across dimensions and across incompatible metrics (section 5.2).
3. A `trace` record: one metric, one source, one fixed UTC period, the samples
   packed as arrays on the record, with a revision and the engine computing
   the statistics. The changelog line count drops from one per sample to one
   per hour per metric, with rewrites of closed periods expected (section 5.3).
4. A derived `samples` index and 15-minute rollups the fold projects from
   traces and observations, exactly as it projects `refs` and `fts` today:
   rebuildable, never a second truth, keyed by offset so a civil day in any
   zone is a sum of rows (section 5.4).
5. An `aggregate` mode on `GET /api/v1/records` with four explicit time modes
   (physical windows, a named zone, each sample's recorded offset, a
   provider's own period), reducers declared legal per form, a `from` choice
   between raw samples and vendor summaries, source policy resolved per stored
   interval, and hard work budgets; the same read for functions and agents
   (section 5.5).
6. A user-owned `health` sample package: `metric`, `observation`, `trace`,
   `source`, `sleep`, `workout` with `lap`, `set` and `plan`, `labreport`,
   `labresult`, `exercise`, `tag`; plus a shipped catalogue of about 70
   wearable metrics and about 240 lab markers as `metric` records (section 6).
7. Canonicalisation in functions shipped as one small adapter package per
   provider, because a record mapping can only copy (section 7). Mirrors keep
   the vendor's units and shapes, as the house rule already says.
8. A provider kit with four entry patterns: cloud OAuth mirror (exists), push
   receiver through a webhook trigger (exists), file import (needs a blob read
   for functions, which does not exist), and on-device companion apps (out of
   scope). Section 8 ranks the providers worth writing and names two that must
   not be written: a Strava API provider (its 2026 policy forbids persistent
   storage, aggregation and agent access) and a Garmin API provider (the
   developer program is closed to new applicants). FIT file import covers both.

**What this does not propose:** TimescaleDB or any columnar store as the
truth (a second truth beside the changelog), a general expression language for
queries, a `unit:` key bolted onto `float` properties, or storing dense
samples as blob bytes (nothing in the server reads blob bytes today and the
samples would leave the changelog).

**What the reviews changed.** Three reviews (a Codex engineering review, and
best-practice comparisons by Codex and by Fable, all under
`health-data/review/`) agreed on the shape and disagreed with the first draft
on nine points, all accepted: the index must keep interval boundaries and
summary status (the first draft would have averaged a nightly resting heart
rate with the weight of one sample); rollups need the offset in their key and
first, last and a duration-weighted sum in their row; the unit belongs on each
stored value, never looked up from an editable metric record; source priority
must resolve per stored interval, not per output bucket; the fold hook must
handle tombstone, purge, restore and merge; the derived tables need row-level
security and must preserve the contributing kind so function and agent grants
hold; `maxBuckets` bounds output, not work, so budgets on scanned samples and
time are needed; phase 0 depended on the `quantity` type that phase 1
introduced; and the plan had no deletion decision for the most sensitive data
the store will hold. Section 3.1 lists the benchmark queries the reviews found
to be measuring less than claimed.

## 2. What the sources look like

### 2.1 Platforms, their access and what changed in 2026

| Platform | Access in 2026 | What it exposes | Dense data | Change signal |
| --- | --- | --- | --- | --- |
| WHOOP v2 | OAuth, open | cycle, recovery, sleep, workout | none through the API | webhooks `*.updated`, `*.deleted`; `updated_at`; cycle boundaries may be revised after the fact |
| Oura v2 | OAuth only (personal tokens removed 2025-12); apps capped at 10 users until approved | daily documents, `sleep`, `session`, `workout`, `heartrate` | packed `PublicSample {interval, items}` inside documents; 5-minute HRV; 30 s sleep stages as a character string | webhooks per data type, renewable subscriptions; no `updated_at` |
| Google Health API v4 | Google OAuth, restricted scopes needing review; replaces the Fitbit Web API (off 2026-10-30) and Google Fit (ends 2026); the developer site says new projects are not being onboarded at the moment | about 40 data types, one resource shape | 1 to 5 s heart rate, per-minute steps and energy, per-minute SpO2 in sleep | subscriptions; `patch` and `batchDelete`; 14-day query cap on heart rate |
| Garmin Health and Activity APIs | partner program closed to new applicants since spring 2026; bought TrainingPeaks 2026-07 | dailies, epochs (15 min), sleeps, stress, HRV, SpO2, body composition, activities with 1 s samples, FIT files | yes, pushed | push or ping per summary type; same `summaryId` re-sent |
| Polar AccessLink | OAuth, open | exercises (FIT), daily activity, continuous HR, sleep, Nightly Recharge | 5-minute HR; 1 s in exercise | webhooks |
| Withings | OAuth, open | measure groups (integer mantissa times 10^exponent), sleep v2 per minute, ECG | per-minute sleep HR, RR, SDNN, RMSSD | `lastupdate` returns changed and deleted groups: the one true change cursor |
| Dexcom v3 | OAuth, open | EGVs every 5 min, events, calibrations | 288 readings a day, 1 to 3 h delay | none; 30-day windows |
| Abbott Libre | partner only; unofficial LibreLinkUp clients | 1-minute (Libre 3) readings | yes | none |
| Ultrahuman | partner token; 7-day windows | ring and CGM daily metrics | about 5-minute series | none |
| Apple HealthKit | no cloud API; a companion app, the `export.xml` zip, or an exporter app (Health Auto Export posts JSON) | 122 quantity types, category types, workouts, routes, clinical records (FHIR) | 5 s heart rate on Series 12; iOS 27 adds RMSSD | anchored queries on device only |
| Health Connect, Samsung Health | on-device only | about 45 record types with typed units and zone offsets | series records with `samples[]` | changes token (30 days) |
| intervals.icu | open API with a published OpenAPI document | activities with 184 fields, streams, intervals, wellness per day, zone settings | streams | polling |
| Concept2, Hevy, Wahoo | open APIs | rowing results with splits and strokes; strength sets; workouts with FIT | yes | Concept2 webhooks; Hevy change feed |
| Strava | API open, but the 2026 policy forbids retention past 7 days, any persistent index, aggregation, and AI use | activities, 11 streams, laps, zones | yes | webhooks |

The full per-platform field lists, units and limits are in
[01-wearables.md](health-data/01-wearables.md) and
[02-workouts.md](health-data/02-workouts.md).

### 2.2 The four shapes

Ten aggregators and five platforms designed their models independently. With
naming differences removed they agree on four shapes, and on replace-by-key
updates for all of them:

| Shape | Example | Identity | Who stores it this way |
| --- | --- | --- | --- |
| instant sample | 62 bpm at 07:00 | (metric, source, timestamp) | everyone |
| interval amount | 412 steps over [07:00, 07:15) | (metric, source, start, end) | Health Connect, Google Health, Junction, Validic, FHIR `effectivePeriod` |
| session with children | a 45-minute run with laps and streams; a night with stages | the upstream id | FIT, HealthKit, Health Connect, Terra, Junction, WHOOP |
| daily summary | steps, energy, resting HR for 2026-10-07 | (source, local date) | Terra, Junction `calendar_date`, Thryve `day`, Validic, Garmin `calendarDate` |

Two findings follow. First, no one keeps one changelog-style envelope per
dense sample: on the wire dense samples travel as arrays per session or per
period (Oura `items`, Health Connect `samples`, Terra `*_samples`, Junction's
columnar `sleep_cycle`, FIT `record` messages, FHIR `SampledData`), and on
disk the stores that do keep one narrow row per sample (Health Connect's
SQLite series table, Open Wearables' `data_point_series`, HAPI and Medplum
search columns) keep it as a thin index beside the parent object, not as a
full record. Second, every vendor states that a provider's daily total and the
sum of its samples will not agree and must not be forced to.

### 2.3 Units

The disagreements are the design input, so here they are in one list:

| Quantity | Disagreement |
| --- | --- |
| energy | WHOOP kJ; Oura, Garmin, Google, Polar, Withings kcal; Apple and Health Connect typed values read in any unit; Health Connect's basal rate is a power (kcal/day) while Apple's basal energy is an energy |
| duration | WHOOP ms; Oura, Garmin, Withings, Polar s (Polar also ISO 8601 periods); Google minutes for zone minutes |
| distance and mass | Google Health mm and g; Garmin g; everyone else m and kg; Fitbit km or miles and kg, lb or stone by the `Accept-Language` header |
| fractions | Apple SpO2 and body fat are 0 to 1; everyone else 0 to 100 |
| HRV | Apple SDNN over 60 s spot readings (RMSSD only from iOS 27); everyone else RMSSD, each over a different window (WHOOP last slow-wave sleep, Oura whole night, Google deep sleep). Same unit, not the same quantity |
| temperature | absolute (WHOOP, Apple, Google, Withings) versus deviation from a personal baseline (Oura, Fitbit, Garmin, Health Connect deltas). A deviation is a temperature difference, a different quantity from a temperature |
| glucose | mg/dL (Dexcom, Google, Terra, Spike) versus mmol/L (Libre outside the US, Junction, Health Connect's base). Mass to molar needs the molar mass of glucose, not a unit table |
| HbA1c | NGSP percent versus IFCC mmol/mol are both dimensionless but related by a method-specific formula (`NGSP = 0.09148 x IFCC + 2.152`); a ratio conversion is wrong by about 30 percent |
| sleep stages | ten different stage-name sets; Apple "Core" is light, WHOOP "slow wave" is deep, Polar splits light in two, Health Connect separates `AWAKE_IN_BED` from `OUT_OF_BED`, and Apple's `inBed` overlaps the stages rather than partitioning time |
| exact numbers | Withings sends an integer mantissa and a base-10 exponent; FIT sends integers with a declared scale and offset. Converting to float at ingest throws away exactness the source had |

Among unit systems only UCUM codes survive contact with labs and FHIR; every
wearable aggregator invented ad hoc strings and then disagreed on glucose,
weight and duration. UCUM alone cannot convert mass to molar concentration
(glucose mg/dL to mmol/L is a factor of 0.0555 that comes from 180.16 g/mol)
and cannot convert arbitrary units at all (`[IU]` of insulin, prolactin and
vitamin D are three different things sharing a symbol; `U/L` enzyme activity
is a real unit and is not one of them). The analyte table in
[03-biomarkers.md](health-data/03-biomarkers.md) lists about 60 factors.

### 2.4 Time

Every platform carries a UTC instant; most carry an offset beside it (WHOOP,
Garmin, Google's `utc_offset` on every point, Health Connect `ZoneOffset` at
both ends of an interval, Junction `timezone_offset`); some carry local time
with no offset (Fitbit legacy, LibreView CSV) and Junction documents one
source (Libre) as floating time that OpenAPI cannot express. Apple's steps and
heart rate never carry a zone; its sleep and workouts usually do. Vendor
"days" differ: Oura has three (an activity day from 04:00, a sleep day from
18:00, a calendar day by the phone's zone), WHOOP's day is a cycle from sleep
onset to sleep onset whose boundaries can be revised later, Garmin uses local
midnight, Google's `dailyRollUp` uses the civil day at each point's offset.
Two consecutive WHOOP cycles can land on the same local date. Sleep belongs
to the day you wake up.

The published bugs follow from this: bucketing by UTC date (Open Wearables
#396), identity built from vendor timestamps that drift by 100 ms (#1607),
daily totals mixed into intraday rows and averaged with the weight of one
sample (#1526). Google's Health API is the cleanest public answer: a physical
instant with its offset and a civil time on every point, and `rollUp`
(physical windows) beside `dailyRollUp` (civil days) as two verbs.

### 2.5 Volumes

A heavy user with a watch, a CGM, a power meter and a scale, estimated:

| Stream | Samples per day |
| --- | --- |
| heart rate at rest, every 5 s | 16,200 |
| heart rate in a 90-minute workout at 1 Hz | 5,400 |
| ride streams at 1 Hz, 6 channels, 4 days in 7 | 18,500 |
| steps and energy per minute | 2,880 |
| SpO2, respiration and skin temperature per minute in sleep | 1,440 |
| stress or body battery every 3 min | 480 |
| HRV every 5 min, CGM every 5 min | 576 |
| sleep stages, sessions, scale, labs | under 100 |
| total | about 45,500 |

That is 16.6 million samples a year, 166 million a decade. A moderate user
without a CGM or power meter is 3,000 to 5,000 a day. Vendor JSON for the
heavy user is 1 to 20 MB a day; Apple's `export.xml` for several years is 1 to
5 GB. Labs are a few hundred values a year and a few thousand in a lifetime;
their problem is semantics, not throughput.

### 2.6 Labs

No consumer lab (Function, Superpower, Everlywell, Medichecks, Randox, Ulta)
has an official API for the person's own results: the export is a PDF. The
machine paths are FHIR R4: Apple Health Records (`HKClinicalRecord` carries the
raw FHIR; Quest and Labcorp feed it, and Quest will sell a 50-marker panel
inside the Health app later in 2026), SMART on FHIR patient apps against each
health system (US Core makes UCUM mandatory and `interpretation` and
`referenceRange` Must Support), and US payer Patient Access APIs that must
include lab results from 2027-01-01. The UK's GP Connect is FHIR STU3 and not
open to consumer apps. Blue Button is claims only. `fasten-onprem` was
archived in July 2026. PDF parsing with a review step stays the common path.

What a lab value needs that a wearable sample does not: a comparator (`<5`),
coded values (`Not detected`, SNOMED), ratios (titre `1:80`), text, an absent
reason (`QNS`, cancelled), the lab's own reference ranges (which vary by lab,
sex, age and method), interpretation flags as received, specimen and fasting
state, a status that moves `preliminary`, `final`, `corrected`, and the report
it belongs to. LOINC puts the property and specimen into the code: glucose in
mg/dL is `2345-7` and in mmol/L is `14749-6`, so a molar conversion derives a
second observation under a second code, and a trend view needs an analyte
grouping above LOINC, which LOINC Groups provide with the published warning
that a group is a navigation aid and never a licence to merge values. The
catalogue in [03-biomarkers.md](health-data/03-biomarkers.md) lists 252 rows
in 22 panels with LOINC codes (about 230 checked against the NLM lookup), US
and SI units, factors and typical ranges.

### 2.7 Workouts

FIT is the lossless interchange format and the de facto model: `activity`,
`session`, `lap`, `length`, `record` (1 Hz wide rows), `event`, `set`
(strength), `workout_step` (plans), `time_in_zone` (with the zone boundaries
and thresholds that applied that day), `hrv` (RR intervals) and developer
fields. Garmin, Wahoo, COROS, Suunto, Zwift, Concept2 and Strava's bulk export
all produce it. TCX knows three sports and drops most of that; GPX is
geometry plus HR and cadence. The FIT SDK licence is internal-use and
anti-copyleft, so an importer should use an independent MIT decoder
(`fitdecode` in Python) rather than vendor the SDK.

Sport taxonomies disagree (Strava 56, FIT 82 sports plus 112 sub-sports,
HealthKit 84, Health Connect 61, WHOOP about 120 free strings including
"dedicated parenting"). Strength data has no shared exercise key; FIT names
1,846 exercises by category and name, wger has 919 with anatomical muscles
(CC-BY-SA), free-exercise-db has 876 with lay muscles (public domain). Every
plan format expresses targets relative to a threshold (%FTP, %LTHR, zone
number), so resolving a target needs the threshold in force on that day, and
intervals.icu and FIT both copy the zone settings onto the session so later
edits do not rewrite history.

### 2.8 What applications compute

From readiness scores, HRV baselines, sleep debt, training load, zone time,
daily totals, weight trend, glucose time-in-range, meal responses, cycle-phase
correlations, illness detection and lab trends, sixteen query primitives fall
out. The ones at least two uses depend on:

| # | Primitive | Needed by |
| --- | --- | --- |
| P1 | bucketed `count, sum, avg, min, max, first, last` | everything |
| P2 | `stddev`, `percentile(p)` | HRV CV, glucose SD, resting HR as a low percentile |
| P3 | calendar buckets in a named zone (day, week with a chosen start, month) across DST | daily totals, weekly zones, correlations |
| P4 | fixed-width buckets from an origin (1 min to 1 h) | charts, intraday |
| P5 | legal reducers declared per value form: an interval amount sums, an instant averages, a summary is read as is | steps versus heart rate; HealthKit's `HKQuantityAggregationStyle` |
| P6 | gap filling: empty buckets as null, zero or carried forward, flagged as synthetic | EWMA over days, weight trend |
| P7 | rolling windows over buckets | HRV 7 and 60 day, sleep debt, glucose 14 day |
| P9 | group by source, filter by source, source policy | any multi-device metric |
| P10 | statistics inside another record's `[at, endsAt)` | resting HR in sleep, SpO2 minimum, TSS, glucose after a meal |
| P11 | several metrics aligned on one bucket axis | correlations, agent summaries |
| P12 | count or duration where value in a range | time in range, HR zones |
| P14 | result unit chosen by the caller | glucose, labs, weight |
| P16 | a work budget and an explicit incompleteness signal | agents, the public API |

EWMA, regressions, correlation tests and the sleep regularity index stay in a
function or the client once P1 to P12 return compact aligned rows. An agent
answering "how did I sleep this month" needs about 30 rows, or one, never
30,000 samples.

### 2.9 Lessons others paid for

- Picking one source per day undercounts additive metrics: Open Wearables
  showed 4,157 steps (watch only) against Apple Health's merged 14,079.
  Cumulative quantities dedupe per time interval; discrete ones per record.
  Picking a winner per output bucket makes the total change with the bucket
  size, so the winner must be picked per stored interval.
- A stored value must say what it is a statistic of and over which interval,
  or a nightly average gets averaged with the weight of one sample.
- Units ignored at ingest are wrong forever: five separate Open Wearables
  bugs (mmol/L stored as mg/dL, km as m, fractions as percent). Validate the
  dimension on write and keep the source unit beside the canonical one.
- Prefer the upstream id; when there is none, fix one deterministic recipe
  and carry a version for replace semantics.
- The legal reducer belongs to the value's form, declared once: a mean of
  session means is not a mean.
- Connectors die faster than models (Shimmer, Human API, Metriport's
  wearables, the Fitbit Web API). The canonical layer must outlive any provider.
- Every long-lived append-only store that came to hold regulated data grew an
  erase path (Datomic excision, XTDB `ERASE`, EventStoreDB scavenge, Kafka
  compaction tombstones). Substrate lost its one rewrite when decision 0050
  superseded 0018.

## 3. What substrate has today, measured

The codebase audit ([06-substrate-fit.md](health-data/06-substrate-fit.md))
established these facts with file references:

- **One write costs about 1 KB of changelog for a two-field sample**, about
  25 times its JSON, dominated by one `manager` effect per property. One
  sample per second for a year is about 32 GB of segment files before the
  `records` row and its eleven index entries. A function's effects commit in
  one transaction but still one changelog line each; there is no bulk write.
- **Range filters on properties use no index**: a numeric comparison casts
  `props->>key` per row, and without `kinds` in the filter it compares as
  text. Only scalar references and the time columns are index-backed.
  `count=1` is the only aggregate, uncapped. The route has four modes (list,
  window, ranked, tail); an aggregation would be a fifth, reusing `buildFilter`.
- **Derived indexes are an established contract.** `refs` and `fts` are pure
  functions of the folded row and the declaration, re-derived in
  `foldRecordOp`, cleared and replayed by `RebuildRepository`. Tombstone and
  purge take different fold paths (`applyTombstone`, `applyPurge`), so a new
  derived table must hook all of them. `embeddings` shows the asynchronous
  queue-fed variant a rollup table could follow.
- **Blobs are opaque to everything**: a function cannot upload one, nothing
  server-side reads blob bytes, there are no range reads, and the bytes are
  plaintext on disk by decision 0031. A file importer needs a bounded
  `host.blobs` read, which does not exist.
- **`money` is a complete template for a structured value type**: an
  engine-owned table that only grows, a console copy held equal by a test,
  typed filter and order expressions, a `fake_test.go` case. A new datatype
  does not touch the wire golden; new aggregate reply structs would. There is
  no unit concept anywhere, and a `propertytype` refinement cannot add one.
- **Record mappings only copy**: identical datatypes, two-level paths, one
  source row to one target row, no constants, no arithmetic. mneme v6's health
  package already documents why its WHOOP projection is a function.
- **Function limits**: 60 s wall clock, 8 MiB response frame, paged drains of
  at most 200,000 effects in 2 minutes with the deadline persisting across
  retries, PEP 723 dependencies resolved at registration. The WHOOP body
  checks its own 40 s budget but not the drain deadline, so a full backfill
  would likely park. A delivery envelope carries the current live record, not
  the historical state of the triggering change, and no record for a delete.
- **Agents** read full records through `query`, at most 10,000 rows a run, no
  projection and no aggregate. Thirty sleep rows are fine; 40,000 samples are
  not answerable.
- **Deletion today** tombstones a record and lets the sweep purge its row and
  sealed material; the segment files keep every past value, and the docs say
  so ("the changelog keeps everything"). Nothing logs reads.
- **History**: the mneme-ported `samples/health` and `samples/fitness` were
  deleted on 2026-09-10 for lack of consumers (#498); `workoutset.weight`
  carried no unit (#125); the owner's repository holds 2.17 million entries in
  15.8 GB of segments (#761); conversions over 10,000 live records are refused
  (#157, open). No issue proposes units, traces or aggregation.

### 3.1 Storage options, measured, and what the measurements do not show

One year of heart rate every 5 s (6.3 million samples), Postgres 16 with
TimescaleDB 2.30 in a throwaway container, queries warm. The headline query
is the average per day over the year, in UTC and in Europe/Athens (which has a
23-hour and a 25-hour day). Scripts and output are in `health-data/bench/`.

| Option | Size | Avg per UTC day, year | Avg per Athens day, year | Update one sample |
| --- | --- | --- | --- | --- |
| (a) one record per sample, `props->>'bpm'` (today) | 3,349 MB plus about 1.5 to 2 KB per sample of changelog | 7.3 s | 2.5 to 5.3 s | one put and one changelog line |
| (b) narrow table, btree primary key | 710 MB | 2.4 s | 2.7 to 3.9 s | 1.5 ms |
| (b2) narrow table, monthly partitions, BRIN | 412 MB | | 4.0 s | |
| (c) TimescaleDB, compressed | 16.7 MB | 0.23 s | 1.3 to 4.6 s (0.15 s via UTC hours) | 5.4 ms |
| (d) one jsonb chunk per day with precomputed stats | 61 MB | about 1 ms, see below | 9.6 s unpacking | rewrite the chunk |
| (d2) native array chunk per day, `unnest` | 35 MB | 1.3 s | 4.7 s | 1.2 ms |
| (e) 15-minute rollups derived from any of the above | 5.3 MB | | about 25 ms, see below | re-derive one row |

Two reviews read the benchmark SQL and found that several headline timings
wrap the aggregate in `SELECT count(*) FROM (... GROUP BY ...)`, which lets
Postgres drop the unused `avg`, `stddev` and M4 expressions. The rollup (25 ms),
rolling-mean, chunk-statistics (1 ms) and M4 (176 ms) numbers therefore
measure grouping, not the statistics. The raw-table numbers in `query3.sql`
run real `EXPLAIN ANALYZE` aggregates and stand. The sizes stand. The rollup
and chunk numbers are kept above as exploratory and must be re-measured
consuming every output, on the full proposed table shape (section 5.4), with
corrections and a replay through the real fold, before any decision record
cites them. What the benchmark does establish: the per-record envelope, not
the sample value, is the cost of today's model, and local-day bucketing per
row costs more than the scan itself.

## 4. Principles the proposal holds to

1. The changelog stays the truth and records its fold. Every derived table is
   a pure function of folded rows and declarations, rebuilt by replay, and
   never reads a mutable record of another kind to know what a value means.
2. Declared is indexed is filterable. Only properties a kind declares as
   samples are projected; a filter the grammar cannot express is one that
   would be slow.
3. One read route. Aggregation is a mode of `GET /api/v1/records`, reached by
   functions and agents through the same `readList`, as the window read is.
4. Mirrors keep the vendor's shape and units; canonicalisation is a separate,
   user-owned layer with per-value provenance back to the mirror row.
5. Exact numbers or refusal. A lab value keeps its digits; a converted value
   records the formula, parameters and version it used so a rebuild
   reproduces it.
6. A stored value carries its own meaning: its form, its unit, and for a
   summary its statistic and period. Nothing is inferred from a bucket.
7. One word per thing. `series` already names a recurring temporal record, so
   the packed-samples record is a `trace`; a `metric` is the definition, an
   `observation` is one reading, a `sample` is one point inside a trace.
8. Additive vocabulary. Every new declaration key is reserved by name and lands
   validated; nothing here narrows an existing kind.

## 5. Engine proposals

### 5.1 The form of a value

Every stored measurement, whether one `observation` record or one sample
inside a `trace`, carries a `form`, and the form decides which reducers are
legal and how overlapping sources combine:

| form | meaning | time | legal reducers | source overlap |
| --- | --- | --- | --- | --- |
| `instant` | a reading at one moment: heart rate, weight, glucose | `at` | `avg`, `min`, `max`, `first`, `last`, `count`, `stddev`, `percentile`, `durationIn` under a declared hold rule | per record: two readings at one instant from two sources are two readings |
| `interval` | an amount accrued over `[at, endsAt)`: steps, energy, distance, water | `at`, `endsAt` | `sum`, `count`, `min`, `max` of the amounts; a partial overlap with a bucket is refused, prorated or assigned by start, as the request says | per stored interval: the winning source's interval covers that span, a lower-priority source fills uncovered spans |
| `counter` | a cumulative total since a session start (Terra steps, FIT distance) | `at` | none; the canonicaliser differences it into `interval` amounts at ingest and keeps the counter on the mirror | n/a |
| `summary` | a vendor's or a function's statistic over a stated period: a nightly average HR, a daily step total, a recovery score | `at`, `endsAt`, plus `statistic` and `period` | read as is; `avg` of summaries is a mean of means and is labelled so; never combined with `instant` or `interval` rows in one bucket | one summary per (metric, source, period); a priority picks whose |

This is the row Open Wearables lacked (`is_daily_total` was an afterthought),
the `descriptive_statistic` IEEE 1752.1 requires on a summarised value, and
the cumulative-versus-discrete split HealthKit refuses to let a query cross.
A metric still declares a default form and the reducers it recommends, but
the engine reads the form on the value, so editing a metric record cannot
change what an indexed number means.

### 5.2 The `quantity` value type

A `quantity` is a structured value, exactly as `money` is:

```yaml
weight:
  type: quantity
  dimension: mass            # the engine refuses a unit outside the dimension
  units: [kg, g, "[lb_av]"]  # optional narrowing to a list; absent admits the dimension
```

```yaml
weight:
  value: "72.50"             # exact digits, the decimal grammar, always a string
  unit: kg                   # a UCUM case-sensitive code from the engine's table
  comparator: "<"            # optional: <, <=, >=, >; admitted only where the declaration says comparator: true
```

Rules, each one a line in a decision record:

- The engine owns a closed unit table: code, dimension, exact rational factor
  to the dimension's base, and for the affine units (`Cel`, `[degF]`) an
  offset. It is a documented UCUM subset, not a UCUM evaluator: it starts at
  about 100 codes covering sections 2.3 and 2.6, accepts a small alias table
  on input (`mmHg`, `bpm`, `mcg` map to `mm[Hg]`, `/min`, `ug`) and stores
  only the code, and only grows, like the currency table. Python (for
  functions), Go and the console are generated from one source and held equal
  by tests against the UCUM conformance cases, not only against each other.
- `value` is always exact digits, including integer readings, as `decimal`
  is. A comparison converts with exact rational arithmetic (cross
  multiplication, never a rounded converted value), because `1 [degF]` is
  `-155/9 Cel` and has no finite decimal. A requested output unit may round,
  and the response says so.
- A filter compares within the operand's unit after converting the stored
  value to it; operands and stored values in different dimensions never match,
  the way money never matches across currencies. Ordering sorts by the value
  converted to the dimension's base for linear units.
- Temperature is two dimensions: `temperature` (affine, `Cel` and `[degF]`
  carry an offset) and `temperature-difference` (linear, declared `delta:
  true`, stored in `K` or `Cel` with the offset suppressed). A deviation from
  a baseline is a difference; converting 0.3 Cel of deviation to Fahrenheit
  gives 0.54, not 32.54. `sum` over an affine absolute unit is refused; a
  standard deviation of temperatures is a difference; a variance has squared
  units and is not served as a quantity.
- Mass to molar concentration is not a unit conversion. The engine refuses it;
  the `metric` record carries `molarMass`, and a function derives a second
  observation under the molar LOINC code, stamped with the formula, its
  parameters and a version, and linked by `derivedFrom`, when a reader needs
  one. The same applies to HbA1c (an affine method formula, not a factor).
  An aggregate never counts an original and its derivation as two readings.
- Dimension equality is not clinical equivalence. A `metric` carries a
  compatibility class (analyte, LOINC property, specimen, method); a
  conversion or a merged trend across two metrics is allowed only within one
  class, so NGSP and IFCC HbA1c, or serum and whole-blood glucose, are never
  silently pooled.
- Arbitrary units (`[IU]`, `[arb'U]`) are commensurable only with themselves
  under one assay; prefix scaling of a metric arbitrary unit (`m[IU]/L` to
  `[IU]/L`) is allowed where UCUM marks the atom metric. `U` (enzyme
  activity, as in `U/L` and `kU/L`) is a real unit, not an arbitrary one.
- `comparator` means the true value lies in the half-line the comparator
  names. A filter is true when it is certainly true for every value in that
  set: a stored `<5` matches `lt: 5` and `lt: 10`, not `lt: 3` and not `eq`;
  `in` is refused on bounded values. Bounded values are excluded from `avg`,
  `percentile` and `durationIn`, counted separately as `bounded` in the
  response, and included in `count`.
- A unit the table does not know is not a write error on a lab result: the
  result keeps `sourceUnit` and the verbatim value text, `value` stays empty,
  and `normalizationFailure` names why, so the row is still listed by date and
  code.
- `displayTemplate` renders `72.5 kg`; search indexes the number as text. A
  `unitpreferences` setting record (kg or lb, mmol/L or mg/dL) is read by the
  console and the `query` tool to choose the requested output unit; it is not
  an engine conversion rule.

Lab values need more than a quantity. Rather than widen the value type, the
`labresult` kind in section 6 declares one `value` quantity and sibling
properties for the other shapes (`codedValue`, `ratio`, `text`,
`absentReason`), and the engine enforces that exactly one is set on every
write, including a patch that switches shapes (the old shape is cleared in
the same write). That keeps `quantity` small and reusable by any kind (an
expenses kind that measures fuel, a recipe that measures flour).

The files to touch are the `money` checklist in the audit (about 20 Go files
plus the console, including editors, filters, titles and unit pickers) and
the console's handling of an unknown datatype, which today falls back to a
string. Estimated size: medium.

### 5.3 The `trace` record and the `samples` trait

A trace holds the samples of one metric from one source over one fixed UTC
period, packed:

```yaml
kind: ada.example.com/health/trace
metadata:
  id: oura/george/heart_rate/2026-10-07T06   # <provider>/<account id>/<metric>/<period>, never a sync page
data:
  properties:
    metric: {ref: ada.example.com/health/metric/heart_rate}
    source: {ref: ada.example.com/health/source/oura-ring-gen3}
    form: instant
    at: 2026-10-07T06:00:00Z
    endsAt: 2026-10-07T07:00:00Z
    utcOffsetStart: 180            # minutes; the civil clock where the user was
    utcOffsetEnd: 180              # a trace is split where the offset changes
    unit: /min
    interval: PT5S                 # regular form: values are every interval from at
    values: ["61", "62", null, "60", ...]
    # irregular form instead: offsets: [0, 4900, 10200, ...] (ms from at) beside values
    revision: 3                    # monotonic per id; a lower revision is refused
    origins:                       # the mirror rows this period was built from
      - {ref: providers.substrate.reamde.dev/oura/heartrate/...}
    stats:                         # managed: the engine computes it on write
      count: 718
      min: "48"
      max: "131"
      mean: 61.4
      m2: 59032.1
      first: {at: 2026-10-07T06:00:00Z, value: "61"}
      last: {at: 2026-10-07T06:59:55Z, value: "58"}
      coverage: PT59M50S           # time between first and last sample
```

The kind binds a new core trait, `substrate.reamde.dev/core/samples`, which
names the properties above by identity the way `accountconfig` and `sync` are
known to the host, with three variants: `point` (one `observation`, its
`value` quantity and `form`), `interval` (an observation with `at`, `endsAt`
and an amount) and `packed` (a trace). The host validates what a trait cannot
today: cardinality, that exactly one timing form is set, that offsets are
ordered and inside the period, that `values` and `offsets` have equal length,
that nulls are gaps. Binding it is what makes the fold project the record into
the `samples` index (section 5.4); a kind that does not bind it pays nothing.

Rules:

- **Identity is fixed boundaries.** The period is a UTC hour (or a declared
  shorter width for high-rate channels) for a continuous metric, or the
  session for a workout stream, never a sync page: changing a provider's page
  size must produce identical live data. Two traces with the same (metric,
  source, period) are the same trace; a different `source` is a different
  trace, so a mirrored measurement reaching two sources is counted twice until
  the source policy picks one, which is the correct answer.
- **Rewrites are routine, not rare.** Apple and Garmin deliver hours late,
  WHOOP revises cycle boundaries, Junction documents corrections replacing
  earlier values. A trace is state: a rewrite replaces the period whole, the
  `revision` must rise (Health Connect's `clientRecordVersion`, HealthKit's
  sync version), and a stale rewrite is refused. The superseded line stays in
  the segment files; section 9's deletion decision owns that cost. Providers
  keep an open period in their own checkpoint and write it when the period
  closes, with a declared maximum freshness delay.
- **Reconciliation per origin.** A canonicaliser computes the full set of
  outputs a mirror row implies, writes them, and deletes outputs the row no
  longer implies; `origins` is repeated because one period comes from several
  rows or pages. A corrected timestamp that moves samples to another period
  rewrites both periods.
- **Size limits by bytes and samples**, not by wall time: a trace is refused
  above a declared sample count and above a byte size that leaves room for
  the function frame (8 MiB) and a later REST re-apply (1 MiB). An hour of
  5-second heart rate is about 8 KB; an hour of 1 Hz power is about 40 KB; a
  30 s ECG strip at 512 Hz is about 15,000 samples and fits; a day of 1 Hz
  anything does not, so a day is not a valid period for 1 Hz data.
- **`stats` is managed**: the engine computes it from the values in one
  deterministic routine (count, min, max, mean and M2 for a stable variance,
  first and last with their times, coverage), so a reader that only needs
  per-period statistics never unpacks the arrays and no provider can supply
  statistics that disagree with the values.
- **Exact clocks for waveforms**: `interval` is an ISO duration and may carry
  fractional seconds (`PT0.001953125S` for 512 Hz); integer millisecond
  offsets are for irregular sampling only. A waveform trace also names its
  `channel` and the acquisition it belongs to; a multi-channel ECG is one
  trace per channel under one `acquisition` reference. Long multi-channel
  recordings are out of scope until a size study is done.
- A sample inside a trace is not addressable: a filter cannot name one
  value, and that is the trade the chunk makes. A metric that needs per-value
  addressability (a weighing, a blood pressure reading, a lab value) is an
  `observation`, one record each.
- The changelog cost for the heavy user drops from about 60 segments a year
  to a few; the exact figure depends on the revision rate and is to be
  measured, not assumed.

### 5.4 The derived `samples` index and rollups

Two new tables in one SQL migration, both derived, both under the same
row-level security and application-role grants as `records` and `refs`:

| table | row | written when | rebuilt by |
| --- | --- | --- | --- |
| `samples` | `(repository, kind, record_id, metric, source, form, ts, ts_end, utc_offset_min, unit, value float8, value_base float8, statistic, bounded bool)` | in `foldRecordOp` for a kind binding `samples`: delete the record's rows, insert its values, like `refs`; and in the tombstone, purge, restore, merge and declaration-removal paths, which delete | cleared and replayed by `RebuildRepository`; reprojected on a vocabulary change |
| `sample_rollups` | `(repository, kind, metric, source, form, utc_offset_min, bucket15 timestamptz, n, sum, min, max, mean, m2, first_ts, first_value, last_ts, last_value, covered_ms)` over `instant` and `interval` rows only, never `summary` | same transaction: recompute every bucket the old and the new rows touched, once per transaction | same |

Why these columns, each answering a review finding: `kind` so a function or
agent grant on kinds still bounds what an aggregate may read; `form` and
`statistic` so a summary is never pooled with samples; `ts_end` so an interval
amount keeps its boundaries and partial overlaps can be prorated or refused;
`utc_offset_min` in the rollup key so a civil day at the sample's own offset
is a sum of whole rollup rows (a source changes offset a few times a year, so
the row count barely moves); `unit` and `value_base` so a filter or an order
across units hits an expression index on the base value (the HAPI and Medplum
normalized-quantity pattern) and a metric edit cannot relabel stored numbers;
`first`, `last` and `covered_ms` so `last`, `first`, M4 and a time-weighted
mean come from rollups; `mean` and `m2` instead of a sum of squares so the
variance is stable and mergeable; `bounded` so censored lab values are counted
but not averaged.

Why 15 minutes: it divides every offset in use today (the odd ones are
+05:45, +08:45, +12:45 and +13:45); it does not divide pre-1972 local mean
time offsets, which no wearable produces, and it does not help a window that
starts at 23:07, so an aggregate reads whole rollup rows for the interior of
a window and raw `samples` rows for its two boundary fragments. Percentiles
are not derivable from rollups; a percentile request scans `samples` under
its own budget, and a sketch (t-digest) is a later addition.

`float8` is for the sensor analytics projection only. The record keeps the
digits; a lab quantity or a money value asked for through the records path
(section 5.5) aggregates in `numeric`. A trace whose values are decimal
strings projects exactly up to float8 precision, which the response labels
approximate.

Retention is a per-repository setting for later: because the index is
derived, old years can keep rollups only, with the response saying "not
materialised" rather than "no samples" for a range whose raw rows are gone.

TimescaleDB is an optional backend for these two tables where the extension is
installed (97.6% smaller, vectorized UTC buckets); the licence question for
hosted use and the image change keep it out of the default.

### 5.5 The aggregation read

A fifth mode of the records route, selected by `aggregate=`, with its own
allow-list like the others. It is refused beside `q`, `watch`, `count`,
`orderBy`, `first`, `after`, `offset` and `expand`, and it never triggers the
window read: the aggregate's `range` clips samples, and a trace that overlaps
the range is included even though its own `at` lies outside it.

```http
GET /api/v1/records?filter={...}&aggregate={...}
```

```json
{
  "metric": {"ref": "ada.example.com/health/metric/heart_rate"},
  "from": "raw",
  "range": {"gte": "2026-01-01T00:00:00+02:00", "lt": "2027-01-01T00:00:00+02:00"},
  "bucket": {"calendar": "day"},
  "reducers": ["avg", "min", "max", "count", "stddev", {"percentile": 0.05}],
  "unit": "/min",
  "fill": "null",
  "rolling": {"buckets": 7, "reducers": ["avg", "stddev"]},
  "sources": {"policy": "separate"},
  "budget": {"maxBuckets": 1440, "maxSamples": 20000000}
}
```

The parts, each with the review that shaped it:

- **Selector.** Either `metric` (a full record reference; the samples index
  is read) or `property` (a declared numeric or quantity property on the
  kinds the filter names, read from `records` and aggregated in `numeric`),
  never a bare word. The second path is what makes "money per month" over an
  expenses kind or "tasks closed per week" the same request, and it needs the
  typed expression indexes the audit calls rank 2 or it stays a scan; a kind
  on that path names its time anchor (`at`, `endsAt` or a declared stamp
  such as `completedAt`).
- **`from`.** `raw` reads `instant` and `interval` rows; `summaries` reads
  `summary` rows whose `statistic` matches the reducer; `prefer-raw` uses raw
  where a bucket has any and the summary otherwise, and the response says per
  row which it used. One bucket never mixes the two. This is how a WHOOP
  recovery score, an Oura readiness contributor or a Garmin daily total is
  read beside a derived daily average from samples without the double count.
- **Time modes, four, named.** `{"every": "PT5M", "origin": ...}` is a
  physical window on UTC instants. `{"calendar": "day", "timezone":
  "Europe/Athens"}` is a calendar window in a named zone (an Elasticsearch
  `time_zone` day, 23 or 25 hours across DST). `{"calendar": "day"}` with no
  zone is the recorded-local day: each sample's own offset decides its date
  (Google `dailyRollUp`, Health Connect `LocalDateTime`), and a travel day may
  be 20 or 28 hours. `{"records": {filter}}` uses each matched record's
  `[at, endsAt)` as a bucket (P10): a sleep, a workout, a WHOOP cycle, a meal
  window with `"window": {"before": "PT15M", "after": "PT2H"}`, with a cap on
  matched records and the record's reference and version on each row; a
  sample inside two windows is counted in both and the rows are labelled
  non-additive. `offset` (an ISO duration) shifts a calendar day's start for
  04:00 and 18:00 vendor days; `weekStart` picks the week's first day. A
  sample with no offset is bucketed by the request's zone when one is named
  and refused otherwise, never by zero and never by the server's clock.
- **Reducers** are structured where they take an argument
  (`{"percentile": 0.05, "method": "exact"}`, `{"durationIn": [70, 180],
  "hold": "PT10M"}`), and each reducer is checked against the value `form`:
  `sum` over an `instant` metric is `422` naming the form, as an undeclared
  filter operator is refused today. Every reducer in the response carries its
  own unit (`count` has none, `durationIn` is a duration, `stddev` of a
  temperature is a temperature difference).
- **Sources.** `separate` groups by source (HealthKit `separateBySource`).
  `priority` with an ordered list resolves per stored interval before any
  bucketing, so an interval amount from a lower-priority source fills spans
  the winner did not cover and the total does not change with the bucket
  width. `summaries` selects one source's summary per period. The response
  lists the sources that contributed and the covered duration.
- **`fill`** makes the buckets dense over `range` (`null`, `zero`, `locf`),
  marks filled rows `synthetic: true`, and is applied after reducers and
  before `rolling`; a rolling window counts buckets, including filled ones,
  and may look back before `range`.
- **Budget.** `maxBuckets` (default 1,440, cap 10,000) bounds output;
  `maxSamples` bounds the rows the plan may read; a statement timeout within
  the caller's remaining deadline bounds time; `groupBy` cardinality and
  matched records are capped. A request over budget is refused before it
  runs, naming the limit; a result is never silently truncated or degraded.
  Functions and agents pay these against their existing read budgets.
- **Downsampling for charts is a separate mode**, `downsample={"points":
  800, "method": "m4"}`, returning the first, last, minimum and maximum
  samples per pixel column with their timestamps, which is a set of points
  and not a statistic and must not be fed into rolling windows.
- **The response** is a named struct in `internal/substrate`, mirrored in the
  console's `types.ts` and pinned by the wire golden: the resolved metric and
  unit, the time mode and zone used, per-row `start` and `end` with offsets
  (so the 23-hour day is visible), per-row `from`, sources and coverage, the
  `bounded` count, an exact-or-approximate flag, and the changelog head and
  generation the read was served at.

Evaluation order, fixed: select records and samples, apply the value's
validity rules, resolve the source policy, clip to the range and windows,
reduce, fill, roll. What it leaves out on purpose: arbitrary expressions,
joins beyond "samples inside a matched record's window", EWMA (a function
computes it from aligned rows), continuous queries (a scheduled function).

Functions get it as `host.records.aggregate(...)` and agents through the
`query` tool's new `aggregate` argument, so "how did I sleep this month" is
one call returning 30 rows of numbers.

### 5.6 Smaller engine changes the above needs

- `host.blobs.get(digest, offset, length)` for functions, bounded by the 8 MiB
  frame and a declared `permissions.blobs` grant, plus `host.blobs.put` for a
  body that keeps a FIT original. Without the read there is no file importer.
- A durable import job for inputs larger than one function run (a multi-GB
  Apple export): resumable, bounded per step, with progress on a record. A
  token-authenticated client that streams locally and submits bounded records
  is the alternative; routing personal imports through the operator's DSN is
  not, because it changes who may do it.
- Cheaper changelog lines for machine writes: one `manager` effect per record
  instead of one per property roughly halves a small record's line. Optional;
  traces make it less urgent.
- The WHOOP body must honor the 2-minute drain deadline (the GitHub provider's
  `DRAIN_SECONDS` pattern), and a backfill must run as bounded completed jobs
  with durable progress, because the deadline persists across retries.
- A canonical backfill: when an adapter is installed over an existing mirror,
  or a converter version changes, every mirror row must be re-projected; that
  is a job, not a trigger.

## 6. The `health` sample package

A sample, so it imports under the user's authority and is theirs to extend
(a user who imports it adds a property the next day, as the samples plan
says). Every kind is `purpose: supporting` except `metric`, `source`,
`labreport`, `workout` and `sleep`, which are `primary`. The base package
declares no reference to any provider kind; provider adapters are their own
packages (section 7).

| Kind | What one record is | Key properties |
| --- | --- | --- |
| `metric` | the definition of a measurable thing: an analyte, a sensor channel, a derived score | `name`, `codes` (repeated object `{system, code, version, display}`: LOINC, MDC, HealthKit identifier, Health Connect type, Google data type, or the package's own code), `loincParts` object (component, property, system, scale, method), `group` (a LOINC Group id or the package's own grouping, for navigation only), `compatibility` (the class within which conversion and pooling are allowed), `dimension`, `unit` (canonical), `delta` (bool, for differences), `form` (default form), `reducers` (repeated enum: the ones recommended), `valueKind` (`quantity`, `coded`, `ratio`), `molarMass`, `conversions` (repeated object: target metric, formula id, parameters, version), `referenceLow`, `referenceHigh` (a labelled fallback only), `vendor` and `algorithmVersion` for vendor scores, `description` |
| `observation` | one reading of one metric: a weighing, a blood pressure, a nightly HRV, a daily step total | `metric` (ref), `form`, `value` (quantity), `at` and optional `endsAt` (temporal range), `utcOffset`, `statistic` (`sum`, `avg`, `min`, `max`, `last`; required when `form` is `summary`), `period` (`day`, `night`, `cycle`, `session`, `week`; required for a summary) and `periodId` (the vendor's own day, cycle or session identity), `denominator` for a duration statistic (`per night`, `per day`), `source` (ref), `recordingMethod` (`automatic`, `active`, `manual`), `context` object (meal relation, body position, specimen source, measurement site), `components` (repeated object `{metric, value}` for blood pressure's two numbers under one acquisition), `origin` (ref to the mirror row), `derivedFrom` (repeated ref), `conversion` object (formula id, parameters, version), `revision` |
| `trace` | one metric from one source over one fixed period, packed | section 5.3 |
| `source` | a device, app, account or lab the data came from | `name`, `kind` (`watch`, `ring`, `band`, `scale`, `cuff`, `cgm`, `chest_strap`, `phone`, `app`, `lab`, `manual`), `account` (ref pinned to the `accountconfig` trait), `manufacturer`, `model`, `priorities` (keyed int by metric or metric group: the user's order per metric, because a chest strap wins for heart rate and a watch for steps) |
| `sleep` | one night or nap as a session | `at`, `endsAt`, `utcOffsetStart`, `utcOffsetEnd`, `isNap`, `dayAnchor` (`end`), `inBed` (repeated object `{at, endsAt}`: the occupancy layer), `stages` (repeated object `{at, endsAt, stage}` over the canonical set `awake, asleep_unspecified, light, deep, rem, unmeasured`, non-overlapping within one source; `inBed` and `stages` are separate layers, as HealthKit's `inBed` overlaps its stages), `stageMapping` (which vendor set and mapping version produced the stages), quantities for latency, time in bed, time asleep, efficiency, lowest and average HR, HRV, respiratory rate, temperature deviation, `source`, `origin`, `revision`, vendor scores as observations of vendor metrics |
| `workout` | one executed session | `sport` (canonical hierarchy value), `sportFacets` (indoor, outdoor, virtual, pool, open water, assisted), `sportSource` (verbatim upstream code and the mapping version), `at`, `endsAt`, `utcOffsetStart`, `utcOffsetEnd`, `elapsed`, `timer`, `moving` (durations), summary quantities (distance, ascent, energy, average and max HR, power, normalized power, cadence), `thresholds` object (FTP, LTHR, threshold pace, max HR, zone boundaries as of that day), `zoneTimes` (repeated object), `rpe`, `feel`, `notes`, `plan` (ref), `gear` (ref to `gear`), `file` (blobref: the FIT original), `source`, `origin`, `revision` |
| `lap` | one recorded boundary inside a workout | `workout` (ref, `onDelete: cascade`), `index`, `trigger`, `intensity`, `at`, `endsAt`, summary quantities, `planStep` (ref to `step`), `origin` |
| `set` | one strength set | `workout` (ref, cascade), `exercise` (ref), `index`, `setType` (`warmup`, `working`, `dropset`, `failure`, `rest`), `reps`, `load` (quantity, mass), `loadMode` (`external`, `bodyweight`, `assisted`, `machine`, `band`), `loadSource` (the vendor's reported load and unit, verbatim), `duration`, `distance`, `rpe`, `rir`, `side`, `superset`, `at`, `origin` |
| `plan` and `step` | a planned workout and its steps, FIT's `workout_step` | `sport`, `blocks` of steps with `durationType`, `targetType`, targets relative to a threshold, `repeat`; a workout points at its plan, and the reverse read (`referencing`) lists the executions |
| `gear` | a bike, a pair of shoes, a sensor | `name`, `kind`, `since`, `retiredAt`; distance is an aggregate over `workout.gear` |
| `exercise` | one strength exercise definition | `name`, `category` (movement pattern, FIT's list), `primaryMuscles`, `secondaryMuscles` (two-level: region and muscle), `equipment`, `codes` (repeated `{system, code, version, display}`: FIT category and name, wger id, free-exercise-db id), `licence` |
| `labreport` | one report from one lab: FHIR `DiagnosticReport` | `lab`, `accession`, `orderedAt`, `issuedAt`, `status` (state: `registered`, `preliminary`, `final`, `amended`, `corrected`, `cancelled`, `entered_in_error`), `file` (blobref: the PDF), `source`, `origin`, `review` (state: `pending`, `reviewed`; for OCR imports) |
| `labresult` | one analyte result: FHIR `Observation` | `report` (ref, cascade), `metric` (ref), `code` (`{system, code, version, display}` as received), `upstreamId`, `value` (quantity with comparator), `codedValue` (`{system, code, display}`), `ratio` (`{numerator, denominator}`), `text`, `absentReason` (exactly one of the five is set), `sourceUnit` and `sourceValue` (verbatim), `normalizationFailure`, `specimen` object (type, collectedAt, fasting, condition; per result, because one report can hold two specimens), `method`, `referenceRanges` (repeated object: low, high, text, appliesTo, age), `interpretation` (repeated, FHIR codes as received), `derivedFlag`, `at` (collection time), `status`, `provenance` object (page, row, confidence for OCR), `origin`, `revision` |
| `tag` | a journal event: alcohol, caffeine, illness, travel, menstrual flow, a note | `kind` (enum, growing), `at`, `endsAt`, `value` (quantity, optional), `note`, `source`, `origin` |

Four choices worth stating:

- **Generic plus typed.** Scalars are generic (`observation` and `trace` keyed
  by `metric`), because 70 wearable metrics and 240 markers as 310 kinds would
  be unmaintainable and a new metric should be a record, not an upgrade.
  Sessions are typed (`sleep`, `workout`, `labresult`), because their children
  and their fields are structural and filters need to name them. This is the
  FHIR and OMOP shape for values and the Health Connect shape for sessions.
- **The value's meaning lives on the value.** `form`, `unit`, `statistic` and
  `period` are on the observation or trace, so the engine never reads the
  metric record to interpret an indexed number and an edit to a metric cannot
  relabel history. The metric is the catalogue entry and the app's guide.
- **Vendor summaries are observations with `form: summary`**, keeping the
  vendor's own period identity (`periodId`: the WHOOP cycle id, the Oura day),
  so two WHOOP cycles on one date stay distinct and the aggregate read's
  `from: summaries` finds them.
- **Vendor scores stay vendor-named.** Readiness, recovery, strain, Body
  Battery and the ten sleep scores have no cross-vendor meaning; each is a
  `metric` whose `codes` name the vendor and whose `algorithmVersion` is
  recorded where the vendor exposes one, never mapped onto another's.

The shipped catalogue is data in the package: `metric` records for every row
of the wearable catalogue (heart, HRV, sleep, respiration, SpO2, temperature,
activity, body composition, glucose, blood pressure, ECG, menstrual, vendor
scores, VO2max, nutrition) and for the 240 lab markers with LOINC codes, units
and factors, each marked `verified` or `unverified` by the research's own
provenance tags, plus `exercise` records seeded from free-exercise-db (public
domain). A user adds a metric by writing a record.

Section 10 condenses the catalogue; the research files carry every row.

## 7. Canonicalisation: adapter packages with functions

A record mapping cannot do this work: it copies one source row to one target
row with identical datatypes, no constants, no arithmetic. mneme v6 reached the
same conclusion and wrote `whoophealth`, which writes three observations per
recovery, two per sleep and one per cycle, converts kJ to kcal and deletes a
reading when WHOOP retracts a score. The proposal generalises that:

- **One adapter package per provider** (`healthwhoop`, `healthoura`,
  `healthgoogle`, `healthwithings`, `healthdexcom`, `healthfit`,
  `healthapple`, `healthlabs`), each a sample that requires both the `health`
  package and its provider, holding one function on a record trigger for the
  provider's kinds with `permissions.reads` on those kinds and `writes` on
  the health kinds. The base `health` package names no provider kind, so it
  imports with no provider installed, and an adapter's grants resolve because
  its provider is present. The owner of the target declares the projection
  (the rule of decision 0049 applied to functions).
- **Every written row carries `origin`** (the mirror row), `source`, a
  `revision` taken from the upstream's `updated_at` or version where one
  exists, and an id composed from source data, never minted: `<provider>/
  <account record id>/<upstream object id>[/<role>]` for a row derived from
  one upstream object (`whoop/george/9f2k/recovery`, `whoop/george/9f2k/hrv`),
  and `<provider>/<account record id>/<metric>/<period start>` for a trace
  (`oura/george/heart_rate/2026-10-07T06`). Supplied ids admit `/`, `:` and
  `@` up to 128 characters, so the composite stays readable; only an upstream
  id outside the id alphabet or past the limit is hashed with
  `host.ids.external`, and then the hash replaces that one segment. A re-sync
  converges, a mirror that yields three observations yields three stable
  ids, and a stale delivery cannot overwrite a newer result.
- **Per-origin reconciliation.** A delivery computes the complete output set
  its mirror row implies, writes it, and deletes any earlier output of that
  origin that is no longer implied; a mirror delete delivers no record, so
  the function finds its outputs by `referencing` the origin and deletes
  them. Upstream retractions reach the mirror first (the WHOOP body does not
  delete today and must learn to).
- **A shared Python module** in the `health` package holds the unit table
  (generated from the engine's), the sleep-stage and sport mappings with
  their versions, the trace builder (fixed periods, splitting at offset
  changes, differencing counters into intervals), the local-day helper and
  the LOINC synonym table, so an adapter function is mostly field assignment.
- **Missing offsets** are filled, in order, from the same source's nearest
  record that carries one (a sleep or workout within a bounded window), else
  the source's last known offset, else the user's declared home zone, never
  zero and never the server's clock; the chosen rule is stamped on the row.
- **Conversions that need the metric** (mass to molar, HbA1c) read the metric
  record's `conversions`, write the formula id, parameters and version into
  `conversion` on the derived observation, and link it with `derivedFrom`.
- **Source priority is the user's**: `source.priorities` per metric, and the
  aggregate read's `sources` policy per request. The function never merges
  two sources into one row.
- **Freshness is reported separately** for the mirror (the sync trait) and
  for the canonical layer (the adapter's own sync record: last origin
  processed, backlog, parked deliveries), so a user can see that WHOOP synced
  an hour ago and the adapter is three deliveries behind.

Vendor-specific derived values (Oura's `equivalent_walking_distance`, which is
an energy equivalent, not distance; WHOOP's `kilojoule`) are mapped only where
the meaning is the same; otherwise they stay vendor metrics.

## 8. Providers worth writing, and two that are not

| Provider | Entry pattern | What it yields | Effort and blockers |
| --- | --- | --- | --- |
| WHOOP (exists) | cloud OAuth mirror | cycles, recovery, sleep, workouts | add the `healthwhoop` adapter; fix the drain deadline; add upstream deletion |
| Oura | cloud OAuth mirror plus webhook change signal | daily documents, sleep with 5-minute HRV and 30 s stages, `heartrate` | the richest open source of packed data; the 10-user cap until Oura approves the app |
| Google Health API | cloud OAuth mirror | heart rate at 1 to 5 s, steps, SpO2, sleep, exercise with TCX | restricted scopes need Google's review and the site says new projects are not being onboarded now; a delivery dependency, not a scope choice |
| Withings | cloud OAuth mirror | scale, blood pressure, sleep per minute, ECG | exact mantissa values; `lastupdate` is a true cursor |
| Dexcom and Libre | cloud OAuth mirror (Dexcom); unofficial client (Libre, best effort) | glucose every 5 min or 1 min | polling only; Libre has no official API |
| Polar, Ultrahuman, Suunto, COROS | cloud mirror | daily and sleep; workouts as FIT | partner approval needed for Suunto and COROS |
| Health Auto Export | push receiver (webhook trigger) | HealthKit metrics, workouts, sleep, ECG as JSON on a schedule | exists today in the engine; units follow the user's Health app settings, so the body must read `units` |
| FIT, TCX, GPX files | file import | every workout from Garmin, Wahoo, Zwift, Concept2, Polar, Strava's bulk export | blocked on `host.blobs.get`; use `fitdecode`; keep the original as `workout.file` |
| Apple `export.xml` | durable import job, streaming | the whole HealthKit history, including clinical FHIR records | multi-GB input, so it needs the import job of section 5.6 |
| Lab FHIR (Apple Health Records, SMART on FHIR, 1upHealth) | companion app or OAuth | `DiagnosticReport` and `Observation` with LOINC, UCUM, ranges and flags | US-centric; each health system is its own endpoint |
| Lab PDF | file import plus OCR | the only path for Function, Superpower, Medichecks, Randox, Everlywell | needs a review state; LOINC assignment from free text is fallible |
| Strong and Hevy CSV, Hevy API, Concept2 API, intervals.icu | file import or cloud mirror | strength sets, rowing, endurance analytics | Strong's CSV has no unit column; Hevy's API is declared unstable |
| On-device HealthKit, Health Connect, Samsung | companion app | everything, with change tokens | out of scope for the server; the push receiver is the bridge |

Not to be written:

- **A Strava API provider.** The 2026 API Policy caps retention at 7 days,
  forbids any persistent index (search indexes, archives, RAG stores named
  explicitly), aggregation, combination with other data, agent-mediated access
  and AI use, and requires deletions within 48 hours. An append-only changelog
  conflicts with every clause. The user's own bulk export (FIT originals plus
  CSV) is untouched by the policy and is the path.
- **A Garmin API provider**, until the developer program reopens (a dated
  observation from secondary reports; recheck when scheduling). FIT import
  and the Garmin push receiver (for existing partners) cover the data.

## 9. Decisions this plan would record

Each is hard to reverse, shapes what other code may do, and is argued above:

1. Every stored measurement carries its form (`instant`, `interval`,
   `counter`, `summary`), its unit, and for a summary its statistic and
   period; the engine reads meaning from the value, never from a metric
   record.
2. A `quantity` is an exact decimal and a UCUM code from an engine-owned
   subset table; conversion happens only within a dimension and only within a
   declared compatibility class, with exact arithmetic; temperature
   differences are their own dimension; mass to molar and method formulas are
   derivations stamped with their version.
3. A trace is a record of packed samples for one fixed period with a
   monotonic revision, and the `samples` index and rollups are derived from
   the fold like `refs` and `fts`, keyed by kind, form and offset.
4. Aggregation is a mode of the records read with four named time modes, a
   `from` choice between raw samples and summaries that never mixes them in
   one bucket, reducers legal per form, and budgets that refuse rather than
   truncate.
5. A mirror keeps the vendor's shape and units; a per-provider adapter package
   canonicalises in functions it owns, with `origin` and `revision` on every
   row and per-origin reconciliation.
6. Source priority resolves per stored interval, per metric, and the user
   owns it.
7. Strava API data is not mirrored; the bulk export is imported.
8. What deletion of health data means: a tombstone and purge remove the row,
   the index rows and the sealed material, and do not reach the segment files
   or blob bytes; the hosted profile requires encryption of the data root and
   a documented backup retention after which purged data is beyond use; a
   bounded operator erase that rewrites segments for purged ids and records
   the excision is either designed before any hosted deployment or ruled out
   in writing. Self-hosted single-user use is the GDPR household case; a
   hosted deployment is Article 9 processing by the operator, with consent at
   registration, erasure on request and a read-access log as hosted-profile
   features. FIT and PDF uploads are directly identifying and are named as
   such before import.

## 10. The catalogue, condensed

The canonical wearable metrics the `health` package ships, with the unit and
default form each `metric` record declares. Per-platform field names, cadences
and ranges are in [01-wearables.md](health-data/01-wearables.md), part B; the
disagreements are its part C.

| Domain | Metrics (canonical unit, default form) |
| --- | --- |
| heart | `heart_rate` (/min, instant), `resting_heart_rate` (/min, summary per night or day), `walking_heart_rate_average`, `heart_rate_recovery_1min`, `heart_rate_zone_time` (s per zone, interval), `rr_interval` (ms, instant), `irregular_rhythm_event` |
| HRV | `hrv_rmssd` (ms, instant; the window is part of the vendor metric), `hrv_sdnn` (ms), `hrv_rmssd_night` (ms, summary) |
| sleep | session kind `sleep`; metrics `sleep_latency`, `time_in_bed`, `time_asleep`, `deep`, `light`, `rem`, `awake`, `unmeasured` (s, interval per session), `sleep_efficiency` (%), `sleep_need`, `disturbances` (count), vendor sleep scores (one metric each, summary) |
| respiration | `respiratory_rate` (/min, instant), `breathing_disturbance_index`, `apnea_hypopnea_index` (/h) |
| oxygen | `spo2` (%, instant; Apple's fraction converted), `spo2_night_min` (summary), `perfusion_index` |
| temperature | `skin_temperature` (Cel, instant), `skin_temperature_deviation` (K, `delta: true`, instant; never merged with absolute), `core_temperature`, `basal_body_temperature` |
| activity | `steps` (count, interval), `step_cadence` (/min, instant), `distance` (m, interval), `floors` (count), `elevation_gain` (m), `active_energy` (kcal, interval), `total_energy`, `basal_energy` (kcal), `basal_metabolic_rate` (kcal/d, instant), `active_minutes`, `sedentary_time` (s), `met` (instant), `stand_hours` |
| workout streams | `speed` (m/s), `power` (W), `cadence` (/min; FIT strides doubled for running), `altitude` (m), `grade` (%), `latitude`, `longitude` (deg), `vertical_oscillation` (mm), `ground_contact_time` (ms), `stride_length` (mm), `left_right_balance` (%), `smo2` (%), `stroke_rate`; all instant |
| body | `weight` (kg), `height` (m), `bmi`, `body_fat` (%), `lean_mass`, `muscle_mass`, `bone_mass`, `body_water` (kg or %), `visceral_fat` (index), `waist` (m), `metabolic_age` (a); all instant |
| glucose | `glucose` (mg/dL canonical, mmol/L derived via molar mass; instant), `glucose_trend_rate` (mg/dL/min), `ketones` (mmol/L), CGM summaries as `summary` observations |
| blood pressure | `systolic`, `diastolic` (mm[Hg], instant, as components of one observation), `pulse_wave_velocity` (m/s), `vascular_age` (a) |
| ECG | `ecg` as a trace per channel at 250 to 512 Hz in uV with an exact `interval` (a 30 s strip is about 15,000 samples), `ecg_classification` (coded), QRS, PR, QT, QTc (ms) |
| reproductive | `menstrual_flow` (coded), `period` (tag with range), `ovulation_test` (coded), `cervical_mucus` (coded), `cycle_day`, `cycle_phase` (derived) |
| vendor scores | Oura readiness, sleep, activity, resilience, stress; WHOOP recovery, strain, sleep performance; Garmin Body Battery, stress, training readiness, HRV status; Polar Nightly Recharge, cardio load; Samsung energy score; Ultrahuman recovery, movement, metabolic; Apple rings and effort scores. One metric each with `vendor` and `algorithmVersion`, `form: summary`, never mapped |
| fitness | `vo2max` (mL/(kg.min); method as context), `ftp` (W), `lthr` (/min), `threshold_pace` (s/km), `fitness_age`, race predictions (vendor) |
| hydration and nutrition | `water` (mL, interval), `energy_consumed` (kcal), about 40 nutrients (g, mg, ug, interval), `caffeine`, `alcohol` (count) |
| environment | `time_in_daylight` (min), `environmental_audio_exposure` (dB; a logarithmic quantity whose `reducers` exclude `avg`), bed and room temperature |

Training metrics that are computed, not measured (NP, IF, TSS, hrTSS, rTSS,
TRIMP variants, CTL, ATL, TSB, ACWR, EF, decoupling, MMP curve, CP and W',
W'bal, e1RM, volume load, SWOLF, polarization index) are catalogued with
their inputs and formulas in [02-workouts.md](health-data/02-workouts.md),
section 5. They become observations with `derivedFrom` pointing at the
workout, computed by a function at ingest, with the thresholds of the day
stored on the workout so later edits do not rewrite them.

The lab catalogue ([03-biomarkers.md](health-data/03-biomarkers.md), part 3)
covers CBC, CMP and BMP, liver and pancreas, lipids and ApoB, Lp(a) and NMR
subfractions, glucose, insulin, HbA1c and HOMA-IR, thyroid, iron, vitamins,
minerals, heavy metals, male and female hormones, adrenal, pituitary and
growth, inflammation, kidney, cardiac and coagulation, urinalysis, coeliac,
allergy IgE and food IgG, fatty acids, infectious disease and blood group,
genetic markers, and ageing composites, epigenetic clocks and microbiome.
Genetic raw files (600,000 rows) and microbiome profiles (hundreds of taxa) are
blobs with a few derived observations, not catalogue rows.

## 11. Phasing

The first draft claimed a phase with no engine change; the reviews showed it
depended on the `quantity` type. The revised order starts small and proves
the contract on one provider and one consumer before any dense data:

| Phase | Builds | Engine change | Proves |
| --- | --- | --- | --- |
| 1 | `quantity`; the `health` package reduced to `metric`, `source`, `observation`, `sleep` and a small catalogue; the `healthwhoop` adapter ported from mneme v6 with reconciliation and revision; the shared Python module; the privacy statement of decision 8 | medium | a monthly sleep view or agent answer from 30 WHOOP rows; identical re-sync changes nothing; a rescored or deleted WHOOP record converges; two cycles on one date stay distinct; rebuild and export reproduce the data; an app consumes it without WHOOP field names |
| 2 | the `samples` trait, `trace`, the `samples` and `sample_rollups` tables with their lifecycle hooks and RLS, the aggregate mode with plain buckets, typed reducers, coverage, budgets and `from`; typed expression indexes; the WHOOP deadline fix | medium to high | one dense instant metric from one source: a bounded raw aggregate equals its rollup, including a partial boundary, a correction, a deletion and a rebuild; full write cost and query work measured on the real tables before the index layout is frozen |
| 3 | interval amounts and source policy; session windows; rolling and fill; `downsample`; `labreport` and `labresult` with the comparator truth table; `workout`, `lap`, `set`, `plan`, `gear`, `exercise`; `host.blobs.get` and `put`; the durable import job | medium | steps from two sources total the same at any bucket width; a lab trend across two labs refuses to pool incompatible codes |
| 4 | Oura, Google Health, Withings, Dexcom, Health Auto Export, FIT import, Apple export import, lab FHIR and PDF import; the full catalogue; TimescaleDB as an optional backend; percentile sketches; retention | small each | the catalogue populated from real sources |

Cut from the first delivery, each kept as its own follow-up with a consumer
and acceptance checks: the 240-marker catalogue, the exercise catalogue,
source merging, session windows, rolling, fill, percentiles, M4, TimescaleDB,
PDF OCR and multi-GB imports.

## 12. Open questions for the owner

1. `samples` as a core trait with three variants (recommended) or a kind-level
   declaration key naming the roles?
2. Canonical glucose unit: mg/dL (Dexcom, Google, three aggregators) or
   mmol/L (Health Connect, Junction, most of the world's labs)? The metric
   record decides and either is derivable; the catalogue proposes mg/dL for
   CGM metrics and the LOINC-coded pair for labs.
3. The default `from` for an aggregate when a metric has both raw samples and
   vendor summaries: `raw` (recommended, with `prefer-raw` as the explicit
   fallback) or `prefer-raw`?
4. Whether the aggregate read returns dense buckets by default or only on
   `fill` (Health Connect and Google return sparse rows; EWMA callers want
   zeros). Sparse by default is proposed.
5. One base `health` sample plus one adapter sample per provider (proposed),
   or adapters shipped inside each provider bundle under the provider's
   authority?
6. The deletion decision's hard half: design the bounded operator erase now,
   or document "beyond use" retention and defer the erase until a hosted
   deployment is planned?
7. Whether to adopt one of the young Go UCUM libraries (`gofhir/ucum`,
   `mattwiller/mu`, both BSD, both with exact `big.Rat` paths and no known
   importers) or hand-write the closed table and pin it against the UCUM
   conformance cases. The table is recommended for the first 100 codes.
8. The trace period width per metric class: one hour for continuous metrics
   and the session for workout streams (proposed), or a declared width on the
   metric?
