# Documentation

The substrate keeps your digital life in one place and lets software act on
it. Messages, mail, calendar, people, tasks and media go into one typed set of
records behind one API, stored in Postgres on a machine you run. Assistants
read those records and write back through the same API.

The design borrows deliberately from Kubernetes, the most battle-tested
answer to "many semi-trusted programs cooperating over shared typed state":

- the substrate is the **API server**: typed records, declared validation,
  an ordered change feed;
- every application and integration is a **controller**: it watches, decides,
  and writes back through the same public API;
- behavior lives in **declarations** (kinds, states, mappings, functions),
  not in bespoke endpoints. The write API is five generic mutations,
  forever: `put`, `patch`, `delete`, `merge`, `split`;
- a closure of those declarations installs and uninstalls as one unit, a
  **bundle**, which is how a provider integration or an automation reaches
  the data without a substrate code change.

These pages build one thing, a to-do list, from registration through to the
API call that completes a task. Start at
[getting started](getting-started.md).

## Start here

- [Terms](terms.md) — one word per thing, and the dead words they replaced
- [Getting started](getting-started.md) — register, log in, write a record

## The model

- [The data model](data-model.md) — the repository and its changelog, records and
  kinds, property types, traits, and validation
- [Traits and interfaces](traits.md) — shared properties, declaring your own,
  and the cross-kind queries binding unlocks
- [Vocabulary as records](vocabulary.md) — the declarable kinds, admission,
  and how the vocabulary evolves
- [Projection](projection.md) — record mappings, managed properties, and merges

## The API

- [The API](api.md) — REST, filters, mutations, errors, GraphQL, and search
- [Users, tokens, and actors](auth.md)
- [The changelog and watch](changelog.md)

## Bundles

- [Bundles](bundles.md) — installable closures of functions, agents,
  vocabulary, or a provider, applied and removed as one unit
- [Functions and the host SDK](functions.md)
- [Agents](agents.md) — assistants that read your records and write back
  through the same API; this part is alpha
- [The bundles catalog](bundles-catalog.md)

## Tools and operations

- [substratectl](substratectl.md) — the CLI
- [The web console](console.md)
- [Running a substrate](operations.md)
- [Testing](testing.md) — every suite, which to reach for, and how to give the
  live one keys
- [Built-in kinds](builtin-kinds.md) — reference

---

These pages describe the system as built. Where a page and the code disagree,
the code is right — `AGENTS.md` in the repository root is the working guide,
and the tests are what the substrate actually promises.
