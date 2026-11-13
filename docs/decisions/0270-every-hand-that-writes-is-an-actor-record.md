---
status: accepted
date: 2026-11-13
decision-makers: George Antoniadis
---

# 0270. Every hand that writes is an actor record, and a write links three of them

## Context and Problem Statement

A write named its writer by a spelling: `console`, `substratectl` and `api`
for a person's requests, the first two claimed by the client in an
`X-Substrate-Actor` header nobody checked, and `function:…`, `agent:…`,
`bundle:…`, `app:…`, `substrate` and `substrate.oauth` for the rest
([0025](0025-an-actor-carries-the-full-authority.md)). The spelling was
stored on every history line, in every manager row and in stored values
(a package's `declaredBy`, a blob's `createdBy`, a call run's `caller`), and
the token behind the write sat beside it as `principal`. Nothing said what a
spelling was: a reader parsed it, a function that was uninstalled left
spellings that named nothing, and an app's write said nothing of the person
whose access it used. `core/actor` was a declaration kind four of whose
records core shipped. The finding is F3 and F5, and the design is "Shared
repositories", in
[pull request 1313](https://github.com/geoah/substrate/pull/1313), the 1.0
plan's step 15.

## Considered Options

- Keep the spellings, and document their grammar as the contract.
- An actor record per hand, keyed by a minted random id, written at first
  write.
- An actor record per hand, keyed by an id derived from the hand, written by
  the engine, linked from every write together with who it acted for and the
  token behind it.

## Decision Outcome

Chosen: an actor record per hand with a derived id, because a link names
what the writer is and survives its code, and because a derived id lets a
history written before the records exist be rewritten one line at a time.

- **An actor is a record of `substrate.reamde.dev/core/actor`**, with a
  `type` (`person`, `app`, `function`, `agent`, `package` or `system`), a
  `name`, one link for the types that have one (`function`, `agent`,
  `package`, and an app's `publisher`), and `disabled`. It is a data kind
  the engine writes; a package that ships an actor document is refused, and
  the vocabulary dialect is 4.
- **The engine writes it** when the hand comes to be: the person and the
  system at the repository's creation, an app at its approval, a package and
  its functions and agents at the install, and any other hand at its first
  write. Uninstalling a package disables its actors; nothing deletes one, so
  an old write still links it.
- **Its id is derived from the hand** (`vocabulary.ActorID`): the same in
  every rebuild, import and repository, and opaque.
- **A write records three links**, on its history line and its change event:
  `actor`, the actor that wrote it; `onBehalfOf`, the person, for an app's
  write and for installed code or the engine a person's request started; and
  `token`, the token a person's or an app's write stood behind. `principal`
  is retired. The change event carries `token` on the owner's reads alone.
- **Work that goes on after its request returned acts for whom its own
  record's creating line names**: a background job its tool row's, an
  approved call its approval's, a resumed turn the resolution's that starts
  it, a judge the request's it decides, and a settlement after a restart the
  same, a thread's by the line that gave its run the lease. Never the
  thread's first line, which other turns continue. A line that names no one
  (unattended work, history from before this record) acts for no one.
- **Every stored attribution is a link**: a manager row's and an offer's
  actor, a package's `declaredBy`, a blob's `createdBy`, a call run's
  `caller` (its token in `token`), a payload's `managers` and
  `triggeredBy`. A message names the callable a tool call ran by its record
  path, and a stopped turn's reason by the type of actor that stopped it.
- **No request names its actor.** The header is not read: a full-access
  token writes as the person, an app's as its app. A client tells its
  sign-ins apart by the token, which History now splits a run on.
- **Existing histories convert.** The changelog conversion to dialect 3
  rewrites each line's spelling to its id and `principal` to `token`, and
  repository migration 9 retires the actor declarations and writes the
  record of every hand the repository's history names. Every spelling the
  engine stored, in the line or in a value, is read by the rule and the
  evidence of the line's own actor: behind a token, any text the retired
  header did not refuse is the person, whatever it looks like.

The engine keeps the hand in memory (`substrate.Actor`) to decide a tier, a
label namespace and a trigger's echo. Label and annotation keys keep their
`<actor>/<name>` namespaces. The writer classes and the `writer:` words are
the plan's step 16, and `pinned` the step 17 that replaces
[0113](0113-a-manager-row-holds-at-its-actors-live-machine-tier.md), whose
re-declared actor this record already makes impossible.

This record supersedes 0025 and amends
[0194](0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md):
an approved app is an actor record, and its writes act for the person.

### Consequences

- Good, because History, a property's manager and every stored attribution
  read what wrote them from one record, and a removed function still has
  one.
- Good, because an app's write says whose access it used, and a person's
  sign-ins are told apart by their tokens rather than by a label the client
  chose.
- Bad, because every client that read a spelling reads a record now: the
  console keeps the actor records in one cached read, and a function body
  that read `change.actor` reads an id (it gains `actorType`,
  `actorPackage` and `onBehalfOf`).
- Bad, because a function or agent a person's request started before this
  record carries no `onBehalfOf` in its history: no line said who started it.
- Bad, because a spelling no hand derives, which no history written by a
  release should hold, converts to an id with no record.

### Confirmation

`TestNoHistoryLineChangeEventOrStoredValueHoldsAnActorAsText` writes a
repository through every type of actor and holds each line, change event and
stored attribution to a link, and
`TestEachWriteRecordsItsActorOnBehalfOfAndToken` (`internal/api`) holds the
five rows of the plan's table over HTTP. `TestAnActorDocumentIsRefused`
(`internal/vocabulary`) holds the loader, and the conversion's tests
(`internal/engine/actorlinks_test.go`) what each old line becomes.

## More Information

[Actors](../api.md#actors) describes the records and the three links, and
[the line](../stored-format.md#the-line) the history line's keys.
