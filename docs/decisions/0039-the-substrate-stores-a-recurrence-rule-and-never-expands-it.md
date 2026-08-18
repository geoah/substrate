---
status: accepted
date: 2026-08-18
decision-makers: George Antoniadis
---

# 0039. The substrate stores a recurrence rule and never expands it

## Context and Problem Statement

Six kinds carry an RRULE: `tasks/task`, `fitness/workout`,
`health/observation`, `health/medicationschedule`, `routines/routine` and
`calendar/calendareventseries`. Extracting the copied `recurrence` / `exdates`
/ `timezone` triple into one `scheduling/recurring` trait
([#122](https://github.com/geoah/substrate/issues/122)) forces one answer to a
question the copies left implicit: does the substrate expand a stored rule into
occurrence rows? Five kinds' comments say it never does; `calendareventseries`
reads as the exception, because its occurrences exist as separate
`calendarevent` records. The trait cannot contract a stance the six kinds do
not share.

## Considered Options

- The substrate stores the rule and never expands it; occurrences materialize
  only off the substrate (a reader computes them, or a connector writes them).
- The substrate expands rules into occurrence rows, and `calendareventseries`
  is the model every recurring kind follows.
- The trait stays silent on expansion and each kind documents its own stance.

## Decision Outcome

Chosen: the substrate stores the rule and never expands it. The `recurring`
trait contracts that stance, and every binding kind holds to it. Occurrences
are materialized only off the substrate, two ways:

- A reader computes them. A recurring `task` or `routine` has an occurrence
  only when its `occurrencelog` marks one; absence is what missed means. No row
  exists for an occurrence nobody logged.
- An external connector writes them as separate records.
  `calendareventseries`' occurrences arrive as `calendarevent` records the
  connector exploded from the rule.

A connector writing records is not the substrate expanding the rule, so
`calendareventseries` CONFORMS to the trait rather than standing as an
exception. The substrate never turns one recurring record into many rows on its
own, whichever kind carries the rule.

### Consequences

- Good, because the trait contracts one stance the six kinds already followed
  once read precisely, so the extraction adds no behavior.
- Good, because a reader or a connector, not the substrate, owns occurrence
  materialization, so the engine grows no RRULE expander to keep correct.
- Bad, because "how many times has this happened" is a computed read every
  consumer writes for itself, never a stored count.

### Confirmation

The `recurring` trait declaration carries the stance as its contract, and every
binding kind's description states it. No expander exists in the engine to hold
back. `mise run kinds:check` refuses a binding kind whose declaration changes
without a version bump.

## More Information

The trait lives in `kinds/scheduling.substrate.reamde.dev/recurring.yaml`. The
partner trait `occurrencelog` carries the reader-computed half of this stance:
one occurrence, marked done or skipped, absence meaning missed.
