---
status: accepted
date: 2026-11-13
decision-makers: George Antoniadis
---

# 0273. Body is an ordinary property, and no property name is stored apart

## Context and Problem Statement

[0041](0041-body-is-a-declarable-property-and-fts-is-per-property.md) made
`body` declarable and kept its column: a declared `body` lived in
`records.body` and in a field of every history entry of its own, and every
path that read either had a branch for the name. It recorded the price:
"`required`, `pattern` and `min` on a `body` property are not enforced".
Reading the code against it found worse: `required` on a `body` refused every
write, because the check looked for the value among the properties it had
been routed out of, and an empty `body` sorted and filtered as present. F14 of
[pull request 1313](https://github.com/geoah/substrate/pull/1313), row 43,
proposes making it an ordinary property: no property name should be special.

## Considered Options

- Keep the column, and route the checks through it.
- Make `body` an ordinary property: its value moves in with the record's other
  properties, and the history with it.
- Store every long text apart from the other properties.

## Decision Outcome

Chosen: `body` is an ordinary property, because the column served one name
and not long text, and every branch it took was a place the name behaved
unlike any other.

A kind declares `body` with any type, as any other name, renames it either
way, and every check applies. Its value lives in `props`. A history entry
carries it in `set` and `del`: a new changelog dialect, whose conversion step
rewrites every older line from `delta.body` (a `""` there into `del`), so a
server reads one shape. A SQL migration drops the column and writes no row,
since the boot check converts each repository's history and rebuilds its
records from it before anything is served. A repository migration then
removes the value from every live record whose kind's usable declaration does
not declare `body`: a value written before 0041 on such a kind stayed in the
column, never served, and in `props` it would be. A kind with no usable
declaration (none stored, or its package quarantined, at that very open or
before: usable is what the open's admission admits) keeps the value, which
a listing served with no kind, and loses it when its kind becomes usable (an
install, or an open that lifts the quarantine). Search indexes `body` in its
place among the long text, band C as before; a record of a kind no binary
declares indexes its title alone; `searchIndexVersion` 4 re-derives every row.
The wire does not change: a record, the envelope `substratectl` reads and
writes, a change event and a function's input already carried `body` in
`properties`.

Keeping the column and teaching every path the checks was rejected: it keeps
a name special in every place 0041 listed and every place since. Storing all
long text apart is left for later, once no name is special, as the plan says.

### Consequences

- Good, because a check, an order, a filter, a search, an embedding and a
  rename answer `body` exactly as they answer `content`.
- Good, because a `body` declared `required` accepts a write that carries it.
- Bad, because the history's shape changes: a converted repository opens on
  no binary before its dialect, so going back after the upgrade means
  restoring a copy taken before it.
- Bad, because a record whose stored `body` breaks its kind's `pattern` is
  served as stored and refused when a write sets `body` again, and one that
  lacks a `body` its kind declares `required` has every write refused until
  one sets it, as for any property. No shipped kind declares either on `body`,
  and before this a `required` `body` refused every write.
- Bad, because a record of a kind no binary declares is no longer found by a
  word in its body.
- Bad, because a body its kind does not declare is removed from the record by
  a write of the system's once a usable declaration hides it; the history
  keeps the value.

### Confirmation

`TestBodyTakesTheChecksEveryPropertyTakes`,
`TestBodySortsAndFiltersAsAnotherLongTextProperty` and
`TestBodyIsFoundAsAnotherLongTextPropertyIs` (`internal/testenv`) hold `body`
to the answers another name gets over HTTP. `TestNoPropertyNameIsSpecialInTheSource`,
`TestNoRowOrDeltaFieldIsAPropertyOfItsOwn` and
`TestBodyHasNoColumnAndNoHistoryFieldOfItsOwn` (`internal/engine`) fail if a
branch, a field or the column comes back.
`TestABodyInAHistoryFromBeforeDialectFourConvertsAndIsFoundAgain` and the
step's line tests hold the conversion, and `TestKeptDirectories` the kept
directory.

## More Information

Amends 0041: `body` loses its column, its `fts` rule is every property's,
and the loader's rules for the name (text only, no rename) are gone. 0016's
`title` is unchanged and keeps its column.
