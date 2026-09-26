---
type: fix
---

# Applying a `recordmapping` links the sources that already exist

The vocabulary apply that admits, changes or re-applies a `recordmapping` now
links every live source whose subject slot is empty, in the same transaction:
one probe candidate links, none mints a subject, and a source that offers
nothing or parks on an ambiguous probe stays unlinked (decision record 0106).
Before, a mirror synced before its mapping existed kept an empty slot until the
provider wrote that row again.

To link sources an earlier release left unlinked, apply the package that
declares the mapping again, with the mapping document in the batch:

```bash
substratectl apply -f people.yaml    # the package closure, recordmapping included
```

A second apply with nothing left to link writes nothing.
