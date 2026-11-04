---
status: proposed
date: 2026-11-04
decision-makers: George Antoniadis
---

# 0226. A kind's status is the core `stateful` trait, and names its done and dropped states

## Context and Problem Statement

Substrate Tasks should work over the owner's own kinds and the well-known
ones beside its own: a GitHub issue, a Linear issue, a sample's task, a
Mneme commitment. An app reaches every implementor of a trait
([0225](0225-an-apps-scope-may-name-a-trait-and-covers-what-star-covers-among-the-kinds-that-bind-it.md)),
but no trait says what such a kind shares. Their statuses are modelled three
ways: GitHub's is an enum it owns, Linear's a reference to a team's workflow
state, the rest state machines, each with its own names. A trait contracts
presence and datatype by name ([traits](../traits.md)), so nothing tells an
app which of a kind's states mean the work is over.

## Considered Options

- A `stateful` trait over the status itself, the kind naming which of its
  states are done and which dropped.
- A `completable` trait over the outcome, `completedAt` and `canceledAt`,
  the status left to each kind.
- A `stateful` trait whose done states every app maps for itself.
- Fixed state names every binder spells alike, as Mneme's `attention` trait
  is held.

## Decision Outcome

Chosen: `substrate.reamde.dev/core/stateful`, over the status itself, the
kind naming which of its states are done and which dropped. An app draws
each kind's own states and moves it between them, any number and any names,
and every app reads the same meaning of open and closed from the kind
rather than mapping it for itself. `completable` would admit every model
with no change to the dialect, but it says only that the work ended, and an
app could not draw or move the states. Fixed names hold only while one
owner writes every kind, and GitHub's names are GitHub's.

It is core's because the loader holds its shape, as it holds every trait
whose shape the engine reads, and because a provider, a sample and an app
must all be able to bind it without requiring a package for it.

The rules this binds:

- **The trait contracts `status: enum`, and a state machine meets it.** A
  state machine is an enum with moves, as an object meets a `json`
  contract. The contract is about reading. The two are still written and
  stored their own ways:
  - An app moves a machine's state by a patch along one of its
    transitions, never a put, so it reads the machine to offer the moves.
  - An enum takes any of its values.
  - A machine's state is stored apart from the record's other properties,
    so a filter over several kinds reads each kind's status where that
    kind stores it.
- **One status property.** An app reads one value. A kind whose status
  spans two properties has its sync fill one enum, and binds that. Two
  examples: GitHub's `state` beside its `stateReason`, and Linear's
  reference to a workflow state, whose type says what kind of state it is.
- **A binding may rename the property it contracts**:
  `stateful(status: state)` binds a kind whose status is called `state`.
  One property, for a trait without variants that the engine does not read
  by name: core's other traits refuse it. The time properties' remap
  (`temporal(point: dueAt)`) is unchanged. Both use one spelling. After the
  colon is the kind's property that receives the trait's. Before it is a
  variant where the trait has variants, and the contract property where it
  has none, and the loader knows which from the trait.
- **`done:` and `dropped:` on an enum or a state property** list its own
  values or states. The rules:
  - Each names a declared one, so a value renamed by `renamedFrom` is
    listed under its new spelling.
  - The two lists do not share one, and at least one is left out of both.
  - A machine's initial state is in neither.
  - They sit on a kind's own property that holds one value. A field, link
    data, a list or a map refuses them.

  Every state in neither list is open. A kind that binds `stateful` lists
  at least one done state, so no app guesses. The lists change no stored
  record, so editing them is not a narrowing.

### Consequences

- Good, because a kind of any status keeps its own states: an enum as
  GitHub's is, a state machine as Tasks', the sample's or an owner's own.
- Good, because what is closed is said once, beside the states, for every
  app, agent and query that reads the kind.
- Bad, because a status that is a reference cannot meet the contract:
  Linear's issue binds once its sync fills an enum from its workflow
  state's type.
- Bad, because a renamed status is filtered under the kind's own name: a
  query across implementors reads each binding to find it.
- Bad, because an app writes a machine and an enum differently, and a
  machine's transitions may refuse a move the trait alone does not show.
- Bad, because a status coarser than its reasons lists them coarse: a
  GitHub issue closed as not planned reads as done until its sync fills a
  finer status from `stateReason`.
- Bad, because three dialect additions land at once, each an upgrade of
  every binary that reads the closures
  ([0020](0020-dialect-keys-are-reserved-not-tolerated.md)), and every
  repository's core gains a trait at its next boot.

### Confirmation

The vocabulary tests hold each rule:

- an enum and a state machine meet the contract;
- the rename and its refusals;
- `done:` and `dropped:` checked against their values and the initial
  state, and refused on a field, link data or a list;
- a binding without a done state is refused.

`mise run kinds:check` holds the shipped bindings (the tasks sample,
GitHub's issue) to their versions.

## More Information

Follows the app's side of
[0225](0225-an-apps-scope-may-name-a-trait-and-covers-what-star-covers-among-the-kinds-that-bind-it.md):
Tasks asks for `write:implements(substrate.reamde.dev/core/stateful)`. Reopen
if a status needs a third closed meaning, or if the engine should answer
"open" across kinds itself.
