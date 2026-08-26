/** Agents (`/agents`): the declared agents — a callable whose body is an LLM
 * loop — on THE table system, off the record surface. A row opens the chat
 * surface; the manifest opens the record page, where the prompt, tools and
 * budgets live and edit.
 *
 * The llmprovider rows are NOT here. They are ordinary records of a core kind,
 * they are not agents, and a table of them on this page implied a relationship
 * the page does not have — an agent names a provider by id, and that pointer
 * reads on the agent's own record. Data → llmproviders is where they live. */

import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"
import { useNavigate } from "@tanstack/react-router"
import { Link } from "@tanstack/react-router"
import type { DataTableColumn } from "@/components/data-table/data-table"
import { BotIcon, SearchXIcon } from "lucide-react"

import { DataTable, useDataTable } from "@/components/data-table/data-table"
import { DataTableColumnHeader } from "@/components/data-table/data-table-column-header"
import { DataTableViewOptions } from "@/components/data-table/data-table-view-options"
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
import { agentsQueryOptions } from "@/lib/api/agents"
import type { SubstrateRecord } from "@/lib/api/types"
import { cellValue, recordTitle } from "@/lib/format"
import { cn } from "@/lib/utils"

/** A declaration row's properties ARE the declaration: `provider` and `model`
 * are columns of the row, not keys inside a blob. */
function declaredField(agent: SubstrateRecord, key: string): string {
  const value = agent.properties[key]
  return value === undefined ? "" : cellValue(value)
}

/** The flat columns of both tables are one shape: a string off the record,
 * truncated, in the muted voice. The cell renders the accessor's value — a cell
 * that recomputed it could disagree with what the column sorts and filters on. */
function textColumn(opts: {
  id: string
  title: string
  value: (record: SubstrateRecord) => string
  meta: DataTableColumn<SubstrateRecord>["meta"]
  /** The data voice, for a value the substrate itself carries. */
  data?: boolean
  /** A title attribute, for a value the column is likely to truncate away. */
  tooltip?: boolean
}): DataTableColumn<SubstrateRecord> {
  return {
    id: opts.id,
    accessorFn: opts.value,
    enableSorting: false,
    header: ({ column }) => (
      <DataTableColumnHeader column={column} title={opts.title} />
    ),
    cell: ({ getValue }) => {
      const text = getValue<string>()
      return (
        <span
          className={cn(
            "block truncate text-muted-foreground",
            opts.data && "data"
          )}
          title={opts.tooltip ? text : undefined}
        >
          {text || "—"}
        </span>
      )
    },
    meta: opts.meta,
  }
}

function agentColumns(): DataTableColumn<SubstrateRecord>[] {
  return [
    {
      id: "agent",
      // The declaration has no `name` property (its local name is the id's
      // last segment, which the title renders); the id is the honest fallback,
      // and it survives as the tooltip.
      accessorFn: (a) => recordTitle(a.properties) || a.id,
      enableSorting: false,
      enableHiding: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="agent" />
      ),
      cell: ({ row }) => (
        <div className="min-w-0">
          <div className="truncate font-medium" title={row.original.id}>
            {recordTitle(row.original.properties) || row.original.id}
          </div>
          <div
            className="truncate text-xs text-muted-foreground"
            title={declaredField(row.original, "description")}
          >
            {declaredField(row.original, "description")}
          </div>
        </div>
      ),
      meta: { label: "agent", size: { min: 200, max: 400, weight: 1.5 } },
    },
    textColumn({
      id: "provider",
      title: "provider",
      value: (a) => declaredField(a, "provider"),
      data: true,
      meta: { label: "provider", width: 120 },
    }),
    textColumn({
      id: "model",
      title: "model",
      value: (a) => declaredField(a, "model"),
      data: true,
      tooltip: true,
      meta: { label: "model", size: { min: 140, weight: 1 } },
    }),
    {
      id: "chat",
      accessorFn: () => "",
      enableSorting: false,
      enableHiding: false,
      header: () => null,
      cell: ({ row }) => (
        <div className="flex justify-end">
          <Button
            variant="outline"
            size="sm"
            className="h-7"
            render={
              <Link to="/agents/$id" params={{ id: row.original.id }}>
                Chat
              </Link>
            }
            onClick={(e) => e.stopPropagation()}
          />
        </div>
      ),
      meta: { label: "chat", width: 90, cellClassName: "text-right" },
    },
  ]
}

export function AgentsPage() {
  const navigate = useNavigate()
  const agents = useQuery(agentsQueryOptions())

  // This page is the CHAT list, so a subagent-only agent (an llm-as-judge,
  // callable only by other agents) stays off it; the row is still an ordinary
  // record under Data → agents, and still selectable as another agent's
  // sub-agent. The chat API refuses such an agent too, so this filter hides
  // nothing that would have worked.
  const allRows = useMemo(() => agents.data?.records ?? [], [agents.data])
  const agentRows = useMemo(
    () => allRows.filter((r) => r.properties.hiddenFromChat !== true),
    [allRows]
  )
  const hiddenFromChatCount = allRows.length - agentRows.length
  const aCols = useMemo(() => agentColumns(), [])
  const aTable = useDataTable({
    columns: aCols,
    data: agentRows,
    getRowId: (r) => r.id,
    prefsKey: "agents",
  })

  if (agents.isPending) return <AgentsSkeleton />

  if (agents.isError) {
    return (
      <div className="flex flex-1 p-6">
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <SearchXIcon />
            </EmptyMedia>
            <EmptyTitle>The agents didn't load</EmptyTitle>
            <EmptyDescription>{agents.error.message}</EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button
              variant="outline"
              size="sm"
              onClick={() => void agents.refetch()}
            >
              Retry
            </Button>
          </EmptyContent>
        </Empty>
      </div>
    )
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-auto">
      <div className="shrink-0 px-6 pt-5 pb-2">
        <h1 className="text-lg font-semibold">Agents</h1>
        <p className="text-xs text-muted-foreground">
          {agentRows.length.toLocaleString()} declared, from{" "}
          <span className="data">core.substrate.reamde.dev/agents</span>
          {hiddenFromChatCount > 0 &&
            ` (${hiddenFromChatCount.toLocaleString()} more hidden from chat, under Data)`}
        </p>
      </div>

      <section className="flex flex-col">
        <div className="flex justify-end px-6">
          <DataTableViewOptions table={aTable} />
        </div>
        <DataTable
          table={aTable}
          onRowClick={(row) =>
            void navigate({ to: "/agents/$id", params: { id: row.id } })
          }
          empty={
            <Empty className="py-12">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <BotIcon />
                </EmptyMedia>
                <EmptyTitle>No agents</EmptyTitle>
                <EmptyDescription>
                  No agents are declared on this substrate yet.
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          }
        />
      </section>
    </div>
  )
}

function AgentsSkeleton() {
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="shrink-0 px-6 pt-5 pb-1">
        <Skeleton className="h-6 w-24" />
        <Skeleton className="mt-1.5 h-3.5 w-48" />
      </div>
      <div className="px-6 pt-4">
        {Array.from({ length: 4 }, (_, i) => (
          <div
            key={i}
            className="flex h-12 items-center gap-6 border-b last:border-0"
          >
            <Skeleton className="h-4 w-48" />
            <Skeleton className="ml-auto h-7 w-16" />
          </div>
        ))}
      </div>
    </div>
  )
}
