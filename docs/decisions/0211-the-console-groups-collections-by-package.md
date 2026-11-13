---
status: accepted
date: 2026-11-13
decision-makers: George Antoniadis
amended-by: 0231
---

# 0211. The console groups collections by package

## Context and Problem Statement

[0130](0130-the-console-serves-four-things-and-its-navigation-follows-them.md)
put every collection that is not a provider's copy or the substrate's
machinery in one **Your data** section, listed in everyday mode as one
alphabetical run of display plurals. On a real repository that run mixes the
Workouts app's `training` kinds with the `health`, `labs` and `observations`
catalogues and every imported sample, so "Workouts" (health) sits beside
"Workout routines" (training) and nothing says which is which. The package,
the one fact that tells them apart, is shown only in technical mode. The
[authorities plan](https://github.com/geoah/substrate/pull/1145) is about to
move `training` from the repository's own authority to
`substrate.reamde.dev`, so whatever groups collections has to survive that
move.

## Considered Options

- Keep one **Your data** section (0130's shape)
- One section per authority, the publisher
- One section per app, the client that declared or uses a package
- One section per package
- **Your data** as an outer heading, with a subsection per package

## Decision Outcome

Chosen: one section per package, titled by the package's word ("Training",
"Health", "Tasks") and ordered by it, beside the **From _Provider_**
sections and the technical-mode **Substrate** section 0130 already has. The
package is the unit one publisher versions, owns and quarantines together
([0047](0047-a-kind-lives-in-a-package.md)), and a section is keyed by its
full `authority/package`, so two packages that share a word (`training` under
the repository and under `substrate.reamde.dev`, while the plan's move is in
flight) are two sections, each naming its authority.

An authority answers who publishes, not what a collection is about:
`substrate.reamde.dev` publishes `health`, `labs`, `observations` and, after
the plan, `training`, and a repository's own authority holds every sample it
imported. Grouping by it would put Health and Workouts in one section and
move the Workouts collections when the plan lands. An app maps to packages
many to many (providers write into `health`, the plan's `apps[].packages`
lists what an app may ask for, not what it owns), and samples and an agent's
packages have no app, so an app is at most a label on a section; 0130
already holds that an app is packages and needs no place of its own. The
nested shape keeps 0130's yours-or-copies split visible, at the cost of a
level in a narrow sidebar, while the "From" in a provider's heading already
says a section holds copies.

Everything else in 0130 stands: the four purposes, the places, the provider
sections, the machinery behind the technical switch, and no inbox.

### Consequences

- Good, because a collection reads with its package on every surface that
  calls `collectionGroups`: the sidebar, All data, ⌘K (which also finds a
  collection by its package's name) and Search's collection picker, and a
  collection's first crumb names its package and opens the package's page.
- Good, because the plan's move of `training` changes one section's
  authority, not where its collections sit.
- Bad, because the sidebar carries a heading per package, and a package with
  one primary kind is a one-row section whose heading may repeat the row
  ("Tasks" over "Tasks").
- Bad, because the sidebar no longer says "Your data" over what the owner may
  change; the distinction rests on the "From" of a provider's section.
- Bad, because Home still ranks the owner's packages together under **Your
  data**: nine cards cannot spend places on a heading per package, so Home is
  the one surface whose sections are not the sidebar's.
- Bad, because a section's title is the package's word, which reads poorly
  for an opaque one; no declared package label exists, and reserving one is a
  dialect change ([0020](0020-dialect-keys-are-reserved-not-tolerated.md)).
- Bad, because a stored fold of the old section (`group:yours`) is ignored:
  every package section opens until the owner folds it.

### Confirmation

`web/console/src/lib/collections.test.ts` holds the sections, their order,
their keys and the shared-word labels; `components/app-sidebar.test.tsx` the
headings and the folds stored under each package's key;
`components/app-shell.test.ts` the crumbs; `components/command-menu.test.tsx`
the match by package name.

## More Information

This amends the section rule of 0130 and nothing else in it.
[The web console](../console.md#navigation) describes the sections as built.
Reopen triggers: a repository whose package sections outgrow the sidebar,
answered by sections the owner arranges in the console preference record
rather than by a category key publishers declare; and a declared package
label, which would replace the word as a section's title.
