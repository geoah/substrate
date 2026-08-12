# The WHOOP bundle — a sync-only wearable integration

The second provider **integration-as-bundle** (after Google Contacts): it
connects a WHOOP account over the host OAuth facility and mirrors the
wearable's daily physiology into the graph — **recovery** (score / HRV /
resting heart rate), **sleeps** (performance, stage durations) and
**workouts** (sport, strain, calories). Sync-only: no recordmapping, no
subject resolution — the mirror records are their own subjects.

Everything composes from the same primitive set the Google bundle uses:
**bundle · kind · trait · function · trigger**, plus the host OAuth
facility (the `oauth2` trait on the config, `accountconfig` on the account,
the trusted `oauth2:` provider block on the bundle document) and the
connector runtime (a **PEP 723** Python body — `requests` through **uv**).

## What it does

```
account (connected, any toggle on)
      │  on-connect trigger (fires once — guarded on "no lastSyncedAt yet")
      ▼
sync (python / uv / WHOOP v2 API)
      │  pages each ENABLED collection over nextToken, off the causal chain:
      │  recovery → sleep → workouts, one API page per invocation
      ▼
recovery / sleep / workout mirrors  (plain puts — re-sync converges)
      │
      └─ final page stamps the account: lastSyncedAt + syncStatus
         ("ok", or "ok (N skipped)" when bad records were tolerated;
          "erroring: <reason>" when the provider failed the account)

hourly schedule ──▶ sync ──▶ every connected account due by its syncFrequency
```

