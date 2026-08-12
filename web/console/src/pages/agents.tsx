/** Agents (`/agents`): the declared agents (a callable whose body is an LLM
 * loop) and the llmprovider rows they complete against. Both on THE table
 * system, off the record surface. An agent row opens the chat surface; a
 * provider row and an agent's manifest open their record page (where the
 * prompt/tools/budgets live and edit). */

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
import {
  agentsQueryOptions,
  providerEndpoint,
  providerHasKey,
  providersQueryOptions,
} from "@/lib/api/agents"
import type { SubstrateRecord } from "@/lib/api/types"
import { cellValue } from "@/lib/format"
import { cn } from "@/lib/utils"

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
      id: "provider",
      accessorFn: (a) => definitionField(a, "provider"),
      enableSorting: false,
      header: ({ column }) => <DataTableColumnHeader column={column} title="provider" />,
      cell: ({ row }) => (
        <span className="block truncate data text-muted-foreground">
          {definitionField(row.original, "provider") || "—"}
        </span>
      ),
      meta: { label: "provider", width: 120 },
    },
    {
      id: "model",
      accessorFn: (a) => definitionField(a, "model"),
      enableSorting: false,
      header: ({ column }) => <DataTableColumnHeader column={column} title="model" />,
      cell: ({ row }) => (
        <span
          className="block truncate data text-muted-foreground"
          title={definitionField(row.original, "model")}
        >
          {definitionField(row.original, "model") || "—"}
        </span>
      ),
      meta: { label: "model", size: { min: 140, weight: 1 } },
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

function providerColumns(): ColumnDef<SubstrateRecord, unknown>[] {
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
    text("wire", "wire", 110),
    {
      id: "endpoint",
      accessorFn: (e) => providerEndpoint(e),
      enableSorting: false,
      header: ({ column }) => <DataTableColumnHeader column={column} title="endpoint" />,
      cell: ({ row }) => {
        const endpoint = providerEndpoint(row.original)
        const own = Boolean(row.original.properties.baseURL)
        return (
          <span
            className={cn("block truncate text-muted-foreground", own && "data")}
            title={endpoint}
          >
            {endpoint}
          </span>
        )
      },
      meta: { label: "endpoint", size: { min: 160, weight: 1.5 } },
    },
    {
      id: "key",
      accessorFn: (e) => (providerHasKey(e) ? "set" : "not set"),
      enableSorting: false,
      // A secret reads back redacted, so the row can only say whether one is
      // there — never which.
      header: ({ column }) => <DataTableColumnHeader column={column} title="key" />,
      cell: ({ row }) => (
        <span className="block truncate text-muted-foreground">
          {providerHasKey(row.original) ? "set" : "not set"}
        </span>
      ),
      meta: { label: "key", width: 90 },
    },
  ]
}

export function AgentsPage() {
  const navigate = useNavigate()
  const agents = useQuery(agentsQueryOptions())
  const providers = useQuery(providersQueryOptions())

  const agentRows = useMemo(() => agents.data?.records ?? [], [agents.data])
  const providerRows = useMemo(() => providers.data?.records ?? [], [providers.data])
  const aCols = useMemo(() => agentColumns(), [])
  const pCols = useMemo(() => providerColumns(), [])
  const aTable = useDataTable({
    columns: aCols,
    data: agentRows,
    getRowId: (r) => r.id,
    prefsKey: "agents",
  })
  const pTable = useDataTable({
    columns: pCols,
    data: providerRows,
    getRowId: (r) => r.id,
    prefsKey: "llmproviders",
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
            <h2 className="text-sm font-medium">Providers</h2>
            <p className="text-xs text-muted-foreground">
              The endpoints agents complete against, from{" "}
              <span className="data">core.substrate.reamde.dev/llmproviders</span>
            </p>
          </div>
          <DataTableViewOptions table={pTable} />
        </div>
        {providers.isError ? (
          // A provider query failure is its own state — never an empty "No
          // providers" table, which would read as "none declared" when the read
          // simply failed.
          <div className="px-6">
            <Empty className="rounded-md border py-10">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <SearchXIcon />
                </EmptyMedia>
                <EmptyTitle>The providers didn't load</EmptyTitle>
                <EmptyDescription>{providers.error.message}</EmptyDescription>
              </EmptyHeader>
              <EmptyContent>
                <Button variant="outline" size="sm" onClick={() => void providers.refetch()}>
                  Retry
                </Button>
              </EmptyContent>
            </Empty>
          </div>
        ) : (
          <DataTable
            table={pTable}
            density="compact"
            loading={providers.isPending}
            empty={
              <Empty className="py-10">
                <EmptyHeader>
                  <EmptyTitle>No providers</EmptyTitle>
                  <EmptyDescription>No llmprovider rows are declared yet.</EmptyDescription>
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
