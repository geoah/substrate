/** The console's unit reader (lib/api/units.ts unitReader): the repository's
 * unit graph where the server's switch is on, the engine's table where it is
 * not or until the graph has loaded. While it is mounted, a change to any
 * unit or conversion record in the change stream drops the cached graph, so
 * the next read fetches the new revision. */

import { useEffect } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"

import { watchChanges } from "@/lib/api/changes"
import { unitGraphSwitchQueryOptions } from "@/lib/api/discovery"
import { getRepository } from "@/lib/api/session"
import {
  CONVERSION_KIND,
  UNIT_KIND,
  unitGraphQueryOptions,
  unitReader,
  unitRecordChanged,
  type UnitReader,
} from "@/lib/api/units"

export function useUnitReader(): UnitReader {
  const queryClient = useQueryClient()
  const { data: on } = useQuery(unitGraphSwitchQueryOptions)
  const repository = getRepository()
  const graphQuery = unitGraphQueryOptions(repository)
  const { data: graph } = useQuery({ ...graphQuery, enabled: on === true })
  useEffect(() => {
    if (on !== true) return
    const handle = watchChanges({
      filter: { kinds: [UNIT_KIND, CONVERSION_KIND] },
      onRow: (row) => {
        if (unitRecordChanged(row.kind)) {
          void queryClient.invalidateQueries({ queryKey: graphQuery.queryKey })
        }
      },
    })
    return () => handle.stop()
    // The query key names the server and the repository.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [on, queryClient, JSON.stringify(graphQuery.queryKey)])
  return unitReader(on === true ? graph : undefined)
}
