# Codebase map: the console

Snapshot of 2026-10-11, taken by a read-only agent for the observations
build. Paths are relative to `web/console/` unless they start with `docs/`,
`internal/` or `.mise.toml`.

The console has no chart library, no date library, no date-range picker and
no Storybook, and `console:render` takes no screenshots. Every list goes
through one records route (`GET /api/v1/records`), and every kind is shown
by the same table, built from the kind's declaration.

## 1. Framework and structure

`package.json`: React 19 and Vite 8, TypeScript about 6, pnpm. Router
`@tanstack/react-router` 1.170 with routes written in code; URL state uses
`nuqs` 2.10. Data fetching `@tanstack/react-query` 5. Tables
`@tanstack/react-table` 9. UI kit: shadcn on `@base-ui/react`
(`components.json` style `base-nova`), Tailwind v4, `lucide-react` icons,
`cmdk`, react-hook-form with zod. Tests run on vitest 4 with jsdom and
Testing Library. `playwright-core` is only used by the render check.

Layout of `src/`: `main.tsx` (QueryClient and providers), `router.tsx`,
`pages/` (one file per route),
`components/{ui,identity,data-table,record,property-sheet,changelog,alerts,agent,...}`,
`hooks/`, `lib/` (pure helpers, each with a `.test.ts` beside it),
`lib/api/` (the typed client).

Registering a page (`src/router.tsx`): every signed-in page is a child of
`shellRoute` (lines 80-92; its `beforeLoad` is the session gate, its
component `AppShell`). The child is created at lines 94-354 and added to the
tree at lines 356-389:

```ts
export const historyRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/history",
  component: HistoryPage,
})
// shellRoute.addChildren([ homeRoute, ..., historyRoute, ... ])
```

Pages read their params through the route (`kindBrowseRoute.useParams()` at
`pages/kind-browse.tsx:183`) and their URL state through nuqs
`useQueryState`. A route only declares search params where a typed
navigation needs them (`validateSearch`, `router.tsx:127-140`).

Navigation: a new page touches four places: sidebar places (the `Place`
type and `NavRow` at `components/app-sidebar.tsx:109-142`, the list at
663-674, the footer rows at 680-684); the command menu places list
(`components/command-menu.tsx:51-57`); breadcrumbs and the browser-tab title
from `crumbsFor` at `components/app-shell.tsx:122` (tested in
`components/app-shell.test.ts`); the docs (the route table in
`web/console/README.md` and the page sections in `docs/console.md`).

How per-kind custom views are chosen: there is no registry that maps a kind
to a view. `/data/:authority/:pkg/:name` (`pages/kind-browse.tsx`) and
`/data/.../:id` (`pages/record.tsx`) are the same for every kind.
`RecordRef` always links to the generic record page
(`components/identity/record-ref.tsx:84-90`). Custom views exist today only
as: their own routes (`/agents`, `/agents/$id` show `core/agent` together
with `llm/thread`, `lib/agent-runs.ts:1-40`, `lib/agent-chat.ts:39`;
`/tools/...` shows `core/function`; `/merge-requests/$id` and
`/change-requests/$id`); the `REVIEW_ROUTES` kind-to-route map
(`components/record/record-model.ts:76-83`, used in
`components/record/connected.tsx:81,189`); sections switched on by a trait
(`pages/record.tsx:128,163-168` adds a "Sync" section when
`kindHasTrait(kind, SYNC_TRAIT_IDENTITY)` holds, `lib/sync.ts:244-253`;
this is the closest existing hook for the `observed` trait); panels
embedded in other pages (`AlertsPanel`,
`components/alerts/alerts-panel.tsx:105`, reads `core/alert`,
`lib/alerts.ts:11`, `lib/api/alerts.ts`, mounted on `pages/agent.tsx:251`,
`pages/tool.tsx:321` and `pages/provider.tsx:283`); a kind check in a form
(`components/record/create-sheet.tsx:212`). `RecordDocument` is exported
(`pages/record.tsx:104`), so a custom detail view can reuse it.

## 2. Data access (`src/lib/api/`)

`http.ts`: `request<T>()` (lines 117-168) adds `Authorization: Bearer`,
`X-Substrate-Actor: console`, and turns the error envelope into an
`ApiError`. A 401 calls `sessionExpired()` (line 164). Path helpers:
`splitKind` and `joinKind` (180-199), `collectionPath` (212), `rootPath`
(232), the `CORE_PACKAGE` and `LLM_PACKAGE` constants (27, 36).

