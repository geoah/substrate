---
type: breaking
---

# `trigger.source` and `recordpatchpolicy.action` are required, and an older binary cannot open the upgraded core

Core's `substrate.reamde.dev/core/trigger` (version 20) now declares `source`
required, and `substrate.reamde.dev/core/recordpatchpolicy` (version 19)
declares `action` required. The engine already refused to run a trigger
without a source or a policy without an action. The declaration now says
so: a form marks both required, and a write that leaves one out answers
`422 validation` naming the property:

```
props.source: trigger requires a value
```

`source` is an `object`, and an `object` property now takes `required:`. A
declaration's key set is closed, and a binary from before this release
refuses that key on an object, so it refuses to open a repository whose core
took this upgrade. The way back is this release or a later one.

A repository still holding a row without the value, left by an older binary,
does not take this upgrade. The boot upgrade refuses the shipped vocabulary
as a whole (core and llm) while one such row lives, logs the count, and opens
the repository on its stored declarations:

```
kind substrate.reamde.dev/core/trigger: property "source" becomes required while 1 live records lack it: declare a default to backfill them, or write them first
```

## What to do

1. Do not roll a server back past this release once it has opened a
   repository.
2. If the server logs the refusal above, or `substratectl catalog` lists it
   under "the upgrade is blocked", list the rows without the value:

   ```sh
   substratectl get substrate.reamde.dev/core/trigger \
     --filter '{"properties":{"source":{"exists":false}}}'
   substratectl get substrate.reamde.dev/core/recordpatchpolicy \
     --filter '{"properties":{"action":{"exists":false}}}'
   ```

3. Delete each one. The engine never ran these rows, so deleting one changes
   no behavior:

   ```sh
   substratectl delete substrate.reamde.dev/core/trigger <id>
   substratectl delete substrate.reamde.dev/core/recordpatchpolicy <id>
   ```

   To keep a policy instead, add `action: gate` (or `allow`, or `refuse`)
   under `data.properties` in the output of `substratectl get
   substrate.reamde.dev/core/recordpatchpolicy <id> -o yaml`, and apply the
   result with `substratectl apply -f`.

4. Restart the server. The boot upgrade runs at a repository's first open
   under a binary, so it lands at the next start.
