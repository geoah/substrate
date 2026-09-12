# Bundles catalog

The substrate ships eleven of these bundles in the binary, in the two tiers
[decision record 0048](decisions/0048-providers-are-published-samples-are-copied.md)
draws. (The vocabulary samples beside them, `people`, `tasks`, `calendar` and
the rest, are in [built-in kinds](builtin-kinds.md).)

**Six providers**: Google, GitHub, Linear, WHOOP, Notion and Beeper. Each is a
package its publisher owns, installed under
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
([0049](decisions/0049-the-owner-of-a-mappings-target-declares-it.md)), so each
provider ships mirrors with empty subject slots and the `people` and `tasks`
samples ship the six SUGGESTED MAPPINGS that fill them. An import keeps a
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
`triggers.yaml`) and the LLM sample's two provider rows are the same kind of
thing, and the Records column counts them.

| Bundle     | Tier     | Auth           | Kinds | Functions | Records | Agents |
| ------------- | -------- | -------------- | ----- | --------- | ------- | ------ |
| Google        | Provider | OAuth          | 8     | 3         | 6       | 0      |
| GitHub        | Provider | OAuth          | 6     | 1         | 2       | 0      |
| Linear        | Provider | OAuth          | 5     | 1         | 2       | 0      |
| WHOOP         | Provider | OAuth          | 5     | 1         | 2       | 0      |
| Notion        | Provider | Internal token | 4     | 1         | 2       | 0      |
| Beeper        | Provider | Pasted token   | 4     | 1         | 2       | 0      |
| LLM           | Sample   | Key, per row   | 1     | 0         | 2       | 6      |
| Notes         | Sample   | none           | 1     | 2         | 0       | 2      |
| Firecrawl     | Sample   | API key        | 1     | 2         | 2       | 0      |
| Reading list  | Sample   | none           | 2     | 4         | 5       | 3      |
| Pebble        | Sample   | none           | 2     | 1         | 2       | 1      |

## Connecting an OAuth provider

