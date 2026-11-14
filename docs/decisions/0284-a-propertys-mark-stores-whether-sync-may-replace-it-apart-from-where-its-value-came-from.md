---
status: accepted
date: 2026-11-13
decision-makers: George Antoniadis
---

# 0284. A property's mark stores whether sync may replace it apart from where its value came from

## Context and Problem Statement

Every property carried a manager row: an actor and a tier. The actor
answered three questions: who wrote a direct write, which source's writer a
synced value was credited to although the engine wrote it, and, as the text
`mapping:<name>`, which mapping filled a subject slot. The tier looked like a
ranking and was read as a yes or no: recompute replaced a value only at
`machine`. Decision 0113 read it again from the actor's declaration, which
decision 0270 made impossible. Offers were grouped by the actor that wrote
their sources, so two accounts of one provider synced by one function were
one alternative. Pinning was a side effect of writing, and releasing was
deleting the property, which a required property refuses. The plan (pull
request 1313, "Where a value came from", row 30, step 17) proposes two facts
stored apart.

## Considered Options

- Store whether sync may replace a value apart from where it came from (the
  Google People API keeps a field's source apart from which value is
  primary).
- Refuse a second writer until it takes the field over (Kubernetes
  server-side apply).
- Keep every source's and every person's statement as a record and select
  one (Wikidata ranks).

## Decision Outcome

Chosen: two facts stored apart, because it is what the serving code already
asks, and the other two change what every direct write does or add more
records and rules than the problem needs.

- **A property's mark** sits beside its value: `pinned`, `setBy` (a link to
  the actor record that wrote it directly), `onBehalfOf`, `derivedFrom` (links
  to the source records whose offers back it) and `setAt`. A read serves it as
  `marks`, keyed by property, with each live source record's differing offer
  as an alternative (`derivedFrom`, `value`, `updatedAt`).
- **`pinned` is stored when the value is written**, from the type of the
  hand: a person's, an app's, a function's and an agent's direct write pins,
  a package's and the engine's do not. Nothing works it out again later, so
  this record replaces 0113 and the `tier:` key an actor document carried is
  gone with the documents (decision 0270).
- **Recompute yields to a pinned value**, writes what nothing pinned with the
  sources that back it (the one offering it, those that agree with it, or,
  on a union, each contributing one), and keeps `derivedFrom` to the live
  sources that still back a value it leaves alone: a required value whose
  last source went keeps the value and loses the link.
- **`pin` and `release` are explicit actions**, each an entry of the
  record's history: pin keeps the current value, release hands it back to
  sync, which picks among the offers at once. Both work on a required
  property.
- **Offers are keyed by source record**, so two accounts of one provider are
  two alternatives.
- **A link a mapping fills carries no mark**: its declaration says which
  mapping fills it.
- **A mark never stands without the value it marks.** The fold holds it: a
  line that takes a property out of a record takes the property's mark with
  it, whether or not the line says `unmark`, applied as the line ends, so a
  mark the line moves to another name stands, and a property the record never
  held keeps whatever mark it has. A merge moves no mark and no value: a
  pin stays with its value on the record it was pinned on, the loser
  keeping both on its tombstone, and a split returns them. A hold
  a merge recorded before this left on a winner with no value is no mark,
  and its replay drops it. Carrying a correction across a merge is a feature
  of its own (issue 1356).
- **The conversion** rewrites each manager effect into a mark, resolving each
  stored tier once: `owner` and `bundle` pin, `machine` does not. Nothing is
  guessed: no converted mark has a `derivedFrom`, and an unpinned one names
  its actor only where the line itself wrote it.

This amends 0092 (an orphan is a target nothing pinned) and 0094 (an
alternative and a synced value name their source records through
`derivedFrom`).

### Consequences

- Good, because a person, an app and a function holding a value read the
  same way, and a function's write over a person's edit is a pin like any
  other, visibly.
- Good, because releasing works on a required property, and two accounts of
  one provider show what each says.
- Bad, because the wire's `propertyMeta` is gone: every client reads `marks`
  (the records feature is at revision 5).
- Bad, because a mark names no token: which token wrote a value is its
  history line's.
- Bad, because a mark a converted history left on a subject slot is never
  served and goes at the slot's next write.

### Confirmation

`TestAFunctionsWriteIsAVisiblePin` and `TestPinnedIsTheTypeOfTheHand`
(`internal/engine`) hold the pin to the hand's type, the release, and the
alternative keyed by source record; `TestTheMarksNameTheSourceRecordsBehindEachOfferAndEachValue`
the sources behind a synced and a union value; the pull request's HTTP
tests the worked example, whose two address books are two accounts of one
provider, and a required property released; and the conversion's tests (`internal/engine/marklines_test.go`)
what each old line becomes, the kept directory included.

## More Information

[Where a value came from](../projection.md#where-a-value-came-from-the-marks)
describes the marks, and [the line](../stored-format.md#the-line) the `mark`
and `unmark` effects.
