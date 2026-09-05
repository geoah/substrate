# The Google bundle — the substrate's first real connector

This is the first genuine **integration-as-bundle**: it connects a Google
account over the host OAuth facility and syncs three streams into the graph.

| Stream   | Toggle            | Scope               | Function       | Mirrors                            |
| -------- | ----------------- | ------------------- | -------------- | ---------------------------------- |
| Contacts | `enabledContacts` | `contacts.readonly` | `contactssync` | `contact`                          |
| Gmail    | `enabledGmail`    | `gmail.readonly`    | `gmailsync`    | `thread`, `message`, `emailaddress` |
| Calendar | `enabledCalendar` | `calendar.readonly` | `calendarsync` | `calendar`, `event`, `emailaddress` |

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
**bundle · kind · trait · function · trigger**.

## What it writes, and what it does not

**Mirrors only.** Every row this closure writes is one of its own kinds, in
Google's own shape: a contact, an address, a thread, a message, a calendar, an
event. It writes nothing into a package it does not own, and it declares no
mapping, because a mapping onto a kind is the declaration of the package that
owns that kind (decision record 0049).

```
messages.get ──▶ message (mirror) ──▶ thread (mirror)
events.list  ──▶ event (mirror)   ──▶ calendar (mirror)
people.list  ──▶ contact (mirror)
every address on a header or an attendee list ──▶ emailaddress (mirror)
                                                       │
                            a mapping YOU declare ─────┴──▶ your person kind
```

The `emailaddress` record is the bridge to the repository's own vocabulary:
one row per address seen, keyed per account, carrying an empty
`subject: true` slot. Declare a mapping from it onto the kind you keep people
in and every address converges on one record of yours; an address-book contact
and a mail sender land on the same one, because both mirrors carry the same
address. Until you declare one, the slot stays empty and the mirrors are the
whole output.

**A mapping cannot mint a message or an event**, which is why nothing here
tries: a mapping's only creator is a shell mint, a bare row with no
references, and a kind like `emailmessage` declares a required `thread`. The
general capability is filed as ticket 041, "structural recordmappings"; until
it exists, a repository that wants messages in its own vocabulary writes them
from a function of its own, reading these mirrors.

## The pieces

- **`config`** (oauth2): the Google OAuth client — `clientId` and
  secret-typed `clientSecret`. The endpoints and the feature-to-scope mapping
  are **trusted manifest metadata** on the bundle document, never properties
  on this mutable row. The bundle's `client` input resolves one record of
  this kind for the host OAuth facility (the sole record, the one named
  `default`, or a bound one); while it is unresolved or incomplete the bundle
  status carries a setup step and the OAuth flow refuses.
- **`account`** (accountconfig): one connected account — `email`, the three
  toggles, `syncFrequency`, `backfillDepth`, `calendarSeries` (deprecated and
  inert), the host-written
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
  subject slot is unpinned, optional and left empty on write: a mapping the
  repository declares is what fills it.
- **`emailaddress`** (source type): one address, keyed by
  `ids.external("google-address", account, address)`. Written by both
  `gmailsync` and `calendarsync`, and carrying the same empty subject slot.
- **`thread` / `message`** (gmail mirrors): ids
  `ids.external("gmail-thread"|"gmail-message", account, providerId)`.
- **`calendar` / `event`** (calendar mirrors): ids
  `ids.external("gcal-calendar", account, calendarId)` and, because a Google
  event id is unique **per calendar** rather than globally,
  `ids.external("gcal-event", <the calendar's derived id>, eventId)` — the
  helper NESTED, never a string composition, which would not be injective.
This closure ships NO recordmapping (decision record 0049). A repository that
wants contacts and addresses to reach its people declares two of its own, from
`contact` and from `emailaddress` on `property: person`, each probing an email
against its people's emails. One note worth copying: map
`emailaddress.displayName` onto the person's `name` only, never `displayName`,
because a header's "Alice Example" is weaker than an address book's nickname
and mail volume would clobber it under latest-write-wins.