Auth (`session.ts`): the token lives in localStorage under
`substrate.token`, `substrate.repository` and `substrate.tokenid` (lines
15-18). `main.tsx:18-20` sends a 401 to `/login`. Lines 22-40 set query
defaults (`staleTime` 30 s, no retry on a definite answer). Lines 45-47
clear the cache when the session changes.

Records list (`records.ts`): `ListParams` (lines 56-80): one kind as
authority, package and name, or `kinds[]`, plus `first`, `after` (opaque
keyset cursor), `offset` (the two cannot be sent together), `filter`,
`orderBy` (`"updatedAt:desc"`), `expand`, `count`. `listPath` is at
111-122. `fetchRecordsPage` (138-154) retries once without `expand` on a
422. `recordsQueryOptions(p)` (156-174) builds the query key from
`scopeKinds(p)` and the paging fields with `placeholderData: prev`. Infinite
keyset pattern: `referencingInfiniteOptions` (756-786). Counts:
`recordCountQueryOptions` (490). Single record: `recordQueryOptions` (596).
Cache invalidation after writes: `recordWriteReaches` (623). Alerts are an
example of a list built straight from `listPath` (`lib/api/alerts.ts:21-68`).

Filter grammar (`types.ts`): `RecordFilter` is at 428-445. `Cond`
(401-416) already has `gt`, `gte`, `lt`, `lte`, so a property time window
can be expressed today. `Page` is at 236-254, with `problems` for a window
read where `at` is bounded on both ends (248-250, and `records.ts:318-326`).

Live updates. The console never tails records with `watch=1`. It tails the
change feed instead: `watchChanges` in `changes.ts:511`, which takes a
`ChangeFeedFilter` (lines 42-63, filtered by `kinds`);
`useLiveRecords(kinds, invalidateKeys)` in `hooks/use-live-records.ts`;
`useLiveInvalidation` and `useChangeMarks` in
`hooks/use-live-invalidation.ts:86,139`, used at `kind-browse.tsx:260-273`.
The time-seek trick for "history until T" is in `changes.ts:410`.

Wire golden test: `types.ts` is written by hand to mirror
`internal/substrate`. `internal/substrate/wire_test.go` (`goldenPath` at
line 33, `wireTypes` from line 45) writes `src/lib/api/wire.golden.json`.
`src/lib/api/wire.golden.test.ts` checks each `Shape<T>` map against the
golden (lines 103-113, mirrors up to about 740). Every exported interface in
`lib/api/*.ts` must be either in the golden or listed in `notOnTheWire`
with a reason (lines 744-790); this is checked at lines 813-830. Regenerate
with `go test ./internal/substrate/ -run TestWireGolden -update`. Never edit
the golden by hand. Any new aggregate response types need a Go struct first.

## 3. Table and list UI

Generic records table: `pages/kind-browse.tsx`. Filters, sort, page, group
and search are URL state through nuqs (lines 186-217). Saved views are at
494-581. The list query is at 368-379. Columns are at 452-461, and
`useDataTable` at 483-492. The toolbar is at 888-975: `GroupByMenu`,
`DataTableFilters`, `SearchBox`, `DataGridSort`, `DataTableViewOptions`.
`DataGrid` is at 1006-1022 and `DataTablePagination` at 1027-1038.

Columns come from the declaration: `buildColumns(kind, kinds, titles,
opts)` is at `pages/kind-browse-columns.tsx:491`. It draws on
`columnProperties`, `filterableProperties`, `temporalProperties` and
`declaredProperties` in `lib/definition.ts:115-215`. `DeclaredProperty` is
at lines 51-101. Shared column factories (`timeColumn`, `actorColumn`, the
change-feed columns) are in `components/data-table/columns.tsx:35-329`.

Table props (`components/data-table/data-table.tsx`): `UseDataTableOptions`
(125-147): `columns`, `data`, `sorting?`, `onSortingChange?`, `getRowId?`,
`prefsKey?` (localStorage column prefs), `defaultHidden?`, `autoHidden?`.
`DataTableProps` (323-338, the simpler renderer for rails and feeds):
`table`, `onRowClick?`, `loading?`, `empty?`, `renderExpanded?`,
`expandable?`, `rowClassName?`, `density?: "default" | "compact"`. Column
`meta` holds `label`, `width` (fixed pixels) and `size` (flexible
`{min,max,weight}`), declared at lines 94-108. `DataGrid` props
(`data-grid.tsx:64-95`): `table`, `density`, `fill`, `loading`, `empty`,
`scrollKey`, `marks`, `label` (required), `groups?: GridGroups` (39-53),
`className`.

