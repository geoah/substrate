---
status: proposed
date: 2026-10-17
decision-makers: George Antoniadis
---

# 0171. A reasoning delta is its own event, and a turn keeps a bounded summary, never the chain

## Context and Problem Statement

A chat client shows nothing while a reasoning model thinks: `internal/llm`
folded only text deltas, so the stream went quiet for seconds and the
`llm/message` row kept no trace of the thinking. Both wires carry it: the
openai wire as `reasoning_content` on stream deltas and on the message
(LiteLLM, and every gateway that copied it), the anthropic wire as
`thinking` blocks with `thinking_delta` and `signature_delta` events. A
choice is needed on how the thinking reaches a client, what the row keeps,
and whether it goes back to the model.

## Considered Options

- A `reasoning` event of its own, and a bounded summary on the assistant row.
- Reasoning inside the `delta` event, marked by a flag.
- Keep the whole chain on the row and replay it on the next request.

## Decision Outcome

Chosen: a `reasoning` event of its own and a bounded summary, because a
client that knows nothing of reasoning keeps rendering `delta` events as the
reply and never shows thinking as an answer, and a flag on `delta` would make
every old client do exactly that. The assistant row keeps `reasoning`, the
first 4000 runes, and `reasoningMillis`, the time from the request to the last
streamed piece. The row is a transcript people read; the chain is thousands of
tokens a turn, and nothing replays it: `replayView` ignores the property and
compaction never reads it.

The thinking is not echoed back inside a run either. LiteLLM's openai wire
was checked live on 2026-10-17 with `anthropic/claude-haiku-5-5`: a
tool-calling turn that streamed a signed thinking block, followed by its tool
result without the assistant message's `thinking_blocks`, was answered `200`.
The same request with the block echoed was answered `200` too. Anthropic's
current models think with their display `omitted` by default, so a request
that names only `reasoning_effort` streams signed blocks with empty text, and
no `reasoning` event, through LiteLLM.

### Consequences

- Good, because a client without reasoning support is unaffected, and a
  model without reasoning changes nothing on the stream or the row.
- Good, because the row stays small and the replayed history is what it was.
- Bad, because the row keeps only the first 4000 runes; a client that needs
  every word must keep the live events.
- Bad, because with an effort alone the current Anthropic models expose no
  thinking text, so most turns show nothing until a request asks for a
  summarized display, which no knob does yet.
- Bad, because the native anthropic wire sends no `thinking` request key, so
  it streams thinking only from a gateway or model that thinks unasked.

### Confirmation

`internal/llm/reasoning_test.go` holds both wires to the event order and the
result's `Reasoning`, against fixtures recorded from LiteLLM.
`internal/engine/agentreasoning_db_test.go` holds the event order, the row's
two properties, the 4000-rune bound and that no request replays the text.

## More Information

Revisit when an agent needs the thinking text from an Anthropic model: that
needs a declared knob for the `thinking` request key (adaptive, with
`display: summarized`) on both wires, and on the native anthropic wire the
signed blocks echoed back ahead of `tool_use` within a run, which a
`ReasoningBlocks` field on `llm.Message` would carry. The plan is
`docs/plans/chat-turns.md`, PR 3.
