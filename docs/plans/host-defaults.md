# Plan: host defaults and the Services page

Status: proposal, 2026-11-06. Reviewed in two rounds by Astra (gpt-6-astra),
approved for owner review with the round-2 changes applied. Not reviewed by
the owner yet.

## The problem

A person who registers on a hosted substrate (crane.garden is the first)
cannot use most of it until they set up paid services themselves:

- Every agent refuses at dispatch until the person pastes an OpenAI key into
  the seeded `openai` row (`kinds/substrate.reamde.dev/llm/provider.yaml`).
- Semantic search stays off until one `llm/provider` row names an
  `embedModel`.
- Connecting Google, GitHub or WHOOP needs an OAuth client the person creates
  in that vendor's developer console.
- Web search and page reading (the Firecrawl sample) need a Firecrawl key,
  and the person must first know to import the sample.

The host (the operator running the deployment) holds accounts for all of
these and will pay for a friends-and-family group. It has no way to offer
them: the codebase removed the host-wide LLM key on purpose ("no host-wide
bearer can reach a repository-chosen endpoint by construction"). Nothing in
the console tells a new person what to set up, in what order, or what is
optional.

## The outcome

1. The host lists, in its own configuration, the services it pays for and
   the packages every repository gets. A new repository can chat with
   agents, search semantically and search the web on day one, and connects
   Google with one approval.
2. Each host-supplied service is a **default**: the person's own key, where
   they write one, always wins; removing it returns to the host's.
3. A host key never enters a repository: not a record, not the sealed store,
   not the changelog, not the recovery export, not a function's input except
   the specific published functions the host names. It is spent only
   against the transport the host configured, and only within the host's
   budget.
4. The console has one page, **Services**, that says for each service where
   its credential comes from, whether it works, and how much of the host's
   budget is left, with the one action that changes it.

## Inventory: what the host could supply

| Service | Slot today | Code that spends the key | Host default |
| --- | --- | --- | --- |
| OpenAI, Anthropic, Gemini chat | seeded `llm/provider` rows `openai`, `anthropic`, `gemini`, `apiKey` | engine LLM adapters (Go) | yes, phase 1 |
| Embeddings | the one `llm/provider` row with `embedModel` | engine embed drain (Go) | yes, phase 1 |
| Google, GitHub, WHOOP sign-in | the bundle's OAuth client input (`config` record) | engine OAuth facility; functions never see the client secret | yes, phase 2 |
| Firecrawl search and scrape | `secret` record of the **sample** | the sample's Python functions | phase 3, after it becomes a hardened provider |
| Linear, Notion, Slack, Beeper | the person's own token on their account | provider functions | no, the token is the person's |
| Apple, Apple Health | pushed by the person's device | none | nothing to supply |

## Design

### Host configuration, never records

One file named by `SUBSTRATE_HOST_DEFAULTS`, mounted from the deployment's
secret store (never from `SUBSTRATE_SITE_DIR`, which is served). The host's
display name comes from the existing `site.yaml`. Key material is read from
a file or an environment variable:

```yaml
install:
  - providers.substrate.reamde.dev/google
  - providers.substrate.reamde.dev/firecrawl

llm:
  openai:                          # the provider row id it stands in for
    wire: openai
    baseURL: https://api.openai.com/v1
    apiKey: {file: /run/secrets/openai}
    models:                        # the only models it serves, at host prices
      - {model: gpt-5, inputPer1M: "1.25", outputPer1M: "10", contextWindow: 400000}
      - {model: gpt-5-mini, inputPer1M: "0.25", outputPer1M: "2", contextWindow: 400000}
    maxOutputTokens: 16000         # imposed where an agent names none, capped where it names more
    titleModel: gpt-5-mini         # absent: a thread is titled by the model it ran
    defaults: {}                   # request params under the agent's own; absent: none
    cache: {key: true}             # absent: the wire's own default
    headers: []                    # absent: none
    embeddings: {model: text-embedding-3-small, per1M: "0.02"}
  anthropic:
    wire: anthropic
    baseURL: https://api.anthropic.com/
    apiKey: {env: HOST_ANTHROPIC_KEY}
    models: [...]

oauthClients:
  providers.substrate.reamde.dev/google:
    clientId: 1234.apps.googleusercontent.com
    clientSecret: {file: /run/secrets/google-client-secret}
    # Shown on Services and on the provider's setup step while the client
    # is in Google's Testing mode (see "Google: testing now, production
    # later").
    notice: >-
      Crane's Google sign-in is in testing. Ask Crane to add your Google
      address before you connect, and expect to reconnect once a week.

functionSecrets:
  providers.substrate.reamde.dev/firecrawl:
    apiKey: {file: /run/secrets/firecrawl}
    consumers: [websearch, scrapepage]
    pricePerCall: {websearch: "0.1", scrapepage: "0.1"}   # cents

budget:
  centsPerDay: 200                 # per repository, everything host-paid
```

The server refuses to boot when a key file is missing, a base URL is not
https, a `functionSecrets` or `oauthClients` entry names a package the
binary does not ship as a provider, or a host model has no price. Rotation
is a file change and a restart. An entry removed stops at the next start.

### LLM: an empty key on a host-listed row uses the host's transport

A live `llm/provider` row with an empty `apiKey` whose id the host lists
(`openai`, `anthropic`) is served by the **host transport**, and the row's own transport
fields are ignored: `wire`, `baseURL`, `headers`, `defaults`, `cache`,
`titleModel` and `pricing` all come from the host entry, each with the
absent-case behavior the example states.

Eligibility, case by case:

- A row the person deleted stays deleted (the seed is create-only and
  never brings it back), and an agent naming it is refused as today. Services
  shows the host-listed service as **Not set up** with **Restore**, which
  recreates the row with the seed's fields and no key.
- A row the person deleted and recreated under the same id with no key is
  host-backed again: eligibility is the id and the empty key, not the seed.
- An agent naming a row id the host does not list behaves as today: it runs
  on that row's own key or is refused.
- An agent asking a host-backed row for a model the host does not list is
  refused with a message naming the allowed models and saying how to add
  your own key.
- A row with the person's key behaves exactly as today.

Nothing the person writes reaches a host-paid request except the prompt and
a model name from the allowed list.

Credential-bearing LLM requests refuse redirects, for host and own keys
alike. `internal/llm/openai.go` and `anthropic.go` follow redirects today;
Go strips `Authorization` on a cross-host redirect but not `x-api-key` or a
row's own headers. `internal/egress/confined.go` already has the policy to
reuse (`CheckRedirect` returning `http.ErrUseLastResponse`).

### Embeddings: a service of its own

The host file names at most one `embeddings` entry across all its `llm`
entries, refused at boot otherwise. When no row claims `embedModel`, that
entry is the repository's embeddings provider. A row that claims it wins, by
the existing single-claimant rule (`internal/engine/embedprovider.go`), and
that row must carry its own key: a claimed `embedModel` never runs on a host
key, because the model and its price would be the person's choice. Nothing
is written to the seed, which is create-only and would never reach existing
rows.

Embeddings are therefore independent of the chat rows. Keying the `openai`
row moves chat to the person's key and leaves embeddings on the host's; the
person's own path is claiming `embedModel` on a keyed row. Services lists
embeddings as their own row, **Search by meaning**, so this is visible.

Vectors are stamped with the `(provider, model)` pair that bought them
(`embeddings.provider`, `embeddings.model`), and the semantic query, the
reconcile after an import or restore, and the "has vectors" check all take
that pair from `embedClaims`, which reads rows only
(`internal/engine/embedprovider.go`, `internal/engine/search.go`). The host
entry stamps the reserved provider id `host`, and every `embedClaims`
consumer goes through the resolver instead, so a host-backed repository is
not read as "no provider" (which would mark every vector stale and refuse
semantic search). Moving between the host's embeddings and your own changes
the pair, so the repository's vectors are bought again under the new one,
as they are today when a row's `embedModel` changes.

### OAuth clients: a fallback inside client resolution only

The host client is used only when the bundle's OAuth client input resolves
to **no record at all**. Ambiguous, dangling and incomplete inputs keep
their current setup items: a person who started their own client and broke
it sees that, not a silent switch. The id and secret are taken as one pair,
never a person's id with the host's secret. Only published providers listed
in `oauthClients` qualify.

Each account records the client its grant was issued to: a nonsecret
`oauthClient` property holding the client id (and `host` or `own`), declared
`writer: oauth` on each provider's account kind with a provider version
bump. Refresh resolves the current client and compares ids; on a mismatch
(the person added or removed their own client, or the host rotated to a new
client) the account reads "reconnect needed" and is not refreshed. An
account connected before the property existed has none and is treated as
connected through the person's own client, which is the only kind there
was. On a host-connected account the functions ignore the config's
`apiBase` test seam and call Google's own API base.

### Google: testing now, production later

Owner decision, 2026-11-06: Crane starts with a Google client in **Testing**
mode and moves to a production client, in a new GCP project, once the rest
of this plan has landed. What Testing mode means for people, and what the
plan does about it:

- Only addresses listed as test users on the client (at most 100) can
  consent. Anyone else gets Google's "access blocked" page. The host adds
  each person's address by hand, and the client's `notice` says so on
  Services and on the provider's setup step.
- Google expires a Testing client's refresh tokens after 7 days. The refresh
  then fails with `invalid_grant`, which the OAuth facility already turns
  into `tokenError` and a parked credential (decision 0180), so the account
  reads **Reconnect** on Services and the provider page. Nothing new is
  built for it; the notice sets the expectation.
- `gmail.readonly` is a restricted scope and works in Testing mode for
  listed users only.

The move to production is a client id change, which the `oauthClient` rule
above already handles: after the host swaps `clientId` and `clientSecret`
in the file and restarts, every account connected through the testing
client reads **Reconnect needed** once, and reconnecting issues a grant
from the production client. The production client needs Google's OAuth
verification for the sensitive scopes (contacts, calendar, Drive) and a
security assessment (CASA) for `gmail.readonly`; that is the host's work in
the new project and is tracked outside this plan.

### Function secrets: handed to named code, never stored

The host's Firecrawl key is not a `secret` record. At invocation, the engine
adds it to `config.settings.apiKey` only when all of these hold: the function
is one of the `consumers` of a package whose `source` is `published`, the
repository holds no own `apiKey` secret for that package, and the host budget
admits the call. It is not a fallback in generic secret reads or input
injection, so it never reaches the sealed store, an export or another
bundle's function.

Firecrawl moves from `samples/firecrawl` to
`providers.substrate.reamde.dev/firecrawl` and is hardened first: in
host-key mode the body calls exactly `https://api.firecrawl.dev` on port 443
with fixed paths, no loopback, and no redirects (`urllib`'s `urlopen`
follows them today). Triggers stay editable by the person, so how often the
function runs is bounded by the budget, not by the code. Existing sample
copies stay as they are and never receive the host key.

### Budget: host prices, host ledger, reserved before the call

The person's own spend cap (decision 0149, the setting
`substrate.reamde.dev/llm/spendCentsPerDay`) stays
theirs and unchanged. The host budget is separate and does not read anything
the person can write:

