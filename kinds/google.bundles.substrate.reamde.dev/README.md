# The Google bundle — the substrate's first real connector

This is the first genuine **integration-as-bundle**: it connects a Google
account over the host OAuth facility and syncs three streams into the graph.

| Stream   | Toggle            | Scope               | Function       | Mirrors             | Core rows                     |
| -------- | ----------------- | ------------------- | -------------- | ------------------- | ----------------------------- |
| Contacts | `enabledContacts` | `contacts.readonly` | `contactssync` | `contact`           | `person` (via mapping)        |
| Gmail    | `enabledGmail`    | `gmail.readonly`    | `gmailsync`    | `thread`, `message` | `emailthread`, `emailmessage` |
| Calendar | `enabledCalendar` | `calendar.readonly` | `calendarsync` | `calendar`, `event` | `calendar`, `calendarevent`   |

Each stream is a separate toggle, a separate scope, a separate function, a
separate pair of triggers and a separate slice of connector state on the
account, so one stream erroring never stalls another.

It is a real, installable closure (not a fixture), and it proves the two
things the URL-harvester conformance example deliberately left out:

1. The **host OAuth facility** — the `oauth2` trait on the config type and
   `accountconfig` on the account. The bundle _declares_ auth; the host runs
   the flow.
2. The **connector runtime** — `contactssync` declares its dependencies inline
   with **PEP 723** (`google-api-python-client`, `google-auth`) and runs
   through **uv**; `gmailsync` and `calendarsync` are stdlib-only and run on
   the shared `python3` fast path with no provisioning at all.

Everything else is the same primitive set the harvester uses:
**bundle · kind · trait · function · recordmapping · trigger**.

## How the records reach the core vocabulary

Mirror types hold Google's own shape. The sync functions **also emit the core
row for the same logical object directly**, under the **same derived id**, with
its required edges supplied explicitly. Recordmappings resolve **people** and
nothing else.

```
messages.get ──▶ message (mirror)  ──same id──▶ emailmessage (core)
                                                  │ thread   ─▶ emailthread
                                                  │ sender   ─┐
                                                  └ recipients┤
                                                              ▼
                       every address ──▶ emailaddress ──mapping──▶ person
                                                              ▲
events.list ───▶ event (mirror)   ──same id──▶ calendarevent  │
                                                  │ calendar ─▶ calendar (core)
                                                  │ organizer ┘
                                                  └ attendees
```

**Why not map onto `emailmessage` / `calendarevent`?** Because the engine
cannot, today, and saying so is part of the contract:

- A type any mapping points at gets **server-assigned ids**
  (`checkCreateID`), so no connector could ever name one.
- A mapping's only creator is its **shell mint**, a bare row with no edges,
  and every candidate target declares a **required** edge (`emailmessage.thread`,
  `emailthread.account`, `calendarevent.calendar`), so the mint would fail
  validation and take the source record's write down with it.
- A mapping carries **properties only, never edges**, so even a successful
  mint would be structurally empty.

`person.people.substrate.geoah.me` is mappable precisely because it declares no
required edges, which is why every mapping in the fleet targets it. The
general capability is filed as ticket 041, "structural recordmappings".

The `emailaddress` record is the bridge: the sync writes one per address it
sees, its mapping matches or mints the person, and the core rows reference the
**record** in their `sender`/`recipients`/`organizer`/`attendees` edges. The
engine's one-hop rule lands the stored edge on the person. An address-book
contact and a mail sender therefore converge on **one** person.

## The pieces

- **`config`** (oauth2): the Google OAuth client — `clientId` and
  secret-typed `clientSecret`. The endpoints and the feature-to-scope mapping
  are **trusted manifest metadata** on the bundle document, never properties
  on this mutable row. The bundle's `client` input resolves one record of
  this kind for the host OAuth facility (the sole record, the one named
  `default`, or a bound one); while it is unresolved or incomplete the bundle
  status carries a setup step and the OAuth flow refuses.
