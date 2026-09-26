---
status: accepted
date: 2026-09-25
decision-makers: George Antoniadis
---

# 0106. A kind declares its purpose

## Context and Problem Statement

The console lists every kind a repository holds. The tree ships 125 of them,
and 30 are things a person browses; the rest are details reached from those
(a calendar series, an email address, a label, a comment) or machinery (an
account, a sync cursor, a provider's configuration, the vocabulary itself).
Each provider install adds a dozen more, so the navigation grows until nobody
can use it. Nothing on a kind says which it is, and the declaration is the
only place that knows why a kind exists: the author wrote it for a reason and
nothing else can recover that reason.

## Considered Options

- A core trait, such as `core/internal`, that a machinery kind binds
- A list the console keeps of the kinds it hides
- Derive it: `source: builtin`, a required `onDelete: cascade` reference, or
  the `accountconfig` and `sync` traits
- A tolerated annotation the loader stores without reading
- A closed kind-level key, `purpose: primary | supporting | internal`

## Decision Outcome

Chosen: the closed key. A kind's `data` may carry `purpose:` with one of
three values: `primary`, a thing a person browses and opens directly;
`supporting`, a detail of another kind, reached from the records it belongs
to; `internal`, machinery. An absent key reads as `primary`, so a kind a user
or an agent declares is listed without anyone classifying it, and the default
is the reader's (`Kind.PurposeOrPrimary`), never written into the stored
declaration. The loader refuses any other value and names the three. The
server stores the key and acts on nothing: no read filters by it and no write
is refused over it. A client reads it off the kind's `definition` to decide
what its navigation lists. Every shipped kind is classified, and every seeded
kind is `internal`.

A trait is a contract of properties a kind promises to declare
([terms](../terms.md)); one with no properties is a flag wearing a contract's
name, and it can say two of the three values at best. A console-side list
knows the shipped kinds and nothing a provider, a sample or a user adds
later, and a second client would keep a second list. Derivation guesses
wrong in both directions: `source: builtin` misses every provider's
`account`, a cascade reference marks a supporting kind and a sync cursor
alike, and the traits say nothing about a mirror's `label`. A tolerated
annotation is what [0020](0020-dialect-keys-are-reserved-not-tolerated.md)
refused: a key nobody validates means whatever each author meant by it.

The word is `purpose` and not `role`, because role already means the writer
role on a property (`writer: oauth | connector | owner`, the one actor allowed
to write it), and one word meaning two things on one declaration is the
ambiguity [terms](../terms.md) exists to prevent.

### Consequences

- Good, because the author says why the kind exists once, in the
  declaration, and every client reads the same answer.
- Good, because a kind nobody classified is shown, never hidden: the
  mistake an unset key can cause is a longer list, not a missing kind.
- Bad, because the key set is closed: a binary older than this one opening a
  repository whose stored declarations carry `purpose` quarantines the
  package that ships it, and for core, whose shipped kinds all carry it,
  refuses the open. It is the cost 0020 names for every new key, paid once
  here, and the rollback path is the newer binary.
- Bad, because a sample already imported keeps the declaration it copied: it
  gains `purpose` only through the upgrade offer, and until it takes it every
  kind in it reads as `primary`.
- Bad, because the three values are a judgment and nothing checks the
  judgment: a provider that marks its main kind `supporting` hides it from a
  console's navigation, and only review catches that.

### Confirmation

`TestPurposeReserved` (internal/vocabulary/reserved_test.go) holds the value
set, the refusal wording, the absent default and the untouched definition.
`TestEveryShippedPurposeIsValidAndTheSeedIsInternal` (kinds/kinds_test.go)
holds every shipped declaration to the three values and every seeded kind to
`internal`. `TestSchemaEvolutionReservedKeysRoundTrip`
(internal/engine/evolution_db_test.go) holds the key through admission, the
stored row and a rebuild from rows.

## More Information

[The reserved keys](../vocabulary.md#the-reserved-keys) documents the key.
Reopen trigger: a server-side reader that wants to act on it, such as a
search that ranks by it, which would turn an advisory key into a contract
and deserves its own record.
