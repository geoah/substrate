---
status: accepted
date: 2026-09-26
decision-makers: George Antoniadis
amended-by: 0148
---

# 0130. The console serves four things, and its navigation follows them

## Context and Problem Statement

Written after the fact, from the console review of 2026-09-24
([the first review](../console/reviews/2026-09-24-first-review.md)) and the
owner's rulings over the redesign in PR #648. The console before it was
organised around the substrate's machinery: the sidebar opened on some twenty
core kinds, one provider was set up across three pages (Registry,
Connections, Settings), and the overview led with "what needs a decision".
Nothing said what the console was for, so every new page was placed by
whoever wrote it, and the navigation grew with the engine rather than with
what a person does.

## Considered Options

- Keep the machinery's shape: a tree of authorities, packages and kinds, one
  page per engine surface (registry, connections, changelog)
- An inbox first: one "what needs you" surface gathering merge requests,
  suggested changes, questions and failing syncs
- Four purposes: seeing, navigating and controlling your data, your
  providers, your agents and your (and your agents') tools, with every page
  belonging to one of them

## Decision Outcome

Chosen: four purposes. The sidebar's places are **Home**, **All data**,
**Agents**, **Tools** and **Providers**, then the collections grouped the way
a person meets them (**Your data**, one **From _Provider_** per provider),
and History, Search and Settings serve those four rather than standing beside
them. There is no inbox. Agents are a chat app with threads, and a change an
agent suggests is a card in the thread that asked for it; a merge request is
reached from the records it names. Apps a person builds through an agent
(a recipe book, a trip planner) are packages like any other, so what they
store shows up as ordinary collections and needs no place of its own.
Providers are one page per provider, samples are added from **Data › Add a
collection** (and from Tools and Agents for samples that bring tools or
agents), and "integration" stays a dead word: the place is **Providers**.

The machinery's shape answers what the engine holds, not what the person is
doing, and it is still one switch away in technical mode (the authority tree
and the **Substrate** section of the sidebar). An inbox gathers suggestions
away from the conversation that explains them, and in practice fills with
the engine's own queues (parked deliveries, pending merges), which the owner
ruled is not what the console is for.

### Consequences

- Good, because every page answers one question: which of the four it
  serves, and the sidebar is ordered the same way.
- Good, because a suggestion is decided where its reason is, in the thread,
  with the agent's rationale beside it.
- Bad, because nothing gathers pending suggestions across threads: one in a
  thread the person has not reopened waits until they do, or until they meet
  it under the record's **Connected to**.
- Bad, because controlling an agent is thinner than seeing it: what an agent
  may see and change is read on its panel but edited on its record, until a
  grants editor exists (issue #678).
- Bad, because an agent-built app cannot yet say who built it: a package does
  not record the actor that declared it (issue #672).

### Confirmation

`components/app-sidebar.test.tsx` holds the groups (everyday and technical)
and the account menu; `router-providers.test.ts` holds that the old
`/registry`, `/connections` and `/settings/{id}` addresses land on Providers. That there
is no inbox is held by review only.

## More Information

[The web console](../console.md) describes each page. The second review ([2026-09-26](../console/reviews/2026-09-26-design-review.md))
measured the build against these purposes. Reopen trigger: a surface a person
needs that none of the four can hold.
