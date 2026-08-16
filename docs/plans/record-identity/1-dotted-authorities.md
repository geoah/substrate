# Proposal 1: reaffirm dotted authorities

Keep the grammar as it is: an authority is a DNS-style name, hierarchy is
spelled in DNS labels, and a publisher must control a domain. Nothing
changes: the flat reference string, the registry-free split, every
validator, every stored path. The two wants stay unmet: publishing a kind
means picking a DNS name, which is honest only with a domain you control
(nothing verifies control today; proposal 9 makes that gap load-bearing),
and grouping stays one subdomain per group, which is why the shipped tree
has 22 sibling authorities under `substrate.reamde.dev`.

```
kind reference          tasks.substrate.reamde.dev/task
a record's path         tasks.substrate.reamde.dev/task/t1
publisher without DNS   squats: vocab.geoah.github.com is GitHub's name, not geoah's
grouping                firecrawl.bundles.substrate.reamde.dev (a subdomain per group)
```
