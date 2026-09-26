---
type: fix
---

# A map rule carries a mirror reference onto its subject

A map rule that copies a mirror reference into a slot pinned at the mirror's
subject kind stores the subject, and a read now shows the rule's source
record as the value's source, with no alternative. Two mirrors of one subject
land in a repeated target once. A mapping that wants a task's assignee copies
the issue's `github/user` reference, and a path through it
(`assignees[].person`) is refused, naming this spelling (decision record
0106):

```yaml
  map:
    assignee:
      path: assignee      # github/user on the issue, person on the task
```

## What to do

A function that patches the assignee onto a mapped task after the fact can
be replaced by the map rule above.
