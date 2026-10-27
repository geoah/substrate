---
status: accepted
date: 2026-10-27
decision-makers: George Antoniadis
---

# 0192. Observations is seeded, and the well-known catalogues are first-party packages

## Context and Problem Statement

The observation kinds and the health catalogue shipped as samples (0159, 0189),
and so did the adapters that turn a provider's mirror into them (`healthapple`,
`healthwhoop`). A sample is imported and rehomed under each repository's own
authority, so the same observation kind had a different name in every
repository, no provider could name it, and an app connecting a device had to
import three samples before one reading became an observation (issue #1068).
The engine already reads these kinds only through core's traits; what was
missing is a fixed name substrate owns and keeps upgrading.

## Considered Options

- Keep the samples (0159 and 0189 as they stand).
- Put the base kinds in core.
- Seed a separate `observations` package, and ship the domain catalogues as
  first-party packages a repository installs when it wants them.
- Publish everything, observations included, under a provider authority.

## Decision Outcome

Chosen: seed `substrate.reamde.dev/observations`, and ship the well-known
catalogues as installable first-party packages under `substrate.reamde.dev`,
because measured data is something every repository and every provider needs
under one name, while a domain is something only some repositories want.

- **The seed is a list, not the authority.** `core`, `llm`, `console` and
  `observations` are seeded into every repository and upgraded at open. Every
  other package under `substrate.reamde.dev` is served by the catalog: it
  installs under its own authority, is published (an owner writes its records,
  never its declarations) and upgrades from the catalog, as a provider does.
- **`observations` holds the five base kinds** (`metric`, `observation`,
  `trace`, `source`, `sourcelink`) and no metrics. Its kinds keep their own
  `purpose`, because they are the owner's data and not the substrate's state,
  and so their records may be merged.
- **A well-known catalogue holds shared metrics, the domain's kinds, its extra
  units and conversions, and its functions.** `health` (sleep, workout, body,
  heart, activity, vitals), `labs` (lab reports and results, analytes, the
  conversion between unit systems) and `environment` (climate and energy) are
  the first three.
- **A metric's id starts with the package that ships it** (`health/heart_rate`,
  `labs/glucose`, `whoop/strain`). That prefix is the package's: an owner's own
  metrics use another one.
- **A provider maps its data into the well-known metrics, and owns only what no
  one else would share.** The Apple Health adapter ships in `applehealth` and
  the WHOOP adapter in `whoop`, each requiring `health` (0168 already lets a
  provider write another package's kinds). WHOOP's strain, recovery and sleep
  performance are `whoop/*` metrics shipped by `whoop`.
- **An install installs the first-party packages it requires.** A required
  package the catalog serves as published (a provider or a first-party
  catalogue) is installed first, so installing `applehealth` brings `health`
  with it. A missing sample is still refused.
- **An install never repoints another package's trigger.** A shipped trigger
  whose id is already held by a trigger calling another package's callable is
  refused, naming both.

The samples `observations`, `health`, `healthapple` and `healthwhoop` are
removed. Rejected: core, because a fifth of core would be domain vocabulary
and its version would move with every metric; a provider authority for
everything, because seeding is what makes observations always present, and
"provider" names an integration; and keeping the samples, for the reasons in
#1068.

### Consequences

- Good: one name for an observation in every repository; a provider writes
  observations directly; Substrate Sync's set up is one install.
- Bad: an owner can no longer add a property to `observation` or `sleep`; a
  domain extends through `qualifiers` and its own kinds pointing at the base
  ones, as 0159 already required of a domain.
- Bad: a repository that imported the old samples holds its rows under its
  own authority. Nothing moves them automatically: the adapters' outputs are
  rebuilt by replaying the mirrors, and anything an owner wrote is moved by
  hand, through the record routes, before the old packages are uninstalled.

### Confirmation

`kinds/kinds_test.go` holds the seed list; the catalog tests hold that a
first-party package installs published and brings its requirements; the
provider tests run both adapters into the first-party kinds.

## More Information

Supersedes [0159](0159-the-base-observation-kinds-live-in-a-sample-every-domain-sample-requires.md)
and [0189](0189-the-observation-source-kind-stays-in-the-observations-sample.md).
Builds on [0047](0047-a-kind-lives-in-a-package.md),
[0048](0048-providers-are-published-samples-are-copied.md),
[0077](0077-the-llm-kinds-live-in-their-own-seeded-package.md),
[0168](0168-a-provider-s-live-events-ship-as-an-opt-in-bundle-that-requires-it.md)
and [0186](0186-units-and-conversions-are-core-records-and-a-value-converts-through-the-repositorys-graph.md).
Issue [#1068](https://github.com/geoah/substrate/issues/1068).
