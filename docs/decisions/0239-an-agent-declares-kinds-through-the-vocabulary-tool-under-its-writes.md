---
status: proposed
date: 2026-11-07
decision-makers: George Antoniadis
---

# 0239. An agent declares kinds through the vocabulary tool, under its writes

## Context and Problem Statement

The owner's assistant should start a collection when asked ("start tracking
my plants") the way it writes a record. An agent could already `put` one
declaration record through `write`, which routes a declaration kind through
admission, but one document at a time: a new package and its first kind are
two writes, a failed second leaves the first behind, and nothing previews a
change before it lands. The apply door (`/vocabulary/apply`, `/vocabulary/plan`)
does all of that, and the MCP door already offers it to an outside assistant
([0230](0230-an-assistant-reaches-a-substrate-through-mcp-with-a-token-that-works-there-alone.md)).
An agent the substrate runs had no way to it.

## Considered Options

- Keep `write` as the only way: the agent writes the package, then each kind.
- A sixth host function, `vocabulary`, that plans or applies one batch under
  the calling agent's grants.
- Let an agent call the REST apply route with a token of its own.

## Decision Outcome

Chosen: the `vocabulary` host function, because it is the apply door's own
batch, held to the same two things every agent write is held to, and it
adds no hand an agent did not have.

- One call is `{documents, plan}`. `plan: true` answers the door's preview
  and writes nothing. An apply admits every document or none.
- Every document's kind is held to the agent's EFFECTIVE emit. Writes naming
  `core/package` and `core/kind` declare kinds and packages and no agent,
  function, trigger or mapping, each of which widens what code runs or what
  it reaches. The loader refuses the tool on an agent whose writes do not
  name `core/kind`.
- The policy door speaks for each document as a `put` of its kind. A refuse
  or a gate refuses the whole batch: a declaration is admission, and no
  request can hold one, so a gated batch is the owner's to apply.
- The batch meets the door's guards unchanged: `authorizeDeclarationWrite`
  (a shipped or published package refuses), `authorizeNewPackage` (a new
  package only under the repository's authority, which the tool's card
  names, since nothing else a model sees does), the refuse-breakage guards,
  and a lossy plan, which refuses because its confirmation is the owner's.
- The apply's changelog entries are stamped on the tool row as `changes`,
  as `write`'s are.

A token for the agent was refused: a token reaches every kind, and the
grants an agent carries would stop meaning anything.

### Consequences

- Good, because the LLM sample's assistant hands collections to a sub-agent
  whose writes name the two declaration kinds alone, and plans before it
  applies.
- Good, because a preview costs nothing, so a model corrects a refused
  batch before anything lands.
- Bad, because an agent granted `*` already reaches `core/kind` and every
  other declaration kind through `write`; this record does not narrow that,
  and the sample keeps declarations off its assistant only by prompt and by
  giving the tool to the sub-agent.
- Bad, because a gate cannot hold a declaration for review; an owner who
  wants every schema change reviewed refuses the agent and applies its plan.

### Confirmation

`internal/engine/agentvocabulary_db_test.go`: a seeded Moss session in which
the architect plans and applies a package and a kind and Moss writes the
first record; the refusals outside the emit, of a data kind and of a shipped
package; a gated apply refused while the plan answers; and the load error.

## More Information

The sample's agents are in `samples/llm/bundle.yaml` (`moss`,
`mossArchitect`, `mossLibrarian`) and the session examples in
[docs/plans/moss.md](../plans/moss.md). Revisit when a declaration can be
held as a request, or when an agent's `*` stops reaching declaration kinds.
