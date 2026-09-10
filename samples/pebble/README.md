# The pebble sample

Package `samples.substrate.reamde.dev/pebble`: voice capture from a Pebble
Index 01 ring. The phone app POSTs each capture to a webhook; the bundle saves
it as a `recording`, and a press-and-hold also writes an `instruction` the
`assistant` agent turns into tasks. It requires
`samples.substrate.reamde.dev/tasks`, which the agent writes into.

`bundle.yaml` is the closure (the two kinds, the `ingest` function and the
agent) and `triggers.yaml` is the delivery wiring: the webhook and the
on-instruction delivery. They are the contract; this file is not.

The endpoint's URL and optional key, the multipart parts and the two gesture
headers, what lands and how to set the ring app up:
[docs/bundles-catalog.md#pebble-sample](../../docs/bundles-catalog.md#pebble-sample).
