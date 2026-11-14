---
status: accepted
date: 2026-11-14
decision-makers: George Antoniadis
---

# 0311. How far a person has read a thread is a record of their own, and its position only moves forward

## Context and Problem Statement

Each chat client kept its unread marks on the device, as the thread's
`finishedAt` when it was last seen, so a reply read on one device stayed
unread on the others, and no client knew which message a person stopped at.
The chat experience plan
([pull request 1363](https://github.com/geoah/substrate/pull/1363), changes
1 and 5) makes the position a record. Since it was written, a repository
holds several people
([0274](0274-a-person-signs-in-as-an-identity-beside-the-repositories-and-picks-the-repository.md)),
each an actor with roles
([0275](0275-what-a-person-may-do-is-the-roles-they-hold-and-nothing-is-called-owner.md)),
so the position is one per thread and person, and somebody has to say who
may move it and how a client finds it.

## Considered Options

- A property on the thread.
- One preference record per person holding a map of threads
  ([0132](0132-console-preferences-follow-the-person-and-a-window-fact-stays-in-the-browser.md)).
- A record per thread and person, `substrate.reamde.dev/llm/threadmark`,
  under an id built from both.

For who may write it: a rule on the shipped roles, or a fixed exemption
beside the one check
([0276](0276-one-check-holds-every-hand-that-writes-and-one-list-names-the-control-kinds.md)).

## Decision Outcome

Chosen: a record per thread and person. A property on the thread would move
the thread's `version` and `updatedAt` at every read, which are a client's
test for a current transcript and the list's order, and no rule could hand
out one property of a kind the engine writes. A map on one record grows with
every thread and is one record two devices reading different chats both
write.

- **The mark.** `thread`, `readBy` (the person's `core/actor`),
  `readThrough` (an `llm/message` of that thread), `readAt` and
  `markedUnread`. Its id is `<thread id>.<actor id>`, which the id alphabet
  admits and which is 53 characters for minted ids; a thread id longer than
  101 characters would have no mark, and the engine mints none. A create
  with no id is given that id and the writing person as `readBy`, so a
  client that does not know its actor writes its mark all the same.
- **A person writes only their own.** A person's token and an app's token
  write the mark of the person they act for, found as the one check finds
  whom a hand acts for. An agent, a function and a package write none.
  `thread` and `readBy` do not change after the create.
- **Forward only.** A message's place is its `turn`, then when its row was
  written, then its id. A write naming a message before the stored one keeps
  the stored position, and a write that then changes nothing lands nothing
  and is answered with the stored record. A message of another thread, or
  one that does not exist, is refused. Two devices racing end on the later
  position whichever commits first.
- **Mark as Unread is a flag.** `markedUnread` never moves the position. A
  write whose `readThrough` is at or after the stored one clears it, unless
  the same write sets it.
- **`readAt` is the engine's**, stamped when a write's position is taken. A
  value a write sends is dropped, not refused: a mark write must not fail
  for being late.
- **Unread is derived.** The thread gains `lastMessage`, the newest message
  it held when it last settled (the highest `turn`, never a `summary` row),
  stamped by the engine on every write that settles a thread. A settled
  thread is unread when the mark says `markedUnread`, or its `lastMessage`
  is not the mark's `readThrough`, or there is no mark. No count is stored.
- **What waits on a person is counted on the thread**: `waitingOnPerson`,
  the thread's pending `llm/interaction`, proposed `core/recordpatchrequest`
  and pending `llm/approval` records, counted again by the engine in each
  write that creates or resolves one. The plan called it `waitingOnOwner`;
  [0290](0290-a-propertys-writer-names-types-of-actor.md) already reads
  `owner` as `person`.
- **Roles.** The shipped `viewer` gains one rule, `write` over
  `llm/threadmark`. A viewer reads chats, and reading one moves their mark;
  a rule is a verb over a kind, which is one more reason the mark is a kind.
  This amends 0275's description of `viewer`.
- **Apps.** This amends
  [0195](0195-an-app-talks-to-agents-decides-their-requests-and-tails-the-feed-it-reads.md):
  `agents:` reaches the marks of the threads it reaches, to read, create and
  move (`readThrough` and `markedUnread` alone). No `write:` scope names an
  `llm` kind, so the grammar is unchanged.
- **Reading one's own.** In a filter, a condition on a reference to
  `core/actor` may name `me`, the actor of the person the request acts for.
  A client lists its own marks for a page of threads without knowing its
  actor, and a person who may not read actor records filters by their own.
- **History.** The kind declares `retention: P7D`: once a compaction runs,
  a mark keeps its state and a week of moves
  ([0282](0282-a-kind-declares-how-long-its-history-is-kept-and-a-repository-may-override-it.md),
  [0296](0296-a-records-history-is-compacted-into-a-state-entry-in-the-line-of-its-newest-write.md)).
- **Existing threads are read.** Repository migration 14
  ([0099](0099-a-repository-migration-is-code-the-boot-runs-once-and-records.md))
  gives every thread a chat list shows (every `mode` but `subagent` and
  `judge`) its `lastMessage` and its count, and each person a mark that
  reads through it.

A fixed exemption for the write would have been a fourth operation outside
the one check, where a rule on a shipped role is the mechanism roles already
are.

### Consequences

- Good, because read state follows the person across devices, and a list
  decides unread from two records and no message read.
- Good, because a late write cannot move a position back, so clients need no
  conflict handling for it.
- Bad, because every read is a changelog entry, about one per reply and
  person. Retention bounds it only once a compaction runs.
- Bad, because whoever reads the kind reads how far each person has read:
  a kind is the unit of permission (0275), and nothing hides one person's
  mark from another who reads `llm/threadmark`.
- Bad, because the migration writes a line per thread and a line per thread
  and person at the first open, in one transaction.
- Bad, because `me` is a word in the filter grammar that only a request with
  a token behind it can say.
- Bad, because a role a repository writes later must name the mark kind for
  its people to keep their place.

### Confirmation

`internal/engine/threadmarks_internal_db_test.go` holds the admission rules,
the race, `lastMessage` for each way a run ends and the count;
`repomigrate_threadmarks_internal_db_test.go` the migration and its rebuild;
`internal/api/threadmarks_db_test.go` the roles, the `agents:` reach, `me`
and the REST round trip; the console's `thread-marks.test.ts` the unread
rule.

## More Information

[Agents](../agents.md#read-state) describes the record. `pinned` and
`archived` are meant for the same record later, and are not added here.
Revisit if marks should be private to their person, which is a read rule per
record that nothing in the tree has.
