---
status: accepted
date: 2026-09-16
decision-makers: George Antoniadis
---

# 0088. A single-record read carries its inbound mapping-owned links

## Context and Problem Statement

[0096](0096-a-mapping-synthesises-its-subject-slot.md) moved the subject slot
onto the SOURCE kind, which is right and leaves the subject blind: a person is
pointed at by a Slack user, a GitHub user, a Google contact and three Google
email addresses, and NOTHING the person's read carries says any of them exist.
The one way to find them is `filter.referencing`
([api.md](../api.md#who-points-at-a-record-referencing)), a paged list the
console shows in its Graph tab and nowhere near the record itself. Owner's
review of Mneme v6, 2026-09-16, on a person: *"there's no link in the manifest
to point to the other mapped things … I should be able to see the related
records. That's definitely something that needs to happen."*

## Considered Options

- Leave it to `filter.referencing`, and have every client learn to run it.
- Put an inverse property on the target kind, filled by the write path.
- Carry the links on the single-record read, computed from the mapping set.
- The same, on list reads too, behind an `expand`-style opt-in.

## Decision Outcome

Chosen: the third. A single-record read carries `linkedFrom`, one entry per
source record whose mapping-owned subject slot points here, each naming the
source's record path and kind, its title, the slot it points from and the
mapping that owns that slot, ordered by kind then id.

It beat leaving it to `referencing` because the question a reader is asking —
"which mirrors converged on this subject" — is narrower than "who points at
this", is answered from the mapping set rather than from a client's guesses
about which properties to ask for, and has no honest client-side spelling: a
client would have to read every mapping, invert it, and issue one query per
source kind. It beat an inverse property because a stored inverse is a second
copy of the link that the fold would have to keep true through every merge,
split and delete — [0044](0044-a-reference-is-the-only-link-between-records.md)
says a reference is THE link — and because the mapping set already knows the
answer. It beat the list-read opt-in on cost: see below.

Five rules make it precise.

1. **It is derived at read time from the mapping set**, `Registry.MappingsTo`
   giving the (source kind, subject property) pairs and the refs index giving
   the rows. It reuses the reverse read's own target predicate
   (`referencingWhere`), so there is one place that says how a target is
   matched and the two answers cannot drift.

2. **Former ids count.** The predicate matches the canonical id and every id
   the record used to live under, exactly as `referencing` does, because a
   merge repoints nothing: a Slack user linked to the loser still names the
   loser's id, and both the winner's read and a read addressed by the former
   id list it.

3. **Absent is not empty.** A kind no recordmapping targets carries no
   `linkedFrom` key at all, so a client can tell "nothing maps onto this kind"
   from "nothing has linked yet" — and, because an empty result also serves no
   key, an absent key never has to be read as a claim.

4. **List reads never carry it**, and there is no opt-in. It is one query per
   record, so a page of 50 would be 50 of them; `filter.referencing` is
   already the bulk answer and it pages. This is the rule `propertyMeta`
   follows for the same reason, and the two are now the single-record read's
   two sidecars.

5. **A read gate that covers the record does not cover its links.** A link
   names a record of ANOTHER kind and carries its title, so the runner's and
   the agent loop's `reads` allowlist filters the list the same way it filters
   the record ([0080](0080-a-kind-grant-may-glob-and-a-glob-never-reaches-auth-material.md)):
   a body granted `person` alone learns nothing about the mirrors pointing at
   it. A bearer token is unaffected — it reads the whole repository already.

### Consequences

- Good, because the manifest finally says what a record is made of. Opening a
  person lists every provider record that converged on it, each a link, with
  no second request and no client-side inversion of the mapping set.
- Good, because there is one implementation of "what points at this target":
  the refs predicate the reverse read already stands on, former-id trail
  included.
- Good, because nothing is stored. No inverse column, no fold rule, no
  migration; a mapping installed or removed changes the answer on the next
  read.
- Bad, because a single-record read now costs one more indexed query on any
  kind a mapping targets. It is the same query shape `referencing` runs, on
  the same index, bounded below.
- Bad, because the list is CAPPED at 500 and truncates silently there. A
  mapping-owned subject slot is convergence-shaped — one mirror per source
  record that resolved to this subject — so the cap is not expected to bind,
  and `filter.referencing` is the complete, paged read past it. A second wire
  key saying "there is more" was not worth it for a case the mapping set has
  to be written strangely to produce; if one shows up, that is the reopening.
- Bad, because `title` on an entry is a second place a source record's title
  is served. It is derived storage, the same value the row itself serves, and
  without it every "Linked from" row would read as a bare id.

### Confirmation

`TestLinkedFromListsEverySourceThatMapsOntoTheRecord` and
`TestLinkedFromAnswersForAMergedFormerID` (`internal/engine`) hold the two
halves: two mappings onto one kind answering with both sources, an ordinary
reference at the same target NOT appearing, a tombstoned source dropping out,
a kind no mapping targets carrying no key, and a merged target answering for
its former id. `TestGetFiltersInboundLinksByTheAllowlist`
(`internal/runner`) holds rule 5: a body granted one kind sees the links of
that kind and no other. The wire golden (`internal/substrate/wire_test.go` and
`web/console/src/lib/api/wire.golden.test.ts`) holds the console's mirror of
the shape, and `PropertiesRail`'s vitest holds the "Linked from" section.

## More Information

Follows [0096](0096-a-mapping-synthesises-its-subject-slot.md), which created
the blindness, and answers the last of its "left undone" notes from the
reader's side. [projection.md](../projection.md#reading-the-links-back-linkedfrom)
is the page. Reopen if a repository declares a mapping whose fan-in is wide
enough for the cap to bind, which is the one case this record trades away.
