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
```

- **task**: something to do. Ids are lowercase UUIDv7s chosen on the device,
  so every outbox retry of a create is a PUT that lands the same row.
- **list**: a named list of tasks, such as Groceries. Ids are chosen by the
  writer; the app uses UUIDv7s too.
- A task with no `list` is in no list, which the app shows as its inbox. The
  inbox is not a record, so there is nothing to seed and nothing the owner
  can delete by mistake.

## Task

`substrate.reamde.dev/tasks/task`, purpose `primary`, title `{name}`, trait
`substrate.reamde.dev/core/temporal(point: dueAt)`.

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
| `status` | state: `open`, `done`, `abandoned` | Born `open`. |
| `completedAt` | datetime | Stamped by the move to `done`. |

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
  Reminders list does. A plain DELETE leaves them listed until the
  collector's next pass; the app deletes a list with `?purge=true`, which
  takes them at once. An app that wants to keep the tasks moves them first.
- **The states.** The sample's, without `proposed`: nothing proposes a task
  to this package yet, and a state nothing writes is not declared; adding it
  is additive. `abandoned` stays, and is reachable from `open`, which the
  sample's is not: dropping a task is a decision worth keeping, where a
  delete keeps nothing. `open` to `done` and back, `open` to `abandoned` and
  back; nothing else. The state moves only by PATCH, so a create replayed
  from the outbox after the task was done never moves it back.
- **Reopening clears `completedAt` by hand.** The stamp is written on the
  way in and nothing removes it on the way out, so the app's reopen patch is
  `{"properties": {"status": "open", "completedAt": null}}`, one write.
- **`priority` keeps the sample's default**, so a filter on `priority: none`
  means the same in both packages. A declared default is stored into every
  record written without one.

## List

`substrate.reamde.dev/tasks/list`, purpose `primary`, label "Task list",
title `{name}`, no traits.

| Property | Type | Why |
| --- | --- | --- |
| `name` | string, required | The list's name, such as Groceries. |
| `position` | decimal | Its place among the lists, lowest first, as a task's is. |

Notes:

- **Not the sample's `project`.** A project has a lifecycle (active, on
  hold, done, abandoned) and a `summary` of where the work stands; it is
  finished one day. A list is a heading and a place in the sidebar:
  "Groceries" is never done. The sample's project carries no dates, so the
  difference is the lifecycle and the summary, not a schedule.
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
- **Assignees, a source, a Linear mapping.** The sample's `assignee` needs
  the `people` sample, which an app's package cannot require; a personal
  to-do list has one person. `source` (the message a task came from) waits
  for a writer that has one.
- **A reminder time apart from the due time.** Notifications fire at `dueAt`,
  or at the app's chosen hour on a task due on a day; both are the device's.
- **Subtasks, tags, a start date, a list's colour or icon, an archived
  list.**
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
