---
type: feature
---

# A `package` row names who declared it in `declaredBy`

The row the engine creates for a new package carries the managed
`declaredBy` property: the actor of the write that created it, for example
`bundle:core`, `bundle:<authority>:<package>`, `console`, `substratectl`,
`api`, `substrate` or `agent:<authority>:<package>:<name>`. Read it from the
package's record:

```
GET /api/v1/substrate.reamde.dev/core/package/<authority>%2F<package>
```

It is in `properties.declaredBy`. A later apply never changes it, and no
document or PUT may write it. A package created before this release carries
none (decision record 0106).
