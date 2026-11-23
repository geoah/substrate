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
  - A log taken back is purged, not kept as deleted: the server refuses a create over a record it keeps, so
    a kept one would stand in the way of the same occurrence being ticked again.
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

- **Function `fade`**, on a daily schedule: a `tasks/task` still `proposed` two weeks after what it came
  from was said ([`originAt`](#when-a-tasks-words-were-said-originat), else when it was created) is
  abandoned, under the version it read, so a task the owner answers meanwhile is left alone. It also
  deletes `today/preparation` records of that age. An automation carrying out the owner's rule, not the
  assistant. It does nothing until the app has written `today/preference`, so adding the package alone
  abandons nothing.
- **Function `prepare`**, every half hour, and when an urgent mark arrives that nobody has checked. It
  calls no model. It works out what has not been prepared and asks for it by writing a `today/preparation`
  record, whose id is derived from what is asked, so asking twice is one record:
  - `d<YYYYMMDD>`, once the owner's clock passes `prepareAt` in their `timezone`: a dossier for at most six
    of the day's meetings, and at most three patch requests for flags.
  - `a…`: advice for the suggested tasks that have none, at most eight per ask and the most lately said
    first, at most every two hours, and only while at least one has none. No trigger fires when a suggestion
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
- **Function `stamporigin`**, when a `tasks/task` names an `origin` and carries no `originAt`: it writes
  the origin's own `at` on the task as `originAt`, and nothing else
  ([below](#when-a-tasks-words-were-said-originat)).
- **The assistant's standing instructions**, as twelve `llm/instruction` records with ids that start `today-`
  and `source: import`, for where the owner talks to their assistant. `today-correct` and `today-dates` are
  described [below](#the-owners-corrections-todaycorrection). One of them, `today-add`, is what the
  app's field relies on: a to-do the owner gives the assistant is written as an open task with `notBefore`
  now, which is what puts it in today's list, in the list named, and a list that does not exist is made. They name the agent, so they hold
  wherever it works. The owner edits or retires one like their own, and an upgrade never writes over a
  record the owner changed.

After the package is added, its `model` input is bound to the AI provider the preparer runs on, unless the
repository has one provider or one named `default`. Until it resolves, nothing is prepared.

Not built: a function that carries out a `today/rule` whose effect is `urgent`. Messages have no trait a
package could read them by, so it would have to name every provider's message kind.

## When a task's words were said: `originAt`

`tasks/task`, added in version 5 of the tasks package:

| Property   | Type     | What                                                                                              |
| ---------- | -------- | ------------------------------------------------------------------------------------------------- |
| `originAt` | datetime | When what the task was made from (`origin`) was said or happened. Absent where its maker did not know. |

A task is often written later than its words: a pass reads a day's messages at night, and a migration
carries weeks of them over in a minute. The record's `createdAt` is then when it was written down, and a
reader that counts from it says "heard yesterday" of words a week old. So:

- **A suggestion's age is counted from `originAt`**, and from `createdAt` only where the task has none.
  That is the rule for the app's "Heard …" line, for the order suggestions are drawn and advised in, and for
  `fade`. Where a task has no `originAt` the app says when it was suggested, not when it was heard.
- **Whoever writes `origin` writes `originAt` in the same write**, from the time the origin itself carries
  (its `at`). The app writes neither: it makes no task from a record.
- **`stamporigin` holds the rule for a writer that did not.** A trigger (`today-stamp-on-origin`) fires on
  a task with an `origin` and no `originAt`; the function reads the origin and writes its `at`. It writes
  nothing where the origin is gone, has no `at`, or is later than the task itself (a meeting next week that
  someone asked the owner to prepare for is not when the matter was said). The origin's own `createdAt` is
  never used: that is when a mirror wrote the row down.
- **An `originAt` after now is not believed** by `fade`, and reads as the creation time.
- **A day in a name or a description is its date.** What a writer puts into a record is read on a later
  day, so "tomorrow", "yesterday" and a weekday alone are wrong by the time they are read. The preparer's
  prompt and the assistant's instruction `today-dates` say so, and Mneme's reflection (version 49) holds
  its own writers to it: "on Thursday 19 November", never an ISO date, and a record named by a link
  (`[message](substrate://<kind>/<id>)`), never by its path. Words that are quoted stay as they were said.
  Text written before this keeps what it has.

## Where a task came from: its links

A task says what it was made from in `origin`, a reference to any kind: a chat message, a mail, an issue, a
transcript. The app gives a task two ways out to it, and a suggested task the same two.

- **The record.** `<server>/data/<kind>/<id>`: the origin's own page in the server's console, in a browser.
  It needs no read: the address is made from the reference.
- **Its page at the source.** No kind declares which of its addresses is the record's own page, so the app
  finds it by the names the providers use. It reads the origin and takes the first of `permalink`,
  `htmlUrl`, `htmlLink`, `webViewLink`, `externalUrl`, `url` and `publicUrl` that holds an `http` or `https`
  address whose host does not start with `api.` (GitHub's `url` is its API's; its page is `htmlUrl`). The
  place is named from the host: Slack, Gmail, Google Docs, Google Drive, Google Calendar, Linear, GitHub,
  Notion, else the host itself.
- **A Gmail message names no address**, so its page is composed as the provider's own tools compose it:
  `https://mail.google.com/mail/u/<the account's email>/#all/<the thread's threadId>`, from the message's
  `thread` and `account` references, which are read too. Without the account's address there is no link:
  it would open in whichever account the browser has first, and find nothing.
- A record with none of these (a chat mirrored from a bridge that has no web address) has the record link
  alone.

Reading: the origins of the tasks that are `open` or `proposed`, eight at a time and one batch at once, each
once. What is found is kept on the device (`today-sources.json` in the repository's folder), so the links
are there offline and are not read again. A record that is gone or that the app may not read names no page
and is not asked for again until the next launch; a read that fails otherwise is asked again when the app
comes back to the front. Signing out empties what was kept.

A meeting's link is its event's `htmlLink`, which the window read already holds. What a dossier names in
`readFirst` opens as its record, or at its source where that page was read for a task.

A kind that named its own page (a trait with one `url` property, say) would let every app do this without
guessing by name. None does today.

## The owner's corrections: `today/correction`

Added in version 4 of the today package. What the owner said was wrong with something the assistant
suggested, or what to do instead, in their words.

| Kind               | Id     | Properties                                                                                          |
| ------------------ | ------ | --------------------------------------------------------------------------------------------------- |
| `today/correction` | minted | `subject` (reference, any kind, required: the record corrected, a suggested task today), `text` (string, required: the owner's words) |

- **The app writes it**, one per answer of "Other" on a suggestion, with an id it chooses (a UUIDv7, as it
  chooses a task's), `subject` the suggested task's full path and `text` what the owner typed. It is
  offered only where the repository holds the kind. `createdAt` is when it was said.
- **The same words go to the assistant** with the task as context, and the instruction `today-correct`
  has it do what they ask: change the task, move it, or mark it abandoned, and say in one sentence what it
  did. It writes no correction itself. Where the assistant cannot be reached the record is still written.
- **Nothing in this package reads a correction.** It is kept for whoever suggests things: Mneme's
  reflection reads the latest twenty on every pass, each with the name of what it corrects, under a line
  that says the owner corrected these suggestions and the mistake is not to be made again
  (`suggestionCorrections` in its window). It is also what a person reads to see what a suggester gets
  wrong.
- `subject` carries no `mustExist` and nothing cascades: a suggestion that fades is abandoned, and a
  correction outlives what it was about.
- Kept for good, like the package's other records.

## Meetings

A meeting is an occurrence of any kind that binds core `temporal(range)` with `recurring` or `override`
(a calendar's event and its series). The app reads today and the next two days with a window read. Two that
overlap are an overlap, settled in the plan (`today/plan.overlaps`). In the day they stay two lines, tied to
each other: marked while no choice is made, and the one not gone to struck through once one is.

One event synced from two accounts is one meeting: two events with the same start, the same end and the same
title (compared without case or surrounding space) are drawn once, with the attendees of both. An event titled
only "Busy" at the same start and end as a named one is that one's shadow and is not drawn.

### Whose calendar an event is on

An account syncs every calendar it can see: the owner's own, a colleague's, a shared one. Only an event on a
calendar of the owner's own is a meeting of theirs, and the others are drawn nowhere: not in the day, in what
is done, in an overlap, on the plan or on a page.

An event names its calendar in a reference property, `calendar`. After the window read the app reads every
record of the kinds those references name, and keeps them with the meetings. A calendar is the owner's own by
what its record says, in the Google provider's words:

- it is never one the owner hid (`hidden` is true);
- it is the account's own calendar (`primary` is true); or
- it is a second calendar the account owns (`accessRole` is `owner`, and it names a `dataOwner`).

Another person's calendar is neither, whatever the owner may do on it: `reader`, `freebusyreader` and `writer`
are not the owner's, and nor is a person's own calendar on which the owner was given `owner` rights, which has
no `dataOwner`. The attendee marked `self` does not help: on someone else's calendar it marks them, not the
owner. A calendar that states no `accessRole` (another provider's) is the owner's unless it says
`isSubscribed`.

An event that names no calendar is drawn. An event that names a calendar the read did not return is not, and a
read of the calendars that fails fails the window read, so meetings are never kept without their calendars.

## People

A person is a `substrate.reamde.dev/core/contact`, read by reference from a task's `people` and a meeting's
attendees. The app reads `name`, `givenName`, `familyName`, `nickname` and the reserved `title`. The name it
shows is `name` when it has an upper-case letter; otherwise `givenName` and `familyName`; otherwise a `title`
with an upper-case letter; otherwise the nickname, or the handle `name` or `title` holds, read as a name. A
handle is read as a name by taking what is before an "@" and giving each of its words (parted by a dot, a dash
or an underscore) a capital: "Christopher" for "christopher", "Dan" for "dan@example.com". A `givenName` or
`familyName` with no upper-case letter is a handle too, since a source can fill it with one. Inside a sentence
("for …", "with …") it is the given name, else the first word of the name.

## Writes the app makes

Log or skip an occurrence (`routines/routinelog`). Answer suggested tasks (`status` to `open`, `done` or
`abandoned`; or the owner's words as a `today/correction`). Add a task. Set a flag (the rule above). Take urgent off. Set or remove a project's
`priority`. Move a task (`notBefore` or `dueDay`, with `deferrals`). Mark a task done. Accept or reject a
patch request. Save the plan and the wrap (`today/plan`, and the task writes its ticks imply). "Got it"
(`today/reviewmark`). Mark the night read and the last look (`today/preference`). Keep a return point.
Every write goes through the outbox, so it works offline and is sent when the app reconnects.

## Suggested tasks: what the app reads to draw them, and what "Other" writes

A suggested task is a `tasks/task` in `proposed`. Beyond what the tasks section lists, the app reads:

- **`originAt`** (datetime, tasks 5): when what the task was made from was said or happened. The line
  under a suggestion says "Heard" with that time. A record without it says "Suggested" with the record's own
  `createdAt`, which is when the suggestion was written and not when anyone said anything. A suggestion's
  age, for the fade after a week and the end after two, is counted from `originAt` where it is set.
- **`description`** as Markdown. A record's path in brackets, such as `(source: <authority>/<package>/<kind>/<id>)`,
  is not shown: where the words came from is the task's `origin`. A bare path in a sentence is drawn as that
  record's pill, as the kit draws one. A writer should put no path in the prose at all. A task that is
  open (not `proposed`) shows all of its description once its row is opened, and on its own page; a closed
  row says its first 80 letters or so where the task has no other line; the current item, which is open
  without being asked, shows the first 160 or so of a description longer than 240, with a button for the
  rest. So what an assistant writes into `description` is read where the task is.
- **`today/advice`** for the same task id: its `answer` starts selected and marks the option, and orders
  the suggestion in its project's group (`task` first, none next, `done` and `dismiss` last).

The fourth answer, **Other**, is offered only where the repository declares
`substrate.reamde.dev/today/correction` (today 4). Saved with words, the app:

1. creates one `today/correction` under an id it mints, with `subject` a reference to the suggested task
   (`{"ref": "substrate.reamde.dev/tasks/task/<id>"}`) and `text` the owner's words, trimmed;
2. sends the same words to the assistant as a chat turn whose context is that task. The task itself is not
   written: the assistant acts on it.

It reads every `today/correction` (the kind is linked and followed). A suggested task whose latest correction
is newer than the task's own `updatedAt` is with the assistant: it is not asked again and not counted. Once
the task changes after the correction (rewritten, say) it is a suggestion again; once it leaves `proposed` it
is not one. The app never changes or deletes a correction.

## The plan: what yesterday left, and what is on today

- **Left from yesterday** is a task that is `open`, has `dueDay` before today, and has no `notBefore` at or
  after the start of today. It is asked after on the plan page, and the plan's Save writes the answer:
  `dueDay` to today (and `notBefore` cleared) for Today; `dueDay` to tomorrow, `notBefore` to the start of
  tomorrow and one more `deferrals` for Tomorrow; `status: abandoned` for Drop it. One chosen for today is in
  the plan's `picked`. A routine with nothing logged yesterday is not planning: it is still asked after in
  the day, and its answer writes its `routines/routinelog` at once.
- **A task is on today** when the saved plan's `picked` holds it, when it is due today, when it is urgent,
  or when its `notBefore` is within today. The last is how anything puts a task on today without a due day:
  the wrap's "Tomorrow", "When", and the assistant adding what the owner asked for (`today-add`). The plan
  page shows such a task ticked; unticked and saved, its `notBefore` is cleared, so it leaves today.
- The plan page lists no suggested task. Ticking no longer makes one a task: they are answered on their own page.

