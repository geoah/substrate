---
status: accepted
date: 2026-11-15
decision-makers: George Antoniadis
---

# 0327. A chat message may name the question it answers, and a resolved card links the hand that resolved it

## Context and Problem Statement

An agent's question (`llm/interaction`) takes option values and no prose,
and its kind says prose belongs in chat. So a person who would rather answer
in their own words typed a message, and the client then dismissed the card.
[0318](0318-a-message-sent-while-a-chat-turn-runs-joins-it-and-one-the-run-never-read-starts-the-next-turn.md)
says a typed message "answers and dismisses nothing", and every resolution
resumes its thread, so the agent replied to the message and then took a
second turn for the dismissal: a reply nobody asked for, and its cost. The
chat app's work met exactly that against a real server (the plan is pull
request #1363, section 8e).

Separately, a question, a call waiting for approval (`llm/approval`) and a
change request (`core/recordpatchrequest`) each end by somebody's hand, and
no record said whose. A repository holds several people
([0274](0274-a-person-signs-in-as-an-identity-beside-the-repositories-and-picks-the-repository.md)),
and a client drew "You answered" for all of them.

## Considered Options

For the answer in words:

- The chat send names the interaction, and the transaction that writes the
  message resolves it.
- A resolution of the interaction that carries the text and writes the user
  row itself.
- Leave the two requests, and have a dismissal start no turn when a message
  from the person is waiting or follows.

For whose hand:

- A declared reference to the actor record, stamped by the engine.
- Read it from the mark of the stamped time (`status.properties`), which
  already links the hand that set it.
- A repository migration that fills the link on records resolved before.

## Decision Outcome

Chosen: the send names the interaction, because the message is then the one
event: one request, one transaction, one row under an id its client chose,
and the turn that reads it is the one continuation. A resolution that wrote
the row would need a second send path through the record route: no stream,
no place in a running turn's queue, and a repeat told apart by a version
conflict instead of by the row. A dismissal that guesses cannot know what
follows, and says nothing to the model about what the message is.

**The request.** A chat body may carry `repliesTo`, the id of a pending
interaction of the thread it is sent to. It needs `thread` and `messageId`
([0312](0312-a-chat-message-may-name-its-own-id-and-a-thread-is-continued-with-no-message.md)):
the question is resolved in the transaction that writes the message, which
only a message that names its id is written in.

**What the transaction does.** It reads the interaction under its lock,
writes the user row, and moves the interaction from `pending` to a new
state, `replied`, as the sender, held to the sender's rules as any write of
the kind is. The engine stamps `repliedIn`, a reference to the message, and
`resolvedAt`. The transition declares no `notifies`: no resolution row is
written and nothing is resumed, because the message is what the thread
hears. No write of the record reaches `replied`; only the send does. A new
state, not `dismissed` or `answered`: the batch was not declined, and
`answered` promises a selection for every question. A client that does not
know the state draws it as it draws any state it does not know.

**What the model is told.** The message's turn context
([0196](0196-a-turn-carries-its-own-context-rendered-once-and-stored-on-its-row.md))
gains one entry that names the interaction and says the message is its
answer in the person's words and the question is closed. It is stored on the
row with the rest of the block, so a replay sends the same bytes.

**While the asking run still goes**, the message joins it as any queued
message does (0318): the question is resolved when the row is written, and
the call that reads the message reads the entry. A message the run never
read starts the next turn, once. **Several questions**: a message answers
the one it names, and `waitingOnPerson` comes down by one. **A repeat** is
found by its row before anything else is read, so it resolves nothing and
runs nothing. **A question resolved meanwhile** (another device answered it)
is left as it is and the message is sent as any other, with no entry: the
person's words are not refused for a card that moved. **A question of
another thread**, a sub-agent's included, is refused `422 validation` in the
same words as an id that names nothing, and nothing is written. A
sub-agent's question lives in the sub-agent's own thread, and a message into
that thread answers it there.

