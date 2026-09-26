---
type: fix
---

# Declaring an actor at `tier: machine` releases what it already holds

A property written under an actor that held above the machine tier (an
undeclared `X-Substrate-Actor` name holds at the owner tier) stayed pinned
against mapping recompute after the actor was declared at `tier: machine`.
Now the apply that declares it, and every later apply of the package that
declares it, recomputes every mapped record the actor holds above machine.
`propertyMeta` reports such a row at the `machine` tier.

The release follows recompute's machine-tier rule, so it can delete data:

- A mapped property the actor wrote that no live source offers is deleted,
  unless the kind declares it required. A person the import wrote that no
  mirror links loses its imported mapped values.
- A `merge: union` property keeps only its sources' items: an imported
  address no mirror carries goes.
- A record with no live source whose only other rows are the actor's is
  marked orphaned, and with `SUBSTRATE_ORPHAN_GRACE` set the GC sweep may
  collect it, unmapped properties included.

To keep an import whatever its sources say, write it as a source kind
instead ([docs/projection.md](../projection.md#contributing-a-value)).

A repository where the actor was already declared at `tier: machine` before
this release releases nothing at boot: each record moves at its next source
write. To release everything now, apply the package that declares the actor
again, unchanged. The actor goes in a package you own, header document
included:

```yaml
kind: substrate.reamde.dev/core/package
metadata:
  id: alice.example.com/imports
data:
  authority: alice.example.com
  package: imports
  version: 1
---
kind: substrate.reamde.dev/core/actor
metadata:
  id: importer
data:
  authority: alice.example.com
  package: imports
  tier: machine
```

```
GET /api/v1/alice.example.com/people/person/<id>
  propertyMeta.emails = {manager: "<the winning source's actor>", tier: "machine"}
```

Declaring an actor above the machine tier still pins only what it writes
next. Decision record 0106 has the rule.
