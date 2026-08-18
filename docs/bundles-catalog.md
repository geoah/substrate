# Bundles catalog

The substrate ships ten of these bundles in the binary, each a real closure a
user installs with `substratectl apply` or from the
[catalog](bundles.md#the-catalog). (The vocabulary bundles beside them —
`people`, `tasks`, `calendar` and the rest — are in
[built-in kinds](builtin-kinds.md).)
Google, GitHub, Linear, WHOOP, Notion, and Beeper carry the `integration`
facet; LLM, notes, and the web harvester carry `example`, because they exist
to be read; Firecrawl carries neither, adding callables behind an API key
with no provider account. Each facet
is the catalog's own curated flag, stated per bundle rather than inferred
from the closure. An example sits outside the stable vocabulary set
([decision record 0015](decisions/0015-unproven-kinds-stay-out-of-the-stable-set.md)):
its declarations may change or leave without an upgrade path, and installing
one accepts that. Every integration syncs from the provider into the
repository; none writes back to the provider, and each ships a README stating
its limits.

This is the map. The source of truth is each bundle's own manifests under
`kinds/`, and everything an install will add — the declarations and the records
it writes beside them — is previewable through
`GET …/catalog/{id}`.

Kinds, functions, agents, and mappings are named `<authority>/<name>`; each
section's first line gives the authority, and its lists give the name. The
authority carries the provider, so no name repeats it — GitHub's issue mirror
is `github.bundles.substrate.reamde.dev/issue`, Linear's is
`linear.bundles.substrate.reamde.dev/issue`, and one record of either is addressed as
`<authority>/<kind>/<id>`. What a closure declares is exactly what its
`installs:` lists. Beside that closure every bundle may ship **ordinary
records**, written by the same install: an integration's triggers (the
delivery wiring, in
`triggers.yaml`) and the LLM example's two provider rows are the same kind of
thing, and the Records column counts them.

| Bundle     | Facet       | Auth           | Kinds | Functions | Records | Agents |
| ------------- | ----------- | -------------- | ----- | --------- | ------- | ------ |
| Google        | Integration | OAuth          | 8     | 4         | 6       | 0      |
| GitHub        | Integration | OAuth          | 6     | 1         | 2       | 0      |
| Linear        | Integration | OAuth          | 5     | 2         | 3       | 0      |
| WHOOP         | Integration | OAuth          | 5     | 1         | 2       | 0      |
| Notion        | Integration | Internal token | 4     | 1         | 2       | 0      |
| Beeper        | Integration | Pasted token   | 4     | 1         | 2       | 0      |
| LLM           | Example     | Key, per row   | 0     | 0         | 2       | 3      |
| Notes         | Example     | none           | 1     | 2         | 0       | 2      |
| Firecrawl     | —           | API key        | 2     | 2         | 0       | 0      |
| Web harvester | Example     | none           | 2     | 4         | 4       | 3      |

## LLM (example)

Authority `llm.examples.substrate.reamde.dev`. Install this bundle first if
you want to run an agent at all. A fresh substrate seeds no `llmprovider` row,
so this bundle ships the two an agent can name (`anthropic` and `openai`),
correctly shaped for their wires and deliberately keyless, plus three agents.
`substrate` is the one to chat with: it reads the whole graph
through the `graphql` built-in and writes nothing directly, proposing every
change as a `recordpatchrequest` the owner decides on. `substrateEcho` and
`substrateSummarizer` are the delegation demo, and the summarizer is
`subagentOnly`: off the chat list, callable only by other agents.

