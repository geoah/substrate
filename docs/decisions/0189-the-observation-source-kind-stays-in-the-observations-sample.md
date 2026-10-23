---
status: accepted
date: 2026-10-23
decision-makers: George Antoniadis
---

# 0189. The observation source kind stays in the observations sample

## Context and Problem Statement

Under [0159](0159-the-base-observation-kinds-live-in-a-sample-every-domain-sample-requires.md)
every repository holds its own copy of the `source` kind,
`<authority>/observations/source`.
[Issue #1036](https://github.com/geoah/substrate/issues/1036) asked whether
`source` should get one home the substrate owns, and named four costs. Checked
against the code, two of them are already handled and one is a non-goal:

- Domain samples pin the sample's spelling of `source`. The import rewrites
  every mention of `samples.substrate.reamde.dev` to the repository's own
  authority, pins included (`internal/vocabulary/rehome.go`), so an imported
  health copy pins the repository's own `source`. Handled.
- The WHOOP adapter composes the kind in its code. The same rewrite reaches a
  function's code, so its `HOME` becomes the repository's authority. Handled.
- A provider cannot write a source row. Narrowly true: a provider has no
  portable, exact reference to the source kind. An exact `writes` entry must
  name a kind the repository holds when the provider is installed
  (`internal/vocabulary/function.go`, `resolveFunction`), and a `requires:`
  is not met by a copy under another authority
  (`internal/vocabulary/bundle.go`). A glob entry skips that resolution and
  the effect check honors it (`internal/engine/effects.go`, `emitAllows`),
  but the provider cannot know the repository's authority, so only `*`
  reaches the copy, which grants every kind but the auth kinds. No provider
  needs to: every shipped writer of sources is an adapter sample, imported
  under the same authority.
- The same device is a different kind in every repository. A repository is
  everything one user has, and the substrate has no sharing between
  repositories, so nothing reads one repository's sources beside another's.
  This is a non-goal.

## Considered Options

| Option | The owner gains | Existing repositories |
| --- | --- | --- |
| Keep 0159 for all four base kinds | nothing new | nothing changes |
| Keep 0159, and pin the samples' `source` references at core's `source` trait | any kind binding the trait is accepted | a widening, taken at the next re-import |
| `source` and `sourcelink` in a new seeded package | one spelling; a provider may write sources | every observation and trace rewritten |
| `source` as a core kind | the same as a seeded package | the same as a seeded package |
| All four base kinds in a seeded package | providers write observed values directly | every observed row moved |

## Decision Outcome

Chosen: keep 0159 for all four kinds, `source` included.

Moving `source` rewrites every observation and trace a repository holds. Each
one pins `source` at the sample's kind, and the observed index keys each
stream by the source's record path (migration 0013), so a move repoints every
stored value and re-keys every stream. The default conversion ceiling is
10,000 records, and the boot upgrade refuses the whole shipped set when one
step is over it, which would also hold back core, `llm` and `console`.
Nothing performs that move today. `movedFrom` names a kind under the
declaring kind's own authority and is honored on the shipped boot upgrade
alone ([0078](0078-a-kind-move-is-ordinary-record-writes.md)). A repository
migration
([0099](0099-a-repository-migration-is-code-the-boot-runs-once-and-records.md))
can write rows of a kind the upgrade has not declared yet, as migration 0003
writes `core/unit` records, but none moves a kind across packages. One would
have to carry each source row with its id, rewrite every reference to it
(observations, traces, source links, the health kinds), refuse or resolve an
id the destination already holds, and rebuild the refs and observed indexes
for every rewritten row.

A seeded package costs what 0159 said it would. The owner could no longer
edit the declaration (to add a `class` value, say), and a bad declaration in
a fourth seeded package would refuse every repository's open. The reason
[0077](0077-the-llm-kinds-live-in-their-own-seeded-package.md) seeded `llm`,
that the engine addresses its kinds by name, does not apply: the engine reads
a source only through core's `source` trait. A core kind adds a clash: core
already has the trait `substrate.reamde.dev/core/source`, and a kind of the
same identity would make `filter.kinds` and `filter.implements` take one
string meaning two things. Seeding all four loses what 0159 chose the sample
for, an owner adding a property to a base kind, and moves every observed row.

Pinning at the trait waits for a second kind that binds it. Until one exists
it gains nothing, and a trait-pinned reference loses the bare-id completion
the aggregate read's filter gives a source (`manual` for
`<authority>/observations/source/manual`, [the API](../api.md#the-aggregate-read)).

Two decisions since 0159 were weighed and change nothing.
[0186](0186-units-and-conversions-are-core-records-and-a-value-converts-through-the-repositorys-graph.md)
moved units into core because `quantity` is a core datatype; nothing in core
needs a source kind.
[0166](0166-a-record-page-layout-is-a-record-in-a-seeded-console-package.md)
showed that a seeded kind's records stay writable by the owner, so 0159's
"a seeded package refuses a repository's own writes" holds for declarations
only, which is the half that matters here.

An app or a script finds a repository's source kind by its trait:
`filter.implements` set to `substrate.reamde.dev/core/source` on
`GET /api/v1/records`, or the kinds list, which names each kind's traits
([observations](../observations.md#finding-the-source-kind)).

### Consequences

- Good, because nothing migrates: source rows, observations, traces and the
  observed streams stay as stored.
- Good, because the owner keeps editing the `source` declaration, and the
  seeded admission unit stays at three packages.
- Bad, because a provider has no exact reference to the source or observed
  kinds, so it writes them only under a `*` grant; otherwise an adapter
  sample imported beside it does.
- Bad, because a script or an agent spells the source kind per repository,
  or finds it by its trait.
- Bad, because a verbatim install of the observations sample beside an import
  gives one repository two source kinds.

### Confirmation

`TestObservationsSampleKindNamesAreFrozen` (`kinds`) holds the kind names,
and `TestRehomeAuthorityReachesEveryString` (`internal/vocabulary`) holds
that an import rewrites the authority inside a function's code. That no
provider needs to write a source is held by review only.

## More Information

Reopen when a provider must write observed data itself; when a mechanism
exists that can move a sample copy's kind, which is 0159's own trigger; or
when scripts or agents working across many repositories make the
per-repository spelling a real cost.
