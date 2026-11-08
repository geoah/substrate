# Building an applet

An applet is a small, phone-first app over the owner's records: a record of
`substrate.reamde.dev/applets/applet` whose screens are A2UI v1.0
components. The substrate renders it and runs its buttons' actions as reads
and writes under the token of whoever opens it. The owner opens it at
`/applets/<id>` in the console, or full screen at `/a/<id>`.

## The loop

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
`{"@now": "datetime"}`; `{"@call": "formatString", "args": {"value": "Added ${/context/title}"}}`.
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

## A good applet

The start screen is a list: a search TextField and status chips (bound
under `/ui`, listed in `refreshOn`), a count, and rows that open a detail
screen (`navigate {screen, params: {"id": {"@path": "/context/id"}}}`). A row:

```json
{"id": "row", "component": "Button", "variant": "borderless", "child": "rowLayout",
 "action": {"event": {"name": "open", "context": {"id": {"@path": "id"}}}}},
{"id": "rowLayout", "component": "Row", "align": "center", "children": ["rowCover", "rowText"]},
{"id": "rowCover", "component": "Image", "variant": "avatar", "fit": "cover",
 "url": {"@path": "properties/coverUrl"}, "description": {"@path": "display/title"}},
{"id": "rowText", "component": "Column", "weight": 1, "children": ["rowTitle", "rowMeta"]},
{"id": "rowTitle", "component": "Text", "text": {"@path": "display/title"}},
{"id": "rowMeta", "component": "Text", "variant": "caption",
 "text": {"@call": "formatString", "args": {"value": "${display/author} · ${display/status}"}}}
```

The detail screen reads one record by `/params/id`, loads its edits in
`init`, shows the current value beside a row of option buttons
("Status: ${/data/book/display/status}"), and puts a delete with `confirm`
last. Adding is its own screen: inputs under `/ui/form`, a primary Button
with required checks, and an action that creates, resets the form with
`set`, notifies and navigates back. Use the owner's words for labels, and
only properties the kind declares.
