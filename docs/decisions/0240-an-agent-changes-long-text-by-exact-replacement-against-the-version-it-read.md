---
status: proposed
date: 2026-11-07
decision-makers: George Antoniadis
---

# 0240. An agent changes long text by exact replacement against the version it read

## Context and Problem Statement

An agent that changes one line of a function's source, a bundle module, an
agent's prompt or a long markdown body has to read the whole value and
write the whole value back. That spends tokens on both sides, invites a
model to rewrite what it did not mean to touch, and turns a long record
into a race with every other writer. Agents that build apps for the owner
([#532](https://github.com/geoah/substrate/pull/532),
[#533](https://github.com/geoah/substrate/pull/533)) make this the common
case, not the rare one.

## Considered Options

- Keep whole-value writes through `write`.
- A host function that views a text property by line and applies exact
  `{old, new}` replacements against a version, as Anthropic's text editor
  tool does.
- Unified diffs (`apply_patch`).
- A workspace: a draft projection of many records as files, edited, tested
  and applied together.

## Decision Outcome

Chosen: exact replacement, `substrate.reamde.dev/core/edit`, because it is
the smallest contract a model follows reliably, and it composes with every
rule a write already meets.

- `op: view` answers numbered lines of one `string`, `text` or `markdown`
  property (or one entry of a keyed one, `key`), at most 400 at once, and
  the record's version. It is held to the agent's reads.
- `op: replace` takes `edits`, a list of `{old, new}`, and a required
  `ifVersion`. Each `old` must match exactly one place in the text as it
  stands after the edits before it; one that matches none or several
  refuses, and nothing applies. The result is a `patch` of that property
  through the door `write` takes, under `ifVersion`: the effective emit,
  the named-grant rule for declarations
  ([0239](0239-an-agent-declares-kinds-through-the-vocabulary-tool-under-its-writes.md)),
  the policy door and admission all hold it, and its changelog entries
  ride the tool row.
- A secret, a digest, a repeated value or a structured one is never a
  target.

Unified diffs were set aside for now: their context matching is either
exact, which is this with more syntax, or fuzzy, which applies changes
nobody read. They belong inside a workspace, where several files fail
together. The workspace is the next step and needs its own record: what is
truth while a draft is open, how it is tested, and how it publishes
declarations and data in one transaction.

### Consequences

- Good, because a one-line fix to a long function is a one-line call, and
  a model's mistake refuses instead of landing.
- Good, because nothing new decides who may write what.
- Bad, because a change across several records is several calls with no
  shared transaction; a half-done multi-record change is possible until
  the workspace lands.
- Bad, because `view` reads a text in full on the server to answer a slice,
  bounded only by the property's size.

### Confirmation

`internal/engine/agentedit_db_test.go`: a view of a line range, a two-edit
replace with its change stamped, and the refusals (a stale version, an old
matching twice, one matching nothing, a declaration under a glob grant),
each leaving the text as it was.

## More Information

[docs/plans/moss-roadmap.md](../plans/moss-roadmap.md) places the workspace
and the app-building tools after this.
