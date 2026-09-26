---
status: accepted
date: 2026-09-26
decision-makers: George Antoniadis (via the issue-624 agent session)
---

# 0107. A function's and an agent's list read is the window read

## Context and Problem Statement

[0081](0081-the-window-read-computes-occurrences-and-recurring-is-core.md)
made a records list whose filter bounds `at` on both ends compute the
occurrences of every series in play, but only the REST route did it: the
merge lived in `internal/api`, and a function body's `host.records.list`
and an agent's `query` tool went straight to `Dataset.List`. Every function
that answers "what happens on day X" carried its own RRULE expander, and
mneme's schedule surface showed 1 of 29 occurrences for one day
([#624](https://github.com/geoah/substrate/issues/624)).

## Considered Options

- Route a function's or an agent's list with a two-sided `at` bound through
  the same window read as the route.
- Add a separate `host.records.window(...)` call and leave `list` plain.
- Ship an expander in the Python SDK.

## Decision Outcome

Chosen: the same window read on every surface. The merge moves from
`internal/api/window.go` to `internal/window`, which depends on
`internal/substrate`, `internal/vocabulary` and `internal/occurrence` alone;
the records route, the engine's function host `list` and the agent `query`
tool all call `window.Read` for a filter bounding `at` on both ends, so one
filter answers one page wherever it is asked. A separate call would leave a
body free to ask `list` the same question and get the stored rows alone, the
exact gap 0081 closed for the route. An SDK expander is a second copy of the
engine's rules, which is the defect being fixed.

0039's confirmation still holds: the engine's storage, fold and `Window`
read contain no expander. The engine's read surface for bodies and agents
calls `internal/window`, as the API does.

### Consequences

- Good, because a body or an agent reads computed ids, override folding,
  `exdates` and `computed: true` exactly as a client of the route does.
- Bad, because a function or agent list that bounds `at` on both ends now
  follows the window read's rules: `orderBy` is `at` alone, `offset` is
  refused, and an empty window is refused. A body that sorted such a list by
  another property fails until it drops the order.
- Bad, because a body that already expanded series itself now sees each
  occurrence twice until it drops its expander.
- Bad, because the overrides that claim a slot are read whatever their kind,
  as on the route, so an override of a kind outside a body's reads allowlist
  still removes its slot from the page.

### Confirmation

`TestTriggerHostListBoundedOnAtComputesOccurrences` and
`TestAgentQueryBoundedOnAtComputesOccurrences` in `internal/engine` pin both
reads against Postgres; `internal/api/window_test.go` still pins the merge
through the route.

## More Information

Amends 0081, whose "the API layer does the expanding" now reads "the read
surfaces do, through `internal/window`". Revisit if a body needs the stored
rows of a window without the occurrences.
