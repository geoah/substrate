---
status: accepted
date: 2026-09-12
decision-makers: George Antoniadis
---

# 0078. A kind move is ordinary record writes

## Context and Problem Statement

Record 0077 moves four kinds from `core` to a new seeded `llm` package. The
first draft carried the `run` → `triggerrun` precedent across whole: the old
names are not retired, the old rows are not migrated, and a repository that
predates the move keeps its dormant kinds and its old rows.

That was safe for `run` because NOTHING referenced a run. It is not safe here.
Core's `agent` declaration pins `provider` at the provider kind and
`recordpatchrequest` pins `thread` at the thread kind, and the three llm kinds
reference each other. On a repository that ever ran an agent, every agent row
holds a `provider` reference at the old kind; the new declaration pins the new
one; `refTargetNarrows` plus `countRefOffTargetQuery`
(`internal/engine/schemadiff.go`) refuse the upgrade while live rows point
"elsewhere"; and the shipped projection is ALL-OR-NOTHING
(`internal/engine/seed.go`), so the refusal withholds the whole seeded upgrade
and the `llm` package never lands either. The repository is stuck, permanently,
with no door out: the migration the guard demands would have to be performed
through kinds the refusal prevented from arriving. Meanwhile the agent loop
reads only the new kinds, so every stored thread, message and interaction is
invisible to it.

The general shape of the problem is older than this move: a kind that changes
its reference is a rewrite of live data, and the substrate already answers that
class of question one way.

## Considered Options

- Leave the rows behind and tell operators to repoint by hand. Impossible: the
  refusal withholds the kinds they would repoint at.
- Special-case the llm move in the boot upgrade.
- A reserved declaration key, `movedFrom:`, that makes a move ordinary record
  writes the way `renamedFrom:` makes a rename one.

## Decision Outcome

Chosen: `movedFrom:`. A property rename is ordinary record writes (record
0063); a backfill and an enum remap are (record 0066); a kind move is the same
argument one level up, and the machinery it needs already exists: a classified
plan, counted steps, a preview, one transaction.