- **`account`** (accountconfig): one connected account — `email`, the three
  toggles, `syncFrequency`, `backfillDepth`, the host-written
  `tokenRef`/`tokenStatus`/`grantedScopes`, and connector state in **two
  tiers**:
  - **The rollup**, account-level and unprefixed: `lastSyncedAt` and
    `syncStatus`. Every accountconfig type in the fleet declares this pair and
    the console reads it _generically_ off any connection ("is this healthy,
    when did it last do anything"), so it stays shared. Whichever stream
    finishes stamps it, with `ok`, `ok (N skipped)` or `erroring: <reason>`.
    The rollup holds **one** string, so a stream only ever **raises** it: it
    is never cleared over another stream's live failure, and a stream that
    already reads erroring is left alone (re-stamping would let the account's
    own update re-fire the on-connect triggers into a loop).
  - **One prefixed slice per stream**: a cadence anchor, a cursor and that
    stream's **own** outcome, so a gmail failure can never swallow a calendar
    one — with a single shared string it did, silently and completely.
    - contacts: `contactsSyncToken`, `contactsLastSyncedAt`, `contactsSyncStatus`
    - gmail: `gmailHistoryId`, `gmailBackfillAnchorAt`, `gmailBackfillResume`,
      `gmailLastSyncedAt`, `gmailSyncStatus`
    - calendar: `calendarBackfillAnchorAt`, `calendarLastSyncedAt`,
      `calendarSyncStatus`

  Each stream's `syncFrequency` check and each stream's on-connect guard read
  **their own anchor**, never the rollup: a gmail run must not suppress a due
  contacts run, and an account whose contacts already synced must still fire
  its first gmail backfill. **Each calendar's own sync token lives on its
  `calendar` mirror row**, not here, so one calendar's advance can never skip
  another's tail.
  - The account implements **accountconfig only, not oauth2** — the as-built
    facility binds the client creds on the _config_ type and the tokens on the
    account.
- **`contact`** (source type): what Google holds about one contact, in
  Google's own shape. Its id is
  `ids.external("google-contacts", account, resourceName)`. Its `person`
  subject edge is left empty on write; the mapping resolves it.
- **`emailaddress`** (source type): one address, keyed by
  `ids.external("google-address", account, address)`. Written by both
  `gmailsync` and `calendarsync`, mapped onto `person`.
- **`thread` / `message`** (gmail mirrors): ids
  `ids.external("gmail-thread"|"gmail-message", account, providerId)`.
- **`calendar` / `event`** (calendar mirrors): ids
  `ids.external("gcal-calendar", account, calendarId)` and, because a Google
  event id is unique **per calendar** rather than globally,
  `ids.external("gcal-event", <the calendar's derived id>, eventId)` — the
  helper NESTED, never a string composition, which would not be injective.
- **`contactperson`** and **`emailaddressperson`** (recordmappings): both
  probe an email against people's emails, link the single live match or mint a
  shell, and contribute machine-managed properties that yield to any owner
  edit. `emailaddress.displayName` maps onto the person's `name` only, never
  `displayName`: a header's "Alice Example" is weaker than an address book's
  nickname, and mail volume would clobber it under latest-write-wins.

## `backfillDepth`, identically for gmail and calendar

The first run of each stream stamps a **backfill anchor** (its own run-start
instant). The window floor is derived from that stored anchor, never from a
clock read per run, so the reach cannot creep forward every night.

| Value                            | Floor                                                         |
| -------------------------------- | ------------------------------------------------------------- |
| `none`                           | the anchor itself: from connect forward, zero reach back      |
| `last30d` / `last90d` / `last1y` | `anchor − Δ`, computed from the stored anchor                 |
| `all`                            | the epoch (gmail: no `after:` clause; calendar: no `timeMin`) |

Contacts ignores `backfillDepth` entirely: the People sync token gives it
full-plus-incremental with no window.

Calendar always caps the forward reach at **now + 365 days**, or a recurring
event exploded to 2099 would page forever.

## What each sync does

### `contactssync` (python / PEP 723 / uv)

Pages `people/me/connections` with a field mask, emits one `contact` per
connection, and on the final page stores the People **sync token** on the
account. An expired sync token (a structured `EXPIRED_SYNC_TOKEN`, never a
bare 400/410) falls back to a full re-read followed by a mark-and-sweep.

### `gmailsync` (python / stdlib)

One provider page, or one hydrate batch of 25 `messages.get`, per invocation.

- **The watermark is the run-start `historyId`**, read from `users.getProfile`
  before a single message is, and stamped only when the account's drain
  completes. A message that changes mid-drain therefore lands inside the next
  run's window instead of falling through the gap a completion clock opens.
- **Expiry is HTTP 404**, and only 404: a too-old `startHistoryId` drops to a
  windowed full re-read under a fresh generation. A 400 is a request bug and a
  403 a scope problem; neither may silently full-re-read.
- **The sweep is window-scoped, and it waits for the walk.** Gmail's full read
  is a _window_, not the whole mailbox, so the generation compare runs only
  over rows whose `sentAt`/`lastMessageAt` is inside it. An unscoped sweep
  would delete the archive. It also runs **only on the run that actually
  drains the walk**: a truncated walk has stamped nothing behind its page
  token, so sweeping there would delete every in-window row it never reached —
  under `backfillDepth: all` the window has no floor and that is the entire
  archive, mirror and core, with the core `emailthread` deletes cascading
  their messages. The pending sweep therefore rides **in the resume**.
- **A cold start is bounded twice over**: at most 20 listing pages per run,
  and at most ~12 **invocations**. The invocation bound is the one that binds:
  the engine's paged drain is capped by cumulative wall clock from the chain's
  first committed page and refuses a middle page _before_ committing it, so a
  chain that keeps asking for one more page is cut off with nothing persisted
  and the next run restarts from page one — forever. The unfinished walk is
  written to `gmailBackfillResume` (page token, floor, generation, the pending
  sweep, and the watermark that must NOT advance while it is pending) and
  logged, then resumed before anything else on the next run. Truncation is
  never silent, and every run moves the walk forward.
- **A stale resume token restarts its window.** Gmail list page tokens are
  short-lived; one held across a run boundary answers 400. That is caught
  where it happens and the window restarts from page one under the same floor
  and the same generation, rather than resuming a dead token forever.
- **Threads are derived from the message stream** — `threads.get` is never
  called, so a thread carries no `raw` and no message count. `subject`,
  `preview` and `lastMessageAt` are written **newest-wins** (a backfill walks
  newest-first, so a blind put from a later page would overwrite newer data),
  and `participants` are unioned rather than replaced. A history delta that
  retracts a thread's **last** message retracts the thread too, in both
  halves: nothing revisits a thread whose messages are all gone.
- **Every address is checked against the RFC 5322 dot-atom production** before
  it reaches an `email`-kind property. The engine validates that kind with
  Go's `net/mail.ParseAddress` at effect **apply**, outside this body's
  try/except, so a looser pattern does not skip one header — it rolls the
  page's transaction back and parks the drain deterministically on every
  retry. Quoted local parts are refused rather than risked.
- **Person edges are capped at 200 per message and per thread.** Every edge
  target is locked in the page's one transaction, and a 1,000-recipient list
  across a 25-message hydrate batch is 25,000 of them. The mirror keeps the
  whole `to`/`cc`/`bcc` header and the whole participant union, so nothing is
  lost; the truncation is logged.

### `calendarsync` (python / stdlib)

One `calendarList` page, then one `events.list` page per calendar, per
invocation.

- **One sync token per calendar**, on that calendar's own mirror row.
  `nextSyncToken` arrives **only on the final page**, and the token commits
  nowhere else: a page that fails leaves the previous token standing and the
  whole delta re-reads. A sync token is all or nothing.
- An incremental call **repeats the initial query parameters exactly**
  (`singleEvents=true`, `showDeleted=true`, `maxResults=250`) and carries no
  time bounds, which Google rejects beside a token.
- **Expiry is HTTP 410 GONE**, and only 410: drop the token, re-read that ONE
  calendar's window under a fresh generation, then sweep that window. The
  sweep is bounded at **both** ends — floor _and_ horizon. An incremental
  delta carries no time bounds at all, so it legitimately stores an event
  eighteen months out; the re-read is capped at `now + 365d` and can never
  re-stamp one, so without the upper bound the sweep would delete it.
- A delta always carries `status: cancelled` entries. Those are **deletes** of
  both halves: core's `calendarevent` is documented as retracted rather than
  tombstoned, so a reader never filters cancellations. They are counted as
  `cancelled`, **not** as `skipped`: a cancellation is the provider's ordinary
  housekeeping, and counting it as a failure made every healthy calendar sync
  read `ok (N skipped)` on the console forever.
- **A calendar that went away is retracted.** The `calendarList` walk asks for
  `showDeleted=true`, and a deleted entry takes its event mirrors, their core
  rows and both halves of the calendar with it, a bounded page at a time — and
  is never enqueued for event sync. Nothing else could: a calendar that is no
  longer walked never 410s, and the sweep is per-calendar.
- **Every calendar except an `accessRole: freeBusyReader` share** is synced (a
  free/busy share carries no content). `selected` is recorded so a later slice
  can add per-calendar opt-out without a re-sync.

Both stdlib bodies **origin-pin** every credentialed call to their provider
host over https, or loopback (the test seam), **refuse to follow redirects**
(which `urllib` would otherwise do with the bearer attached) and install an
**empty `ProxyHandler`**, because the default opener honours `http_proxy` /
`https_proxy` from the runner's environment and would route the token through
whatever those name. The engine's `network` capability is declared but not
enforced, so these refusals are the defense.

## What this slice deliberately does not do

Stated plainly, because a README that overclaims is worse than none:

- **No attachment bytes.** The runner has no in-invocation atomic blob-write
  primitive (ticket 008), so `message.attachments` keeps metadata and the
  `attachmentId` a later slice can fetch by. `raw` has every base64 `data`
  field stripped.
- **No `calendareventseries`.** `singleEvents=true` means every row is a
  concrete occurrence, which is what core's `calendarevent` is documented to
  be. `recurringEventId` and `recurrence` ride the mirror so the series slice
  is a pure follow-up; core's `calendarevent.series` edge stays empty.
- **No label type.** Core deliberately chose plain `labelIds` strings over an
  edge into a connector-owned label type, so there is no `label` mirror.
- **Attendee `responseStatus`, `optional` and `resource` live on the mirror
  only.** An edge carries no properties, so the core row's `attendees` edge
  cannot hold them.
- **No writeback.** Every stream is read-only.
- **Deletes reconcile only where the provider says so.** Gmail's history
  tombstones and the sweep cover mail; calendar's `cancelled` entries, its
  deleted-calendar retraction and the sweep cover events; a stream that never
  runs a full re-read never sweeps.
- **The calendar stream has no resume state.** Gmail's backfill is bounded by
  invocations because it can persist a page token; a calendar walk cannot
  (there is no `calendarBackfillResume`), so a repository with very many very
  large calendars can still outrun the engine's drain deadline. Filed rather
  than papered over: it needs an account property, not a constant.

## Quotas and scope caveats

- `gmail.readonly` is one of Google's **restricted** scopes. A published
  client needs CASA verification, and an **External + Testing** OAuth client
  has its refresh tokens **revoked after 7 days** — expect to reconnect.
- `messages.get` costs **20 quota units** against a **6,000 units/min/user**
  ceiling, roughly 300 messages a minute. The 25-message hydrate batch and the
  20-page backfill cap are sized against that.
- The requested scope union is derived per `StartOAuth` from the account's
  enabled toggles. **Turning gmail or calendar on for an already-connected
  account requires a reconnect**; until the new grant lands the sync stamps
  `erroring: forbidden (…)`.

## Install, configure, connect

```sh
substratectl apply -f bundle.yaml -f triggers.yaml
```

Then, once (the config is per-repository — client id + secret — so it is not
shipped here):

1. **Create the config.** One `config` carrying your Google OAuth client's
   `clientId` and `clientSecret`. The endpoints and scopes are in the bundle
   manifest; there is nothing else to type.

2. **Register the callback URL** in your Google client. The host surfaces one
   redirect URI — the OAuth facility's callback:

   ```
   https://<your-substrate-host>/api/v1/core.substrate.reamde.dev/oauth/callback
   ```

   (The value is the host's `SUBSTRATE_OAUTH_CALLBACK_URL`.) This is the URI
   Google redirects the browser back to after consent.

3. **Add an account.** Create an `account`, set the toggles you want, pick a
   `syncFrequency` (`hourly`/`daily`) and a `backfillDepth`. It starts
   `tokenStatus: pending`.

4. **Connect.** Run the host OAuth flow against the account
   (`substratectl bundle connect`, or `POST …/core.substrate.reamde.dev/oauth/start
   {"record": "<accountId>"}`) → consent at Google → the callback stores the
   grant, sets `tokenStatus: connected`, and each enabled stream's on-connect
   trigger fires its first backfill.

## Migrating contact ids (upgrading a pre-SDK-id install)

A repository that ran the bundle before the `ids.external` scheme holds `contact`
rows under the old `account-people-c123` ids. Upgrading the bundle alone would
DUPLICATE every contact on the next sync (new ids, old rows still live) — run
the migration loop right after the upgrade.

The migration is **batched**: one call examines at most 200 contacts
(`{limit?, cursor?}` in — the input is an object, so pass at least `{}`),
re-puts each old-scheme row under its new id carrying its existing `person`
edge, tombstones the old row, and returns
`{migrated, absorbed, skipped, nextCursor}`. Feed `nextCursor` back as
`cursor` until it comes back **empty** — that bound is what keeps every
response frame (the `raw` People payloads included) well under the runner's
8 MiB cap, whatever the book size.

What the counts mean:

- **`migrated`** — re-keyed rows: new id created (guarded create-only, so a
  racing write turns the batch into a clean conflict re-run, never a
  clobber), person edge carried, old id tombstoned. Person records are
  untouched: no shell is minted, no mapping re-match happens, owner edits
  and offers stand.
- **`absorbed`** — a sync already wrote the contact under its new id: the
  fresher row and its data stand; the old row only tombstones. If the raced
  row resolved a **different** person (an email-less contact re-mints a
  shell), the migration **merges the new shell into the original person** —
  the original wins because it may carry owner edits — so no person is
  orphaned and none is duplicated.
- **`skipped`** — rows written by nothing: either no `account`/`resourceName`
  to derive a key from, or the new id is a **tombstone** (Google deleted the
  contact after the upgrade; a re-put would resurrect it). Skipped rows stay
  in place and are re-reported on every pass — inspect them by hand.

Re-running a drained loop is a no-op (`migrated: 0`, `nextCursor: ""`).

### Prod steps

Disable the contacts triggers before migrating so a scheduled sync cannot race
the re-key mid-loop, and re-enable them after (`substratectl apply -f triggers.yaml`
would reset `enabled: true`, so the upgrade apply is `bundle.yaml` alone):

```sh
# 1. pause the contacts stream: both its triggers off
substratectl patch triggers google-contacts-on-connect -p '{"properties":{"enabled":false}}'
substratectl patch triggers google-contacts-scheduled -p '{"properties":{"enabled":false}}'

# 2. the upgrade: the schema closure only (NOT triggers.yaml — that would
#    re-enable the sync mid-migration)
substratectl apply -f bundle.yaml

# 3. the re-key loop: repeat, feeding each nextCursor back, until it is empty
substratectl function call google.bundles.substrate.reamde.dev/contactsidmigration --input '{}'
# → {"output": {"migrated": 200, "absorbed": 0, "skipped": 0, "nextCursor": "0"}, …}
substratectl function call google.bundles.substrate.reamde.dev/contactsidmigration --input '{"cursor":"0"}'
# … until: {"…": …, "nextCursor": ""}
#
# nextCursor is a retained-prefix count, not a progress offset — the same
# value coming back twice is normal (rows the batch re-keyed have LEFT the
# listing). Loop until it is EMPTY, nothing else.

# 4. resume the contacts stream: both triggers back on
substratectl patch triggers google-contacts-on-connect -p '{"properties":{"enabled":true}}'
substratectl patch triggers google-contacts-scheduled -p '{"properties":{"enabled":true}}'
```

Run the loop **before** the first post-upgrade sync gets to full-read the
book if you can: an expired-sync-token full read stamps a fresh generation
and its sweep deletes every old-scheme row for that account wholesale, which
loses the person continuity this migration exists to preserve.

## Files

- `bundle.yaml` — the **schema closure**: authority + bundle + config type +
  account type + the six source/mirror types + the three sync functions +
  the one-shot id migration + the two mappings onto person.
- `triggers.yaml` — the **delivery wiring**: three on-connect backfill
  triggers and three hourly re-sync schedules, as ordinary data records.

## Tested

`engine/google_contacts_bundle_db_test.go` installs this closure from these
very files. It admits the schema through the loader (types, traits, the
bundle closure, the mapping shape, the stream-prefixed account state — no uv
needed) and, where uv is available, applies the whole closure into a test
repository and asserts every member and all six triggers install.

`engine/google_gmail_bundle_db_test.go` and
`engine/google_calendar_bundle_db_test.go` prove the two new streams. Each
starts with a no-DB admission test (the feature scope is really wired, the
mirrors declare what the body writes, and the emit ceiling names the core
types the body emits directly) and then drives the sync body **page by page**
through the runner against a loopback provider, so the paged cursor itself is
observable:

- the mirrors and the core rows land under the same derived ids, with the
  required edges filled and the addresses resolved to people one hop away;
- the stamp carries the **run-start** watermark, not the completion clock;
- only a Gmail 404 (and only a Calendar 410) drops to a full re-read — a 400
  surfaces as `erroring` instead;
- the sweep that follows is **window-scoped**: an archived message or event
  below the window's floor survives, and so does a calendar event above its
  horizon — both are the archive-deletion regression;
- a **truncated** re-read does not sweep at all, its resume carries the
  pending sweep across the run boundary, and the run that finally drains the
  walk sweeps then (`TestGoogleGmailCappedRereadDefersSweep` — the
  first-live-connect data-loss regression, and the reason the fake pages its
  listing);
- the backfill is bounded by **invocations** as well as list pages, and each
  run resumes further along than the last, so the chain cannot livelock inside
  the engine's drain deadline;
- a stale `gmailBackfillResume` page token restarts its window instead of
  parking the account on a dead token;
- an address the engine's `email` validator would reject is skipped rather
  than staged, so one malformed header cannot park the drain;
- a history delta's retractions take the thread once its last message is gone;
- a `cancelled` calendar entry retracts both halves and is counted as a
  cancellation rather than a failure, a deleted calendar takes its whole event
  tree with it, and the sync token commits only from the page that omits
  `nextPageToken`;
- a calendar failure is recorded even while gmail already owns the erroring
  rollup;
- a tampered API base refuses to send the token and stamps `erroring` without
  advancing either the rollup `lastSyncedAt` or the stream's own anchor;
- one poisoned account never stalls the account behind it;
- and the same address on two accounts, or an address an address-book contact
  already minted a person for, converges on ONE person.

Real Google API calls can't run in a test (no creds); live OAuth + sync is
verified against a connected account (alice@example.com), which needs a
reconnect after this upgrade because the existing grant carries only
`contacts.readonly`.

`engine/google_id_migration_db_test.go` proves the contact re-key end to end:
contacts seeded under the OLD ids (email-matched, email-less, and
cross-account shared-email — the person-shell hazard cases), the migration
driven as the operator drives it — batched, more contacts than the limit,
looping until `nextCursor` is empty — and the same logical contacts asserted
under the NEW ids with the SAME person records (none re-minted), old ids
tombstoned, an idempotent re-run, and a post-migration upsert under the sync's
new scheme landing on the migrated row. Two more tests pin the review fixes:
the absorbed-merge case and the skip paths.
