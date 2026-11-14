---
status: accepted
date: 2026-11-13
decision-makers: George Antoniadis
---

# 0268. A new declaration key raises the dialect, and a package states the dialect it needs

## Context and Problem Statement

[0020](0020-dialect-keys-are-reserved-not-tolerated.md) keeps the
declaration key sets closed and calls a new key "an ecosystem event",
because bundles are shared across servers. It left the event without a
number anyone could read. The vocabulary dialect was a per-repository stamp
that "never appears on the wire" (`internal/engine/dialect.go`); it had
moved once, for a seeded closure, and the package header's `stability` key
arrived without moving it. A package could not say which format it needed,
and the catalog took the newest version a publisher listed and failed if it
did not parse (`internal/catalog/published.go`), so a publisher that adopted
a new key hid the package from every older server, including those that had
installed it. This record amends 0020. The finding is F11, points 1 to 3, in
[pull request 1313](https://github.com/geoah/substrate/pull/1313).

## Considered Options

- Keep the number internal, and let an older server fail on the key.
- A minimum server release on the package.
- One dialect number, raised by every key that changes, published by the
  server and stated by a package and by each version its publisher lists.

## Decision Outcome

Chosen: one dialect number, raised by every key that changes, because it is
what a server can compare without fetching a closure, and what an author can
state without knowing which release added a key.

What this sets, beside 0020, which stands:

- **A key added to a declaration's closed key sets, removed or moved raises
  `vocabulary.Dialect`** in the release that admits it.
  `internal/vocabulary/testdata/dialect.golden.json` records every key set
  under its number. A test refuses a set that moved under an unchanged
  number and a key set it does not hold, and `mise run dialect:check`
  refuses the same against the base branch. The number is 3: `stability`,
  `interface` and `dialect` on the package header are new since 2.
- **The server publishes its dialect** in discovery as
  `vocabulary.dialect`, at the `discovery` feature's revision 2.
- **A package states the dialect it needs** in its header's `dialect`. A
  server that reads a lower one refuses the package, naming both numbers.
- **Each version a publisher lists may state its dialect.** The catalog
  installs the newest version at or above the floor whose dialect it reads,
  and a refresh passes over a version written for a newer one. A version
  that states none is fetched, and passed over the same way when its
  header states a newer dialect; any other refusal of a fetched version is
  the answer.
- **The repository stamp at open is unchanged**: a repository is raised to
  the binary's dialect, and a binary below a stamp refuses to open it.

### Consequences

- Good, because a publisher can adopt a new key and every older server goes
  on installing the version before, and a refusal names the number to
  upgrade to.
- Good, because a key cannot change without the number, by test and against
  the base branch.
- Bad, because every new key closes the way back: a repository opened by
  the new binary is stamped with its dialect, and the release before
  refuses it.
- Bad, because a server older than the `dialect` key refuses a header that
  states it, as an unknown key, so the field is useful from dialect 3 on.
- Bad, because a closed set of values, such as the property types, is not
  in the golden: a new type is not held to the number by a test.

### Confirmation

`TestDialectKeysMatchTheGolden` and
`TestEveryClosedKeySetIsInTheDialectGolden` (`internal/vocabulary`), and
`mise run dialect:check` in `ci:lint`. `TestAPackageStatesTheDialectItNeeds`
holds the header key, `TestTheResolverTakesTheNewestVersionThisServerCanRead`
the resolver, and `TestAnOlderServerInstallsTheLastVersionItUnderstands`
(`internal/catalog`) the install and the refresh.

## More Information

[Vocabulary evolution and the dialect contract](../vocabulary.md#vocabulary-evolution-and-the-dialect-contract)
describes the number, and [publishing a package](../apps.md#publish-a-package)
the version entry.
