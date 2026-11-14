---
status: accepted
date: 2026-11-14
decision-makers: George Antoniadis
---

# 0312. A chat message may name its own id, and a thread is continued with no message

## Context and Problem Statement

A chat send whose answer was lost could not be repeated: nothing identified
it, so a client compared message text to guess whether it arrived and
otherwise told the person "it may not have arrived". The chat route takes no
`Idempotency-Key`, because a stream has no outcome to store
([0271](0271-an-error-that-may-follow-a-writes-commit-says-so-and-a-write-of-a-named-record-takes-a-retry-key.md)).
And going on after a stopped, over-budget or failed turn meant sending a
message, so clients sent "Continue where you left off." as the person's own
words, which then stayed in the transcript. The plan is pull request #1363,
section 4.

## Considered Options

- Give the chat route the `Idempotency-Key` header.
- Let the client choose the id of its message's record, and tell a repeat
  from a first send by that record.
- For going on: keep sending a message, written by the client.
- For going on: a route that runs a continuation with no user message.

## Decision Outcome

Chosen: the client chooses the message's record id, and a continue route
runs with no message, because both put the fact in the repository's own
records instead of beside them.

**`messageId`.** A chat body may carry `messageId`, any legal record id. The
engine writes the user `llm/message` row under it, in the transaction that
claims the thread or creates it, as a create that never replaces. So a
record with the id exists exactly when the turn was admitted. A request
whose id names that same message (a user row of the thread it names, or the
row its thread opened with when it names none, of the agent it addresses,
with the text it sends) writes nothing and runs nothing: its stream opens
with the `thread` event and is then what an attach answers. An id that names
anything else is `409 conflict` with the reason `message_id_taken`, the same
for every caller; its words say what the id names only to a hand that may
continue the thread it is on. A key in `idempotency_keys` would answer for
24 hours, is not in the changelog, and would hold no outcome for a stream;
the record is the repository's history, so the check survives a restart, a
restore and any length of time. Two copies that race are settled by the
transaction: one creates the row, the other is refused and then finds it.

**Continue.** `POST …/llm/thread/{id}/continue` runs one turn with no user
message, on the path a resolution's resume takes, streamed as a chat turn.
It is the person's word, so it is the one continuation a `stopped` thread
accepts, and
[0169](0169-a-chat-turn-runs-detached-from-its-client-and-its-thread-is-attached-by-id.md)'s
rule that nothing resumes a stopped thread on its own stands. `error` tries
the failed call again, `stopped` takes the turn up, `overbudget` goes on
under fresh budgets. A `running` thread refuses with the reason
`thread_busy` and one that settled `ok` with `nothing_to_continue`, both
`409 conflict`. Where the stored rows end on the assistant's own text, the
request ends with one engine sentence that is never stored, as the
deadline's last-turn message is not, because providers refuse or answer
nothing to a conversation that ends on the assistant's turn.

### Consequences

- Good, because a lost answer is repaired by sending the same body again, on
  a new chat as on an old one, and the client's bubble and the stored row
  are one record from the start.
- Good, because the transcript holds only what the person wrote.
- Bad, because a message with a chosen id is written in the agent's claim
  transaction and one without is written after it, so the loop has two
  paths for one row until every client sends an id.
- Bad, because a continued reply is a second `assistant` row after the cut
  one, and a client that wants one bubble joins them.
- Bad, because the engine's sentence is text the person never sees, and a
  model may still repeat part of the cut reply.
- Bad, because a copy of a send refused before the first copy committed is
  refused as itself (a cap, a busy thread) and must be sent once more.

### Confirmation

`internal/engine/agentsend_db_test.go`:
`TestAMessageIsWrittenUnderTheIdItsClientChose`,
`TestARepeatedSendWritesNothingAndRunsNothing`,
`TestARepeatedSendAttachesToTheTurnItStarted`,
`TestCopiesOfASendRaceToOneTurn`,
`TestAMessageIDThatIsNotThisMessageIsRefused`,
`TestARepeatedSendHoldsAcrossARestart`,
`TestAGuessedMessageIDSaysNothingAboutAThreadTheHandMayNotContinue`,
`TestContinueTakesUpAStoppedTurnWithNoUserMessage`,
`TestContinueTriesAFailedTurnAgain`,
`TestContinueRefusesAThreadATurnRunsOn`,
`TestContinueGivesAnOverBudgetRunAFreshBudget`; the wire is
`TestThreadContinueStreamsOrRefusesWithAReason`,
`TestChatCarriesItsMessageIDAndABusyThreadSaysSo` and
`TestAMessageIDThatNamesAnotherMessageIsRefusedAsTaken` in
`internal/api/agents_test.go`.

## More Information

Revisit the two paths for the user row once no supported client sends a
message without an id.
