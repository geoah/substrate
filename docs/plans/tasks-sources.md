# Tasks over any task-like kind

Status: proposal, 2026-11-09, revised after a design review the same day.
The vocabulary change (decision record
[0247](../decisions/0247-a-status-names-the-open-states-a-record-waits-in-until-its-owner-accepts-it.md)),
the scoped implementors route, the kits' status reading and the tasks
package's version 2 are on this branch. The rest is the plan.

## The problem

Substrate Tasks reads exactly two kinds, `substrate.reamde.dev/tasks/task` and
`substrate.reamde.dev/tasks/list`, named once in `TasksKinds.swift` and
`model.ts`. An owner whose tasks already live somewhere else cannot use the
app without copying them. Their tasks might be the tasks sample imported under
their own authority, a kind they declared, a provider's issues, or a system
of agents that writes its own task kind. Agents also suggest tasks, and the
app has no place for a suggestion the owner has not taken on.

The pieces the vocabulary needs:

- [0225](../decisions/0225-an-apps-scope-may-name-a-trait-and-covers-what-star-covers-among-the-kinds-that-bind-it.md):
  an app may ask for `read:implements(<trait>)`.
- [0226](../decisions/0226-a-kinds-status-is-the-core-lifecycle-trait-and-names-its-done-and-dropped-states.md):
  core's `lifecycle` names a kind's status and the states that close a
  record.
- [0247](../decisions/0247-a-status-names-the-open-states-a-record-waits-in-until-its-owner-accepts-it.md):
  `proposed:` names the open states a suggestion waits in.

## Candidates and collections

**A candidate** is a kind that binds `substrate.reamde.dev/core/lifecycle`
and that the token reads. The app lists candidates through
`GET …/core/trait/substrate.reamde.dev%2Fcore%2Flifecycle/implementors`. That
route now answers an app's token with the implementors its grant reads, each
with its declaration (`scopedDataset.TypesImplementing`).

Binding `lifecycle` does not make a kind a to-do: a project, an order or a
ticket binds it too. So a candidate stays a candidate until the owner turns
it on.

**A collection** is a candidate the owner turned on. The app's own
`substrate.reamde.dev/tasks/task` is on from the start. Every other one is
turned on by hand in Settings → Collections, and that is also when the app
asks for write access to it.

## What the app reads from a declaration

The app reads a declaration conservatively. Where a reading is a guess, the
guess is shown on the collection's page and the owner may change it. The
owner's choice is kept in the collection's settings record (below).

| What           | Reading                                                                                              | Guess the owner can change              |
| -------------- | ---------------------------------------------------------------------------------------------------- | --------------------------------------- |
| status         | the property `lifecycle` binds, its values, `done:`, `dropped:`, `proposed:`, a machine's moves       | none: it is the declaration's           |
| title, shown   | the record's server-rendered `title`                                                                 | none                                    |
| title, edited  | only where `displayTemplate` is exactly one `{property}` and the property is a string                 | none: otherwise the title is not editable |
| due            | a `datetime` property, the point `temporal` binds where it binds one                                  | which property, or none                 |
| due day        | a `date` property named `dueDay`, used beside due as the app's own kind uses it                       | which property, or none                 |
| priority       | an `enum` named `priority`, its values in declared order, lowest first                                | which property, or none                 |
| order          | a `decimal` property named `position`                                                                 | which property, or none                 |
| group          | a single reference whose target kind the token reads                                                  | which reference, or none                |
| everything else| read-only under Details, with the property's declared label                                           | none                                    |

Without a due day, a day-only due date is not offered: the app sets a due
time, or nothing, and never invents midnight. Without an order property,
rows sort by due date, then by creation, and dragging is off. A collection
whose kind requires a property the editor does not edit is not offered for
new tasks.

## What the app may do with a record

Each action's availability is decided on its own, from the declaration and
the grant. A refusal from the server always wins. "View only" is a label for
a collection whose grant writes nothing; viewing is never taken away.

| Action       | Offered when                                                                                                                    |
| ------------ | ------------------------------------------------------------------------------------------------------------------------------- |
| accept       | the record stands in a `proposed:` value and the status moves to an open value that is not proposed                              |
| dismiss      | the record stands in a `proposed:` value and the status moves to a dropped value                                                 |
| complete     | the record stands open and the status moves to a done value                                                                      |
| won't do     | the record stands open and the status moves to a dropped value                                                                   |
| reopen       | the record stands done or dropped and the status moves to an open value                                                          |
| edit a field | the grant writes the kind, and the field is the one the reading above names                                                      |
| create       | the grant writes the kind, it is the collection new tasks go to, and its required properties are all ones the editor edits       |

