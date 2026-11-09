// Sources and imports on the owner's substrate. The source records, the
// latest read, the counts and the links are reads beside the linked
// records (`useRead`), read again when their kinds change; the imports are
// the app's own linked records. A rank goes through the outbox, a link and
// every step of an import through the client, each followed by an
// invalidation of what it changed.

import { describe, uuidv7, type SubstrateClient } from "@substrate/client"
import {
  useInvalidate,
  useListed,
  useRead,
  useRecord,
  useRecords,
  useSubstrate,
  useWrites,
} from "@substrate/react"
import { useMemo, useSyncExternalStore } from "react"
import type { MetricRecord } from "../catalogue/types"
import { useBackend } from "../data"
import type { Read } from "../data/read"
import type {
  Done,
  ImportActions,
  SourceSummary,
  SourceWrites,
  SourcesData,
} from "../data/sources"
import type { HeldResult } from "../import/match"
import type { PlannedImport } from "../import/plan"
import { RunRegistry, deviceId } from "../import/registry"
import {
  clientStore,
  countFiltered,
  listFiltered,
  pointingAt,
} from "../import/store"
import { Import, KIND, type LabImport } from "../kinds"
import {
  convert as convertUnit,
  parseUnitGraph,
  type UnitGraph,
} from "../labs/units"
import { refPath } from "../import/records"
import { metricPath } from "./groups"
import { buildSourceList } from "./list"
import { keepLinkedTogether, linkProps, readLink } from "./links"
import { priorityWrites } from "./ranks"
import {
  heldOf,
  isLabMetric,
  plannedSource,
  readMetric,
  sortImports,
} from "./records"

const SOURCE_KINDS = [KIND.source]
const LATEST_KINDS = [KIND.labresult, KIND.observation, KIND.trace]
const COUNT_KINDS = [KIND.sleep, KIND.workout, KIND.labreport, KIND.source]
const LINK_KINDS = [KIND.sourcelink]
const METRIC_KINDS = [KIND.metric]

/** Counts per source id of one kind's records, for the sources given. */
async function countsBySource(
  client: SubstrateClient,
  kind: string,
  ids: string[],
  signal: AbortSignal
): Promise<Map<string, number>> {
  const out = new Map<string, number>()
  const total = await countFiltered(
    client,
    { kinds: [kind] },
    () => true,
    signal
  )
  if (!total) return out
  for (const id of ids) {
    const path = `${KIND.source}/${id}`
    const n = await countFiltered(
      client,
      { kinds: [kind], properties: { source: { eq: path } } },
      (r) => refPath(r.properties.source) === path,
      signal
    )
    if (n) out.set(id, n)
  }
  return out
}

function useLiveSourceList(): Read<SourceSummary[]> {
  const records = useRead(
    "healthspan sources",
    (client, signal) => listFiltered(client, { kinds: SOURCE_KINDS }, signal),
    { kinds: SOURCE_KINDS }
  )
  const latest = useRead(
    "healthspan sources latest",
    (client, signal) =>
      client.aggregate(
        { latest: true, groupBy: ["metric", "source"] },
        { filter: { kinds: LATEST_KINDS }, signal }
      ),
    { kinds: LATEST_KINDS }
  )
  const counts = useRead(
    "healthspan sources counts",
    async (client, signal) => {
      const sources = await listFiltered(
        client,
        { kinds: SOURCE_KINDS },
        signal
      )
      const all = sources.map((r) => r.id)
      const labs = sources
        .filter((r) => r.properties.class === "lab")
        .map((r) => r.id)
      const others = all.filter((id) => !labs.includes(id))
      const [nights, workouts, reports] = await Promise.all([
        countsBySource(client, KIND.sleep, others, signal),
        countsBySource(client, KIND.workout, others, signal),
        countsBySource(client, KIND.labreport, labs, signal),
      ])
      return { nights, workouts, reports }
    },
    { kinds: COUNT_KINDS }
  )
  const data = useMemo(() => {
    if (!records.data || !latest.data || !counts.data) return undefined
    return buildSourceList({
      sources: records.data,
      latest: latest.data.rows,
      ...counts.data,
    })
  }, [records.data, latest.data, counts.data])
  return {
    data,
    error: records.error ?? latest.error ?? counts.error,
    loading: !data && (records.loading || latest.loading || counts.loading),
    refreshing: records.refreshing || latest.refreshing || counts.refreshing,
    refresh: () => {
      records.refresh()
      latest.refresh()
      counts.refresh()
    },
  }
}

