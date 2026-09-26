---
type: fix
---

# `trigger replay` reads only the trigger's kinds from the changelog

A record trigger's drain, and a `trigger replay` from seq 0, read the
entries of the kinds the trigger's source names and skip every other entry
in one cursor step. A replay over one small kind in a large repository takes
time in proportion to that kind's entries instead of the whole changelog
(decision record 0106). A `*` source still reads every entry.

The first boot on this release runs migration 0007, which builds the
`changelog (repository, kind, seq)` index and drops `changelog_kind_idx`.
The build holds writes to the changelog while it runs, so a repository with
millions of changelog entries starts accepting writes a little later than
usual on that boot. Nothing needs doing.
