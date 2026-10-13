# Bundles catalog

The catalog in the binary serves twenty-six bundles, in the two tiers
[decision record 0048](decisions/0048-providers-are-published-samples-are-copied.md)
draws: ten providers and sixteen samples. This page covers nineteen of them;
the seven vocabulary samples (`people`, `tasks`, `calendar`, `messaging`,
`scheduling`, `observations` and `health`) are in
[built-in kinds](builtin-kinds.md).

**Ten providers**: Google, GitHub, Linear, WHOOP, Garmin, Google Fit, Notion,
Beeper, Slack and Apple Health. Each is a package its publisher owns,
installed under `providers.substrate.reamde.dev` and upgraded there. Nine sync
from the provider into the repository; Apple Health has no API to read, so its
provider receives what the Health Auto Export app on the phone sends to a
webhook. Three also ship one write each, a function
nothing fires on its own: Slack's `postmessage`, Beeper's `sendmessage` and
GitHub's `submitreview`. A write sends one request with the credential the
sync spends and writes no record; what it sent reaches the mirror only when
the sync's own discovery reads it back, which it does not always do (see each
write below and
[0125](decisions/0125-a-provider-write-is-a-callable-that-writes-no-record.md)).
Do not treat the mirror as confirmation that a send happened.

**Nine samples**: LLM, notes, the reading list, Firecrawl, Pebble, WHOOP to
health, Apple Health to health, Garmin to health and Google Fit to health.
Each is a worked example to read and copy, imported under the repository's own
authority and owned by it afterwards. A fix here reaches a repository that
imported it as an upgrade offer read off the copy's origin stamp, taken by
importing again
([0070](decisions/0070-a-copy-is-upgraded-through-its-origin-stamp-and-requires-pins-a-floor.md);
[0015](decisions/0015-unproven-kinds-stay-out-of-the-stable-set.md) is what
0048 amends).

**No provider ships a mapping.** A mapping onto a kind is declared by the
package that owns that kind
([0049](decisions/0049-the-owner-of-a-mappings-target-declares-it.md)), so no
provider declares a person or task property: the `people` and `tasks`
samples ship the five SUGGESTED MAPPINGS, and each mapping synthesises its
subject slot on the source kind when it installs
([0096](decisions/0096-a-mapping-synthesises-its-subject-slot.md)). An import
keeps a
suggested mapping only where the provider it reads is installed and fits it,
and reports the rest `waiting`, `blocked` or `ready`: see
[suggested mappings](bundles.md#suggested-mappings) for the door, and each
provider section below for the mappings pointed at it.

This is the map. The source of truth is each bundle's own manifests, under
`kinds/<authority>/<package>/` for the providers and `samples/<package>/` for
the samples, and everything taking one will add (the declarations and the
records it writes beside them) is previewable through
`GET …/catalog/{id}`.

Kinds, functions, agents, and mappings are named `<authority>/<package>/<name>`;
each section's first line gives the package, and its lists give the name. The
package carries the provider, so no name repeats it — GitHub's issue mirror
is `providers.substrate.reamde.dev/github/issue`, Linear's is
`providers.substrate.reamde.dev/linear/issue`, and one record of either is
addressed as `<authority>/<package>/<kind>/<id>`. What a closure declares is
exactly what its `installs:` lists. Beside that closure every bundle may ship **ordinary
records**, written by the same install: a provider's triggers (the
delivery wiring, in
`triggers.yaml`) and the LLM sample's three provider rows are the same kind of
thing, and the Records column counts them.

| Bundle     | Tier     | Auth           | Kinds | Functions | Records | Agents |
| ------------- | -------- | -------------- | ----- | --------- | ------- | ------ |
| Google        | Provider | OAuth          | 14    | 4         | 12      | 0      |
| GitHub        | Provider | OAuth          | 15    | 2         | 3       | 0      |
| Linear        | Provider | API key        | 13    | 1         | 3       | 0      |
| WHOOP         | Provider | OAuth          | 7     | 2         | 4       | 0      |
| Garmin        | Provider | OAuth          | 19    | 2         | 4       | 0      |
| Google Fit    | Provider | OAuth          | 5     | 1         | 3       | 0      |
| Notion        | Provider | Internal token | 8     | 1         | 3       | 0      |
| Beeper        | Provider | Pasted token   | 9     | 2         | 3       | 0      |
| Slack         | Provider | User token     | 9     | 2         | 3       | 0      |
| Apple Health  | Provider | Webhook key    | 10    | 1         | 1       | 0      |
| LLM           | Sample   | Key, per row   | 1     | 0         | 3       | 6      |
| Notes         | Sample   | none           | 1     | 2         | 0       | 2      |
| Firecrawl     | Sample   | API key        | 1     | 2         | 2       | 0      |
| Reading list  | Sample   | none           | 2     | 4         | 6       | 3      |
| Pebble        | Sample   | none           | 2     | 1         | 2       | 1      |
| WHOOP to health | Sample | none        | 0     | 1         | 1       | 0      |
| Apple Health to health | Sample | none      | 0     | 1         | 1       | 0      |
| Garmin to health | Sample | none          | 0     | 1         | 1       | 0      |
| Google Fit to health | Sample | none        | 0     | 1         | 1       | 0      |

## Connecting an OAuth provider

