# The Apple Health bundle

Package `providers.substrate.reamde.dev/applehealth`: a receiver for the
Apple Health data the [Health Auto Export](https://help.healthyapps.dev/en/health-auto-export/automations/rest-api/)
iOS app POSTs from the phone. Apple Health has no cloud API, so nothing here
calls out and there is no OAuth: the app's REST API automation sends JSON to
the `applehealth-webhook` trigger, and `applehealthingest` writes it as
mirror rows in the app's own field names and units.

`bundle.yaml` is the closure (the account and export kinds, eight mirror
kinds and the function) and `triggers.yaml` is the delivery wiring. They are
the contract; this file is not.

## Setting it up

1. Install it: `substratectl install providers.substrate.reamde.dev/applehealth`,
   or Add on the console's Registry page.
2. Create one account per phone, with an id of your choosing:

   ```yaml
   kind: providers.substrate.reamde.dev/applehealth/account
   metadata:
     id: iphone
   data:
     properties:
       displayName: Ada's iPhone
       timezone: Europe/Athens
   ```

   Every `enabled*` toggle is on unless you turn it off.
3. Arm the trigger. It ships disabled and without a key, because an enabled
   trigger with no key writes health records for anyone who reaches the
   server. `substratectl get substrate.reamde.dev/core/trigger applehealth-webhook -o yaml`,
   set `enabled: true` and `source.webhook.key` (16 to 128 characters of
   `[A-Za-z0-9_-]`), and `substratectl apply -f` it back.
   `substratectl trigger status` prints the endpoint,
   `/webhooks/<authority>/applehealth-webhook`.
4. In the app, add a REST API automation:
   - URL: `https://<your-substrate-host>/webhooks/<authority>/applehealth-webhook`
   - Headers: `Authorization: Bearer <the key>` and
     `X-Substrate-Account: iphone` (the account's id; with one account the
     header may be left out)
   - Export Format JSON, Export Version 2
   - Date Range: Since Last Sync
   - Summarize Data on, Time Grouping Minutes or coarser
   - Batch Requests on

## Limits

- **8 MiB per request.** The trigger declares `maxBody: 8388608`, raising
  the door's 1 MiB default for this endpoint alone. The door answers `413`
  above it, and the app does not send that part again. A day of per-minute metrics is well under it; a
  day of per-second data, a long GPS workout at second grouping or several
  ECGs can pass it, which is why Time Grouping should be Minutes or coarser.
- **6 MiB per record.** A function answers in one frame of at most 8 MiB, so
  one record bigger than 6 MiB as written (a workout route of more than
  about 30,000 points) is skipped, and the export receipt's `oversized` names
  it. The rest of the delivery is written.
- **No full-history export by push.** A first export of years of data is
  hundreds of megabytes. Push only what arrives from now on; importing the
  history from a file is a later piece of work.
- **Since Last Sync misses back-dated samples**, such as a resting heart rate
  the watch writes hours after its timestamp, or history another app
  backfills. A manual export of Previous 7 Days from the same automation
  catches them up: overlapping deliveries rewrite the same records rather
  than adding new ones.

## When a delivery fails

The app logs a failed request in its Activity Logs and does not send that
part again in the same run: a part answered with a status other than 2xx, or
not answered within 60 seconds, is skipped. Retry behaviour beyond that is
not documented. The door answers `202` as soon as the request is recorded, so
the app sees a failure only for a wrong key or a disabled trigger (`404`), a
body over 8 MiB (`413`) or more than eight deliveries in flight (`503`).

A delivery the function refuses after the `202` parks on the trigger with
its body kept: no account record, an `X-Substrate-Account` naming none, two
accounts and no header, a CSV body, a body that is not the app's JSON, a
version 1 workout. `substratectl trigger parked applehealth-webhook` lists
them with the reason; fix the cause and `substratectl trigger retry
applehealth-webhook <parked-id>` writes it.

A section whose toggle is off, and every delivery while the account's
`syncPaused` is true, is dropped unwritten. The app will not send it again.

## What lands where

| The app sends | Lands as | Keyed by |
| --- | --- | --- |
| one metric's rows in `data.metrics` | `metricbatch`, one per metric and period | `applehealth/<account>/<metric>/<grouping>/<period>` |
| a workout in `data.workouts` | `workout` | `applehealth/<account>/<id>` |
| an entry in `data.stateOfMind` | `stateofmind` | `applehealth/<account>/<id>` |
| a dose in `data.medications` | `medication` | `applehealth/<account>/<medication>/<scheduled time>` |
| an entry in `data.symptoms` | `symptom` | `applehealth/<account>/<name>/<start>` |
| an entry in `data.cycleTracking` | `cycleevent` | `applehealth/<account>/<name>/<start>` |
| a recording in `data.ecg` | `ecg` | `applehealth/<account>/<start>` |
| an entry in `data.heartRateNotifications` | `heartratenotification` | `applehealth/<account>/<notification>/<start>` |
| the request itself | `export`, the receipt | the webhook fire id |

Every key but the receipt's is composed by `host.observed.record_id`, the
rule every provider and adapter here follows: each part has every byte
outside the record id alphabet, a `/`, a `~` and a `-._:@` at either end
written as `~` and two hex digits (account `phone/a` is `phone~2Fa`), so two
distinct parts never compose one id, and the joined id is used as written
when it is at most 128 bytes, else replaced by `host.ids.external`'s hash of
it.

A metric batch covers one UTC hour at Seconds or Minutes grouping, one UTC
day at Hours, one UTC month at Days and one UTC year above that, so it holds
at most a few thousand rows. Its rows are keyed by their instant, never by
`source`, because a correction sends the same bucket again under another
label: a later delivery replaces the rows inside the time span it covers for
that metric and keeps the rest, and deletes a batch inside that span it
carries no row for. A metric sampled less often than the period (a weigh-in
at minute grouping) gets one batch per sample, so a weigh-in deleted in Health
leaves the mirror with the next export that spans it.

Units are the app's own Unit Preferences, kept verbatim (`count/min`, `kJ`,
`kcal/hr·kg`). Dates are kept as the app wrote them; the offset is the
phone's zone when it sent the export, not where the sample was recorded, and
`at` and `endsAt` hold the instants. `source` is Health's own name for the
device: data from a Garmin watch arrives as `Garmin Connect`, a bucket fed by
several sources joins them with `|`, and app 10.0.4 sends merged series with
an empty `source`. Workouts carry no `source`; their `heartRateData` rows do.

