# The GitHub bundle — sync-only mirrors of the owner's code work

The second real **integration-as-bundle** (after
`google.bundles.substrate.reamde.dev`): it connects a GitHub account over the host
OAuth facility and mirrors what GitHub holds about the owner into the graph —
the connected user, the repositories they can reach, and every **issue and
pull request they are involved in** (author / assignee / mentioned /
commenter / review-requested). The posture is deliberately narrow: the
owner's own work, never the firehose of every item in every repo. GitHub's `involves:` qualifier
does **not** cover review requests, so the sync runs a third
`review-requested:<login>` search beside the two `involves:` searches; a PR
returned by more than one search lands at the same deterministic id and
upserts, never duplicates.

**Sync-only, no writeback.** Nothing in this bundle writes back to GitHub —
no closing issues, no posting comments, no review submissions. The outbound
outbox (issue 009) does not exist yet; until it does, an integration is a
mirror. When it lands, writeback arrives as a separate slice over these same
mirror types.

It composes from the exact primitive set the google bundle proved:
**bundle · kind · trait · function · recordmapping · trigger**, the
host OAuth facility (the `oauth2:` manifest block with its `client` input +
the `accountconfig`/`oauth2` traits), and the connector runtime (a PEP 723
Python body pulling `requests` through uv).

## What it does

```
account (connected, a feature on)
      │  on-connect trigger (fires once — guarded on "no lastSyncedAt yet")
      ▼
sync (python / uv / GitHub REST)
      │  /user → /user/repos → search issues (involves:)
      │        → search PRs (involves:) → search PRs (review-requested:),
      │  one page per invocation, off the causal chain
      ▼
user ──recordmapping──▶ person   (match on public email, or mint a shell)
repository
issue ────repository edge──▶ repository
pullrequest ──repository edge──▶ repository
      │
      └─ final stage stamps the account: syncCursor (per-stage watermarks,
         → incremental next run) + login + lastSyncedAt + syncStatus

hourly schedule ──▶ sync ──▶ every connected account due by its syncFrequency
```

