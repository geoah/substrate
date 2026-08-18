# notes: the agent example

The smallest bundle that shows the whole agent loop: an agent that calls
**functions as tools**, hands the hard part to a **sub-agent**, and ends with a
record written through the capability envelope.

It needs no network, no OAuth consent, no credentials and no other bundle, so
it installs on a fresh substrate and runs from one command.

```
notekeeper (agent)
├── titler (sub-agent)          writes the title. No tools, no writes.
├── stats (function tool)       counts words and characters. No network.
└── savenote (function tool)    puts the note. Emits notes/note.
```

## Install

```sh
substratectl apply -f kinds/notes.bundles.substrate.reamde.dev/bundle.yaml
```

## Run

The functions stand on their own, and need no model at all:

```sh
substratectl function call stats --input '{"text": "hello world"}'
# {"output": {"words": 2, "characters": 11}, "effects": 0}
```

The agent needs a provider. `notekeeper` names `default`, so a repository
running it wants an `llmprovider` row at that id — nothing seeds one, and the
LLM example bundle ships `anthropic` and `openai` instead, so this row is one
you write:

```sh
curl -s -X POST "$SUBSTRATE_SERVER/api/v1/core.substrate.reamde.dev/agent/notekeeper/call" \
  -H "Authorization: Bearer $SUBSTRATE_TOKEN" -H 'Content-Type: application/json' \
  -d '{"input": {"text": "id: my-note\n\nSomething worth keeping."}}'
```

It replies with the id and the title, having written one `note`:

```sh
substratectl get notes my-note -o yaml
```

## Pointing it at Anthropic directly

Re-pointing the `default` row at the Anthropic wire is one patch, and it is all
that is needed. The `anthropic` wire is the only one with an endpoint of its
own, so this row names no `baseURL`:

```sh
substratectl apply -f - <<'YAML'
kind: core.substrate.reamde.dev/llmprovider
metadata: {id: default}
data:
  properties: {name: anthropic, wire: anthropic}
YAML
substratectl patch llmproviders default --prop apiKey="$ANTHROPIC_API_KEY"
```

Two things then differ, both of them properties of the row rather than of this
bundle:

- **Model names are bare on an `anthropic` row.** The manifests here say
  `anthropic/claude-sonnet-5`, which is the alias a gateway understands;
  against the Anthropic API directly they are `claude-sonnet-5` and
  `claude-haiku-4-5-20251001`. Re-apply the two agent documents with the bare
  names, or leave the gateway in place.
- **Cost reads zero until the pricing map matches.** `pricing` is keyed by the
  model string AS SENT, and the seed keys it by the gateway aliases. A model
  the map does not name still runs, just uncosted.

## What to look at afterwards

```sh
substratectl get llmthreads
```

There are **two** threads, not one: the root `notekeeper` run and the `titler`
sub-agent's own, each with its own turn and token tallies. Cost rolls up onto
the root; a child carries only its own.
