# Agents

**Agents are alpha and not part of the frozen v1 contract.** The `agent`
kind, the agent-loop vocabulary (`llmprovider`, `llmthread`, `llmmessage`),
and the `/agents` chat and call wire are all alpha and unfrozen at v1. They
are marked alpha in the docs and in [API discovery](api.md#discovery),
which lists the agent surface under the `agents` feature carrying the
stability `alpha`, and they may change, or be superseded, without counting as
a v1 wire break. Treat agents as a preview.

The rest of the surface is not frozen either, so there is no frozen core to
retreat to yet. Every other feature discovery lists reports `beta`: served and
supported, with the shape still moving until v1 freezes it. Read
[discovery](api.md#discovery) for what each value promises, and pin the server
version where a break would cost you.

An `agent` is a callable whose body is an **LLM loop**, run host-side. It has
one reference and the same four ways in as a [function](functions.md): a
trigger delivery (a `callable` whose `kind` is `core.substrate.reamde.dev/agent`), the
call API, a sub-agent call, and chat. Its actor is `agent:<authority>:<name>`,
its own machine hand, held apart from a function's so an agent and a function
of one name under one authority are two writers; its dispatch stamps the
bundle tier on its writes, exactly like a function's. A
bundled agent obeys its bundle's
[lifecycle](bundles.md#install-and-lifecycle): while the bundle is
disabled or uninstalled, every entry refuses with a guard error.

The `agent` kind is a first-class core kind, `core.substrate.reamde.dev/agent`, declared
like every other core kind. Its runtime vocabulary lives in one authority of
core's own — `core.substrate.reamde.dev/llmprovider`,
`core.substrate.reamde.dev/llmthread` and `core.substrate.reamde.dev/llmmessage`
— beside the rest of the substrate's machinery.

## The manifest

Here is the URL harvester's classifier, the agent a freshly fetched page is
handed to:

```yaml
kind: core.substrate.reamde.dev/agent
metadata: {id: web.bundles.substrate.reamde.dev/pageclassifier}
data:
  authority: web.bundles.substrate.reamde.dev
  description: Classify a fetched page and route it to the reading-list agent.
  prompt: |
    You are the page classifier. Read the page in the first message, decide
    whether it is an article, a tool, or a video, set its class with the
    setclass tool, then hand the page to the reading-list agent.
  provider: default
  model: anthropic/claude-opus-5
  tools:
    - function: web.bundles.substrate.reamde.dev/setclass
  agents: [web.bundles.substrate.reamde.dev/readinglistagent]
  budgets: {maxTurns: 4, maxToolCalls: 8, depth: 3}
  permissions:
    writes:
      - web.bundles.substrate.reamde.dev/page
      - core.substrate.reamde.dev/recordpatchrequest
```

`data` carries:

- **`description`** (required), model-facing: the agent is its own tool card
  wherever it appears as a sub-agent.
- **`prompt`** (required, at most 64 KiB): the row is the prompt store, and the
  changelog's full retention is its version history.
- **`provider`** (required): a `core.substrate.reamde.dev/llmprovider` record id — **where**
  the completions are bought, resolved at dispatch and never at load.
- **`model`** (required): the model id sent on every completion, a plain string
  the provider's endpoint understands — a gateway's alias
  (`anthropic/claude-opus-5`) on a row pointed at that gateway, the bare
  `claude-opus-5` on an `anthropic`-wire row.
  The substrate keeps no model table: re-pointing an agent at a cheaper model
  is one word here.
- optional **`params`**: `{temperature, maxTokens}` for this agent's calls,
  merged over the provider row's `defaults`. The set is closed, so a knob the
  loop could not pass on is a load error rather than a line that silently does
  nothing.
- **`tools:`**, the functions the model may invoke (below).
- **`agents:`**, sub-agent references (self-reference is a load error).
- **`budgets:`** bounds one run: `maxTurns` (default 8, max 64),
  `maxToolCalls` (default 32, max 256), `deadlineSeconds` (default 120, max
  600), and `depth` (default 3, max 3).
- **`permissions:`**, what the agent is allowed to do while it runs: a
  function's grant, minus the three an LLM loop has no body to spend.
  - **`writes:`**, which record kinds it may create or change, the change
    requests it proposes among them. Empty means the agent writes nothing.
  - optional **`reads:`**, which record kinds it may read and how much: a
    `kinds:` allowlist plus `budgets:` calls and rows. Leave it out and the
    `query` tool is withheld.
- optional **`subagentOnly:`**, the chat-surface withholding: `true` keeps the
  agent off the console's chat list and makes the chat API refuse it, while
  sub-agent calls, the call API and triggers still dispatch it. An
  llm-as-judge is the shape it exists for. Any agent, marked or not, remains
  selectable as another agent's sub-agent.

Tool functions, sub-agents, and every emitted and read kind resolve against
the registry at admission, where same-batch installs count.

What fires an agent is the same [trigger](functions.md#triggers) record a
function's delivery rides; only the `callable` reference names the other kind:

```yaml
kind: core.substrate.reamde.dev/trigger
metadata: {id: web-classify-on-page}
data:
  properties:
    enabled: true
    source:
      record:
        kinds: [web.bundles.substrate.reamde.dev/page]
        ops: [update]
        when: 'record != null && record.properties.fetch == "fetched" && !("class"
          in record.properties)'
    callable: {kind: core.substrate.reamde.dev/agent, id: web.bundles.substrate.reamde.dev/pageclassifier}
```

Because vocabulary is records, a parsed agent projects to a row the console
lists and creates like any other, and **the properties are the declaration.**
The row holds the manifest's own keys, one property per key: `params`, `tools`,
`budgets` and `permissions` are declared properties like `prompt` and `model`,
and the loader rebuilds the registry from exactly these. There is no
`definition` blob and no projected mirror beside it, so what an author writes is
what gets stored, and a write that names the retired blob is refused rather than
half-obeyed.

## Tools

A `tools:` entry names its **function**: `{function: <function reference>}`,
optionally with `name` and `description` to recolor this agent's prompt context
without changing the function's canonical card. Tool names are unique per agent.
The key is `function` because an entry admits nothing else: a sub-agent is named
on `agents:`, and `callable` is the [trigger](functions.md#triggers)'s word,
where a target really may be a function or an agent.

That is the only arm, because the four built-ins are
[**host functions**](functions.md#host-functions) — `runtime: host` records core
ships — so an agent names one exactly as it names a bundle's function:

```yaml
  tools:
    - function: core.substrate.reamde.dev/graphql
    - function: core.substrate.reamde.dev/propose
    - function: web.bundles.substrate.reamde.dev/setclass
```

Three older spellings are refused, each naming its replacement. A bare string
(`tools: [query]`) named the arm by its value, so a typo in a built-in's name
silently became a function nothing declares. `{builtin: query}` was the
interim arm that split the union explicitly: it made the built-ins the one thing
an agent could name that no record declared, and it is gone now that they are
records. And `{callable: …}` was this key's first name, before it was clear an
entry could name only a function.

- **`core.substrate.reamde.dev/query`** is the capability-scoped read, and
  requires `permissions.reads`, a load error otherwise. A get outside the allowlist
  answers like an absent id; list and search clamp to the remaining row budget;
  a blown budget is a tool error the model sees.
- **`core.substrate.reamde.dev/graphql`** is the whole-repository read: the
  **same** schema and resolvers the `/graphql` endpoint executes
  (`internal/gql`), run in-process against the loop's dataset under the agent's
  actor. Declaring it is the grant, and it grants reads only: the document is
  parsed first, and a mutation or subscription in it is a tool error naming
  where those verbs live. A result over 64KB is refused with a narrowing hint
  rather than truncated. Use `query` instead when an agent should read a few
  named kinds under a row budget; use `graphql` when the agent's job is the
  graph itself.
- **`core.substrate.reamde.dev/mutate`** executes GraphQL mutations (`put`,
  `patch`, `delete`, `link`, `unlink`) through the same resolvers, and requires
  a non-empty `permissions.writes` (a load error otherwise). Every written kind is resolved
  and held to the agent's **effective** emit before the write applies, so a
  sub-agent's ceiling narrows it like any other effect; `merge` and `split`
  refuse outright, as fusing identities is the owner's reviewed decision. Writes
  ride the full public path (kind guards, schema-record admission) under the
  agent's actor.
- **`core.substrate.reamde.dev/propose`** is the reviewed write, and requires
  `permissions.writes` to name
  `core.substrate.reamde.dev/recordpatchrequest`. It lands one
  [`recordpatchrequest`](projection.md#the-patch-request-sibling), never a
  direct mutation. It carries a `rationale` and an `op`: `patch`, the default,
  names an existing `target` and a `diff` the accept applies; `create` names a
  `targetKind`, a stable `targetId` and a `diff` the accept mints
  create-if-absent; `delete` names an existing `target` the accept tombstones.
  The diff is validated and normalised at propose time — the `properties`
  wrapper is required or a bare property map is coerced into it, and every
  property must be writable on the target kind — so a malformed proposal (a
  wrapper-less diff, an immutable or unknown key) is a tool error the model
  sees, never a bad request reaching the owner's inbox. The loop stamps the
  proposing `thread` onto the request, which is where the decision reports
  back (below).
- **A function tool** runs through the same runner invoke a host call uses, its
  declared [arguments and returns](functions.md#arguments-and-returns) enforced,
  and its compiled argument schema is the card the model is shown. Its effects
  pass both grants: decoded against the function's own, then held whole to the
  agent's emit. One effect outside the agent's emit fails the whole tool
  call as a result the model sees, and nothing applies.

Applied effects land in their own transaction under the agent's actor, each row
recording the triggering seq as its cause. Creation and deletion still flow
through the reviewed request: `propose` emits the request, and accepting it is
the write.

**Every dispatch's committed writes ride the tool row.** The engine stamps
`changes` onto the tool's `llmmessage` — one `{seq, op, kind, id}` entry per
changelog row the dispatch committed, whether a `mutate` mutation, a
`propose`'s request row, or a function tool's applied effects — so any reader
of the thread (the console, GraphQL) resolves WHAT changed from the changelog
instead of parsing tool payloads. A rolled-back dispatch stamps nothing, and a
sub-agent call stamps nothing on the parent: the child thread's own rows carry
the child's writes.

## The decision loop

A request an agent's `propose` landed knows its thread, so the decision
reports back instead of vanishing into the inbox. When anybody decides it —
the owner in the console, a judge agent through `mutate` — the deciding
transaction also writes one `system` message into the proposing thread: the
content is a JSON envelope (`event: "proposalDecision"`, the request's record
path, the verdict, the target and — on an accepted patch or create — the
target's new version), and its `changes` carry the decision's own changelog
entries (the request's patch, and the accept's apply). Decision and report
land together or not at all.

After commit the thread **resumes**: a continuation with no new user turn, in
which the replayed system row is what the model reacts to — on the wire it
travels as user content, since the system slot belongs to the agent's prompt.
Resume is always-on, best-effort and in-process: a thread that is mid-turn
refuses the lease and picks the decision up on its next continuation, and a
request no thread proposed (a human's, a function's) reports and resumes
nothing.

## Sub-agents, budgets, and the emit ceiling

Sub-agents are child invocations with their own budgets and a child thread
carrying the `parent` edge. `agentDepth` is 0 at the root and increments per
hop, checked against the calling agent's `budgets.depth` (hard-capped at 3), so
the default chain refuses the hop that would run at depth 4, as a tool error
the caller sees. It is a separate counter from causal depth: an agent hop never
moves the causal one, while a function sub-call inside a tool still rides it.

**The emit ceiling rides every hop.** A child's effective emit is its own
declared emit intersected with the caller's effective emit, enforced against
function-tool effects and `propose` alike. An empty-emit parent delegating to a
write-capable child yields a child that writes nothing: no chain of children can
ever write a kind its narrowest ancestor could not. Accepting a proposed
request is authorized as the transitive write too, so a function or agent that
drives an accept must have the concrete written kind in its effective emit, or
the accept refuses. An owner's acceptance stays unbounded.

## Threads, messages, and cost

The conversation state is the run: there is no separate run record. A `thread`
is written as the loop runs under the agent's actor, carrying `agent`,
`provider`, `model`, `mode`, `status` (`running` then
`ok`/`overbudget`/`error`), `agentDepth`, the tallies (`turns`, `toolCalls`,
the token counts, `costUSD`), and
`startedAt`/`finishedAt`. A `message` carries role (`user`, `assistant`,
`tool`, or the engine-written `system`), content, turn, the tool-call audit,
the engine-stamped `changes`, and the required `thread` it belongs to.
Self-actor exclusion covers the transcript, so an agent's own trigger never
redelivers its thread and message writes.

`agent`, `parent` and `thread` are **references**, not edges: a thread is the
audit row of a run and has to keep naming the agent that ran it, and an edge
would be deleted along with its target. Each declares its `inverse`, so the
graph reads the same link from the other side — an agent's `threads`,
a thread's `messages` and its `subAgentThreads`.

**Cost rolls up onto the root thread**: every loop on a chain adds to one
shared tally, so the root thread's numbers include every descendant while a
child thread carries only its own. Pricing is data on the provider row, keyed
by model id, never a table in code. The loop terminates on the final tool-free
reply, any budget, or its deadline; over-budget is a settled outcome (thread
`overbudget` with a reason), never a park.

An agent delivery is **at-least-once** where a function delivery is
effectively-once: the loop's writes are incremental and the cursor advances
only after the loop settles, so a lost swap means a concurrent dispatcher also
ran and this run's thread stands as a duplicate trace. A loop error (the LLM
transport failing) rides the ordinary retries and parks, each attempt a fresh
thread but not fresh idempotency keys — a tool's key derives from the stable
delivery identifier plus the call path and the tool ordinal, so an effectful
tool that honors keys never double-fires across a retry. Thread ids are trace
ids only; they never enter a key.

## Providers

An `llmprovider` row is one place completions are bought, as pure data:
`name`, `wire`, `baseURL`, a secret `apiKey`, `headers`, `defaults`
(request params the agent's own `params` merge over), and `pricing`.

**`wire` is a protocol, not a company** — `openai`, `anthropic`, or `azure`,
the three wires an adapter speaks. OpenRouter, LiteLLM, Together, Groq and a
local Ollama are all `wire: openai` rows differing only in `baseURL`, so adding
one is a record and never code; Anthropic's own API is `wire: anthropic`,
spoken natively rather than through a translating gateway; `azure` needs its
deployment `baseURL` spelled out. New code is only ever needed for a new wire,
and a model that speaks none of these three wraps as a function tool, never as
the transport.

`pricing` is a table, one row per model, keyed by the model id **as sent** —
because one provider row serves many models: `claude-opus-5` on an
`anthropic`-wire row, the gateway's alias (`anthropic/claude-opus-5`) on a row
that speaks to a gateway. A model absent from the table leaves the thread's
`costUSD` at 0 and the token tally authoritative.

```yaml
pricing:
  - {model: claude-opus-5, inputPer1M: 5, outputPer1M: 25}
```

**Every one of these is declared, not a json blob.** `wire` is an enum of the
three wires, so a typo is refused at the write; `defaults` is an object of the
two request knobs there are (`temperature`, `maxTokens`); `headers` and
`pricing` are repeated objects whose key is a declared field (`name`, `model`),
and a later row for the same key wins. A map is declarable: a property marked
`keyed: true` is one, which is how a kind's own `properties` block stays a map.
These two stay lists because each row's key is a value with a name of its own.

**Every row carries its own endpoint and its own key.** There is no host
gateway and no host key: the server takes no LLM configuration at all, so
nothing process-wide can travel to a repository-chosen endpoint. An `openai` or
`azure` row that names no `baseURL` refuses to resolve, and every wire requires
the row's own `apiKey`. Only `anthropic` may leave `baseURL` empty, for its
official endpoint.

**One row buys the embeddings.** A row that declares `embedModel` is the
repository's embeddings provider, and only one row may declare it. Because only
the `openai` wire has an embeddings endpoint, a row on any other wire that
names an `embedModel` is refused at the write, as is a model whose vectors are
not 1536 wide ([decision
0026](decisions/0026-embedding-vectors-are-1536-wide-or-refused.md)). Every
stored vector names the row and the model that produced it, and semantic search
scores only the current pair's vectors, so changing either hides the older ones
rather than mixing two models' distances. `substratectl --dsn … repository
reembed <username>` and `POST
/api/v1/core.substrate.reamde.dev/embeddings/reembed` queue their replacement.

**Nothing seeds a provider.** A fresh repository holds no `llmprovider` row at
all: a row is where the wire, the endpoint and the key live, and a substrate
cannot invent a key — one shipped without it only postpones the failure to the
first dispatch while looking configured. An agent naming a row that is not
there refuses at dispatch and says which row it wanted.

The web and notes bundles' agents name `provider: default`, so a repository
that installs either wants a row at that id, and no shipped bundle writes
one. The LLM example (**Registry → Examples**) installs two correctly-shaped
keyless rows at `anthropic` and `openai`, which its own agents name directly.
Write a `default` row yourself, by hand as the document below or by copying
one of those two. There are
no `cheap`/`mid`/`strong` rows: a tier was a model id hiding behind a name, and
the model is the agent's own word now.

### Registering a provider

A provider is a record, so adding one is a write — `apply -f`, or the console
at **Data → `core.substrate.reamde.dev` → llmproviders → New**. (The Agents
page does not list providers: an agent names one by id, and that pointer reads
on the agent's own record.) All three below are ordinary data documents:
`data.properties`, never a declaration.

```yaml
# OpenRouter — the OpenAI wire at its own endpoint. So is LiteLLM, Together,
# Groq or a local Ollama: same wire, different baseURL.
kind: core.substrate.reamde.dev/llmprovider
metadata: {id: openrouter}
data:
  properties:
    name: OpenRouter
    wire: openai
    baseURL: https://openrouter.ai/api/v1
    apiKey: sk-or-…
    headers:
      - {name: HTTP-Referer, value: https://substrate.example}
      - {name: X-Title, value: substrate}
---
# Anthropic, natively. No baseURL: the official endpoint.
kind: core.substrate.reamde.dev/llmprovider
metadata: {id: anthropic}
data:
  properties:
    name: Anthropic
    wire: anthropic
    apiKey: sk-ant-…
    pricing:
      - {model: claude-opus-5, inputPer1M: 5, outputPer1M: 25}
---
# Azure OpenAI. The deployment endpoint is the row's, and so is the key.
kind: core.substrate.reamde.dev/llmprovider
metadata: {id: azure}
data:
  properties:
    name: Azure OpenAI
    wire: azure
    baseURL: https://example-resource.openai.azure.com
    apiKey: …
```

Every one of the three carries its own `apiKey`, and must: there is no host key
to fall back to. Nothing checks that at write time — a half-written row applies
fine and refuses at the first dispatch that resolves it, naming the row and
what it lacks. The embeddings rules are the exception, and are checked at the
write, because the row that carries them is resolved by a background loop with
nobody watching:

```yaml
# The embeddings provider: one row per repository declares embedModel.
kind: core.substrate.reamde.dev/llmprovider
metadata: {id: vectors}
data:
  properties:
    name: Vectors
    wire: openai
    baseURL: https://api.openai.com/v1
    apiKey: sk-…
    embedModel: text-embedding-3-small
```

`apiKey` is secret-typed: every read surface hands back `<redacted>`, and
writing the sentinel back is a round trip, so

```sh
substratectl get llmproviders -o yaml
```

is both a safe way to read the rows and directly `apply -f`-able — an edit of
the baseURL beside an untouched key is one round trip, and the key never
leaves the box.

### Setting or rotating the key

The key is a property, so it is a record write like any other: **Data →
llmproviders → the row → Edit**, put it in `apiKey`, apply. Because
`apiKey` is secret-typed it reads back redacted — the field shows nothing of
what is stored, writing it again replaces it, and that is also how a rotation
is done. There is no separate credentials screen, and no way to read a stored
key back out of the substrate.

From the CLI it is the same write. A here-document keeps the key out of your
shell history:

```sh
cat <<'EOF' | substratectl apply -f -
kind: core.substrate.reamde.dev/llmprovider
metadata: {id: anthropic}
data:
  properties: {apiKey: sk-ant-…}
EOF
```

`apply` merges and never prunes, so naming `apiKey` alone leaves the row's
wire, endpoint and pricing exactly as they were.

The editor is a YAML textarea today, which knows nothing about the kind it is
editing — a number where a string belongs is refused by the substrate rather
than by the field. Making it schema-aware is
[issue 17](https://github.com/geoah/substrate/issues/17).

### Testing a provider

Point an agent at the row and run it once. The smallest agent that proves a
provider works is a throwaway of your own authority:

```yaml
kind: core.substrate.reamde.dev/authority
metadata: {id: smoke.example.com}
data:
  version: 1
---
kind: core.substrate.reamde.dev/agent
metadata: {id: smoke.example.com/echo}
data:
  authority: smoke.example.com
  description: Smoke-test one provider.
  prompt: Reply with exactly OK.
  provider: anthropic
  model: claude-haiku-4-5
  budgets: {maxTurns: 1}
```

No `tools:` and no `permissions:`, so it can write nothing at all. Swap `provider:`
for the row under test and `model:` for an id that row serves — on the
`default` gateway row the alias form (`anthropic/claude-haiku-4-5`), on a
native `anthropic` row the bare id.

There is no agent verb on `substratectl` — `function call` is functions only —
so a run is the call API or the console's chat:

```sh
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" -d '{"input": "ping"}' \
  http://localhost:8080/api/v1/core.substrate.reamde.dev/agents/smoke.example.com%2Fecho/call
```

The id carries a `/`, so the path segment spells it `%2F`. The answer carries
`reply` and the `thread` id, and the thread is the durable half:

```sh
substratectl get llmthreads <thread> -o yaml
```

`status: ok` with a `turns`/`promptTokens`/`completionTokens` tally is a
working provider, and `costUSD` is non-zero exactly when the row prices the
model it just used. The two failures read differently, and the difference is
where they happen:

- **A row that cannot resolve** — no `wire`, a `baseURL` without an `apiKey`,
  an `azure` row missing either — refuses **before** a thread exists. The call
  answers `422` naming the row and what it lacks; nothing is minted.
- **A row that resolves but does not work** — a wrong key, an unreachable
  endpoint, a model id the endpoint does not serve — settles the thread it
  already opened at `status: error`, with the transport's own words in
  `reason`.

## Calling an agent

`POST /api/v1/core.substrate.reamde.dev/agents/{name}/call` with `{"input": …}` runs the
loop once: the input becomes the first user message, and the answer carries
`reply`, `thread`, `status`, `effects` with its `effectsByAction` breakdown,
`turns`, `toolCalls`, and the token and `costUSD` tallies. Unlike a function
call, something durable is minted — the thread is the trace.

`POST /api/v1/core.substrate.reamde.dev/agents/{name}/chat` with `{"thread"?, "message"}`
opens or continues a thread and streams the loop: `application/x-ndjson`, one
JSON object per line keyed by `kind` (`thread` first, `delta` carrying `text`
per streamed token, `toolStarted` and `toolFinished` around each dispatch, one
`done` carrying the settled result under `result`). A loop that fails after the
`200` status line has already gone out terminates the stream with an `error`
event instead, the same choice the
[changelog](changelog.md#frames-and-the-horizon) makes, so a failure is never a
`done` with no result. A continued thread replays its prose history (user and
assistant turns; tool exchanges are audit, not context) and keeps one running
tally on the thread row; one active turn per thread is enforced by a lease, so
a second concurrent turn is refused as a conflict, leaving nothing on the
thread, and a crashed turn's expired lease is taken over by the next
continuation.

Next: the [bundles catalog](bundles-catalog.md), what ships today.
