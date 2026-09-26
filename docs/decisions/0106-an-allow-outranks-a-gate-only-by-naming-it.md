---
status: proposed
date: 2026-09-26
decision-makers: George Antoniadis
---

# 0106. An allow outranks a gate only by naming it

## Context and Problem Statement

The console's "Always allow this" wrote an `allow` `recordpatchpolicy` for
one agent, kind and verb. Policy composes most restrictive first (refuse over
gate over allow, [0005](0005-the-policy-door-is-deterministic-and-the-judge-recommends.md)),
so the gate that held the write kept holding every later one and the rule was
inert. PR #648 proposes removing the control; issue #671 asks for a rule
that actually outranks the gate, narrow and revocable.

## Considered Options

- An `overrides` reference on an allow, naming the gate it lifts
- An exception list on the gate itself
- Let any allow narrower than a gate outrank it (specificity wins)

## Decision Outcome

Chosen: `overrides` on an allow. Where the allow and the gate it names both
match a write, that gate steps aside and the allow governs. The write door
admits `overrides` only on an allow whose selector names exactly one kind
reference, one op and one agent, and only when it names a live gate other
than itself. A refuse is never lifted, and a gate the allow does not name
still holds the write. Revoking is deleting or disabling the allow.

This beat an exception list because each exception stays its own record: it
lists, versions and revokes on its own, and the gate's owner-authored row is
never rewritten by a console click. It beat specificity because that would
change what every stored allow means on upgrade, and "narrower" has no total
order across kind globs, ops and agents.

### Consequences

- Good, because nothing stored changes meaning: an allow without `overrides`
  composes exactly as before.
- Good, because each exception is its own record, which lists, versions and
  revokes on its own.
- Bad, because one exception answers one gate. A write held by two gates
  needs two allows, or the owner narrows a gate.
- Bad, because a gate edited into a refuse, or deleted, leaves its allows
  standing and inert; nothing sweeps them.
- Bad, because the console cannot write one allow per click yet. A gated
  create or patch request does not record whether the agent called `put` or
  `patch`, and an override names exactly one op, so the card must write two
  allows until the engine records the door verb on the request.

### Confirmation

`TestPolicyAllowOverridesTheGateItNames` and
`TestPolicyOverrideIsRefusedUnlessANarrowAllowNamesALiveGate` in
internal/engine/agentpolicy_db_test.go.

## More Information

Revisit if owners need one exception to lift every gate for a pair, or a
refuse that an owner can except.
