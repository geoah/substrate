---
status: superseded
superseded-by: 0044
date: 2026-08-31
decision-makers: George Antoniadis
---

# 0032. An owner pointer may be a reference, and a mirror's `account` is one

## Context and Problem Statement

Deleting a connected account does not collect the records it synced.
`cascadeOwned` (`internal/engine/gc.go`) reads the `edges` table and skips
every row whose `rel` is not declared `ownerRef`, so the sweep walks edges and
nothing else. Sixteen edges in `kinds/` declare `ownerRef` and none of them is
on a provider mirror kind: those carry `account` as a `type: string`, which
nothing joins on and nothing follows. Disconnecting a Google account leaves
its contacts, threads, messages, calendars and events behind. Record 0021, on
the unmerged branch of
[#181](https://github.com/geoah/substrate/pull/181), records that as its open
cost and names it as the trigger to revisit. Nothing in the tree says when a
pointer should be an edge and when it should be a reference either, so the
mirrors kept the string by default rather than by choice.

## Considered Options

- Teach the cascade to follow an `ownerRef` reference, and retype the mirrors'
  `account` to one
- Retype the mirrors' `account` to an `ownerRef` EDGE, so one mechanism keeps
  the whole cascade
- Leave the cascade alone and sweep a mirror's rows by filtering on the
  `account` string when a connection is removed

## Decision Outcome

Chosen: teach the cascade to follow an `ownerRef` reference, and retype
`account` on all twenty provider mirror kinds to a reference pinned at the
bundle's own `account` kind, `required: true` and `ownerRef: true`.

The edge option loses what the string was good at. A mirror's `account` is
written by the same connector `put` that writes the row's provider fields,
filtered by every sync's mark-and-sweep (`{"account": {"eq": aid}}`), and read
back as a value; an edge is none of those, so retyping to an edge would have
rewritten six bundles' sync bodies to write a record plus a link and to filter
on a join. The per-connection sweep option puts a fourth deletion mechanism
beside tombstone, purge and cascade, and it only ever works for the kinds
somebody remembered to write it for.

### The rule this settles

An edge and a reference are both a named, directed pointer at another record,
and the choice is not stylistic. Neither `many`, `required`, `inverse`,
`incoming` nor `ownerRef` decides it: a reference takes `repeated: true`, both
carry an `inverse` label, `incoming` answers over both (`substrate.ViaEdge`
and `substrate.ViaReference`), and after this record both take `ownerRef`.

**It is an EDGE when the LINK is a thing.** Four capabilities live only on the
`edges` row and none of them has a reference equivalent:

- the link carries its own `properties`, which
  [#243](https://github.com/geoah/substrate/pull/243) makes declarable;
- a `recordmapping` names it as the subject edge, which `checkMapping`
  (`internal/vocabulary/mapping.go`) requires to be a declared EDGE;
- `link`/`unlink` write and clear it without writing the record, which is how
  a bundle uninstall clears its bindings;
- a merge REPOINTS it (`moveEdges`), while reference values are left alone.

**It is a REFERENCE when the pointer is part of what the record IS.** It lands
in `data.properties` beside the rest, so it travels in the record delta with
its value, round-trips through `get -o yaml | apply -f`, filters as a scalar
(`condReference`: eq, in, exists) over `records_props_idx`, and hops in a
`displayTemplate` exactly as an edge does.

**`to: any` is the tell, and pinning is the price.** An unpinned pointer that
is really a pointer wants to be a reference, but `kind: any` costs it the two
things the registry can only do with a pin: `incomingArms` cannot enumerate an
unpinned reference without reading every row of every kind, and the cascade
cannot list the kinds to probe. So `ownerRef` is refused on a reference that
is unpinned, repeated, keyed, or declared inside an object, and a pointer that
genuinely cannot name one target kind stays an edge until it can.

That last clause is why the three shared kinds are NOT converted here.
`calendar.calendar`, `messaging.conversation` and `messaging.emailthread`
carry `account` as a `to: any` `ownerRef` edge because six bundles each
declare their own `account` kind (`google.bundles…/account`,
`linear.bundles…/account`, and four more) and a reference pin names exactly
one. Pinning them needs either one shared account kind or a pin that names a
TRAIT, neither of which exists; until one does, the edge is the honest
spelling and it keeps its cascade unchanged.

### Consequences

- Good, because disconnecting one account now collects exactly that account's
  synced rows, on every one of the six provider bundles at once, through the
  sweep that already existed rather than a new verb.
- Good, because a mirror's `account` is checked: the referent kind must exist
  and match the pin, where the string admitted any text at all.
- Good, because the connector's own spellings survive. A pinned reference
  takes the bare account id a sync writes and a filter passes, and stores the
  canonical `<kind>/<id>` path.
- Bad, because retyping a property is a NARROWING. `classifyNarrowings` counts
  live rows carrying `account` and the boot upgrade skips the whole authority
  while any exist, logging the count; a repository that has synced anything
  keeps its stored declarations until those rows are deleted. Pre-v1 the
  answer is `mise run dev:wipe`, and there is no migration.
- Bad, because a body that reads the stored value back gets a path where it
  had an id. Two do (`contactsidmigration` and Linear's `taskprojection`, both
  composing a derived record id) and both now strip the pin's prefix.
- Bad, because `account` still has two spellings across the tree: a reference
  on the twenty mirrors, an edge on the three shared kinds.
- Bad, because the reference half of the cascade costs one indexed query per
  (kind, ownerRef reference) pair pinned at the collected kind, where the edge
  half is one join.

### Confirmation

`TestOwnerRefReferenceCascade` (`internal/engine/gc_reference_db_test.go`)
collects a record through an `ownerRef` reference and holds the three
non-collections beside it: another account's row, a reference the declaration
did not mark `ownerRef`, and a string carrying the same value. It rebuilds the
repository from the changelog and compares the fold, because the cascade's
tombstones are fold effects. `TestOwnerRefRefusesTheShapesItCannotFollow`
(`internal/vocabulary/ownerref_test.go`) holds the four refused shapes.
`TestFinalizersAndOwnerRefGC` still holds the edge half.

## More Information

This supersedes the mirror half of record 0021, which said an `account`
PROPERTY is a string carrying the account record's id that the engine never
follows, and it answers the revisit trigger 0021 names. 0021's shared-kind
half stands. 0021 is on the unmerged branch of
[#181](https://github.com/geoah/substrate/pull/181) and is not in `main`, so
nothing here could mark it superseded; whichever of the two lands second owes
the other that edit.

Record 0027, on [#243](https://github.com/geoah/substrate/pull/243)'s branch,
is unchanged and agreed with: an edge is refused at write when its target is
absent, survives a tombstone, and dies with a purge. A reference keeps its own
posture, which 0027 does not cover: it is admitted against a target that does
not exist yet, because it is a pointer and not a link.

Thirty of the sixty edges in `kinds/` are reference candidates under this rule
and none of them is converted here; that is a second change, enumerated in
this record's pull request. The other thirty stay edges for one of three
stated reasons: fifteen point at `to: any`, five are a mapping's subject edge,
and ten are `many: true` where the link is where per-link data belongs.
