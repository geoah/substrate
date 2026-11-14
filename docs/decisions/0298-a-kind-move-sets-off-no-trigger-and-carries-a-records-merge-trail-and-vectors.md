---
status: accepted
date: 2026-11-14
decision-makers: George Antoniadis
---

# 0298. A kind move sets off no trigger, and carries a record's merge trail and vectors

## Context and Problem Statement

[0078](0078-a-kind-move-is-ordinary-record-writes.md) makes a kind move
ordinary record writes. For each carried record it writes a put under the
new kind, marked `created`, and a tombstone under the old kind. Two doors
move records:

- the boot upgrade of the packages a server ships;
- a publish, which carries a package's records out of `~`
  ([0289](0289-a-repository-publishes-a-package-by-moving-it-out-of-the-local-namespace-to-an-authority-it-proves.md)).

Three things went wrong with those lines:

- A record trigger whose source follows the move reads every carried record
  as a creation. Its cursor stands before the move's lines, and only the
  callable's own writes are left out (`matchChanges`). So it runs its
  callable once per record, and a `*` source runs it once more for each
  tombstone. A function that appends records, sends a webhook or pushes to
  a provider does that once per record.
- A merge trail is keyed by kind. The sweep's purge of the old row deleted
  it, so a reference or an address through a merged-away id stopped
  resolving.
- Every carried record was embedded again, at the provider's cost.

## Considered Options

- The lines say they are a move, and nothing that reacts to changes
  delivers them; the trail and the vectors travel with the record.
- Advance every trigger whose source follows the move past the move's
  lines, in the same transaction.
- Leave the lines as they are, and say so.

## Decision Outcome

Chosen: the lines say they are a move. Nothing that reacts to changes
delivers them; the merge trail and the vectors travel with the record.

What each line carries:

- A carried record's put names the kind it arrives from, `movedFrom`, as its
  tombstone already named `movedTo`.
- A line that only moves the names a record holds to where they now point
  says `repointed`, as the move's repoint lines already did. This covers a
  reference repointed at a carried record, and a publish's renames of
  tokens, triggers, policies and views.
- The dispatcher passes over every such line, for every trigger, a source
  of `*` included (`movedLine` in `functions.go`).

What travels with the record:

- Its former ids, as the fold's own `former` effects under the new kind, so
  a rebuild gives them back.
- The record a merge holds as its loser: a tombstone the merge's finalizer
  keeps from the sweep, carried as a tombstone with that hold under the new
  kind. The merge's record points at it there, and a split brings it back.
- Its vectors, and its place in the embedding queue, as derived storage. Its
  text is unchanged, so it is not embedded again.

Advancing cursors was ruled out:

- It would also skip a real change written before the move that a trigger
  had not delivered yet.
- It would rewrite the cursor of every trigger whose source follows the
  move.
- A restore to a cursor before the move would deliver the move again.

Leaving the lines as they are runs a callable once per carried record, which
is what this record is about.

### Consequences

- Good, because a publish or a shipped move changes no record's meaning,
  and nothing reacts to it as if it did.
- Good, because a merged-away id resolves after a move as it did before,
  and a rebuild agrees.
- Good, because a move costs no embedding calls.
- Bad, because a trigger cannot ask to see a move. None needs to today; a
  client that does reads the change feed, which serves `movedFrom`, `movedTo`
  and `repointed`.
- Bad, because a callable that keeps a copy of a reference's text elsewhere
  is not told when that text changes by a repoint. The record says the same
  thing as before, and the copy keeps the old spelling.

### Confirmation

`TestAPublishSetsOffNoTriggerForWhatItCarries` (`internal/engine`):

- the publish fixture's own trigger runs zero times for the carried records
  and once for a record written after;
- a merged record's former id resolves under the published kind, after a
  rebuild too, and the publish carries the merge's loser.

## More Information

Amends [0078](0078-a-kind-move-is-ordinary-record-writes.md) on these three
points. The rest of 0078 stands. docs/stored-format.md lists the three keys
among a payload's.
