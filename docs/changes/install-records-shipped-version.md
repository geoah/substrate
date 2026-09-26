---
type: fix
---

# A provider install records `shippedVersion`, and the upgrade preview measures from it

`POST /api/v1/catalog/{id}/install` of a provider stamps the shipped package
version it took on the package row as `shippedVersion`, and the catalog's
`upgrade` preview offers any shipped closure past that stamp. Before, the
preview compared against the stored package version, so a provider whose
stored version ran ahead of the shipped one (from hand applies before the
install) was never offered the next shipped versions: stored google 35 over
shipped 33 landed 36, and shipped 34 to 36 were never offered. Where the
stamp drives the offer, `upgrade.changes` lists each declaration the install
would change at the version it lands at (stored+1), the package header
included: a header edit (its `description`, its `retired` names) now moves
the package to stored+1 on every door, where before it kept the stored
version. A release that only bumps the package version is not offered. A
sample installed verbatim through the same route stays editable and is not
stamped.

A provider installed before this release carries no stamp and is measured
from its stored version until it is installed once more:

```bash
substratectl install providers.substrate.reamde.dev/google
```
