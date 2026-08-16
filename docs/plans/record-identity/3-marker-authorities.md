# Proposal 3: a reserved marker character

An authority may contain path segments, spelled with a marker from outside
the id alphabet; `!` is the leading candidate (`,` splits YAML flow
sequences, `;` `$` `&` are shell structure, `*` is the type-glob wildcard),
though it carries records of its own: interactive bash history-expands `!`
even inside double quotes, and the Go module proxy already spends `!` as a
case escape in module paths, a neighboring meaning waiting to confuse. Two
placements exist. Inside the authority (`github.com!geoah!vocab/note`) the
record-path split stays byte-identical in every implementation, provided
the host keeps its mandatory dot (`localhost!vocab` would misread as a bare
kind), and the id alphabet gains `!` so a declaration's id stays its kind
reference, which repeals by name 0014's rule that the id alphabet never
gains a character. At the kind boundary (`github.com/geoah/vocab!note`) the
authority reads like the URL, but the split itself changes in every
implementation. Either way the stored form is not the URL, every surface
that shows or imports a URL owes the mapping, and the marker is spent
forever: a URL path segment containing a literal `!` can never be an
authority, and Bazel's forced ecosystem migration off its first marker
(`~`, replaced by `+` for a Windows performance problem found years later)
shows a marker can also be spent wrong.

```
URL of the publisher    github.com/geoah/vocab
inside spelling         github.com!geoah!vocab/note/n1
boundary spelling       github.com/geoah/vocab!note/n1
REST (inside spelling)  GET /api/v1/github.com!geoah!vocab/notes/n1   (! is a legal raw path character)
```
