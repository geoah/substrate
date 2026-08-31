---
status: accepted
date: 2026-08-31
decision-makers: George Antoniadis
---

# 0043. Occurrences expand at read, in the API layer, never in the engine

## Context and Problem Statement

[0039](0039-the-substrate-stores-a-recurrence-rule-and-never-expands-it.md)
settled where recurrence expansion does not happen: the substrate stores the
rule and never turns it into rows. It named two materialization paths, a
reader computes occurrences or a connector writes them, and only the connector
path was ever built. No reader exists: the console has no expander, agents and
functions have nothing to call, and `substratectl` reads what the API serves.

The cost surfaced as soon as a recurring record that no connector feeds was
asked a calendar question. The repository-wide temporal window query
(`implements: temporal` with an `at` range) returns rows, so it returns
Google's exploded `calendarevent`s but not tomorrow's dose of an open-ended
daily `medicationschedule`: the schedule matches only the one window holding
its own `at`, and its occurrences match none, because they are not rows.
Every recurring kind without a materializer, `medicationschedule`, `routine`,
`workout`, `observation`, `task`, is invisible to the one query that was
supposed to unify things-with-a-time. `calendareventseries` has the mirror
problem beyond the sync horizon: the connector materializes about a year, so
"when does this meeting fall in 2028" has no answer at all.

## Considered Options

- Each reader writes its own expander: the console in TypeScript, functions in
  Python, the CLI in Go.
- One expander in the API layer, serving a computed read endpoint; the engine
  stays expander-free.
- The engine expands at query time, merging computed occurrences into the
  generic records query (supersedes 0039).
- A materializer writes occurrence rows for local recurring kinds, the way the
  Google connector does for series.

## Decision Outcome

Chosen: one expander in the API layer. `GET /api/v1/occurrences?from=&to=`
computes the occurrences of every record implementing the
`scheduling.substrate.reamde.dev/recurring` trait inside the window and
returns them as a read. It writes nothing: no record, no changelog entry, no
id. The expander is `internal/occurrence`, built on the same rrule-go the
write-time validation uses; `internal/api` imports it and `internal/engine`
never does, so 0039's confirmation ("no expander exists in the engine") stays
literally true. The fold and the generic records query are untouched.

The contract, in five clauses:

- **The anchor is `startsAt`, else `at`.** A rule names no instants without a
  DTSTART. `calendareventseries` declares `startsAt`; a kind that binds
  `temporal(range)` anchors at its own `at`. A recurring record with neither,
  or with no `recurrence` at all (an as-needed schedule), yields no
  occurrences.
- **`exdates` subtract, `rdates` add**, both after the rule runs, matching
  iCalendar's union-then-except semantics. The `timezone` property names the
  zone a time-of-day rule resolves in, so a daily 09:00 stays 09:00 across a
  DST boundary.
- **Materialized spans are suppressed.** A kind whose occurrences exist as
  rows declares `materializedFrom` / `materializedUntil` (until is exclusive,
  Google's `timeMax` convention), stamped by whatever writes the rows. The
  expander emits nothing inside the stamped span: there, the rows are the
  truth, and a computed twin would duplicate every synced event. Outside the
  span the rule is the best available answer. The Google connector stamps both
  on every full (token-less) events fetch and leaves them alone on delta runs,
  whose window Google remembers; a series first seen on a delta run stays
  unstamped until the next full fetch, and its computed occurrences may
  duplicate delta-delivered rows until then.
- **Logs annotate, never suppress.** An occurrence whose slot an
  `occurrencelog` row answers (matched on `scheduledAt` and the log's edge to
  the recurring record) carries that log's reference and status. Absence still
  means missed, and a logged dose still appears, marked.
- **A computed occurrence is not a record.** It has no id, no edges, cannot be
  watched, and never enters the changelog. A consumer building an agenda runs
  the temporal window query for rows and this read for the rest, and merges;
  a row and a computed occurrence for the same slot meet on the source record
  and the instant.

### Consequences

- Good, because one implementation serves every consumer, console, agents,
  CLI, tests, against the trait rather than any one kind, so a new recurring
  kind is covered the day it binds the trait.
- Good, because the medication question ("what do I take on Tuesday, beside
  my meetings") is answerable with two reads and a merge, with the travel-week
  override expressed as `exdates` plus `rdates`, the same mechanics the
  Google series uses.
- Good, because the sync horizon stops being the only source of far-future
  answers: the connector's window could shrink later without losing reads.
- Bad, because a computed occurrence is not addressable: nothing can link to
  it, watch it, or attach anything to it. The moment an occurrence needs
  identity, something must write a record for it, and that something is a
  reader or connector, per 0039.
- Bad, because merge responsibility sits with the consumer, and an unstamped
  series (first seen on a delta run) can show a computed twin beside a row
  until the next full fetch re-stamps it.
- Bad, because a dense rule is a compute cost at read time; the expander
  carries an iteration budget and the endpoint a slot cap, and a rule that
  blows the budget is reported per record, not expanded.

### Confirmation

`internal/occurrence` has no engine import and `internal/engine` imports no
expander; the API's handler tests pin the contract against the fake, the
expander's unit tests pin the rule matrix (DST included), and the live e2e
suite proves the medication scenario end to end. `mise run kinds:check` holds
the stamped kinds' versions.

## More Information

The trait lives in `kinds/scheduling.substrate.reamde.dev/recurring.yaml`;
0039 holds the storage stance this read completes. The partner trait
`occurrencelog` supplies the annotation half.
