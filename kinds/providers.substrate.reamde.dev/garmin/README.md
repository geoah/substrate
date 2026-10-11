# The Garmin bundle

Package `providers.substrate.reamde.dev/garmin`: an OAuth provider that
mirrors what Garmin's Health API and Activity API push about one Garmin
Connect user (daily summaries, epochs, sleeps, stress, HRV, Pulse Ox,
respiration, body composition, blood pressure, activities with their samples)
and asks Garmin to backfill history. Read only.

## Not verified against Garmin

Garmin's Connect Developer Program was reported closed to new partners
through mid-2026; on 2026-10-11 its program pages read only "Stay tuned for
more updates on the program" and the application form was gone. Nothing here
has run against a live Garmin account.

Every field-level fact is secondary. Garmin's API specifications (Health API
and Activity API 1.2.x, the Start Guide) sit behind its partner portal login,
and only the OAuth 2.0 PKCE document is public. The field lists, types and
paths follow Garmin's OpenAPI description of the wellness API as republished
in the MORE-Platform repository
(`more-data-gateway/src/main/resources/openapi/GarminWellnessAPI.json`), with
the push envelope keys, the `202` and `409` backfill answers, the lifecycle
notices and the ping shape taken from developers' notes that cite the
specifications (the mealvana and open-wearables repositories). Where they
disagree:

- the token endpoint is `connectapi.garmin.com/di-oauth2-service/oauth/token`
  per the public PKCE document; one source names the host `diauth.garmin.com`;
- the backfill reach per request is read as 30 or 90 days, and one source
  says backfill works only within about a month of the first connection;
- some deliveries are reported to spell body composition fields `percentFat`,
  `percentHydration` and `bmi`, and a Body Battery event's length `duration`;
  the function reads the first three into the documented names, and both
  spellings of the last are declared.

The tests (`internal/providertest/garmin_test.go` and the `garmin` case of
`internal/providere2e`) drive both functions with deliveries built from those
shapes for a made-up user.

## Files

`bundle.yaml` is the closure: the `config` and `account` kinds, one mirror
kind per summary type, the `notice` kind, the `garminpush` and `garminsync`
functions and the trusted `oauth2:` metadata. `triggers.yaml` is the delivery
wiring: `garmin-webhook` (ships disabled), `garmin-on-connect`,
`garmin-on-request` and `garmin-scheduled`. They are the contract; this file
is not.

| Kind | What it is |
| --- | --- |
| `config` | the Garmin app: client id, sealed secret, API origin |
| `account` | one connected Garmin user: its Garmin user id, permissions, toggles and backfill cursors |
| `notice` | a disconnect or a permission change Garmin pushed |
| `daily` | one day's summary: steps, energy, heart rate samples, stress, Body Battery |
| `epoch` | one activity type in a 15-minute slice |
| `sleep` | one night: stage durations and map, SpO2 and respiration samples, scores, naps |
| `bodycomp` | one weigh-in |
| `stressdetail` | one day of stress levels and Body Battery values |
| `usermetric` | VO2 max and fitness age on one date |
| `pulseox` | overnight or on-demand SpO2 readings |
| `respiration` | 15 minutes of breathing rate |
| `healthsnapshot` | one two-minute Health Snapshot |
| `hrv` | one night's HRV |
| `bloodpressure` | one blood pressure reading |
| `skintemp` | one night's skin temperature deviation |
| `activity` | one recorded activity's summary |
| `activitydetail` | one activity's summary, samples and laps |
| `manuallyupdatedactivity` | an activity as the user edited it in Garmin Connect |
| `moveiq` | an activity the device detected on its own |

## Setting it up

1. Register an app in Garmin's developer portal and note its consumer key and
   secret. Its redirect URI is the substrate's OAuth callback.
2. Write a `config` record with `clientId` and `clientSecret`.
3. Write an `account`. Every summary type's toggle is on by default;
   `backfillDepth` defaults to `last30d`. Connect it: the first sync records
   the Garmin user id the pushes are matched on and asks for the backfill.
4. Set `source.webhook.key` on the `garmin-webhook` trigger (16 to 128
   characters of `[A-Za-z0-9_-]`), enable it, and register
   `https://<your-substrate-host>/webhooks/<authority>/garmin-webhook/<key>` for
   every summary type in Garmin's Endpoint Configuration tool. Garmin signs
   nothing, so the key is the only credential the door checks.

## Payload size

The webhook door refuses an inline body over 1 MiB with `413`
(`internal/api/webhooks.go`). One 36-minute run's activity details at one
sample a second is about 738 KB, so an activity details push longer than
about 50 minutes is refused at the door until that cap is raised (a raise to
8 MiB is planned in another change). Setting the activity details type to
ping mode in Garmin's tool avoids the cap: a ping carries only a callback URL,
and `garminpush` fetches the summaries itself. `garminpush` writes any body
the door admits, over several invocations of one chain, and skips and names
in its output a single summary over 6 MiB.

What each toggle writes, the backfill windows, and what the account reports:
[docs/bundles-catalog.md#garmin](../../../docs/bundles-catalog.md#garmin).
