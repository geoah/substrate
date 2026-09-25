/** The Tools pages' one model: every function joined with the agents that
 * list it, the triggers that invoke it, its newest runs (trigger runs and an
 * agent's tool calls) and, for a provider's sync, whether the provider is
 * set up yet. */

import { useQuery } from "@tanstack/react-query"
import { useMemo } from "react"

import { agentsQueryOptions } from "@/lib/api/agents"
import {
  functionsQueryOptions,
  messageAgent,
  recentTriggerRunsQueryOptions,
  toolMessagesQueryOptions,
} from "@/lib/api/functions"
import { kindsQueryOptions } from "@/lib/api/kinds"
import { repositoryQueryOptions } from "@/lib/api/repository"
import { getRepository } from "@/lib/api/session"
import {
  syncStatusesQueryOptions,
  triggerRecordsQueryOptions,
} from "@/lib/api/sync"
import type { SubstrateRecord, SyncStatus } from "@/lib/api/types"
import type { ProviderInfo } from "@/lib/actor-identity"
import {
  buildTools,
  kindLabeler,
  refId,
  runFromTriggerRun,
  sortRuns,
  toolMessageRuns,
  watchedKinds,
  type KindLabeler,
  type Tool,
  type ToolRun,
} from "@/lib/tools"

/** `*`, `<authority>/*`, `<authority>/<package>/*`, or the exact reference. */
function globMatches(pattern: string, kind: string): boolean {
  if (pattern === "*") return true
  if (pattern.endsWith("/*")) return kind.startsWith(pattern.slice(0, -1))
  return pattern === kind
}

export interface ToolsModel {
  tools: Tool[]
  isPending: boolean
  error: unknown
  label: KindLabeler
  /** Newest first, trigger runs and agent calls together. */
  runsOf: (tool: Tool) => ToolRun[]
  /** The provider a sync is waiting on: none of the records its change
   * triggers watch exists yet. */
  waitingFor: (tool: Tool) => ProviderInfo | undefined
  /** The connected records (accounts) a sync's change triggers watch. */
  accountsOf: (tool: Tool) => SyncStatus[]
  agentsById: Map<string, SubstrateRecord>
}

export function useTools(): ToolsModel {
  const functions = useQuery(functionsQueryOptions)
  const agents = useQuery(agentsQueryOptions())
  const triggers = useQuery(triggerRecordsQueryOptions)
  const kinds = useQuery(kindsQueryOptions)
  const syncs = useQuery(syncStatusesQueryOptions)
  const runs = useQuery(recentTriggerRunsQueryOptions)
  const repository = useQuery(repositoryQueryOptions)
  const authority = repository.data?.authority || getRepository()

  const tools = useMemo(
    () =>
      buildTools(
        functions.data ?? [],
        agents.data?.records ?? [],
        triggers.data ?? [],
        authority
      ),
    [functions.data, agents.data, triggers.data, authority]
  )
  const names = useMemo(
    () => tools.flatMap((t) => t.uses.map((u) => u.name)),
    [tools]
  )
  const messages = useQuery(toolMessagesQueryOptions(names))

  const label = useMemo(() => kindLabeler(kinds.data), [kinds.data])

  const runsByTrigger = useMemo(() => {
    const out = new Map<string, ToolRun[]>()
    for (const r of runs.data ?? []) {
      const trigger = refId(r.properties.trigger)
      if (!trigger) continue
      const list = out.get(trigger) ?? []
      list.push(runFromTriggerRun(r))
      out.set(trigger, list)
    }
    return out
  }, [runs.data])

  const agentsById = useMemo(
    () => new Map((agents.data?.records ?? []).map((a) => [a.id, a])),
    [agents.data]
  )

  return {
    tools,
    isPending: functions.isPending || agents.isPending || triggers.isPending,
    error: functions.error ?? triggers.error ?? agents.error,
    label,
    runsOf: (tool) => {
      const fromTriggers = tool.triggers.flatMap(
        (t) => runsByTrigger.get(t.id) ?? []
      )
      const page = messages.data
      const fromAgents = page
        ? toolMessageRuns(tool, page.records ?? [], (m) =>
            messageAgent(page, m)
          )
        : []
      return sortRuns([...fromTriggers, ...fromAgents])
    },
    waitingFor: (tool) => {
      if (tool.origin.kind !== "provider" || !syncs.data) return undefined
      const watched = watchedKinds(tool)
      if (!watched.length) return undefined
      const connected = syncs.data.some((s) =>
        watched.some((p) => globMatches(p, s.kind))
      )
      return connected ? undefined : tool.origin.provider
    },
    accountsOf: (tool) => {
      const watched = watchedKinds(tool)
      return (syncs.data ?? []).filter((s) =>
        watched.some((p) => globMatches(p, s.kind))
      )
    },
    agentsById,
  }
}

/** An agent record's display name: its local name. */
export function agentName(id: string): string {
  return id.slice(id.lastIndexOf("/") + 1)
}

/** An agent record id as its actor string. */
export function agentActor(id: string): string {
  return `agent:${id.split("/").join(":")}`
}
