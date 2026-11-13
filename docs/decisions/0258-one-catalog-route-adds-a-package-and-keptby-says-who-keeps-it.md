---
status: accepted
date: 2026-11-13
decision-makers: George Antoniadis
---

# 0258. One catalog route adds a package, and `keptBy` says who keeps it

## Context and Problem Statement

A catalog entry said how a package arrives in a field called `tier`, with the
values `published` and `sample`
([0238](0238-the-catalog-tier-says-who-keeps-a-package-and-what-it-is-for-is-read-from-its-declarations.md)),
and a client read it to choose between two routes,
`POST /api/v1/catalog/{id}/install` and `…/import`. "Tier" suggests a ranking
that does not exist, and the word already names something unrelated: the
standing of a property's writer (`owner`, `bundle`, `machine`). The two routes
put the choice on every client, which the server could make itself. The 1.0
review asked for one "add" route and a field that says who keeps the package
([#1313](https://github.com/geoah/substrate/pull/1313), row 20), before the
names freeze.

## Considered Options

- Keep `tier` and the two routes.
- Rename the field and keep the two routes.
- One route, `POST /api/v1/catalog/{id}/add`, and a field `keptBy` whose
  values name the keeper: `publisher` or `repository`.

## Decision Outcome

Chosen: one route and `keptBy`. The server already knows who keeps every
package, so it takes the door itself: a package its publisher keeps lands
under the authority that publishes it, and one the repository keeps (a
sample) is copied under the repository's own authority first. Every sample
ships in the binary, so the binary's closure decides, and anything else is
its publisher's. The body and the answer are what the two routes took and
gave; `expect`, the publisher's version a reader reviewed, is refused on a
sample, which has no publisher.

`keptBy` replaces `tier` on a catalog entry and on an approval's package row.
Its values name who keeps the package, not how it travels. `published` and
`sample` stay the words for the two sorts: a published package is kept by its
publisher, a sample by the repository that adds it.

`substratectl add` is the verb, and `install` and `import` stay its aliases,
so a script that names either still runs. The MCP `packages` tool takes `add`.

This amends 0238 (the tier's name, and that it picks the door) and
[0048](0048-providers-are-published-samples-are-copied.md) (the two doors).
What a copy is and where it lands are unchanged.

### Consequences

- Good, because a client makes one call for every package and cannot pick the
  wrong door.
- Good, because one word has one meaning again: a tier is a property
  writer's standing, and nothing in the catalog.
- Bad, because the routes and the field change on the wire, so every client
  that called `install` or `import`, or read `tier`, breaks, and the change
  ships as a breaking one.
- Bad, because a sample can no longer be installed verbatim under the sample
  authority over HTTP: adding one always copies it.

### Confirmation

`TestCatalogAddLandsEachPackageWhereItsKeeperSays` and
`TestCatalogListSaysWhoKeepsEachPackage` (`internal/api`) hold the route and
the field; `TestInstallAndImportAreAliasesOfAdd`
(`cmd/substratectl/commands`) the verbs; and the console's
`bundle-actions` tests that adding either sort makes the one call.

## More Information

Revisit when a sample is published by an authority document: then the
binary's closure no longer decides alone, and the catalog resolver has to.
