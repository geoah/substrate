# The Google Fit bundle

**Dated caveat.** Google announced that the Google Fit APIs, the REST API
included, end with 2026, and on 2024-05-01 it closed them to new sign-ups: a
Google Cloud project that has not enabled the Fitness API before cannot
enable it now. The successor for cloud integrations, the Google Health API,
is not onboarding new projects either. Nothing in this bundle was run against
a live account. It is written against Google's published reference and
discovery document (revision 20261008) and tested against a loopback fake
and recorded fixtures (`internal/providertest/googlefit_test.go`,
`internal/providere2e/providers/googlefit/`).

Package `providers.substrate.reamde.dev/googlefit`: an OAuth provider that
mirrors one Google account's fitness data from the Fit REST API v1. Read only.

`bundle.yaml` is the closure (the config and account kinds, the three
mirrors, the `googlefitsync` function and the trusted `oauth2:` metadata) and
`triggers.yaml` is the delivery wiring: `googlefit-on-connect`,
`googlefit-on-request` and `googlefit-scheduled`, all three firing
`googlefitsync`.

## Kinds

| Kind | What it is |
| --- | --- |
| `config` | the Google OAuth client: client id, sealed secret, the Fit API base |
| `account` | one connected Google account, its toggles, its time zone and every cursor |
| `datasource` | one entry of `GET users/me/dataSources`: a device or app stream, or a stream Google derives |
| `dataset` | one data source over one UTC day: the day's `point[]`, every page joined |
| `session` | one entry of `GET users/me/sessions`: a workout, a walk or a night's sleep |

Fit points carry no UTC offset, so a dataset is keyed by UTC day, and the
account's `timezone` is the owner's statement of where the data was
recorded. The sync never reads it; a reader that needs local days does.

The scopes, what each toggle reads, the cursors and the read windows:
[docs/bundles-catalog.md#google-fit](../../../docs/bundles-catalog.md#google-fit).
