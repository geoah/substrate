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

What each Garmin field becomes, how traces are cut into hours, the ids, and
what is skipped:
[docs/bundles-catalog.md#garmin-to-health-sample](../../docs/bundles-catalog.md#garmin-to-health-sample).
