# Console design guide

This is the console's design system as it is built: what the console is for,
who it speaks to, how its pages are laid out, the tokens, the words, and the
accessibility rules every page keeps. [The web console](../console.md)
describes each page; this page describes what they share, and
[the element catalogue](elements.md) describes each shared component.

The code is the source of truth. The tokens live in
`web/console/src/index.css`, the identity components in
`web/console/src/components/identity/`, the controls in
`web/console/src/components/ui/`, and the helpers this page names in
`web/console/src/lib/`. Where this page and the code disagree, the code is
right and this page is the bug.

The direction is **Paper**, chosen over a denser Workbench direction on
2026-09-25 from the [approved prototype](prototype/console-prototype.html).
[The first review](reviews/2026-09-24-first-review.md) says why the console
was redesigned and [the second](reviews/2026-09-26-design-review.md) measured
the build.

## Principles

1. **Your data first, the system when you ask for it.** A page leads with
   what the person keeps and what acts on it; the substrate's own machinery
   waits for Technical details.
2. **One element per idea.** A kind, a record, an actor, a state, an enum
   value, a status, an origin and an id each have exactly one component, and a
   page shows one only through it. Never hand-draw a kind.
3. **Friendly names for display, full references as identity.** A display
   name ("Tasks", "People") is a label and nothing else; wherever a kind is
   identified, it is `{authority}/{package}/{name}`.
4. **Dense where you scan, open where you read.** Tables and property sheets
   run 30 to 38px rows; descriptions and prose get room.
5. **Monospace only for what you would copy.** Ids, references, YAML, URLs
   and code. Never times, verbs, statuses or counts.
6. **Everyday words, technical details on a switch.** Every label is one a
   person who is not a developer understands; the switch adds the exact
   facts.
7. **Every destructive action confirms and names the consequence.** Errors
   say what went wrong and what to do.

## What the console is for

