---
status: accepted
date: 2026-08-17
decision-makers: George Antoniadis
---

# 0034. A reference may pin a trait, not only a kind

## Context and Problem Statement

Record 0032 left three kinds carrying `account` as a `to: any` `ownerRef`
edge: `calendar.calendar`, `messaging.conversation` and
`messaging.emailthread`. It converted the twenty provider mirror kinds to a
reference pinned at the bundle's own `account` kind, but it could not convert
these three. Each is deliberately provider-agnostic: one `calendar` kind covers
Google, Notion, Beeper and the rest, and six bundles each declare their own
account kind (`google.bundles…/account`, `linear.bundles…/account`, and four
more). A reference pins with `kind:`, and `kind:` names exactly one kind, so
pinning one would tie the shared kind to one provider. `ownerRef` on a
reference is therefore refused unless it is pinned, and there was nothing single
to pin at, so these three kept the edge and its `to: any` cascade.

## Considered Options

- Pin the reference at a TRAIT the account kinds already share
  (`accountconfig`), and teach the write path and the cascade to read a trait
  pin.
- Declare one shared `account` kind that every provider's account kind
  references or embeds, and pin the three shared kinds at that.
- Leave the three as `to: any` `ownerRef` edges, the state 0032 left them in.

## Decision Outcome

Chosen: a `reference` property may pin a TRAIT instead of a kind
(`trait: accountconfig`), and the three shared kinds carry `account` as a
trait-pinned `ownerRef` reference, OPTIONAL rather than required.

The six provider account kinds already implement `core.substrate.reamde.dev/accountconfig`,
a host-recognized trait: the console's account-configs view is a trait query
and the runner's config resolution enumerates a bundle's accounts as a
trait-scoped read. "Point at anything that IS an account" is exactly what the
shared kinds mean, and the trait already spells it. A trait pin is enumerable
for the same reason a kind pin is: the kinds implementing a trait are a fixed
set the registry lists (`Registry.Implementing`), so `incomingArms` and the GC
cascade can both walk it without reading every row of every kind. That
enumerability is what `ownerRef` requires, so the pin that was refused for
being absent is admitted once it names a trait.

The shared-account-kind option adds a kind that exists only to be pinned, and
every provider account would then carry a second pointer to it or embed it,
which is more machinery than a trait the kinds already implement. Leaving the
edges keeps two spellings of `account` and a `to: any` cascade that reads every
edge row at the account rather than an indexed containment probe.

`account` stays optional on the shared kinds, per record 0021's reasoning: a
person may hand-author a calendar or a conversation that synced through
nothing, and a required account would refuse it. The mirror kinds keep
`required: true`, because a provider mirror row that names no account is a bug.

### What a trait pin means

- **Declaration.** A reference pins `kind:` OR `trait:`, never both; the loader
  refuses both. A trait pin resolves from a bare name to a full trait identity
  in `Finalize`, the way a trait binding does, so a bundle-local trait cannot
  counterfeit a host-recognized one. It leaves `Property.To` empty and fills
  `Property.ToTrait`.
- **Value.** A trait pin supplies no single kind for a bare id to borrow, so
  the stored value is a full `<kind>/<id>` path, exactly as `kind: any`
  requires. The write path (`normalizeReference`) resolves the referent kind
  and refuses one that does not implement the trait.
- **Enumeration.** The GC cascade and the `incoming` view share one reading,
  `referencePinsKind`: a property points at a kind when its `kind:` names that
  kind or its `trait:` names a trait that kind implements. Collecting an
  account walks every `(kind, property)` whose trait pin the account's kind
  satisfies, one indexed containment probe each, the same finite list a kind
  pin produces.

### Consequences

- Good, because disconnecting any provider's account now collects the
  calendars, conversations and email threads it synced, through the same sweep
  that already collects a mirror's rows, with no per-provider code.
- Good, because a shared kind's `account` is checked against the trait: a
  referent must be a kind that implements `accountconfig`, where the `to: any`
  edge accepted a link to anything.
- Good, because `incoming` on an account now lists the shared-kind records that
  name it, which an unpinned pointer could not answer.
- Bad, because a trait pin is a new dialect key that every binary reading the
  closure must understand; a binary that predates it quarantines the authority
  (record 0020), which is why the three declarations bump their authority
  version.
- Bad, because retyping `account` from an edge to a reference is a narrowing:
  the boot upgrade refuses the authority while live `account` edge rows exist,
  and pre-v1 the answer is `mise run dev:wipe`.
- Bad, because a trait pin's referent check costs a registry `Implements` read
  per write, where a kind pin is a string compare.

### Confirmation

`TestOwnerRefTraitReferenceCascade` (`internal/engine/gc_trait_reference_db_test.go`)
declares a trait, two account kinds that implement it, and a shared kind whose
`account` is a trait-pinned `ownerRef` reference. It collects the shared record
when its account is deleted, leaves alone the record on the other account and a
trait reference without `ownerRef`, and refuses a write pointing at a kind that
implements nothing. `TestOwnerRefOnATraitReference` and
`TestReferenceRefusesBothPins` (`internal/vocabulary/ownerref_test.go`) hold
the parse: a trait pin survives and resolves, and both pins together are
refused.

## More Information

This completes the conversion record 0032 began and closes the gap 0032 named:
"pinning them needs either one shared account kind or a pin that names a TRAIT,
neither of which exists." The pin that names a trait now exists.

It supersedes nothing. Record 0021's never-required reasoning for the shared
kinds' `account` survives and is applied here.
