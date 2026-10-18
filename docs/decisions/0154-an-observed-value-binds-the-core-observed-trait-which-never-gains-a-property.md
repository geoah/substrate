---
status: accepted
date: 2026-10-18
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
period (a trace). From the first version both list the time (`at`, `endsAt`
and one `utcOffset`), what and who (`metric`, `source`, `about`, `channel`), the
value (`form`, `value` or `unit` with `values`, `code` or `codes`,
`statistic`, `period`, and the managed `measure` its unit resolved to), the
reading rules (`hold`, `resetRule`, `resetAt`, `delta`, `circular`,
`wrapAt`), the delivery (`origin`, a trace's `origins`) and `revision`;
`single` adds `sourceValue`, `sourceUnit` and `absentReason` for a value the
substrate could not normalize or that is absent, and `packed` adds `every`,
`offsets` and the managed `stats`. Health writes no `about`, `code` or
counter field and pays nothing for declaring them. The host checks what a
trait cannot say: `value` or `code` and never both (an amount may carry
its category in `code` beside `value`), equal lengths of
`offsets` and `values`, a form from the four
([0156](0156-the-forms-are-gauge-amount-counter-and-summary-and-live-on-the-value.md)),
and a `revision` that never falls. The loader also holds each binding kind's
property shapes, because the rules and the index read them: `offsets`,
`values` and `codes` are lists and every other `observed` property is one
unkeyed value.

Core reads no property a trait does not contract. What the engine and the
console read from the records a value points at is contracted by three
traits beside `observed`: `metric` (`measures`, `unit`, `bucketPlace`,
`rollups` and what a reader shows) on the record a value's `metric` names,
which an observed kind's `metric` reference must pin; `source`
(`priorities`) on the record its `source` names; and `result`, which a lab
result kind binds to opt into the one-shape rule over `codedValue`,
`ratio` and `text`. Without `result` those names are ordinary properties.

A trace carries one `utcOffset`, which every point takes, and none means
unknown. Its ends cannot say when, or how often, the offset changed inside the
period, so the index never guesses a transition: a writer splits a trace
where its points' offset changes.

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

`TestObservedTraitContractIsPinned` (internal/vocabulary) pins both variants'
property names and datatypes and fails on any addition, removal or retype.
`TestObservedBindingHoldsListAndSingleShapes` holds a binding kind's list and
single shapes. `TestObservedCompanionTraitContractsArePinned` pins `metric`,
`source`, `result` and `revised` the same way, and
`TestResultBindingHoldsItsObjectShapes` holds `result`'s object fields.
`mise run kinds:check` refuses a changed
core declaration that ships without a version bump.

## More information

The second-trait rule answers the review's twelfth finding
([the plan, section 13](../plans/observations.md)), and
the implementation
settled it over versioned variants. The full trait is in
[under the hood](../plans/observations/proposal/04-under-the-hood.md).
Revisit if the loader ever gains optional trait properties.
