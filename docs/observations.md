# Observations

An observation is a measured value: a weighing, an hour of heart rate, a
nightly heart rate variability, a vendor's recovery score, an LDL result from
a blood test. This page covers what holds one today: core's `observed` trait,
the rules every write of an observed record is held to, the managed `stats`
of a trace, the seeded `observations` package and the `health`, `labs` and
`environment` catalogues, the host SDK helpers
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
| `metric` | reference | what was measured, a record of a kind that binds core's `metric` ([below](#the-metric-and-source-contracts)) |
| `source` | reference | the device, app, service or lab that produced the value |
| `about` | reference | the thing observed, any kind; absent means the repository's owner ([decision 0155](decisions/0155-an-observed-value-names-what-it-is-about-with-an-about-reference.md)) |
| `channel` | string | which of a source's channels this is, where one source measures one metric twice |
| `form` | string | `gauge`, `amount`, `counter` or `summary` ([below](#forms)) |
| `hold` | duration | on a gauge, how long a value stands without a newer one |
| `resetRule` | string | on a counter, its contract: `monotonic`, `signed` or `wrap` |
| `resetAt` | datetime | on a counter, when the current accumulation began |
| `delta` | bool | true when the value is a difference, such as a temperature deviation, which converts by the factor alone ([units](units.md#how-the-server-converts)) |
| `circular` | bool | true when the value is an angle averaged on the circle, such as a wind direction |
| `wrapAt` | decimal | on a `wrap` counter, the value it wraps to zero at |
| `utcOffset` | int | minutes east of UTC where the value was observed; on a trace, every point's ([below](#points-nulls-and-offsets)) |
| `revision` | int | rises with each rewrite from the upstream; a lower one is refused |

`observed(single)` adds:

| Property | Datatype | Means |
| --- | --- | --- |
| `value` | quantity | the number and its unit's id, `{value: "72.4", unit: kg}` ([units](units.md)) |
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
| `unit` | string | the id of the unit every value is in; a trace of codes has none |
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

The `substrate.reamde.dev/observations` package holds the five kinds every
observing domain shares. It is seeded into every repository beside `core`,
`llm` and `console` and upgraded at open, so every repository and every
provider names an observation the same way
([decision 0192](decisions/0192-observations-is-seeded-and-the-well-known-catalogues-are-first-party-packages.md)).
Unlike the other three seeded packages its kinds are your data, not the
substrate's state: they keep their own `purpose`, and their records can be
merged ([merges](projection.md#merges)). The package ships no metrics; the
[catalogues](#the-well-known-catalogues) and the providers do.

| Kind | Binds | One record is |
| --- | --- | --- |
| `metric` | `metric` | something that can be measured: its names, codes and how its values are best read |
| `observation` | `temporal(range)`, `observed(single)` | one measured value |
| `trace` | `temporal(range)`, `observed(packed)` | the values of one metric from one source over one fixed period |
| `source` | `source` | the device, app, service or lab a value came from |
| `sourcelink` | `sourcelink` | the sources that are one device reached by several routes ([below](#one-device-by-several-routes)) |

**`metric`** is a catalogue entry and a guide for writers, and it binds
core's `metric` trait ([below](#the-metric-and-source-contracts)). The write
observed index reads nothing from it but its rollup tiers: each value
carries its own form, unit and hold. The write rules read its `unit` and
`units`, the [unit records](units.md) a value may be written in
([below](#the-metric-and-source-contracts)), and the
[aggregate read](api.md#the-aggregate-read) answers in its `unit` when the
request names none. Editing `unit` changes every aggregate answer for the
metric and no stored value. `name` is its heading
and `description` its prose; `codes`
lists it in other code systems (`{system, code, version, display}`, `system`
being `http://loinc.org`, `healthkit`, `healthconnect`, `googlefit`, `whoop`,
`garmin`, `withings`, `homeassistant` or `cf`); `unit` is a reference to
the unit record writers store values in, and `units` the references to the
units a value may be written in besides it; `codeSet` lists a categorical metric's codes for a reader, and
no write is held to it ([free-text states](#free-text-states)); `form` and
`hold` are the defaults a writer copies onto each value; `reducers` names the
reducers that answer something useful; `compatibility` names the class values
pool within where the unit alone is not enough, such as serum against
whole-blood glucose, and nothing in the engine reads it; `displayPrecision`
is the decimal places a reader shows; `category` groups it for navigation;
`bucketPlace` (`start` or `end`) says where a read places a value's span,
which the aggregate read applies where the request names no `bucket.place`
(`health/sleep_stage` sets `end`, so a night lands on the day it ends, as
do the nightly summaries of `hrv_rmssd`, `resting_heart_rate`,
`respiratory_rate`, `spo2`, `skin_temperature` and WHOOP's
`whoop/recovery` and `whoop/sleep_performance`, whose raw readings are instants and place the same
either way);
`rollups` lists the rollup tiers the observed index keeps for it;
`algorithm` (`{vendor, name, version}`) names a vendor's scoring method. A
metric's id starts with the package that ships it (`health/heart_rate`,
`labs/ldl_cholesterol`, `whoop/strain`); that prefix is the package's, so a
metric of your own takes another one.

**`observation`** declares the trait's single properties and adds:
`periodId`, the source's own identity for a summary's period (a WHOOP cycle
id), so two cycles on one date stay two records; `denominator`, what a
duration statistic is per; `qualifiers`, coded context (`{system, code}`, such
as after a meal); `components`, the parts of a value measured together
(`{metric, value}`); `normalizationFailure`, why a reading kept in the
trait's `sourceValue` could not be normalized; `quality` (`valid`, `estimated`, `questionable`, `stale`,
`validated`, `unavailable`); `issuedAt`, when the value became known;
`recordingMethod` (`automatic`, `active`, `manual`); `adapter`, the code
that wrote it from the trait's `origin`
([the adapters](#the-adapters)); `derivedFrom` and `conversion`, for a value computed
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
two core traits, never by the `observations` kinds' property names, so a
kind of your own can be a metric or a source by binding them.

`substrate.reamde.dev/core/metric` contracts `name`, `unit`, `units`,
`form`, `codeSet`, `displayPrecision`, `category`, `bucketPlace` and
`rollups`; `unit` and `units` are references pinned to
`substrate.reamde.dev/core/unit`. A write of a record of a kind that binds it
is refused where `unit` or a listed unit names no unit of the repository,
where a listed unit does not convert to `unit` through linear conversions,
where `form` is not one of the four forms, where `bucketPlace` is not `start` or `end`, or where
`rollups` lists a tier outside `5m`, `15m`, `1h` and `1d`, lists one twice
or lists them out of order (finest first, so each divides the next). An
absent `rollups` keeps the tiers `15m`, `1h` and `1d`; an empty list keeps
none, and every read of the metric reads its raw values. A record of a kind
that does not bind the trait is no metric to the aggregate read: it takes
no unit from it.

The metric governs every value that points at it, checked at the end of
each transaction against its final state. An observed value whose `metric`
names no live record of a kind binding the trait is refused, and so is one
whose unit the metric does not admit: its `unit`, its listed `units`, or,
where it lists none, every unit that converts to `unit` through linear
conversions. A metric with no `unit` admits every unit. A transaction that
creates, edits or restores a metric so that a stored value pointing at it,
tombstones included, is no longer admitted is refused, and so is one that
deletes a metric any stored value points at; deleting a metric means purging
its values first.

`substrate.reamde.dev/core/source` contracts `priorities`, a keyed map of
ranks. A write is refused where a key is not the full record path of a
record of a kind that binds `metric`
(`substrate.reamde.dev/observations/metric/health/heart_rate`, never
`health/heart_rate`), or where a rank is below 1. The aggregate read ranks
by the priorities of a source whose kind binds the trait and of no other.

```text
422 props.priorities.health/heart_rate: a priority is keyed by the metric's full record path, <kind>/<id>, such as <authority>/observations/metric/health/heart_rate
422 props.rollups[1]: 15m comes after the coarser 1h; list the tiers finest first, each dividing the next
```

### Finding the source kind

Every repository names the seeded `source` kind the same way,
`substrate.reamde.dev/observations/source`, but a kind of your own may bind
core's `source` trait too, so a script or an app that reads every source
finds them by the trait instead of spelling one kind.
`GET /api/v1/records` with `filter.implements` set to
`substrate.reamde.dev/core/source` lists the sources of every kind that
binds it, each row naming its `kind`. The kinds list (`substratectl kinds -o
json`, or the records of `substrate.reamde.dev/core/kind`) names each kind
with the traits it binds, which answers before any source is written.

```http
GET /api/v1/records?filter={"implements":"substrate.reamde.dev/core/source"}
```

### One device by several routes

A scale that syncs to its maker's app and to Apple Health reaches the
repository as two sources, one per adapter, because nothing upstream says
they are one. The priority policy keeps the higher-ranked source's reading
only where it overlaps the other's, so a weigh-in Apple Health stamps two
seconds early counts twice. A `sourcelink` record says the sources are one
device ([decision 0187](decisions/0187-linked-sources-are-one-device-read-from-their-highest-ranked-source.md)):

```yaml
kind: substrate.reamde.dev/observations/sourcelink
metadata:
  id: withings-body
data:
  properties:
    name: Withings Body+
    sources:
      - {ref: substrate.reamde.dev/observations/source/withings/ada/scale}
      - {ref: substrate.reamde.dev/observations/source/apple/ada/scale}
```

Under `priority`, a linked device answers each metric and `about` from one
source: the highest-ranked (the request's `order`, else the sources'
`priorities`, the stream order breaking a tie) among the linked sources
holding a value in the read's range and `from`; a range of recorded-local
dates counts only values on those dates, never on the days the scan reaches
around them. The other linked sources'
values of that metric and `about` leave the read, whole, so a reading only a
lower-ranked route delivered is not counted while the winner has any value
in the read. Links join: two links sharing a source make one device of every
source either names. The read follows links outward from its own sources
through the refs index, each lookup reading at most the `maxSamples` the
read has left plus one, and charges each reference it reads.
The `separate` policy ignores links. The read takes links from records of any kind
binding core's `substrate.reamde.dev/core/sourcelink` trait, which contracts
`sources`, a list of references; an entry naming a record whose kind does not
bind `source` links nothing. Nothing pairs sources by close values or times,
and nothing writes a link but the owner.

A function or an agent that reads under a reads allowlist needs the link
kind granted for every priority read, whether or not it names an `order`,
because what the read drops reveals what the links hold.

## Writing an observation by hand

The `observation` kind is seeded, and the `health/body_mass` metric ships
with the `health` catalogue, so install that first:

```bash
bin/substratectl install substrate.reamde.dev/health
```

A weighing entered by hand:

```yaml
kind: substrate.reamde.dev/observations/observation
metadata:
  id: manual/2026-10-09-weight
data:
  properties:
    metric:
      ref: substrate.reamde.dev/observations/metric/health/body_mass
    source:
      ref: substrate.reamde.dev/observations/source/manual
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
kind: substrate.reamde.dev/observations/trace
metadata:
  id: strap/ada/chest/heart_rate/2026-10-07T06
data:
  properties:
    metric:
      ref: substrate.reamde.dev/observations/metric/health/heart_rate
    source:
      ref: substrate.reamde.dev/observations/source/strap/ada/chest
    form: gauge
    unit: 1/min
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
- `delta: true` is admitted on any unit. `circular: true` needs a unit with
  a `turn` ([units](units.md#unit-records)), so that one direction has one
  reading in every unit of its component.
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
  20,000, and its properties, with the `stats` the engine
  computes for it, are at most 1 MiB as JSON. A longer recording is split into shorter
  periods.
- It places points by `every` or by `offsets`, never both. `offsets` are
  integer milliseconds, one per point, never decreasing, inside
  `[0, endsAt - at)`. With `every`, the count of points times `every` fits
  the period.
- A trace of values names its `unit`, any spelling of one of the
  repository's [unit records](units.md); an alias such as `°C` is stored as
  the unit's id, `Cel`, and a spelling no unit answers is refused. A trace of
  codes has no unit.
- A `summary` is refused: it is one value per record.
- `null` is admitted inside `values` and `codes` and nowhere else.
- A code is never empty: `""` inside `codes` is refused naming its index
  (`props.codes[1]: a code is not empty`), and so is `code: ""` on a single
  record. `null` is the one way to say a point has nothing.
- A code is at most 255 characters, Home Assistant's bound on a state:
  `props.code: a code is at most 255 characters, got 300`, or
  `props.codes[i]` naming the first longer one in a trace. The observed index
  keys its rollup rows on the code. The bound holds a new or changed code: a
  longer code the stored record already holds is admitted, so a row written
  before the bound stays editable.

A kind that binds core's `result` trait beside `observed(single)`, as the
`labs` catalogue's `labresult` does, holds exactly one of `value`, the coded
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

## The well-known catalogues

Three first-party packages under `substrate.reamde.dev` each ship one
domain's shared metrics, kinds, units and functions
([decision 0192](decisions/0192-observations-is-seeded-and-the-well-known-catalogues-are-first-party-packages.md)).
They are not seeded: the catalog serves them, and a repository installs one
when it wants it, through the door a provider takes
(`bin/substratectl install substrate.reamde.dev/labs`, or
`POST /api/v1/catalog/{id}/install`). Like a provider, each lands under the
authority that publishes it, is published (you write its records, never its
declarations) and upgrades from the catalog
([the two doors](bundles.md#the-two-doors)). An install first installs every
published package the bundle requires and the repository lacks, so
installing `labs`, or the Apple Health or WHOOP provider, installs `health`.

| Package | Ships |
| --- | --- |
| `substrate.reamde.dev/health` | the `sleep` and `workout` kinds; 46 metrics, `health/<name>`: the heart, sleep, activity, body, blood pressure, glucose and fitness metrics wearables, scales and meters report, and what a phone logs: mindful minutes, water, eleven nutrients and basal body temperature; two glucose units; the sleep and workout record views |
| `substrate.reamde.dev/labs` | requires `health`; the `analyte`, `labreport` and `labresult` kinds; 22 lab metrics, `labs/<name>`, and 17 `analyte` records; the function `convertlab` and its trigger `labs-on-labresult`; three record views |
| `substrate.reamde.dev/environment` | no kinds; six climate and energy metrics, `environment/air_temperature`, `environment/relative_humidity`, `environment/illuminance`, `environment/atmospheric_pressure`, `environment/energy` and `environment/power` |

`sleep`, `workout` and `labreport` carry `origin` and `revision` as the base
kinds do, and `sleep` and `workout` carry `adapter`, because an adapter
writes them too. The three bind core's `revised` trait, so a rewrite with a
lower `revision` is refused on them as it is on an observation.

| Kind | One record is |
| --- | --- |
| `health/sleep` | one night or nap: `name`, the heading the adapter writes on the clock the night was recorded on (`Night of Fri 10 Oct`, `Nap, Sun 12 Oct 14:30`) and the record's title, the span, `timeInBed`, `timeAsleep`, the stage durations `awake`, `light`, `deep` and `rem`, `latency`, `efficiency`, `respiratoryRate`, `disturbances` and the device's `scores` by their own names |
| `health/workout` | one session: `name`, the heading the adapter writes from the sport and the local start (`Running, Sat 11 Oct 08:43`) and the record's title; `sport`, one code set every adapter maps onto (`running`, `walking`, `hiking`, `cycling`, `swimming`, `strength`, `yoga`, `rowing`, `hiit` and more, `other` for a sport with no value of its own) with the provider's own word in `sportSource`; the span, `duration`, `distance`, `activeEnergy` (above the resting rate) and `totalEnergy` (resting and active together), `averageHeartRate`, `maxHeartRate`, `elevationGain`, `strain`, `zoneDurations` by zone name, and the device's original `file` as a blob |
| `labs/analyte` | the lab facts about one blood test metric: its LOINC parts, `molarMass` in grams per mole, the `conversions` a function may write, and a `fallbackRange` |
| `labs/labreport` | one report from a lab: `lab`, `accession`, `orderedAt`, `issuedAt`, `status`, the original `file` and the owner's `review` |
| `labs/labresult` | one analyte's result, binding `observed(single)`: `report` (deleted with it), `coding` (the analyte's code as the lab sent it), the result shapes, `specimen`, `method`, `referenceRanges` (each bound a quantity, `{low, high, text, appliesTo, age, tone}`; `tone` is how a reader draws the band, absent for the lab's normal range and `poor` for a cut-off past which a value is abnormal), `channel` (the body region of a regional body composition result, such as `trunk`), `interpretation`, `status`, `provenance`, and for a derived result `derivedFrom` and `conversion` |

A vendor's own score ships with its provider, not in a catalogue: WHOOP's
are `whoop/recovery`, `whoop/strain` and `whoop/sleep_performance`, shipped
by the [WHOOP](bundles-catalog.md#whoop) provider. A lab analyte reported in
both unit systems is two metrics, the
amount concentration unsuffixed (`labs/ldl_cholesterol`, mmol/L) and the mass
concentration suffixed `_mass` (`labs/ldl_cholesterol_mass`, mg/dL), because no
conversion joins the two units (it takes a molar mass) and a read of one in
the other's unit is refused. HbA1c is `labs/hba1c` (NGSP, `percent`) and
`labs/hba1c_ifcc` (IFCC, `mmol/mol`), which never convert into each other: they
relate by a formula, not a unit. A meter's glucose is one metric in two
units ([glucose in either unit](#glucose-in-either-unit)).
The codes were written from the vendors' published documentation without a
check against a live account; a metric's description marks the least
certain ones. The six `environment` metrics keep the `1h` and `1d` rollup
tiers and no `15m`, because a home sensor reports about once a minute
([rollup tiers](#rollup-tiers)).

`health` and `labs` ship five [record views](console.md#record-views) between
them, records of the seeded console package's `recordview` kind that
describe a record page's layout. Each names its metrics by reference to the
metric records and each listed kind by reference to its declaration, and
its id starts with the package that ships it
(`substrate.reamde.dev/health/sleep`).

- **`health/sleep`**: the heart rate, heart rate variability (`hrv_rmssd`)
  and respiratory rate over the night, left out where none was measured
  (WHOOP writes no samples inside a night); the night's `sleep_stage` spans in
  the order awake, in bed, REM, asleep, light, deep, from the sleep's own
  source, with the time in each; tiles of the night's own `timeAsleep`,
  `timeInBed`, `efficiency`, `disturbances` and `respiratoryRate`; and the
  four stage durations as shares of the time in bed.
- **`health/workout`**: the distance and time moving; tiles of the
  workout's `strain`, `averageHeartRate`, `maxHeartRate` and `totalEnergy`;
  the device's `zoneDurations` as shares of the duration; the heart rate
  over the workout; the time in five heart rate zones read from heart rate
  samples (95 to 114, 114 to 133, 133 to 152, 152 to 171 and 171 to 220 a
  minute, five 19-beat bands from half of a 190 maximum); and the speed.
  The heart rate, zone and speed charts are left out where nothing was
  measured, as on a WHOOP workout.
- **`labs/labreport`**: the report's lab, accession, issue time, status
  and review, and a table of its results with the metric, value, flag and
  reference ranges, a converted twin under the lab's own row.
- **`labs/labresult`**: every result of the same metric and channel over
  time as points (a DXA scan's trunk lists the trunk's alone), each against
  the reference range its own lab printed, the page's own result ringed,
  with the rows below.
- **`labs/metric`**, on the seeded
  `substrate.reamde.dev/observations/metric` kind: the lab results
  naming the metric, as the same points and rows. A metric no lab result
  names lists none, and the view stays hidden, so the page of a metric such
  as `health/heart_rate` is unchanged.

The WHOOP provider ships two more ([WHOOP to health](bundles-catalog.md#whoop-to-health)):
a WHOOP cycle's page and WHOOP's scores on a sleep's page.

Edit a view to change it, such as zone bounds from a measured maximum; an
upgrade keeps your edit. The console draws a view above the record's
property sheet.

### Lab results in both unit systems

The `labs` catalogue ships an `analyte` record for each metric of the eight
lab pairs (`kinds/substrate.reamde.dev/labs/metrics_analytes.yaml`, ids the
metric's): the LOINC parts
of the metric's code, the analyte's `molarMass` in grams per mole, and one
conversion onto the other metric of the pair. A mass and an amount
concentration convert by the molar mass (`molar-mass`: cholesterol 386.65,
glucose 180.156, creatinine 113.12, 25-hydroxyvitamin D 400.64, and 885.7 for
triglycerides by the clinical convention); HbA1c converts by the IFCC and
NGSP master equation, IFCC mmol/mol = (NGSP % - 2.15) x 10.929
(`ngsp-to-ifcc` and `ifcc-to-ngsp`).

Its function `convertlab`, fired by its trigger `labs-on-labresult`
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
latest values by metric, shows both. A result written before the package was
installed is converted by replaying the trigger from 0. Meter and continuous
monitor readings of glucose are observations, not lab results, and are not
converted: their units convert instead.

### Glucose in either unit

A glucose meter reports in mg/dL or in mmol/L, by country and by setting,
and one person's history can hold both. The plain units never convert to
each other, because the factor is the analyte's molar mass. The `health`
catalogue therefore ships two units annotated with the analyte
(`kinds/substrate.reamde.dev/health/analyte_units.yaml`), records of core's
`unit` kind:
`mmol/L_glucose` (code `mmol/L{glucose}`) and `mg/dL_glucose` (code
`mg/dL{glucose}`), joined by one conversion of factor 18.0156 (glucose's
180.156 g/mol).

`health/blood_glucose` anchors to `mmol/L_glucose`, so it admits a reading in
either annotated unit and refuses plain `mg/dL` and `mmol/L`. An adapter
writes the annotated code for the unit the device reported, and the value
keeps the device's digits: 97 mg/dL is stored as `97` in `mg/dL_glucose`. An
aggregate of the metric reads every value as one series, converted at read
into the request's `unit`, else the metric's (mmol/L), so a mixed history
averages, ranks and buckets together. Each title prints the unit's symbol,
`mg/dL` or `mmol/L`.

### How long a gauge value stands

Every gauge metric the `health` and `labs` catalogues ship carries a
`hold`, which a writer copies onto each value: about twice the time between
a source's readings
for a sampled stream (`PT20M` for SpO2, `PT10S` for a workout's speed), and
`PT0S` for a sporadic measurement, which is a point: a weighing, a height, a
VO2 max estimate, a night's skin temperature, a lab value and a blood
pressure. An average of `PT0S` values is the mean of the readings. The
`environment` metrics (`environment/air_temperature` and the rest) carry
none, so a writer names the hold its sensor's reporting interval implies. A
gauge value written without a hold stands until the next value of its
stream, often a day later, and the coverage, the time-weighted mean and the priority policy all read it as
still standing ([decision 0158](decisions/0158-a-gauge-carries-a-hold-a-null-ends-it-and-its-mean-is-time-weighted.md)).
A `PT0S` value holds no time, so under `priority` it covers only its own
instant: a blood pressure two apps deliver at one instant is the higher
source's alone, and one stamped seconds apart is kept twice
([G-31](plans/observations/requirements.md#g-31-one-device-under-several-sources)).

## The adapters

An adapter is a provider's function that turns its own mirror rows into
these records, each naming the mirror row it came from in `origin` and
rewritten or deleted with it. It ships in the provider's package, which
requires the `health` catalogue it writes, so installing the provider brings
both ([decision 0192](decisions/0192-observations-is-seeded-and-the-well-known-catalogues-are-first-party-packages.md)).
Two ship, each with its mapping and rules in the catalog:
[WHOOP to health](bundles-catalog.md#whoop-to-health) (`adaptwhoop`, in
`whoop`) and [Apple Health to
health](bundles-catalog.md#apple-health-to-health) (`adaptapplehealth`, in
`applehealth`). Garmin and Google Fit adapters are planned.
Every adapter that writes traces composes their ids as
[a trace's](#the-base-kinds) are,
`<provider>/<account record id>/<source>/<metric>[/<channel>]/<period start>`,
the period being the UTC hour, because one account delivers several
devices' data.

The two hold to one set of rules:

- **One reconcile.** Each hands what a mirror row implies to
  `host.observed.reconcile`, which writes each output in one put, leaves one
  that already holds it unwritten, never lowers a stored `revision`, and
  deletes what the row no longer implies.
- **One spelling.** Each writes its numbers, lengths of time, instants,
  composed ids and a night's `efficiency` through `host.observed`
  (`decimal`, `duration`, `instant`, `record_id` and `percent`), so a
  value has the same text whichever adapter wrote it.
- **The catalogue's codes and holds.** Every gauge carries its metric's
  `hold` from the catalogue, a workout's `sport` is one value of the
  workout code set (`other` where the provider's word has none, the word in
  `sportSource`), a workout's energy is `activeEnergy` or `totalEnergy` as
  the provider measures it, a day total is a `sum` summary, and sleep the
  device did not stage is the `asleep` code. A night's whole time in bed is
  its `timeInBed`, never an `in_bed` amount.
- **The analyte in the unit.** A glucose reading is `health/blood_glucose`
  in `mg/dL{glucose}` or `mmol/L{glucose}`, whichever the device reported,
  with its digits as sent: never plain `mg/dL` or `mmol/L`, and never
  converted by the adapter
  ([glucose in either unit](#glucose-in-either-unit)).
- **A night's own figures.** A sleep carries `efficiency` (time asleep over
  time in bed) and `latency` where the provider sends what they are
  computed from, and `isNap`: WHOOP sends the flag, and the Apple Health
  adapter calls a sleep shorter than three hours that begins between 09:00
  and 21:00 local time a nap.
- **What the provider cannot tell them.** Apple Health pushes no deletion,
  so a record built from a summary deleted upstream stays until its mirror
  row is deleted by hand; the adapter's delete trigger then removes it.

Every output also names the code that wrote it, in `adapter`:
`<package>@<version>` (`whoop@25`), a constant at the top of the
adapter's function whose version is the package's own. The base kinds and `health/sleep` and `health/workout`
declare it as their own property, apart from `revision`, which is the
upstream's
([decision 0162](decisions/0162-an-observed-row-names-its-origin-and-source-and-composes-its-id-from-upstream-data.md)).
An adapter upgrade does not reach mirror rows the upstream never changes on
its own. To reprocess them, raise the constant and the package's `version`
with the change, apply the new closure, and replay the adapter's record
trigger from the start:

```bash
bin/substratectl trigger replay whoop-to-health --from 0
```

Every mirror row is delivered again and every output is rewritten under the
new `adapter` at the `revision` it already holds, which the write path admits
because an equal revision is not a stale one. An output whose code is
unchanged is not rewritten.

### Correcting a value an adapter wrote

An adapter's outputs are read-only to the owner. Every delivery of a mirror
row, and every replay, writes each output whole: each property the adapter
owns is set to what the row implies, and an output the row implies is
written again if the owner deleted it. An edit to an adapter's output lasts
until the row is delivered again.

A correction is the owner's own record from the owner's own source. Write a
`source` for the device or the hand that is right (a scale, `manual`), rank
it above the provider for the metric in its `priorities`, and write the
corrected value as an observation of that source:

```yaml
kind: substrate.reamde.dev/observations/source
metadata:
  id: owner/scale
data:
  properties:
    name: My scale
    class: scale
    priorities:
      substrate.reamde.dev/observations/metric/health/body_mass: 1
---
kind: substrate.reamde.dev/observations/observation
metadata:
  id: owner/body_mass/2026-09-11T04:02:41Z
data:
  properties:
    metric: substrate.reamde.dev/observations/metric/health/body_mass
    source: substrate.reamde.dev/observations/source/owner/scale
    form: gauge
    hold: PT0S
    value:
      value: "71.8"
      unit: kg
    at: "2026-09-11T04:02:41Z"
    utcOffset: 180
```

A read with `sources.policy: priority` then answers the owner's value where
the two overlap, and `separate` shows both. The adapter never writes the
owner's source or the owner's records, so a delivery or a replay leaves the
correction and its ranking in place. The provider's own value stays as the
provider sent it, which keeps a later re-read of the provider honest.

## Moving from the samples

Before [decision 0192](decisions/0192-observations-is-seeded-and-the-well-known-catalogues-are-first-party-packages.md)
these kinds shipped as the samples `observations`, `health`, `healthapple`
and `healthwhoop`. A repository that imported them holds its rows under its
own authority (`ada.example.com/observations/observation`), and nothing
moves them to the first-party kinds. The move, by hand:

1. Take a recovery export, `bin/substratectl export > before.tar`.
2. Disable the old `healthapple`, `healthwhoop` and `health` packages, so
   nothing writes the old kinds: `bin/substratectl bundle disable
   ada.example.com/healthwhoop`, and the same for the others.
3. Install the providers again, which upgrades them (`bin/substratectl
   install providers.substrate.reamde.dev/whoop`, `…/applehealth`), and,
   for lab results, `substrate.reamde.dev/labs`. Each brings `health`. The
   upgrade creates the provider's adapter trigger, which starts at the changelog's
   start, so every mirror row the repository holds is converted into the
   first-party kinds without a hand.
4. Move what you wrote yourself (sources, source links, observations
   entered by hand, lab reports and results) through the record routes: read
   each with `get -o yaml`, rewrite its kind and every reference onto the
   first-party spelling, and `apply -f` it. A metric id moves with its
   package: a lab metric `health/<name>` is `labs/<name>`, an unprefixed
   climate metric is `environment/<name>`, and `healthwhoop/<name>` is
   `whoop/<name>`. Leave out a lab result that carries `derivedFrom`:
   `convertlab` writes it again.
5. Remove the old rows and packages. A metric any stored value points at,
   tombstones included, cannot be deleted
   ([the metric and source contracts](#the-metric-and-source-contracts)), so
   purge the old observations, traces and lab results first (`delete
   --purge`), then purge (`bundle purge --yes`) and uninstall each old
   package, the adapters before `health` and `health` before `observations`.
   The old record views (`ada.example.com/health/sleep` and the rest) are
   records of the console's `recordview` kind, which no purge of those
   packages reaches: delete them.

## Building observed values in a function

`host.units.convert`, `host.units.canonical` and `host.units.converts` ask
the repository's [unit graph](units.md) over the host call
([functions](functions.md#the-sdk)). `host.observed.trace` packs a body's
own timestamped points into trace documents that pass the rules above, one
per fixed UTC period, split where the UTC offset changes and before a
document passes the size limits, narrowed to the span a delivery covers when
the body names one; `period_id` gives
the label a trace id ends with and `local_day` the civil date at an offset.
[Functions](functions.md#the-sdk) has the signatures and an example.

## The observed index

The fold derives four tables from the rows of every kind that binds
`observed`, the way it derives `refs`
([decision 0160](decisions/0160-the-observed-index-keeps-a-segment-per-record-and-a-rollup-serves-only-exact-answers.md),
migration `0013_observed_index`). It keeps one row per record, never one per
point: a record's points stay in its own properties, and every reader that
needs them derives them again with the same code the fold runs. A rollup
row is a function of a stream's points in its bucket and of the tiers its
metric names, and a counter's rollup row also reads the stream's reading
before its bucket, which may be another record's ([below](#counters)).
Nothing else enters, no clock. `repository rebuild` clears the tables and
the background pass derives them again from the records; `repository verify`
and the export never read them.

A **stream** is one run of readings: the `kind`, `metric`, `source`, `about`
and `channel` of a value, and a summary's `statistic`, so a night's average
and its maximum at one instant are two streams. A reference column holds the
target's record path (`substrate.reamde.dev/observations/metric/health/heart_rate`);
an absent `source`, `about`, `channel` or `statistic` is the empty string.

| Table | One row per | Holds |
| --- | --- | --- |
| `observed_streams` | stream | a private `bigint` id the other tables key by, and `max_extent_ms`, the longest extent of any record the stream has held |
| `observed_segments` | observed record | its stream, `form`, `first_ts` and `last_ts` (its earliest and latest point), `end_ts` (the latest end of a span), `points` and `available` (its points, and those with a value or a code), `last_available`, `utc_offset_min`, `offset_known`, `codes` (the codes of its available points, the empty string for a number) and `fingerprint` |
| `observed_rollups` | stream, form, tier, bucket and group (`code`, `unit`, `delta`, `utc_offset_min`, `offset_known`) | `n`, `bounded`, `sum`, `min`, `max`, `mean`, `m2`, `first_ts`, `first_value`, `last_ts`, `last_value`, `covered_ms`, `held_ms`, `twa_sum`, `last_hold_ms`, `max_gap_ms`, `start_gap_ms`, `end_gap_ms`, `increase`, `resets`, `sin_sum`, `cos_sum`, `span_end`, `first_record_id`, `first_ord`, `last_record_id`, `last_ord`, `min_value`, `max_value` |
| `observed_latest` | stream | its newest available point: `ts`, `ts_end`, `value`, `code`, `unit`, `delta`, `form`, `comparator`, `utc_offset_min`, `offset_known`, `record_id`, and `points`, the stream's count of available points |

The stream id is the engine's alone: no record, changelog line, API answer
or test names it, and a rebuild may hand a stream another. A read of a
stream's range `[lo, hi)` takes the segments that start in
`[lo - max_extent_ms, hi)` and end at or after `lo`, one range of the index
on `(stream, first_ts)`, and derives their points. `max_extent_ms` only
grows: a bound too wide costs a read time, one too narrow would cost it
points.

The rules every column follows are written once, as Go, in
`internal/observed`: the engine computes each rollup row with it, composes a
coarser row with it, and the aggregate read reduces raw points with it, so a
bucket answers the same from rollup rows and from points.

### When rows are written

A write records what each record it touched now derives, and at the end of
the transaction, under the repository's observed lock, the index applies it.
The record's fingerprint, a hash of everything the index reads from it (its
stream, form, unit, hold, counter contract and every point), decides first:
a write that changes none of that, such as a `quality` or a note, writes
nothing. Otherwise the record's segment is replaced, and every bucket of
every tier its old and its new points stood in is computed again, and the
latest row of its stream. A tombstone takes the segment (a tombstone keeps
its `refs` rows and loses its points: a deleted reading is in no aggregate);
a put onto the tombstone derives it again; a purge leaves nothing.

A single record derives a point only when it holds a `value` or a `code`;
one that says why it has none (`absentReason`) or keeps the reading as
received (`sourceValue`) derives nothing, and neither does a lab result
holding only `ratio` or `text`. A summary derives a segment and no rollup
row: summaries are never pooled with the other forms.

A kind that starts or stops binding `observed` has every row derived again by the background pass behind
the commit that changed it, the one that re-derives the search index
([operations](operations.md#upgrading-the-binary)). A metric record whose
`rollups` change has the rows of its streams derived again by the same
pass. `repository rebuild` and the boot import of a repository directory
clear the tables and queue every observed kind for the pass instead of
deriving inside the replay, so the replay holds the repository's write lock
for the records alone; `repository reindex --observed` queues the same work
on a running index ([operations](operations.md#operator-recovery)). While
the pass owes a kind anything, an aggregate read over it is a `503`
([readiness](api.md#readiness)). A tombstone and a purge in that window
still take the record's segment, whatever the kind now declares: a purged
row is gone before the pass could find it.

`observed_index` records the version of the rules the tables were derived
by (`observedIndexVersion` in `internal/engine/observedindex.go`). An open
whose binary derives by newer rules, or finds none recorded, asks the same
pass to derive every row of every observed kind again and records its own
version. An aggregate read over an index recorded at older rules is a `503`
too. `TestObservedDerivationMatchesTheGolden` derives a fixed corpus and
fails when what it derives moves: a change to the derivation, the rollup
statistics, the composition, the tiers, the fingerprint or the latest rule
bumps the version.

### Points, nulls and offsets

A trace's point is at `at` plus its offset (or its index times `every`). A
gauge's and a counter's point is an instant; an amount's spans to the next
point of the trace, to its start plus `every` with regular spacing, and to
`endsAt` for the last. A single record's point spans `[at, endsAt)` where it
has an `endsAt`, and a single gauge's value holds no longer than that: its
hold is the shorter of its `hold` and `endsAt` minus `at`, so a door `open`
at 10:00 with `endsAt` 10:05 and no `hold` is open for five minutes, not
until the next point.

A `null` in `values` or `codes` is an unavailable point. It ends the hold of
the point before it and counts in nothing: not in `n`, not in `first` or
`last`, not in the latest row. A counter's steps skip it, because an outage
between two readings of a running total still accrued.

A point is available when it holds a value or a code that is not empty, and
unavailable otherwise. That one test decides every count: a segment's
`available`, a trace's `stats`, the rollup rows and the latest row. An empty
code a row stored before the write refused it is therefore an unavailable
point, the same as a `null`.

Every point takes its record's `utcOffset`: a single record's one value, and
every point of a trace. A trace has one offset because its ends say nothing
about when, or how often, the offset changed inside the period; a writer
whose points cross an offset change (a clock change, a flight) splits the
trace there, as `host.observed.trace` does. An absent offset is 0 with
`offset_known` false, which a reader tells apart from a value written at
offset 0. The aggregate read's recorded-local calendar refuses such a value,
or places it by the request's `bucket.fallbackTimezone`
([api](api.md#the-four-time-modes)); a calendar in a named zone places it by
that zone like any other.

A point and a rollup row keep their values in their own unit; the aggregate
read converts them into the read's unit at read, through the repository's
linear conversions, so a changed conversion needs no re-derivation. A
`delta` value converts by the factor alone (0.3 `Cel` is 0.3 `K`). A
circular point's angle reads its unit's `turn`, so a write that changes a
turn, or a conversion inside a circular component, queues the observed kinds
holding circular values for the background pass, and an aggregate read
over them is a `503` until it finishes. A point keeps its value's comparator (`<`, `<=`,
`>=`, `>`, a lab's `<0.01`); a rollup row counts the points that carry one
in `bounded`, and a reader answers such a value with its comparator
([censored values](api.md#censored-values)).

Points at one instant are ordered by record id, compared byte by byte (the
record id columns sort `COLLATE "C"`), then by their index in the record.
Every reader uses that order: a rollup row's first and last point, a raw
read and the latest row pick the same point of a tie.

### Rollup tiers

A metric names the widths its streams are rolled up in, in its `rollups`:
any of `5m`, `15m`, `1h` and `1d`, finest first. A metric that names none
takes `15m`, `1h` and `1d`; one that names an empty list keeps no rollup
row, and every read of it takes raw points.

A metric whose sources report about once a minute or more often (a home
sensor, a meter) names `["1h", "1d"]`. At a reading a minute the `15m` tier
is four rows an hour per stream: in [the benchmark](plans/observations/benchmark.md)
it was 79% of the rollup rows, and dropping it took a sensor's index from
about 50 bytes a reading to about 16. A read in 15-minute buckets then takes
the raw points: 146 ms for a week of one sensor, against 45 ms from `15m`
rows. A tier's bucket starts at a
multiple of its width **in the group's own offset**, and in UTC where the
offset is unknown, so a `1d` row of a `+02:00` group is a local day and a
`1h` row of a `+05:30` group starts at half past the UTC hour.

The finest tier is computed from the points. Each coarser one is composed
from the next finer tier's rows inside its bucket (`observed.Compose`): the
counts and sums add, the extremes compare, the first and last are the first
row's and the last row's, a gauge's held time walks the finer rows and
carries the value between them, and a counter's steps add. Where the finer
rows cannot say a coarser row exactly, it is computed from the points: a
finer row of another alignment crosses its bucket (the day of a clock
change), or an amount's spans overlap across finer rows. A write recomputes
the buckets of every tier its points stood in.

Within a bucket a stream's points of one form split into **groups** by
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
  included; `start_gap_ms` and `end_gap_ms` are those two ends. **A bucket
  may answer a gauge's `avg` and `timeInState` from `held_ms` only when the
  request's `maxGap` is absent or not below the bucket's `max_gap_ms`**;
  otherwise the read takes a finer tier, and at the finest the raw points.
- An amount belongs whole to the bucket its span starts in; `covered_ms` is
  the union of its row's spans and `span_end` the end of the latest of them,
  so the union lies inside `[first_ts, span_end)`. A read joining two rows
  adds their `covered_ms` only where those extents do not overlap
  (`internal/observed.UnionMs`).
- A bucket holding only unavailable points still has a row, with `n` 0, so
  the read sees where a hold stops.

### How a read picks its rows

For each stream the read covers its windows with rows, the coarsest tier
first. A tier's bucket is taken where it lies inside what is still to be
covered, crosses none of the read's bucket boundaries (in the recorded-local
calendar: lands in one local bucket at its own offset, and does not cross
the range's start), overlaps no row of another alignment, and, for a gauge,
is eligible under the request's `maxGap`. A stretch no row of a tier meets
holds no point. What no tier covers is read from the points: the parts of
an amount's or a counter's buckets, merged into the rows, or for a gauge or
a recorded-local read the whole group. A year of Berlin days of a home
sensor is one `1d` row a day, and the hour rows on the days around each
clock change, where the `+01:00` and `+02:00` day rows overlap.

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

`observed_latest` holds each stream's newest available point. A summary's
`statistic` is part of its stream, so a night's `avg` and `max` summaries at
one instant are two rows, and neither stands for the other. The aggregate
read takes each row as a stream of its own, so a row of a bucketed read
reads one statistic, and the latest read answers the newest of a group's
rows with the sum of their `points`. A write moves `points` by the available
points it added or took away, and the background pass counts it again from
the segments, so a re-derivation repairs a count that drifted.

Two points at one instant go to the greater record id in byte order, then
the later point of one trace: the point the `last` reducer answers. A
`revision` counts one record's rewrites, so it breaks no tie between two
records.

A write that moves a stream's newest point to another stream (a corrected
`source`) recomputes both streams' rows, and a row left with no available
point goes. A null after the newest value does not replace it: the row keeps
the last value, and a reader that must say "unavailable since" finds the null
among the stream's points.

### How a sensor bridge writes

A bridge that forwards a home's sensors (Home Assistant, a meter reader)
writes each sensor's readings as **closed chunks**: one trace per sensor and
hour, or per quarter hour, written once the period has ended and never
appended to. Its id is composed from the sensor and the chunk's start
(`homeassistant/sensor.kitchen_temperature/2026-10-25T14`), so a resend of
the same chunk lands on the same record, and a resend with the same values
moves nothing in the index: the segment's fingerprint holds. A chunk carries
one `utcOffset`; the hour a clock changes splits where the offset changes.
The metric a sensor writes to keeps the `1h` and `1d` tiers, as the six
shared metrics do ([rollup tiers](#rollup-tiers)).
A sensor that reports numbers writes `values`; one whose state is a word or
free text writes `codes` ([free-text states](#free-text-states)).

What each choice costs, measured on a durable Postgres
([the benchmark](plans/observations/benchmark.md)):

| Choice | Cost |
| --- | --- |
| One trace per sensor and hour | about 160 bytes a reading: 144 in the records and the changelog, about 16 in the index with the `1h` and `1d` tiers; 8,760 writes a sensor a year, each about 120 to 160 ms on a disk that takes 40 ms to sync |
| One trace per sensor and quarter hour | four times the writes, 35,040 a sensor a year, and four times the segments; each write costs about what an hour's does, so the write time is four times as much |
| One record per reading | a records row, a changelog line and a segment for every reading, each carrying the fixed part of the 8.7 KB an hour's trace of 60 readings costs, and a write for each |
| Appending to an open chunk | every append rewrites the whole record: its changelog line grows with the chunk, so an hour appended a minute at a time writes about 30 times its readings, and each append recomputes the chunk's buckets |

A dashboard that shows the current value reads it from the bridge, or from
the latest row once the chunk is written; the substrate keeps the history.

### Free-text states

A sensor whose state is neither a number nor one of a fixed list of codes,
such as the title a media player shows or a thermostat's mode string, is
written as an observed code under a metric that lists no `codeSet`. No write
is held to a metric's `codeSet`, so any non-empty string of at most 255
characters is a code, and a bridge writes it as it writes a door's `open`: a
coded gauge, `code` on an observation or `codes` in a trace. Where the
sensor is unavailable, a trace holds `null` at that point, and a single
observation leaves out `code` and says why in `absentReason` (such as
`unavailable`), because a single record with neither a value nor a reason
is refused.

```yaml
kind: substrate.reamde.dev/observations/metric
metadata:
  id: homeassistant/media_title
data:
  properties:
    name: Media title
    form: gauge
    codes:
      - {system: homeassistant, code: media_title}
```

A read takes a coded gauge's reducers: `count`, `first`, `last` and
`timeInState`, which answers the time each title held, one value per code.
`durationIn` takes a range of numbers and is refused on codes.
`groupBy: ["code"]` answers one row per distinct state, which `maxGroups`
counts. The [latest read](api.md#the-latest-read) answers the last
available state, not necessarily the current one: an unavailable point
after it does not replace it ([the latest row](#the-latest-row)). A reader
that must say whether the sensor is unavailable now lists the stream's
records that end after the latest row's start (`endsAt` greater than it),
which takes a trace that began before that instant, and those that start at
or after it, then reads each point's own instant, one at the same instant
included, for a `null` or an `absentReason`. Listing only the records that
start after it misses an outage inside the trace that holds the latest
state.

Two kinds of text are not states and go in ordinary records of a kind of
your own: text longer than 255 characters, and text that is content rather
than a reading, such as a notification's body or a transcript. A state's
attributes (a track's artist and album) are each a metric of their own when
they are read over time, and otherwise belong on such a record.

## Reading

One record is a list read, as for any kind: today's WHOOP recovery is
`filter.properties.periodId` equal to last night's sleep id beside `metric`
equal to the `whoop/recovery` metric, and every body weight under 155 lb is
`metric` equal to the `health/body_mass` metric and `value` less than
`{"value": "155", "unit": "lb"}` (without the metric, muscle and bone
mass rows match too). Everything bucketed is the records route's
[aggregate read](api.md#the-aggregate-read): a metric, a range, a time mode,
reducers checked against the form, and a source policy, answered from the
rollup rows where they answer exactly and from the points otherwise. The
requests below are the proposal's screens
([reading the data](plans/observations/proposal/03-reading-the-data.md)),
each one request. They name source ids from the demo repository
[synthetic data](synthetic-data.md) loaded while it carried a Garmin adapter
too (`garmin/ada/forerunner265`, `whoop/e2e`); Garmin is planned and not
shipped. [Building health apps](health-apps.md) has more, a section for
each screen of a health app, each with the answer a demo repository gave.

Heart rate zones per week for six months is
[the aggregate read's first example](api.md#the-aggregate-read) with its
range widened from one week to 26 and one `durationIn` reducer per zone (95
to 114, 114 to 133, 133 to 152, 152 to 171 and 171 to 220 a minute).
`durationIn` reads raw points, and `maxSamples` counts every selected
source's points, not only the ones the policy keeps. A source sampling
every 5 minutes writes 288 heart rate points a day, so 26 weeks of it are
about 52,000, inside the default `maxSamples` of 1,000,000. The 182 days of a one-second stream are 15.7
million, past the cap of 2,000,000 one read may hold, so such a stream is
read a few weeks at a time.

Daily steps for a year on each sample's own local day. Under the default
`separate` policy each source is a row of its own, and `prefer-raw` serves
each source's day from its step amounts where they cover 90 percent of it
and from its daily total (a `sum` summary) otherwise; each row's `from` says
which. Apple Health writes a `sum` summary only from an export grouped by hour, day,
week, month or year, under the device the export's row names, or under the
Health app's own source (`applehealth/<account>/health`) where the row names
no device or joins several. A day of a source with no total is served from
its amounts whatever they cover. Four step sources times 365 days is 1,460
series times buckets, over the default
`maxBuckets` of 1,440 ([budgets](api.md#budgets)), so the read raises it:

```json
{"aggregate": {
   "metric": {"ref": "substrate.reamde.dev/observations/metric/health/steps"},
   "from": "prefer-raw",
   "range": {"gte": "2025-10-05T00:00:00+03:00", "lt": "2026-10-05T00:00:00+03:00"},
   "bucket": {"calendar": "day"},
   "reducers": ["sum"],
   "unit": "steps",
   "budget": {"maxBuckets": 2000}}}
```

Weight weekly with a four-week rolling mean, one row per source, from rollup
rows alone:

```json
{"aggregate": {
   "metric": {"ref": "substrate.reamde.dev/observations/metric/health/body_mass"},
   "range": {"gte": "2025-10-06T00:00:00+03:00", "lt": "2026-10-05T00:00:00+03:00"},
   "bucket": {"calendar": "week", "weekStart": "monday", "timezone": "Europe/Athens"},
   "reducers": ["avg", "min", "max", "count"],
   "rolling": {"buckets": 4, "reducers": ["avg"]},
   "unit": "kg"}}
```

Garmin's nightly heart rate variability per wake day, with the 30-night
baseline before it: `place` `end` puts a night on the day it ends, however
late it starts; the statistic filter keeps Garmin's nightly average and
leaves out its maximum; and the rolling window ending at the previous day
leaves the night out of its own baseline (its `n` says how many nights the
window held):

```json
{"filter": {"properties": {"source": {"eq": "garmin/ada/account"}, "statistic": {"eq": "avg"}}},
 "aggregate": {
   "metric": {"ref": "substrate.reamde.dev/observations/metric/health/hrv_rmssd"},
   "from": "summaries",
   "range": {"gte": "2026-09-01T00:00:00+03:00", "lt": "2026-10-01T00:00:00+03:00"},
   "bucket": {"calendar": "day", "timezone": "Europe/Athens", "place": "end"},
   "reducers": ["last"],
   "rolling": {"buckets": 30, "reducers": ["avg"], "through": "previous"},
   "unit": "ms"}}
```

WHOOP writes a night's heart rate variability (its `vendor` value) as a
summary over the night, as Garmin does, so the same read of `whoop/e2e` with
`statistic` `vendor` keeps `place` `end` and lands each night on its wake
day.

Sleep stages per night: the coded stage amounts summed by code inside each
`health/sleep` record that is not a nap, one row per night and code, each
carrying the night's `record`. Every device writes the night as a sleep
record of its own, so `bucket.records.sources` keeps one per night, the
WHOOP's where it has one and the Garmin's otherwise, and the request's
`sources` keeps one device's stages inside it. `utcOffsets` writes each
row's start and end at the night's own offsets, so a night slept abroad
reads in the time it happened:

```json
{"filter": {"kinds": ["substrate.reamde.dev/observations/observation"]},
 "aggregate": {
   "metric": {"ref": "substrate.reamde.dev/observations/metric/health/sleep_stage"},
   "range": {"gte": "2026-09-01T00:00:00+03:00", "lt": "2026-10-01T00:00:00+03:00"},
   "bucket": {"records": {
     "filter": {"kinds": ["substrate.reamde.dev/health/sleep"], "properties": {"isNap": {"eq": false}}},
     "sources": {"policy": "priority", "order": ["whoop/e2e", "garmin/ada/account"], "property": "source"},
     "utcOffsets": {"start": "utcOffsetStart", "end": "utcOffsetEnd"}}},
   "groupBy": ["code"],
   "reducers": ["sum"],
   "sources": {"policy": "priority", "order": ["whoop/e2e", "garmin/ada/account"]},
   "unit": "min"}}
```

A night and its recovery: WHOOP writes a recovery, and its heart rate
variability, resting heart rate, SpO2 and skin temperature, over the night
it scores, so each WHOOP night's records-mode window holds them. Each row
carries the night's `record` beside its recovery:

```json
{"filter": {"kinds": ["substrate.reamde.dev/observations/observation"]},
 "aggregate": {
   "metric": {"ref": "substrate.reamde.dev/observations/metric/whoop/recovery"},
   "from": "summaries",
   "range": {"gte": "2026-09-01T00:00:00+03:00", "lt": "2026-10-01T00:00:00+03:00"},
   "bucket": {"records": {
     "filter": {"kinds": ["substrate.reamde.dev/health/sleep"],
                "properties": {"source": {"eq": "whoop/e2e"}, "isNap": {"eq": false}}}}},
   "reducers": ["last"],
   "unit": "percent"}}
```

A window is `[at, endsAt)`, so a source that stamps a nightly value at the
instant the night ends puts it outside its night; such a value is read by
the day it ends, `calendar` with `place` `end`.

Training time and distance per sport per week, from the workout records
themselves (a property read): each session once where several devices
recorded it, the Garmin's first, and each week on the local days the
sessions began:

```json
{"filter": {"kinds": ["substrate.reamde.dev/health/workout"]},
 "aggregate": {
   "property": {"name": "duration", "utcOffset": "utcOffsetStart", "groupBy": ["sport"],
                "sources": {"policy": "priority", "property": "source", "match": ["sport"],
                            "order": ["garmin/ada/forerunner265", "whoop/e2e"]}},
   "range": {"gte": "2026-09-07T00:00:00+03:00", "lt": "2026-10-05T00:00:00+03:00"},
   "bucket": {"calendar": "week"},
   "reducers": ["sum", "count"],
   "unit": "h"}}
```

The same read with `"name": "distance"` and `"unit": "km"` is the weekly
distance. The policy weighs only the records that hold the property read,
so a higher-ranked device's workout without a distance leaves the next
device's distance in.

kWh per day from a meter that resets, reset-proof, for a meter a function
writes as a counter under the `environment` catalogue's `environment/energy`
metric (no shipped adapter writes one):

```json
{"aggregate": {
   "metric": {"ref": "substrate.reamde.dev/observations/metric/environment/energy"},
   "range": {"gte": "2026-09-01T00:00:00+03:00", "lt": "2026-10-01T00:00:00+03:00"},
   "bucket": {"calendar": "day", "timezone": "Europe/Athens"},
   "reducers": ["increase"],
   "unit": "kW.h"}}
```

The current value of every stream, the dashboard, is
[the latest read](api.md#the-latest-read), `"latest": true`. An 800-pixel chart of any range is
`downsample={"points": 800}` beside a metric and a range
([downsample](api.md#fill-rolling-and-downsample)).

A function sends the same body to `host.records.aggregate`. An agent sends
its three parts as the `query` tool's arguments of the same names, `filter`,
`aggregate` and `downsample`, and `substratectl aggregate` builds it from
flags.