- Prices come from the host file: per model for chat and titles, per million
  tokens for embeddings (the embed client must return usage; it does not
  today, `internal/embed/embed.go`), per call for function secrets.
- The window is one UTC calendar day: `centsPerDay` covers the
  reservations a repository opened between 00:00 and 24:00 UTC, and resets
  at midnight UTC. This differs on purpose from the person's own cap, which
  is a rolling 24 hours, because a friend can read "resets at 1:00 in the
  morning" and cannot read a rolling window. The console shows the reset in
  the person's local time.
- **Ledger.** A control-plane table `host_spend_reservations (id,
  repository, opened_at, ceiling_cents, settled_cents, settled_at)` holds
  one row per host-paid call, so settlement is idempotent by id and expiry
  has a timestamp.
- **Reserve.** Before each call the engine reserves a ceiling in one
  transaction that sums the repository's rows for the day, refused when it
  would pass `centsPerDay`. The ceiling counts every billable part at its
  highest rate: the prompt estimate with a safety margin at the higher of
  the input and cache-write prices, plus `maxOutputTokens` at the output
  price; an embedding batch's token estimate with the same margin; the
  per-call price for a function.
- **Settle.** The call settles to its reported cost. A call whose usage is
  missing keeps its ceiling.
- **Expire.** A reservation unsettled after an hour settles at its ceiling,
  so a crash or a silent vendor charges more, never less. The overshoot is
  bounded by how far a vendor's real bill exceeds the margined estimate,
  which the host watches on its vendor dashboards.
