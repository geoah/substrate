# The tasks package

Substrate Tasks' package: the owner's tasks and the task lists they are kept
in. The manifests are the YAML files beside this one, published under the
authority `substrate.reamde.dev`
([0214](../../../docs/decisions/0214-an-apps-approval-installs-published-packages-and-declare-and-the-agent-detail-are-retired.md),
[0221](../../../docs/decisions/0221-first-party-apps-publish-under-substrate-reamde-dev.md)):
the release builds each version's closure from them and lists it in the
site's authority document, the app's approval asks for the package as a
`package` detail at the version the build was written against, and the
server installs it from the document under this very name, the same in every
repository. The app applies nothing.

| File | Holds |
| --- | --- |
| `tasks.yaml` | the package document (its version) and the bundle document (`installs`, no `requires`) |
| `task.yaml` | `substrate.reamde.dev/tasks/task`: something to do |
| `list.yaml` | `substrate.reamde.dev/tasks/list`, labelled "Task list" |

`closures.lock.json`, each published version and its closure's sha256, is
written by the release when it first publishes the package, never by hand,
as [Workouts'](../../workouts/package/README.md) is.

**It is published.** The kinds are the publisher's: a repository installs
them as a `published` package, and the owner does not edit them; the records
are the owner's. An owner's token cannot apply these documents itself: a
token declares new packages only under its own repository's authority
([0210](../../../docs/decisions/0210-a-token-declares-new-packages-only-under-its-repositorys-authority.md)),
and the install through the catalog is the one door.

**Changing it.** A change ships as the next `version` in `tasks.yaml`, and
only additive changes (a new kind, a new optional property, a new enum
value, a new state): the server refuses a narrowing while live records hold
the old shape, and a published version never changes. A kind declares no
`indices` and no `embed` (below).

## Not the `tasks` sample

The repository ships a sample of the same name,
[`samples.substrate.reamde.dev/tasks`](../../../samples/tasks/bundle.yaml). The
two are different packages:

| | The sample | This package |
| --- | --- | --- |
| Lands as | `<the repository's authority>/tasks/task`: imported, a copy the owner edits | `substrate.reamde.dev/tasks/task`: installed, the same name everywhere |
| Comes with | `project`, `tasklog`, a Linear mapping; requires `people` and `scheduling` | `list`; requires nothing |
| Brought by | the owner, from the catalog or the console | Substrate Tasks' approval |

A repository that imported the sample and approved the app holds both, side
by side, and their records never mix: every surface names a kind by its full
reference (the console, the CLI, an agent's kinds list, the API's
`filter.kinds`), so `ada.example.com/tasks/task` and
`substrate.reamde.dev/tasks/task` never read as one. Nothing maps one onto
the other; an owner who wants their sample tasks in the app moves them with
ordinary writes.

The words are the sample's wherever the meaning is the same, so a reader
who knows one knows the other: `name` (the title), `description`, `url`,
`status` with its `completedAt` stamp, `priority` on the same ladder with the
same default, and `dueAt` through core's `temporal(point: dueAt)`. An app's
package cannot `require` the sample: a sample is imported under each
repository's own authority, so there is no published name to require.

## The shape

```text
task ──list (mustExist, onDelete: cascade)──► list
task ──people (repeated, mustExist)──► substrate.reamde.dev/core/contact
task ──origin (any kind)──► the record it was made from
```

- **task**: something to do. Ids are lowercase UUIDv7s chosen on the device,
  so every outbox retry of a create is a PUT that lands the same row.
- **list**: a named list of tasks, such as Groceries. Ids are chosen by the
  writer; the app uses UUIDv7s too.
- A task with no `list` is in no list, which the app shows as its inbox. The
  inbox is not a record, so there is nothing to seed and nothing the owner
  can delete by mistake.

## Task

`substrate.reamde.dev/tasks/task`, purpose `primary`, title `{name}`, traits
`substrate.reamde.dev/core/temporal(point: dueAt)` and
`substrate.reamde.dev/core/lifecycle`.

| Property | Type | Why |
| --- | --- | --- |
| `name` | string, required | What to do, in one line. The sample's is optional only because its older tasks have none; requiring it later would be a narrowing. |
| `description` | markdown | The details. The sample's name and type. |
| `url` | url | A link that goes with the task, as Reminders has. The sample's name and type. |
| `list` | reference to `list`, `mustExist`, `onDelete: cascade` | The task list it is in; absent for no list. |
| `position` | decimal | Its place among the tasks of its list, lowest first. |
| `dueDay` | date | The day it is due, on every task with a due date. |
| `dueAt` | the trait's datetime | The moment it is due, only when it is due at a time. |
| `priority` | enum, default `none` | none, low, medium, high, urgent: the sample's ladder. |
| `status` | state: `proposed`, `open`, `done`, `abandoned` | Born `open`; an agent's suggestion waits as `proposed`. |
| `completedAt` | datetime | Stamped by the move to `done`. |
| `origin` | reference to any kind | The record the task was made from, such as a message or an issue. Version 3. |
| `people` | repeated reference to `substrate.reamde.dev/core/contact`, `mustExist` | The people the task involves. Version 3. |

Notes:

- **Due: a day, or a moment.** Most tasks are due on a day, and a day has no
  instant (midnight in which zone?). So, as the Apple provider keeps an
  EKReminder (`dueDay` and `dueDate`), `dueDay` is the civil day and `dueAt`
  the moment, written only for a task due at a time. A timed task carries
  both, `dueDay` being the day of `dueAt` on the writer's clock, so "due by
  Friday" is one filter on `dueDay` for every task. The app writes the pair;
  nothing on the server ties them. A timed task sits on the timeline through
  the trait, so a window read finds it; a task due on a day alone does not,
  and is found by `dueDay`. In a hand-written YAML document, quote a date
  (`dueDay: "2026-11-06"`): bare, YAML reads it as a timestamp and the
  server refuses it as not a civil date.
- **Order is a decimal.** Moving a task writes that one task, with a number
  between its new neighbours (`"1.5"` between `"1"` and `"2"`). An int would
  renumber every task below the move, from every device's outbox at once.
  A `decimal` is exact (written as a string) and sorts as a number, so
  halving never runs out of precision, where a `float` would after about
  fifty moves into the same gap. Moving a task to another list rewrites
  `list` and `position` in one patch. It is the task's own property, not a
  link property on `list`: a task in no list has no link to carry it, and a
  read orders by a property only.
- **The list owns its tasks.** Deleting a list takes its tasks with it, as a
  Reminders list does. The app deletes a list with a plain DELETE, no
  purge: the list is tombstoned at once (the app shows none of its tasks
  from then), and the collector's next pass, within five minutes, purges
  the list and, in the same pass, the tasks its cascade reaches. Until that
  pass a PUT of the list restores it with its tasks; after it nothing does,
  so the app's question promises no restore. An app that wants to keep the
  tasks moves them first.
- **The states.** The sample's, and since version 2 `proposed`, the state an
  agent's suggestion waits in until the owner adds it (to `open`) or
  dismisses it (to `abandoned`), which the app shows under Suggestions
  ([0247](../../../docs/decisions/0247-a-status-names-the-open-states-a-record-waits-in-until-its-owner-accepts-it.md)).
  `abandoned` is reachable from `open`, which the sample's is not: dropping
  a task is a decision worth keeping, where a delete keeps nothing. `open` to
  `done` and back, `open` to `abandoned` and back, and since version 3
  `proposed` to `done`, for a suggestion the owner already did (an agent
  that sees the reply closes it); nothing else. A create may
  name any of the states, so an importer writes a finished task as
  finished; after that the state moves only by PATCH, so a create replayed
  from the outbox after the task was done never moves it back.
- **`origin` is the record a task was made from**, as an observed row names
  its upstream record
  ([0162](../../../docs/decisions/0162-an-observed-row-names-its-origin-and-source-and-composes-its-id-from-upstream-data.md)):
  the message an agent read, the mail, the issue, the meeting's transcript.
  Unpinned, because naming each kind would make this package depend on every
  provider, and without `mustExist`, because the message may be deleted
  upstream after the task is written. `source` is not the word: on
  substrate it names the app or device a row came through.
- **`people` are the contacts a task involves**: who asked, who is waiting,
  who it is for. Not an assignee: every task in a repository is its owner's.
  Each must exist when it is written; deleting a contact later leaves the
  value pointing at nothing, as any reference without `onDelete` does.
- **Reopening clears `completedAt` by hand.** The stamp is written on the
  way in and nothing removes it on the way out, so the app's reopen patch is
  `{"properties": {"status": "open", "completedAt": null}}`, one write.
- **`priority` keeps the sample's default**, so a filter on `priority: none`
  means the same in both packages. A declared default is stored into every
  record written without one.

## List

`substrate.reamde.dev/tasks/list`, purpose `primary`, label "Task list",
title `{name}`, trait `substrate.reamde.dev/core/lifecycle` (version 3).

| Property | Type | Why |
| --- | --- | --- |
| `name` | string, required | The list's name, such as Groceries. |
| `position` | decimal | Its place among the lists, lowest first, as a task's is. |
| `description` | markdown | What the list is for and where its work stands. Version 3. |
| `status` | state: `active`, `done`, `abandoned` | Born `active`. Version 3. |

Notes:

- **A list can be finished.** A list that holds a piece of work ("Budget
  2027") ends one day, and deleting it would delete its tasks, so it is
  closed instead: `active` to `done` or `abandoned`, and back. "Groceries"
  simply stays active. A list from before version 3 is entered at `active`
  when the version is admitted
  ([0082](../../../docs/decisions/0082-a-record-meets-a-new-state-machine-in-its-initial-state.md)).
  The app shows a list's status, and a closed list while it still holds
  open tasks. A list is not one of the app's collections: its own task is
  the collection, and the list is what it groups by.
- **Not the sample's `project`.** The sample's project also waits `onhold`,
  and its prose is a `summary`. A list has no on-hold state, and its prose
  is `description`, the word the task already uses.
- **Labelled "Task list"**, because "List" alone reads as a page of the
  console rather than a record. The kind's name stays `list`: the package
  segment already says what it lists, and every surface shows the full
  reference.

## Left out, and why

Each is additive later: an optional property, a trait binding whose
properties are optional, or a new kind.

- **Repeating tasks.** The sample binds core's `recurring` and `override`
  and marks each occurrence off with a `tasklog`. Binding them later adds
  `recurrence`, `rdates`, `exdates`, `timezone`, `recurrenceOf` and
  `originalAt`, the sample's names.
- **An assignee, a Linear mapping.** A personal to-do list has one person;
  a task's `people` are who it involves, not who does it. No mapping may
  target the task: a mapped kind takes server-assigned ids only, and the app
  chooses its ids on the device, so a task names the issue it came from
  through `origin` instead.
- **A reminder time apart from the due time.** Notifications fire at `dueAt`,
  or at the app's chosen hour on a task due on a day; both are the device's.
- **Subtasks, tags, a start date, a list's colour or icon, a list's links**
  to the records its work is about.
- **Indices.** None is declared. A list's tasks are a reverse read through
  the `refs` table, and every single reference gets an index on its path
  without a declaration; a personal list is hundreds of tasks. Declaring one
  later builds it and touches no record.
- **`embed`.** The sample embeds `description`; this package does not, so
  installing it spends nothing from the owner's embedding budget. Search
  still finds tasks by their words. Declaring it later queues the existing
  records.

## Checked

`bin/substratectl validate -f tasks.yaml -f list.yaml -f task.yaml` passes
offline. On a development substrate the package as published is refused to
an owner's token (0210, above), and a copy re-homed onto the repository's
own authority applied and held: a task without `name`, a bare-number
`position` and a `list` naming no list are refused; tasks order by
`position` as numbers (`1.5`, `2`, `10`); `dueDay` filters; a window read
over `at` finds the timed task; the transitions stamp and clear
`completedAt` as above, and `done` to `abandoned` is refused; purging a list
takes its tasks and leaves the task in no list.

Version 3, on a development substrate reading this tree's authority
document: a repository holding version 2 with three lists took the upgrade
through the catalog, and the three were entered at `active`; a migration
then created 101 lists, some `abandoned`, and 955 tasks in each of the four
states, with an `origin` of nine different kinds and `people` naming
contacts, and every one was admitted.
