# The Notion bundle

Package `providers.substrate.reamde.dev/notion`: a provider that mirrors the
Notion pages and data sources shared with an internal integration into `page`
and `database` records. It takes a pasted integration token rather than OAuth,
because Notion authenticates its token exchange with HTTP Basic and the host
facility declares one auth style for every bundle. Read only: the token is
spent on `search` and `blocks/{id}/children` and nothing else.

`bundle.yaml` is the closure (the config and account kinds, the two mirrors and
the sync function) and `triggers.yaml` is the delivery wiring. They are the
contract; this file is not.

What it writes, the API version it pins, the integration and sharing steps, and
its content limits:
[docs/bundles-catalog.md#notion](../../../docs/bundles-catalog.md#notion).
