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
| [`@substrate/react`](react/README.md)     | `SubstrateApp` and the headless `SubstrateProvider`, the record and write hooks, Settings' sections, the connect screen, the grouped screen pieces and `theme.css`                                                       | `AppShell`, `SubstrateScreens`, `SubstrateUI`                                             |
| [`@substrate/vite`](vite/README.md)       | the build: `SUBSTRATE_WEB_URL`, the client metadata document, the web manifest                                                                                                                                           | the apps' `apple:release` stamping                                                        |
| [`@substrate/testing`](testing/README.md) | a fake substrate behind `fetch`, for the kit's tests                                                                                                                                                                     | `SubstrateTesting`                                                                        |

The layering holds by lint (`apps/eslint.config.js`): client is below
everything, auth and local sit on client alone, and only react knows React.
An app imports the kit by package name, never by a path into it.

## An app, file by file

[The template](../../template/web/README.md) is the smallest complete app,
and what a new one copies. Its files below are the files themselves (a test
holds them to it). Everything comes from `@substrate/react`.

The kind, declared once: how a record reads as the app's value, and how a
draft writes as properties. A write names only the fields it sets, so a
change or an edit sends nothing else.

[`src/kinds.ts`](../../template/web/src/kinds.ts):

```ts
import { defineKind, stringProp } from "@substrate/react"

/** An item as the screens read it. */
export interface Item {
  id: string
  /** The version the owner sees: what an edit holds to. */
  version: number
  name: string
}

/** What a write of an item names: a create every field, a change or an
 * edit the fields it sets. */
export interface ItemDraft {
  name: string
}

/** The template's one kind, declared once. The package is published under
 * `substrate.reamde.dev`, so the name is the same in every repository
 * (../package/README.md). */
export const Item = defineKind("substrate.reamde.dev/template/item", {
  read: (r): Item => ({
    id: r.id,
    version: r.version,
    name: stringProp(r, "name") ?? "",
  }),
  write: (draft: Partial<ItemDraft>) =>
    draft.name === undefined ? {} : { name: draft.name.trim() },
})

/** An item's name as a screen shows it. */
export function itemTitle(item: Item): string {
  return item.name || "Untitled item"
}
```

The manifest and the shell. `SubstrateApp` shows the connect screen until a
session is kept, handles the sign-in's return, checks the app's standing
with its server, and opens the repository: the records from what this
browser kept, then read and followed.

[`src/main.tsx`](../../template/web/src/main.tsx):

```tsx
import { SubstrateApp, packageDetail, webManifest } from "@substrate/react"
import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import { App } from "./App"
import { Item } from "./kinds"
import "./index.css"

// app.json's ids, package and scope: the approval installs the published
// package at version 1 or later, as the Apple build asks.
const manifest = webManifest({
  id: "template",
  name: "Substrate Template",
  scopes: ["write:substrate.reamde.dev/template/*"],
  details: [packageDetail("substrate.reamde.dev/template", 1)],
  packageNames: { "substrate.reamde.dev/template": "Template" },
  softwareVersion: __APP_VERSION__,
})

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <SubstrateApp
      manifest={manifest}
      kinds={[Item]}
      kept="items"
      tagline="Your items, kept in your own Substrate."
      icon={
        <img
          src={`${import.meta.env.BASE_URL}icon-512.png`}
          alt=""
          className="size-24 rounded-[22%] shadow-md"
        />
      }
    >
      <App />
    </SubstrateApp>
  </StrictMode>
)
```

Reading: `useRecords` answers the kind's records as the app's values, the
same array until one of them changes, and `useListed` tells "none yet" from
"none". The sort is the app's.

[`src/ItemsScreen.tsx`](../../template/web/src/ItemsScreen.tsx):

```tsx
import {
  AccessBanner,
  BarButton,
  LargeTitle,
  LinkStatus,
  NavBar,
  RefusedBanner,
  Section,
  Spinner,
  useLinkedStatus,
  useListed,
  useRecords,
} from "@substrate/react"
import { ChevronRight, Settings } from "lucide-react"
import { useMemo } from "react"
import { AddItem } from "./AddItem"
import { Item, itemTitle } from "./kinds"
import { WriteState } from "./WriteState"

/** The items, sorted by name, and the field that adds one. */
export function ItemsScreen(props: {
  onOpen: (id: string) => void
  onSettings: () => void
}) {
  const records = useRecords(Item)
  const listed = useListed(Item)
  const link = useLinkedStatus()
  const items = useMemo(
    () =>
      [...records].sort(
        (a, b) =>
          itemTitle(a).localeCompare(itemTitle(b)) || a.id.localeCompare(b.id)
      ),
    [records]
  )

  return (
    <div className="mx-auto w-full max-w-2xl pb-40">
      <NavBar
        actions={
          <BarButton aria-label="Settings" onClick={props.onSettings}>
            <Settings className="size-5" aria-hidden />
          </BarButton>
        }
      />
      <LargeTitle>Items</LargeTitle>
      <LinkStatus status={link.status} words={link.words} />
      <AccessBanner className="mx-4 mb-4" />
      <RefusedBanner className="mx-4 mb-4" onOpenSettings={props.onSettings} />
      <div className="px-4">
        {items.length > 0 ? (
          <Section>
            {items.map((item) => (
              <button
                key={item.id}
                type="button"
                onClick={() => props.onOpen(item.id)}
                className="flex min-h-11 w-full items-center gap-3 px-4 py-2.5 text-left active:bg-selected"
              >
                <span className="flex min-w-0 flex-1 flex-col">
                  <span className="text-[17px] break-words">
                    {itemTitle(item)}
                  </span>
                  <WriteState id={item.id} />
                </span>
                <ChevronRight
                  className="size-4 shrink-0 text-tertiary"
                  aria-hidden
                />
              </button>
            ))}
          </Section>
        ) : !listed ? (
          <div className="flex justify-center py-16 text-secondary">
            <Spinner />
          </div>
        ) : (
          <div className="py-16 text-center">
            <p className="text-[20px] font-semibold">No items</p>
            <p className="mt-1 text-[15px] text-secondary">Add one below.</p>
          </div>
        )}
      </div>
      <AddItem />
    </div>
  )
}
```

