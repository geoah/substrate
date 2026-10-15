# The Google Fit fixture set

Recordings of the Google Fit REST API v1, served by `runner/mockserver.py`
for the `googlefit` case, and read by the loopback fake in
`internal/providertest/googlefit_test.go`. Cut on 2026-10-11 from research
against Google's reference and discovery document (revision 20261008).

**These are not a cut of a real pull.** Google closed the Fit APIs to new
sign-ups on 2024-05-01 and says they end with 2026, so no new Google Cloud
project can call them. Every payload was written from the documented shapes
for an invented persona: Ada, with a Pixel 8, a Pixel Watch 2, a Withings
scale writing through its app, and Strava writing some runs. Nothing here came
from anybody's body.

## How a day is replayed

The sync reads every data source one UTC day at a time, and Fit names the day
in the PATH (`.../datasets/<startNs>-<endNs>`), which no exact recording name
can answer on a later run date. The mock's `PATH_RELAXATIONS` drops that
segment, so each dataset recording is named without it
(`..._merge_weight_datasets.json`) and answers every day. The recordings hold
the whole recorded period, and the sync keeps the points that started on the
day it asked for, so each day's row is right whatever the run date. The
scenario reads the days the sync asked for off the mock's request log.

The heart rate recordings keep their query (`__limit-1000` and
`__limit-1000_pageToken-1791344581754000000`): page one answers every day and
names page two, so a run that does not follow `nextPageToken` misses points.

## The recordings

| Recording (prefix `GET_fitness_v1_users_me_`) | Holds |
| --- | --- |
| `dataSources.json` | 22 sources: derived `com.google.android.gms` streams, raw watch, phone and scale streams |
| `dataSources_..._merge_heart_rate_bpm_datasets__limit-1000.json` | the latest 1,000 of 1,403 one-a-minute points of the Athens day 2026-10-07, and `nextPageToken` |
| the same `..._pageToken-1791344581754000000.json` | the earlier 403 points |
| `dataSources_..._estimated_steps_datasets.json` | 184 step deltas on 2026-10-07, spans of 47 to 120 s |
| `dataSources_..._merge_weight_datasets.json` | 30 days of weight, 17 points, two weigh-ins on 2026-10-03 |
| `dataSources_..._sleep.segment_..._merged_datasets.json` | 853 stage points over 30 nights |
| twelve more `..._datasets.json` | an empty day for every other derived source of a data type the sync reads |
| `sessions__includeDeleted-true.json` and its two `_pageToken-` pages | 30 sessions, then 29 and one `deletedSession`, then an empty page |
| `sessions.json` | the windowed read: all 59 sessions, unpaged |

`GET_oauth2_v3_userinfo.json` is the address the OAuth facility reads at the
end of the consent.

The research also recorded the weight read a second time, with the
2026-10-03 evening weigh-in changed from 72.56 kg to 71.76 kg (a later
`modifiedTimeMillis`) and the 2026-09-18 weigh-in deleted. That second read
lives in `internal/providertest/testdata/googlefit/weight_reread.json`, where
the provider test drives the re-read. The change feed
(`dataPointChanges`) and the daily aggregate (`POST dataset:aggregate`) were
researched and are not called by the sync, so they are not recorded here.

Special cases: the watch charged the night of 2026-09-16, so that night's
sleep is one phone-detected segment of type 2 with no stages; the watch was
off from 20:00 to 20:37 on 2026-10-07, a gap in heart rate; three runs come
from `com.strava` with Strava's own session ids; the deleted session is an
auto-detected walk on 2026-10-02; the London days look like any other day
because Fit carries no offset.

## What the API does, as researched

Provenance: **[verified]** the reference, the guides or the discovery
document; **[recalled]**; **[inferred]**.

**Datasets paging** [verified]: `limit` "is applied from the end of the time
range", so the first page holds the most recent points; each next page
(`pageToken` = the previous `nextPageToken`) holds points with end times
"strictly smaller" than the previous page's. Points in a page are ordered by
`endTimeNanos`. A read answers every point whose interval overlaps the
range, so a sleep segment that crosses midnight comes back on both days.

