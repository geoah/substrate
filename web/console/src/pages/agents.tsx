/** Agents (`/agents`): the declared agents (a callable whose body is an LLM
 * loop) and the llm connection rows they resolve (cheap/mid/strong). Both on
 * THE table system, off the record surface. An agent row opens the chat
 * surface; an llm row and an agent's manifest open their record page (where
 * the prompt/tools/budgets live and edit). */

import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"
import { useNavigate } from "@tanstack/react-router"
import { Link } from "@tanstack/react-router"
import type { ColumnDef } from "@tanstack/react-table"
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
import { agentsQueryOptions, llmsQueryOptions } from "@/lib/api/agents"
import type { SubstrateRecord } from "@/lib/api/types"
import { cellValue } from "@/lib/format"

function definitionField(agent: SubstrateRecord, key: string): string {
  const def = agent.properties.definition
  if (typeof def !== "object" || def === null) return ""
  const value = (def as Record<string, unknown>)[key]
  return value === undefined ? "" : cellValue(value)
}

function agentColumns(): ColumnDef<SubstrateRecord, unknown>[] {
  return [
    {
      id: "agent",
      accessorFn: (a) => a.properties.name ?? a.id,
      enableSorting: false,
      enableHiding: false,
      header: ({ column }) => <DataTableColumnHeader column={column} title="agent" />,
      cell: ({ row }) => (
        <div className="min-w-0">
          <div className="truncate font-medium">
            {cellValue(row.original.properties.name) || row.original.id}
          </div>
          <div className="truncate data text-xs text-muted-foreground" title={row.original.id}>
            {row.original.id}
          </div>
        </div>
      ),
      meta: { label: "agent", size: { min: 200, max: 400, weight: 1.5 } },
    },
    {
      id: "llm",
      accessorFn: (a) => definitionField(a, "llm"),
      enableSorting: false,
      header: ({ column }) => <DataTableColumnHeader column={column} title="llm" />,
      cell: ({ row }) => (
        <span className="block truncate data text-muted-foreground">
          {definitionField(row.original, "llm") || "—"}
        </span>
      ),
      meta: { label: "llm", width: 120 },
    },
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

function llmColumns(): ColumnDef<SubstrateRecord, unknown>[] {
  const text = (key: string, title: string, width?: number): ColumnDef<SubstrateRecord, unknown> => ({
    id: key,
    accessorFn: (e) => e.properties[key],
    enableSorting: false,
    header: ({ column }) => <DataTableColumnHeader column={column} title={title} />,
    cell: ({ row }) => (
      <span className="block truncate data text-muted-foreground" title={cellValue(row.original.properties[key])}>
        {cellValue(row.original.properties[key]) || "—"}
      </span>
    ),
    meta: width ? { label: title, width } : { label: title, size: { min: 140, weight: 1 } },
  })
  return [
    {
      id: "row",
      accessorFn: (e) => e.id,
      enableSorting: false,
      enableHiding: false,
      header: ({ column }) => <DataTableColumnHeader column={column} title="row" />,
      cell: ({ row }) => (
        <div className="min-w-0">
          <div className="truncate font-medium">{row.original.id}</div>
          <div className="truncate data text-xs text-muted-foreground">
            {cellValue(row.original.properties.name)}
          </div>
        </div>
      ),
      meta: { label: "row", size: { min: 140, max: 240, weight: 1 } },
    },
    text("provider", "provider", 120),
    text("model", "model"),
    text("baseURL", "baseURL"),
  ]
}

export function AgentsPage() {
  const navigate = useNavigate()
  const agents = useQuery(agentsQueryOptions())
  const llms = useQuery(llmsQueryOptions())

  const agentRows = useMemo(() => agents.data?.records ?? [], [agents.data])
  const llmRows = useMemo(() => llms.data?.records ?? [], [llms.data])
  const aCols = useMemo(() => agentColumns(), [])
  const lCols = useMemo(() => llmColumns(), [])
  const aTable = useDataTable({
    columns: aCols,
    data: agentRows,
    getRowId: (r) => r.id,
    prefsKey: "agents",
  })
  const lTable = useDataTable({
    columns: lCols,
    data: llmRows,
    getRowId: (r) => r.id,
    prefsKey: "llms",
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
            <Button variant="outline" size="sm" onClick={() => void agents.refetch()}>
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
        </p>
      </div>

      <section className="flex flex-col">
        <div className="flex justify-end px-6">
          <DataTableViewOptions table={aTable} />
        </div>
        <DataTable
          table={aTable}
          onRowClick={(row) => void navigate({ to: "/agents/$id", params: { id: row.id } })}
          empty={
            <Empty className="py-12">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <BotIcon />
                </EmptyMedia>
                <EmptyTitle>No agents</EmptyTitle>
                <EmptyDescription>No agents are declared on this substrate yet.</EmptyDescription>
              </EmptyHeader>
            </Empty>
          }
        />
      </section>

      <section className="mt-6 flex flex-col">
        <div className="flex items-end justify-between px-6 pb-1">
          <div>
            <h2 className="text-sm font-medium">Models</h2>
            <p className="text-xs text-muted-foreground">
              The llm connection rows agents resolve, from{" "}
              <span className="data">core.substrate.reamde.dev/llms</span>
            </p>
          </div>
          <DataTableViewOptions table={lTable} />
        </div>
        {llms.isError ? (
          // An LLM query failure is its own state — never an empty "No models"
          // table, which would read as "none declared" when the read simply
          // failed.
          <div className="px-6">
            <Empty className="rounded-md border py-10">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <SearchXIcon />
                </EmptyMedia>
                <EmptyTitle>The models didn't load</EmptyTitle>
                <EmptyDescription>{llms.error.message}</EmptyDescription>
              </EmptyHeader>
              <EmptyContent>
                <Button variant="outline" size="sm" onClick={() => void llms.refetch()}>
                  Retry
                </Button>
              </EmptyContent>
            </Empty>
          </div>
        ) : (
          <DataTable
            table={lTable}
            density="compact"
            loading={llms.isPending}
            empty={
              <Empty className="py-10">
                <EmptyHeader>
                  <EmptyTitle>No models</EmptyTitle>
                  <EmptyDescription>No llm rows are declared yet.</EmptyDescription>
                </EmptyHeader>
              </Empty>
            }
          />
        )}
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
          <div key={i} className="flex h-12 items-center gap-6 border-b last:border-0">
            <Skeleton className="h-4 w-48" />
            <Skeleton className="ml-auto h-7 w-16" />
          </div>
        ))}
      </div>
    </div>
  )
}