The console serves seeing, navigating and controlling four things: **your
data**, **your providers**, **your agents**, and **your (and your agents')
tools**. Every page belongs to one of them. History, Search and Settings
serve the four rather than standing beside them, and there is no inbox or
"what needs you" page: a change an agent suggests is a card in the thread
that asked for it, and a merge request is reached from the records it names
([0130](../decisions/0130-the-console-serves-four-things-and-its-navigation-follows-them.md)).

People build apps (a task manager, a trip planner, a recipe book) by asking
an agent, on top of the substrate. What those apps store shows up here as
ordinary collections, so an app needs no place of its own.

## Two readers

Everyday copy, for a person who is not a developer, is the default. One
per-person switch, **Technical details**, reveals what a developer or an
agent author needs, without taking anything away:

- full kind references, record ids, actor ids and property keys;
- holding tiers, mappings, permissions and changelog sequence numbers;
- the YAML (and JSON) source of a record and a kind's definition;
- the authority and package tree in the sidebar, the supporting and internal
  kinds, and the **Substrate** group.

Everything technical stays reachable in everyday mode: every identity mark
opens a hover card whose footer carries the full reference. A page asks one
question, `useTechnicalDetails()`, and the identity components ask it for
themselves, so a page built from them is right in both modes
([0131](../decisions/0131-the-console-writes-for-two-readers-behind-one-switch.md)).

## Navigation

The sidebar, top to bottom:

- the repository's name, which is the one account menu: **Account and
  settings**, **Appearance** (system, light, dark), the **Technical details**
  switch and **Sign out…**;
- **Search or jump to…**, which opens ⌘K;
- the five places: **Home**, **All data**, **Agents**, **Tools**,
  **Providers**;
- **Favorites**, then the collections grouped the way a person meets them:
  **Your data**, then one **From _Provider_** group per provider (folded at
  first), then, with Technical details on, **Substrate**;
- at the foot, **History**, **Settings** and the **Technical details**
  switch.

What a group lists is decided by each kind's declared `purpose`
([0106](../decisions/0106-a-kind-declares-its-purpose.md)): everyday mode
lists `primary` kinds by display plural; technical mode lists the authority,
package and kind tree by each kind's own name, supporting and internal kinds
tagged. Every kind under `substrate.reamde.dev` reads as internal. A row tips
its description, never its raw reference. Collapsed, the sidebar peeks as an
overlay while the pointer rests at the page's left side or on the toggle.

The breadcrumb above every page reads where the page sits ("Your data /
Tasks / Test the landing page"); the browser tab reads the same words, the
page first ("Test the landing page · Tasks", "Google · Providers",
"Settings"), and a polite live region announces each move.

## Layout

There are two page shapes, both in `components/identity/page-layout.tsx`, and
neither is ever centred.

| Shape | Used by | Padding | Width |
| --- | --- | --- | --- |
| `DocPage` | a record, a tool, a provider, a merge or change request, Home, History, Search, Settings, an actor | 36px top, 48px sides (24px and 16px on a phone) | the **record width** preference: narrow 720px, wide 960px, full |
| `TablePage` | a collection, All data, Tools, Providers, an authority or package | 24px top, 32px sides (16px on a phone) | the **table width** preference: wide 1200px, full |

A document starts at the left edge at the reader's chosen width. A collection
is a grid that fills its page: a sticky 34px header row and a pinned first
column (the title), rows of 38px (comfortable) or 30px (compact) from the
**Table rows** preference, fifty rows a page with the pager in the footer.

Every page's head is one `PageHeader`: the title, one quiet meta line, a
description, the actions. A record, a merge, a change request and a new
record use its `record` layout, the kind's glyph above a 32px title; every
other page uses `page`, a 26px title with the glyph beside it. Under 560px
the actions drop below the title, so a long title wraps between words. Every
section inside a page opens with one `SectionHead`.

### Preferences

The display settings follow the person: record width (default wide), table
width (default full), table rows (default comfortable), Technical details
(default off) and appearance (default system) are properties of the
`substrate.reamde.dev/core/consolepreference/navigation` record, beside the
favorites and folded groups. Whether the sidebar is open is a fact about one
window and is kept in this browser's `localStorage` alone, as are a
collection's last filters, sort, nesting and columns and the Search page's
ranking
([0132](../decisions/0132-console-preferences-follow-the-person-and-a-window-fact-stays-in-the-browser.md)).
`lib/console-preferences.ts` holds the split.

## Tokens

Every colour is a semantic token in `index.css`, defined for light (`:root`)
and dark (`.dark`), so both modes tune it and a page never reaches for a raw
palette utility. The shadcn names (`background`, `foreground`, `primary`,
`muted`, `border`, `destructive`, `sidebar-*`) carry the Paper values; the
console adds its own beside them.

### Colour

| Token | Light | Dark | For |
| --- | --- | --- | --- |
| `--background` | `#ffffff` | `#191919` | the page |
| `--sidebar` | `#f8f7f5` | `#202020` | the sidebar |
| `--panel` | `#fbfaf8` | `#1d1d1d` | a quiet panel, the hover card's footer |
| `--hover` | `rgba(55,53,47,.055)` | `rgba(255,255,255,.055)` | a hovered row, a neutral fill |
| `--selection` | `rgba(47,107,219,.09)` | `rgba(91,146,240,.16)` | a chosen row |
| `--foreground` | `#252420` | `#e8e7e3` | ink |
| `--muted-foreground` | `#5d5c56` | `#a9a8a3` | secondary ink |
| `--faint-text` | `#73716a` | `#8a8983` | quiet text that names or counts something (`text-faint`) |
| `--faint` | `#9a9892` | `#74736e` | icons, separators, decoration (`text-faint-deco`, `bg-faint-deco`) |
| `--border` | `#ecebe7` | `#2b2b29` | rules and cell lines |
| `--border-strong` | `#dedcd6` | `#3a3a37` | control outlines |
| `--primary` | `#2f6bdb` | `#5b92f0` | the one accent: primary buttons, focus rings |
| `--primary-text` / `--primary-soft` | `#2358bf` / `#eaf1fd` | `#8ab2f6` / 16% accent | accent words on their soft fill |
| `--ok` / `--ok-soft` | `#256f46` / `#e6f4ec` | `#5bc08a` / 14% | done, working |
| `--warning` / `--warn-soft` | `#94600f` / `#fbf1de` | `#e0a84a` / 14% | waiting, transient trouble |
| `--destructive` / `--bad-soft` | `#b94033` / `#fbe9e6` | `#ea7a6d` / 14% | failed, destructive actions |

**Why two faints.** Paper's quietness comes from its palette, and the faint
grey carries much of it. At `#9a9892` it reads about 2.9:1 on white and 2.7:1
on the sidebar, which is fine for an icon or a rule and too little for the
words that tell a reader what a number is: column headers, sidebar counts,
meta lines, setting descriptions, "Empty" (review finding V1). So the token
split. `--faint-text` clears 4.5:1 on every light surface (4.9:1 on white,
4.6:1 on the sidebar) and on the dark page (5.0:1), and the `text-faint`
utility points at it, because text is what that utility is overwhelmingly used
for; `--faint` keeps its old value for decoration, under the `faint-deco`
utilities. The review proposed `#7d7b74`; the build went one step darker so
the sidebar clears the bar too. The light ok and warning inks are one step
darker than the prototype's for the same reason: a pill's word on its soft
fill at 12px. So are the destructive ink (about 4.0:1 on its soft fill
before) and the gray, orange, yellow, green and teal kind hues' inks (the
yellow was about 3.5:1), because an enum value is its words on its hue:
every ink now clears 4.5:1 on its own fill in both modes, and
`src/index-css.test.ts` holds it.

### Kind hues

Ten hues, each a tile background and a glyph ink, used by `KindGlyph`,
`EnumTag` and the actor discs.

| Hue | Light bg / fg | Dark bg / fg |
| --- | --- | --- |
| gray | `#efeeeb` / `#6c6a65` | `#2c2c2a` / `#b3b1ab` |
| brown | `#f3eae3` / `#8a5a3b` | `#352a22` / `#d2a17e` |
| orange | `#fcebdd` / `#a55319` | `#3a2717` / `#f09a5a` |
| yellow | `#fbf1d2` / `#87670f` | `#352e17` / `#e3c15a` |
| green | `#e3f2e8` / `#2c774b` | `#1b3024` / `#6fcb93` |
| teal | `#ddf1f0` / `#1e7673` | `#163130` / `#5ecfc9` |
| blue | `#e3edfb` / `#2f63c0` | `#1b2a42` / `#86aef3` |
| purple | `#eee8fa` / `#6b4bb8` | `#2a2340` / `#b7a0f0` |
| pink | `#fbe7f0` / `#b03a72` | `#3a1f2c` / `#f08dba` |
| red | `#fbe6e3` / `#be3b2e` | `#3c211d` / `#f28b7e` |

A kind's glyph is stable (`lib/kind-glyph.ts`): the icon comes from the words
of its name, the last word that has one winning (`task` a check, `person` a
user, `calendar` and `event` a calendar, `series` a repeat, `email` and
`message` mail and a speech bubble, a box when nothing matches); the hue
comes from the same word where it has one, else from an FNV-1a hash of the
full reference, so a kind looks the same on every surface for as long as it
exists.

An enum value's hue (`lib/enum-hue.ts`): a property named like priority,
severity, urgency or importance climbs a ladder in its declared order (gray,
blue, yellow, orange, red); the words none, unknown and other read plain; any
other value takes a stable hashed hue. A state's colour is its meaning
(`lib/state-words.ts`): active (blue), ok (green), pending (amber), stopped
(grey) and bad (red), a state the table does not know reading as waiting at
its machine's initial state and active after.

### Type

The face is Geist (the variable build, bundled); the mono voice is the
system's monospace stack (`ui-monospace`, SF Mono, Menlo), and the `data`
utility sets it at 0.86em for an identifier inside a sentence. Body text is
14px at 1.45.

| Size | For |
| --- | --- |
| 11.5px | the floor: purpose tags, badge letters, the technical line under a sentence, inline ids |
| 12px | pills, a hover card's sub line, quiet notes |
| 12.5px | a page's meta line, section hints, column headers, the footer |
| 13px | list rows, choices, controls |
| 13.5px | the sidebar's rows, the sheet's labels, a hover card's title |
| 14px | body text and values |
| 15px | a section heading (600, −0.01em) |
| 22px | the numbers on Home's cards |
| 26px | a page title (650, −0.02em) |
| 32px | a record's title (700, −0.025em) |

Nothing is set below 11.5px; `lib/type-floor.test.ts` reads every size the
source writes in pixels or rems and refuses one under the floor.

### Space, radius, rows

Spacing is Tailwind's default 0.25rem step (the old 0.235rem override is
gone). The base radius is 6px (`--radius: 0.375rem`), with the smaller and
larger radii derived from it. Rows: 38px comfortable and 30px compact in the
grid, a 34px grid header, 36px rows on the property sheet, 30px rows in the
sidebar and in a `ChoiceList`. One shadow, `shadow-card`, for anything that
floats.

