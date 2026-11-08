---
status: proposed
date: 2026-11-08
decision-makers: George Antoniadis
---

# 0242. A model a provider offers is a record, and an agent may run on a list of them

## Context and Problem Statement

An agent named a provider row and a model id as free text, and the models a
row offered, with their prices and context windows, were a list on the row,
`pricing`. The three seeded rows are written create-only
(`internal/engine/seedproviders.go`), so each repository kept the list of the
binary that first wrote it: a model added later ran unpriced, which a spend
cap never counts, and without a window, so its threads never compacted. A
model named once was also a model required everywhere: on a row the operator
pays for (0235), an agent naming a model off the operator's list refused, and
a seeded agent cannot be edited. There was nothing to pick a model from.

## Considered Options

- Keep the list on the row and refresh it from the seed.
- Make the model an enum on the agent kind.
- A model kind, one record per row and model, kept current by the record
  seed, with agents naming records.

## Decision Outcome

Chosen: a model kind, because the record seed already keeps shipped records
current while leaving the owner's edits and deletions alone (0191, 0229),
which a list inside one row cannot do entry by entry, and because a model
carries facts an enum value cannot: what it costs, its window, its row. An
enum would also tie every new model to a release and make retiring one a
narrowing.

- **`substrate.reamde.dev/llm/model`** is a seeded, internal kind:
  `provider` (a reference to the row), `model` (the id the row's API
  takes), `label`, `pricing` (USD per 1M tokens, `decimal`; both base rates
  required when present) and `contextWindow`. `money` does not fit: it holds
  whole minor units, and a token rate is often a fraction of a cent.
- **One record per row and model**, refused at every write as a second
  `embedModel` claim is. The id is free; the pair is compared as stored and
  never read out of an id, since row ids and model ids may both hold a colon.
  The shipped records, `kinds/substrate.reamde.dev/llm/models.yaml`, use
  `<row>:<model>`, carry the prices the seeded rows carried, and are skipped
  where another record holds their pair. The seeded rows no longer carry
  `pricing`.
- **A row's own `pricing` entry still wins** for the model it names,
  priced exactly as before, so no price stored today changes; the property
  is deprecated. A record prices a model no entry names, and only with both
  base rates. The window resolves on its own, the entry's first. A row the
  operator serves reads neither: the operator's file prices its models.
- **An agent may name `runsOn`**: one to eight model records, best first,
  instead of `provider` + `model` or `providerInput` + `models`. A new thread
  runs on the first entry whose record and row are live and whose row has a
  key, or is served by the operator with the model on its list. Only those
  four refusals move to the next entry; a vendor's answer, a refused key,
  an allowance and a storage error stop the run. A thread that already ran
  stays on the row and model it is stamped with, because its history holds
  that model's reasoning, which the anthropic wire sends back unchanged.
- **Every reader asks the same question.** Dispatch, the allowance hold and
  the operator diagnostics choose through one resolver; app approval offers
  a row by the same prices, through `Dataset.ModelOffers`; the console and
  the Chat app mirror the selection and the prices.

### Consequences

- Good, because a model the binary adds reaches every repository with a
  price and a window, and a seeded agent on a list runs where its first
  model is not served.
- Good, because the console can offer models to pick from.
- Bad, because a model an older row left unpriced starts counting against
  spend caps and allowance holds at the first open, and a published agent's
  preferred model can change when its row gains priced models.
- Bad, because a stale seeded entry on an older row keeps winning until the
  owner removes it; the Providers page marks it.
- Bad, because a binary older than this one parks an owner's package whose
  agent names `runsOn`. The seeded assistant moves to it in a later release,
  once a binary carrying it is everywhere, since a seeded package that does
  not load refuses the open.

### Confirmation

`internal/engine/llmmodels_db_test.go` holds the seeded models and prices,
the precedence, the one-record rule, the seed's skip, the selection on a
new thread, on the operator's transport and on a continued thread, and an
agent moving to `runsOn` in one patch. `TestAgentRefusals` and
`TestAgentRunsOn` hold the loader's rule, and `mise run kinds:check` the
versions of the kinds and the shipped models. The clients' copies of the
precedence and the selection are held by `web/console/src/lib/llm-models.test.ts`
and ChatKit's `RunsOnTests`, case for case with the engine's.

## More Information

Moving the seeded assistant to `runsOn` is the follow-up. Classifier
models, validating `reasoningEffort` against a model and reading a vendor's
model list are not part of this.
