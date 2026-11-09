import { useEffect, useMemo, useState } from "react"
import { useBackend } from "../../data"
import { useMode } from "../../data/mode"
import type { Context } from "../../import/importers/types"
import { EMPTY_GRAPH, type UnitGraph } from "../../labs/units"

/** What an importer's reading needs from the repository and the owner,
 * once each read has answered: the metrics, the unit graph, the sources
 * and the owner's contact. The answers are the screen's. */
export function useImportContext(): Omit<Context, "answers"> | undefined {
  const { today } = useMode()
  const backend = useBackend()
  const metrics = backend.sources.useMetrics().data
  const list = backend.sources.useSourceList().data
  const owner = backend.profile.useOwner().data
  const actions = backend.sources.useImportActions()
  const [units, setUnits] = useState<UnitGraph>()
  useEffect(() => {
    let live = true
    actions.units().then(
      (g) => {
        if (live) setUnits(g)
      },
      // Without the graph a unit converts through the app's own table.
      () => {
        if (live) setUnits(EMPTY_GRAPH)
      }
    )
    return () => {
      live = false
    }
  }, [actions])
  const contact = owner?.contact?.id
  return useMemo(
    () =>
      metrics && list && units
        ? {
            today,
            metrics,
            units,
            sources: list.map((s) => ({
              id: s.id,
              name: s.name,
              class: s.class,
            })),
            ownerContact: contact,
          }
        : undefined,
    [today, metrics, list, units, contact]
  )
}
