---
status: accepted
date: 2026-08-17
decision-makers: George Antoniadis (via the issue-202 agent session)
---

# 0028. A record's URL is its reference, and verbs live behind a reserved `-`

## Context and Problem Statement

The v1 path grammar puts records, verbs and sub-resources in one namespace and
resolves the shape by counting segments, so three defects are structural rather
than accidental
([#202](https://github.com/geoah/substrate/issues/202)). `POST
/api/v1/{plural}/{id}` resolves the collection, drops `{id}` and creates a
record under a server-assigned id: `addressed` (internal/api/rest.go) reads the
two-segment path by the dot rule, but `POST` was bound to
`createInCollection` at both depths, so a client that believes it is upserting
accumulates duplicates. `PUT /api/v1/{authority}/{plural}` is the mirror. A
verb segment also outranks a record id in chi's tree, which is why
`recordmerges` and `recordsplits` are shipped kinds whose collections are
unreachable, why a record whose id is `incoming` or `edges` cannot be
addressed, and why every verb added after v1 would silently remove an id from
an existing collection. The grammar has to settle before v1 freezes it, because
every one of these is a URL break.

## Considered Options

- Address by kind name, reserve `-` for verbs, dispatch every method through
  `addressed`
- Keep plurals and reserve a verb segment only
- Keep the shape and refuse the ambiguous method bindings (`405`) without
  reserving anything
- Move verbs to a query parameter (`?verb=enable`) instead of a segment
- Suffix verbs onto the id (`/{id}:enable`, the AIP spelling)

## Decision Outcome

Chosen: address by kind name, reserve `-`, and dispatch every method through
`addressed`. Three rules, one break.

**The collection segment is the kind's name.** `/api/v1/{authority}/{name}` for
a published kind, `/api/v1/{name}` for a repository-local one. The path after
`/api/v1/` is therefore byte-identical to the record path a `reference`
property stores (`vocabulary.RecordPath`): `tasks.substrate.reamde.dev/task/t9`
is both the URL and the stored value, so a client that holds a reference can
`GET` it by concatenation and a reader of a URL knows the kind without a
registry. Under plurals the two differed by one segment for no gain, and the
difference is what forced every client to carry a plural→kind map. The dot rule
that separates an authority from a name is unchanged and still does the whole
job of telling the two path shapes apart.

**`-` is the reserved verb segment, at every depth.** `/api/v1/-/{verb}` is a
repository verb, `/api/v1/{authority}/{name}/-/{verb}` a collection verb, and
`/api/v1/{authority}/{name}/{id}/-/{verb}` a record verb. Nothing stored can
collide with it: a record id must begin with an alphanumeric (`reID`,
internal/vocabulary/naming.go), a kind name is `[a-z][a-z0-9]*` and an
authority must carry a dot, so no id, name or authority is ever the single
character `-`. A verb added after v1 lands behind the segment and takes nothing
from any collection.

**Every method dispatches through `addressed`.** A method that has no meaning
at the address it was sent to answers `405` and names the address it would
work at, so `POST` to a record path and `PUT` to a collection path both refuse
instead of writing. This is the defect the record exists for; the other two
rules are what stop it recurring.

The rejected options each leave one of the three defects standing. Reserving a
verb segment without dropping plurals fixes the collisions and keeps the
reference/URL split, so every client keeps its plural map for nothing. Refusing
the ambiguous bindings without reserving a segment fixes today's writes and
leaves the next verb to steal an id. A `?verb=` parameter makes a mutation
invisible to any proxy, cache or log that routes on paths, and puts the verb in
the one part of a URL this API already refuses unknown keys in. The AIP
`{id}:enable` suffix is unparseable here, because `:` is inside the record id
alphabet: `alice:2` is a legal id and `alice:enable` would be one too.

**`plural` stops being a path segment**, and with it stops being what
docs/terms.md says it is. The word survives in exactly one place, the required
`data.names.plural` key on a kind declaration, and it survives there for a
mechanical reason rather than a good one: every stored kind declaration row
holds `names.plural`, the loader admits a closed key set under `names`
(internal/vocabulary/load.go), and a registry built from rows the loader
refuses is a repository that does not open. Retiring the key is therefore
gated on a dialect rung that strips it from stored rows first
(internal/engine/dialect.go's ladder), which is a separate change against the
engine and is tracked as its own issue. Until that lands the key is authored,
stored and read by nothing: `docs/terms.md` says exactly that, so the word is
not left claiming a job it no longer has.

### Consequences

- Good, because a record's URL and its stored reference value are the same
  string, so a client resolves a reference by concatenation and never needs a
  plural map.
- Good, because `recordmerge` and `recordsplit` collections become reachable,
  and a record whose id is `incoming`, `edges`, `status` or `call` is
  addressable.
- Good, because a verb added after v1 cannot remove an id from a collection,
  which makes the verb set extensible without another URL break.
- Good, because the two silent creates are gone: a client that meant to upsert
  now learns it addressed the wrong thing.
- Bad, because every client URL changes at once. The console, `substratectl`
  and every documented example move in the same commit, and an old client gets
  a `404` rather than a redirect: there is no alias prefix and nothing shipped
  under an earlier grammar was owed a sunset window (`APIVersion`,
  internal/api/api.go).
- Bad, because an OAuth redirect URI registered with a provider contains the
  callback path, which moves out of the core authority and behind the reserved
  segment, so every provider app registration is re-entered by hand.
- Bad, because `data.names.plural` stays required while being read by nothing,
  which is a dead key in the declaration grammar until the dialect rung lands.
- Bad, because the collection segment now reads as a singular
  (`/api/v1/core.substrate.reamde.dev/kind`), which is worse English than
  `/kinds` and is the price of the URL and the reference being one string.

### Confirmation

internal/api/grammar_test.go holds all three rules.
`TestPostToRecordPathIsMethodNotAllowed` and
`TestPutToCollectionPathIsMethodNotAllowed` pin the two silent creates shut and
assert that nothing was written. `TestRecordURLIsItsReference` asserts the path
a record is served at equals `vocabulary.RecordPath` of its kind and id, which
is the property the whole record rests on.
`TestReservedVerbSegmentKeepsIdsAddressable` reads back records whose ids are
`incoming`, `edges`, `status` and `call`, and
`TestShadowedCollectionsAreReachable` lists the `recordmerge` collection the
verb used to shadow. `mise run lint:docs` holds `docs/terms.md` against the
prose. What is NOT held: nothing refuses a newly added route that binds a verb
outside `-`, so that rule is held by review.

## More Information

Supersedes nothing. It spends part of the character budget
[0014](0014-authorities-widen-only-outside-the-id-alphabet.md) reserved: `-` is
inside the id alphabet but never its first character, so the single-character
segment is claimable without widening or narrowing the alphabet, and 0014's
rule that a new separator comes from outside the alphabet is met in the only
sense that binds, which is that no stored value can be mistaken for the
separator.

Reopen trigger: the move of kind identity to URLs
([#109](https://github.com/geoah/substrate/issues/109)). An authority carrying
a `/` changes how many segments precede the kind name, so the dot rule this
record leans on has to be replaced rather than adjusted, and this record's
first rule is re-derived there.
