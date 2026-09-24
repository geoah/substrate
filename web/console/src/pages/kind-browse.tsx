/** Kind browse (`/data/:authority/:package/:kind`): ONE schema-driven DataTable for
 * every kind ever installed. Server-side everything — the filter, the search,
 * the sort and the page live in the URL (nuqs) and travel to the wire as
 * `?filter=/orderBy=/offset=`. Pagination is NUMBERED: a page is `offset=`
 * on the wire (decision 0084), so every page is one request away and `?page=`
 * makes one linkable. The bounded count query sizes the bar as well as the
 * header, and Next reads the page's own cursor rather than that count, so a
 * collection past the count's ceiling still pages to its end.
 *
 * TOP TABS, the record page's idiom (owner ask, 2026-08-12): **Records** is the
 * collection, **Definition** is the kind that shapes it — its declaration YAML
 * and the properties it declares. The active tab lives in `?tab=` so it
 * is linkable, and both tabs read the ONE kinds query this page already makes.
 *
 * A TREE where the kind allows one: a single reference pinned at the kind
 * itself (a team's `parent` team) nests the collection. The page's rows are
 * then the records naming no parent, and each opens onto the records naming
 * it (`hooks/use-record-tree.ts`). `?nest=false` draws the same rows flat; a
 * filter or a search draws them flat regardless, so a match is shown wherever
 * it sits rather than hidden under a parent that does not match. */

import { useEffect, useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import type { SortingState, Updater } from "@tanstack/react-table"
import { InboxIcon, PlusIcon, SearchXIcon } from "lucide-react"
import {
  parseAsArrayOf,
  parseAsBoolean,
  parseAsInteger,
  parseAsString,
  parseAsStringLiteral,
  useQueryState,
} from "nuqs"

import { DataTable, useDataTable } from "@/components/data-table/data-table"
import { DataTablePagination } from "@/components/data-table/data-table-pagination"
import { DataTableFilters } from "@/components/data-table/data-table-filters"
import { RowTreeProvider } from "@/components/data-table/data-table-tree"
import { DataTableViewOptions } from "@/components/data-table/data-table-view-options"
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
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useRecordTree } from "@/hooks/use-record-tree"
import {
  recordsQueryOptions,
  recordCountQueryOptions,
  formatCount,
} from "@/lib/api/records"
import { kindsQueryOptions } from "@/lib/api/kinds"
import {
  decodeFilters,
  encodeFilter,
  loadBrowsePrefs,
  saveBrowsePrefs,
  toRecordFilter,
} from "@/lib/filters"
import {
  expandableReferences,
  filterableProperties,
  kindByCollection,
} from "@/lib/definition"
import { nestingProperty, rootsFilter } from "@/lib/record-tree"
import { titlesFromIncluded } from "@/lib/reference-titles"
import { cn } from "@/lib/utils"
import {
  buildColumns,
  columnIdOf,
  defaultHiddenColumns,
  sortPropertyOf,
} from "@/pages/kind-browse-columns"
import { kindBrowseRoute } from "@/router"

const PAGE_SIZE = 50
const DEFAULT_SORT = "updatedAt:desc"

/** The tab keys, in bar order; the records lead and are the default. */
const TABS = ["records", "definition"] as const
const tabParser = parseAsStringLiteral(TABS)
  .withDefault("records")
  .withOptions({ history: "push" })

function parseSort(sort: string): SortingState {
  const [property, dir] = sort.split(":")
  return property ? [{ id: columnIdOf(property), desc: dir !== "asc" }] : []
}

