---
status: accepted
date: 2026-11-14
decision-makers: George Antoniadis
---

# 0306. A record has a forget door that erases its history before its current state

## Context and Problem Statement

A person may want a live record's earlier values gone while it stays, and
may need to finish an erasure that waits on a trigger. Step 24 of the 1.0
plan (#1313) needed a door for both, with the same machinery as a permanent
delete ([0304](0304-a-permanent-delete-erases-the-records-history-and-the-intent-is-in-the-history.md)).

## Considered Options

- A field on the delete request.
- A verb on the record, as pin and release are.

## Decision Outcome

Chosen: a verb on the record, `POST /api/v1/<kind path>/<id>/forget`, in the
`records` feature at its next revision, because it acts on a live record and
a delete request is the wrong place for that. It is the owner's alone: the
check reads the owner's hand, which the roles make an admin's.

On a live record it writes a line that carries the intent to erase the
record's history up to it, and the same for every mapped target holding a
copy derived from it, then erases: the record's own lines become one state
entry with its current values, and nothing else changes. The horizon does
not move, since no deletion is forgotten, and a trigger behind meets the
record once, at its state. A parked failure or a drain on one of its lines
is kept, since the record lives and the retry still means something: the
erasure waits for it, and the reply says so.

On a record whose erasure waits, it finishes it now, with `finish` passing
every trigger the erasure would wait for; a line known not to verify still
holds it, and the reply names the seq.

### Consequences

- Good, because a person can drop a record's past without losing it, and
  finish an erasure a trigger holds without an operator.
- Bad, because a feature gains a route, and a client that offers the verb
  checks the revision first.
- Bad, because finishing an erasure past a trigger means that trigger never
  sees the record's values for the change it had not been delivered.

### Confirmation

`TestAnErasureWaitsBelowALineKnownNotToVerify` forgets a live task through
the door; the route sits in the feature table and its golden.
