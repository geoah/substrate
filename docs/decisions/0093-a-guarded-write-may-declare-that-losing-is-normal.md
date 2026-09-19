---
status: accepted
date: 2026-09-17
decision-makers: George Antoniadis
---

# 0093. A guarded write may declare that losing its race is normal

## Context and Problem Statement

Two invocations of one provider sync can run over one account at once — an
on-request trigger firing while a scheduled one is mid-drain, or an operator's
`function call` beside either. Both read the account's `syncCursors`, both
advance it, and one write wins: the failure mode is duplicated work (pages
re-read, hydration re-queued), never lost data, and every provider's cursor
object has the same exposure (geoah/mneme-v6 T-033, from the GitHub v16
review). The engine already had `ifVersion` on a put and patch effect, but a
body could not reach the version of the record it was invoked on without
spending a host read, and a lost CAS failed the whole delivery as a conflict —
which parks. Parking a race that two triggers are DESIGNED to have turns a
working system into an alarm every half hour, so nobody used the primitive.

## Considered Options

- A per-record lease the trigger dispatcher takes before invoking a function
  and releases after, so a second delivery for the same record waits or is
  skipped.
- Expose the existing `ifVersion` compare-and-set: carry the invoked record's
  version to the body, and let a guarded effect declare that losing the race is
  an ordinary outcome rather than a failure.
- Leave `ifVersion` as it is and let a losing sync park, to be retried by hand.

## Decision Outcome

Chosen: the compare-and-set, in two parts.

**The version rides the invocation.** A record delivery's envelope carries
`record.version`, and every record `config` injects — a bundle input, and each
`accounts` entry — carries `version` beside `id`, `kind` and `properties`. A
body stamps a guarded write with `host.version(…)` of what it read, at no extra
read and with no budget charge, and the version it stamps is the one the
invocation is actually about rather than whatever a read a moment later returns.

**A guarded effect may declare its lost-race policy.** `onConflict` rides a put
or patch beside `ifVersion`: `park` (the default, today's behaviour — the
delivery fails `conflict`) or `yield`. A yielded race rolls the whole delivery
back, writes nothing, settles as a **skip** with the conflict as its reason,
and moves the trigger's cursor or fire state past it. It is the same outcome a
false `when` guard and an `errClaimedElsewhere` delivery already get: another
invocation owns this work.

The dispatcher lease was rejected, and the reason is not size. **The unit that
must be exclusive is the ACCOUNT, and the dispatcher does not know which
accounts an invocation is about.** A provider's scheduled sweep fires one
delivery that walks every connected account; its on-request twin fires one
delivery per account record; an operator's `function call` names accounts in
`args`. A lease keyed on what the dispatcher can see — the trigger, the fire,
the changed record — gives those three different keys, so the very collision
the ticket describes still happens. Only the body knows what it is about to
work, so the exclusion has to be expressible by the body, over a record,
through the write path that already exists. Leaving `ifVersion` alone was
rejected because the primitive was already there and unused: what was missing
was a way to lose without an alarm.

### Consequences

- Good, because it composes with the existing write path. A yielded effect is
  an ordinary guarded write; nothing new is durable, there is no lease table,
  no expiry and no reaper to get wrong.
- Good, because it covers every door. A trigger delivery, a fire, a paged
  drain's first page and a direct call all stage effects the same way, so a
  sync is protected however it was invoked — which a dispatcher lease could
  never be.
- Good, because a yield still matches `ErrConflict`, so the direct-call door —
  which has a caller to tell — answers the same 409 it always did.
- Bad, because a NON-PAGED body's effects commit only at the end, so the loser
  has already done the work by the time it discovers it lost. The write is
  correct and nothing parks, but the duplicated work the ticket names is only
  avoided by a body that CLAIMS first: a paged sync stakes the record on its
  first page and does the work from the second, which the drain commits between.
  The doc says this in as many words.
- Bad, because a mid-chain yield is not a yield. Once a paged drain has
  committed pages it owns the chain, so a conflict inside it parks like any
  other page error rather than silently stranding the pages already written.
- Bad, because `onConflict` is one more key on the effect vocabulary, and the
  effect key set is closed — a body on an older engine spelling it is refused
  rather than ignored. That is the intended failure: a silently dropped policy
  would park a sync every scheduled pass.

### Confirmation

`internal/engine`'s `TestTwoInvocationsOverOneAccountOneAdvances` is the
acceptance: two deliveries of one sync over one account, both stamping the
version they read; one advances the cursor, the other observes the conflict and
exits without writing, neither parks.
`TestGuardedEffectYieldsForExactlyOneRacer` runs the same race truly
concurrently at the effect-commit seam;
`TestGuardedCursorWriteParksWithoutTheYieldPolicy` holds the policy opt-in; and
`TestOnConflictDecode` refuses a misspelled policy and one without an
`ifVersion` under it.

## More Information

[Functions](../functions.md#two-invocations-over-one-record) is the body-facing
half. The delivery ledger a yielded delivery settles in is
[0064](0064-trigger-bookkeeping-is-a-delivery-ledger-folded-from-the-changelog.md);
the paged drain that makes a claim-then-work sync possible is described in
`internal/engine/functions.go`. Worth reopening if a body ever needs to hold a
record across SEPARATE invocations rather than within one — that is a lease
with an expiry, and it is a different decision.
