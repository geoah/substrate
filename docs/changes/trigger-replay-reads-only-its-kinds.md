---
type: fix
---

# `trigger replay` reads only the trigger's kinds from the changelog

A record trigger's drain, and a replay from seq 0, read only the entries of
the kinds the trigger's source names, at most 200 of each kind per batch,
and skip every other entry in one cursor step (decision record 0106). A
replay such as

```sh
substratectl trigger replay extractlinks-on-calendarevent
```

over one small kind in a large repository reads that kind's entries instead
of the whole changelog. A `*` source still reads every entry.

The first boot on this release runs migration 0007 when the engine opens,
before it serves any repository. It builds the `changelog (repository, kind,
seq)` index over the shared changelog table, every repository's entries at
once, and drops `changelog_kind_idx`. The build holds writes to the
changelog while it runs, so on a host with millions of changelog entries in
total every repository starts later than usual on that boot. Nothing needs
doing.
