/** Kind browse (`/data/:authority/:plural`): ONE schema-driven DataTable for
 * every kind ever installed. Server-side everything — the filter and sort live
 * in the URL (nuqs) and travel to the wire as `?filter=/orderBy`. Pagination is
 * keyset: there is no offset and no page-jump, so a
 * cursor stack walks Prev/Next over the opaque `after` tokens the server
 * returns. A bounded count query backs the header total only.
 *
 * TOP TABS, the record page's idiom (owner ask, 2026-08-12): **Records** is the
 * collection, **Definition** is the kind that shapes it — its declaration YAML
 * and the properties/edges it declares. The active tab lives in `?tab=` so it
 * is linkable, and both tabs read the ONE kinds query this page already makes. */

import { useEffect, useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import type { SortingState, Updater } from "@tanstack/react-table"
import { InboxIcon, PlusIcon, SearchXIcon } from "lucide-react"
import {
  parseAsArrayOf,
  parseAsString,
  parseAsStringLiteral,
  useQueryState,
} from "nuqs"

import { DataTable, useDataTable } from "@/components/data-table/data-table"
import { DataTableCursorPagination } from "@/components/data-table/data-table-cursor-pagination"
import { DataTableFilters } from "@/components/data-table/data-table-filters"
import { DataTableViewOptions } from "@/components/data-table/data-table-view-options"
import { KindDefinition } from "@/components/record/definition"
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
  declaredEdges,
  filterableProperties,
  kindByCollection,
} from "@/lib/definition"
import {
  buildColumns,
  columnIdOf,
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
  // The route path still spells `$authority/$plural`; a collection is addressed by
  // its (authority, plural) pair, so read them under their v1 names.
  const { authority, name: plural } = kindBrowseRoute.useParams()
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

  // A BARE url restores the last-used view from localStorage, one dimension
  // at a time; an explicit ?filter=/?sort= always wins (shareable views stay
  // exact). Writes happen in the change handlers below, never here — an
  // effect writing on mount would wipe the store before this restore ran.
  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    const stored = loadBrowsePrefs(authority, plural)
    if (!stored) return
    if (!params.has("filter") && stored.filter?.length) {
      void setFilterTokens(stored.filter, { history: "replace" })
    }
    if (!params.has("sort") && stored.sort) {
      void setSort(stored.sort, { history: "replace" })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- once per collection
  }, [authority, plural])

  /** Write-through: the store always mirrors the view the handlers just set. */
  function persist(next: { filter?: string[]; sort?: string }) {
    saveBrowsePrefs(authority, plural, {
      filter: next.filter ?? filterTokens,
      sort:
        (next.sort ?? sort) === DEFAULT_SORT ? undefined : (next.sort ?? sort),
    })
  }

  const registry = useQuery(kindsQueryOptions)
  const kindInfo = registry.data
    ? kindByCollection(registry.data, authority, plural)
    : undefined

  const filters = useMemo(() => decodeFilters(filterTokens), [filterTokens])
  const filterFields = useMemo(
    () => (kindInfo ? filterableProperties(kindInfo) : []),
    [kindInfo]
  )
  const recordFilter = useMemo(
    () => toRecordFilter(filters, filterFields),
    [filters, filterFields]
  )

  // Keyset pagination: a stack of the opaque `after` cursors visited, one per
  // page (index 0 = page one, no cursor). There is no offset, so Next walks
  // the server cursor forward and Prev pops back. A changed view resets it.
  const [cursorStack, setCursorStack] = useState<(string | undefined)[]>([
    undefined,
  ])
  const [pageIndex, setPageIndex] = useState(0)
  const viewKey = `${authority}/${plural}|${JSON.stringify(recordFilter ?? null)}|${sort}`
  const [lastViewKey, setLastViewKey] = useState(viewKey)
  if (lastViewKey !== viewKey) {
    setLastViewKey(viewKey)
    setCursorStack([undefined])
    setPageIndex(0)
  }

  const listOptions = recordsQueryOptions({
    authority,
    name: plural,
    first: PAGE_SIZE,
    after: cursorStack[pageIndex],
    filter: recordFilter,
    orderBy: sort,
    withEdges: kindInfo ? declaredEdges(kindInfo).length > 0 : false,
  })
  const records = useQuery({ ...listOptions, enabled: Boolean(kindInfo) })

  const rows = records.data?.records ?? []
  const pageCursor = records.data?.cursor
  // A single cursorless page IS the exact count, for free. A larger collection
  // pays the bounded count walk (header total only — navigation never needs it).
  const derivedTotal =
    records.data && !pageCursor && pageIndex === 0 ? rows.length : undefined
  const count = useQuery({
    ...recordCountQueryOptions(authority, plural, recordFilter),
    enabled: Boolean(kindInfo) && Boolean(pageCursor),
  })
  const totalText =
    derivedTotal !== undefined
      ? derivedTotal.toLocaleString()
      : count.data
        ? formatCount(count.data)
        : undefined

  const hasNext = pageIndex < cursorStack.length - 1 || Boolean(pageCursor)
  function nextPage() {
    if (pageIndex < cursorStack.length - 1) {
      setPageIndex(pageIndex + 1)
    } else if (pageCursor) {
      setCursorStack([...cursorStack, pageCursor])
      setPageIndex(pageIndex + 1)
    }
  }
  function resetPages() {
    setCursorStack([undefined])
    setPageIndex(0)
  }

  const columns = useMemo(
    () =>
      kindInfo && registry.data ? buildColumns(kindInfo, registry.data) : [],
    [kindInfo, registry.data]
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
    prefsKey: `browse:${authority}/${plural}`,
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
        title="The registry didn't load"
        description="The kind registry is what names this collection."
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
        title="Unknown collection"
        description={`${authority}/${plural} is not in the kind registry.`}
      />
    )
  }

  const hasFilters = filters.length > 0

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 items-start justify-between gap-3 px-6 pt-5 pb-3">
        <div className="min-w-0">
          <h1 className="text-lg font-semibold">{kindInfo.name}</h1>
          <p className="text-xs text-muted-foreground">
            {totalText !== undefined ? `${totalText} records in ` : ""}
            <span className="data">{authority}</span>
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
              to="/data/$authority/$name/new"
              params={{ authority: authority, name: plural }}
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
              title={`${kindInfo.plural} didn't load`}
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
                  onChange={(next) => {
                    const tokens = next.map(encodeFilter)
                    void setFilterTokens(tokens.length ? tokens : null)
                    resetPages()
                    persist({ filter: tokens })
                  }}
                />
                <div className="ml-auto py-2.5 pl-2">
                  <DataTableViewOptions table={table} />
                </div>
              </div>
              <div className="min-h-0 flex-1 overflow-auto">
                <DataTable
                  table={table}
                  loading={records.isPlaceholderData && records.isFetching}
                  onRowClick={(row) =>
                    void navigate({
                      to: "/data/$authority/$name/$id",
                      params: {
                        authority: authority,
                        name: plural,
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
                            : `No ${kindInfo.plural} yet`}
                        </EmptyTitle>
                        <EmptyDescription>
                          {hasFilters
                            ? "No record satisfies the active filters."
                            : "Nothing has written to this collection."}
                        </EmptyDescription>
                      </EmptyHeader>
                      {hasFilters && (
                        <EmptyContent>
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => {
                              void setFilterTokens(null)
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
              </div>
              <DataTableCursorPagination
                page={pageIndex + 1}
                rows={rows.length}
                hasPrev={pageIndex > 0}
                hasNext={hasNext}
                onPrev={() => setPageIndex(Math.max(0, pageIndex - 1))}
                onNext={nextPage}
                loading={records.isFetching}
                summary={
                  totalText !== undefined ? `${totalText} in total` : undefined
                }
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
