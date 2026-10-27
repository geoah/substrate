# WHOOP developer API v2: notes for the WHOOP provider and its `adaptwhoop` adapter

The provider (`kinds/providers.substrate.reamde.dev/whoop`; its version is
the `version:` in that bundle's package header) pulls `/v2/cycle`,
`/v2/recovery`, `/v2/activity/sleep`, `/v2/activity/workout` and the two
`/v2/user/*` calls. These fixtures give
the adapter a 30-day series (local dates 2026-09-11 to 2026-10-10), a second
sync that sees a rescore and two deletions, and the webhook deliveries WHOOP
would send in between. Researched 2026-10-11.

Provenance: **[verified]** read today on an official WHOOP page or in
`openapi.json`; **[repo]** read in this repository; **[recalled]** not
re-checked today; **[inferred]** my reading, not stated anywhere.

## Endpoints

Base URL `https://api.prod.whoop.com/developer` [verified, OpenAPI and the
pagination page].

| Call | Scope | Notes |
| --- | --- | --- |
| `GET /v2/cycle` | `read:cycles` | collection |
| `GET /v2/cycle/{cycleId}` | `read:cycles` | one cycle |
| `GET /v2/cycle/{cycleId}/recovery` | `read:recovery` | the recovery of one cycle |
| `GET /v2/cycle/{cycleId}/sleep` | `read:sleep` | the sleep that starts the cycle |
| `GET /v2/recovery` | `read:recovery` | collection |
| `GET /v2/activity/sleep[/{sleepId}]` | `read:sleep` | sleep ids are UUIDs |
| `GET /v2/activity/workout[/{workoutId}]` | `read:workout` | workout ids are UUIDs |
| `GET /v2/user/profile/basic` | `read:profile` | `user_id`, `email`, names |
| `GET /v2/user/measurement/body` | `read:body_measurement` | no timestamp in the object |
| `DELETE /v2/user/access` | any | revokes the grant; webhooks stop |
| `GET /v1/activity-mapping/{activityV1Id}` | | v1 id to v2 UUID |

Collection parameters, all four collections [verified, OpenAPI]:

| Parameter | Meaning |
| --- | --- |
| `limit` | default 10, maximum 25 |
| `start` | date-time, inclusive; absent means no lower bound, so full history is readable |
| `end` | date-time, exclusive; absent means now |
| `nextToken` | the previous page's `next_token`; repeat `start`/`end` with it |

The page is `{"records": [...], "next_token": "..."}`; no `next_token`
means the last page [verified]. Records come newest first [repo: the
existing fixture README; not stated by WHOOP].

Rate limits: 100 requests a minute and 10,000 a day per app by default, a
`429` past either, and `X-RateLimit-Limit`, `X-RateLimit-Remaining`,
`X-RateLimit-Reset` (seconds) on responses [verified, rate-limiting page].
The existing sync reads `Retry-After` [repo]; WHOOP's page does not mention
that header, so treat `X-RateLimit-Reset` as the documented one.

## Auth

OAuth 2.0 authorization code [verified, OAuth page and OpenAPI].

| Item | Value |
| --- | --- |
| Authorize | `https://api.prod.whoop.com/oauth/oauth2/auth` |
| Token and refresh | `https://api.prod.whoop.com/oauth/oauth2/token` |
| Scopes | `read:recovery read:cycles read:workout read:sleep read:profile read:body_measurement`, plus `offline` to receive a refresh token |
| `state` | must be eight characters if you generate it yourself |
| Access token lifetime | `expires_in` seconds; one hour [recalled, matches the repo's T-017 note] |
| Refresh | rotates: the refresh response invalidates the old access AND refresh token; two concurrent refreshes fail the second |
| PKCE | not mentioned on the OAuth page |

## Webhooks

All facts in this section are [verified] on
`developer.whoop.com/docs/developing/webhooks` unless marked.

- **Registration**: one HTTPS webhook URL per app, set in the Developer
  Dashboard with a model version per URL. v2 is the default; v1 webhooks
  "are no longer published". There is no API call to register.
- **Body**: `{"user_id": int64, "id": UUID string, "type": string,
  "trace_id": string}`. It is a notification only; the receiver calls the
  API for the data.
- **Events**: `workout.updated`, `workout.deleted`, `sleep.updated`,
  `sleep.deleted`, `recovery.updated`, `recovery.deleted`. A create arrives
  as `*.updated`. There are no webhooks for cycles or body measurements.
- **Recovery ids**: in v2 the `id` of both recovery events is the UUID of
  the recovery's SLEEP, not the cycle id. "A recovery is deleted when its
  associated sleep is deleted."
- **Signature**: headers `X-WHOOP-Signature` and
  `X-WHOOP-Signature-Timestamp` (milliseconds since epoch). The signature is
  `base64(HMAC-SHA256(key = the app's client secret, message = timestamp
  header value + raw request body))`. A mismatch means drop the request. No
  freshness window is documented.
- **Retries**: five retries over about one hour after any non-2xx answer or
  a timeout. WHOOP asks for a 2XX within a second.
- **Duplicates and gaps**: duplicates happen (dedupe on `trace_id`), and a
  webhook "may be missed", so WHOOP recommends a reconciliation job that
  re-reads the API.

What this means here [repo]: the door's `202` fits. The `whoop-webhook`
trigger declares WHOOP's signature, and the server checks it against the
config record's client secret before it stores the request (decision 0164),
so `whoopwebhook` never sees the secret. The mirror keys a recovery by cycle
id, so `recovery.deleted` (a sleep UUID) needs a lookup by the recovery's
`sleep` reference, and `recovery.updated` needs the sleep read, which is why
the recovery toggle also requests `read:sleep`.

## Objects, units and time

| Object | Fields (units) |
| --- | --- |
| Cycle | `id` int64, `user_id`, `created_at`, `updated_at`, `start`, `end` (absent while current), `timezone_offset`, `score_state`, `score`, `step_count` (int32, `null` when no step data) |
| CycleScore | `strain` (0 to 21), `kilojoule` (kJ), `average_heart_rate`, `max_heart_rate` (int, beats/min) |
| Recovery | `cycle_id`, `sleep_id` (UUID), `user_id`, `created_at`, `updated_at`, `score_state`, `score` |
| RecoveryScore | `user_calibrating` bool, `recovery_score` (%), `resting_heart_rate` (beats/min, float), `hrv_rmssd_milli` (ms RMSSD), `spo2_percentage` (%), `skin_temp_celsius` (degC, absolute); the last two only from WHOOP 4.0 hardware |
| Sleep | `id` UUID, `cycle_id`, `v1_id` (not past 2025-09-01), `user_id`, `created_at`, `updated_at`, `start`, `end`, `timezone_offset`, `nap` bool, `score_state`, `score` |
| SleepScore | `stage_summary` (all `_milli` fields: `total_in_bed`, `total_awake`, `total_no_data`, `total_light_sleep`, `total_slow_wave_sleep`, `total_rem_sleep`, plus `sleep_cycle_count`, `disturbance_count`), `sleep_needed` (`baseline_milli`, `need_from_sleep_debt_milli`, `need_from_recent_strain_milli`, `need_from_recent_nap_milli` which is zero or negative), `respiratory_rate` (breaths/min), `sleep_performance_percentage`, `sleep_consistency_percentage`, `sleep_efficiency_percentage` (%); performance and consistency "may not be reported" |
| Workout | `id` UUID, `v1_id`, `user_id`, `created_at`, `updated_at`, `start`, `end`, `timezone_offset`, `sport_name`, `sport_id` (not past 2025-09-01), `score_state`, `score` |
| WorkoutScore | `strain`, `average_heart_rate`, `max_heart_rate`, `kilojoule` (kJ), `percent_recorded` (%), `distance_meter`, `altitude_gain_meter`, `altitude_change_meter` (m, only when sent), `zone_durations.zone_zero_milli` to `zone_five_milli` (ms) |
| Body | `height_meter` (m), `weight_kilogram` (kg), `max_heart_rate` |

All [verified, OpenAPI]. `step_count` on Cycle is newer than the shipped
bundle's declarations [repo: the wearables page says the bundle lacks it].

Instants are RFC 3339 in UTC with milliseconds and `Z`. `timezone_offset`
is a W3C TZD string (`+hh:mm`, `-hh:mm` or `Z`) holding "the user's timezone
offset at the time the cycle was recorded" [verified]. The local date of a
cycle is `start` shifted by that offset [inferred]. `score_state` is
`SCORED`, `PENDING_SCORE` or `UNSCORABLE`, and `score` is present only when
`SCORED` [verified].

A cycle is not a calendar day: the sleep page calls the main sleep "the
sleep activity that starts a Cycle" [verified]. These fixtures therefore
start each cycle at its main sleep's `start`, and so do the provider e2e
fixtures and the synthetic WHOOP data (internal/synth).

## Corrections, deletions, backfill

- A rescore keeps the id and moves `updated_at`; there is no version field
  [verified that `updated_at` is "last updated"; that it moves on rescore is
  [recalled] and is what WHOOP's webhook test steps describe].
- Deletion has no tombstone in the collections: the object stops appearing.
  The signals are `sleep.deleted`, `workout.deleted`, `recovery.deleted`
  [verified]. `GET` on a deleted id answering `404` is [inferred]; the body
  WHOOP sends with it is unknown, so the fixture uses `{"message": "Not Found"}`.
- Cycles have no delete event; how a cycle disappears (if ever) is not
  documented.
- Backfill: `start` absent means no lower bound [verified], so the whole
  history is one paged read; there is no documented horizon.

## The fixtures

Persona: WHOOP member 4021873 (`ada@zephyr-lane.example`, as in the existing
set), Athens (+03:00), in London (+01:00) from 2026-09-24 to 2026-09-28.
Sync 1 runs at 2026-10-10T15:00Z, sync 2 at 19:00Z. Files that differ
between the syncs use the mock's `{"__responses": [sync 1, sync 2]}`
envelope, so the second read of a file returns the second body.

| File | Holds |
| --- | --- |
| `GET_v2_cycle.json`, `GET_v2_cycle__nextToken-MTA0NzA0MQ.json` | 30 cycles, 25 + 5, two syncs |
| `GET_v2_recovery.json`, `..._nextToken-MTA0NzAzOXJlYw.json` | 30 recoveries, 25 + 5, same in both syncs |
| `GET_v2_activity_sleep.json`, `..._nextToken-c2xlZXAtMjAyNjA5MTY.json` | 30 sleeps + 3 naps in sync 1, the deleted nap gone in sync 2 |
| `GET_v2_activity_workout.json`, `..._nextToken-d29ya291dC0yMDI2MDkxNQ.json` | 29 workouts each sync |
| `GET_v2_user_profile_basic.json`, `GET_v2_user_measurement_body.json` | member, body |
| `GET_v2_activity_sleep_{uuid}.json`, `GET_v2_cycle_1047561_recovery.json` | the follow-up reads for deliveries 01 and 02 |
| `GET_v2_activity_workout_9e266b74-....json`, `GET_v2_cycle_1047510.json` | the late workout and the rescored cycle |
| `GET_v2_activity_sleep_d728f6f4-....json`, `GET_v2_activity_workout_e3f89923-....json` | `404` envelopes for the deleted objects |
| `inbound/01..08_*.json` | webhook deliveries as `{"request": {method, contentType, headers, query, body: {text}}}`, the function's input envelope |
| `inbound/SECRET.txt` | the fake client secret every signature uses |

Special cases, by identifier:

| Case | Where |
| --- | --- |
| proposal's worked example | cycle `1047392` (2026-10-06 22:41 to 2026-10-07 22:38 local), recovery 67 / 52 / 61.3 / 96.5 / 33.8, strain 14.2, 8125.4 kJ, sleep `e3b0c442-98fc-4c14-9afb-1c1e0a7d4b9a`; its stage totals are generated and differ from the proposal's illustrative minutes |
| rescored cycle | `1047510`: sync 1 strain 12.0731, `updated_at` 2026-10-09T20:58:14.302Z; sync 2 strain 13.6118, `updated_at` 2026-10-10T15:31:47.865Z |
| late workout | padel `9e266b74-...`, logged by hand after sync 1 (`created_at` 2026-10-10T15:20:31Z); sync 1 instead holds the auto-detected `activity` `e3f89923-...`, deleted in between |
| deleted sleep | nap `d728f6f4-...` (2026-10-04); `06_sleep.deleted.json`; no `recovery.deleted` follows because a nap has no recovery |
| two cycles on one local date | `1046422` starts 2026-09-19 00:47, `1046463` starts 2026-09-19 22:51 |
| offset change | cycles `1046739` to `1046910` carry `+01:00` |
| current cycle | `1047561`: no `end`, `SCORED` with running strain and `step_count` |
| unscorable | cycle `1046319` and its recovery and sleep (night of 2026-09-16), `step_count: null`, no `score` |
| missing optional | the sleep of 2026-09-12 has no `sleep_consistency_percentage` |
| duplicate webhook | `03` repeats `01`'s `trace_id` with a new timestamp and signature |
| bad signature | `08` is signed with another secret |
| shape-only | `07_recovery.deleted.example.json` names a sleep outside the series |

The webhook bodies are compact JSON (`separators=(",", ":")`); WHOOP's real
serialization is unknown, which does not matter because the signature is
over whatever bytes arrive.

## Not verified

- PKCE support, access-token lifetime, and whether `Retry-After` is sent.
- The `404` body for a deleted object.
- Whether the current cycle is `SCORED` with partial strain before it ends
  (the cycle page says its `step_count` "updates as the Cycle continues").
- Collection ordering (newest first) and the `next_token` format (opaque
  here).

## Sources

- https://developer.whoop.com/docs/developing/webhooks
- https://developer.whoop.com/docs/developing/oauth
- https://developer.whoop.com/docs/developing/rate-limiting
- https://developer.whoop.com/docs/developing/pagination
- https://developer.whoop.com/docs/developing/user-data/cycle , `/sleep`, `/recovery`, `/workout`
- https://api.prod.whoop.com/developer/doc/openapi.json (downloaded 2026-10-11)