export function KindBrowsePage() {
  // The route params are the kind reference, segment for segment; `$name` (the
  // kind name) is the collection segment.
  const { authority, pkg, name } = kindBrowseRoute.useParams()
  const navigate = useNavigate()

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
  // not persisted either: where a reader had got to is not a view
  // preference, and restoring page 9 on a bare url would open a collection
  // at rows nobody asked for.
  const [pageParam, setPageParam] = useQueryState(
    "page",
    parseAsInteger.withDefault(1)
  )
  // The tree switch: in the URL beside the sort so a flat view is shareable,
  // and in the stored prefs so a reader who turned it off stays off. On by
  // default, and shown only on a kind that can nest at all.
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
    if (!params.has("filter") && stored.filter?.length) {
      void setFilterTokens(stored.filter, { history: "replace" })
    }
    if (!params.has("sort") && stored.sort) {
      void setSort(stored.sort, { history: "replace" })
    }
    if (!params.has("nest") && stored.nest === false) {
      void setNest(false, { history: "replace" })
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

  // The reference this kind nests by, where it declares one at itself.
  const nestProperty = useMemo(
    () => (kindInfo ? nestingProperty(kindInfo) : undefined),
    [kindInfo]
  )
  const hasFilters = filters.length > 0 || search.trim().length > 0
  // A filter or a search draws the table flat: a match nested under a parent
  // that does not match would otherwise be a row the reader cannot reach.
  const nesting = nestProperty !== undefined && nest && !hasFilters
  // What the page reads: the whole view, or only the records naming no parent.
  const listFilter = useMemo(
    () =>
      nesting && nestProperty
        ? rootsFilter(recordFilter, nestProperty.name)
        : recordFilter,
    [nesting, nestProperty, recordFilter]
  )

  // A hand-typed ?page= is clamped to a page that exists; the request below
  // is built from this, never from the raw parameter.
  const page = Number.isFinite(pageParam)
    ? Math.max(1, Math.trunc(pageParam))
    : 1
  // A changed filter, sort or tree switch renumbers the whole collection, so
  // the page a reader was on no longer names the same rows: the view resets to
  // page one.
  const viewKey = `${authority}/${pkg}/${name}|${JSON.stringify(recordFilter ?? null)}|${sort}|${nesting ? "tree" : "flat"}`
  const [lastViewKey, setLastViewKey] = useState(viewKey)
  if (lastViewKey !== viewKey) {
    setLastViewKey(viewKey)
    if (page !== 1) void setPageParam(null, { history: "replace" })
  }

  // Every reference this kind declares rides the page read as `expand=`, so
  // a reference column reads as the referent's NAME instead of the record id
  // the value carries (owner report, 2026-09-18: a task's `assignee`). It is
  // one sidecar on the read the table already makes, not a second request,
  // and a kind whose expansion the server refuses degrades to no expansion
  // rather than to no rows (`fetchRecordsPage`).
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

  // The page's own rows are the tree's roots; under the tree the table draws
  // each open root's children right after it.
  const roots = records.data?.records ?? []
  const tree = useRecordTree({
    kind: kindInfo,
    property: nesting ? nestProperty : undefined,
    roots,
    filter: recordFilter,
    orderBy: sort,
    expand,
  })
  const rows = tree.rows
  // Absent `included` — the expansion degraded, or the kind declares no
  // reference at all — this is empty and every pill reads as its id. The
  // tree's level reads expand the same properties, so their referents join.
  const referenceTitles = useMemo(
    () => titlesFromIncluded({ ...records.data?.included, ...tree.included }),
    [records.data, tree.included]
  )
  const pageCursor = records.data?.cursor
  // A single page with nothing behind it IS the exact count, for free. Any
  // larger collection pays the bounded count walk, which the numbered bar
  // needs as well as the header — but only for the NUMBERS: Next below reads
  // the page's own cursor, so a collection past the walk's ceiling still
  // pages to its end. Under the tree these count the top-level rows, which is
  // what the bar pages through.
  const derivedTotal =
    records.data && !pageCursor && page === 1 ? roots.length : undefined
  const count = useQuery({
    // Only once a second page is known to exist: a collection that fits on
    // one page has already answered its own size above, and the walk is a
    // second round trip over the same rows.
    ...recordCountQueryOptions(authority, pkg, name, listFilter),
    enabled: Boolean(kindInfo) && (page > 1 || Boolean(pageCursor)),
  })
  const total = derivedTotal ?? count.data?.value
  const totalCapped = derivedTotal === undefined && count.data?.capped
  // The header counts the collection, which under the tree is more than the
  // top-level rows: the same bounded walk over the view's own filter says.
  const collectionCount = useQuery({
    ...recordCountQueryOptions(authority, pkg, name, recordFilter),
    enabled: Boolean(kindInfo) && nesting,
  })
  const totalText = nesting
    ? collectionCount.data
      ? formatCount(collectionCount.data)
      : undefined
    : derivedTotal !== undefined
      ? derivedTotal.toLocaleString()
      : count.data
        ? formatCount(count.data)
        : undefined

  // A ?page= past the end — hand-typed, or bookmarked before rows were
  // deleted — answers an empty page that reads like an empty collection.
  // Land on the last page that has rows instead. A CAPPED count is a floor,
  // so it can never justify moving a reader back.
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

  const columns = useMemo(
    () =>
      kindInfo
        ? buildColumns(kindInfo, registry.data ?? [], referenceTitles)
        : [],
    [kindInfo, registry.data, referenceTitles]
  )
  // Only the OPENING set: a reader who has saved a column preference for this
  // kind keeps it, and the Columns menu turns any of these back on.
  const defaultHidden = useMemo(
    () => (kindInfo ? defaultHiddenColumns(kindInfo) : []),
    [kindInfo]
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
  })

  // Only the REGISTRY gates the whole page — it names the collection and it
  // is what the Definition tab reads. Records pending or failing is the
  // Records tab's business alone, so a collection whose rows won't load still
  // opens on its definition.
  if (registry.isPending) {
    return <BrowseSkeleton />
  }

  if (registry.isError) {
    return (
      <PageEmpty
        icon={<SearchXIcon />}
        title="Kinds didn't load"
        description="This page needs the list of kinds to name the collection."
      >
        <Button
          variant="outline"
          size="sm"
          onClick={() => void registry.refetch()}
        >
          Retry
        </Button>
      </PageEmpty>
    )
  }

  if (!kindInfo) {
    return (
      <PageEmpty
        icon={<SearchXIcon />}
        title="No such kind"
        description={`This repository has no kind called ${authority}/${pkg}/${name}.`}
      />
    )
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 items-start justify-between gap-3 px-6 pt-5 pb-3">
        <div className="min-w-0">
          <h1 className="text-2xl font-semibold tracking-tight">
            {kindInfo.name}
          </h1>
          <p className="text-xs text-muted-foreground">
            {totalText !== undefined ? `${totalText} records in ` : ""}
            <span className="data">
              {authority}/{pkg}
            </span>
          </p>
          {/* What the kind IS, from its declaration — the reader arriving at a
           * collection they did not install should not have to open the
           * definition tab to find out what lives in it. */}
          {kindInfo.description && (
            <p className="mt-1.5 max-w-prose text-sm text-muted-foreground">
              {kindInfo.description}
            </p>
          )}
        </div>
        <Button
          variant="outline"
          size="sm"
          className="shrink-0 gap-1.5"
          render={
            <Link
              to="/data/$authority/$pkg/$name/new"
              params={{ authority: authority, pkg: pkg, name: name }}
            />
          }
        >
          <PlusIcon className="size-3.5" />
          New
        </Button>
      </div>
      <Tabs
        value={tab}
        onValueChange={(next) => void setTab(next as (typeof TABS)[number])}
        className="min-h-0 flex-1 gap-0"
      >
        <TabsList variant="line" className="mx-4 shrink-0 justify-start">
          <TabsTrigger value="records">Records</TabsTrigger>
          <TabsTrigger value="definition">Definition</TabsTrigger>
        </TabsList>

        <TabsContent value="records" className="flex min-h-0 flex-col border-t">
          {records.isError ? (
            <PageEmpty
              icon={<SearchXIcon />}
              title={`${kindInfo.name} records didn't load`}
              description={records.error.message}
            >
              <Button
                variant="outline"
                size="sm"
                onClick={() => void records.refetch()}
              >
                Retry
              </Button>
            </PageEmpty>
          ) : records.isPending ? (
            <BrowseTableSkeleton />
          ) : (
            <>
              <div className="flex shrink-0 flex-wrap items-center gap-2 pr-6">
                <DataTableFilters
                  fields={filterFields}
                  filters={filters}
                  kinds={registry.data ?? []}
                  onChange={(next) => {
                    const tokens = next.map(encodeFilter)
                    void setFilterTokens(tokens.length ? tokens : null)
                    resetPages()
                    persist({ filter: tokens })
                  }}
                />
                <div className="ml-auto flex items-center gap-2 py-2.5 pl-2">
                  {nestProperty && (
                    <label
                      className={cn(
                        "flex h-8 shrink-0 items-center gap-2 rounded-lg border px-2.5 text-sm text-muted-foreground",
                        hasFilters
                          ? "opacity-50"
                          : "cursor-pointer hover:text-foreground"
                      )}
                      title={
                        hasFilters
                          ? "Flat while a filter or a search is set, so a match shows wherever it sits"
                          : `The rows are the ${kindInfo.name} records with no ${nestProperty.name}; each opens onto the ones naming it`
                      }
                    >
                      <input
                        type="checkbox"
                        className="accent-primary"
                        checked={nest}
                        disabled={hasFilters}
                        onChange={(e) => {
                          void setNest(e.target.checked)
                          resetPages()
                          persist({ nest: e.target.checked })
                        }}
                      />
                      Nest by <span className="data">{nestProperty.name}</span>
                    </label>
                  )}
                  {/* Words against every text this kind indexes (the wire's
                      `filter.search`), composed with the property filters and
                      the sort. */}
                  <SearchBox
                    className="w-64"
                    label="Search these records"
                    placeholder="Search these records…"
                    value={search}
                    onChange={(next) => {
                      void setSearch(next || null)
                      resetPages()
                    }}
                  />
                  <DataTableViewOptions table={table} />
                </div>
              </div>
              <div className="min-h-0 flex-1 overflow-auto">
                <RowTreeProvider
                  tree={
                    tree.active
                      ? { nodes: tree.nodes, toggle: tree.toggle }
                      : null
                  }
                >
                  <DataTable
                    table={table}
                    loading={records.isPlaceholderData && records.isFetching}
                    onRowClick={(row) =>
                      void navigate({
                        to: "/data/$authority/$pkg/$name/$id",
                        params: {
                          authority: authority,
                          pkg: pkg,
                          name,
                          id: row.id,
                        },
                      })
                    }
                    empty={
                      <Empty className="py-16">
                        <EmptyHeader>
                          <EmptyMedia variant="icon">
                            <InboxIcon />
                          </EmptyMedia>
                          <EmptyTitle>
                            {hasFilters
                              ? "Nothing matches"
                              : `No ${kindInfo.name} records yet`}
                          </EmptyTitle>
                          <EmptyDescription>
                            {hasFilters
                              ? "No record matches the filters and search you set."
                              : "Press New to create the first one."}
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
                    }
                  />
                </RowTreeProvider>
              </div>
              <DataTablePagination
                page={page}
                pageSize={PAGE_SIZE}
                rows={roots.length}
                total={total}
                totalCapped={totalCapped}
                hasNext={Boolean(pageCursor)}
                onPage={goToPage}
                loading={records.isPlaceholderData && records.isFetching}
              />
            </>
          )}
        </TabsContent>

        <TabsContent value="definition" className="min-h-0 border-t">
          <ScrollArea className="h-full">
            <KindDefinition kind={kindInfo} kinds={registry.data ?? []} />
          </ScrollArea>
        </TabsContent>
      </Tabs>
    </div>
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
 * name: header block, tab bar, filter row, table rows, pagination seam. */
function BrowseSkeleton() {
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="shrink-0 px-6 pt-5 pb-3">
        <Skeleton className="h-6 w-32" />
        <Skeleton className="mt-1.5 h-3.5 w-48" />
      </div>
      <div className="flex shrink-0 gap-2 px-4 pb-3">
        <Skeleton className="h-7 w-20" />
        <Skeleton className="h-7 w-24" />
      </div>
      <div className="flex min-h-0 flex-1 flex-col border-t">
        <BrowseTableSkeleton />
      </div>
    </div>
  )
}

/** The Records tab's own loading state: the collection is named and the tabs
 * are live — only the page of rows is still on the wire. */
function BrowseTableSkeleton() {
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 items-center gap-2 px-6 py-2.5">
        <Skeleton className="h-8 w-24" />
      </div>
      <div className="min-h-0 flex-1 space-y-0 overflow-hidden px-6">
        {Array.from({ length: 12 }, (_, i) => (
          <div key={i} className="flex h-9 items-center border-b last:border-0">
            <Skeleton className="h-4 w-2/5" />
          </div>
        ))}
      </div>
      <div className="flex shrink-0 items-center justify-between border-t px-6 py-2">
        <Skeleton className="h-4 w-24" />
        <Skeleton className="h-4 w-40" />
      </div>
    </div>
  )
}
