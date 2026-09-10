# The web harvester sample

Package `samples.substrate.reamde.dev/web`: the shipped end-to-end conformance
example. It harvests URLs from a message, fetches and classifies each page, and
proposes reading-list and weekly-digest changes the owner accepts, composed
entirely from `bundle`, `kind`, `trait`, `function`, `trigger`, `agent` and
`llmprovider`. If this chain ever needed a workflow primitive, the core would
still be too specific; it needs none.

`bundle.yaml` is the closure (the config and `page` kinds, four functions and
three agents) and `triggers.yaml` is the delivery wiring. They are the
contract; this file is not.

The chain step by step, the emit ceiling it proves, and the config it wants:
[docs/bundles-catalog.md#web-harvester-sample](../../docs/bundles-catalog.md#web-harvester-sample).
