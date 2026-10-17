---
status: proposed
date: 2026-10-17
decision-makers: George Antoniadis
---

# 0170. A chat thread is titled by its first message, then by its model, and an owner rename holds

## Context and Problem Statement

An `llm/thread` had no heading of its own: its `displayTemplate` was
`{agent}: {status}`, so every chat with one agent rendered the same title, and
each client derived a heading from the first user message on its own (the
console's `threadTitle` read a second query for it). A chat list needs a
heading a client can read off the thread row, that a model can improve after
the first reply, and that the owner can rename without a later write undoing
it. `title` is reserved on every kind
([0016](0016-a-kind-titles-itself-from-a-declared-property.md)), so the
heading is a declared property rendered into `title`.

## Considered Options

- Leave headings to clients.
- A `subject` property set from the first message at open, replaced once by a
  model after the first turn, with an owner rename detected by a flag property
  (`subjectLocked`) the rename sets.
- The same `subject`, with the rename detected by the property's manager row:
  the generated title is written only while the agent's actor still manages
  `subject`.

## Decision Outcome

Chosen: the third option. Every write already stamps a `property_managers`
row per changed property with the writing actor, and the provisional subject
is written under the agent's actor in the transaction that opens the thread.
Any other actor's write (`console`, `api`, `substratectl`, a function) moves
the manager, and the title task checks it under the row lock before it
patches, so a rename wins whether it lands before or after the model answers.
A flag would be a second fact to keep in step with the first and a second
write path every client must know; the manager row is visible on every read
as `propertyMeta.subject.manager`.

The title runs as a background task (`ds.spawn`) after the turn's `done`
event, and only when the run opened a `chat` thread and its first turn
settled `ok`. It never blocks the turn and never fails it: an error, an empty
answer or a `length` stop with no text is logged at `Warn` and leaves the
provisional subject, and nothing retries. A later turn never titles again.

**The model and the cap.** The model is the provider row's new optional
`titleModel`, else the thread's own model. It sits on the row because the row
already keys `pricing` and `contextWindow` by model id, so a small model named
there is priced like any other, and most agents share a row; a per-agent knob
would be a second place to name one model. The request is one user message
holding the first message and the reply, each cut to 2,000 characters, under
a fixed system prompt asking for at most eight words, with no tools. The
output cap is 32 tokens, which holds eight words in any language the tokenizers
in use split finely. A call that sends a reasoning effort other than `none`
gets 512, because a reasoning model counts its thinking against the same cap
and would otherwise stop on it with no title. The effort is the agent's on
the thread's own model, and the row's `defaults.reasoningEffort` on the
`titleModel`, because the agent's word was chosen for the agent's model.

What that costs, at a sonnet-class rate of 3 USD per 1M input and 15 USD per
1M output tokens: a typical first exchange is about 300 input tokens, so a
title is about 0.0014 USD; the cut bounds the input near 1,150 tokens, so the
most a title can cost is about 0.004 USD without reasoning and 0.011 USD with
the 512-token cap spent. A small model on `titleModel` costs its own rates.
The call's tokens and cost are added to the thread's tallies under the same
row lock, so `costUSD` stays the thread's whole cost and the spend cap
([0149](0149-a-spend-cap-holds-an-agent-trigger-a-third-state-beside-running-and-parked.md))
counts it at the next admission; the title call itself is never refused by
the cap.

The template becomes `{subject|agent}`: a thread with a subject renders it,
every other thread renders its agent's title. The status is dropped from the
heading, because the row carries `status` beside it.

### Consequences

- Good, because every client reads a heading off the thread row (`title`, or
  `properties.subject`) with no second query.
- Good, because the upgrade is two optional strings and a template: the boot
  upgrade converts no record and re-derives no reference in its transaction.
- Bad, because a rename to exactly the provisional value is not a write, so
  the manager stays the agent's and the generated title replaces it.
- Bad, because a stored title is rendered at write time: an existing thread
  keeps its `<agent>: <status>` title until its next write, so a list mixes
  the two forms for a while.
- Bad, because a chat's first turn now buys a second, small completion, and
  a repository has no switch to turn titling off.

### Confirmation

`internal/engine/agenttitle_db_test.go`:
`TestANewChatThreadOpensWithASubjectFromItsMessage`,
`TestTheFirstTurnTitlesTheThreadAndChargesTheRow`,
`TestAnOwnerRenameIsNeverOverwritten`,
`TestATitleCallThatFailsKeepsTheProvisionalSubject`,
`TestTitleModelOnTheProviderRowIsUsed`; and
`TestBootUpgradeLandsTheThreadSubjectWithNoConversions` in
`upgrade_guard_db_test.go`.

## More Information

Revisit when a client needs titles regenerated as a thread drifts, or when
repositories want titling off: either is a declared knob, not a change to the
rename rule.
