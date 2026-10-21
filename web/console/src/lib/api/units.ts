/** The repository's unit graph from the server (decision 0186): `GET
 * /api/v1/units`, read into a UnitGraph (lib/unit-graph.ts) and cached by
 * server, repository and graph revision, so a refetch that finds the same
 * revision keeps the graph already built. A change to any unit or conversion
 * record in the change stream drops it (`unitRecordChanged`), and so does a
 * repository switch, whose query key differs.
 *
 * `unitReader` is the one place the console chooses how it converts: through
 * the graph where the server's switch is on (`units.graph` in discovery),
 * else through lib/units.ts's copy of the engine's table. */

import { queryOptions } from "@tanstack/react-query"

import { CORE_AUTHORITY, CORE_PACKAGE_NAME } from "@/lib/core-kinds"
import {
  canonicalUnit,
  checkQuantity,
  convertUnit,
  formatQuantity,
  unitSymbol,
  unitsFor,
  type QuantityDeclaration,
  type UnitGroup,
} from "@/lib/units"
import { unitChoices } from "@/lib/unit-choices"
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
 * revision keys the built graph inside it. */
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

/** The unit operations the console's components call, through the graph
 * where the server's switch is on and through the engine's table where it
 * is not. */
export interface UnitReader {
  graph?: UnitGraph
  symbol(unit: string): string
  canonical(input: string): string
  convert(
    value: number,
    from: string,
    to: string,
    delta?: boolean
  ): number | undefined
  unitsFor(spec?: QuantityDeclaration): UnitGroup[]
  check(value: unknown, spec?: QuantityDeclaration): string | undefined
  choices(stored: string | undefined, measures?: string): string[]
  format(value: unknown): string
}

/** The reader for a graph, or the table's for none. */
export function unitReader(graph?: UnitGraph): UnitReader {
  if (!graph) {
    return {
      symbol: unitSymbol,
      canonical: canonicalUnit,
      convert: (value, from, to, delta) =>
        convertUnit(
          value,
          from,
          to,
          delta ? "temperature-difference" : undefined
        ),
      unitsFor,
      check: checkQuantity,
      choices: unitChoices,
      format: formatQuantity,
    }
  }
  return {
    graph,
    symbol: (unit) => graph.symbol(unit),
    canonical: (input) => graph.canonical(input),
    convert: (value, from, to, delta) => graph.convert(value, from, to, delta),
    unitsFor: (spec) => graph.unitsFor(spec),
    check: (value, spec) => graph.checkQuantity(value, spec),
    choices: (stored) => graph.unitChoices(stored),
    format: (value) => graph.formatQuantity(value),
  }
}
