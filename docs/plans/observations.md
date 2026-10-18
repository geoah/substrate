# Observations: one model for every measured thing

Version 2, all domains. Status: research and proposal, 2026-10-08, reviewed
2026-10-10 (section 13 records the review's verdicts, which supersede the
text above them where the two differ, and links the current proposal). Nothing
here is built and no decision record is written yet; section 10 lists the
records this plan would need. Version 1, [health-data.md](health-data.md), is
the health-only proposal with its own research and catalogue; where the two
disagree, this page is the current proposal and section 2 says exactly what
changed. The four research reports behind this version are under
[docs/plans/observations/](observations/01-iot-models.md), and
`observations/coretable.py` regenerates the unit table in section 6 with exact
rational arithmetic.

The question: should the health model (a `metric` definition, an
`observation` with a form, a packed `trace`, a `source`, a `quantity` value
type with UCUM units, a derived index with rollups, an aggregate read) widen to
every observed quantity a person accumulates (home sensors, energy meters,
weather, vehicles, plants, a second person's health), and should the engine
ship core units and conversions for well-known quantities?

## 1. The answer in one page

**Yes to the engine, one domain at a time to the packages.** Nothing in the
engine pieces of version 1 is health-specific: the value type, the unit table,
the form of a value, the packed trace, the derived index, the aggregate read.
Every standard and product studied for this version reduces to the same five
shapes (a level at an instant, an amount over a span, a running counter, a
categorical state, a summary row) with the same five parts (what is observed,
of what, by what, when, the result with its unit), and version 1 already had
four of the five parts. It lacked the fifth, the thing observed, because in
health the thing is always the user.

Two facts in the code make the widening urgent rather than optional:

1. **A core trait cannot gain a property after it ships.** A kind that binds a
   trait must declare every property the trait lists, and traits have no
   optional properties. Adding "the thing observed" later would break every
   user's imported copy of `observation`. The trait must be complete in its
   first version.
2. **A sample's kind can never be moved by an upgrade.** `movedFrom` works
   only on the shipped boot upgrade of a seeded package. Where the base kinds
   live is fixed by the first record written.

So the proposal, in seven pieces:

1. The engine pieces ship domain-neutral from day one: a core trait
   `observed` whose contract already includes the thing observed (`about`), a
   categorical value (`code`), the hold rule and the counter fields, even
   though health writes none of them (section 4).
2. Four forms with domain-neutral names: `gauge`, `amount`, `counter`,
   `summary`. A categorical value is a gauge whose value is a code, not a
   fifth form. Counters are stored as sent and differenced at read, with a
   declared reset rule (section 5).
3. An engine-owned unit table of about 200 UCUM codes in 54 named quantity
   kinds, with exact rational factors, grown by release only, and conversion
   allowed only within one kind (section 6).
4. The four base kinds (`metric`, `observation`, `trace`, `source`) live in a
   small base sample, `observations`, that every domain sample requires, the
   way `calendar` requires `people`. No generic `subject` kind: rooms, cars and
   plants are typed kinds in their domain samples, and `about` points at them
   (section 7).
5. The derived index gains `about`, `code`, the offset and the kind in its
   keys, and the rollup row gains what the new reducers need (`twa`,
   `timeInState`, `increase`, `rate`, `circularMean`), plus a small derived
   table for "the current value of everything" (section 8).
6. Domains are ranked by fit and value. Health ships first, unchanged in
   scope; a minimal `home` sample fed by Home Assistant is the second domain,
   because it exercises the four seams health never touches (a thing observed
   that is not the user, categorical values, counters that reset, many
   sources). Everything else waits for a consumer (section 9).
7. Substrate is the archive and the analytics store for a home, not its live
   state bus. Home Assistant keeps doing the second-by-second work; substrate
   receives traces in bounded batches and keeps rollups forever (section 9.3).

**What this does not propose:** a generic `subject` kind, a `location`
property on every value, user-declared units, a fifth `state` form, a
persistent stream record, forecasts in the index, currency as a unit, or
infrastructure metrics as a domain.

## 2. What changed from version 1

Every change below was forced by one of the four reports or by the code; the
report is named so the reasoning can be read in full.

| Version 1 | Version 2 | Why |
| --- | --- | --- |
| trait `samples`, tables `samples` and `sample_rollups` | trait `core/observed` with variants `single` and `packed`; tables `observed_points`, `observed_rollups`, `observed_latest`; one value inside a trace is a `point` | `sample` is the catalog tier (decision 0048); "the health sample's samples trait" is unreadable (design assessment, section 4) |
| forms `instant`, `interval`, `counter`, `summary` | `gauge`, `amount`, `counter`, `summary` | the form's job is to say what the number means, not when; `interval` also collided with the trace's spacing property; these are the OpenMetrics and Home Assistant words every IoT reader knows |
| no thing observed; the user is implicit | `about`, a reference pinned to `kind: any`, absent means the owner, on every observation and trace and in the index key | every standard has a feature-of-interest slot; a house has sixty things observed, not one; a parent's repository holds two children (domains report, part 3) |
| categorical values only on `labresult` | `code` beside `value` on every observation and trace, exactly one set; `code` in the rollup key; reducers `count`, `last`, `timeInState` | doors, presence, modes, moods, habits, diapers (domains report); a boolean is a code, never `0` and `1` |
| a counter is differenced into intervals at ingest | a counter is stored as sent, indexed, and differenced at read under a declared `resetRule` with `resetAt`; rollups carry `increase` and `resets` | the meter reading is the fact; differencing at ingest needs the previous period's last point and breaks on late data (IoT report, section 11.3) |
| no hold rule; averages are sample means | `hold` on the value; `twa` (time-weighted mean) reducer; `maxGap` on the request; rollups carry `twa_sum`, `held_ms`, `last_hold_ms` | most home protocols report on change, so a thermometer that sits at 21.5 for three hours has one point and a sample mean is wrong (IoT report, section 10) |
| `trace.interval` for the regular spacing | `trace.every` | one kind must not use one word for a form value and a property |
| `source.kind`, `tag.kind`, `source.model` | `source.class`, `tag.class`, `source.hardwareModel` | `kind` and `model` are terms in `docs/terms.md` |
| `observation.context` object with health keys | `qualifiers`, repeated `{system, code}` | a neutral kind cannot carry health keys; a domain extends by code, not by editing the base |
| `metric.loincParts`, `molarMass`, `conversions`, `referenceLow`, `referenceHigh` on `metric` | moved to `health/analyte`, a health kind pointing at the metric | a domain never edits a base kind; the base's upgrade must not depend on health's |
| `metric.vendor`, `algorithmVersion` | `metric.algorithm` `{vendor, name, version}` | a Tesla battery-health percentage or an air-quality index is a vendor score too |
| a unit table organised by dimension vector | named quantity kinds, each with a dimension vector; `temperature` and `temperature-difference` are two kinds sharing codes | a UCUM reference library converted newton-metres to joules, grays to sieverts, hertz to becquerels and percent to beats without complaint (units report, section 2.3) |
| arbitrary units "convert by prefix" stated as UCUM | the same rule, labelled as a substrate extension and restricted to the same atom and annotation | UCUM forbids it and ucum-lhc refuses it; labs still send TSH in both `m[IU]/L` and `u[IU]/mL` (units report, section 1.5) |
| aggregate `"window": {"before", "after"}` on record buckets | `around` | `window` is the window read |
| `maxBuckets` counts buckets | `maxBuckets` counts groups times buckets | "average temperature per room per day for a year" over sixty rooms is 21,900 rows |
| health is the first and only package | base sample `observations`; `health` requires it; `home` is the second domain | design assessment, section 3 |

What did not change: the `quantity` value type's rules (exact digits, a UCUM
code, comparator only where declared, conversion refused across kinds and
across incompatible metrics), the trace as one record per fixed period with a
monotonic revision, the derived index as a pure function of the fold, the
aggregate read as a mode of the records route with four named time modes and
a `from` choice between raw points and vendor summaries, the composed id
recipe, mirrors in the vendor's units with canonicalisation in adapter
functions, the deletion decision, and every health-specific kind.

