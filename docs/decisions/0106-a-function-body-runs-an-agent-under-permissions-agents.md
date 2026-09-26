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
the function kind: a stored row holds `{ref: <kind>/<id>}` against that pin,
and an authored manifest writes a bare id. Widening it to any kind would
retype the property and break every declaration that writes one. Resolving one identity against both registries is ambiguous, because a
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

Three rules follow from the agent committing as it runs. The input becomes a
committed message and an LLM request, so an input carrying a secret injected
into the body is refused, as a secret in a returned effect is. A trigger
never delivers a function the writes of the agents it grants, as it never
delivers a function its own writes: a callee function's effects commit under
the caller's actor and never wake it, but the agent's rows carry the agent's
actor, and a function watching a kind its agent writes would otherwise wake
on each of them until the causal-depth cap stopped the chain, fanning out at
every hop. And a body that opened a thread is not retried on its own: a
trigger delivery that fails after it parks on that attempt, and a call under
an `Idempotency-Key` binds the key to the first thread, so a repeat is `409
conflict` naming it, as for an agent call.

### Consequences

- Good, because a classifier runs its extraction agent in the body, with no
  staging kind and no trigger.
- Good, because existing declarations are untouched: `agents` is a new
  optional key.
- Bad, because the agent's writes stay when the caller fails afterwards. The
  delivery parks rather than retrying, and a retry of the park by hand runs
  the agent again. A timeout, which would otherwise ride three attempts,
  parks on the first once the agent has run.
- Bad, because a function no longer sees the writes of the agents it grants,
  including those agents' runs on behalf of anyone else.
- Bad, because the agent's writes are not the caller's effects: a body that
  only runs an agent records `ran = 0` and answers `effects: 0`.
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
refused at load. The same file checks that a secret in the input is refused
before a thread opens, that a function triggered on a kind its agent writes
runs once, that a keyed call that failed after its agent ran answers the
repeat with `409` naming the thread, and that such a delivery parks after one
attempt.

## More Information

Revisit if the agent's writes need to join the caller's transaction, which
would mean a loop that stages its writes instead of committing them.
