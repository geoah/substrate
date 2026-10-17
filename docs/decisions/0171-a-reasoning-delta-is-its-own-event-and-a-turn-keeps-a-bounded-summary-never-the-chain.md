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
- Bad, because the thinking text needs `params.reasoningSummary`: the
  current Anthropic models omit it otherwise, and even with it the model
  skips thinking on a turn it finds easy.
- Bad, because on the openai wire the knob is a key no OpenAI endpoint
  defines; it is for a row whose endpoint takes it.

### Confirmation

`internal/llm/reasoning_test.go` holds both wires to the event order and the
result's `Reasoning`, against fixtures recorded from LiteLLM.
`internal/engine/agentreasoning_db_test.go` holds the event order, the row's
two properties, the 4000-rune bound and that no request replays the text.

### How an agent asks for the text

An agent sets `params.reasoningSummary: true` (an optional bool, under the
provider row's defaults like the other knobs). Every wire sends Anthropic's
`thinking: {type: "adaptive", display: "summarized"}`: the native anthropic
wire as its request key beside `output_config.effort`, the openai and azure
wires as an extra body key, because LiteLLM forwards that key to an Anthropic
model and that is the gateway a row most often names. A bounded
`params.extraBody` passthrough map was the other option: it is more general,
but it hands the request body to every agent author, lets one spelling
diverge per row, and is a second way to say what the closed `params` set
already says, so the knob won. A model that takes only a thinking budget
refuses the adaptive value; that refusal reaches the loop as a refused effort
does. Checked live on 2026-10-17 through LiteLLM: the key reaches Anthropic
(a disabled value came back as Anthropic's 400); `openrouter/openai/gpt-5-nano`,
`openrouter/openai/gpt-4.1-nano` and `gemini/gemini-2.5-flash-lite` answered
200 with it present; `anthropic/claude-haiku-5-5` streamed reasoning with it
and the turn's row kept `reasoningMillis`, across a three-completion tool
loop with no signed block echoed; `anthropic/claude-sonnet-5-5` accepted it
and chose not to think at any effort on the short prompts tried.

## More Information

Revisit when a model refuses a tool result without its signed thinking
blocks: the native anthropic wire would then echo them back ahead of
`tool_use` within a run, which a `ReasoningBlocks` field on `llm.Message`
would carry. The native wire was not checked live for this. The plan is
`docs/plans/chat-turns.md`, PR 3.