**One destination is one button; several are a choice.** If a move has one
eligible value, the action writes it. If it has several, such as "Backlog" or
"In progress" for accept, the action opens a short list of the values by
their declared labels. An enum moves to any of its values, so its choices
are every eligible value.

**Undo cancels, it does not reverse.** Accept, dismiss, complete and won't do
wait three seconds before they reach the outbox, as completion does today.
Undo within the window writes nothing. After it, undo is the reverse move,
offered only where the status declares one. A suggestion's edited fields
and its accept are one patch. A stale version, a refusal, an offline replay
and a partly failed batch are each reported on the row they belong to,
through the outbox as today.

## Groups

Grouping is per collection, never one choice for the app. Each collection
groups by one single reference, read as the group:

- **The guess:** a reference named `list`, `project` or `area` whose target
  the token reads. It is a visible suggestion, not a silent default; with
  none or several matches, the collection starts ungrouped.
- **A group kind needs no `lifecycle`.** The app reads it under an exact
  `read:` scope, asked for when the owner turns grouping on.
- **A group kind that binds `lifecycle` shows its status.** A closed group
  still shows while it holds open tasks.
- **"No project" or "No list"** is named after the reference's label, for
  records with nothing in the reference. A reference to a record that is
  gone or out of reach shows as "Missing" with the reference, never as "No
  project".

## Settings records

Settings live in the app's package, so the phone, the iPad and the web agree.

`substrate.reamde.dev/tasks/collection`, one record per collection the owner
changed anything about:

- **Its id is derived from the kind it describes:** a UUIDv8 of the sha256 of
  `collection:` and the kind's full reference. Every device names the same
  record.
- **It is created with `PUT` and `ifVersion: 0`.** A device that loses the
  race reads the winner and patches it.

| Property  | Type             | Holds                                                          |
| --------- | ---------------- | -------------------------------------------------------------- |
| `kind`    | string, required | the collection's kind, by its full reference                    |
| `enabled` | bool             | shown or not                                                    |
| `group`   | string           | the reference property it groups by; absent for none            |
| `due`, `dueDay`, `priority`, `order` | string | the property each reading uses, where the owner changed the guess; `-` for none |

`substrate.reamde.dev/tasks/preference`, one record with the id derived from
`preference`:

| Property  | Type                     | Holds                                                                  |
| --------- | ------------------------ | ---------------------------------------------------------------------- |
| `newTasks`| string                   | the kind new tasks are written to; absent, the app's own                |
| `pins`    | reference, repeated, any | the pinned groups in one order across every collection                 |

**A pin is a group record, wherever it is grouped.** A pin whose record is
deleted or out of reach is not shown, and is dropped at the next write of
the preference. A pin whose collection is turned off is hidden, not dropped.

## Scopes

- **The first approval:** `write:substrate.reamde.dev/tasks/*` (the app's own
  task, list and settings) and `read:implements(substrate.reamde.dev/core/lifecycle)`,
  which finds the candidates and counts them.
- **Turning a collection on:** asks for `write:<its kind>`. Turning its
  grouping on asks for `read:<the group kind>`.

Each is asked with the kits' existing approval flow for missing scopes, so
the owner approves exactly the kinds they chose. A trait write scope is not
asked: hiding a collection does not narrow what a token may write.

## Discovery and the link

Both kits fix the kinds a link reads when it opens. Swift's
`LinkedRecords(kinds:)` and the web's `LinkedRecords` and `SubstrateProvider`
both do.

1. The settings kinds are read first, on a link of their own that stays open.
2. The data link opens over the enabled collections and their group kinds.
3. When a collection is turned on or off, a grouping changes, or an approval
   grants new scopes, the data link is closed and opened again over the new
   set. Pending writes stay in the outbox, which outlives the link. The
   selection is kept when the record it names is still read.
4. Discovery runs again at every sign-in, after an approval, and when the
   watch reports a `core/kind` change the token reads.

## Suggestions

A suggestion is a record of a collection whose status stands in `proposed:`.

- It is not open work: it never counts toward Needs you's badge or a group's
  open count.
- It shows under Suggestions, with its own count.
- Its row and its page show what the record says: its title, its collection,
  its group, its due date, and its description under the declared label.
