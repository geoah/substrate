/** What an agent runs on, read the same way on every surface that names it
 * (lib/llm-models.ts): the AI model rows and whose key each runs on
 * (useModelKeys), and, for an agent on `runsOn`, the llm/model records its
 * entries name. */

import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"

import { useModelKeys, type ModelKeys } from "@/hooks/use-model-keys"
import { llmModelsQueryOptions } from "@/lib/api/agents"
import type { SubstrateRecord } from "@/lib/api/types"
import {
  agentRuns,
  agentRunsOn,
  threadRuns,
  type AgentRuns,
  type AgentRunsContext,
} from "@/lib/llm-models"

export interface AgentRunsRead {
  keys: ModelKeys
  /** The live llm/model records, once read. */
  models: SubstrateRecord[] | undefined
  ctx: AgentRunsContext
  /** What a new thread of the agent runs on. */
  of: (agent: SubstrateRecord) => AgentRuns
  /** What the next turn of a conversation runs on: a continued thread of
   * an agent on `runsOn` stays on its stamped row and model. */
  next: (
    agent: SubstrateRecord,
    thread: SubstrateRecord | undefined,
    threadId: string
  ) => Pick<AgentRuns, "row" | "canRun" | "keyRow">
  /** A read this needs has not answered. */
  pending: boolean
}

/** `agents`: the agents a surface shows; the model records are read only
 * where one of them is on `runsOn`. */
export function useAgentRuns(
  agents: readonly (SubstrateRecord | undefined)[]
): AgentRunsRead {
  const keys = useModelKeys()
  const needed = agents.some((a) => a && agentRunsOn(a))
  const read = useQuery(llmModelsQueryOptions(needed))
  const models = read.data?.records
  return useMemo(() => {
    const ctx: AgentRunsContext = {
      models,
      rows: keys.rows,
      keyOf: keys.keyOf,
      resolves: keys.resolves,
    }
    return {
      keys,
      models,
      ctx,
      of: (agent) => agentRuns(agent, ctx),
      next: (agent, thread, threadId) =>
        threadRuns(agent, thread, threadId, ctx),
      pending: keys.pending || (needed && read.isPending),
    }
  }, [keys, models, needed, read.isPending])
}
