# The Linear bundle — assigned issues, mirrored and projected as tasks

Connects a Linear workspace over the host OAuth facility and syncs the issues
assigned to the connected login into `issue` mirrors, mapped to persons and
projected onto `task.tasks.substrate.reamde.dev` rows the owner actually works.

**SYNC-ONLY.** Nothing in this bundle writes back to Linear: completing a task
in the substrate does **not** move the Linear issue. Provider writeback is an
outward write to someone else's system and waits on the outbound-writes outbox
(issue 009); until that lands, this bundle's only writes are substrate writes.

It composes from the same primitive set as the Google bundle —
**bundle · kind · trait · function · recordmapping · trigger** — and adds
the one thing Google didn't need: a **joint-ownership projection**.

## What it does

```
account (connected, issues on)
      │  on-connect trigger (fires once — guarded on "no lastSyncedAt yet")
      ▼
issuessync (python / uv / Linear GraphQL)
      │  pages viewer.assignedIssues, off the causal chain
      ▼
user ──mapping──▶ person      (match on email, or mint a shell)
team ◀──team edge── issue ──mapping──▶ person (assignee)
      │                        │
      │                        ▼  linear-task-projection trigger
      │                  taskprojection ──▶ task  (read, diff, patch if_version)
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
  the raw node, an `assignee` subject edge at person and a `team` edge at its
  team mirror. The team edge is kept **current**: `team` is a single edge, so
  the sync's re-link replaces a moved issue's previous team (the engine's
  one-target-per-rel law), and an issue that lost its team upstream gets its
  stale edge read back and unlinked.
- **`userperson` / `issueperson`** (recordmappings): match on the
  login/assignee email against people's emails — exactly one live match links,
  zero-or-many mints a person shell. The user mapping copies names and unions
  the address onto the person; the issue mapping only resolves the edge (an
  issue describes work, not the human). When the workspace **hides the viewer's
  email** (admin-restricted visibility), the issues carry no probe value — and
  an empty probe would mint one person shell _per issue_ — so the sync instead
  points each issue's `assignee` edge at the viewer's own `user` mirror and the
  engine's one-hop subject resolution lands it on the same person that record
  resolves to: one shell per login, never one per issue (logged once per run).
- **`issuessync`** (python / PEP 723 / requests): reads the account's
  host-resolved access token off the injected config, pages
  `viewer.assignedIssues` (50/page, `orderBy: updatedAt`) with an `updatedAt`
  floor, and emits mirrors. Large workspaces drain **off the causal chain**
  via the paged-checkpoint `more`/`resume` contract. Mirrors are minted with
  put-if-absent and updated with **patch** (never re-put whole), so an idle
  re-sync is no-op-suppressed end to end and the projection baseline survives.
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
- **`taskprojection`**: the joint-ownership policy, ported from the v4 Linear
  integration's read-diff-patch contract. A synced task is jointly owned —
  Linear owns the issue's identity (`title`, `url`); the owner owns how they
  are dealing with it (`status`, once they have moved it). So the projection:
  1. **reads** the task (`host.records.get`) — committed state, version
     included;
  2. **diffs** only the Linear-owned keys, patching what actually moved;
  3. moves **`status` only when the upstream state departed from the adopted
     baseline** (`projectedState` on the mirror — v4's
     `_meta.linear_state_type`), **compared folded** onto the task machine's
     open/done: "Linear still says started" is not news, and neither is an
     open-family drag (backlog → started, triage → unstarted) — only a real
     open↔done transition moves the task, so upstream churn can never undo a
     task the owner ticked off. A row with no baseline counts as unchanged —
     except where adopting cannot undo anything (a local task not yet done),
     so an issue completed in Linear still closes here;
  4. guards every task patch with `if_version=host.version(task)` — the
     SDK's optimistic precondition — so a concurrent owner edit conflicts and
     the redelivery re-reads instead of a stale diff clobbering it.

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
   https://<your-substrate-host>/api/v1/core.substrate.reamde.dev/oauth/callback
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
   (`substratectl bundle connect`, or `POST …/core.substrate.reamde.dev/oauth/start
   {"record": "<accountId>"}`) → consent at Linear → the callback stores the
   grant, sets `tokenStatus: connected`, and the on-connect trigger fires the
   first backfill.

## Upgrading to version 7

Version 7 renames each mirror's provider key: `user.providerId` is `userId`,
`team.providerId` is `teamId`, `issue.providerId` is `issueId`. A dropped
property is a narrowing, so a repository whose live mirror rows still carry
`providerId` refuses the whole closure with a guard error naming the count.

Stop the bundle first. The hourly schedule and the projection trigger keep
running otherwise, and one version-6 sync between the clearing and the apply
writes `providerId` back onto the rows it touches:

```sh
substratectl bundle disable linear.bundles.substrate.reamde.dev/linear
```

Then take the old key off every live `user`, `team` and `issue`, either by
deleting the mirrors (the guard counts live rows only) or by nulling the
property on each:

```sh
substratectl patch issues <id> --authority linear.bundles.substrate.reamde.dev \
  -p '{"properties":{"providerId":null}}'
```

Apply the closure and start the bundle again:

```sh
substratectl apply -f bundle.yaml -f triggers.yaml
substratectl bundle enable linear.bundles.substrate.reamde.dev/linear
```

A cleared row stays without a provider key until Linear touches its issue
again, because the sync is incremental off `lastSyncedAt` and only re-fetches
what changed. Null `lastSyncedAt` on the account to make the next run re-read
its whole `backfillDepth` window, which is also what rebuilds mirrors deleted
above.

## Deliberately out of scope (for now)

- **No writeback** — completing/reopening tasks never touches Linear
  (outbound outbox, issue 009). The v4 integration's `completeIssue` mutation
  has no counterpart here yet.
- **No projects** — v4 mapped Linear projects onto its own projects but never
  synced them as records; no `enabledProjects` toggle is declared until a
  slice wants them.
- **No delete sweep** — an issue deleted or unassigned upstream keeps its
  mirror (and its task) until a tombstone slice adds reconciliation; the
  incremental floor means a full re-read is rare anyway.
- **No comments/attachments/cycles** — issue-level only.

## Files

- `bundle.yaml` — the **schema closure**: authority + bundle + config type +
  account type + three mirror types + the sync and projection functions + the
  two →person mappings.
- `triggers.yaml` — the **delivery wiring**: the on-connect backfill, the
  hourly re-sync schedule, and the per-issue projection trigger, as ordinary
  data records.

## Tested

`engine/linear_bundle_db_test.go` installs this closure from these very files:
admission through the schema loader (traits, subject edges, both mappings, the
install closure), the whole-closure install into a live repository (skips when uv
cannot provision the PEP 723 body), and a FAKE-provider end-to-end sync —
loopback OAuth + a loopback GraphQL stub — proving the mirrors and the person
land, the paging drains, and the joint-ownership policy holds: a task the
owner marked done survives an idle re-sync untouched, an OPEN-FAMILY upstream
move (started → backlog → started) leaves it untouched too — the folded-state
regression — and an issue completed upstream still closes its task. The team
edge stays current across a team move (exactly one edge, the new team) and a
team removal (the stale edge unlinks). Real Linear API calls never run in
tests.
