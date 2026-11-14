# Agents and LLMs

The owner's agents (Moss and its helpers), when to hand work to one with `chat`, shaping Moss, declaring an agent, providers, models, threads and interactions.

## What the owner has

- An agent is a record of `substrate.reamde.dev/core/agent`, id `<authority>/<package>/<name>`; list them with `query {kinds: ["substrate.reamde.dev/core/agent"]}`.
- Every repository ships **Moss**, `substrate.reamde.dev/assistant/moss`: the owner's assistant, with the host functions below, every offered bundle function, pinned memories and a `*` reads and writes grant.
- Its helpers are `hiddenFromChat: true`, so `chat` refuses them and only Moss calls them: `substrate.reamde.dev/assistant/architect` (the only one with `core/vocabulary`), `…/assistant/librarian` (bulk record work) and `…/assistant/designer` (builds applets), those two in the background, and `…/assistant/secretary` (Google's tools).
- All five run on the seeded `anthropic` provider row and refuse at dispatch until its `apiKey` is written (below).

## When to use `chat`

Prefer doing the work yourself with `query`, `write` and `apply`: a chat turn costs the owner money on a model you cannot watch. Hand to Moss when the owner asks, when the job needs its helpers (an applet, a collection, a bulk cleanup), or when its instructions and memories should apply.

- `chat {agent, message, thread?, timezone?}` answers `{thread, reply, status, tools?, error?}`.
- The 50 second bound is yours alone: `status: "running"` means the turn runs on detached; read the thread's messages (below), and continue it with the same `thread` once settled. One turn per thread.
- `timezone` is the IANA zone "today" resolves in (default: the `substrate.reamde.dev/llm/timezone` setting, then UTC). `chat {thread, stop: true}` stops a turn.
- A background helper reports into the thread later as a `toolCompleted` message: "the designer has started" is not the end.

## Threads, messages, interactions

- `substrate.reamde.dev/llm/thread`: `agent` (reference), `model`, `mode` (`chat`, `record`, `schedule`, `webhook`, `subagent`, …), `status` (`running`, `ok`, `overbudget`, `error`, `stopped`), `reason`, `lastReply`, `costUSD`, `parent`.
- `substrate.reamde.dev/llm/message`: `thread` (reference), `turn`, `role` (`user`, `assistant`, `tool`, `system`, `summary`), `content`, `toolCalls [{id, name, callable, arguments}]`; a tool row's `ok`, `job {state}` and `changes [{seq, op, kind, id}]`.
- A transcript in order: `query {kinds: ["substrate.reamde.dev/llm/message"], filter: {referencing: {ref: "substrate.reamde.dev/llm/thread/<id>"}}, orderBy: [{property: "turn"}]}`; an agent's threads likewise, with the agent's path.
- `ask` lands a `substrate.reamde.dev/llm/interaction`: `thread`, `questions [{id, prompt, options?: [{value, label?}], multi?}]`, `state` `pending` → `answered` | `dismissed`, `answers [{question, selected: [values]}]`.
- Answer with one `write` patch carrying `state: "answered"` and `answers` together; dismiss with `state: "dismissed"`. Either resumes the thread. Only answer what the owner told you.
- An agent's proposal is a `substrate.reamde.dev/core/recordpatchrequest` (`decision: proposed`), decided with `decide`; the verdict reports into its thread.
- A call of a function declaring `approval: required` lands a `substrate.reamde.dev/llm/approval`; only on the owner's word, patch its `state` to `approved` or `denied` with `ifVersion`. The outcome reports into the thread.

## Shaping Moss without editing it

- `substrate.reamde.dev/assistant` is a `builtin` package: a write to its declarations is refused.
- A standing direction is a `substrate.reamde.dev/llm/instruction`: `agent` (the agent's record path), `text` (one or two sentences, the owner's words), `active` (default true), `source` (`owner`, `agent`, `import`), read at the start of every thread. `write {op: "put", kind: "substrate.reamde.dev/llm/instruction", properties: {agent: "substrate.reamde.dev/core/agent/substrate.reamde.dev/assistant/moss", text: "Answer in British English.", source: "owner"}}`.
- A lasting fact about the owner is a `substrate.reamde.dev/llm/memory` (`text`, `active`, `source`), pinned by every agent that pins the kind; an instruction reaches one agent.
- A different prompt is a fork: read Moss's record, set `authority` to `~` and `package` and the id to the owner's, `apply`. The copy upgrades with nothing.

## Declaring the owner's own agent

`apply` documents of `{kind, metadata: {id}, data}` under `~`, in one of the owner's packages (a new one needs its `substrate.reamde.dev/core/package` document; guide topic `packages`). The row IS the declaration: no `data.properties` wrapper.

```json
{"kind": "substrate.reamde.dev/core/agent",
 "metadata": {"id": "~/garden/waterer"},
 "data": {
  "authority": "~", "package": "garden", "purpose": "supporting",
  "description": "Proposes watering records for plants due today.",
  "prompt": "Propose one watering record per plant due today; never write directly.",
  "provider": "anthropic", "model": "claude-sonnet-5-5",
  "params": {"reasoningEffort": "low", "maxTokens": 4000},
  "tools": [{"function": "substrate.reamde.dev/core/query"}, {"function": "substrate.reamde.dev/core/propose"}],
  "permissions": {"reads": {"kinds": ["~/garden/*"]}, "writes": ["substrate.reamde.dev/core/recordpatchrequest"]},
  "budgets": {"maxTurns": 6, "maxToolCalls": 12, "deadlineSeconds": 120}}}
```

- Required: `authority`, `description` (the card other agents see), `prompt`.
- What it runs on: `provider` (an `llm/provider` reference; bare `anthropic` completes) + `model`, or `runsOn`, one to eight `substrate.reamde.dev/llm/model` references best first (a new thread takes the first available).
- `params`: `temperature`, `maxTokens`, `reasoningEffort` (`none` … `max`; the model sets which it accepts, and a tool-using agent should name one), `reasoningSummary`, `engineGuidance`.
- `tools: [{function, name?, description?, background?}]`, `function` a `substrate.reamde.dev/core/function` reference. `toolsFrom: [{functions: "*" | "<authority>/*" | "<authority>/<package>/*"}]` takes what bundles offer. `subagents`: agent references, `{ref, background: true}` for a background one.
- `permissions.reads.kinds` and `permissions.writes`: kinds or globs (`*`, `<authority>/*`, `<authority>/<package>/*`). A glob never reaches `core/token`, `core/credential` or `core/secret`, nor (for writes) a declaration or `internal` kind: name them. No `reads` means no `query` tool; an agent that proposes names `substrate.reamde.dev/core/recordpatchrequest` in `writes`.
- `budgets`: `maxTurns` (≤64), `maxToolCalls` (≤256), `deadlineSeconds` (≤600), `depth` (≤3), `toolTimeoutSeconds`, `maxParallelTools`, `spendCentsPerDay` (rolling 24 h; at the cap triggers hold and chat refuses).
- `context.pinned: [{kind, filter (JSON string, no kinds), orderBy, first, fields}]`: reads rendered into the prompt when a thread opens; each kind must be in `reads`.
- `hiddenFromChat: true` keeps it off `chat`; triggers and sub-agents still reach it. `purpose`: `primary`, `supporting`, `internal`.
- Change an agent by sending its whole document again with `apply`, starting from the record `query` reads. A trigger fires it with `callable: "substrate.reamde.dev/core/agent/<id>"` (guide topic `automation`).

## Host functions an agent may name

`runtime: host`, in core: `substrate.reamde.dev/core/query` (every read), `…/core/write` (put, patch, delete), `…/core/propose` (a change request: `op` patch | create | delete, `target`, `kind`, `id`, `diff: {properties}`, `rationale`), `…/core/ask`, `…/core/wait` (hold on background jobs), `…/core/edit` (view or replace lines of a long text), `…/core/vocabulary` (`documents`, `plan`); and `substrate.reamde.dev/applets/preview` (one applet screen, with its problems).

## Providers and models

- `substrate.reamde.dev/llm/provider`: `wire` (`openai`, `anthropic`, `azure`), `baseURL` (only `anthropic` may leave it empty), `apiKey` (secret), `label`, `headers`, `defaults` (request params under the agent's `params`). Seeded keyless: `openai`, `anthropic`, `gemini`.
- `apiKey` is a secret-typed property on an ordinary record, not auth material, so you may write it when the owner hands you a key: `write {op: "patch", kind: "substrate.reamde.dev/llm/provider", id: "anthropic", properties: {apiKey: "<key>"}}`. It reads back `<redacted>` (a put carrying that leaves it alone). Never echo a key or put it anywhere else.
- `substrate.reamde.dev/llm/model`: `provider` (reference, required), `model` (the id the API takes), `label`, `pricing {inputPer1M, outputPer1M, cacheReadPer1M?, cacheWritePer1M?}` as decimal strings, `contextWindow` (without it a thread never compacts). Seeded ids look like `anthropic:claude-opus-5-5`; an unlisted model is a `write` of one referencing its row. Prices feed `costUSD` and the spend cap; unpriced runs count 0.
