# Tasks over any task-like kind

Status: proposal, 2026-11-09. Nothing here is merged. Decision record
[0247](../decisions/0247-a-status-names-the-open-states-a-record-waits-in-until-its-owner-accepts-it.md)
is drafted on this branch beside the loader change it describes.

## The problem

Substrate Tasks reads exactly two kinds, `substrate.reamde.dev/tasks/task` and
`substrate.reamde.dev/tasks/list`, named once in `TasksKinds.swift` and
`model.ts`. An owner whose tasks already live somewhere else cannot use the
app without copying them. Their tasks might be the tasks sample imported under
their own authority, a kind they declared, a provider's issues, or a system
of agents that writes its own task kind. Agents also suggest tasks, and the
app has no place for a suggestion that the owner has not taken on yet.

The pieces the vocabulary needs are mostly in place:

- [0225](../decisions/0225-an-apps-scope-may-name-a-trait-and-covers-what-star-covers-among-the-kinds-that-bind-it.md):
  an app may ask for `write:implements(<trait>)`.
- [0226](../decisions/0226-a-kinds-status-is-the-core-lifecycle-trait-and-names-its-done-and-dropped-states.md):
  core's `lifecycle` names a kind's status and the states that close a
  record.
- [0247](../decisions/0247-a-status-names-the-open-states-a-record-waits-in-until-its-owner-accepts-it.md),
  new here: `proposed:` names the open states a suggestion waits in.

## The model

Nothing below names any one owner's kinds. The app learns everything from
declarations.

**A source** is a kind that binds `substrate.reamde.dev/core/lifecycle`, which
the owner chose to see. From the kind's declaration the app reads:

| What           | Where it comes from                                                                                     |
| -------------- | ------------------------------------------------------------------------------------------------------- |
| status         | the property `lifecycle` binds (renamed or not), its values, and a machine's transitions                 |
| open, closed   | `done:` and `dropped:` (0226); every other value is open                                                 |
| suggestion     | `proposed:` (0247): open, but waiting for the owner                                                      |
| title          | the property the `displayTemplate` names                                                                |
| due            | the point `temporal` binds (`temporal(point: dueAt)`); a `date` property named `dueDay`, where declared  |
| priority       | an enum named `priority`, drawn in its declared order; absent, rows show none                           |
| grouping       | one single, non-repeated reference the owner picks (below)                                              |
| everything else| shown read-only under Details, with the property's declared label                                       |

**Writes follow the kind's rules.** A machine's status moves only along a
declared transition, by a patch, and the app offers only the moves the
current state has. An enum takes any of its values. A property the kind does
not declare is never written: no `dueDay` on a kind without one, no
`position` where there is none, so manual order exists only where the kind
declares `position`. A status a provider's sync owns is shown, not edited.
The source says "View only".

**Grouping is per source**, never one choice for the whole app. Each source
groups by one of its own single references: Tasks by `list`, a kind with a
`project` reference by that. The app's guess is a reference named `list`,
`project` or `area` whose target the token can read. The owner may pick
another or none. Records with nothing in the reference show under "No list"
or "No project", named after the reference's label. Nothing falls into an
inbox only because of a setting.

A group kind the token cannot read can't be named, so the app offers only
readable targets. Today a token reads its own package and every kind that
binds `lifecycle`. A container kind with no status of its own is out of reach
until a later approval adds its exact kind.

**A suggestion** is a record of a source whose status is in `proposed:`.

- Accept is the transition to an open value in none of the lists. Where there
  are several, the machine's first.
- Dismiss is the transition to a dropped value. Where there is none, the app
  offers no Dismiss.
- Swiping either way does the same, with the undo window completion already
  has.

**Settings are records** of a new kind in the app's package,
`substrate.reamde.dev/tasks/source`, so the phone, the iPad and the web agree.
It has one record per source the owner configured:

| Property   | Type             | Holds                                                    |
| ---------- | ---------------- | -------------------------------------------------------- |
| `kind`     | string, required | the source kind's full reference                         |
| `enabled`  | bool             | shown or not                                             |
| `groupBy`  | string           | the reference property it groups by; absent for none     |
| `pins`     | string, repeated | record paths of pinned groups, in order                  |
| `default`  | bool             | new tasks are written here (one source at most)          |
| `position` | decimal          | the source's place in Sources                            |

