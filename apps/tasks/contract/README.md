# The Tasks contract

What Substrate Tasks reads from any task-like kind, and what it writes back,
as JSON cases every platform runs ([the plan](../../../docs/plans/tasks-sources.md)).
TasksKit's tests (Swift, which Android draws through the bridge) and the web
app's vitest load every `*.json` file here, build their model from the case's
input, and compare it to the case's `expect` and each command's `writes`. A
case one side fails fails that side's build. Words on screen are not here:
they are the presenters' (`TasksPresenter`, `present.ts`, `PARITY.md`).

## A case

```text
name        one line
now         RFC 3339, the clock
timeZone    IANA, the device's zone
grant       the token's scopes
declarations  {identity, label?: {singular, plural}, definition} for every kind
            the token reads that the case needs: candidates, group kinds,
            the settings kinds
records     envelopes {kind, id, version, createdAt, properties, deletedAt?}:
            tasks, groups and the settings records
expect      collections, items, needsYou, suggestions, groups (below)
commands    [{do, writes}] each against the input, never chained
```

A record path is `<kind>/<id>`. Two kinds may hold the same id, so a path is
the only identity.

## The readings

A **candidate** is a declaration that binds `substrate.reamde.dev/core/lifecycle`
with a status the kits read (`KindStatus`). A **collection** is a candidate
that is on:

- the app's own `substrate.reamde.dev/tasks/task` unless its collection record
  says `enabled: false`;
- any other only where its collection record says `enabled: true`.

The collection record of a kind is the `substrate.reamde.dev/tasks/collection`
record whose `kind` names it. Its id is the UUIDv8 of the sha256 of
`collection:` + the kind, but a case reads it by `kind`.

The **grant** reads a kind when a `read:` or `write:` scope covers it and
writes it when a `write:` scope does. A scope covers a kind by:

- `*`;
- `<authority>/<package>/*` over the kinds of that package;
- the kind exactly;
- `implements(<trait>)` over a kind whose declaration binds the trait and
  whose `purpose` is not `internal`.

Each reading is the collection record's property where it holds one (`-`
means none), else the guess:

| Reading  | Guess                                                                                                                          |
| -------- | ------------------------------------------------------------------------------------------------------------------------------ |
| title    | shown: `properties.title`, else the id. Editable: the property `displayTemplate` names when it is exactly `{name}` and `name` is a string; else none |
| due      | the property a `temporal(point: <name>)` binding names (the trait admits `at`, `endsAt` and `dueAt`), declared or supplied by the trait; else a declared `datetime` named `dueAt`; else none |
| dueDay   | a `date` named `dueDay`; else none                                                                                              |
| priority | an `enum` named `priority`; else none                                                                                           |
| order    | a `decimal` named `position`; else none                                                                                         |
| group    | the one single (not repeated) `reference` named `list`, `project` or `area` whose `kind` the grant reads; none when there is no such reference or more than one |

Each collection also says:

- **`viewOnly`:** the grant does not write the kind.
- **`creatable`:**
  - the grant writes the kind;
  - it is the preference's `newTasks` (absent: the app's own task);
  - every `required` property is the editable title or one of the readings.

## A record

Every record of a collection that is not deleted is an item.

- **`standing`:** `proposed`, `open`, `done` or `dropped`, from its status
  value (`KindStatus.standing`).
- **Moves:** the status value's moves (a machine's declared moves, or an
  enum's every other value), each with its standing.
- **Actions:** each is the moves to one standing, empty unless the
  collection is writable:
  - `accept`: proposed to open;
  - `dismiss`: proposed to dropped;
  - `complete`: open to done;
  - `wontDo`: open to dropped;
  - `reopen`: done or dropped to open.

  `accept` and `reopen` never lead to a proposed value. A move that names an
  undeclared value is not a move.
- **`group`:** the group reference's record path, `none` when the record holds
  none, `missing` when it names a record the token does not hold or holds
  deleted. `null` when the collection has no group reading.
- **Its due:** the due reading's instant, and the due day reading's day, else
  the instant's day in `timeZone`.
- **`band`, for an open item only:**
  - dated: `lateOrUrgent` when past due (the instant before `now`, else the
    day before today), `comingUp` when due within today and the next 7 days,
    `later` beyond;
  - undated: `lateOrUrgent` when its priority is pressing, else `noDate`.
- **Pressing:** a priority's declared values, lowest first, are pressing as
  follows. Of four or more, the top two; of two or three, the top one; of
  one, none.

## Order

- **lateOrUrgent:** dated first, by due ascending (an instant itself; a day
  alone at its start in `timeZone`); then undated by priority, highest first.
- **comingUp, later:** by due ascending.
- **Ties, in any band:** the order reading where both are of one kind and
  hold one, as numbers; then `createdAt`; then the path.
- **suggestions:** every proposed item, newest `createdAt` first, ties by
  path.

