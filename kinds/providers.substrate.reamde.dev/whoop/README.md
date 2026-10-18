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
| `cycle` | one physiological cycle, from one sleep onset to the next: the day WHOOP scores strain over |
| `recovery` | the verdict on one cycle, keyed on the cycle it scores |
| `sleep` | one sleep, a night or a nap |
| `workout` | one recorded activity: sport, strain, zones and distance |

The scopes to enable on the WHOOP app, what each toggle syncs, the read
windows, how deletions reach the mirror and why revocation is manual:
[docs/bundles-catalog.md#whoop](../../../docs/bundles-catalog.md#whoop).

## Upgrading from version 18

The upgrade to version 21 adds the `recovery.cycleId` property and changes no
property an installed repository holds. Take it from the console's Registry
page, or with `substratectl install providers.substrate.reamde.dev/whoop`.

`whoopsync` now deletes mirror rows. When a stream that has a cursor drains
its window, the sync reads back by id every row the mirror holds in that
window and the walk did not return, and deletes each row WHOOP answers `404`
for. The stream's cursor moves only once every read-back has answered.

Recovery rows synced before version 21 have no `cycleId`. The first sync
after the upgrade reads the recovery collection again from a week before the
oldest of them, which writes each one's `cycleId`, and deletes a recovery
row the walk does not return. Where the
[WHOOP to health](../../../docs/bundles-catalog.md#whoop-to-health-sample)
sample is installed, the health records built from a deleted row are deleted
with it.

## What the tests are checked against

No test fixture is a response captured from a real WHOOP account. Under
`internal/providertest/testdata/whoop`:

| Fixture | Written from |
| --- | --- |
| the `GET_v2_*.json` pages | WHOOP's OpenAPI document and developer pages, for an invented member over 30 days |

The tests therefore prove the provider and the adapter agree with this
reading of the API, not that WHOOP sends it.