- The ledger is host data and is not in a repository's recovery export.
  Restoring or rebuilding a repository leaves its host allowance as it was.
  Losing the host database loses at most one day of ledger, so the
  allowance restarts at full for that day; the host accepts that.
- The gate sits beneath every caller: agent runs, chat, title generation,
  retries, embeddings, scoped app tokens calling functions or search. At the
  limit the caller gets an explicit `quota` error naming the host's limit
  and the reset time, never an empty key.

### Installs, recorded in the repository

`install:` runs the ordinary published catalog install for each listed
package, at registration and at a repository's first open under a
configuration that lists a package it has not held. "Has not held" is read
from the repository's own changelog: if a bundle record for the package was
ever written there, including one later uninstalled, the host does not
install it again. That fact travels with the recovery export, so a restore
never reinstalls something the person removed, and no new table is needed.
It holds while the changelog keeps full history, which is today's policy
(`internal/api/watch.go`); a future compaction must keep each package's
"was installed" fact before it discards the records that show it.
A failed install is logged and retried at the next open; it shows on
Services as "Couldn't add" with the server's message.

### What the API tells the console

`GET /api/v1/host/services` answers one entry per service, resolved through
the **same** resolver dispatch, OAuth and injection use, so the page and the
bundle setup items cannot disagree:

