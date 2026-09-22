# The WHOOP bundle

Package `providers.substrate.reamde.dev/whoop`: an OAuth provider
that mirrors one WHOOP member's cycles, recoveries, sleeps and workouts from
the WHOOP v2 API. Read only, and the mirrors describe one member, so the
closure declares no mapping: `account.user` names that member and "mine" is a
reference filter.

`bundle.yaml` is the closure (the config and account kinds, the five mirrors,
the `whoopsync` function and the trusted `oauth2:` metadata) and
`triggers.yaml` is the delivery wiring: `whoop-on-connect`, `whoop-on-request`
and `whoop-scheduled`, all three firing `whoopsync`. They are the contract;
this file is not.

## Kinds

| Kind | What it is |
| --- | --- |
| `config` | the WHOOP OAuth app: client id, sealed secret, data API origin |
| `account` | one connected WHOOP account, its toggles and every cursor |
| `user` | one WHOOP member, from the profile and body measurement calls |
| `cycle` | one physiological cycle, the wake-to-wake day WHOOP scores strain over |
| `recovery` | the verdict on one cycle, keyed on the cycle it scores |
| `sleep` | one sleep, a night or a nap |
| `workout` | one recorded activity: sport, strain, zones and distance |

The scopes to enable on the WHOOP app, what each toggle syncs, the read
windows and why revocation is manual:
[docs/bundles-catalog.md#whoop](../../../docs/bundles-catalog.md#whoop).