**Sessions paging** [verified]: `nextPageToken` is a sync token, returned
"only if both startTime and endTime are omitted". With neither times nor a
token, the call returns "sessions modified in the last 30 days";
`includeDeleted=true` adds `deletedSession[]` in that mode only. With a time
window, sessions "starting before endTime and ending after startTime up to
(endTime + 1 day)" are returned, unpaged. A token never runs out; a page with
no sessions and no deletions is the end [inferred].

**Wire encoding** [verified]: every int64 is a JSON string
(`startTimeNanos`, `endTimeNanos`, `modifiedTimeMillis`,
`rawTimestampNanos`, `minStartTimeNs`, `maxEndTimeNs`, and a session's
`startTimeMillis`, `endTimeMillis`, `modifiedTimeMillis`,
`activeTimeMillis`). `value[]` is positional, one entry per field of the data
type in its order, each setting one of `intVal`, `fpVal`, `stringVal` or
`mapVal`. An instantaneous reading has `startTimeNanos` equal to
`endTimeNanos`. No point and no session carries a time zone or UTC offset.

**Data sources** [verified]: `dataStreamId` is
`type:dataType.name:application.packageName:device.manufacturer:device.model:device.uid:dataStreamName`,
absent parts left out. The project number and `device.uid` are obfuscated per
reading client. `originDataSourceId` on a derived point names the raw writer
and is documented as debugging information. `estimated_steps` is the step
source the Fit app shows.

**Data types** [verified]:

| Type | Scope | `value[]` fields (units) |
| --- | --- | --- |
| `com.google.heart_rate.bpm` | heart_rate | `bpm` fp (beats/min) |
| `com.google.step_count.delta` | activity | `steps` int |
| `com.google.calories.expended` | activity | `calories` fp (kcal) |
| `com.google.calories.bmr` | activity | `calories` fp (kcal per day) |
| `com.google.distance.delta` | location | `distance` fp (m) |
| `com.google.activity.segment` | activity | `activity` int (activity type) |
| `com.google.active_minutes` | activity | `duration` int (ms) |
| `com.google.heart_minutes` | activity | `intensity` fp (heart points) |
| `com.google.weight` | body | `weight` fp (kg) |
| `com.google.height` | body | `height` fp (m) |
| `com.google.body.fat.percentage` | body | `percentage` fp (0 to 100) |
| `com.google.sleep.segment` | sleep | int: 0 unspecified, 1 awake, 2 sleeping (no stages), 3 out of bed, 4 light, 5 deep, 6 REM |
| `com.google.oxygen_saturation` | oxygen_saturation | saturation fp (%), supplemental flow fp (L/min), then three optional ints |
| `com.google.blood_pressure` | blood_pressure | systolic fp, diastolic fp (mmHg), optional body position int (1 standing to 4 reclining), optional location int (1 left wrist to 4 right upper arm) |
| `com.google.blood_glucose` | blood_glucose | level fp (mmol/L), optional: relation to meal, meal type, relation to sleep, specimen source (ints) |

Sleep is sessions with `activityType: 72` plus `sleep.segment` points inside
them. Session types in the recordings: running 8, biking 1, road biking 16,
walking 7, strength training 80, other 108. The sync does not read
`com.google.distance.delta`, because its scope also grants location samples.

## Sources

- https://developers.google.com/fit/overview (the deprecation notice)
- https://developer.android.com/health-and-fitness/health-connect/migration/fit
- https://developers.google.com/health/migration
- https://fitness.googleapis.com/$discovery/rest?version=v1
- https://developers.google.com/fit/rest/v1/reference/users/dataSources/datasets/get
- https://developers.google.com/fit/rest/v1/reference/users/sessions/list
- https://developers.google.com/fit/datatypes/activity , `/health`, `/location`
- https://developers.google.com/fit/scenarios/read-sleep-data