function useLiveMetrics(): Read<MetricRecord[]> {
  return useRead(
    "healthspan metrics",
    async (client, signal) =>
      (await listFiltered(client, { kinds: METRIC_KINDS }, signal)).map(
        readMetric
      ),
    { kinds: METRIC_KINDS }
  )
}

function useLiveLabMetrics(): Read<MetricRecord[]> {
  const read = useLiveMetrics()
  const data = useMemo(() => read.data?.filter(isLabMetric), [read.data])
  return useMemo(() => ({ ...read, data }), [read, data])
}

function useLiveSourceLinks() {
  return useRead(
    "healthspan source links",
    async (client, signal) =>
      (await listFiltered(client, { kinds: LINK_KINDS }, signal)).map(readLink),
    { kinds: LINK_KINDS }
  )
}

function useLiveSourceWrites(): SourceWrites {
  const writes = useWrites()
  const invalidate = useInvalidate()
  const { client } = useSubstrate()
  const profile = useBackend().profile.useProfileWrites()
  const metrics = useLiveMetrics().data
  return useMemo<SourceWrites>(() => {
    const rankSessions = async (
      kind: "nights" | "workouts",
      order: string[]
    ): Promise<Done> => {
      await profile.change(
        kind === "nights" ? { nightSources: order } : { workoutSources: order }
      )
      return { ok: true }
    }
    return {
      rankMetrics: async (group, order) => {
        const known = (metrics ?? []).map((m) => m.id)
        for (const w of priorityWrites(group, order, known)) {
          const r = await writes.change(KIND.source, w.id, {
            priorities: w.priorities,
          })
          if (!r.kept) return { ok: false, words: r.words }
        }
        invalidate(SOURCE_KINDS)
        return { ok: true }
      },
      rankSessions,
      link: async (name, sources, orders) => {
        const id = uuidv7()
        try {
          await client.put(KIND.sourcelink, id, linkProps(name, sources), 0)
        } catch (e) {
          return { ok: false, words: describe(e) }
        }
        invalidate(LINK_KINDS)
        const link = { id, version: 1, name, sources }
        for (const kind of ["nights", "workouts"] as const) {
          const next = keepLinkedTogether(orders[kind], [link])
          if (next.join(" ") !== orders[kind].join(" "))
            await rankSessions(kind, next)
        }
        return { ok: true }
      },
      unlink: async (link) => {
        try {
          await client.delete(KIND.sourcelink, link.id)
        } catch (e) {
          return { ok: false, words: describe(e) }
        }
        invalidate(LINK_KINDS)
        return { ok: true }
      },
    }
  }, [writes, invalidate, client, profile, metrics])
}

function useLiveImports(): Read<LabImport[]> {
  const records = useRecords(Import)
  const listed = useListed(Import)
  return useMemo(
    () => ({
      data: listed || records.length ? sortImports(records) : undefined,
      error: undefined,
      loading: !listed && !records.length,
      refreshing: false,
      refresh: () => {},
    }),
    [records, listed]
  )
}

function useLiveImport(id: string): Read<LabImport | null> {
  const read = useRecord(Import, id)
  return useMemo(
    () => ({
      data:
        read.state === "present"
          ? read.value
          : read.state === "absent"
            ? null
            : undefined,
      error: read.state === "failed" ? read.words : undefined,
      loading: read.state === "loading",
      refreshing: false,
      refresh: read.state === "failed" ? read.retry : () => {},
    }),
    [read]
  )
}

function useLiveStillPointing(id: string, enabled: boolean): Read<number> {
  const path = `${KIND.import}/${id}`
  return useRead(
    enabled ? `healthspan pointing ${id}` : null,
    async (client, signal) =>
      (await pointingAt(client, [KIND.labresult, KIND.labreport], path, signal))
        .length,
    { kinds: [KIND.labresult, KIND.labreport] }
  )
}

