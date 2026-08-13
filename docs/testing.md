# The live tests

Almost every suite here is hermetic: the agent loop is driven by an in-process
fake that speaks the OpenAI wire, and the engine's database cases run against a
throwaway Postgres container. Two things a fake cannot prove — that the wire
adapters still match what the providers actually accept, and that a whole agent
chain works end to end against them — are what the LIVE suite is for.

It buys real completions with real keys, so it runs only when those keys are in
the environment and **skips** otherwise. `mise run test` and CI never need one.

## What it covers

**The adapters** (`internal/llm/live_test.go`), once per wire — OpenAI's own
endpoint and Anthropic's:

- a one-shot completion: content back, and a usage tally the loop's cost
  accounting depends on;
- a tool-call round trip: the model asks for a tool with parseable arguments,
  the answer goes back as a tool turn, and the second completion carries the
  result and no further calls;
- streaming: the deltas, concatenated, are exactly the settled content;
- and on the Anthropic wire, two consecutive user turns — the role-alternation
  fold, which a replayed thread history hits and which fails as a 400 forever
  if it ever regresses.

**The chain** (`internal/engine/agents_live_db_test.go`), one case through the
real engine with no fakes anywhere: two `llmprovider` records (one per wire), a
deterministic function tool, an OpenAI-backed sub-agent, and an
Anthropic-backed root agent that calls the tool, delegates to the sub-agent and
settles. What it asserts is durable state — the thread rows and their token and
cost tallies, the child's `parent` edge, the tool result in the transcript, the
roll-up onto the root — never the model's prose, which is nobody's to promise.

## Running it

```bash
mise run test:llm     # both halves; without keys, everything skips and it exits 0
```

The engine half also wants a database, like every other `*_db_test.go`: Docker
for the pgvector container, or `SUBSTRATE_TEST_DATABASE_URL` pointing at one.
Without keys the skip happens first, so no container is started.

## Keys

`mise` reads a **gitignored** `.mise.local.toml` beside `.mise.toml` — or in
any parent directory of your checkout, which is the tidier place if you keep
several worktrees:

```toml
[env]
OPENAI_API_KEY = "..."
ANTHROPIC_API_KEY = "..."
```

That file is in `.gitignore` and stays there. **Key material never goes into a
committed file** — not a test fixture, not a doc, not a default. Nothing in the
suite prints a key, and nothing should start.

Each case names the variable it wanted when it skips, so a partial setup is
legible: with only `OPENAI_API_KEY` the OpenAI adapter cases run, the Anthropic
ones skip, and the chain — which needs both — skips too.

## Cost, and the models

The suite is deliberately cheap: tiny prompts, `maxTokens` capped at 256, and
the smallest tool-calling model on each wire — `gpt-4.1-mini` and
`claude-haiku-4-5`. A whole pass costs well under a cent. It is still real
money, so it is not something to loop.

Either default can be re-pointed without touching the code, which is also how a
key that lacks one of those models is made green:

```bash
SUBSTRATE_LLM_TEST_OPENAI_MODEL=gpt-4.1-nano mise run test:llm
SUBSTRATE_LLM_TEST_ANTHROPIC_MODEL=claude-3-5-haiku-latest mise run test:llm
```

Both halves honor the same two variables.

## Why it stays out of the default suites

Every live case is named `TestLive…`. One name, used twice: `test:llm` selects
the set with `-run '^TestLive'`, and `test:db` excludes it with
`-skip '^TestLive'`. On top of that every live case skips under `-short`, which
is what `test:short` runs.

So `mise run test` spends nothing even on a machine whose environment is full
of keys — the live suite runs when you ask for it by name, and never as a side
effect of running the tests.
