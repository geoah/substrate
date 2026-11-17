---
status: accepted
date: 2026-11-17
decision-makers: George Antoniadis
---

# 0334. Every deletion is kept unless a deployment sets how long

## Context and Problem Statement

[0300](0300-a-paced-pass-compacts-what-is-past-each-kinds-retention-and-forgets-old-deletions.md)
has the compaction pass forget a purged record's whole history once its
deletion is older than `SUBSTRATE_DELETIONS_KEPT`, 30 days by default,
whatever the record's kind keeps, and move the repository's horizon to the
line the deletion was on. The same day
[0333](0333-a-mirror-of-an-outside-service-keeps-all-of-its-history.md)
ruled that a mirror keeps all of its history, the owner ruled that a deleted
record keeps its history too: 0333 names the 30-day default as a cost it
leaves, a mirror's deleted items losing their history whatever the kind
keeps. This amends 0300's default and nothing else of it.

## Considered Options

- Keep every deletion by default: `SUBSTRATE_DELETIONS_KEPT` defaults to 0,
  and a deployment that wants forgetting sets a duration.
- Keep 30 days by default, and a deployment that wants every deletion sets
  0.
- Remove the forgetting and the setting.

## Decision Outcome

Chosen: every deletion is kept by default. `SUBSTRATE_DELETIONS_KEPT`
defaults to 0, which already meant "keep every deletion", and the engine's
own default is the same, so a server, a test and the operator's tools that
set nothing all forget no deletion. Nothing then moves a repository's
horizon: it stays at 0, and no resume is told `compacted` for a forgotten
deletion.

A deletion's history is the owner's. An outside service is not its backup:
once the service deletes an item, the substrate's history is the one place
its earlier states remain, and the same holds for a record the owner deleted
here. Forgetting it after 30 days makes losing it the default and keeping it
an operator's setting the owner may not know exists. Removing the forgetting
outright was not taken: it costs nothing while it is off, and a deployment
with a reason to bound what a deletion leaves keeps the way to do it.

A deployment still forgets deletions by setting `SUBSTRATE_DELETIONS_KEPT`
to a positive duration (`720h` is the old default). Everything 0300 says of
the forgetting then holds as written: the bounds, the triggers that hold it
back, the horizon and `410 compacted`, and the deletions a package's record
or a bundle record keeps instead
([0319](0319-the-hands-that-wrote-a-shipped-record-outlive-its-history.md)).

Retention is unchanged: a kind's history past its retention is still
compacted into its state, and bookkeeping keeps its seven days (core's
`trigger`, `triggerrun` and `alert`, and each provider's sync-progress
kind).

### Consequences

- Good, because a purged record keeps what its kind's retention keeps, all
  of its history for every kind but bookkeeping, as a mirror's history
  outlives its service's copy (0333).
- Good, because a client's cursor is never refused for a forgotten deletion
  on a deployment that sets nothing.
- Bad, because a purged record of a kind kept all now grows the changelog
  for good: a repository that syncs a service whose items come and go keeps
  every one it ever held, unless the deployment sets a period or the owner
  permanently deletes the record, which erases its history at once.
- Bad, because a deployment that ran a build with the 30-day default (no
  release carried it) has already forgotten what it forgot: its horizon
  stays where that build moved it, and the history is not brought back.
- Bad, because what a deletion leaves is a deployment's setting, not an
  owner's: an owner who wants a deleted record gone for good deletes it
  permanently, one record at a time.

### Confirmation

`TestDeletionsAreKeptForeverUnlessADurationIsSet` (config) holds the
default at 0 and a positive duration accepted. The engine's forgetting
tests set a period of their own (`WithDeletionsKept`).

## More Information

Amends the deletion period of 0300. The settings are in
[running a substrate](../operations.md#the-compaction-pass).
