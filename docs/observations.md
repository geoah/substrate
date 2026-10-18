# Observations

An observation is a measured value: a weighing, an hour of heart rate, a
nightly heart rate variability, a vendor's recovery score, an LDL result from
a blood test. This page covers what holds one today: core's `observed` trait,
the rules every write of an observed record is held to, the managed `stats`
of a trace, the `observations` and `health` samples, and the host SDK
helpers a function builds observed values with. The plan is
[observations](plans/observations.md).

The observed index and the aggregate read, which consume these values, are
not built yet. Where this page says what the index or a read does with a
value (a metric's `rollups`, `measures` and `bucketPlace`, a source's
`priorities`, a gauge's hold), it states the contract they are built to,
not behavior a substrate has today.

## The observed trait

`substrate.reamde.dev/core/observed` is a [trait](traits.md) with two
variants. A kind binds `observed(single)` when each record holds one value,
and `observed(packed)` when each record holds many values of one metric over
one fixed period (a trace). Binding it does two things: every write of the
kind is held to the rules below, and it is what the observed index will read
the kind by, without knowing it by name. A kind that binds `observed` should bind
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
| `metric` | reference | what was measured, a record of a kind that binds core's `metric` ([below](#the-metric-and-source-contracts)) |
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
| `utcOffset` | int | minutes east of UTC where the value was observed; on a trace, every point's |
| `measure` | string | the engine's: the measure the unit was read as when the record was written ([below](#the-measure-a-value-was-written-in)) |
| `revision` | int | rises with each rewrite from the upstream; a lower one is refused |

`observed(single)` adds:

| Property | Datatype | Means |
| --- | --- | --- |
| `value` | quantity | the number and its unit, `{value: "72.4", unit: kg}` ([units](units.md)) |
| `code` | string | a categorical value instead of a number: `open`, `rem`, `not-detected` |
| `statistic` | string | on a summary, which statistic it is: `avg`, `max`, `vendor` for a vendor's own method |
| `period` | string | on a summary, the period it covers: `day`, `night`, `cycle` |
| `sourceValue` | string | the reading as received, kept when it could not be normalized |
| `sourceUnit` | string | the unit as received |
| `absentReason` | string | why there is no value, such as `not-performed` |
| `origin` | reference | the provider record an adapter built the value from |

`observed(packed)` adds:

| Property | Datatype | Means |
| --- | --- | --- |
| `unit` | string | the UCUM code every value is in; a trace of codes has none |
| `every` | duration | the spacing of regular points from `at` |
| `offsets` | int, repeated | each point's milliseconds from `at`, for irregular points |
| `values` | decimal, repeated | the numbers, exact decimal strings; `null` means unavailable from that point |
| `codes` | string, repeated | the codes of a categorical trace; `null` means unavailable from that point |
| `origins` | reference, repeated | the provider records an adapter built the period from |
| `stats` | json | computed by the engine on every write ([below](#stats)) |

A kind binding the trait declares every one of these properties itself but
`at` and `endsAt`, which are the record's own time columns (the loader
refuses a declared `at` or `endsAt` as reserved), with its own descriptions,
`repeated: true` on the four lists and its own pins on the references. The
trait holds their names and datatypes, and the loader holds their shapes: a
binding kind whose `offsets`, `values`, `codes` or `origins` is not a list,
or whose other properties are lists or keyed maps, is refused, because the
rules and the index read one value from each of those. The `metric`
reference pins a kind that binds core's `metric`, or the trait itself
(`trait: substrate.reamde.dev/core/metric`); any other pin is refused.

### The measure a value was written in

`measure` is the engine's, like a trace's `stats`: every write of an
observed record stamps the measure its unit resolves to, from the `value`
property's `measures:` where the kind declares one and from the
[unit table](units.md) otherwise, and drops any a writer sends. A
temperature written with `delta: true` is a `temperature-difference`, and a
record holding a code has none. The observed index converts each value in
its stamped measure, so a later unit table that reads a code another way
(a measure added beside it, say) never moves a stored value or a rebuild
([decision 0153](decisions/0153-a-quantity-is-exact-digits-and-a-ucum-code-converted-only-within-its-measure.md)).

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
| `summary` | a statistic over a stated period, computed by the source | WHOOP's recovery of 67 % for one night |

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
| `metric` | `metric` | something that can be measured: its names, codes and how its values are best read |
| `observation` | `temporal(range)`, `observed(single)` | one measured value |
| `trace` | `temporal(range)`, `observed(packed)` | the values of one metric from one source over one fixed period |
| `source` | `source` | the device, app, service or lab a value came from |

**`metric`** is a catalogue entry and a guide for writers, and it binds
core's `metric` trait ([below](#the-metric-and-source-contracts)). The write
rules and the observed index read nothing from it but its rollup tiers: each
value carries its own form, unit and hold. The
aggregate read takes two of its properties:
`measures`, the measure every value of the read converts into (a stream
whose unit is outside it is a `422`), and `unit`, the unit the read answers
in when the request names none. Editing either changes every aggregate
answer for the metric and no stored value. `name` is its heading
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
`bucketPlace` (`start` or `end`) says where a read places a value's span,
which the aggregate read applies where the request names no `bucket.place`
(`health/sleep_stage` sets `end`, so a night lands on the day it ends);
`rollups` lists the rollup tiers the observed index keeps for it;
`algorithm` (`{vendor, name, version}`) names a vendor's scoring method. The sample ships
six metrics several domains share: `air_temperature`, `relative_humidity`,
`energy`, `power`, `illuminance` and `atmospheric_pressure`.

**`observation`** declares the trait's single properties and adds:
`periodId`, the source's own identity for a summary's period (a WHOOP cycle
id), so two cycles on one date stay two records; `denominator`, what a
duration statistic is per; `qualifiers`, coded context (`{system, code}`, such
as after a meal); `components`, the parts of a value measured together
(`{metric, value}`); `normalizationFailure`, why a reading kept in the
trait's `sourceValue` could not be normalized; `quality` (`valid`, `estimated`, `questionable`, `stale`,
`validated`, `unavailable`); `issuedAt`, when the value became known;
`recordingMethod` (`automatic`, `active`, `manual`); `adapter`, the code
that wrote it from the trait's `origin`; `derivedFrom` and `conversion`, for a value computed
from another. It titles itself `{metric} {value}`, "Body weight 72.40 kg".

**`trace`** declares the trait's packed properties and adds `acquisition`
(the recording a trace is one channel of), `quality` and `adapter`. A
record's `quality` is its kind's own property: no trait contracts it and
nothing in the engine reads it. One trace covers one metric,
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
(its rank per metric when sources overlap, keyed by the metric's full record
path, 1 first). The aggregate read's `priority` and `summaries` policies
take `priorities` as their order when a request names none, an unranked
source after the ranked ones and a value naming no source after every named
one; a request's `sources.order` wins.

### The metric and source contracts

The engine and the console read a metric record and a source record through
two core traits, never by the sample's property names, so a kind of your own
can be a metric or a source by binding them.

`substrate.reamde.dev/core/metric` contracts `name`, `measures`, `unit`,
`form`, `codeSet`, `displayPrecision`, `category`, `bucketPlace` and
`rollups`. A write of a record of a kind that binds it is refused where
`measures` names no measure of the [unit table](units.md), where `unit` is
not one of its units (or not a unit the table reads), where `form` is not
one of the four forms, where `bucketPlace` is not `start` or `end`, or where
`rollups` lists a tier outside `5m`, `15m`, `1h` and `1d`, lists one twice
or lists them out of order (finest first, so each divides the next). An
absent `rollups` keeps the tiers `15m`, `1h` and `1d`; an empty list keeps
none, and every read of the metric reads its raw values. A record of a kind
that does not bind the trait is no metric to the aggregate read: it takes
neither measure nor unit from it.

`substrate.reamde.dev/core/source` contracts `priorities`, a keyed map of
ranks. A write is refused where a key is not the full record path of a
record of a kind that binds `metric`
(`ada.example.com/observations/metric/health/heart_rate`, never
`health/heart_rate`), or where a rank is below 1. The aggregate read ranks
by the priorities of a source whose kind binds the trait and of no other.

```text
422 props.priorities.health/heart_rate: a priority is keyed by the metric's full record path, <kind>/<id>, such as <authority>/observations/metric/health/heart_rate
422 props.rollups[1]: 15m comes after the coarser 1h; list the tiers finest first, each dividing the next
```

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
    hold: PT0S
    recordingMethod: manual
    at: 2026-10-09T07:02:00+03:00
    utcOffset: 180
```

`bin/substratectl apply -f weight.yaml` writes it. The `metric` must exist
(`mustExist: true`); the `source` need not, so a writer may create it after.
`hold` copies the `health/body_mass` metric's, `PT0S`: a weighing is a point
in time, so it covers its own instant alone and an average of weighings is
the mean of the readings. Without a hold it stands until the next weighing,
and a read counts it as covering every day between
([holds](#how-long-a-gauge-value-stands)).
`recordingMethod: manual` says a person entered it.

## Writing a trace

An hour of heart rate at irregular times, with one unavailable reading:

```yaml
kind: ada.example.com/observations/trace
metadata:
  id: strap/ada/chest/heart_rate/2026-10-07T06
data:
  properties:
    metric:
      ref: ada.example.com/observations/metric/health/heart_rate
    source:
      ref: ada.example.com/observations/source/strap/ada/chest
    form: gauge
    unit: /min
    hold: PT4M
    at: 2026-10-07T06:00:00Z
    endsAt: 2026-10-07T07:00:00Z
    utcOffset: 180
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
- `utcOffset` is minutes within 1080 either way.
- `revision` never falls: a rewrite with a lower one than the stored one is
  refused as a stale delivery, an equal one is admitted, and a stored one is
  never removed. A create, and a put onto a deleted record, start afresh. A
  kind that binds core's `revised` trait instead is held to this rule alone.
- A `comparator` on `value` (`<0.01`) is admitted only where the kind
  declares `comparator: true` on it, as for any quantity.
- A value's magnitude is at most 1e280: a larger one is refused
  (`props.value`, or `props.values[i]` naming the first such point). The
  observed index reduces in float64 and weighs a gauge's value by the
  milliseconds it holds, and the bound keeps that weighted sum finite over any
  span a timestamp can name. A trace whose spread passes 1.8e308, the
  largest float64 (values more than about 1e154 apart), is refused at
  `props.stats`.

A packed record is also held to these:

- It holds `values` or `codes`, never both, except an amount trace, which
  may carry one code per value; at least one point and at most
  20,000, and its properties, with the `stats` and `measure` the engine
  computes for it, are at most 1 MiB as JSON. A longer recording is split into shorter
  periods.
- It places points by `every` or by `offsets`, never both. `offsets` are
  integer milliseconds, one per point, never decreasing, inside
  `[0, endsAt - at)`. With `every`, the count of points times `every` fits
  the period.
- A trace of values names its `unit`, a code the unit table reads; an alias
  such as `°C` is stored as its code, `Cel`, and a refused spelling (`count`)
  is refused with the same guidance a quantity's unit gets. A trace of codes
  has no unit.
- A `summary` is refused: it is one value per record.
- `null` is admitted inside `values` and `codes` and nowhere else.
- A code is never empty: `""` inside `codes` is refused naming its index
  (`props.codes[1]: a code is not empty`), and so is `code: ""` on a single
  record. `null` is the one way to say a point has nothing.

A kind that binds core's `result` trait beside `observed(single)`, as the
health sample's `labresult` does, holds exactly one of `value`, the coded
result (`codedValue`, with `code` as its index key), `ratio`, `text` and
`absentReason` on every write; with none of them, `sourceValue` must keep the
reading as received. `result` contracts `codedValue`, `ratio`, `text`,
`interpretation` (the lab's flags) and `referenceRanges`; the loader holds
the object ones to their fields (`codedValue` and each flag `{system, code,
display}`, `ratio` `{numerator, denominator}` as decimals, each range
`{low, high}` as quantities with its `text`). On a kind that does not bind
`result`, a property named `text` or `ratio` is an ordinary property.

## Stats

A trace's `stats` is the engine's. On every write it is computed from the
points, and a `stats` the writer sends is dropped rather than refused, so a
function that writes back a trace it read never trips over it. `null` points
count in nothing.

The trace above, whose middle point is `null`, gets:

```yaml
stats:
  count: 2
  min: "58"
  max: "61"
  mean: 59.5
  m2: 4.5
  first:
    at: 2026-10-07T06:00:00Z
    value: "58"
  last:
    at: 2026-10-07T06:04:01Z
    value: "61"
  coverage: PT4M1S
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
| `sleep` | one night or nap: `name`, the heading the adapter writes on the clock the night was recorded on (`Night of Fri 10 Oct`, `Nap, Sun 12 Oct 14:30`) and the record's title, the span, `timeInBed`, `timeAsleep`, the stage durations `awake`, `light`, `deep` and `rem`, `latency`, `efficiency`, `respiratoryRate`, `disturbances` and the device's `scores` by their own names |
| `workout` | one session: `name`, the heading the adapter writes from the sport and the local start (`Running, Sat 11 Oct 08:43`) and the record's title; `sport`, one code set every adapter maps onto (`running`, `walking`, `hiking`, `cycling`, `swimming`, `strength`, `yoga`, `rowing`, `hiit` and more, `other` for a sport with no value of its own) with the provider's own word in `sportSource`; the span, `duration`, `distance`, `activeEnergy` (above the resting rate) and `totalEnergy` (resting and active together), `averageHeartRate`, `maxHeartRate`, `elevationGain`, `strain`, `zoneDurations` by zone name, and the device's original `file` as a blob |
| `labreport` | one report from a lab: `lab`, `accession`, `orderedAt`, `issuedAt`, `status`, the original `file` and the owner's `review` |
| `labresult` | one analyte's result, binding `observed(single)`: `report` (deleted with it), `coding` (the analyte's code as the lab sent it), the result shapes, `specimen`, `method`, `referenceRanges` (each bound a quantity, `{low, high, text, appliesTo, age}`), `interpretation`, `status`, `provenance`, and for a derived result `derivedFrom` and `conversion` |

The catalogue (`samples/health/metrics.yaml`) is 59 `metric` records with ids
`health/<name>`: the heart, sleep, activity, body, blood pressure, glucose
and fitness metrics wearables report, the WHOOP scores (`whoop_recovery`,
`whoop_strain`, `whoop_sleep_performance`), and the
common labs. A lab analyte reported in both unit systems is two metrics, the
amount concentration unsuffixed (`ldl_cholesterol`, mmol/L) and the mass
concentration suffixed `_mass` (`ldl_cholesterol_mass`, mg/dL), because the
two are different measures and a read of one in the other's unit is refused.
HbA1c is `hba1c` (NGSP %, measure `fraction`) and `hba1c_ifcc` (IFCC
mmol/mol, measure `amount-fraction`), which never convert into each other:
they relate by a formula, not a unit.
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
input's: 130 mg/dL of LDL is 3.362 mmol/L. Each converted reference bound
takes the value's decimal places, so the two compare digit for digit: a
high of 3 mmol/L beside a result of 3.1 converts to 116 mg/dL beside 120. A
derived result is never
converted again, a corrected input rewrites its derivations, and a deleted
input takes them with it. A read of one metric counts each reading once, the
lab's own or the derived one; a read that names both metrics, or groups the
latest values by metric, shows both. A result written before the sample was
imported is converted by replaying the trigger from 0. Meter and continuous
monitor readings of glucose are observations, not lab results, and are not
converted.

### How long a gauge value stands

Every gauge metric the health sample ships carries a `hold`, which a writer
copies onto each value: about twice the time between a source's readings
for a sampled stream (`PT20M` for SpO2, `PT10S` for a workout's speed), and
`PT0S` for a sporadic measurement, which is a point: a weighing, a height, a
VO2 max estimate, a night's skin temperature, a lab value and a blood
pressure. An average of `PT0S` values is the mean of the readings. The
observations sample's shared physical metrics (`air_temperature` and the
rest) carry none, so a writer names the hold its sensor's reporting interval
implies. A gauge value written without
a hold stands until the next value of its stream, often a day later, and the
coverage, the time-weighted mean and the priority policy all read it as
still standing ([decision 0158](decisions/0158-a-gauge-carries-a-hold-a-null-ends-it-and-its-mean-is-time-weighted.md)).
A `PT0S` value holds no time, so under `priority` it covers only its own
instant: a blood pressure two apps deliver at one instant is the higher
source's alone, and one stamped seconds apart is kept twice
(G-31 in the observations requirements).

## Building observed values in a function

`host.units.convert`, `host.units.canonical` and `host.units.measure` ask the
engine's unit table over the host call. `host.observed.trace` packs a body's
own timestamped points into trace documents that pass the rules above, one
per fixed UTC period, split where the UTC offset changes and before a
document passes the size limits, narrowed to the span a delivery covers when
the body names one; `period_id` gives
the label a trace id ends with and `local_day` the civil date at an offset.
[Functions](functions.md#the-sdk) has the signatures and an example.

