---
status: accepted
date: 2026-09-23
decision-makers: George Antoniadis
---

# 0098. A declaration names a kind or trait in full

## Context and Problem Statement

A `kind:` pin, a `trait:` pin, a `traits:` binding and a function's
`writes`/`reads.kinds` allowlist could name their target by its bare word
(`kind: person`), and the loader resolved the word: the declaring package
first, then the one package anywhere that declared it. Record
[0042](0042-every-kind-carries-an-authority.md) kept that shorthand, record
[0034](0034-a-reference-may-pin-a-trait-not-only-a-kind.md) gave a `trait:`
pin the same one, and record
[0081](0081-the-window-read-computes-occurrences-and-recurring-is-core.md)
added a tier so a bare trait name core declares resolves to core's.

The search is what breaks. A repository that holds `people/person` under two
authorities, its own imported copy beside the shipped one
([#614](https://github.com/geoah/substrate/issues/614)), refuses every
imported sample that pins `person` bare as ambiguous, and nothing short of
editing the shipped closure can say which was meant. A kind's name is its
authority, package and word, and a declaration that writes one of the three
is asking the loader to guess the other two.

## Considered Options

- Keep the shorthand and add tiers: the declaring package, then the packages
  under the declaring authority, then everywhere.
- Keep the shorthand within the declaring authority and refuse it across
  authorities.
- No shorthand: every declaration writes the full identity, and a bare word
  is refused naming the full spellings the repository declares under it.

## Decision Outcome

Chosen: no shorthand. A `kind:` pin, a `trait:` pin, a `traits:` entry and an
allowlist entry are each `<authority>/<package>/<name>`, the declaring
package's own kinds included, and a bare word is refused at admission with
`"person" is a bare name, and a kind pin is named in full`, followed by every
full spelling the repository declares under that word. A `traits:` entry
carries its variant and column remap after the identity
(`substrate.reamde.dev/core/temporal(point: dueAt)`). The tiers lost because
each one is a guess with a different answer in different repositories: the
same shipped closure resolved `person` to one kind on a fresh substrate and
to nothing on a substrate holding a second copy. A spelling that means one
thing everywhere is the only one a shipped closure can carry.

The read-time lookups keep their bare form. A records filter, a trigger's
selector and `substratectl get task` name nothing in a declaration, and a
bare word there is a caller's convenience that already refuses when it is
ambiguous. A bare `kind:` on a record envelope written through the API is the
same convenience. Neither is part of this decision.

This amends 0034, 0042 and 0081: the sentences in each that describe a bare
name resolving no longer hold, and the rest of each record stands.

### Consequences

- Good, because a shipped closure means the same kinds in every repository,
  whatever else the repository holds.
- Good, because the refusal names the spellings to copy, so a bare word is a
  one-edit fix rather than a search.
- Bad, because every declaration in `kinds/` and `samples/` was rewritten and
  every changed package's version bumped, so every repository takes a boot
  upgrade of core and is offered one for each provider and sample it holds.
- Bad, because a stored declaration written bare before this change no
  longer admits. A repository created before this change holds core's own
  declarations bare (`kind: recordmerge`, `traits: [temporal(point)]`), and
  the open reads the stored closure before the shipped upgrade runs
  (`loadStoredVocabulary`, then `upgradeShippedVocabulary`), so the seeded
  bucket fails to build and the repository refuses to open under this
  binary. A provider's or sample's stored copy would park instead. Nothing
  rewrites a stored declaration in place yet: a repository from before this
  change is recreated, or a boot-time rewrite of stored bare names as
  ordinary record writes is built first.
- Bad, because a declaration is longer to write, and a repository's own
  kinds pointing at each other spell their authority every time.

### Confirmation

`internal/vocabulary`'s `TestReferencePinResolutionRules`,
`TestCapabilityResolutionRules` and `TestATraitBindingIsSpelledInFull` hold
the refusal and its text. `go test ./kinds/` loads the shipped trees, so a
bare word landing there fails the build, and `mise run kinds:check` holds the
version bumps.

## More Information

Amends [0034](0034-a-reference-may-pin-a-trait-not-only-a-kind.md),
[0042](0042-every-kind-carries-an-authority.md) and
[0081](0081-the-window-read-computes-occurrences-and-recurring-is-core.md).
Reopen if the read-time conveniences are to go the same way, which is a
change to the API and the CLI rather than to the loader.
