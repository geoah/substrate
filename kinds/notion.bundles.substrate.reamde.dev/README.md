# The Notion bundle — a sync-only workspace mirror

Connects a Notion workspace with an **internal-integration token** and mirrors
the pages and data sources shared with that integration into `page` /
`database` records. **Sync-only, by contract**: the bundle never creates,
edits, or archives anything in Notion — the token is spent exclusively on reads
(`search`, `blocks/{id}/children`). See the writeback note at the end.

Same primitive set as the google bundle — **bundle · kind · trait ·
function · trigger** — minus the OAuth facility, deliberately (below), and
minus a recordmapping: a Notion page mirrors as a document, not as a person.

## API version: 2025-09-03, the data-source model

The sync pins `Notion-Version: 2025-09-03`. This matters: under the older
`2022-06-28` pin, a database that gains a **second data source disappears from
search results entirely** (Notion's documented old-version behavior) — the
mirror would silently lose whole databases. Under `2025-09-03`, search returns
one `data_source` object per source, each naming its **containing database**:

- **one `database` row per data source**, keyed by the data-source id
  (`dataSourceId`), with the parent database modeled explicitly as `databaseId`
  — a multi-source database yields one mirror per source, all sharing the same
  `databaseId`;
- pages inside a database report a `data_source_id` parent, so their `parent`
  reference points at the data-source mirror.

## One account per repository

The bundle holds **one** credential (the `connector` input's token), and a
Notion internal-integration token addresses one workspace — so a second
`account` row could only mirror the same workspace twice under different ids.
The sync treats the **lexicographically-first** account row as the connection;
every other row is stamped `syncStatus: "ignored: duplicate account (this
provider is one-account-per-repository until issue 011)"` and never syncs.
Per-account credentials are issue-011 territory.

## Why a pasted token and not OAuth

Notion's OAuth token exchange authenticates the client with **HTTP Basic**
(`Authorization: Basic base64(client_id:client_secret)`). The host's OAuth
facility declares its auth style once, for every bundle —
`AuthStyle: oauth2.AuthStyleInParams` in `internal/oauthflow/oauthflow.go`
(client credentials in the POST body), with no per-bundle auth-style knob on
the bundle's `oauth2:` manifest block (`schema.BundleOAuth2`). The host
therefore **cannot run Notion's token exchange today**, and this bundle does
not pretend otherwise: it ships Notion's _other_ first-class auth model, the
internal integration — the right shape for a personal substrate anyway (one
workspace, no consent redirect, no refresh loop; internal-integration tokens
do not expire). When the facility grows a declared token-endpoint auth style,
an oauth2 revision of this bundle is mechanical against the google template
(`authorize https://api.notion.com/v1/oauth/authorize`,
`token https://api.notion.com/v1/oauth/token`).

**Where the token lives — the config, not the account.** The engine seals every
secret-typed property of an `accountconfig`-trait type at rest
(`engine/write.go` `sealsSecretsAtRest`) and the runner's config resolution
injects account properties _as stored_ — a token pasted onto the `account`
would reach the sync body as sealed ciphertext. An injected input's own
secrets are the host's supported pattern (`engine/invocationconfig.go`: a
connector's OWN input secrets ARE injected; the web bundle's `firecrawlKey`
is the precedent): injected in plaintext inside the runner boundary, held
there by the invocation scrubber, never read back over the API.

**Where the token may travel — origin pinning.** The body refuses to send the
token anywhere except HTTPS `api.notion.com` or a **loopback** host
(127.0.0.1 / localhost / ::1, any scheme — the test seam). An `apiBase`
pointing anywhere else logs and **parks the sync without sending**: an
owner-editable URL must never become a token-exfiltration channel. (The
declared `network` capability is the future host-side enforcement of the same
rule.)

## What it does

```
account (pages/databases on, never synced)
      │  on-connect trigger (fires once — guarded on "no lastSyncedAt yet")
      ▼
workspacesync (python / uv / Notion search API, three resumable phases)
      │  search — 100 objects per invocation, DESCENDING by last_edited_time;
      │           the first below-cutoff result ends the walk; unchanged
      │           last_edited_time → skip (no block fetch, no effect);
      │           the account stamps when the walk concludes
      │  blocks — a bounded batch of block-tree walks per invocation, each
      │           patching its page's `content`
      │  links  — deferred parent references; unresolved targets stay stored as
      │           pendingParent on the child, repaired by a later sync
      ▼
page mirrors (title, url, normalized content, raw, parent reference)
database mirrors (one per data source: title, url, databaseId, raw)

hourly schedule ──▶ workspacesync ──▶ the primary account, when due by its
                                      syncFrequency; duplicate rows stamped
```

- **`config`**, the `connector` input's kind: the integration token,
  secret-typed `integrationToken` (writer: owner), plus `apiBase`, a
  loopback-only test override of `https://api.notion.com` (anything else
  refuses to sync). The bundle's `connector` input resolves one record of
  this kind into the sync body (the sole record, the one named `default`,
  or a bound one).
- **`account`** (accountconfig): the connected workspace — `displayName`, the
  `enabledPages`/`enabledDatabases` toggles, labeled required `syncFrequency`
  (default daily) and `backfillDepth` (default everything) enums, and the
  connector-written `lastSyncedAt`/`syncStatus`/`backfillAnchor`. The
  trait-required `tokenRef`/`tokenStatus`/`grantedScopes` are declared but
  **dormant** — no OAuth flow ever writes them under the token model.
- **`page`**: title (the built-in), `url`, `lastEditedAt`, `archived`,
  `content` (the block tree normalized to markdown-ish text: headings, lists,
  to-dos, quotes, code fences, bookmarks — capped at 500 blocks, depth 2),
  `pendingParent` (a parent reference awaiting its mirror), `raw` (the page
  object verbatim), and an optional `parent` reference to the mirrored parent page
  or data source.
- **`database`**: one row per **data source** — title (the built-in),
  `dataSourceId` (the key), `databaseId` (the containing database, modeled
  explicitly), `url`, `raw`.
- **`workspacesync`** (python / PEP 723, stdlib HTTP): pages `POST /v1/search`
  (100 results per invocation, `more`/`resume`), ids by
  `host.ids.external("notion", account, id)`, mirrors what the toggles ask
  for, and stamps the account when the search walk concludes.

**The sync-status vocabulary** (all three are really written):
`ok` — the walk completed; `erroring: <reason>` — a provider failure degraded
this run (the account stays due, so the next scheduled fire retries);
`ignored: duplicate account …` — a non-primary account row.

**Backfill depth** (the fleet-wide mapping): `none` mirrors only what is
edited **after the account connects** — the first sync stamps
`backfillAnchor` at its run-start and every later run measures from that
fixed floor (never a per-run "cutoff = now", which would skip everything
forever); `last30d`/`last90d`/`last1y` reach back from each run;
`all` mirrors everything. Because the search walk descends by
`last_edited_time` and **stops at the first below-cutoff result**, a narrow
window never scans the workspace's whole history — but note the flip side: a
mirrored object that falls behind a moving window is no longer visited, so
its stored `pendingParent` (if any) stops being repaired.

**The delta short-circuit** (the old linkprovider's `last_edited_time`
optimization, kept): Notion carries no ETag — `last_edited_time` is the
authoritative change marker. Before staging anything the body reads the
mirror back (`host.records.get`) and compares the fetched object's
`last_edited_time` (and `archived` flag) against the mirror's **verbatim
`raw` copy** — the `lastEditedAt` datetime property is engine-normalized on
write and would false-mismatch, `json` round-trips byte-true. Unchanged means
skip: no block fetch, no staged effect, no changelog noise. Notion's search
has no incremental sync token, so every run walks the search feed — the
short-circuit is what makes steady state cheap. Two repairs run
**independently of the skip**: a stored `pendingParent` whose target has
since been shared gets its reference, and a mirror whose blocks phase never
completed (content absent) re-queues its content walk.

**Content lands in two steps.** The search phase puts the page row (title,
raw, parent) and the blocks phase patches `content` afterwards — so a changed
page's content is briefly absent mid-drain. If a drain dies between the two,
the next sync detects the content-less mirror and re-queues the walk.

**The direct-call path** (`{account}` input): the single-shot callers discard
paged checkpoints, so a direct call **drains the whole account inside its one
invocation** — cadence ignored. That is bounded by the function's 60s
timeout, the read budget (300 calls) and the frame cap, which makes it right
for steady-state re-syncs; a large first backfill should ride the triggers,
which page off the causal chain.

## Install, configure, connect

```sh
substratectl apply -f bundle.yaml -f triggers.yaml
```

1. **Create the integration.** In Notion: Settings → Connections → Develop or
   manage integrations → New internal integration. Grant it **read content**
   capabilities only (this bundle needs nothing else). Copy the token.
2. **Share content with it.** In Notion, share each top-level page or database
   (or a whole teamspace) with the integration — the search API returns only
   what has been shared.
3. **Create the config.** One `config` with the pasted `integrationToken`.
   Leave `apiBase` empty.
4. **Add the account.** Create ONE `account` with a `displayName`, set
   `enabledPages: true` (and `enabledDatabases` if wanted), pick a
   `syncFrequency`. The on-connect trigger fires the first sync immediately —
   there is no OAuth consent step. (If the account is created before the
   config, the first run logs "no integrationToken" and the hourly schedule
   picks the account up once the token lands. A second account row is stamped
   as a duplicate, never synced.)

## Known limits

- **Content truncates** at 500 blocks per page, nesting depth 2 — a
  `[content truncated]` marker closes anything larger.
- **Search is the only feed**: Notion has no incremental sync token, so every
  run re-walks the (descending) search feed until it drains or passes the
  backfill cutoff.
- **A parent that is never shared** with the integration stays a stored
  `pendingParent` — the reference exists only between mirrored rows.
- **`database_id`-typed page parents** (rare under 2025-09-03, which reports
  data-source parents for database rows) resolve only if a matching mirror
  exists; otherwise they stay pending.
- **One account per repository** (above); per-account credentials are issue 011.

## No writeback — and the deferred question

This bundle is **read-only against Notion** by design, not as a stopgap
someone should "finish":

- Provider writeback in general (create/edit upstream) is gated on the
  outbound claim/confirm facility (issue 009) — a direct write from a function
  body has no answer for the send-then-crash window.
- **NOTE — the named future case:** the function-write-model design names
  **Notion create-page-then-read-computed-state** as the one plausible case
  for reading engine-_computed_ state within a delivery — the open question
  that would justify a narrow **workflow primitive**, never mid-body function
  writes. That question is deliberately left open; if a Notion-writing flow
  ever lands it arrives as that primitive, on top of this read-only mirror.
  Writeback is deferred regardless of how it resolves.

## Files

- `bundle.yaml` — the **schema closure**: authority + bundle + config type +
  account type + the two mirror types + the sync function.
- `triggers.yaml` — the **delivery wiring**: the on-connect backfill trigger
  and the hourly re-sync schedule, as ordinary data records.

## Tested

`engine/notion_bundle_db_test.go` installs this closure from these very files:
schema admission through the loader (no DB, no uv), then — where uv is
available — the full install into a live repository plus staged end-to-end syncs
against a **fake Notion API** (`httptest`, reached through the config's
loopback `apiBase` seam, holding the body to `Notion-Version: 2025-09-03`):
a mid-drain account disable ends the drain with fresh state (no stale search
cursor, nothing parked); the full first sync lands data-source and page
mirrors with normalized content, inline and deferred parent references, a stored
`pendingParent`, and the stamp; a direct-call re-sync proves the delta
short-circuit (no block fetches, versions untouched); a later sync repairs
the `pendingParent` once the parent is shared; a below-cutoff result stops
the descending walk before the next search page; a second account row is
stamped as a duplicate and never reaches the provider; and a provider failure
stamps `erroring: …`, recovering to `ok` on the next healthy run.
