# Bundles catalog

The substrate ships ten of these bundles in the binary, in the two tiers
[decision record 0048](decisions/0048-providers-are-published-samples-are-copied.md)
draws. (The vocabulary samples beside them, `people`, `tasks`, `calendar` and
the rest, are in [built-in kinds](builtin-kinds.md).)

**Six providers**: Google, GitHub, Linear, WHOOP, Notion and Beeper. Each is a
package its publisher owns, installed under
`providers.substrate.reamde.dev` and upgraded there. Every one syncs from the
provider into the repository; none writes back, and each ships a README
stating its limits.

**Four samples**: LLM, notes, the web harvester and Firecrawl. Each is a worked
example to read and copy, imported under the repository's own authority and
owned by it afterwards. It is never offered an upgrade, so a fix here never reaches
a repository that imported it
([0015](decisions/0015-unproven-kinds-stay-out-of-the-stable-set.md) is what
that amends).

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
| Firecrawl     | Sample   | API key        | 2     | 2         | 0       | 0      |
| Web harvester | Sample   | none           | 2     | 4         | 4       | 3      |

## LLM (sample)

Package `samples.substrate.reamde.dev/llm`. Install this bundle first if
you want to run an agent at all. A fresh substrate seeds no `llmprovider` row,
so this bundle ships the two an agent can name (`anthropic` and `openai`),
correctly shaped for their wires and deliberately keyless, plus a `scratchpad`
kind to practise on and six agents:

- `substrate` is the one to chat with: it reads the whole graph through the
  `graphql` built-in, writes nothing directly, proposes every change as a
  `recordpatchrequest` the owner decides on, and asks clarifying questions
  through the `ask` built-in.
- `substrateEditor` writes scratchpads directly through the `mutate` built-in,
  the demo for engine-stamped `changes` on a thread's tool rows.
- `substrateArbiter` is a judge you point a trigger at: it accepts or rejects a
  change request through `mutate`.
