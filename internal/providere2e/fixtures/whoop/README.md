# The WHOOP fixture set

Seven recordings, served by `tools/mockserver.py` as WHOOP's developer API v2.
`python3 providers/whoop/fixtures/audit.py` gates the commit and must print 0.

## READ THIS FIRST: these are NOT a cut of a real pull

`docs/provider-practices.md` §2 says a fixture set is a CUT of real payloads,
never invented ones, and it is right. **This set breaks that rule, knowingly,
because there is no pull to cut from.**

The owner's stored WHOOP credential (`raw/credentials.local.json`, metadata
`{"provider": "whoop"}`) is a BARE ACCESS TOKEN minted 2026-07-25. WHOOP access
tokens live one hour, there is no refresh token beside it, and
`tools/rawpull/whoop.py` therefore answers:

    whoop: 401 Authorization was not valid

Probed live on 2026-09-16 against both `/v1/user/profile/basic` and
`/v2/user/profile/basic`. `raw/whoop/` is empty. T-017 carries the whole
account of it and what it costs.

So every payload here was written from **WHOOP's own published schema** —
`https://api.prod.whoop.com/developer/doc/openapi.json`, which needs no token —
field by field, with the persona's owner (Ada Lovelace,
`ada@zephyr-lane.example`) as the member and plausible physiology. Nothing in
this directory came from anybody's body.

**What that means for what a green run proves.** It proves the kinds, the
sync, the references, the paging and the write contract against the shape
WHOOP publishes. It does NOT prove them against the shape WHOOP *sends* — the
gap every one of the first three providers found the hard way (a field the
schema documents and no payload carries; a field every payload carries and the
schema does not document; an enum value the docs forgot). **This set is to be
replaced by a real cut the moment a WHOOP grant exists**, and the replacement
is not a re-run of a generator: it is the four-command pipeline in
`provider-practices.md` §2, with `tools/pseudonymise.py` and this audit as the
gate. There is deliberately no `makefixtures.py` here to make the wrong thing
easy.

Three pseudonymiser rules have to land before that cut can be committed at all
— an RFC 3339 instant shift, a body-measurement jitter, and a resource class
the path can name. They are written up in `RULES["whoop"]` in
`tools/pseudonymise.py` and in this provider's `README.md` under *Platform
asks*.

## What is in the set

| Recording | What it holds |
| --- | --- |
| `GET_v2_user_profile_basic.json` | the member: id, address, first and last name |
| `GET_v2_user_measurement_body.json` | height in metres, weight in kilograms, max heart rate |
| `GET_v2_cycle.json` | three cycles and a `next_token` — page one of two |
| `GET_v2_cycle__nextToken-…json` | the last two cycles — page two, no token |
| `GET_v2_recovery.json` | four recoveries, one page |
| `GET_v2_activity_sleep.json` | four sleeps, one page |
| `GET_v2_activity_workout.json` | four workouts, one page |

Five cycles, four recoveries, four sleeps, four workouts and one member, all
inside 2026-09-08 → 2026-09-12. The dates are FIXED rather than relative to the
clock: a set that moves with today is a set nobody can reproduce.

Every collection is ordered **newest first**, as WHOOP serves one. Nothing in
the sync depends on the order — it pages on `nextToken` and keys every row on
WHOOP's own identity — but a fixture that contradicts the API teaches the next
reader something false (Codex review, finding 11).

## The cases it was built to carry

Each of these is asserted by `../e2e/scenario.py`, by reference and not by
string.

- **the `nextToken` walk** — the cycle stream is the one recorded over two
  pages, so a sync that read only the first page would land three rows instead
  of five and the scenario would say so;
- **a CURRENT cycle** — `742812004` carries no `end` at all, because WHOOP
  omits it until the member wakes. It is what proves an absent trait slot is
  absent and not null;
- **the two unscored states** — one cycle is `UNSCORABLE` (the strap was off),
  the current one is `PENDING_SCORE`, one recovery is `UNSCORABLE` and one
  sleep is `PENDING_SCORE`. All four carry NO `score` object, which is how
  WHOOP says it and what the mirror has to reproduce;
- **a nap and three nights** — one `sleep` kind, told apart by WHOOP's own
  `nap` flag, never by two kinds;
- **the two upstream identities**, as a COMPATIBILITY fixture — exactly one
  sleep and one workout carry a `v1_id` beside the v2 UUID, and the rest carry
  none. WHOOP documents `v1_id` as not existing past 09/01/2025, so a record
  dated 2026 carrying one is a record captured before that cutoff and kept:
  the mirror has to hold both identities where they exist and write neither as
  null where they do not, and no payload WHOOP sends today would prove it;
- **the deprecated `sport_id`** — on the same older workout and nowhere else,
  beside the `sport_name` every v2 workout carries. A compatibility fixture for
  the same reason;
- **a recovery from older hardware** — no `spo2_percentage` and no
  `skin_temp_celsius`, because WHOOP only sends them from 4.0 onward. Absent,
  not zero;
- **a member still calibrating** — `user_calibrating: true` on the last
  recovery, WHOOP's own fact about the member and not a judgement this mirror
  makes;
- **a workout WHOOP measures distance for and one it does not** — a run with
  `distance_meter` and `altitude_*`, a weightlifting session with neither;
- **a sport the published table grew later** (`padel`), which is the whole
  reason `sportName` is a `string` and not an `enum`;
- **one reference that deliberately DANGLES** — the `UNSCORABLE` recovery's
  `sleep_id` names a sleep this cut does not hold. It is a **deliberate
  coverage gap, not a window exclusion**: the four streams share a floor here,
  so nothing about this set explains the absence, and saying it did would be a
  story rather than a reason (Codex review, finding 11). What it proves is the
  thing worth proving — `recovery.sleep` carries no `mustExist`, so the row is
  written anyway and heals on a walk that reaches the sleep, instead of minting
  a sleep row out of a reference.

## What it does NOT cover, and what that costs

A green run over this set proves nothing about any of the following. Each is a
real gap, not a theoretical one.

| Not covered | Why it matters |
| --- | --- |
| **Every payload is schema-shaped** | See above. The one thing a real cut buys — the difference between what WHOOP documents and what WHOOP sends — is exactly what is missing here. |
| **No failure matrix** | No 401, no 403 on the body-measurement scope, no 429 with a `Retry-After`, no 5xx. The sync's permanent/transient split and `retryNotBefore` are written and UNEXERCISED. `tools/mockserver.py`'s `__response` envelope is how they would be recorded. |
| **No multi-page stream but one** | Only `cycle` pages. A drain that crossed the wall-clock budget mid-stream, and the `more`/`resume` checkpoint that carries it, is not exercised. |
| **No second-generation payload** | Nothing changes between the two syncs, so "a re-scored cycle converges on the same row" is proved only as "nothing moved". The `__responses` envelope (a different body on the second call) is how that would be recorded. |
| **No unknown `score_state`** | The sync leaves the property absent for a word its table does not hold; no recording carries one. |
| **No late correction** | Nothing here is dated outside the window the sync reads, so the one real limitation of a windowed feed — an activity WHOOP corrects weeks later — is not exercised and cannot be. See the provider README, *What could not be expressed*. |
| **One member, one account** | No queue of two accounts, and no thin `user` row minted from a record's `user_id` with the profile toggle off. |
| **No `activity-mapping` call** | `GET /v1/activity-mapping/{v1Id}` (v1 id → v2 id) is in WHOOP's published description and the sync never calls it, so nothing here answers it. |
