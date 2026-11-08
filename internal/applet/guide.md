# Building an applet

An applet is a small, phone-first app over the owner's records: a record of
`substrate.reamde.dev/applets/applet` whose screens are A2UI v1.0
components. The substrate renders it and runs its buttons' actions as reads
and writes under the token of whoever opens it. The owner opens it at
`/applets/<id>` in the console, or full screen at `/a/<id>`.

## The loop

0. If the owner's request leaves open what they will mostly do with the
   applet, or what they open it to see, and their records do not answer
   it, ask them first: two or three questions with short options. Decide
   layout, wording and icons yourself.
1. Read the declaration of every kind the applet shows (`describe_kind`), a
   few of its records (`query`, first 5), and, when changing an applet, the
   applet itself (`query`, kind `substrate.reamde.dev/applets/applet`, id
   its id). For a function you will call, read its declaration (kind
   `substrate.reamde.dev/core/function`, id its reference).
2. Preview the draft before writing it: `applets` op `preview` with the
   draft's properties as `applet` and the id it will have. Fix every
   problem; each names its path.
3. Write the whole applet: `write` op `put`, kind
   `substrate.reamde.dev/applets/applet`, id a short lowercase word
   (`books`), the properties below. A refusal names each problem by path:
   fix exactly those and write again.
4. Preview every screen (`applets` op `preview`, id, screen; a detail
   screen with `params: {"id": "<a real record id>"}`). Fix every problem
   and every "resolved to nothing" that is a wrong path (the line says what
   the nearest object holds instead). A value a record lacks is fine.
5. Tell the owner the link: `/applets/<id>`.

## The record

- `name` (required), `description` (one sentence), `icon` (a lucide name:
  `book-open`, `sprout`, `dumbbell`, `list-checks`, `wallet`).
- `runtime` (required): `"a2ui"`, the one runtime there is: screens of A2UI
  components the server renders. It has no default, so a runtime added
  later never changes what an applet written now means.
- `start`: the screen it opens on. It must read no `/params`.
- `state`: the initial UI state, the data model's `/ui`. Every input binds a
  path under a key declared here: `{"q": "", "status": [], "form": {"title": ""}, "edit": {}}`.
- `sources`: named reads, run when a screen that lists them renders.
  `{"kind": <full kind>, "where": {<prop>: <condition>}, "search": <text>, "orderBy": "title" | "dueAt:desc", "first": 100, "expand": [<reference props>]}`,
  or one record: `{"kind": ..., "id": {"@path": "/params/id"}, "expand": [...]}`.
  Conditions: `eq`, `in`, `prefix`, `match`, `gt`, `gte`, `lt`, `lte`,
  `contains`, `exists`, or a bare value for `eq`. A condition whose binding
  resolves to nothing (`""`, `[]`, null) is dropped, so an empty filter
  shows everything.
  A kind on the timeline (it binds `substrate.reamde.dev/core/temporal`)
  holds `at`, or `dueAt`, without declaring it, to filter and order by.
  What is due soon, overdue included, is a source over `dueAt` with an
  upper bound alone:
  `{"dueAt": {"lt": {"@call": "addDays", "args": {"value": {"@now": "date"}, "days": 7}}}}`,
  `orderBy: "dueAt"`. A source whose `where` bounds `at` on both ends is a
  **window read**, ordered by `"at"`: for a kind that repeats on a calendar
  (it binds `substrate.reamde.dev/core/recurring`) it answers each
  occurrence in the window, computed.
- `actions`: named lists of steps a button runs in order, stopping at the
  first refusal. An action is not retried for you: a step that wrote stays
  written if a later one fails.
  - `create {kind, properties}`
  - `patch {kind, id, properties}`: a state property moves along a declared
    transition; a property whose binding reads a `/ui` path the state does
    not hold is left out
  - `delete {kind, id}`
  - `ensure {kind, where, properties}`: the first match, or a new record
  - `read {kind, id, pick: "/properties", into: "/ui/edit"}`
  - `call {function, input, pick: "/results", into: "/ui/results"}`
  - `navigate {screen, params}`: pass every param the screen reads
  - `set {values: {"/ui/form": {"title": ""}}}`
  - `notify {text}`

  Any step may carry `as: <name>`, its result then at `/results/<name>`.
  An action may ask one question, `confirm: <text>` on its first step that
  needs it, asked before any step runs (so it reads `/ui`, `/params`,
  `/context`, never `/results`). Give every delete one; it is answered
  "Delete" and drawn as destructive. Name the record in it: the delete
  button carries the title in its context (`{"title": {"@path": "/data/book/display/title"}}`)
  and the question reads it
  (`{"@call": "formatString", "args": {"value": "Delete “${/context/title}”?"}}`).
- `screens`: named `{title, sources, refreshOn, init, components}`.
  `refreshOn` lists the `/ui` paths whose edits re-render the screen: every
  filter and search input. `init` holds `read` and `set` steps run each time
  the screen is entered (by a tap, a link or a step back), so a detail
  screen loads its edits there:
  `[{"op": "read", "kind": ..., "id": {"@path": "/params/id"}, "pick": "/properties", "into": "/ui/edit"}]`.
  The host draws the title and a back button; a screen draws neither.

## Bindings