A kind with no record shows with the guessed grouping, and `Tasks` is the
default for new tasks.

**Discovery needs one server change.** An app token cannot list a trait's
implementors today: the implementors route is owner-only. The change opens it
on the scoped surface, narrowed to the kinds the grant reaches, returning
their declarations. The app then learns its sources without a
`read:substrate.reamde.dev/core/kind` scope over every declaration.

**Scopes:** `write:substrate.reamde.dev/tasks/*` and
`write:implements(substrate.reamde.dev/core/lifecycle)`. The approval page
already lists today's implementors and says more may come (0225).

**Suggested changes come second.** A change an agent suggests to an existing
task is a `core/recordpatchrequest`. It is shown as a one-line note on the
row, with Apply and Dismiss in the detail and in the Suggestions queue's
Changes tab. An app's `decide:` scope reaches the requests of the agents it
names, but a list must name their threads, so the app cannot ask for "every
pending request on my tasks" yet. Phase 2 adds that filter on the scoped
surface, and shows who suggested a record from its history.

## The UI

The same screens, words and order on iPhone, iPad, Android and the web.
TasksKit's presenter writes them, and `present.ts` mirrors it (PARITY.md).
One accent; a suggestion is marked by a small sparkle and its trailing
controls, never by a second color. One row shape everywhere: a title, one
meta line (due · group · who suggested it), and one trailing control.

- **Needs you:** pinned-group chips; one Suggestions row with its count;
  Late or urgent with Move and Won't Do; Coming up; Later (dated); one
  counted No date row.
- **Suggestions:** New tasks and Changes; rows with Dismiss and Add; Select
  for many; each opens a detail.
- **A suggestion:** who suggested it and when; the title; the editable fields
  (group, due, priority) set before adding; Why (the description); Details
  (every other property, the collection by its full reference); Dismiss and
  Add to tasks docked.
- **Lists:** search; Pinned; one section per grouping kind, headed by its
  label and full reference, listing groups with open tasks, "No project",
  and "N more with nothing open". A group kind that binds `lifecycle` shows
  its status and hides closed groups.
- **A group:** its kind and status, a Suggestions row, To Do, Done.
- **Settings → Sources:** each candidate kind with its label, full reference,
  counts and a switch ("View only" where its status is not writable); "Save
  new tasks to". A source's page shows Group by, how its states read, and the
  fields the app shows.
- **iPad and the web from 1000 px:** the sidebar holds Needs you,
  Suggestions, No date, Pinned, each grouping kind's busiest groups with "All
  N…", and Settings; the list and the detail beside it.

The mockups behind this are drawn as HTML in the app's current visual language.

## Steps

1. **Vocabulary.** `proposed:` (0247) with its loader rules and tests; the
   tasks sample lists `proposed: [proposed]` (sample 14).
2. **Server.** The implementors route on the scoped surface, narrowed to the
   grant, with its test against the hand-written fake and a real token.
3. **The Tasks package, version 2.** `task` binds `lifecycle` (`done: [done]`,
   `dropped: [abandoned]`), gains `proposed` with `proposed: [proposed]` and
   its two transitions; `source` joins. All additive.
4. **The kits.**
   - Swift and web: read declarations and `done:`, `dropped:` and `proposed:`.
   - Open the link over the kinds the sources name, rebuilt when the owner
     turns one on or off.
   - Fix the web kit's sentence for an `implements(…)` scope.
5. **TasksKit.** A source model and one codec driven by it. The presenter
   gains Suggestions, No date, the grouping sections, Pinned and Sources.
   Then the SwiftUI views, the Compose screens through the bridge, and the
   fixtures.
6. **The web app.** The same model in `model.ts`, `state.ts` and `present.ts`;
   the same screens; PARITY.md rows for each.
7. **Phase 2.** Suggested changes, and who suggested a record.

A system with its own task kinds takes part by binding `lifecycle` on its
task and container kinds and listing `proposed:`. The app needs nothing else
from it.

## Open questions

- Should Accept also offer a choice of open state where a machine has several?
- Is the reference-name guess (`list`, `project`, `area`) worth having, or
  should a new source start ungrouped until the owner picks?
- Pins as record paths in a string list, or a reference that can name any
  kind?
