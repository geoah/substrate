---
status: proposed
date: 2026-10-11
decision-makers: George Antoniadis
---

# 0154. An observed value binds the core `observed` trait, which never gains a property

## Context and problem statement

The fold has to recognise an observed value without knowing the kind that
holds it, because those kinds (`observation`, `trace`, a lab result) live in
samples a user imports under their own authority
([0048](0048-providers-are-published-samples-are-copied.md)). A kind that
binds a trait must declare every property the trait lists, and a trait has no
optional properties (`internal/vocabulary/load.go` refuses with "trait ...
requires property ..."). A property added to a core trait after it ships
therefore refuses every repository's imported kinds that bind it, at the next
boot ([the plan, sections 1 and 4](../plans/observations.md)).

## Considered options

- A core trait, `observed`, complete in its first version; a later need lands
  as a second trait bound beside it
- The same trait, grown by adding versioned variants
- Optional trait properties, so a trait can grow
- No trait: the engine recognises the `observations` sample's kinds by
  reference

## Decision outcome

Chosen: the core `observed` trait, complete in its first version, and a
second trait for anything later.

`substrate.reamde.dev/core/observed` has two variants that describe layout
only: `single`, one value per record, and `packed`, many values over a fixed
period (a trace). From the first version both list the time (`at`, `endsAt`,
the UTC offsets), what and who (`metric`, `source`, `about`, `channel`), the
value (`form`, `value` or `unit` with `values`, `code` or `codes`,
`statistic`, `period`), the reading rules (`hold`, `resetRule`, `resetAt`,
`delta`, `circular`, `wrapAt`) and `revision`; `packed` adds `every`,
`offsets` and the managed `stats`. Health writes no `about`, `code` or
counter field and pays nothing for declaring them. The host checks what a
trait cannot say: exactly one of `value` and `code`, equal lengths of
`offsets` and `values`, a form from the four
([0156](0156-the-forms-are-gauge-amount-counter-and-summary-and-live-on-the-value.md)),
and a `revision` that never falls.

A later need never adds a property to `observed`. It lands as a second trait
that the kinds needing it bind beside `observed`. Versioned variants are a
property list under another name: a kind bound to the old variant still
lacks the new properties. Optional properties would change how every trait
loads, and would let two kinds that bind one trait disagree on what they
carry. Recognising the sample's kinds by reference fails the reasoning of
[0077](0077-the-llm-kinds-live-in-their-own-seeded-package.md): the engine
would address kinds a repository may not hold, and an import rehomes them
under each user's authority, so their reference differs per repository.

### Consequences

- Good, because no core upgrade can refuse a user's imported `observation` or
  `trace`.
- Good, because the fold indexes any kind that binds the trait, so a domain
  sample's own kind (a lab result) is indexed with no engine change.
- Bad, because the first contract carries properties health never writes, and
  every kind that binds it declares them.
- Bad, because a property found missing later is a second trait, and a reader
  then checks two traits to see one value whole.
- Bad, because a mistake in the first contract cannot be removed, only left
  unused.

### Confirmation

`TestObservedTraitContractIsPinned` (internal/vocabulary, to be written in
work package 2) pins both variants' property names and datatypes and fails on
any addition, removal or retype. `mise run kinds:check` refuses a changed
core declaration that ships without a version bump.

## More information

The second-trait rule answers the review's twelfth finding
([the plan, section 13](../plans/observations.md)), and
[the implementation brief](../plans/observations/implementation/README.md)
settled it over versioned variants. The full trait is in
[under the hood](../plans/observations/proposal/04-under-the-hood.md).
Revisit if the loader ever gains optional trait properties.
