# The Google bundle

Package `providers.substrate.reamde.dev/google`: an OAuth provider that mirrors
a Google account's address book, mail and calendars into the repository, in
Google's own shape. Three streams (contacts, gmail, calendar) share one
account, each with its own toggle, scope, function and cursor.

`bundle.yaml` is the closure (the config and account kinds, the six mirrors,
the three sync functions and the trusted `oauth2:` metadata) and
`triggers.yaml` is the delivery wiring. They are the contract; this file is
not.

What it writes, what it deliberately does not, the scopes, `backfillDepth` and
how to connect it:
[docs/bundles-catalog.md#google](../../../docs/bundles-catalog.md#google).