## Words

Copy is second person, plain and short, written for a person who is not a
developer: "Works at", "Kept up to date by Google", "Use Linear's", "Stop
overriding · follow Linear", "Tasks with this as their Assignee", "1 of 3
done", "Nothing else points to this yet", "What it's allowed to do", "It
hasn't run yet." A confirmation is a question, a sentence of consequence and
a button that says the verb ("Pause Google Contacts sync? It won't run on its
own until you resume it. Nothing it brought in changes."). An error says what
went wrong and what to do.

One word per job, everywhere:

| Say | Not |
| --- | --- |
| Apply | Accept |
| Dismiss (a merge keeps "Keep them apart") | Reject |
| Suggested | Proposed |
| Update | Upgrade |
| Add | Import, Take |
| Try again | Retry |
| Appearance | Theme |
| History | changelog, log |
| property | field |
| Loading | Reading |
| Providers | integrations |
| collection (everyday) | kind |

The contract's live words (record, kind, package, authority, provider,
sample, bundle, agent, trigger, reference) stay the truth in the API and in
technical mode; a function is labelled a **tool**. The dead words in
[terms](../terms.md) never appear in UI copy: `relationship` is a sample
property's name, which is data and fine, and nowhere else.

A kind's **display name** is built by `lib/kind-names.ts` from its lowercase
compound name: the name is split into known words (the fewest-words split, the
longer first word winning a tie), acronyms and brands keep their capitals
("API keys", "Gmail threads", "WHOOP"), the first word is capitalised, and the
last word takes the plural ("People", "Calendar event series", "Codes of
conduct" for an "X of Y" name). A name that does not split reads as itself,
capitalised. A record with no title is "Untitled _person_", never its id.
Display names are UI labels only: never sent to the API, never standing where
a kind is identified
([0101](../decisions/0101-a-kind-trait-or-callable-is-named-in-full-on-every-surface.md)).

A state's words come from `lib/state-words.ts`: `proposed` reads
**Suggested**, `abandoned` **Dropped**, any other state its name capitalised;
technical mode appends the stored value.

## Accessibility

- **Contrast.** Text that names or counts something takes `--faint-text` or
  darker, 4.5:1 on its surface; decoration takes `--faint`.
- **Focus.** Every control shows a 2px ring on `:focus-visible`. An in-place
  editor that closes returns focus to the cell that opened it
  (`components/property-sheet/focus-return.ts`), so Tab carries on from the
  row, not from the top of the page. A record's title edits from the
  keyboard, and a value cell announces its label and value ("Status: Open,
  edit").
- **Targets.** A small control (a copy button, a tree expander, the switch,
  a row expander) grows its hit area to 24px with the `hit-area` utility, an
  invisible square around it, without growing what it draws.
- **Reduced motion.** Under `prefers-reduced-motion: reduce`, transitions and
  animations collapse to nothing (a spinner still turns), and a scroll the
  console starts itself asks `scrollMotion()` whether to glide.
- **Tooltips from the keyboard.** A tooltip hangs on a button, never on a
  plain span, so a keyboard reaches it.
- **Live regions.** The shell announces each route change politely; an open
  collection announces rows that changed under the reader ("3 tasks
  updated"); the calendar announces its month.
- **Keys.** `Segmented` is a radio group with roving tabindex, arrow keys,
  Home and End. `ChoiceList` moves with the arrows and picks with Enter. The
  grid's scroll container takes focus and has a name, and a focused cell
  scrolls clear of the pinned column.
- **Landmarks.** The shell holds the page's one `main`.
- **Type.** Nothing below 11.5px.

`aria-sort` on sorted column headers is not there yet (issue #677).

## What holds this

Tests hold some of it: the type floor (`lib/type-floor.test.ts`), the
preference split (`lib/console-preferences.test.ts`), display names
(`lib/kind-names.test.ts`), the identity marks in both modes
(`components/identity/identity.test.tsx`), `Segmented`, `ChoiceList` and
`ConfirmDialog` (their tests beside them in `components/ui/`). The rest (one
element per idea, the word list, mono only for what you copy, no reference
outside technical mode) is held by review.
