---
status: accepted
date: 2026-11-13
decision-makers: George Antoniadis
---

# 0231. A contact is a core kind of the owner's data, and a setting names the owner's own

## Context and Problem Statement

Nothing every repository holds can stand for a person, business, group or bot
the owner deals with. `samples.substrate.reamde.dev/people/person` is a
sample, rehomed to `<authority>/people/person` in each repository, so no
provider, app or other substrate can name it, and Mneme had to fork it. The
design is [the contacts plan](../plans/contacts.md); this record settles where
the kind lives and how the owner is found among the contacts. The mappings
onto it, the people sample's removal and the shipped views are later
records; nothing migrates a repository off the sample.

## Considered Options

- A seeded package of its own, `substrate.reamde.dev/contacts`, as 0192 did
  for `observations`
- A kind in core, `substrate.reamde.dev/core/contact`
- For the owner: a flag property, a match of `substrate` against the
  repository's name, a `contact` reference on the core `repository` record,
  a contact the engine seeds under the fixed id `me`, or a core `setting`
  naming an ordinary contact

## Decision Outcome

Chosen: `substrate.reamde.dev/core/contact`, declaring `purpose: primary`, and
the core setting `substrate.reamde.dev/core/ownerContact` naming the owner's
own contact by its id. A package holding one kind was
rejected by the owner, and no plan names a second kind that belongs with
contacts: the sample's organizations and teams become contacts of type
`organization` and `group`.

Core is the substrate's machinery, and three places assume every core kind is
too. `vocabulary.SeededOwnerData` names the exceptions inside core, llm and
console (`core/contact`, and `llm/memory`, which the tree already treated
so), and each place reads it:

- the generic merge (`guardMergeType`) admits them, because duplicate
  contacts are what an address book collects;
- the console lists them under the purpose they declare, in a group of their
  own, instead of forcing them internal into the Substrate group
  (`SEEDED_OWNER_DATA_KINDS` in `web/console/src/lib/core-kinds.ts` is the
  same list);
- the shipped-purpose test admits a non-internal purpose for them.

The owner's own contact is an ordinary contact under a minted id, and the
setting's `value` is that id, as Apple Contacts' "My Card" points at one card.
The console asks for the owner's name after registration, creates the contact
and writes the setting; the owner may point it at another card later. Nothing
seeds either record. A flag or a `substrate` value can be on two contacts,
and `repository` is a system kind no request may patch. A seeded `contact/me`
was built first and dropped: it was the one chosen id in a kind of minted
ids, so it needed an exemption from the rule that a mapped kind's ids are
server-assigned and had to be written above the machine tier, or the first
recompute would have emptied it; and every repository would hold a
`contact/me`, so a card shared between substrates would collide on it. A read
by a former id returns the merge winner, so the setting keeps reaching the
owner's card after it is merged into their address-book card.

### Consequences

- Good, because every provider, app and other substrate names one kind,
  `substrate.reamde.dev/core/contact`, and an agent finds the owner by
  reading one setting.
- Good, because the exception is one named list, so the next owner-data kind
  in a machinery package is one line in Go and one in the console.
- Bad, because core now holds owner data, and a reader can no longer take
  "core" to mean machinery without the list.
- Bad, because `llm/memory` becomes mergeable and listed in the console's
  everyday navigation as a side effect of sharing the list.
- Bad, because finding the owner takes two reads, and `value` is a plain
  string: nothing checks that it names a live contact, so a deleted card
  leaves the setting naming nothing until the owner points it elsewhere.
- Bad, because a repository registered before the console asks has no owner
  contact until its owner names one.

### Confirmation

`TestEveryShippedPurposeIsValidAndTheSeedIsInternal` (kinds/kinds_test.go)
refuses a non-internal machinery kind outside the list.
`TestMergeTakesContacts` (`internal/engine/contact_db_test.go`) holds the
merge exception, its limit, and the setting reaching a merged card.

## More Information

Plan and reviews: [docs/plans/contacts.md](../plans/contacts.md),
[PR 1206](https://github.com/geoah/substrate/pull/1206). Revisit if a
second owner-data kind in core would need behavior the list cannot express,
which is the point a separate seeded package earns its place.