const registries = new WeakMap<SubstrateClient, RunRegistry>()

// The repository's unit graph, read once per session: the import converts
// a lab's units through it rather than asking the server pair by pair,
// where every pair that does not convert is a refused request.
const graphs = new WeakMap<SubstrateClient, Promise<UnitGraph>>()

function graphFor(client: SubstrateClient): Promise<UnitGraph> {
  let g = graphs.get(client)
  if (!g) {
    g = client
      .json<unknown>("GET", "api/v1/units")
      .then(parseUnitGraph)
      .catch((e: unknown) => {
        graphs.delete(client)
        throw e
      })
    graphs.set(client, g)
  }
  return g
}

function registryFor(client: SubstrateClient): RunRegistry {
  let r = registries.get(client)
  if (!r) {
    r = new RunRegistry(clientStore(client), {
      device: deviceId(),
      lockPrefix: "healthspan.import.",
    })
    registries.set(client, r)
  }
  return r
}

function useLiveRunState(id: string) {
  const { client } = useSubstrate()
  const registry = registryFor(client)
  return useSyncExternalStore(registry.subscribe, () => registry.state(id))
}

function useLiveImportActions(): ImportActions {
  const { client } = useSubstrate()
  const invalidate = useInvalidate()
  return useMemo<ImportActions>(() => {
    const registry = registryFor(client)
    return {
      convert: async (value, from, to) => {
        const v = convertUnit(await graphFor(client), Number(value), from, to)
        return v === undefined ? undefined : String(v)
      },
      held: async (metrics, days) => {
        if (!metrics.length || !days.length) return []
        const sorted = [...days].sort()
        const from = new Date(`${sorted[0]}T00:00:00`)
        from.setDate(from.getDate() - 1)
        const to = new Date(`${sorted[sorted.length - 1]}T00:00:00`)
        to.setDate(to.getDate() + 2)
        try {
          const rows = await listFiltered(client, {
            kinds: [KIND.labresult],
            properties: {
              metric: { in: metrics.map(metricPath) },
              at: { gte: from.toISOString(), lt: to.toISOString() },
            },
          })
          return rows.flatMap((r): HeldResult[] => heldOf(r))
        } catch {
          // Without the read, nothing is marked: every row stays ticked.
          return []
        }
      },
      start: async (planned: PlannedImport[]) => {
        const sources = await listFiltered(client, { kinds: SOURCE_KINDS })
        const ids: string[] = []
        for (const p of planned) {
          const id = uuidv7()
          const source = plannedSource(p.lab, sources)
          await client.put(
            KIND.import,
            id,
            Import.write({
              name: p.name,
              format: p.format,
              lab: p.lab,
              ...(source ? { source } : {}),
              rows: p.rows,
              reports: p.reports,
              results: 0,
              ...(p.skipped ? { skipped: p.skipped } : {}),
            }),
            0
          )
          registry.rememberLab(id, p.lab)
          ids.push(id)
        }
        void (async () => {
          for (const id of ids) await registry.resume(id)
          invalidate([KIND.labresult, KIND.labreport, KIND.source])
        })()
        return ids
      },
      resume: async (id) => {
        const out = await registry.resume(id)
        invalidate([KIND.labresult, KIND.labreport, KIND.source])
        return out
      },
      undo: async (id) => {
        const out = await registry.undo(id)
        invalidate([KIND.labresult, KIND.labreport, KIND.source])
        return out
      },
    }
  }, [client, invalidate])
}

export const liveSourcesData: SourcesData = {
  useSourceList: useLiveSourceList,
  useMetrics: useLiveMetrics,
  useLabMetrics: useLiveLabMetrics,
  useSourceLinks: useLiveSourceLinks,
  useSourceWrites: useLiveSourceWrites,
  useImports: useLiveImports,
  useImport: useLiveImport,
  useStillPointing: useLiveStillPointing,
  useRunState: useLiveRunState,
  useImportActions: useLiveImportActions,
}
