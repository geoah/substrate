/** A collection (`/data/:authority/:package/:kind`): every record of one kind
 * in a grid that fills the page — the header, the toolbar, the grid (which
 * scrolls both ways under a pinned header row and a pinned title column), and
 * the footer pinned under it with the range and the pages.
 *
 * Server-side everything: the filter, the search, the sort and the page live
 * in the URL (nuqs) and travel to the wire as `?filter=/orderBy=/offset=`.
 * A page is `offset=` on the wire (decision 0084), so every page is one
 * request away and `?page=` makes one linkable. The bounded count sizes the
 * footer, and Next reads the page's own cursor rather than that count, so a
 * collection past the count's ceiling still pages to its end.
 *
 * The kind's DEFINITION is the second view (`?tab=definition`), reached from
 * the header in technical mode.
 *
 * A TREE where the kind allows one: a single reference pinned at the kind
 * itself nests the collection. The page's rows are then the records naming no
 * parent, and each opens onto the records naming it
 * (`hooks/use-record-tree.ts`). `?nest=false` draws the same rows flat.
 * A filter or a search keeps the tree but nests the MATCHES: the page is every
 * match, a match sits under its parent when that parent matches too and is on
 * the page, and any other stands at the top level saying which record it is
 * in (lib/record-tree.ts, `matchedRoots`). */

