---
status: accepted
date: 2026-11-01
decision-makers: George Antoniadis
---

# 0200. A background tool returns a handle, and its result resumes the thread

## Context and Problem Statement

A tool call holds the agent loop until it returns, inside the run's deadline
(600 s at most), so a long import, a slow external call or several research
sub-agents either do not fit or keep the owner waiting for the whole of them.
The decision loop of
[0003](0003-notifies-is-the-one-resolution-primitive.md) (a `system` row, then
a resume) is the only later delivery a thread has, and only proposals and
interactions use it. Making a call outlive its run widens what a tool row is
and what resumes a thread, so it binds the loop, the wire and every client.

## Considered Options

- A background job: the call returns a handle at once, runs on the engine's
  background context, and its result lands as a `system` row that the
  decision loop delivers.
- A longer deadline: raise the 600 s ceiling and keep every call synchronous.
- A job record kind: a new `llm/job` kind the loop polls and clients list.

## Decision Outcome

Chosen: a background job delivered by the decision loop, because it reuses
the one delivery path a thread already has instead of adding a second one.

A function declares `execution: background`, or an agent's `tools[]` entry or
`subagents[]` entry sets `background: true`. A host function never runs in
the background. The call writes its tool row with `job: {state: working}`
and answers `{"job": "<tool call id>", "status": "working"}`. The job runs on
the service's background context, bounded by the function's own timeout or
the sub-agent's own budgets and never by the calling run's deadline. A
background sub-agent's spend still counts against the root agent's caps and
rolls up onto the root thread, in the transaction that reports it.

Completion writes, in one transaction, the tool row's `job` (`completed`,
`failed` or `canceled`, with `finishedAt`) and a `system` row
`{event: "toolCompleted", toolCallId, ok, output}`. A run still going reads it
after its next batch of tool results; a settled thread resumes under the
resume budget and the agent's `resume: never`; the resolution sweep picks up
what a lost resume dropped. A built-in `wait` host function blocks on named
jobs for at most the run's remaining time. Stopping a thread cancels its jobs,
and a job a restart interrupted settles `failed` at the next open.

A longer deadline was rejected because a lease, a chat client and a spend
check all assume a run ends soon, and it still cannot answer before the slow
part is done. A job kind was rejected because the tool row and the thread
already hold every fact a job has, and a second record would need its own
fold, sweep and client surface.

### Consequences

- Good, because a reply can land before the slow work is done, several
  sub-agents can run at once, and a job can outlive the 600 s ceiling.
- Good, because delivery, budget, opt-out and recovery are the decision
  loop's, already tested, rather than new machinery.
- Bad, because a background effect commits after the run that asked for it
  settled, so the thread's history reads the call's result out of order.
- Bad, because a job lives in one process: a restart cannot resume it, only
  report it failed, and the agent has to call again.
- Bad, because each completion is a resume, a paid turn, until the hourly
  resume budget withholds it.

### Confirmation

`internal/engine/agentjobs_db_test.go` holds the handle, the completion row,
the mid-run delivery, the resume after a settle, `wait`, a stop and the
resume budget; `internal/vocabulary/background_test.go` holds the
declarations.

## More Information

[docs/agents.md](../agents.md#background-jobs) documents the behavior.
Revisit when a job must survive a restart: that needs a durable job record
and a runner that can pick one up.