## 3. What the wider world looks like

### 3.1 Five shapes, five parts

Home Assistant, OGC SensorThings, SOSA/SSN, Matter, Zigbee, HomeKit, Z-Wave,
Sparkplug, Haystack, Brick, OpenADR, DSMR, Victron, Enphase, Tesla, Green
Button, OBD-II, Smartcar, Traccar, Open-Meteo, NWS, Netatmo, PurpleAir,
OpenTelemetry, Prometheus and InfluxDB were read for this version
([01-iot-models.md](observations/01-iot-models.md), with a side-by-side table
in its section 7). Stripped of vocabulary they hold five shapes:

| Shape | Example | Version 2 name |
| --- | --- | --- |
| a level at an instant, held until the next | 21.5 Cel in the living room; 62 /min; a door `open` | `gauge` (numeric or coded) |
| an amount accrued over a span | 412 steps over [07:00, 07:15); 1.2 kW.h this quarter hour | `amount` |
| a running total that may reset or wrap | the meter at 18342.655 kW.h; the odometer; a rain gauge's daily total | `counter` |
| a statistic someone computed over a stated period | Home Assistant's hourly mean; a vendor's daily total; a trailing 10-minute PM2.5 average | `summary` |
| a session with children | a sleep, a workout, a trip, a charge, a print job | a typed kind in a domain sample, never an observation |

And the same five parts, which the standards name the same way: what is
observed (`metric`), of what (`about`), by what (`source`), when (`at`,
`endsAt`, the offset), and the result with its unit. Every standard also
separates the time the property had the value from the time the result was
known; version 2 adds an optional `issuedAt` for that and keeps forecasts out
of the index.

### 3.2 What home data has that health data does not

The differences a shared model absorbs, each from the IoT report's section 10:

