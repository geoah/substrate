---
status: accepted
date: 2026-11-01
decision-makers: George Antoniadis
---

# 0198. A bundle offers its functions, and an agent takes them by pattern

## Context and Problem Statement

An app that ships a bundle today also ships its own assistant, because the
owner's general-purpose agent (`samples.substrate.reamde.dev/llm/substrate`)
can only call the functions its `tools:` names, one reference at a time. The
app's read-only feed functions ("what needs me", "what's on today") are
already the right tools; nothing lets them reach the owner's assistant
without the owner editing that agent for every install. The choice binds two
declarations (`function`, `agent`) and the loop's tool block, which prompt
caching keys on, so it is recorded here.

## Considered Options

- A bare `offered: true` on the function.
- An `offer: {agents: true, name}` object on the function, and
  `toolsFrom: [{functions: <pattern>}]` on the agent.
- The bundle ships an agent and the assistant names it under `subagents:`.
- The owner keeps naming each function under `tools:`.

## Decision Outcome

Chosen: an `offer` object on the function and a `toolsFrom` list on the
agent, because the bundle states what it offers and the owner states, once,
what their agent takes. `offer` is an object so the audience is named
(`agents`, the only one today) and the tool name may be declared; a bare
boolean would have needed a second key for the name and a new meaning for a
second audience (0020 reserves keys by name). There is no offer-level
description: the function's own `description` and `arguments` are the card,
as they are under `tools:`. A sub-agent per bundle costs a second LLM loop per
question and is what this removes. Naming every function in `tools:` is what
the owner does today and is the problem.

The rules that make it safe and stable:

- An offered function declares its `effect`, and a host function is never
  offered. Writing functions may be offered: a call dispatches as a `tools:`
  entry's does, its effects held to the calling agent's `permissions.writes`
  and the policy door, so taking one grants nothing new.
- A pattern is `*`, `<authority>/*`, `<authority>/<package>/*` or one function
  reference, the shapes a kind grant takes (0080), full references only
  (0101). It is resolved when a run builds its tools, never at load.
- Only functions of enabled bundles are taken, after every named tool and
  sub-agent, in function-reference order, so one set of installed bundles
  gives one tool block byte for byte.
- Named tools win: a function already named is not offered twice, and an
  offered tool name already taken is left out and logged.
- The tool name is the offer's `name`, else `<package>_<name>`; a derived name
  over 64 characters is refused at load. Two authorities offering one name
  keep the first by reference.
- At most 32 offered tools per run; the rest are left out and logged rather
  than failing the run, since an install must not break the owner's agent.
- A judge with `toolsFrom` is refused, as one with `tools:` is.

### Consequences

- Good, because an app's read functions reach the owner's assistant with one
  key in its bundle and one line in the agent.
- Good, because disabling or uninstalling a bundle removes its tools from the
  next run with no edit to any agent.
- Bad, because what an agent can call is no longer readable off its own
  declaration: the console resolves and lists it, and the run's log names
  what was left out.
- Bad, because the 33rd offered function, and a name collision, are dropped
  silently to the model: only the log and the console say so.
- Bad, because every offered tool costs every turn's prompt, which the cap
  bounds but does not remove.

### Confirmation

`TestAgentTakesOfferedFunctionsOfEnabledBundles` (the tool block sent, its
order, named tools winning, the emit gate, a disabled bundle) and
`TestOfferedToolsCapAndCollisions` in `internal/engine`; `TestFunctionOffer*`
and `TestAgentToolsFromPatterns` in `internal/vocabulary`.

## More Information

[Offered functions](../agents.md#offered-functions). Revisit if an audience
other than agents (an app's token, a trigger) needs to read offers, or if the
cap proves too small for a real repository.
