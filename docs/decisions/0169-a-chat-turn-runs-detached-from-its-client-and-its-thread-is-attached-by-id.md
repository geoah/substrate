---
status: proposed
date: 2026-10-17
decision-makers: George Antoniadis
---

# 0169. A chat turn runs detached from its client, and its thread is attached by id

## Context and Problem Statement

A chat turn ran on its request's context. When the client's connection
closed (a phone backgrounding the page, a tab reloading) the model call
aborted, the loop's settle then ran on the canceled context and failed, and
the thread stayed `running` under its lease. Until the lease expired, the next
message on the thread was refused `409 conflict`; after it, the sweep settled
the thread `error`. There was no way to reach a turn that was still running,
and no way to stop one. The plan is
[docs/plans/chat-turns.md](../plans/chat-turns.md).

## Considered Options

- Keep the turn bound to its request and shorten the lease.
- Run the turn in a Postgres-backed queue, so any process can serve it and
  stream it.
- Run the turn detached in the process serving the repository, with an
  in-memory hub every client attaches to by thread id.

## Decision Outcome

Chosen: run the turn detached in the serving process with an in-memory hub,
because a repository already has exactly one writer
([0083](0083-a-repository-has-one-writer-and-a-second-is-refused-at-open.md)),
so that process is the only one that could run the turn anyway, and a queue
would be a second runtime for one surface. A shorter lease fixes nothing: the
turn still dies with its client.

`POST …/core/agent/{name}/chat` keeps its wire. Every refusal still answers
with its own status before the stream opens; after that the loop runs as a
background task of the service, on a context that keeps the request's values
and drops its cancellation. The request only attaches to the turn. Two verbs
on the thread record reach the turn again: `GET …/llm/thread/{id}/events`
(replay then live, or one `idle` line when no chat turn runs) and
`POST …/llm/thread/{id}/stop`. A stopped turn, and one a server shutdown
ends, settles with the new status `stopped`, keeps the text it streamed as its
assistant row, and settles on a context of its own with a 30 second budget,
never on the canceled one. A triggered run or a call is not stoppable and
keeps its settle on its caller's context
([0151](0151-an-agent-delivery-a-restart-interrupted-is-rerun-once-at-the-first-pass.md)
owns what a shutdown leaves of those).

### Consequences

- Good, because a client leaving no longer costs the turn, and the next
  message is admitted as soon as the turn settles.
- Good, because a client can stop a turn on purpose, and the thread reads
  `stopped`, not `error`.
- Bad, because the turns and their replay live in process memory: a second
  replica behind one load balancer could not attach to or stop a turn the
  other runs. That is a change to 0083 first.
- Bad, because a crash still leaves the thread `running` for the next open
  to settle as interrupted, and a settle that misses the shutdown's 10 second
  drain does the same.
- Bad, because each running turn holds up to 4 MiB of replay, and the
  lifecycle fence's read lock for the whole turn, so a bundle disable waits
  for a turn no client is watching.

### Confirmation

`internal/engine/agentdetached_db_test.go`:
`TestAChatTurnOutlivesTheClientThatStartedIt`,
`TestAttachReplaysTheRunningTurnThenStreamsLive`,
`TestStopSettlesAChatTurnWithItsPartialOutput`,
`TestStopRefusesAThreadWithNoRunningTurn`,
`TestAShutdownSettlesADetachedTurnStopped`. The replay bound is
`TestTheReplayBufferIsBounded` in `internal/engine/agentturnhub_test.go`, and
the wire is `internal/api/agents_test.go`.

## More Information

Revisit when a repository may be served by more than one process.
