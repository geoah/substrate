# Plan: Moss, the assistant with a name

Status: proposed 2026-11-07, a proof of concept. It is a plan, not a
contract: [agents](../agents.md) describes what is in the tree, and the
assistant's declarations are in `kinds/substrate.reamde.dev/assistant/assistant.yaml`.

The seeded assistant was called `substrate`, the same word the console uses
for the engine's own hand, so a transcript could not say which of the two had
written a record. It was also only half of what people expect from an
assistant: it answered from records and nothing else, and it could not start a
collection when asked. This plan gives it a name and a temperament, lets it
answer a question about the world the way a general assistant does, and gives
it the hands to shape the owner's substrate.

## Who Moss is

**The name.** Moss grows on a substrate. It takes what is already there,
covers it slowly and keeps it; it does not uproot anything. That is the job:
tend what the owner keeps, extend it where it is thin, and ask before
removing anything. The word is short, not a person's name, and no actor, kind
or engine path uses it. Two names were weighed and dropped: Loam (the right
soil, a word few people use) and Lichen (awkward to say).

**The temperament.** Calm, warm and plain-spoken, with a dry humour used
sparingly. On the owner's side, and it gives an opinion when asked ("I'd take
the second one"). No filler: never "Great question", never "As an AI", no
exclamation marks, no emoji unless the owner uses them first. It says what it
does not know. Asked who it is, it says it is Moss, the assistant that lives
in the owner's substrate, running on whichever model the owner set.

## What changed

### Routing: the world or the owner

Moss decides first which kind of question it has.

- **About the world** (facts, explanations, writing, translation, code,
  maths, advice): answered from the model's own knowledge, with no tool call,
  as any general assistant would. For anything that may have changed since
  the model's cutoff it says so, and calls a web tool when an installed
  bundle offers one (Firecrawl,
  [0234](../decisions/0234-firecrawl-is-a-provider-whose-tools-are-offered-to-agents.md)).
- **About the owner** (their plans, people, things, history, preferences,
  or the substrate itself): answered from records, with tools.
- **Both** ("plan dinner on Friday"): read what is theirs, then answer with
  both.

The pinned memories shape every answer, world questions included: a
vegetarian owner asking for a recipe gets a vegetarian one without a tool
call.

### Access: three agents, three grants

| Agent | Tools | Writes | Why it is separate |
| --- | --- | --- | --- |
| `moss` (primary, chat) | `query`, `write`, `propose`, `ask`, `wait`, every offered function, the two sub-agents | `*` | The owner's assistant: reads and keeps every record |
| `architect` (hidden) | `query`, `vocabulary` | `core/package`, `core/kind` | The one agent that writes declarations, and only those two kinds |
| `librarian` (hidden, background) | `query`, `write` | `*` | Bulk record work on a budget a chat turn cannot spend (128 tool calls, 24 turns, 10 minutes) |

The emit ceiling intersects each sub-agent's writes with Moss's, so neither
can write what Moss could not. The architect's grant leaves out `agent`,
`function`, `trigger` and `recordmapping`, each of which would widen what
code runs or what it reaches; that work stays the owner's.

### The engine: a `vocabulary` tool

