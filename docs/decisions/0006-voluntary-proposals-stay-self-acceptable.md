---
status: accepted
date: 2026-08-15
decision-makers: George Antoniadis
---

# 0006. Voluntary proposals stay self-acceptable; only gated requests refuse bundle decisions

## Context and Problem Statement

The interactions plan's review round demanded that "a bundle-tier actor
never decides a request it proposed", because an agent whose emit covers the
target could otherwise accept its own gate and make the policy layer
decorative. Implementing that blanket rule in phase 3 collided with a
shipped, tested contract (`TestChangeRequestAcceptHeldToEmitCeiling`,
internal/engine/core_db_test.go): a function whose emit covers the target
may stage propose-then-accept in one effect batch, precisely because it
could have written the target directly.

## Considered Options

- Refuse bundle decisions only on POLICY-GATED requests; voluntary
  proposals stay self-acceptable within the emit ceiling
- The blanket rule as the plan wrote it, changing the shipped contract
- No guard at all, trusting the emit ceiling alone

## Decision Outcome

Chosen: the narrow guard. A request carrying the engine-stamped `policy`
property refuses every bundle-tier decision (the policy's own judge path,
`effectCeiling.policyDecision`, is the one exception); a voluntary proposal
remains self-acceptable when the proposer's emit covers the target, because
nothing escalates — the actor could have written the target directly, and
refusing it breaks a legitimate staged-write pattern with no attacker in
the story. The blanket rule defended nothing the narrow rule does not, and
cost a documented behavior. This record is the correction to the plan's
text, which still states the blanket rule.

### Consequences

- Good, because the policy layer's load-bearing invariant (deciding one's
  own gate is no gate) holds with zero collateral.
- Good, because the propose-then-accept effect pattern keeps working.
- Bad, because "the agent approved its own proposal" is a sentence that can
  surprise a reader until they check the emit ceiling; the transcript and
  changelog make it visible, not impossible.

### Confirmation

`TestGatedRequestsRefuseBundleDecisions` and
`TestVoluntaryProposalsStaySelfAcceptable`
(internal/engine/agentpolicy_db_test.go), beside the pre-existing
`TestChangeRequestAcceptHeldToEmitCeiling`.

## More Information

Landed in PR #81, divergence noted in that PR's description. Reopen only if
a real escalation is found that passes the emit ceiling and the gate guard.
