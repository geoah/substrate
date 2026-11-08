# Substrate Tasks on the web

The Tasks app in a browser, on a phone, a tablet and a desktop: the same
package (`substrate.reamde.dev/tasks`, [its README](../package/README.md)),
the same records and the same writes as the Apple and Android apps, on
[the web kit](../../kit/web/README.md). React 19, Vite and Tailwind CSS 4.

| Screen | What it shows |
| --- | --- |
| Today | open tasks due today or before, soonest first, with their list; quick add for today |
| No List, a list | the list's open tasks in their order, its completed ones when Settings shows them, quick add, New Task, rename and delete the list |
| A task | Status, List and Priority (both change in place), Due, Notes and Link; Mark as Done, Won't Do or Reopen, Delete Task; Edit |
| Settings | Show Completed Tasks (kept on this device), the repository, server, access and package the approval gave, Sign Out |

A phone has the tab bar over stacks, from 768 px wide the lists sit beside
the list, and from 1180 px the open task is a third column. Where the owner
is lives in the address bar (`#/today`, `#/list/<id>/<task>`), so a link to
a task opens it.

## The writes

As TasksKit's (`TaskWrites`): a task or list is created with a PUT on a
lowercase UUIDv7 the app chose (`ifVersion: 0`); a toggle, a move or a
priority is a patch of what changed; Save in the editor patches the fields
the owner changed on the version they opened, so a change made elsewhere
meanwhile is refused and said, never written over; a state moves only by
patch, and a reopen clears `completedAt` in the same write; moving a task
rewrites `list` and `position` together, the position a decimal after the
last of its new list's; a list is deleted with a plain DELETE and the
server's cascade takes its tasks.

## Run it

```bash
cd apps && pnpm install
pnpm dev            # http://127.0.0.1:5173, a local app to any substrate
pnpm typecheck
```

## Build it for a site

```bash
SUBSTRATE_WEB_URL=https://tasks.example.com/ pnpm build
```

`SUBSTRATE_WEB_URL` is where the build is served, an https URL ending in `/`.
It names the deployment, so it comes from the environment. The build is
static (`dist/`): the app, `client.json` (its client id is
`<url>client.json`, its one redirect `<url>`) and `manifest.webmanifest`.
Serve `<url>` as `index.html`; nothing else needs rewriting, since the app
routes in the hash. A host serving it must allow `connect-src https:` in
any CSP it sets, because each owner's substrate is on a host of their own.
