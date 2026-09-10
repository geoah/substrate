# The Linear bundle

Package `providers.substrate.reamde.dev/linear`: an OAuth provider that mirrors
the issues assigned to the connected login into `issue`, `user` and `team`
records, in Linear's own shape. Sync only, mirrors only: what an issue means
to the repository (a task, a person) is the repository's own declaration,
reached by a mapping it declares.

`bundle.yaml` is the closure (the config and account kinds, the three mirrors,
the sync function and the trusted `oauth2:` metadata) and `triggers.yaml` is
the delivery wiring. They are the contract; this file is not.

What it writes, the one scope it requests, the hidden-email fallback and how to
connect it:
[docs/bundles-catalog.md#linear](../../../docs/bundles-catalog.md#linear).
