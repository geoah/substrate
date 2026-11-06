# Plan: operator defaults on the Providers page

Status: accepted by the owner 2026-11-06. The design went through two review
rounds by Astra (gpt-6-astra) and one by Fable; the vocabulary and the console
were then revised once more, reviewed by both, to drop a separate Services
page. Implementation follows the phases below.

## The problem

A person who registers on a substrate someone else runs for them cannot use
most of it until they set up paid services themselves:

- Every agent refuses at dispatch until the person pastes an OpenAI key into
  the seeded `openai` row (`kinds/substrate.reamde.dev/llm/provider.yaml`).
- Search by meaning stays off until one `llm/provider` row names an
  `embedModel`.
- Connecting Google, GitHub or WHOOP needs an OAuth client the person creates
  in that vendor's developer console.
- Web search and page reading (the Firecrawl sample) need a Firecrawl key,
  and the person must first know to import the sample.

The operator (whoever runs the deployment) holds accounts for all of these
and may be willing to pay for the people it invites. It has no way to offer
them: the codebase removed the server-wide LLM key on purpose ("no host-wide
bearer can reach a repository-chosen endpoint by construction"). Nothing in
the console tells a new person what to set up, in what order, or what is
optional.

## The outcome

1. The operator lists, in its own file, the keys it pays for and the
   packages every repository gets. A new repository can chat with agents,
   search by meaning and search the web on day one, and connects Google with
   one approval.
2. Each key the operator supplies is a **default**: the person's own key,
   where they write one, always wins; removing it returns to the operator's.
3. An operator key never enters a repository: not a record, not the sealed
   store, not the changelog, not the recovery export, not a function's input
   except the specific published functions the operator names. It is spent
   only against the transport the operator configured, and only within the
   operator's allowance.
4. The console's **Providers** page lists every outside service the
   substrate spends a key on, says for each whose key it uses and whether it
   works, and is where a new person lands after registering.

## Words

| Word | Means |
| --- | --- |
| **operator** | Whoever runs the deployment, as in docs/operations.md and the CLI's operator hat. Never "host" for this meaning: host already means the server (host functions, the host credential key, the `host` SDK object). |
| **operator default** | A key or an OAuth client the operator supplies, used only where the repository holds none of its own. |
| **allowance** | What the operator pays for one repository in one UTC calendar day. |
| **provider** | Unchanged in code: the catalog tier. The Providers page also lists `llm/provider` rows, as AI models. |

The console never says operator, host, bundle or `llm/provider` in everyday
copy. It names the operator by the site's name from `site.yaml` ("Crane" in
the examples), or "this server" where the deployment has none. "Service" is
not a substrate word. Names avoid "credential", which is the core kind for
the login credential.

## Inventory

| Provider | Providers group | What it is | Key or sign-in | Operator default |
| --- | --- | --- | --- | --- |
| Google, GitHub, WHOOP | Accounts you connect | catalog provider | OAuth client input | yes, phase 2 |
| Linear, Notion, Slack, Slack live updates, Beeper | Accounts you connect | catalog provider | the person's own token on the provider's config | no, the token is the person's |
| Apple | Accounts you connect | catalog provider with kinds only; the device app writes | none | nothing to supply |
| Apple Health | Accounts you connect | webhook provider | a key the phone sends in, on its trigger | nothing to supply |
| Firecrawl | Tools for your agents | catalog provider (phase 3; a sample today) | an API key | yes, phase 3 |
| OpenAI, Anthropic, Gemini, and rows the person adds | AI models | `llm/provider` rows; no package, no code | the row's `apiKey` | yes, phase 1, for rows the operator lists |
| Search by meaning | a line on one AI model card | the embeddings provider | the claiming row's key, or the operator's | yes, phase 1 |

A key the substrate sends out belongs to exactly one card. A key others send
in (Apple Health's webhook key, any trigger's) is set up where it is today and
has no "whose key" line. A sample's own secret stays on the sample's package
page.

## Design

### The operator's defaults file, never records

