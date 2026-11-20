// The labs (docs/plans/healthspan.md, The catalogue): every lab result
// resolved against the catalogue, the views the Labs screens draw, and the
// findings Today reads. Live, one read of the lab kinds and the unit graph;
// in the demo, the invented member's rows through the same transforms. The
// targets are the ones for the owner's sex, from the profile's contract.

import type { SubstrateClient } from "@substrate/client"
import { useRead } from "@substrate/react"
import { useMemo } from "react"
import { demoLabIndex } from "../demo/labs"
import { KIND, type Sex } from "../kinds"
import { toLabIndex } from "../labs/labIndex"
import type { LabIndex } from "../labs/types"
import { EMPTY_GRAPH, parseUnitGraph, type UnitGraph } from "../labs/units"
import { labViews, type LabView } from "../labs/view"
import { sortFindings, type Finding } from "../rules/types"
import { useMode } from "./mode"
import { demoProfile, liveProfile } from "./profile"
import { settled, type Read } from "./read"

/** The labs as the screens read them. */
export interface LabsModel {
  index: LabIndex
  /** Every subject with a result, for the owner's sex and today. */
  views: LabView[]
  /** The sex the targets are for; unknown, every entry's default bands. */
  sex?: Sex
}

export interface LabsData {
  /** Results outside the lab's range or off target, important tests
   * overdue, for Today. */
  useLabFindings(): Read<Finding[]>
  /** Every lab result resolved against the catalogue, with the views. */
  useLabs(): Read<LabsModel>
}

/** The kinds the labs read depends on. */
export const LAB_KINDS = [
  KIND.labresult,
  KIND.labreport,
  KIND.metric,
  KIND.analyte,
  KIND.source,
]

/** The most rows of one kind a read takes. */
const ROW_LIMIT = 100_000

async function readUnits(
  client: SubstrateClient,
  signal: AbortSignal
): Promise<UnitGraph> {
  try {
    return parseUnitGraph(
      await client.json<unknown>("GET", "api/v1/units", { signal })
    )
  } catch (e) {
    // An older server without the route: the labs convert through
    // lib/units' table alone.
    if (signal.aborted) throw e
    return EMPTY_GRAPH
  }
}

/** The lab kinds and the unit graph, folded into the index. Every metric
 * is read, not only the `labs` and `body` categories: a result may point at
 * a metric of another (blood pressure, height). */
export async function readLabs(
  client: SubstrateClient,
  signal: AbortSignal
): Promise<LabIndex> {
  const all = async (kind: string) =>
    (await client.listAll({ kinds: [kind], first: 500 }, ROW_LIMIT, signal))
      .records ?? []
  const [results, reports, metrics, analytes, sources, graph] =
    await Promise.all([
      all(KIND.labresult),
      all(KIND.labreport),
      all(KIND.metric),
      all(KIND.analyte),
      all(KIND.source),
      readUnits(client, signal),
    ])
  return toLabIndex({ results, reports, metrics, analytes, sources }, graph)
}

function useModel(read: Read<LabIndex>, sex: Sex | undefined): Read<LabsModel> {
  const { today } = useMode()
  const index = read.data
  const data = useMemo(
    () =>
      index ? { index, views: labViews(index, today, sex), sex } : undefined,
    [index, today, sex]
  )
  const { error, loading, refreshing, refresh } = read
  return useMemo(
    () => ({ data, error, loading, refreshing, refresh }),
    [data, error, loading, refreshing, refresh]
  )
}

function useFindings(labs: Read<LabsModel>): Read<Finding[]> {
  const model = labs.data
  const data = useMemo(
    () =>
      model ? sortFindings(model.views.flatMap((v) => v.findings)) : undefined,
    [model]
  )
  const { error, loading, refreshing, refresh } = labs
  return useMemo(
    () => ({ data, error, loading, refreshing, refresh }),
    [data, error, loading, refreshing, refresh]
  )
}

function useLiveLabs(): Read<LabsModel> {
  const read = useRead("healthspan.labs", readLabs, { kinds: LAB_KINDS })
  const owner = liveProfile.useOwner()
  return useModel(read, owner.data?.sex?.value)
}

function useDemoLabs(): Read<LabsModel> {
  const { today } = useMode()
  const owner = demoProfile.useOwner()
  const index = useMemo(() => demoLabIndex(today), [today])
  const read = useMemo(() => settled(index), [index])
  return useModel(read, owner.data?.sex?.value)
}

export const liveLabs: LabsData = {
  useLabFindings: () => useFindings(useLiveLabs()),
  useLabs: useLiveLabs,
}

export const demoLabs: LabsData = {
  useLabFindings: () => useFindings(useDemoLabs()),
  useLabs: useDemoLabs,
}
