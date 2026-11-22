# What Substrate Today reads and writes

The pane is drawn from records and writes a few of them. This page names every kind and property the app
depends on, who writes each, and the rules that turn records into what the pane shows. The packages are
`substrate.reamde.dev/routines`, `substrate.reamde.dev/today` and `substrate.reamde.dev/health` (in the
binary's catalogue) and `substrate.reamde.dev/tasks` (published by Substrate Tasks). The app asks for `full`
and publishes nothing, so the owner adds the packages from the catalogue.

A kind the repository does not hold is a part of the pane that stays empty: the app reads only the kinds it
finds, and never fails because a package is missing.

## Projects and tasks: `substrate.reamde.dev/tasks`

A project is a `tasks/list`. A task and a suggested task are both a `tasks/task`: a suggested task is one
whose `status` is `proposed`.

Version 4 adds optional properties only, none with a default, so a repository takes it without review. An
absent value reads as the first one named.

`tasks/list`, added in version 4:

| Property   | Type                         | What                                                                                 |
| ---------- | ---------------------------- | ------------------------------------------------------------------------------------ |
| `priority` | enum `none low medium high`  | The project's flag. The strip is the active lists whose `priority` is not `none`.     |
| `sources`  | reference, any kind, repeated | Where its work happens: an issue tracker's project, a channel, a repository.         |

`tasks/task`, added in version 4:

| Property           | Type                                    | What                                                                                               |
| ------------------ | --------------------------------------- | -------------------------------------------------------------------------------------------------- |
| `notBefore`        | datetime                                | Hidden until then. "When" writes it.                                                               |
| `deferrals`        | int                                     | How many times `notBefore` or `dueDay` was moved later. Whoever moves it adds one in the same write. |
| `prioritySetBy`    | enum `owner assistant automation api`   | Who set the `priority` it has now. Absent reads as `owner`.                                        |
| `priorityReason`   | string                                  | The setter's one sentence, shown beside an urgent pill.                                            |
| `priorityWas`      | enum `none low medium high`             | Written in the same write as `urgent`: what "Not urgent" puts back.                                |
| `urgentCheckedAt`  | datetime                                | The mark was checked, and it cannot wait.                                                          |
| `urgentHeldAt`     | datetime                                | The mark was checked, and it can wait.                                                             |
| `urgentHeldReason` | string                                  | Why it can wait, in one sentence.                                                                  |

Rules:

- The app never writes `priority: urgent`. It writes `none`, `low`, `medium` or `high`, always with
  `prioritySetBy: owner`, and in the same write clears `priorityReason`, `priorityWas`, `urgentCheckedAt`,
  `urgentHeldAt` and `urgentHeldReason`.
- "Not urgent" writes `priority` back to `priorityWas` (or `none`) in that same shape.
- Whoever sets `urgent` writes `priorityWas`, `prioritySetBy` and `priorityReason` in the same write, and
  clears `urgentCheckedAt`, `urgentHeldAt` and `urgentHeldReason` in it, so a mark set again is checked
  again.
- A task whose `priority` is `urgent` is in one of three states:
  - **checked**: `prioritySetBy` is `assistant`, or `urgentCheckedAt` is set, or `prioritySetBy` is `owner`
    or absent (set in another app by the owner's hand). It interrupts.
  - **held**: `urgentHeldAt` is set and `urgentCheckedAt` is not. It waits in what is happening, with the
    urgent pill and `urgentHeldReason`.
  - **not checked**: `prioritySetBy` is `automation` or `api` and neither stamp is set. It waits for
    `today/preference.urgentWait` (two minutes) from the write that set it, then interrupts, and its card
    says nobody has checked it.
- A task with `notBefore` in the future is not in the order of the day. From `deferrals >= 3` its row says
  how many times it was put off.
- What the assistant suggests on a task or list that already exists is an open
  `substrate.reamde.dev/core/recordpatchrequest` whose `target` is that record and whose `diff` sets
  `priority` or `list`. The pane draws it as the suggested flag or project; choosing it accepts the request,
  and choosing anything else rejects it. A decision is a `PATCH` of the request's `decision` to `accepted`
  or `rejected`, and it must carry `ifVersion`. An accept may carry `adjustedDiff`, which is applied in place
  of `diff`: the app accepts a flag on a task with `{"properties": {"priority": …, "prioritySetBy":
  "owner"}}`, so the rule above holds for a flag taken from a suggestion too.

## Routines: `substrate.reamde.dev/routines`

Traits and kinds only: no functions, so a package that requires it brings no reach.

- Trait `routines/series`: what a thing needs to be shown and ticked as a routine. Properties: `name`
  (string), `window` (enum), `status` (state), `group` (string). A kind that binds it also binds core
  `temporal(point)` and core `recurring`, which give its occurrences.
  - `window`: `exact`, `morning`, `afternoon`, `evening`, `anytime`.
  - `status`: `active` (initial), `paused`, `archived`.
  - `group`: occurrences on the same local day whose series share a group are one row and one tap
    ("Morning medication, 5 items"), in slot order, at the time of the first slot; a tap logs each at its own
    slot. So the steps of one routine can sit a minute apart to keep their order. Absent: a row of its own.
- Trait `routines/occurrencelog`: `status` (state), `scheduledAt` (datetime), `details` (markdown).
- Kind `routines/routine`: the general series (a habit, a ritual, a chore, a skincare step). Binds
  `routines/series`, `temporal(point)`, `recurring`. Properties: `name` (required), `category` (enum `chore
  exercise hygiene habit other`, default `other`), `window`, `status`, `group`, `recurrence`, `exdates`,
  `rdates`, `timezone`, `notes` (markdown).
- Kind `routines/routinelog`: one log kind for every series. (A kind may not be named `log` alone.) Binds `routines/occurrencelog` and `temporal(point)`
  (`at` is when it was logged). Properties: `series` (reference pinned at trait `routines/series`,
  required, `mustExist`, `onDelete: cascade`), `scheduledAt` (datetime, the slot), `status` (state `done`
  or `skipped`, initial `done`, each way allowed), `evidence` (reference, any kind: what satisfied it;
  absent when ticked by hand), `details` (markdown).
  - Its id is `<series id>_<slot>`, the slot as UTC `YYYYMMDDTHHMMSSZ`: the id the window read gives the
    occurrence. So one occurrence has at most one log, and a tick made twice offline is one record.
  - A slot with no log is what missed means.

`substrate.reamde.dev/health` version 3 requires `routines` and adds:

- Kind `health/medication`: `name` (required), `activeIngredient`, `form` (enum), `strengthAmount` (float),
  `strengthUnit` (enum), `category` (enum `prescription otc supplement skincare`), `notes` (markdown).
- Kind `health/medicationschedule`: a dose on a rule. Binds `routines/series`, `temporal(point)`,
  `recurring`. Properties: `name` (required; what the row says, such as the medication's name),
  `medication` (reference to `health/medication`, required, `mustExist`, `onDelete: cascade`),
  `doseAmount` (float), `doseUnit` (enum), `window`, `status`, `group`, `recurrence`, `exdates`, `rdates`,
  `timezone`, `notes`.

What the server does with these, as checked on a dev server:

- A window read with `filter.implements` naming `substrate.reamde.dev/routines/series` and `at` bounded on
  both ends answers the occurrences of every kind that binds the trait, each a record of the series' kind with
  `computed: true`, `version: 0`, the id `<series id>_<slot>`, the series' own properties and the slot as `at`.
  The same filter with no `at` bound answers the series rows.
- A paused series is not left out: its occurrences come back with `status: paused`. Adding
  `"status": {"eq": "active"}` to the filter's `properties` leaves them out on the server.
- A log is written with `PUT` at `<series id>_<slot>`. With `ifVersion: 0` the first write creates it and a
  second answers `409 conflict`; with no `ifVersion` the write is an upsert, which is what a tick replayed
  from an outbox or made on a second device needs. Done to skipped and back is a `PATCH` of `status`.
- `series` takes a full `<kind>/<id>` path. A record that does not bind the trait, or does not exist, is
  refused.
- A routine, a medication and a schedule take a minted id: leave the id out, or send a UUID in lowercase.

Reading: the app lists the kinds that implement `routines/series`, keeps their records, and reads the
occurrences with a window read (`filter.implements` the trait, `at` bounded) from yesterday's start to seven
days ahead. A series whose `status` is not `active` is left out by the app. Names and doses are shown inside
the app only; a notification, a widget and the Lock Screen say the group and a count.

## The app's own kinds: `substrate.reamde.dev/today`

Every kind but `rule` takes the id its writer names (`ids: named`), fixed by what the record is about, so a
writer upserts. A `rule` takes a minted id.

A reference to a task or a task list is pinned at core's `lifecycle` trait, not at the tasks package's kinds:
this package ships in the binary and `tasks` is published by an app, so it cannot be required. The value is
a full `<kind>/<id>` path, such as `substrate.reamde.dev/tasks/list/<id>`, and a record that does not bind
`lifecycle` is refused. On a repository without `tasks` the package installs and those parts stay empty.

| Kind                | Id                          | Properties                                                                                                                                                                                                                                                                                                                                                                          | Written by                        |
| ------------------- | --------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------- |
| `today/preference`  | `default`                   | `timezone` (timezone), `prepareAt` (string `HH:MM`, default `06:30`), `lastActiveAt` (datetime), `nightReadAt` (datetime), `urgentWait` (duration, an ISO 8601 string, default `PT2M`), `assistantPrepares` (bool, default true), `quietProjects` (reference at `lifecycle`, repeated)                                                                                              | the app                           |
| `today/plan`        | `d<YYYYMMDD>`               | `day` (date, required), `picked` (reference, any, repeated), `overlaps` (json: `[{events: [path, path], chosen: path or "both", at: datetime, message: text, sentAt: datetime}]`; `at` is when it was chosen), `sends` (json: `[{to: path, text, sentAt}]`), `moves` (json: `[{task: path, to: date or "dropped"}]`: where the wrap sent each thing that carried over, `"dropped"` for one closed as not done), `savedAt` (datetime), `wrapSavedAt` (datetime)                                                                                 | the app, and nothing else         |
| `today/advice`      | the task's id               | `task` (reference at `lifecycle`, required, `mustExist`, cascade), `answer` (enum `task done dismiss`, required), `reason` (string), `evidence` (reference, any), `objectedAt` (datetime), `objection` (string)                                                                                                                                                                     | the assistant                     |
| `today/digest`      | the list's id, or `unfiled` | `project` (reference at `lifecycle`, `mustExist`, cascade; absent on `unfiled`), `since` (datetime), `until` (datetime), `line` (string: the latest thing, in one sentence), `count` (int), `items` (json: `[{at: datetime, text, sources: [path], people: [path]}]`, newest first, at most 30), `needsOwner` (bool), `needsOwnerReason` (string), `sources` (reference, any, repeated) | whatever keeps the owner's memory |
| `today/reviewmark`  | the list's id, or `unfiled` | `project` (reference at `lifecycle`, cascade), `through` (datetime, required)                                                                                                                                                                                                                                                                                                       | the app, on "Got it"              |
| `today/dossier`     | the occurrence's id         | `event` (reference, any: the event or its series), `slot` (datetime: the occurrence's start), `purpose` (string), `since` (markdown), `raise` (string, repeated), `after` (string), `tasks` (reference at `lifecycle`, repeated), `readFirst` (reference, any, repeated), `sources` (reference, any, repeated), `writtenAt` (datetime)                                              | the assistant                     |
| `today/returnpoint` | the record's id             | binds `temporal(point)`; `record` (reference, any, required), `mark` (enum `note viewed arrived`), `text` (string)                                                                                                                                                                                                                                                                  | the app                           |
| `today/rule`        | minted                      | `text` (string, required), `scope` (enum `source person channel kind sender`, required), `match` (string, required), `effect` (enum `hold raise briefing skip include urgent`, required), `from` (reference, any), `enabled` (bool, default true)                                                                                                                                   | the owner                         |
| `today/preparation` | by what is asked            | `day` (date, required), `reason` (enum `morning advice urgent`, required), `ask` (text, required: what is asked for, in full), `subjects` (reference, any, repeated) |
| `today/urgentcheck` | the task's id               | `task` (reference at `lifecycle`, required, `mustExist`, cascade), `verdict` (enum `cannotwait canwait`, required), `reason` (string), `checkedAt` (datetime). The app does not read it: `stampurgent` turns it into the task's stamps |

What the digest means: for one project, what came in after `since` and up to `until`. The app shows the
items newer than that project's `today/reviewmark.through` as "what is happening", and the items between
`today/preference.lastActiveAt` of the day before and waking as the night. "Got it" writes the review mark
at the digest's `until`. A digest whose items are all at or before the mark is not drawn. Whatever writes
digests keeps the items newer than the mark and may drop the rest.

The plan is the app's alone. Nothing else creates or stamps it, so the app's own create of the day's plan
never meets a record already there.

The package also holds:

- **Function `fade`**, on a daily schedule: a `tasks/task` still `proposed` two weeks after it was created
  is abandoned, under the version it read, so a task the owner answers meanwhile is left alone. It also
  deletes `today/preparation` records of that age. An automation carrying out the owner's rule, not the
  assistant. It does nothing until the app has written `today/preference`, so adding the package alone
  abandons nothing.
- **Function `prepare`**, every half hour, and when an urgent mark arrives that nobody has checked. It
  calls no model. It works out what has not been prepared and asks for it by writing a `today/preparation`
  record, whose id is derived from what is asked, so asking twice is one record:
  - `d<YYYYMMDD>`, once the owner's clock passes `prepareAt` in their `timezone`: a dossier for at most six
    of the day's meetings, and at most three patch requests for flags.
  - `a…`: advice for the suggested tasks that have none, at most eight per ask and the newest first, at
    most every two hours, and only while at least one has none. No trigger fires when a suggestion
    arrives.
  - `u…`: the check of one urgent mark an automation or the API set.

  A normal day is one run for the morning, three to six for advice (twelve at most), and one per urgent
  mark. It asks for nothing until the app has written `today/preference`, and nothing while
  `assistantPrepares` is false.
- **Agent `preparer`**, run by a trigger on each new `today/preparation`, with the record as its message.
  It is not the owner's assistant: it runs on the provider bound to the package's `model` input, on a
  mid-priced model, against a daily spend of its own (100 cents), so preparing a day never spends what the
  owner's chats are bought with. It reads what other people wrote (a suggested task's words come from a
  mail or a message), so it writes only `today/advice`, `today/dossier`, `today/urgentcheck` and patch
  requests, and holds no write on a task or a list. A function may call an agent only inside its own sixty
  seconds, which a morning does not fit, so the ask is a record. An install or an upgrade turns a shipped
  trigger back on, so the switch that lasts is `assistantPrepares`.
- **Function `stampurgent`**, when a `today/urgentcheck` is written: it writes `urgentCheckedAt`, or
  `urgentHeldAt` with `urgentHeldReason`, on the task, only while its mark is still unchecked, and nothing
  else.
- **The assistant's standing instructions**, as ten `llm/instruction` records with ids that start `today-`
  and `source: import`, for where the owner talks to their assistant. One of them, `today-add`, is what the
  app's field relies on: a to-do the owner gives the assistant is written as an open task with `notBefore`
  now, which is what puts it in today's list, in the list named, and a list that does not exist is made. They name the agent, so they hold
  wherever it works. The owner edits or retires one like their own, and an upgrade never writes over a
  record the owner changed.

After the package is added, its `model` input is bound to the AI provider the preparer runs on, unless the
repository has one provider or one named `default`. Until it resolves, nothing is prepared.

Not built: a function that carries out a `today/rule` whose effect is `urgent`. Messages have no trait a
package could read them by, so it would have to name every provider's message kind.

## Meetings

A meeting is an occurrence of any kind that binds core `temporal(range)` with `recurring` or `override`
(a calendar's event and its series). The app reads today and the next two days with a window read. Two that
overlap are an overlap, settled in the plan (`today/plan.overlaps`).

One event synced from two accounts is one meeting: two events with the same start, the same end and the same
title (compared without case or surrounding space) are drawn once, with the attendees of both. An event titled
only "Busy" at the same start and end as a named one is that one's shadow and is not drawn.

## People

A person is a `substrate.reamde.dev/core/contact`, read by reference from a task's `people` and a meeting's
attendees. The app reads `name`, `givenName`, `familyName`, `nickname` and the reserved `title`. The name it
shows is `name` when it has an upper-case letter; otherwise `givenName` and `familyName`; otherwise a `title`
with an upper-case letter; otherwise the nickname, or the handle `name` or `title` holds, with its first letter
in upper case. Inside a sentence ("for …", "with …") it is the given name, else the first word of the name.

## Writes the app makes

Log or skip an occurrence (`routines/routinelog`). Answer suggested tasks (`status` to `open`, `done` or
`abandoned`). Add a task. Set a flag (the rule above). Take urgent off. Set or remove a project's
`priority`. Move a task (`notBefore` or `dueDay`, with `deferrals`). Mark a task done. Accept or reject a
patch request. Save the plan and the wrap (`today/plan`, and the task writes its ticks imply). "Got it"
(`today/reviewmark`). Mark the night read and the last look (`today/preference`). Keep a return point.
Every write goes through the outbox, so it works offline and is sent when the app reconnects.
