---
type: breaking
release: v0.91.0
---

# A declaration naming a kind or trait by a bare word is refused

Vocabulary authors are hit: anyone applying kind, function or bundle
documents through `POST /api/v1/vocabulary/apply`, `substratectl apply` or a
catalog import. Five places now take `<authority>/<package>/<name>` only:
a reference's `kind:` pin, a reference's `trait:` pin, a `traits:` entry, and
a function's `permissions.writes` and `permissions.reads.kinds`. Nothing
resolves a bare word any more, not even to the declaring package's own kind.

Before, this property was admitted:

```yaml
owner:
  type: reference
  kind: person
```

From v0.91.0 the apply answers `422` `validation` with a problem like:

```
kind example.com/tasks/task: data.properties.owner.kind: "person" is a bare name, and a kind pin is named in full as <authority>/<package>/<name>; this repository declares samples.substrate.reamde.dev/people/person
```

A `traits:` entry keeps its variant after the identity:
`substrate.reamde.dev/core/temporal(point: dueAt)`.

Stored declarations are not affected. Repository migration
`0001_qualify_bare_declaration_names` rewrites every stored bare name at the
repository's first open under v0.91.0 or later.

## What to do

1. Operators: nothing. The migration runs at boot.
2. Authors: in every document you apply, replace each bare word in `kind:`,
   `trait:`, `traits:`, `writes` and `reads.kinds` with the full spelling the
   refusal lists. `substratectl kinds` prints every installed kind.
3. Accept the upgrades offered for shipped providers and samples; their
   package versions were bumped for this change.