In the console it is the four numbered steps on the provider's page, opened
from **Providers** ([console](console.md#providers)): *Add* it if it is not
there yet; *Sign-in details* asks for the client id and secret of an OAuth
client you create with the provider, and shows the callback URL to register
as that client's redirect URI; *Connect your account* asks what to bring in
and how often, and *Create and connect* opens the provider in a new tab, where
once you approve the account's first sync starts on its own. The current step
is highlighted with its one action.

Over the API and the CLI, Google, GitHub, WHOOP, Garmin and Google Fit take
the same four steps, because the host runs the flow and the bundle only declares it
([bundles](bundles.md#the-oauth-facility)):

1. **Register an app** with the provider and note its client id and secret.
   Its redirect URI is the host's one callback,
   `https://<your-substrate-host>/api/v1/oauth/callback`, the value of
   `SUBSTRATE_OAUTH_CALLBACK_URL` ([operations](operations.md#configuration)).
2. **Create a `config` record** carrying `clientId` and `clientSecret`. The
   endpoints and the toggle-to-scope map are trusted metadata on the bundle
   document, so there is nothing else to type; the bundle's `client` input
   resolves the record (the sole one, the one named `default`, or a bound one)
   and the flow refuses while it does not.
3. **Create an `account`**, switch on the streams you want, and pick a
   `syncFrequency` and a `backfillDepth`. It starts `tokenStatus: pending`.
4. **Connect** (`substratectl bundle connect <authority>/<package>/<kind>/<id>`,
   or `POST …/oauth/start` with the account's record path as `record`; a bare
   id is refused). Consent at the provider, and the callback
   stores the grant, sets `tokenStatus: connected`, and fires each enabled
   stream's on-connect trigger.

Linear, Notion, Beeper and Slack take a pasted key or token instead: their
`config` record holds it and there is no consent step, so step 4 is the
account's own create. Apple Health takes neither: the phone sends, and the
key is the one set on its webhook trigger ([Apple Health](#apple-health)).

**`backfillDepth` bounds the FIRST window only.** Every run after it reads
from that stream's own stored watermark instead, so the value decides how far
back the first sync reaches and nothing else: `none` is from connect time
forward, `last2d`, `last30d`, `last90d` and `last1y` reach that far back
(each account kind's `backfillDepth` enum lists the values that provider
declares), and `all` is unbounded (WHOOP reads from a fixed pre-API epoch, its
stand-in for unbounded).

Where that floor is measured from is each bundle's own, and they differ: some
stamp an anchor on the first run and measure every later floor from it, so
the reach cannot creep forward night after night; others compute a dated
window from the clock of the run that needs one, so a first sync deferred by
a day reaches a day later. Each provider section below says which its streams
do, what `none` reaches, and which streams ignore the property because the
provider's own sync token gives them full-plus-incremental with no window.

## LLM (sample)

Package `samples.substrate.reamde.dev/llm`. A new repository already holds
this bundle, rehomed onto its own authority, and three keyless `llm/provider`
rows (`openai`, `anthropic`, `gemini`). The demo agents name `openai`. Key
that row and they run. A later import of this bundle onto a repository born
before the seed writes the same three rows and the same agents.

- `substrate` is the one to chat with: it reads the whole graph through the
  `query` built-in, writes a change the owner asked for through `write`,
  proposes a change it inferred as a `recordpatchrequest` the owner decides
  on, and asks clarifying questions through the `ask` built-in. Its prompt is
  what chooses between `write` and `propose`.
- `substrateEditor` writes scratchpads directly through the `write` built-in,
  the demo for engine-stamped `changes` on a thread's tool rows.
- `substrateArbiter` is a judge you point a trigger at: it accepts or rejects a
  change request through `write`.
- `substrateJudge` is the tool-less verdict agent a
  [`recordpatchpolicy`](agents.md#the-policy-door) names under `judge:`, the
  policy layer's example.
- `substrateEcho` and `substrateSummarizer` are the delegation demo, and the
  summarizer is `hiddenFromChat`: off the chat list, callable only by other
  agents.

Importing it gives you three rows that refuse until you key them: `openai`
on OpenAI's own wire, `anthropic` on Anthropic's, and `gemini` on Google's
OpenAI-compatible endpoint. Keying one is an ordinary record write,
and [registering a provider](agents.md#registering-a-provider) is where that
write, the wires and the pricing table are described.

## Notes (sample)

Package `samples.substrate.reamde.dev/notes`. The smallest bundle that shows
an agent calling functions as tools and delegating to a sub-agent, and the one
to read first. It needs no network, no credentials and no other bundle's
vocabulary, so it imports on a fresh substrate, and its two functions stand on
their own with no model at all:

```bash
substratectl import samples.substrate.reamde.dev/notes   # or: apply --as-mine -f samples/notes/bundle.yaml
substratectl function call stats --input '{"text": "hello world"}'
```

`notekeeper` is the root agent. It calls `titler` (a sub-agent with its own
budget and thread, no tools and an empty `permissions.writes`), then `stats`
(pure Python, declaring no `permissions.network`, so the sandbox denies it
sockets), then `savenote`,
which writes the one kind the bundle declares — `note`. That write lands only
because the kind is in BOTH the function's writes and the calling agent's,
which is the capability envelope in one closure.

Both agents name `provider: openai`, so running them wants that
`llm/provider` row keyed — [creation seeds it](agents.md#providers), keyless.
Calling an agent is an API call, not a CLI verb. The path segment is the
agent's id, `<authority>/notes/notekeeper` once the import has rehomed it, with
each slash written `%2F`; a bare `notekeeper` is refused
([calling an agent](agents.md#calling-an-agent)):

```bash
curl -s -X POST "$SUBSTRATE_SERVER/api/v1/substrate.reamde.dev/core/agent/<authority>%2Fnotes%2Fnotekeeper/call" \
  -H "Authorization: Bearer $SUBSTRATE_TOKEN" -H 'Content-Type: application/json' \
  -d '{"input": {"text": "id: my-note\n\nSomething worth keeping."}}'
```

One run leaves TWO `llm/thread` rows, the root agent's and the sub-agent's own,
each with its own turn and token tallies; cost rolls up onto the root. These
manifests name models the way OpenAI's wire does (`gpt-5`,
`gpt-5-mini`), which is what the seeded `openai` row speaks; point that
row at a gateway instead and the model names become the gateway's aliases.

## Google

Package `providers.substrate.reamde.dev/google`. An OAuth provider that
mirrors a Google account's address book, mail, calendars and Drive files into
the repository, in Google's own shape. Four streams share one account:
contacts, gmail, calendar and drive. Each has its own toggle, its own scope,
its own function, its own three triggers, and its own prefixed cadence anchor,
cursor and status on the account, so one stream erroring never stalls another.
The bundle's `oauth2` block derives one scope per toggle: `enabledContacts`
takes `contacts.readonly` and `directory.readonly`, `enabledGmail`
`gmail.readonly`, `enabledCalendar` `calendar.readonly`, `enabledDrive`
`drive.readonly`, and `userinfo.email` rides on all four. Read only: nothing
is ever written back to Google.

- **Kinds (14)**: `config` (the OAuth client kind, `oauth2`, named by the
  bundle's `client` input) and `account` (the Connection, `accountconfig` and
  `sync`) hold the connection; `emailaddress` holds one mailbox address per
  account and takes no product prefix, because all four streams converge on
  it. Contacts writes `contact` (one People `Person`, from the address book or
  the Workspace directory) and `contactgroup` (one `ContactGroup` a contact is
  filed under). Gmail writes `gmailthread`, `gmailmessage`, `gmailattachment`
  (one message part carrying a filename) and `gmaillabel`. Calendar writes
  `calendar` (one `calendarList` entry), `calendarseries` (one recurring
  master), `calendarevent` (one single event, or one exception naming the
  series it overrides) and `calendarsync` (one calendar's own drain state).
  Drive writes `drivefile` (one Drive v3 `File`, with the exported text of a
  Doc, a Sheet or a Slides deck).
- **Functions (4)**: `synccontacts` reads one page of `people/me/connections`
  or `people:listDirectoryPeople` per invocation, mirrors each `Person`, walks
  `contactGroups.list`, and stores a People sync token per endpoint.
  `syncgmail` reads one `messages.list` or `history.list` page, or hydrates
  one batch of up to 15 `messages.get`, per invocation. Every unit of its
  work reads one invocation clock before it starts (a fetch, a message's html
  flatten, a host read for a thread, an attachment set or a swept row), and
  what does not fit in 45 s, in 5 MB of staged payloads or in 200 host reads
  goes to the next invocation. One message's flatten stops after 3 s and the
  body says it was cut. A message whose full fetch has failed three fires
  running is read with `format=metadata` (headers, no body), so one message
  cannot hold the account. Each invocation logs how long each phase took. `synccalendar` reads one
  `calendarList` page, or one events page of one calendar, per invocation.
  `syncdrive` reads one `files.list` page, one `changes.list` page, or one
  batch of eight `files.export` calls, per invocation. All four write an
  `emailaddress` row per address they see, and none writes a kind this package
  does not own.
- **Triggers (12)**, three per stream. `google-contacts-on-connect`,
  `google-gmail-on-connect`, `google-calendar-on-connect` and
  `google-drive-on-connect` fire their stream's sync while the account is
  `connected`, carries that stream's toggle, and has no `<stream>LastSyncedAt`
  yet. `google-contacts-scheduled`, `google-gmail-scheduled`,
  `google-calendar-scheduled` and `google-drive-scheduled` tick hourly and
  sync every account due by its own `syncFrequency`.
  `google-contacts-on-request`, `google-gmail-on-request`,
  `google-calendar-on-request` and `google-drive-on-request` fire while
  `syncRequestedAt` and that stream's own `<stream>SyncRequestedAck` disagree.
- **Mappings**: none. This closure declares no consumer slot and writes no
  person. The `people` sample ships two suggested mappings instead:
  `googlecontactperson` (from `contact`, matching `emailAddresses[].value`
  against a person's emails, and mapping `names[].displayName`,
  `emailAddresses[].value` and `phoneNumbers[].value`) and
  `googleaddressperson` (from `emailaddress`, matching `address`, and mapping
  the address alone, because the hub carries no name). Import `people` with
  this provider installed and both land; import it first and both are reported
  `waiting` for this package, and a later install does not land them on its
  own: import `people` again
  ([suggested mappings](bundles.md#suggested-mappings)).

**All four scopes are wired.** The requested union is derived per consent from
the account's enabled toggles, so turning a stream on after the grant landed
needs a reconnect, which `grantedScopes` drives. `gmail.readonly` and
`drive.readonly` are both restricted Google scopes: a published client needs
CASA verification, and an External plus Testing client has its refresh tokens
revoked after seven days. An account whose grant predates the drive scope
records `driveSyncState: needsreconsent` on its first Drive call while the
other three streams sync on. The facility reads the connected address from
`oauth2/v3/userinfo` into `account.email`, which is why `userinfo.email` rides
on every toggle rather than on one.

**The owner sets four toggles, a cadence and a depth.** `enabledContacts`
syncs the address book and the Workspace directory, `enabledGmail` the
threads, messages, attachments and labels, `enabledCalendar` the calendar
list, its series and its events, and `enabledDrive` the files and their
exported text. `syncFrequency` is `off`, `hourly` or `daily` (default
`daily`): the hourly tick syncs a stream whose own anchor is older than that,
and `off` stops the schedule without touching a toggle or a cursor.
`backfillDepth` is `none`, `last30d`, `last90d`, `last1y` or `all` (default
`last30d`) and bounds the first window only: gmail, calendar and drive each
stamp their own backfill anchor on their first run and compute the floor from
that stored instant, so no reach creeps forward, and contacts ignores the
property because the People sync token gives it full-plus-incremental with no
window. Stamping `syncRequestedAt` asks every enabled stream for a sync now;
each stream echoes the value it acted on into its own
`<stream>SyncRequestedAck`, and the account-level `syncRequestedAck` follows
once every enabled stream has answered. `syncPaused` stops every stream on the
account without disabling one.

**Contacts walks two endpoints into one kind.** `people/me/connections` is the
account's own address book and `people:listDirectoryPeople` is its Workspace
directory, and a `Person` from either lands as one `contact` row keyed on its
`resourceName`. The two sync tokens are separate properties
(`contactsSyncToken` and `contactsDirectorySyncToken`) and are never merged. A
second run reads the delta under the stored token, reconciles a rename through
`metadata.previousResourceNames` and retracts a tombstone; only a structured
`EXPIRED_SYNC_TOKEN` drops back to a full re-read. An account with no
Workspace directory records `contactsDirectoryState: unavailable`, and a
skipped directory never fails the contacts sync.

**Gmail's cursor is the run-start `historyId`.** `users.getProfile` reads the
watermark before a single message is fetched, and `gmailHistoryId` is stamped
only when the drain completes and owes nothing, so a message that changes
mid-drain falls inside the next run's window. A second run reads
`users.history.list` from it; only a 404 drops to a windowed full re-read
followed by a sweep scoped to that same window. `messages.list` asks for
`includeSpamTrash=true`, so a message in Spam is mirrored with the `SPAM`
label rather than reconciled away. `gmailBackfillResume` holds the page token,
the window floor, the sweep evidence, and the message ids a page listed that
the fire did not hydrate.

**Each calendar keeps its own sync token in a `calendarsync` row**, one row
per calendar, so draining a calendar's events never bumps the `calendar` row's
version and one calendar's advance cannot skip another's tail. The token
commits only from the page that carries `nextSyncToken`, and only once that
page's reconciliation has finished. Only a 410 drops to a full re-read of that
one calendar plus a sweep of it, and a token whose `syncWalk` is not the
current walk reads as absent, which costs the same full re-read. The walk asks
for `singleEvents=false`: a recurring master is one `calendarseries` row
carrying its rule, a modified or cancelled exception is a `calendarevent` row
pointing at the series through `recurrenceOf` and naming its slot in
`originalAt`, and a plain event is a `calendarevent` row. One Google event on
one calendar is one `calendarevent` row: an exception's row id is keyed by its
series, so when a "this and following" edit splits a series and Google
re-parents the later exceptions onto the new master, writing the re-parented
exception retracts the row it left under the old series, found by the
`eventId` the two share. Nothing is expanded
into rows: a records read that bounds `at` computes each series' occurrences
beside the stored rows. `calendarPending` holds the calendar ids an
interrupted walk has not reached, and the next run resumes from exactly those.

**Drive's cursor is a changes start page token.** `changes.getStartPageToken`
is read at the start of a walk and stamped as `driveStartPageToken` only when
the walk completes and owes nothing. The cold walk is `files.list` with
`trashed = false` under the backfill window, newest `modifiedTime` first; a
second run reads `changes.list` from the stored token and stamps the
`newStartPageToken` its last page hands back. A change carrying
`removed: true`, or a file the delta reports trashed, retracts the row after a
read proves the row exists. A Doc, a Sheet or a Slides deck is exported again
only when its `modifiedTime` has moved past the one the stored row was written
with, decided from one read per page. `driveResume` holds the page token, the
window floor, and the exports the fire did not fetch.

**Every fire is self-limiting, and a failing stream stamps only its own
fields.** Each chain watches its invocation count and its wall clock from its
first page, hands back with what it owes on the account, and reports
`ok (N pending)`; `config.drainBudgetMs` overrides the built-in budget,
clamped to 1..110000 ms. A per-account drain that raises stamps that account's
`<stream>SyncState` and `<stream>SyncStatus` once and leaves both
`lastSyncedAt` and `<stream>LastSyncedAt` alone, so the schedule retries and
no window floor advances past unread data. A 429 writes `retryNotBefore` from
Google's `Retry-After`: the quota is the account's, so no stream is due while
that instant is in the future, and a rate limit reads as `throttled` rather
than `erroring`. The account binds the core `sync` trait beside
`accountconfig` ([connections](bundles.md#connections)): every stream writes
`syncStreams.<stream>` as `{state, message, lastAt, pending, requestedAck}`
beside the account-level `syncState`, `syncMessage` and `syncProgress`. The
account-level `lastSyncedAt` and `syncStatus` stay shared: they are the rollup
every connection reports, and whichever stream finishes stamps them.

**What this slice does not do.** No writeback: every call it makes is a GET.
No attachment bytes: `gmailattachment` carries the part's `filename`,
`mimeType`, `size`, its own headers and the `attachmentId` a later
`users.messages.attachments.get` would read the bytes by, and this closure
never makes that call. An inline part's bytes ride on `gmailmessage.payload`,
the MIME tree stored whole, and past about 1 MB the overflowing parts keep
`size` and `attachmentId` and lose `data`. No Drive file bytes either:
`drivefile.text` is the `files.export` of a Doc, a Sheet or a Slides deck,
capped at 200 KB (`exportTruncated` says when the cap cut one), and every
other type has no `text`. The Drive fields mask leaves `capabilities`,
`permissions`, `contentHints`, `spaces`, `labelInfo` and `shortcutDetails`
unrequested, and `gmaillabel` declares `messagesTotal`, `messagesUnread`,
`threadsTotal` and `threadsUnread` but never fills them, because only
`users.labels.get` returns them and the sync never calls it. And no consumer
slot: no person, no task, and no kind this package does not own.

**Upgrading from version 18.** Four kinds are renamed: `event` to
`calendarevent`, `series` to `calendarseries`, `thread` to `gmailthread` and
`message` to `gmailmessage`. All three functions are renamed too:
`contactssync` to `synccontacts`, `gmailsync` to `syncgmail`, and
`calendarsync` to `synccalendar`, which frees the name `calendarsync` for the
new per-calendar state kind. Since version 18 the package gained the drive
stream (`drivefile`, `syncdrive`, and the triggers `google-drive-on-connect`,
`google-drive-scheduled` and `google-drive-on-request`), the kinds
`contactgroup`, `gmailattachment` and `gmaillabel`, the three other
`on-request` triggers, the `enabledDrive` toggle with its
`drive.readonly` scope, `directory.readonly` under `enabledContacts`,
`syncRequestedAt` with one `<stream>SyncRequestedAck` per stream,
`syncPaused`, `retryNotBefore`, `calendarPending`, the core `sync` trait's
twelve properties on the account, and `config.drainBudgetMs`. It removes
`account.calendarSeries`, `series.cancelledSlots` (a cancelled exception is a
`calendarevent` row carrying `status: cancelled` now), `message.labelIds`
(replaced by `gmailmessage.labels`, a repeated reference at `gmaillabel`), and
the `subject` slot on `contact` and `emailaddress`. The `emailaddress` kind
now carries `account` and `address` and nothing else. `account.syncState` is
retyped from an enum to a string, because the `sync` trait contracts the
datatype.

## GitHub

Package `providers.substrate.reamde.dev/github`. An OAuth provider that mirrors
the code work you are involved in: the connected user, the repositories the
account can reach, and every issue and pull request you author, are assigned,
are mentioned in, comment on, are review-requested on or have reviewed, with
the reviews on those pull requests. The host runs the flow against the endpoints the bundle
document carries and derives the scope union from the account's toggles:
`enabledUser` asks for `read:user`, and `enabledRepos`, `enabledIssues` and
`enabledPullRequests` each ask for `read:user` and `repo`. The sync never
writes to GitHub; `submitreview` is the one write.

- **Kinds (15)**: `config` (the OAuth app, `oauth2`, named by the bundle's
  `client` input) and `account` (the Connection, `accountconfig` and the core
  `sync` trait), then thirteen mirrors: `user` (a person, an organization or a
  bot), `repository` (the full-repository schema, its owner and parent and
  source as references), `license` and `codeofconduct` (the catalogue entries
  behind a repository's keys), `team` (a requested reviewer team), `milestone`,
  `label`, `issuetype` (an organization issue type), `app` (the GitHub App an
  item was performed via), `comment` (today only an issue's pinned comment mints
  one), `issue`, `pullrequest` (keyed on its pull id, carrying its issue id
  beside it) and `review`.
- **Functions (2)**: `githubsync`. One invocation works one stage and
  checkpoints: one search or listing page (100 items), or up to 400 hydration
  entries, stopping once 40 seconds of its 60-second deadline are spent. A
  drain stops itself after 75 seconds, inside the engine's two-minute drain
  deadline, and saves where it got to for the next run. It
  writes only this package's own kinds, and it refuses to send the access token
  anywhere but `https://api.github.com` or a loopback host. `submitreview`
  submits one review on a pull request, `approve` or `comment`, through
  `POST /repos/{owner}/{repo}/pulls/{number}/reviews`; see
  [writing to GitHub](#writing-to-github).
- **Triggers (3)**: `github-on-connect` fires `githubsync` the first time an
  account is `connected` with a toggle on and no `lastSyncedAt`;
  `github-scheduled` fires it hourly and the body syncs the accounts due by
  their own `syncFrequency`; `github-on-request` fires it while
  `syncRequestedAt` differs from the `syncRequestedAck` the sync echoes back.
- **Mappings**: none. `user` declares no person property: the package that owns
  the person declares the mapping (record 0049), and the mapping synthesises the
  subject slot when it installs (record 0096). The `people` sample ships that
  declaration as a SUGGESTED MAPPING (`githubuserperson`, beside
  `googlecontactperson`, `googleaddressperson` and `linearuserperson`): it
  matches the profile's `email` against a person's emails and maps the name, the
  `login` as the display name, and the email. Import `people` with this provider
  installed and it lands; import it first and it is reported `waiting`, and
  importing `people` again after the install is what lands it ([suggested
  mappings](bundles.md#suggested-mappings)).

**Four toggles on the account decide the scopes and the walk.** `enabledUser`
mirrors the connected user's profile and hydrates a full profile (name, public
email) for every other login the sync meets; `enabledRepos` the repositories the
account can reach; `enabledIssues` the issues the connected user is involved in;
`enabledPullRequests` the pull requests the user is involved in,
review-requested on or has reviewed, with their reviews. GitHub's classic scopes are coarse:
there is no read-only repository grant, so the last three all ride the one
`repo` scope, which grants write on private repositories even though this bundle
only ever reads. `user:email` is never requested, so the identity probe reads
the PUBLIC profile email from `GET /user`, and an account whose profile sets
none carries a blank `email`. The bundle declares no `revocationEndpoint`:
disconnecting deletes the stored credential and nothing else. The owner revokes
the grant from GitHub's settings page.

**`syncFrequency` and `backfillDepth` are the owner's two dials.**
`syncFrequency` is `off`, `hourly` or `daily` (default `daily`) and is what the
hourly schedule honours; `syncPaused` holds the schedule and the console's
button both, while an operator naming the account explicitly still gets a run.
`backfillDepth` is `none`, `last30d`, `last90d`, `last1y` or `all` (default
`last30d`) and bounds the FIRST search window only: every run after it reads
from that stage's own stored watermark. `none` is forward-only from the first
run's start, which the stamp persists so the floor ratchets; the dated depths
are computed from the clock of the run that needs one. Stamp `syncRequestedAt`
to ask for a run whatever the cadence says.

**The walk is a stage list, pinned into the cursor at queue head.** `user`
(`GET /user`) always runs, because the searches need the login, and it sets
`account.user`. `enabledRepos` adds `repos` (`GET /user/repos`, one page per
invocation), `reposFetch` and `licensesFetch`; `enabledIssues` adds the `issues`
search; `enabledPullRequests` adds the `pulls`, `pullsReview` and
`pullsReviewed` searches and then `pullsFetch` and `reviewsFetch`; `enabledIssues` then adds `issuesFetch`
and `graphFetch`; `enabledUser` adds `usersFetch`. The four searches are
`GET /search/issues` over `type:issue involves:<login>`,
`type:pr involves:<login>`, `type:pr review-requested:<login>` and
`type:pr reviewed-by:<login>`, because `involves:` covers neither a review
request nor a review, and GitHub drops a requested reviewer from the list once
they review.

**The watermarks and the backlog live on the account and nowhere else.**
`syncCursors` is a keyed `datetime` map with one entry per search stage
(`issues`, `pulls`, `pullsReview`, `pullsReviewed`), stamped with the run's start rather than the
completion clock and re-queried with a 120-second overlap, so one stage stalling
never advances another's tail. `syncPending` is the hydration backlog, keyed by
queue (`repos`, `licenses`, `pulls`, `reviews`, `issues`, `graph`, `users`),
durable because the engine bounds a drain at 512 invocations and two minutes;
`syncProgress` reports `{phase, done, total, pending}` at every checkpoint, so a
console watching a long drain sees the number fall. A run that starts with a
backlog drains the backlog first and then walks the searches in the same run.
A walk that does not fit one drain stops after 75 seconds of it, because the
engine parks a drain that runs past two minutes and drops its last page. The
stop stamps the account and writes `syncWalk`: the stages the walk still owes,
the search position it stopped at (a search past its first page resumes from
the newest `updated_at` it mirrored) and the run start its watermarks will
carry. An account holding `syncWalk` is due on the next hourly tick whatever
its `syncFrequency` says; that run drains the backlog, resumes the walk there,
and clears `syncWalk` when the walk completes. A pull request's `url` is its
own `/pulls/{n}` address from the search hit and the pull read alike.

**A restricted organization is a counted skip.** An organization that has not
approved the OAuth app, or whose SAML session the token has not authorised,
answers every read of its resources with a 403 while the search still lists its
items. The sync skips that item, mints nothing for it, and counts it against
the organization on `syncSkipped`, which holds the count, one repository of
that organization and when it was last refused. The organization is asked once
per run, so a search page full of its items costs one refused request rather
than one per item, and the run ends `ok` with the count and the advice on
`syncStatus` and `syncMessage`. The next run re-asks one
remembered repository per organization (at most eight); once the owner has
approved the app the search watermarks are dropped and the window the refusal
hid is re-walked from the backfill floor, which under `backfillDepth: none` is
empty by definition.

**Errors report on the account.** `syncState` is `never`, `running`, `ok`,
`erroring` or `throttled`; a rate limit is `throttled`, with the retry instant
in `syncMessage`, the one human line the last run left. `syncError` and
`syncErrorAt` hold the last error text and when it was written; a later run
that ends `ok`, scheduled or record-triggered, clears them, and so does a
reconnect. The legacy `syncStatus` string stays beside the trait
with its own prefixes (`ok`, `ok (partial: …)`, `ok (capped: …)`,
`ok (deferred: …)`, `erroring: …`). `syncStreams` is declared because the trait
contracts it and written by nobody: GitHub is one function over one account.

**What this bundle does not do**: the sync writes nothing back (`submitreview`
is the one write). Deletes are not reconciled, because GitHub's search feed
carries no tombstones: a deleted issue stops updating and its mirror stands,
and a hydration read that 404s is counted as `unreachable` on `syncStatus`
rather than removing the row. There is no comments stream, so a `comment` row
exists only where an issue embeds a pinned comment, and a pull request carries
the diff counts and no commit, file or diff rows.

**Upgrading from version 12.** Version 21 installs nine more kinds: `team`,
`milestone`, `label`, `review`, `license`, `codeofconduct`, `issuetype`, `app`
and `comment`. `config` adds `apiBase`, a test-only override that refuses any
origin but `https://api.github.com` or a loopback host. `account` binds the core
`sync` trait beside `accountconfig` and adds `syncPaused`, `syncRequestedAt`,
`syncRequestedAck`, `syncState`, `syncMessage`, `lastSyncStartedAt`,
`lastSyncDurationMs`, `syncProgress`, `syncError`, `syncErrorAt`, `syncStreams`,
`syncPending` and `syncSkipped`; its `syncCursor` (one string) is now
`syncCursors` (a keyed `datetime` map, one entry per search stage), and its
`login` (a string) is now `user`, a reference at the `user` mirror. On the
mirrors `raw` is gone and every documented field has a typed home: `htmlURL` is
`htmlUrl`, `createdAt` is the temporal trait's `at`, `authorLogin` is the `user`
reference, `labels` is a list of references at `label`, `state` is an enum,
GitHub's `title` is `issueTitle` on `issue` and `pullRequestTitle` on
`pullrequest` (`title` is reserved on every record), and `pullrequest` carries
both `pullRequestId` and `issueId`. The function's `permissions.network` adds
`127.0.0.1` and `localhost`, and `triggers.yaml` adds `github-on-request`.

### Writing to GitHub

`submitreview` reviews one pull request as the connected user. It takes
`repository` (`owner/name`, a mirrored repository's `fullName`), `number`,
`event` (`approve` or `comment`), `body` (required for `comment`) and an
optional `account`, the `github/account` id whose token to spend, which may be
left out when one account carries a token. The token is the one the host
injects for the sync, and the `repo` scope that `enabledRepos`,
`enabledIssues` and `enabledPullRequests` ask for is what grants the write; an
account granted neither `repo` nor `public_repo` is refused before a request.
It answers the review's `id`, `state` (`APPROVED` or `COMMENTED`) and `url`,
and it writes no record: the review reaches the `review` mirror on the next
sync. GitHub stops listing a requested reviewer once they review, so the
`reviewed-by:` search is what reads back an approval by an owner who was only
asked to review ([issue #710](https://github.com/geoah/substrate/issues/710)).
A GitHub refusal (`422` for approving your own pull request, `404` for
a repository the grant cannot see) fails the call with GitHub's message.
Redirects are refused rather than followed, so the origin pin holds for the
one request made.

```bash
curl -X POST "$SUBSTRATE_SERVER/api/v1/substrate.reamde.dev/core/function/providers.substrate.reamde.dev%2Fgithub%2Fsubmitreview/call" \
  -H "Authorization: Bearer $SUBSTRATE_TOKEN" -H 'Content-Type: application/json' \
  -d '{"input": {"repository": "octo-org/octo-repo", "number": 42, "event": "approve"}}'
```

## Linear

Package `providers.substrate.reamde.dev/linear`. A provider that mirrors one
Linear workspace in Linear's own shape: the organization, its teams and
members, the workflow states, labels and project statuses those teams define,
the projects and cycles work is planned into, and the issues, comments and
reactions in the account's window. It authenticates with a pasted personal API
key rather than a consent flow, so the bundle document declares no `oauth2`
block: the key is the `config` record's `apiKey`, sealed against read-back and
injected into the sync body alone, and the body refuses to send it anywhere
but `https://api.linear.app` or a loopback host whatever the config's
`apiBase` says. Read only, and nothing here ever writes back to Linear.

- **Kinds (13)**: `config` (the key and the pinned origin), `account` (the
  connection, its toggles and every watermark the sync keeps), and the mirrors
  `organization`, `team`, `user`, `workflowstate`, `issuelabel`,
  `projectstatus`, `project`, `cycle`, `issue`, `comment` and `reaction`.
- **Functions (1)**: `linearsync` walks the enabled stages in reference order
  and reads at most eight pages of Linear's API per invocation (50 rows a
  page, 25 for issues and for every nested connection), then parks what it did
  not finish on the account.
- **Triggers (3)**: `linear-on-connect` fires `linearsync` when an account
  carries a toggle and has no `lastSyncedAt`; `linear-scheduled` fires it
  hourly; `linear-on-request` fires it while `syncRequestedAt` differs from
  the `syncRequestedAck` the sync echoes back.
- **Mappings**: none. `user` and `issue` declare no person or task property:
  a mapping onto a person or a task is the declaration of the package that
  owns that target (record 0049), and it synthesises the subject slot when it
  installs (record 0096).

The two samples ship them as SUGGESTED MAPPINGS. `people` declares
`linearuserperson`, matching a member's `email` against a person's `emails`
and mapping `name`, `displayName` and the address; `tasks` declares
`linearissuetask`, matching an issue's `url` against a task's `url` and
mapping `issueTitle` onto the task's name beside the link. Import either
sample with this provider installed and its mapping lands; import it first and
the mapping is reported `waiting` for this package, and installing this
package afterwards is not enough on its own: import that sample again
([suggested mappings](bundles.md#suggested-mappings)). A projected task's
`status` is not mapped and cannot be: a state moves through its declared
transitions, never through a mapping
([0040](decisions/0040-the-four-occurrence-logs-say-done.md)), so the task the
mapping mints starts `open` and every move after that is yours. `name` and
`url` recompute at the machine tier, so retyping the heading keeps it and
Linear's next title lands only where you have not.

**Setting it up takes two records and no consent step.** Mint a personal API
key in Linear (Settings, Security & access, Personal API keys), paste it onto
a `config` record as `apiKey`, and leave `apiBase` empty: it is a
loopback-only test override and any other origin refuses to sync. Then create
an `account` with the toggles you want. `enabledWorkspace` mirrors the
organization, teams, members, workflow states, labels and project statuses,
and every other stage references those rows. `enabledProjects` mirrors the
projects and cycles. `enabledIssues` mirrors the issues in the window with
their labels, subscribers, comments and reactions. The account's own create is
what starts the first sync, and `lastSyncedAt` is the completion marker there
is. `syncFrequency` is `off`, `hourly` (the default) or `daily`; `syncPaused`
holds the schedule while it is true; stamping `syncRequestedAt` asks for a run
now whatever the cadence says. `backfillDepth` (`none`, `last14d`, `last30d`
by default, `last90d`, `last1y`, `all`) bounds the first issues walk only, by
the issue's own `updatedAt`, and `none` means from that first run's start
forward.

**The walk is nine stages in reference order**, so a row is never written
before the rows it points at: `viewer`, `organization` (with its project
statuses), `teams`, `users`, `workflowstates`, `labels`, `projects`, `cycles`,
`issues`. The `viewer` stage is not optional: it writes the account's `user`
reference, and the `organization` stage writes its `organization` reference.
Only the issues stage is incremental. It keeps a watermark in the account's
`syncCursors`, walks `updatedAt` order behind it with a 15-minute overlap, and
stamps the run-start instant when it drains. The other stages are a few
hundred rows each and are walked whole every sync, which keeps a renamed team,
a restored label and a reopened cycle correct without a tombstone protocol.
Every root stage connection is read with `includeArchived: true` (the nested
`labels`, `subscribers` and `comments` reads inside an issue are not), and the
users stage adds `includeDisabled: true`, so an archived label or a member who
left stays a reference that resolves.

**One invocation stops at eight pages**, or sooner: a round ends once the
time it has spent plus the 30-second HTTP timeout reaches its 35-second drain
budget, so it never starts a page the function's `PT60S` deadline could kill
mid-write. What it stopped on is parked in the account's `syncPending`: the
stage, its page number, its Linear page cursor and its floor, because the engine
drops a paged checkpoint when it bounds a drain. An account with a parked
walk reports itself due whatever `syncFrequency` says, so the hourly tick is
also the continuation and a cold workspace converges over consecutive ticks
instead of restarting. `lastSyncedAt` marks the attempt finished either way;
`lastCompletedAt` moves only when a walk ran out of work, and only that
decides whether the next walk is cold. A failing run leaves the parked walk
in place, so the next one resumes on the page that failed rather than from
page one.

**The account reports the run.** It binds the core `sync` trait, so
`syncState` is one of `never`, `running`, `ok`, `erroring` and `throttled`,
`syncMessage` is the one line the last run left, `syncError` and `syncErrorAt`
hold the last error until a newer one replaces it or a later run ends `ok`, and `syncProgress` is
`{phase, done, total, pending}` over the stage walk, written at every park so
a cold walk shows a phase that moves. Linear is one stream, so `syncStreams`
is declared and written by nobody and `syncRequestedAck` alone answers a
request. A rate limit reads `throttled` rather than `erroring`: the 429, or
the `RATELIMITED` extension Linear serves inside an HTTP 400, lands on
`retryNotBefore`, and no entry path makes a request before that instant. The
legacy `syncStatus` string stays beside the trait, reading `ok`,
`ok (deferred: issues x1)` while a walk is parked, or `erroring: <reason>`.

**What it does not do.** There is no writeback. Deletes are not reconciled:
Linear's connections carry no tombstones and the sync runs no mark-and-sweep,
so an issue deleted upstream keeps its mirror, and `trashed` and `archivedAt`
cover the two states Linear itself models. An unassignment IS reconciled,
because every payload is authoritative and a selected field returned null is a
clear: the next walk over that issue writes `assignee` back to null.
Initiatives, documents, templates, project milestones, project updates,
attachments and issue relations are not mirrored, and each relation at one is
the API's own id with the scope boundary in its description. A nested
connection is written whole or not at all, so an `issue.labels` page that says
`hasNextPage` leaves the property alone and says
`ok (short: issue.labels past 25)` on the status.

**Upgrading from version 14.** Version 15 adds eight mirrors to the five kinds
version 14 declared, taking the closure to 13: `organization`,
`workflowstate`, `issuelabel`, `projectstatus`, `project`, `cycle`, `comment`
and `reaction`. The function `issuessync` is renamed `linearsync`. The
triggers `linear-issues-on-connect` and `linear-issues-scheduled` are renamed
`linear-on-connect` and `linear-scheduled`, and `linear-on-request` is new.
The bundle document drops its `oauth2` block and the `read` scope with it: the
`config` record now holds `apiKey` and `apiBase` instead of an OAuth client,
and the bundle's one input is `connector`. On `issue`, `assigneeEmail` is
removed and `assignee` is a reference at `linear/user`, and the heading is
`issueTitle`. On `account`, `tokenRef`, `tokenStatus` and `grantedScopes` stay
declared but dormant, the viewer and the workspace are the `user` and
`organization` references (there is no `email` property), and the core `sync`
trait adds `syncPaused`, `syncState`, `syncMessage`, `syncProgress`,
`syncError`, `syncErrorAt`, `syncStreams`, `lastSyncStartedAt` and
`lastSyncDurationMs`. The `people` sample drops `linearissueperson`: the
assignee is a reference at `linear/user`, so a repository reaches the person
through `linearuserperson` and the user mirror.

## WHOOP

Package `providers.substrate.reamde.dev/whoop`. An OAuth provider
that mirrors one WHOOP member's cycles, recoveries, sleeps and workouts from
the WHOOP v2 API at `https://api.prod.whoop.com/developer`. Read only: nothing
in this closure writes back to WHOOP. The bundle's `oauth2` block carries the
endpoints and the toggle-to-scope map, so each consent requests the union the
account's enabled toggles derive: `read:cycles` for `enabledCycles`,
`read:recovery`, `read:cycles` and `read:sleep` for `enabledRecovery` (a
recovery webhook names a sleep, which `whoopwebhook` reads for its cycle),
`read:sleep`, `read:workout`, `read:profile` with `read:body_measurement` for
`enabledProfile`, and `read:profile` with `offline` on every toggle.

- **Kinds (7)**: `config` (the WHOOP OAuth app), `account` (the connection,
  and every cursor in the closure), and the mirrors `user` (the member),
  `cycle` (WHOOP's physiological day, from one sleep onset to the next,
  which WHOOP scores strain over), `recovery` (the verdict on one cycle), `sleep` (a night or a nap, which WHOOP tells apart
  with `nap`) and `workout` (a recorded activity).
- **Functions (2)**: `whoopsync` reads the member's profile and body
  measurement, then walks `/v2/cycle`, `/v2/recovery`, `/v2/activity/sleep`
  and `/v2/activity/workout` in that order (cycles first, because a recovery
  and a sleep reference a cycle). One invocation asks for one page of 25
  records and hands its checkpoint back; a page that drains its collection
  lets the same invocation start the next while its 40-second budget lasts.
  `whoopwebhook` takes one webhook WHOOP posts and rewrites or deletes the one
  mirror row it names.
- **Triggers (4)**: `whoop-on-connect` fires `whoopsync` the first time an
  account is connected, has a toggle on and has no `lastSyncedAt`;
  `whoop-on-request` fires it while `syncRequestedAt` and `syncRequestedAck`
  disagree; `whoop-scheduled` fires it hourly, and the body syncs the
  accounts due by their own `syncFrequency`. `whoop-webhook` is the webhook
  endpoint, declares WHOOP's signature, and fires `whoopwebhook`.
- **Mappings**: none shipped. The mirrors describe one member: `user` is that
  member's row, keyed on WHOOP's `user_id`, and `account.user` points at it.
  The `people` sample ships `googlecontactperson`, `googleaddressperson`,
  `githubuserperson` and `linearuserperson` and `tasks` ships
  `linearissuetask`; none maps a WHOOP kind.

**Seven scopes to enable on the WHOOP app**: `read:profile`,
`read:body_measurement`, `read:cycles`, `read:recovery`, `read:sleep`,
`read:workout` and `offline`. `offline` is what mints the refresh token, and
`read:profile` is what lets the facility read the connected member's address
off `GET /v2/user/profile/basic` into the account's `email`. `grantedScopes`
records what the last consent granted, so switching on a toggle whose scope is
not in it needs a reconnect. The `config` record holds the client id, the
sealed client secret and `apiBase`; the sync refuses to send the bearer
anywhere but `https://api.prod.whoop.com` or a loopback host.

**The five toggles choose what the walk reads.** `enabledProfile` fills the
`user` row from `GET /v2/user/profile/basic` and
`GET /v2/user/measurement/body`, the second only while `read:body_measurement`
is in `grantedScopes`. `enabledCycles`, `enabledRecovery`, `enabledSleep` and
`enabledWorkouts` each add their collection. `backfillDepth` bounds the first
window of each collection and nothing else: `none` reads from the run's own
start, `last2d`, `last30d` (the default), `last90d` and `last1y` reach that far
back from the clock of the run that computes them, and `all` reads from
`2015-01-01T00:00:00Z`, a fixed pre-API epoch that stands in for unbounded.
`syncFrequency` is `off`, `hourly` (the default) or `daily`, and the hourly
schedule passes over an account whose `lastSyncedAt` is newer than that.
`syncRequestedAt` is "sync now": stamp it and the next run acts whatever the
cadence says, then echoes the value into `syncRequestedAck`. `syncPaused` stops
the account on every path, the schedule included.

**Windows.** WHOOP's v2 API has no incremental cursor, so each collection is
read over a `[start, end]` window. The sync sends `start` and no
`end`, because WHOOP defaults `end` to now. A collection's first window starts
where `backfillDepth` says, and that floor is banked in `account.syncFloors`
until the collection drains, so a cold walk cut off part way resumes the window
it began rather than a newer one. Every later window starts at the collection's
own `account.syncCursors` entry less 48 hours, because WHOOP re-scores a cycle,
a recovery and a sleep for hours after the record first appears; every row id
is composed from WHOOP's own id, so the overlap converges instead of
duplicating. A collection that drains banks the instant its walk began as its
cursor, not the finishing invocation's clock.

**The account reports the run.** It binds core's `sync` trait
([connections](bundles.md#connections)): `syncState` is `never`, `running`,
`ok`, `erroring` or `throttled`, `syncMessage` is the one human line,
`syncProgress` is `{phase, done, total, pending}` counted in collections, and
`syncError` with `syncErrorAt` holds the last failure. `lastSyncedAt` marks the
last attempt either way; `lastCompletedAt` is written only by a walk that ran
out of work with nothing skipped and nothing abandoned. The older `syncStatus`
string stays beside them (`ok`, `ok (N skipped)`, `ok (M pending)`,
`erroring: <reason>`). A 429
puts WHOOP's `Retry-After` instant on `retryNotBefore` and reads `throttled`
rather than `erroring`; every entry path refuses to start before it. A
malformed record is skipped and counted, and a 401, a 403 or any other 4xx that
is not 408 or 429 abandons that collection for the run. `syncStreams` is
declared and left unwritten: the four collections are one function, one queue
and one outcome.

**WHOOP's own units, WHOOP's own names.** Energy is kilojoules
(`score.kilojoule` on `cycle` and `workout`), distance and height are metres
(`user.heightMeter`, `score.distanceMeter`, `score.altitudeGainMeter`), and
every duration is milliseconds (`score.stageSummary`, `score.sleepNeeded`,
`score.zoneDurations`): kcal, a total-asleep sum and a calendar day are the
reader's arithmetic. `scoreState` is an enum of `scored`, `pendingscore` and
`unscorable`, each labelled with WHOOP's own spelling, and `score` is present
only where `scoreState` is `scored`. `cycle`, `sleep` and `workout` bind
`temporal(range)`, so `at` is the record's `start` and `endsAt` its `end`; the
current cycle has no `end` and so no `endsAt`. `recovery` binds
`temporal(point)` at `created_at`, and `user` binds no temporal trait.
`sleep.sleepId` and `workout.workoutId` are v2 UUID strings, each carrying the
retired v1 integer beside it as `v1Id`; `cycle.cycleId` and `user.userId` are
integers; a v2 recovery has no id of its own, so its row is keyed on the cycle
it scores and keeps that cycle's `cycleId`, which is what the sync reads it
back by when no cycle row exists. The references are `recovery.cycle`, `recovery.sleep`,
`sleep.cycle`, and `user` and `account` on every mirror.

**Revisions and deletions.** WHOOP's `updated_at` is the only revision an
object has: the sync and the webhook never write a payload whose `updated_at`
is older than the stored row's `updatedAt`, and a payload equal to the stored
row changes nothing, so re-reading a page is silent and a rescored cycle is
rewritten. A deleted object just stops appearing in its collection. When a
collection that already has a cursor drains, `whoopsync` reads back by id
each row the mirror holds in that run's window (the 48 hours before the
cursor, and later) that the walk did not see, at most 25 per collection: a
`404` deletes the row, and any other answer keeps it. A first sync reads
nothing back, and a deletion older than the window reaches the mirror only
through the webhook.

**Webhooks.** WHOOP posts `{user_id, id, type, trace_id}` to one URL per WHOOP
app when a sleep, a recovery or a workout is written or deleted; it sends no
event for a cycle or the member. To receive them, set the app's webhook URL
in WHOOP's developer dashboard to
`https://<your-substrate-host>/webhooks/<authority>/whoop-webhook` with model
version v2. `substratectl trigger status` prints the path in its `WEBHOOK`
column.

The `whoop-webhook` trigger declares WHOOP's signature: `X-WHOOP-Signature`
is base64(HMAC-SHA256(client secret, `X-WHOOP-Signature-Timestamp` + raw
body)), and the server checks it against the `config` record's client secret
before it stores the request
([webhook signatures](functions.md#webhook-signatures)). The secret never
reaches a function. A delivery whose signature is missing or does not match,
or whose timestamp is more than five minutes from when the server received
it, is answered `404` and leaves nothing behind: the trigger carries no key,
so a refusal gets the answer a trigger that does not exist gets. The trigger
ships enabled because the check fails closed: until the `config` record holds
the client secret, every delivery is refused the same way. A verified event is
matched to the account whose `account.user` is the member row for its
`user_id`, which the first sync writes. `sleep.updated` and `workout.updated`
read the object by id and rewrite its row; a create arrives as `.updated`.
`recovery.updated` carries the sleep's id, so the body reads that sleep for
its `cycle_id` and then `GET /v2/cycle/{cycle_id}/recovery`. A `404` on any
of those reads deletes the row. `sleep.deleted` and `workout.deleted` delete
the row the id names, and `recovery.deleted` deletes the recovery whose
`sleep` reference is the named sleep. A paused account drops every event. A
toggle that is off, a missing token, a `retryNotBefore` still in the future,
a `429`, or a `401` or `403` on a read back drops an update; the output's
`skipped` names the reason. The next sync re-reads the object only if it
started within its window, the 48 hours before the collection's cursor, so a
dropped edit to an older sleep or workout does not reach the mirror. An account
connected with recoveries on before `enabledRecovery` requested `read:sleep`
has its `recovery.updated` events dropped this way until it reconnects. A
deletion needs no read and is applied whatever the toggles say.

**What it does not do.** No writeback, and no mirror carries a `json` blob, a
sync cursor or a page token. There is no `sport` kind: `workout.sportName` is
the API's string and `workout.sportId` its deprecated integer, neither looked
up from the other, and a workout names no cycle because v2 states none. WHOOP's
documented revocation is an OAuth-authenticated `DELETE /v2/user/access`, a
shape the facility cannot speak, so the bundle declares no
`revocationEndpoint`: deleting the account tears down the stored credential,
and the grant itself is revoked by hand in the WHOOP app (App & Privacy,
connected apps).

**Upgrading from version 9.** Version 11 adds the `user` and `cycle` kinds and
the `whoop-on-request` trigger, and reads WHOOP's v2 API. `recovery.cycleId`, a
string, is now `recovery.cycle`, a reference at a `cycle` row; `recovery.sleep`
and `sleep.cycle` are references v2 states on the record; every mirror gains a
`user` reference and the account gains `account.user`. `sleep`'s and
`workout`'s `start` and `end` properties are now the `temporal(range)` trait's
`at` and `endsAt`, `workout.sport` is now `sportName` and `sportId`, and
`hrvMs` is now `score.hrvRmssdMilli`. Version 11 declares no `raw: json` and
none of version 9's derived properties: `recovery.date`, `workout.calories` and
`sleep.durations.asleepMs` are gone. The account adds the `enabledProfile` and
`enabledCycles` toggles for the two new collections, and binds the core `sync`
trait beside its own bookkeeping: `syncPaused`, `syncState`, `syncMessage`,
`syncProgress`, `syncError`, `syncErrorAt`, `lastSyncStartedAt`,
`lastSyncDurationMs` and `syncStreams`.

## Garmin

Package `providers.substrate.reamde.dev/garmin`. An OAuth provider that
mirrors what Garmin's Health API and Activity API push about one Garmin
Connect user. Read only. **Not verified against Garmin**: Garmin's Connect
Developer Program was reported closed to new partners through mid-2026
(checked 2026-10-11), its API specifications sit behind its partner login,
and every field below follows Garmin's OpenAPI description of the wellness
API as republished outside Garmin. The bundle is tested against deliveries
built from that description and a mock Garmin, never a live account; the
bundle's [README](../kinds/providers.substrate.reamde.dev/garmin/README.md)
lists the sources.

- **Kinds (19)**: `config` (the Garmin app), `account` (the connection, the
  Garmin user id every push is matched on, and every cursor), `notice` (a
  disconnect or a permission change Garmin pushed), and one mirror per
  summary type Garmin pushes: `daily`, `epoch`, `sleep`, `bodycomp`,
  `stressdetail`, `usermetric`, `pulseox`, `respiration`, `healthsnapshot`,
  `hrv`, `bloodpressure`, `skintemp`, `activity`, `activitydetail`,
  `manuallyupdatedactivity` and `moveiq`.
- **Functions (2)**: `garminpush` writes one push delivery; `garminsync` records
  the user id and permissions and asks Garmin to backfill history.
- **Triggers (4)**: `garmin-webhook` fires `garminpush` for each delivery
  Garmin posts, and ships disabled; `garmin-on-connect` fires `garminsync` the
  first time an account is connected and has no `lastSyncedAt`;
  `garmin-on-request` fires it while `syncRequestedAt` and `syncRequestedAck`
  disagree; `garmin-scheduled` fires it hourly for every account with backfill
  left.
- **Mappings**: none shipped.

**Garmin pushes; the substrate does not poll.** Garmin POSTs each summary type
to the URL its app registers for it in Garmin's Endpoint Configuration tool,
as `{"<summary type>": [<summary>, ...]}`. Every summary type can share the
one URL, `https://<your-substrate-host>/webhooks/<authority>/garmin-webhook/<key>`.
Garmin signs nothing, so the trigger's key is the only credential: set
`source.webhook.key` on `garmin-webhook` to 16 to 128 characters of
`[A-Za-z0-9_-]` before enabling it. Where Garmin sends the
`garmin-client-id` header, a push naming another app's client id is refused.

**Each summary is matched to its account by `userId`.** `garminsync` reads the
user id from `GET /rest/user/id` on the first sync and commits it in an
invocation of its own, before it asks for any backfill window. A push for an
account whose first sync has not committed the id has nothing to match. A
delivery none of whose
summaries names a known user fails with an error naming the user id, and parks
for a retry; a delivery that mixes users writes the known ones and lists the
rest in its output.

**One row per `summaryId`, rewritten whole.** A row's id is
`<account id>/<summaryId>` (a hash when that would leave the id alphabet), and
each summary is one put carrying every declared property, absent ones as null.
Garmin re-sends a summary under the same id when it has more (a day filling
in, a sleep turning from tentative to final), and the re-send replaces the row:
what it no longer carries is cleared. Every field keeps Garmin's camelCase name
and Garmin's units (seconds since the epoch, metres, grams, kilocalories,
beats per minute), including the per-second sample maps, the sleep stage map
and an activity's samples. `startTimeInSeconds`, `startTimeOffsetInSeconds`
and `calendarDate` are kept as Garmin sends them; `at` and `endsAt` are the
same seconds as instants (a sleep ends at its last stage; a `bodycomp` and a
`bloodpressure` sit at `measurementTimeInSeconds`; a `usermetric` carries a date
and no instant). A ping (`callbackURL` instead of summaries) is followed with
the account's token, only to a URL under `apiBase`.

**The toggles choose what is written and backfilled.** Each summary type has
its own toggle on the account, on by default, and each maps in `featureScopes`
to the Garmin permission that covers it: `HEALTH_EXPORT` for the Health API
types, `ACTIVITY_EXPORT` for `activities`, `activityDetails` and
`manuallyUpdatedActivities`. Garmin's consent asks for no scopes (the user
picks what to share on Garmin's page), so `grantedScopes` records what the
account asked for and `account.permissions` what the user granted, read from
`GET /rest/user/permissions` and updated by each permission change Garmin
pushes. A push for a type whose toggle is off, or for a paused account, is not
written, and Garmin does not send it again.

**Backfill asks; the data arrives by push.** `garminsync` asks for every enabled
type except `manuallyUpdatedActivities` (Garmin offers no backfill for it), one
24-hour window per request, the newest day first across every type, back to
`backfillDepth`: `none`, `last7d`, `last30d` (the default) or `last90d`, counted
from `backfillUntil`, the instant of the first sync. Garmin answers `202` and
pushes the summaries later; it answers `409` to a window it already accepted,
which counts as accepted too. `syncCursors` keeps, per type, the start of the
oldest accepted window, so a run cut off by its 100-second budget, a `429` or an
outage continues on the next hourly run, and a deeper `backfillDepth` extends
the walk. A `429` holds the account until Garmin's `Retry-After` and reads
`throttled`; a `4xx` on one type (a `403` for a permission the user did not
grant) stops that type and names it in `syncMessage` while the others go on.
The refusal stays in `backfillRefused`, and the type is not asked for again
until Garmin pushes a permission change dated after it or the owner asks for a
sync now, so a refused type does not keep the account due every hour.
Garmin is reported to backfill 30 or 90 days per request and only within about
a month of the first connection, by sources that disagree.

**The account reports both paths.** It binds core's `sync` trait. A push stamps
`lastSyncedAt`, `lastPushAt`, `syncState: ok` and a `syncMessage` counting what
it wrote; a backfill run stamps the same with `syncProgress` counted in summary
types, and `lastCompletedAt` once every enabled type reached its floor with
nothing refused. A disconnect Garmin pushes lands as a `notice`, sets
`deregisteredAt` and reads `erroring`; the schedule passes over the account
until a push of data, or a "sync now" that reaches Garmin, clears the mark.

**What it does not do.** Garmin's disconnect is `DELETE
/rest/user/registration` under the user's token, which the OAuth facility
cannot send, so the bundle declares no `revocationEndpoint` and deleting the
account here does not deregister it at Garmin: disconnect the app in Garmin
Connect as well. No activity files (FIT), no solar intensity, blood glucose,
menstrual cycle or pregnancy summaries, and no writeback. The webhook door
refuses an inline body over 8 MiB (`413`), and `garminpush` skips (and names
in its output) a single summary over 6 MiB, because no function frame holds
one. At one sample a second, 1 MiB of activity details is about 50 minutes, so
an activity of up to about five hours (a marathon, for example) arrives whole
by push. A longer one does not: `garminpush` skips it up to 8 MiB (about six
and a half hours) and the door refuses it past that. Ping mode avoids the
door's cap when one push carries several large activities, because a ping
carries only the callback URL; it does not avoid the 6 MiB skip. `garminpush`
writes any body the door admits over several invocations of one chain.
Garmin pushes no deletion: a summary or activity deleted in Garmin Connect
stays mirrored, and so do the health records the
[adapter](#garmin-to-health-sample) built from it. A re-send of a summary
clears the fields it no longer carries. To remove one, delete its mirror row
(`substratectl delete <kind> <id>`, the kind in full, such as
`providers.substrate.reamde.dev/garmin/daily`): the adapter's delete trigger
removes what it built.

## Google Fit

Package `providers.substrate.reamde.dev/googlefit`. An OAuth provider that
mirrors one Google account's fitness data from the Google Fit REST API v1 at
`https://www.googleapis.com/fitness/v1`. Read only: nothing in this closure
writes back to Google.

**Dated caveat.** Google announced that the Fit APIs, the REST API included,
end with 2026, and closed them to new sign-ups on 2024-05-01: a Google Cloud
project that has not enabled the Fitness API before cannot enable it now. The
Google Health API that replaces it is not onboarding new projects either. The
bundle is built against Google's published reference and tested against a
loopback fake and recordings; nothing was run against a live account.

- **Kinds (5)**: `config` (the Google OAuth client), `account` (the
  connection, its time zone and every cursor), and the mirrors `datasource`
  (one entry of `GET users/me/dataSources`, raw or derived), `dataset` (one
  data source over one UTC day, every page joined) and `session` (a workout,
  a walk or a night's sleep).
- **Functions (1)**: `googlefitsync` lists the data sources, reads every
  derived source of each enabled data type one UTC day at a time, then walks
  the session list.
- **Triggers (3)**: `googlefit-on-connect` fires `googlefitsync` the first
  time an account is connected, has a toggle on and has no `lastSyncedAt`;
  `googlefit-on-request` fires it while `syncRequestedAt` and
  `syncRequestedAck` disagree; `googlefit-scheduled` fires it every 15
  minutes, and the body syncs the accounts that are due.
- **Mappings**: none shipped. The
  [Google Fit to health](#google-fit-to-health-sample) sample turns these
  mirror rows into health observations.

**Seven toggles, one scope each**, every scope under
`https://www.googleapis.com/auth/`:

| Toggle | Scope | Data types read |
| --- | --- | --- |
| `enabledActivity` | `fitness.activity.read` | `com.google.step_count.delta`, `com.google.calories.expended`, `com.google.calories.bmr`, `com.google.activity.segment`, `com.google.active_minutes`, `com.google.heart_minutes`, and the sessions |
| `enabledBody` | `fitness.body.read` | `com.google.weight`, `com.google.height`, `com.google.body.fat.percentage` |
| `enabledHeartRate` | `fitness.heart_rate.read` | `com.google.heart_rate.bpm` |
| `enabledSleep` | `fitness.sleep.read` | `com.google.sleep.segment`, and the sessions |
| `enabledOxygenSaturation` | `fitness.oxygen_saturation.read` | `com.google.oxygen_saturation` |
| `enabledBloodPressure` | `fitness.blood_pressure.read` | `com.google.blood_pressure` |
| `enabledBloodGlucose` | `fitness.blood_glucose.read` | `com.google.blood_glucose` |

Every toggle also asks for `userinfo.email`, which is how the facility fills
the account's `email`. Location is not requested: `fitness.location.read` also
grants GPS samples, and Google files `com.google.distance.delta` under it, so
the bundle reads no distance. The health scopes need Google's verification
before a published client may ask for them. The bundle declares Google's
RFC 7009 `revocationEndpoint`, so deleting the account revokes the grant.

**Which streams are read.** Every data source whose `type` is `derived` and
whose data type a toggle covers. Google merges each raw device stream into
those, and each derived point names its raw writer in `originDataSourceId`,
so reading the raw streams too would store every point twice. Every listed
source, raw ones included, gets a `datasource` row, which is where a point's
origin resolves to a device.

**One row per data source per UTC day.** A `dataset` id is
`<account id>/<data source id>/<YYYY-MM-DD>`, a `datasource` id is
`<account id>/<data source id>` and a `session` id is
`<account id>/<session id>`. The account comes first because a derived
stream's id is the same for every Google user. Where a data source or session
id leaves the record id alphabet (a space in a device model, the `<` in
`resting_heart_rate<-merge_heart_rate_bpm`) that segment is
`host.ids.external("googlefit", <account id>, <id>)`, and so is a whole id past
128 bytes. `at` and `endsAt` are the day's bounds. The row holds every point
that started on the day, sorted by end time: Fit answers every point that
overlaps a range, so a sleep segment crossing midnight comes back on both
days, and the later day's row leaves it to the earlier one. Every int64 stays
Google's decimal string (`startTimeNanos`, `endTimeNanos`,
`modifiedTimeMillis`). `value[]` keeps its positions; an empty `mapVal` is not
stored, and a map entry's `value.fpVal` sits beside its `key` because a
declaration nests four levels at most. A day with more than 20,000 points is
skipped and counted rather than written short.

**Cursors and re-reads.** `syncCursors` holds, per data type, when the last
walk that reached today began, and the next walk starts two UTC days before
it, so a late upload from a watch lands. A re-read day whose points changed
is rewritten in one put, an unchanged day is a no-op, and a day that comes
back empty deletes its row. An edit to a day older than the overlap is not
seen. The first walk starts where `backfillDepth` says: `none` is today's UTC
day, and `last2d`, `last30d` (the default), `last90d` and `last1y` reach that
far back. There is no `all`, because every day is one request per data
source. A walk the two-minute drain budget cuts off saves the day it reached
in `syncFloors`, and its `syncMessage` reads `ok (N pending)`; the 15-minute
schedule treats it as due, so the tick rate is the backfill rate. A run drops
the saved day of a data type it does not walk, because its toggle is off or
Google refused it. Once nothing is left over, the account syncs at its
`syncFrequency`: `off`, `hourly` (the default) or `daily`.

**Sessions** come from Google's own sync token, kept in
`syncTokens.sessions`. With no token the list answers what changed in the
last 30 days, page by page, until a page names nothing, and that page's token
is where the next run starts. A deeper first backfill (`last90d`, `last1y`)
reads its window with `startTime` and `endTime` first. On the first walk a
session that ended before the backfill floor is not written, and a session
the list reports in `deletedSession` is deleted here.

**Time zones.** Fit stores no UTC offset on any point or session. The
account's owner-set `timezone`, an IANA name, records where the data was
recorded; the sync never reads it, and a reader that needs local days uses
it.

**The account reports the run.** It binds core's `sync` trait
([connections](bundles.md#connections)) like WHOOP's: `syncState`,
`syncMessage`, `syncProgress` counted in data types plus the session list,
and `syncError` with `syncErrorAt`. A 429 puts Google's `Retry-After` instant
on `retryNotBefore` and reads `throttled`. A 401 fails the whole account as
`erroring`. A 403 abandons that data type for the run, which records it in
`syncMessage`, and a 404 on one data source drops that source for the run.
`lastCompletedAt` is written only by a run that reached today with nothing
left over, skipped or failed.

**What it does not do.** No writeback, no distance or location, and no
nutrition, body temperature or reproductive health. It calls neither the
aggregate read (`POST dataset:aggregate`) nor the change feed
(`dataPointChanges`), whose behaviour on derived streams Google does not
document.

## Notion

Package `providers.substrate.reamde.dev/notion`. A provider that mirrors one
Notion workspace: its people, the pages and data sources shared with the
connection its token belongs to, the databases those data sources hang off, and
the blocks the pages are made of. It is authorized by an internal token
(`ntn_…`, the one a workspace owner mints in Notion's settings) pasted onto
the `config` record's `integrationToken` rather than by OAuth: an internal
token has no consent flow, and Notion's public OAuth exchange authenticates
with HTTP Basic, which the host facility does not speak. The token is
origin-pinned. The sync body refuses to send it anywhere but
`https://api.notion.com` or a loopback host, and `api.notion.com`, `127.0.0.1`
and `localhost` are the only entries in the function's `network` permission.
Read only: nothing here writes back to Notion.

- **Kinds (8)**: `config` (the pasted token and the API origin it may reach),
  `account` (one connected workspace: the owner's toggles, the cursors and the
  backlog), the mirrors `user` (a Notion person or bot), `page` (a page, which
  is also how Notion models a row of a data source), `datasource` (a database's
  schema and the set its rows belong to), `database` (the container its data
  sources hang off), `block` (one piece of a page's content: one kind carrying
  34 block-type payload properties), and `pagesync` (connector state, where the
  block walk of one page got to).
- **Functions (1)**: `notionsync` does one page of one walk per invocation,
  writes the rows that page produced, checkpoints its cursor and its backlog on
  the account, and hands the drain back to the engine.
- **Triggers (3)**: `notion-on-connect` fires when an `account` carries
  `enabledPages` true and no `lastSyncedAt`; `notion-on-request` fires while
  `syncRequestedAt` differs from `syncRequestedAck`; `notion-scheduled` fires
  hourly.
- **Mappings**: none shipped. The `people` sample ships `googlecontactperson`,
  `googleaddressperson`, `githubuserperson` and `linearuserperson`, and `tasks`
  ships `linearissuetask`; none of the five reads a Notion kind. `user` is the
  kind a repository maps onto its own people, and `user.person.email` is the
  address such a mapping probes.

Setting it up takes two steps in Notion. Mint an internal token (Settings,
Connections, Develop or manage) with read capabilities (the user-email one
included, or `user.person.email` stays absent), and paste it onto the `config`
record. Then SHARE each top-level page, database or teamspace with the
connection that token belongs to: `/v1/search` returns only what has been
shared with that connection, so an unshared page is invisible to the sync
rather than refused. Leave the config's `apiBase` empty. It takes a loopback
origin as a test seam and nothing else.

The `account` record carries the owner's settings. `enabledPages` syncs the
workspace roster, the pages, the data sources and the databases, as one toggle:
a page is meaningless without the person who wrote it and the data source it is
a row of. `enabledBlocks` also walks each page's block tree; it is off by
default, because it costs at least one call per page. `syncFrequency` is `off`,
`hourly` (the default) or `daily`, and `syncPaused` stops every path, the hourly
tick included. Stamp `syncRequestedAt` with the current time to sync now,
whatever the cadence says.

**`backfillDepth` is a floor on `last_edited_time`, and it is anchored.** `all`
is NO floor, `none` is the start of the run that set it, and `last30d`,
`last90d` (the default) and `last1y` are that many days before the run that
computed the floor. The floor is stored in `account.syncCursors` beside the
depth that produced it (`floorDepth`), so the same depth keeps its floor for
ever while a changed depth recomputes once and re-anchors. That is what makes a
widened depth reach the walk: `all` stores the depth and no floor, and so clears
a narrower floor an earlier depth left behind.

**The walk is five stages, in order.** `me` reads `GET /v1/users/me` and stamps
`account.user`. `users` pages the roster. `search` makes one descending walk
over `POST /v1/search`, sorted by `last_edited_time`, which returns pages and
data sources together. `databases` makes an addressed `GET /v1/databases/{id}`
for every database a data source named, because search never returns one.
`blocks` pages `GET /v1/blocks/{id}/children` for each queued page and recurses
three levels, stopping at a `child_page` or `child_database` (the search walk
reaches that page on its own). `enabledPages` off drops the `users`, `search`
and `databases` stages; `enabledBlocks` off drops `blocks`. Every call carries
`Notion-Version: 2025-09-03`, the data-source model: under the older 2022-06-28
pin a database that grows a second data source disappears from `/v1/search`
entirely, and the mirror would lose whole databases.

**One invocation does one page of one walk**, 100 objects for search and the
roster, 100 children for one block level, and the hydrating stages check a
40-second wall-clock budget against the function's PT60S deadline. The engine
bounds a whole drain at 512 invocations and two minutes, which a cold walk of a
large workspace does not fit, so the body checkpoints as it goes: the search and
roster cursors land in `account.syncCursors`, the databases and blocks backlogs
in `account.syncPending`, and each page's own block cursor on its `pagesync`
row, where a page of blocks cannot bump the mirrored page's version. An account
carrying pending work reports itself due whatever `syncFrequency` says, so the
hourly tick is also the continuation, and a round that starts with a backlog
runs no search: it is hydration until the queue is clear.

**A delta run short-circuits twice.** The search walk descends by
`last_edited_time` and breaks at the first object below the floor, so a narrow
`backfillDepth` never scans the whole workspace; where the first page of the
walk stops on its first item the status says
`ok (backfill window admitted nothing: floor …, newest object …)` rather than a
bare zero. A page's block tree is skipped when its `pagesync.blocksEditedTime`
is at or past the page's `last_edited_time`, its `blocksCursor` is absent and
its `blocksStatus` is `ok`: Notion bumps a page's instant for any edit anywhere
in it, so nothing has changed to re-read. The sync never calls
`GET /v1/pages/{id}` or `GET /v1/data_sources/{id}`: search returns the same
object.

**The account binds core's `sync` trait, so its health reads like every other
connection's** ([connections](bundles.md#connections)). `syncState` is `ok` only
where the drain ran out of work, `running` while a round left a backlog behind,
`throttled` where Notion answered 429 (the instant from its `Retry-After` lands
on `retryNotBefore` and holds every path, "sync now" included, until it passes),
and `erroring` on a failure, which also writes `syncError` and `syncErrorAt`.
`syncMessage` is the one human line, the sentence the older `syncStatus` carries
without its `erroring:` prefix; both truncate at 400 characters. `syncProgress`
is `{phase, done, total, pending}` over the run's own queue. `syncStreams` is
declared and left unwritten: one function walks one workspace as one stream. A
database or page Notion refuses permanently (`object_not_found`,
`restricted_resource`) is counted on the status as `unreachable: N` with the
first few named, and a block failure lands on that page's own
`pagesync.blocksStatus`, so one unreadable page never stops the rest.

**What it does not do.** No writeback. No comments: the closure declares no
comment kind, and the `comment_id` a block mention carries stays a string. No
deletion reconciliation: `/v1/search` never returns a trashed page, so a page
deleted in Notion stops appearing and nothing removes its mirror. No workspace
kind, because Notion publishes no workspace resource; the workspace's name and
id arrive on the token's own `bot` object. The `accountconfig` trait's
`tokenRef`, `tokenStatus` and `grantedScopes` are declared and stay unwritten,
since no OAuth flow runs here.

**Upgrading from version 9.** Version 11 installs eight kinds where 9 installed
four: `user`, `datasource`, `block` and `pagesync` are new. The sync function is
renamed from `workspacesync` to `notionsync`, and the trigger
`notion-on-request` joins `notion-on-connect` and `notion-scheduled`. `database`
changed role: version 9 wrote one `database` row per data source, and version 11
writes the schema and the rows on `datasource` and keeps `database` for the
container that data source names. The `page` kind no longer declares `content`
or `pendingParent`: block text lives on `block` rows, and a parent is a
reference composed from the id the payload carries. On the account,
`enabledDatabases` is gone and `enabledBlocks` is new, and the kind binds the
core `sync` trait beside `accountconfig`: `syncRequestedAt`, `syncRequestedAck`
and `lastSyncedAt` keep the names and datatypes they already had, and
`syncPaused`, `syncState`, `syncMessage`, `lastSyncStartedAt`,
`lastSyncDurationMs`, `syncProgress`, `syncError`, `syncErrorAt` and
`syncStreams` are new. Version 9 synced one account per repository and stamped
the rest `ignored: duplicate account`; version 11 syncs every enabled account
that is due.

## Beeper

Package `providers.substrate.reamde.dev/beeper`. A provider that mirrors one
machine's Beeper inbox: the networks it bridges, the logins on them, the
people, the chats and the messages in them. It authenticates with a token
pasted onto a `config` record and declares no `oauth2` block, because Beeper
Desktop serves its own OAuth flow on loopback and the host facility has no
browser to send anywhere. The sync reads only: eight GET routes and no media.
`sendmessage` is the one write.

- **Kinds (9)**: `config`, `account`, and the mirrors `bridge` (one network
  this Beeper can run), `chataccount` (one login on one bridged network),
  `user` (one person Beeper knows, keyed on a Matrix MXID), `label` (one label
  the owner puts on a chat), `chat` (one conversation, whose id is a Matrix
  room id) and `message` (one message, `timestamp` bound as its instant),
  beside the state kind `chatsync` (this connector's cursors for one chat).
- **Functions (2)**: `messagessync` makes at most one Beeper HTTP call per
  invocation and pages the rest off its own cursor, through eight phases:
  `info`, `accounts`, `bridges`, `labels`, `contacts`, `chats`, `whole` (the
  authoritative `GET /v1/chats/{id}` read, with `maxParticipantCount=-1`, which
  is the only one that returns every participant) and `messages`.
  `sendmessage` sends one text message to a chat, optionally as a reply; see
  [writing to Beeper](#writing-to-beeper).
- **Triggers (3)**: `beeper-messages-on-connect` fires when an account carries
  `enabledMessages` and no `lastSyncedAt`; `beeper-messages-scheduled` fires
  every 15 minutes and walks the enabled account when it is due;
  `beeper-messages-on-demand` fires while the owner's `syncRequestedAt`
  differs from `syncRequestedAck`.
- **Mappings**: none shipped. `user` is the kind a repository's person mapping
  would probe, and it carries `fullName`, `username`, `email` and
  `phoneNumber` to probe on; it declares no person slot, so the mapping
  synthesises one (record 0096). The `people` sample ships
  `googlecontactperson`, `googleaddressperson`, `githubuserperson` and
  `linearuserperson`, and `tasks` ships `linearissuetask`: none of the five
  reads a Beeper kind.

Setting it up takes two records and no scopes. Paste the token Beeper Desktop
hands out onto a `config` record: it is a secret, redacted on read-back and
injected into this bundle's two functions. Beeper Desktop mints one token for
everything it serves, and `sendmessage` is the only function that calls a
write route with it. The
config's `apiBase` is the Beeper Desktop origin, `http://localhost:23373` by
default; the function's `permissions.network` lists `localhost` and
`127.0.0.1`, and the body refuses any origin that is not private (loopback, an
RFC 1918 address, or the CGNAT range a tailnet hands out), so an edit of
`apiBase` cannot send the token to a public host.

Then create an `account`. `enabledMessages` is the toggle that runs the walk,
one toggle for everything, because one token opens every bridged network at
once. `enabledContacts` adds each connected network's address book, one page
per invocation, which is where a person with no open chat appears.
`chatFilter` is an optional case-insensitive substring of a chat's title: a
chat whose title does not contain it is still mirrored, but its messages are
not read. `displayName` names the connection wherever accounts are listed.

`syncFrequency` is `hourly` (the default), `daily` or `off`, and it decides
when a NEW walk starts. An account with queued work is due whatever the
cadence says, and `off` still drains work already queued rather than stranding
a chat half read. `backfillDepth` bounds the first walk of each chat and
nothing else: `none` (from the run's own clock forward), `last2d`, `last30d`
(the default), `last90d`, `last1y` or `all` (no floor at all). The window is
measured back from the clock of the run that starts the walk, and a resumed
walk keeps the floor it started with. Stamp `syncRequestedAt` with the current
time to sync now, whatever the cadence says; the walk copies it to
`syncRequestedAck` once it has run out of work. `syncPaused` stops the
scheduled walk, and work already queued stays queued.

One invocation makes one Beeper call, and the drain bounds ITSELF at 75
seconds and 300 calls, inside the engine's 512 invocations and two minutes. A
cold walk of a large inbox therefore stops cleanly and leaves its queues on the
account, and because an account with pending work is due whatever its cadence
says, the 15-minute tick is also the continuation.

The cursors live in two places. The chat-list cursor, the queue of chats still
to read, the address books still to page and the failure list are the
account's `streamCursors`, opaque and rewritten whole. Each chat's own
`newestCursor`, `oldestCursor`, `backfilledThrough` and `backfillComplete` are
on its `chatsync` row, so a page of history bumps that row and never the
mirrored `chat`. A second run re-lists the chats from the top, asks the
authoritative chat read again only while the list says the roster is still
truncated, and pages each chat's messages `direction=after` from its stored
`newestCursor`, so it sees only what has arrived since. A chat whose backfill
has not reached its floor keeps paging `direction=before` instead.

The `account` binds the core `sync` trait beside `accountconfig`, and that is
where failures are reported. `syncState` is `ok`, `erroring` or `throttled`
(`running` belongs to the dispatcher, and a bounded drain is `ok` with the
count in `syncProgress.pending`), `syncMessage` carries the same sentence as
`syncStatus`, and `syncStreams` breaks the run down into `chats` and
`messages`, each with its own state, message, `pending` count and `lastAt`.
A 429's `Retry-After` sets `retryNotBefore` and holds the whole account until
it passes. One chat that will not read is that chat's own business: the reason
lands on its `chatsync.messagesStatus`, the queue moves on, the unit of work
goes onto `streamCursors.failed` with its attempt count (retried up to three
times, and queued again at the top of the next walk), and `syncStatus` gains a
trailing `[N failed]`. One account per repository syncs, since one token
addresses one Beeper Desktop: the lexicographically first account with
`enabledMessages` on is the one that walks, and every other one is stamped
`ignored: duplicate account`.

The sync never writes to Beeper. It sends no message, adds no reaction,
archives nothing and marks nothing read. It fetches no media: an attachment is mirrored
as metadata (`mimeType`, `fileName`, `fileSize`, `duration`, pixel `size` and
the `transcription` of a voice note, where Beeper made one), and its `srcUrl`
and its `mxc://` handle are never opened. It calls no search route, so a chat
the chat-list walk does not reach is invisible, and it calls none of a
bridge's capability, login-flow or login endpoints. A merged chat hides its
member chats from the chat list, so those are mirrored thin, from the
reference alone.

**Upgrading from version 10.** Version 10 mirrored a Matrix homeserver into
two kinds, `room` and `message`, each carrying a `raw` blob beside four typed
fields; version 13 mirrors the Beeper Desktop Client API, which the desktop
app serves locally. The kind `room` is removed and `chat` carries the
conversation. No mirror carries `raw`: `json` survives on
`chataccount.capabilities`, `chat.capabilities` and `message.seen`, each
saying why in its own description. `message.sender` is a reference at `user`
rather than a string, and the other relations are references too, so the API's
`…ID` fields are gone with their suffix (`chatID` is `chat`, `accountID` is
`chatAccount`, `linkedMessageID` is `linkedMessage`). The account's filter is
`chatFilter` and it matches a chat's title. The config declares `apiBase`, the
Beeper Desktop origin, in place of a homeserver URL, and the token it holds is
the one Beeper Desktop hands out. The account carries a `user` reference to
the identity the token turned out to belong to, and each chat's cursors moved
off the account onto `chatsync`.

### Writing to Beeper

`sendmessage` sends one text message to a chat as the owner, on whatever
network the chat is bridged to. It takes `chat` (the Matrix room id, a
mirrored chat's `chatId`), `text`, and an optional `replyTo`, the `messageId`
of the message it answers. It calls `POST /v1/chats/{chatID}/messages` with
the config's token and the same private-origin rule the sync holds, and
answers the `chat` and the `pendingMessageId` Beeper Desktop assigns until
the network confirms the send. It writes no record: the confirmed message
reaches the `message` mirror when the sync next reads its chat. A Beeper refusal fails the
call with Beeper's status and message. Redirects are refused rather than
followed.

## Slack

Package `providers.substrate.reamde.dev/slack`. A provider that mirrors one
Slack workspace as Slack's own object model: the workspace, its users, its
conversations (channels, group DMs and 1:1 DMs), the messages in them, the
files they share and the bots that post. It is authorized by a pasted Slack
user token on the `config` record rather than OAuth, and the bundle document
declares no `oauth2:` block: the host's consent URL emits `scope=`, which
Slack reads as the bot scope set, and a bot token sees neither DMs nor group
chats. The sync never posts, reacts or joins; `postmessage` is the one
write.

- **Kinds (9)**: `config`, `account`, the mirrors `team`, `user`,
  `conversation`, `message`, `file` and `bot`, and `conversationsync`, the
  connector's own per-conversation cursor row.
- **Functions (2)**: `slacksync` makes at most one Slack Web API call per
  invocation, writes what that page carried, and hands the rest of the queue
  to the next invocation. Its phases run in this order: `auth.test`,
  `team.info`, `users.list`, `conversations.list`, `conversations.info`,
  `conversations.history`, `conversations.replies`, `users.info`,
  `files.info`, `bots.info`, `conversations.members`. `postmessage` posts one
  message, at the top level or in a thread; see
  [writing to Slack](#writing-to-slack).
- **Triggers (3)**: `slack-messages-on-connect` fires on an account whose
  `enabledMessages` is true and which carries no `lastSyncedAt`;
  `slack-messages-scheduled` fires every 15 minutes and takes the account
  when it is due; `slack-messages-on-demand` fires while `syncRequestedAt`
  differs from `syncRequestedAck`.
- **Mappings**: none shipped. `user` is the identity kind a repository would
  map onto a person, and `profile.email` is the property a mapping matches
  on.

The token is a secret on the `config` record, origin-pinned: the sync sends
it to `https` on a `slack.com` host or to loopback, and an `apiBase` naming
any other origin refuses to sync before a request is made. Only one account
per repository syncs: the lexicographically first live account is the
connection, and every other live row is stamped
`syncStatus: ignored: duplicate account`.

Setting it up is one paste. Mint a Slack user token (`xoxp-…`) and put it on
`config.userToken`, not on the account: the engine seals an accountconfig's
secrets at rest and the runner injects account properties as stored, so a
token pasted on the account reaches the sync body as ciphertext. The sync
names one scope requirement, `users:read.email`, without which a user
mirror's `profile.email` is absent (it is also absent for most bots).
`postmessage` adds a second, `chat:write` (see
[Writing to Slack](#writing-to-slack)).

The account carries the toggles. `enabledMessages` turns the sync on and
covers people, conversations and messages together. `enabledMembers` adds one
`conversations.members` call per channel and is off by default.
`conversationFilter` is a case-insensitive substring a conversation's name
must contain for its MESSAGES to sync; every conversation is mirrored either
way. `syncPaused` holds the scheduled walk, and a drain already queued stays
queued.

`syncFrequency` is `off`, `hourly` (the default) or `daily`. Under `off` no
new walk starts and work already queued still drains to the end. Unfinished
work outranks the cadence: an account holding a queue reports itself due at
every tick, so a cold sync converges over consecutive ticks rather than
waiting out the cadence between bites. Stamp `syncRequestedAt` with the
current time for a sync now; `syncRequestedAck` equals it once a walk has run
out of work.

`backfillDepth` bounds the FIRST walk only, and adds `last2d` to the usual
`none`, `last30d` (the default), `last90d`, `last1y` and `all`. The floor is
computed once from the clock when the cold walk starts and carried in
`streamCursors`, so a drain that stopped on its own bounds resumes the window
it was in. It scopes history roots and not replies: a thread parent inside
the window brings its whole thread. `lastCompletedAt`, not `lastSyncedAt`, is
what says a walk ran out of work, so a bounded drain is still a first sync.

Slack has no workspace-wide change feed, so the history cursor is per
conversation, on a `conversationsync` row that cascades from the
conversation. It holds `latestTs` (the newest message ts mirrored; the next
run asks `conversations.history` for `oldest=` that value less a day, and
the re-read day stops at the backfill floor),
`repliesTs` (the newest reply ts from any thread there),
`threadWatch` (the ts of every thread parent the conversation has shown,
newest first, capped at 200), `membersSyncedAt` (rosters are walked at most
daily) and `historyStatus`. Keeping them off the conversation means a page of
history bumps the state row's version and never the mirrored row's. A
conversation's `latestTs` moves only after its messages have committed, so a
crash re-reads a window and the repeated writes are absorbed.

The thread walk descends into a parent whose `latest_reply` is newer than
`repliesTs` less a 30-minute margin, and an incremental walk re-asks every
watched thread: an incremental history page never returns a parent more than
a day older than `latestTs`, so a reply to one would otherwise be invisible.
The two go on separate queues in `streamCursors`: `newReplies` for parents
known to have a new reply, `threads` for watch re-checks and retries, and the
walk drains `newReplies` first. A thread already on either queue is not
queued again. A parent's `latestReplyAt` is the instant of its
`latest_reply`, so a reader can see a thread has a newer reply before the
walk has fetched it.
The day of overlap is what finds a FIRST reply: Slack never lists it in
`conversations.history` and the watch holds only parents seen with replies,
so the re-read parent's `reply_count` is the one place it shows. A first
reply to a message more than a day older than the conversation's newest one
is still missed. A `has_more` page
with no cursor continues by window, `latest` at the oldest ts that page
carried, rather than being read as the end of the history.

The drain bounds itself at 75 seconds and 300 Slack calls, inside the
engine's 512 invocations and two minutes. What it did not reach goes to the
account's `streamCursors` (where the `users.list` and `conversations.list`
walks stopped, and the queues of conversations, threads, users, files, bots
and rosters), and `syncStatus` reads `ok (N pending: …)`. A run that resumes
a walk past `history` spends its first 25 seconds and 100 calls on new
history for every conversation, then walks any queued threads before files,
bots and rosters. Every invocation is one Slack call at most, so one run
drains at most about 200 queued items: a backlog of thousands of files takes
many runs. The file, bot and user queues step over a row that its own
endpoint (or `users.list`) already filled in without a call, and a file whose
embed says it was deleted, is past the plan's history limit or is not
readable by the owner is never asked for.

`message_changed` and `message_deleted` are mutation envelopes and are never
mirrored as rows. A change lands on the nested message's own identity, and
`message_deleted` deletes the message its `deleted_ts` names: `parent`, `root`
and `latestReply` are references without `mustExist`, so the hole a delete
leaves is legal. Every other subtype is a message. Joins, topic and purpose
changes, pins and archives are all persisted by `conversations.history` and
all mirrored on the one `message` kind, with `subtype` a string because Slack
extends the list without notice.

The account binds the core `sync` trait, so every run writes `syncState`
(`never`, `running`, `ok`, `erroring` or `throttled`), `syncMessage`,
`syncProgress` and `syncStreams` (`users`, `conversations` and `messages`,
each with its own state, message and pending count) beside the older
`syncStatus`, `lastSyncedAt` and `lastCompletedAt`. `syncError` and
`syncErrorAt` hold the last error text and when it was written; a later run
that ends `ok`, scheduled or record-triggered, clears them, and `syncState` is
what says it is over. A 429 puts
Slack's `Retry-After` on `retryNotBefore`, which the sync honours for every
call including `auth.test`, and reports `throttled`. Work that fails
transiently keeps its attempt count, is re-queued at the top of the next
walk, and after three attempts shows on the status as `[N failed]`. One
conversation's failure is that conversation's: the Slack error
(`not_in_channel`, `channel_not_found`) lands on its
`conversationsync.historyStatus` and the queue moves on. An error that says
the target itself cannot be read (`channel_not_found`, `not_in_channel`,
`is_archived`, `missing_scope`, `thread_not_found`, `file_not_found`,
`bot_not_found`, `user_not_found` and a few more) is a refusal: it goes to
`streamCursors.refused`, the status shows `[N refused]`, and no later walk
asks for that conversation or item again until its `conversations.list`
entry changes, the `config` record is edited (a new token), or a walk starts
with no `lastCompletedAt`. A failure is logged once, by the run it happened
in.

The sync writes nothing back. It spends the token on reads only, and a file's
bytes are never fetched: the `file` row holds what `files.info` returns and
the URLs Slack sends. A conversation the token's user is not a member of is
mirrored as a `conversation` row, but its history is not walked, and neither
is an archived one's. Rosters are walked only with `enabledMembers` on and
only for channels, never for DMs or group DMs. A file re-shared into a new
channel after its row was filled in is not read again, so its `shares` and
`channels` age.

### Writing to Slack

`postmessage` posts one message as the token's user. It takes `channel` (a
mirrored conversation's `conversationId`), `text`, and an optional `threadTs`,
the `ts` of the thread's parent, to reply in that thread. It calls
`chat.postMessage` with the config's token, under the same origin pin as the
sync, and answers the `channel` and the new message's `ts`. The pasted token
needs Slack's `chat:write` user scope; without it Slack answers
`missing_scope`, and every refusal (`not_in_channel`, `channel_not_found`)
fails the call with Slack's own error code. It writes no record: the message
reaches the `message` mirror only when the sync reads it back. A top-level
post is read on the next sync. A reply is read when the sync already
watches its thread, which it does for a parent it saw with replies. The next
sync reads a first reply when its parent is within a day of the
conversation's newest message, and misses it otherwise.
Redirects are refused rather than followed, so the origin pin holds for the
one request made.

## Apple Health

Package `providers.substrate.reamde.dev/applehealth`. A receiver for what the
Health Auto Export iOS app sends from the phone: Apple Health has no cloud
API, so the app's REST API automation POSTs its JSON export to a webhook and
the bundle writes it as mirror rows, in the app's field names and units.
Nothing here calls out and there is no OAuth.

- **Kinds (10)**: `account` (one phone, created by hand, carrying its
  toggles and the `sync` trait), `export` (the receipt for one delivery), and
  the mirrors `metricbatch` (one metric over one period), `workout`,
  `stateofmind`, `medication`, `symptom`, `cycleevent` (one cycle tracking
  entry), `ecg` and `heartratenotification`.
- **Functions (1)**: `applehealthingest` reads one delivery, resolves its
  account, writes the mirror rows and the receipt, and stamps the account.
- **Triggers (1)**: `applehealth-webhook`, shipped disabled and without a
  key.
- **Mappings**: none. The `health` sample's adapter reads these mirrors.

**Setting it up.** Install it, create an `account` per phone with an id of
your choosing (`iphone`), then set `source.webhook.key` (16 to 128 characters
of `[A-Za-z0-9_-]`) and `enabled: true` on the `applehealth-webhook` trigger:
an enabled trigger with no key would write health records for anyone who
reaches the server. In the app's automation, the URL is
`https://<your-substrate-host>/webhooks/<authority>/applehealth-webhook`
(`substratectl trigger status` prints the path), the headers are
`Authorization: Bearer <key>` and `X-Substrate-Account: <account id>`, the
format is JSON at Export Version 2, the date range Since Last Sync, Summarize
Data on with a Time Grouping of Minutes or coarser, and Batch Requests on.
The door reads the key and strips `Authorization` before the function runs;
the trigger declares `x-substrate-account` and the five headers the app adds
itself (`automation-name`, `automation-id`, `automation-aggregation`,
`automation-period`, `session-id`), which is what makes them reach the
function ([functions](functions.md#triggers)). With one account the
`X-Substrate-Account` header may be left out.

**What a delivery writes.** Each metric's rows land in `metricbatch` records
keyed `<account>/<metric>/<grouping>/<period>`, where the grouping is the
`automation-aggregation` header and the period is the UTC hour at seconds or
minutes, the UTC day at hours, the UTC month at days and the UTC year above.
A row is keyed by its instant and never by `source`, because a correction
sends the same bucket again under another label: a later delivery replaces
the batch's rows inside the span it covers for that metric (its first to its
last row), keeps the rest, and leaves a batch whose rows did not change
unwritten. A batch inside that span the delivery carries no row for is read
back and deleted, so a sample deleted in Health leaves the mirror with the
next export that spans it; a delivery reads back at most 1000 such periods per
metric, six weeks of hour batches. A change of units replaces the batch whole.
A record deleted here is absent to later deliveries: one that carries it again
writes it again, without the rows the deleted batch held. A workout and a state of mind entry are keyed by
their HealthKit id; a medication dose, a symptom, a cycle tracking entry, an
ECG and a heart rate notification, which carry none, by their name and
start. The `export` receipt, at the webhook fire id, holds the automation's
headers, the body's size and SHA-256 (the body itself is not kept), the
count per section, the metric names, the time span covered, the sections a
toggle dropped, the fields no declaration has a place for, and how many
records the delivery wrote (`written`), left as they were (`unchanged`) and
deleted (`deleted`, the batches its spans emptied). Every mirror
names the receipt of the delivery that last changed it in `export` and its
arrival in `receivedAt`.

**The app's own words.** Units are the app's Unit Preferences, verbatim
(`count/min`, `kJ`, `kcal/hr·kg`). Every date stays the string the app wrote
(`2026-10-07 08:05:37 +0300`; the offset is the phone's zone when it sent,
not where the sample was taken), and `at` and `endsAt` hold the instants; a
date without an offset is read in the account's `timezone`, else UTC.
`source` is Health's name for the device: a Garmin watch's data arrives as
`Garmin Connect`, a bucket fed by several sources joins them with `|`, and
app 10.0.4 sends merged series with an empty `source`. The app's `Min`,
`Avg` and `Max` are the fields `min`, `avg` and `max`. Workouts carry no
`source`; their `heartRateData` and series rows do.

**Bounds and failures.** A non-multipart webhook body may be 8 MiB
([API](api.md#webhooks)); the function writes a large delivery over several
pages so its own response stays under the 8 MiB frame. One record bigger than
6 MiB as written (a workout route of more than about 30,000 points) fits no
frame: it is skipped, and the receipt's `oversized` and the function's output
name it. A full-history export
is hundreds of megabytes and is not supported by push. The app skips a part
answered with anything but 2xx, or not answered in 60 seconds, and never sends
it again in that run. A delivery the function refuses after the door's `202`
(no account, two accounts and no header, a CSV body, a body that is not the
app's JSON, a version 1 workout) parks on the trigger with its body kept, and
`substratectl trigger retry applehealth-webhook <parked-id>` writes it once
the cause is fixed. A section whose toggle is off, and every delivery while
`syncPaused` is true, is dropped unwritten. Since Last Sync misses samples the
phone writes back-dated (a resting heart rate, another app's backfill); a
manual Previous 7 Days export catches them up, and overlapping deliveries
rewrite the same records. The app sends no deletion. A sample deleted on the
phone drops out of a metric batch only when a later export re-sends the
batch's surviving samples on both sides of it; one at the start or end of
what a delivery covers, a lone weigh-in, an emptied hour, and a deleted
workout or other id-keyed record stay mirrored, and so do the health records the
[adapter](#apple-health-to-health-sample) built from them. To remove one,
delete its mirror row (`substratectl delete <kind> <id>`, the kind in full,
such as `providers.substrate.reamde.dev/applehealth/metricbatch`): the
adapter's delete trigger removes what it built. The provider's
[README](../kinds/providers.substrate.reamde.dev/applehealth/README.md) has
the setup as steps.

## Apple Health to health (sample)

Turns [Apple Health](#apple-health) mirror rows into
[observations](observations.md), traces and health records under the
repository's own authority. Package `samples.substrate.reamde.dev/healthapple`
requires `observations`, `health` and
`providers.substrate.reamde.dev/applehealth`, each with a `requiresAtLeast`
floor of 1.

- **Kinds (0)**: it writes the `observations` and `health` kinds.
- **Functions (1)**: `adaptapplehealth` reads one mirror row and writes
  everything that row implies.
- **Triggers (1)**: `healthapple-on-mirror`, on the provider's `metricbatch`
  and `workout` kinds, for `create`, `update` and `delete`, coalesced.

**What it writes.** A Health Auto Export metric maps to a `health/` metric
or is skipped and named in the function's `skipped` result, never guessed.
Values are converted into the metric's unit through the engine's unit table
(`host.units`), which reads HealthKit's spellings (`lb`, `mi`, `hr`,
`count/min`, `bpm`, `mmol<180.1558800000541>/L` and the rest) as
[aliases](units.md#aliases). The adapter maps two spellings itself: `ft` is
a foot, which the table reads as the femtotonne, and `st` a stone, which UCUM
spells as the stere. The app's `count` is the metric's own counted unit,
which the table refuses as a bare spelling.

| App metric | Metric | Written as | Unit |
| --- | --- | --- | --- |
| `heart_rate` | `heart_rate` | gauge trace of `Avg`, hold `PT4M` | `/min` |
| `respiratory_rate` | `respiratory_rate` | gauge trace, hold `PT30M` | `/min` |
| `blood_oxygen_saturation` | `spo2` | gauge trace, hold `PT20M` | `%` |
| `step_count` | `steps` | amount trace | `{steps}` |
| `walking_running_distance` | `distance` | amount trace | `m` |
| `flights_climbed` | `floors` | amount trace | `{floors}` |
| `active_energy` | `active_energy` | amount trace | `kcal` |
| `basal_energy_burned` | `basal_energy` | amount trace | `kcal` |
| `heart_rate_variability` | `hrv_sdnn` | gauge observation, hold `PT6H` | `ms` |
| `apple_sleeping_wrist_temperature` | `skin_temperature` | gauge observation, hold `P1D` | `Cel` |
| `vo2_max` | `vo2_max` | gauge observation | `mL/(kg.min)` |
| `weight_body_mass` | `body_mass` | gauge observation, hold `P1D` | `kg` |
| `body_fat_percentage` | `body_fat_fraction` | gauge observation, hold `P1D` | `%` |
| `body_mass_index` | `bmi` | gauge observation, hold `P1D` | `kg/m2` |
| `height` | `height` | gauge observation | `m` |
| `blood_pressure` | `systolic_blood_pressure`, `diastolic_blood_pressure` | two gauge observations, each with both as `components`, hold `PT0S` | `mm[Hg]` |
| `blood_glucose` | `blood_glucose_mass` or `blood_glucose`, by the app's unit | gauge observation, hold `PT15M`, `mealTime` as a qualifier | `mg/dL`, `mmol/L` |
| `resting_heart_rate` | `resting_heart_rate` | summary, `statistic: vendor`, `period: day` | `/min` |
| `sleep_analysis` | `sleep_stage` and a `health/sleep` | amount observations with the stage as `code` and its minutes as `value`; see Sleep below | `min` |

Everything else the app sends (`apple_exercise_time`, `physical_effort`,
`walking_asymmetry_percentage`, `lean_body_mass` and the rest) is skipped.

**Raw values come from the Seconds and Minutes groupings only.** A batch at
Hours, Days or a coarser grouping holds the app's own aggregates, so each row
becomes `summary` observations (`sum` for an amount, `avg` for a gauge, and
`avg`, `min` and `max` for heart rate) with the bucket as `period`, which a
raw read never adds to the minutes. A dense metric is one trace per source and
UTC hour, split where the UTC offset changes; an amount's minute ends in a
`null` where the next minute has no row. Every gauge carries its metric's
hold from the health catalogue, so a night's last SpO2 reading stops
standing at the hold.

**Sleep.** A sleep segment is one `amount` observation per segment
(`awake`, `light` for the app's Core, `deep`, `rem`, and `asleep` for Asleep
and Unspecified, sleep the device did not stage). The iPhone's In Bed
segment spans the night under the watches' stages, so it is the night's
`timeInBed` and no stage: summed by code, the night would count twice; the
function's `skipped` result counts it. A night is one `health/sleep`
spanning the time in bed (`inBedStart` to `inBedEnd` where the app names
them), with `timeInBed`, `latency` (from getting into bed to falling
asleep) and `isNap`. A night told by segments is written by the batch its
`sleepStart` falls in, from the night fields every segment repeats, under
the source most of that batch's stages of the night come from (the first
by name on a tie); its stage totals are the stage amounts, which the hours'
batches hold between them. An aggregated night row adds its stage
durations, `timeAsleep`, `efficiency` (time asleep over time in bed) and
one stage amount per stage over the sleep, `asleep` included; its `inBed`
total is `timeInBed`, never an `in_bed` amount. The app sends no nap flag,
so a sleep shorter than three hours that begins between 09:00 and 21:00 on
its own clock is a nap.

A workout is a `health/workout` with its sport, span, duration, distance,
`activeEnergy`, heart rate and climb, attributed to the device its heart rate
rows name. The sport is HealthKit's workout name mapped onto the workout code
set (`Traditional Strength Training` is `strength`), `other` where the name
has no value of its own, and the name itself is `sportSource`.

**Ids, sources and reconciliation.** Ids are composed: a trace is
`applehealth/<account>/<source>/<metric>/<UTC hour>`, a single value
`applehealth/<account>/<source>/<metric>/<instant>`, a summary
`.../<metric>/<period>/<instant>/<statistic>` (the period keeps an hours and
a days automation's buckets at one instant apart), a workout
`applehealth/<account>/<HealthKit id>`; an id past 128 bytes is hashed
through `host.ids.external`. The source is
the row's `source` label with the owner's name taken off (`Ada's Apple Watch`
is `apple-watch`, `Garmin Connect` is `garmin-connect`), and a label that is
empty, absent or joins several devices with `|` is `health`, the phone's
merged series; each is an `observations/source` record at
`applehealth/<account>/<source>`, minted `ifAbsent` so an owner's edit
stands. Every output names its mirror row in `origin` (`origins` on a trace)
and carries `revision`, the row's `receivedAt` in Unix seconds. Each delivery
hands the whole set the row implies to `host.observed.reconcile`, which
leaves an output that already holds it unwritten, never lowers a stored
revision, and deletes the outputs that point at the row and are no longer
implied, so a corrected redelivery rewrites the same ids at a higher revision
and a deleted row (an account deleted, say) takes its outputs with it.

**One rule for the automations.** Send each metric at one raw grouping:
a Seconds automation and a Minutes automation both carrying heart rate write
the same trace ids from two rows, and each delivery replaces the other's
points.

## Firecrawl (sample)

Package `samples.substrate.reamde.dev/firecrawl`. Not a provider: no
provider account is connected and nothing syncs. It is web search and page
scraping over the Firecrawl API, exposed as two callables an agent binds as
tools, behind an API key.

- **Kinds (1)**: `webdocument`, a scraped page kept as markdown.
- **Functions (2)**: `websearch` returns hits as title, URL, and snippet and
  writes nothing; `scrapepage` scrapes a page to markdown and upserts a
  `webdocument` at the URL's deterministic id, so re-scraping the same URL
  updates the one document.
- **Settings (2)**: a `secret` at `…/firecrawl/apiKey`, shipped empty and
  required, and a `setting` at `…/firecrawl/baseUrl`, shipped pinned at the
  Firecrawl origin. Both are ordinary records the closure ships
  ([record 0076](decisions/0076-a-bundle-ships-its-settings-as-core-setting-and-secret-records.md)),
  and the bodies read them as `config.settings.apiKey` and
  `config.settings.baseUrl`. The bundle declares no input.
- **Triggers**: none. Both functions are callables an agent or a client invokes
  directly, so the closure is `bundle.yaml` plus the two records in
  `settings.yaml`.

The API key is a core `secret` record, sealed at rest and injected only into
this bundle's two functions, and the bodies refuse any
base URL that is not the pinned Firecrawl origin or loopback, so an edit of the
`baseUrl` setting can never redirect the key. While the key is empty the bundle
carries one setup item, coded `setting`. Keys come from
[firecrawl.dev](https://www.firecrawl.dev) and look like `fc-…`; a scraped
page's markdown is capped at 24,000 characters, and `truncated: true` marks a
cut. An agent that binds `scrapepage` must name `webdocument` in its own
`permissions.writes` as well, because a tool's effective writes are its own
intersected with its caller's
([agents](agents.md#sub-agents-budgets-and-the-emit-ceiling)).

## Reading list (sample)

Package `samples.substrate.reamde.dev/readinglist`. The substrate's
shipped end-to-end conformance example: it proves that `bundle`, `kind`,
`function`, `agent`, and `trigger` declarations compose into a real
feature (pull the links out of a chat message, read and classify each page,
propose what to save and a weekly digest) with no bespoke workflow primitive.
It is the running example these pages build on, and it requires
`samples.substrate.reamde.dev/messaging`, whose messages it reads.

- **Kinds (2)**: `page` (a harvested URL and its fetched, classified content)
  and `digest` (a week's summary, written when a rollup proposal is accepted).
- **Functions (4)**: `findurls` extracts URLs from a triggering message and
  mints pending `page` records; `fetchpage` turns a pending page into markdown;
  `setclass` is the classifier's write hand; `stampdigest` writes a digest
  record directly and exists to prove the emit ceiling refuses a write outside
  it.
- **Triggers (4)**: `readinglist-findurls-on-message` runs `findurls` on a new
  conversation message, `readinglist-fetch-on-page` runs `fetchpage` on a
  pending page, `readinglist-classify-on-page` runs the `pageclassifier` agent
  on a fetched, unclassified page, and `readinglist-rollup-weekly` runs the
  `weeklyrollup` agent on a Monday schedule.
- **Agents (3)**: `pageclassifier` classifies a page and delegates to
  `curator`, which proposes adding it to the reading list; `weeklyrollup`
  queries the week's pages and proposes a digest. Both proposals travel as
  `substrate.reamde.dev/core/recordpatchrequest` records for the owner to accept. All
  three name `provider: openai`, which creation seeds, and each
  names its own `model` — what the agent does is what picks the model, not a
  tier.

Its functions are deterministic stubs, because the bundle exists to exercise
the machinery rather than talk to a provider. Its one knob is a shipped
[`setting`](bundles.md#settings) record, `denyDomains`: a
comma-separated list of URL pieces `findurls` skips, empty as shipped, which
the bodies read as `config.settings.denyDomains`. The closure also ships one
`digest` record, `latest`, with an empty `summary`: the rollup only ever
proposes a patch, and a patch of a record that is not there is refused.

## Pebble (sample)

Package `samples.substrate.reamde.dev/pebble`. Voice capture from a Pebble
Index 01 ring: the phone app POSTs each capture to a webhook, and the bundle
saves it as a `recording`, plus an `instruction` an agent turns into tasks when
the capture came from the Double click & hold gesture. It requires
`samples.substrate.reamde.dev/tasks`, which the agent writes into.

- **Kinds (2)**: `recording` (one capture: the `transcription` it heads itself
  with, `recordedAt`, `mode`, `client`, the `audio` blob digest and the app's
  `audioFilename`) and `instruction` (the agent-mode capture's `text`, pointing
  back at its recording, and the agent's one-line `agentResponse`).
- **Functions (1)**: `ingest` writes both records off one webhook fire. The id
  is the app's `X-Index-Delivery` when it sends one (with request signing on),
  else the fire id, so a retried delivery updates the same records.
- **Triggers (2)**: `pebble-webhook` receives the POST; `pebble-on-instruction`
  delivers each new instruction to the agent.
- **Agents (1)**: `assistant` reads the open tasks through the `query` host
  function, writes `samples.substrate.reamde.dev/tasks/task` records through
  `write`, and patches its receipt onto the instruction. It names `provider:
  openai`, which creation seeds: key that row.

**The endpoint** is `POST
https://<your-substrate-host>/webhooks/<authority>/pebble-webhook`, where
`<authority>` is the repository's own authority;
`substratectl trigger status` prints the path in its `WEBHOOK` column. It is
open as shipped, and setting `source.webhook.key` on the `pebble-webhook`
trigger (16 to 128 characters of `[A-Za-z0-9_-]`) makes the server require that
key as a trailing path segment, as `?key=<key>`, or as a bearer token
([functions](functions.md#triggers)). In the Index app (Settings, Webhook),
each gesture has its own URL, headers and payload mode: give both gestures
this one URL with "Send" set to Transcription only or Both, and leave "Sign
requests" off (the door checks its key; the function verifies no HMAC).

**The request** is version 1 of the app's webhook contract
(`coredevices/mobileapp`, `INDEX_WEBHOOK_API.md`): `multipart/form-data` with
`transcription` (text; absent when the mode is Recording only), `audio`
(`audio/mp4`, `<recordingId>.m4a`; absent when the mode is Transcription
only), `recordedAt` (milliseconds since the Unix epoch, as text) and `client`
(the text `ring`). The host stores the audio in the repository's blob store
before the function runs, and the function writes the digest to the
recording's `audio`. The app adds `X-Index-Webhook-Version: 1`,
`X-Index-Trigger` (`single-click-hold` for Hold & talk, `double-click-hold`
for Double click & hold, `test-event` for the app's "Send test event"),
`X-Index-Test: true` plus a `test=true` part on a test event, `X-Audio-Size`
when audio is included, and with signing on `X-Index-Delivery`,
`X-Index-Timestamp` and `X-Index-Signature`; the `pebble-webhook` trigger
declares all of them in `source.webhook.headers`, which is what makes them
reach the function ([functions](functions.md#triggers)). **The gesture
decides the mode**: `single-click-hold` saves a note,
`double-click-hold` saves the recording and writes an instruction, a test
event is acknowledged and saved nowhere, and a request with no gesture header
is saved as a note. To swap the gestures without touching the repository, add
the custom header `X-Pebble-Mode: note` or `X-Pebble-Mode: agent` to a gesture
in the app; it overrides the trigger header. Transcription alone works; the
recording then has no `audio`. One fire, mimicked by hand:

```bash
printf '' > empty.m4a
curl -s -X POST "https://<your-substrate-host>/webhooks/<authority>/pebble-webhook" \
  -H "X-Index-Webhook-Version: 1" \
  -H "X-Index-Trigger: double-click-hold" \
  -F "transcription=call the dentist tomorrow morning" \
  -F "recordedAt=$(( $(date +%s) * 1000 ))" \
  -F "client=ring" \
  -F "audio=@empty.m4a;type=audio/mp4;filename=recording.m4a"
```

## WHOOP to health (sample)

Turns [WHOOP](#whoop) mirror rows into [observations](observations.md) and
health records under the repository's own authority. WHOOP sends scored
summaries, never raw samples, so it writes no traces. Package
`samples.substrate.reamde.dev/healthwhoop` requires `health` (and so
`observations`) and the WHOOP provider at version 21 or later, the version
that deletes a mirror row WHOOP deleted.

- **Kinds**: none; it writes `observations/observation`, `observations/source`,
  `health/sleep` and `health/workout`.
- **Functions (1)**: `adaptwhoop` rebuilds every record one mirror row
  implies and hands the set to `host.observed.reconcile`, which writes what
  changed and deletes the records naming that row in `origin` that it no
  longer implies. A deleted mirror row implies nothing, so its records go with
  it. A sleep delivery rebuilds the recoveries that name the sleep, because
  a recovery's records span its night, and a cycle delivery rebuilds the
  cycle's recoveries whose sleep row is absent.
- **Triggers (1)**: `healthwhoop-on-mirror` fires `adaptwhoop` on every
  create, update and delete of a WHOOP `cycle`, `recovery`, `sleep` or
  `workout` row. It starts at the changelog's head: to convert rows the mirror
  held before the import, replay it from the start (`POST
  /api/v1/substrate.reamde.dev/core/trigger/healthwhoop-on-mirror/replay` with
  `{"from": 0}`). A replay over converted rows writes nothing.

| WHOOP field | Written as | Form | Unit |
| --- | --- | --- | --- |
| recovery `recoveryScore` | `health/whoop_recovery` | summary, `vendor`, period `sleep`, over the night it scores | `%` |
| recovery `hrvRmssdMilli` | `health/hrv_rmssd` | summary, `vendor`, period `sleep`, over the night it scores | `ms` |
| recovery `restingHeartRate` | `health/resting_heart_rate` | summary, `vendor`, period `sleep`, over the night it scores | `/min` |
| recovery `spo2Percentage` | `health/spo2` | summary, `vendor`, period `sleep`, over the night it scores | `%` |
| recovery `skinTempCelsius` | `health/skin_temperature` | summary, `vendor`, period `sleep`, over the night it scores | `Cel` |
| cycle `strain` | `health/whoop_strain` | summary, `vendor`, period `cycle` | `{score}` |
| cycle `kilojoule` | `health/energy_expended` | amount | `kJ`, never converted |
| cycle `averageHeartRate`, `maxHeartRate` | `health/heart_rate` | summary, `avg` and `max`, period `cycle` | `/min` |
| workout `averageHeartRate`, `maxHeartRate` | `health/heart_rate`, channel `workout` | summary, `avg` and `max`, period `workout`, over the workout's span | `/min` |
| sleep stage totals (awake, light, slow wave, REM) | `health/sleep_stage`, `code` `awake`, `light`, `deep`, `rem` | amount over the sleep's span | `min` |
| sleep `sleepPerformancePercentage` | `health/whoop_sleep_performance` | summary, `vendor`, period `sleep` | `%` |
| sleep `respiratoryRate` | `health/respiratory_rate` | summary, `vendor`, period `sleep` | `/min` |
| sleep | `health/sleep`: span, `isNap` (WHOOP's `nap`), `timeInBed`, the four stage durations, `timeAsleep` (light, deep and REM together), `efficiency` (WHOOP's own), `respiratoryRate`, `disturbances`, `scores.sleepPerformance` and `scores.sleepConsistency`; WHOOP sends no latency | | |
| workout | `health/workout`: span, `sport` (WHOOP's `sportName` mapped onto the workout code set, `other` where it has no value, so `weightlifting` is `strength` and `padel` is `other`) and `sportSource` (the `sportName` itself), `duration`, `distance`, `totalEnergy` (kJ, resting and active together), `averageHeartRate`, `maxHeartRate`, `elevationGain`, `strain`, `zoneDurations` `zone0` to `zone5` | | |

**Ids and spans.** Every id is composed from WHOOP's own identifiers, read off
the mirror row: `whoop/<account record id>/<cycle id>/recovery` (and `hrv`,
`resting_heart_rate`, `spo2`, `skin_temperature`, `strain`, `energy`),
`whoop/<account>/<sleep id>/sleep`, `.../<sleep id>/stage/<code>`,
`.../<sleep id>/sleep_performance` and `respiratory_rate`, and
`whoop/<account>/<workout id>/workout`; the heart rate summaries are
`heart_rate_avg` and `heart_rate_max` under the cycle id or the workout id. A
cycle's observations span the cycle and carry its id in `periodId`, so two
cycles on one date stay two; a workout's heart rate spans the workout and
carries the workout id. The current cycle has no end yet, so its span ends
at the cycle's `updatedAt` and moves as WHOOP rescores it.
A recovery's observations measure the night the recovery scores: they span
the sleep the recovery names and carry the sleep's id in `periodId`, as the
night's sleep performance and respiratory rate do. A day read with
`bucket.place` `end` therefore puts them on the wake day, the way it places
Garmin's nightly summaries, and a records-mode window over the
WHOOP night holds them. A recovery whose sleep row is absent (sleeps toggled
off, or the sleep in a later page) spans its cycle with `period` `cycle`
until the sleep row lands; one whose cycle row is absent too writes nothing.
A cycle runs from one sleep onset to the next, so a cycle's `at` is the
evening the member fell asleep, and `place` `start` files its strain and
energy under that evening's date.
Every record carries `origin` (the mirror row), `revision` (WHOOP's
`updated_at` in seconds since the epoch, for a recovery the later of its own
and its sleep's) and the UTC offset WHOOP recorded. While WHOOP is still
calibrating the member, the recovery score is written with `quality:
estimated`. The source is `whoop/<account record id>`, class `watch`, pointing
at the WHOOP account: the API names no hardware model.

**A stage total spans the whole sleep.** WHOOP reports the minutes in each
stage, not when each stage happened, so each total is one amount with its
stage in `code` beside its minutes in `value`, over the sleep's span. WHOOP's
`totalInBedTimeMilli` is every stage together, so it is the sleep's
`timeInBed` and never an `in_bed` total, which would count the night twice.

**Heart rate.** WHOOP's API has no heart rate samples, only each scored
cycle's and workout's average and maximum. Each is a `summary` with the
statistic it is, `avg` or `max`, not `vendor`. An aggregate read with `from:
summaries` or `prefer-raw` reads one statistic per row, so it names WHOOP's
with `filter.properties.statistic` (`avg` reads the averages beside the other
sources' average summaries) or splits them with `groupBy: ["statistic"]`; a
read that would pool both is refused. A raw read takes gauges alone and
shows no WHOOP heart rate. A workout's two are on the `workout` channel: the
`avg` reducer over summaries is the plain mean of the values in a bucket, so
on one stream a day's average would mix the cycle's with the run's. Group by
`channel` to read the two apart, or filter `channel` with `exists: false`
for the cycles alone.

**Skipped.** A field no metric and no health property holds is counted in the
function's output and not written: a sleep's `sleepNeeded`,
`totalNoDataTimeMilli` and `sleepCycleCount`, and a workout's
`percentRecorded` and `altitudeChangeMeter`. A cycle's `step_count` never
reaches the mirror.

## Google Fit to health (sample)

Turns [Google Fit](#google-fit) mirror rows into
[observations](observations.md), traces and health records under the
repository's own authority. Package
`samples.substrate.reamde.dev/healthgooglefit` requires `observations`,
`health` and `providers.substrate.reamde.dev/googlefit`, each with a
`requiresAtLeast` floor, so install the provider and import `health` first.

- **Kinds (0)**: it writes the `observations` and `health` kinds.
- **Functions (1)**: `adaptgooglefit` rebuilds the observed values one
  mirror row implies and deletes the ones it no longer does.
- **Triggers (1)**: `healthgooglefit-on-mirror` delivers every create, update
  and delete of a `googlefit/dataset` or `googlefit/session` row, one
  delivery per row per batch (`coalesce`).

| Fit data type | Stream read | Metric | Form and unit | Written as |
| --- | --- | --- | --- | --- |
| `com.google.heart_rate.bpm` | `merge_heart_rate_bpm` | `health/heart_rate` | gauge, `/min`, hold `PT4M` | hourly trace per device |
| `com.google.step_count.delta` | `estimated_steps` | `health/steps` | amount, `{steps}` | hourly trace per device |
| `com.google.calories.expended` | `merge_calories_expended` | `health/energy_expended` | amount, `kcal` | hourly trace per device |
| `com.google.weight` | `merge_weight` | `health/body_mass` | gauge, `kg`, hold `P1D` | an observation per reading |
| `com.google.height` | `merge_height` | `health/height` | gauge, `m` | an observation per reading |
| `com.google.body.fat.percentage` | `merged` | `health/body_fat_fraction` | gauge, `%`, hold `P1D` | an observation per reading |
| `com.google.oxygen_saturation` | `merged` | `health/spo2` | gauge, `%`, hold `PT20M` | an observation per reading |
| `com.google.blood_glucose` | `merged` | `health/blood_glucose` | gauge, `mmol/L`, hold `PT15M` | an observation per reading |
| `com.google.blood_pressure` | `merged` | `health/systolic_blood_pressure` and `health/diastolic_blood_pressure` | gauge, `mm[Hg]`, hold `PT0S` | two observations per reading, each with both as `components` |
| `com.google.sleep.segment` | `merged` | `health/sleep_stage` | amount, `min`, with `code` | an observation per segment of a sleep session |

A session with `activityType` 72 becomes a `health/sleep` with its stage
totals and `efficiency` (time asleep over time in bed); Fit sends no nap
flag, so a session shorter than three hours that begins between 09:00 and
21:00 on the account's clock is `isNap`. Any other session becomes a
`health/workout` whose `sport` is Fit's activity code mapped onto the
workout code set (100 is `yoga`, 80 `strength`, 108 and any code without a
value of its own `other`) and whose `sportSource` is the code. Fit's sleep
codes map as 1 `awake`, 2 `asleep` (sleep with no stage), 4 `light`, 5
`deep` and 6 `rem`; 3 (out of bed) and 0 are skipped and counted. A stage's
id is its session and its start, so a corrected earlier segment renames no
later one. Each data type is read from one derived stream, so a quantity Google
keeps in two views is never counted twice; the resting heart rate stream,
`merge_step_deltas`, `com.google.calories.bmr` (a rate, kcal per day),
activity segments and active and heart minutes have no catalogue metric here
and are skipped and counted. A step or energy delta keeps its end: a null
follows it in its trace at `endTimeNanos` unless the next delta starts there,
and a delta that crosses a UTC hour is one `amount` observation over its own
span instead of a point in either hour's trace.

Every output names its origin row (`origin`, or `origins` on a trace), and a
delivery rewrites that row's whole output set, deleting what the row no
longer implies, so a re-read day, an emptied day and a deleted session
converge. `revision` is the newest `modifiedTimeMillis`, in seconds, of the
points an output was built from, and a stored higher one is kept. Sources are
minted `ifAbsent`, so an owner's edit to one stands. A reading entered by
hand in the Fit app carries `recordingMethod: manual`.

**Offsets come from the account's current zone.** Fit stores no UTC offset,
so each offset comes from the account's `timezone` as it is when the value
is written, and an observation names it in a `utcOffsetRule` qualifier
(`timezone:Europe/Athens`). An account that names no zone gives no offset,
and none is written: the observed index marks such a value offsetless,
never a guessed UTC. Two costs follow. A day spent in another zone is
stamped with the account's zone, so a recorded-local read files a London
night by its Athens date. And a replay, which an adapter upgrade asks for,
rewrites every past value with the zone in force at the replay, so an owner
who moves and updates the zone moves every past Fit value onto the new
zone's days. The ids, the sources and the rest are in the sample's
[README](../samples/healthgooglefit/README.md).

## Garmin to health (sample)

Turns [Garmin](#garmin) mirror rows into [observations](observations.md),
traces and health records under the repository's own authority. Package
`samples.substrate.reamde.dev/healthgarmin` requires
`samples.substrate.reamde.dev/observations`,
`samples.substrate.reamde.dev/health` and the installed
`providers.substrate.reamde.dev/garmin`, each with a `requiresAtLeast` floor,
so import it after installing the provider and importing `health`.

- **Kinds**: none of its own; it writes the base and health kinds.
- **Functions (1)**: `adaptgarmin` rebuilds what one mirror row implies.
- **Triggers (1)**: `healthgarmin-on-mirror` delivers every create, update and
  delete of the fifteen Garmin mirror kinds that carry measurements (every
  kind but `notice` and `moveiq`), coalescing a burst on one row.

| Garmin field | Metric | Form | Unit |
| --- | --- | --- | --- |
| `daily.timeOffsetHeartRateSamples` | `heart_rate` | gauge trace, hold PT4M | `/min` |
| `daily.steps`, `distanceInMeters`, `floorsClimbed`, `activeKilocalories`, `bmrKilocalories` | `steps`, `distance`, `floors`, `active_energy`, `basal_energy` | summary, `sum`, day | `{steps}`, `m`, `{floors}`, `kcal`, `kcal` |
| `daily.restingHeartRateInBeatsPerMinute` | `resting_heart_rate` | summary, `vendor`, day | `/min` |
| `daily.averageStressLevel` | `garmin_stress` | summary, `avg`, day | `{score}` |
| `epoch.steps`, `distanceInMeters`, `activeKilocalories`, summed per slice | `steps`, `distance`, `active_energy` | amount trace | `{steps}`, `m`, `kcal` |
| `sleep` | a `health/sleep` (and one per nap, `isNap` true), with `efficiency` the time asleep over the span | | |
| `sleep.sleepLevelsMap` segments | `sleep_stage`, coded `awake`, `light`, `deep`, `rem` | amount; `quality: estimated` while `validation` is tentative; `recordingMethod` `manual` for a `MANUAL` night, else `automatic` | `min` |
| `bodycomp` weight, muscle, bone; fat, water; BMI | `body_mass`, `muscle_mass`, `bone_mass`; `body_fat_fraction`, `body_water_fraction`; `bmi` | gauge, hold P1D | `kg` (from grams); `%`; `kg/m2` |
| `bloodpressure.systolic`, `diastolic`, `pulse` | `systolic_blood_pressure`, `diastolic_blood_pressure`, `heart_rate` | gauge, the two pressures each with both as `components`, `recordingMethod` from `sourceType` | `mm[Hg]`, `/min` |
| `stressdetail` stress and Body Battery values | `garmin_stress`, `garmin_body_battery` | gauge trace, hold PT6M | `{score}` |
| `usermetric.vo2Max` | `vo2_max` | gauge at the local midnight of `calendarDate` | `mL/(kg.min)` |
| `pulseox.timeOffsetSpo2Values` | `spo2` | gauge trace, or one gauge for a spot check, hold PT20M | `%` |
| `respiration.timeOffsetEpochToBreaths` | `respiratory_rate` | gauge trace, hold PT30M | `/min` |
| `healthsnapshot` heart rate, respiration, stress, SpO2, RMSSD | `heart_rate`, `respiratory_rate`, `garmin_stress`, `spo2`, `hrv_rmssd` | gauge trace on channel `snapshot`, each metric's hold | as above, `ms` |
| `hrv.lastNightAvg`, `lastNight5MinHigh`; `hrvValues` | `hrv_rmssd` | summary `avg` and `max`, period `sleep`; gauge trace, hold PT10M | `ms` |
| `skintemp.avgDeviationCelsius` | `skin_temperature_deviation` | summary, `avg`, period `sleep`, `delta` | `K` |
| `activity` (or its `manuallyupdatedactivity` edit) | a `health/workout`: `sport` (the `activityType` mapped onto the workout code set, `other` where it has no value, so `PADDELBALL` is `other`), `sportSource` (the `activityType`), `activeEnergy` from `activeKilocalories` | | |
| `activitydetail.samples` heart rate, speed, step cadence, power | `heart_rate`, `speed`, `cadence`, `running_power` | gauge trace, each metric's hold | `/min`, `m/s`, `/min`, `W` |
| `activitydetail.samples` pedal cadence (a ride) | `cycling_cadence` | gauge trace, hold PT10S | `/min` |
| `activitydetail.samples.totalDistanceInMeters` | `distance` | amount trace, each sample's step from the one before, summing to the activity's distance | `m` |

**Traces are an hour, from one origin.** A trace covers one metric from one
source over one UTC hour, split where the UTC offset changes, and it never
claims time outside the row it came from: the last hour of one daily and the
first of the next, or two activities in one hour, are separate traces, each
named for its own start. Epochs and respiration rows are a quarter hour each,
so their traces are rebuilt per hour from every slice of that hour, and step
amounts sum the slice's activity types. Ids are
`garmin/<account id>/<source>/<metric>/<hour>` for a trace and
`garmin/<account id>/<summaryId>/<role>` for a single record (a stage segment
is `.../stage/<start second>`), hashed only where Garmin's data would leave
the id alphabet.

**Every gauge carries its metric's hold** from the health catalogue (heart
rate PT4M, HRV PT10M, SpO2 PT20M, respiration PT30M, stress and Body
Battery PT6M, an activity's speed, cadence and power PT10S, the body
measurements P1D), so a night's last reading stops standing at the hold
instead of until the next morning.

**Sources** are `garmin/<account id>/account`, named `Garmin Connect` with
class `service`, for everything Garmin pushes without naming a device
(dailies, sleeps, a scale's weigh-ins, a cuff's pressure), and
`garmin/<account id>/<deviceName>` for each device an activity names
(`forerunner265`, class `watch`). An activity that names no device (or
`unknown`) is `.../unknown-device`, so its samples never share the stream
the daily's heart rate fills, and a hand-entered one is `.../manual`. Each
points at the Garmin account and is written once, so the owner's edits to
it stand.

**Day totals and VO2 max.** The five day totals are `sum` summaries over the
day, so a `from: summaries` read's `sum` answers Garmin's total and a
`prefer-raw` read falls back to it; resting heart rate is Garmin's own figure
and stays `vendor`. A user metric carries a date and no instant: its VO2 max
is a gauge at the local midnight of `calendarDate`, taking the start and the
offset of that date's daily summary. With no daily for the date it stands at
the UTC midnight with no `utcOffset` (the index marks it offsetless), and the
daily's delivery rebuilds it at the local one.

**Reconciled per origin.** Every row written names the mirror row it came
from (`origin`, or `origins` on a trace). A delivery reads what that row
produced before and hands the new set to `host.observed.reconcile`, which
writes what changed and deletes what the row no longer implies; a deleted
mirror row takes its outputs with it. `revision` is the
mirror row's own version, which the garmin provider moves only when a re-send
changes the summary: an identical re-send writes nothing, and a fuller one
rewrites the same ids at a higher revision. A trace built from several rows
carries the highest of their versions, and never a lower one than it holds.
An edit in Garmin Connect wins over the activity it edits while the edit
lives. A deleted mirror row counts as absent: deleting the edit rebuilds the
workout from the activity, deleting the activity keeps the edit's, and the
workout is deleted once both are, whichever of the two deletes is delivered.

**What it skips, and counts.** Each delivery's output names the fields it
left unwritten: those with no catalogue metric (minimum, maximum and average
daily heart rate, intensity and stress durations, Body Battery charge and
drain, goals, fitness age, cycling VO2 max, MET and motion intensity, Body
Battery events, a snapshot's SDRR), positions and elevation samples (no
`geopoint` and no altitude metric yet; the rise between samples is not
written as `elevation_gain`, because summed raw it overstates the climb:
84.9 m against Garmin's own 21 m on the recorded run), a ride's power (the
catalogue has only running power), air temperature, Garmin's `unmeasurable`
sleep level, and a sleep's own SpO2 and respiration maps, which the Pulse Ox and
respiration summaries already carry. Move IQ events and lifecycle notices
imply no observation. Garmin sends no sleep latency, so none is written.

Next: [substratectl](substratectl.md), the command line over all of it.
