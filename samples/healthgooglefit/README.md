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
| `com.google.weight` | `merge_weight` | `health/body_mass` | gauge, hold `P1D` | `kg` | one observation per reading |
| `com.google.height` | `merge_height` | `health/height` | gauge | `m` | one observation per reading |
| `com.google.body.fat.percentage` | `merged` | `health/body_fat_fraction` | gauge, hold `P1D` | `%` | one observation per reading |
| `com.google.oxygen_saturation` | `merged` | `health/spo2` | gauge, hold `PT20M` | `%` | one observation per reading |
| `com.google.blood_glucose` | `merged` | `health/blood_glucose` | gauge, hold `PT15M` | `mmol/L` | one observation per reading |
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
`timeAsleep`, the `awake`, `light`, `deep` and `rem` totals, and
`efficiency`, time asleep over time in bed). Fit sends no nap flag: a sleep
session shorter than three hours that begins between 09:00 and 21:00 on the
account's clock is `isNap: true`, any other `false`. Any other session
becomes a `health/workout` with `sportSource` set to Fit's activity code,
`sport` the code mapped onto the workout kind's code set (1, 14 to 19
cycling, 7 walking, 8 running, 80 strength, 100 yoga and the rest the
function's `SPORTS` table names; any other code `other`) and `duration` from
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
`asleep` (sleep with no stage), 4 `light`, 5 `deep` and 6 `rem`. Code 3 (out
of bed) and 0 (unspecified) are no stage of the set and are skipped and
counted; out-of-bed time is left out of `timeInBed`.

## Ids, sources and offsets

- A trace is `googlefit/<account id>/<device slug>/<metric>/<UTC hour>`, a
  reading `googlefit/<dataset row id>/<source slug>/<metric>/<start in epoch
  ns>`, a stage `googlefit/<session row id>/stage/<start in epoch ns>`
  (so a corrected earlier segment renames no later one), and a sleep or
  workout `googlefit/<session row id>`. A reading's id names its source and
  its full `startTimeNanos`, so two sources reading one instant, or one
  source reading twice inside a millisecond, are separate observations, and
  correcting or removing one leaves the other alone. Each blood pressure
  reading's two observations take the same shape with the systolic and
  diastolic metric names. An id that leaves the record-id alphabet or passes
  128 bytes is hashed through `host.ids.external`.
- A source is the device behind a point's `originDataSourceId`, looked up in
  the account's `datasource` rows (by id, else by a listed device's `uid`
  inside the id): `googlefit/<account id>/<manufacturer-model>.<device>`,
  named for its manufacturer and model, with `class` `watch` or `scale`
  where the device type maps. `<device>` is the device's `uid` when it is 1
  to 32 lowercase letters and digits, else the first 16 hex digits of the
  uid's SHA-256, else, for a device listed with no uid, of the full
  `originDataSourceId`'s. Two devices of one model are therefore two sources,
  and their readings at one instant two observations, `withings-body.4a91be20`
  beside `withings-body.4a91be21`. A hand entry is the `manual` source, and
  its readings carry `recordingMethod: manual`; a session's source is its
  app (`com-strava`). A source is minted
  `ifAbsent`, so an owner's rename or `priorities` stands.
- Fit stores no UTC offset. Each value's offset comes from the account's
  `timezone`, read at write time, and an observation records it in a
  qualifier, `{system: utcOffsetRule, code: timezone:Europe/Athens}`. An
  account that names no zone gives no offset, so none is written on any
  output (the observed index marks such a value offsetless) rather than a
  guessed UTC. `quality` is left alone, because `estimated` would describe
  the values, not their offset.
- The zone is the account's CURRENT one, which costs two things. A day spent
  in another zone is stamped with the account's: a London night reads as an
  Athens one, filed on the Athens date by a recorded-local read. And a
  replay from 0, which the upgrade note below asks for, rewrites every past
  value with the zone in force at the replay, so after the owner moves and
  updates the zone every past Fit value moves onto the new zone's days.

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
keeps the stored one (`host.observed.reconcile` holds every adapter to that,
and leaves an output that already holds what the row implies unwritten). That is per output rather than per day row, so a late
point rewrites only the hour or the reading it lands in.

Every output also carries `adapter: healthgooglefit@<version>`, the
`ADAPTER` constant at the top of the function, apart from `revision`. A
change to what a mirror row becomes raises the constant and the package's
`version` together, so `<version>` is the package version and a repository
that imported the package is offered the upgrade. After an upgrade, replay
the trigger from the start
(`bin/substratectl trigger replay healthgooglefit-on-mirror --from 0`):
every mirror row is delivered again, and each output is rewritten under the
new version at the revision it already holds. Days Fit never re-serves are
not reprocessed any other way.