Filters: `DataTableFilters` (`components/data-table/data-table-filters.tsx:58-71`)
takes `fields: DeclaredProperty[]`, `filters`, `onChange`, `kinds?`,
`labelOf?` and `words?`. Each filter is a `field~op~value` token in the
URL, and `toRecordFilter` converts the list to the wire filter
(`lib/filters.ts:24-128`).

Pagination: `DataTablePagination` (numbered pages over `offset`,
`data-table-pagination.tsx:17-40`); `DataTableCursorPagination` (previous
and next for seq feeds, `data-table-cursor-pagination.tsx:16-30`).

Dates and time windows: there is no range picker. History's time filter
uses the same filter toolbar with `since` and `until` typed as text
(`lib/changelog.ts:205-209,252-262,307-318`), applied in
`components/changelog/changelog-table.tsx:125-191`.
`components/property-sheet/date-picker.tsx:59-82` is a single-value inline
editor; `calendar.ts` and `dates.ts` hold its helpers. Formatting lives in
`lib/format.ts` (`relativeTime`, `tableDateTime`, `shortDateTime`).
`Segmented` (`components/ui/segmented.tsx`) is the radio-group control
(History's view switch at `pages/history.tsx:119-134`); it suits a bucket
picker.

Charts and SVG: nothing exists to reuse. The only SVG is a 10 px progress
ring at `pages/kind-browse-columns.tsx:457-483`. `--chart-1` to `--chart-5`
exist (`index.css:21-25,129-133`) but are unused shadcn defaults. The repo's
`dataviz` skill (under `.claude/skills/`) is meant to be read before writing
chart code.

## 4. Testing and tooling

mise tasks (`.mise.toml`): `console:install`, `console:dev`,
`console:build`, `console:test`, `console:typecheck`, `console:lint`,
`console:fmt` and `console:fmt:check` are at lines 592-637.
`console:browser` and `console:render <base>` are at 639-656. `ci:console`
(typecheck, lint, fmt:check, build) is at 897-906, and `ci:console:test` at
908-910.

vitest: config is in `vite.config.ts` (`test` block); the default
environment is `node`. A test that renders starts with
`// @vitest-environment jsdom`. Setup is `src/test-setup.ts`.
Component-test pattern (`pages/kind-browse.test.tsx:1-60,398`): mock
`@tanstack/react-router` and `@/router`, stub `watchChanges`,
`vi.stubGlobal("fetch", ...)`, wrap in `QueryClientProvider`,
`NuqsTestingAdapter` and the preferences context. Guard tests a new page
must pass: `lib/type-floor.test.ts` (no text under 11.5 px);
`index-css.test.ts` (colour contrast); `wire.golden.test.ts`;
`lib/shipped-kinds.test.ts` (display names for shipped kinds);
`app-shell.test.ts` (breadcrumbs).

Lint and format: `eslint.config.js` uses typescript-eslint, react-hooks and
react-refresh, with prettier config last. `.prettierrc`: no semicolons,
double quotes, width 80, Tailwind class sorting.

`console:render` (`scripts/render-smoke.mjs`) drives headless Chrome over
`/`, `/login`, `/register` (creates a repository), `/data` and `/history`
(route table at lines 247-325). A route fails on a page error, a console
error, or an unexpected HTTP status of 400 or more (`EXPECTED_RESPONSES` at
48-54). It takes no screenshots. A new route is added to the `routes` array.

## 5. Styling and house rules

- `CLAUDE.md`, "Give each UI fact a clear role": no unrelated fragments side
  by side; headings, labels, values, status and actions kept apart; full
  kind and actor references where identity matters (wrapped, never
  shortened); the same labels in read and edit views.
- `docs/console/design-guide.md`: principles (22-41: everyday words with
  technical facts behind the `useTechnicalDetails()` switch, one component
  per idea, mono only for text you would copy); page shapes (105-125:
  `DocPage` and `TablePage` in `components/identity/page-layout.tsx`,
  `PageHeader` in `components/identity/page-header.tsx`, `SectionHead`);
  tokens (140-219), type scale (221-243), the word list (253-300),
  accessibility (304-333).
- `docs/console/elements.md` lists the identity components: `StateBadge`,
  `EnumTag`, `RecordRef`, `EmptyValue`, `Pill`.
- Tokens are in `src/index.css`: the Paper palette (`:root` from about line
  88, `.dark` from about 164) and semantic utilities (`text-faint`,
  `bg-hover`, `ok`, `warning`, `destructive` with their soft fills, ten kind
  hues). Never use raw palette colours.
- Dark mode uses the `.dark` class variant (`index.css:6`) through
  `components/theme-provider.tsx`. The choice is stored in the console
  preference record and set from the account menu (`app-sidebar.tsx:568-586`).
