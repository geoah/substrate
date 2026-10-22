/** The console's unit reader (lib/unit-reader.ts): the repository's graph,
 * and a pending reader while it loads or after it failed.
 *
 * Mount it once, through `UnitsProvider` (components/units-provider.tsx):
 * each mount opens a watch on the change stream. The graph is refetched when
 * a unit or conversion record changes, each time the watch opens (a change
 * committed between the fetch and the watch's start reaches no stream), and
 * after a compaction, which ends the watch and starts a new one at the
 * head. */

import { useEffect, useMemo, useState } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"

import { watchChanges } from "@/lib/api/changes"
import { getRepository } from "@/lib/api/session"
import {
  CONVERSION_KIND,
  UNIT_KIND,
  unitGraphQueryOptions,
  unitRecordChanged,
} from "@/lib/api/units"
import { graphUnits, pendingUnits, type UnitReader } from "@/lib/unit-reader"

export function useUnitReader(): UnitReader {
  const queryClient = useQueryClient()
  const repository = getRepository()
  const graphQuery = unitGraphQueryOptions(repository)
  const { data: graph, error, isError } = useQuery(graphQuery)
  // A compaction ends the watch; the next epoch opens a new one at the head.
  const [epoch, setEpoch] = useState(0)
  const key = JSON.stringify(graphQuery.queryKey)
  useEffect(() => {
    const queryKey = JSON.parse(key) as unknown[]
    const refetch = () => void queryClient.invalidateQueries({ queryKey })
    const handle = watchChanges({
      filter: { kinds: [UNIT_KIND, CONVERSION_KIND] },
      onRow: (row) => {
        if (unitRecordChanged(row.kind)) refetch()
      },
      onStatus: (status) => {
        if (status === "live") refetch()
      },
      onCompacted: () => {
        refetch()
        setEpoch((n) => n + 1)
      },
    })
    return () => handle.stop()
  }, [queryClient, key, epoch])
  return useMemo(() => {
    if (graph) return graphUnits(graph)
    if (isError) return pendingUnits("error", error)
    return pendingUnits("loading")
  }, [graph, isError, error])
}
