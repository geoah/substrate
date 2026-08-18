---
status: accepted
date: 2026-08-18
decision-makers: George Antoniadis
---

# 0037. `gated` is an agent-loop verdict, not a wire code

## Context and Problem Statement

`gated` (409) is a member of the closed error-code set
([internal/api/errors.go](../../internal/api/errors.go)), but no HTTP request
can produce it ([#242](https://github.com/geoah/substrate/issues/242)). The
policy door is a method on `agentMutateDataset`
([internal/engine/agentgql.go](../../internal/engine/agentgql.go) `door`), built
only inside the agent loop, and its three `policyVerdict` call sites each turn a
`gate` verdict into a `recordpatchrequest` and a tool-result string the model
reads. `agentEntryError`
([internal/engine/agents.go](../../internal/engine/agents.go)) does not list
`ErrGated` among the sentinels it passes through, so even an escaped one surfaces
as `422 validation`. The closed set freezes at v1: a published code no request
can produce forces the conformance suite (#210) to excuse it rather than assert
it, and removing it after v1 is a breaking change to a client that switches
exhaustively.

## Considered Options

- Make `gated` reachable over HTTP: give a direct API write a policy door so a
  user's own write can answer `409 gated`.
- Remove `gated` from the published set, keep `ErrGated` as the internal
  sentinel the agent loop's tool result carries.

## Decision Outcome

Chosen: remove `gated` from the published wire set. The policy door is a
bundle-tier concept by design ([0005](0005-the-policy-door-is-deterministic-and-the-judge-recommends.md)):
`internal/engine/policy.go`'s header states "Policy never runs for owner or
machine writes", and the three interactive clients (`api`, `console`,
`substratectl`) all write at the owner tier. A direct HTTP write is never
routed through `agentMutateDataset`, so it has no door and nothing gates it. A
gate is only ever an agent's own write held for the owner's review, and it
resolves inside the loop as a tool result naming the request id. Making `gated`
reachable would mean giving user writes a policy door they were deliberately
kept out of, which contradicts 0005 and 0006, not a small honesty fix.

`ErrGated` stays in `internal/substrate` because `heldForReview` still wraps it
for the loop's tool result; it is no longer a wire code, so
`problemFor` no longer maps it and `internal/api/errors.go` no longer declares
`codeGated`. Removing it now, before v1, is the one time the closed set may lose
a member.

### Consequences

- Good, because the closed error-code set is honest: every code a client can
  switch on is one a request can produce, and the conformance suite asserts each
  rather than excusing one.
- Good, because the door stays deterministic and owner-only, with no new
  synchronous gating path on the REST write surface to freeze at v1.
- Bad, because a future direct-write policy door (if one is ever wanted) has to
  re-add `gated` as an additive change, and a client written against the v1 set
  will not have the case.
- Bad, because `ErrGated` now lives in `internal/substrate` without a wire
  mapping, which reads as a loose end until the comment explains it is
  loop-internal.

### Confirmation

`internal/testenv/conformance_db_test.go` parses the `code*` declarations out of
`internal/api` and requires each to have a producing case; the `gated` entry in
its `unreachable` map is removed, so re-adding `codeGated` without a case fails
the check. `docs/api.md`'s error table lists the twelve reachable codes and no
longer names `gated`, held by `lint:docs` and review.

## More Information

Revisit if a policy door is ever wanted on a user's own direct write, not only
an agent's. That is new design work (a synchronous gate on the REST surface),
and re-adding `gated` to the published set is the additive change it would carry.