**The older client** still sends the message and then the dismissal, and
both still work. It stops paying for the second turn in the case it meets:
a dismissal that lands while a chat turn runs on the thread, after the
person sent a message there since the question was asked, writes its row and
starts no turn. The next turn's replay reads the row. This is narrow on
purpose. A dismissal into a settled thread resumes it as before, and so does
one with no later word from the person, since nothing else would tell the
agent; an answer, a decision and an approval always resume, since they carry
what the agent acts on.

**Whose hand.** Each of the three kinds gains a `managed` reference to
`core/actor`, named beside the time it already kept: `resolvedBy` on an
interaction, `decidedBy` on an approval and on a change request. The write
path stamps it in the write that moves the record out of its waiting state:
the person, the person an app's token acts for, or the function or agent
that decided a change request. A policy rule's decision links nobody; the
request names the rule. The mark already linked the hand, but a view held to
a person's roles drops links to kinds it does not read, and the actor kind
is one; a reference value is part of the record and stays
([0313](0313-how-far-a-person-has-read-a-thread-is-a-record-of-their-own-and-its-position-only-moves-forward.md)
did the same for a mark's `readBy`). A client says "You" with the filter
value `me`, without reading an actor or knowing its own.

**No migration.** A record resolved before has no link, and a client says
only that it was resolved. A repository migration would close the rollback
for every repository to label old cards, and it could not be exact: an
approval's deciding line is not its newest, so compaction
([0296](0296-a-records-history-is-compacted-into-a-state-entry-in-the-line-of-its-newest-write.md))
may already have folded the hand away.

The revision is `agents` 7. Below it a server refuses `repliesTo` as an
unknown field (`400 bad_request`) and a filter on `resolvedBy` as an
undeclared property (`422 validation`).

### Consequences

- Good, because a person answers a question in words with one request, the
  agent takes one turn, and the record says what happened instead of
  "dismissed".
- Good, because a card says whose hand resolved it, for a person who may
  not read actors as for an admin.
- Bad, because an interaction has a fourth state, and a reader that
  switches over the three it knew must take a default.
- Bad, because the quiet dismissal reads what runs in this process: one
  that lands a moment after its turn settled still starts a second turn.
- Bad, because the entry is text the person never sees, in a block the
  model is trusted to read.
- Bad, because a person who cannot read actors learns another person's
  actor id from a card, and no name: a client says "someone else".
- Bad, because cards resolved before this release never say who.

### Confirmation

`internal/engine/agentreply_db_test.go`:
`TestAMessageThatNamesItsQuestionAnswersItInOneTurn`,
`TestOnlyTheSendMovesAQuestionToReplied`,
`TestAReplyNamesAPendingQuestionOfItsOwnThread`,
`TestAReplySentWhileTheAskingRunGoesIsReadByThatRun`,
`TestAReplyTheAskingRunNeverReadStartsOneTurn`,
`TestADismissalAfterThePersonsWordStartsNoSecondTurn`,
`TestADismissalWithNoLaterWordStillResumes`,
`TestADismissalInASettledThreadStillResumes`,
`TestASubagentsQuestionIsAnsweredInItsOwnThread`;
`internal/engine/resolvedby_internal_db_test.go`:
`TestAnAnswerLinksThePersonWhoGaveIt`,
`TestAnApprovalLinksThePersonWhoDecidedIt`,
`TestAChangeRequestLinksWhoDecidedItAndAPolicysLinksNobody`,
`TestARequestAPolicyDecidedLinksNobody`; the wire is
`TestAnAppsReplyAnswersItsQuestionAndLinksItsPerson` and
`TestAViewerReadsWhoResolvedACardWithoutReadingActors` in
`internal/api/agentreply_db_test.go`.

## More Information

This amends 0318's "It answers and dismisses nothing": that still holds for
a message that names no question. Revisit the quiet dismissal when no
supported client sends a message and then a dismissal, and the missing name
when a person's roles may read the names of the people they share a
repository with.