import { useEffect, useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import type { SortingState, Updater } from "@tanstack/react-table"
import {
  CodeIcon,
  InboxIcon,
  ListTreeIcon,
  PlusIcon,
  SearchXIcon,
} from "lucide-react"
import {
  parseAsArrayOf,
  parseAsBoolean,
  parseAsInteger,
  parseAsString,
  parseAsStringLiteral,
  useQueryState,
} from "nuqs"

import { DataGrid } from "@/components/data-table/data-grid"
import { DataGridSort } from "@/components/data-table/data-grid-sort"
import { useDataTable } from "@/components/data-table/data-table"
import { DataTableFilters } from "@/components/data-table/data-table-filters"
import { DataTablePagination } from "@/components/data-table/data-table-pagination"
import { RowTreeProvider } from "@/components/data-table/data-table-tree"
import { DataTableViewOptions } from "@/components/data-table/data-table-view-options"
import { CopyButton } from "@/components/identity/copy-button"
import { KindGlyph } from "@/components/identity/kind-glyph"
import { IdentityHoverCard } from "@/components/identity/identity-hover-card"
import { KindCard, KindPath } from "@/components/identity/kind-ref"
import { PageHeader } from "@/components/identity/page-header"
import { TablePage } from "@/components/identity/page-layout"
import { ProviderBadge } from "@/components/identity/provider-badge"
import { KindDefinition } from "@/components/record/definition"
import { SearchBox } from "@/components/search-box"
import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import {
  useDensity,
  useLayoutWidths,
  useTechnicalDetails,
} from "@/hooks/use-console-preferences"
import {
  useChangeMarks,
  useLiveInvalidation,
} from "@/hooks/use-live-invalidation"
import { useRecordTree } from "@/hooks/use-record-tree"
import { providerOfKind } from "@/lib/actor-identity"
import {
  formatCount,
  recordCountQueryOptions,
  recordsQueryOptions,
} from "@/lib/api/records"
import { kindsQueryOptions } from "@/lib/api/kinds"
import type { KindInfo } from "@/lib/api/types"
import {
  expandableReferences,
  filterableProperties,
  kindByCollection,
} from "@/lib/definition"
import {
  decodeFilters,
  encodeFilter,
  loadBrowsePrefs,
  saveBrowsePrefs,
  toRecordFilter,
} from "@/lib/filters"
import { emptyColumnIds, propertyLabel } from "@/lib/grid-values"
import { displayName, displayPlural, lowerFirst } from "@/lib/kind-names"
import { nestingProperty, rootsFilter } from "@/lib/record-tree"
import { titlesFromIncluded } from "@/lib/reference-titles"
import { cn } from "@/lib/utils"
import {
  buildColumns,
  columnIdOf,
  columnValue,
  defaultHiddenColumns,
  sortPropertyOf,
} from "@/pages/kind-browse-columns"
import { kindBrowseRoute } from "@/router"
import { kindDescription } from "@/lib/kind-copy"

const PAGE_SIZE = 50
const DEFAULT_SORT = "updatedAt:desc"

/** The views, in bar order; the records lead and are the default. */
const TABS = ["records", "definition"] as const
const tabParser = parseAsStringLiteral(TABS)
  .withDefault("records")
  .withOptions({ history: "push" })

/** The page's side gutter, shared by every band so they line up. */
const GUTTER = "px-4 md:px-8"
const GUTTER_MX = "mx-4 md:mx-8"

function parseSort(sort: string): SortingState {
  const [property, dir] = sort.split(":")
  return property ? [{ id: columnIdOf(property), desc: dir !== "asc" }] : []
}

/** What a nested row's children are called: "subtasks" for tasks, "nested
 * calendar events" where the plural is more than a word. */
function childNoun(kind: KindInfo): string {
  const plural = lowerFirst(displayPlural(kind))
  return plural.includes(" ") ? `nested ${plural}` : `sub${plural}`
}

export function KindBrowsePage() {
  // The route params are the kind reference, segment for segment.
  const { authority, pkg, name } = kindBrowseRoute.useParams()
  const [technical] = useTechnicalDetails()
  const [density] = useDensity()
  const { tableWidth } = useLayoutWidths()

  const [tab, setTab] = useQueryState("tab", tabParser)
  const [sort, setSort] = useQueryState(
    "sort",
    parseAsString.withDefault(DEFAULT_SORT)
  )
  const [filterTokens, setFilterTokens] = useQueryState(
    "filter",
    parseAsArrayOf(parseAsString).withDefault([])
  )
  // The whole-record text filter, in the URL beside the property filters so a
  // view that searched is shareable; not in the stored prefs, because a search
  // is the question of the moment, not the shape of the view.
  const [search, setSearch] = useQueryState(
    "search",
    parseAsString.withDefault("")
  )
  // The page is in the URL too, so a page of a collection is a link. It is
  // not persisted: where a reader had got to is not a view preference.
  const [pageParam, setPageParam] = useQueryState(
    "page",
    parseAsInteger.withDefault(1)
  )
  // The tree switch: in the URL so a flat view is shareable, and in the
  // stored prefs so a reader who turned it off stays off.
  const [nest, setNest] = useQueryState(
    "nest",
    parseAsBoolean.withDefault(true)
  )

  // A BARE url restores the last-used view from localStorage, one dimension
  // at a time; an explicit ?filter=/?sort= always wins (shareable views stay
  // exact). Writes happen in the change handlers below, never here — an
  // effect writing on mount would wipe the store before this restore ran.
  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    const stored = loadBrowsePrefs(`${authority}/${pkg}`, name)
    if (!stored) return
    let restored = false
    if (!params.has("filter") && stored.filter?.length) {
      void setFilterTokens(stored.filter, { history: "replace" })
      restored = true
    }
    if (!params.has("sort") && stored.sort) {
      void setSort(stored.sort, { history: "replace" })
      restored = true
    }
    if (!params.has("nest") && stored.nest === false) {
      void setNest(false, { history: "replace" })
      restored = true
    }
    // A restored view is another view, numbered from its own first page.
    if (restored && params.has("page")) {
      void setPageParam(null, { history: "replace" })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- once per collection
  }, [authority, pkg, name])

  /** Write-through: the store always mirrors the view the handlers just set. */
  function persist(next: { filter?: string[]; sort?: string; nest?: boolean }) {
    saveBrowsePrefs(`${authority}/${pkg}`, name, {
      filter: next.filter ?? filterTokens,
      sort:
        (next.sort ?? sort) === DEFAULT_SORT ? undefined : (next.sort ?? sort),
      nest: (next.nest ?? nest) ? undefined : false,
    })
  }

  const registry = useQuery(kindsQueryOptions)
  const kindInfo = registry.data
    ? kindByCollection(registry.data, authority, pkg, name)
    : undefined

  // Records an agent, a sync or another tab writes re-read here as they
  // land, and the rows they moved carry a brief mark.
  const { marks, mark } = useChangeMarks()
  const [announcement, setAnnouncement] = useState("")
  useLiveInvalidation(
    { kinds: kindInfo ? [kindInfo.identity] : [] },
    (changed) => {
      const live = changed.filter((c) => !c.deleted)
      mark(live.map((c) => c.id))
      if (kindInfo && live.length)
        setAnnouncement(
          `${live.length} ${lowerFirst(live.length === 1 ? displayName(kindInfo) : displayPlural(kindInfo))} updated`
        )
    }
  )

  const filters = useMemo(() => decodeFilters(filterTokens), [filterTokens])
  const filterFields = useMemo(
    () => (kindInfo ? filterableProperties(kindInfo) : []),
    [kindInfo]
  )
  const recordFilter = useMemo(() => {
    const base = toRecordFilter(filters, filterFields)
    const words = search.trim()
    return words ? { ...base, search: words } : base
  }, [filters, filterFields, search])

  const nestProperty = useMemo(
    () => (kindInfo ? nestingProperty(kindInfo) : undefined),
    [kindInfo]
  )
  const hasFilters = filters.length > 0 || search.trim().length > 0
  const nesting = nestProperty !== undefined && nest
  // Unfiltered, the page is the top level and the footer pages it; filtered,
  // the page is the matches (the tree hook sorts out which stand on top).
  const nestingRoots = nesting && !hasFilters
  const listFilter = useMemo(
    () =>
      nestingRoots && nestProperty
        ? rootsFilter(recordFilter, nestProperty.name)
        : recordFilter,
    [nestingRoots, nestProperty, recordFilter]
  )

  // A hand-typed ?page= is clamped to a page that exists; the request below
  // is built from this, never from the raw parameter.
  const page = Number.isFinite(pageParam)
    ? Math.max(1, Math.trunc(pageParam))
    : 1
  // A changed filter, sort or tree switch renumbers the whole collection, so
  // the view resets to page one — in the handlers that change it, never by
  // watching the derived view: that also moves when the registry arrives and
  // turns a flat view into a tree, and on back and forward, where the URL
  // already names the page it wants.

  // Every reference this kind declares rides the page read as `expand=`, so
  // a reference column reads as the referent's NAME instead of its id, in one
  // sidecar on the read the grid already makes.
  const expand = useMemo(
    () => (kindInfo ? expandableReferences(kindInfo) : []),
    [kindInfo]
  )

  const listOptions = recordsQueryOptions({
    authority,
    package: pkg,
    name,
    first: PAGE_SIZE,
    offset: (page - 1) * PAGE_SIZE,
    filter: listFilter,
    orderBy: sort,
    expand,
  })
  const records = useQuery({ ...listOptions, enabled: Boolean(kindInfo) })

  const roots = records.data?.records ?? []
  const tree = useRecordTree({
    kind: kindInfo,
    property: nesting ? nestProperty : undefined,
    roots,
    filter: recordFilter,
    filtered: hasFilters,
    orderBy: sort,
    expand,
  })
  const rows = tree.rows
  const referenceTitles = useMemo(
    () => titlesFromIncluded({ ...records.data?.included, ...tree.included }),
    [records.data, tree.included]
  )
  const pageCursor = records.data?.cursor
  // A single page with nothing behind it IS the exact count, for free. Any
  // larger collection pays the bounded count walk, which the footer's numbers
  // need — but only the numbers: Next reads the page's own cursor. Under the
  // tree these count the top-level rows, which is what the footer pages.
  const derivedTotal =
    records.data && !pageCursor && page === 1 ? roots.length : undefined
  const count = useQuery({
    ...recordCountQueryOptions(authority, pkg, name, listFilter),
    enabled: Boolean(kindInfo) && (page > 1 || Boolean(pageCursor)),
  })
  const total = derivedTotal ?? count.data?.value
  const totalCapped = derivedTotal === undefined && count.data?.capped
  // Under the tree the collection is more than its top-level rows: the same
  // bounded walk over the view's own filter says how many.
  const collectionCount = useQuery({
    ...recordCountQueryOptions(authority, pkg, name, recordFilter),
    enabled: Boolean(kindInfo) && nestingRoots,
  })
  const totalText = nestingRoots
    ? collectionCount.data
      ? formatCount(collectionCount.data)
      : undefined
    : derivedTotal !== undefined
      ? derivedTotal.toLocaleString()
      : count.data
        ? formatCount(count.data)
        : undefined

  // A ?page= past the end lands on the last page that has rows. A CAPPED
  // count is a floor, so it can never justify moving a reader back.
  const pageCount =
    total !== undefined && !totalCapped
      ? Math.max(1, Math.ceil(total / PAGE_SIZE))
      : undefined
  useEffect(() => {
    if (pageCount !== undefined && page > pageCount) {
      void setPageParam(pageCount === 1 ? null : pageCount, {
        history: "replace",
      })
    }
  }, [page, pageCount, setPageParam])

  function goToPage(next: number) {
    void setPageParam(next <= 1 ? null : next)
  }
  function resetPages() {
    if (page !== 1) void setPageParam(null)
  }

  const noun = kindInfo ? childNoun(kindInfo) : "rows"
  const columns = useMemo(
    () =>
      kindInfo
        ? buildColumns(kindInfo, registry.data ?? [], referenceTitles, {
            technical,
            childNoun: noun,
          })
        : [],
    [kindInfo, registry.data, referenceTitles, technical, noun]
  )
  const defaultHidden = useMemo(
    () => (kindInfo ? defaultHiddenColumns(kindInfo) : []),
    [kindInfo]
  )
  // Columns with nothing in them on the rows loaded open hidden; the footer
  // says so and the Columns menu brings any of them back.
  const emptyIds = useMemo(
    () =>
      records.data
        ? emptyColumnIds(
            columns.map((c) => c.id ?? "").filter((id) => id !== "title"),
            rows,
            columnValue
          )
        : [],
    [records.data, columns, rows]
  )

  const sorting = useMemo(() => parseSort(sort), [sort])
  function onSortingChange(updater: Updater<SortingState>) {
    const next = typeof updater === "function" ? updater(sorting) : updater
    const first = next[0]
    const nextSort = first
      ? `${sortPropertyOf(first.id)}:${first.desc ? "desc" : "asc"}`
      : DEFAULT_SORT
    void setSort(nextSort)
    resetPages()
    persist({ sort: nextSort })
  }

  const table = useDataTable({
    columns,
    data: rows,
    sorting,
    onSortingChange,
    getRowId: (row) => row.id,
    prefsKey: `browse:${authority}/${pkg}/${name}`,
    defaultHidden,
    autoHidden: emptyIds,
  })

  // Only the REGISTRY gates the whole page — it names the collection and it
  // is what the Definition view reads. Records pending or failing is the
  // grid's business alone.
  if (registry.isPending) {
    return <BrowseSkeleton />
  }

  if (registry.isError) {
    return (
      <PageEmpty
        icon={<SearchXIcon />}
        title="This collection didn't load"
        description="The list of kinds didn't come back, and this page needs it to know what the collection is."
      >
        <Button
          variant="outline"
          size="sm"
          onClick={() => void registry.refetch()}
        >
          Try again
        </Button>
      </PageEmpty>
    )
  }

  if (!kindInfo) {
    return (
      <PageEmpty
        icon={<SearchXIcon />}
        title="No such collection"
        description={`This repository has no kind called ${authority}/${pkg}/${name}.`}
      />
    )
  }

  const plural = displayPlural(kindInfo)
  const singular = displayName(kindInfo)
  const provider = providerOfKind(kindInfo.identity)
  const showTabs = tab === "definition"
  const emptyHidden = table.options.meta?.emptyHidden ?? []
  const loadingPage = records.isPending || tree.loading
  const refetching = records.isPlaceholderData && records.isFetching

  // Everyday, the head is what the collection is to the reader: its mark,
  // its name, what it holds and who keeps it. The kind reference is a
  // technical fact: on the line with the switch on, in the hover card always.
  // The whole collection's size, where the page knows it exactly.
  const kindCount = hasFilters
    ? undefined
    : nestingRoots
      ? collectionCount.data && !collectionCount.data.capped
        ? collectionCount.data.value
        : undefined
      : totalCapped
        ? undefined
        : total
  const providerNote = provider && (
    <span className="inline-flex items-center gap-1.5">
      <ProviderBadge provider={provider.key} size="xs" />
      Read-only copies, kept up to date by {provider.name}
    </span>
  )
  const header = (
    <div className={cn("shrink-0 pt-6", GUTTER)}>
      <PageHeader
        title={
          <IdentityHoverCard
            trigger={<span />}
            className="inline-flex items-center gap-2.5"
            card={(open) =>
              open && <KindCard kind={kindInfo} count={kindCount} />
            }
          >
            <KindGlyph kind={kindInfo} size="md" />
            {plural}
          </IdentityHoverCard>
        }
        meta={
          technical ? (
            <>
              <span className="inline-flex min-w-0 items-center gap-1">
                <KindPath reference={kindInfo.identity} />
                <CopyButton
                  value={kindInfo.identity}
                  label="Copy the kind reference"
                />
              </span>
              {providerNote}
              {tab !== "definition" && (
                <Button
                  variant="ghost"
                  size="xs"
                  className="-my-1 h-[22px] gap-1 px-1.5 font-normal text-faint"
                  onClick={() => void setTab("definition")}
                >
                  <CodeIcon className="size-3.5" />
                  Definition
                </Button>
              )}
            </>
          ) : (
            providerNote
          )
        }
        description={kindDescription(kindInfo, technical)}
        actions={
          provider ? undefined : (
            <Button
              size="sm"
              className="gap-1.5"
              render={
                <Link
                  to="/data/$authority/$pkg/$name/new"
                  params={{ authority, pkg, name }}
                />
              }
            >
              <PlusIcon className="size-3.5" />
              New {singular.charAt(0).toLowerCase() + singular.slice(1)}
            </Button>
          )
        }
      />
    </div>
  )

  const tabBar = showTabs && (
    <div
      role="tablist"
      aria-label="Views of this collection"
      className={cn(
        "mt-4 flex shrink-0 gap-4 border-b border-border",
        GUTTER_MX
      )}
    >
      {TABS.map((key) => (
        <button
          key={key}
          type="button"
          role="tab"
          aria-selected={tab === key}
          className={cn(
            "-mb-px cursor-pointer border-b-2 border-transparent pb-2 text-[13px] text-muted-foreground outline-none hover:text-foreground focus-visible:text-foreground",
            tab === key && "border-foreground font-medium text-foreground"
          )}
          onClick={() => void setTab(key)}
        >
          {key === "records" ? "Records" : "Definition"}
        </button>
      ))}
    </div>
  )

  if (tab === "definition") {
    return (
      <TablePage className="flex min-h-0 flex-1 flex-col px-0 py-0 md:px-0">
        {header}
        {tabBar}
        <div className="min-h-0 flex-1 overflow-auto">
          {/* The definition pads itself; this tops it up to the page's
              gutter so it lines up with the header. */}
          <div className="md:px-2">
            <KindDefinition kind={kindInfo} kinds={registry.data ?? []} />
          </div>
        </div>
      </TablePage>
    )
  }

  const emptyState = (
    <Empty className="py-16">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <InboxIcon />
        </EmptyMedia>
        <EmptyTitle>
          {hasFilters ? "Nothing matches" : `No ${lowerFirst(plural)} yet`}
        </EmptyTitle>
        <EmptyDescription>
          {hasFilters
            ? "No record matches the filters and search you set."
            : provider
              ? `${provider.name} adds them here when it syncs.`
              : `Add the first one with New ${singular.toLowerCase()}, or ask an agent to.`}
        </EmptyDescription>
      </EmptyHeader>
      {hasFilters && (
        <EmptyContent>
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              void setFilterTokens(null)
              void setSearch(null)
              resetPages()
              persist({ filter: [] })
            }}
          >
            Clear filters
          </Button>
        </EmptyContent>
      )}
    </Empty>
  )

  const summary =
    nestingRoots && total !== undefined && collectionCount.data ? (
      <span className="tabular-nums">
        {formatCount(collectionCount.data)} {lowerFirst(plural)},{" "}
        {total.toLocaleString()}
        {totalCapped ? "+" : ""} at the top level
      </span>
    ) : undefined

  return (
    <TablePage className="flex min-h-0 flex-1 flex-col px-0 py-0 md:px-0">
      {header}
      {tabBar}
      <div
        className={cn(
          "flex shrink-0 flex-wrap items-center gap-1.5 border-b border-border py-2.5",
          showTabs ? "mt-0" : "mt-3.5",
          GUTTER_MX
        )}
      >
        {nestProperty && (
          <button
            type="button"
            aria-pressed={nest}
            title={
              hasFilters
                ? `While filtered, a match sits under the ${lowerFirst(singular)} it belongs to when that one matches too. Otherwise it shows at the top level, with the ${lowerFirst(singular)} it's in beside its name`
                : `The top level is the ${lowerFirst(plural)} with no ${lowerFirst(propertyLabel(nestProperty.name))}; each opens onto the ones that name it`
            }
            className="inline-flex h-7 cursor-pointer items-center gap-1.5 rounded-md border border-border-strong bg-background px-2.5 text-[12.5px] text-muted-foreground outline-none hover:bg-hover focus-visible:ring-2 focus-visible:ring-ring/50 aria-pressed:border-transparent aria-pressed:bg-primary-soft aria-pressed:text-primary-text"
            onClick={() => {
              void setNest(!nest)
              resetPages()
              persist({ nest: !nest })
            }}
          >
            <ListTreeIcon className="size-3.5" />
            {technical ? (
              <>
                Nested by{" "}
                <span className="font-mono text-[11.5px]">
                  {nestProperty.name}
                </span>
              </>
            ) : (
              `Show ${noun} nested`
            )}
          </button>
        )}
        <DataTableFilters
          className="gap-1.5 px-0 py-0"
          fields={filterFields}
          filters={filters}
          kinds={registry.data ?? []}
          labelOf={technical ? undefined : propertyLabel}
          words
          onChange={(next) => {
            const tokens = next.map(encodeFilter)
            void setFilterTokens(tokens.length ? tokens : null)
            resetPages()
            persist({ filter: tokens })
          }}
        />
        <span className="flex-1" />
        {/* Words against every text this kind indexes (the wire's
            `filter.search`), composed with the property filters and the
            sort. */}
        <SearchBox
          className="h-[30px] w-60 [&_input]:font-sans"
          label={`Search ${lowerFirst(plural)}`}
          placeholder={`Search ${totalText && totalText !== "0" && !nesting ? `${totalText} ` : ""}${lowerFirst(plural)}`}
          value={search}
          onChange={(next) => {
            void setSearch(next || null)
            resetPages()
          }}
        />
        <DataGridSort table={table} />
        <DataTableViewOptions table={table} toolbar />
      </div>
      {records.isError ? (
        <PageEmpty
          icon={<SearchXIcon />}
          title={`${plural} didn't load`}
          description={records.error.message}
        >
          <Button
            variant="outline"
            size="sm"
            onClick={() => void records.refetch()}
          >
            Try again
          </Button>
        </PageEmpty>
      ) : (
        <>
          <RowTreeProvider
            tree={
              tree.active
                ? {
                    nodes: tree.nodes,
                    toggle: tree.toggle,
                    context: tree.context,
                  }
                : null
            }
          >
            <DataGrid
              table={table}
              density={density}
              fill={tableWidth === "full"}
              loading={loadingPage}
              empty={emptyState}
              scrollKey={page}
              marks={marks}
              className={cn(
                "flex-1 border-b border-border",
                refetching && "opacity-60 transition-opacity",
                GUTTER_MX
              )}
            />
          </RowTreeProvider>
          <p aria-live="polite" className="sr-only">
            {announcement}
          </p>
          <DataTablePagination
            className={cn("pt-2.5 pb-3", GUTTER)}
            page={page}
            pageSize={PAGE_SIZE}
            rows={roots.length}
            total={total}
            totalCapped={totalCapped}
            hasNext={Boolean(pageCursor)}
            onPage={goToPage}
            loading={refetching}
            summary={summary}
          >
            {emptyHidden.length > 0 && (
              <span>
                ·{" "}
                {emptyHidden.length === 1
                  ? "1 column with nothing in it is hidden"
                  : `${emptyHidden.length} columns with nothing in them are hidden`}
                <button
                  type="button"
                  className="ml-1.5 cursor-pointer text-muted-foreground underline decoration-border-strong underline-offset-2 hover:text-foreground"
                  onClick={() => table.options.meta?.showEmptyColumns?.()}
                >
                  Show
                </button>
              </span>
            )}
          </DataTablePagination>
        </>
      )}
    </TablePage>
  )
}

