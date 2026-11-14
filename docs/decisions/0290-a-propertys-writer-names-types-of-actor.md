---
status: accepted
date: 2026-11-13
decision-makers: George Antoniadis
---

# 0290. A property's writer names types of actor

## Context and Problem Statement

A property's `writer:` said which hand may write it in three words of its
own: `owner`, `connector` and `oauth`. The write path checked each against
the tier the write ran at, a class (owner, bundle, machine) that every
writer was folded into. Decision 0270 made every hand that writes an actor
record with a `type` (`person`, `app`, `function`, `agent`, `package`,
`system`), so the classes said again, less exactly, what the actor already
says: `connector` meant "installed code", the bundle tier, which a
package's own hand fell inside beside a function and an agent, and `owner`
meant the owner tier, which an app's actor and an assistant's were given
too and only a token's scopes held apart. The 1.0 plan (row 29) asks that
the class of a writer be its actor's type, and that the old words be
refused.

## Considered Options

- `writer:` names types of actor, one or a list, and the three words are
  refused naming their replacement
- Keep the three words as aliases of type lists
- Keep the classes and the words, and map an actor to a class at the check

## Decision Outcome

Chosen: `writer:` names types of actor. It takes one type or a list of them
from the six an actor record's `type` holds; absent, any actor may write the
property. Who may write a property is decided by the type of the hand that
chose the value (`substrate.ActorTypeOf`): the write path compares it with
the list and refuses `403 forbidden` naming the property, the types it
admits, and the hand in words with its type. In this release no hand gains
a guarded property it could not write before.

- **The system's own pass** writes any property: it is the substrate's hand
  for what the engine itself decides or replays, such as the OAuth
  facility's tokens, a run cut short, a migration's rewrite.
- **A value the engine only carries** is held to the hand that chose it. A
  mapping's recompute writes as the system, but it writes no guarded
  property, whoever started it: what the engine keeps of who wrote a source
  (a record's manager, a union's first contributor) is attribution, not
  proof of who chose each value, so it gives no hand a property it could
  not write directly. Nothing shipped maps onto a guarded property. A change-request policy that accepts a request carries the
  request's values for the person whose rule it is, and is held as the
  person is. Every place the engine takes a hand of the system's type is
  listed, as deciding, carrying (with what it is held to) or only naming
  it, and a test holds the list to the code.
- **A scoped token whose hand reads as the person**, one whose client id
  names no app, passes no guarded property, as `owner` refused a scoped
  token.
- **A field of an object takes no `writer:`.** The write path holds a
  property's value whole, so the property's `writer:` guards its fields.
  Enforcing one per field (what replacing the parent may do to a guarded
  field, repeated and keyed objects) is design this decision does not make,
  and nothing shipped declares one; the loader refuses one from dialect 5.

The old words map once, by a fixed rule: `owner` is `person`, `connector`
is `[function, agent]`, `oauth` is `system`. Every door that loads a
declaration (the apply route, the catalog, the boot upgrade, `substratectl
validate`) refuses each of them with `writer "owner" is retired: write
person`, as the envelope refuses a retired key naming the key that took its
job. Aliases would leave two spellings of one meaning in every stored
declaration and every client that reads one. A declaration already stored
with an old word is rewritten at the repository's first open by repository
migration 10, as ordinary record writes, before the stored vocabulary loads:
every stored declaration, whatever its package's admission and tombstoned
ones too, and a field's `writer:`, which never had an effect, is removed.

The grammar is closed: an empty list (it admits no writer), a type named
twice and a word that is not a type of actor are refused at load. A list is
how one property admits more than one type, which `connector` did implicitly
for a function and an agent.

The key's value changes shape, so the vocabulary dialect is 5, and each
shipped package that declares a `writer:` states `dialect: 5` and moves one
version.

This decides only who may write a property. The tiers that still feed a
property's manager mark (step 17) and the checks that only the owner may
start an OAuth flow, approve an app or answer an interaction (step 19) are
not `writer:` and stay as they are until those steps.

### Consequences

- Good, because a property says who writes it in the words the history
  uses for who wrote it, and a reader of an actor record can tell whether
  it could have.
- Good, because the list can say what the three words could not, such as a
  property a person and an app may write but installed code may not.
- Bad, because an assistant's token (0230), which wrote at the owner's
  tier, writes as its app (0270), so a `writer: person` property it could
  write before now refuses it: a sync's `syncPaused` and `syncRequestedAt`
  and what a person sets on an account. The person sets those. A person's
  sign-in and a full-access token write as the person, as before, and a
  scoped app's token was refused them already.
- Bad, because a server from before this release refuses a repository this
  one opened (the dialect), and a client that read `writer` as a string
  reads a string or a list.

### Confirmation

`TestWriterOnEveryShape`, `TestARetiredWriterIsRefusedNamingItsReplacement`
and `TestWriterRefusals` (internal/vocabulary) hold the grammar, a field's
refusal among it; the door tests hold the refusal on each door; the write
matrix holds each of the six types against each value over HTTP on the real
engine, and a scoped token naming no app against each;
`TestAMappingWritesNoPropertyThatNamesItsWriters` holds a mapping's
recompute; `TestEveryPlaceTheEngineTakesTheSystemsHandIsListed`
holds the list of the system's sites; the repository migration's test holds
a stored declaration rewritten, a parked and a tombstoned one among them;
and a tree test fails if one of the three words is left anywhere outside
decision records, the changelog and the migration's test.

## More Information

Replaces the writer roles of 0085 (the `sync` trait's `owner` and
`connector` properties) and of the OAuth facility's `writer: oauth`
properties, by the mapping above. docs/vocabulary.md, "Who may write a
property", describes the key.
