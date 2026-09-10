# The WHOOP bundle

Package `providers.substrate.reamde.dev/whoop`: an OAuth provider that mirrors
a WHOOP wearable's daily physiology into `recovery`, `sleep` and `workout`
records. Sync only, and the mirrors are their own subjects: there is no person
to resolve, so the closure declares no mapping.

`bundle.yaml` is the closure (the config and account kinds, the three mirrors,
the sync function and the trusted `oauth2:` metadata) and `triggers.yaml` is
the delivery wiring. They are the contract; this file is not.

What it writes, the five scopes to enable, the read windows and why revocation
is manual:
[docs/bundles-catalog.md#whoop](../../../docs/bundles-catalog.md#whoop).