One file named by `SUBSTRATE_OPERATOR_DEFAULTS`, mounted from the
deployment's secret store (never from `SUBSTRATE_SITE_DIR`, which is served).
Key material is read from a file or an environment variable, never inline:

```yaml
install:
  - providers.substrate.reamde.dev/google
  - providers.substrate.reamde.dev/firecrawl

llm:
  openai:                          # the llm/provider row id it stands in for
    wire: openai
    baseURL: https://api.openai.com/v1
    apiKey: {file: /run/secrets/openai}
    models:                        # the only models it serves, at the operator's prices
      - {model: gpt-5, inputPer1M: "1.25", outputPer1M: "10", contextWindow: 400000}
      - {model: gpt-5-mini, inputPer1M: "0.25", outputPer1M: "2", contextWindow: 400000}
    maxOutputTokens: 16000         # required: imposed where an agent names none, caps one that names more
    titleModel: gpt-5-mini         # optional; absent: a thread is titled by the model it ran
    defaults: {}                   # optional request params under the agent's own
    cache: {key: true}             # optional
    headers: []                    # optional; Idempotency-Key and X-Idempotency-Key are refused
    embeddings: {model: text-embedding-3-small, per1M: "0.02"}   # at most one entry in the file
  anthropic:
    wire: anthropic
    baseURL: https://api.anthropic.com/
    apiKey: {env: OPERATOR_ANTHROPIC_KEY}
    models: [...]
    maxOutputTokens: 16000

oauthClients:
  providers.substrate.reamde.dev/google:
    clientId: 1234.apps.googleusercontent.com
    clientSecret: {file: /run/secrets/google-client-secret}
    # Shown on the provider's card and setup step while the client is in
    # Google's Testing mode.
    notice: >-
      Crane's Google sign-in is in testing. Ask Crane to add your Google
      address before you connect, and expect to reconnect once a week.

functionSecrets:
  providers.substrate.reamde.dev/firecrawl:
    apiKey: {file: /run/secrets/firecrawl}
    consumers: [websearch, webscrape]
    pricePerCall: {websearch: "0.1", webscrape: "0.1"}   # cents

budget:
  centsPerDay: 200                 # required: the allowance per repository per UTC day
```

The server refuses to boot on a missing key file or unset variable, an
inline key, a base URL that is not https, a model without a price, more than
one embeddings entry, an idempotency header, an unknown key anywhere, or an
`oauthClients` or `functionSecrets` entry naming a package the binary does
not ship as a provider. Rotation is a file change and a restart. An entry
removed stops at the next start. Without the file the server behaves as
before, apart from two changes that apply everywhere: credential-bearing
LLM and embeddings requests refuse redirects, and migration 0019 creates the
(empty) ledger table.

### LLM: an empty key on an operator-listed row uses the operator's transport

A live `llm/provider` row with an empty `apiKey` whose id the operator lists
(`openai`, `anthropic`) runs on the operator's whole transport: `wire`,
`baseURL`, `headers`, `defaults`, `cache`, `titleModel`, pricing and
`maxOutputTokens` all come from the file, and the row's own values are
ignored. Nothing the person writes reaches an operator-paid request except
the prompt and a model name from the allowed list.

- A row the person deleted stays deleted (the seed is create-only), and an
  agent naming it is refused as today. The Providers page shows it as an
  **Add** card that recreates it with the seed's fields and no key.
- A row deleted and recreated under the same id with no key is operator-backed
  again: eligibility is the id and the empty key.
- An agent naming a row the operator does not list runs on that row's own key
  or is refused, as today.
- A model off the operator's list is refused, naming the allowed models and
  saying how to add your own key.
- A row with the person's key behaves exactly as today.

Turns, retries, compaction, titles and sub-agents all go through the same
gated client. Operator-paid Anthropic calls stop retrying once an attempt may
have been billed (a 5xx or a broken answer), so one reservation covers the one
billable attempt; a 408, 409, 429 or an unsent request still retries.
Credential-bearing requests refuse redirects for every key, reusing
`internal/egress/confined.go`'s `CheckRedirect`.