Installing it gives you rows that refuse until you key them. The key is a
record write: **Data → llmproviders → `anthropic` → Edit**, put it in `apiKey`,
apply. It is secret-typed, so it reads back redacted from then on and rotating
it is the same write again. The `openai` row is the other wire, and with an
empty `baseURL` it resolves to the host's configured gateway:
[providers](agents.md#providers) has the host-gateway rule, the wires, and
pricing.

## Notes (example)

Authority `notes.bundles.substrate.reamde.dev`. The smallest bundle that shows
an agent calling functions as tools and delegating to a sub-agent, and the one
to read first. It needs no network, no credentials and no other bundle's
vocabulary, so it installs on a fresh substrate and is driven by hand in one
command, and its two functions stand on their own with no model at all:

```bash
substratectl apply -f kinds/notes.bundles.substrate.reamde.dev/bundle.yaml
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
curl -s -X POST "$SUBSTRATE_SERVER/api/v1/core.substrate.reamde.dev/agent/notekeeper/call" \
  -H "Authorization: Bearer $SUBSTRATE_TOKEN" -H 'Content-Type: application/json' \
  -d '{"input": {"text": "id: my-note\n\nSomething worth keeping."}}'
```

## Google

Authority `google.bundles.substrate.reamde.dev`. An OAuth integration that syncs a Google
account's address book, mail, and calendars into the repository and folds them
onto your people.

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
- **Functions (4)**: `contactssync` pages `people/me/connections`, emits
  `contact` records, and stores the People sync token for incremental runs;
  `gmailsync` drains Gmail history (or a bounded backfill window) into thread
  and message mirrors plus core `messaging.substrate.reamde.dev/emailthread` and
  `emailmessage` rows; `calendarsync` drains each calendar's events on that
  calendar's own sync token into event mirrors plus core
  `calendar.substrate.reamde.dev/calendar` and `calendarevent` rows; `contactsidmigration`
  is a bounded, trigger-less callable that re-keys older ad-hoc contact ids onto
  the deterministic external-id scheme.
- **Triggers (6)**: `google-contacts-on-connect`, `google-gmail-on-connect`,
  and `google-calendar-on-connect` fire their stream's sync the first time a
  connected account carries that toggle; `google-contacts-scheduled`,
  `google-gmail-scheduled`, and `google-calendar-scheduled` fire them hourly.
- **Mappings (2)**: `contactperson` folds `contact` onto
  `people.substrate.reamde.dev/person`, matching on email and mapping name plus the union
  of emails and phones; `emailaddressperson` folds `emailaddress` onto the same
  person, matching on the address and contributing the header name plus that
  address.

**Mirrors plus direct core emission.** Provider records are mirrored in
Google's own shape, and the sync functions also emit the core row for the same
logical object under the same derived id, with its required edges filled in.
The mappings resolve people and nothing else, because a mapping mints shells
that carry no edges and so could never satisfy the required `thread`, `account`
and `calendar` edges the core mail and calendar kinds declare. The
`emailaddress` record is the bridge: the core rows reference it, and the
engine's one-hop resolution lands the stored edge on the person its mapping
resolved.

**All three scopes are wired.** `enabledContacts` maps to `contacts.readonly`,
`enabledGmail` to `gmail.readonly`, `enabledCalendar` to `calendar.readonly`,
and the facility reads the connected address off the People profile endpoint
the bundle declares. The requested union is derived per consent from the
account's enabled toggles, so turning a stream on after the grant landed needs
a reconnect. `gmail.readonly` is one of Google's restricted scopes: a published
client needs CASA verification, and an External plus Testing client has its
refresh tokens revoked after seven days.

**What this slice does not do**: no attachment bytes (metadata and the
attachment id only), no `calendareventseries` (every event row is a concrete
occurrence, with the series id and recurrence rules kept on the mirror), no
label kind (core keeps provider label ids as plain strings), and no writeback.

## GitHub

Authority `github.bundles.substrate.reamde.dev`. An OAuth integration that mirrors the
code work you are involved in.

- **Kinds (6)**: `config`, `account`, and the mirrors `user`, `repository`,
  `issue`, `pullrequest`.
- **Functions (1)**: `githubsync` walks the connected user, reachable
  repositories, and the issues and pull requests you are involved in or
  review-requested on, one REST page per invocation with per-stage watermarks.
- **Triggers (2)**: `github-on-connect` fires `githubsync` once an account is
  connected and has a feature toggle on; `github-scheduled` fires it hourly.
- **Mapping (1)**: `userperson` folds `user` onto `people.substrate.reamde.dev/person`,
  matching on the public email and mapping name, login as the display name, and
  the union of emails.

Scopes are derived per toggle (`read:user`, and `repo` for repositories, issues
and pull requests), and the facility reads the account's public email from
`GET /user` after the exchange. GitHub revokes app grants over a Basic-auth
call the facility cannot speak, so the bundle declares no `revocationEndpoint`:
disconnecting deletes the stored credential, and the grant itself is revoked
from GitHub's settings.

## Linear

Authority `linear.bundles.substrate.reamde.dev`. An OAuth integration that mirrors the
issues assigned to you and projects them onto jointly-owned tasks.

- **Kinds (5)**: `config`, `account`, and the mirrors `user`, `team`, `issue`.
- **Functions (2)**: `issuessync` pages the viewer's assigned issues and
  mirrors the viewer, teams, and issues; `taskprojection` projects one `issue`
  onto a `tasks.substrate.reamde.dev/task` row, minting open tasks and patching the
  Linear-owned keys under a version check, moving status only on a real
  upstream transition.
- **Triggers (3)**: `linear-issues-on-connect` and `linear-issues-scheduled`
  drive `issuessync` (on connect and hourly); `linear-task-projection` fires
  `taskprojection` on every `issue` change.
- **Mappings (2)**: `userperson` folds `user` onto `people.substrate.reamde.dev/person`
  (match on email, map names and emails), and `issueperson` resolves an issue's
  `assignee` edge onto a person by the assignee's email.

This is the one integration that both mirrors a provider and projects into the
shipped `tasks.substrate.reamde.dev` vocabulary, so a Linear issue and a hand-written task
live side by side.

## WHOOP

Authority `whoop.bundles.substrate.reamde.dev`. An OAuth integration that mirrors a WHOOP
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

Authority `notion.bundles.substrate.reamde.dev`. An integration that mirrors the Notion
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

Authority `beeper.bundles.substrate.reamde.dev`. A non-OAuth integration: it connects a
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

## Firecrawl

Authority `firecrawl.bundles.substrate.reamde.dev`. Not an integration: no
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

## Web harvester

Authority `web.bundles.substrate.reamde.dev`. The substrate's
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
  `core.substrate.reamde.dev/recordpatchrequest` records for the owner to accept. All
  three name `provider: default`, a row nothing seeds (the owner writes and
  keys it before they can run), and each names its own `model` —
  what the agent does is what picks the model, not a tier.

Its functions are deterministic stubs, because the bundle exists to exercise
the machinery rather than talk to a provider.

Next: [substratectl](substratectl.md), the command line over all of it.
