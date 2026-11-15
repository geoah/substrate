---
status: accepted
date: 2026-11-14
decision-makers: George Antoniadis
---

# 0318. A message sent while a chat turn runs joins it, and one the run never read starts the next turn

## Context and Problem Statement

[0169](0169-a-chat-turn-runs-detached-from-its-client-and-its-thread-is-attached-by-id.md)
gave a thread one turn at a time: a message sent while one runs is `409
conflict` (`thread_busy`). So the one way to say "not that one" to an
assistant that is working is to stop it, which ends the run, and a client
must hold the composer shut until the turn settles. The plan is pull request
#1363, section 6.

## Considered Options

- Keep the refusal, and let clients hold the message until the turn settles.
- Admit the message and cancel the model call in flight, so it is read at
  once.
- Admit the message, write its row at once, and have the loop read it
  before its next model call.
- For a message the run never reads: go on inside the same run; or settle
  the run and start a turn of the message's own.

## Decision Outcome

Chosen: write the row at once and read it at the run's next call, and start
a new turn for a message the run never read. This amends 0169 and keeps its
ground: one loop per thread, in the serving process. A client that holds the
message loses it with the tab. Cancelling a call throws away the text and
the tool calls it was producing.

**Admission.** A chat send that names a `messageId`
([0312](0312-a-chat-message-may-name-its-own-id-and-a-thread-is-continued-with-no-message.md)),
into a thread a detached chat turn of its own agent runs on, writes the
user row under that id and is answered with that turn's stream, as an attach
is. A message with no id is still refused: the row is written before any
turn is known to read it, so it must be one a repeat can be told from. A
running sub-agent's thread, a call's and a triggered run's refuse as before.
The message takes no chat-turn slot and passes no spend check: it adds no
run. Its sender is held to `mayContinue` as before, and is added to whom the
rest of the run is held to
([0276](0276-one-check-holds-every-hand-that-writes-and-one-list-names-the-control-kinds.md)). A write whose
commit was sent and never answered is settled by reading its row, before
the next call or the settle goes on.

**Where it is read.** Before each model call, after the tool results of the
last one. A call in flight is not touched. A message adds no call and moves
no budget: a steered run is one run under its turn, tool-call and deadline
budgets. Every call reads what was sent, the run's last turn under a
deadline too: a reply written past a message's ordinal must be one that
read it.

**A message the run never read** (sent during the final answer, or when a
budget ended the run) starts the next turn itself where the run settled `ok`
or `overbudget`: a new run, with budgets of its own, held to the sender of
every message it starts from, which claims the thread only while those very
messages wait on it. Going on inside the first run would let messages hold one run, its
slot and its lifecycle lock open without end. A run that was stopped, or
failed, starts nothing: the stop is the later word, so 0169's rule stands,
and a failure is not retried unasked. The message stays in the thread, and
the person's next message or continue reads it. A continue now also takes up
a thread that settled `ok` with a message unanswered. A message is not
withdrawn once it is sent: a route that takes one back would race the call
that reads it.

**After a restart.** That turn is started from memory. The next process
finds an `ok` chat thread that settled within the hour and holds a message
no reply came after, at its first resolution sweep, and starts the turn
then. Only `ok`: there the run wrote a reply after its own message, so a
message past every reply was never read, where on an `overbudget` or a
failed thread the same rows may be the message the run could not answer.

**`lastMessage`**
([0313](0313-how-far-a-person-has-read-a-thread-is-a-record-of-their-own-and-its-position-only-moves-forward.md))
passes over a person's message that no reply came after, while the thread
holds anything else, so a queued message never makes a chat unread. A run
that ends with one unread names what it wrote last (its reply, a reply cut
short, a tool row), or what the thread named before where it wrote nothing;
the turn the message starts then names its own reply. The rule reads the
rows alone, so the settle after a crash names the same, and `lastMessage`
never moves back, so a mark the upgrade seeded on such a message stays
read. It moves 0313's
case of a call that failed before any reply: the thread keeps naming the
reply before the message, and names the message only in a chat that holds
nothing else. `waitingOnPerson` counts no message.

**A turn that waits on the person** (a question, an approval) has settled,
so a typed message is an ordinary send. It answers and dismisses nothing.