Google, GitHub, Linear and WHOOP take the same four steps, because the host
runs the flow and the bundle only declares it
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
4. **Connect** (`substratectl bundle connect`, or `POST …/oauth/start` with
   the account's id as `record`). Consent at the provider, and the callback
   stores the grant, sets `tokenStatus: connected`, and fires each enabled
   stream's on-connect trigger.

Notion and Beeper take a pasted token instead: their `config` record holds it
and there is no consent step, so step 4 is the account's own create.

**`backfillDepth` bounds the FIRST window only.** Every run after it reads
from that stream's own stored watermark instead, so the value decides how far
back the first sync reaches and nothing else: `none` is from connect time
forward, `last30d` / `last90d` / `last1y` reach that far back, and `all` is
unbounded (WHOOP reads from a fixed pre-API epoch, its stand-in for
unbounded).

Where that floor is measured from is each bundle's own, and they differ.
Google's gmail and calendar streams derive it from the backfill anchor their
first run stamped, so their reach cannot creep forward night after night
(`_floor`, `kinds/providers.substrate.reamde.dev/google/bundle.yaml`). Notion,
GitHub, Linear and Beeper compute a dated window from the clock of the run
that needs one (`_cutoff`, `_backfill_since`, `_since_floor`, `_cutoff_ms`),
so a first sync deferred by a day reaches a day later. No provider reaches
behind the connect under `none`: Notion pins it with the `backfillAnchor` its
first sync stamps, GitHub persists that run's own start, Linear takes the same
start and then its `lastSyncedAt`, and Beeper queues no history walk at all.
Google's contacts stream ignores the property altogether, because the People
sync token gives it full-plus-incremental with no window.

## LLM (sample)

Package `samples.substrate.reamde.dev/llm`. Import this bundle first if
you want to run an agent at all. A fresh substrate seeds no `llm/provider` row,
so this bundle ships the two an agent can name, correctly shaped for their
wires and deliberately keyless, plus a `scratchpad` kind to practise on and
six agents. The Anthropic row's id is `default`, which is the row every
shipped agent names, and the second row is `openai`:

- `substrate` is the one to chat with: it reads the whole graph through the
  `query` built-in, writes nothing directly, proposes every change as a
  `recordpatchrequest` the owner decides on, and asks clarifying questions
  through the `ask` built-in.
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

Importing it gives you two rows that refuse until you key them: `default` on
Anthropic's own wire and `openai` pointed at `https://api.openai.com/v1`,
re-pointable at any gateway that speaks that wire. Keying one is an ordinary record write,
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

Both agents name `provider: default`, so running them wants an `llm/provider`
row at that id — [nothing seeds one](agents.md#providers), and the LLM example
above is what ships it. Import that bundle too and key its `default` row.
Calling an agent is an API call, not a CLI verb:

```bash
curl -s -X POST "$SUBSTRATE_SERVER/api/v1/substrate.reamde.dev/core/agent/notekeeper/call" \
  -H "Authorization: Bearer $SUBSTRATE_TOKEN" -H 'Content-Type: application/json' \
  -d '{"input": {"text": "id: my-note\n\nSomething worth keeping."}}'
```

One run leaves TWO `llm/thread` rows, the root agent's and the sub-agent's own,
each with its own turn and token tallies; cost rolls up onto the root. These
manifests name models the way Anthropic's own wire does (`claude-sonnet-5`,
`claude-haiku-4-5`), which is what the shipped `default` row speaks; point that
row at a gateway instead and the model names become the gateway's aliases
(`anthropic/claude-sonnet-5`). `pricing` is keyed by the model string AS SENT,
so a model the table does not name runs uncosted
([providers](agents.md#providers)).

## Google

Package `providers.substrate.reamde.dev/google`. An OAuth provider that mirrors
a Google account's address book, mail, and calendars into the repository, in
Google's own shape.

Three independent streams share one account: contacts, gmail, and calendar.
Each has its own toggle, its own scope, its own function, its own pair of
triggers, and its own prefixed cadence anchor and cursor on the account, so one
stream erroring never stalls another. The account-level `lastSyncedAt` and
`syncStatus` stay shared: they are the rollup every connection reports, and
whichever stream finishes stamps them.

- **Kinds (8)**: `config` (the OAuth client kind, `oauth2`, named by the
  bundle's `client` input), `account` (the Connection, `accountconfig`), `contact` (the
  mirrored contact), `emailaddress` (one address, the shared people source),
  `thread` and `message` (the Gmail mirrors), `calendar` and `event` (the
  Calendar mirrors).
- **Functions (3)**: `contactssync` pages `people/me/connections`, emits
  `contact` records, and stores the People sync token for incremental runs;
  `gmailsync` drains Gmail history (or a bounded backfill window) into thread
  and message mirrors; `calendarsync` drains each calendar's events on that
  calendar's own sync token into calendar and event mirrors. All three also
  write an `emailaddress` mirror per address they see, and none of them writes
  a kind this package does not own.
- **Triggers (6)**: `google-contacts-on-connect`, `google-gmail-on-connect`,
  and `google-calendar-on-connect` fire their stream's sync the first time a
  connected account carries that toggle; `google-contacts-scheduled`,
  `google-gmail-scheduled`, and `google-calendar-scheduled` fire them hourly.
- **Mappings**: none. `contact` and `emailaddress` each carry an empty subject
  slot, and a mapping onto a person is the declaration of the package that owns
  that person (record 0049). The `people` sample ships both of them as
  SUGGESTED MAPPINGS (`googlecontactperson` and `googleaddressperson`, matching
  on the address), so importing `people` with this provider installed is what
  lands them; importing it first lands the kinds and reports the two `waiting`
  for this package, and installing this package afterwards is not enough on its
  own: import `people` again
  ([suggested mappings](bundles.md#suggested-mappings)).

**Mirrors only.** Every row this closure writes is one of its own kinds, and
the `emailaddress` mirror is the bridge to the repository's own vocabulary: one
row per address seen, keyed per account, carrying an empty subject slot.
Declare a mapping from it onto the kind you keep people in and every address
converges on one record of yours, an address-book contact and a mail sender
landing on the same one. A mapping cannot mint a message or an event, which is
why nothing here tries: a mapping's only creator is a shell mint, a bare row
with no references, and a kind like `emailmessage` declares a required
`thread`. A repository that wants its own message rows writes them from a
function of its own, reading these mirrors.

**All three scopes are wired.** `enabledContacts` maps to `contacts.readonly`,
`enabledGmail` to `gmail.readonly`, `enabledCalendar` to `calendar.readonly`,
and the facility reads the connected address off the People profile endpoint
the bundle declares. The requested union is derived per consent from the
account's enabled toggles, so turning a stream on after the grant landed needs
a reconnect. `gmail.readonly` is one of Google's restricted scopes: a published
client needs CASA verification, and an External plus Testing client has its
refresh tokens revoked after seven days.

**A recurring event carries its rule on the mirror.** The event walk keeps
Google's `singleEvents=true` expansion, so every `event` mirror is a concrete
occurrence carrying its master's `recurringEventId` and, where Google moved the
occurrence off the slot the rule produced, its `originalStartTime`. The rule
itself appears on no instance, so `calendarsync` fetches each distinct master
by id, once per delivery, and writes its `recurrence` lines verbatim onto every
instance of it. Deriving a series record from those is the repository's to do,
from a kind of its own. The account's `calendarSeries` property is deprecated
and inert.

**The three streams read `backfillDepth` differently.** Gmail and calendar
each stamp their own backfill anchor on the first run and take the window off
that stored instant, so neither reach creeps forward; contacts ignores the
property entirely, because the People sync token gives it full-plus-incremental
with no window. The calendar walk also caps its forward reach at now plus 365
days, or a recurring event exploded to 2099 would page forever. Gmail's own cap
is a quota: `messages.get` costs 20 units against a ceiling of 6,000 units per
minute per user, which is what sizes the 25-message hydrate batch and the
20-page backfill.

**What this slice does not do**: no attachment bytes (metadata and the
attachment id only), no label kind (core keeps provider label ids as plain
strings), and no writeback. The calendar stream also carries no resume state
where gmail's backfill has `gmailBackfillResume`, so a repository with very
many very large calendars can still outrun the engine's drain deadline: it
needs an account property rather than a constant, and is filed rather than
papered over.

## GitHub

Package `providers.substrate.reamde.dev/github`. An OAuth provider that mirrors the
code work you are involved in.

- **Kinds (6)**: `config`, `account`, and the mirrors `user`, `repository`,
  `issue`, `pullrequest`.
- **Functions (1)**: `githubsync` walks the connected user, reachable
  repositories, and the issues and pull requests you are involved in or
  review-requested on, one REST page per invocation with per-stage watermarks.
- **Triggers (2)**: `github-on-connect` fires `githubsync` once an account is
  connected and has a feature toggle on; `github-scheduled` fires it hourly.
- **Mappings**: none. `user` carries an empty subject slot, and a repository
  declares what fills it (record 0049). The `people` sample ships that
  declaration as a SUGGESTED MAPPING (`githubuserperson`): it matches on the
  profile's public email and maps the name, the login as the display name, and
  the union of emails. Import `people` with this provider installed and it
  lands; import it first and it is reported `waiting` for this package, and
  installing this package afterwards is not enough on its own: import `people`
  again ([suggested mappings](bundles.md#suggested-mappings)).

Deletes are not reconciled: the search feed carries no tombstones, so a
deleted issue simply stops updating and its mirror stands.

Scopes are derived per toggle (`read:user`, and `repo` for repositories, issues
and pull requests), and the facility reads the account's public email from
`GET /user` after the exchange. GitHub's classic scopes are coarse: `repo`
grants read and write on private repositories even though this bundle only ever
reads, so consent to the toggles you want and no more; public-only use can
narrow that to `public_repo` by editing the manifest. `user:email` is
deliberately never requested, so the identity probe sees the PUBLIC profile
email only: an account whose profile sets none carries a blank `email` and
displays as its `login`, and a person mapping mints a shell for it rather than
matching. GitHub revokes app grants over a Basic-auth
call the facility cannot speak, so the bundle declares no `revocationEndpoint`:
disconnecting deletes the stored credential, and the grant itself is revoked
from GitHub's settings.

## Linear

Package `providers.substrate.reamde.dev/linear`. An OAuth provider that mirrors the
issues assigned to you, in Linear's own shape.

- **Kinds (5)**: `config`, `account`, and the mirrors `user`, `team`, `issue`.
- **Functions (1)**: `issuessync` pages the viewer's assigned issues and
  mirrors the viewer, teams, and issues.
- **Triggers (2)**: `linear-issues-on-connect` and `linear-issues-scheduled`
  drive `issuessync`, on connect and hourly.
- **Mappings**: none. `user.person`, `issue.assignee` and `issue.task` are
  three empty subject slots, and the mappings that fill them belong to the
  packages that own their targets (record 0049).

The two samples ship them as SUGGESTED MAPPINGS: `people` declares
`linearuserperson` and `linearissueperson`, matching on the login and assignee
addresses, and `tasks` declares `linearissuetask`, matching an issue's URL
against a task's `url` and carrying the heading and the link and nothing else.
Import either sample with this provider installed and its mapping lands;
import it first and the mapping is reported `waiting` for this package, and
installing this package afterwards is not enough on its own: import that
sample again ([suggested mappings](bundles.md#suggested-mappings)).

It mirrors issues and nothing around them: no comments, no attachments, no
cycles, no projects. Nothing sweeps either, so an issue deleted or unassigned
upstream keeps its mirror until a tombstone slice adds reconciliation.

`read` is the only scope this bundle ever requests. Linear exposes no userinfo
endpoint the facility could read at the exchange, so the account's `email` is
stamped by the sync from Linear's own `viewer` query instead, and the owner
never types it. A workspace that HIDES the viewer's email leaves an issue with
no address to probe, so the sync points each issue's `assignee` slot at the
viewer's own `user` mirror: a mapping that reaches people through that mirror
resolves in one hop, and a hidden address costs one shell per login rather
than one per issue.

A projected task's `status` is not mapped and cannot be: a state moves through
its declared transitions, never through a mapping
([0040](decisions/0040-the-four-occurrence-logs-say-done.md)), so the task the
mapping mints starts `open` and every move after that is yours, untouched by
any sync. The projection's TIERS are what protect the two properties that ARE
mapped: `name` and `url` are recomputed at the machine tier, so retyping the
heading keeps it and Linear's next title lands only where you have not.

## WHOOP

Package `providers.substrate.reamde.dev/whoop`. An OAuth provider that mirrors a WHOOP
wearable's daily physiology.

- **Kinds (5)**: `config`, `account`, and the mirrors `recovery`, `sleep`,
  `workout`.
- **Functions (1)**: `whoopsync` pages each enabled collection (recovery,
  sleep, workouts) over the provider's page token, one page per invocation, and
  stamps the account's `lastSyncedAt` and `syncStatus`.
- **Triggers (2)**: `whoop-on-connect` and `whoop-scheduled` drive `whoopsync`.
- **Mappings**: none. The mirrors are their own subjects; there is no person to
  resolve.

Every feature toggle carries its read scope plus `read:profile` and `offline`,
so a refresh token is minted and the facility can derive the connected address.
The five to enable on the WHOOP app are therefore `read:recovery`,
`read:sleep`, `read:workout`, `read:profile` and `offline`.
WHOOP's documented revocation is an OAuth-authenticated delete, a shape the
facility does not speak, so the bundle declares no `revocationEndpoint`:
deleting the account tears down the stored credential, and the grant itself is
revoked by hand in the WHOOP app (App & Privacy, connected apps).

Windows, not sync tokens: WHOOP's v2 API has no incremental cursor, so each
collection is read over a `[start, end]` window: the first one starts where
`backfillDepth` says (`all` reads from a fixed pre-API epoch), and every later
run re-reads a 48-hour overlap behind `lastSyncedAt`, because recovery and
sleep scores settle hours after a record first appears. The provider-keyed puts
make the overlap converge instead of duplicate, and under `none` a record from
up to two days before the connect can still land once its score settles.

## Notion

Package `providers.substrate.reamde.dev/notion`. A provider that mirrors the Notion
pages and data sources the user has shared with it. It is authorized by an
internal Notion token (the one a workspace owner mints in Notion's settings)
rather than OAuth, because Notion authenticates its token exchange with HTTP
Basic and the host facility declares one auth style for every bundle.

- **Kinds (4)**: `config`, `account`, and the mirrors `page` and `database`
  (one row per data source, recording its containing database).
- **Functions (1)**: `workspacesync` runs three resumable phases (search,
  blocks, links), pinned to the 2025-09-03 data-source API version, and
  short-circuits on the last-edited time for a delta sync.
- **Triggers (2)**: `notion-on-connect` fires when pages or databases are
  enabled; `notion-scheduled` fires hourly.
- **Mappings**: none. A Notion page mirrors as a document, not a person.

The Notion token is a secret on the configuration record, origin-pinned to
Notion's API host. Only one account per repository syncs: every other account
row is stamped `syncStatus: ignored: duplicate account`.

Setting it up takes two steps in Notion. Mint an internal token (Settings,
Connections, Develop or manage) with read-content capabilities only, and paste
it onto the `config` record. Then SHARE each top-level page, database or
teamspace with the connection that token belongs to: the search
API returns only what has been shared with it, so an unshared page is invisible
to the sync rather than refused. A page's mirrored `content` truncates at 500
blocks and nesting depth 2, closed by a `[content truncated]` marker.

The search walk stops at the first below-cutoff result, descending by
`last_edited_time`, so a narrow `backfillDepth` never scans the whole
workspace. The flip side of a window that moves with the clock: a mirror that
falls behind it stops being visited, so a `pendingParent` it still holds stops
being repaired.

## Beeper

Package `providers.substrate.reamde.dev/beeper`. A non-OAuth provider: it connects a
Beeper (Matrix) homeserver with a pasted access token and mirrors bridged rooms
and messages (WhatsApp, Telegram, Signal, iMessage, and the rest). Read only,
it never sends.

- **Kinds (4)**: `config`, `account`, and the mirrors `room` and `message`.
- **Functions (1)**: `messagessync` makes one Matrix sync or messages call per
  invocation, mirrors rooms and messages, and stores the next-batch token for
  incremental runs.
- **Triggers (2)**: `beeper-messages-on-connect` and
  `beeper-messages-scheduled` drive `messagessync`.
- **Mappings**: none by design. Bridge ghosts have no clean identifier to probe
  on, so the closure declines to fold messages onto conversations or senders
  onto people.

The token is a secret on the configuration record, origin-pinned to Beeper's
hosts or loopback. Only one account per repository syncs: every other account
row is stamped `syncStatus: ignored: duplicate account`.

The token is a Matrix access token, from `bbctl login` (which writes it to
`~/.config/bbctl/config.json`) or from Beeper Desktop's Settings, Developer;
treat it like a password. The config's `homeserverUrl` defaults to
`https://matrix.beeper.com` and a set one must be `https` on a `beeper.com`
host; the account carries the `userId` it belongs to (`@you:beeper.com`). Its
optional `roomFilter` narrows the walk to rooms whose name or bridged network
contains a case-insensitive substring (`whatsapp` syncs only WhatsApp rooms),
and it falls back to the stored mirror when a delta carries neither field, so a
matching room never silently stops syncing.

History arrives across runs: one drain walks at most 2,000 events per room;
a room with deeper history inside its `backfillDepth` window is recorded in the
account's `backfillResume`, stamped `syncStatus: ok (N rooms backfill
pending)`, and resumed by the next run rather than cut off. Message edits,
reactions and redactions are not folded in: an edit arrives as its own event
and the original row stands.

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
  three name `provider: default`, which the LLM example above ships, and each
  names its own `model` — what the agent does is what picks the model, not a
  tier.

Its functions are deterministic stubs, because the bundle exists to exercise
the machinery rather than talk to a provider. Its one knob is a shipped
[`setting`](bundles.md#settings) record, `denyDomains`: a
comma-separated list of URL pieces `findurls` skips, empty as shipped, which
the bodies read as `config.settings.denyDomains`.

## Pebble (sample)

Package `samples.substrate.reamde.dev/pebble`. Voice capture from a Pebble
Index 01 ring: the phone app POSTs each capture to a webhook, and the bundle
saves it as a `recording`, plus an `instruction` an agent turns into tasks when
the capture came from a press-and-hold. It requires
`samples.substrate.reamde.dev/tasks`, which the agent writes into.

- **Kinds (2)**: `recording` (one capture: the `transcription` it heads itself
  with, `recordedAt`, `mode`, `client` and the `audio` blob digest) and
  `instruction` (the agent-mode capture's `text`, pointing back at its
  recording).
- **Functions (1)**: `ingest` writes both records off one webhook fire. The id
  is derived from the fire id, so a retried delivery updates the same records.
- **Triggers (2)**: `pebble-webhook` receives the POST; `pebble-on-instruction`
  delivers each new instruction to the agent.
- **Agents (1)**: `assistant` reads the open tasks through the `query` host
  function and writes `samples.substrate.reamde.dev/tasks/task` records through
  `write`. It names `provider: default`, which the LLM example above ships:
  import that bundle too and key its row.

**The endpoint** is `POST
https://<your-substrate-host>/webhooks/<authority>/pebble-webhook`, where
`<authority>` is the repository's own authority;
`substratectl trigger status` prints the path in its `WEBHOOK` column. It is
open as shipped, and setting `source.webhook.key` on the `pebble-webhook`
trigger (16 to 128 characters of `[A-Za-z0-9_-]`) makes the server require that
key as a trailing path segment, as `?key=<key>`, or as a bearer token
([functions](functions.md#triggers)).

**The request** is `multipart/form-data` with four parts: `transcription`
(text), `audio` (`audio/mp4`), `recordedAt` (milliseconds since the Unix
epoch, as text) and `client` (the text `ring`). The host stores the audio in
the repository's blob store before the function runs, and the function writes
the digest to the recording's `audio`. The two gestures are told apart by a
header rather than by URL: a single press sends `X-Pebble-Mode: note`, a
press-and-hold `X-Pebble-Mode: agent`, and a request with neither is saved as
a note. So the ring app carries the same URL twice, once per header value,
with "Send" set to transcription and recording. Transcription alone works too;
the recording then has no `audio`. One fire, mimicked by hand:

```bash
printf '' > empty.m4a
curl -s -X POST "https://<your-substrate-host>/webhooks/<authority>/pebble-webhook" \
  -H "X-Pebble-Mode: agent" \
  -F "transcription=call the dentist tomorrow morning" \
  -F "recordedAt=$(( $(date +%s) * 1000 ))" \
  -F "client=ring" \
  -F "audio=@empty.m4a;type=audio/mp4;filename=recording.m4a"
```

Next: [substratectl](substratectl.md), the command line over all of it.
