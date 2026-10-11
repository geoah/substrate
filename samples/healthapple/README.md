# The healthapple sample

Package `samples.substrate.reamde.dev/healthapple`: the adapter that writes
what the Apple Health provider (`providers.substrate.reamde.dev/applehealth`)
receives from the Health Auto Export app as the `health` sample's records. It
requires `health` (and so `observations`) and the provider; import it after
both.

`bundle.yaml` is the closure, one function, `healthappleadapt`, and
`triggers.yaml` its delivery wiring: `healthapple-on-mirror`, a record trigger
over the provider's `metricbatch` and `workout` kinds on create, update and
delete. They are the contract; this file is not.

The metric mapping, what is skipped, how outputs are keyed and reconciled, and
the one rule an automation setup must follow:
[docs/bundles-catalog.md#apple-health-adapter-sample](../../docs/bundles-catalog.md#apple-health-adapter-sample).
