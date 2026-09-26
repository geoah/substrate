---
type: feature
---

# A `recordmapping` takes `where` to project only some source records

A mapping may name which records of its source kind it covers, one filter
condition object per declared source property. A record outside the `where`
links and mints no subject and contributes nothing; one that leaves it
releases what it projected, and its subject is orphan-marked like one whose
source was deleted. A bare value (`state: open`) is refused: write the
condition object, as `filter.properties` takes it.

```yaml
kind: substrate.reamde.dev/core/recordmapping
metadata:
  id: <authority>/tasks/pullrequesttask
data:
  authority: <authority>
  package: tasks
  from: providers.substrate.reamde.dev/github/pullrequest
  to: <authority>/tasks/task
  property: task
  where:
    state:
      eq: open
  map:
    name:
      path: title
```
