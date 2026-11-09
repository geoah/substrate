---
status: proposed
date: 2026-11-09
decision-makers: George Antoniadis (via an agent session)
---

# 0254. An agent's call of a function that declares approval waits for the owner in chat

## Context and Problem Statement

The Google provider now sends mail and changes calendars
([0255](0255-a-providers-writes-ride-toggles-of-their-own-and-a-seeded-secretary-takes-the-providers-tools-by-pattern.md)),
and a provider write is a callable that writes no record
([0125](0125-a-provider-write-is-a-callable-that-writes-no-record.md)). The
policy door and `confirmation: always` act on the record effects a body
returns, after it ran, so neither can hold a send: by then the mail is gone.
Only the prompts stood between an agent and a send, and a model can be
talked out of its prompt by the very mail it reads. The owner wants such a
call to wait for them in the chat, and wants room for agents, triggers and
workflows to skip the wait later, but only where the owner set them up to.

## Considered Options

- The function declares `approval: required`, and the engine holds an
  agent's call of it before the body runs, as a record the owner decides.
- Prompts alone: the agent drafts, shows the owner, and asks in words.
- The policy door widened to calls: a `recordpatchpolicy` selector on
  functions with a new action that holds the call.
- The agent declares which of its tools wait, per agent.
- An outbox kind per provider: the function writes a row, and a trigger
  sends each row once it is reviewed (0125's alternative).

## Decision Outcome

Chosen: a function declares `approval: required`, and an agent's call of it
lands an `llm/approval` record instead of running. It runs once the owner
approves it, exactly as stored, and never twice.

Whether a call is irreversible is a fact about the function, which its
publisher knows, and every agent that takes it by pattern
([0198](0198-a-bundle-offers-its-functions-and-an-agent-takes-them-by-pattern.md))
inherits the hold without the owner configuring anything. Per-agent
declarations would leave the next agent unguarded. A policy action would
make the safe state something each owner has to write, and the door has
no hook before a body runs. An outbox would reshape every write function
into a kind and a trigger. Prompts remain, but as manners, not the guard.

**The record.** `substrate.reamde.dev/llm/approval` (internal, seeded with
`llm`) holds the asking agent, the function, the tool name, the call id
and the arguments, all frozen at admission. `thread` is the root
conversation, where the owner is. `requestedIn` is a sub-agent's own
thread when the call came from one. The tool answers the model at once
with the approval's id and "waiting for the owner's approval", and the
turn goes on or ends as it would.

**The decision.** Only the owner moves `pending` to `approved` or `denied`,
as a patch of `state` alone on the version they read. An agent, a function,
a bundle and the engine are refused. An app reaches the record through the
agent scopes that let it talk to the thread's agent, as it reaches the
thread. The owner cannot edit the arguments. To change a draft, they
decline it and say what they want instead.

**The run.** After the commit, the engine claims `approved` → `running` by
compare-and-set. It runs the function under the asking agent's actor and
ceiling, with the writes the approval recorded, and ends the record `done`
or `failed` with the output or the error. It then writes one
`approvalDecided` system row into the root thread and resumes the thread,
so the agent reports the outcome. A declined call writes the same row and
never runs. At open, an approval found `approved` or `running` is failed as
interrupted and reported, never re-run, because a call that may have
reached the provider must not reach it twice.

**The scope.** The hold is on agents' calls. Two other paths run the body
directly. The owner's own call (`POST …/call`, the console) is one. A
trigger the owner wired straight to the function is the other, and it is the
owner's set-up, not a model's choice. A function body that calls an
approval-required function through the host inside an agent's chain is
refused, so the hold cannot be laundered through a second function.

**Skipping the wait is not built.** When it is, it is the owner's standing
approval, declared on the agent, trigger or workflow it covers and naming
the functions it answers, as an allow outranks a gate only by naming it
([0108](0108-an-allow-outranks-a-gate-only-by-naming-it.md)). An agent's
own claim that it is certain only ever narrows what that grant covers
([the plan](../plans/approvals.md)).

### Consequences

- Good, because a send waits for the owner whatever the model was told,
  and the card shows exactly what will run.
- Good, because a restart never sends twice, and a sub-agent's call waits
  where the owner is talking, not in a thread they never open.
- Good, because Google's send, its four calendar changes, and any later
  function that declares the key are held with no per-agent configuration.
- Bad, because an unattended run (a trigger's agent thread) cannot send at
  all until standing approvals exist: its approvals wait in a thread the
  owner may not read, and nothing expires them.
- Bad, because an approved call the restart interrupted is failed even if
  it never started, and the owner asks again.
- Bad, because the owner cannot fix a typo in the card, only decline it.
- Bad, because a client older than this shows the tool row as done, with an
  opaque handle and no buttons, and a binary older than this refuses a
  function declaring `approval`, so rolling back quarantines the Google
  provider.
- Bad, because the hold reads the function's own declaration: a publisher
  that leaves the key off ships a send that does not wait.

### Confirmation

In `internal/engine/approvals_db_test.go`:
`TestApprovedCallRunsExactlyAndReports`,
`TestDeclinedCallNeverRunsAndReports`,
`TestSubagentApprovalIsAskedOnTheRootConversation`,
`TestApprovalIsTheOwnersAlone`,
`TestHostCallOfAnApprovalFunctionIsRefusedUnderAnAgent` and
`TestInterruptedApprovalSettlesFailedAndNeverRuns`. `TestFunctionApproval`
holds the key, `TestGoogleToolsAdmit` holds which Google tools declare it,
and `internal/api/appagents_test.go` holds an app's reach. The console's
`approval-card.test.tsx` and `approvals.test.ts`, and ChatKit's
`ApprovalTests.swift`, hold the cards and that a decision sends the state
alone.

## More Information

[Approvals](../agents.md#approvals), [the `approval` key](../functions.md#the-manifest),
[the plan for standing approvals](../plans/approvals.md). Reopen when a
standing approval is built, or when an owner needs to edit a held call
rather than decline it.
