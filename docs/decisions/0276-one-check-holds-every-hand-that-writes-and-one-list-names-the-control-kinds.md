---
status: accepted
date: 2026-11-14
decision-makers: George Antoniadis
---

# 0276. One check holds every hand that writes, and one list names the control kinds

## Context and Problem Statement

The 1.0 plan's F5 ([#1313](https://github.com/geoah/substrate/pull/1313))
found every kind of writer checked in a different place: a function's
writes only against the list its declaration names, so a declaration that
listed all of `core` could write a policy or a trigger; an agent's against
its own list and a refusal of the policy kind; an app's against its scopes;
a person not at all. With roles ([0275](0275-what-a-person-may-do-is-the-roles-they-hold-and-nothing-is-called-owner.md)),
a person with limited access could ask an agent to write what they may not.

## Considered Options

- One check every write passes, each hand's rules and those of the actor it
  acts for, with one list of the kinds no glob reaches.
- Keeping each writer's check, and adding a person's roles to each.

And, for whose rules hold a run's work once its request is over:

- Each record the run writes carries them, written by the engine, and
  work reads its own record's.
- Reading them from the changelog line that started the turn: its token
  link, looked for back through the thread's history.
- Code's writes in a run naming the app's token, so the creating line
  would carry it.
- The app that opened the thread, for the thread's life.

## Decision Outcome

Chosen: one check. A write is allowed when the rules of the hand that
writes allow it and, where it acts for another, that actor's rules allow it
too: never more than both. It runs in the engine's write path for every
write a hand makes (`txn.authorizeWrite`), and before the doors that write
in a transaction of their own on a hand's behalf (the declaration door, the
agent's vocabulary tool).

- **Each hand's rules:** a person's are the roles they hold; an app's are
  its token's scopes, held by the API's scoped view, so the engine holds an
  app's token to no control kind and to the person it acts for; an
  assistant's reach everything but sign-in material (decision 0230); a
  function's and an agent's are the writes their declaration lists, which
  the dispatcher holds every write to, so the engine holds them to the
  control kinds those writes name exactly and to no sign-in material; a
  package's own hand writes what its install ships; the system's is
  everything.
- **What holds a run is read from the run's own records, in the engine,
  each time it reads or writes**, and no door decides it once
  (`internal/engine/run.go`). Every read and every write of a run's code
  passes its code's declaration; the roles, now, of the person its turn
  acts for and of a person who approved a call it runs; and the grant, now,
  of the app its turn works for and of an app that approved such a call. A
  read is held as a write is: the query tool, a direct call of the query
  function, a turn's context records, pinned context, mentions, the edit
  tool and a function body's host reads.
- **Whose rules hold a run's work is stored on the run's own records**:
  `heldTo` on `llm/thread`, `llm/message`, `llm/interaction` and
  `llm/approval`, which the engine alone writes (`writer: system`): the
  person the work acts for (`onBehalfOf`), the persons who continued it
  (`continuedBy`) and the tokens (`tokens`), each a link. A record the run
  creates takes the holders of the work writing it, a sub-agent's those of
  its parent's turn. A thread's is rewritten at each turn's claim, to that
  turn's. A person or an app that answers, approves or declines an item is
  added to it in the same transaction, and the holders before stay; the
  message that reports the outcome into the thread carries the item's, so
  the resume it starts is held the same way. A token among them, revoked,
  refuses the rest of the work, a continuer's included.
- **Work reads its own item's `heldTo`**: an approved call its approval's,
  a job its tool row's, a resume its report's. It never falls back to the
  thread's, which names the turn running now and may be another person's,
  and nothing looks for a token in the history. A run a restart cut short
  in the middle of its turn is the one reader of the thread's: it is that
  turn's.
- **An item with no `heldTo` was written before this release**, when
  nothing recorded it: its work is refused (`not_permitted`, "ask again"),
  the report of the refusal carries none either, so no resume runs from it
  for nobody, and the person asks again. A present, empty `heldTo` is work
  nobody asked for, a trigger's or a function's, held to its code's
  declaration alone. A read of a holder that fails is an error, and a
  recovery that meets one settles nothing until the next pass.
- **`heldTo` is the engine's bookkeeping, and it names tokens**: the view a
  person who is not an admin reads through, and an app's, cut it from every
  record they return and from history values.
- **A hand that continues a run** (answers its ask, approves its call,
  decides its proposal, writes its next message, stops its turn) is
  admitted only where it could have started that run itself, by its rules
  now: its roles, and an app's grant, cover what the run's agent reads
  (0214's rule, held in the engine where the continuation is written). The
  person whose own turn it is continues it whatever the agent reads.
- **A read reaches nothing it was not given.** A query whose kinds resolve
  to none returns nothing, never every kind; whether a proposal's target
  exists is a read, held to the agent's reads and the run's holders as the
  query tool is, so an agent proposes a patch or a delete only of a kind it
  declares reading.
- **Installed code never writes a policy**, whatever its declaration names:
  a function's effect, an agent's write, a package's own hand, a sync and a
  policy's decision alike. A policy is a gate, and one code could write is
  one it could open. It is a fixed rule beside the one check, as on the
  release before; a person writes policies, by their roles.
- **What is not held to the run's holders is a closed list, with its
  reasons.** A function's configuration, its resolved inputs: the code's,
  read for the code; the output scrubber removes secret-typed values and
  live access tokens alone, by exact value, so a body can return a plain
  configuration value to a caller who may not read the input record, which
  installing the function, an admin's, answers for. An agent's instruction
  records (`llm/instruction`): the owner's standing direction, in every
  run's prompt whoever drives it. The repository's time zone, the one
  setting `substrate.reamde.dev/llm/timezone` (a `core/setting` row): every
  turn's context and every report into a thread states the time in it, and
  the zone says nothing a person could not learn from the times they read.
  The provider and model rows an agent runs on, their keys among them:
  the engine reads them to buy the run's completions and hands them to no
  code and no model, and the agent's declaration names them. The record a
  trigger delivers, handed to its callable as the envelope: the trigger's
  to hand over, and a trigger is a control kind, so who may set one up is
  an admin. Everything else a run reads is held. The pinned block, a
  stored snapshot of reads a run replays, is a read like any other: it is
  stored under whose work it was and what its reads' own scope answered
  for each pinned kind, taken from the answer those reads used, and
  replayed only while the same function answers the same now; a scope
  that cannot be read replays nothing, and no list of roles or grants is
  kept beside it.
- **The record of a run is exempt, by operation, three of them**: the
  loop's own write of its conversation (thread, messages, approvals, jobs,
  and the resolution row an answer writes), the ask's question, and a
  trigger run's own record. They are held to the hand that writes them
  alone, so a chat a viewer starts keeps its record and writes nothing
  else. Every place a write skips the check, an exemption or the engine's
  own bookkeeping, however it takes it (a flag, a transaction opened with
  it, an internal write), is listed with what it writes
  (`onecheckskips_test.go`), or, where it names the system's hand, in the
  inventory of those (`systemhands_test.go`).
