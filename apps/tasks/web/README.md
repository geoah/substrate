# Substrate Tasks on the web

The Tasks app in a browser, on a phone, a tablet and a desktop: the same
package (`substrate.reamde.dev/tasks`, [its README](../package/README.md)),
the same records and the same writes as the Apple and Android apps, on
[the web kit](../../kit/web/README.md). React 19, Vite and Tailwind CSS 4.

| Screen          | What it shows                                                                                                                                                                                                              |
| --------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Needs you       | every open task across the lists in its band: Needs you (late, or high and urgent with no day), each with Move and Won't Do; Coming up (within the week); Later; the chips that narrow it to one list; quick add for today |
| No List, a list | the list's open tasks in my order or by due date, its done ones under Done when Settings shows them, quick add, New Task, and the ⋯ menu: Sort Tasks By, Show or Hide Completed, Rename List…, Delete List…                |
| A task          | its name beside its round mark; Status, List and Priority (both change in place), Due, Completed; its notes as markdown and its link; Mark as Done, Won't Do or Reopen (Undo while marked), Delete Task; Edit              |
| Settings        | Reminders and Lists as TasksSettings has them, the keys on a desktop, then the kit's sections: Your Substrate, the changes refused, Repository, Approved access, Storage, About                                            |

A phone has a tab per section of the App menu (Needs you with its badge,
Lists, Settings) and the round + that adds a task; from 768 px wide the
sections and the lists sit in a sidebar beside the list, and from 1000 px
the open task is a third column. Where the owner is lives in the address
bar (`#/needs`, `#/list/<id>/<task>`), so a link to a task opens it.

A row's mark completes its task after a 3 s undo window, a second tap
within it taking the mark back. A right click or a long press opens a
row's menu (its state, Edit, Priority, Move to List, Delete Task) and a
list's (Rename…, Delete…). In my order, tasks and lists drag into place: a
mouse at once, a finger after a hold, Alt with ↑ or ↓ from the keyboard.
On a phone a row swipes: from the left, what its mark does; from the
right, Delete and Won't Do.

With a keyboard: N is New Task (⌘N is the browser's), ⌘, Settings, ↑ and ↓
move through the list beside an open task, E edits it, ⌘↵ saves the
editor and Escape closes what is open.

**Reminders** are the native app's plan (`Reminders.plan`): each open task
due ahead, at its moment or at the hour Settings chooses for a task due on
a day. The browser shows them through the Notification API, and only while
the page is open: a page that is closed runs nothing. Where a browser lets
only a worker notify (Chrome on Android), `reminders-sw.js` shows them and
brings a tap back; it caches nothing. [PARITY.md](PARITY.md) is the rest of
what the web matches and does not.

## The writes

As TasksKit's (`TaskWrites`, `TasksEnrichment`): a task or list is created
with a PUT on a lowercase UUIDv7 the app chose (`ifVersion: 0`); a toggle,
a move, a reschedule or a priority is a patch of what changed; a task or
list dragged writes its own `position`, a decimal between its new
neighbours (`Position.moves`, in `src/position.ts`), and a neighbour only
where the order needs it (two with one number, one with none, a gap used
up); Save in the editor patches the fields
the owner changed on the version they opened, so a change made elsewhere
meanwhile is never written over: the kit's outbox parks it, the lists say
"1 change needs you", and Settings offers Keep My Edit and Use Theirs; a state moves only by
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
