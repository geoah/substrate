# The LLM sample

Package `samples.substrate.reamde.dev/llm`: the decision-loop lessons, three
agents and the record they practise on. Nothing imports it for you; the
owner's assistant, Moss, ships with the binary instead
([builtin kinds](../../docs/builtin-kinds.md#substratereamdedevassistant)).
Import it to watch an agent write, judge and decide: `substratectl import
samples.substrate.reamde.dev/llm`, or All data, Add a collection.

- `substrateEditor` writes scratchpads directly through `write`, so its tool
  rows show the engine-stamped `changes`.
- `substrateArbiter` decides change requests through `write`; point a trigger
  at fresh requests and it accepts or rejects each one.
- `substrateJudge` is the tool-less verdict a `recordpatchpolicy` names under
  `judge:`.

Every agent names the provider row `openai`, which is keyless until you
write an `apiKey` onto it.

`bundle.yaml` is the closure: the package, the three agents and one kind,
`scratchpad`, a name and a note with no stakes, so the edits and verdicts
have a record to land on. `providers.yaml` is the three keyless
`substrate.reamde.dev/llm/provider` rows, `openai`, `anthropic` and `gemini`,
each with its pricing list. The engine seeds the same three rows on every
repository, so applying the file merges onto what is already there. There
are no functions, no triggers and no inputs.

What each agent does and how to key a provider:
[docs/bundles-catalog.md#llm-sample](../../docs/bundles-catalog.md#llm-sample).