**Order.** A message's ordinal and its place in the loop's queue are taken
in one step, and so are a call's reading of the queue and its reply's
ordinal. So a call read a message exactly when the message's `turn` is below
that call's reply's. Rows are replayed in creation order, and the message's
row is created before the reply that was streaming, so replay moves a user
row behind every row created after it under a smaller ordinal. The result of
a call under way when the message was sent may carry a larger ordinal than
the message; replay already puts each result right after its call.
Compaction plans over the rows in replay order, so a summary covers a
message where the model read it.

**The stream.** Two events: `messageQueued` (the row is written: `id`,
`turn`, `text`) and `messageTaken` (a model call now reads it), which the
turn a message starts sends too. The plan's signal, any event whose `turn`
is past the message's, is not sound: a tool event of the call that did not
read it can carry a larger ordinal. A reply's does mean it.

**The revision** is `agents` 6. Below it a client must not send to a running
thread. At it, a client built for 3 to 5 that does so gets a stream where it
expected `409`: it reads it as the answer to a repeated send, which is the
same shape. A client that sends no id sees no change.

### Consequences

- Good, because a person corrects a run without ending it, and the message
  is in the repository's history from the moment it is sent.
- Good, because a queued message cannot run twice or be lost: it is a row
  under an id its sender chose, in a thread whose status says what happened.
- Bad, because the next turn is started from memory, and the sweep that
  finds one a restart lost reads back one hour and only once per open. Past
  that, and where that turn is refused (a spend cap, a key removed), an
  `ok` thread holds an unanswered message until its person continues it;
  the refusal is logged and not written to the thread.
- Bad, because a chat whose last send failed no longer reads as unread to
  another device of its sender: the failure shows by the thread's status
  alone.
- Bad, because a client that follows a queued message past the run's `done`
  must attach again: the next turn is another stream.
- Bad, because a message sent in the moments a run is settling waits for
  that settle before its own turn's stream opens.
- Bad, because ordinal order and replay order differ for one row shape (a
  tool result after the message), and the replay rule is one more thing a
  reader of stored rows must apply.

### Confirmation

`internal/engine/agentqueue_db_test.go`:
`TestAMessageSentWhileATurnRunsIsReadAtTheNextCall`,
`TestSeveralQueuedMessagesAreReadInOrder`,
`TestARepeatedMessageIDOnARunningThreadWritesNothing`,
`TestAStopKeepsAQueuedMessageForThePersonsNextWord`,
`TestAFailedRunKeepsAQueuedMessage`,
`TestAQueuedMessageStartsTheNextTurnWhenABudgetEndsTheRun`,
`TestAQueuedMessageHoldsAcrossARestart`,
`TestOnlyAChatTurnTakesAMessageAndOnlyOneThatNamesItsID`,
`TestAQueuedMessageTakesNoSlotAndItsTurnWaitsForOne`,
`TestContinueTakesUpAMessageNoTurnAnswered`,
`TestAMessageToAThreadWaitingOnAnAnswerStartsATurnAndLeavesTheQuestion`,
`TestATurnIsHeldToWhoeverItReadAMessageFrom`,
`TestTheNextTurnIsHeldToEverySenderItStartsFrom`,
`TestTheLastTurnUnderADeadlineReadsAQueuedMessage`,
`TestAQueuedMessageWhoseCommitAnswerWasLostIsStillRead`,
`TestAMessageInDoubtIsSettledByTheNextCall`,
`TestARunEndsRatherThanReplyPastAMessageItCannotRead`,
`TestTheNextTurnReadsItsPinnedContextUnderEverySender`,
`TestAQueuedMessageIsNotTheThreadsLastMessage`,
`TestTheNextTurnMovesLastMessageToItsReply`,
`TestAnUnansweredMessageGetsItsTurnAfterARestart`,
`TestTheNextTurnRunsOnlyForTheMessagesItWasStartedFor`;
`TestReplayReadsAQueuedMessageWhereTheModelDid` in
`internal/engine/agentqueue_test.go`;
`TestASteeredConversationIsOneEachWireAccepts` in
`internal/llm/steered_test.go`; the wire is
`TestAMessageIntoARunningThreadStreamsItsTurn` in
`internal/api/agents_test.go`.

## More Information

Revisit with 0169 when a repository may be served by more than one process,
and when a sweep can find a settled thread with an unanswered message at a
cost a repository of many threads can pay.
