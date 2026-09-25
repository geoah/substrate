/** Home's first row: the four things the console is for (your data, your
 * providers, your agents, your tools), each a card that opens its page. */

import type { ReactNode } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import {
  BotIcon,
  DatabaseIcon,
  PlugIcon,
  WrenchIcon,
  type LucideIcon,
} from "lucide-react"

import { Skeleton } from "@/components/ui/skeleton"
import { PROVIDERS_AUTHORITY } from "@/lib/actor-identity"
import { agentsQueryOptions } from "@/lib/api/agents"
import { bundleStatusesQueryOptions } from "@/lib/api/bundles"
import { CORE_AUTHORITY, CORE_PACKAGE_NAME, splitKind } from "@/lib/api/http"
import { recordsQueryOptions } from "@/lib/api/records"
import type { CollectionGroup } from "@/lib/collections"
import {
  dataSummary,
  namesSummary,
  plural,
  providersSummary,
  type CardSummary,
} from "@/lib/home-summary"

/** Every declared function: the tools the repository and its agents use. */
// eslint-disable-next-line react-refresh/only-export-components -- a query shared with History's provider view
export const functionsQueryOptions = recordsQueryOptions({
  authority: CORE_AUTHORITY,
  package: CORE_PACKAGE_NAME,
  name: "function",
  first: 500,
})

function OverviewCard({
  to,
  icon: Icon,
  title,
  summary,
}: {
  to: "/data" | "/providers" | "/agents" | "/tools"
  icon: LucideIcon
  title: string
  summary: CardSummary | undefined
}) {
  return (
    <Link
      to={to}
      className="flex min-w-0 flex-col gap-1.5 rounded-[10px] border border-border bg-background p-3.5 text-foreground no-underline transition-colors hover:border-border-strong hover:bg-panel"
    >
      <span className="flex items-center gap-2 font-semibold">
        <Icon className="size-4 text-faint" strokeWidth={1.8} />
        {title}
      </span>
      {summary ? (
        <>
          <span className="text-[22px] font-semibold tracking-[-0.02em] tabular-nums">
            {summary.big}
          </span>
          <span className="text-[12.5px] text-faint">{summary.sub}</span>
        </>
      ) : (
        <>
          <Skeleton className="h-7 w-24" />
          <Skeleton className="h-3.5 w-32" />
        </>
      )}
    </Link>
  )
}

function lastSegment(id: string): string {
  return id.split("/").at(-1) ?? id
}

export function OverviewCards({
  groups,
}: {
  /** The collection groups, when the kind registry has answered. */
  groups: CollectionGroup[] | undefined
}): ReactNode {
  const statuses = useQuery(bundleStatusesQueryOptions)
  const agents = useQuery(agentsQueryOptions())
  const functions = useQuery(functionsQueryOptions)

  const data = groups
    ? dataSummary(
        groups
          .filter((g) => g.type !== "system")
          .reduce((n, g) => n + g.primary.length, 0),
        groups.filter((g) => g.type === "provider").length
      )
    : undefined
  const providers = statuses.data ? providersSummary(statuses.data) : undefined
  const agentNames = (agents.data?.records ?? []).map((r) => lastSegment(r.id))
  const agentSummary = agents.data
    ? {
        big: plural(agentNames.length, "agent", "agents"),
        sub: agentNames.length
          ? namesSummary(agentNames)
          : "Add one to work on your data",
      }
    : undefined
  const tools = functions.data?.records ?? []
  const fromProviders = tools.filter(
    (r) => splitKind(r.id).authority === PROVIDERS_AUTHORITY
  ).length
  const toolSummary = functions.data
    ? {
        big: plural(tools.length, "tool", "tools"),
        sub: fromProviders
          ? `${fromProviders.toLocaleString()} from your providers`
          : "for you and your agents",
      }
    : undefined

  return (
    <div className="grid grid-cols-[repeat(auto-fill,minmax(190px,1fr))] gap-2.5">
      <OverviewCard
        to="/data"
        icon={DatabaseIcon}
        title="Data"
        summary={data}
      />
      <OverviewCard
        to="/providers"
        icon={PlugIcon}
        title="Providers"
        summary={providers}
      />
      <OverviewCard
        to="/agents"
        icon={BotIcon}
        title="Agents"
        summary={agentSummary}
      />
      <OverviewCard
        to="/tools"
        icon={WrenchIcon}
        title="Tools"
        summary={toolSummary}
      />
    </div>
  )
}
