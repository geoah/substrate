---
status: accepted
date: 2026-09-26
decision-makers: George Antoniadis (via the issue-635 agent session)
---

# 0106. A function body runs an agent under `permissions.agents`

## Context and Problem Statement

A function body could call other functions through its host call, and nothing
else ([issue #635](https://github.com/geoah/substrate/issues/635)). A body
that wants an agent's judgement on what it found had to write a row and hang
a record trigger on it, so a manifest carried an extra kind and trigger for
one call. The grant needs a place to live, and the agent's writes cannot join
the caller's delivery transaction the way a callee function's effects do: the
loop commits its thread and each tool's effects as it runs
([0064](0064-trigger-bookkeeping-is-a-delivery-ledger-folded-from-the-changelog.md)).

## Considered Options

- Widen `permissions.call` to name either callable, as a trigger's
  `callable` does, with the full `<kind>/<id>` path
- Accept an agent identity under `permissions.call` and resolve it against
  both registries
- A second list, `permissions.agents`, pinned to the agent kind, beside
  `call`

## Decision Outcome

Chosen: a second list, `permissions.agents`. `call` is a reference pinned to
the function kind, and every stored declaration writes a bare id against that
pin; widening it to any kind would retype the property and break every one of
them. Resolving one identity against both registries is ambiguous, because a
function and an agent of one name in one package are two callables. Two lists
repeat the split an agent already makes between `tools:` and `subagents:`.

The body reaches both through the one host call (`host.functions.call` and
`host.agents.call` send the same frame). The runner gates the union and
charges the call budget; the engine reads which list names the target, and an
identity may sit on only one. The gates are a function call's: the callee's
bundle lifecycle, the call stack and the causal-depth cap. The call stack
carries into the agent (an agent sits on it under its record path, so it
never matches a function of the same name), so the agent's function tools and
sub-agents cannot call a function already running. The agent runs under its
own actor and `permissions.writes`, as a callee function runs under its own
envelope, with no ceiling from the caller. Its writes commit as the loop runs,
and every row names the delivery's change as its cause, so a loop through the
changelog still meets the causal-depth cap. The body gets
`{reply, thread, status}`, the result a sub-agent call hands its parent.

### Consequences

- Good, because a classifier runs its extraction agent in the body, with no
  staging kind and no trigger.
- Good, because existing declarations are untouched: `agents` is a new
  optional key.
- Bad, because the agent's writes stay when the caller fails afterwards, and
  a retried delivery runs the agent again. A caller that must not repeat the
  agent reads the thread it already opened.
- Bad, because the agent runs inside the caller's `timeout`, at most 60s,
  where an agent alone may run 600s. The loop takes the caller's deadline as
  its own and settles its thread when it passes (its writes run
  on an uncancelled context, so the settle lands), and the caller then fails
  its timeout. Nothing checks the two deadlines against each other at load.
- Bad, because a binary before this one refuses a stored function that
  declares `agents`, as with any new dialect key
  ([0020](0020-dialect-keys-are-reserved-not-tolerated.md)).

### Confirmation

`internal/engine/functions_callagent_db_test.go`: a body reads an agent's
reply, an ungranted call is refused, a tool that calls back its runner is
refused as recursion, an agent run from a record delivery stamps the change
as the cause of every row, and an unknown, bare or doubly granted agent is
refused at load.

## More Information

Revisit if the agent's writes need to join the caller's transaction, which
would mean a loop that stages its writes instead of committing them.
