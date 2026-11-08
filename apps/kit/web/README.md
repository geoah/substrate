# The web kit

What every Substrate web app shares, the browser's side of
[the kit](../README.md): TypeScript packages in the pnpm workspace rooted at
`apps/` (`apps/pnpm-workspace.yaml`), each exported as source, so an app's
Vite build compiles them with it. The apps on them are
[Substrate Tasks on the web](../../tasks/web/README.md) and
[the template](../../template/web/README.md), the smallest complete one,
which a new web app copies.

| Package                                   | Holds                                                                                                                                                                                                                    | The Swift kit's                                                                           |
| ----------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------- |
| [`@substrate/client`](client/README.md)   | the REST API (records, lists, writes, the authorization check), the change feed's watch with its resume and relist, `locate` and the server requirement, `defineKind`, UUIDv7 ids, decimal positions, the error envelope | `SubstrateAPI`, `WatchFollower`, `ServerLocator`, `ServerRequirement`, `Position`         |
| [`@substrate/auth`](auth/README.md)       | the app authorization ([0194](../../../docs/decisions/0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md)), the session, the app status check, the revocations still owed                 | `SubstrateAuth` (`OAuthClient`, `SessionStore`, `AppStatusCheck`)                         |
| [`@substrate/local`](local/README.md)     | what the browser keeps per repository in IndexedDB: the outbox, the record cache with the watch's cursor, and the linked records the screens read                                                                        | `SubstrateLocal` (`Outbox`, `RecordCache`, `RecordFold`, `RecordWrites`), `LinkedRecords` |
| [`@substrate/react`](react/README.md)     | `SubstrateApp`, the record and write hooks, Settings' sections, the connect screen, the grouped screen pieces and `theme.css`                                                                                            | `AppShell`, `SubstrateScreens`, `SubstrateUI`                                             |
| [`@substrate/vite`](vite/README.md)       | the build: `SUBSTRATE_WEB_URL`, the client metadata document, the web manifest                                                                                                                                           | the apps' `apple:release` stamping                                                        |
| [`@substrate/testing`](testing/README.md) | a fake substrate behind `fetch`, for the kit's tests                                                                                                                                                                     | `SubstrateTesting`                                                                        |

The layering holds by lint (`apps/eslint.config.js`): client is below
everything, auth and local sit on client alone, and only react knows React.
An app imports the kit by package name, never by a path into it.

## An app in thirty lines

```tsx
import { packageDetail, webManifest } from "@substrate/auth"
import { defineKind, stringProp } from "@substrate/client"
import {
  SubstrateApp,
  SubstrateSettings,
  useKindWrites,
  useRecords,
} from "@substrate/react"

const Item = defineKind("substrate.reamde.dev/template/item", {
  read: (r) => ({
    id: r.id,
    version: r.version,
    name: stringProp(r, "name") ?? "",
  }),
  write: (draft: { name: string }) => ({ name: draft.name.trim() }),
})

function Items() {
  const items = useRecords(Item)
  const writes = useKindWrites(Item)
  return (
    <>
      {items.map((i) => (
        <p key={i.id}>{i.name}</p>
      ))}
      <button onClick={() => void writes.create({ name: "Milk" })}>Add</button>
      <SubstrateSettings />
    </>
  )
}

const manifest = webManifest({
  id: "template",
  name: "Substrate Template",
  scopes: ["write:substrate.reamde.dev/template/*"],
  details: [packageDetail("substrate.reamde.dev/template", 1)],
  softwareVersion: __APP_VERSION__,
})

export const App = () => (
  <SubstrateApp manifest={manifest} kinds={[Item]} kept="items">
    <Items />
  </SubstrateApp>
)
```

`SubstrateApp` shows the connect screen until a session is kept, handles the
sign-in's return, checks the app's standing with its server, and opens the
repository: the records from what this browser kept, then read and followed;
every write kept in the outbox before it resolves, shown at once, and sent
when the server can be reached.

## An app's client id

A web app's `client_id` is the URL of its client metadata document and its
redirect is the app itself, on the same origin: `webManifest` makes both
from where the page is served (`<base>client.json` and `<base>`), and
`@substrate/vite` writes the document for the URL a build is deployed at.
Served from a loopback address (`pnpm dev`), the same build is a local app,
whose document the server never fetches.

The server verifies an app (decision
[0209](../../../docs/decisions/0209-an-app-is-verified-by-its-authoritys-txt-record-and-document.md))
only when its redirect is on its authority's name or under it, so an app
served from any other host shows as unverified on the approval page, with
its host named. It still asks for and installs a published package, which
its publisher's own document vouches for.

## Work on it

```bash
cd apps && pnpm install
pnpm test        # vitest: every package's tests, jsdom and fake-indexeddb
pnpm lint        # eslint over the kit and the apps
pnpm typecheck   # tsc in every package
pnpm build       # every app's production build
```

## Not yet

- **Opening with no network at all.** The records and the outbox survive a
  reload, so a page that loads opens with its last state while the server
  cannot be reached; loading the page itself offline needs a service worker
  caching the app's build, which no app has yet.
- **Publishing the packages** for other repositories: they are workspace
  packages, `private`.
- **CI**: nothing runs `pnpm test` or `pnpm lint` in a workflow yet.
