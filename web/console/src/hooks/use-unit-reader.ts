/** The console's unit reader (lib/unit-reader.ts): the engine's table where
 * the server's `units.graph` switch is off, the repository's graph where it
 * is on, and a pending reader while either answer is outstanding, so a
 * graph-enabled server never gets a table answer.
 *
 * Mount it once, through `UnitsProvider` (components/units-provider.tsx):
 * each mount opens a watch on the change stream, and a change to any unit or
 * conversion record drops the cached graph, so the next read fetches the new
 * revision. */

import { useEffect, useMemo } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"

import { watchChanges } from "@/lib/api/changes"
import { unitGraphSwitchQueryOptions } from "@/lib/api/discovery"
import { getRepository } from "@/lib/api/session"
import {
  CONVERSION_KIND,
  UNIT_KIND,
  unitGraphQueryOptions,
  unitRecordChanged,
} from "@/lib/api/units"
import {
  TABLE_UNITS,
  graphUnits,
  pendingUnits,
  type UnitReader,
} from "@/lib/unit-reader"

export function useUnitReader(): UnitReader {
  const queryClient = useQueryClient()
  const { data: on, isPending: switchPending } = useQuery(
    unitGraphSwitchQueryOptions
  )
  const repository = getRepository()
  const graphQuery = unitGraphQueryOptions(repository)
  const {
    data: graph,
    error,
    isError,
  } = useQuery({ ...graphQuery, enabled: on === true })
  const key = JSON.stringify(graphQuery.queryKey)
  useEffect(() => {
    if (on !== true) return
    const queryKey = JSON.parse(key) as unknown[]
    const refetch = () => void queryClient.invalidateQueries({ queryKey })
    const handle = watchChanges({
      filter: { kinds: [UNIT_KIND, CONVERSION_KIND] },
      onRow: (row) => {
        if (unitRecordChanged(row.kind)) refetch()
      },
    })
    return () => handle.stop()
  }, [on, queryClient, key])
  return useMemo(() => {
    if (switchPending) return pendingUnits("loading")
    if (on !== true) return TABLE_UNITS
    if (graph) return graphUnits(graph)
    if (isError) return pendingUnits("error", error)
    return pendingUnits("loading")
  }, [switchPending, on, graph, isError, error])
}