### Embeddings: independent of chat keys

When no row claims `embedModel`, the operator's `embeddings` entry is the
repository's embeddings provider. A row that claims it wins, by the existing
single-claimant rule, and must carry its own key: a claimed `embedModel` never
runs on an operator key. Keying a chat row never moves embeddings, because a
person's row may point at a gateway with no embeddings endpoint, and moving
would re-buy every vector unannounced.

Vectors are stamped with the pair that bought them. Operator-bought vectors
carry the provider id `(operator)`, which the record id alphabet refuses, so
no row can collide with it. The drain, the reconcile after an import or
restore, the semantic arm's pair check and the agent loop all read the pair
through one resolver (`embedPair`), so an operator-backed repository is never
read as having no provider. Switching between the operator's embeddings and
your own changes the pair and re-buys the vectors, as any embeddings change
does. At the allowance the embed queue waits, and a hybrid search falls back
to word matching with a note.

### OAuth clients: a fallback inside client resolution only

The operator's client is used only when the provider's OAuth client input
resolves to no record at all. Ambiguous, dangling and incomplete inputs keep
their current setup items. The id and secret are taken as one pair. Only
published providers listed in `oauthClients` qualify. An operator-registered
client says nothing about who pays: it is a sign-in, not a billed key.

Each account records the client its grant was issued to: a nonsecret
`oauthClient` property (`writer: oauth`) on each provider's account kind,
with a provider version bump. Refresh compares it with the client resolving
now; on a mismatch the account reads **Reconnect** and is not refreshed. An
account connected before the property existed is treated as connected through
the person's own client. On an operator-connected account the functions
ignore the config's `apiBase` test seam.

**Google in Testing mode.** Only listed test users (at most 100) can consent,
so the operator adds each address by hand and the client's `notice` says so.
Google expires a Testing client's refresh tokens after 7 days; the refresh
then fails with `invalid_grant`, which the OAuth facility already turns into
`tokenError` and a parked credential (decision 0180), so the account reads
**Reconnect**. A production client later is a client id change: after the swap
every Google account reconnects once. Its verification (sensitive scopes, and
a security assessment for `gmail.readonly`) is the operator's own work.

### Function secrets: handed to named code, never stored

The operator's Firecrawl key is not a `secret` record. At invocation the
engine adds it to `config.settings.apiKey` only when the function is one of
the `consumers` of a package whose `source` is `published`, the repository
holds no `apiKey` secret of its own for that package, and the allowance admits
the call at its `pricePerCall`. It never reaches the sealed store, an export
or another package's function.

Firecrawl becomes `providers.substrate.reamde.dev/firecrawl`, published so its
code cannot be edited by the repository. Its tools are `websearch` and
`webscrape` (the sample's `scrapepage`, renamed), both declaring `offer` and
`effect` (decision 0198) so the default assistant takes them. In operator mode
the bodies call only `https://api.firecrawl.dev`, never loopback, and refuse
redirects. The Firecrawl sample leaves the catalog; copies already imported
keep `scrapepage` and their own key and never receive the operator's.
Installing the provider while a copy exists offers to remove the copy, because
both would offer `firecrawl_websearch` and the copy's would win.

### Allowance: operator prices, an operator ledger, reserved before the call

The person's own spend cap (decision 0149, the setting
`substrate.reamde.dev/llm/spendCentsPerDay`) stays theirs and unchanged. The
allowance is separate and reads nothing the person can write.

- **Day.** One UTC calendar day: `centsPerDay` covers the reservations a
  repository opened between 00:00 and 24:00 UTC. The console shows the reset
  in the viewer's local time.
- **Ledger.** The control-plane table `operator_spend_reservations` (migration
  0019) holds one row per operator-paid call, under row level security like
  every per-repository table. It is not a fold table and not in the recovery
  export, so a restore or rebuild leaves the allowance as it was.
