# Proposal 2: percent-encode the authority's slashes

The lane [decision 0014](../../decisions/0014-authorities-widen-only-outside-the-id-alphabet.md)
reserved: an authority may contain path segments, stored with each `/`
spelled `%2F`, a character the id alphabet deliberately excludes. The split
never changes (the stored authority still carries a dot and no slash) and
the mapping back to the URL is exact for any URL, because anything else
outside the stored alphabet percent-encodes too (uppercase, a literal `!`),
at the price of a stored form even further from the URL. The stored form is
unreadable, the wire
double-encodes (a client sends `%252F` for a stored `%2F`), and a
declaration's id can no longer be its kind reference because `%` may never
enter ids, so declarations need minted ids.

```
URL of the publisher    github.com/geoah/vocab
authority (stored)      github.com%2Fgeoah%2Fvocab
kind reference          github.com%2Fgeoah%2Fvocab/note
a record's path         github.com%2Fgeoah%2Fvocab/note/n1
REST                    GET /api/v1/github.com%252Fgeoah%252Fvocab/notes/n1
```
