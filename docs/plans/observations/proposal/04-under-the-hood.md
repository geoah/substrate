# Under the hood

The engine pieces, for whoever builds them: the trait that makes the fold index a kind, the four forms, the quantity value and its unit table, the three derived tables and the rule that decides when a rollup may answer.

## The observed trait and the four forms

A core trait, recognised by the host the way `accountconfig` and `sync` are, with two variants that describe layout only. Every property is contracted from the first version, because a core trait cannot gain a property after it ships; health writes `about`, `code`, `hold` and the counter fields on nothing and pays nothing. A later need lands as a second trait a kind binds beside this one.

```yaml
kind: substrate.reamde.dev/core/trait
metadata:
  id: substrate.reamde.dev/core/observed
data:
  oneOf:
    - name: single      # one value per record: observation, labresult
      properties: {at: datetime, endsAt: datetime, metric: reference, source: reference,
                   about: reference, channel: string, form: string, value: quantity, code: string,
                   statistic: string, period: string, hold: duration, resetRule: string,
                   resetAt: datetime, delta: bool, circular: bool, wrapAt: decimal,
                   utcOffset: int, revision: int}
    - name: packed      # many values in one record over a fixed period: trace
      properties: {at: datetime, endsAt: datetime, metric: reference, source: reference,
                   about: reference, channel: string, form: string, unit: string,
                   every: duration, offsets: int, values: decimal, codes: string,
                   hold: duration, resetRule: string, resetAt: datetime, delta: bool,
                   circular: bool, wrapAt: decimal, utcOffsetStart: int, utcOffsetEnd: int,
                   revision: int, stats: json}
```

The host validates what a trait cannot express: exactly one of `value` and `code` (or neither with `sourceValue` set), equal lengths of `offsets` and `values`, offsets ordered and inside the period, a `form` from the four words, `statistic` and `period` on a summary, `resetRule` only on a counter, `hold` only on a gauge, a `revision` that never falls.

| Form | Means | Example | Legal reducers |
| --- | --- | --- | --- |
| `gauge` | a level at an instant, held until the next point or for `hold` | 62 /min, 72.4 kg, a door `open` | `avg`, `twa`, `min`, `max`, `first`, `last`, `count`, `stddev`, `percentile`, `durationIn`; on codes `count`, `last`, `timeInState` |
| `amount` | a quantity accrued over `[at, endsAt)` | 412 steps in a quarter hour, 1.2 kW.h this hour, 94 minutes of `rem` | `sum`, `count`, `min`, `max` |
| `counter` | a running total under one of three contracts: `monotonic` (a drop is a reset), `signed` (may fall; `increase` is the net change inside one `resetAt` epoch), `wrap` (`wrapAt` on the value) | the meter at 18342.655 kW.h, an odometer | `increase`, `rate`, `last` |
| `summary` | a statistic a vendor computed over its own period | WHOOP's recovery 67 % for cycle 1047392 | read as is; never pooled with the other forms |

A categorical value is a gauge or an amount whose `code` is set instead of `value`. A boolean is two named codes, never `0` and `1`. A `null` inside a trace means unavailable from here: it ends the hold and counts in nothing. `about` is a reference pinned to any kind; absent means the owner.

## The quantity value and the unit table

`quantity` is `{value, unit, comparator?}`, a structured value exactly as `money` is:

- `value` is exact digits as a string, like `decimal` (decision 0012). A lab `<0.5` is `{value: "0.5", unit: "ng/mL", comparator: "<"}`, admitted only where the property declares `comparator: true`.
- `unit` is a case-sensitive UCUM code from the engine's table. An alias table maps `bpm`, `°C`, `kWh`, `mmHg`, `mcg` and both micro signs on input; only the code is stored.
- Conversion and comparison happen only inside one named quantity kind, never across kinds and never across incompatible metrics within a kind (NGSP and IFCC HbA1c, serum and whole-blood glucose). `kg` to `[lb_av]` converts; `kg` to `J` is refused; `{beats}/min` to `{steps}/min` is refused although UCUM would allow it; `mg/dL` to `mmol/L` is a derived observation a function writes from the analyte's molar mass.
- Temperature is two kinds: `temperature` (affine) and `temperature-difference` (linear; a 0.3 Cel deviation is 0.54 \[degF\]). The value says which with `delta: true`.
- A filter operand converts the stored value to its own unit with exact rational arithmetic; a requested output unit may round, and the response says so.
- A compound unit whose dimension vector matches more than one kind (`N.m` is energy or torque) is refused unless the property type or the metric declares the kind.

The table ships 198 codes in 54 kinds, grown by release only, no user-declared units (a private count is an annotation such as `{cups}`). The kinds a health or home app meets first:

| Kind | Base | Codes | Note |
| --- | --- | --- | --- |
| mass | kg | `kg`, `g`, `mg`, `ug`, `[lb_av]`, `[oz_av]` | exact factors |
| rate | /s | `/min`, `/s`, `/h` | `{beats}/min` is the annotated form; annotations are part of identity |
| fraction | 1 | `%`, `1` | SpO2, body fat, humidity |
| temperature | K | `K`, `Cel`, `[degF]` | affine; the same codes appear under temperature-difference |
| mass concentration | kg/L | `mg/dL`, `g/L`, `ng/mL`, `ug/L` | labs in US units |
| amount concentration | mol/L | `mmol/L`, `umol/L`, `nmol/L` | labs in SI units |
| energy | J | `J`, `kJ`, `kcal`, `W.h`, `kW.h` | `kcal` is exact: 4184 J |
| power, apparent power | W, V.A | `W`, `kW`; `V.A` | two kinds, never converted |
| duration | s | `s`, `min`, `h`, `d`, `ms` | sleep stages, time in zone |
| count | 1 | `{steps}`, `{floors}`, `{reps}` | an annotation is a count; two annotations never convert |
| arbitrary | none | `[IU]`, `m[IU]/L`, `[arb'U]` | converts to nothing, except by metric prefix on the same atom |
| angle | deg | `deg`, `rad` | `circular: true` on a wind direction turns `avg` into `circularMean` |

