# Apple Health to health

Turns Apple Health mirror rows into observations, traces and health records
under the repository's own authority. Package
`samples.substrate.reamde.dev/healthapple` reads what the Apple Health
provider (`providers.substrate.reamde.dev/applehealth`) receives from the
Health Auto Export app. It requires `observations`, `health` and the
provider; import it after all three.

`bundle.yaml` is the closure, one function, `adaptapplehealth`, and
`triggers.yaml` its delivery wiring: `healthapple-on-mirror`, a record trigger
over the provider's `metricbatch` and `workout` kinds on create, update and
delete. They are the contract; this file is not.

Every output carries `adapter: healthapple@<version>`, the `ADAPTER` constant
at the top of the function, apart from `revision`, which is when the
provider received the row. A change to what a mirror row becomes raises the
constant. After an upgrade, replay the trigger from the start
(`bin/substratectl trigger replay healthapple-on-mirror --from 0`): every
mirror row is delivered again, and each output is rewritten under the new
version at the revision it already holds. Rows the app never resends are not
reprocessed any other way.

The metric mapping, what is skipped, how outputs are keyed and reconciled, and
the one rule an automation setup must follow:
[docs/bundles-catalog.md#apple-health-to-health-sample](../../docs/bundles-catalog.md#apple-health-to-health-sample).
