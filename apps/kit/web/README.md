# The web kit

What every Substrate web app shares, the browser's side of
[the kit](../README.md): three TypeScript packages in the pnpm workspace
rooted at `apps/` (`apps/pnpm-workspace.yaml`), each exported as source, so
an app's Vite build compiles them with it. The first app on them is
[Substrate Tasks on the web](../../tasks/web/README.md).

| Package | Holds | The Swift kit's |
| --- | --- | --- |
| `@substrate/client` (`client/`) | `SubstrateClient` over the REST API (a record, a list read and all its pages, put, patch, delete), the change feed's watch with its resume and relist, `locate` (a server address, or a repository's `_substrate` TXT record read over DNS over HTTPS, its document, its parent), lowercase UUIDv7 ids, exact decimal positions, the error envelope | `SubstrateAPI`, `WatchFollower`, `ServerLocator`, `Position` |
| `@substrate/auth` (`auth/`) | the app authorization ([0194](../../../docs/decisions/0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md)): the manifest, PKCE, the redirect to the server's own approval page with the package details, the callback's checks (`state`, `iss`, the repository typed), the code exchange, the session in the browser's storage and its revocation | `SubstrateAuth` (`OAuthClient`, `AuthorizationDetail`, `SessionStore`) |
| `@substrate/react` (`react/`) | `SubstrateApp` (the connect screen until a session is kept, then the app), `LiveCollection` and `useLive` (the app's kinds read once and kept current from the feed), `useWrites` (each write shown at once, replaced by the server's answer, undone with its words when refused), the account sections of Settings, and the screen pieces in the Apple apps' grouped style, with the design tokens in `theme.css` | `SubstrateScreens`, `SubstrateUI`, `AppShell` |

## An app's client id

A web app's `client_id` is the URL of its client metadata document and its
redirect is the app itself, on the same origin: `webManifest` makes both
from where the page is served (`<base>client.json` and `<base>`), and the
app's build writes the document for the URL it is deployed at. Served from a
loopback address (`pnpm dev`), the same build is a local app, whose document
the server never fetches.

The server verifies an app (decision
[0209](../../../docs/decisions/0209-an-app-is-verified-by-its-authoritys-txt-record-and-document.md))
only when its redirect is on its authority's name or under it, so an app
served from any other host shows as unverified on the approval page, with
its host named. It still asks for and installs a published package, which
its publisher's own document vouches for.

## Not yet

- An outbox: a write made offline fails and is undone, where the Apple and
  Android apps keep it and send it later.
- Tests, lint and CI: the workspace builds with `pnpm --dir apps build`, and
  `tsc` checks every package through the app that imports it.
- Publishing the packages for other repositories: they are workspace
  packages, `private`.
