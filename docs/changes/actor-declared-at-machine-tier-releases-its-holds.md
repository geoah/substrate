---
type: fix
---

# Declaring an actor at `tier: machine` releases what it already holds

A property written under an actor that held above the machine tier (an
undeclared `X-Substrate-Actor` name holds at the owner tier) stayed pinned
against mapping recompute after the actor was declared at `tier: machine`.
Now the apply that declares it recomputes every mapped record the actor
holds, and the sources' values replace the held ones. `propertyMeta` reports
such a row at the `machine` tier.

An import that wrote people as `importer`, then this document applied:

```yaml
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
