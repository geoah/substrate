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
| `urgentCheckedAt`  | datetime                                | The assistant looked and agrees it cannot wait.                                                    |
| `urgentHeldAt`     | datetime                                | The assistant looked and thinks it can wait.                                                       |
| `urgentHeldReason` | string                                  | Why it can wait, in one sentence.                                                                  |

Rules:

- The app never writes `priority: urgent`. It writes `none`, `low`, `medium` or `high`, always with
  `prioritySetBy: owner`, and in the same write clears `priorityReason`, `priorityWas`, `urgentCheckedAt`,
  `urgentHeldAt` and `urgentHeldReason`.
- "Not urgent" writes `priority` back to `priorityWas` (or `none`) in that same shape.
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
  and choosing anything else rejects it.

## Routines: `substrate.reamde.dev/routines`

Traits and kinds only: no functions, so a package that requires it brings no reach.

- Trait `routines/series`: what a thing needs to be shown and ticked as a routine. Properties: `name`
  (string), `window` (enum), `status` (state), `group` (string). A kind that binds it also binds core
  `temporal(point)` and core `recurring`, which give its occurrences.
  - `window`: `exact`, `morning`, `afternoon`, `evening`, `anytime`.
  - `status`: `active` (initial), `paused`, `archived`.
  - `group`: series that share a group and a slot time are one row and one tap ("Morning medication, 5
    items"). Absent: a row of its own.
- Trait `routines/occurrencelog`: `status` (state), `scheduledAt` (datetime), `details` (markdown).
- Kind `routines/routine`: the general series (a habit, a ritual, a chore, a skincare step). Binds
  `routines/series`, `temporal(point)`, `recurring`. Properties: `name` (required), `category` (enum `chore
  exercise hygiene habit other`, default `other`), `window`, `status`, `group`, `recurrence`, `exdates`,
  `rdates`, `timezone`, `notes` (markdown).
- Kind `routines/log`: one log kind for every series. Binds `routines/occurrencelog` and `temporal(point)`
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

Reading: the app lists the kinds that implement `routines/series`, keeps their records, and reads the
occurrences with a window read (`filter.implements` the trait, `at` bounded) from yesterday's start to seven
days ahead. A series whose `status` is not `active` is left out by the app. Names and doses are shown inside
the app only; a notification, a widget and the Lock Screen say the group and a count.

## The app's own kinds: `substrate.reamde.dev/today`

Ids are fixed by what the record is about, so a writer upserts.

| Kind                | Id                         | Properties                                                                                                                                                                                                                                                                                                                  | Written by                                   |
| ------------------- | -------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------- |
| `today/preference`  | `default`                  | `timezone` (timezone), `prepareAt` (string `HH:MM`, default `06:30`), `lastActiveAt` (datetime), `nightReadAt` (datetime), `urgentWait` (string, an ISO 8601 duration, default `PT2M`), `quietProjects` (reference to `tasks/list`, repeated)                                                                                   | the app                                      |
| `today/plan`        | `d<YYYYMMDD>`              | `day` (date, required), `picked` (reference, any, repeated), `overlaps` (json: `[{events: [path, path], chosen: path, message: text, sentAt: datetime}]`), `sends` (json: `[{to: path, text, sentAt}]`), `moves` (json: `[{task: path, to: date}]`), `savedAt` (datetime), `wrapSavedAt` (datetime), `preparedAt` (datetime) | the app; `preparedAt` by the `prepare` function |
| `today/advice`      | the task's id              | `task` (reference to `tasks/task`, required, `mustExist`, cascade), `answer` (enum `task done dismiss`, required), `reason` (string), `evidence` (reference, any), `objectedAt` (datetime), `objection` (string)                                                                                                             | the assistant                                |
| `today/digest`      | the list's id, or `unfiled` | `project` (reference to `tasks/list`, `mustExist`, cascade; absent on `unfiled`), `since` (datetime), `until` (datetime), `line` (string: the latest thing, in one sentence), `count` (int), `items` (json: `[{at: datetime, text, sources: [path], people: [path]}]`, newest first, at most 30), `needsOwner` (bool), `needsOwnerReason` (string), `sources` (reference, any, repeated) | whatever keeps the owner's memory            |
| `today/reviewmark`  | the list's id, or `unfiled` | `project` (reference to `tasks/list`, cascade), `through` (datetime, required)                                                                                                                                                                                                                                               | the app, on "Got it"                         |
| `today/dossier`     | the occurrence's id        | `event` (reference, any: the event or its series), `slot` (datetime: the occurrence's start), `purpose` (string), `since` (markdown), `raise` (string, repeated), `after` (string), `tasks` (reference to `tasks/task`, repeated), `readFirst` (reference, any, repeated), `sources` (reference, any, repeated), `writtenAt` (datetime) | the assistant                                |
| `today/returnpoint` | the record's id            | binds `temporal(point)`; `record` (reference, any, required), `mark` (enum `note viewed arrived`), `text` (string)                                                                                                                                                                                                           | the app                                      |
| `today/rule`        | any                        | `text` (string, required), `scope` (enum `source person channel kind sender`, required), `match` (string, required), `effect` (enum `hold raise briefing skip include urgent`, required), `from` (reference, any), `enabled` (bool, default true)                                                                             | the owner                                    |

What the digest means: for one project, what came in after `since` and up to `until`. The app shows the
items newer than that project's `today/reviewmark.through` as "what is happening", and the items between
`today/preference.lastActiveAt` of the day before and waking as the night. "Got it" writes the review mark
at the digest's `until`. A digest whose items are all at or before the mark is not drawn. Whatever writes
digests keeps the items newer than the mark and may drop the rest.

The package also holds:

- **Function `fade`**, on a daily schedule: a `tasks/task` still `proposed` two weeks after it was created
  is abandoned. An automation carrying out the owner's rule, not the assistant.
- **Function `prepare`**, on an hourly schedule and when a suggested task or an unchecked urgent mark
  arrives: once a day at `today/preference.prepareAt` it asks the assistant to write a `today/advice` for
  every suggested task without one, a `today/dossier` for today's meetings, and patch requests for the flags
  it suggests; in between, only the advice or the check that is missing.
- **The assistant's standing instructions**, as `llm/instruction` records with fixed ids and `source:
  import`.

## Meetings

A meeting is an occurrence of any kind that binds core `temporal(range)` with `recurring` or `override`
(a calendar's event and its series). The app reads today and the next two days with a window read. Two that
overlap are an overlap, settled in the plan (`today/plan.overlaps`).

## Writes the app makes

Log or skip an occurrence (`routines/log`). Answer suggested tasks (`status` to `open`, `done` or
`abandoned`). Add a task. Set a flag (the rule above). Take urgent off. Set or remove a project's
`priority`. Move a task (`notBefore` or `dueDay`, with `deferrals`). Mark a task done. Accept or reject a
patch request. Save the plan and the wrap (`today/plan`, and the task writes its ticks imply). "Got it"
(`today/reviewmark`). Mark the night read and the last look (`today/preference`). Keep a return point.
Every write goes through the outbox, so it works offline and is sent when the app reconnects.
