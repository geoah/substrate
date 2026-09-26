---
type: fix
---

# Applying a `recordmapping` links the sources that already exist

The vocabulary apply that admits, changes or re-applies a `recordmapping` now
links every live source whose subject slot is empty or names a deleted
record, in the same transaction:
one probe candidate links, none mints a subject, and a source that offers
nothing or parks on an ambiguous probe stays unlinked (decision record 0106).
Before, a mirror synced before its mapping existed kept an empty slot until the
provider wrote that row again.

To link sources an earlier release left unlinked, apply the mapping again:
the `recordmapping` document alone is enough, and for a package imported from
the catalog, so is re-importing the sample.

```bash
substratectl apply -f people.yaml    # any batch that names the recordmapping
```

A second apply with nothing left to link writes nothing.
