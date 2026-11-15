---
status: accepted
date: 2026-11-15
decision-makers: George Antoniadis
---

# 0322. A person pins and archives a thread on their own mark, and a list is narrowed by the records that point at its rows

## Context and Problem Statement

A chat list has no pin and no archive, and a thread is never deleted: it is a
run, and its cost counts. The chat experience plan
([pull request 1363](https://github.com/geoah/substrate/pull/1363), sections
1, 8c and 8d) puts both on the person's mark of the thread, which
[0313](0313-how-far-a-person-has-read-a-thread-is-a-record-of-their-own-and-its-position-only-moves-forward.md)
left for later. Three things had to be settled: what the two facts are, what
an archive holds against while the thread goes on without its reader, and
how a client reads "my chats that are not archived" when that fact is on
another record than the row it lists.

## Considered Options

- The facts: two bools, or the time each was set.
- An archived thread that gets a reply: the engine clears the flag, so it
  comes back to the list; or it stays archived until its person takes it
  back.
- The read: the person's marks first and then the threads by id; a filter
  arm for thread marks alone; or a general arm, a list narrowed by the
  records of another kind that point at each row.

## Decision Outcome

Chosen: two bools only their person writes, an archive only its person ends,
and the general arm, `referencedBy`.

- **The flags.** `pinned` and `archived` on `llm/threadmark`, absent until
  set, bools as `markedUnread` is. A time would have to be the server's, as
  `readAt` is, so it would be a second property beside each flag; and no
  read could order by it, because the lists below are lists of threads.
  Pinned chats are ordered by activity, like the rest. Every rule of 0313
  holds for both: a person writes only their own, a viewer may, and
  `agents:` reaches them, which amends 0313's list of what an app's patch
  may carry. A write of a flag moves no position, and a position moves no
  flag.
- **A mark may hold no position.** A person pins or archives from the list a
  thread they never opened. 0313's "a thread with no mark has never been
  opened" becomes: a person has not opened a thread they hold no
  `readThrough` for. The unread rule is unchanged, and already reads such a
  mark as unread.
- **Only its person ends an archive.** Archiving hides the thread from that
  person's list on every device. It deletes nothing and stops nothing (a
  running thread may be archived, and runs on), its cost counts, and no
  reply, job report or settle clears the flag: the engine writes neither
  flag. A repository holds several people and one set of threads, so a
  person archives chats that are not theirs to follow, and a flag every
  reply cleared would put another person's conversation back in their list
  at each turn. It would also be the engine writing a mark 0313 gave to its
  person alone.
- **What waits on a person is never hidden.** A thread whose
  `waitingOnPerson` is above 0 is listed, counted and notified for whether
  or not its reader archived it, from the thread's own count, which needs no
  mark. The flag stays, so the thread leaves the list again once answered.
- **An archived thread counts nowhere else.** No unread count holds it and
  no reply in it notifies. This binds each client, and the push sender when
  one is built.
- **Saying something ends it.** A client whose person sends a message into a
  thread they archived, or continues it, writes `archived: false` first.
  This is the client's: the send path writes no mark.
- **Archived wins over pinned.** A thread with both is with the archived
  ones, keeps its pin, and is pinned again when unarchived.
- **The read.** `filter.referencedBy` names a kind, a single reference of it,
  conditions on the pointing record (`me` included) and `exists`:
  `{"kind": "…/llm/threadmark", "property": "thread", "properties":
  {"readBy": {"eq": "me"}, "archived": {"eq": true}}, "exists": false}` on a
  list of threads is the chats its reader has not archived. Without
  `exists: false` it is the archived ones, and with `pinned` the pinned
  ones, however old. Marks first cannot drive the list: threads are ordered
  by their own activity, and a thread never opened has no mark. Dropping
  archived threads after the page is cut makes a client page until its
  screen fills, ten pages to show one for a person who archives nine chats
  in ten. An arm for marks alone would cost the same code and serve one
  kind, where `me` was added to the grammar as a general word.
- **Where the arm is taken.** The records list alone. The ranked read, the
  tail, the window read and both aggregates refuse it by name, and so do an
  agent's query tool, an agent's pinned read and a function's `list`: each
  holds a read to an allowlist by `filter.kinds`, and the arm names a kind
  beside them. A scoped token names a kind it reads whole, or, on a list of
  threads, a kind `agents:` reaches through its thread.
- **Versions.** The mark kind is 10 and `records` is revision 9. No
  repository migration: an absent flag reads as off.

### Consequences

- Good, because pins and archives follow a person to every device, and a
  list page is full of chats its reader wants.
- Good, because nothing can hide a question, a proposed change or an
  approval, and nobody's archive is undone by another person's chat.
- Good, because the arm serves any fact kept on a record about another.
- Bad, because a reply in an archived thread reaches nobody who archived it,
  its asker included if they archived it while it ran.
- Bad, because ending an archive on a send is each client's to do, and an
  older client that sends into a thread archived elsewhere leaves it so.
- Bad, because `waitingOnPerson` is not per person: a thread that waits
  shows to everyone who archived it.
- Bad, because the arm costs a probe of the reference index and a read of
  each pointing record for every row the list passes over, so its cost
  follows the rows filtered out and not the page. It is written as subplans
  run per row and never as joins: joined, the planner computed the matching
  marks again for every thread, and 3,000 threads against 6,300 marks did
  not answer in five minutes. As subplans each of the three reads took 30 to
  50 ms there, and 1.2 to 2 s in the minute after the rows were loaded,
  before Postgres had gathered statistics on them.
- Bad, because the grammar has no "not", so the pinned read also answers a
  pinned thread its reader archived, and the client drops it by the mark.
- Bad, because every door that takes a filter has one more arm to refuse,
  and an agent cannot ask for its person's pinned chats.

### Confirmation

`internal/engine/referencedby_db_test.go` holds the arm, a pointer from
before a merge, the read while refs are owed and every refusal;
`threadmarkflags_internal_db_test.go` the two flags, a mark with no position,
that no run moves a flag and the lists with three people;
`internal/api/threadpins_db_test.go` roles, the `agents:` reach and the
other modes' refusals. The console's `agents-pins.test.tsx`,
`thread-list.test.tsx` and `thread-marks.test.ts` hold the list's parts, the
Undo and the client's rules.

## More Information

[Agents](../agents.md#pinned-and-archived-chats) and
[the API](../api.md#what-points-at-each-record-referencedby) describe both.
Revisit when push is built (the sender must apply the two notification
rules), if `waitingOnPerson` becomes per person, and if the arm is wanted
with a repeated reference or by an agent.
