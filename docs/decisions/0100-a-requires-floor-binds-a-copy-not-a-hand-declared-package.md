---
status: accepted
date: 2026-09-23
decision-makers: George Antoniadis
---

# 0100. A `requiresAtLeast` floor binds a copy, not a hand-declared package

## Context and Problem Statement

Record [0070](0070-a-copy-is-upgraded-through-its-origin-stamp-and-requires-pins-a-floor.md)
gave a bundle `requiresAtLeast`, the least version of a required package the
closure declares against, and the loader compares it against the stored
package version on every door. An import rehomes the floor with the rest, so
calendar's `samples.substrate.reamde.dev/people: 4` lands as
`<home>/people: 4`. A repository whose owner declared a `people` package of
their own, at version 1, is then refused calendar for good
([#614](https://github.com/geoah/substrate/issues/614)): "needs
`<home>/people` at version 4 or later, and this repository holds version 1".
The owner's version line has nothing to do with the sample's, and the two
ways out are both wrong. Re-importing people replaces the owner's package
whole (0048). Pinning `version: 4` on their own package asserts a number
that means nothing there.

## Considered Options

- Compare the floor against every held package, as 0070 does today, and tell
  the owner to pin their package's version.
- Refuse a hand-declared package outright: the closure declares against the
  sample, and a package outside the sample's version line cannot be known to
  fit.
- Bind the floor to a copy: drop it against a package the repository
  declared by hand under its own authority, and let admission's pins decide.

## Decision Outcome

Chosen: the floor binds a copy. Before admission, on every door
(`relaxFloorsOnOwnPackages`, `internal/engine/requiresfloor.go`), a floor
whose package is under the repository's own authority, held, and stamped
with no origin is dropped from the bundle document, and the landed bundle
requires that package by name alone. A floor on a copy (origin stamped), on
a package the batch itself declares, on a package under another authority,
or on one the repository does not hold is left for the loader to compare or
refuse, exactly as 0070 wrote it.

It beat the first option because the version on a user's own package is the
user's, and asking them to move it to satisfy a sample's floor is asking
them to lie about it. It beat the second because a sample is vocabulary the
user owns (0048), and a user who wrote their `people` by hand instead of
importing it has done what 0048 invites. What the floor exists to catch, a
closure landing against a copy whose shape is older than it was verified
against, does not arise for a hand-declared package: the closure's pins are
spelled in full (0098) and admission refuses a pin at a kind the package
does not declare.

The drop happens in the engine and not in the loader because the loader
reads documents, and `origin` is a property the engine stamps and never a
document key. It lands in the document rather than being skipped at compare
time so that the stored bundle, the boot's re-check of it and every later
read agree on what the closure requires here.

### Consequences

- Good, because a repository with its own `people` imports every sample that
  declares against people, and the pins land on the owner's kinds.
- Good, because a copy is held to the floor exactly as before, and the
  console's read of the floor (which compares against held bundle versions
  and has none for a hand-declared package) agrees with the server.
- Bad, because a hand-declared package that lacks a property or trait the
  closure's mappings or functions rely on is refused one problem at a time
  by admission, not by one line naming a version.
- Bad, because the landed bundle document differs from the shipped one by
  the dropped key, so a reader comparing the two sees an edit the owner did
  not make. The origin digest is computed over the landed rows, so the copy
  still reads pristine.

### Confirmation

`internal/engine`'s `TestARequiresFloorBindsACopyAndNotAHandDeclaredPackage`
holds the drop and the three cases the floor still refuses;
`internal/catalog`'s `TestImportLandsOnAHandDeclaredRequirementWhateverItsVersion`
replays the repository in #614 through the catalog doors to a landed
calendar with pins on the owner's person and a pristine stamp.

## More Information

Amends [0070](0070-a-copy-is-upgraded-through-its-origin-stamp-and-requires-pins-a-floor.md):
its floor is compared against a copy and not against every held package;
the rest of 0070 stands. Rests on
[0048](0048-providers-are-published-samples-are-copied.md) and
[0098](0098-a-declaration-names-a-kind-or-trait-in-full.md). Reopen if a
sample needs to state a shape requirement on a hand-declared package, which
is a trait contract on the required kind rather than a version.
