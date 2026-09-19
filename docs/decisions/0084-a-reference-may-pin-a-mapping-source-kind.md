---
status: accepted
date: 2026-09-15
decision-makers: George Antoniadis
---

# 0084. A reference may pin a mapping's source kind

## Context and Problem Statement

[0049](0049-the-owner-of-a-mappings-target-declares-it.md) carried forward a
rule from record 33: no reference anywhere may name a kind that is some
`recordmapping`'s `from`, so that resolution stays one hop deep.
`mappingInvariantProblems` (`internal/vocabulary/mapping.go`) enforced it over
every declared reference site in the registry. The rule is registry-wide and
the mapping set is the repository's, so **importing a sample retroactively
narrowed what a provider was allowed to declare**: the moment the `people`
sample's mappings named `github/user`, `google/emailaddress` and
`google/contact` as sources, a GitHub mirror could no longer declare
`issue.assignees` at `github/user[]`, a Google mirror could no longer declare
`event.attendees` at `google/emailaddress[]`, and a provider could not know at
publication time whether its own closure would admit. Measured live on
`f2213cb` while writing the Google and GitHub providers for Mneme v6
([#569](https://github.com/geoah/substrate/issues/569)).

## Considered Options

- Keep the refusal, and have a provider ship scalar id lists beside its raw
  objects (`assigneeLogins: string[]`) with a function resolving them after
  the fact — what Mneme v6 shipped around it
- Allow the pin only when the pointing kind is in the SAME PACKAGE as the
  source, keeping the refusal across packages
- Remove the refusal: a reference may pin any kind

## Decision Outcome

Chosen: the third. A pin AT a mapping source is satisfied by the value exactly
as written — `referenceAdmits` matches the kind before `subjectHop` is ever
reached (`internal/engine/references.go`) — so it takes no hop at all and costs
the one-hop rule nothing. What keeps resolution one hop deep is the bipartite
rule, which stands untouched: a mapping's `to` may never itself be any
mapping's `from`, so the hop can never land on a kind that hops again.

The same-package variant was rejected as a rule that reads as a rule and
behaves as an accident. It is the right GUIDANCE and the wrong admission
check: a package boundary is not where the harm is, the harm is a consumer
tying itself to one provider's view, and a check that fires on package
identity would still refuse legitimate cross-package modelling (two provider
bundles whose kinds genuinely reference each other) while admitting the bad
case inside one package. Guidance belongs in the docs, where it can say why.

**What is lost, plainly.** A consumer kind may now pin a mirror where it
should pin the subject — `task.assignee` at `github/user` rather than at
`person`. That record is tied to one provider, is left dangling when the
provider is uninstalled, and is NOT carried by a merge, which moves subject
pointers and not mirror pointers. The subject hop remains available and
remains the recommended pointer: a connector holding only a `github/user`
path writes it straight into a `person`-pinned slot and the hop resolves it.
So the rule moves from admission to
[projection.md](../projection.md#record-mappings) and
[data-model.md](../data-model.md), which is where a reader deciding what to
pin is actually looking.

### Consequences

- Good, because a provider bundle is admissible on its own terms. What it may
  declare no longer depends on which samples the repository happens to have
  imported, which is a property a publisher can reason about and the old rule
  denied them.
- Good, because a mirror kind can be written as the complete object its API
  describes — `issue.assignees`, `message.user`, `event.attendees` as
  references — instead of scalar id lists plus a resolver function.
- Good, because the refusal was registry-wide and therefore order-dependent in
  practice: the same two bundles admitted or did not depending on which was
  installed first.
- Bad, because nothing now stops a consumer kind pointing at a mirror. Merges
  will not carry it, an uninstall will dangle it, and the only thing saying so
  is documentation.
- Bad, because a `match` probe still reads the source record's own values, so
  a provider that models its emails as references cannot probe them onto
  `person.emails`. A mirror that wants to be matchable keeps the scalar beside
  the reference. This record does not change that, and
  [projection.md](../projection.md#record-mappings) now says it out loud.
- Neutral for locking: `lockEffectTargets` already planned a `subject|<to>`
  key for every mapping whose source an effect's reference names, whether or
  not that reference could hop. A pin at the source takes strictly fewer
  locks, never more, so no ordering changes.

### Confirmation

`TestMappingRules/a_reference_may_pin_a_mapping_source` and
`TestMappingRules/bipartite_still_holds_beside_a_pinned_source`
(`internal/vocabulary`) are the pair: the first loads a kind pinning a mapping
source at a top-level property, a repeated one and a nested reference FIELD,
which is every site the old refusal walked; the second holds the rule that
actually keeps resolution one hop deep.
`TestAReferenceMayPinAMappingSource` (`internal/engine`) is the write path:
the stored value names the mirror and not its subject, and the subject hop
beside it still answers.

## More Information

Amends [0049](0049-the-owner-of-a-mappings-target-declares-it.md), whose
"the bipartite rule stays" paragraph carried this refusal; the rest of 0049
stands, including the bipartite rule itself. Related:
[0044](0044-a-reference-is-the-only-link-between-records.md), which made a
reference the only link there is, and
[0034](0034-a-reference-may-pin-a-trait-not-only-a-kind.md), the other way a
pin is written.

Reopen if a consumer kind pinned at a mirror turns out to be a mistake people
make rather than one they could make: the answer then is a lint over the
repository's own closure, naming the subject to pin instead, and not a
refusal that a published bundle cannot predict.