Writing: `useKindWrites` writes a draft through the codec. It resolves once
the outbox keeps the write (`kept`), which is not the server taking it: the
write is shown at once and sent when the server can be reached.

[`src/AddItem.tsx`](../../template/web/src/AddItem.tsx):

```tsx
import { BarButton, ProblemBox, useKindWrites } from "@substrate/react"
import { useState, type FormEvent } from "react"
import { Item } from "./kinds"

/** The field that adds an item, at the bottom of the screen. What it holds
 * is cleared only once the outbox keeps the item, so nothing typed is lost
 * to a write the browser could not keep; the words for one it could not
 * keep stand beside the field, with Retry. */
export function AddItem() {
  const writes = useKindWrites(Item)
  const [draft, setDraft] = useState("")
  const [notKept, setNotKept] = useState<string | null>(null)

  const add = async (e?: FormEvent) => {
    e?.preventDefault()
    if (!draft.trim()) return
    const written = await writes.create(
      { name: draft },
      { onNotKept: setNotKept }
    )
    if (!written.kept) return
    setDraft("")
    setNotKept(null)
  }

  return (
    <form
      onSubmit={add}
      className="fixed inset-x-0 bottom-0 z-20 border-t border-separator bg-grouped/90 px-4 pt-3 pb-[max(0.75rem,env(safe-area-inset-bottom))] backdrop-blur-xl"
    >
      <div className="mx-auto max-w-2xl space-y-2">
        {notKept && (
          <div className="overflow-hidden rounded-[10px] bg-card">
            <ProblemBox
              role="alert"
              title="A change was not kept"
              message={notKept}
              action={{ title: "Retry", onClick: () => void add() }}
            />
          </div>
        )}
        <div className="flex gap-2">
          <label className="sr-only" htmlFor="new-item">
            Add an item
          </label>
          <input
            id="new-item"
            className="h-11 min-w-0 flex-1 rounded-[10px] bg-card px-4 text-[17px] outline-none placeholder:text-tertiary focus-visible:ring-2 focus-visible:ring-accent"
            placeholder="Add an item"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            enterKeyHint="done"
          />
          <BarButton type="submit" prominent disabled={!draft.trim()}>
            Add
          </BarButton>
        </div>
      </div>
    </form>
  )
}
```

The rest, [one item](../../template/web/src/ItemScreen.tsx) with its edit
on the version opened and [Settings](../../template/web/src/SettingsScreen.tsx),
is in the template.

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

## The outbox

Every write is kept in the browser's outbox before it resolves, and sent in
order when the server can be reached. A write the server refuses for itself
is **parked**: kept with the server's words, holding back that record's
later writes and never another's, until the owner's Retry or Discard in
Settings. What each answer does:

| The server answers | create: PUT `ifVersion: 0`, an id the device minted                                     | change: a guarded PATCH                                                                                | edit: a PATCH on the version the owner saw        | delete: a plain DELETE |
| ------------------ | --------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------- | ---------------------- |
| 409 or 412         | an earlier try landed and its answer was lost: the row is read and laid over, as landed | the record is read again and the change sent once more on top; a second one holds it for the next send | parked as a conflict: Keep My Edit, or Use Theirs | held for the next send |
| 404                | parked                                                                                  | parked: not on the substrate any more                                                                  | parked                                            | done: already gone     |

For every write, a 422 is parked, unless it is a stale delivery (the stored
revision is past the one the write carries), which is dropped; a 401 stops
the send as signed out, and a 403 for a scope the token lacks as needing
approval, nothing lost; any other 403 is parked; a 429, a 5xx and no network
stop the send, which is tried again with backoff (a fifth of jitter, and the
server's Retry-After), at once when the browser is back online or the page
back in front.

A create that meets its own row never sends its properties again, so it
cannot write over an edit another device made in between. The edits queued
on it before it landed (on version 0) are sent at version 1, the version a
create makes, so a change made elsewhere meanwhile parks them as a conflict
rather than being overwritten. A row found gone since still counts the
create as landed, and the record goes from what this browser shows and
keeps, a read of it from before answering into nothing; its later writes
then meet a 404 and park.

## Work on it

```bash
cd apps && pnpm install
pnpm test        # vitest: every package's tests, jsdom and fake-indexeddb
pnpm lint        # eslint over the kit and the apps
pnpm typecheck   # tsc in every package
pnpm build       # every app's production build
pnpm fmt:check   # prettier
```

CI's `apps web` job runs the same five (`mise run ci:apps:web`).

## Not yet

- **Opening with no network at all.** The records and the outbox survive a
  reload, so a page that loads opens with its last state while the server
  cannot be reached; loading the page itself offline needs a service worker
  caching the app's build, which no app has yet.
- **Publishing the packages** for other repositories: they are workspace
  packages, `private`.