The engine runs UCUM's 529 validation cases and 30 linear conversions against its parser, plus its own cases for every annotation, arbitrary and affine rule. `coretable.py` under `docs/plans/observations/` regenerates the table with exact rational arithmetic.

## The three derived tables

One SQL migration. Each table carries `repository` and the same row-level security and grants as `records` and `refs`; each is written in `foldRecordOp` and in the tombstone, purge, restore, merge and declaration-removal paths; `substratectl repository rebuild` clears them and replays the segment files; `repository verify` never reads them.

| Table | One row per | Columns | Written when |
| --- | --- | --- | --- |
| `observed_points` | value (one per observation, one per point of a trace) | `repository, kind, record_id, metric, source, about, channel, form, ts, ts_end, utc_offset_min, unit, value float8, value_exact numeric (counters), value_base float8, code, statistic, bounded, quality` | the record's rows are deleted and re-inserted on every write of that record |
| `observed_rollups` | stream and 15-minute bucket, for gauges, amounts and counters, never summaries | the stream key (`kind, metric, source, about, channel, form, code, utc_offset_min`), `bucket15`, then `n, sum, min, max, mean, m2, first_ts, first_value, last_ts, last_value, covered_ms, held_ms, twa_sum, last_hold_ms, max_gap_ms, increase numeric, resets, sin_sum, cos_sum` | every bucket the old and the new rows touch is recomputed once per transaction |
| `observed_latest` | stream | the stream key, `ts, value, code, unit, form, hold, utc_offset_min, record_id, revision` | when a newer point lands; recomputed from `observed_points` for both the old and the new stream when a correction moves or re-keys the newest point; ties break by revision, then record id |

Why 15 minutes: it divides every UTC offset in use today, so a civil day at the sample's own offset is a sum of whole rollup rows. A window that starts at 23:07 reads rollups for its interior and raw points for its two boundary fragments. Why `about`, `code` and `channel` are in the key: a rollup can serve a request only when every predicate is constant over its grouping keys, so a filter to one room, a group by room and "hours the door was open" all read rollups.

`float8` is for the analytics projection only; the record keeps the digits. Counters are differenced in `numeric` so an increment that was exact in storage stays exact. A lab quantity asked for through the records path aggregates in `numeric`.

## Which reducer reads what

A rollup answers a request only when its stored statistics answer it exactly. The `max_gap_ms` column is what makes time-weighted answers decidable per bucket.

| Reducer | Rollups | Raw points | Rule |
| --- | --- | --- | --- |
| `count`, `sum`, `min`, `max`, `first`, `last` | yes |  | exact merge across buckets |
| `avg`, `stddev` | yes |  | `mean` and `m2` merge without loss |
| `twa`, `timeInState` | yes, when the request's `maxGap` is absent or not below the bucket's `max_gap_ms` | otherwise | the held time stored in the bucket was computed under the value's own hold |
| `increase`, `rate` | yes, plus one window function for the step between rows |  | exact `numeric`; `rate` divides by covered time and names its unit |
| `circularMean` | yes |  | `sin_sum`, `cos_sum`; undefined below a resultant-length threshold |
| `percentile`, `durationIn`, any threshold |  | yes, within `maxSamples` | a sketch (t-digest) is a later addition |
| `from: summaries` |  | summary rows in `observed_points` | summaries are never rolled up |
| the latest read |  |  | `observed_latest`, one row per stream |
| anything over an expired range | rollups only |  | raw-only reducers answer `unavailable`, never an empty number |

A mean pace is total duration over total distance, never a mean of paces; a `stddev` of temperatures is a temperature difference; `sum` over a gauge or `avg` over a counter is a `422` naming the form.

## Corrections, volumes and the benchmark gate

**Corrections.** A rewrite of a trace with a higher `revision` deletes its old point rows, inserts the new ones, recomputes the touched buckets and the stream's latest row, all in the write's transaction. A stale rewrite is refused. A tombstone removes the points and recomputes the same buckets. One trace per (metric, source, about, channel, period) has exactly one origin provider, so no trace mixes origins; two providers delivering one device are two sources. The adapter's version is stamped separately from the upstream revision, so an adapter bug fix can regenerate output when the upstream has not changed. The point current at a window's start is fetched at read time; no adapter copies it into the next trace.

**Volumes, estimated.** The heavy health user makes about 45,500 samples a day: about 17 million `observed_points` rows a year, about 175 thousand trace records a year at one per stream per hour, and a few hundred thousand rollup rows. A home with 150 streams adds about 5.3 million rollup rows a year. Rollups are why a year-long chart takes tens of milliseconds instead of seconds. Point retention is a policy for the index alone: the records and the changelog keep every raw value in phase 2, the index may drop old points, and a request over a dropped range says `unavailable`.

**The benchmark gate.** The rollup row above is wide. Phase 2 measures the complete path (trace write, changelog bytes, point insert, rollup recompute, WAL, correction, rebuild time, with late data and concurrent reads) on real tables before the layout is frozen; the earlier count-only timings are exploratory. Plain Postgres first; TimescaleDB is an optional backend for the two large tables where the extension is installed.