The people sample already writes both, that note included:
`samples/people/mappings.google.yaml` holds the two suggested mappings, and
importing the sample with this provider installed lands them. Import it first
and they are dropped and reported waiting for this package (docs/bundles.md,
"Suggested mappings"). There is no suggested mapping onto a message, a thread
or a calendar event, and there cannot be one: a mapping's only creator is a
shell mint, which cannot fill a required reference such as a message's thread.

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
  archive. The pending sweep therefore rides **in the resume**.
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
- **An html body is flattened to markdown, links kept.** `text` prefers the
  `text/plain` part, but only if it says something: an empty or whitespace
  plain part is a template's placeholder, not a body. Otherwise the
  `text/html` part goes through `html.parser` and an `<a href>` comes out as
  `[label](url)`, because the mirror's `text` is a markdown property
  and a tag-strip left a mail whose visible words are "click here" with
  nothing to follow. The rest of the rules, each one a mail shape that got
  them written:
  - `<script>`, `<style>` and `<title>` contents are dropped; block tags
    become line breaks; a list item becomes `- `.
  - A picture is dropped but its `alt` is kept, because inside an anchor it
    is the only label a button has. An anchor with no words at all (a spacer,
    an image with no `alt`) is dropped rather than written out as a bare
    tracking url.
  - A destination is kept only for `http`, `https`, `mailto` and `tel`. The
    tag-strip let no href through at all, and `data:text/html,<script>` in a
    rendered body is somebody else's code.
  - An anchor ends where the next one begins and where its paragraph, list
    item or cell ends, so a missing `</a>` cannot pull the rest of the letter
    into one link label. An inline tag never ends one: `Click <b>here</b>` is
    a whole label.
  - The read is bounded three times: the source is cut at 4,000,000
    characters, comments (Outlook's conditional blocks are comments) and
    `data:` URI payloads go before the parser sees them, and the html is fed
    in chunks that stop as soon as there is enough text for the
    8,000-character body, with a 600,000-character ceiling for markup that
    never produces any.
  - Markup that makes the parser raise, or that swallows its own body, falls
    back to the tag-strip for that one message and logs that it did. So does
    a parse that hit the ceiling holding nothing but a preheader, but only if
    the strip, which reads the whole source, finds the letter that was behind
    it: otherwise the short parse stands and keeps its links.
- **The flattener is forward-only, like every other mirror change.** A
  message already synced under an earlier version keeps the `text` it was
  written with until Gmail reports a change to it. Clearing the account's
  `gmailHistoryId` forces the windowed re-read that rewrites them.
- **Person references are capped at 200 per message and per thread.** Every
  reference target is locked in the page's one transaction, and a
  1,000-recipient list across a 25-message hydrate batch is 25,000 of them.
  The mirror keeps the
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
- A delta always carries `status: cancelled` entries. Those are **deletes**:
  an event mirror is retracted rather than tombstoned, so a reader never
  filters cancellations. They are counted as
  `cancelled`, **not** as `skipped`: a cancellation is the provider's ordinary
  housekeeping, and counting it as a failure made every healthy calendar sync
  read `ok (N skipped)` on the console forever.
- **A calendar that went away is retracted.** The `calendarList` walk asks for
  `showDeleted=true`, and a deleted entry takes its event mirrors and the
  calendar mirror with it, a bounded page at a time, and
  is never enqueued for event sync. Nothing else could: a calendar that is no
  longer walked never 410s, and the sweep is per-calendar.
- **Every calendar except an `accessRole: freeBusyReader` share** is synced (a
  free/busy share carries no content). `selected` is recorded so a later slice
  can add per-calendar opt-out without a re-sync.
- **Recurrence rides the mirror, and no master is fetched.** The instance
  walk stays `singleEvents=true`, so Google does the expanding and each
  instance mirror carries its master's `recurringEventId`, `recurrence` (the
  verbatim RRULE/EXDATE/RDATE lines) and `originalStartTime`. Deriving a
  series record out of those is the repository's to do, from its own kinds:
  this closure writes nothing outside its own package. The account's
  `calendarSeries` toggle is deprecated and inert.

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
- **No second view of a recurring event.** `singleEvents=true` stays, so every
  event mirror is a concrete occurrence carrying its master's rule verbatim in
  `recurrence`. Nothing here expands a rule, and nothing here derives a series
  record from one
  ([0039](../../docs/decisions/0039-the-substrate-stores-a-recurrence-rule-and-never-expands-it.md)).
- **No label type.** The message mirror keeps plain `labelIds` strings rather
  than a reference into a label mirror, so there is no `label` kind.
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
   https://<your-substrate-host>/api/v1/oauth/callback
   ```

   (The value is the host's `SUBSTRATE_OAUTH_CALLBACK_URL`.) This is the URI
   Google redirects the browser back to after consent.

3. **Add an account.** Create an `account`, set the toggles you want, pick a
   `syncFrequency` (`hourly`/`daily`) and a `backfillDepth`. It starts
   `tokenStatus: pending`.

4. **Connect.** Run the host OAuth flow against the account
   (`substratectl bundle connect`, or `POST …/oauth/start
   {"record": "<accountId>"}`) → consent at Google → the callback stores the
   grant, sets `tokenStatus: connected`, and each enabled stream's on-connect
   trigger fires its first backfill.

## Files

- `bundle.yaml` — the **schema closure**: package + bundle + config type +
  account type + the six source/mirror types + the three sync functions +
  the one-shot id migration. No mapping: this package owns no person.
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
mirrors declare what the body writes, and the emit ceiling names the
types the body emits directly) and then drives the sync body **page by page**
through the runner against a loopback provider, so the paged cursor itself is
observable:

- the mirrors land under their derived ids, and every address on a header or
  an attendee list lands as one `emailaddress` row;
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
- a `cancelled` calendar entry retracts the event mirror and is counted as a
  cancellation rather than a failure, a deleted calendar takes its whole event
  tree with it, and the sync token commits only from the page that omits
  `nextPageToken`;
- an instance mirror carries its master's `recurringEventId`, `recurrence`
  and `originalStartTime`, and no master is fetched
  (`TestGoogleCalendarMirrorsCarryTheRecurrence`);
- a calendar failure is recorded even while gmail already owns the erroring
  rollup;
- a tampered API base refuses to send the token and stamps `erroring` without
  advancing either the rollup `lastSyncedAt` or the stream's own anchor;
- one poisoned account never stalls the account behind it;
- and the same address on two accounts lands on ONE `emailaddress` row, which
  is what makes a repository's mapping converge them on one person.

Real Google API calls can't run in a test (no creds); live OAuth + sync is
verified against a connected account (alice@example.com), which needs a
reconnect after this upgrade because the existing grant carries only
`contacts.readonly`.
