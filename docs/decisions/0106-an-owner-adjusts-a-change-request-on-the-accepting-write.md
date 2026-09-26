---
status: accepted
date: 2026-09-26
decision-makers: George Antoniadis (via the issue-673 agent session)
---

# 0106. An owner adjusts a change request on the accepting write

## Context and Problem Statement

A `recordpatchrequest` could only be accepted or rejected as proposed
([issue #673](https://github.com/geoah/substrate/issues/673)). An owner who
agreed with most of a suggestion had to reject it and edit the target by hand,
which lost the link to the request and the agent's rationale. The console
redesign (PR #648) needs the change request page to edit the values before
applying them. The envelope, `diff` included, is frozen at propose time
([projection.md](../projection.md#the-patch-request-sibling)), so that the
values a reviewer read cannot be swapped under them.

## Considered Options

- Let the owner rewrite `diff` while the request is `proposed`
- Let the accept carry overrides, stored in a new `adjustedDiff` property
  beside `diff`

## Decision Outcome

Chosen: the accepting write may carry `adjustedDiff`, and the accept applies
it instead of `diff`. The property is admitted as a proposed diff is
(`normalizeDiffFor` against the target's kind), then applied under the
accept's own re-validation (target version and no-op check). It replaces
`diff` whole, so a proposed property it omits is not applied. Its own
`ifVersion`, where present, is the check on the target, so the owner can
accept against a target version newer than the proposal's; otherwise the
check is the request's `targetVersion`.
It is written on the one write that moves `decision` from `proposed` to
`accepted`, by the owner tier alone, never on a delete request, and it is
frozen afterwards. Rewriting `diff` would have opened the freeze to any
pending write and kept only the last value on the record, so the request
would no longer show what the proposer asked for. With `adjustedDiff`, `diff`
stays the proposal, the request row holds both, and the freeze on `diff`
stands unchanged.

### Consequences

- Good, because the proposal and what was applied both stay on the request,
  and the proposing thread's `proposalDecision` message carries the
  adjustment.
- Good, because the freeze on the reviewed envelope stays whole: a callable
  or a judge cannot adjust, and nothing can change a stored adjustment.
- Bad, because there is no saved draft: an adjustment the owner abandons is
  not stored, and the console holds it until the accept.
- Bad, because every reader that wants "what was applied" reads two
  properties, `adjustedDiff` first and then `diff`.

### Confirmation

`internal/engine/adjusteddiff_db_test.go` (admission, apply, create, the
moved-target conflict, the function refusal) and
`TestAdjustedAcceptReportsTheAppliedValues` in
`internal/engine/agentdecision_db_test.go`.

## More Information

Reopen if a client needs to save an adjustment without deciding, which would
make `adjustedDiff` writable while `proposed`.
