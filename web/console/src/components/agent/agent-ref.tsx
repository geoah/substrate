/** An agent, named: its mark and its name, and on hover the card the agents
 * panel heads with (what it runs on, what it is for, what it may see and
 * change) over the agent's full reference. Every surface that names an agent
 * by its record uses this, so an agent reads one way everywhere. */

import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"

import { AgentMark } from "@/components/agent/agent-mark"
import {
  IdentityCard,
  IdentityHoverCard,
} from "@/components/identity/identity-hover-card"
import {
  agentName,
  agentProviderId,
  canChange,
  canSee,
  providerName,
} from "@/lib/agent-chat"
import { CORE_AUTHORITY, CORE_PACKAGE_NAME } from "@/lib/api/http"
import { recordQueryOptions } from "@/lib/api/records"
import type { SubstrateRecord } from "@/lib/api/types"
import { cn } from "@/lib/utils"

/** The card itself, reading the agent record only once it opens, unless the
 * caller already holds it. */
export function AgentCard({
  id,
  agent,
  open = true,
}: {
  id: string
  agent?: SubstrateRecord
  open?: boolean
}) {
  const read = useQuery({
    ...recordQueryOptions(CORE_AUTHORITY, CORE_PACKAGE_NAME, "agent", id),
    enabled: open && !agent,
  })
  const record = agent ?? read.data
  const model =
    typeof record?.properties.model === "string" ? record.properties.model : ""
  const provider = record ? agentProviderId(record) : undefined
  const description =
    typeof record?.properties.description === "string"
      ? record.properties.description
      : undefined
  const sub = [model, provider && providerName(provider)]
    .filter(Boolean)
    .join(" · ")
  return (
    <IdentityCard
      mark={<AgentMark id={id} size="sm" />}
      title={agentName(id)}
      sub={sub || "Agent"}
      description={description}
      loading={!record && read.isPending && open}
      facts={
        record
          ? [
              {
                label: "Can see",
                value: canSee(record) ?? "Nothing in your data",
              },
              { label: "Can change", value: canChange(record) },
            ]
          : undefined
      }
      reference={`${CORE_AUTHORITY}/${CORE_PACKAGE_NAME}/agent/${id}`}
    />
  )
}

export function AgentRef({
  id,
  agent,
  link = false,
  size = "xs",
  className,
}: {
  /** The agent record's id, `<authority>/<package>/<name>`. */
  id: string
  /** The agent record, when the caller already has it. */
  agent?: SubstrateRecord
  /** Opens a new chat with the agent. Off inside something that is already
   * a button. */
  link?: boolean
  size?: "xs" | "sm"
  className?: string
}) {
  return (
    <IdentityHoverCard
      trigger={
        link ? (
          <Link
            to="/agents"
            search={{ agent: id } as never}
            onClick={(e) => e.stopPropagation()}
          />
        ) : (
          <span />
        )
      }
      className={cn(
        "group/agent inline-flex max-w-full min-w-0 items-center gap-1.5 align-middle text-foreground no-underline outline-none focus-visible:ring-2 focus-visible:ring-ring/50",
        className
      )}
      card={(open) => <AgentCard id={id} agent={agent} open={open} />}
    >
      <AgentMark id={id} size={size} />
      <span className="truncate underline-offset-2 group-hover/agent:underline">
        {agentName(id)}
      </span>
    </IdentityHoverCard>
  )
}
