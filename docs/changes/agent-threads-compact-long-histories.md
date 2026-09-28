---
type: feature
---

# A long agent thread compacts its older turns into a `summary` message

A chat thread or a resumed thread whose history nears its model's context
window now has its older turns summarized by the thread's own model. The
summary is a new `substrate.reamde.dev/llm/message` row with `role: summary`,
and later continuations replay it in place of the rows it covers. Those rows
stay in the thread unchanged, so the transcript still shows every turn.

Compaction runs only for a model whose `pricing` entry on its
`substrate.reamde.dev/llm/provider` row declares `contextWindow`, the model's
context window in tokens. A model without one never compacts. New
repositories get `contextWindow` on the seeded `openai`, `anthropic` and
`gemini` rows. The seed is create-only, so a repository created before this
release keeps its rows as they are: add `contextWindow` to each pricing entry
to turn compaction on.

```yaml
pricing:
  - {model: claude-opus-5, inputPer1M: "5", outputPer1M: "25", contextWindow: 200000}
```

An agent tunes or disables it with `data.compaction`. The defaults are shown:

```yaml
compaction:
  enabled: true           # false: this agent's threads never compact
  reserveTokens: 16384    # compact when the context passes window minus this
  keepRecentTokens: 20000 # the newest turns kept verbatim after the summary
```

The summary row carries `content`, `covers` (the ids of the first and last
message rows it replaces), `tokensBefore`, and the summarizer call's `model`,
`promptTokens` and `completionTokens`. The summarizer's tokens and cost are
added to the thread's tallies. The chat stream sends one event per
compaction:

```json
{"kind": "compacted", "thread": "k3v9qzr2xw1a", "tokensBefore": 184233, "covered": 42}
```

A reader that lists a thread's messages by `role` sees one more value. The
design is decision record 0138.
