---
type: feature
---

# `llm/message` tool rows carry `callable`, the identity behind the alias

A `substrate.reamde.dev/llm/message` row's `name` is the tool name the model
saw, which is the agent's alias for a function or a sub-agent. The engine now
also writes `callable` on each `toolCalls` entry of an assistant row and on
the tool row that answers it. The value uses the actor spelling of decision
record 0025: `function:<authority>:<package>:<name>` for a function tool,
`agent:<authority>:<package>:<name>` for a sub-agent, and
`function:substrate.reamde.dev:core:query` (or `write`, `propose`, `ask`) for
a host function. To list every answered call of one function across agents
and aliases, filter the tool rows on `callable` instead of `name`:

```http
GET /api/v1/records?filter={"kinds":["substrate.reamde.dev/llm/message"],
                             "properties":{"callable":{"eq":"function:ada.example.com:tasks:triage"}}}
```

The filter matches tool rows, one per dispatched call. A call whose tool row
was never written (the run stopped mid-dispatch) is only in its assistant
row's `toolCalls`, which this filter does not match. Rows written
before this release have no `callable`, and a tool name the agent does not
declare gets none. The chat stream's `toolStarted` and `toolFinished` events
carry the same `callable`.
