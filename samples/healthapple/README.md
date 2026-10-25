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
delete. They are the contract; this file is not. The trigger declares
`fromStart`, so the import creates it with its cursor at the changelog's
start, and the rows the mirror held before the import are converted on the
dispatcher's next pass.

Every output carries `adapter: healthapple@<version>`, the `ADAPTER` constant
at the top of the function, apart from `revision`, which is when the
provider received the row. A change to what a mirror row becomes raises the
constant and the package's `version` together, so `<version>` is the package
version and a repository that imported the package is offered the upgrade.
After an upgrade, replay the trigger from the start
(`bin/substratectl trigger replay healthapple-on-mirror --from 0`): every
mirror row is delivered again, and each output is rewritten under the new
version at the revision it already holds. Rows the app never resends are not
reprocessed any other way.

Two limits come from the app. It sends no nap flag, so the adapter calls a
sleep shorter than three hours that begins between 09:00 and 21:00 on its
own clock a nap. And it sends no deletion: a sample or workout deleted on
the phone keeps its mirror row, and the records built from it, unless a
later export re-sends the samples around it. Delete the mirror row to remove
them (`bin/substratectl delete <kind> <id>`, the kind in full, such as
`providers.substrate.reamde.dev/applehealth/workout`).

The metric mapping, what is skipped, how outputs are keyed and reconciled, and
the one rule an automation setup must follow:
[docs/bundles-catalog.md#apple-health-to-health-sample](../../docs/bundles-catalog.md#apple-health-to-health-sample).
