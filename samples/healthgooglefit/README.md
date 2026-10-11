# Google Fit to health

Turns Google Fit mirror rows into observations, traces and health records
under the repository's own authority. Package
`samples.substrate.reamde.dev/healthgooglefit` requires `observations`,
`health` and the `googlefit` provider, and declares no kind of its own:
`bundle.yaml` holds the `adaptgooglefit` function and `triggers.yaml` the
`healthgooglefit-on-mirror` record trigger that runs it on every create,
update and delete of a `googlefit/dataset` or `googlefit/session` row.

Google Fit's REST API ends with 2026 and is closed to new projects, so like
the provider this was tested against recordings only.

## What each mirror row becomes

| Fit data type | Stream read | Metric | Form | Unit | Written as |
| --- | --- | --- | --- | --- | --- |
| `com.google.heart_rate.bpm` | `merge_heart_rate_bpm` | `health/heart_rate` | gauge, hold `PT4M` | `/min` | hourly trace per device |
| `com.google.step_count.delta` | `estimated_steps` | `health/steps` | amount | `{steps}` | hourly trace per device |
| `com.google.calories.expended` | `merge_calories_expended` | `health/energy_expended` | amount | `kcal` | hourly trace per device |
| `com.google.weight` | `merge_weight` | `health/body_mass` | gauge | `kg` | one observation per reading |
| `com.google.height` | `merge_height` | `health/height` | gauge | `m` | one observation per reading |
| `com.google.body.fat.percentage` | `merged` | `health/body_fat_fraction` | gauge | `%` | one observation per reading |
| `com.google.oxygen_saturation` | `merged` | `health/spo2` | gauge | `%` | one observation per reading |
| `com.google.blood_glucose` | `merged` | `health/blood_glucose` | gauge | `mmol/L` | one observation per reading |
| `com.google.blood_pressure` | `merged` | `health/systolic_blood_pressure`, `health/diastolic_blood_pressure` | gauge, hold `PT0S` | `mm[Hg]` | two observations per reading, each carrying both as `components` |
| `com.google.sleep.segment` | `merged` | `health/sleep_stage` | amount | `min` | one observation per segment inside a sleep session, `code` beside the duration |

A step or energy point is a delta over `[startTimeNanos, endTimeNanos)`, and
a trace point's amount spans to the trace's next point. So the adapter keeps
each delta's end: a null point follows it at `endTimeNanos` unless the next
delta from the same device starts exactly there, and a null in a trace means
nothing was measured from that instant. A delta that crosses a UTC hour is
written as one `amount` observation over its own span, the reading's id
below, and not split between two hourly traces: the amount stays whole and
neither hour claims time it does not cover.

A session with `activityType` 72 becomes a `health/sleep` (span, `timeInBed`,
`timeAsleep` and the `awake`, `light`, `deep` and `rem` totals); any other
session becomes a `health/workout` with `sportSource` set to Fit's activity
code, `sport` where the code is one the research verified (1 and 16 cycling,
7 walking, 8 running, 80 strength, 108 other) and `duration` from
`activeTimeMillis`.

Each data type is read from ONE derived stream. Google keeps several views
of one quantity (`estimated_steps`, the count the Fit app shows, and
`merge_step_deltas` count the same steps), so a second stream would count
everything twice. The other streams are passed over and counted: the resting
heart rate stream, `merge_step_deltas`, `com.google.calories.bmr` (a rate in
kcal per day, which no catalogue metric holds), `com.google.activity.segment`,
`com.google.active_minutes` and `com.google.heart_minutes` (no catalogue
metric). Distance is never mirrored: the provider does not request the
location scope it needs.

Fit's sleep segment codes map onto the catalogue's code set as 1 `awake`, 2
`in_bed` (sleep with no stage), 4 `light`, 5 `deep` and 6 `rem`. Code 3 (out
of bed) and 0 (unspecified) are no stage of the set and are skipped and
counted; out-of-bed time is left out of `timeInBed`.

## Ids, sources and offsets

- A trace is `googlefit/<account id>/<device slug>/<metric>/<UTC hour>`, a
  reading `googlefit/<dataset row id>/<metric>/<start in epoch ms>`, a stage
  `googlefit/<session row id>/stage/<n>`, and a sleep or workout
  `googlefit/<session row id>`. An id that leaves the record-id alphabet or
  passes 128 bytes is hashed through `host.ids.external`.
- A source is the device behind a point's `originDataSourceId`, looked up in
  the account's `datasource` rows (by id, else by a listed device's `uid`
  inside the id): `googlefit/<account id>/<manufacturer-model>`, with `class`
  `watch` or `scale` where the device type maps. A hand entry is the
  `manual` source; a session's source is its app (`com-strava`).
- Fit stores no UTC offset. Each value's offset comes from the account's
  `timezone`, read at write time, or UTC when the account names none. An
  observation records which in a qualifier, `{system: utcOffsetRule, code:
  timezone:Europe/Athens}` or `code: utc`. A trace, a sleep and a workout
  declare no property for it, so the rule is not stamped on them; it is the
  same for every row of one account. `quality` is left alone, because
  `estimated` would describe the values, not their offset.

## How a row's outputs stay right

Every output names the mirror row it came from (`origin`, or `origins` on a
trace), and each delivery rebuilds that row's whole output set: it writes
what the row implies and deletes the earlier outputs it no longer does, found
by a `referencing` read on the row. A dataset row that comes back empty is
deleted by the provider, and its delete deletes its outputs. A stage segment
belongs to its night, so a sleep day's delivery refreshes the sleep sessions
it overlaps, and a stage observation lists the day rows it was cut from in
`derivedFrom`.

`revision` is the newest `modifiedTimeMillis`, in seconds, among the points
an output was built from, and never falls: a rewrite that would lower it
keeps the stored one. That is per output rather than per day row, so a late
point rewrites only the hour or the reading it lands in.
