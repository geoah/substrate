# WHOOP to health

Turns WHOOP mirror rows into observations and health records under the
repository's own authority. WHOOP sends scored summaries, never raw samples,
so it writes no traces. Package `samples.substrate.reamde.dev/healthwhoop`
requires the `health` sample (and so `observations`) and the WHOOP provider
at version 21 or later.

`bundle.yaml` is the closure (the `adaptwhoop` function) and `triggers.yaml`
is the delivery wiring: `healthwhoop-on-mirror` fires the function on every
create, update and delete of a WHOOP cycle, recovery, sleep or workout row.
`views.yaml` holds two record views: a WHOOP cycle's page and WHOOP's scores
on a sleep's page, with WHOOP's published bands as data. They are the
contract; this file is not.

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

A recovery's five readings (`health/whoop_recovery`, `hrv_rmssd`,
`resting_heart_rate`, `spo2` and `skin_temperature`) measure the night the
recovery scores, so they span that sleep, with `period` `sleep` and the
sleep's id as `periodId`, which the night's sleep performance and
respiratory rate carry too. A day read with `bucket.place` `end` puts them
on the wake day, beside the nightly summaries of the other sources. A
recovery whose sleep row is absent (sleeps toggled off) spans its cycle
until the sleep arrives.

A cycle runs from one sleep onset to the next, so a cycle's strain, energy
and heart rate start the evening before: `place` `start` files them under
that evening's date.

The trigger declares `fromStart`, so the import creates it with its cursor at
the changelog's start: a mirror that already held rows when this sample was
imported is converted on the dispatcher's next pass, with no replay by hand.

Every output carries `adapter: healthwhoop@<version>`, the `ADAPTER`
constant at the top of the function, apart from `revision`, which is
WHOOP's `updated_at`. A change to what a mirror row becomes raises the
constant and the package's `version` together, so `<version>` is the package
version and a repository that imported the package is offered the upgrade.
After an upgrade, the same replay from 0
(`bin/substratectl trigger replay healthwhoop-on-mirror --from 0`) delivers
every mirror row again, and each output is rewritten under the new version at
the revision it already holds; mirror rows WHOOP never rescores are not
reprocessed any other way. Version 8 added the views alone and changes no
output but its stamp, so an upgrade to it needs no replay: a replay would
rewrite every output once with identical data.

The mapping table, what is skipped and how deletions reach the outputs:
[docs/bundles-catalog.md#whoop-to-health-sample](../../docs/bundles-catalog.md#whoop-to-health-sample).
