# The GitHub bundle

Package `providers.substrate.reamde.dev/github`: an OAuth provider that mirrors
the code work the owner is involved in (the connected user, the repositories
they can reach, and the issues and pull requests they author, are assigned,
are mentioned in, comment on or are review-requested on, with the reviews on
those pull requests). Sync only: nothing here writes back to GitHub.

`bundle.yaml` is the closure (the config and account kinds, the thirteen
mirrors, the sync function and the trusted `oauth2:` metadata) and
`triggers.yaml` is the delivery wiring. They are the contract; this file is
not.

## Kinds

| Kind | What it is |
| --- | --- |
| `config` | the OAuth app: client id, sealed secret, test-only `apiBase` |
| `account` | one connection: the grant, the toggles, the watermarks, the backlog |
| `user` | one GitHub account: a person, an organization or a bot |
| `repository` | one repository, its owner and parent and source as references |
| `license` | one open-source license, completed from the catalogue read |
| `codeofconduct` | one code of conduct, completed from the catalogue read |
| `issuetype` | one organization issue type, as an issue names it |
| `app` | one GitHub App an issue or a pull request was performed via |
| `comment` | one comment; today only an issue's pinned comment mints a row |
| `team` | one team, as a pull request's requested reviewer |
| `milestone` | one milestone, as an issue or a pull request embeds it |
| `label` | one label in its repository |
| `issue` | one issue, complete, every relation a reference |
| `pullrequest` | one pull request, its merge facts and its issue half |
| `review` | one review on a pull request |

The scopes to enable, the toggles, the stages and where the watermarks live:
[docs/bundles-catalog.md#github](../../../docs/bundles-catalog.md#github).
