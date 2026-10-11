# Apple Health to health

Turns Apple Health mirror rows into observations, traces and health records
under the repository's own authority. Package
`samples.substrate.reamde.dev/healthapple` reads what the Apple Health
provider (`providers.substrate.reamde.dev/applehealth`) receives from the
Health Auto Export app. It requires `health` (and so `observations`) and the
provider; import it after both.

`bundle.yaml` is the closure, one function, `adaptapplehealth`, and
`triggers.yaml` its delivery wiring: `healthapple-on-mirror`, a record trigger
over the provider's `metricbatch` and `workout` kinds on create, update and
delete. They are the contract; this file is not.

The metric mapping, what is skipped, how outputs are keyed and reconciled, and
the one rule an automation setup must follow:
[docs/bundles-catalog.md#apple-health-to-health-sample](../../docs/bundles-catalog.md#apple-health-to-health-sample).
