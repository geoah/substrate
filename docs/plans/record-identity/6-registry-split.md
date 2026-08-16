# Proposal 6: split against the registry

Keep the flat string, allow raw slashes everywhere, and resolve a stored
reference by matching its prefix against the kinds the repository has
declared (a declaration always precedes the records that use it, so the
fold always knows the candidates; the longest declared kind that prefixes
the string wins). Nothing changes shape and identifiers are literal URLs.
The cost is the property every other proposal preserves: a changelog stops
being readable by inspection, so any reader (a verifier, an export,
another tool) must replay the declarations to parse a reference, and a path
whose kind was never declared is unparseable rather than merely unknown.
Two sharper edges: a kind pruned or quarantined later leaves every stored
reference to it unsplittable (today such a value splits fine and is merely
unknown), and longest-prefix makes parsing time-dependent over immutable
bytes: declare authority `github.com/geoah` with kind `vocab`, store
`github.com/geoah/vocab/note/n1`, later install authority
`github.com/geoah/vocab` with kind `note`, and the same stored bytes now
name a different record. Go ships this trade but pins the resolved split in
a stored artifact (`go.mod`), so its readers never re-ask; a changelog has
no such pin per reference.

```
URL of the publisher    github.com/geoah/vocab
a record's path         github.com/geoah/vocab/note/some/id
the split               declared kinds [github.com/geoah/vocab/note, ...]: longest prefix wins
without the registry    github.com/geoah/vocab/note/some/id   (no boundary is knowable)
```
