---
type: breaking
release: v0.85.0
---

# Provider mirror kinds stop declaring subject slots; the `recordmapping` adds them

Clients reading provider mirrors and authors of source kinds are hit. The
`github` (12), `google` (18) and `linear` (14) packages no longer declare
these references:

- `person` on `github/user`, `google/contact`, `google/emailaddress` and
  `linear/user`
- `assignee` and `task` on `linear/issue`

A `substrate.reamde.dev/core/recordmapping` now adds its `property` to the
source kind when the mapping installs (decision record 0096). With the
`people` and `tasks` samples imported, those samples' mappings add the same
names back, so the values stay where they were. Without a mapping the
property is absent.
A kind read (`GET /api/v1/substrate.reamde.dev/core/kind/<ref>`) serves an
added slot with `managed: true` and `mappedBy`. The stored declaration
does not carry it.

Linear's sync wrote the viewer's own user mirror into `linear/issue.assignee`
when the viewer's address was hidden. It now writes it to `assigneeUser`, a
reference at `linear/user`:

```python
props["assignee"] = USER_TYPE + "/" + host.ids.external("linear", aid, "user:" + vid)      # linear 13
props["assigneeUser"] = USER_TYPE + "/" + host.ids.external("linear", aid, "user:" + vid)  # linear 14
```

Removing a mapping is refused while live records still link through its
slot.

## What to do

1. Read the hidden-address assignee from `assigneeUser` on `linear/issue`.
   v0.88.0 reshapes the Linear bundle again (see
   [its note](provider-bundles-reshaped-in-v0-88.md)).
2. Authors: stop declaring a `subject: true` reference on a source kind; the
   mapping adds it. A source kind that still declares one keeps working.
3. To remove a mapping, delete the records linking through it first.
