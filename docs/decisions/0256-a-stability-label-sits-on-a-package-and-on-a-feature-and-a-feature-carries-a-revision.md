---
status: accepted
date: 2026-11-12
decision-makers: George Antoniadis
---

# 0256. A stability label sits on a package and on a feature, and a feature carries a revision

## Context and Problem Statement

Discovery (`GET /.well-known/substrate/server.json`) stamped a `stability`
of `stable`, `beta` or `alpha` on each entry of a feature list that was a
literal in `internal/api/discovery.go`. Nothing held the list to the router,
so reading and writing a record, the error format and signing in were served
and not listed. No client acted on the labels. A kind had no label a client
could read at all: that `core` was the frozen part and the agent kinds were
not was a comment in `kinds/substrate.reamde.dev/core/core.yaml`. Two
changes show what that cost. v0.121.0 changed a value the catalog answers,
under the `stable` feature `bundles`, and v0.112.0 made a property of
`core/trigger` required, which changed a kind and no route
(`CHANGELOG.md`). The proposal this record settles is written in
[pull request 1313](https://github.com/geoah/substrate/pull/1313), and the
policy it leads to is [docs/stability.md](../stability.md).

## Considered Options

- Keep the hand-written list, and add the missing entries to it.
- Semantic versions per feature.
- A label on each kind.
- A list of packages, with their labels, in the discovery document.
- A label on a package, for its kinds; a label and a revision on a feature,
  for its routes; one table of features that a test holds to the router.

## Decision Outcome

Chosen: the label on a package and on a feature, with a revision on the
feature and one table behind the list, because it gives a kind one place to
look and a route one place to look, and because a list a test holds to the
router cannot leave a served route unlabelled again.

What this sets:

- **Three labels**: `stable`, `beta` and `experimental`
  (`internal/substrate/stability.go`). `alpha` is renamed `experimental`,
  and the wire value changes with it. The policy page says what each
  promises.
- **A package states its label** in its header's `stability`, for every
  kind it declares. The key is optional. An absent one stores nothing and
  reads as `experimental`; any other value is a load error. It is an
  authored property of the `package` record, so a client reads it there. No
  write is refused over it.
- **A feature is a group of routes** with a label and a `revision`: a
  number that starts at 1, only goes up, and goes up when the feature gains
  something a client may want to check for. An entry may carry a `notice`,
  where a coming break to a beta feature or a deprecation is announced.
- **The list is one table**, `featureTable` in `internal/api/features.go`,
  which discovery serializes. Every route the router registers is in
  exactly one feature. `records`, `errors`, `auth` and the other served
  routes that were missing are listed.

The hand-written list is what failed: nothing made an entry follow a route.
Semantic versions per feature add nothing a client can use. The server's own
version is already one and is in the document; under `/api/v1` every change
to a feature is an addition and a break is a new prefix, so one increasing
number says all there is, as a kind's and a package's `version` already do.
A label on each kind is a second place to look for the same answer, and
every kind of a package would repeat one word. A package list in
discovery makes the document open a repository, which it never does (it
answers before a client holds a token, and packages are a repository's), and
it would be a second copy of what the package record says.

[0015](0015-unproven-kinds-stay-out-of-the-stable-set.md) left an ALPHA
marker on the judge half of `recordpatchpolicy`, inside a kind that could
not leave `core`. That marker stands and is now spelled `experimental`: the
policy page names the judge half, and the `agent` kind, as the two parts of
`core` its `stable` label does not cover. A package label cannot say that,
which is the cost of having no label on a kind.
[0053](0053-rest-is-supported-all-of-graphql-is-preview.md) kept
`stability` as the per-feature axis and put `compatibility` on a surface;
[0079](0079-graphql-is-removed-and-the-records-read-is-one-route.md), which
superseded it, kept the keyed `surfaces` object. Both axes stand. This
record changes the third stability value and what earns an entry.

### Consequences

- Good, because a route added to the router fails a test until a feature
  holds it, so the list is complete by construction.
- Good, because a client asks for "at least revision N" before it sends a
  request field a later release added, instead of trying the request to see
  whether it is refused.
- Good, because a kind's label is on a record every client can already
  read, and the console shows it.
- Bad, because `alpha` to `experimental` breaks a client that compared the
  value. No first-party client did outside its tests.
- Bad, because the test holds routes and nothing else: a revision that
  should have risen for a new request field or parameter is caught by
  review alone.
- Bad, because a package label is coarse. An exception inside a labelled
  package lives in prose on the policy page, where no client reads it.
- Bad, because the label is a statement the server does not enforce. A
  `stable` package can still ship a narrowing where no live record holds
  the old shape.
- Bad, because the key was added without raising the vocabulary dialect, so
  a binary from before it refuses a package header that states a label.

### Confirmation

In `internal/api`: `TestEveryRouteIsInOneFeature` walks the router with
every optional route on; `TestFeaturesMatchTheGolden` holds
`testdata/features.golden.json`, and its `-update` refuses a route set that
changed under an unchanged revision, a revision that went down and a new
feature that does not start at 1, and `mise run features:check` holds the
same three against the base branch's golden;
`TestDiscoveryServesTheFeatureTable` and `TestDiscoveryFeatureEntryKeys`
hold the served document to the table, and `TestDocsListTheFeatures` holds
the API guide's list to it. In
`internal/vocabulary`, `TestAPackageStatesItsStability` and
`TestStabilityIsAPackageHeaderKeyAlone`; in `kinds`,
`TestEverySeededPackageStatesItsStability`; in `internal/engine`,
`TestAPackageRecordCarriesTheStabilityItsHeaderStates`. `.mise/docscheck.sh`
refuses a reader-facing page that calls a label `alpha`.

## More Information

The house rule is in `AGENTS.md`: a route is placed in a feature, and a
feature that gains something raises its revision. Revisit when a package
needs two labels often enough that the by-name exceptions on the policy page
stop being a short list, or when a client needs the routes of a feature on
the wire.
