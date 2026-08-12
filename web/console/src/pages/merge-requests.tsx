/** Merge requests (`/merge-requests`): "should I agree these are
 * duplicates?" — the dedupe queue on THE table system (owner ruling
 * 2026-08-06; the evidence-card list is gone): one row per request — pair,
 * evidence, score, age, decision — expandable in place to the matcher's case,
 * clicking through to the detail page for the verdict. The decision facet rides
 * the shared filter toolbar and defaults to the pending pile; resolved
 * requests stay queryable. Filters live in the URL (nuqs); pagination is
 * keyset (a cursor stack walks Prev/Next, no offset) like every collection. */

import { useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import type { ColumnDef } from "@tanstack/react-table"
import { ArrowUpRightIcon, GitMergeIcon, SearchXIcon } from "lucide-react"
import { parseAsArrayOf, parseAsString, useQueryState } from "nuqs"

import { timeColumn } from "@/components/data-table/columns"
import { DataTable, useDataTable } from "@/components/data-table/data-table"
import { DataTableColumnHeader } from "@/components/data-table/data-table-column-header"
import { DataTableCursorPagination } from "@/components/data-table/data-table-cursor-pagination"
import { DataTableFilters } from "@/components/data-table/data-table-filters"
import { DataTableViewOptions } from "@/components/data-table/data-table-view-options"
import { RowDetail } from "@/components/data-table/row-detail"
import { RecordPeek } from "@/components/record-peek"
import { StateBadge } from "@/components/state-badge"
import { Badge } from "@/components/ui/badge"
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
import { formatCount } from "@/lib/api/records"
import {
  mergeRequestCountQueryOptions,
  mergeRequestsQueryOptions,
  pendingMergeCountQueryOptions,
} from "@/lib/api/mergerequests"
import { kindsQueryOptions } from "@/lib/api/kinds"
import type { EdgeTarget, SubstrateRecord, KindInfo } from "@/lib/api/types"
import { decodeFilters, encodeFilter, type ActiveFilter } from "@/lib/filters"
import {
  DECISION_INITIAL,
  DECISIONS,
  decisionOf,
  evidenceScore,
  evidenceSignals,
  signalText,
  type Decision,
} from "@/lib/mergerequests"
import { splitKind } from "@/lib/definition"
import type { DeclaredProperty } from "@/lib/definition"
import { cn } from "@/lib/utils"

const PAGE_SIZE = 20

/** The queue's facet vocabulary: the decision state machine, offered through
 * the same filter toolbar as every other table. */
const MR_FILTER_FIELDS: DeclaredProperty[] = [
  {
    name: "decision",
    kind: "state",
    repeated: false,
    states: [...DECISIONS],
    description: "The request's decision",
  },
]

/** The default view is the pending pile; "every decision" is said explicitly
 * (an EMPTY param would round-trip back to the default). */
const DEFAULT_FILTER_TOKENS = [
  encodeFilter({ field: "decision", op: "eq", value: "proposed" }),
]
const ALL_DECISIONS_TOKENS = [
  encodeFilter({ field: "decision", op: "eq", value: DECISIONS.join(",") }),
]

/** Old links said `state`; the wire (and the console) say `decision` now.
 * Reading the alias keeps shared URLs working — writes re-encode as
 * `decision`. */
function normalizeMergeFilters(filters: ActiveFilter[]): ActiveFilter[] {
  return filters.map((f) =>
    f.field === "state" ? { ...f, field: "decision" } : f
  )
}

function decisionsOf(filters: ActiveFilter[]): Decision[] {
  return filters
    .filter((f) => f.field === "decision")
    .flatMap((f) => f.value.split(","))
    .map((s) => s.trim())
    .filter((s): s is Decision => (DECISIONS as readonly string[]).includes(s))
}

/** The pair, direction first: who merges away → who survives. Each side
 * peeks and links like every edge cell in the console. */
export function MergePair({
  loser,
  winner,
  types,
}: {
  loser?: EdgeTarget
  winner?: EdgeTarget
  types: KindInfo[]
}) {
  return (
    <span className="inline-flex min-w-0 flex-wrap items-baseline gap-x-1.5 gap-y-0.5">
      <PairSide target={loser} types={types} />
      <span className="text-muted-foreground" aria-hidden>
        →
      </span>
      <PairSide target={winner} types={types} />
    </span>
  )
}

function PairSide({
  target,
  types,
}: {
  target?: EdgeTarget
  types: KindInfo[]
}) {
  if (!target) return <span className="text-muted-foreground">unknown</span>
  return (
    <span className="inline-flex min-w-0 items-baseline gap-1.5">
      <span className="min-w-0 truncate font-medium">
        <RecordPeek target={target} types={types} />
      </span>
      <span className="data text-xs text-muted-foreground">
        {splitKind(target.kind).name}
      </span>
    </span>
  )
}

/** The matcher's evidence as honest chips off the real fields. Evidence the
 * console cannot read as signals shows nothing rather than guessing. */
export function EvidenceChips({ mr }: { mr: SubstrateRecord }) {
  const signals = evidenceSignals(mr.properties.evidence)
  if (!signals.length) return null
  return (
    <span className="flex flex-wrap items-center gap-1.5">
      {signals.map((s, i) => (
        <Badge
          key={`${s.kind}-${i}`}
          variant="outline"
          className="data font-normal text-muted-foreground"
        >
          {signalText(s)}
        </Badge>
      ))}
    </span>
  )
}

/** The expanded row: the matcher's whole case, in place — rationale,
 * every signal, the door to the verdict. */
function RequestDetail({ mr }: { mr: SubstrateRecord }) {
  const rationale =
    typeof mr.properties.rationale === "string"
      ? mr.properties.rationale
      : undefined
  return (
    <RowDetail>
      {rationale && <p>{rationale}</p>}
      <EvidenceChips mr={mr} />
      <span className="flex items-center gap-3 text-muted-foreground">
        <span className="data">{mr.id}</span>
        <Link
          to="/merge-requests/$id"
          params={{ id: mr.id }}
          className="inline-flex items-center gap-0.5 underline-offset-4 hover:underline"
          onClick={(e) => e.stopPropagation()}
        >
          Open request
          <ArrowUpRightIcon className="size-3" />
        </Link>
      </span>
    </RowDetail>
  )
}

function buildColumns(types: KindInfo[]): ColumnDef<SubstrateRecord, unknown>[] {
  return [
    {
      id: "pair",
      accessorFn: (mr) => mr.id,
      enableSorting: false,
      enableHiding: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="pair" />
      ),
      cell: ({ row }) => (
        <MergePair
          loser={row.original.edges?.loser?.[0]}
          winner={row.original.edges?.winner?.[0]}
          types={types}
        />
      ),
      // the queue's identity column: the biggest share, capped.
      meta: { label: "pair", size: { min: 260, max: 560, weight: 2 } },
    },
    {
      id: "evidence",
      accessorFn: (mr) => evidenceSignals(mr.properties.evidence).length,
      enableSorting: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="evidence" />
      ),
      cell: ({ row }) => {
        const signals = evidenceSignals(row.original.properties.evidence)
        if (!signals.length)
          return <span className="text-muted-foreground">—</span>
        return <EvidenceChips mr={row.original} />
      },
      meta: { label: "evidence", size: { min: 220, max: 340, weight: 1 } },
    },
    {
      id: "score",
      accessorFn: (mr) => evidenceScore(mr.properties.evidence),
      enableSorting: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="score" align="right" />
      ),
      cell: ({ row }) => {
        const score = evidenceScore(row.original.properties.evidence)
        return score !== undefined ? (
          <span className="block text-right data text-muted-foreground">
            {score.toFixed(2)}
          </span>
        ) : (
          <span className="block text-right text-muted-foreground/50">—</span>
        )
      },
      meta: {
        label: "score",
        width: 70,
        headerClassName: "text-right",
        cellClassName: "text-right",
      },
    },
    timeColumn<SubstrateRecord>({
      id: "age",
      title: "age",
      iso: (mr) => mr.createdAt,
      voice: "relative",
      width: 90,
    }),
    {
      id: "decision",
      accessorFn: (mr) => decisionOf(mr),
      enableSorting: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="decision" />
      ),
      cell: ({ row }) => (
        <StateBadge
          value={decisionOf(row.original)}
          initial={DECISION_INITIAL}
        />
      ),
      meta: { label: "decision", width: 110 },
    },
    // decided-by lives on the detail page: the list read carries no
    // propertyMeta (wire shape), so the queue speaks the decided instant.
    timeColumn<SubstrateRecord>({
      id: "decided",
      title: "decided",
      iso: (mr) =>
        typeof mr.properties.decidedAt === "string"
          ? mr.properties.decidedAt
          : undefined,
      voice: "relative",
      width: 100,
    }),
  ]
}

