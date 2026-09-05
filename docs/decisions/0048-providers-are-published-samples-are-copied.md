---
status: accepted
date: 2026-09-02
decision-makers: George Antoniadis
---

# 0048. Providers are published packages; samples are copied and owned

## Context and Problem Statement

The catalog holds 23 closures in one tier, and whether the repository's token
may edit one follows from how its authority is spelled rather than from what
the closure is: a bundle on a bare org-domain authority (`people`, `tasks`, ten
more) is rebuilt as `source: builtin` even when installed (`BuildAuthorities`,
`internal/vocabulary/load.go`, keyed by `ValidVocabularyAuthority`), while
`google` and the worked examples land `source: installed` and stay writable.
Both halves have it backwards: a user who imports `tasks` cannot add a property
to vocabulary meant as their starting point, and a user who installs `google`
can rewrite the mirror kinds its sync writes. Record 0047 removes that rule
along with the authority-shape test that decided it, so under the package
grammar every shipped closure installs as `source: installed` and the seed is
the only vocabulary a token may not write. The work is
[the plan](../plans/providers-and-samples.md).

## Considered Options

- Keep one tier, with immutability following the authority's shape
- Two tiers: a provider is published, a sample is copied under the
  repository's own authority

Three questions inside the sample tier were on the table with it: whether the
worked examples (`llm`, `notes`, `web`, `pebble`) and `firecrawl` are providers
or samples; whether each sample is authored under its own authority or all of
them under one placeholder; and whether a repository that already installed the
shared vocabulary is rehomed under its own authority.

## Decision Outcome

Chosen: two tiers, provider and sample.

**A provider is a package a publisher owns.** The shipped six are
`providers.substrate.reamde.dev/google`, `/github`, `/linear`, `/notion`,
`/whoop` and `/beeper` (record 0047). A provider installs as a copy, and
afterwards only the substrate path (the seed, an install, an upgrade) may write
its declarations. The stored `source` set gains `published` beside `builtin`
and `installed`; a provider install writes it on the package row, and
`authorizeDeclarationWrite` (`internal/engine/seed.go`), which reads the
package's `source` since 0047, refuses a non-substrate actor with the same
`403` it gives `builtin`. Records of a provider's kinds stay writable under the
existing tier rules. The publisher ships each change with a version bump, and
the existing upgrade preview (`PlanBundleUpgrade`) offers it.

**A sample is a package the user copies.** It is authored under
`samples.substrate.reamde.dev/<package>`, and importing it rewrites the
authority alone: `samples.substrate.reamde.dev/tasks/task` lands as
`ada.example.com/tasks/task`, the repository's authority from
[0046](0046-a-repository-owns-one-authority-chosen-at-registration.md) and the
sample's package from 0047. What lands is the user's: `source: installed`,
writable through the API, never offered an upgrade, never touched by the boot
upgrade. Admission refuses a document that still spells the placeholder.

Because the package is the version, quarantine and ownership unit and a bundle
is named for its package (0047), two samples never compete for a kind name:
`people` and `tasks` are two packages under the repository's one authority. The
earlier worry about seventeen samples sharing a single authority does not
arise.

The three questions answered: the worked examples and `firecrawl` are samples,
since each exists to be read and changed; every sample is authored under the
one placeholder authority, since the package segment already separates them;
and nothing rehomes vocabulary a repository already installed.

The word **bundle** stays for the mechanism: a provider install and a sample
import both land as a `bundle` record with the lifecycle verbs it already has.
Provider and sample are the catalog tiers, and they replace the two curated
facets keyed by bundle id (`integrationFacets`, `exampleFacets` in
`internal/catalog/catalog.go`) together with the `vocabulary` flag derived from
the authority's shape.

