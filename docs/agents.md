# Agents

**Agents are alpha and not part of the frozen v1 contract.** The `agent`
kind, the agent-loop vocabulary (`llm/provider`, `llm/thread`, `llm/message`,
`llm/interaction`), and the agent call and chat routes are all alpha and
unfrozen at v1. They
are marked alpha in the docs and in [API discovery](api.md#discovery),
which lists the agent surface under the `agents` feature carrying the
stability `alpha`, and they may change, or be superseded, without counting as
a v1 wire break. Treat agents as a preview.

This page is about the agents a substrate runs. A program that uses a
substrate from outside, with a token, reads
[Using a substrate from an agent](for-agents.md) instead.

The features of the supported REST surface (`triggers`, `functions`,
`bundles`, `blobs`, `changefeed`) report `stable`: frozen for v1, additive
only, so there is a frozen surface to retreat to. `export` and `search`
report `beta`, and `embeddings` is the other `alpha` entry. Read
[discovery](api.md#discovery) for what each value promises, and pin the
server version where an alpha break would cost you.

An `agent` is a callable whose body is an **LLM loop**, run host-side. It has
one reference and the same four ways in as a [function](functions.md): a
trigger delivery (a `callable` whose `kind` is `substrate.reamde.dev/core/agent`), the
call API, a sub-agent call, and chat. Its actor is
`agent:<authority>:<package>:<name>`, its own machine hand, held apart from a
function's so an agent and a function of one name in one package are two
writers; its dispatch stamps the
bundle tier on its writes, exactly like a function's. A
bundled agent obeys its bundle's
[lifecycle](bundles.md#install-and-lifecycle): while the bundle is
disabled or uninstalled, every entry refuses with a guard error.

The `agent` kind is a first-class core kind, `substrate.reamde.dev/core/agent`, declared
like every other core kind: a manifest document is a record of a core kind
whatever package it describes. Its runtime vocabulary is the SECOND seeded
package, `substrate.reamde.dev/llm` — `provider`, `thread`, `message` and
`interaction` — seeded into every repository exactly as core is (decision
record [0077](decisions/0077-the-llm-kinds-live-in-their-own-seeded-package.md)).

## The manifest

Here is the reading-list sample's classifier, the agent a freshly fetched page is
handed to:

```yaml
kind: substrate.reamde.dev/core/agent
metadata: {id: samples.substrate.reamde.dev/readinglist/pageclassifier}
data:
  authority: samples.substrate.reamde.dev
  package: readinglist
  description: Work out whether a page is an article, a tool or a video, then
    hand it to the curator.
  prompt: |
    You are the page classifier. Read the page in the first message, decide
    whether it is an article, a tool, or a video, set its class with the
    setclass tool, then hand the page to the reading-list agent.
  provider: openai
  model: gpt-5
  params:
    reasoningEffort: minimal
  tools:
    - function: samples.substrate.reamde.dev/readinglist/setclass
  subagents: [samples.substrate.reamde.dev/readinglist/curator]
  budgets: {maxTurns: 4, maxToolCalls: 8, depth: 3}
  permissions:
    writes:
      - samples.substrate.reamde.dev/readinglist/page
      - substrate.reamde.dev/core/recordpatchrequest
```

`data` carries:

- **`description`** (required), model-facing: the agent is its own tool card
  wherever it appears as a sub-agent.
- **`prompt`** (required, at most 64 KiB): the row is the prompt store, and the
  changelog's full retention is its version history.
- **`provider`** (required): a `substrate.reamde.dev/llm/provider` record id — **where**
  the completions are bought, resolved at dispatch and never at load.
- **`model`** (required): the model id sent on every completion, a plain string
  the provider's endpoint understands — a gateway's alias
  (`anthropic/claude-opus-5`) on a row pointed at that gateway, the bare
  `claude-opus-5` on an `anthropic`-wire row.
  The substrate keeps no model table: re-pointing an agent at a cheaper model
  is one word here.
- optional **`params`**: `{temperature, maxTokens, reasoningEffort}` for this
  agent's calls, merged over the provider row's `defaults`. The set is closed,
  so a knob the loop could not pass on is a load error rather than a line that
  silently does nothing. `reasoningEffort` is one of `none`, `minimal`, `low`,
  `medium`, `high`, `xhigh`, `max` — the union of what the wires take, and
  **the accepted subset is the model's, not the wire's**, so the endpoint
  refuses one its model does not know: `gpt-5` takes `minimal` through `high`
  and rejects `none`, while the gpt-5.6 family takes `none`. **Absent is not
  `none`**: a gpt-5.6 model reasons by default and then refuses function tools
  for any effort but none on chat completions, so an agent with `tools:` on one
  of those needs `reasoningEffort: none` said out loud. For the same reason a
  provider row's `defaults` is the wrong home for this knob — one row serves
  several models — and none of the seeded rows sets it.
- **`tools:`**, the functions the model may invoke (below).
- **`subagents:`**, sub-agent references (self-reference is a load error).
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

  Either list may **glob**, in the spellings a trigger selector uses: `*`,
  `<authority>/*` or `<authority>/<package>/*`
  ([0080](decisions/0080-a-kind-grant-may-glob-and-a-glob-never-reaches-auth-material.md)).
  A glob covers kinds the repository does not have yet, so an agent granted
  `ada.example.com/*` keeps working as bundles are imported — which is the
  point, and also the cost: importing one widens an existing grant, and a
  misspelled glob matches nothing where a misspelled kind would have been
  refused at load. **A glob never reaches
  `substrate.reamde.dev/core/token`, `/credential`, `/secret` or
  `/recoverykey`.** Those four are granted only by an entry that spells one
  out, so `*` means everything the owner has rather than everything including
  the keys to the substrate. `permissions.call` takes no glob: it names
  functions, not kinds.
- optional **`hiddenFromChat:`**, the chat-surface withholding: `true` keeps the
  agent off the console's chat list and makes the chat API refuse it, while
  sub-agent calls, the call API and triggers still dispatch it. An
  llm-as-judge is the shape it exists for. Any agent, marked or not, remains
  selectable as another agent's sub-agent.
- optional **`resume:`**, `always` or `never`; absent means `always`. It says
  whether a resolution reported into this agent's thread (a decided proposal,
  an answered interaction) also resumes the thread. The `system` row lands
  either way ([the decision loop](#the-decision-loop)).

Tool functions, sub-agents, and every emitted and read kind resolve against
the registry at admission, where same-batch installs count.

What fires an agent is the same [trigger](functions.md#triggers) record a
function's delivery rides; only the `callable` reference names the other kind:

```yaml
kind: substrate.reamde.dev/core/trigger
metadata: {id: readinglist-classify-on-page}
data:
  properties:
    enabled: true
    source:
      record:
        kinds: [samples.substrate.reamde.dev/readinglist/page]
        ops: [update]
        when: 'record != null && record.properties.fetch == "fetched" && !("class"
          in record.properties)'
    callable: substrate.reamde.dev/core/agent/samples.substrate.reamde.dev/readinglist/pageclassifier
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
on `subagents:`, and `callable` is the [trigger](functions.md#triggers)'s word,
where a target really may be a function or an agent.

That is the only arm, because the four built-ins are
[**host functions**](functions.md#host-functions) — `runtime: host` records core
ships — so an agent names one exactly as it names a bundle's function:

```yaml
  tools:
    - function: substrate.reamde.dev/core/query
    - function: substrate.reamde.dev/core/propose
    - function: samples.substrate.reamde.dev/readinglist/setclass
```

Three older spellings are refused, each naming its replacement. A bare string
(`tools: [query]`) named the arm by its value, so a typo in a built-in's name
silently became a function nothing declares. `{builtin: query}` was the
interim arm that split the union explicitly: it made the built-ins the one thing
an agent could name that no record declared, and it is gone now that they are
records. And `{callable: …}` was this key's first name, before it was clear an
entry could name only a function.

- **`substrate.reamde.dev/core/query`** is the read, and it speaks the
  [records route](api.md#the-records-route)'s grammar as a tool: `kind` + `id`
  reads one record; `q` (with `mode`, `first` and `filter.kinds`) is the ranked read;
  otherwise `filter` (the whole [grammar](api.md#the-filter-grammar),
  `referencing` included), `orderBy`, `first`, `after` and `expand` list a
  page, which answers `{records, cursor, head, generation, included?,
  matches?}` exactly as the route does. It requires `permissions.reads`, a
  load error otherwise, and every arm is held to that allowlist: a get outside
  it answers like an absent id, a kind outside it in `filter.kinds` or `kinds`
  is refused by name, an expanded referent outside it is left out, and a list
  that names no kinds lists the allowlist — expanded against the repository's
  current kinds where the allowlist globs. List and search clamp to the
  remaining row budget; a blown budget is a tool error the model sees.
- **`substrate.reamde.dev/core/write`** is the direct write: one call is
  `{op, kind, id, input, ifVersion}`, where `op` is `put` (create or update,
  merging and never pruning; omit `id` to mint one), `patch` (change an
  existing record) or `delete` (tombstone one), `input` is
  `{properties, labels, annotations}` decoded through the same strict path a
  REST body takes, and `ifVersion` holds the write to the version the model
  read. It requires a non-empty `permissions.writes` (a load error otherwise).
  Every written kind is held to the agent's **effective** emit before the
  write applies, so a sub-agent's ceiling narrows it like any other effect,
  and the [policy door](#the-policy-door) runs for each write exactly as it
  does for a function tool's effects. There is no `merge` or `split` op:
  fusing identities is the owner's reviewed decision. Writes ride the full
  public path (kind guards, declaration admission) under the agent's actor.
- **`substrate.reamde.dev/core/propose`** is the reviewed write, and requires
  `permissions.writes` to name
  `substrate.reamde.dev/core/recordpatchrequest`. It lands one
  [`recordpatchrequest`](projection.md#the-patch-request-sibling), never a
  direct mutation. It carries a `rationale` and an `op`: `patch`, the default,
  names an existing `target` with its `kind` and a `diff` the accept applies;
  `create` names a `kind`, a stable `id` and a `diff` the accept mints
  create-if-absent; `delete` names an existing `target` with its `kind`, which
  the accept tombstones.
  The diff is validated and normalised at propose time — the `properties`
  wrapper is required or a bare property map is coerced into it, and every
  property must be writable on the target kind — so a malformed proposal (a
  wrapper-less diff, an immutable or unknown key) is a tool error the model
  sees, never a bad request reaching the owner's inbox. The loop stamps the
  proposing `thread` onto the request, which is where the decision reports
  back (below).
- **`substrate.reamde.dev/core/ask`** pauses to ask the user, and requires
  `permissions.writes` to name `substrate.reamde.dev/llm/interaction`. It
  lands one [`interaction`](builtin-kinds.md#substratereamdedevllm) carrying
  a batch of at most eight questions and returns the record id, not an answer.
  The run continues without one; the answer arrives in a later turn, when the
  user answers the interaction and the thread resumes, so the model must not
  poll for it.
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
`changes` onto the tool's `llm/message` — one `{seq, op, kind, id}` entry per
changelog row the dispatch committed, whether a `write` call, a
`propose`'s request row, or a function tool's applied effects — so any reader
of the thread (the console, a client on the changefeed) resolves WHAT changed from the changelog
instead of parsing tool payloads. A rolled-back dispatch stamps nothing, and a
sub-agent call stamps nothing on the parent: the child thread's own rows carry
the child's writes.

## The decision loop

A request an agent's `propose` landed knows its thread, so the decision
reports back instead of vanishing into the inbox. When anybody decides it —
the owner in the console, a judge agent through `write` — the deciding
transaction also writes one `system` message into the proposing thread: the
content is a JSON envelope (`event: "proposalDecision"`, the request's record
path, the verdict, the target and — on an accepted patch or create — the
target's new version, plus `adjustedDiff` where the owner adjusted the values
before accepting), and its `changes` carry the decision's own changelog
entries (the request's patch, and the accept's apply). Decision and report
land together or not at all.

After commit the thread **resumes**: a continuation with no new user turn, in
which the replayed system row is what the model reacts to — on the wire it
travels as user content, since the system slot belongs to the agent's prompt.
Resume is bounded, because a resume is a paid turn: the agent's own
`resume: never` withholds it (the system row still lands), a resolution the
thread's own agent performed never resumes that thread, and a per-thread
budget caps resumes at 20 resolutions an hour, past which the sweep or the
next continuation picks the rows up. A thread that is mid-turn refuses the
lease and picks the decision up on its next continuation, and a request no
thread proposed (a human's, a function's) reports and resumes nothing.

## Sub-agents, budgets, and the emit ceiling

Sub-agents are child invocations with their own budgets and a child thread
carrying the `parent` reference. `agentDepth` is 0 at the root and increments per
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

## The policy door

Inside the emit ceiling, a `recordpatchpolicy` record says what happens to an
agent's put, patch or delete before it lands: `allow` lands it, `refuse`
bounces it, and `gate` converts it into a `recordpatchrequest` for the owner
to decide. Policies are the owner's hand alone: a bundle-tier actor cannot
write the kind, and policy never runs for owner or machine writes. When
several match one write the most restrictive action wins, `refuse` over `gate`
over `allow`; no match means the write lands. Two matching rules with the same
action are separated by which one can land the write without the owner: a rule
with no judge to auto-accept on governs one that has, so the judge that runs is
never decided by which id sorts first.

`selector` says which writes a policy speaks for, and an empty dimension
matches everything in it. `kinds` takes the same grammar a trigger's
`source.record.kinds` does — a kind reference, every kind one package declares
(`samples.substrate.reamde.dev/tasks/*`), every kind one authority publishes
(`samples.substrate.reamde.dev/*`), or every kind (`*`) — so a rule covering a
package does not have to enumerate today's kinds and miss tomorrow's. Any other spelling (`tasks.*`), and any exact reference to a kind
the repository does not have, is refused when the policy is written: a
selector that matches no write gates nothing. `ops` are the write verbs the
agent called (`put`, `patch`, `delete`), never a trigger's change classes, and
`agents` are agent identities, both matched exactly. `action` is required:
there is no default, and a rule without one speaks for nothing.

```yaml
kind: substrate.reamde.dev/core/recordpatchpolicy
metadata:
  id: gate-tasks
data:
  properties:
    selector:
      kinds:
        - samples.substrate.reamde.dev/tasks/*
      agents:
        - crew.example.com/bots/taskbot
    action: gate
```

A policy may name a `judge`, an agent the engine runs over what the policy
gated, tool-less, replying `{verdict, confidence, rationale}`. It only ever
recommends: in `mode: enforce` an `accept` verdict at or above `autoAccept`
accepts and a `reject` at or above `autoRefuse` rejects, and everything else
leaves the request for the owner with the verdict on it; in `mode: advise`
every verdict does. The verdict picks
which threshold is read and one reply carries one verdict, so the two are
independent floors rather than a band around one score.

**The engine states the reply shape**, so a judge author never has to. Every
judge run sends the agent's own prompt and then the engine's contract
paragraph under it: exactly one JSON object, `verdict` one of `accept`,
`reject` or `escalate`, `confidence` in [0,1], `rationale` a sentence or two,
all three required, no prose and no fence. The reply is then decoded strictly
— an unknown key, a missing field, a confidence outside the range or text
around the object all fail closed into the owner's review, with the reason on
the request. The one thing normalised before that decode is a single Markdown
code fence wrapping the whole object, because a model that has been told not
to fence its JSON still does; a fence around the object is presentation, and
what is inside it is held to the same rules.

`criteria` is the owner's instructions to the judge, in prose: what to accept
and what to reject. It never has to describe the reply.

Two dials say how much the judge reads, and they are independent — a policy
may set both:

- `context: thread` adds the proposing thread's recent prose turns beside the
  envelope. More signal, and more injection surface: it travels only where the
  policy asked for it, delimited as data.
- `expandReferents: true` adds every record the diff points at, one hop, as
  `referents` keyed by record path. A judge is tool-less, so a reference in
  the diff is a pointer it cannot follow; this is how a judge asked "is this
  summary supported by its source?" gets to read the source. The records
  arrive through the ordinary read projection, so a sensitive property is
  redacted exactly as it is for any other reader, and the hop is capped like a
  page's expansion.

```yaml
kind: substrate.reamde.dev/core/recordpatchpolicy
metadata:
  id: judge-tasks
data:
  properties:
    selector:
      kinds:
        - samples.substrate.reamde.dev/tasks/*
    action: gate
    judge: crew.example.com/bots/referee
    criteria: >-
      reject a summary the cited source does not support
    expandReferents: true
    mode: enforce
    autoAccept: 0.9
    autoRefuse: 0.9
```

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

`agent`, `parent` and `thread` are references that never cascade: a thread is
the audit row of a run and has to keep naming the agent that ran it, so
deleting the agent leaves the row standing. Each declares its `inverse`, so the
graph reads the same link from the other side — an agent's `threads`,
a thread's `messages` and its `subagentThreads`.

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

An `llm/provider` row is one place completions are bought, as pure data:
`label`, `wire`, `baseURL`, a secret `apiKey`, `headers`, `defaults`
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
  - {model: claude-opus-5, inputPer1M: "5", outputPer1M: "25"}
```

The two rates are `decimal` properties, so they are written as quoted strings:
a bare YAML number rides a float and is refused
([data model](data-model.md#property-types)).

**Every one of these is declared, not a json blob.** `wire` is an enum of the
three wires, so a typo is refused at the write; `defaults` is an object of the
three request knobs there are (`temperature`, `maxTokens`, `reasoningEffort`);
`headers` and `pricing` are repeated objects whose key is a declared field
(`name`, `model`), and a later row for the same key wins. A map is declarable: a
property marked `keyed: true` is one, which is how a kind's own `properties`
block stays a map.
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
rather than mixing two models' distances. `repository reembed` is how their
replacement is bought, on the box and never over HTTP
([there is no LLM configuration](operations.md#there-is-no-llm-configuration)).

**Creation seeds three keyless provider rows** — `openai`, `anthropic` and
`gemini`. A row is where the wire, the endpoint and the key live, and a
substrate cannot invent a key, so they land without one: dispatch refuses
until the owner writes `apiKey`. An agent naming a row that is not there
refuses at dispatch and says which row it wanted.

Every shipped sample agent names `provider: openai`, so a fresh repository's
demo agents run once that row is keyed. Those that carry `tools:` or
`subagents:` name `reasoningEffort: minimal` on their own `params`, the
weakest value their `gpt-5` model accepts; the rows themselves set no
reasoning default, because the accepted set belongs to the model and one row
serves several. The LLM example (**Registry →
Samples**) is the same closure creation already imported, and it ships the
same three rows so a later import onto a repository born before the seed
still has them. Key one, or write another row yourself as the document
below. There are no `cheap`/`mid`/`strong` rows: a tier was a model id hiding
behind a name, and the model is the agent's own word now.

### Registering a provider

A provider is a record, so adding one is a write: `apply -f`, or the console at
**Data → `substrate.reamde.dev/llm` → `provider` → New**. (The Agents page
does not list providers: an agent names one by id, and that pointer reads on
the agent's own record.) All four below are ordinary data documents,
`data.properties`, never a declaration.

```yaml
# OpenRouter — the OpenAI wire at its own endpoint. So is LiteLLM, Together,
# Groq or a local Ollama: same wire, different baseURL.
kind: substrate.reamde.dev/llm/provider
metadata: {id: openrouter}
data:
  properties:
    label: OpenRouter
    wire: openai
    baseURL: https://openrouter.ai/api/v1
    apiKey: sk-or-…
    headers:
      - {name: HTTP-Referer, value: https://substrate.example}
      - {name: X-Title, value: substrate}
---
# Anthropic, natively. No baseURL: the official endpoint.
kind: substrate.reamde.dev/llm/provider
metadata: {id: anthropic}
data:
  properties:
    label: Anthropic
    wire: anthropic
    apiKey: sk-ant-…
    pricing:
      - {model: claude-opus-5, inputPer1M: "5", outputPer1M: "25"}
---
# Azure OpenAI. The deployment endpoint is the row's, and so is the key.
kind: substrate.reamde.dev/llm/provider
metadata: {id: azure}
data:
  properties:
    label: Azure OpenAI
    wire: azure
    baseURL: https://example-resource.openai.azure.com
    apiKey: …
---
# The embeddings provider: one row per repository declares embedModel.
kind: substrate.reamde.dev/llm/provider
metadata: {id: vectors}
data:
  properties:
    label: Vectors
    wire: openai
    baseURL: https://api.openai.com/v1
    apiKey: sk-…
    embedModel: text-embedding-3-small
```

**What is checked when.** A half-written row applies fine and refuses at the
first dispatch that resolves it, naming the row and what it lacks. The
embeddings rules are the exception and are checked at the write, because the
row that carries them is resolved by a background loop with nobody watching.

**Setting a key and rotating one are the same write.** `apiKey` is
secret-typed: every read surface hands back `<redacted>`, there is no way to
read a stored key back out, and writing the property again replaces what is
sealed. Because `apply` merges and never prunes, naming `apiKey` alone leaves
the row's wire, endpoint and pricing exactly as they were, and writing the
`<redacted>` sentinel back is a round trip, so `substratectl get provider
-o yaml` is both a safe read and directly `apply -f`-able. A here-document
keeps the key out of your shell history:

```sh
cat <<'EOF' | substratectl apply -f -
kind: substrate.reamde.dev/llm/provider
metadata: {id: anthropic}
data:
  properties: {apiKey: sk-ant-…}
EOF
```

### Testing a provider

Point an agent at the row and run it once. The smallest agent that proves a
provider works is a throwaway package of your own:

```yaml
kind: substrate.reamde.dev/core/package
metadata: {id: smoke.example.com/smoke}
data:
  authority: smoke.example.com
  package: smoke
  version: 1
---
kind: substrate.reamde.dev/core/agent
metadata: {id: smoke.example.com/smoke/echo}
data:
  authority: smoke.example.com
  package: smoke
  description: Smoke-test one provider.
  prompt: Reply with exactly OK.
  provider: anthropic
  model: claude-haiku-4-5
  budgets: {maxTurns: 1}
```

No `tools:` and no `permissions:`, so it can write nothing at all. Swap
`provider:` for the row under test and `model:` for an id that row serves: on a
gateway row the alias form (`anthropic/claude-haiku-4-5`), on a native
`anthropic` row the bare id.

There is no agent verb on `substratectl` (`function call` is functions only),
so a run is the call API or the console's chat:

```sh
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" -d '{"input": "ping"}' \
  http://localhost:8080/api/v1/substrate.reamde.dev/core/agent/smoke.example.com%2Fsmoke%2Fecho/call
```

The id carries two slashes, so the path segment spells each `%2F`. The answer
carries `reply` and the `thread` id, and the thread is the durable half:

```sh
substratectl get thread <thread> -o yaml
```

`status: ok` with a `turns`/`promptTokens`/`completionTokens` tally is a
working provider, and `costUSD` is non-zero exactly when the row prices the
model it just used. The two failures read differently, and the difference is
where they happen:

- **A row that cannot resolve** (no `wire`, a `baseURL` without an `apiKey`, an
  `azure` row missing either) refuses **before** a thread exists. The call
  answers `422` naming the row and what it lacks; nothing is minted.
- **A row that resolves but does not work** (a wrong key, an unreachable
  endpoint, a model id the endpoint does not serve) settles the thread it
  already opened at `status: error`, with the transport's own words in
  `reason`.

## Calling an agent

`POST /api/v1/substrate.reamde.dev/core/agent/{name}/call` with `{"input": …}` runs the
loop once. `{name}` is the agent's full id with each slash written `%2F`
(`smoke.example.com%2Fsmoke%2Fecho`); a bare `echo` is `422 validation`
naming every agent declared under the word
([0101](decisions/0101-a-kind-trait-or-callable-is-named-in-full-on-every-surface.md)),
and the chat route reads it the same way. The input becomes
the first user message, and the answer carries
`reply`, `thread`, `status`, `effects` with its `effectsByAction` breakdown,
`turns`, `toolCalls`, and the token and `costUSD` tallies. Unlike a function
call, something durable is minted — the thread is the trace.

`POST /api/v1/substrate.reamde.dev/core/agent/{name}/chat` with `{"thread"?, "message"}`
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