- **Reserve.** Before each call the engine reserves a ceiling in one
  transaction, serialized per repository, refused when it would pass
  `centsPerDay`. The ceiling counts every billable part at its highest rate:
  the prompt estimate with a margin at the higher of the input and cache-write
  prices plus `maxOutputTokens` at the output price; an embedding batch's
  token estimate with the same margin; a function's `pricePerCall`.
- **Settle.** A call settles to the usage the vendor reported, even when the
  call then failed. It settles to zero only when no request left or the vendor
  refused it outright with a redirect or a 4xx (`egress.ErrUnprocessed`).
  Anything else keeps its ceiling: a 5xx, a timeout, a broken or undecodable
  answer, a reply without usage. A partial usage report settles at the ceiling
  or its own cost if higher, and still counts on the person's own spend cap.
- **Expire.** A reservation unsettled after an hour settles at its ceiling.
- **At the limit.** Triggered runs are held the way decision 0149 holds them;
  a direct call is refused with the `quota` reason, a message naming the site,
  the reset time and how to add your own key. The gate sits beneath every
  caller: agent runs, chat, titles, compaction, retries, embeddings and scoped
  app tokens.

### Installs, recorded in the repository

`install:` runs the ordinary published catalog install at registration and at
a repository's first open under a file that lists a package it has not held.
"Has not held" is read from the repository's own changelog: a package whose
bundle record was ever written there, even if later uninstalled, is not
installed again, and that fact travels with the recovery export. It holds
while the changelog keeps full history; a future compaction must keep it. A
failed install is logged, retried at the next open, and shown on its card as
the server's message.

### What the API tells the console

`GET /api/v1/operator/defaults` answers `OperatorDefaults`
(`internal/substrate/operatordefaults.go`), resolved by the same resolver
dispatch, the OAuth facility and injection use:

```json
{
  "operator": {"name": "Crane", "allowance": {"centsPerDay": 200, "usedCents": 74, "resetsAt": "2026-11-07T00:00:00Z",
    "bySlot": [{"slot": "llm", "cents": 61}, {"slot": "embeddings", "cents": 2}, {"slot": "function", "cents": 11}]}},
  "items": [
    {"id": "llm/openai", "slot": "llm", "label": "OpenAI", "target": "substrate.reamde.dev/llm/provider/openai",
     "source": "operator", "operatorOffers": true, "readiness": "ready", "models": ["gpt-5", "gpt-5-mini"]},
    {"id": "llm/anthropic", "slot": "llm", "label": "Anthropic", "target": "substrate.reamde.dev/llm/provider/anthropic",
     "source": "own", "operatorOffers": true, "readiness": "ready"},
    {"id": "embeddings", "slot": "embeddings", "label": "Search by meaning", "target": "substrate.reamde.dev/llm/provider/openai",
     "source": "operator", "operatorOffers": true, "readiness": "ready"},
    {"id": "oauth/providers.substrate.reamde.dev/google", "slot": "oauth", "label": "Google",
     "target": "providers.substrate.reamde.dev/google", "source": "operator", "operatorOffers": true,
     "readiness": "needs-connect", "notice": "Crane's Google sign-in is in testing. ..."}
  ]
}
```

