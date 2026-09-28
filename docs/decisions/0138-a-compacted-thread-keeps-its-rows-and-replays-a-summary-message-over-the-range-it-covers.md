---
status: accepted
date: 2026-09-28
decision-makers: George Antoniadis
---

# 0138. A compacted thread keeps its rows and replays a `summary` message over the range it covers

## Context and Problem Statement

A continued agent thread replays its whole prose history on every
continuation (`loadHistory`, `internal/engine/agentloop.go`), nothing bounds
it, and no code knows a model's context window, so a long chat thread costs
more per turn and eventually fails at the provider
([issue 76](https://github.com/geoah/substrate/issues/76)). Whatever shape a
summary takes is stored in every repository that compacts and read by every
later replay, so the shape is hard to change once written.

Issue 76 asked for the summary as an engine-written `system` message row.
Three queries read any `system` row as a resolution (a decided proposal or an
answered interaction): `recheckResolutions` (`agentloop.go`) starts a
continuation when one lands after the run opened, `SweepResolutions`
(`agentdecision.go`) resumes a settled thread whose newest one postdates
`finishedAt`, and `systemRowsInLastHour` counts them against the thread's
hourly resume budget. A summary written as a `system` row resumes the thread
it just compacted and spends that budget.

[0008](0008-keep-the-loop-do-not-adopt-adk-go.md) kept the loop in this
repo. The survey in [the compaction plan](../plans/compaction.md#libraries-nothing-to-import)
found no Go library to import: adk-go's compaction (added 2026-08-31) keeps
its cut logic under `internal/`, typed on `genai.Content`, and adk-go still
has no first-party Anthropic model; eino's middleware is typed on eino's
schema, and langchaingo only trims.

## Considered Options

- A `system` row with a marker property, and the three resolution queries
  taught to skip it
- A new `summary` role on `substrate.reamde.dev/llm/message`
- Provider-side compaction as the store: Anthropic's signed `compaction`
  block or OpenAI's `encrypted_content` item, kept on the thread
- Adopt adk-go's `session/compaction` and map message rows onto its events

## Decision Outcome

Chosen: a new `summary` role. The loop writes one summary row per compaction
through `putMessage` under the agent's actor, so the row lands in the
changelog and a rebuild replays it. It carries `covers: {from, through}`, the
ids of the first and last message rows it replaces, inclusive, plus
`tokensBefore` and the summarizer call's `model`, `promptTokens` and
`completionTokens`. `covers` names row ids, not `turn` ordinals, because
ordinals can collide and replay orders by `created_at, id`. The covered rows
are never deleted or patched: they stay as the audit.

Replay walks the rows in stored order. At the row a summary's `from` names,
it emits the latest such summary as one user message in `<summary>` tags and
continues after its `through`; a summary whose ids are not in the thread is
ignored. A row created after `through`, such as a resolution that landed
while the summarizer ran, replays as before.

The cut follows adk-go's open-obligation rule (`longestSelfContainedPrefix`,
`skipBlockedHead`), keyed on the record ids the transcript already carries:
an assistant tool call opens an obligation its `tool` row closes, and a
`tool` row whose `changes` names an `llm/interaction` or a
`recordpatchrequest` opens one that the `system` row naming that record path
closes. A range ends only before a user row with nothing open, so a pending
ask and its run stay raw, which is the failure adk-python issue 4740 fixed.
The summarizer is the thread's own provider and model, called with no tools
and charged to the thread and the root tally.

The marker row loses because every reader of `role: system`, today's three
and any later one, has to carry the exclusion, and one that misses it resumes
a thread on its own summary. Provider-side storage loses because Chat
Completions has no compaction at all, the stored block is opaque or signed
and bound to one vendor, so moving an agent to another provider row would
lose the history, and the repo's anthropic-sdk-go (v1.63.1) predates the
feature. adk-go loses for the reasons above and because its session store is
a second source of truth outside the changelog, which 0008 refused.

### Consequences

- Good, because the three resolution queries and the judge's
  `recentProseTurns`, which reads user and assistant rows only, need no
  change.
- Good, because the summary is an ordinary record: in the changelog, plain
  text a person can read, and independent of the provider that wrote it.
- Good, because no row is lost: deleting a summary row puts the rows it
  covered back into the replay.
- Bad, because `message` gains a role that every reader of messages has to
  know, the console first.
- Bad, because an obligation opened in the oldest run and never closed keeps
  that run raw for as long as it stays open.
- Bad, because each compaction is one more completion at the thread's model
  price, and a detail the summary drops is gone from the model's context
  (the rows still hold it).
- Bad, because the cut and obligation rules are copied from adk-go rather
  than imported, so an upstream fix reaches this repo only by hand.
- Bad, because only a model whose `pricing` entry declares `contextWindow`
  compacts; any other model still fails at the provider when its thread
  outgrows the window.

### Confirmation

`internal/engine/agentcompaction_db_test.go`:
`TestCompactionAtSettleWritesSummaryAndReplaysIt`,
`TestCompactionAtOpenUsesTheEstimate`, `TestCompactionKeepsAPendingAskRaw`,
`TestCompactionRejectsATruncatedSummary` and
`TestCompactionOnOverflowRetriesOnce`. The obligation walk, the cut and the
replay view are unit tests in `internal/engine/agentcompaction_test.go`.
`TestAgentChatRoundTrip` holds that a provider with no `contextWindow` never
compacts.

## More Information

The design, the survey of other systems and the later phases are in
[docs/plans/compaction.md](../plans/compaction.md). This keeps 0008 standing.
Reopen when provider-native compaction exists on every wire the repo speaks
(`openai`, `anthropic`, `azure`), or when adk-go gains a first-party
Anthropic model, the half of 0008's trigger that has not fired.
