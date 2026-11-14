# Substrate Tasks on the web

The Tasks app in a browser, on a phone, a tablet and a desktop: the same
package (`substrate.reamde.dev/tasks`, [its README](../package/README.md)),
the same records and the same writes as the Apple and Android apps, on
[the web kit](../../kit/web/README.md). React 19, Vite and Tailwind CSS 4.

| Screen           | What it shows                                                                                                                                                                                                                                                                                                                  |
| ---------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Needs you        | the suggestions waiting, in one row; every open task across the collections that are on in its band: Late or urgent, each with Move and Won't Do; Coming up (within the week); Later; one counted No date row; the chips of the pinned groups; quick add for today                                                             |
| Suggestions      | every suggestion, newest first, with Dismiss and Accept on each row, and Select for one decision over several; a suggestion's page sets its fields before Accept writes them in the same patch                                                                                                                                 |
| Lists            | search; Pinned; a section per group kind with its full reference, its groups with open tasks, No list, Missing and "N more with nothing open"                                                                                                                                                                                  |
| A group, No list | its kind and status, the suggestions in it, To Do and Done, Pin or Unpin, quick add and New Task where it is the app's own, and the ⋯ menu: Sort Tasks By, Show or Hide Completed, Rename List…, Delete List…                                                                                                                  |
| A task           | its name beside its round mark; Status, its group and its priority (both change in place where its collection is writable), Due, Completed; its notes as markdown and its link; another collection's other properties under Details; Mark as Done, Won't Do or Reopen (Undo while marked), Delete Task; Edit for the app's own |
| Settings         | Collections (each kind with a status the token reads, turned on or off, its grouping and readings, where new tasks are saved), Reminders and Lists as TasksSettings has them, the keys on a desktop, then the kit's sections                                                                                                   |

The app reads any task-like kind as the contract says
([apps/tasks/contract](../contract/README.md)): `src/sources.ts` is its
model, and `src/contract.test.ts` runs every case. A task is named by its
record path, `<kind>/<id>`, in the address bar too.

A phone has a tab per section of the App menu (Needs you with its badge,
Lists, Settings) and the round + that adds a task; from 768 px wide the
sections and the lists sit in a sidebar beside the list, and from 1000 px
the open task is a third column. Where the owner is lives in the address
bar (`#/needs`, `#/list/<id>/<task>`), so a link to a task opens it.

A row's mark completes its task after a 3 s undo window, a second tap
within it taking the mark back; Accept, Dismiss and Won't Do wait the same
window, with Undo beside them. A right click or a long press opens a
row's menu (its state, Edit, Priority, Move to List, Delete Task) and a
list's (Rename…, Delete…). In my order, tasks and lists drag into place: a
mouse at once, a finger after a hold, Alt with ↑ or ↓ from the keyboard.
On a phone a row swipes: from the left, what its mark does; from the
right, Delete and Won't Do.

With a keyboard: N is New Task (⌘N is the browser's), ⌘, Settings, ↑ and ↓
move through the list beside an open task, E edits it, A accepts and D
dismisses the open suggestion (or every one picked under Select, which S
turns on), P pins the group shown, Z takes back the last decision still in
its window, ⌘↵ saves the editor and Escape closes what is open.

**Reminders** are the native app's plan (`Reminders.plan`): each open task
due ahead, at its moment or at the hour Settings chooses for a task due on
a day. The browser shows them through the Notification API, and only while
the page is open: a page that is closed runs nothing. Where a browser lets
only a worker notify (Chrome on Android), `reminders-sw.js` shows them and
brings a tap back; it caches nothing. [PARITY.md](PARITY.md) is the rest of
what the web matches and does not.

## The writes

A decision on a task of any collection (accept, dismiss, complete, won't
do, reopen) is the contract's one patch: the status set to the action's
value, a suggestion's edited fields beside it, and on a reopen every
property the move into the closed state stamped set to null. It is an edit
on the version the owner saw, so a change another device made meanwhile
parks it as a conflict rather than being written over, and its row says
"Not saved" until Settings resolves it. Accept, dismiss, complete and won't
do are kept in the outbox the moment they are made, held through their
3 s undo window: Undo withdraws the entry, and a page closed within the
window sends it at the next open (offline, when the network is back).
Where the move has several values the owner picks one. A new task goes to
the collection "Save new tasks to" names, from quick add and the editor
alike, written through its readings; where it cannot take one (off, view
only, or a required property the app does not fill in), "Can't add a task"
says why. The settings records, `tasks/collection` and `tasks/preference`,
are created with a PUT on an id every device derives (a UUIDv8 of the
sha256 of `collection:<kind>` or `preference`) at `ifVersion: 0`, and
patched right after, so the device that lost the race writes its choice
over the winner's row; where two records stand for one kind, the one under
the derived id is read and written, else the oldest.

The app's own tasks and lists, as TasksKit's (`TaskWrites`, `TasksEnrichment`): a task or list is created
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

The approval installs `substrate.reamde.dev/tasks` at version 2. Against a
development substrate before that version is published, build the closure
and an authority document listing it, and start the server reading it:

```bash
.mise/python.sh .mise/closures.py --out build/apps/site/packages --authority substrate.reamde.dev tasks
SUBSTRATE_INSECURE_AUTHORITY_DOCS=substrate.reamde.dev=$PWD/build/apps/site/packages/authority.json mise run dev:restart
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
