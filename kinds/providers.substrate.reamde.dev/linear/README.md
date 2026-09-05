# The Linear bundle: the issues assigned to you, mirrored

Connects a Linear workspace over the host OAuth facility and syncs the issues
assigned to the connected login into `issue`, `user` and `team` mirrors, in
Linear's own shape.

**MIRRORS ONLY.** Every row it writes is one of its own kinds: what an issue
means to your repository (a task, a person) is your vocabulary, reached by a
mapping you declare from these mirrors (decision record 0049). A mapped
property is recomputed at the machine tier, so a value you write by hand wins
over the sync, which is what the deleted `taskprojection` used to arrange by
hand.

**SYNC-ONLY.** Nothing in this bundle writes back to Linear: completing a task
in the substrate does **not** move the Linear issue. Provider writeback is an
outward write to someone else's system and waits on the outbound-writes outbox
(issue 009); until that lands, this bundle's only writes are substrate writes.

It composes from the same primitive set as the Google bundle:
**bundle · kind · trait · function · trigger**.

## What it does

```
account (connected, issues on)
      │  on-connect trigger (fires once — guarded on "no lastSyncedAt yet")
      ▼
issuessync (python / uv / Linear GraphQL)
      │  pages viewer.assignedIssues, off the causal chain
      ▼
user (empty subject slot: a mapping the repository declares fills it)
team ◀──team reference── issue (empty `assignee` slot, same rule)
      │
      └─ final page stamps the account: lastSyncedAt (→ incremental next run),
         syncStatus, email (from `viewer`)

hourly schedule ──▶ issuessync ──▶ every connected account due by its syncFrequency
```

- **`config`** (oauth2): the Linear OAuth application — `clientId` and
  secret-typed `clientSecret`, resolved by the bundle's `client` input for
  the host OAuth facility (the sole record, the one named `default`, or a
  bound one). Endpoints and the feature→scope
  mapping are TRUSTED manifest metadata on the bundle document (`oauth2:`
  block), never config-record properties (review-google #1).
- **`account`** (accountconfig): one connected login — the `enabledIssues`
  toggle, `syncFrequency`, `backfillDepth`, the host-written
  `tokenRef`/`tokenStatus`/`grantedScopes`, and the connector-written
  `email`/`lastSyncedAt`/`syncStatus` (`ok` or `erroring: <reason>` — the
  degrade state is actually written, see below). Unlike `account`, `email` is
  `writer: connector`, not `writer: oauth`: Linear exposes no userinfo GET the
  facility could read at the exchange (identity lives behind the GraphQL
  `viewer` query), so the sync stamps it from `viewer.email`. The user still
  never types it.
- **`user` / `team` / `issue`** (mirror types): what Linear holds, in Linear's
  own shape, ids composed with `host.ids.external("linear", account,
  "user:|team:|issue:<uuid>")`. The issue carries `identifier`, `title` (the
  reserved built-in), `state` + `stateType`, `priority` (as its label), `url`,
  the raw node, an unpinned `assignee` subject slot and a `team`
  reference at its team mirror. The team reference is kept **current**: `team`
  is not repeated, so a write replaces a moved issue's previous team, and an
  issue that lost its team upstream gets its stale reference cleared.
This closure ships NO recordmapping. A mapping onto a kind is declared by the
package that owns that kind (decision record 0049), and this package owns no
person: a repository declares its own, from `user` on `property: person` and
from `issue` on `property: assignee`, probing the login and assignee addresses
it mirrors. When the workspace **hides the viewer's email** (admin-restricted
visibility) the issues carry no probe value at all, so the sync points each
issue's `assignee` slot at the viewer's own `user` mirror instead: a repository
whose mapping reaches people through that mirror resolves it in one hop, and a
hidden address costs one shell per login rather than one per issue (logged once
per run).
- **`issuessync`** (python / PEP 723 / requests): reads the account's
  host-resolved access token off the injected config, pages
  `viewer.assignedIssues` (50/page, `orderBy: updatedAt`) with an `updatedAt`
  floor, and emits mirrors. Large workspaces drain **off the causal chain**
  via the paged-checkpoint `more`/`resume` contract. Mirrors are minted with
  put-if-absent and updated with **patch** (never re-put whole), so an idle
  re-sync is no-op-suppressed end to end.
  - **Incremental floor = run-start watermark.** When an account reaches the
    head of the queue the clock is captured into the page cursor, and a
    _completed_ drain stamps that value — never the completion clock — into
    `lastSyncedAt`; the next run reads from it minus a 15-minute overlap.
    Issues updated _while_ a multi-page drain is running therefore land on
    the next run instead of falling behind the floor forever, and the
    overlap absorbs substrate↔Linear clock skew (converging patches make the
    re-read free).
  - **`backfillDepth`** maps as the fleet convention: `none` = from connect
    time, forward only (the first window is [connect, now]); `last30d` /
    `last90d` / `last1y` = now minus the window; `all` = no floor at all.
  - **Per-account isolation.** A provider failure (401 on a grant revoked at
    Linear, persistent 5xx/429, dead network) stamps _that_ account's
    `syncStatus: "erroring: <reason>"` and the queue drains on behind it —
    one poisoned account never parks the chain; the schedule retries it next
    tick and an ok run overwrites the stamp. `lastSyncedAt` is not advanced
    on a failed run. (`tokenStatus` stays the OAuth facility's hand —
    `writer: oauth` — so auth failures surface on `syncStatus` too.)
  - **Origin pinning.** The bearer token leaves the body only over HTTPS to
    `api.linear.app`, or to loopback (the test seam); anything else refuses
    before a byte is sent.