Three separate facts per item: `source` (`operator`, `own`, `none`),
`operatorOffers` (removing your own key would fall back to the operator's),
and `readiness` (`ready` means the key resolves, not that a call succeeded;
`not-set-up`, `needs-connect`, `reconnect-needed`, `failing` from the newest
open alert of an agent on the row, `install-failed`). For embeddings,
`target` is the row whose card shows the search line. Owner tokens and
full-access app tokens may read it; scoped app tokens are refused. The
discovery feature `operatorDefaults` is listed only when a file is loaded.
Bundle setup items and the keyless-provider probe use the same resolver, so a
provider page never asks for what the operator covers.

### The console: one Providers page

`/providers` lists three groups:

1. **Accounts you connect**: catalog providers that bring data in, one card
   per package as today.
2. **Tools for your agents**: catalog providers with no account kind that
   offer functions to agents (Firecrawl).
3. **AI models**: one card per `llm/provider` row, seeded or added by the
   person, titled by its label. A deleted seeded row shows as an **Add** card.

Every card shows what it gives you, whether it works (On, Set up, Needs
attention, Paused; an AI model card is On when a key resolves, Set up when
none does, Needs attention when `failing`, never Paused), and, only where
someone can supply a key, whose: "Using Crane's key", "Using your key",
"Through Crane's Google sign-in" with the client's `notice`. AI model cards
are their own component with the key form inline: **Use your own key**,
**Replace key**, and **Use Crane's again** where `operatorOffers` (confirmed;
it clears the row's key). The card the embeddings item names shows a second
line, "Search by meaning: Crane's key" or "your key"; changing the embeddings
provider stays a Technical details action in v1. The record page of an
`llm/provider` row is linked only with Technical details on.

The allowance is one bar on the AI models group header (used of total, split
by slot, reset in local time), and a one-line note on the Tools header when a
tool runs on the operator's key. It never sits over the data cards.

Registration lands on `/providers` with a dismissible welcome header when
discovery lists `operatorDefaults`, except when it began from an app's
authorization or a domain verification link. Without the feature it lands
where it does today. Home is unchanged, so decision 0148 stands. One optional
`consolepreference` property, `welcomeDismissed`, remembers the dismissal.

Elsewhere: an agent's panel says "Runs on OpenAI, gpt-5", linking to the
card, and the "Add your OpenAI key" prompt in a chat links there too. The
sidebar's and Home's provider counts count what the page shows as set up:
installed provider packages plus AI model rows whose key resolves. Settings
keeps the person's own spend cap. Tools is unchanged; Firecrawl's two tools
show there as Google's syncs do.

## What this amends

- Decision 0232 (new, PR #1213): an empty key on an operator-listed row runs on
  the operator's whole transport; operator keys never enter a repository; the
  allowance is the operator's ledger; the embeddings stamp.
- A new console decision (PR #1212): the Providers page lists every outside
  service the substrate spends a key on, as provider packages and
  `llm/provider` rows in three groups; it amends what docs/console.md says
  Providers holds (decision 0130).
- Firecrawl leaves the sample tier for the provider tier (decision 0048), and
  its tools declare offers (decision 0198). Phase 3 records it.
- docs/terms.md gains operator, operator default and allowance.

## Implementation plan

| PR | Holds | State |
| --- | --- | --- |
| #1205 | this plan | open |
| #1209 | the wire shape, `OperatorDefaults` | open, stacked on #1205 |
| #1213 | phase 1 engine: the file, the operator transport, embeddings, the ledger and gate, redirect refusal, the read, decision 0232 | draft, stacked on #1209; three Astra rounds, all findings fixed; being renamed to operator words |
| #1212 | phase 1 console: the Providers page, AI model cards, the welcome landing, the console decision | draft, stacked on #1209; being reworked from the Services page |
| phase 2 | OAuth client defaults with `oauthClient` provenance (Google first), `install:` | after #1213 |
| phase 3 | Firecrawl as a provider with `websearch` and `webscrape`, function secrets, the sample's removal | after #1213 |

Each PR's tests are shown to fail against a deliberate break of the behavior
they cover. The orchestrator runs the full database suites once, one after
another, before the stack leaves draft, and takes the final screenshots
against a running server with a defaults file.

## Owner decisions, 2026-11-06

- The plan must work with a Google client in Testing mode; a production
  client is the operator's own work after the plan lands.
- One per-repository daily allowance is enough; no deployment-wide monthly
  stop.
- No per-repository withdrawal: removing an entry from the file withdraws it
  for everyone.
- No separate Services page: every key belongs to one card on Providers.
- Firecrawl's page tool is `webscrape`, beside `websearch`.
- Speech (ElevenLabs) and transcription (Soniox) are out of scope for now; a
  later provider follows phase 3's rule.
