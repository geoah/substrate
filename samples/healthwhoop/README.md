# The healthwhoop sample

Package `samples.substrate.reamde.dev/healthwhoop`: turns the WHOOP provider's
mirror rows into health records under the repository's own authority. It
requires the `health` sample (and so `observations`) and the WHOOP provider at
version 19 or later.

`bundle.yaml` is the closure (the `whoophealth` function) and `triggers.yaml`
is the delivery wiring: `healthwhoop-on-mirror` fires the function on every
create, update and delete of a WHOOP cycle, recovery, sleep or workout row.
They are the contract; this file is not.

A WHOOP sleep stage total is one `amount` observation of `health/sleep_stage`
carrying both `code` (the stage) and `value` (the total in minutes), spanning
the whole sleep: WHOOP reports totals per stage, not when each stage happened.

The trigger starts at the changelog's head, so a mirror that already held
rows when this sample was imported is converted by replaying the trigger from
the start (`POST
/api/v1/substrate.reamde.dev/core/trigger/healthwhoop-on-mirror/replay` with
`{"from": 0}`).

The mapping table, what is skipped and how deletions reach the outputs:
[docs/bundles-catalog.md#whoop-to-health-sample](../../docs/bundles-catalog.md#whoop-to-health-sample).