In sources and actions: `{"@path": "/ui/..."}`, `{"@path": "/params/..."}`;
in actions also `{"@path": "/context/<key>"}` (the tapped button's context)
and `{"@path": "/results/<as>/..."}`; `{"@now": "date"}` or
`{"@now": "datetime"}`; `{"@call": "formatString", "args": {"value": "Added ${/context/title}"}}`;
`{"@call": "addDays", "args": {"value": <a date or an instant>, "days": <a whole number, negative to go back>}}`.
A reference property is written as a record path: `{"@path": "/results/author/ref"}`.

## What a screen binds to

- `/applet` `{id, name, screen, title}`; `/ui`; `/params`;
  `/data/<source>` (an array of records, or one record or null);
  `/counts/<source>`.
- A record: `{id, kind, ref, title, version, properties, display}`.
  `display/<prop>` is the value as a person reads it: an enum's label, a
  list joined with ", ", an expanded reference's title, "Yes" or "No". Show
  `display/...` and keep `properties/...` for what you send back
  (`{"@path": "id"}`, a value to filter by).
- An expanded reference holds its record in the same shape
  (`properties/author/display/name`); one not expanded holds `{ref}`.
- An occurrence a window read computed also carries `computed: true` and
  `series {id, ref}`, the repeating record it comes from: a step reads or
  opens the series, never writes the occurrence. Marking something done
  that recurs some days after the last time patches its `dueAt` to
  `{"@call": "addDays", "args": {"value": {"@now": "datetime"}, "days": {"@path": "/context/days"}}}`
  (the button's context carrying the record's interval) and stamps when it
  was done.
- Inside a list template, paths are relative to the item: `id`,
  `display/title`. Outside one, a path starts at a root.

## Components (A2UI v1.0 basic catalog)

A flat list with exactly one id `root`. Ids are letters, digits and `_`
(no `-`). A value is a literal, `{"@path": ...}` or `{"@call": <fn>, "args": {...}}`.
`${...}` is filled inside `formatString` alone.

- `Text {text, variant: body|caption}`: simple Markdown (`## heading`,
  `**bold**`). Functions: `formatString`, `formatDate {value, format: "MMM d, yyyy"}`,
  `formatNumber`, `formatCurrency {value, currency}`, `pluralize {value, one, other}`.
- `Image {url, description, fit, variant: icon|avatar|smallFeature|mediumFeature|largeFeature|header}`:
  `description` is what the image shows (the book's title).
- `Icon {name}`, `Row {children, justify, align}`, `Column`,
  `List {children, direction}`, `Card {child}`, `Tabs {tabs: [{title, child}]}`,
  `Modal {trigger, content}`, `Divider`. `children` is a list of ids or a
  template `{"path": "/data/books", "componentId": "row"}`. `weight: 1`
  takes a Row's or a Column's free space.
- `Button {child, variant: default|primary|borderless, action: {"event": {"name": <action>, "context": {"id": {"@path": "id"}}}}, checks}`.
  A list row is a borderless Button whose child is a Row.
- Inputs, each `value` bound under `/ui`: `TextField {label, value, placeholder, variant: shortText|longText|number|obscured}`,
  `CheckBox`, `ChoicePicker {label, options: [{label, value}], value (a list), variant: mutuallyExclusive|multipleSelection, displayStyle: chips|checkbox}`,
  `Slider`, `DateTimeInput`. `checks: [{"condition": {"@call": "required", "args": {"value": {"@path": "/ui/form/title"}}}, "message": "..."}]`
  disable a Button until they pass.

There is no conditional visibility: design screens that read well with an
empty value, and prefer `display/...`, which is empty when there is nothing
to show. A label beside a value a record may lack draws bare ("Rating:"
and nothing): label only what every record has, and give an optional value
a Text of its own with no label, or an input whose label reads well empty.

## Designing

Start from the owner, not from a layout: the two or three things they will
do most, and the one question they open the applet to answer. The start
screen answers that question and puts the most frequent action one tap
away; everything else is a screen further in. Shapes that recur, to combine
as the tasks need, never to copy whole:

- **Things that come due** (plants, chores, medication, bills): the start
  screen is what is due now and next, from a window read, each with a
  one-tap "done"; a second screen lists everything, to add and edit.
- **A log** (workouts, meals, moods): the start screen adds today's entry
  in a tap or two and shows the latest few; a summary counts the week.
- **A collection to browse** (books, recipes, wine): search and filter
  chips over a list, rows opening a detail screen to edit.
- **A summary** (spending, habits): counts and totals first, each opening
  the list behind it.

The parts, in syntax. A list row is a borderless Button whose child is a
Row, its context carrying the row's id:

```json
{"id": "row", "component": "Button", "variant": "borderless", "child": "rowLayout",
 "action": {"event": {"name": "open", "context": {"id": {"@path": "id"}}}}},
{"id": "rowLayout", "component": "Row", "align": "center", "children": ["rowIcon", "rowText"]},
{"id": "rowIcon", "component": "Icon", "name": "event"},
{"id": "rowText", "component": "Column", "weight": 1, "children": ["rowTitle", "rowMeta"]},
{"id": "rowTitle", "component": "Text", "text": {"@path": "display/name"}},
{"id": "rowMeta", "component": "Text", "variant": "caption", "text": {"@path": "display/location"}}
```

A detail screen reads one record by `/params/id`, loads its edits in
`init`, and puts a delete with `confirm` last. Adding is inputs under
`/ui/form`, a primary Button with required checks, and an action that
creates, resets the form with `set`, notifies and navigates back. Use the
owner's words for labels, and only properties the kind declares.
