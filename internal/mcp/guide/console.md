# The console's views

Shape collection tabs, record pages and display preferences with records, while declarations supply names, labels and collection purpose.

## Start with the existing records

The console presents the owner's collections and record pages, provider connections, agents and applets. An assistant can shape these views through records; it does not need to change the console's code. Use `describe_kind` to inspect the current declarations, then `query` to find views already written for the collection. Preserve existing choices and use `ifVersion` when updating a record.

The main records are:

- `substrate.reamde.dev/console/recordview`: a record page's layout.
- `substrate.reamde.dev/console/collectionview`: a saved collection view displayed as a tab.
- `substrate.reamde.dev/core/consolepreference`: repository-owned navigation and display preferences.
- `substrate.reamde.dev/applets/applet`: a dedicated applet, opened at `/applets/<id>`.

These are data records. The console package's declaration protection does not prohibit writing a view record. Use `write` with `properties`, not a vocabulary declaration, to create or edit one.

## Collection tabs

A `substrate.reamde.dev/console/collectionview` record names its collection through `appliesTo`, a reference to the collection's **kind declaration**. For example, the reference for memory is `substrate.reamde.dev/core/kind/substrate.reamde.dev/llm/memory`; it is not a reference to an individual memory.

Its properties are:

- `appliesTo` (required): the kind declaration reference.
- `name` (required): the tab label.
- `where`: an object of filter conditions by property, such as `{"active":{"eq":true}}`. Absent means all rows.
- `sort`: `property:asc` or `property:desc`; absent keeps the collection's order.
- `columns`: property names in display order; every other column is hidden. Absent leaves the columns as they are.
- `group`: the property to group rows by; absent means no grouping.
- `nest`: false flattens a collection that otherwise nests records under a parent; absent keeps its tree.
- `default`: whether to open this tab when the browser remembers no choice. If several request it, the first by id wins.

Use property names, not console column ids or filter-token strings. The console checks whether it can render a view; an unusable one is shown with an explanation. A view does not change the underlying records or their declarations.

For example, this `write` adds a tab for active memories, using the same filter shape as the shipped Active view:

```json
{
  "op":"put",
  "kind":"substrate.reamde.dev/console/collectionview",
  "id":"my-active-memories",
  "properties":{
    "appliesTo":"substrate.reamde.dev/core/kind/substrate.reamde.dev/llm/memory",
    "name":"Active memories",
    "where":{"active":{"eq":true}}
  }
}
```

Check existing views before adding this: the seeded console already ships memory tabs. Collection-view records appear after All and before the owner's preference-stored saved views. They can be edited or deleted like other records.

## Record page layouts

A `substrate.reamde.dev/console/recordview` record has `appliesTo`, also a reference to the kind declaration; optional `name` and markdown `intro`; and ordered `sections`. The console renders these above the generic property sheet. With no record view, the generic page still works.

Each section holds exactly one block. Small blocks are:

- `heading: {text, note}`: required heading text and optional note.
- `text: {text}`: required markdown text.
- `properties: {label, list: [{property, label}]}`: chosen properties; each list entry requires `property`, while labels are optional.
- `related`: either an outgoing `property`, or `kind` (a kind declaration reference) plus `via` (the reference property on that kind). Do not set both forms. Optional `show` is `list` or `count`, and `limit` bounds a list.
- `request: {label, property, confirm}`: a button that stamps one of the record's own datetime properties the owner writes with now, after a confirmation saying `confirm`.

For measurements and larger layouts, inspect `describe_kind` for the exact fields of `chart`, `spans`, `stats`, `trend`, `table`, `breakdown`, `conversation` and `lanes`. A `conversation` reads the records of a kind that point here as a chat, oldest first, and with `compose` writes a record of a request kind for each send, reaction and read mark; it names every kind and property it uses, so nothing about a chat is assumed. Declared blocks are not a guarantee that every console version draws them; unsupported or invalid blocks show a notice. Do not invent block properties.

`window` binds blocks to the current record through property names, not expressions:

- `start` and `end` name its time properties, often `at` and `endsAt`.
- `open: true` reads an unset end up to now.
- `last` supplies a duration ending now for a record without a start.
- `about` names the reference property for the thing observed; `aboutSelf: true` makes this record that thing.
- `source` names its source reference property; `sourceSelf: true` makes this record the source.

Do not combine `start` with `last`, `about` with `aboutSelf`, or `source` with `sourceSelf`. Measurement blocks name metric records by full references. Read `guide` topic `observations` for the forms, reducers and units behind those reads.

`fold` lists properties the generic sheet folds away because sections show them. `collapsed` lists properties initially kept closed under one line. Both contain property names; neither deletes data.

## Preferences are a different shape

`substrate.reamde.dev/core/consolepreference` holds optional preferences, shared across sessions. Query the existing record before writing rather than creating competing preferences.

- `collapsed`: authority and package references whose navigation children are collapsed.
- `favorites`: full kind references in display order.
- `recordWidth`: `narrow`, `wide`, `full`; `tableWidth`: `wide`, `full`.
- `density`: `comfortable`, `compact`; `theme`: `system`, `light`, `dark`.
- `technicalDetails`: shows references, ids, actors, tiers and changelog numbers.
- `connectGaps`: draws dashed connections across missing chart buckets.
- `welcomeDismissed`: records dismissal of the Providers welcome.

Its `views` list stores the owner's own saved views. Each entry requires `id`, `collection` (the kind reference) and `name`. Optional keys are `filter` (console filter-token strings), `sort`, `columns` (console column ids), `hidden`, `nest` and `group`. This is deliberately a different shape from collection-view records: do not copy `where` into a preference view, or preference filter tokens into `where`. Prefer a collection-view record when authoring a portable tab.

`sidebarOpen` is deprecated; the console keeps that choice per browser. Omitted preferences use console defaults. Do not replace a preferences list without preserving entries unrelated to the requested change.

## Labels and discoverability

A kind's `purpose` is `primary`, `supporting` or `internal`; absent means primary. It helps clients decide which collections to surface. Quick search uses primary kinds; everyday search includes primary and supporting kinds, with technical browsing widening to internal machinery. Purpose does not grant or revoke access.

A kind's `displayTemplate` derives a record's title from declared properties. Write those properties, such as `name`, rather than trying to override the derived title. Enum entries may carry a `label`; the console uses that human-readable label while the stored value remains the enum's declared value. Filters and writes must use the value, not its display label.

Change these declaration details only in an owner-writable package; use `guide` topic `vocabulary` for admission rules. A view record cannot change a published kind's purpose or rename its properties.

For a dedicated interactive surface, use an applet record instead of forcing a collection tab to serve as an application. Applets open within the console at `/applets/<id>`. Read `guide` topic `applets` and call `applets` with `op: "guide"` before building its screens.
