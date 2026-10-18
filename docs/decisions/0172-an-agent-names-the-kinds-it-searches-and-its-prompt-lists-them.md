---
status: proposed
date: 2026-10-18
decision-makers: George Antoniadis
---

# 0172. An agent names the kinds it searches, and its prompt lists them

## Context and Problem Statement

An agent's `query` tool took a `q` that named no kinds and searched the
whole reads allowlist, narrowed to the `primary` and `supporting` kinds by a
default commit 96be6499 added (the console's narrowing from
[0136](0136-search-ranks-by-bm25f-and-a-kinds-purpose.md), applied to
agents). Nothing told the agent which kinds exist: its system prompt was its
own `prompt`, and the tool card is the same text for every agent. So a model
guessed kind names or named none. On a repository of about 620,000 records,
a search naming none ranked about 500,000 chat messages and links: the
slowest and noisiest read the tool has. An assistant asked "when is my
flight?" searched `flight booking confirmation` over all of them.

## Considered Options

- Keep the default and tune the ranking further
- Accept `filter.purposes` alone as the narrowing
- Require kinds for an agent's text search, and list the readable kinds in
  the system prompt

## Decision Outcome

Chosen: require kinds, and list them.

The agent `query` tool refuses a `q`, and a list with `filter.search`, that
names no `filter.kinds` and no `kind`, with a tool error that points at the
kinds list. A plain list that names no kinds still lists the grant. The
engine appends the kinds list to the system prompt of every agent holding
`query`: the `primary` and `supporting` kinds its allowlist reaches (globs
expanded as the gate expands them; an `internal` kind only where named
outright), each with its purpose, the first sentence of its description and
the references it declares to listed kinds. A model that knows the kinds
picks the two that would hold a flight; a model that does not cannot pick
any, so the requirement without the list would only trade a slow answer for
a refusal.

Ranking cannot fix the cost: the candidate pool is what is slow, and it is
the size of what the call names. `filter.purposes` alone is the same pool
the default already was.

There is no agent-level key to turn the list off or narrow it. An agent
narrows its list by narrowing its reads grant. A declaration key added later
is a reserved-key change under
[0020](0020-dialect-keys-are-reserved-not-tolerated.md), so it waits until a
grant proves too coarse.

This narrows 0136 for agents alone. The REST records route, the direct call
of the `query` host function and the console's search still take a search
that names nothing. It amends
[0133](0133-a-kind-declares-its-purpose.md) again: the engine now acts on
`purpose` when it builds an agent's prompt.

### Consequences

- Good, because an agent's search reads only the kinds it named, and the
  list gives it the names, the shape (purpose, references) and a
  description to choose by.
- Good, because the list is rendered once per run in reference order, so
  its bytes are stable across turns and runs while the rendered fields do
  not change, which lets a provider that caches prompt prefixes reuse them.
  The bytes change when a listed kind's description, purpose or references
  change, when a package is installed, uninstalled or parked, and when the
  grant changes. Each chat turn is a new run and renders the list again.
- Bad, because every completion of an agent holding `query` pays the list
  in input tokens: a `*` grant over the shipped kinds adds about 23 KB, and
  about 28 KB on a real repository with 94 primary and supporting kinds.
  Past 40 KiB the rest are listed by reference alone, past a further 8 KiB
  counted.
- Bad, because kind descriptions now reach system prompts, and a kind
  description is writable by any writer whose grant reaches
  `substrate.reamde.dev/core/kind`. The list is fenced under a sentence
  saying the descriptions are data, never instructions, which a model may
  still not honor.
- Bad, because a question the agent cannot place in a kind now costs a
  refused call before it guesses, where before it searched everything.
- Bad, because an agent prompt that taught a bare `q` or `filter.search`
  breaks until it names kinds; the shipped samples were updated, a user's
  own agents were not.

### Confirmation

`TestAgentQueryListsKindsAndRequiresThemForQ`,
`TestSubagentGetsItsOwnKindsList` and
`TestDirectQueryCallKeepsItsSearchDefaults`
(internal/engine/agentquery_db_test.go) hold the list in the system message
the provider receives, its bytes across turns and runs, the refusals and the
unchanged direct call. The `TestKindsCatalogue*` tests
(internal/engine/agentcatalogue_test.go) hold the expansion, the `internal`
rule, the references, the order and the size caps.

## More Information

Reopen if real grants outgrow the cap often enough that agents search by
reference alone, if a search that names nothing becomes cheap (an index per
purpose, or a pool that does not grow with the repository), or if agents
need a list narrower than their grant.
