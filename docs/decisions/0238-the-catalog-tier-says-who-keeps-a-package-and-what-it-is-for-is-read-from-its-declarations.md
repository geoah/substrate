---
status: accepted
date: 2026-11-07
decision-makers: George Antoniadis
amended-by: 0258
---

# 0238. The catalog tier says who keeps a package, and what it is for is read from its declarations

## Context and Problem Statement

The catalog gave every package under `kinds/` the `provider` tier, the
well-known catalogues included
([0192](0192-observations-is-seeded-and-the-well-known-catalogues-are-first-party-packages.md)),
and gave it to every package an authority document publishes
([0212](0212-a-package-is-published-by-its-authoritys-document-and-installed-from-it.md)).
The tier says who keeps the declarations, and `substratectl` reads it that
way to choose `install` or `import`. The console read it as "an outside
service": Providers listed `health`, `labs`, `environment` and `clinical`
under **Accounts you connect**
([0233](0233-the-providers-page-lists-every-outside-service-the-substrate-uses-a-key-or-a-sign-in-for.md)),
and with them the packages an app's approval installs, such as Workouts'
`training` and `trainingcoach`. Slack live events read as a tool for agents
before it was added and as an account after, the guess 0233 recorded as a
Bad consequence. Add a collection offered only samples, so a catalogue had
no right place at all.

## Considered Options

- Keep the tier, and fix the console's grouping rules.
- A third tier, `catalogue`, loaded from its own tree.
- Name the tier for what it says (`published`), and have the catalog say
  what a package is for: whether it is a provider, what it brings, and which
  provider an add-on extends.

## Decision Outcome

Chosen: the third. The tier is `published` or `sample` and picks the door,
nothing else. Beside it a catalog entry carries `provider`, `brings` and
`addonFor`, read when the closure is loaded, so the answer is the same before
a package is added and after.

- **`brings`** lists `account` (a kind binds `core/accountconfig`), `tools`
  (a function offers itself to agents,
  [0198](0198-a-bundle-offers-its-functions-and-an-agent-takes-them-by-pattern.md)),
  `collections` (a primary kind), `metrics` (a record of the observations
  package's `metric` kind) and `agents`.
- **`provider`** is true for every package the binary ships under
  `providers.substrate.reamde.dev`, which the catalog loads as its own root,
  and for any other published package that brings an account. Apple is why
  the tree speaks: its devices push through the Sync app, so it declares no
  account kind, and binding `accountconfig` on its kinds would refuse every
  push, because an app's token never writes an account record.
- **`addonFor`** is the provider a package extends: what its publisher's
  document names
  ([0216](0216-an-add-on-package-waits-for-the-provider-it-extends-under-the-approval-already-given.md))
  when the binary ships that as a provider, else the first package it
  requires that is a provider, for a published package that brings no
  account of its own. WHOOP requires `health` and is no add-on, and a
  document naming a catalogue makes nothing an add-on, since nothing could
  list it there; the approval still waits on the document's word.

Each bundle status carries `provider` and `addonFor` too, joined in by the
API: the catalog's answer where the binary ships the package, else whether
the repository's stored declarations bring an account. A package from an
authority document whose publisher cannot be read keeps its place the same
way. The sidebar's and Home's counts read the statuses, because the
catalog's previews are too dear for a read on every page.

The console places packages by these. **Providers** lists providers only,
and an add-on on the page of the provider it extends; its Tools group is a
provider that brings tools and no account. **Add a collection** gains
**Standard collections**: a published package that is neither, bringing
collections or metrics. Every other held package's home is its package page
in All data, with its update, pause and removal: **Added** links to it, and
so does each package an app set up, under Settings › Apps.

A console-only fix kept the word that invited the mistake and the guess made
before installation. A third tier keyed the role on a tree, which a package
from an authority document does not have, so `training` would still have
needed a rule of its own.

This amends [0048](0048-providers-are-published-samples-are-copied.md) (the
tier's name, and that a provider is what the providers tree ships) and 0233
(what Providers lists, and how it groups). Neither is superseded.

### Consequences

- Good, because a catalogue and an app's package are offered where
  collections are, and Providers holds only what connects to an outside
  service.
- Good, because the groups are right before installation, so a package no
  longer changes group when it is added.
- Bad, because the wire value `tier: "provider"` is now `"published"`: a
  client outside this repository that compares it breaks, so the change
  ships as a breaking one.
- Bad, because `provider` for a shipped package comes from its tree, not its
  closure: moving a package into the providers tree changes the console
  without changing a declaration.
- Bad, because the internal word **catalogue** stays, one letter from
  **catalog**; only the console says standard collection.
- Bad, because a status cannot read an authority document, so a package
  from one counts beside Providers as its stored declarations say, and an
  add-on that only its document names counts as a provider there while the
  page lists it with the one it extends.

### Confirmation

`TestCatalogSaysWhatEachPackageIsFor` and
`TestPublishedPackageSaysWhatItIsFor` (`internal/catalog`) hold the reading
over the shipped tree and a document's package, and
`TestBundleStatusesSayWhatEachPackageIsFor` (`internal/api`) the statuses.
In the console, `providers.test.tsx` holds that a standard collection and an
add-on get no row on Providers, `provider.test.tsx` the add-ons on their
provider's page and the page of a package not yet added,
`add-collection-dialog.test.tsx` and `sample-picks.test.ts` the standard
collections, and `standard-package.test.tsx` the update and removal on the
package page.

## More Information

Revisit when a package from an authority document must say it is a provider
without an account kind, as Apple does from the tree: that wants a statement
in the authority document.