The one engine change is a sixth host function,
`substrate.reamde.dev/core/vocabulary`
([0239](../decisions/0239-an-agent-declares-kinds-through-the-vocabulary-tool-under-its-writes.md),
[agents](../agents.md#tools)). It is the apply door as a tool: a batch of
declaration documents admitted whole or not at all, or previewed with
`plan: true`, held to the agent's writes and the policy door, and to every
guard the door holds a batch to. Its card names the owner's authority,
because nothing else the model sees does and a new package must live under
it.

Before it, an agent with `*` could `put` one declaration record at a time
through `write`: a new package and its kind were two writes with nothing
between them, and nothing previewed either.

### The models

Moss runs on `claude-opus-5-5` at effort `low`, with `reasoningSummary`
so the chat streams its thinking; the architect on the same model at
`medium`, because a declaration outlives every conversation that made it;
the librarian on `claude-sonnet-5-5` at `low`, for many small, similar
writes. Opus 5.5 always thinks (effort is its only control), so `low` is
where chat answers stay quick: the world questions answered in two to seven
seconds. Every one sets `maxTokens: 16000`, since the wire's 8192 default
is tight once a turn thinks. The rest of the sample stays on the `openai`
row.

### The prompt

Moss's prompt keeps the old assistant's reading, writing, decision and asking
paragraphs, and changes in five places:

- **Persona** and **routing**, above, open it.
- **New records take no id.** The old prompt told the model to choose a
  short lowercase id, which a kind that does not name composed ids now
  refuses
  ([0228](../decisions/0228-a-person-an-app-or-an-agent-creates-a-record-under-a-minted-id.md)).
  Moss omits the id and the substrate mints one.
- **Shaping the substrate.** A new collection, or a field added, renamed or
  changed, goes to `architect` with whether the owner asked for it. A
  sample that fits is suggested instead of a new kind.
- **Bulk work.** More than about ten records at once goes to
  `librarian` as a background job; Moss says it has started and reports
  when the `toolCompleted` message arrives.
- **Outside its reach.** Credentials, connecting an account, installing a
  provider and merging records are the owner's, and Moss says where.

### Where it lives

The assistant ships with the binary
([0241](../decisions/0241-the-assistant-ships-with-the-binary-and-the-owner-shapes-it-through-instruction-records.md)):
`substrate.reamde.dev/assistant`, the fifth seeded package, holds `moss`,
`architect` and `librarian`, the same three in every repository, upgraded
at boot. A prompt fix reaches every repository with the binary, and the
declarations are `builtin`, so nothing but the binary writes them. It was a
sample for a day; an owner's edits to a sample are replaced by the next
upgrade they accept, and its prompts will change every week while the
evals are written.

### How the owner shapes it

An owner shapes the shipped agents without editing them, with three kinds of
record, each with its own job:

| Record | What it says | How an agent reads it | Who writes it |
| --- | --- | --- | --- |
| `llm/instruction` | how one agent should work: "no bullet points", "go by Juniper", "give new collections a notes field" | rendered into that agent's system prompt as the owner's direction, `<owner_instructions>` | the owner; an agent only by proposal |
| `llm/memory` | what the owner is: "vegetarian", "works from home on Fridays" | pinned as data by the agents that pin it | the owner; Moss by proposal |
| the owner's policies | what an agent may do without asking | the policy door, on every write | the owner alone |

An instruction names one agent, so "tell the architect to always add a
notes field" is an instruction for the architect, read by no one else.
Moss changes itself the same way: asked for a lasting change, it proposes
an instruction for itself, and the owner's accept is what lands it. A
glob grant never reaches the instruction kind, so a record or a web page
that talks Moss into "updating its instructions" produces a proposal the
owner reads, never a change. No instruction widens what an agent may do;
grants and policies decide that.

#### What the other harnesses do

Researched 2026-11-07; items marked (unv.) rest on secondary sources.

Personal agent harnesses converged on one stack, most trusted and smallest
first: the vendor's prompt; an identity or voice (OpenClaw's `SOUL.md` and
`IDENTITY.md`, Letta's `persona` block, ChatGPT's personality); operating
instructions (`AGENTS.md`, `CLAUDE.md`, Cursor's rules, custom and project
instructions); a model of the user (`USER.md`, Letta's `human` block, memory
summaries); and memory that is searched rather than loaded (daily notes,
chat history, Letta's archival memory), with skills loaded on demand by
name and description. Precedence is the order the layers are concatenated
in: Claude Code and Codex read the files nearest the work last, so
"closer files override earlier guidance because they appear later"
([Codex](https://learn.chatgpt.com/docs/agent-configuration/agents-md),
[Claude Code](https://code.claude.com/docs/en/memory)).

| | Who changes it | Does the agent change itself | What holds it |
| --- | --- | --- | --- |
| OpenClaw `SOUL.md`, `AGENTS.md`, `USER.md` ([templates](https://github.com/openclaw/openclaw/tree/main/docs/reference/templates)) | owner and agent | yes: "This file is yours to evolve", and "If you change this file, tell the user" | the model's own restraint, a git backup, and since July 2026 a provenance taint that keeps a file written on a tainted turn out of context ([provenance](https://github.com/openclaw/openclaw/blob/main/docs/concepts/memory-provenance.md)) |
| Letta memory blocks ([docs](https://docs.letta.com/guides/agents/memory-blocks)) | API, agent, a background agent | yes, through exact-match `memory_replace` and whole `memory_rethink` | `read_only` blocks, size limits, a history of every block's states with its actor |
| Claude Code `CLAUDE.md` and auto memory | user; Claude writes memory unprompted | memory yes, instructions only when asked | a "Saved N memories" notice; instructions arrive as a user message, "context, not enforced configuration" |
| ChatGPT custom instructions and memory (unv.) | user; the model through a `bio` tool | memory yes | "Memory updated", and a page to manage them |
| Cursor and Windsurf rules ([Cursor](https://cursor.com/docs/context/rules)) | user | on request | rules scoped by file glob or loaded when their description fits |
| Hermes `SOUL.md` and memory ([docs](https://hermes-agent.nousresearch.com/docs/user-guide/features/memory)) | user and agent | yes | an injection scan on every write, a snapshot frozen at session start, approval for skill writes |

Every layer an agent can write has been poisoned in public:
- A web page planted a ChatGPT memory that exfiltrated later chats
  ([2024](https://embracethered.com/blog/posts/2024/chatgpt-macos-app-persistent-data-exfiltration/)).
- A document injection scheduled an OpenClaw `SOUL.md` rewrite every two
  minutes ([Zenity, 2026-02-04](https://labs.zenity.io/post/openclaw-or-opendoor-indirect-prompt-injection-makes-openclaw-vulnerable-to-backdoors-and-much-more)).
- An email-summary job carried an attacker's claims into `MEMORY.md` and
  `USER.md` ([IronCore, 2026-08-26](https://ironcorelabs.com/blog/2026/prompt-laundering/)).

The mitigations that held sit outside the prompt: approval, provenance,
versioned writes, layers the agent cannot write, and keeping what an agent
wrote out of the system prompt.

Substrate's instructions take the layer shape and keep the agent out of the
write. The shipped prompt is the vendor's layer and the binary's alone. An
instruction is the operating layer, scoped to one agent the way a Cursor
rule is scoped to files. A memory is the user model, read as data. What an
agent changes in its own direction is a proposal: the owner reads it before
it exists, and every version has an author in the changelog. A glob grant
cannot write one at all, which a model's restraint cannot promise.

Next steps the survey suggests:
- **An identity** clients show, so "go by Juniper" renames the agent in the
  console and the apps as well as in its replies; OpenClaw's first run asks
  for it.
- **Skills as records**: a procedure an agent loads when its description
  fits, rather than a standing instruction in every prompt.
- **Instructions scoped to a kind**, read when the agent touches that kind,
  as a rule scoped to files is.

### One assistant

A repository ships with one assistant and nothing else to chat with. The
LLM sample used to be imported at registration, with six demo agents beside
it: `substrate`, the assistant before Moss, `substrateEcho` and
`substrateSummarizer`, a delegation demo, and three decision-loop lessons.
Moss replaces the first, its own helpers are the delegation example now, and
nothing imports the sample any more: its three lessons and their scratchpad
stay in the catalog for anyone learning how agents decide. A repository that
imported the sample before keeps it until its owner removes it; its next
upgrade drops the three agents that left.

## Examples

What Moss should do with each, and which tools it should reach for. These are
the cases a live recording should cover.

| The owner says | Route | What should happen |
| --- | --- | --- |
| "What's the difference between a Roth and a traditional IRA?" | world | A direct answer, no tool call |
| "Write a haiku about Monday mornings" | world | A haiku, no tool call |
| "Who are you?" | world | "I'm Moss, the assistant that lives in your substrate…" |
| "I'm vegetarian, remember that" | owner | One `propose` of an `llm/memory` create, then one line |
| "Suggest something for dinner" (after the memory is accepted) | world, shaped by memory | A vegetarian suggestion, no tool call |
| "What needs me today?" | owner | Offered feeds first, then proposed requests, pending interactions, what is due |
| "Start tracking my plants: name, where it lives, how often to water" | owner, shaping | `architect` plans and applies `<authority>/garden/plant`; Moss offers to add the first ones |
| "Add these: fern, kitchen, weekly; basil, balcony, every 2 days; …" (15 lines) | owner, bulk | `librarian` in the background; Moss says it started, then reports the count |
| "Also track when I last fertilized each one" | owner, shaping | The architect adds an optional `lastFertilizedAt`; nothing else changes |
| "Drop the location field" | owner, shaping | The architect plans it, the plan refuses while plants hold a location, and Moss puts the choice to the owner (deprecate it, or clear the values first) |
| "Delete my scratchpads" | owner | One `propose` per delete, never a direct delete |
| "Change my password" | outside reach | Says it cannot, and that Settings is where the owner does it |

## Sessions

### Recorded with a scripted model

`TestMossShapesACollectionThroughItsArchitectAndFillsIt`
(`internal/engine/agentvocabulary_db_test.go`) runs the seeded Moss against a
fake model that answers a fixed script, so it proves the wiring, not the
model's judgment. The thread it records:

```text
owner     start tracking my plants; I have a fern I water weekly
moss      → architect {"input": "The owner asked to start tracking their
            plants: a name and how often each wants water. The owner asked
            for this."}
  architect  → vocabulary {"plan": true, "documents": [
               {kind: core/package, id: <authority>/garden},
               {kind: core/kind,    id: <authority>/garden/plant,
                properties: {name: string, waterEveryDays: int}}]}
             ← {"plan": {"lossy": false, …}}                  no blockers
  architect  → vocabulary {"documents": [the same two]}
             ← {"applied": [{kind: core/package, id: <authority>/garden},
                            {kind: core/kind, id: <authority>/garden/plant}]}
               changes: two changelog entries stamped on the tool row
  architect  Applied <authority>/garden/plant.
moss      ← subagentWrites {records: 2, kinds: [core/package, core/kind]}
moss      → write {"op": "put", "kind": "<authority>/garden/plant",
                   "input": {"properties": {"name": "Fern", "waterEveryDays": 7}}}
          ← {"record": {id: <minted>, …}}
moss      Your plants have a home, and the fern is in it.
```

The same file holds the architect's refusals (a document outside its writes,
a data record, a shipped package), a gated apply refused while the plan still
answers, and the load error for an agent whose writes do not name
`core/kind`.

### Live

[The recorded sessions](moss-sessions.md) are every example above, run on
2026-11-07 against a development substrate with a real Anthropic key:
fourteen conversations, nineteen threads with the sub-agents', USD 0.88 in
all, 79% of Moss's prompt tokens read from the cache. What they showed:

- **Routing holds.** The four world questions and the dinner suggestion
  made no tool call. The question past the cutoff (the latest Formula 1
  champion) answered what it knew, said the season was still running, and
  said it had no web search to check.
- **Memory works end to end.** Two `propose` calls, the owner's accepts,
  the `proposalDecision` rows, and a resume of one line. The next thread's
  dinner suggestion was vegetarian and short, from the pinned block alone.
- **Shaping works, and the guards teach.** The architect's first batch
  carried `names.plural`, a retired key; the refusal named it, the next
  plan was clean, and the apply landed `garden/plant` with `min: 1` on the
  interval and a `date` for the day. Asked to drop `location` while plants
  held one, it deprecated the property instead and Moss asked whether to
  clear the values.
- **The background job is the right shape.** Moss answered the pasted
  twelve plants in eight seconds, the librarian searched for duplicates
  and wrote twelve records, and the `toolCompleted` resume told the owner
  which plants were due, with the dates right.

Two prompt changes came out of the first runs, and the later sessions run
on them: text beside a tool call is the owner's to read, so Moss writes one
sentence there or nothing (its first memory turn had written out its
deliberation), and the architect's first vocabulary call is always a plan
(its first batch had gone straight to an apply).

Each example is one call, and the thread's `llm/message` rows hold every
tool call:

```bash
agent="$(printf '%s' "$AUTHORITY/assistant/moss" | sed 's|/|%2F|g')"
curl -s -X POST "$SUBSTRATE_SERVER/api/v1/substrate.reamde.dev/core/agent/$agent/call" \
  -H "Authorization: Bearer $SUBSTRATE_TOKEN" -H 'Content-Type: application/json' \
  -d '{"input": "Start tracking my plants: name, where it lives, how often to water"}' \
  | jq '{status, reply, thread, toolCalls, costUSD}'
```

## What is left

What Moss needs next is [the roadmap](moss-roadmap.md), which merges two
reviews with the sessions; [the capabilities plan](moss-capabilities.md)
keeps the survey of the general assistants.

- **An eval.** The sessions are fourteen examples read by eye. A graded
  set run on every prompt change would hold what they show.
- **The console's default.** The console lists primary agents first, so Moss
  leads; whether a new chat opens on Moss without a choice is the console's
  call and is not changed here.
- **Narrowing `*` further.** A glob no longer reaches a declaration, a
  trigger or an instruction; any other kind an agent should not write by
  glob joins that list the same way, as a rule rather than a request in a
  prompt.
