---
type: fix
---

# Dropping a `type: state` property clears the state after a data-loss confirmation

A vocabulary apply that removes a `type: state` property from a kind with
records used to be refused with `state property "status" dropped while 3 live
records hold a state`. Every record of such a kind holds
a state and no write clears one, so the refusal fired on every kind with data.
The drop is now the same lossy `null` step as dropping any other property: it
runs only with the previewed confirmation, and it removes the state from
every live record as one `patch` entry each. `createdAt`, the version history
and every other property stay. The drop is not a transition: it writes no
stamp and runs no `onEnter` effect or `notifies:` resume.

```bash
substratectl apply --allow-data-loss -f note.yaml
# confirming plan 3f9c… at changelog seq 812, which removes values:
#   drops status on notes.example.com/capture/note: its value leaves 3 live records (lossy: the values stay in the changelog only)
```

Without the flag the apply is refused with the `lossy` code, naming the step
and the `planHash` to confirm. On `GET /api/v1/changes?values=1` each
record's drop entry lists the state with its `before` and no `after`. The
shipped boot upgrade still refuses such a drop, because it never runs a lossy
step.
