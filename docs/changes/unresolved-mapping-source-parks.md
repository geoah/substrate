---
type: breaking
release: v0.85.0
---

# A mapping source with several candidates, or nothing to offer, is left unlinked

Consumers of mapping targets (a `person` behind every `google/contact`, for
example) are hit. Before v0.85.0 a source record whose probes found no single
match always got a new, empty subject record. From v0.85.0 the engine leaves
the source's subject slot unset in two cases (decision record 0087):

- the probes find several candidates;
- the source offers nothing: no probe value and no mapped value, where empty
  lists and blank strings count as nothing.

A source with something to offer and no candidate still gets a new subject.
Two callers still always get one: a write that names the mirror in a slot
pinned at the subject kind, and a source kind that declares its slot
`required:`.

An unset slot is resolved again on the source's next write, so after the
owner merges the two candidates, the next sync links it.

v0.95.0 adds the `recordmapping` key `onAmbiguous` (`park`, the default, is
this behavior; `oldest`; `mint`). It also adds `filter.ambiguous` and
`substratectl get <kind> --ambiguous`, which list sources left unlinked by
several candidates:

```
substratectl get providers.substrate.reamde.dev/google/contact --ambiguous
```

## What to do

1. Treat an empty subject slot on a mapping source as a normal state, not an
   error.
2. On v0.95.0 or later, list waiting sources with `--ambiguous` and merge the
   candidates they name.
3. To keep the old behavior for one mapping, set `onAmbiguous: mint` on it
   (v0.95.0 or later).