- **Counters are primary and they reset**, in at least six ways: never
  (odometers), on a meter swap, at a dial maximum (Green Button's bulk
  register, ANT+ counters that wrap every 64 s), at a local calendar boundary
  (daily rain, monthly meters), after an event (a dry spell), or detected by a
  drop (Home Assistant's rule: below 90 percent of the previous value).
- **The reset epoch is metadata on the value**: OpenTelemetry's start time,
  Home Assistant's `last_reset`, Matter's `CumulativeEnergyReset`.
- **Sampling is on change, not on a clock**, gated by a minimum interval, so
  a quiet sensor writes nothing for hours and the time-weighted mean is the
  only correct average.
- **The thing observed is not the user**, and ten room thermometers are ten
  things observed, not ten sources competing for one truth.
- **Many clocks per value**: device time, fix time and server time (Traccar);
  the OEM's, the fetch and the delivery time (Smartcar); no clock at all, so
  the reader stamps it (OBD, ANT+, AirGradient).
- **Interval timestamps differ by convention**: stamped at the end (Enphase,
  Haystack consumption, Davis archives), at the start with a duration (Green
  Button, OpenADR), in the middle (Netatmo), or "the preceding hour"
  (Open-Meteo). Canonicalisation turns all of them into `[at, endsAt)`.
- **Device-defined precision and scaling**: Matter centidegrees, half-percent
  battery, a log-encoded lux, Zigbee multiplier and divisor attributes,
  Z-Wave precision per report, rain-gauge tips. All decoding belongs in the
  adapter; the decoded value keeps exact digits.
- **Retention tiers are normal**: Home Assistant purges raw states after ten
  days and keeps hourly statistics forever. Users accept it.
- **Circular quantities**: wind direction and heading; the arithmetic mean of
  350 and 10 degrees is 180.
- **Rolling-window summaries** (rain in the last 60 minutes, a 10-minute PM2.5
  average) overlap and must never be summed.
- **Direction and partitions**: import and export, charge and discharge,
  tariff 1 and tariff 2 are separate metrics or labels, never a sign each
  vendor defines differently.
- **Volume**: a 16-circuit energy monitor at 1 Hz is about 1.4 million samples
  a day, thirty times version 1's heavy health user.

### 3.3 Which domains are observations

The domains report ([03-domains-and-prior-art.md](observations/03-domains-and-prior-art.md))
assessed eighteen candidate domains and ranked them. Condensed:

| Rank | Domain | Fit | What it needs beyond version 1 |
| --- | --- | --- | --- |
| 1 | home sensors and energy | high | `about`, coded values, stored counters with resets, the hold rule |
| 2 | weather and air quality at places | high, and high value as context for health | `about` = a place, `issuedAt`, forecasts kept out, circular mean |
| 3 | a second person (baby tracking, a partner's data) | high | `about` = a person; the clearest case that version 1 was wrong inside health |
| 4 | mood, journaling, habits | high | coded and ordinal values, local-date identity for daily summaries |
| 5 | meter and odometer readings by hand | high | stored counters |
| 6 | vehicle telemetry | medium to high | `about` = the car, typed `trip` and `charge` sessions, positions |
| 7 | location history | medium | a `geopoint` value type and a packed `track`; positions are not quantities |
| 8 | garden and weather stations, pets, plants, aquariums | high, modest value | `about` |
| 9 | finance balances and prices | medium | money is not a unit; a balance is a typed daily summary of an `account` kind |
| 10 | screen time | low raw, medium derived | a typed span kind; derived minutes per category are amounts |
| not observations | spending, media plays, parcel tracking | events with their own identity | typed kinds; their counts come from the aggregate read's `property` path |
| out of scope | computer and infrastructure metrics | millions of points a day per host, open label sets | a time-series database, not a personal store |

The systems that tried to be the general store split cleanly. The ones that
accepted anything (HAT's any JSON, Solid's RDF, Open Humans' files, Nomie's
note strings, Google Fit's custom types) pushed all meaning onto each reader,
and Google deprecated custom types because only the app that wrote one could
read it. The ones alive and liked stayed narrow on one axis: Exist.io on grain
(one typed value per local day, which is what makes its cross-domain
correlations work), Apple Health on subject (always the user), Home Assistant
on purpose (one home, value semantics declared per series; in 2025 it had to
make the unit class and the circular mean explicit, the same correction
version 1's reviews made). The lesson is the split version 1 already chose
inside health, applied one level up: generic scalars, typed sessions, and
every value carrying its own meaning.

## 4. The `observed` trait

A core trait, recognised by the host the way `accountconfig` and `sync` are,
with two variants that describe layout only (one value per record, or many):

```yaml
kind: substrate.reamde.dev/core/trait
metadata:
  id: substrate.reamde.dev/core/observed
data:
  authority: substrate.reamde.dev
  package: core
  description: "observed values the substrate indexes: one per record, or
    many packed into one record over a fixed period"
  oneOf:
    - name: single
      properties:
        at: datetime
        endsAt: datetime
        metric: reference
        source: reference
        about: reference
        form: string
        value: quantity
        code: string
        statistic: string
        period: string
        hold: duration
        resetRule: string
        resetAt: datetime
        utcOffset: int
        revision: int
    - name: packed
      properties:
        at: datetime
        endsAt: datetime
        metric: reference
        source: reference
        about: reference
        form: string
        unit: string
        every: duration
        offsets: int
        values: decimal
        codes: string
        hold: duration
        resetRule: string
        resetAt: datetime
        utcOffsetStart: int
        utcOffsetEnd: int
        revision: int
        stats: json
```

Every property is contracted from the first version because of fact 1 in
section 1; health writes `about`, `code`, `hold`, `resetRule` and `resetAt` on
nothing, and pays nothing. The host validates what a trait cannot express:
exactly one of `value` and `code` (or neither with `sourceValue` set), equal
lengths of `offsets` and `values` or `codes`, offsets ordered and inside the
period, a `form` from the four words, `statistic` and `period` present on a
summary, `resetRule` only on a counter, `hold` only on a gauge.

**`about`.** A reference, `kind: any`, optional, never cascading. Absent means
the repository's owner. A living-room temperature is `about` the room, a fuel
level `about` the car, a soil moisture `about` the plant, a weather reading
`about` the place, a child's weight `about` the child. Why a property on the
value and not on the metric or the source: the same metric is observed about
sixty things, a sensor can be moved to another room, and the engine must never
read another record to know what an indexed number means. Why in the index
key: a filter to one room and a group by room must read rollups, not raw
points. Why not a `subject` word: it is the mapping's `subject: true`
reference already. Why not a marker trait the observed things bind: a trait
with no properties that rooms, cars and people all bind is "anything" under
another name, and `kind: any` already stores the full path the index needs.

**No `location` property.** A fixed sensor's location is a fact about the
thing it observes (the room is on the ground floor), held on that record. A
moving source's position is itself an observation (section 5.5). A weather
reading is `about` a place record. A `location` on every point would copy the
room onto millions of rows and go stale when a sensor moves.

## 5. Forms, values and the hard cases

### 5.1 The four forms

| form | means | time | legal reducers (numeric) | legal reducers (coded) |
| --- | --- | --- | --- | --- |
| `gauge` | a level at an instant, held for `hold` | `at` | `avg`, `twa`, `min`, `max`, `first`, `last`, `count`, `stddev`, `percentile`, `durationIn`; `circularMean` on an angle | `count` by code, `first`, `last`, `timeInState` |
| `amount` | a quantity accrued over `[at, endsAt)` | `at`, `endsAt` | `sum`, `count`, `min`, `max` | refused |
| `counter` | a running total that may reset | `at` | `increase`, `rate`, `last` | refused |
| `summary` | a statistic over a stated period | `at`, `endsAt`, `statistic`, `period` | read as is; `avg` of summaries is labelled a mean of means; never pooled with the other forms in one bucket | read as is |

A reducer not legal for the form is a `422` naming the form, as an undeclared
filter operator is refused today. `sum` over a gauge is the canonical mistake
this prevents; so is `avg` over a counter.

### 5.2 Categorical values

A door's `open`, a presence `home`, an HVAC `heating`, a sleep stage, a mood
`good`, a lab `Not detected`: the value carries `code` instead of `value`, the
form stays `gauge` (a level held until the next change) or `amount` (a stage
over a span). The metric's `codeSet` lists the admitted codes for a reader; a
boolean is two named codes (`open`, `closed`), never `0` and `1`, because a
mean of door states is meaningless and the form check must be able to refuse
it. A doorbell press or a motion trigger is a coded gauge with `hold: PT0S`:
countable, holding no time. Not named `state`, because `state` is a datatype
(a state machine the substrate polices) and a door's code is data it does not.

`timeInState` is the held duration per code under the hold rule;
`durationIn` on numbers (time above 140 mg/dL) is the same computation on a
range.

### 5.3 Counters that reset

The counter is stored as sent and indexed as it is. On the value:

- `resetRule: decrease`: any drop is a reset to zero, then counting resumes
  (Home Assistant `total_increasing`, an odometer rollover, a plug that
  reboots).
- `resetRule: declared`: the value may fall (a net meter exporting solar);
  only a changed `resetAt` starts a new accumulation (Home Assistant `total`
  with `last_reset`, OpenTelemetry's start time, Matter's reset timestamp).
- `resetRule: wrap` with `wrapAt` on the metric: the register wraps at a dial
  maximum (Green Button's bulk register; ANT+ counters that wrap at 256 m).

The rollup row stores `increase` (the reset-aware sum of positive steps
between consecutive points inside the bucket) and `resets`; the step from one
row's last point to the next row's first point is computed at read time with a
window function, so projecting one trace never reads another and a correction
to an earlier trace re-derives only its own buckets. `rate` is `increase`
divided by the bucket's covered duration, so a `kW.h` counter reads as `kW`.
No extrapolation to the window boundaries (Prometheus extrapolates; exact numbers or
refusal says a partial bucket says so instead).

### 5.4 The hold rule and time-weighted means

`hold` on a gauge value is how long it stands after its timestamp when no
newer point exists; absent means "until the next point", capped by the
request's `maxGap`. A periodic sensor sets twice its period, an on-change
sensor with a heartbeat sets the heartbeat, a press sets `PT0S`. `twa`, the
time-weighted mean, is the default a console should pick for on-change data;
rollups carry `twa_sum`, `held_ms` and `last_hold_ms`. The point current at a
window's start is carried in (Home Assistant's `include_start_time_state`); an
adapter writes the previous period's last value at offset 0 of a new trace so
the hour never opens with a gap.

### 5.5 Positions, angles, levels, money

- **Positions are not quantities.** Averaging latitude and longitude is wrong
  across the antimeridian and meaningless as a statistic; distance is
  geodesic; the useful filters are spatial. Version 1 listed `latitude` and
  `longitude` as instant metrics; that works for storage and fails for every
  question asked of a track. A `geopoint` value type (`{lat, lon, alt?,
  accuracy?}`, WGS84) and a packed `track` whose managed stats are a bounding
  box, a length and a duration are a phase of their own (section 11), and
  until then positions stay inside workout and vehicle mirrors.
- **Angles** are their own quantity kind with base `deg` (exact for everyday
  data; radians round); a direction (wind, heading) declares `circular: true`
  on the metric, `avg` becomes `circularMean` from sums of sines and cosines
  in the rollup, and `sum` is refused.
- **Logarithmic levels** (`dB`, `dB[SPL]`, `[pH]`) are stored and compared in
  their own unit, converted by nothing, and `sum` is refused; an energy-mean
  reducer for sound exposure can come when a consumer asks.
- **Money is not a unit** and currency is not a dimension: an exchange rate is
  a market observation with a date, not a definition. A balance or a price is
  a typed kind with a `money` property, aggregated through the records path.
  A price per unit, if ever needed, is money beside a quantity unit, not a
  UCUM code.
- **Forecasts stay out of the index.** A forecast's result time precedes its
  phenomenon time; Home Assistant's `measurement` class excludes "a prediction
  of the future" and every aggregate assumes observed values. Forecasts live
  in their provider's mirror. An optional `issuedAt` on an observation records
  when a result became known for late lab results and vendor recomputations.
- **Quality**: an optional `quality` code on a value (`valid`, `estimated`,
  `questionable`, `stale`, `validated`); an estimated utility interval later
  replaced by a validated one is an ordinary revision.
- **Multi-channel traces** sharing one time axis (a vehicle's 25 channels, a
  weather station's row, FIT records) are an efficiency, not a semantic need;
  deferred.

## 6. Units: the engine's table

A core facility like the currency table (decision 0140): a stored value is
read through the table, so a code is added and never removed or redefined; the
console holds a copy equal by test; the Python module for functions is
generated from the same source. The units report
([02-units-and-catalogues.md](observations/02-units-and-catalogues.md))
parsed UCUM 2.2's `ucum-essence.xml`, ran the NLM reference library against
every proposed code, and computed every factor as an exact fraction from the
defining constants.

**Named quantity kinds, each with a dimension vector.** A dimension vector
alone cannot keep apart what users need kept apart: energy and torque, frequency
and radioactivity, absorbed dose and dose equivalent, a fraction and a count,
body mass index and areal density, real power and apparent power, a
temperature and a temperature difference. The reference library converted
`N.m` to `J`, `Gy` to `Sv`, `Hz` to `Bq`, `%` to `{beats}` and `{beats}/min`
to `{steps}/min`, each a correct UCUM answer and a wrong answer for a person.
Every store that ships to end users names the quantity (Home Assistant's
converter classes, Health Connect's unit classes, Haystack, QUDT, CF
conventions); the ones that use only vectors are calculators. So a kind has a
name, a vector (the SI seven plus plane angle and information, with amount of
substance its own component so `mmol/L` never converts to a cell count), a base
unit, a scale (`ratio`, `affine`, `difference`, `logarithmic`, `arbitrary`,
`count`), its units, and a default display unit. Conversion and comparison are
allowed only within one kind, and never across incompatible metrics within a
kind (section 5.2 of version 1: HbA1c, serum and whole-blood glucose, real and
reactive power).

**Day one: 198 codes in 54 kinds** (201 rows, because `K`, `Cel` and `[degF]`
appear under both `temperature` and `temperature-difference`). Health, home,
energy, weather, vehicle, devices and everyday use: mass, length, area, volume,
duration, the two temperatures, energy, power, pressure, speed, pace,
acceleration, frequency, rate, count, fraction, amount, amount and mass
concentration, catalytic activity, arbitrary, illuminance, luminous flux,
irradiance, level, sound pressure level, power level, acidity, potential,
current, resistance, charge, conductivity, information, data rate, angle,
volume flow, mass flow, force, torque, activity concentration, dose rate, body
mass index, oxygen uptake, specific power and energy, energy per distance, fuel
consumption and economy, precipitation rate. Twenty-nine factors have no finite
decimal (every per-minute rate, `km/h`, `[psi]`, `[degF]`, mile pace), one is
irrational (degree to radian), and nine rows have no factor at all (the
arbitrary and logarithmic units and pH). The table in the report is the
proposal; `coretable.py` is its source.

Rules the decision record states, each checked against UCUM 2.2:

- The engine accepts the case-sensitive UCUM grammar over the table's atoms
  and prefixes (products, quotients, integer powers), so compound units like
  `kW.h/(100.km)` need no row of their own; the kind of a compound is checked
  by its vector and must be one the table names.
- Annotations (`{steps}`, `{beats}/min`) are part of a unit's identity here,
  although UCUM discards them: `{beats}/min` and `{steps}/min` never convert
  into each other, an annotated unit converts to the same annotation on
  another time base, and a lone annotation is a count. This is the one rule
  UCUM leaves to the application.
- An alias table on input maps `°C`, `µg`, `mmHg`, `bpm`, `rpm`, `kWh`, `Wh`,
  `Cal`, `ppm`, `mcg` and `m³` to their codes, including both micro signs
  (U+00B5 and U+03BC) and the compatibility characters for degrees; the stored
  value holds the code only; display symbols come from UCUM's print symbols.
- `Cel` and `[degF]` are affine in `temperature` and linear in
  `temperature-difference` (0.3 Cel of deviation is 0.54 `[degF]`); a metric
  declares which; imported data whose source did not say gets CF's third
  state, `unknown`, and converts to nothing.
- Arbitrary units convert to nothing, with one labelled extension: the same
  arbitrary atom with the same annotation may be rescaled by a metric prefix
  (`m[IU]/L` to `[IU]/L`), because labs send the same analyte both ways; a
  test proves `[IU]` never reaches `[arb'U]` or any proper unit.
- `U/L` and `kat` are proper units of catalytic activity, not arbitrary
  (version 1 had `kU/L` wrong).
- `dBm` has no UCUM spelling and every radio reports it: a local code with a
  documented definition. Reactive power (`var`) is `V.A{reactive}` in its own
  kind. `W.h` and `kW.h` are the codes; `Wh` and `kWh` are aliases.
- Substrate runs UCUM's 529 validation cases and 30 linear conversions
  against its parser, and writes its own cases for every special, arbitrary
  and annotation rule, because the official suite tests none of those.
- Growth is by release only. No user-declared units in v1: a private count is
  an annotation (`{cups}`); a private conversion (a cup of rice is 185 g) is a
  derivation a function writes, stamped with its formula. A `propertytype`
  may narrow a quantity's kind (`airtemperature: {base: quantity, dimension:
  temperature}`) and cannot define a unit.

The declaration key: version 1 wrote `dimension: mass`. Because the word
`kind` is taken by record kinds, the key stays `dimension` with the documented
meaning "a named quantity kind, not a bare vector"; `measures:` is the
alternative if the owner prefers to avoid UCUM's meaning of the word (open
question 1).

## 7. The base sample and the domain samples

### 7.1 Why a base sample

The four options and why the fifth wins, from the design assessment's
section 3:

| Option | Why not |
| --- | --- |
| seed the base kinds in `substrate.reamde.dev/observations` | fails decision 0077's own test (the engine addresses a trait and a reference path, never a kind); a seeded package refuses user writes, so nobody could add a property to `observation`; a bad declaration there stops every repository from opening |
| the four kinds in core | core is "what exists because the substrate exists"; 0077 moved even the agent runtime's data out of it; 0015 keeps unproven kinds out of the stable set |
| one big `observations` sample with every domain inside | one version line: a `weather` fix offered as an upgrade discards the owner's edits to `health` |
| a copy of the base kinds per domain | two `metric` kinds mean two `air_temperature` records and no rule that they are the same; the copies drift within a release |
| **a small base sample, `observations`, that each domain sample requires** | the `people` precedent decisions 0070 and 0100 were written for: the base stays small and stable, floors rarely bind, a domain re-import discards only that domain's edits |

The machinery already supports it: a sample may require another with a
`requiresAtLeast` floor; the import rehomes cross-package pins; a sample may
ship data records of another package's kind and they are rehomed too (the `llm`
sample ships `llm/provider` rows); removing a sample tombstones only its own
kinds' rows. Two consequences to document: a domain's shipped `metric` rows
survive the domain's removal, and a user who adds a property to a base kind
loses it at the next base upgrade.

### 7.2 The base kinds

`samples.substrate.reamde.dev/observations`, four kinds. Full declarations are
in the design assessment's section 8; the properties, with the health-only
ones removed and the neutral ones added:

| Kind | Properties |
| --- | --- |
| `metric` | `name`, `description`, `codes` (repeated `{system, code, version, display}`: LOINC, MDC, HealthKit, Health Connect, a Home Assistant device class, an OBD PID, CF standard name, or the package's own), `dimension` (a quantity kind), `unit` (canonical), `delta`, `circular`, `codeSet` (the codes a categorical metric admits), `form` (default), `hold` (default), `reducers` (recommended), `compatibility`, `wrapAt`, `displayPrecision`, `group` (navigation only), `algorithm` `{vendor, name, version}` |
| `observation` | `metric`, `source`, `about`, `form`, `value` (quantity), `code`, `at`, `endsAt`, `utcOffset`, `statistic`, `period`, `periodId`, `denominator`, `hold`, `resetRule`, `resetAt`, `qualifiers` (repeated `{system, code}`), `components` (repeated `{metric, value}`), `sourceValue`, `sourceUnit`, `normalizationFailure`, `quality`, `issuedAt`, `recordingMethod`, `origin`, `derivedFrom`, `conversion`, `revision` |
| `trace` | `metric`, `source`, `about`, `form`, `unit`, `at`, `endsAt`, `utcOffsetStart`, `utcOffsetEnd`, `every` or `offsets`, `values` or `codes`, `hold`, `resetRule`, `resetAt`, `channel`, `acquisition`, `quality`, `origins`, `revision`, `stats` (managed) |
| `source` | `name`, `class` (watch, ring, scale, thermometer, meter, contact, camera, app, service, lab, manual), `manufacturer`, `hardwareModel`, `firmware`, `resolution` (a quantity), `about` (the thing the device is fixed to; writers copy it), `account` (ref pinned to the `accountconfig` trait), `priorities` (keyed int by metric) |

The metric is a catalogue entry and a guide for writers and readers. The
engine reads nothing from it: form, unit, code, hold and reset rule are on the
value, so an edit to a metric cannot relabel an indexed number. The base ships
only the physical metrics several domains share (`air_temperature`,
`relative_humidity`, `energy`, `power`, `illuminance`, `atmospheric_pressure`);
a domain ships its own with a package-prefixed id (`health/heart_rate`,
`home/contact_state`) so two domains never write the same row.

There is no persistent stream record. The stream is the index key (`metric`,
`source`, `about`), and `observed_latest` lists every stream with its last
value, which is what a persistent record would mostly be read for. Its
remaining uses (per-stream defaults, a display name, a tariff partition) are
open question 6.

### 7.3 The domain samples

| Sample | Requires | Adds |
| --- | --- | --- |
| `health` | `observations` | `analyte` (the metric's lab-only facts: LOINC parts, molar mass, conversions, fallback range), `sleep`, `workout`, `lap`, `set`, `plan`, `step`, `gear`, `exercise`, `labreport`, `labresult`, `tag`; the wearable and lab catalogues; `qualifiers` codes for meal relation, body position, specimen |
| `home` | `observations` | `area` (a room or zone, with a floor), `fixture` (a door, a window, a meter, a boiler, with an area), `appliance`; the home catalogue (about 150 metrics across climate, air quality, electricity, water and gas, appliances); `qualifiers` codes for tariff and direction |
| `weather` | `observations`, `places` (does not exist yet) | `station`; the outdoor catalogue (about 25 metrics); forecasts stay in mirrors |
| `vehicle` | `observations` | `vehicle`, `trip`, `charge`; the vehicle catalogue (about 24 metrics) |
| `healthwhoop`, `homeassistant`, ... | a domain sample and a provider | one adapter function on a record trigger, with reads on the provider's kinds and writes on `<home>/observations/observation` and `trace` exactly (a function grant cannot name a trait) |

A domain never edits a base kind: health-only context is a `qualifiers` code,
health-only definition data is `health/analyte`. Retiring a base kind or
property name follows decision 0055; because user copies cannot receive a
move, the four base kind names are frozen before the first import.

## 8. The index, the rollups and the aggregate read

### 8.1 Tables

Three derived tables, one migration, all under the same row-level security and
grants as `records` and `refs`, all pure functions of the fold:

| table | row | notes |
| --- | --- | --- |
| `observed_points` | `(repository, kind, record_id, metric, source, about, form, ts, ts_end, utc_offset_min, unit, value float8, value_base float8, code, statistic, bounded, quality)` | one row per value; written in `foldRecordOp` and in the tombstone, purge, restore, merge and declaration-removal paths; cleared and replayed by a rebuild |
| `observed_rollups` | `(repository, kind, metric, source, about, form, code, utc_offset_min, bucket15, n, sum, min, max, mean, m2, first_ts, first_value, last_ts, last_value, covered_ms, held_ms, twa_sum, last_hold_ms, increase, resets, sin_sum, cos_sum)` | gauges and amounts only, never summaries; every bucket the old and the new rows touched is recomputed once per transaction |
| `observed_latest` | `(repository, kind, metric, source, about, ts, value, code, record_id)` | the last point per stream; updated when a newer point lands; recomputed from `observed_points` when the record holding the latest point is tombstoned or purged |

Why `about` and `code` are in the rollup key: rollups can serve a request only
when every predicate is constant over their grouping keys (version 1's review
P1.2). A filter to one room, a group by room, and "hours the door was open"
all read rollups because of it. The cardinality is the number of streams, which
for a fixed sensor is one per (metric, source) anyway.

Rollup volume for a home: about 150 streams times 96 quarter hours is 14,400
rows a day, 5.3 million a year (estimate). A daily rollup level is the same
derivation at a coarser key and can come when multi-year home charts need it.

### 8.2 The aggregate read, beyond version 1

Everything in version 1's section 5.5 stands. Added:

| Addition | Legal on | Served from |
| --- | --- | --- |
| `twa` | numeric gauge | `twa_sum`, `held_ms`, plus the read-time tail step |
| `timeInState` (`{"timeInState": ["open"]}` or all codes) | coded gauge | `held_ms` per code, plus the tail; the result is a duration per code |
| `increase`, `rate` | counter | `increase`, `resets`, plus the read-time step between rows; `rate` divides by covered duration and names its unit |
| `circularMean` | gauge with dimension `angle` and `circular: true` | `sin_sum`, `cos_sum` |
| `groupBy` | `source`, `about`, `code`, `metric`, `kind` | all in the rollup key, so grouping never forces a raw scan; cardinality counts against `maxBuckets` as groups times buckets |
| `maxGap` | gauges | the cap on `hold` when a value has no successor |
| the latest read | `groupBy` on the stream keys with the one reducer `last`, no `bucket`, no `range`, `metric` optional | `observed_latest`, one row per stream, under the series budget; not a fourth mode and not a list filter, because "newest per stream" is a group-wise maximum |
| `about` and `code` as filter arms on the samples path | | so an app asks "the living room" or "when the door was open" without a join |

Grouping by a property of the `about` record (every room on the first floor)
is a join: resolve the small set of matching `about` records first, then group
by `about`. It is left out of v1; a function does it from rows grouped by
`about`.

### 8.3 Four requests

Average temperature per room per day, one month:

```json
{"filter": {"kinds": ["ada.example.com/observations/trace"]},
 "aggregate": {
   "metric": {"ref": "ada.example.com/observations/metric/air_temperature"},
   "range": {"gte": "2026-09-01T00:00:00+03:00", "lt": "2026-10-01T00:00:00+03:00"},
   "bucket": {"calendar": "day", "timezone": "Europe/Athens"},
   "groupBy": ["about"],
   "reducers": ["twa", "min", "max"],
   "maxGap": "PT3H",
   "unit": "Cel"}}
```

kWh per day from a meter that resets:

```json
{"filter": {"properties": {"about": {"eq": "ada.example.com/home/fixture/main_meter"}}},
 "aggregate": {
   "metric": {"ref": "ada.example.com/observations/metric/energy_imported"},
   "range": {"gte": "2026-09-01T00:00:00+03:00", "lt": "2026-10-01T00:00:00+03:00"},
   "bucket": {"calendar": "day", "timezone": "Europe/Athens"},
   "reducers": ["increase"],
   "unit": "kW.h"}}
```

Hours the front door was open per week:

```json
{"filter": {"properties": {"about": {"eq": "ada.example.com/home/fixture/front_door"}}},
 "aggregate": {
   "metric": {"ref": "ada.example.com/observations/metric/home/contact_state"},
   "range": {"gte": "2026-07-06T00:00:00+03:00", "lt": "2026-10-05T00:00:00+03:00"},
   "bucket": {"calendar": "week", "weekStart": "monday", "timezone": "Europe/Athens"},
   "reducers": [{"timeInState": ["open"]}],
   "maxGap": "P1D",
   "unit": "h"}}
```

The current value of every stream in the house:

```json
{"filter": {"implements": "substrate.reamde.dev/core/observed"},
 "aggregate": {"groupBy": ["metric", "source", "about"], "reducers": ["last"]}}
```

And the three records behind them (a living-room temperature trace from an
on-change sensor, a resetting energy meter trace, a door observation) are
worked in the design assessment's section 8.5.

## 9. Domains, in order

### 9.1 Health first, unchanged

Version 1's phases 1 to 4 stand, with the renames of section 2 applied before
anything is built. The first delivery proves the contract on one provider and
one consumer: a monthly sleep view from thirty WHOOP rows, identical re-sync
changing nothing, a rescored or deleted WHOOP record converging, two cycles on
one date staying distinct, rebuild and export reproducing the data.

### 9.2 Home second, minimal

After health phase 2 (the index and the aggregate read exist): a `home` sample
with `area`, `fixture` and the home catalogue, and a `homeassistant` provider
plus adapter, scoped to one acceptance list:

1. average temperature per room per day, from on-change sensors, by `twa`;
2. kWh per day from a meter that reset once in the month, by `increase`;
3. hours a door was open per week, by `timeInState`;
4. the current value of every stream in the house, from `observed_latest`.

Each exercises one seam health never touches. The ingest question (open
question 5): Home Assistant's raw state history (exact, costly in lines) or its
five-minute and hourly statistics (already time-weighted, cheap, but arriving
as `summary` values with `mean`, `min`, `max`, `state` and `sum`), or both with
the statistics as the backfill and the states as the live feed.

### 9.3 The volume stance

A home is a write-heavy source. Hourly traces for 150 streams are 3,600
changelog lines a day, 1.3 million a year (estimate), against 2.17 million
entries in the owner's whole repository today. So the plan states:

- the trace period is chosen per stream by rate (version 1's open question 8
  becomes a rule): hourly for a power sensor every few seconds, daily for a
  thermometer every five minutes; a daily trace is written when the day closes
  and is a day stale by design;
- low-rate categorical streams (doors, presence, modes) are one `observation`
  per change, so their current state is fresh;
- substrate is the archive and the analytics store for the home; Home
  Assistant stays the live state bus, and the adapter sends bounded batches;
- retention tiers are a per-repository policy: raw points for N days, rollups
  forever, with the response saying "not materialised" rather than "no
  points" for a range whose raw rows are gone (version 1's section 5.4);
- infrastructure metrics are out of scope, and a provider for them is refused
  on volume and label cardinality.

### 9.4 Then, each with a consumer

`weather` (needs `places`), `vehicle` (needs `trip` and `charge` sessions and
the position work of section 5.5), a second person inside `health` (`about` a
`person`), mood and habits (coded and ordinal values are already in the base),
meters read by hand. Spending, media plays and parcels stay typed kinds and
use the aggregate read's records path. Finance balances wait for a decision on
money as an observed value (open question 7).

## 10. Decisions this plan would record

Version 1's eight decisions stand, reworded where section 2 renamed things,
and these are added:

1. Every observed value names what it is about through an `about` reference
   pinned to any kind; absent means the owner; the thing observed is a typed
   kind in its domain, never a generic subject kind.
2. The `observed` trait's contract is complete in its first version, because
   a core trait cannot gain a property after it ships.
3. The forms are `gauge`, `amount`, `counter` and `summary`; a categorical
   value is a coded gauge or amount, never a fifth form and never a number.
4. A counter is stored and indexed as sent and differenced at read under its
   declared reset rule; the fold never reads another record to project one.
5. A gauge carries a hold; averages over on-change data are time-weighted.
6. The unit table is a closed set of named quantity kinds with exact rational
   factors, UCUM case-sensitive codes over a bounded grammar, annotations as
   part of identity, an alias table on input, release-only growth, and
   conversion only within one kind.
7. The base kinds live in the `observations` sample, which every domain
   sample requires; the four kind names are frozen before the first import.
8. Positions are not quantities; a `geopoint` value type and a `track` are
   the only way positions enter the index.
9. Forecasts never enter the index; money is never a unit.

## 11. Phasing

| Phase | Builds | Engine change | Proves |
| --- | --- | --- | --- |
| 1 | `quantity` with the kind-based unit table; the `observed` trait; the `observations` base sample with the four kinds; the `health` sample reduced to `analyte`, `sleep` and a small catalogue; `healthwhoop`; the shared Python module; version 1's privacy statement | medium | version 1's phase 1 list |
| 2 | `trace`; `observed_points`, `observed_rollups`, `observed_latest` with lifecycle hooks and RLS; the aggregate mode with plain buckets, typed reducers, `groupBy`, the latest read, coverage, budgets and `from`; typed expression indexes | medium to high | a bounded raw aggregate equals its rollup across a partial boundary, a correction, a deletion and a rebuild; measured write cost on the real tables |
| 3 | `home` with `area`, `fixture`, the home catalogue and the Home Assistant provider and adapter; `twa`, `timeInState`, `increase`, `rate`, `circularMean`; `maxGap`; interval amounts and source policy; session windows; rolling and fill; `downsample` | medium | the four home requests of section 9.2 |
| 4 | the rest of health (labs with the comparator truth table, workouts with laps, sets, plans, gear, exercises; `host.blobs.get` and `put`; the durable import job) | medium | version 1's phase 3 |
| 5 | `geopoint` and `track`; `weather` with `places`; `vehicle`; a second person in health; retention tiers; TimescaleDB as an optional backend; percentile sketches | small each | a track answers "where was I" and a vehicle trip reads as one session |

## 12. Open questions for the owner

1. The declaration key for a quantity's kind: keep `dimension:` with the
   documented meaning "a named quantity kind", or rename to `measures:`?
2. `about` (recommended; avoids the mapping's `subject: true`) or `subject`
   (the standard word in FHIR and SOSA)?
3. The form names: `gauge` and `amount` (recommended) or keep `instant` and
   `interval`?
4. Which second domain: `home` through Home Assistant (recommended; four
   seams) or `energy` alone (one seam)?
5. The home adapter's ingest: Home Assistant's raw state history, its
   statistics, or statistics as backfill and states as the live feed?
6. A persistent stream record (`stream` or `datastream`; `series` and
   `channel` are taken) for per-stream defaults and display names, or none,
   with `observed_latest` as the stream list (recommended for v1)?
7. Money as an observed value (a balance `about` an account) or balances as a
   typed kind read through the records path (recommended)?
8. The arbitrary-unit prefix extension: keep it, labelled, or refuse as UCUM
   does?
9. `dBm`: a local code (recommended), or `dB[W]` with a stated offset?
10. Retention tiers: a per-repository setting in phase 2, or deferred until a
    home has a year of data?

## 13. The review of 2026-10-10 and what it changed

Codex (gpt-6-astra) reviewed sections 1 to 12 on 2026-10-10 without editing
them; the full text is `review/codex-review.md`.
All fourteen findings change this plan in whole or in part, one sub-rule is
declined, and one point stays an open question. Where a row below contradicts
an earlier section, the row wins. The current end-to-end proposal, with the
verdicts folded in and worked examples for WHOOP, Garmin, a scale, a lab and a
home, is the shared document
[Observations in substrate: the end-to-end proposal](https://claude.ai/code/artifact/1e4ddd78-c7ac-4423-b3a7-782f322acec8).

| Finding | Verdict | What changes |
| --- | --- | --- |
| 1. The engine claims to read nothing from `metric`, yet `wrapAt`, `circular` and the temperature-difference flag live there | adopt | `delta`, `circular` and `wrapAt` move onto the value and the trace, beside `form`, `unit`, `hold` and the reset rule; the metric keeps names, codes, display and recommended reducers only |
| 2. A rollup cannot answer every `maxGap`, threshold or priority request | adopt | a rollup serves a request only when its stored statistics answer it exactly; otherwise the read scans raw points within budget, or says `unavailable` for a range whose points are expired; the rollup row gains `max_gap_ms` so the rule is decidable per bucket |
| 3. Filtering `closed` out before building intervals overcounts door-open time | adopt | the evaluation order becomes: select streams, build held intervals from every transition, then filter intervals by code or threshold; an executable specification precedes the SQL |
| 4. Copying the previous period's last value into the next trace fakes a measurement | adopt | section 5.4's carry-in rule is withdrawn; the read fetches the point current at the window start with one indexed lookup per stream; a hold expires at `maxGap` inside a gap even when a later point arrives |
| 5. One `resetRule` cannot describe signed totals, several resets, or wraps | adopt, one part declined | three counter contracts replace section 5.3's list: `monotonic` (a drop is a reset), `signed` (the value may fall; `increase` is the net change inside one `resetAt` epoch) and `wrap` (`wrapAt` on the value); a trace splits at every reset epoch; Home Assistant's 10 percent drop tolerance is declined as an engine rule, a noisy total declares `signed` or is smoothed in the adapter |
| 6. Unavailable, not measured and invalid are different and none of them is zero | adopt | `absentReason` on an observation (FHIR's list); in a trace a `null` value means unavailable from here, ends the hold and counts in nothing; `quality` gains `unavailable` |
| 7. (metric, source, about) merges two channels of one device | adopt in part | the stream key gains `channel`; no stream record yet (open question 6 stands) |
| 8. Choosing between sources is not the same as knowing two rows are one measurement | adopt | a row carries `origin` (the mirror row that delivered it) and the source names the producing device; deduplication only on an exact upstream identity; `prefer-raw` takes `minCoverage` (default 0.9) below which the summary wins |
| 9. Expiring the index and deleting raw data are different operations | adopt | phase 2 keeps every raw record; expiring `observed_points` rows is an index policy and the response says `unavailable` for those ranges; permanent reduction waits until the supported historical queries are listed |
| 10. `V.A` sits under `power`; compounds get their kind from the vector again | adopt | `V.A` gets its own `apparent-power` kind in `coretable.py`; a compound unit whose vector matches more than one kind is refused unless the property type or the metric declares the kind; `measures:` versus `dimension:` stays open question 1 |
| 11. Counters differenced in `float8` lose increments | adopt | counter endpoints and `increase` stay exact (`numeric`); `float8` only for gauge and amount analytics; mean pace is total duration over total distance; `circularMean` is undefined below a resultant-length threshold |
| 12. A trait that must be complete on day one cannot be | adopt the framing | the first contract stays complete, and a later need lands as a second trait a kind binds beside `observed`, never as a property added to it; a sample kind move is expensive, not impossible |
| 13. Multi-origin traces and `observed_latest` corrections are underspecified | adopt | one trace per (metric, source, about, channel, period) has one origin provider, so two providers delivering one device are two sources; `observed_latest` is recomputed for the old and the new stream when a correction changes a key or moves the newest point, ties break by revision then record id; the adapter version is stamped separately from the upstream revision |
| 14. Benchmark the whole write and rebuild path first | adopt as the phase 2 gate | traces, revisions, points, rollups, WAL, corrections and rebuild are measured together before the rollup layout is frozen; plain Postgres first |

The three contradictions the review found are resolved the same way: coded
`amount` values are allowed (a sleep stage is one); counters are in
`observed_rollups` with `increase` and `resets`; `observed_latest` gains
`form`, `unit`, `hold` and `utc_offset_min` so a latest value reads without
opening its record.

Four questions join section 12: the word for a vendor-method summary
(`statistic: vendor`, recommended, or `score`); the `prefer-raw` coverage
floor (0.9 recommended); how the trait grows (a second trait, recommended, or
versioned variants); and whether a `null` inside a trace ends the hold
(recommended) or bridges a gap.