## Install, configure, connect

```sh
substratectl apply -f bundle.yaml -f triggers.yaml
```

Then, once (the config is per-repository — client id + secret — so it is not
shipped here):

1. **Create the Linear OAuth app.** Linear → workspace settings → **API** →
   **OAuth applications** → new application. Give it the callback URL below,
   and note the client id and secret. The only scope this bundle ever
   requests is `read`.

2. **Register the callback URL** on that application. The host surfaces one
   redirect URI — the OAuth facility's callback:

   ```
   https://<your-substrate-host>/api/v1/oauth/callback
   ```

   (The value is the host's `SUBSTRATE_OAUTH_CALLBACK_URL`.)

3. **Create the config.** One `config` carrying the application's `clientId`
   and `clientSecret`. The authorize/token/revoke endpoints are already on the
   bundle manifest (`https://linear.app/oauth/authorize`,
   `https://api.linear.app/oauth/token`,
   `https://api.linear.app/oauth/revoke`).

4. **Add an account.** Create an `account` with `enabledIssues: true` and a
   `syncFrequency`. It starts `tokenStatus: pending`.

5. **Connect.** Run the host OAuth flow against the account
   (`substratectl bundle connect`, or `POST …/oauth/start
   {"record": "<accountId>"}`) → consent at Linear → the callback stores the
   grant, sets `tokenStatus: connected`, and the on-connect trigger fires the
   first backfill.

## Deliberately out of scope (for now)

- **No writeback**: nothing here touches Linear (outbound outbox, issue
  009). The v4 integration's `completeIssue` mutation has no counterpart here
  yet.
- **No task projection**: the mirrors are the whole output. A repository that
  wants issues as tasks declares a mapping from `issue` onto its own task
  kind, or writes a function of its own; version 11 deleted the projection
  this closure used to ship, because it wrote a kind the package does not own.
- **No projects** — v4 mapped Linear projects onto its own projects but never
  synced them as records; no `enabledProjects` toggle is declared until a
  slice wants them.
- **No delete sweep** — an issue deleted or unassigned upstream keeps its
  mirror until a tombstone slice adds reconciliation; the incremental floor
  means a full re-read is rare anyway.
- **No comments/attachments/cycles** — issue-level only.

## Files

- `bundle.yaml` — the **schema closure**: package + bundle + config type +
  account type + three mirror types + the sync function. No mapping and no
  projection: every kind it writes is its own.
- `triggers.yaml`, the **delivery wiring**: the on-connect backfill and the
  hourly re-sync schedule, as ordinary data records.

## Tested

`engine/linear_bundle_db_test.go` installs this closure from these very files:
admission through the schema loader (traits, subject slots, the
install closure), the whole-closure install into a live repository (skips when uv
cannot provision the PEP 723 body), and a FAKE-provider end-to-end sync —
loopback OAuth + a loopback GraphQL stub, proving the mirrors land, the
paging drains, and an idle re-sync writes nothing. The subject slots stay
empty until a mapping fills them, and with one declared the assignee resolves
onto the repository's own person. The team reference stays current across a
team move (exactly one reference, the new team) and a team removal (the stale
reference is cleared). Real Linear API calls never run in tests.
