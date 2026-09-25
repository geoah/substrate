# Bundles catalog

The catalog in the binary serves seventeen bundles, in the two tiers
[decision record 0048](decisions/0048-providers-are-published-samples-are-copied.md)
draws: seven providers and ten samples. This page covers twelve of them; the
five vocabulary samples (`people`, `tasks`, `calendar`, `messaging` and
`scheduling`) are in [built-in kinds](builtin-kinds.md).

**Seven providers**: Google, GitHub, Linear, WHOOP, Notion, Beeper and Slack.
Each is a package its publisher owns, installed under
`providers.substrate.reamde.dev` and upgraded there. Every one syncs from the
provider into the repository, and none writes back.

**Five samples**: LLM, notes, the reading list, Firecrawl and Pebble. Each is
a worked example to read and copy, imported under the repository's own
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
| GitHub        | Provider | OAuth          | 15    | 1         | 3       | 0      |
| Linear        | Provider | API key        | 13    | 1         | 3       | 0      |
| WHOOP         | Provider | OAuth          | 7     | 1         | 3       | 0      |
| Notion        | Provider | Internal token | 8     | 1         | 3       | 0      |
| Beeper        | Provider | Pasted token   | 9     | 1         | 3       | 0      |
| Slack         | Provider | User token     | 9     | 1         | 3       | 0      |
| LLM           | Sample   | Key, per row   | 1     | 0         | 3       | 6      |
| Notes         | Sample   | none           | 1     | 2         | 0       | 2      |
| Firecrawl     | Sample   | API key        | 1     | 2         | 2       | 0      |
| Reading list  | Sample   | none           | 2     | 4         | 6       | 3      |
| Pebble        | Sample   | none           | 2     | 1         | 2       | 1      |

## Connecting an OAuth provider

