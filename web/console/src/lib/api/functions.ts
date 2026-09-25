/** The Tools pages' reads and verbs: the function records, one function, the
 * direct call (`POST …/core/function/{ref}/call`), the tool-result messages
 * an agent's calls left, and switching a tool's triggers on and off. Every one
 * is an existing route; nothing here adds to the wire. */

import { queryOptions } from "@tanstack/react-query"

import {
  CORE_AUTHORITY,
  CORE_PACKAGE_NAME,
  LLM_PACKAGE,
  corePath,
  request,
  seg,
} from "./http"
import { fetchRecordsPage, patchRecord, recordQueryOptions } from "./records"
import type { FunctionCalled, Page, SubstrateRecord } from "./types"

const FUNCTION_KIND = `${CORE_AUTHORITY}/${CORE_PACKAGE_NAME}/function`
const MESSAGE_KIND = `${LLM_PACKAGE}/message`

/** Every function record: a registry-sized read, one page. */
export const functionsQueryOptions = queryOptions({
  queryKey: ["records", [FUNCTION_KIND], "tools"],
  queryFn: async ({ signal }) => {
    const page = await fetchRecordsPage(
      { kinds: [FUNCTION_KIND], first: 500 },
      signal
    )
    return page.records ?? []
  },
  staleTime: 60_000,
})

/** One function by its reference. */
export function functionQueryOptions(ref: string) {
  return recordQueryOptions(CORE_AUTHORITY, CORE_PACKAGE_NAME, "function", ref)
}

/** Run one function now with `input` as its arguments; what it changes lands
 * under the function's own actor. */
export function callFunction(
  ref: string,
  input: unknown
): Promise<FunctionCalled> {
  return request<FunctionCalled>(
    "POST",
    `${corePath("function")}/${seg(ref)}/call`,
    { input }
  )
}

/** The newest tool-result messages answering under any of `names`, each
 * with its thread alongside (`included`), which names the agent that made
 * the call. A tool row's `name` is the name the agent's model calls the tool
 * by, so the caller matches it back through the agents' `tools:`. */
export function toolMessagesQueryOptions(names: string[], first = 50) {
  const sorted = [...new Set(names)].sort()
  return queryOptions({
    queryKey: ["records", [MESSAGE_KIND], "tool-calls", sorted, first],
    queryFn: ({ signal }): Promise<Page> =>
      fetchRecordsPage(
        {
          kinds: [MESSAGE_KIND],
          first,
          orderBy: "createdAt:desc",
          filter: {
            properties: { role: { eq: "tool" }, name: { in: sorted } },
          },
          expand: ["thread"],
        },
        signal
      ),
    enabled: sorted.length > 0,
    staleTime: 15_000,
  })
}

/** The agent a tool message's thread belongs to, off the page's `included`. */
export function messageAgent(
  page: Page | undefined,
  message: SubstrateRecord
): string | undefined {
  const thread = message.properties.thread as
    { ref?: string } | string | undefined
  const path = typeof thread === "string" ? thread : thread?.ref
  if (!path || !page?.included) return undefined
  const agent = page.included[path]?.properties.agent as
    { ref?: string } | string | undefined
  const agentPath = typeof agent === "string" ? agent : agent?.ref
  if (!agentPath) return undefined
  return agentPath.split("/").slice(3).join("/") || undefined
}

/** Switch one trigger on or off; off stops its deliveries and keeps its
 * place in the changelog. */
export function setTriggerEnabled(
  id: string,
  enabled: boolean
): Promise<SubstrateRecord> {
  return patchRecord(CORE_AUTHORITY, CORE_PACKAGE_NAME, "trigger", id, {
    properties: { enabled },
  })
}

const TRIGGER_RUN_KIND = `${CORE_AUTHORITY}/${CORE_PACKAGE_NAME}/triggerrun`

/** The newest trigger runs across every trigger: the ledger keeps twenty per
 * trigger plus the parked ones, so one page covers every tool's latest. */
export const recentTriggerRunsQueryOptions = queryOptions({
  queryKey: ["records", [TRIGGER_RUN_KIND], "tools"],
  queryFn: async ({ signal }) => {
    const page = await fetchRecordsPage(
      { kinds: [TRIGGER_RUN_KIND], first: 500, orderBy: "updatedAt:desc" },
      signal
    )
    return page.records ?? []
  },
  staleTime: 15_000,
})
