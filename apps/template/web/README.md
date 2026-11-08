# Substrate Template on the web

The template app in a browser: the smallest complete app on
[the web kit](../../kit/web/README.md), and the one a new web app copies. The
same package (`substrate.reamde.dev/template`, [its README](../package/README.md))
and the same writes as the Apple build ([the template](../README.md)).
React 19, Vite and Tailwind CSS 4.

| Screen   | What it shows                                                                                                                                                                            |
| -------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Items    | the items by name, each marked while its change waits or was refused; the field that adds one, kept until the outbox has the item; the one issue to act on, and a refused write's notice |
| An item  | its Name, Edit (a sheet holding to the version opened) and Delete Item, which asks first                                                                                                 |
| Settings | the kit's sections: Your Substrate, Refused by Your Substrate, Repository, Approved access, Storage, About                                                                               |

Where the owner is lives in the address bar (`#/`, `#/item/<id>`,
`#/settings`).

| File                                        | What                                                                       |
| ------------------------------------------- | -------------------------------------------------------------------------- |
| `src/kinds.ts`                              | the one kind, declared once with `defineKind`: its read and its write      |
| `src/main.tsx`                              | the manifest (app.json's ids, package and scope) and `SubstrateApp`        |
| `src/ItemsScreen.tsx`, `src/ItemScreen.tsx` | the screens, on `useRecords`, `useListed`, `useRecord` and `useKindWrites` |
| `src/AddItem.tsx`                           | the field that adds an item, kept until the outbox has it                  |
| `src/WriteState.tsx`                        | an item's write waiting or refused, from `useWriteState`                   |
| `src/SettingsScreen.tsx`                    | `SubstrateSettings`; an app's own sections go above it                     |
| `vite.config.ts`                            | `@substrate/vite`'s `substrateSite`                                        |

Everything the app imports from the kit comes from `@substrate/react`. The
kit's README walks through these files: [an app, file by
file](../../kit/web/README.md#an-app-file-by-file).

## The writes

As the Apple build's: an item is created with a PUT on a lowercase UUIDv7
the app chose (`ifVersion: 0`); Save in the editor patches the name on the
version the owner opened, so a change made elsewhere meanwhile is parked as
a conflict that Settings offers to resolve, never written over; Delete is a
plain DELETE. Each is kept in the browser's outbox before the screen moves
on, and sent when the server can be reached.

## Run it

```bash
cd apps && pnpm install
pnpm --filter @substrate/template-web dev   # http://127.0.0.1:5174, a local app
```

Its approval installs the template package, which the server takes only
from an authority document that lists it: against a local substrate, start
it with one written from this tree, as [the template's README](../README.md)
says (`SUBSTRATE_INSECURE_AUTHORITY_DOCS`).

## Build it for a site

```bash
SUBSTRATE_WEB_URL=https://template.example.com/ pnpm --filter @substrate/template-web build
```

The build is static (`dist/`): the app, `client.json` and
`manifest.webmanifest`. Serve `<url>` as `index.html`; nothing else needs
rewriting, since the app routes in the hash. A host serving it must allow
`connect-src https:` in any CSP it sets, because each owner's substrate is
on a host of their own.
