---
status: accepted
date: 2026-09-26
decision-makers: George Antoniadis (via the #582 agent session)
---

# 0106. The apply door holds back a mapping whose provider is absent, on request

## Context and Problem Statement

A `recordmapping` resolves its `from` kind at admission
([0049](0049-the-owner-of-a-mappings-target-declares-it.md)), and
`POST /api/v1/vocabulary/apply` admits a batch whole or not at all. So a
consumer's package holding mappings onto two providers cannot be applied while
one provider is absent: the one waiting mapping refuses every other document
with it ([#582](https://github.com/geoah/substrate/issues/582), mneme-v6
T-013). The catalog's install and import doors already drop such a mapping
([suggested mappings](../bundles.md#suggested-mappings)); a hand apply has no
equivalent, so the consumer prunes its tree to match whatever is installed.

## Considered Options

- A request key on the apply door, `holdWaitingMappings`, that holds only
  `waiting` mappings (the catalog's rule for an absent provider)
- The same key holding `waiting` and `blocked` mappings (the catalog doors'
  full rule)
- A marker on the mapping document (`when: provider-installed`)
- Hold waiting mappings by default

## Decision Outcome

Chosen: the request key, holding only `waiting` mappings. With
`"holdWaitingMappings": true`, the door takes out each suggested mapping (a mapping onto the declaring package's own kind
from another package's kind) whose source kind neither the repository nor the
batch declares, prunes its `installs:` entry, applies the rest, and answers
each one in `heldMappings` as a `SuggestedMapping` in state `waiting`, the wire
type the catalog already reports. `POST /api/v1/vocabulary/plan` takes the same
key. `substratectl apply --hold-waiting-mappings` sends it. The pruning is the
catalog's own (`vocabulary.WithoutMappings`), in `internal/api`, so the engine
and its admission rules do not change.

A document marker was rejected because a declaration's key set is closed
([0020](0020-dialect-keys-are-reserved-not-tolerated.md)): a new key is an
upgrade of every binary that reads the closure, and the marker would be stored
on every mapping that lands, where it means nothing. Holding by default was
rejected because an apply is exact where an import is best-effort: a caller
that did not ask would get a 200 for a batch that dropped a document it sent.

Only an ABSENT source is held. The catalog doors also drop a `blocked` mapping,
one whose source kind is present but does not fit it. That rule was rejected
here: a catalog closure is shipped and tested against a provider version, so a
misfit there means the provider is older than the sample needs. A hand-written
batch has no such reference, so the door cannot tell a mistake in the mapping
from an outgrown provider, and the loader's refusal names the problem either
way. A missing source is the one case that needs no judgement.

### Consequences

- Good, because a consumer's tree may hold mappings onto every provider it
  supports and apply as-is on a repository that has only some of them.
- Good, because a held mapping lands on the first apply of the same documents
  after its provider is installed, with no state kept in between.
- Bad, because a sample's files applied by hand onto an older provider still
  fail the whole batch on a `blocked` mapping, where `substratectl import`
  drops it. The caller upgrades the provider or removes the mapping.
- Bad, because nothing is stored for a held mapping: the repository cannot
  list what is waiting, only the response of the apply that held it can.
- Bad, because the check is a read before the apply's transaction. A provider
  uninstalled between the two is refused by the loader as before, and one
  installed between them is held and lands on the next apply.

### Confirmation

`TestSchemaApplyHoldsWaitingMappings` (`internal/api`) holds the pruning, the
`installs:` entry, the report, the plan, a batch of nothing but waiting
mappings, and the default; `TestWaitingMappingsSkipASourceTheBatchDeclares`
(`internal/vocabulary`) holds that a source the batch declares is not a wait,
and `TestWaitingMappingsSkipAMappingWithNoID` that an id-less mapping is left
for the loader to refuse; `TestApplyHoldWaitingMappings` (`cmd/substratectl/commands`) holds the flag and
the printed line.

## More Information

Closes [#582](https://github.com/geoah/substrate/issues/582). The door is
described in [vocabulary.md](../vocabulary.md#how-the-vocabulary-reaches-a-repository).