```json
{
  "host": {"name": "Crane", "budget": {"centsPerDay": 200, "usedCents": 37, "resetsAt": "2026-11-07T00:00:00Z"}},
  "items": [
    {"id": "llm/openai", "label": "OpenAI", "target": "substrate.reamde.dev/llm/provider/openai",
     "source": "host", "readiness": "ready"},
    {"id": "llm/gemini", "label": "Gemini", "target": "substrate.reamde.dev/llm/provider/gemini",
     "source": "own", "readiness": "failing", "message": "the last call was refused: 401 invalid api key"},
    {"id": "oauth/providers.substrate.reamde.dev/google", "label": "Google",
     "source": "host", "readiness": "needs-connect"}
  ]
}
```

Three separate facts, never one state: `source` (`host`, `own`, `none`),
`readiness` and the one host budget. Each readiness value says where the
server reads it:

- `ready`: the service resolves (a key, a client, an installed package).
  It does not claim a call has succeeded.
- `not-set-up`: source `none`, or a host-listed row the person deleted.
- `needs-connect`, `reconnect-needed`: an OAuth provider's accounts, from
  their `tokenStatus`, `tokenError` and the `oauthClient` comparison.
- `failing`: the newest open alert (decision 0148) of an agent or function
  whose provider row or package this service is, with its message. A row
  itself records no last error.
- `install-failed`: the last install attempt's error, kept in memory by
  the server until the next attempt.

`withdrawn` is not a server state: the server keeps no record of what the
host once offered. The console derives it from the seen ids on
`consolepreference`: an id it showed before that the server no longer
lists. Readable by the
owner and by a full-access app token; a scoped app token is refused. A named
struct in `internal/substrate`, pinned by the wire golden. `server.json`
gains a `hostServices` feature so a client knows to ask. The bundle status
`setup` items and the keyless-provider probe call the same resolver, so a
provider page stops asking for what the host covers.

### The console: Services

`/services`, in the sidebar foot beside Settings. Account connection details
stay on each provider's page; Services links there.

- **First sign-in**: registration lands on `/services` with a welcome header
  (unless it was started from an app's authorization or a domain-verification
  link, which keep their return addresses). It says what works now (chat,
  search, web search), what needs the person (connecting accounts), and that
  any service can use their own key. **Done** dismisses it.
