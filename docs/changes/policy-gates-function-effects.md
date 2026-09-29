---
type: feature
---

# `recordpatchpolicy` gates function effects through `selector.functions`

An owner can now hold what a function writes for review. A
`recordpatchpolicy` whose `selector.functions` names a function gates each
put, patch or delete that function returns, wherever it runs: a trigger
delivery, a schedule or webhook fire, a drain page, a direct call, or an
agent's tool call. The effect lands as a `recordpatchrequest` written by the
function's actor and stamped with `function`; the target is untouched until
the owner accepts it.

```yaml
kind: substrate.reamde.dev/core/recordpatchpolicy
metadata:
  id: gate-triage
data:
  properties:
    selector:
      functions:
        - crew.example.com/bots/triage
    action: gate
```

A policy without `functions` still speaks for agent writes alone, so an
existing `{}` gate does not start holding trigger writes.

A function's own `confirmation: always` now holds its effects the same way
on every path, not only when an agent runs it as a tool, and refuses a
`merge` or `split` it returns. Installed code can no longer accept a request
that carries `function`; the owner, or the governing policy's judge, decides
it. An effect outside `permissions.writes` is refused, never queued.
