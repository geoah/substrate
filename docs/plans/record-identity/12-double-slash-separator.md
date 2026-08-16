# Proposal 12: a reserved separator sequence

Bazel names targets `@repo//package:target`: the separators are
multi-character or from outside the label alphabet, so a label parses with
no registry however many slashes a package carries. The analog: the
authority-to-kind boundary is spelled `//`, and only the FIRST `//` is
structure. The split is deterministic with nothing else reserved: authority
before the first `//`, kind name the next segment (names are slash-free),
id everything after, slashes and even later `//` included, so a
declaration's id stays its kind reference. Every real URL is spellable
because URL paths carry no empty segments in practice, and the engine
already refuses empty segments in ids, so `//` is unclaimed in stored data
(a one-time audit confirms it). The costs: an authority may never contain
`//`, a sequence ban 0014's character-based rule does not cover; the split
itself changes, so every grammar copy changes with it; and a bare kind
reference is told from a record path only by what follows the name, as
today.

```
URL of the publisher    github.com/geoah/vocab
kind reference          github.com/geoah/vocab//note
a record's path         github.com/geoah/vocab//note/n1       (the first // ends the authority)
a declaration's path    core.substrate.reamde.dev//kind/github.com/geoah/vocab//note
REST                    GET /api/v1/github.com/geoah/vocab//notes/n1
```
