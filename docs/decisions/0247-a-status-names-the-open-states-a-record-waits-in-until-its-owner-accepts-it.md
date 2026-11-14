---
status: accepted
date: 2026-11-13
decision-makers: George Antoniadis
---

# 0247. A status names the open states a record waits in until its owner accepts it

## Context and Problem Statement

Agents write suggestions as records: a task an agent read in a mail waits in a
`proposed` state until the owner accepts it or dismisses it. The tasks sample
declares that state, and so do the owner's own task kinds. Substrate Tasks is
meant to work over every kind that binds core's `lifecycle`
([0226](0226-a-kinds-status-is-the-core-lifecycle-trait-and-names-its-done-and-dropped-states.md)),
and 0226 tells it which states close a record, but not which open states are
still waiting for the owner. Without that, an app either lists every proposal
as a task the owner took on, or guesses from the word `proposed`.

## Considered Options

- A third list on the status, `proposed:`, beside `done:` and `dropped:`.
- A fixed state name that every kind spells the same way.
- The app's own setting, one per kind, naming the waiting states.
- Suggestions only as change requests (`core/recordpatchrequest` creating a
  record), never as records of the kind.

## Decision Outcome

Chosen: `proposed:`, a third list on the status. It works for any number of
states and any names, and every app, agent and query reads the same meaning
from the kind. A fixed name holds only while one owner writes every kind,
which is why 0226 rejected one for `done`. A per-app setting would make every
app ask the owner the same question. A change request is the right shape for
a change to a record that exists already. But a suggested task needs to be a
record: an agent that saw the same request again adds evidence to it, a
dismissed one stays to stop the same suggestion coming back, and every reader
finds it by its kind.

The rules:

- **`proposed:` lists open values.** Each names one of the property's own
  values, and none is in `done:` or `dropped:`. The key sits only where those
  two may: on an enum or a state property that is the kind's own and holds
  one value.
- **One value at least is in none of the three lists,** so a proposal has an
  open state to be accepted into.
- **A machine's initial state may be proposed.** A suggestion is born
  waiting, and a record created without a state is then a suggestion.
- **A machine has a transition from each proposed state straight to an open
  state that is not proposed.** Accepting is that move, and an app offers it.
  Dismissing is a move to a dropped state where the machine has one, and no
  dismissal edge is required. A staged proposal (draft, then review, then
  accepted) does not fit: each proposed state needs its own way out. The
  rule is about structure. A write along the edge can still be refused, by a
  policy or by the property's writer.
- **Listing proposed values changes no stored record,** so editing the list is
  not a narrowing.
- **`lifecycle` does not require the list.** A kind without it has no
  proposals.
- **A proposed value says where a record stands, not who put it there.** It
  proves neither that an agent wrote the record nor that only its owner may
  move it. Who wrote a record is its history's to say, and who may write it
  is the grant's and the policies'.

### Consequences

- Good, because an app can show suggestions apart from accepted work, and
  offer accept and dismiss, for any kind, provider or sample.
- Good, because "what is waiting for the owner" is said once, beside the
  states.
- Bad, because it is another dialect key, an upgrade of every binary that
  reads the closures
  ([0020](0020-dialect-keys-are-reserved-not-tolerated.md)).
- Bad, because a kind whose suggestions live in another property (a flag
  beside the status) cannot use it until its status carries the meaning.

### Confirmation

`internal/vocabulary/lifecycle_test.go` holds each rule:

- loaded: a machine born proposed, an enum with a proposed value, and two
  proposed states on a renamed status;
- refused: a value not declared, one listed twice, one already done or
  dropped, a list that leaves nothing to accept into, a list that is not a
  list, the key on a string, a repeated property or an object field, and a
  proposal whose only way out leads to another proposal.

Each refusal names the key the declaration wrote.

## More Information

Extends
[0226](0226-a-kinds-status-is-the-core-lifecycle-trait-and-names-its-done-and-dropped-states.md),
whose rules are unchanged. Substrate Tasks reads it as its Suggested queue
([the plan](../plans/tasks-sources.md)).
