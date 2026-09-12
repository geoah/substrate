# The reading-list sample

Package `samples.substrate.reamde.dev/readinglist`: the shipped end-to-end
conformance example. It pulls every link out of a chat message, stands in for
a fetch of each one, has an agent say what sort of page it is, and proposes
what to save and a weekly digest for you to accept, composed entirely from
`bundle`, `kind`,
`trait`, `function`, `trigger`, `agent` and `llmprovider`. If this chain ever
needed a workflow primitive, the core would still be too specific; it needs
none.

`bundle.yaml` is the closure (the `page` and `digest` kinds, four functions and
three agents), `settings.yaml` is its one knob, `digest.yaml` is the empty
digest the rollup writes on and `triggers.yaml` is the delivery wiring. They
are the contract; this file is not. Nothing here goes to the network: the
fetch is a deterministic stub, because the bundle exists to exercise the
machinery.

The chain step by step, the emit ceiling it proves, and the setting it reads:
[docs/bundles-catalog.md#reading-list-sample](../../docs/bundles-catalog.md#reading-list-sample).