In the console it is the four numbered steps on the provider's page, opened
from **Providers** ([console](console.md#providers)): *Add* it if it is not
there yet; *Sign-in details* asks for the client id and secret of an OAuth
client you create with the provider, and shows the callback URL to register
as that client's redirect URI; *Connect your account* asks what to bring in
and how often, and *Create and connect* opens the provider in a new tab, where
once you approve the account's first sync starts on its own. The current step
is highlighted with its one action.

Over the API and the CLI, Google, GitHub and WHOOP take the same four steps,
because the host runs the flow and the bundle only declares it
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
account's own create.

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
  one batch of 25 `messages.get`, per invocation. `synccalendar` reads one
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
are mentioned in, comment on or are review-requested on, with the reviews on
those pull requests. The host runs the flow against the endpoints the bundle
document carries and derives the scope union from the account's toggles:
`enabledUser` asks for `read:user`, and `enabledRepos`, `enabledIssues` and
`enabledPullRequests` each ask for `read:user` and `repo`. Read only: nothing in
this closure writes back to GitHub.

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
- **Functions (1)**: `githubsync`. One invocation works one stage and
  checkpoints: one search or listing page (100 items), or up to 400 hydration
  entries, stopping once 40 seconds of its 60-second deadline are spent. It
  writes only this package's own kinds, and it refuses to send the access token
  anywhere but `https://api.github.com` or a loopback host.
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
`enabledPullRequests` the pull requests the user is involved in or
review-requested on, with their reviews. GitHub's classic scopes are coarse:
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
search; `enabledPullRequests` adds the `pulls` and `pullsReview` searches and
then `pullsFetch` and `reviewsFetch`; `enabledIssues` then adds `issuesFetch`
and `graphFetch`; `enabledUser` adds `usersFetch`. The three searches are
`GET /search/issues` over `type:issue involves:<login>`,
`type:pr involves:<login>` and `type:pr review-requested:<login>`, because
`involves:` does not cover a review request.

**The watermarks and the backlog live on the account and nowhere else.**
`syncCursors` is a keyed `datetime` map with one entry per search stage
(`issues`, `pulls`, `pullsReview`), stamped with the run's start rather than the
completion clock and re-queried with a 120-second overlap, so one stage stalling
never advances another's tail. `syncPending` is the hydration backlog, keyed by
queue (`repos`, `licenses`, `pulls`, `reviews`, `issues`, `graph`, `users`),
durable because the engine bounds a drain at 512 invocations and two minutes;
`syncProgress` reports `{phase, done, total, pending}` at every checkpoint, so a
console watching a long drain sees the number fall. A run that starts with a
backlog drains the backlog first and then walks the searches in the same run.

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
`syncErrorAt` hold the last error text and when it was written, and a later good
run does not clear them. The legacy `syncStatus` string stays beside the trait
with its own prefixes (`ok`, `ok (partial: …)`, `ok (capped: …)`,
`ok (deferred: …)`, `erroring: …`). `syncStreams` is declared because the trait
contracts it and written by nobody: GitHub is one function over one account.

**What this bundle does not do**: no writeback. Deletes are not reconciled,
because GitHub's search feed carries no tombstones: a deleted issue stops
updating and its mirror stands, and a hydration read that 404s is counted as
`unreachable` on `syncStatus` rather than removing the row. There is no comments
stream, so a `comment` row exists only where an issue embeds a pinned comment,
and a pull request carries the diff counts and no commit, file or diff rows.

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
hold the last error until a newer one replaces it, and `syncProgress` is
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
`read:recovery` and `read:cycles` for `enabledRecovery`, `read:sleep`,
`read:workout`, `read:profile` with `read:body_measurement` for
`enabledProfile`, and `read:profile` with `offline` on every toggle.

- **Kinds (7)**: `config` (the WHOOP OAuth app), `account` (the connection,
  and every cursor in the closure), and the mirrors `user` (the member),
  `cycle` (the wake-to-wake day WHOOP scores strain over), `recovery` (the
  verdict on one cycle), `sleep` (a night or a nap, which WHOOP tells apart
  with `nap`) and `workout` (a recorded activity).
- **Functions (1)**: `whoopsync` reads the member's profile and body
  measurement, then walks `/v2/cycle`, `/v2/recovery`, `/v2/activity/sleep`
  and `/v2/activity/workout` in that order (cycles first, because a recovery
  and a sleep reference a cycle). One invocation asks for one page of 25
  records and hands its checkpoint back; a page that drains its collection
  lets the same invocation start the next while its 40-second budget lasts.
- **Triggers (3)**: `whoop-on-connect` fires `whoopsync` the first time an
  account is connected, has a toggle on and has no `lastSyncedAt`;
  `whoop-on-request` fires it while `syncRequestedAt` and `syncRequestedAck`
  disagree; `whoop-scheduled` fires it hourly, and the body syncs the
  accounts due by their own `syncFrequency`.
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
it scores. The references are `recovery.cycle`, `recovery.sleep`,
`sleep.cycle`, and `user` and `account` on every mirror.

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
  `enabledPages` true and no `lastSyncedAt`; `notion-on-request` fires when
  `syncRequestedAt` is newer than `lastSyncedAt`; `notion-scheduled` fires
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
browser to send anywhere. Read only: eight GET routes, nothing written back to
Beeper, and no media.

- **Kinds (9)**: `config`, `account`, and the mirrors `bridge` (one network
  this Beeper can run), `chataccount` (one login on one bridged network),
  `user` (one person Beeper knows, keyed on a Matrix MXID), `label` (one label
  the owner puts on a chat), `chat` (one conversation, whose id is a Matrix
  room id) and `message` (one message, `timestamp` bound as its instant),
  beside the state kind `chatsync` (this connector's cursors for one chat).
- **Functions (1)**: `messagessync` makes at most one Beeper HTTP call per
  invocation and pages the rest off its own cursor, through eight phases:
  `info`, `accounts`, `bridges`, `labels`, `contacts`, `chats`, `whole` (the
  authoritative `GET /v1/chats/{id}` read, with `maxParticipantCount=-1`, which
  is the only one that returns every participant) and `messages`.
- **Triggers (3)**: `beeper-messages-on-connect` fires when an account carries
  `enabledMessages` and no `lastSyncedAt`; `beeper-messages-scheduled` fires
  every 15 minutes and walks the enabled account when it is due;
  `beeper-messages-on-demand` fires when the owner stamps `syncRequestedAt`
  later than `lastSyncedAt`.
- **Mappings**: none shipped. `user` is the kind a repository's person mapping
  would probe, and it carries `fullName`, `username`, `email` and
  `phoneNumber` to probe on; it declares no person slot, so the mapping
  synthesises one (record 0096). The `people` sample ships
  `googlecontactperson`, `googleaddressperson`, `githubuserperson` and
  `linearuserperson`, and `tasks` ships `linearissuetask`: none of the five
  reads a Beeper kind.

Setting it up takes two records and no scopes. Paste the token Beeper Desktop
hands out onto a `config` record: it is a secret, redacted on read-back and
injected only into `messagessync`. Beeper Desktop mints one token for
everything it serves, and this bundle calls no write route with it. The
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

It never writes to Beeper. It sends no message, adds no reaction, archives
nothing and marks nothing read. It fetches no media: an attachment is mirrored
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

## Slack

Package `providers.substrate.reamde.dev/slack`. A provider that mirrors one
Slack workspace as Slack's own object model: the workspace, its users, its
conversations (channels, group DMs and 1:1 DMs), the messages in them, the
files they share and the bots that post. It is authorized by a pasted Slack
user token on the `config` record rather than OAuth, and the bundle document
declares no `oauth2:` block: the host's consent URL emits `scope=`, which
Slack reads as the bot scope set, and a bot token sees neither DMs nor group
chats. Read only, it never posts, reacts or joins.

- **Kinds (9)**: `config`, `account`, the mirrors `team`, `user`,
  `conversation`, `message`, `file` and `bot`, and `conversationsync`, the
  connector's own per-conversation cursor row.
- **Functions (1)**: `slacksync` makes at most one Slack Web API call per
  invocation, writes what that page carried, and hands the rest of the queue
  to the next invocation. Its phases run in this order: `auth.test`,
  `team.info`, `users.list`, `conversations.list`, `conversations.info`,
  `conversations.history`, `conversations.replies`, `users.info`,
  `files.info`, `bots.info`, `conversations.members`.
- **Triggers (3)**: `slack-messages-on-connect` fires on an account whose
  `enabledMessages` is true and which carries no `lastSyncedAt`;
  `slack-messages-scheduled` fires every 15 minutes and takes the account
  when it is due; `slack-messages-on-demand` fires when `syncRequestedAt` is
  newer than `lastSyncedAt`.
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
token pasted on the account reaches the sync body as ciphertext. The bundle
names one scope requirement, `users:read.email`, without which a user
mirror's `profile.email` is absent (it is also absent for most bots).

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
run asks `conversations.history` for `oldest=` that value, which Slack treats
as exclusive), `repliesTs` (the newest reply ts from any thread there),
`threadWatch` (the ts of every thread parent the conversation has shown,
newest first, capped at 200), `membersSyncedAt` (rosters are walked at most
daily) and `historyStatus`. Keeping them off the conversation means a page of
history bumps the state row's version and never the mirrored row's. A
conversation's `latestTs` moves only after its messages have committed, so a
crash re-reads a window and the repeated writes are absorbed.

The thread walk descends into a parent whose `latest_reply` is newer than
`repliesTs` less a 30-minute margin, and an incremental walk re-asks every
watched thread: an incremental history page never returns a parent older than
`latestTs`, so a reply to one would otherwise be invisible. A `has_more` page
with no cursor continues by window, `latest` at the oldest ts that page
carried, rather than being read as the end of the history.

The drain bounds itself at 75 seconds and 300 Slack calls, inside the
engine's 512 invocations and two minutes. What it did not reach goes to the
account's `streamCursors` (where the `users.list` and `conversations.list`
walks stopped, and the queues of conversations, threads, users, files, bots
and rosters), and `syncStatus` reads `ok (N pending: …)`.

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
`syncErrorAt` hold the last error text and when it was written, and a later
success does not clear them; `syncState` is what says it is over. A 429 puts
Slack's `Retry-After` on `retryNotBefore`, which the sync honours for every
call including `auth.test`, and reports `throttled`. Work that fails
transiently keeps its attempt count, is re-queued at the top of the next
walk, and after three attempts shows on the status as `[N failed]`. One
conversation's failure is that conversation's: the Slack error
(`not_in_channel`, `channel_not_found`) lands on its
`conversationsync.historyStatus` and the queue moves on.

Nothing is written back. The sync spends the token on reads only, and a file's
bytes are never fetched: the `file` row holds what `files.info` returns and
the URLs Slack sends. A conversation the token's user is not a member of is
mirrored as a `conversation` row, but its history is not walked, and neither
is an archived one's. Rosters are walked only with `enabledMembers` on and
only for channels, never for DMs or group DMs. A file re-shared into a new
channel after its row was filled in is not read again, so its `shares` and
`channels` age.

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

Next: [substratectl](substratectl.md), the command line over all of it.
