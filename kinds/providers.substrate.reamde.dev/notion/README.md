# The Notion bundle

Package `providers.substrate.reamde.dev/notion`: a provider that mirrors one
Notion workspace into eight kinds. It takes a Notion internal token (`ntn_…`)
pasted on the `config` record's `integrationToken` rather than OAuth, because Notion authenticates
its token exchange with HTTP Basic and the host facility declares one auth
style for every bundle. Read only: the token is spent on `users`, `search`,
`databases` and `blocks/{id}/children`, and never on a write.

`bundle.yaml` is the closure (the config and account kinds, the five mirrors,
the per-page sync-state kind and the sync function) and `triggers.yaml` is the
delivery wiring. They are the contract; this file is not.

## Kinds

| Kind | What it is |
| --- | --- |
| `config` | the pasted `integrationToken` and the API origin it may reach |
| `account` | one connected workspace: the toggles, the cursors and the backlog |
| `user` | one Notion user, a person or a bot |
| `page` | one page, which is also how Notion models a row of a data source |
| `datasource` | one data source: a database's schema and the set its rows belong to |
| `database` | one database, the container its data sources hang off |
| `block` | one piece of a page's content, the union of every block type's payload |
| `pagesync` | connector state: where the block walk of one page got to |

What it writes, the API version it pins, the token and sharing steps,
and how a bounded drain resumes:
[docs/bundles-catalog.md#notion](../../../docs/bundles-catalog.md#notion).
