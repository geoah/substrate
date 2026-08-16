# Proposal 5: structured references

Stop composing. A stored reference becomes two fields, the kind and the id,
and the composed `<kind>/<id>` string retires everywhere it is stored
(reference values, trigger callables). Nothing needs splitting, so the
authority and the id may both carry raw slashes and identifiers are literal
URLs; a kind reference alone still splits with no registry, from the right,
because a kind name never contains a slash. The costs: it reopens the
settled flat reference value model (one string, chosen over the pair after
three review rounds), every stored reference value migrates, and everything
that today matches one string (query filters, CEL bindings, a grep over a
changelog export) starts handling a pair. Two more costs: the raw wire path
stays ambiguous (ids keep slashes while authorities gain them, so REST must
encode the authority or ask the registry), and dialect 2 just retired the
pair by name (the loader refuses `{kind, id}` as the retired shape and a
boot rung rewrote stored pairs into paths), so this proposal runs that rung
in reverse. In its favor, the store already speaks pairs everywhere but
reference values, trigger callables and declaration ids: the chain's frame,
the fold's ops and the edges table all carry kind and id apart.

```
URL of the publisher    github.com/geoah/vocab
kind reference          github.com/geoah/vocab/note
a reference value       {"kind": "github.com/geoah/vocab/note", "id": "some/id"}
GraphQL (unchanged)     { record(kind: "github.com/geoah/vocab/note", id: "some/id") { title } }
```