## Expect

```text
collections  [{kind, enabled, viewOnly, creatable, title: name|null, due, dueDay,
              priority, order: name|null, group: {property, kind}|null}],
             every candidate, the app's own first, then by kind
items        {path: {standing, accept, dismiss, complete, wontDo, reopen: [values],
              group, band: band|null}}
needsYou     {lateOrUrgent, comingUp, later: [paths], noDate, suggestions, badge: counts}
             badge is lateOrUrgent's count; suggestions never count as open
suggestions  [paths]
groups       [{collection, kind, rows: [{group: path|none|missing, open}]}]
             one per collection with a group reading, in collections' order;
             rows are groups holding open items, in the group kind's order
             reading (a decimal named position) where it has one, else by
             title; then none, then missing; each only when its count is above 0
pins         [paths], where a case says it: the preference's pins that show, in
             its order: a record held and not deleted, of the group kind of an
             enabled collection's group reading
```

## Commands

```text
{accept|dismiss|complete|wontDo|reopen: path, to?: value, edits?: {property: value}}
```

- **The value:** the action's one value, or `to` when it names one of them.
- **A choice:**
  - With several values and no `to`, the expected `writes` is empty and the
    case says `choices: [values]`.
  - An action not offered writes nothing.
- **The write:** one patch, `{op: "patch", path, ifVersion: version,
  properties}`. The properties are the status property set to the value,
  then `edits`.
- **Reopening** also sets to `null` each property the move into the current
  state stamps (a machine transition's `stamps`), so a reopened task holds no
  completion time.

## Words

The new screens' words, written once here so every platform says the same
thing. A presenter writes them; a test on each side holds its presenter to
them. `{n}`, `{collection}` and the like are filled in, and a count of one
takes the singular.

| Where                     | Words                                                                                                   |
| ------------------------- | ------------------------------------------------------------------------------------------------------- |
| Needs you, the queue row  | `{n} suggestions` / `1 suggestion`; under it `Tasks waiting for you to accept or dismiss`               |
| Needs you, the undated row| `No date`; under it `Open tasks with no due date`; its count                                            |
| The queue                 | title `Suggestions`; under it `Accept one to put it on your list, or dismiss it.`; `Select`; empty: `No suggestions`, `Suggested tasks show here until you accept or dismiss them.` |
| A suggestion's row        | its title; meta `{collection} · {written}` then its due and its group; `Dismiss`, `Accept`              |
| A suggestion's page       | `Suggestion in {collection} · written {written}`; the description under the property's label; `Details`; `Collection`; docked `Dismiss`, `Accept` |
| Several destinations      | a sheet titled `Accept as`, `Dismiss as`, `Done as`, `Won't do as` or `Reopen as`, each value by its declared label, and `Cancel` |
| Select                    | `Select`; while selecting `{n} selected`, `Accept`, `Dismiss`, `Cancel`                                 |
| Undo                      | `Accepted`, `Dismissed`, `Done`, `Won't do`, each with `Undo`                                           |
| Lists                     | `Search lists and projects` (the group kinds' plural labels, lower case, joined); `Pinned`; a section per group kind headed by its plural label with its full reference under it; `No {reference label}` (`No list`, `No project`) with `Tasks with no {reference label}` under it; `Missing`, with `Tasks whose {reference label} is gone or out of reach`; `{n} more with nothing open` |
| A group                   | `{kind label} · {status}` and `{n} open` under the title; the queue row as on Needs you; `To Do`; `Done`; `Pin`, `Unpin` |
| Settings                  | a row `Collections`, `{n} on`                                                                           |
| Collections               | title `Collections`; `Every collection of yours with a done state. Turn one on to see its tasks here; Substrate Tasks then asks to change them.`; section `Show tasks from`; a row's meta `{n} open`, `{n} suggested`, `grouped by {reference label}`, `View only`; footer `Turning a collection off only hides it here. Its tasks stay where they are.`; section `Save new tasks to`, row `New tasks` |
| A collection              | its label as the title and its full reference under it; `Group by`, each single reference by its label with its target kind under it, and `Nothing`, `One list of every task`; `How its states read`: `Suggestion`, `Open`, `Done`, `Dropped`, each with its values; `Fields the app shows`: `Title`, `Due`, `Due day`, `Priority`, `Order`, each with its property or `None` |
| Approval                  | turning a collection on: `Substrate Tasks needs your approval to change your {collection}.` and `Approve`; refused: `Not approved. {collection} stays view only.` Grouping by a kind out of reach: `Substrate Tasks needs your approval to read your {group kind plural}.` and `Approve`; refused: `Not approved. {collection} stays ungrouped.` |
| Lists, ungrouped          | a section `Collections` holding each on collection that groups by nothing, with its open count          |
| A group kind's label      | its declared label where the token reads its declaration, else its name in words (`area` gives `Area`, `Areas`) |
