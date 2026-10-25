---
status: accepted
date: 2026-10-25
decision-makers: George Antoniadis
---

# 0191. A shipped record is held by any hand but the one that ships it

## Context and Problem Statement

[0166](0166-a-record-page-layout-is-a-record-in-a-seeded-console-package.md)
let an install, import or upgrade write a shipped data record only where the
record's changelog held no entry under a human door (`api`, `console`,
`substratectl`), and [0186](0186-units-and-conversions-are-core-records-and-a-value-converts-through-the-repositorys-graph.md)
gave the unit seed the same rule. Those three names are not the whole of the
owner: a request may attribute itself to any unreserved name
(`X-Substrate-Actor: my-import-script`) and is still the owner's write, and an
agent, a function or another bundle writes on the owner's behalf under its own
reserved hand. Every one of those edits was overwritten by the next re-import
(issue #950). The provider seed had a second gap: it read only the stored row,
so a deleted provider whose tombstone the sweep had collected was written
again at the next open.

## Considered Options

- Widen the list of owner names: the three doors plus `agent:` and
  `function:` prefixes.
- Invert the rule: a shipped record is the shipper's to replace only while
  every changelog entry for it is the shipper's, the core seed's or the
  engine's.

## Decision Outcome

Chosen: invert the rule. Unreserved names are an open set, so no list of
owner names can be complete; the shipping hands are a closed set of reserved
names (`substrate.ReservedActor`), which no request can claim. The engine
answers it with `anotherHandWrote` over the record's whole changelog, purged
records included, and the closure install, the unit seed and the provider
seed all ask it. This amends 0166's upgrade clause, which also let a record
another bundle wrote take the newer shipped one, and 0186's seeding sentence.

### Consequences

- Good, because an edit survives whoever made it, and a deletion survives the
  sweep that collects its tombstone.
- Bad, because a record another bundle or the engine's dispatch hands touched
  for a reason that was not an edit (a function stamping a field) stops
  receiving shipped corrections, the same as one the owner edited.
- Bad, because the owner is still not told a newer record shipped; issue
  #1047 holds that.

### Confirmation

`TestReimportKeepsAnEditedMetricRecord` (internal/catalog) holds an edit
under a caller-named actor, an agent and another bundle against a re-import;
`TestSeededProviderDeleteIsNotResurrected` (internal/engine) holds a deleted
provider through a sweep and a reopen.
