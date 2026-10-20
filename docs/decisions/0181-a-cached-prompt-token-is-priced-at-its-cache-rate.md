---
status: proposed
date: 2026-10-20
decision-makers: George Antoniadis
---

# 0181. A cached prompt token is priced at its cache rate

## Context and Problem Statement

The cost stamp priced every prompt token at the pricing row's `inputPer1M`.
On the openai wire, where `prompt_tokens` includes cached tokens, that charged
a cache read at the full input rate; on the anthropic wire, where
`input_tokens` excludes them, it charged cache reads and writes nothing. On
2026-10-20 one repository's thread `costUSD` summed to USD 154 by 13:00 UTC
while LiteLLM's accounting for the same key read USD 73. LiteLLM caches that
repository's reflect prefix of about 18.9k tokens, Anthropic bills a cache
read at a tenth of the input rate, and the spend cap
([0149](0149-a-spend-cap-holds-an-agent-trigger-a-third-state-beside-running-and-parked.md))
counts `costUSD`, so the cap was in a unit that moved with the cache hit rate.

## Considered Options

- Price cached tokens by two optional rates on the pricing row, with default
  ratios of the input rate when a row states none
- Take the gateway's per-call cost (LiteLLM's `x-litellm-response-cost`
  header)
- Keep the old formula and document the overstatement

## Decision Outcome

Chosen: the row's cache rates with default ratios. The cost header exists on
one gateway and never on the anthropic wire, and it makes cost a number the
engine cannot recompute from what it stored. The old formula keeps every cap
in a unit that moves with the cache hit rate.

`llm.Usage` carries `PromptTokens`, the whole input the model saw, and
`CacheReadTokens` and `CacheWriteTokens`, the parts of it the provider served
from and wrote to its cache. The openai wire reads
`usage.prompt_tokens_details.cached_tokens` and `cache_creation_tokens`, and
falls back to the top-level `cache_read_input_tokens` and
`cache_creation_input_tokens`; the two forms carry the same numbers and are
never summed. LiteLLM's `prompt_tokens` includes both counts (`calculate_usage`
in `litellm/llms/anthropic/chat/transformation.py`), and OpenAI's includes
`cached_tokens`, so the counts are parts of `prompt_tokens`, clamped never to
exceed it. The anthropic wire adds `cache_read_input_tokens` and
`cache_creation_input_tokens` to `input_tokens`. The old `ContextTokens` field
goes: it equalled the new `PromptTokens` on both wires.

Cost per 1M tokens is the uncached prompt times `inputPer1M`, reads times
`cacheReadPer1M`, writes times `cacheWritePer1M`, and the completion times
`outputPer1M`. An absent or unreadable cache rate defaults to 0.1 and 1.25
times `inputPer1M`, Anthropic's and OpenAI's list ratios for a cache read and
a 5-minute cache write. A model whose ratio differs (Claude Opus 5.5 reads at
0.05x, a 1-hour cache writes at 2x) states its rate on the row, and the
shipped sample does. A stated `"0"` is a free cache. The thread records
`cacheReadTokens` and `cacheWriteTokens` beside `promptTokens`, rolled up on
the root thread like the other tallies.

### Consequences

- Good, because `costUSD` and the spend cap are in the bill's unit when the
  row's rates match the provider's list.
- Good, because cutting cached tokens no longer moves the reported cost ten
  times more than it moves the bill, and the cached share is readable on each
  thread.
- Bad, because a default ratio is a guess for a model off the list.
- Bad, because a thread settled before the upgrade keeps the cost it
  recorded, so the 24-hour spend window mixes units after a roll, and a cap
  sized in the old unit is looser in real terms once those threads leave it.
- Bad, because `promptTokens` on the anthropic wire grows by the cached share,
  so rows before and after the upgrade are not comparable on that wire.
- Bad, because a gateway whose `prompt_tokens` excludes cached tokens is
  undercharged: the uncached count clamps at zero rather than going negative.
- Bad, because the stamp is still float math, an estimate and not a ledger.

### Confirmation

`TestOpenAIReadsCacheCountsFromUsage` and
`TestAnthropicMapsStopReasonAndCacheCounts` (internal/llm) hold both wires'
counts. `TestModelPriceChargesCacheReadsAndWritesAtTheirRates` holds the
formula and the defaults. `TestAThreadRecordsCacheCountsAndPricesThemAtTheRowsRates`,
`TestARootThreadRollsUpItsSubAgentsCacheCounts` and
`TestTheFirstTurnTitlesTheThreadAndChargesTheRow` (internal/engine) hold the
thread tallies and `costUSD`. `TestSeededProviderRowsMatchTheShippedSample`
holds the seeded rows equal to `samples/llm/providers.yaml`.

## More Information

Builds on [0149](0149-a-spend-cap-holds-an-agent-trigger-a-third-state-beside-running-and-parked.md)
(the spend cap) and [0138](0138-a-compacted-thread-keeps-its-rows-and-replays-a-summary-message-over-the-range-it-covers.md)
(compaction, which now measures a run's first context by `PromptTokens`).
Revisit if a wire reports cache writes per TTL worth pricing apart, or if a
gateway exposes a cost the engine should prefer over its own arithmetic.
