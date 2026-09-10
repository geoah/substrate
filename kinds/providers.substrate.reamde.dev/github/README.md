# The GitHub bundle

Package `providers.substrate.reamde.dev/github`: an OAuth provider that mirrors
the code work the owner is involved in (the connected user, the repositories
they can reach, and the issues and pull requests they author, are assigned,
are mentioned in, comment on or are review-requested on). Sync only: nothing
here writes back to GitHub.

`bundle.yaml` is the closure (the config and account kinds, the four mirrors,
the sync function and the trusted `oauth2:` metadata) and `triggers.yaml` is
the delivery wiring. They are the contract; this file is not.

What it writes, the scopes and what they cost, and how to connect it:
[docs/bundles-catalog.md#github](../../../docs/bundles-catalog.md#github).
