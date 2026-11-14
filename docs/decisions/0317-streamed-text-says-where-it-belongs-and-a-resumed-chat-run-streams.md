---
status: accepted
date: 2026-11-14
decision-makers: George Antoniadis
---

# 0317. Streamed text says where it belongs, and a resumed chat run streams

## Context and Problem Statement

[0169](0169-a-chat-turn-runs-detached-from-its-client-and-its-thread-is-attached-by-id.md)
made a chat turn a detached task with an in-memory hub that replays the
turn to whoever attaches. Three things it left show to a person. A replayed
`delta` did not say where its text belongs, so a client that attached again
had to empty what it showed first, and the reply blinked. The run that
follows an answer, a decision, an approval or a background job ran with no
hub, so the chat said the assistant was working in the background and the
reply appeared whole. And a model call that failed after it had streamed
settled the thread with nothing: only a stop kept what streamed. The plan is
pull request #1363, section 2.

## Considered Options

- A stored event stream with a resume cursor, so an attach asks for what it
  lacks.
- A position on each piece of text, with the replay as it is.
- For resumed runs: leave them unstreamed; or run them through the hub.

## Decision Outcome

Chosen: positions, and resumed chat runs through the hub. This amends 0169
and keeps its ground: the hub stays in memory, the replay stays bounded,
and nothing stores an event. 0169 rejected a queue in Postgres because a
repository has one writer, and that still holds; a cursor would buy what a
position already gives, a replay that is safe to apply twice.

**Positions.** A `delta` and a `reasoning` event carry `at`: how many
Unicode code points of that `turn`'s text of that kind came before the
piece. Text and reasoning count apart, each from 0 with its `turn`. A
`turn` is one model call's assistant row, so the count starts over after a
round of tool calls. A turn's text never restarts: a call made again for a
turn that already streamed (the retry after a context overflow) streams
under a new ordinal. The unit is the code point because Go, Swift and
JavaScript all count it the same way, which is true of neither bytes nor
UTF-16 units. Writing each piece at its `at` reproduces the stored row's
`content` exactly. The hub's replay merges a turn's consecutive pieces into
one event at the first piece's position.

**Resumed runs.** A chat thread's continuation publishes into a hub exactly
as a sent turn does: an attach streams it, a stop stops it, and a stop or a
shutdown settles it `stopped`. It holds one of its person's chat turns, the
person the resolution is held to. With none free it claims nothing and
waits, kept with the slots, and starts when one of that person's turns
settles or at the next sweep. The sweep alone would not do: it finds a
resolution only on a thread that settled `ok` or `overbudget` before the
resolution landed, so one that landed mid-turn, or on a failed thread,
would be lost. A thread of another mode resumes as before.

**What streamed is kept.** The text a call had streamed when it failed, or
when the run's deadline ended it, is written as the turn's assistant row
before the thread settles, as a stop already did.

**A reply says how it ended.** An assistant `llm/message` gains `cutBy`,
absent when the model finished: `length`, `filter`, `stop`, `failure`. Each
wire maps its stop reason. A continue takes up a thread that settled `ok`
on a cut reply.

**A quiet or slow stream.** A turn's stream writes its heartbeat every 10
seconds, not 30. A reader the hub dropped for reading too slowly gets a
`lagged` event and attaches again; it no longer gets an `error`, because the
turn did not fail.

### Consequences

- Good, because a client keeps what it shows across an attach, and one row
  can be the same view from the first token to the stored message.
- Good, because every run of a chat streams and can be stopped.
- Bad, because a crash still loses the text of the call in flight. Writing
  the partial row every few seconds would close that at a changelog entry
  per few seconds, for text that settles a moment later.
- Bad, because a resumed run now needs a free chat turn, so a person at
  their cap waits for one of their turns to settle where the resume used to
  run at once, and that wait is in memory: a restart keeps only the resumes
  the sweep can find.
- Bad, because a shutdown during a resumed run settles it `stopped`, which
  nothing resumes on its own; before, the next start settled it `error`,
  which nothing resumed either.
- Bad, because `stop` on a row also covers the run's deadline, which the
  plan did not list: the alternative was to drop that text.
- Bad, because a client must count code points, and a JavaScript client
  cannot use a string's `length`.

### Confirmation

`internal/engine/agentstream_db_test.go`:
`TestTextPlacedByPositionReproducesTheStoredRows`,
`TestReasoningIsPlacedApartFromTheReply`, `TestAnAttachIsSafeToApplyTwice`,
`TestAResumedChatRunStreams`, `TestAStopStopsAResumedChatRun`,
`TestAResumedChatRunWaitsForASlot`,
`TestAResolutionMidTurnAtTheCapStillResumes`,
`TestAResumeThatWaitsOnAFailedThreadStartsWhenASlotFrees`,
`TestWhatACallStreamedIsKeptWhenItFails`,
`TestAReplyCutAtTheModelsLimitSaysSoAndIsContinued`,
`TestADeadlineInsideACallKeepsWhatItStreamed`; the hub is
`TestAReplayedTurnIsPlacedByPosition` and
`TestASlowSubscriberIsDroppedNotWaitedFor` in
`internal/engine/agentturnhub_test.go`; the wire is
`TestALaggedAttachEndsWithoutAnErrorAndPositionsAreWritten` in
`internal/api/agents_test.go`.

## More Information

Revisit with 0169, when a repository may be served by more than one
process: positions do not need the hub to be in memory, and a stored stream
would be decided then.