function PageEmpty({
  icon,
  title,
  description,
  children,
}: {
  icon: React.ReactNode
  title: string
  description: string
  children?: React.ReactNode
}) {
  return (
    <div className="flex flex-1 p-6">
      <Empty>
        <EmptyHeader>
          <EmptyMedia variant="icon">{icon}</EmptyMedia>
          <EmptyTitle>{title}</EmptyTitle>
          <EmptyDescription>{description}</EmptyDescription>
        </EmptyHeader>
        {children && <EmptyContent>{children}</EmptyContent>}
      </Empty>
    </div>
  )
}

/** The whole-page loading state — nothing is known yet, not even the kind's
 * name: header, toolbar, rows, footer. */
function BrowseSkeleton() {
  return (
    <TablePage className="flex min-h-0 flex-1 flex-col px-0 py-0 md:px-0">
      <div className={cn("shrink-0 pt-6", GUTTER)}>
        <Skeleton className="h-7 w-40" />
        <Skeleton className="mt-2 h-3.5 w-72" />
      </div>
      <div
        className={cn(
          "mt-3.5 flex shrink-0 gap-2 border-b border-border py-2.5",
          GUTTER_MX
        )}
      >
        <Skeleton className="h-7 w-24" />
        <span className="flex-1" />
        <Skeleton className="h-7 w-60" />
      </div>
      <div className={cn("min-h-0 flex-1 overflow-hidden", GUTTER_MX)}>
        {Array.from({ length: 12 }, (_, i) => (
          <div key={i} className="flex h-[38px] items-center border-b">
            <Skeleton className="h-4 w-2/5" />
          </div>
        ))}
      </div>
    </TablePage>
  )
}
