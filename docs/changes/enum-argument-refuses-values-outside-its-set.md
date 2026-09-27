---
type: fix
---

# A function's `enum` argument refuses a value outside its `values`

Before this release, an argument declared `type: enum` checked only that the
value was a string, so a call passing `period: wekly` to
`values: [daily, weekly, monthly]` ran the body with the typo. The engine now
refuses it with `422 validation` on the call API, a host call, an agent's
function tool and a schedule trigger write:

```
.period: "wekly" is not one of the allowed values: daily, weekly, monthly
```

A schedule trigger written earlier with such a value is refused at its next
fire, which parks the occurrence after one attempt.

## What to do

1. List parked deliveries on schedule triggers
   (`GET /api/v1/substrate.reamde.dev/core/trigger/{id}/parked`) and look for
   the message above.
2. Correct the trigger's `arguments` to one of the listed values, then retry
   the parked delivery.
