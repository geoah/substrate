# Proposal 9: URL provenance, not URL identity

Reframe the want: the substrate never verifies DNS ownership anywhere, and
authorities only collide within one repository, so "publish from a URL
nobody owns DNS for" is a trust-at-install problem, not a grammar problem.
Identity stays the dotted string, publisher-chosen; the authority's
declaration gains a `source` URL, and the installer verifies the URL serves
a well-known document naming that exact authority, the way Go vanity
imports bind a module path to a repository. Both wants land without
touching a stored byte or a grammar copy: anyone with a GitHub repo can
publish (they pick a dotted name and prove the repo claims it), and
grouping is the publisher's naming choice. The costs: dotted names stay
first-come within a repository (exactly as unverified as today, now with a
proof available), the proof is only as live as the URL, and the wants are
met by convention plus one install-time check rather than by the identifier
itself.

```
URL of the publisher    github.com/geoah/vocab
authority (identity)    vocab.geoah.dev            (any legal dotted name; DNS ownership not required)
the declaration         data.source: https://github.com/geoah/vocab
the proof               https://raw.githubusercontent.com/geoah/vocab/main/substrate.json
                        { "authority": "vocab.geoah.dev" }   (checked at install, recorded on the bundle)
kind reference          vocab.geoah.dev/note       (splits today, byte-identical)
```
