# Proposal 11: scoped names under a registry

One hosted namespace maps scopes to publishers, the way JSR owns
`@scope/name` and npm owns `@org`: a publisher claims a scope once, and
their authorities are dotted names under the registry's domain. Grammar,
split, validators, stored data: all untouched, because this is proposal 1
with the DNS-ownership problem moved to one domain somebody already runs.
Publishing needs a scope claim instead of a domain, and grouping is scope
labels. The costs are social and operational, not syntactic: the project
runs (or blesses) a registry, the registry is a trust root and an
availability dependency for name claims (installs themselves stay files),
and the system gains its first global namespace where today identity is
per-repository. JSR exists because Deno's raw-URL identity failed; this is
the retreat position the JavaScript ecosystem actually took.

```
URL of the publisher    github.com/geoah/vocab         (linked from the scope's registry entry)
scope claim             geoah, registered once at the registry
authority               vocab.geoah.r.substrate.dev    (splits today, byte-identical)
kind reference          vocab.geoah.r.substrate.dev/note
grouping                notes.geoah.r.substrate.dev, tasks.geoah.r.substrate.dev
```
