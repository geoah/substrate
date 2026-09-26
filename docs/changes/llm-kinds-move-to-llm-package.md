---
type: breaking
release: v0.76.0
---

# Move the four `core/llm*` kinds to the seeded `substrate.reamde.dev/llm` package

The agent runtime's kinds changed reference in v0.76.0 (decision record
0077):

| Before | After |
| --- | --- |
| `substrate.reamde.dev/core/llmprovider` | `substrate.reamde.dev/llm/provider` |
| `substrate.reamde.dev/core/llmthread` | `substrate.reamde.dev/llm/thread` |
| `substrate.reamde.dev/core/llmmessage` | `substrate.reamde.dev/llm/message` |
| `substrate.reamde.dev/core/llminteraction` | `substrate.reamde.dev/llm/interaction` |

The first boot of v0.76.0 moves every live row to the new kind with the same
id and properties, and repoints every reference at it (decision record 0078).
The old kinds stay declared and empty. This hits clients, scripts and YAML
files that name the old references, for example a provider key written as:

```http
before: PATCH /api/v1/substrate.reamde.dev/core/llmprovider/openai
after:  PATCH /api/v1/substrate.reamde.dev/llm/provider/openai
        {"properties": {"apiKey": "sk-..."}}
```

A write to an old kind is not refused: it lands in a kind the agent runtime
no longer reads.

## What to do

1. Nothing for stored data: the boot upgrade moves the rows and references.
2. Replace the four old references in client code, saved filters and
   `apply -f` documents, including `provider:` references in agent
   manifests kept outside the server.
3. Re-bookmark console pages for the old kinds.
