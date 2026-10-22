/** The repository's unit graph from the server (decision 0186): `GET
 * /api/v1/units`, read into a UnitGraph (lib/unit-graph.ts) and cached by
 * server, repository and graph revision, so a refetch that finds the same
 * revision keeps the graph already built. A change to any unit or conversion
 * record in the change stream drops it (`unitRecordChanged`), and so does a
 * repository switch, whose query key differs. hooks/use-unit-reader.ts
 * reads it into a reader (lib/unit-reader.ts). */

import { queryOptions } from "@tanstack/react-query"

import { CORE_AUTHORITY, CORE_PACKAGE_NAME } from "@/lib/core-kinds"
import { UnitGraph } from "@/lib/unit-graph"

import { request, rootPath } from "./http"
import { getRepository } from "./session"
import type { UnitConversion, UnitTable } from "./types"

/** The two kinds whose records are the graph. */
export const UNIT_KIND = `${CORE_AUTHORITY}/${CORE_PACKAGE_NAME}/unit`
export const CONVERSION_KIND = `${CORE_AUTHORITY}/${CORE_PACKAGE_NAME}/conversion`

/** Whether a changed record is one the graph is read from. */
export function unitRecordChanged(kind: string): boolean {
  return kind === UNIT_KIND || kind === CONVERSION_KIND
}

/** The server a cache entry belongs to: the console is served by the
 * substrate it talks to, so the origin names it. */
function serverKey(): string {
  return typeof location === "undefined" ? "" : location.origin
}

/** Built graphs by server, repository and revision. */
const graphs = new Map<string, UnitGraph>()
const MAX_GRAPHS = 8

/** The graph for an answer, built once per server, repository and
 * revision. */
export function graphOf(
  table: UnitTable,
  repository: string | null = getRepository(),
  server: string = serverKey()
): UnitGraph {
  const key = `${server}|${repository ?? ""}|${table.revision}`
  const held = graphs.get(key)
  if (held) return held
  const g = new UnitGraph(table)
  graphs.set(key, g)
  while (graphs.size > MAX_GRAPHS) graphs.delete(graphs.keys().next().value!)
  return g
}

/** Forget every built graph: a sign-out or a repository switch. */
export function forgetUnitGraphs(): void {
  graphs.clear()
}

/** The repository's graph as a query, keyed by server and repository; the
 * revision keys the built graph inside it. Between refetches the change
 * stream is what keeps it current (hooks/use-unit-reader.ts), so it is never
 * stale by age; a new observer refetches it anyway, because no stream
 * watched the repository while none was mounted. */
export function unitGraphQueryOptions(
  repository: string | null = getRepository()
) {
  const server = serverKey()
  return queryOptions({
    queryKey: ["units", server, repository ?? ""],
    queryFn: async () =>
      graphOf(
        await request<UnitTable>("GET", rootPath("units")),
        repository,
        server
      ),
    staleTime: Infinity,
    refetchOnMount: "always",
  })
}

/** One value converted by the server, for an expression conversion only it
 * evaluates; delta converts a difference. */
export function convertOnServer(
  value: string,
  from: string,
  to: string,
  delta = false
): Promise<UnitConversion> {
  const params = new URLSearchParams({ value, from, to })
  if (delta) params.set("delta", "true")
  return request<UnitConversion>(
    "GET",
    `${rootPath("units", "convert")}?${params}`
  )
}
