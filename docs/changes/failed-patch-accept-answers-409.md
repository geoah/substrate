---
type: breaking
release: v0.80.0
---

# A failed accept of a `recordpatchrequest` answers `409 conflict` naming its reason

Accepting a change request whose change no longer applies used to answer
with the status of the inner cause, and the message claimed a version
conflict the caller's `ifVersion` never lost. A diff that applied no change,
for example, answered `422 validation` with `substrate: version conflict:
substrate: validation failed: the diff applied no change …`. From v0.80.0
every failed accept (a no-op diff, a target that moved or vanished, a guard,
an emit ceiling refusal) answers one `409 conflict` whose message names the
reason and never says "version conflict". The request stays `proposed` and
carries the same reason as its conflict annotation.

```http
PATCH /api/v1/substrate.reamde.dev/core/recordpatchrequest/noop
{"ifVersion": 1, "properties": {"decision": "accepted"}}

before: 422 {"error": {"code": "validation", "message": "substrate: version conflict: substrate: validation failed: the diff applied no change …"}}
after:  409 {"error": {"code": "conflict", "message": "substrate: the accepted diff did not apply: the diff applied no change …"}}
```

This hits clients and agents that decide change requests.

## What to do

1. Treat `409 conflict` on an accept as "the change no longer applies":
   read the reason from `message` or from the request's conflict
   annotation, then reject or re-propose. Do not retry the same accept.
2. Keep treating a `409` whose message says "version conflict" as a stale
   `ifVersion`: re-read the request and retry.
