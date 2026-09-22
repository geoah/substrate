# The LLM sample

Package `samples.substrate.reamde.dev/llm`: the agents the substrate seeds
into every new repository, imported onto the repository's own authority at
registration. It is the practice surface for the decision loop: `substrate`
answers questions about your records and proposes changes, `substrateEditor`
writes them, `substrateArbiter` and `substrateJudge` decide on proposals,
`substrateSummarizer` summarizes a thread (hidden from chat), and
`substrateEcho` returns its input. Every agent names the provider row
`openai`, which is keyless until you write an `apiKey` onto it.

`bundle.yaml` is the closure: the package, the six agents and one kind,
`scratchpad`, a name and a note with no stakes, so the proposals, edits and
verdicts have a record to land on. `providers.yaml` is the three keyless
`substrate.reamde.dev/llm/provider` rows, `openai`, `anthropic` and `gemini`,
each with its pricing list. The engine seeds the same three rows on every
repository, so applying the file merges onto what is already there. There
are no functions, no triggers and no inputs.

What each agent does, how to key a provider and how to chat with them:
[docs/bundles-catalog.md#llm-sample](../../docs/bundles-catalog.md#llm-sample).
