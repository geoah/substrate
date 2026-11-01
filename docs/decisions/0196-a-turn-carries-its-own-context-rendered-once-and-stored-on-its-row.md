---
status: accepted
date: 2026-11-01
decision-makers: George Antoniadis
---

# 0196. A turn carries its own context, rendered once and stored on its row

## Context and Problem Statement

An agent's prompt is its declaration, the kinds list and an engine suffix
(`composeSystem`): nothing tells a chat agent what time it is, in which zone
the owner lives, or which record the person is looking at. Clients worked
around it in-band, with a clock tool and an `About [...]:` message prefix
every client then had to strip or render. Whatever the engine
adds must replay byte for byte, because prompt caching keys on the exact
prefix, and the clock changes every minute.

## Considered Options

- Put the clock and the zone in the system prompt.
- Render a turn context per turn, append it after the user's text as its
  own content block, and store it on the row the turn opened with.
- Leave it to clients: a message prefix, or a tool that returns the time.

## Decision Outcome

Chosen: a turn context rendered once per turn and stored on its row,
because it is the only option where replay sends the bytes the first request
sent and every client reads one structured field instead of a convention.

- **The request.** A chat turn (`POST …/core/agent/{name}/chat`, the body is
  `substrate.ChatInput`) takes `timezone`, an IANA name, and `context`, at
  most eight record paths. A zone `time.LoadLocation` refuses, `Local`, or a
  malformed path is a 400 before the turn starts. Each record is read under
  the agent's `reads` grant, and one the grant does not reach, or that does
  not exist, refuses the turn with its own 4xx naming the path. Rendering it
  as "not readable" instead would let a turn run on a question about a record
  the agent never saw. The call API is unchanged: its input is a caller's
  JSON, not a person's message.
- **The zone.** The request's `timezone`, else the repository's
  `substrate.reamde.dev/llm/timezone` setting (a core `setting`, like
  `llm/spendCentsPerDay`; nothing seeds it, and a write naming a zone the
  server cannot load is refused), else UTC.
- **The block.** `<turn_context>` with `now:` (the minute, in the zone, with
  the weekday and the zone's name) and one `about: substrate://<path>` per
  record, followed by the record's kind, title and properties as JSON, capped
  per record. A chat turn's now is when it was admitted; a record trigger's is
  the change's commit time; a schedule or webhook fire's is the fire time. A
  resolution (a decision or an interaction answer) stamps its `system` row
  with a block of its own as it lands, so the resume that follows reads the
  time of the resolution, not the time of the turn it answers.
- **The row.** The block is stored on the turn's `llm/message` row as
  `context`, and the records as `contextRecords` (`kind`, `id`). Replay sends
  the stored block, never a fresh one. On the anthropic wire it is a second
  text block of the same user message; on the openai wire the message's
  content becomes two text parts.

### Consequences

- Good, because the agent knows today, the owner's zone and the record in
  view without a tool call or a prompt rule.
- Good, because clients show `contextRecords` as chips and never parse text.
- Good, because a replayed history is the same bytes, so a cached prefix
  holds across turns.
- Bad, because every triggered run's first message grows by a line, and an
  openai-wire user message with a context is an array, not a string.
- Bad, because a record rendered into a turn is a copy as of that turn: a
  later turn replays the old values, and the agent has to query to see edits.
- Bad, because a resume the sweep starts long after its resolution reads the
  resolution's time.

### Confirmation

`TestTurnContext*` in `internal/engine` (the block on the wire, replay
stability, the zone's precedence, the grant refusal), the api handler tests
of the chat body, and the wire golden for `ChatRequest`.

## More Information

[docs/agents.md](../agents.md#turn-context) describes the block as a reader
of a thread sees it.
