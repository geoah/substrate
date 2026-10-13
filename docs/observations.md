# Observations

An observation is a measured value: a weighing, an hour of heart rate, a
nightly heart rate variability, a vendor's recovery score, an LDL result from
a blood test. This page covers what holds one today: core's `observed` trait,
the rules every write of an observed record is held to, the managed `stats`
of a trace, the `observations` and `health` samples, the host SDK helpers
a function builds observed values with, the observed index the fold derives
from them, and the aggregate read that answers "average heart rate per week"
from that index ([reading](#reading)). The plan is
[observations](plans/observations.md).

## The observed trait

`substrate.reamde.dev/core/observed` is a [trait](traits.md) with two
variants. A kind binds `observed(single)` when each record holds one value,
and `observed(packed)` when each record holds many values of one metric over
one fixed period (a trace). Binding it does two things: every write of the
kind is held to the rules below, and the observed index reads the kind
without knowing it by name. A kind that binds `observed` should bind
`temporal(range)` beside it, so the window read and every timeline query
cover its records.

The property list is complete from the first version and never grows: a
property added later would refuse every imported kind that binds the trait at
the next boot, so a later need is a second trait bound beside this one
([decision 0154](decisions/0154-an-observed-value-binds-the-core-observed-trait-which-never-gains-a-property.md)).
`TestObservedTraitContractIsPinned` holds the list.

Properties of both variants:

| Property | Datatype | Means |
| --- | --- | --- |
| `at` | datetime | when the value was observed, or the start of its span |
| `endsAt` | datetime | the end of the span `[at, endsAt)`; required on an amount, a summary and every trace |
| `metric` | reference | what was measured, a `metric` record |
| `source` | reference | the device, app, service or lab that produced the value |
| `about` | reference | the thing observed, any kind; absent means the repository's owner ([decision 0155](decisions/0155-an-observed-value-names-what-it-is-about-with-an-about-reference.md)) |
| `channel` | string | which of a source's channels this is, where one source measures one metric twice |
| `form` | string | `gauge`, `amount`, `counter` or `summary` ([below](#forms)) |
| `hold` | duration | on a gauge, how long a value stands without a newer one |
| `resetRule` | string | on a counter, its contract: `monotonic`, `signed` or `wrap` |
| `resetAt` | datetime | on a counter, when the current accumulation began |
| `delta` | bool | true when the value is a temperature difference, such as a deviation |
| `circular` | bool | true when the value is an angle averaged on the circle, such as a wind direction |
| `wrapAt` | decimal | on a `wrap` counter, the value it wraps to zero at |
| `revision` | int | rises with each rewrite from the upstream; a lower one is refused |

`observed(single)` adds:

| Property | Datatype | Means |
| --- | --- | --- |
| `value` | quantity | the number and its unit, `{value: "72.4", unit: kg}` ([units](units.md)) |
| `code` | string | a categorical value instead of a number: `open`, `rem`, `not-detected` |
| `statistic` | string | on a summary, which statistic it is: `avg`, `max`, `vendor` for a vendor's own method |
| `period` | string | on a summary, the period it covers: `day`, `night`, `cycle` |
| `utcOffset` | int | minutes east of UTC where the value was observed |

`observed(packed)` adds:

| Property | Datatype | Means |
| --- | --- | --- |
| `unit` | string | the UCUM code every value is in; a trace of codes has none |
| `every` | duration | the spacing of regular points from `at` |
| `offsets` | int, repeated | each point's milliseconds from `at`, for irregular points |
| `values` | decimal, repeated | the numbers, exact decimal strings; `null` means unavailable from that point |
| `codes` | string, repeated | the codes of a categorical trace; `null` means unavailable from that point |
| `utcOffsetStart`, `utcOffsetEnd` | int | minutes east of UTC at the start and the end of the period |
| `stats` | json | computed by the engine on every write ([below](#stats)) |

A kind binding the trait declares every one of these properties itself, with
its own descriptions, `repeated: true` on the three lists and its own pins on
the references. The trait holds their names and datatypes, and the loader
holds their shapes: a binding kind whose `offsets`, `values` or `codes` is
not a list, or whose other properties are lists or keyed maps, is refused,
because the rules and the index read one value from each of those.

## Forms

The form says what a number means, which decides how it may be added up or
averaged ([decision 0156](decisions/0156-the-forms-are-gauge-amount-counter-and-summary-and-live-on-the-value.md)).
It lives on the value, never on the metric, so editing a metric cannot
relabel a stored value.

| Form | Means | Example |
| --- | --- | --- |
| `gauge` | a level at an instant, held until its `hold` ends, its `endsAt`, or the next point | 62 /min, 72.4 kg, a door `open` |
| `amount` | a quantity accrued over `[at, endsAt)` | 412 steps in a quarter hour, 94 minutes of `rem` |
| `counter` | a running total under one of three contracts ([decision 0157](decisions/0157-a-counter-is-stored-as-sent-and-differenced-at-read-under-one-of-three-contracts.md)) | a meter at 18342.655 kW.h |
| `summary` | a statistic over a stated period, computed by the source | WHOOP's recovery of 67 % for one cycle |

A categorical value is a gauge with `code` set instead of `value`, or an
amount with `code` beside `value`: the amount is the quantity and the code its
category, so 94 minutes of `rem` is `{value: {value: "94", unit: min}, code:
rem}`. A boolean is two named codes, never `0` and `1`. A summary is always
one value per record: a trace is never a summary. A `null` inside a trace
means the source had nothing from that point on: it ends a gauge's hold and
counts in nothing
([decision 0158](decisions/0158-a-gauge-carries-a-hold-a-null-ends-it-and-its-mean-is-time-weighted.md)).

## The base kinds

The `observations` sample (`samples.substrate.reamde.dev/observations`) holds
the four kinds every observing domain shares, and a repository imports it
under its own authority
([decision 0159](decisions/0159-the-base-observation-kinds-live-in-a-sample-every-domain-sample-requires.md)).
Their names are frozen: a sample's kind cannot be moved by an upgrade, and
`TestObservationsSampleKindNamesAreFrozen` holds them.

| Kind | Binds | One record is |
| --- | --- | --- |
| `metric` | | something that can be measured: its names, codes and how its values are best read |
| `observation` | `temporal(range)`, `observed(single)` | one measured value |
| `trace` | `temporal(range)`, `observed(packed)` | the values of one metric from one source over one fixed period |
| `source` | | the device, app, service or lab a value came from |

**`metric`** is a catalogue entry and a guide for writers. The write rules
and the observed index read nothing from it: each value carries its own form,
unit and hold. The [aggregate read](api.md#the-aggregate-read) takes two of
its properties: `measures`, the measure every value of the read converts
into (a stream whose unit is outside it is a `422`), and `unit`, the unit the
read answers in when the request names none. Editing either changes every
aggregate answer for the metric and no stored value. `name` is its heading
and `description` its prose; `codes`
lists it in other code systems (`{system, code, version, display}`, `system`
being `http://loinc.org`, `healthkit`, `healthconnect`, `googlefit`, `whoop`,
`garmin`, `withings`, `homeassistant` or `cf`); `measures` is the measure
from the [unit table](units.md) its values belong to, and `unit` the code
writers store; `codeSet` lists a categorical metric's codes; `form` and
`hold` are the defaults a writer copies onto each value; `reducers` names the
reducers that answer something useful; `compatibility` names the class values
pool within where the measure alone is not enough, such as serum against
whole-blood glucose, and nothing in the engine reads it; `displayPrecision`
is the decimal places a reader shows; `category` groups it for navigation;
`algorithm` (`{vendor, name, version}`) names a vendor's scoring method. The sample ships
six metrics several domains share: `air_temperature`, `relative_humidity`,
`energy`, `power`, `illuminance` and `atmospheric_pressure`.

**`observation`** declares the trait's single properties and adds:
`periodId`, the source's own identity for a summary's period (a WHOOP cycle
id), so two cycles on one date stay two records; `denominator`, what a
duration statistic is per; `qualifiers`, coded context (`{system, code}`, such
as after a meal); `components`, the parts of a value measured together
(`{metric, value}`); `sourceValue`, `sourceUnit` and `normalizationFailure`,
the reading as received when it could not be normalized; `absentReason`, why
there is no value; `quality` (`valid`, `estimated`, `questionable`, `stale`,
`validated`, `unavailable`); `issuedAt`, when the value became known;
`recordingMethod` (`automatic`, `active`, `manual`); `origin`, the provider
record that delivered it; `adapter`, the code that wrote it from that record
([the adapters](#the-adapters)); `derivedFrom` and `conversion`, for a value computed
from another. It titles itself `{metric} {value}`, "Body weight 72.40 kg".

**`trace`** declares the trait's packed properties and adds `acquisition`
(the recording a trace is one channel of), `quality`, `origins` (the
provider records the period was built from) and `adapter`. One trace covers one metric,
source, `about`, `channel` and period, from one origin provider, and its id is
composed from them: `<provider>/<account record id>/<source>/<metric>[/<channel>]/<period start>`,
where `<source>` names the device within the account, so two devices on one
account never share an id
([decision 0162](decisions/0162-an-observed-row-names-its-origin-and-source-and-composes-its-id-from-upstream-data.md)).

**`source`** is the device, never the delivery: `name`, `class` (`watch`,
`ring`, `scale`, `thermometer`, `meter`, `contact`, `camera`, `app`,
`service`, `lab`, `manual`), `manufacturer`, `hardwareModel`, `firmware`,
`resolution` (the smallest step it reports, a quantity), `about` (the thing it
is fixed to, which writers copy onto each value), `account` (the connected
account it syncs through, any kind binding `accountconfig`) and `priorities`
(its rank per metric id when sources overlap, 1 first). The aggregate read's
`priority` and `summaries` policies take `priorities` as their order when a
request names none, an unranked source last; a request's `sources.order`
wins.

## Writing an observation by hand

Import the samples first, `observations` and then `health`, which requires
it:

```bash
bin/substratectl import samples.substrate.reamde.dev/observations
bin/substratectl import samples.substrate.reamde.dev/health
```

A weighing entered by hand, in a repository whose authority is
`ada.example.com`:

```yaml
kind: ada.example.com/observations/observation
metadata:
  id: manual/2026-10-09-weight
data:
  properties:
    metric:
      ref: ada.example.com/observations/metric/health/body_mass
    source:
      ref: ada.example.com/observations/source/manual
    form: gauge
    value:
      value: "72.1"
      unit: kg
    at: 2026-10-09T07:02:00+03:00
    utcOffset: 180
```

`bin/substratectl apply -f weight.yaml` writes it. The `metric` must exist
(`mustExist: true`); the `source` need not, so a writer may create it after.

## Writing a trace

An hour of heart rate at irregular times, with one unavailable reading:

```yaml
kind: ada.example.com/observations/trace
metadata:
  id: garmin/ada/forerunner265/heart_rate/2026-10-07T06
data:
  properties:
    metric:
      ref: ada.example.com/observations/metric/health/heart_rate
    source:
      ref: ada.example.com/observations/source/garmin/ada/forerunner265
    form: gauge
    unit: /min
    hold: PT4M
    at: 2026-10-07T06:00:00Z
    endsAt: 2026-10-07T07:00:00Z
    utcOffsetStart: 180
    utcOffsetEnd: 180
    offsets:
      - 0
      - 118000
      - 241000
    values:
      - "58"
      - null
      - "61"
    revision: 1
```

Regular points take `every` instead of `offsets`: `every: PT5S` with 720
values covers the hour. A trace of codes (sleep stages) takes `codes` instead
of `values` and has no `unit`.

## The rules the substrate refuses

Every write of a kind binding `observed`, a patch included, is held to these
rules after the write is merged into the stored record. A refusal is a `422`
whose problems name the property and the rule, such as
`props.hold: only a gauge holds its value; a counter does not`.

- `form` is required and is `gauge`, `amount`, `counter` or `summary`;
  `metric` and `at` are required.
- A single record holds `value` or `code`, never both, except an amount,
  which may carry its category in `code` beside `value`. It may hold neither
  only where `sourceValue` keeps the reading as received or `absentReason`
  says why there is none, and `absentReason` beside a value is refused.
- An `amount` and a `summary` need `endsAt` after `at`. A `summary` needs
  `statistic` and `period`, and nothing else states either.
- `hold` goes only on a gauge and is never negative. `resetRule` goes only on
  a counter. `resetAt` needs a `resetRule`. `wrapAt` goes with `resetRule:
  wrap` and `wrap` needs it, above zero.
- A counter is a number, never a code.
- `delta: true` needs a unit whose measure is `temperature`, and
  `circular: true` one whose measure is `angle`.
- `utcOffset`, `utcOffsetStart` and `utcOffsetEnd` are minutes within 1080
  either way.
- `revision` never falls: a rewrite with a lower one than the stored one is
  refused as a stale delivery, an equal one is admitted, and a stored one is
  never removed. A create, and a put onto a deleted record, start afresh. A
  kind that binds core's `revised` trait instead is held to this rule alone.
- A `comparator` on `value` (`<0.01`) is admitted only where the kind
  declares `comparator: true` on it, as for any quantity.

A packed record is also held to these:

- It holds `values` or `codes`, never both, except an amount trace, which
  may carry one code per value; at least one point and at most
  20,000, and its properties are at most 1 MiB as JSON. A longer recording is
  split into shorter periods.
- It places points by `every` or by `offsets`, never both. `offsets` are
  integer milliseconds, one per point, never decreasing, inside
  `[0, endsAt - at)`. With `every`, the count of points times `every` fits
  the period.
- A trace of values names its `unit`, a code the unit table reads; an alias
  such as `°C` is stored as its code, `Cel`. A trace of codes has no unit.
- A `summary` is refused: it is one value per record.
- `null` is admitted inside `values` and `codes` and nowhere else.
- A code is never empty: `""` inside `codes` is refused naming its index
  (`props.codes[1]: a code is not empty`), and so is `code: ""` on a single
  record. `null` is the one way to say a point has nothing.

A kind that declares any of the result shapes `codedValue`, `ratio` and
`text` beside the trait, as the health sample's `labresult` does, holds
exactly one of `value`, the coded result (`codedValue`, with `code` as its
index key), `ratio`, `text` and `absentReason` on every write; with none of
them, `sourceValue` must keep the reading as received.

## Stats

A trace's `stats` is the engine's. On every write it is computed from the
points, and a `stats` the writer sends is dropped rather than refused, so a
function that writes back a trace it read never trips over it. `null` points
count in nothing.

```yaml
stats:
  count: 3
  min: "57"
  max: "61"
  mean: 58.666666666666664
  m2: 8.666666666666666
  first:
    at: 2026-10-07T06:00:00Z
    value: "58"
  last:
    at: 2026-10-07T06:06:02Z
    value: "57"
  coverage: PT6M2S
```

`min` and `max` are the stored digits. `mean` and `m2` (the sum of squared
deviations from the mean, so the variance is `m2 / count`) are computed
exactly and stored as the nearest float64. `first` and `last` are the first
and last available points, and `coverage` is the duration between them. A
trace of codes carries `count`, `first`, `last` and `coverage`, with each
code as the `value`. The same properties always produce the same `stats`, so
a rewrite that changes no point writes nothing.

## What the health sample adds

`samples.substrate.reamde.dev/health` requires `observations` with a
`requiresAtLeast` floor (`TestHealthRequiresObservations`) and adds five
kinds, a catalogue, the lab analytes and one function. `sleep`, `workout` and `labreport` carry `origin` and
`revision` as the base kinds do, and `sleep` and `workout` carry `adapter`,
because an adapter writes them too. The three bind core's `revised` trait, so
a rewrite with a lower `revision` is refused on them as it is on an
observation.

| Kind | One record is |
| --- | --- |
| `analyte` | the lab facts about one blood test metric: its LOINC parts, `molarMass` in grams per mole, the `conversions` a function may write, and a `fallbackRange` |
| `sleep` | one night or nap: the span, `timeInBed`, `timeAsleep`, the stage durations `awake`, `light`, `deep` and `rem`, `latency`, `efficiency`, `respiratoryRate`, `disturbances` and the device's `scores` by their own names |
| `workout` | one session: `sport`, one code set every adapter maps onto (`running`, `walking`, `hiking`, `cycling`, `swimming`, `strength`, `yoga`, `rowing`, `hiit` and more, `other` for a sport with no value of its own) with the provider's own word in `sportSource`; the span, `duration`, `distance`, `activeEnergy` (above the resting rate) and `totalEnergy` (resting and active together), `averageHeartRate`, `maxHeartRate`, `elevationGain`, `strain`, `zoneDurations` by zone name, and the device's original `file` as a blob |
| `labreport` | one report from a lab: `lab`, `accession`, `orderedAt`, `issuedAt`, `status`, the original `file` and the owner's `review` |
| `labresult` | one analyte's result, binding `observed(single)`: `report` (deleted with it), `coding` (the analyte's code as the lab sent it), the result shapes, `specimen`, `method`, `referenceRanges` (each bound a quantity, `{low, high, text, appliesTo, age}`), `interpretation`, `status`, `provenance`, and for a derived result `derivedFrom` and `conversion` |

The catalogue (`samples/health/metrics.yaml`) is 61 `metric` records with ids
`health/<name>`: the heart, sleep, activity, body, blood pressure, glucose
and fitness metrics the Apple Health, WHOOP, Garmin and Google Fit adapters
write, the vendor scores (`whoop_recovery`, `whoop_strain`,
`whoop_sleep_performance`, `garmin_stress`, `garmin_body_battery`), and the
common labs. A lab analyte reported in both unit systems is two metrics, the
amount concentration unsuffixed (`ldl_cholesterol`, mmol/L) and the mass
concentration suffixed `_mass` (`ldl_cholesterol_mass`, mg/dL), because the
two are different measures and a read of one in the other's unit is refused.
HbA1c is `hba1c` (NGSP %) and `hba1c_ifcc` (IFCC mmol/mol), which relate
by a formula, not a unit.
The codes were written from the vendors' published documentation without a
check against a live account; a metric's description marks the least
certain ones.

### Lab results in both unit systems

The sample ships an `analyte` record for each metric of the nine lab pairs
(`samples/health/metrics_analytes.yaml`, ids the metric's): the LOINC parts
of the metric's code, the analyte's `molarMass` in grams per mole, and one
conversion onto the other metric of the pair. A mass and an amount
concentration convert by the molar mass (`molar-mass`: cholesterol 386.65,
glucose 180.156, creatinine 113.12, 25-hydroxyvitamin D 400.64, and 885.7 for
triglycerides by the clinical convention); HbA1c converts by the IFCC and
NGSP master equation, IFCC mmol/mol = (NGSP % - 2.15) x 10.929
(`ngsp-to-ifcc` and `ifcc-to-ngsp`).

The sample's function `convertlab`, fired by its trigger `health-on-labresult`
on every write of a lab result, writes the result's value in the paired
metric as a `labresult` of its own, with id `<input id>/as/<target metric
id>`. The derived result repeats the input's report, source, time, specimen,
status, flags and `revision`, converts its reference ranges with it, and
names the input in `derivedFrom` and the formula in `conversion` (`{formula,
parameters, version}`). Its value keeps one significant digit more than the
input's: 130 mg/dL of LDL is 3.362 mmol/L. A derived result is never
converted again, a corrected input rewrites its derivations, and a deleted
input takes them with it. A read of one metric counts each reading once, the
lab's own or the derived one; a read that names both metrics, or groups the
latest values by metric, shows both. A result written before the sample was
imported is converted by replaying the trigger from 0. Meter and continuous
monitor readings of glucose are observations, not lab results, and are not
converted.

Every gauge metric but `height` and `vo2_max` carries a `hold`, which a
writer copies onto each value: about twice the time between a source's
readings for a sampled stream (`PT20M` for SpO2, `PT10S` for a workout's
speed), a day for a weighing or a nightly reading, and `PT0S` for a lab
value and a blood pressure, which are points. A gauge value written without
a hold stands until the next value of its stream, often a day later, and the
coverage, the time-weighted mean and the priority policy all read it as
still standing ([decision 0158](decisions/0158-a-gauge-carries-a-hold-a-null-ends-it-and-its-mean-is-time-weighted.md)).

## The adapters

An adapter is a sample whose function turns one provider's mirror rows into
these records, each naming the mirror row it came from in `origin` and
rewritten or deleted with it. Four ship, each with its mapping and rules in
the catalog: [WHOOP to health](bundles-catalog.md#whoop-to-health-sample)
(`healthwhoop`), [Apple Health to
health](bundles-catalog.md#apple-health-to-health-sample) (`healthapple`),
[Garmin to health](bundles-catalog.md#garmin-to-health-sample)
(`healthgarmin`) and [Google Fit to
health](bundles-catalog.md#google-fit-to-health-sample) (`healthgooglefit`).
The Apple Health adapter's trace ids carry the source,
`applehealth/<account>/<source>/<metric>/<UTC hour>`, because one phone
delivers several devices' data.

Every output also names the code that wrote it, in `adapter`:
`<package>@<version>` (`healthwhoop@1`), a constant at the top of the
adapter's function. The base kinds and `health/sleep` and `health/workout`
declare it as their own property, apart from `revision`, which is the
upstream's
([decision 0162](decisions/0162-an-observed-row-names-its-origin-and-source-and-composes-its-id-from-upstream-data.md)).
An adapter upgrade does not reach mirror rows the upstream never changes on
its own. To reprocess them, raise the constant with the change, apply the
new closure, and replay the adapter's record trigger from the start:

```bash
bin/substratectl trigger replay healthwhoop-on-mirror --from 0
```

Every mirror row is delivered again and every output is rewritten under the
new `adapter` at the `revision` it already holds, which the write path admits
because an equal revision is not a stale one. An output whose code is
unchanged is not rewritten.

## Building observed values in a function

`host.units.convert`, `host.units.canonical` and `host.units.measure` ask the
engine's unit table over the host call. `host.observed.trace` packs a body's
own timestamped points into trace documents that pass the rules above, one
per fixed UTC period, split where the UTC offset changes and before a
document passes the size limits, narrowed to the span a delivery covers when
the body names one; `period_id` gives
the label a trace id ends with and `local_day` the civil date at an offset.
[Functions](functions.md#the-sdk) has the signatures and an example.

## The observed index

The fold derives three tables from the rows of every kind that binds
`observed`, the way it derives `refs`
([decision 0160](decisions/0160-the-observed-index-is-three-derived-tables-and-a-rollup-serves-only-exact-answers.md),
migration `0012_observed_index`). A point is a function of one record's
properties and its kind's binding; a rollup row and a latest row are
functions of a stream's points, and a counter's rollup row also reads the
stream's reading before its bucket, which may be another record's
([below](#counters)). Nothing else enters: no clock, no metric record.
`repository rebuild` clears the three and derives them again from the
changelog; `repository verify` and the export never read them.

A **stream** is one run of readings: the `kind`, `metric`, `source`, `about`
and `channel` of a value. A summary's `statistic` splits a stream's latest
row: a night's average and its maximum at one instant keep a latest row
each. A reference column holds the target's record path
(`ada.example.com/observations/metric/health/heart_rate`); an absent
`source`, `about`, `channel` or `statistic` is the empty string.

| Table | One row per | Holds |
| --- | --- | --- |
| `observed_points` | value: one per observation, one per point of a trace | the stream, `form`, `ts` and `ts_end`, `utc_offset_min`, `offset_known`, `unit`, `delta`, `value` (float8), `value_exact` (the stored digits), `value_base` (in the measure's base unit), `code`, `statistic`, `bounded`, `quality`, and the parameters a hold and a counter step read: `hold_ms`, `reset_rule`, `reset_at`, `wrap_at`, `circular`, `revision` |
| `observed_rollups` | stream, form, 15-minute bucket and group (`code`, `unit`, `delta`, `utc_offset_min`, `offset_known`) | `n`, `bounded`, `sum`, `min`, `max`, `mean`, `m2`, `first_ts`, `first_value`, `last_ts`, `last_value`, `covered_ms`, `held_ms`, `twa_sum`, `last_hold_ms`, `max_gap_ms`, `start_gap_ms`, `increase`, `resets`, `sin_sum`, `cos_sum`, `span_end`, `first_record_id`, `first_ord`, `last_record_id`, `last_ord`, `min_value`, `max_value` |
| `observed_latest` | stream and `statistic` | its newest available point: `ts`, `ts_end`, `value`, `code`, `unit`, `delta`, `form`, `statistic`, `bounded`, `hold_ms`, `utc_offset_min`, `offset_known`, `record_id`, `revision`, and `points`, the row's count of available points |

The rules every column follows are written once, as Go, in
`internal/observed`: the engine computes each rollup row with it, and the
aggregate read will be held to it.

### When rows are written

Every write that changes a record of an observed kind deletes the record's
points and inserts the ones its row now derives, in the write's transaction.
A tombstone deletes them (a tombstone keeps its `refs` rows, and loses its
points: a deleted reading is in no aggregate); a put onto the tombstone
derives them again; a purge leaves nothing. At the end of the transaction the
rollup rows of every bucket the old and the new points touched are computed
again from `observed_points`, once, and so is every latest row (a stream and
a statistic) they touched. A rebuild and a boot import compute every rollup and latest row
after the replay. Either reads a bucket's points in pages of at most 20,000,
in `(ts, record_id, ord)` order, into a running state
(`internal/observed.BucketBuilder`), so a dense bucket is never held in
memory whole; a write still reads every point of each bucket it touches.
That read is a known limit: it runs under the repository's observed lock,
and writing N traces into one bucket reads about N squared over 2 traces'
worth of points in all ([the benchmark](plans/observations/implementation/benchmark.md#a-dense-bucket)).

A single record derives a point only when it holds a `value` or a `code`;
one that says why it has none (`absentReason`) or keeps the reading as
received (`sourceValue`) derives nothing, and neither does a lab result
holding only `ratio` or `text`. A summary derives points and no rollup row:
summaries are never pooled with the other forms.

A kind that starts or stops binding `observed`, or changes the measure its
`value` declares, has every row's points derived again by the background
pass behind the commit that changed it, the one that re-derives the search
index ([operations](operations.md#upgrading-the-binary)); until the pass
reaches a row, its points are the ones the previous declaration derived. A
tombstone and a purge in that window still take the record's points with
them, whatever the kind now declares: a purged row is gone before the pass
could find it.

`observed_index` records the version of the rules the tables were derived
by (`observedIndexVersion` in `internal/engine/observedindex.go`). An open
whose binary derives by newer rules, or finds none recorded (a repository
written before the tables existed), asks the same pass to derive every row
of every observed kind again and records its own version. A rebuild and a
boot import derive the tables whole and record theirs, so the open after one
asks for nothing. A change to the derivation, the rollup statistics or the
latest rule that changes what a stored row derives bumps the version.

### Points, nulls and offsets

A trace's point is at `at` plus its offset (or its index times `every`). A
gauge's and a counter's point is an instant, `ts_end` equal to `ts`; an
amount's spans to the next point of the trace, to `ts` plus `every` with
regular spacing, and to `endsAt` for the last. A single record's point spans
`[at, endsAt)` where it has an `endsAt`, and a single gauge's value holds no
longer than that: its `hold_ms` is the shorter of its `hold` and `endsAt`
minus `at`, so a door `open` at 10:00 with `endsAt` 10:05 and no `hold` is
open for five minutes, not until the next point.

A `null` in `values` or `codes` is a point with `value` null, `code` empty
and `quality` `unavailable`. It ends the hold of the point before it and
counts in nothing: not in `n`, not in `first` or `last`, not in the latest
row. A counter's steps skip it, because an outage between two readings of a
running total still accrued.

A point is available when it holds a value or a code that is not empty, and
unavailable otherwise. That one test decides every count: the points an
insertion adds to a stream, the ones a deletion takes back, a trace's
`stats`, the rollup rows, the latest row and a rebuild. An empty code a row
stored before the write refused it is therefore an unavailable point, the
same as a `null`.

A point's `utc_offset_min` is a single record's `utcOffset`. A trace's point
takes `utcOffsetStart` when it lies before the midpoint of the period and
`utcOffsetEnd` from the midpoint on; a trace naming one of the two uses it
for every point. An absent offset is 0, so a value written without one falls
on its UTC day, and its `offset_known` is false: the point, its rollup row
and its stream's latest row say the offset was never recorded, which a
reader tells apart from a value written at offset 0.

`value_base` is the value in its measure's base unit, in
`temperature-difference` where `delta` is true (0.3 `Cel` is 0.3 `K`), and
null for an arbitrary unit, a level and `rad`, which convert to nothing.
`bounded` is true where the value carries a comparator (a lab's `<0.01`).

Points at one instant are ordered by record id, compared byte by byte (the
record id columns sort `COLLATE "C"`), then by their index in the record.
Every reader uses that order: a rollup row's first and last point, a raw
read and the latest row pick the same point of a tie.

### Rollup rows

A rollup row covers one 15-minute bucket aligned on UTC, which every offset
in use divides, so a civil day at a point's own offset is a run of whole
rows. Within a bucket a stream's points of one form split into **groups** by
`code`, `unit`, `delta`, `utc_offset_min` and `offset_known`, and each group
is one row. The statistics are in the row's own `unit`, so two units with no
common base never add; a reader converts each row to the unit it asks for.

- A gauge's held time is built from every point of the bucket, whatever its
  code: each available point holds until the next point (an unavailable one
  included), its own `hold`, or the bucket's end, whichever comes first, and
  the time is kept on the row of the point that held. A door's `closed`
  therefore ends the `open` before it. `held_ms` is that time and `twa_sum`
  the sum of value times held milliseconds, so a row's time-weighted mean is
  `twa_sum / held_ms`. The value the bucket's last point holds past the
  bucket's end is the read's to carry: `last_hold_ms` says how long after
  `last_ts` it may hold (null for until the next point) and the next row's
  `start_gap_ms` says where that next point is.
- `max_gap_ms` is the bucket's largest gap between consecutive points, from
  the bucket's start to its first point and from its last point to its end
  included. **A bucket may answer `twa` and `timeInState` from `held_ms` only
  when the request's `maxGap` is absent or not below the bucket's
  `max_gap_ms`**; otherwise no hold inside the bucket is guaranteed to be the
  request's, and the read takes the bucket's raw points.
- An amount belongs whole to the bucket its span starts in; `covered_ms` is
  the union of its row's spans and `span_end` the end of the latest of them,
  so the union lies inside `[first_ts, span_end)`. A read joining two rows
  adds their `covered_ms` only where those extents do not overlap
  (`internal/observed.UnionMs`).
- A bucket holding only unavailable points still has a row, with `n` 0, so
  the read sees where a hold stops.

### Counters

A counter is differenced at its rollup row, exactly, in `numeric`
([decision 0157](decisions/0157-a-counter-is-stored-as-sent-and-differenced-at-read-under-one-of-three-contracts.md)).
Each step runs from the stream's previous available reading to the next,
under the later reading's contract:

- `monotonic`: a drop is a reset to zero, so the step is the later reading. A
  counter with no `resetRule` is read as `monotonic`.
- `signed`: the step is the difference, which may be negative.
- `wrap`: a drop wrapped at `wrapAt`, so the step is `wrapAt` minus the
  earlier reading plus the later one.

Under every contract, two readings naming two different `resetAt`, or written
in two different units, start over: the step is the later reading. `resets`
counts the steps that started over, wraps included. A row's `increase` holds
every step whose later reading is in the row, the step in from the stream's
previous reading included, wherever that is, and `covered_ms` the time those
steps span; so the increase of a run of whole buckets is the sum of their
rows, and `rate` is that sum over the sum of `covered_ms`. A correction to a
reading therefore also recomputes the bucket of the stream's next reading. A
row whose readings have no reading before them holds no step, and its
`increase` and `resets` are null: no answer, not a change of zero.

### The latest row

`observed_latest` holds each stream's newest available point, one row per
summary `statistic` (the empty string for the other forms). A night's `avg`
and `max` summaries at one instant are two rows, so neither stands for the
other. The aggregate read takes each row as a stream of its own, so a row of
a bucketed read reads one statistic, and the latest read answers the newest
of a group's rows with the sum of their `points`.

Two points at one instant go to the greater record id in byte order, then
the later point of one trace: the point the `last` reducer answers. A
`revision` counts one record's rewrites, so it breaks no tie between two
records.

A write that moves a stream's newest point to another stream (a corrected
`source`) recomputes both streams' rows, and a row left with no available
point goes. A null after the newest value does not replace it: the row keeps
the last value, and a reader that must say "unavailable since" finds the null
in `observed_points`.

## Reading

One record is a list read, as for any kind: today's WHOOP recovery is
`filter.properties.periodId` equal to the cycle id, every body weight under
155 lb is `metric` equal to the `health/body_mass` metric and `value` less
than `{"value": "155", "unit": "[lb_av]"}` (without the metric, muscle and
bone mass rows match too). Everything
bucketed is the records route's [aggregate read](api.md#the-aggregate-read):
a metric, a range, a time mode, reducers checked against the form, and a
source policy, answered from the rollup rows where they answer exactly and
from the points otherwise. The requests below are the proposal's screens
([reading the data](plans/observations/proposal/03-reading-the-data.md)),
each one request.

Heart rate zones per week for six months, the chest strap preferred where it
recorded, in minutes. `durationIn` reads raw points, so the request raises
`maxSamples`:

```json
{"filter": {"kinds": ["ada.example.com/observations/trace"]},
 "aggregate": {
   "metric": {"ref": "ada.example.com/observations/metric/health/heart_rate"},
   "range": {"gte": "2026-04-06T00:00:00+03:00", "lt": "2026-10-05T00:00:00+03:00"},
   "bucket": {"calendar": "week", "weekStart": "monday", "timezone": "Europe/Athens"},
   "reducers": [{"durationIn": [95, 114]}, {"durationIn": [114, 133]}, {"durationIn": [133, 152]},
                {"durationIn": [152, 171]}, {"durationIn": [171, 220]}],
   "maxGap": "PT5M",
   "sources": {"policy": "priority", "order": ["chest-strap-h10", "garmin-forerunner-265"]},
   "unit": "min",
   "budget": {"maxSamples": 5000000}}}
```

Daily steps for a year on each sample's own local day, the step chunks where
they cover 90 percent of a day and the vendor's daily total otherwise; each
row's `from` says which:

```json
{"aggregate": {
   "metric": {"ref": "ada.example.com/observations/metric/health/steps"},
   "from": "prefer-raw",
   "range": {"gte": "2025-10-05T00:00:00+03:00", "lt": "2026-10-05T00:00:00+03:00"},
   "bucket": {"calendar": "day"},
   "reducers": ["sum"],
   "unit": "{steps}"}}
```

Weight weekly with a four-week rolling mean, from rollup rows alone:

```json
{"aggregate": {
   "metric": {"ref": "ada.example.com/observations/metric/health/body_mass"},
   "range": {"gte": "2025-10-06T00:00:00+03:00", "lt": "2026-10-05T00:00:00+03:00"},
   "bucket": {"calendar": "week", "weekStart": "monday", "timezone": "Europe/Athens"},
   "reducers": ["avg", "min", "max", "count"],
   "rolling": {"buckets": 4, "reducers": ["avg"]},
   "unit": "kg"}}
```

Sleep stages per night: the coded stage amounts summed by code inside each
`health/sleep` record, one row per night and code, each carrying the night's
`record`:

```json
{"filter": {"kinds": ["ada.example.com/observations/observation"]},
 "aggregate": {
   "metric": {"ref": "ada.example.com/observations/metric/health/sleep_stage"},
   "range": {"gte": "2026-09-01T00:00:00+03:00", "lt": "2026-10-01T00:00:00+03:00"},
   "bucket": {"records": {"filter": {"kinds": ["ada.example.com/health/sleep"]}}},
   "groupBy": ["code"],
   "reducers": ["sum"],
   "sources": {"policy": "priority", "order": ["whoop-4-0", "garmin-forerunner-265"]},
   "unit": "min"}}
```

Hours a door was open per week (`closed` ends each `open`, and a door shut
over a weekend is not open until Monday), and kWh per day from a meter that
resets, reset-proof:

```json
{"aggregate": {
   "metric": {"ref": "ada.example.com/observations/metric/home/contact_state"},
   "range": {"gte": "2026-07-06T00:00:00+03:00", "lt": "2026-10-05T00:00:00+03:00"},
   "bucket": {"calendar": "week", "weekStart": "monday", "timezone": "Europe/Athens"},
   "reducers": [{"timeInState": ["open"]}],
   "maxGap": "P1D",
   "unit": "h"}}
```

```json
{"aggregate": {
   "metric": {"ref": "ada.example.com/observations/metric/energy"},
   "range": {"gte": "2026-09-01T00:00:00+03:00", "lt": "2026-10-01T00:00:00+03:00"},
   "bucket": {"calendar": "day", "timezone": "Europe/Athens"},
   "reducers": ["increase"],
   "unit": "kW.h"}}
```

The current value of every stream, the dashboard, needs no range and no
bucket; an 800-pixel chart of any range is `downsample={"points": 800}`
beside a metric and a range:

```json
{"filter": {"implements": "substrate.reamde.dev/core/observed"},
 "aggregate": {"groupBy": ["metric", "source", "about"], "reducers": ["last"]}}
```

A function sends the same body to `host.records.aggregate`, an agent to the
`query` tool's `aggregate` argument, and `substratectl aggregate` builds it
from flags. Where the build differs from the proposal's contract is listed in
[the API reference](api.md#where-this-differs-from-the-plan).

## Where this differs from the plan

The proposal's worked examples ([proposal](plans/observations/proposal/README.md))
were written before this build; these points differ, each for a stated
reason:

- A lab result's code as received is `coding`, an object, not `code`: the
  trait contracts `code` as a string, the categorical key the index reads
  (`not-detected`).
- A lab result's `specimen` names its material `material`, not `type`, a word
  the vocabulary has retired.
- The metric names its measure with `measures`, not `dimension`, and keeps no
  `delta`, `circular` or `wrapAt`: they live on each value.
- A metric's `reducers` and a lab's `status` are free strings rather than
  enums, because an enum value is one lowercase word and `durationIn` and
  `entered-in-error` are not.
- The engine holds the result-shape rule for any observed kind that declares
  `codedValue`, `ratio` or `text`, since a kind declaration has no
  "exactly one of" key; `code` beside `codedValue` counts as one shape.
- Garmin's Pulse Ox is `health/spo2` with Garmin's code, not a metric of its
  own; Apple's heart rate variability is SDNN, so the catalogue has
  `hrv_sdnn` beside `hrv_rmssd`; Health Connect's skin temperature is a
  deviation, so it is `skin_temperature_deviation`.
- The write rules a [decision](decisions/README.md) did not name are this
  build's choices: a `statistic` or a `period` outside a summary is refused,
  a removed `revision` is refused, a coded trace with a `unit` is refused,
  and a trace needs at least one point.
- The rollup key adds `unit` and `delta` to the stream key, and a rollup row
  keeps its statistics in its own unit, so two units that share no base
  never add. `first_value` and `last_value` are the stored digits
  (`numeric`), so a counter's endpoints stay exact.
- A counter's step from the previous bucket is in the later row's
  `increase`, computed when the row is, not at read time with a window
  function as the plan had it, so the read needs no counter contract from
  the rows. A correction recomputes the next bucket holding a reading too.
  Decision 0157 states this rule and its cost.
- A rollup row gains `start_gap_ms`, where the bucket's first point lies, and
  `bounded`, its count of bounded points, so the read can carry a hold across
  rows and report bounded values without the raw points. It also keeps
  `span_end`, the record id and index of the first and last point, and the
  stored digits of the smallest and largest number (`min_value`,
  `max_value`), so a read served from rows joins amount coverage, breaks ties
  at one instant and answers a `min` or `max` as the raw points do. The
  table's CHECK constraints refuse a row without them, and `increase` is null
  on a row that holds no step.
- `observed_points` carries each point's hold, counter contract, `delta`,
  `circular` and `revision`, and its index `ord` in its record; the plan
  named neither, and the rollups are computed from the points alone.
- `observed_latest` names its hold `hold_ms` and adds `ts_end`, `delta`,
  `statistic`, `bounded` and `points`.
