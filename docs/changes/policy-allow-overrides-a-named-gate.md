---
type: feature
---

# A `recordpatchpolicy` allow can lift the gate it names with `overrides`

An `allow` with `overrides` naming a gate policy lands the writes both match
instead of gating them, for exactly one agent, one kind reference and one op.
A refuse still wins, and any other matching gate still holds the write.
Before this, an allow never outranked a gate, so "always allow this agent"
had nothing to write.

```yaml
kind: substrate.reamde.dev/core/recordpatchpolicy
metadata:
  id: taskbot-may-put-tasks
data:
  properties:
    selector:
      kinds:
        - samples.substrate.reamde.dev/tasks/task
      ops:
        - put
      agents:
        - crew.example.com/bots/taskbot
    action: allow
    overrides: gate-tasks
```

The write is refused with `422` when the allow's selector is wider, or when
`overrides` names a missing policy, the allow itself, or a policy that is not
a gate. Delete the allow, or set `disabled: true`, to revoke it; disabling is
admitted even after the named gate is gone.
