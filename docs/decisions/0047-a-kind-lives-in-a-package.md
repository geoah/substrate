---
status: accepted
date: 2026-09-02
decision-makers: George Antoniadis
---

# 0047. A kind lives in a package, and a package lives in an authority

## Context and Problem Statement

A kind reference was `{authority}/{name}`, so the only way to group kinds was
to give each group its own authority. The shipped vocabulary did exactly that,
with 25 subdomains of one placeholder (`tasks.substrate.reamde.dev`,
`google.bundles.substrate.reamde.dev`): grouping, ownership, versioning and the
DNS name were one knob. A repository owns exactly one authority
([0046](0046-a-repository-owns-one-authority-chosen-at-registration.md)), so it
had no way to group its own kinds at all — every sample it imported would land
flat under `ada.example.com/`, and two samples could never share a kind name.
[#194](https://github.com/geoah/substrate/issues/194) asked whether an authority
may carry path segments instead; the answer was no
([0014](0014-authorities-widen-only-outside-the-id-alphabet.md)). This is the
alternative, designed in [the plan](../plans/packages.md).

## Considered Options

- A required PACKAGE segment: `{authority}/{package}/{name}`
- Path segments inside the authority (`ada.example.com/tasks`), which 0014 ruled
  out
- A prefix convention inside the kind name (`tasks_task`), leaving the grammar
  alone
- Leave grouping to a subdomain per group, as before

## Decision Outcome

Chosen: the package segment. A kind reference is
`{authority}/{package}/{name}`, and the package is REQUIRED — a plain word, the
kind-name grammar (`[a-z][a-z0-9]*`) — everywhere a kind is spelled: a
declaration id, a reference pin, a stored reference value, a REST path, a
console route. A stored reference value is `{authority}/{package}/{kind}/{id}`.

**The split stays registry-free**, which is the property the whole grammar
rests on: the authority is the one segment carrying a dot, the next two are
words, and everything after them is the id, whose alphabet is untouched.
`SplitRecordPath` reads a changelog entry without loading the vocabulary that
wrote it, exactly as before.

**This supersedes one clause of
[0014](0014-authorities-widen-only-outside-the-id-alphabet.md)**: `/` now has a
third job. The rest of 0014 stands and is what made this cheap — the id
alphabet is still frozen and still never gains `%`, an authority still never
gains a raw `/`, and a kind name still never gains a dot. 0014's last
first-label keying, the GraphQL prefix, is discharged here rather than
inherited.

**The package is the unit.** Three things that were the authority's are the
package's:

- the VERSION unit: a kind may pin its own `data.version`, else it takes its
  package's, and the boot upgrade, `kinds:check` and the upgrade preview all
  key on it. Two packages under one authority upgrade independently, which is
  what lets `google` and `github` publish under one name;
- the QUARANTINE unit: a stored closure the loader refuses parks its package,
  not every package its authority publishes;
- the OWNERSHIP unit: `authorizeDeclarationWrite` reads the package row's
  `source`, so only the seeded `substrate.reamde.dev/core` is closed to a
  repository's own token.

A package is DECLARED, by a `substrate.reamde.dev/core/package` document whose
id is the package identity and which carries the authority, the package name
and the version. It replaces the authority document as a closure's header. The
`authority` document stays, stripped to what an authority is: a name, a
description and the packages published under it.

A **bundle** owns at least one package, and its `metadata.id` IS the package it
is named for. Whether a bundle may span two packages is left open: nothing in
the tree needs it and the loader does not forbid it. `requires:` names
packages.

**Actors gain a colon segment, never a slash**, because `<actor>/<name>` label
and annotation keys reserve the slash: `bundle:<authority>:<package>`,
`function:<authority>:<package>:<name>`,
`agent:<authority>:<package>:<name>`. This amends
[0025](0025-an-actor-carries-the-full-authority.md), whose derivation and
never-declared rule are unchanged.

**The GraphQL name of an installed kind is `<Package>_<Kind>`** (`Tasks_Task`),
and the authority's first label joins it only when two authorities install a
package of the same name, for every kind of both packages so a name never
depends on load order. That retires the last first-label keying 0014 reserved.

**A REST path routes by segment count, shifted by one**: three segments
(`/api/v1/{authority}/{package}/{kind}`) is a collection, four is a record.
This amends rule 2 of [0042](0042-every-kind-carries-an-authority.md) and the
grammar of [0033](0033-the-path-grammar-has-no-separators.md); the reserved
top-level words and the record sub-resources are untouched, and no separator
segment is introduced. The core verbs move with core, to
`/api/v1/substrate.reamde.dev/core/…`.

**The shipped tree becomes two authorities and a samples placeholder**:
`substrate.reamde.dev` publishing the `core` package (the seed),
`providers.substrate.reamde.dev` publishing `google`, `github`, `linear`,
`notion`, `whoop` and `beeper`, and `samples.substrate.reamde.dev` publishing
the seventeen sample packages, which live in a new top-level `samples/`
directory with its own embed. A repository may not claim an authority under
`substrate.reamde.dev` (0046), so nothing a user declares can be mistaken for
shipped vocabulary.

The alternatives all cost more than they saved. Path segments inside the
authority reopen 0014's character budget and make the record path ambiguous. A
name prefix leaves the grouping unenforced and unqueryable: nothing could
version, quarantine or own a "package" that is only a spelling habit. A
subdomain per group is the status quo, and it is what a repository with one
authority cannot do.

### Consequences

- Good, because a repository can group its own kinds without owning more DNS
  names, and two imported samples may share a kind name.
- Good, because ownership, versioning and quarantine are one small unit, so a
  provider's broken closure parks one package instead of six.
- Good, because 0014's last first-label keying is discharged: nothing derives
  an identifier from an authority's first label any more.
- Bad, because every stored kind string changes. There is no rung that
  translates the old grammar, so the store is wiped: the dialect gate refuses a
  store written before this (`maxVocabularyDialect` 3) and an older binary
  refuses one written after it.
- Bad, because every client URL changes at once, again — one breaking change
  (`refactor(vocabulary)!`), taken before v1 so no shipped client is owed a
  migration.
- Bad, because a bare kind name is one word further from its identity: an
  ambiguous `substratectl get task` now has more ways to be ambiguous, and the
  `--package` flag is what disambiguates it.
- Neutral: the shipped samples install as the repository's own
  (`source: installed`), so their GraphQL names are `People_Person` and
  `Tasks_Task` rather than the bare singulars. The vocabulary-bundle promotion
  to `builtin` is gone with the authority-shape rule that decided it.

### Confirmation

`TestSplitRecordPath` and `TestKindGrammarSeparatesAuthorityFromName`
(internal/vocabulary) pin the split and the three grammars;
`TestABundleIsThePackageItIsNamedFor`, `TestAPackageNameIsOneWord` and
`TestTwoAuthoritiesMayShareAPackageName` pin the bundle id, the package name
and the two-authority case; `TestGraphQLNamesDisambiguateBySharedPackageName`
and `TestOneGraphQLNameIsStillOneKind` pin the naming rule and its refusal;
`internal/api/grammar_test.go` pins the segment counts; `kinds_test.go` installs
every shipped package on the seed without a database. `mise run kinds:check`
diffs both shipped trees per package.

## More Information

Supersedes the "`/` never gains a third job" clause of
[0014](0014-authorities-widen-only-outside-the-id-alphabet.md) and amends
[0025](0025-an-actor-carries-the-full-authority.md),
[0033](0033-the-path-grammar-has-no-separators.md) and
[0042](0042-every-kind-carries-an-authority.md) as described above. Closes
[#194](https://github.com/geoah/substrate/issues/194). The design and what it
deliberately leaves open are in [the plan](../plans/packages.md). Reopen
trigger: kind identity moving to URLs, which would revisit whether an authority
is still one path segment.
