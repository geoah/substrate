# Console elements

The console's shared components, one per idea. A page shows a kind, a
record, an actor, a state, an enum value, a status, an origin or an id only
through the component named here, and never draws one by hand: a hand-drawn
kind is a second rendering that drifts from the first, and the first review
counted thirteen of them ([2026-09-24](reviews/2026-09-24-first-review.md)).
[The design guide](design-guide.md) gives the tokens and rules these
components apply.

Paths are under `web/console/src/`. Props are listed one per line; the
component's own doc comment is the full contract.

## Identity marks

In `components/identity/`. Every mark reads Technical details for itself, so
a page that uses it is right in both modes, and every mark that names
something opens the one hover card.

### KindGlyph

A kind's tile: its stable icon on its stable hue
([how both are chosen](design-guide.md#kind-hues)).

- `kind`: a registry entry or a full kind reference
- `size`: `xs` 16px, `sm` 20px (default), `md` 28px, `lg` 40px

Used by the sidebar, ⌘K, the breadcrumb, Home's collection cards, the record
head, pickers, and inside `KindRef` and `RecordRef`. **Rule:** a kind is
never marked by an icon of a page's own choosing.

### KindRef and KindPath

A kind, named: its glyph and its display plural ("People") in `label` mode,
or its full reference in `reference` mode. With Technical details on, label
mode shows the reference beside the label. Links to the collection; its hover
card gives the display name, where the collection comes from (an
`OriginMark`: "Made by _agent_" where an agent declared its package), the
everyday description, how many records it holds, how it is listed (its
purpose) and the full reference.

- `kind`: a registry entry or a full kind reference
- `mode`: `label` (default) or `reference`
- `link`: whether it links to the collection (default true)
- `count`: the record count for the hover card, when known

`KindPath` is the reference alone, `{authority}/{package}/{name}` with the
authority and package toned down and the name emphasised; it wraps and never
truncates (`reference`). `KindCard` is the hover card's body for a trigger
that is not a `KindRef`, such as a collection's own heading (`kind`,
`count`); the sidebar's rows open it too.

Used by the record head, **Connected to**, **Where it comes from**, History,
Search, a provider's contents and the record editor. **Rule:** never a short
kind name as an identifier; the display name is a label, the reference is the
identity.

### RecordRef

A record, named: its kind's glyph and its title, in the mention style
(underlined on hover) or as a soft chip. A record with no title reads
"Untitled _kind_", never its id; where only the reference is known the title
is read, batched with every other mark on the page. Its hover card
(`RecordCard`) gives the title, the collection (and "from Google" for a
provider's copy), up to three property values under their declared labels (a
reference as its referent's title, through the same batched read; the key
under the label only in technical mode) and the full reference.

- `kind`: the record's kind reference
- `id`: the record id
- `title`: the title, when the caller has it
- `variant`: `mention` (default) or `chip`
- `link`: whether it links to the record (default true)

Used by the grid's reference cells, the property sheet, **Connected to**,
History, ⌘K, the agent chat (tool calls, suggestion cards, messages) and the
ownership detail. **Rule:** never a bare id.

### ReferenceValue

One stored reference value (`{ref: <kind>/<id>, …}`), drawn one way: the
referent as a `RecordRef` and any link data the reference declares beside it.
A value that is not a reference reads as it is.

- `value`: the stored value
- `title`: the referent's title, when already read

Used by the property sheet and the merge page. **Rule:** a reference never
prints as its `{ref}` shape.

### ActorRef and ActorMark

Who did something: **You** (a person on a neutral disc, never a letter), an
agent (a bot on purple), a provider's function or bundle (the provider's
badge), a tool of the repository's own (a bolt), the substrate itself (a
shield). Plain names on the page; the raw actor string is in the hover card
and, with Technical details on, inline.

