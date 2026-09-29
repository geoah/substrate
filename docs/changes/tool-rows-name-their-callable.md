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
a host function. To list every call of one function across agents and
aliases, filter on `callable` instead of `name`:

```http
GET /api/v1/records?filter={"kinds":["substrate.reamde.dev/llm/message"],
                             "properties":{"callable":{"eq":"function:ada.example.com:tasks:triage"}}}
```

Rows written before this release have no `callable`. A tool name the agent
does not declare gets none either.