- **`config`** (oauth2): the GitHub OAuth app — `clientId` and the
  secret-typed `clientSecret`, nothing else. The bundle's `client` input
  resolves one record of this kind for the host OAuth facility (the sole
  record, the one named `default`, or a bound one). The provider endpoints and
  the feature→scope mapping are TRUSTED manifest metadata on the bundle
  document (review-google #1), never config properties.
- **`account`** (accountconfig): one connected account — the `enabledUser` /
  `enabledRepos` / `enabledIssues` / `enabledPullRequests` toggles,
  `syncFrequency`, `backfillDepth`, the host-written
  `tokenRef`/`tokenStatus`/`grantedScopes`/`email` (writer: oauth), and the
  connector state `syncCursor`/`login`/`lastSyncedAt`/`syncStatus` (writer:
  connector). Displays as the public email, falling back to the
  connector-stamped `login` when GitHub exposes no public email.
- **`user`** (mirror + mapped source type): one row per (account, login). The
  connected user's row is written whole off `GET /user`; issue and PR authors
  land as create-only stubs. Its required `person` subject edge is left empty
  on write — the mapping resolves it.
- **`repository`** (mirror): written whole off `GET /user/repos` under
  `enabledRepos`; issues and PRs also mint create-only stubs for repositories
  outside that listing, so their required parent edge always resolves.
- **`issue` / `pullrequest`** (mirrors): the provider's shape, plainly —
  `state` is a plain string (the PR state folds merged / closed / draft / open
  the way v4 did), `title`/`body` ride the built-in columns (body truncated at
  4000 chars, and dropped from `raw` to keep a 100-item page inside the frame
  cap), `authorLogin`, `labels`, `htmlURL`, the provider timestamps, and `raw`.
  Each carries a required `repository` edge. There is deliberately NO edge at
  `user`: it is a mapped source type, and the bipartite rule (record 50)
  forbids any edge landing on one — the human behind `authorLogin` is reached
  through the user mirror and its person mapping.
- **`githubsync`** (python / PEP 723): reads the account's host-resolved access token
  off the injected config, walks the stages **pinned at queue-head** (a toggle
  flipped mid-drain can neither crash nor shift the walk), and pages the REST
  API **off the causal chain** via the paged-checkpoint `more`/`resume`
  contract — one GitHub page per invocation, at constant causal depth. Ids are
  `host.ids.external("github", account, key)` with keys like `user/<login>`,
  `repo/<owner/name>`, `issue/<owner/name>#<n>` — deterministic, so a re-sync
  upserts the same rows and never duplicates.
- **`userperson`** (recordmapping): `match` probes the profile's public email
  against people's emails — exactly one live match links, zero-or-many mints a
  person shell (author stubs carry no email, so they mint `utility`-prominence
  shells). `map` copies the full name, prefers the login as the friendly
  `displayName`, and unions the email onto the person as machine-managed
  properties.

## Incremental windows: per-stage watermarks

Each search stage — `issues`, `pulls` (involves:), `pullsReview`
(review-requested:) — owns its **own** floor and watermark, stored together
in `syncCursor` as a JSON object (`{"issues": …, "pulls": …,
"pullsReview": …}`). One stage hitting a search cap can therefore never
advance another stage's tail past un-synced items. A legacy plain-RFC-3339
`syncCursor` (the old single shared watermark) still reads as every stage's
floor, so an upgrade never re-backfills.

- A **completed** stage stamps the **run-start** time as its new watermark —
  never the local completion clock, never the newest item seen — so items
  updated while the drain ran stay inside the next window. Queries step the
  stored floor back by a 120 s overlap.
- A **partial** response (`incomplete_results: true` from the search API)
  still upserts what arrived but **refuses to advance** that stage's
  watermark; `syncStatus` reads `ok (partial: <stages>)` and the next run
  retries the window.
- The **1,000-result search ceiling**: when a stage drains 10 full pages,
  the sync partitions by updated-range — it restarts the same stage at page
  one with the drained boundary as the new floor (inclusive), and repeats
  until the window drains. If more than 1,000 results share one updated
  instant (the boundary cannot move), the stage stamps the boundary,
  `syncStatus` reads `ok (capped: <stages>)`, and the sync never advances
  past the un-drained instant.

## Backfill depth

`backfillDepth` maps the same way across the whole bundle fleet:

| value                            | first window                                                         |
| -------------------------------- | -------------------------------------------------------------------- |
| `none`                           | from connect time, forward-only (the first run stamps its own start) |
| `last30d` / `last90d` / `last1y` | now − 30/90/365 days                                                 |
| `all`                            | epoch (unbounded)                                                    |

After the first sync the per-stage watermarks take over and every run is
incremental.

## Failure posture

- **Per-account isolation**: the scheduled drain wraps each account — one
  erroring account (a 401 on a token revoked at GitHub, a 403/429, a
  timeout) stamps **its** `syncStatus: "erroring: <reason>"` plus
  `lastSyncedAt`, and the queue moves on to the accounts behind it. GitHub
  OAuth tokens do not expire, so a revoked grant keeps "refreshing" fine and
  surfaces only here; `tokenStatus` belongs to the OAuth facility, so
  `syncStatus` carries auth failures.
- **Origin pinning**: the body refuses to send the access token anywhere
  except `https://api.github.com` or loopback (the test seam) — the
  bundle-side defense while the engine's `network` capability is
  declared-but-unenforced.
- **Deletes are not reconciled** — the search feed carries no tombstones; a
  deleted issue simply stops updating.

## Scopes

GitHub's classic OAuth scopes are coarse — there is no read-only repository
grant — so the manifest maps:

| toggle                | scopes              |
| --------------------- | ------------------- |
| `enabledUser`         | `read:user`         |
| `enabledRepos`        | `read:user`, `repo` |
| `enabledIssues`       | `read:user`, `repo` |
| `enabledPullRequests` | `read:user`, `repo` |

`repo` grants read/write on private repositories even though this bundle
only ever reads; enable only what you want consented (public-only use could
narrow to `public_repo` by editing the manifest). `read:user` rides every
toggle so an issues-only account still derives its email and login off
`GET /user`. The bundle deliberately does **not** request `user:email`: that
scope unlocks the private `GET /user/emails` list, which nothing here reads —
the identity probe uses the **public** profile email only. An account whose
profile sets no public email carries a blank `email` (its person mapping
mints a shell instead of matching) and displays as its `login`. v4
sidestepped scopes entirely with a personal access token; the bundle model
makes the grant explicit at consent time instead.

## Install, configure, connect

```sh
substratectl apply -f bundle.yaml -f triggers.yaml
```

Then, once (the config is per-repository — client id + secret — so it is not
shipped here):

1. **Register a GitHub OAuth app** (github.com → Settings → Developer
   settings → OAuth Apps). The **Authorization callback URL** is the host's
   OAuth facility callback:

   ```
   https://<your-substrate-host>/api/v1/core.substrate.reamde.dev/oauth/callback
   ```

   (The value is the host's `SUBSTRATE_OAUTH_CALLBACK_URL`.)

2. **Create the config.** One `config` carrying the app's `clientId` +
   `clientSecret`; the `client` input resolves it. Until one resolves the
   bundle status carries a setup step and the OAuth flow refuses.

3. **Add an account.** Create an `account`, switch on the features you want
   (`enabledIssues`, `enabledPullRequests`, …), pick a `syncFrequency` and a
   `backfillDepth`. It starts `tokenStatus: pending`.

4. **Connect.** Run the host OAuth flow against the account
   (`substratectl bundle connect`, or `POST …/core.substrate.reamde.dev/oauth/start
   {"record": "<accountId>"}`) → consent at GitHub → the callback stores the
   grant, sets `tokenStatus: connected`, writes the account's public email
   off `GET /user`, and the on-connect trigger fires the first backfill.

Note: GitHub has no RFC 7009 revocation endpoint (app grants revoke over a
Basic-auth `DELETE /applications/{client_id}/grant`), so the manifest
declares none — disconnecting deletes the stored credential; revoke the
grant itself from GitHub's settings if desired.

## Files

- `bundle.yaml` — the **schema closure**: authority + bundle (with the trusted
  `oauth2:` provider metadata) + config type + account type + the four
  mirror types + the sync function + the user→person mapping.
- `triggers.yaml` — the **delivery wiring**: the on-connect backfill trigger
  and the hourly re-sync schedule, as ordinary data records.

## Tested

`engine/github_bundle_db_test.go` installs this closure from these very
files. It admits the schema through the loader (types, traits, the bundle
closure, the compiled `oauth2:` manifest metadata, the scope decisions, and
the mapping shape — no uv needed) and, where uv is available: applies the
whole closure into a test repository and asserts every member installs; drives
the **whole flow against a loopback fake GitHub** (OAuth connect, the
on-connect trigger, all three searches with cross-search dedupe, the person
mapping, and the per-stage `syncCursor` stamp); steps the paged chain
page-by-page to prove the cursor shape (pinned stages, per-stage floors and
watermarks), the mid-drain toggle-off resilience, the `incomplete_results`
watermark refusal, and the 1,000-result partition hop; and proves the
origin-pin refusal path stamps `syncStatus: erroring` without sending the
token. Real GitHub API calls never run in a test — only loopback fakes;
live OAuth + sync is verified against a connected account.
