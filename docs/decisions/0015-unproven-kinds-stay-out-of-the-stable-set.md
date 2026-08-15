---
status: accepted
date: 2026-08-15
decision-makers: George Antoniadis
---

# 0015. Unproven kinds stay out of the stable vocabulary set

## Context and Problem Statement

The v1 freeze is close, and freezing is one-way: once a kind ships in the
stable vocabulary and repositories hold records of it, dropping or retyping a
property is a refused narrowing. The 2026-08 domain-kinds audit
([#121](https://github.com/geoah/substrate/issues/121), under the
[#119](https://github.com/geoah/substrate/issues/119) tracker) named the
shipped kinds with no consumer in the tree: `media.*` (13 kinds, 2 property
types and the one shipped mapping, shaped for connectors that do not exist),
`memory/episode` (one specific agent-memory architecture, every field a bet),
and the three self-described demos (`llm.examples`, `web.bundles`,
`notes.bundles`). Doing nothing freezes all of them.

## Considered Options

- Ship everything and mark the unproven kinds ALPHA, the way the agent
  vocabulary is marked
- Remove the unproven domain vocabulary from the tree; keep the demos shipped
  but state that an `example` bundle sits outside the stable set
- Remove everything unproven, demos included

## Decision Outcome

Chosen: remove `media` and `memory` from the tree, keep the demos shipped
outside the stable set. An ALPHA marker alone does not stop repositories
importing 15 unproven kinds and accreting records that make every later fix a
refused narrowing, so the domain vocabulary with no consumer leaves the tree;
git keeps it, and it returns redesigned against its first real consumer.
Removing the demos too would gut the onboarding path: they are the worked
examples the docs walk through, and `llm.examples` ships the provider rows a
substrate needs before any agent runs. So the demos stay, and the `example`
facet now states what it always meant: an example's declarations may change or
leave without an upgrade path.

`recordpatchpolicy` cannot leave (the policy door ships in core), so its
unproven judge half (`judge`, `criteria`, `context`, `autoAccept`,
`autoRefuse`, `mode` — declared but inert until the thread-interactions plan's
phase 4) carries the existing ALPHA marker instead: those properties may
change or leave without a v1 break, while the selector/action half freezes.

### Consequences

- Good, because the stable set at v1 is only vocabulary with usage behind it,
  and every held-back field can still be renamed, retyped or dropped for free.
- Good, because the other audit issues (#120, #122–#125, #143) shrink: the
  removed files leave their scope.
- Bad, because a repository that already imported `media` or `memory` keeps
  the installed declarations with no upgrade or removal path shipped; they
  are frozen in place until the vocabulary returns.
- Bad, because a media-shaped connector now has no shared vocabulary to map
  onto and must wait for (or motivate) the redesign.

### Confirmation

`cmd/vocabularydiff` (the `kinds:check` gate) treats a directory removed whole
as a bundle leaving the catalog, so the removal passes the version gate by
design. The example fences are comments in the three `bundle.yaml`s, the
`exampleFacets` note in `internal/catalog/catalog.go` and the catalog page;
the judge-half fence is the marker in `recordpatchpolicy.yaml`. Held by review
beyond that.

## More Information

Revisit when a real consumer of a held-back vocabulary lands in the tree — a
media connector, an agent-memory implementation — or when remote bundle
install (ticket 011) gives unproven vocabulary a home outside the binary.
