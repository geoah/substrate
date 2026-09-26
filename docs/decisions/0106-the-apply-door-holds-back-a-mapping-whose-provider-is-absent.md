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

- A request key on the apply door, `holdWaitingMappings`, with the catalog's
  semantics
- A marker on the mapping document (`when: provider-installed`)
- Hold waiting mappings by default

## Decision Outcome

Chosen: the request key. With `"holdWaitingMappings": true`, the door takes
out each suggested mapping (a mapping onto the declaring package's own kind
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
on a declaration that was never stored. Holding by default was rejected because
an apply is exact where an import is best-effort: a caller that did not ask
would get a 200 for a batch that dropped a document it sent.

Only an ABSENT source is held. A mapping whose source kind is present and does
not fit it still refuses the batch, because that is a mistake in the document,
not a provider that has not arrived yet.

### Consequences

- Good, because a consumer's tree may hold mappings onto every provider it
  supports and apply as-is on a repository that has only some of them.
- Good, because a held mapping lands on the first apply of the same documents
  after its provider is installed, with no state kept in between.
- Bad, because nothing is stored for a held mapping: the repository cannot
  list what is waiting, only the response of the apply that held it can.
- Bad, because the check is a read before the apply's transaction. A provider
  uninstalled between the two is refused by the loader as before, and one
  installed between them is held and lands on the next apply.

### Confirmation

`TestSchemaApplyHoldsWaitingMappings` (`internal/api`) holds the pruning, the
`installs:` entry, the report, the plan and the default; `TestWaitingMappingsSkipASourceTheBatchDeclares`
(`internal/vocabulary`) holds that a source the batch declares is not a wait;
`TestApplyHoldWaitingMappings` (`cmd/substratectl/commands`) holds the flag and
the printed line.

## More Information

Closes [#582](https://github.com/geoah/substrate/issues/582). The door is
described in [vocabulary.md](../vocabulary.md#how-the-vocabulary-reaches-a-repository).
