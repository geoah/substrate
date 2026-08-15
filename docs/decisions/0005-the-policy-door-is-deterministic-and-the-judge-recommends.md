---
status: accepted
date: 2026-08-15
decision-makers: George Antoniadis
---

# 0005. The policy door is deterministic; the judge only ever recommends

## Context and Problem Statement

Between an agent wanting a write and the write landing there was only the
emit ceiling: land or refuse, nothing between. The owner needed a middle
(writes that land only as reviewed requests) and an optional LLM judge with
confidence thresholds, without a model call becoming an authorization
boundary. Alternatives and research are in the plan
([docs/plans/thread-interactions.md](../plans/thread-interactions.md)).

## Considered Options

- A deterministic door (allow/gate/refuse) with the judge running AFTER, over
  the materialized request, engine-routed within owner thresholds
- The judge inside the tool call, deciding the write directly
- Policy in agent declarations rather than owner-editable records

## Decision Outcome

Chosen: the deterministic door plus the engine-run judge over the request.
`recordpatchpolicy` records (owner's hand alone; bundle writes to the kind
refuse) select writes by kind/op/agent; matches compose most-restrictive-
wins; `gate` converts the write into a `recordpatchrequest` with the
governing policy pinned at its revision and the request id derived from the
dispatch's idempotency identity. The judge, when a policy names one, is
TOOL-LESS, reads a typed projection (envelope, proposer, criteria; thread
turns only where `context: thread` opted in), replies a strict
`{verdict, confidence, rationale}`, and the ENGINE decides only past the
owner's `autoAccept`/`autoRefuse`, under the POLICY's actor bounded by the
request's target kind — never under the judge's emit. Everything else fails
closed into the owner's review. This beat judging inside the tool call
because no model latency or outage sits in a tool's path, what was judged is
verbatim what would land, and a judge outage degrades to "a human decides".
Records beat declaration-borne policy because the owner edits policy without
touching installed bundles, and the changelog versions every edit.

Declarations still carry the author's objective facts (`effect`, and a
`confirmation: always` floor no policy loosens), so tightening composes:
ceiling, floor, owner rules, judge, in that order, each only narrowing.

### Consequences

- Good, because no LLM verdict is a security boundary: the deterministic
  layers cannot be talked out of the way.
- Good, because a judged auto-accept is auditable to a policy revision and a
  judge thread.
- Bad, because a gated agent always experiences a refusal-shaped result
  (`ErrGated`) even when the judge lands the write seconds later; the resume
  is how it learns.
- Bad, because policy evaluation reads the policy records per write; a cache
  is deferred until somebody measures the need.

### Confirmation

internal/engine/agentpolicy_db_test.go (the door, composition, the floor,
the owner-only kind) and internal/engine/judge_db_test.go (thresholds,
escalation, advise, strict parsing, armed judges refused, the policy-actor
decision visible in the changelog).

## More Information

Landed in PRs #81 and #83. The research digest in the plan records that no
surveyed protocol has a first-class confidence primitive; the fail-closed
posture is the one thing every harness agrees on.