- **A change request is no held kind**: its judge reads the person and the
  token from the request's own line, so a proposal a background turn made
  is held to its person alone.
- **Two routes stay an admin's at 1.0.** Merge and split: a merge rewrites
  references wherever they are, in kinds the person may not write among
  them, so "the editor writes the kind" does not cover it. `/sync/status`:
  every shipped sync sits on an account kind, a control kind, and a status
  read through the person's rules for an ordinary kind that implements the
  sync trait is not worth its surface now.
- **A failed read of any of this is an error**, never a wider answer.
- **The control kinds are one list, by class** (`vocabulary.ControlClassOf`):
  triggers, policies, providers and their models, settings (a package's
  settings, the kinds its bundle takes as inputs, accounts), applets,
  declarations, actors and roles. A pattern reaches one only by naming it;
  sign-in material is held the same way. A kind's description says its
  class (`control`). A repository's override of how long a kind's history
  is kept (`core/retention`, decision 0282) is one, of the declarations
  class: it stands in for a key of the kind's declaration, a record only
  because a token may not edit a declaration it does not own, and
  shortening it removes history, which is what a person does to the
  repository itself and so an admin's. It is an internal core kind, which
  a role's glob reaches; that is the reason it is named here.
- **A change request is not a control kind.** It is the gate: it writes
  nothing until it is decided, and its accept, adjusted or not, writes the
  target through the ordinary write path as the hand that decides, held to
  that hand's rules. So code proposes what it may not write itself, and a
  person accepts or adjusts a request only where they could write its
  target.
- **A refusal by the check** is `403 forbidden` with the reason
  `not_permitted`.

Keeping each writer's own check was the failure F5 found: two layers that
do not protect the same things. Holders on the records beat the others.
Read from lines, they were looked for: a search bounded at five hundred
lines that a long turn outran, a token link that code's lines do not
carry (0270), and a thread's newest turn taken for an older item's, so a
person's message between an app's call and its approval lent the call the
person's rules. Making code's writes name a token changes what every
line's token link means. A thread capped for ever by whoever opened it
surprises its person, protects little (an app can as well leave text in a
thread the person opened, or in any record an agent later reads), and dies
with the app's token.

### Consequences

- Good, because an agent or a function a person starts can never write what
  that person may not, and a declaration's glob no longer reaches a trigger.
- Good, because one function answers whether a role reaches a control kind
  (`scope.Roles.Reaches`), so a class key can be added there alone.
- Bad, because a function or an agent that wrote a control kind through a
  glob in its declaration must now name it; every shipped package already
  does.
- Good, because a role reduced or an app's token revoked holds from the
  run's next read or write, not from its next door.
- Bad, because an app's own requests stay the view's, not the engine's:
  the rows an agent verb reaches are read there alone, so the engine holds
  an app's runs and the view its own requests.
- Bad, because every read and write of a run reads the token row and the
  roles again, a lookup per check.
- Bad, because an ask, an approval or a job asked before this release is
  refused once and asked again: nothing recorded who held it.
- Bad, because a function can still hand a caller a plain configuration
  value the caller may not read; the scrubber is no read check.
- Bad, because a write a policy holds for a person may now be decided by
  an editor where they could write its target, where only the owner
  decided it before.

### Confirmation

`TestEveryActorTypeAgainstEveryControlKind` (internal/engine): every actor
type against every class, and the acting-for case of each, failing on a
type or a class without its place. `TestAnAgentAViewerStartsWritesNothing`,
and the API's `TestThePersonsViewAndTheEngineAgree`. The run tests in
`internal/engine/runholders_internal_db_test.go`: an app's turn and the
person's next, two apps' turns of one thread, a role reduced between two
turns, a token revoked between two reads and between two turns, an
approval by an editor, an answer by one who could not start the run, the
ask's exemption, an approved call after a restart, and a sub-agent through
its parent. The holders' tests in `heldto_internal_db_test.go`: what each
record carries, an item from before the release, work nobody asked for, a
continuer's token revoked, an approval after another hand's turn in both
orders, a function's thread, five hundred messages, a sub-agent's approval
after the root's next turn, a job outliving two turns, a failed actor read
in recovery, a stop, and a proposal's probe. `TestEveryPlaceAWriteSkipsTheOneCheckIsListed`
fails on a skip not listed.
