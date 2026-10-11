# Observations

An observation is a measured value: a weighing, an hour of heart rate, a
nightly heart rate variability, a vendor's recovery score, an LDL result from
a blood test. This page covers what holds one today: core's `observed` trait,
the rules every write of an observed record is held to, the managed `stats`
of a trace, the `observations` and `health` samples, and the host SDK helpers
a function builds observed values with. The observed index and the aggregate
read that answer "average heart rate per week" are the next pieces of work and
do not exist yet; the plan is [observations](plans/observations.md).

## The observed trait

`substrate.reamde.dev/core/observed` is a [trait](traits.md) with two
variants. A kind binds `observed(single)` when each record holds one value,
and `observed(packed)` when each record holds many values of one metric over
one fixed period (a trace). Binding it does two things: every write of the
kind is held to the rules below, and the observed index will read the kind
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
the references; the trait holds their names and datatypes.

## Forms

The form says what a number means, which decides how it may be added up or
averaged ([decision 0156](decisions/0156-the-forms-are-gauge-amount-counter-and-summary-and-live-on-the-value.md)).
It lives on the value, never on the metric, so editing a metric cannot
relabel a stored value.

| Form | Means | Example |
| --- | --- | --- |
| `gauge` | a level at an instant, held until its `hold` ends or the next point | 62 /min, 72.4 kg, a door `open` |
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

**`metric`** is a catalogue entry and a guide for writers. The engine reads
nothing from it. `name` is its heading and `description` its prose; `codes`
lists it in other code systems (`{system, code, version, display}`, `system`
being `http://loinc.org`, `healthkit`, `healthconnect`, `googlefit`, `whoop`,
`garmin`, `withings`, `homeassistant` or `cf`); `measures` is the measure
from the [unit table](units.md) its values belong to, and `unit` the code
writers store; `codeSet` lists a categorical metric's codes; `form` and
`hold` are the defaults a writer copies onto each value; `reducers` names the
reducers that answer something useful; `compatibility` names the class values
pool within where the measure alone is not enough (NGSP and IFCC HbA1c are
both a fraction and never pool); `displayPrecision` is the decimal places a
reader shows; `category` groups it for navigation; `algorithm`
(`{vendor, name, version}`) names a vendor's scoring method. The sample ships
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
record that delivered it; `derivedFrom` and `conversion`, for a value computed
from another. It titles itself `{metric} {value}`, "Body weight 72.40 kg".

**`trace`** declares the trait's packed properties and adds `acquisition`
(the recording a trace is one channel of), `quality` and `origins` (the
provider records the period was built from). One trace covers one metric,
source, `about`, `channel` and period, from one origin provider, and its id is
composed from them: `<provider>/<account record id>/<metric>/<period start>`
([decision 0162](decisions/0162-an-observed-row-names-its-origin-and-source-and-composes-its-id-from-upstream-data.md)).

**`source`** is the device, never the delivery: `name`, `class` (`watch`,
`ring`, `scale`, `thermometer`, `meter`, `contact`, `camera`, `app`,
`service`, `lab`, `manual`), `manufacturer`, `hardwareModel`, `firmware`,
`resolution` (the smallest step it reports, a quantity), `about` (the thing it
is fixed to, which writers copy onto each value), `account` (the connected
account it syncs through, any kind binding `accountconfig`) and `priorities`
(its rank per metric id when sources overlap, 1 first).

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
  id: applehealth/iphone/heart_rate/2026-10-07T06
data:
  properties:
    metric:
      ref: ada.example.com/observations/metric/health/heart_rate
    source:
      ref: ada.example.com/observations/source/garmin-forerunner-265
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
  never removed. A create, and a put onto a deleted record, start afresh.
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
kinds and a catalogue.

| Kind | One record is |
| --- | --- |
| `analyte` | the lab facts about one blood test metric: its LOINC parts, `molarMass` in grams per mole, the `conversions` a function may write, and a `fallbackRange` |
| `sleep` | one night or nap: the span, `timeInBed`, `timeAsleep`, the stage durations `awake`, `light`, `deep` and `rem`, `latency`, `efficiency`, `respiratoryRate`, `disturbances` and the device's `scores` by their own names |
| `workout` | one session: `sport`, the span, `duration`, `distance`, `energy`, `averageHeartRate`, `maxHeartRate`, `elevationGain`, `strain`, `zoneDurations` by zone name, and the device's original `file` as a blob |
| `labreport` | one report from a lab: `lab`, `accession`, `orderedAt`, `issuedAt`, `status`, the original `file` and the owner's `review` |
| `labresult` | one analyte's result, binding `observed(single)`: `report` (deleted with it), `coding` (the analyte's code as the lab sent it), the result shapes, `specimen`, `method`, `referenceRanges`, `interpretation`, `status` and `provenance` |

The catalogue (`samples/health/metrics.yaml`) is 60 `metric` records with ids
`health/<name>`: the heart, sleep, activity, body, blood pressure, glucose
and fitness metrics the Apple Health, WHOOP, Garmin and Google Fit adapters
write, the vendor scores (`whoop_recovery`, `whoop_strain`,
`whoop_sleep_performance`, `garmin_stress`, `garmin_body_battery`), and the
common labs. A lab analyte reported in both unit systems is two metrics, the
amount concentration unsuffixed (`ldl_cholesterol`, mmol/L) and the mass
concentration suffixed `_mass` (`ldl_cholesterol_mass`, mg/dL), because the
two are different measures and only a derived observation bridges them. HbA1c
is `hba1c` (NGSP %) and `hba1c_ifcc` (mmol/mol). The codes were written from
the vendors' published documentation without a check against a live account;
a metric's description marks the least certain ones.

## The adapters

An adapter is a sample whose function turns one provider's mirror rows into
these records. `samples.substrate.reamde.dev/healthapple` is the first: it
reads the Apple Health provider's metric batches and workouts and writes
traces, observations, sleeps and workouts, each naming the mirror row it came
from in `origin` and rewritten or deleted with it. Its mapping and rules are
in the [catalog](bundles-catalog.md#apple-health-adapter-sample). Its trace
ids carry the source, `applehealth/<account>/<source>/<metric>/<UTC hour>`,
because one phone delivers several devices' data.

## Building observed values in a function

`host.units.convert`, `host.units.canonical` and `host.units.measure` ask the
engine's unit table over the host call. `host.observed.trace` packs a body's
own timestamped points into trace documents that pass the rules above, one
per fixed UTC period, split where the UTC offset changes; `period_id` gives
the label a trace id ends with and `local_day` the civil date at an offset.
[Functions](functions.md#the-sdk) has the signatures and an example.

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
