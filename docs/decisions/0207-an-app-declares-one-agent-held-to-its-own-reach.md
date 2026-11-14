---
status: superseded
superseded-by: 0214
date: 2026-11-13
decision-makers: George Antoniadis
---

# 0207. An app declares one approved agent, held to a live approval at every run

## Context and Problem Statement

The owner asked for a workout coach agent that reads their health data,
observations, clinical conditions and medications, writes their routines,
asks them questions and remembers the answers, and that Substrate Workouts
installs and keeps current, for the owner to use from Substrate Chat
([the plan](../plans/workouts-coach.md)). An app
cannot declare an agent today: `declare:` refuses agent documents
([0203](0203-an-app-keeps-its-own-package-under-a-declare-scope.md)) because
an agent carries a grant, and an app that writes one could widen its own
reach. Reviews of the first draft found that holding the agent to the app's
token scopes once, at apply, drifted after expiry or revocation, measured
pattern coverage rather than effective permission, put every health read on
the phone's token, and promised a spend cap an unpriced model voids.

## Considered Options

- The approval installs the agent from a manifest in its details.
- Ship the agent as a sample or a catalog bundle.
- Give the app a write scope on the agent kind.
- Hold an app-declared agent to the app's token scopes, checked at apply.
- An `agent` detail approves one agent, its exact reach and its cap; the
  app declares it; the engine runs it only while a live approval covers it.

## Decision Outcome

Chosen: the approved agent held to a live approval, because it keeps the
instructions the app's to update while what the agent can touch, spend and
outlive stays what the owner approved, checked where the agent runs.

- A manifest in the details is several times their 2 KB cap, and every
  instruction update would need a browser round.
- A sample or catalog copy is the owner's or the server's, not the app's.
- A write scope on `core/agent` reaches every agent.
- A check at apply alone drifts, and widens the app's own token.

The rules:

- **The `agent` detail** names `~/<package>/<name>`, its exact `reads` and
  `writes` and its `spendCentsPerDay` (1 to 200), and the request carries
  `declare:~/<package>`. No kind of `core` or `llm`, no auth kind and no glob,
  read or written: the effective-permission rule. Approving stores the
  approved agent on the token record (`agents`); it adds no scope and writes
  nothing else. Without the package's declare scope, unchecked or refused
  under 0205, the rest is approved and the agent left out.
- **The scoped apply** admits that agent alone: tools `query`, `write` and
  `ask`; no `toolsFrom`, sub-agents, background calls or hidden-from-chat;
  reach within the approval, plus `llm/interaction` beside `ask`; a cap at
  most the approved one; a provider row that prices its model; the engine
  guidance kept.
- **At every run and before every model call**, an agent of an app-owned
  package ([0205](0205-an-app-owns-the-package-it-declares.md)) runs only
  while that app, by the client id 0205 owns the package by, holds an
  unexpired,
  undeleted token whose approval covers its reach and cap, its model is
  priced, and it is a root run. Otherwise it refuses with words the owner
  can act on, and a running turn stops.
- **Every live approval** of the app that names the agent must cover it,
  so the lowest cap and the narrowest reach hold; an unreadable approval
  fails alone.
- **A person's entry only:** a chat, its resume, or the owner's call;
  never a trigger, a function's call or a policy's judge.
- **No channel to more than a caller reads:** another token reaches the
  agent, its row and its chats only when its own reads cover every kind
  the agent reads.
- **The off switch** is the owner's bundle disable, which the app cannot
  undo and its re-apply keeps.

### Consequences

- Good, because revoking the app, letting its access expire or narrowing it
  stops the coach, and signing in again restores it, with no state to flip.
- Good, because the phone's token keeps its own narrow scopes; the health
  reads are the agent's, approved by name.
- Good, because nothing the coach spends counts as zero, and its cap is
  checked before every model call.
- Bad, because the engine reads token records on every run and turn of an
  app's agent, and the agent loop knows about apps.
- Bad, because a run overshoots its cap by at most one model call and its
  tool calls; this is a bounded threshold, not a hard ceiling.
- Bad, because the owner approves a named agent, its reach and its cap, not
  the prompt, which they read afterwards on the agent's page.
- Bad, because exact kinds mean a new kind needs an app release and a new
  approval.
- Bad, because an app's agent cannot be another agent's sub-agent, nor run
  from a trigger, a function or a judge, and an app whose reads do not cover
  it cannot talk to it however its `agents:` scope reads.
- Bad, because `declare:` admits one document type beyond vocabulary, and an
  owner edits the coach's model or cap only through the app or a takeover.
  The app installs it on default provider, model and a $1 daily cap; its
  screen for changing them, reviewing memories and asking the coach from a
  routine comes later (owner, 2026-11-02), and until then the owner sees and
  deletes memories as records in the console.

### Confirmation

See [the plan's tests](../plans/workouts-coach.md#10-tests): strict parsing
of the detail in `internal/authorize`; the scoped apply's ceilings and the
scoped reads in `internal/api`; the live-token ceiling, the sub-agent and
pricing refusals, the in-run stop and the owner's bundle disable in
`internal/engine`; and an end-to-end run in `internal/testenv` from approval
to a coach that remembers, then stops after revocation.

## More Information

Revisit when a second app ships an agent, when an app's agent needs a
trigger (proactive check-ins), when the owner wants to approve part of an
agent's reach, or when a hard spend ceiling with reservations is built.