The full reference, with what each field means:
[docs/bundles-catalog.md#apple-health](../../../docs/bundles-catalog.md#apple-health).

## What the tests are checked against

No test fixture is a delivery captured from a real account. The eight
`delivery_*` files under `internal/providertest/testdata/applehealth` describe an
invented person (Ada, in Athens, with a Garmin scale and an Apple Watch) and
each carries a `_fixture.note` saying what it shows:

| Fixture | What it is |
| --- | --- |
| `delivery_0` to `delivery_5` | scenarios for app version 10.0.3 (a month of days, a day by minute, a night and its workouts, body measurements, a corrected redelivery, a delivery sent from London) |
| `delivery_6` | app 10.0.4's empty and missing `source`, as the app's public issue tracker reports them (Lybron/health-auto-export#78) |
| `delivery_7` | the shapes the help pages document for the other sections and two shapes the issue tracker reports (#68, #69); its note says it is not a recorded delivery |
| `app_minutes`, `app_seconds` | what Substrate Sync (`apps/apple/SubstrateSync`) sends for one day, written by its encoder's test, which holds the app to them byte for byte |

The tests therefore prove the provider and the adapter agree with this
reading of the format, not that the app sends it. A delivery captured from
a real phone, with personal data replaced, would be the first fixture that
does.

