# The WHOOP bundle

Package `providers.substrate.reamde.dev/whoop`: an OAuth provider
that mirrors one WHOOP member's cycles, recoveries, sleeps and workouts from
the WHOOP v2 API. Read only, and the mirrors describe one member, so the
closure declares no mapping: `account.user` names that member and "mine" is a
reference filter.

`bundle.yaml` is the closure (the config and account kinds, the five mirrors,
the `whoopsync` and `whoopwebhook` functions and the trusted `oauth2:`
metadata), and `triggers.yaml` is the delivery wiring (`whoop-on-connect`,
`whoop-on-request` and `whoop-scheduled` fire `whoopsync`; `whoop-webhook`
fires `whoopwebhook`). They are the contract; this file is not.

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

## Webhooks

WHOOP signs each webhook with the WHOOP app's client secret. The
`whoop-webhook` trigger declares that signature, and the server checks it
against the client secret on the `config` record before it stores the
request ([decision 0164](../../../docs/decisions/0164-a-webhook-signature-is-verified-by-the-host-when-the-trigger-declares-one.md)).
The secret stays in the server: `whoopwebhook` never receives it, and there is
no second copy to paste anywhere. To receive webhooks, register
`https://<your-substrate-host>/webhooks/<authority>/whoop-webhook` as the
app's webhook URL in WHOOP's developer dashboard.

The trigger ships enabled because the check fails closed. Until the `config`
record holds the client secret, the server answers every delivery `404`,
stores nothing (no blob, no ledger entry, no invocation) and logs that the
secret does not resolve. A delivery with a missing or wrong signature, or a
timestamp more than five minutes from when the server received it, gets the
same answer. It is `404`, the answer for a trigger that does not exist,
because the trigger carries no key: a distinct answer would tell anyone who
posts that the repository has WHOOP installed.

The scopes to enable on the WHOOP app, what each toggle syncs, the read
windows, what each webhook event does, how deletions reach the mirror and why
revocation is manual:
[docs/bundles-catalog.md#whoop](../../../docs/bundles-catalog.md#whoop).

## Upgrading from version 18

Version 21 adds the `whoopwebhook` function, the `whoop-webhook` trigger and
the `recovery.cycleId` property, and changes no property an installed
repository holds. Taking the upgrade (the console's Registry page, or
`substratectl install providers.substrate.reamde.dev/whoop`) writes the
trigger; then register the webhook URL above.

`whoopsync` now deletes mirror rows. When a stream that has a cursor drains
its window, the sync reads back by id up to 25 rows the mirror holds in that
window and the walk did not return, and deletes each row WHOOP answers `404`
for. Where the
[WHOOP to health](../../../docs/bundles-catalog.md#whoop-to-health-sample)
sample is installed, the health records built from a deleted row are deleted
with it.

An account that has recoveries on and sleeps off reconnects once:
`enabledRecovery` now asks for `read:sleep`, and until the grant carries it,
`whoopwebhook` drops that account's `recovery.updated` events and names
WHOOP's `403` in its output.
