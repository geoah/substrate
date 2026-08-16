# Proposal 4: raw URL authorities, one-segment ids

Let the authority carry raw slashes and take the slashes away from ids
instead: an id becomes one path segment, so a record path splits from the
right with no marker and no registry (the last segment is the id, the
second-to-last the kind name, the rest the authority). Identifiers are
literally URLs, on every surface at once. The costs: a declaration's id can
no longer be its kind reference (it is minted, and the identity moves to a
declared property), a record that embeds its own publisher spells it inside
the one segment or in a declared property, and every stored path whose id
spans segments (every declaration, every published function) misparses
under the new rule (one-segment ids parse the same either way), so this is
a pre-v1
break: repositories are wiped or their changelogs re-minted, and the
re-mint is cryptographic, not textual, because the chain hashes ids and
payload bytes and signs every entry, so rewritten history re-signs under
the current key and the original signatures do not survive. The break is
also wider than declarations: the id alphabet hands `/` to every writer
today. REST list
routing needs the registry (whether the last segment is an id or a plural is
not knowable from the string), while stored paths, always full, stay
registry-free.

```
URL of the publisher    github.com/geoah/vocab
kind reference          github.com/geoah/vocab/note        (name is the last segment)
a record's path         github.com/geoah/vocab/note/n1     (id is exactly one segment)
REST                    GET /api/v1/github.com/geoah/vocab/notes/n1
a declaration's id      minted (k7f2q9x4); the identity lives in a declared property
```
