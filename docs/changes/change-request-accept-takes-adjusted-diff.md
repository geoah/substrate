---
type: feature
---

# A change request's accept takes `adjustedDiff`, the owner's values

The owner's accept of a `substrate.reamde.dev/core/recordpatchrequest` may
carry `adjustedDiff` beside `decision: accepted`. The accept applies it
instead of `diff`, with the same checks, and stores it on the request, where
`diff` keeps what was proposed. `adjustedDiff` replaces `diff` whole: name every
value to apply, since a proposed property it omits is not applied. An
`ifVersion` inside it is the accept's check on the target; without one the
accept checks the request's `targetVersion`. A patch or create request only;
installed code and the policy judge are refused with `403`.

```http
PATCH /api/v1/substrate.reamde.dev/core/recordpatchrequest/r41c
{"ifVersion": 1, "properties": {"decision": "accepted",
  "adjustedDiff": {"properties": {"priority": "urgent", "dueAt": "2026-10-01T17:00:00Z"}}}}
```
