# Garmin to health

Turns Garmin mirror rows into observations, traces and health records under
the repository's own authority. Package
`samples.substrate.reamde.dev/healthgarmin` writes `observations/observation`
and `observations/trace` records against the health sample's metric
catalogue, `observations/source` records for the devices, and `health/sleep`
and `health/workout` records. It requires the `observations` and `health`
samples and the installed `providers.substrate.reamde.dev/garmin`.

`bundle.yaml` is the closure (the `adaptgarmin` function) and
`triggers.yaml` its wiring, `healthgarmin-on-mirror`, which delivers every
change to a Garmin mirror row. They are the contract; this file is not.

Install the provider, import `health`, then import this sample:

```bash
bin/substratectl import samples.substrate.reamde.dev/observations
bin/substratectl import samples.substrate.reamde.dev/health
bin/substratectl import samples.substrate.reamde.dev/healthgarmin
```

Every output carries `adapter: healthgarmin@<version>`, the `ADAPTER`
constant at the top of the function, apart from `revision`, which is the
mirror row's own version. A change to what a mirror row becomes raises the
constant and the package's `version` together, so `<version>` is the package
version and a repository that imported the package is offered the upgrade.
After an upgrade, replay the trigger from the start
(`bin/substratectl trigger replay healthgarmin-on-mirror --from 0`): every
mirror row is delivered again, and each output is rewritten under the new
version at the revision it already holds. Rows Garmin never resends are not
reprocessed any other way.

Garmin pushes no deletion, so a summary or activity deleted in Garmin
Connect keeps its mirror row and the records built from it. Delete the
mirror row to remove them (`bin/substratectl delete <kind> <id>`, the kind
in full, such as `providers.substrate.reamde.dev/garmin/daily`): the trigger
delivers the delete and the adapter deletes what it built.

What each Garmin field becomes, how traces are cut into hours, the ids, and
what is skipped:
[docs/bundles-catalog.md#garmin-to-health-sample](../../docs/bundles-catalog.md#garmin-to-health-sample).