- `substrateJudge` is the tool-less verdict agent a
  [`recordpatchpolicy`](agents.md#the-policy-door) names under `judge:`, the
  policy layer's example.
- `substrateEcho` and `substrateSummarizer` are the delegation demo, and the
  summarizer is `hiddenFromChat`: off the chat list, callable only by other
  agents.

Installing it gives you rows that refuse until you key them. The key is a
record write: **Data → llmproviders → `anthropic` → Edit**, put it in `apiKey`,
apply. It is secret-typed, so it reads back redacted from then on and rotating
it is the same write again. The `openai` row is the other wire, and with an
empty `baseURL` it resolves to the host's configured gateway:
[providers](agents.md#providers) has the host-gateway rule, the wires, and
pricing.

## Notes (sample)

Package `samples.substrate.reamde.dev/notes`. The smallest bundle that shows
an agent calling functions as tools and delegating to a sub-agent, and the one
to read first. It needs no network, no credentials and no other bundle's
vocabulary, so it installs on a fresh substrate and is driven by hand in one
command, and its two functions stand on their own with no model at all:

```bash
substratectl apply -f samples/notes/bundle.yaml
substratectl function call stats --input '{"text": "hello world"}'
```

`notekeeper` is the root agent. It calls `titler` (a sub-agent with its own
budget and thread, no tools and an empty `permissions.writes`), then `stats`
(pure Python, declaring no `permissions.network`, so the sandbox denies it
sockets), then `savenote`,
which writes the one kind the bundle declares — `note`. That write lands only
because the kind is in BOTH the function's writes and the calling agent's,
which is the capability envelope in one closure.

Both agents name `provider: default`, so running them wants an `llmprovider`
row at that id — [nothing seeds one](agents.md#providers), and the LLM example
above ships `anthropic` and `openai` rather than `default`. Calling an agent is
an API call, not a CLI verb:

```bash
curl -s -X POST "$SUBSTRATE_SERVER/api/v1/substrate.reamde.dev/core/agent/notekeeper/call" \
  -H "Authorization: Bearer $SUBSTRATE_TOKEN" -H 'Content-Type: application/json' \
  -d '{"input": {"text": "id: my-note\n\nSomething worth keeping."}}'
```

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

**What this slice does not do**: no attachment bytes (metadata and the
attachment id only), no label kind (core keeps provider label ids as plain
strings), and no writeback.

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

Scopes are derived per toggle (`read:user`, and `repo` for repositories, issues
and pull requests), and the facility reads the account's public email from
`GET /user` after the exchange. GitHub revokes app grants over a Basic-auth
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
WHOOP's documented revocation is an OAuth-authenticated delete, a shape the
facility does not speak, so the bundle declares no `revocationEndpoint` and
revocation is manual.

## Notion

Package `providers.substrate.reamde.dev/notion`. A provider that mirrors the Notion
pages and data sources shared with an internal integration. It is authorized by
an internal-integration token rather than OAuth, because Notion authenticates
its token exchange with HTTP Basic and the host facility declares one auth
style for every bundle.

- **Kinds (4)**: `config`, `account`, and the mirrors `page` and `database`
  (one row per data source, recording its containing database).
- **Functions (1)**: `workspacesync` runs three resumable phases (search,
  blocks, links), pinned to the 2025-09-03 data-source API version, and
  short-circuits on the last-edited time for a delta sync.
- **Triggers (2)**: `notion-on-connect` fires when pages or databases are
  enabled; `notion-scheduled` fires hourly.
- **Mappings**: none. A Notion page mirrors as a document, not a person.

The integration token is a secret on the configuration record, origin-pinned to
Notion's API host. Only one account per repository syncs: every other account
row is stamped `syncStatus: ignored: duplicate account`.

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

## Firecrawl (sample)

Package `samples.substrate.reamde.dev/firecrawl`. Not a provider: no
provider account is connected and nothing syncs. It is web search and page
scraping over the Firecrawl API, exposed as two callables an agent binds as
tools, behind an API key.

- **Kinds (2)**: `config` (holding an API key) and `webdocument` (a scraped
  page kept as markdown).
- **Functions (2)**: `websearch` returns hits as title, URL, and snippet and
  writes nothing; `scrapepage` scrapes a page to markdown and upserts a
  `webdocument` at the URL's deterministic id, so re-scraping the same URL
  updates the one document.
- **Triggers**: none. Both functions are callables an agent or a client invokes
  directly, so the whole closure is `bundle.yaml`.

The API key is a secret on the configuration record, and the bodies refuse any
base URL that is not the pinned Firecrawl origin or loopback, so an edit of the
owner-editable `baseUrl` can never redirect the key.

## Web harvester (sample)

Package `samples.substrate.reamde.dev/web`. The substrate's
shipped end-to-end conformance example: it proves that `bundle`, `kind`,
`function`, `agent`, and `trigger` declarations compose into a real
feature (harvest URLs from a message, fetch and classify each page, propose
reading-list and weekly-digest notes) with no bespoke workflow primitive. It is
the running example these pages build on.

- **Kinds (2)**: `config` and `page` (a harvested URL and its fetched,
  classified content).
- **Functions (4)**: `findurls` extracts URLs from a triggering message and
  mints pending `page` records; `fetchpage` turns a pending page into markdown;
  `setclass` is the classifier's write hand; `stampconfig` writes the
  configuration record and exists to prove the emit ceiling refuses a write
  outside it.
- **Triggers (4)**: `web-findurls-on-message` runs `findurls` on a new
  conversation message, `web-fetch-on-page` runs `fetchpage` on a pending page,
  `web-classify-on-page` runs the `pageclassifier` agent on a fetched,
  unclassified page, and `web-rollup-weekly` runs the `weeklyrollup` agent on a
  Monday schedule.
- **Agents (3)**: `pageclassifier` classifies a page and delegates to
  `readinglistagent`, which proposes reading-list notes; `weeklyrollup` queries
  the week's pages and proposes a digest. Both proposals travel as
  `substrate.reamde.dev/core/recordpatchrequest` records for the owner to accept. All
  three name `provider: default`, a row nothing seeds (the owner writes and
  keys it before they can run), and each names its own `model` —
  what the agent does is what picks the model, not a tier.

Its functions are deterministic stubs, because the bundle exists to exercise
the machinery rather than talk to a provider.

Next: [substratectl](substratectl.md), the command line over all of it.