- **`config`** (bundleconfig + oauth2): the ONE WHOOP OAuth client — `clientId`
  and secret-typed `clientSecret`. The provider endpoints and the feature→scope
  mapping are TRUSTED manifest metadata on the bundle document, never config
  properties (review-google #1).
- **`account`** (accountconfig): one connected WHOOP account — the host-written
  `tokenRef`/`tokenStatus`/`grantedScopes`/`email` (writer: oauth), the owner's
  `enabledRecovery`/`enabledSleep`/`enabledWorkouts` toggles plus
  `syncFrequency` and `backfillDepth` (labeled required enums with defaults),
  and the connector-owned `lastSyncedAt`/`syncStatus`.
- **`recovery`** — cycle-keyed: one row per WHOOP physiological cycle per
  account (`cycleId`, `date`, `recoveryScore`, `hrvMs`, `restingHeartRate`,
  `raw`). The cycle id is the identity (the sleep id when a record carries no
  cycle): WHOOP re-scores a cycle as data settles, so a re-sync converges on
  the same row — and cycles do not reliably align with calendar days, so two
  records created on one UTC date stay two rows. The `date` is display data
  derived from the record's UTC `created_at`.
- **`sleep`** — one row per WHOOP sleep (`start`/`end`, `nap`, `performance`,
  stage `durations` in ms, `raw`), keyed by the provider's sleep id.
- **`workout`** — one row per workout (`start`/`end`, `sport`, `strain`,
  `calories` in kcal, `raw`), keyed by the provider's workout id.
- **`whoopsync`** (python / PEP 723): reads the account's host-resolved access token
  off the injected `config`, pages each enabled collection with `nextToken`
  through the paged-checkpoint `more`/`resume` contract (the sync window is
  pinned in the cursor), and upserts mirrors by `host.ids.external("whoop",
  account, "recovery-<cycle_id>" | "sleep-<id>" | "workout-<id>")`.
  **Per-record failure tolerance**: a malformed record is skipped, logged and
  counted into the final `syncStatus` stamp — the page keeps draining.
  **Per-account isolation**: a page-level provider failure (a 401 on a revoked
  grant, a 5xx, a dead network) stamps THAT account `syncStatus: "erroring:
  <reason>"` and the queue moves on — a poisoned account never blocks the
  accounts behind it; the schedule retries it next tick, and a later ok run
  overwrites the stamp. The token only ever leaves the body over HTTPS to
  `api.prod.whoop.com` (or loopback, the test seam).

## Windows, not sync tokens

WHOOP's v2 API has no incremental sync token — collections are queried by
`[start, end]` window. The first sync's start date comes from
`backfillDepth`: `none` means **from connect forward** (the first window is
`[connect, now]` — nothing earlier is requested), `last30d`/`last90d`/
`last1y` reach that far back, and `all` reads from a fixed pre-WHOOP-API
epoch. Steady-state re-syncs read a 48-hour overlap behind `lastSyncedAt`,
because recovery and sleep scores settle hours after their records first
appear — so with `none`, records from up to two days before connect can
still land once their scores settle. The provider-keyed puts make the
overlap converge instead of duplicate.

## Install, configure, connect

```sh
substratectl apply -f bundle.yaml -f triggers.yaml
```

Then, once (the config is per-repository — client id + secret — so it is not
shipped here):

1. **Register a WHOOP app** in the [WHOOP developer dashboard](https://developer-dashboard.whoop.com): create an app, note its
   client ID and secret, and enable the scopes you intend to grant —
   `read:recovery`, `read:sleep`, `read:workout`, `read:profile`, `offline`
   (`offline` is what mints the refresh token; `read:profile` lets the host
   derive the connected email).

2. **Register the redirect URL** on that WHOOP app. The host surfaces one
   redirect URI — the OAuth facility's callback:

   ```
   https://<your-substrate-host>/api/v1/core.substrate.reamde.dev/oauth/callback
   ```

   (The value is the host's `SUBSTRATE_OAUTH_CALLBACK_URL`.)

3. **Create the config.** One `config` carrying the app's `clientId` +
   `clientSecret`. Until it exists the bundle reads "needs configuration" and
   the OAuth flow refuses.

4. **Add an account.** Create an `account`, turn on the collections you want
   (`enabledRecovery`/`enabledSleep`/`enabledWorkouts`), pick a `syncFrequency`
   and a `backfillDepth`. It starts `tokenStatus: pending`; the email is
   derived from the grant, never typed.

5. **Connect.** Run the host OAuth flow against the account
   (`substratectl bundle connect`, or `POST …/core.substrate.reamde.dev/oauth/start
   {"record": "<accountId>"}`) → consent at WHOOP → the callback stores the
   grant, sets `tokenStatus: connected`, and the on-connect trigger fires the
   first backfill.

The requested scope union is derived per StartOAuth from the account's
enabled toggles through the manifest's `featureScopes` — a disabled
collection's scope is never requested.

## Disconnecting — revocation is MANUAL

WHOOP has no RFC 7009-style revocation endpoint. Its documented revocation is
an OAuth-**authenticated** `DELETE
https://api.prod.whoop.com/developer/v2/user/access` — a method + auth shape
the host OAuth facility cannot speak (it only posts a `token=` form,
unauthenticated, per RFC 7009). The bundle therefore declares **no
`revocationEndpoint`** (the field is optional): deleting an `account` tears down
the locally stored credentials and releases the finalizer, but the grant at
WHOOP **stays live** until revoked by hand — in the WHOOP app (App & Privacy →
connected apps) or by calling the DELETE above with a still-valid access token.

## Files

- `bundle.yaml` — the **schema closure**: authority + bundle + config type +
  account type + the three mirror types + the sync function.
- `triggers.yaml` — the **delivery wiring**: the on-connect backfill trigger
  and the hourly re-sync schedule, as ordinary data records.

## Tested

`engine/whoop_bundle_db_test.go` installs this closure from these very files:
schema admission through the loader (traits, the oauth2 block's scope map —
and NO revocation endpoint — the install closure), the live install into a
test repository, and — against a loopback fake WHOOP (httptest, no live providers
ever) — the whole flow: OAuth connect, the on-connect trigger, paged sync
over `nextToken`, the three mirror rows, the cycle-keyed recovery ids (two
records created on the SAME UTC date land as two rows), one deliberately
malformed record skipped and counted into `syncStatus: "ok (1 skipped)"`, and
the `lastSyncedAt` stamp.
