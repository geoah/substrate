---
status: accepted
date: 2026-09-14
decision-makers: George Antoniadis
---

# 0080. The window read computes occurrences, and `recurring` and `override` are core traits

## Context and Problem Statement

[0039](0039-the-substrate-stores-a-recurrence-rule-and-never-expands-it.md)
stores a recurrence rule and never expands it into rows, which stands.
[0043](0043-occurrences-expand-at-read-in-the-api-layer.md) then put the
expansion behind a SEPARATE read, `GET /api/v1/occurrences`, made a computed
occurrence "not a record", and assigned the merge of rows and occurrences to
every consumer. The one read that answers "what is on my timeline this week",
the records list over `temporal` with an `at` window, answered without the
occurrences, so a five-year medication schedule, a recurring task or a
repeating meeting was invisible to it. The separate read also matched nothing
in a real repository: it filtered on the shipped sample spelling of the
`recurring` trait, which a sample import rehomes onto the repository's
authority. And a provider's kinds could not bind `recurring` at all, because
it was a sample trait and no provider requires a sample. The plan is
[docs/plans/google-calendar-mirror.md](../plans/google-calendar-mirror.md).

## Considered Options

- Keep the separate read and add an agenda read (or a CLI command) that
  merges the two halves.
- Fold six optional properties into `temporal` and have the binding supply
  them the way `at` is supplied.
- Two contract-style core traits, `recurring` and `override`, and the
  records read computing occurrences whenever its filter bounds `at` on both
  ends.
- Expand in the engine, merging computed occurrences into the generic query.
- Materialize occurrence rows on a schedule.

## Decision Outcome

Chosen: two contract-style core traits and the window read. `recurring`
(`recurrence`, `rdates`, `exdates`, `timezone`) marks a series; `override`
(`recurrenceOf`, `originalAt`) marks one occurrence of a series that was moved
or edited, iCalendar's `RECURRENCE-ID`; both expect `temporal` on the same
kind (a kind without it is not on the timeline and no window read computes
from it, but the loader does not refuse the pair, because a repository that
imported the `scheduling` sample before the move holds exactly that shape
and must keep booting), and the kind declares the properties itself, so it
keeps `repeated`, the reference pin and `onDelete`. A records list whose filter bounds `at` on both
ends is a **window read**: it answers with the rows in the window and, beside
them, the occurrences computed from every series among the kinds in play,
minus each series' `exdates` and minus every slot an override claims, ordered
by slot, on one page. A computed occurrence is served in the record envelope
(the series' kind, the id `<seriesId>_<slot>`, `computed: true`, version 0)
and a put at that id materializes it as an override; an exdate cancels one
occurrence. `GET /api/v1/occurrences` is removed.

It beat the agenda read because a second read leaves the timeline read wrong
and every future consumer rebuilds the merge. It beat properties on
`temporal` because the loader has no trait-supplied property (only `at`,
`endsAt` and `dueAt` are column-backed) and reserving `timezone` or
`recurrence` would refuse core's own trigger schedule. It beat expanding in
the engine because the API layer sees the same rows through one new dataset
read, `Window`, that answers rows, series and overrides on one snapshot, so
the fold and the engine stay expander-free and 0039's confirmation holds
literally. Materializing rows is what 0039 rules out.

Two rules travel with it. A bare trait name that core declares resolves to
core's, so a repository still holding the `scheduling` sample's `recurring`
keeps booting with its bindings intact and shadowed. And the computed id is
not a change to the path grammar
([0014](0014-authorities-widen-only-outside-the-id-alphabet.md) stands): the
`_` is inside one id segment, parsed only on a miss for a kind binding
`recurring`, and a stored record at that id always wins.

### Consequences

- Good, because one read serves every consumer, and a new recurring kind is
  on the timeline the day it binds the trait.
- Good, because a provider's series binds a core trait, so the Google mirror
  is a copy of Google's own model (masters, exceptions, singles) and the
  future is computed from local rows alone.
- Good, because "move this one", "edit this one" and "attach something to
  this one" are one ordinary write, and "cancel this one" is one exdate.
- Bad, because a window read orders by `at` only, in either direction, and
  descending pays the window's whole expansion per page.
- Bad, because the candidate series set must be complete for a page to be
  correct, so a repository with more than the budget of series in one window
  is refused rather than paged.
- Bad, because a computed occurrence has a version of 0 and no changelog
  entry: it cannot be watched, and a timeline client re-reads when a series
  changes.

### Confirmation

`internal/api/window_test.go` pins the merge, the paging, the refusals, the
computed envelope and the GET at a computed id against the fake;
`internal/engine/window_db_test.go` pins the engine half against Postgres;
`internal/occurrence` pins the rule semantics (the anchor is an occurrence,
`UNTIL` short-circuits); the vocabulary tests pin core-wins resolution; the
wire golden pins `computed` and `problems`.

## More Information

Supersedes 0043. 0039 is unchanged: the substrate still never stores an
expansion, and its confirmation ("no expander exists in the engine") is what
`Window` preserves. Revisit if a consumer needs a computed occurrence to be
addressable without materializing it, or if span-overlap semantics are wanted
for the window (today the window bounds each item's start, as the `at`
predicate always has).
