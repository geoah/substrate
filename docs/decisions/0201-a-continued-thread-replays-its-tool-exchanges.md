---
status: accepted
date: 2026-11-01
decision-makers: George Antoniadis
---

# 0201. A continued thread replays its tool exchanges

## Context and Problem Statement

A continued thread replayed only its prose: user rows, assistant rows without
`toolCalls`, system rows and summaries. `docs/agents.md` said "tool exchanges
are audit, not context". So the second message of a chat re-read everything
the first answer had read, and paid for it again, and a prompt cache over the
history could never hold the results that cost the most to fetch. The rule
binds every agent, compaction
([0138](0138-a-compacted-thread-keeps-its-rows-and-replays-a-summary-message-over-the-range-it-covers.md))
and any later cache work, so its reversal is a record.

## Considered Options

- Replay the stored tool exchanges verbatim, by default
- Keep prose-only replay
- Replay a digest of each exchange (a line per call) instead of the results

## Decision Outcome

Chosen: replay the stored exchanges verbatim, by default, because it is the
only option that both spares the second fetch and keeps the prefix
byte-stable. The thread is append-only, so the same rows replay as the same
messages on every continuation, and the previous run's last request is the
next run's prefix. A digest would be new text the model never saw and would
be lossy in exactly the identifiers a follow-up asks about. Prose-only stays
available per agent as `compaction.replayTools: none`.

The replay follows both wires' rule that a call is answered right after it:

- each assistant row with `toolCalls` replays with its calls, and its tool
  rows follow it in call order, whatever rows landed between them;
- a call with no tool row (a run that died mid-dispatch) is answered with an
  error saying it was interrupted, rather than dropped, so the model learns
  the call may have taken effect, and the assistant turn stays as stored;
- a tool row whose call is not replayed is dropped;
- the stored result replays, truncated where the run truncated it;
- signed thinking is never stored, so earlier tool turns replay without it.
  Both wires take such a history: the Anthropic documentation's own recovery
  for a broken thinking chain is to strip every thinking block and keep the
  `text` and `tool_use` blocks;
- an agent left with no tools replays prose only, since the request then
  declares no tool for the replayed calls to name.

### Consequences

- Good, because a follow-up answers from what the thread already read, and a
  provider cache written by one run serves the next.
- Good, because compaction already holds every call with its result
  (obligations), and its estimate now counts calls and results, so the window
  still bounds the replay.
- Bad, because a continued thread's requests grow with every result it read,
  and compaction fires sooner on tool-heavy threads.
- Bad, because a result read in one run can be stale in the next, and the
  model is not told how old it is.
- Bad, because a tool renamed or removed since still appears by its old name
  in replayed calls.

### Confirmation

`TestReplayViewReplaysToolExchanges` and `TestReplayViewIsAFunctionOfTheRows`
hold the message sequence; `TestAFollowUpRepeatsThePreviousRequestAsItsPrefix`
holds the byte-stable prefix against the fake endpoint;
`TestLiveReplayedToolExchange` holds both wires, thinking included, in the
live suite.

## More Information

This supersedes no record: the rule it reverses lived only in
`docs/agents.md` and a comment on `loadThreadRows`. Revisit if a result's age
starts to matter more than its cost, in which case a replayed result could
carry the time it was read.