A kind declaration may carry `movedFrom: <kind reference>`. The key is reserved
by name, not tolerated by prefix (record 0020): it is in the loader's closed key
set, so a binary that did not know it quarantines the package rather than
storing it inert. The loader validates the GRAMMAR (a fully qualified
reference, under the declaring kind's own authority, never the kind itself),
and the engine validates what only the stored side can answer.

It is HONORED ON THE SHIPPED BOOT UPGRADE AND NOWHERE ELSE in this build. The
key is reserved by name everywhere and stored wherever it is written, which is
what record 0020 asks of a reserved key; performing the move is narrower. The
apply door and the catalog's import and install refuse a batch that would imply
one, naming the key, for two reasons: that door publishes its candidate
registry before any rewrite could run, so the registry a caller sees would be
the pre-move one; and the move reaches past the write path's admission rules,
which is a license no repository token holds.

When the shipped boot upgrade admits a kind carrying `movedFrom` whose named
kind the repository STILL DECLARES, then in the transaction that projects the
declaration, before every other conversion:

1. every live record of the old kind is written as a record of the new one,
   same id, same properties, same states, labels, annotations, body, temporal
   columns, property managers and sealed material, as an ordinary `put`,
   coerced and validated against the new declaration. The creation-only
   contracts do not judge it a second time (an interaction's batch, a change
   request's reviewed envelope): the record was admitted once already, under
   the old kind, so a resolved interaction arrives resolved. A SECRET is
   re-sealed under its new owner, because a sealed payload is bound by AAD to
   the record that owns it and a carried reference would be stored as
   plaintext and open as its own name;
2. a reference the moved record itself holds at a kind the same batch is moving
   arrives naming where that kind went, because the declaration it arrives
   under pins the new one;
3. every live reference in the REST of the repository that named the old kind
   is repointed at the new one, at any depth and inside any container;
4. the old rows are tombstoned, each entry naming where the record went;
5. the narrowing counts are taken with the move known (`acceptedTargets`), so a
   reference at a kind this transaction is carrying is not counted as pointing
   elsewhere and no guard refuses an upgrade for rows it is about to move.

The old kind stays DECLARED and empty: nothing prunes it, no name is retired
(record 0055), and the dormant declaration is the only thing that tells an
upgraded repository from a fresh one.

The move is classified from the LIVE rows, so it is idempotent by construction:
the second admission finds nothing to move and plans nothing. The declaration
keeps saying `movedFrom` forever and does nothing forever.

It is a counted conversion step like the others, `StepMove`, so the upgrade
preview says how many records move and where, in the console and in
`GET /api/v1/vocabulary/upgrade`. It runs FIRST in the plan, because everything
else rewrites the rows it carries.

The engine refuses a move it cannot honor, as plan blockers the preview reports
and the boot upgrade skips on, never a silent merge:

- a property the old kind declares and the new one does not, because the rows
  travel with their properties untouched and a value under a key no declaration
  names would be dropped in silence. A property the new kind takes over under a
  new name is refused the same way: move first, rename after, because the
  destination's coercion sees the old key;
- ANY destination row at a moving id, live or tombstoned. A live one would be
  merged into and a tombstoned one resurrected, and both are the write path
  doing something to a record the plan never counted. The destination kind is
  new in every real move, so nothing is lost by refusing both;
- an old kind that a `recordmapping` names, or that declares a `subject:`
  reference. A mapping source's write ENSURES its subject, minting a target
  record where none stands, and the move writes under an admission bypass meant
  for carrying a record that already existed: a mapping firing there would
  create records nobody asked for, outside the plan that said how much moves;
- a reference DECLARATION left pinned at the old kind, whose values can neither
  follow the move (they would leave their own pin) nor stay (they would name a
  kind with no rows). Repin it in the same batch;
- two moves out of one kind, a move whose source is another move's destination,
  and a REQUIRED `mustExist` reference inside a genuine cycle of carried rows.
  Ordering is per ROW and not per kind, because a thread whose `parent` names
  another thread depends on that row and on nothing else of its kind: every
  carried row is written after the rows its own `mustExist` references name,
  across kinds and within one. What no order satisfies is deferred to a second
  pass, and only a required reference in a real cycle is refused.

EVERY reference at the old kind is repointed, whatever its target: the
narrowing guard counts them all, so one left behind would sit outside the pin
the same upgrade installs. A value that was already dangling stays dangling, at
the kind its declaration now admits. Only a reference whose declaration admits
the destination moves; one left pinned at the old kind is the blocker above.
The rewritten value goes through the ordinary reference validation.

A GRANT is the one kind reference the repoint reaches by another road: an
agent's or a function's `permissions` names kinds, and the projection stores
each as a reference at the kind's own DECLARATION record, so the refs index
keys it on `core/kind` rather than on the kind granted. The move rewrites those
by name, as a RECORD write and not a declaration change: the grant says the
same thing about the same kind under the name that kind now has, and the
declaration `version` is what keys the shipped upgrade, so moving it would
claim an upgrade the tree never shipped. Both closures that can hold a grant
re-declare themselves anyway, the seeded ones from the tree at every boot and a
sample's on re-import.

### Consequences

- Good, because a repository that ever ran an agent upgrades unattended, with
  its threads, messages, interactions and provider rows intact and every
  reference to them following.
- Good, because moving a kind is now a thing the vocabulary can SAY, once, for
  everyone: the next package split declares `movedFrom` and gets the same
  transaction.
- Good, because the narrowing guard that would have refused stays exactly as
  strict for every repointing nobody is moving.
- Bad, because a move is a bigger hammer than a rename: it writes a put and a
  delete per moved record and a patch per repointed source, so a repository
  with a large thread history pays for that in the boot transaction. All three
  are counted into the plan's `work`, which is what the conversion ceiling
  bounds (`SUBSTRATE_CONVERSION_CEILING`, record 0067's upper-bound contract).
- Bad, because the moved rows get new `createdAt` stamps under the new kind.
  The changelog keeps the whole history, and the old rows' entries still say
  what they were, but a reader of `records` alone sees the move's moment.
- Bad, because the moved row's put writes as the substrate's own hand, so the
  creation-only contracts and the guards that ask WHO is writing are bypassed
  for it. That is correct, since the row was admitted once already under the old
  kind, and the bypass is scoped to exactly that one put: the deferred patches,
  the repoints and the grant rewrites are judged as the ordinary writes they
  are. It stays sound only while no repository token can declare a move, which
  is why this build honors `movedFrom` on the shipped upgrade alone.

### Confirmation

`internal/testenv/llmmove_db_test.go` is the whole promise end to end: a
repository seeded from the previous shipped tree with a provider, an agent
referencing it, a thread with two messages, an interaction and a change
request; this binary booted over it; every row readable under its new kind with
the same id, every reference repointed, the old kinds declared and empty, the
agent loop's own transcript read answering with both messages, and a third boot
that moves the changelog head by nothing at all.

## More Information

Until the move has run, the loader accepts the pre-move spelling of the ask
grant, exactly as it accepts the pre-move thread pin: a stored package is parsed
at open, BEFORE the upgrade that migrates it, and refusing it there would
quarantine every sample with an ask agent on the boot that was supposed to carry
it. The agent loop itself takes no such fallback: it addresses its kinds by
constant and REFUSES a thread write on a repository whose upgrade has not
landed them, because writing a turn into a dormant kind would put it under
ordinals nothing else counts.

Records 0063 and 0066 are the same answer for a property and for an enum value.
Record 0067 governs when a plan needs confirmation; a move loses nothing, so it
is never lossy and never asks. Record 0055 is why the old kind stays declared.