export function MergeRequestsPage() {
  const navigate = useNavigate()
  const [filterTokens, setFilterTokens] = useQueryState(
    "filter",
    parseAsArrayOf(parseAsString).withDefault(DEFAULT_FILTER_TOKENS)
  )
  const filters = useMemo(
    () => normalizeMergeFilters(decodeFilters(filterTokens)),
    [filterTokens]
  )
  const decisions = useMemo(() => decisionsOf(filters), [filters])

  // Keyset pagination: a stack of the opaque `after` cursors visited (index
  // 0 = page one). No offset, so Next walks the server cursor and Prev pops.
  const [cursorStack, setCursorStack] = useState<(string | undefined)[]>([
    undefined,
  ])
  const [pageIndex, setPageIndex] = useState(0)
  const viewKey = JSON.stringify(decisions)
  const [lastViewKey, setLastViewKey] = useState(viewKey)
  if (lastViewKey !== viewKey) {
    setLastViewKey(viewKey)
    setCursorStack([undefined])
    setPageIndex(0)
  }
  function resetPages() {
    setCursorStack([undefined])
    setPageIndex(0)
  }

  const registry = useQuery(kindsQueryOptions)
  const requests = useQuery(
    mergeRequestsQueryOptions({
      decision: decisions,
      first: PAGE_SIZE,
      after: cursorStack[pageIndex],
    })
  )
  const pending = useQuery(pendingMergeCountQueryOptions())

  const rows = useMemo(() => requests.data?.records ?? [], [requests.data])
  const pageCursor = requests.data?.cursor
  const hasNext = pageIndex < cursorStack.length - 1 || Boolean(pageCursor)
  function nextPage() {
    if (pageIndex < cursorStack.length - 1) {
      setPageIndex(pageIndex + 1)
    } else if (pageCursor) {
      setCursorStack([...cursorStack, pageCursor])
      setPageIndex(pageIndex + 1)
    }
  }
  // A single cursorless page IS the exact count; a longer list pays the walk.
  const derivedTotal =
    requests.data && !pageCursor && pageIndex === 0 ? rows.length : undefined
  const count = useQuery({
    ...mergeRequestCountQueryOptions(decisions),
    enabled: Boolean(pageCursor),
  })
  const totalText =
    derivedTotal !== undefined
      ? derivedTotal.toLocaleString()
      : count.data
        ? formatCount(count.data)
        : undefined

  const types = useMemo(() => registry.data ?? [], [registry.data])
  const columns = useMemo(() => buildColumns(types), [types])
  const table = useDataTable({
    columns,
    data: rows,
    getRowId: (row) => row.id,
    prefsKey: "merge-requests",
  })

  if (requests.isError) {
    return (
      <div className="flex flex-1 p-6">
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <SearchXIcon />
            </EmptyMedia>
            <EmptyTitle>The merge requests didn't load</EmptyTitle>
            <EmptyDescription>{requests.error.message}</EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button
              variant="outline"
              size="sm"
              onClick={() => void requests.refetch()}
            >
              Retry
            </Button>
          </EmptyContent>
        </Empty>
      </div>
    )
  }

  if (registry.isPending || requests.isPending) return <QueueSkeleton />

  const filtered = decisions.length > 0 && decisions.length < DECISIONS.length

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="shrink-0 px-6 pt-5 pb-1">
        <h1 className="text-lg font-semibold">Merge requests</h1>
        <p className="text-xs text-muted-foreground">
          {pending.data !== undefined
            ? `${formatCount(pending.data)} pending, from `
            : ""}
          <span className="data">core.substrate.reamde.dev/recordmergerequests</span>
        </p>
      </div>
      <div className="flex shrink-0 flex-wrap items-center gap-2 pr-6">
        <DataTableFilters
          fields={MR_FILTER_FIELDS}
          filters={filters}
          onChange={(next) => {
            void setFilterTokens(
              next.length ? next.map(encodeFilter) : ALL_DECISIONS_TOKENS
            )
            resetPages()
          }}
        />
        <div className="ml-auto py-2.5 pl-2">
          <DataTableViewOptions table={table} />
        </div>
      </div>
      <div
        className={cn(
          "min-h-0 flex-1 overflow-auto",
          requests.isPlaceholderData && requests.isFetching && "opacity-60"
        )}
      >
        <DataTable
          table={table}
          loading={requests.isPending}
          onRowClick={(row) =>
            void navigate({ to: "/merge-requests/$id", params: { id: row.id } })
          }
          renderExpanded={(row) => <RequestDetail mr={row} />}
          empty={
            <Empty className="py-16">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <GitMergeIcon />
                </EmptyMedia>
                <EmptyTitle>
                  {filtered
                    ? `No ${decisions.join("/")} requests`
                    : "No merge requests"}
                </EmptyTitle>
                <EmptyDescription>
                  {!filtered || decisions.includes("proposed")
                    ? "When the dedupe matcher finds two records that look like one, the suggestion lands here."
                    : "Nothing has been decided this way yet."}
                </EmptyDescription>
              </EmptyHeader>
              {filtered && (
                <EmptyContent>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => {
                      void setFilterTokens(ALL_DECISIONS_TOKENS)
                      resetPages()
                    }}
                  >
                    Show every decision
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
        loading={requests.isFetching}
        summary={
          totalText !== undefined ? `${totalText} in total` : undefined
        }
      />
    </div>
  )
}

/** Mirrors the final layout: header, filter row, table rows, pagination. */
function QueueSkeleton() {
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="shrink-0 px-6 pt-5 pb-1">
        <Skeleton className="h-6 w-36" />
        <Skeleton className="mt-1.5 h-3.5 w-56" />
      </div>
      <div className="flex shrink-0 items-center gap-1.5 px-6 py-2.5">
        <Skeleton className="h-8 w-32" />
        <Skeleton className="ml-auto h-8 w-24" />
      </div>
      <div className="flex flex-col px-6">
        {Array.from({ length: 6 }, (_, i) => (
          <div
            key={i}
            className="flex h-10 items-center border-b last:border-0"
          >
            <Skeleton className="h-4 w-2/3" />
          </div>
        ))}
      </div>
    </div>
  )
}