- Who suggested it waits for phase 2: the app does not infer authorship from
  a collection, a property name or a description.

## Phase 2

- **Suggested changes.** A change an agent suggests to an existing task is a
  `core/recordpatchrequest`.
  - It is shown as a one-line note on the row, with Apply and Dismiss in the
    detail and in a Changes tab.
  - Apply and Dismiss are decided under `decide:<agent>` scopes the owner
    grants per agent, never `decide:*` by default.
  - The scoped surface first needs a list of the pending requests on the
    records a token reads, from the agents it decides for, filtered before
    pagination.
- **Provenance.** Who wrote a suggestion, read from its history, with the
  full actor reference one tap away.

## The UI

The same screens, words and order on iPhone, iPad, Android and the web.
TasksKit's presenter writes them, and the web's `present.ts` mirrors it.

**One accent.** A suggestion is marked by a small sparkle and its own
controls, never by a second color.

**One row shape:** a title, one meta line, and one trailing control.

- The meta line is due, then group, then collection where more than one is
  on.
- The trailing control is a check for a task, and Dismiss beside Accept for
  a suggestion.
- Controls are at least 44 points.

The screens:

- **Needs you:** pinned-group chips; one Suggestions row with its count;
  Late or urgent, with Move and Won't Do; Coming up; Later (dated); one
  counted No date row. The badge counts Late or urgent.
- **Suggestions:** a list with Dismiss and Accept on each row. Select picks
  several for one decision. On a phone, a row swipes either way.
- **A suggestion:**
  - its collection and when it was written;
  - the title;
  - the editable fields, set before accepting;
  - the description, under its declared label;
  - Details: every other property;
  - Dismiss and Accept, docked.
- **Lists:** search; Pinned; one section per group kind, headed by its label
  with its full reference on its own line. Each section lists the groups with
  open tasks, "No project", "Missing" where any are, and "N more with nothing
  open".
- **A group:** its kind and status, a Suggestions row, To Do, Done.
- **Settings → Collections:**
  - each candidate with its label, full reference, counts and a switch;
  - "View only" where the grant writes nothing;
  - "Save new tasks to".
  - A collection's page shows Group by, how its states read, and each field
    reading with its guess.
- **Wide (iPad, and the web from 1000 px):** the sidebar holds Needs you,
  Suggestions, No date, Pinned, each group kind's busiest groups with "All
  N…", and Settings; the list and the detail sit beside it.
- **The web** says "Select" and "Accept" where a phone swipes, and gives each
  action a keyboard shortcut.

## Keeping the platforms together

`PARITY.md` lists what each platform does. It cannot check that they do it
alike, so the contract becomes executable:

- **`apps/tasks/contract/` holds JSON cases.** Each gives declarations, the
  grant, settings records, records, the clock and the time zone, a command,
  and what follows: the collections read, each record's standing and actions,
  the Needs you bands, the suggestion queue, the group sections, and the
  writes in order.
- **TasksKit's tests and the web's vitest run every case.** A case one side
  fails fails the build.
- **The cases cover the hard edges:** the same id in two kinds, a renamed
  status, an enum status, a machine with two accept destinations, no due
  property, no order property, a group kind out of reach, a missing group
  record, a pinned group deleted.
- **Android crosses the bridge.**
  - Identities and commands carry the full record reference, not a bare id.
  - Rows carry their actions as data.
  - The bridge golden, the Kotlin protocol and the fingerprint are written
    again (`apps/tasks/android/bridge.json`), with a serialization test and
    the gallery's Compose check.

## Steps

1. **Vocabulary and server:** done on this branch, with the scoped
   implementors route's tests.
2. **The contract:** the readings, the actions, the identities and the
   settings records as above; the JSON cases for the first slice.
3. **The package, version 2:** `task` binds `lifecycle` and gains `proposed`
   (done). Then `collection` and `preference` join.
4. **The kits:** status reading (done in both), and closing and reopening the
   data link over a new set of kinds with the outbox kept.
5. **The first slice, on Swift, the web and Android together:**
   - several collections on;
   - suggestions with accept and dismiss;
   - Needs you across collections;
   - groups per collection;
   - Settings → Collections.
6. **The rest of the UI:** pins, search, No date, the Missing group, the
   chooser for several destinations, keyboard shortcuts.
7. **Phase 2.**

A system with its own task kinds takes part by binding `lifecycle` on its
task kind and listing `proposed:`. Its group kinds need nothing.