This **amends** [0015](0015-unproven-kinds-stay-out-of-the-stable-set.md),
which kept the demos shipped and said an `example` bundle's declarations may
change or leave without an upgrade path. A sample is what "outside the stable
set" meant, enforced by the import rather than stated in a comment, and the
stable set shrinks to `substrate.reamde.dev/core` plus the six providers:
everything else in the shipped tree is a sample. 0015's Confirmation fences go
with it, because phase 1 removes the `exampleFacets` note and the example
comments in the four `bundle.yaml` files that carry it (`llm`, `notes`, `web`,
`pebble`). The rest of 0015 stands (`media` and
`memory` stay out of the tree, `recordpatchpolicy`'s judge half keeps its
marker), so it is not superseded.

### Consequences

- Good, because a user who imports `tasks` may add a property the next day.
  Today that package is immutable to the user who installed it.
- Good, because a provider's mirror kinds cannot be edited under the sync that
  writes them, so a publisher can ship a change and its migration.
- Good, because the tier is read from the package's stored `source` rather than
  inferred from a DNS name, so nothing depends on how an authority is spelled.
- Bad, because installing Google alone yields mirrors and nothing else. Until
  the user imports the samples and the mappings onto them
  ([0049](0049-the-owner-of-a-mappings-target-declares-it.md)), a sync produces
  `gmailmessage` and `event` rows and no `emailmessage`, `calendarevent` or
  `person`.
- Bad, because re-importing a sample replaces the package rather than merging
  into it. A batch carrying a bundle document lists its package for wholesale
  replacement (`internal/engine/vocabularywrite.go`): existing documents of a
  replaced package are skipped, so the replacement starts from the incoming set
  alone, the projection prunes what the set omits, and
  `declarationReplacement` nulls every authored property the new declaration
  lacks. A kind or property the user added is therefore dropped by the
  re-import, or the re-import is refused by the narrowing guard while live
  records still carry it. The changelog is the only record of what they had.
- Bad, because nothing rehomes what a repository already installed. 0047 wipes
  every store written before the package grammar, so the case that survives is
  a repository created after it and before import lands: its packages stay
  under `samples.substrate.reamde.dev`, and only a fresh import puts one under
  its own authority.
- Bad, because a provider upgrade is a person's click in the console. The boot
  upgrade keeps touching `core` alone, so a repository whose owner never clicks
  runs the provider declarations it installed with, indefinitely.
- Bad, because a sample has no upgrade path at all, by construction. A fix to
  `samples/tasks` never reaches a repository that imported it, and
  `kinds:check`, which 0047 has diffing both trees, stops reading samples,
  whose versions decide nothing.

### Confirmation

Nothing holds this yet: the record is written before the code, as phase 0 of
the plan. The gates the phases name are `kinds_test.go`, extended to install
providers and samples on the seed without a database; a refusal test on
`authorizeDeclarationWrite` asserting the `403` on a `published` package; and
the wire golden for the `Tier` field that replaces the facets. That golden does
not reach the catalog today: `wireTypes` (`internal/substrate/wire_test.go`)
lists the record envelope, the change, the occurrence shapes and the
operational list, and `catalog.Bundle` is in none of them, so holding `Tier`
across Go and `types.ts` means adding the catalog's wire shape to `wireTypes`
first. Until those land this is held by review only.

## More Information

The plan is [docs/plans/providers-and-samples.md](../plans/providers-and-samples.md),
whose phase 0 names this record. Amends
[0015](0015-unproven-kinds-stay-out-of-the-stable-set.md); rests on
[0046](0046-a-repository-owns-one-authority-chosen-at-registration.md) for the
authority a sample is rewritten onto and on record 0047 for the package segment
that groups it; the mapping half is
[0049](0049-the-owner-of-a-mappings-target-declares-it.md).

Reopen when a provider installs from outside the binary, which needs a closure
signed by the authority it names
([#339](https://github.com/geoah/substrate/issues/339)), or when a sample needs
a real upgrade path, which means a copy that tracks its origin and is a
different decision from this one.
