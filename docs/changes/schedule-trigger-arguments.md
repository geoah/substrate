---
type: feature
---

# A schedule trigger's `arguments` reach its function as `input["args"]`

A `substrate.reamde.dev/core/trigger` with a `schedule` source and a
function callable may carry `arguments`, a map checked against the
function's declared `arguments:` at write time and at every fire. One
function can now serve several schedules instead of one entrypoint each.

```yaml
kind: substrate.reamde.dev/core/trigger
metadata:
  id: rollup-weekly
data:
  properties:
    source:
      schedule:
        recurrence: FREQ=WEEKLY;BYDAY=MO;BYHOUR=6;BYMINUTE=0;BYSECOND=0
        timezone: Europe/Athens
    arguments:
      period: weekly
    callable: substrate.reamde.dev/core/function/example.com/rollup/run
```

The body reads `input["args"]["period"]`. A record or webhook source, an
agent callable, or a function that declares no `arguments:` is refused with
`422`.