- **Later**: a plain list. A service the person has not seen carries **New**,
  and the sidebar entry a dot until the page is opened. Dismissal and seen
  ids are two additive properties on `consolepreference`, with a version
  bump.
- **No host defaults** (a self-hosted substrate, `hostServices` absent from
  `server.json`): registration lands where it does today, not on Services;
  the page lists the same services with source `none` and the provider
  pages' existing "add your key" actions, and no welcome copy about a host.

Sections:

1. **Today's allowance**: one bar for the host budget, used of total, and
   when it resets in the person's local time.
2. **Provided by _host_**: each host-backed service, its readiness, and **Use
   your own key**. **Search by meaning** (embeddings) is its own row, whose
   button opens the provider row to claim `embedModel` there.
3. **Using your own**: each own-key service, its readiness (an invalid key
   shows the vendor's refusal), **Replace key**, and, where the host offers
   that service, **Use _host_'s again** (confirmed; it clears your key).
4. **Connect your accounts**: OAuth providers with their readiness
   (**Connect**, **Reconnect**), token providers with **Set up**.
5. **Not set up**: services with source `none`, such as the seeded `gemini`
   row the host does not list, each with **Add key**, and host-listed rows
   the person deleted, with **Restore**.
6. **Couldn't add / Withdrawn**: installs that failed and services the host
   stopped offering, each with what to do. Withdrawing is for every
   repository at once (an entry removed from the file); there is no
   per-repository switch.
7. **Who sees what**: host-paid calls are made with the host's vendor
   accounts and billed to it; your own key bills your account. Either way
   this server runs your agents, so the host operating it can see the
   requests; your own key changes who pays, not who operates the server.

Home stays as decisions 0130 and 0148 leave it: it asks for nothing.

A prototype of the page, drawn from fixed data and not wired to the API, is
on the `screenshots/host-defaults` branch with these screens:

- [First sign-in](https://raw.githubusercontent.com/geoah/substrate/screenshots/host-defaults/1-welcome.png)
- [Services, steady state](https://raw.githubusercontent.com/geoah/substrate/screenshots/host-defaults/2-services.png)
- [Replacing the host's key with your own](https://raw.githubusercontent.com/geoah/substrate/screenshots/host-defaults/3-own-key.png)
- [The prototype's source](https://raw.githubusercontent.com/geoah/substrate/screenshots/host-defaults/services-prototype.tsx)

## What this amends

- The `llm/provider` comment and docs/agents.md: an empty key on a row the
  host lists is served by the host's transport. Needs a decision record.
- docs/bundles.md: the OAuth client resolution gains the host step;
  function injection gains host secrets for named consumers. Same record.
- Firecrawl changes tier, sample to provider (decision 0048's line).

## Implementation plan

Three pull requests, in order. Each lands behind the host file: a deployment
without `SUBSTRATE_HOST_DEFAULTS` behaves exactly as today, which is what
every existing test keeps proving.

### Phase 1: LLM, embeddings, budget, Services

Makes agents and semantic search work on day one.

1. **Decision record** (the `adr` skill; number checked with `mise run
   decisions:check`): an empty key on a host-listed row runs on the host's
   transport; host keys never enter a repository; the host budget is the
   host's ledger. It amends the "no host key" line of
   `kinds/substrate.reamde.dev/llm/provider.yaml` and docs/agents.md.
2. **Host file**: a new `internal/hostdefaults` package parses and
   validates the file (https base URLs, a price for every model, at most one
   `embeddings` entry, packages the binary ships as providers, key files
   present); `internal/config` gains `SUBSTRATE_HOST_DEFAULTS`; an invalid
   file refuses the boot, like an invalid `site.yaml`. Documented in
   docs/operations.md.
3. **Host transport**: `resolveProvider` (`internal/engine/agentloop.go`)
   takes the host entry whole for an empty-key, host-listed row and refuses
   a model outside its list. `internal/llm/openai.go` and `anthropic.go`
   refuse redirects on every credential-bearing request, reusing the
   `CheckRedirect` of `internal/egress/confined.go`.
4. **Host embeddings**: `internal/engine/embedprovider.go` falls back to the
   host entry when no row claims `embedModel`, stamping the provider id
   `host`; every `embedClaims` consumer (the drain, the reconcile, the
   semantic arm's pair check in `search.go`, the agent loop) reads the
   resolver instead of the rows; `internal/embed/embed.go` returns usage so
   the ledger can settle.
5. **Ledger and gate**: a SQL migration adds `host_spend_reservations`; a
   new `internal/engine/hostspend.go` reserves, settles and expires; the
   gate runs beneath agent runs, chat, titles, embeddings and scoped app
   calls, and answers a `quota` error.
6. **Read**: `GET /api/v1/host/services` with its named struct in
   `internal/substrate` (wire golden regenerated), the handler in
   `internal/api` tested against `fake_test.go`, and a `hostServices`
   feature in `server.json`. The keyless-provider setup item reads the same
   resolver.
7. **Console**: `/services` (the prototype behind this plan's screenshots
   is the starting point; it is not in the route tree until this step, and
   the host's name comes from `site.yaml`, never from code), the sidebar entry and its dot, the registration
   redirect in `pages/register.tsx` that keeps app and domain return
   addresses, `types.ts` matching the golden, and two additive
   `consolepreference` properties (welcome dismissed, seen service ids) with
   the kind's version bumped.

Checks: engine tests per rule, each shown to fail against the code before
the change (empty key uses the host transport; an edited `baseURL` or
header never reaches a host-paid request; a deleted row is not revived; a
model off the list is refused; redirects are refused; reservations admit,
refuse at the limit, settle, and expire at the ceiling; a scoped app token
is held by the same gate; host embeddings survive an import without marking
vectors stale; a deployment with no host file behaves as before), the wire golden, console tests for each
Services state, and a smoke on a local substrate with a host file pointing
at real keys.

### Phase 2: Google sign-in and installs

1. Client resolution (`internal/engine/inputs.go`, `oauth.go`) gains the host
   step for an input that resolves to no record, taking id and secret as a
   pair, for published providers listed in `oauthClients` only.
2. The account kinds of `google`, `github` and `whoop` declare
   `oauthClient` with `writer: oauth`, each provider's version bumped
   (`mise run kinds:check`). The callback writes it; refresh compares it and
   marks a mismatch "reconnect needed".
3. Host-connected Google accounts ignore `apiBase`.
4. `install:` runs the catalog's published install in `CreateRepository`
   (`internal/engine/engine.go`) and at a repository's first open, skipping
   a package the changelog shows was ever held.
5. Bundle status `setup` items (`internal/engine/bundles.go`) read the same
   resolver, so a host-covered client is not a setup item. The client's
   `notice` reaches Services and the provider page.

Checks: engine tests for the pair rule, the mismatch, the no-revive install
rule across export and restore, and a real connect of a listed test user
against the Testing client on Crane.

### Phase 3: Firecrawl as a provider

1. `providers.substrate.reamde.dev/firecrawl`, the sample's closure moved
   and hardened. The bodies already pin the origin to
   `https://api.firecrawl.dev`; what changes is no loopback in host mode and
   no redirects. `samples/firecrawl` stays for people who want to edit it,
   and its copies never receive a host key.
2. Injection (`internal/engine/invocationconfig.go`) adds the host secret
   for the named consumers only, after the gate admits the call at its
   per-call price.
3. Services lists it; the welcome copy names web search.

Checks: `internal/providertest` cases for host mode (key injected, refused
past the budget, absent for a sample copy, absent for a function not named
as a consumer).

### After phase 3: Google production client

The host creates the production client in a new GCP project, completes
Google's verification, swaps the client in the file, and every Google
account reconnects once.

## Owner decisions, 2026-11-06

- Google stays in Testing mode for now; a production client in a new GCP
  project follows the rest of this plan.
- One per-repository daily budget is enough; no host-wide monthly stop.
- No per-repository withdrawal: abuse is not a concern for this group, and
  removing an entry from the file withdraws it for everyone.
- Speech (ElevenLabs) and transcription (Soniox) are out of scope for now.
  A later provider would follow phase 3's rule for function secrets.
