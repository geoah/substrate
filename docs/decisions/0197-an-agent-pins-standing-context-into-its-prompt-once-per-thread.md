---
status: accepted
date: 2026-11-01
decision-makers: George Antoniadis
---

# 0197. An agent pins standing context into its prompt once per thread

## Context and Problem Statement

An assistant needs a few standing facts about its owner (a name, a diet, how
summaries should read) on every turn. Today an agent can only fetch them with
a `query` call, so its prompt says "first, always, call memories" and every
conversation pays a tool round trip before it can answer. Whatever replaces
that must stay inside the agent's existing grant, and must not move the system
prompt between the messages of one thread, or the prompt cache misses on
every turn.

## Considered Options

- Keep fetching with a tool call, and tell the model to do it first
- Re-evaluate the pinned reads on every run of a thread
- Evaluate them once when a thread opens, store the rendered block on the
  `llm/thread` row, and replay those bytes for the thread's life
- Store the snapshot as a `system` message row of the thread

## Decision Outcome

Chosen: evaluate once at thread open and store the rendered block on the
thread row, because it is the one option that is both free of a tool round
trip and byte-stable across a conversation. An agent declares
`context.pinned` (each entry a list read: `kind`, `filter`, `orderBy`,
`first`, `fields`) and `context.maxBytes`. The filter is a string holding the
JSON the records route's `filter` parameter takes, because a field of an
object property is a declared shape and the filter grammar is open-ended. Each read runs under the agent's own
`permissions.reads`, and a pinned kind outside that grant is refused when the
declaration loads, so pinning needs no new permission. The engine renders the
results as a `<pinned_context>` block after the agent's prompt and its kinds
list, one JSON object per record in the declared order, capped at `maxBytes`
with a marker saying how many records it left out. `orderBy` is required,
because an unordered read is not the same bytes twice.

The block is stored on the thread as `pinnedContext`, with a digest of the
`context` declaration it was taken under. A later run of the thread replays
the stored bytes. It takes a fresh snapshot when there is none, when the
declaration's digest changed, and after a compaction, which clears the stored
block because the prefix moves there anyway. A new thread always takes its
own.

Re-evaluating per run would move the prefix whenever a pinned record changed,
which is exactly the cache miss this is meant to avoid. A system message row
would place the facts in the message history, after earlier turns, where they
are neither in the cached system prefix nor findable by the compactor's rules
for what a summary keeps.

A seeded kind, `substrate.reamde.dev/llm/memory` (`text`, `active`,
`source`), is what an agent pins when the facts are the owner's. It is
`primary`, unlike the rest of the `llm` package: its rows are the owner's own
data, listed, searched and edited in the console like any record, so a fact an
agent remembers is one the owner can see and take back. It is the one kind of
a substrate-state package that is not `internal`, and the kinds test that
holds the rest lists it by name.

### Consequences

- Good, because a standing fact reaches the model with no tool call and no
  prompt instruction, and stays in the cacheable prefix.
- Good, because the grant is the one the query tool already has, checked when
  the declaration loads.
- Good, because a thread's prompt can be reconstructed from its own row.
- Bad, because a fact changed during a thread is not seen until the next
  thread or compaction. Noting the change as a system row is left for later.
- Bad, because the thread row grows by up to `maxBytes` (64 KiB at most).
- Bad, because `llm/memory` still sits in a substrate-state package, so a
  records merge refuses it like the rest of `llm`.
- Bad, because a pinned read is not charged to the run's read budget: it runs
  once per snapshot, bounded by `first` and `maxBytes` instead.

### Confirmation

`internal/vocabulary/agentcontext_test.go` holds the declaration's
refusals, and `internal/engine/agentpinned_db_test.go` holds the rendered
block in the prompt the model receives, the snapshot across two messages
with a memory changed between them, a fresh snapshot on a new thread, the
cap and the grant. `TestEveryShippedPurposeIsValidAndTheSeedIsInternal`
names `llm/memory` as the seeded kind that is the owner's data.

## More Information

[docs/agents.md](../agents.md#pinned-context) is the reference. Revisit if a
change during a thread needs to reach the model before the next one.
