# WHOOP to health

Turns WHOOP mirror rows into observations and health records under the
repository's own authority. WHOOP sends scored summaries, never raw samples,
so it writes no traces. Package `samples.substrate.reamde.dev/healthwhoop`
requires the `health` sample (and so `observations`) and the WHOOP provider
at version 21 or later.

`bundle.yaml` is the closure (the `adaptwhoop` function) and `triggers.yaml`
is the delivery wiring: `healthwhoop-on-mirror` fires the function on every
create, update and delete of a WHOOP cycle, recovery, sleep or workout row.
They are the contract; this file is not.

A WHOOP sleep stage total is one `amount` observation of `health/sleep_stage`
carrying both `code` (the stage) and `value` (the total in minutes), spanning
the whole sleep: WHOOP reports totals per stage, not when each stage happened.

A scored cycle's and a scored workout's `averageHeartRate` and
`maxHeartRate` are two `summary` observations of `health/heart_rate` over the
cycle's or the workout's span, with `statistic` `avg` and `max` and `period`
`cycle` or `workout`; a workout's are on the `workout` channel, so a day's
average is not the plain mean of the cycle's and a run's. They are what puts
WHOOP beside the watches as a heart rate source: read them with `from:
summaries` (or `prefer-raw`) and the `avg` and `max` reducers, because a raw
read takes gauges and WHOOP sends no samples.

A workout's `sport` is WHOOP's sport name mapped onto the workout kind's code
set (`weightlifting` is `strength`), `other` where the name has no value of
its own (`padel`), and the name itself is `sportSource`. Its kilojoules are
`totalEnergy`: WHOOP's figure includes the resting energy.

A cycle runs from one sleep onset to the next, so a cycle's summaries and its
recovery's start the evening before the morning WHOOP scores the recovery: a
calendar-day read files that recovery under the previous date.

The trigger starts at the changelog's head, so a mirror that already held
rows when this sample was imported is converted by replaying the trigger from
the start (`POST
/api/v1/substrate.reamde.dev/core/trigger/healthwhoop-on-mirror/replay` with
`{"from": 0}`).

Every output carries `adapter: healthwhoop@<version>`, the `ADAPTER`
constant at the top of the function, apart from `revision`, which is
WHOOP's `updated_at`. A change to what a mirror row becomes raises the
constant. After an upgrade, the same replay from 0
(`bin/substratectl trigger replay healthwhoop-on-mirror --from 0`) delivers
every mirror row again, and each output is rewritten under the new version at
the revision it already holds; mirror rows WHOOP never rescores are not
reprocessed any other way.

The mapping table, what is skipped and how deletions reach the outputs:
[docs/bundles-catalog.md#whoop-to-health-sample](../../docs/bundles-catalog.md#whoop-to-health-sample).