- `actor`: the actor string as stored (`console`, `agent:<authority>:<package>:<name>`, `substrate`, …)
- `link`: `actor` (the actor's History, default), `record` (its declaration), or `false`
- `inlineId`: show the raw actor inline in technical mode (default true); off where a copyable id sits elsewhere

`ActorMark` is the mark alone (`identity`, `size` `xs` to `lg`), for a place
that names the actor another way. Used by History, the record's history and
details, the ownership detail, the grid's row detail, the tool page, merge
and change requests, Home's recent chats. **Rule:** an actor is never a raw
id in everyday mode, and every actor mark opens the same card. Where a
sentence names the actor ("You changed Home"), `inlineId` is off and the raw
id sits after the sentence, on its faint technical line with a copy button,
so the sentence still reads as one.

### AgentRef and AgentMark

An agent named by its record, in `components/agent/agent-ref.tsx` and
`agent-mark.tsx`: its mark and name, and on hover the card the agents panel
heads with (what it runs on, what it is for, what it may see and change).

- `id`: the agent record's id
- `agent`: the record, when the caller has it
- `link`: open a new chat with it (default false)
- `size`: `xs` (default) or `sm`

Used by the chats column, a tool's **Used by** and the change request page.
**Rule:** an agent reads one way everywhere.

### ProviderBadge

A provider's mark: its letter on a small bordered square in its brand colour.

- `provider`: the provider's package word (`google`) or its resolved info
- `size`: `xs`, `sm` (default), `md`

Used by the sidebar's provider groups, ⌘K, All data, Home, the record head
and **Where it comes from**, and inside `ActorMark` and `OriginMark`.
**Rule:** the only way a provider is marked.

### OriginMark

Where something comes from, in one set of words (`lib/origin.ts`): the
origin's actor mark and "Yours", "From Google", "Made by _agent_" or "Built
into substrate".

- `origin`: an `Origin`, from `originOfActor` or `originOfKind`
- `short`: the name alone ("You", "Google"), for a chip

`useKindOrigin(kind)` (`hooks/use-kind-origin.ts`) is a collection's origin,
reading the package rows so an agent's app reads "Made by _agent_"
(`lib/packages.ts`, decision 0111).

Used by tool cards and the tool page, Home's collection cards, a kind's hover
card, All data's **Made by** column, the package page and the ownership chip.
**Rule:** one rendering of "yours / from _Provider_ / made by _agent_".

### StateBadge

A record's state: a dot coloured by what the state means and the state in
plain words ("Suggested", "Dropped"); technical mode appends the stored
value.

- `value`: the stored state
- `label`: a surface's own word for the state, where it names it its own way
- `initial`: the machine's initial state, which colours a state the words do not know
- `variant`: `dot` (default) or `tag`, the word on a soft fill

Used by the grid, the property sheet and its state mover, filters, the
history value moves, suggestion cards and the request pages. **Rule:** a
state is never a bare stored value and never a `Pill`.

### EnumTag

An enum value: its label on its hue, one colour map everywhere
([enum hues](design-guide.md#kind-hues)), so "High" is the same red in the
grid, on the sheet and in a filter. A quiet value (none, unknown, other)
reads plain.

- `prop`: the property (its name picks the ladder, its values order it)
- `value`: the stored value

Used by the grid, the property sheet, its editor, filters and the create
sheet. **Rule:** an enum is never its stored value in everyday mode.

### EmptyValue

The one mark for "nothing here": a faint "—", read aloud as "Empty". Takes
`children` for words in its place where the absence says more ("Cleared").
Used by the grid and the property sheet. **Rule:** one empty token.

### Pill

A status on a rounded soft fill of the colour of what it means: "On", "Ran 2
min ago", "Having trouble", "Update available".

- `tone`: `ok`, `warn`, `bad`, `neutral` or `accent`
- `dot`: the small dot before the words (default true); off for a fact that is not a status
- `live`: the dot pulses, where motion is allowed
- `title`: a tooltip

Used by Providers, a provider's page, Tools, a tool, the sync panel, tokens,
merge requests and the definition view. **Rule:** a status is a `Pill`; a
record's state is a `StateBadge`.

### PurposeTag

A kind's purpose where it is not primary (`supporting`, `internal`), as a
small outlined tag beside its name. Technical mode only (`purpose`). Used by
the sidebar, All data and the authority and package pages.

### IdentityHoverCard and IdentityCard

The one hover card every mark opens: a top block (mark, title, a line under
it), an optional grid of facts, and a mono footer with the full reference.
Opens after 500ms, closes on any scroll, and is never put on a whole table
row.

`IdentityHoverCard`:

- `trigger`: the element the mark renders as (a link, a button, a span)
- `children`: the mark itself
- `card`: the card's body, or a function that mounts it only while open
- `delay`: the open delay (default 500ms)
- `label`: the trigger's accessible name, where its text does not say it
- `side`: `bottom` (default) or `right`, beside the mark where a card below
  would cover the next row (the sidebar)

`IdentityCard`: `mark`, `title`, `sub`, `description`, `facts` (each a
`label`, a `value` and an optional `detail`, the quiet line under the label
that technical mode fills with a property key), `reference` (the footer, with
a copy button), `loading`. Used by every mark above and by the property sheet's labels and
the ownership chip. **Rule:** one hover card design; a new mark uses it.

### CopyButton and IdText

`CopyButton` copies one value and says "Copied"; it sits beside an
identifier, never inside a link, and a click on it never reaches the row or
link around it. Its hit area is 24px.

- `value`: what is copied
- `label`: the accessible name ("Copy record id")

`IdText` is an identifier someone might copy (a record id, a reference, an
actor) in the mono voice, wrapping rather than truncating.

- `value`: the identifier
- `children`: what shows, when not the value itself
- `copy`: add a `CopyButton`

Used wherever technical mode shows an id: the record head and details, the
source view, History's technical line, providers' connection details, the
tool page, the request pages. **Rule:** an id shows only in technical mode or
where the reader must copy it, and always in this voice.

## Page structure

### PageHeader

Every page's head, in one of two layouts. `page`: the glyph beside a 26px
title, the actions on the right. `record`: the glyph above, on one row with
the actions, and a 32px title under it. Under 560px the actions drop below
the title. The breadcrumb is the shell's, never the page's.

- `title`: the words of the `h1`
- `heading`: a heading the page draws itself instead (a title edited in place), styled with `pageTitleClass(size)`
- `meta`: one quiet line under the title
- `description`: a sentence or two about the page
- `actions`: the page's buttons
- `glyph`: a `KindGlyph` (size `lg`) or another mark
- `size`: `page` (default) or `record`
- `children`: what else belongs to the head, such as a callout

Used by every page. **Rule:** no page hand-rolls its head.

### SectionHead

A section's heading row: a 15px `h2`, a quiet hint beside it, the section's
actions on the right.

- `title`: the heading
- `hint`: a few quiet words (a count, what the section holds)
- `actions`: right-aligned buttons or links
- `id`: the heading's id, for `aria-labelledby` and for scrolling to it

Used by the record page, Home, All data, the tool and Tools pages, a
provider's page, Settings, the actor page, the authority page and the request
pages. **Rule:** one section recipe.

### DocPage and TablePage

The two page shapes ([layout](design-guide.md#layout)): `DocPage` is a
left-aligned document at the reader's record width, `TablePage` a table page
at the reader's table width. Both take `children` and `className`.
**Rule:** a page is never centred.

## Controls

In `components/ui/`, the three the console wrote for itself.

### ChoiceList

One keyboard-driven list for every place a reader picks from a declared set:
a filter's states and enum values, a property's value on the sheet and in the
create sheet, a state's moves. Rows read in display words, with the stored
value beside them in technical mode; arrows move, Enter picks, and a list of
eight or more offers a filter box.

- `options`: `{value, label, display?, hint?, disabled?}` each; the value is what a write sends
- `selected`: the stored values chosen now
- `onChange`: the next set (multiple) or the one value picked (single)
- `multiple`: toggle membership and stay open, a box at each row's end
- `label`: the list's accessible name ("Status")
- `showValues`: show stored values (follows Technical details by default)
- `filter`: offer the filter box (by default from eight choices)
- `heading`, `footer`: a quiet line above or under the choices
- `clearLabel`: a last row that clears the choice
- `ref`, `className`

Used by the collection filters, the property sheet's enum and state editors
and the create sheet. **Rule:** a declared set is picked from this, never
from a native `<select>`.

### ConfirmDialog, PauseDialog and SignOutDialog

The one confirmation: a question for a title, the consequence in a sentence,
and a button that says the verb. While the action runs it cannot be
dismissed, so a result never lands on a closed dialog.

- `title`: a question ("Delete “Groceries”?")
- `consequence`: what happens, and what does not
- `confirm`: the verb ("Delete", "Sign out")
- `destructive`: a red confirm, for what cannot be undone
- `pending`: the action is running
- `disabled`: hold the confirm back
- `error`: what went wrong, under the consequence
- `children`: anything to see or fill in before confirming
- `onConfirm`, `onClose`, `open`, `className`

`PauseDialog` (`name`, `pending`, `onConfirm`, `onClose`) is the one Pause,
for a tool, a provider and an account's sync. `SignOutDialog` is the one Sign
out, from the account menu and from Settings. Used by record delete, provider
and account actions, the ownership chip's writes, tokens, suggested changes
and merge requests. **Rule:** every destructive action confirms, here, and
names its consequence.

### Segmented

One choice among a few, all shown at once, as a radio group: only the chosen
option is in the tab order, and arrows, Home and End move the choice.

- `value`, `options` (`{value, label}` each), `onChange`
- `label`: the group's accessible name ("Rank by")
- `disabled`
- `look`: `boxed` (default, the choice filled in) or `plain` (words in a row, the choice tinted)

`radioKeys` and `radioTabIndex` are the same keys for a row of radio items
that is not a `Segmented`. Used by Settings, Search, History, the actor page
and the record's source view. **Rule:** no hand-copied segmented buttons.

### The primitives

The rest of `components/ui/` are the shadcn primitives the console restyles
through its tokens and does not otherwise change: `avatar`, `breadcrumb`,
`button`, `card`, `collapsible`, `command` (under ⌘K and `ChoiceList`),
`dialog`, `dropdown-menu`, `empty`, `field`, `hover-card` (under
`IdentityHoverCard`), `input`, `input-group`, `kbd`, `label`, `popover`,
`scroll-area`, `separator`, `sheet`, `sidebar`, `skeleton`, `spinner`,
`table`, `tabs`, `textarea`, `toast` and `tooltip`. A tooltip hangs on a
button, never on a plain span.

## The property sheet

In `components/property-sheet/`, the record page's body and the create
sheet's.

### PropertySheet

One row per property: its label (and its key in technical mode) on the left,
its value on the right, 36px rows, empty properties folded into one "N empty"
line. Read and edit are the same row: a click on a value edits it in place,
with the control its datatype earns, and a save is a single-property `patch`
under `ifVersion`. Focus returns to the cell when the editor closes.

- `record`: the record, or a draft
- `kind`, `kinds`: its declaration and the registry
- `readOnly`: a provider's copy, nothing edited here
- `holders`: show who holds every value, not only departures
- `mappings`: the mapping declarations, so a synced value names its mapping
- `moved`: properties that just changed under the reader, marked briefly

### OwnershipChip

At a row's end, who holds the value when it departs from the default
(a provider's badge, an agent's mark, "_Provider_ differs"); a click opens the
detail under the row: the tier in words, who and when, the source record,
other versions with **Use _Provider_'s**, **Stop overriding** and **Use my
own value**. Props: `row`, `open`, `onToggle`, `through`.

### DeclaredValue

A declared property's value as every read surface draws it, containers and
all: an enum as its `EnumTag`, a state as its `StateBadge`, a reference as its
`ReferenceValue`, a secret as sealed dots, an empty value as `EmptyValue`.
Props: `spec`, `value`. Used by the sheet, the ownership detail, suggested
changes and History's value moves, so a value reads the same wherever it is
shown.

## The collection grid

In `components/data-table/`, the collection page's grid and what sits above
it.

### DataGrid

One page of records in a sheet that scrolls both ways under a pinned header
row and a pinned title column. The sorted column's header cell carries
`aria-sort`. Grouped, the rows are cut wherever their group changes and each
run is its own `tbody` under a head row with a fold button.

- `table`: the `useDataTable` instance
- `density`, `fill`, `loading`, `empty`, `scrollKey`, `marks`, `label`
- `groups`: `keyOf`, `head`, `label`, `collapsed`, `onToggle`

### ViewTabs

A collection's saved views as a strip of tabs above its toolbar: **All**, each
view by name, and **Save view**. A tab is chosen when the grid shows what it
names; the view picked last stays marked once the reader changes something,
and its menu saves or discards the changes. Save, rename, save changes and
delete each confirm through `ConfirmDialog`, and Save refuses while a saved
view is chosen, naming it, since a second view of the same shape could never
be told apart.

- `views`: this collection's `SavedView`s
- `active`: `all`, a view's id, or null
- `edited`: the changed view's id
- `busy`, `onPick`, `onSave`, `onRename`, `onReplace`, `onDelete`

### GroupByMenu and GroupHead

`GroupByMenu` is the toolbar's **Group** control: the enums, states and single
references a collection may be grouped by, and **No grouping** (`options`,
`value`, `labelOf`, `technical`, `onChange`). `GroupHead` is what a group's
head row says: the value as the grid's cells draw it (`EnumTag`, `StateBadge`,
`RecordRef`, or "No _property_"), the whole group's count, and where a run
carries on from or to another page (`prop`, `groupKey`, `label`, `count`,
`note`, `kinds`, `titles`). **Rule:** a group head draws its value through the
same mark the cells do.

## Agents and suggested changes

In `components/agent/` and beside `components/change-request.tsx`, the
controls the agents pages and the change request page share.

### GrantsEditor

What an agent may see and change, edited by collection: "Can see" and "Can
change", each the collections its grant names (a `KindRef`, or "All your
data" for `*`) with a remove, and **Add** opening a `ChoiceList` of
collections. Every pick is one patch of the agent record's `permissions`
under `ifVersion`, the rest of the object carried through; an edit that would
leave a tool the agent holds without its grant is held back with the reason
(`lib/agent-grants.ts`). Props: `agent`. Used by the agent page.

### AlwaysAllowButton and AllowRules

**Always allow this** on a suggested change a gate held: it confirms, saves
one `allow` per door verb naming that gate in `overrides`
([0109](../decisions/0109-an-allow-outranks-a-gate-only-by-naming-it.md)),
then applies the suggestion (`request`, `rule`, `deleting`). `AllowRules`
lists the rules it wrote for one agent, each with **Revoke**, which deletes
them after a confirmation (`agent`, `rules`). The rules are read and worded
by `lib/agent-rules.ts`. Used by the chat card, the agent panel and the agent
page.

### ModelKeyDialog and AddKeyButton

"Add your _Provider_ key": one sealed write of the llm/provider row's
`apiKey`, through the record page's own patch (`providerId`, `open`,
`onOpenChange`). `AddKeyButton` is the button that opens it (`providerId`,
`size`, `variant`). Used wherever an agent refuses for want of a key: the
chat's callout, the agent panel, the agent page.

### ReviewComparison and SuggestedAndApplied

The change request page's **Now** and **If applied** grid, editable: a click
on an If applied value opens the property sheet's own editor on a draft
(`SheetDraftContext`), and a row can be left out and put back. Props: `rows`
(`reviewRows` in `lib/changerequests.ts`), `compare`, `specs`, `kind`,
`kinds`, `target`, `edited`, `onEdit`, `emptyText`, `create`.
`SuggestedAndApplied` reads a decided request the owner adjusted: what was
suggested beside what was applied (`rows`, `specs`).
